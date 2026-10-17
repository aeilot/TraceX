package experiment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) Start(req Request) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.catalog.Ready {
		return Job{}, ErrNotReady
	}
	if m.active != nil {
		return Job{}, ErrBusy
	}
	if req.Action != "prepare" && req.Action != "run" {
		return Job{}, fmt.Errorf("%w: action must be prepare or run", ErrInvalid)
	}
	if req.Action == "run" && !req.ConfirmModelCalls {
		return Job{}, fmt.Errorf("%w: explicitly acknowledge model calls", ErrInvalid)
	}
	var found *Preset
	for i := range m.catalog.Profiles {
		if m.catalog.Profiles[i].ID == req.ProfileID {
			found = &m.catalog.Profiles[i]
			break
		}
	}
	if found == nil {
		return Job{}, ErrNotFound
	}
	path := filepath.Join(m.cfg.Root, "profiles", filepath.FromSlash(req.ProfileID))
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return Job{}, ErrNotFound
	}
	base := filepath.Join(m.cfg.Root, "profiles") + string(os.PathSeparator)
	if !strings.HasPrefix(real, base) {
		return Job{}, ErrInvalid
	}
	raw, e := readProfile(real)
	if e != nil {
		return Job{}, fmt.Errorf("%w: profile unavailable", ErrInvalid)
	}
	p := preset(req.ProfileID, raw)
	selected := p.Tasks
	if req.Tasks != nil {
		if len(req.Tasks) == 0 {
			return Job{}, fmt.Errorf("%w: task selection is empty", ErrInvalid)
		}
		allowed := map[string]bool{}
		for _, t := range p.Tasks {
			allowed[t] = true
		}
		seen := map[string]bool{}
		for _, t := range req.Tasks {
			if !allowed[t] || seen[t] {
				return Job{}, fmt.Errorf("%w: select unique declared tasks", ErrInvalid)
			}
			seen[t] = true
		}
		if p.ArrivalSchedule && !reflect.DeepEqual(req.Tasks, p.Tasks) {
			return Job{}, fmt.Errorf("%w: arrival profiles require the declared task sequence", ErrInvalid)
		}
		selected = append([]string{}, req.Tasks...)
		obj(raw, "benchmark")["tasks"] = selected
	}
	concurrency := p.Concurrency
	if req.Concurrency < 0 || req.Concurrency > 4096 {
		return Job{}, fmt.Errorf("%w: concurrency must be between 1 and 4096", ErrInvalid)
	}
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
		obj(raw, "execution")["concurrency"] = concurrency
	}
	random := make([]byte, 12)
	if _, e = rand.Read(random); e != nil {
		return Job{}, e
	}
	id := hex.EncodeToString(random)
	dir := filepath.Join(m.cfg.JobsDir, id)
	if e = os.Mkdir(dir, 0700); e != nil {
		return Job{}, e
	}
	artifacts := filepath.Join(dir, "artifacts")
	if e = os.Mkdir(artifacts, 0700); e != nil {
		return Job{}, e
	}
	for _, key := range []string{"versions_file", "overrides_file"} {
		if value := str(raw, key); value != "" {
			if !filepath.IsAbs(value) {
				value = filepath.Join(filepath.Dir(real), value)
			}
			raw[key] = m.executionPath(value)
		}
	}
	runtimeConfig := obj(obj(raw, "runtime"), "config")
	if value := str(runtimeConfig, "file"); value != "" {
		if !filepath.IsAbs(value) {
			value = filepath.Join(filepath.Dir(real), value)
		}
		runtimeConfig["file"] = m.executionPath(value)
	}
	raw["output_dir"] = m.executionPath(artifacts)
	profilePath := filepath.Join(dir, "profile.json")
	b, e := json.MarshalIndent(raw, "", "  ")
	if e != nil {
		return Job{}, e
	}
	if e = os.WriteFile(profilePath, b, 0600); e != nil {
		return Job{}, e
	}
	args := []string{}
	if req.Action == "prepare" {
		args = append(args, "setup")
	}
	args = append(args, profilePath)
	cmd, e := m.command(dir, profilePath, args)
	if e != nil {
		return Job{}, e
	}
	stdout, e := os.OpenFile(filepath.Join(dir, "stdout.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return Job{}, e
	}
	stderr, e := os.OpenFile(filepath.Join(dir, "stderr.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		stdout.Close()
		return Job{}, e
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = m.cfg.Root
	rec := &record{job: Job{ID: id, Action: req.Action, ProfileID: p.ID, Name: p.Name, Workload: p.Workload, Harness: p.Harness, Model: p.Model, Backend: p.Backend, Tasks: selected, Concurrency: concurrency, Status: "running", StartedAt: time.Now().UTC(), CleanupStatus: "pending"}, cmd: cmd, dir: dir, profile: profilePath, done: make(chan struct{})}
	m.jobs = append(m.jobs, rec)
	if e = cmd.Start(); e != nil {
		stdout.Close()
		stderr.Close()
		now := time.Now().UTC()
		rec.job.Status = "failed"
		rec.job.FinishedAt = &now
		rec.job.CleanupStatus = "not_started"
		rec.job.Error = "ARIES process could not start; inspect private job logs"
		close(rec.done)
		return copyJob(rec.job), nil
	}
	m.active = rec
	go m.observe(rec, stdout, stderr)
	return copyJob(rec.job), nil
}
func (m *Manager) executionPath(path string) string {
	if m.cfg.Executor == "orb" {
		return "/mnt/mac" + path
	}
	return path
}
func (m *Manager) command(dir, profile string, args []string) (*exec.Cmd, error) {
	if m.cfg.Executor == "local" {
		return exec.Command(m.cfg.Binary, args...), nil
	}
	// The wrapper records its child's PID and completion. Only fixed text is written; all
	// operator/configuration data travels as positional arguments, never shell code.
	wrapper := filepath.Join(dir, "launch.sh")
	script := "#!/bin/sh\nset -eu\numask 077\npidfile=$1\nexitfile=$2\nshift 2\n\"$@\" &\npid=$!\nprintf '%s\\n' \"$pid\" > \"$pidfile\"\nset +e\nwait \"$pid\"\nstatus=$?\nprintf '%s\\n' \"$status\" > \"$exitfile\"\nexit \"$status\"\n"
	if e := os.WriteFile(wrapper, []byte(script), 0600); e != nil {
		return nil, e
	}
	prefix := []string{"-m", m.cfg.Machine, "-w", m.executionPath(m.cfg.Root), "/bin/sh", m.executionPath(wrapper), m.executionPath(filepath.Join(dir, "pid")), m.executionPath(filepath.Join(dir, "exit")), m.executionPath(m.cfg.Binary)}
	for _, arg := range args {
		if filepath.IsAbs(arg) {
			arg = m.executionPath(arg)
		}
		prefix = append(prefix, arg)
	}
	return exec.Command("orb", prefix...), nil
}
func (m *Manager) observe(rec *record, stdout, stderr *os.File) {
	finished := make(chan error, 1)
	go func() { finished <- rec.cmd.Wait() }()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-finished:
			stdout.Close()
			stderr.Close()
			if m.cfg.Executor == "orb" {
				if _, markerErr := os.Stat(filepath.Join(rec.dir, "exit")); markerErr != nil {
					m.mu.Lock()
					rec.job.Status = "stop_failed"
					rec.job.Error = "OrbStack transport ended without ARIES completion; process remains owned"
					m.mu.Unlock()
					// Keep the live Linux child owned until its wrapper records
					// completion. Transport closure is not runtime termination.
					for {
						time.Sleep(300 * time.Millisecond)
						if _, markerErr = os.Stat(filepath.Join(rec.dir, "exit")); markerErr == nil {
							break
						}
						m.attach(rec)
					}
				}
			}
			m.attach(rec)
			m.mu.Lock()
			now := time.Now().UTC()
			rec.job.FinishedAt = &now
			code := rec.cmd.ProcessState.ExitCode()
			if m.cfg.Executor == "orb" {
				if b, e := os.ReadFile(filepath.Join(rec.dir, "exit")); e == nil {
					if n, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil {
						code = n
					}
				}
			}
			rec.job.ExitCode = &code
			if rec.stopRequested {
				rec.job.Status = "cancelled"
			} else if code != 0 || (err != nil && m.cfg.Executor != "orb") {
				rec.job.Status = "failed"
				rec.job.Error = "ARIES process exited unsuccessfully; inspect private job logs"
			} else if rec.job.Status == "stopping" || rec.job.Status == "stop_failed" {
				rec.job.Status = "cancelled"
			} else {
				rec.job.Status = "completed"
			}
			m.inspectResult(rec)
			if rec.job.Action == "prepare" {
				rec.job.CleanupStatus = "not_applicable"
			}
			m.active = nil
			close(rec.done)
			m.mu.Unlock()
			return
		case <-ticker.C:
			m.attach(rec)
		}
	}
}
func (m *Manager) attach(rec *record) {
	m.mu.Lock()
	attached := rec.job.RunID != ""
	m.mu.Unlock()
	if m.cfg.OnArtifacts == nil || attached {
		return
	}
	entries, e := os.ReadDir(filepath.Join(rec.dir, "artifacts"))
	if e != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(rec.dir, "artifacts", entry.Name())
		m.mu.Lock()
		already := rec.attached == path
		m.mu.Unlock()
		if already {
			continue
		}
		id, e := m.cfg.OnArtifacts(rec.job.ID, path)
		if e != nil {
			continue
		}
		m.mu.Lock()
		rec.attached = path
		rec.job.RunID = id
		m.mu.Unlock()
		return
	}
}
func (m *Manager) inspectResult(rec *record) {
	rec.job.CleanupStatus = "unconfirmed"
	entries, _ := os.ReadDir(filepath.Join(rec.dir, "artifacts"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(rec.dir, "artifacts", entry.Name(), "run-result.json"))
		if e != nil || len(b) > 32<<20 {
			continue
		}
		var raw map[string]any
		if json.Unmarshal(b, &raw) != nil {
			continue
		}
		summarizeResult(&rec.job, raw)
		return
	}
}
func (m *Manager) Stop(id string) (Job, error) {
	m.mu.Lock()
	var rec *record
	for _, r := range m.jobs {
		if r.job.ID == id {
			rec = r
			break
		}
	}
	if rec == nil {
		m.mu.Unlock()
		return Job{}, ErrNotFound
	}
	if m.active != rec {
		j := copyJob(rec.job)
		m.mu.Unlock()
		return j, nil
	}
	if rec.job.Status == "stopping" {
		j := copyJob(rec.job)
		m.mu.Unlock()
		return j, nil
	}
	rec.job.Status = "stopping"
	rec.stopRequested = true
	j := copyJob(rec.job)
	m.mu.Unlock()
	go func() {
		err := m.interrupt(rec)
		m.mu.Lock()
		if err != nil && m.active == rec {
			rec.job.Status = "stop_failed"
			rec.job.Error = "Could not confirm delivery of cancellation; process remains owned"
		}
		m.mu.Unlock()
		if err == nil {
			m.stopDeadline(rec)
		}
	}()
	return j, nil
}

