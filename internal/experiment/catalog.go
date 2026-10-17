package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxProfileBytes = 2 << 20

func New(ctx context.Context, c Config) (*Manager, error) {
	m := &Manager{cfg: c}
	m.catalog = Catalog{Profiles: []Preset{}, Executor: c.Executor, Machine: c.Machine}
	if c.Root == "" {
		return m, nil
	}
	root, e := filepath.Abs(c.Root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	m.cfg.Root = root
	if c.Executor == "" {
		m.cfg.Executor = "local"
	}
	m.catalog.Executor = m.cfg.Executor
	m.catalog.Enabled = true
	if c.Binary == "" {
		m.cfg.Binary = filepath.Join(root, "bin", "aries")
	} else if !filepath.IsAbs(c.Binary) {
		m.cfg.Binary = filepath.Join(root, c.Binary)
	}
	if c.JobsDir == "" {
		m.cfg.JobsDir = filepath.Join(root, ".cache", "tracex", "jobs")
	}
	if !filepath.IsAbs(m.cfg.JobsDir) {
		m.cfg.JobsDir = filepath.Join(root, m.cfg.JobsDir)
	}
	if m.cfg.StopTimeout == 0 {
		m.cfg.StopTimeout = 2 * time.Minute
	}
	if e = os.MkdirAll(m.cfg.JobsDir, 0700); e != nil {
		return nil, e
	}
	if e = os.Chmod(m.cfg.JobsDir, 0700); e != nil {
		return nil, e
	}
	m.catalog.Ready = true
	binaryFile, openErr := os.Open(m.cfg.Binary)
	b := make([]byte, 4)
	e = openErr
	if openErr == nil {
		_, e = io.ReadFull(binaryFile, b)
		binaryFile.Close()
	}
	if e != nil {
		m.catalog.Ready = false
		m.catalog.Message = "ARIES binary is missing or unreadable"
	} else if m.cfg.Executor == "local" && runtime.GOOS == "darwin" && string(b[:4]) == "\x7fELF" {
		m.catalog.Ready = false
		m.catalog.Message = "Linux ARIES binary requires an explicitly configured OrbStack machine"
	}
	if info, err := os.Stat(m.cfg.Binary); err == nil && info.Mode().Perm()&0111 == 0 {
		m.catalog.Ready = false
		m.catalog.Message = "ARIES binary is not executable"
	}
	if m.cfg.Executor == "local" && runtime.GOOS == "linux" && (string(b) == "\xcf\xfa\xed\xfe" || string(b) == "\xfe\xed\xfa\xcf" || string(b) == "\xca\xfe\xba\xbe") {
		m.catalog.Ready = false
		m.catalog.Message = "macOS ARIES binary cannot run on this Linux host"
	}
	if m.cfg.Executor == "orb" {
		if c.Machine == "" {
			m.catalog.Ready = false
			m.catalog.Message = "OrbStack machine must be explicit"
		}
		if _, e = exec.LookPath("orb"); e != nil {
			m.catalog.Ready = false
			m.catalog.Message = "OrbStack executable is unavailable"
		}
	} else if m.cfg.Executor != "local" {
		return nil, fmt.Errorf("%w: unknown executor", ErrInvalid)
	}
	base := filepath.Join(root, "profiles")
	e = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		raw, err := readProfile(path)
		if err != nil {
			return nil
		}
		if str(obj(raw, "benchmark"), "type") == "" || str(obj(raw, "harness"), "type") == "" || str(obj(raw, "runtime"), "backend") == "" {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		m.catalog.Profiles = append(m.catalog.Profiles, preset(filepath.ToSlash(rel), raw))
		return nil
	})
	if e != nil {
		return nil, e
	}
	watch, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	go func() { <-watch.Done(); _ = m.Close(context.Background()) }()
	return m, nil
}
func readProfile(path string) (map[string]any, error) {
	s, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > maxProfileBytes {
		return nil, fmt.Errorf("invalid profile file")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var raw map[string]any
	e = json.Unmarshal(b, &raw)
	return raw, e
}
func obj(raw map[string]any, key string) map[string]any {
	if v, ok := raw[key].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	raw[key] = v
	return v
}
func str(raw map[string]any, key string) string { v, _ := raw[key].(string); return v }
func tasks(raw map[string]any) []string {
	out := []string{}
	for _, v := range arr(obj(raw, "benchmark")["tasks"]) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func arr(v any) []any { a, _ := v.([]any); return a }
func preset(id string, raw map[string]any) Preset {
	c, _ := obj(raw, "execution")["concurrency"].(float64)
	if c == 0 {
		c = 1
	}
	return Preset{ID: id, Name: str(raw, "name"), Workload: str(obj(raw, "benchmark"), "type"), Harness: str(obj(raw, "harness"), "type"), Model: str(obj(raw, "model"), "id"), Backend: str(obj(raw, "runtime"), "backend"), Tasks: tasks(raw), Concurrency: int(c), LoopDuration: str(obj(raw, "execution"), "loop_duration"), ArrivalSchedule: str(obj(raw, "execution"), "arrivals_file") != ""}
}
func (m *Manager) Catalog() Catalog {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.catalog
	c.Profiles = append([]Preset{}, c.Profiles...)
	for i := range c.Profiles {
		c.Profiles[i].Tasks = append([]string{}, c.Profiles[i].Tasks...)
	}
	return c
}
func (m *Manager) Jobs() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for i := len(m.jobs) - 1; i >= 0; i-- {
		out = append(out, copyJob(m.jobs[i].job))
	}
	return out
}
func copyJob(j Job) Job { j.Tasks = append([]string{}, j.Tasks...); return j }
