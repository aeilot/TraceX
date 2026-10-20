package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxProfileBytes = 2 << 20

func New(ctx context.Context, c Config) (*Manager, error) {
	m, err := configured(c)
	if err != nil {
		return nil, err
	}
	watch, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	go func() { <-watch.Done(); _ = m.Close(context.Background()) }()
	return m, nil
}

func configured(c Config) (*Manager, error) {
	if c.Backend == "" {
		c.Backend = BackendARIES
	}
	if c.Backend != BackendARIES && c.Backend != BackendASB {
		return nil, fmt.Errorf("%w: unknown backend", ErrInvalid)
	}
	m := &Manager{cfg: c}
	m.catalog = Catalog{Backend: c.Backend, Profiles: []Preset{}, Executor: c.Executor, Machine: c.Machine}
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
	if c.Backend == BackendASB && !isASBRoot(root) {
		return nil, fmt.Errorf("%w: AgentSysBench directory must contain benchmark/deployment", ErrInvalid)
	}
	if c.Executor == "" {
		m.cfg.Executor = "local"
	}
	m.catalog.Executor = m.cfg.Executor
	m.catalog.Enabled = true
	if c.Backend == BackendASB {
		m.cfg.Binary = ""
	} else if c.Binary == "" {
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
	if c.Backend == BackendASB {
		if m.cfg.Executor == "local" && runtime.GOOS != "linux" {
			m.catalog.Ready = false
			m.catalog.Message = "AgentSysBench runs Docker deployments on Linux; choose an OrbStack machine"
		}
	} else {
		m.checkBinary()
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
	if c.Backend == BackendASB {
		m.catalog.Profiles = asbCatalog(root)
		return m, nil
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
	return m, nil
}

// checkBinary marks an ARIES runner unready when its binary cannot run on the target.
func (m *Manager) checkBinary() {
	binaryFile, openErr := os.Open(m.cfg.Binary)
	b := make([]byte, 4)
	e := openErr
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
	model := obj(raw, "model")
	settings := ModelSettings{ID: str(model, "id"), BaseURL: str(model, "base_url"), APIKeyEnv: str(model, "api_key_env"), ReasoningEffort: str(model, "reasoning_effort")}
	// Display only endpoint identity, never URL credentials or query parameters.
	if u, err := url.Parse(settings.BaseURL); err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		settings.BaseURL = ""
	}

	c, _ := obj(raw, "execution")["concurrency"].(float64)
	if c == 0 {
		c = 1
	}
	return Preset{ModelSettings: settings, ModelEditable: str(obj(raw, "runtime"), "mode") == "external", ID: id, Name: str(raw, "name"), Workload: str(obj(raw, "benchmark"), "type"), Harness: str(obj(raw, "harness"), "type"), Model: str(obj(raw, "model"), "id"), Backend: str(obj(raw, "runtime"), "backend"), Tasks: tasks(raw), Concurrency: int(c), LoopDuration: str(obj(raw, "execution"), "loop_duration"), ArrivalSchedule: str(obj(raw, "execution"), "arrivals_file") != ""}
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