func (m *Manager) stopDeadline(rec *record) {
	timer := time.NewTimer(m.cfg.StopTimeout)
	defer timer.Stop()
	select {
	case <-rec.done:
		return
	case <-timer.C:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active == rec {
			rec.job.Status = "stop_failed"
			rec.job.Error = "Cleanup grace period exceeded; process remains owned; no forced kill was sent"
		}
	}
}
func (m *Manager) interrupt(rec *record) error {
	if m.cfg.Executor == "local" {
		return rec.cmd.Process.Signal(os.Interrupt)
	}
	b, e := os.ReadFile(filepath.Join(rec.dir, "pid"))
	if e != nil {
		return e
	}
	pid, e := strconv.Atoi(strings.TrimSpace(string(b)))
	if e != nil || pid <= 1 {
		return fmt.Errorf("invalid owned PID")
	}
	// Validate the unique private profile argument in the live Linux cmdline
	// before signaling. Refuse PID reuse and never signal the transport process.
	script := `set -eu; pid=$1; profile=$2; test -r "/proc/$pid/cmdline"; tr '\000' '\n' < "/proc/$pid/cmdline" | grep -Fqx -- "$profile"; kill -INT "$pid"`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "orb", "-m", m.cfg.Machine, "/bin/sh", "-c", script, "tracex-cancel", strconv.Itoa(pid), m.executionPath(rec.profile))
	return cmd.Run()
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	rec := m.active
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if rec == nil {
		return nil
	}
	_, _ = m.Stop(rec.job.ID)
	timer := time.NewTimer(m.cfg.StopTimeout)
	defer timer.Stop()
	select {
	case <-rec.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("cleanup timeout: ARIES process remains owned")
	}
}
