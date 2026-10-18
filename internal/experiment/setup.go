package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Settings contains operator-selected paths, never provider credentials.
type Settings struct {
	Root     string `json:"root"`
	Binary   string `json:"binary"`
	Executor string `json:"executor"`
	Machine  string `json:"machine"`
}
type ModelSettings struct {
	ID              string `json:"id"`
	BaseURL         string `json:"base_url"`
	APIKeyEnv       string `json:"api_key_env"`
	ReasoningEffort string `json:"reasoning_effort"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validModel(s ModelSettings) bool {
	u, err := url.Parse(s.BaseURL)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.TrimSpace(s.ID) != "" && envName.MatchString(s.APIKeyEnv) && len(s.ID) <= 256 && len(s.ReasoningEffort) <= 64
}
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Settings{m.cfg.Root, m.cfg.Binary, m.cfg.Executor, m.cfg.Machine}
}

// Configure serializes with Start. Running jobs retain their execution target and ownership.
func (m *Manager) Configure(s Settings, savePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrNotReady
	}
	if m.active != nil {
		return ErrBusy
	}
	if !filepath.IsAbs(s.Root) || (s.Executor != "local" && s.Executor != "orb") || (s.Executor == "orb" && (s.Machine == "" || strings.HasPrefix(s.Machine, "-"))) || (s.Executor == "local" && s.Machine != "") {
		return fmt.Errorf("%w: choose an absolute ARIES directory and an execution target", ErrInvalid)
	}
	jobsDir := ""
	if root, err := filepath.EvalSymlinks(s.Root); err == nil && root == m.cfg.Root {
		jobsDir = m.cfg.JobsDir
	}
	next, err := configured(Config{JobsDir: jobsDir, Root: s.Root, Binary: s.Binary, Executor: s.Executor, Machine: s.Machine, StopTimeout: m.cfg.StopTimeout, OnArtifacts: m.cfg.OnArtifacts})
	if err != nil {
		return fmt.Errorf("%w: ARIES directory must contain readable profiles", ErrInvalid)
	}
	if savePath != "" {
		s = Settings{next.cfg.Root, next.cfg.Binary, next.cfg.Executor, next.cfg.Machine}
		b, _ := json.MarshalIndent(s, "", "  ")
		if err = os.MkdirAll(filepath.Dir(savePath), 0700); err != nil {
			return fmt.Errorf("cannot save runner settings")
		}
		f, e := os.CreateTemp(filepath.Dir(savePath), ".runner-*")
		if e != nil {
			return fmt.Errorf("cannot save runner settings")
		}
		name := f.Name()
		defer os.Remove(name)
		_, err = f.Write(b)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return fmt.Errorf("cannot save runner settings")
		}
		if err = os.Rename(name, savePath); err != nil {
			return fmt.Errorf("cannot save runner settings")
		}
	}
	m.cfg, m.catalog = next.cfg, next.catalog
	return nil
}

type Discovery struct {
	Roots        []string `json:"roots"`
	Machines     []string `json:"machines"`
	Platform     string   `json:"platform"`
	OrbAvailable bool     `json:"orb_available"`
	Message      string   `json:"message,omitempty"`
}

// Discover searches only conventional nearby locations; it never walks the user's disk.
func Discover(ctx context.Context, current string) Discovery {
	d := Discovery{Roots: []string{}, Machines: []string{}, Platform: runtime.GOOS}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	for _, p := range []string{current, os.Getenv("ARIES_DIR"), filepath.Join(cwd, "ARIES"), filepath.Join(filepath.Dir(cwd), "ARIES"), filepath.Join(home, "Developer", "ARIES"), filepath.Join(home, "Developer", "research", "ARIES")} {
		if p == "" {
			continue
		}
		p, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		p, err = filepath.EvalSymlinks(p)
		if err != nil || seen[p] {
			continue
		}
		info, err := os.Stat(filepath.Join(p, "profiles"))
		if err == nil && info.IsDir() {
			d.Roots = append(d.Roots, p)
			seen[p] = true
		}
	}
	if _, err := exec.LookPath("orb"); err != nil {
		return d
	}
	d.OrbAvailable = true
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Quiet output contains one machine name per line (no terminal table parsing).
	cmd := exec.CommandContext(probeCtx, "orb", "list", "-q")
	cmd.WaitDelay = 250 * time.Millisecond
	b, err := cmd.Output()
	if err != nil {
		d.Message = "OrbStack machine discovery failed or timed out. Start OrbStack or enter the machine name manually."
		return d
	}
	for _, line := range strings.Split(string(b), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !strings.ContainsAny(name, " \t\r") && !strings.HasPrefix(name, "-") {
			d.Machines = append(d.Machines, name)
		}
	}
	return d
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (m *Manager) Check(ctx context.Context) []Check {
	m.mu.Lock()
	cfg, catalog := m.cfg, m.catalog
	m.mu.Unlock()
	checks := []Check{{"binary", catalog.Ready, catalog.Message}}
	if !catalog.Enabled {
		return []Check{{"runner", false, "Save an ARIES directory and execution target first."}}
	}
	probe := func(name string, args ...string) {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var cmd *exec.Cmd
		if cfg.Executor == "orb" {
			prefix := []string{"-m", cfg.Machine, "-w", "/mnt/mac" + cfg.Root}
			cmd = exec.CommandContext(checkCtx, "orb", append(prefix, args...)...)
		} else {
			cmd = exec.CommandContext(checkCtx, args[0], args[1:]...)
			cmd.Dir = cfg.Root
		}
		// Discard tool output: diagnostics expose only success/failure, never environment values.
		err := cmd.Run()
		message := "Available on execution host"
		if err != nil {
			message = "Unavailable or timed out on execution host"
		}
		checks = append(checks, Check{name, err == nil, message})
	}
	probe("linux", "/bin/sh", "-c", `test "$(uname -s)" = Linux`)
	binary := cfg.Binary
	if cfg.Executor == "orb" {
		binary = "/mnt/mac" + binary
	}
	probe("shared binary", "test", "-x", binary)
	probe("docker", "docker", "info", "--format", "{{.ServerVersion}}")
	// Check only named variables. Values and arbitrary profile fields never leave the host.
	names := map[string]bool{}
	for _, p := range catalog.Profiles {
		if envName.MatchString(p.ModelSettings.APIKeyEnv) {
			names[p.ModelSettings.APIKeyEnv] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		probe("credential: "+name, "/bin/sh", "-c", `printenv "$1" >/dev/null && test -n "$(printenv "$1")"`, "tracex-check", name)
	}
	return checks
}
