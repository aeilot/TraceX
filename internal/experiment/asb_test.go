package experiment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeDocker emulates the docker CLI calls the AgentSysBench driver makes. The
// loadgen reports running until the "loadgen-done" file exists in state.
const fakeDocker = `#!/bin/sh
state=$FAKE_DOCKER_STATE
echo "$*" >> "$state/calls"
case "$1" in
image) exit 0 ;;
ps) echo "abc123 browsergym-loadgen" ;;
inspect)
  case "$3" in
  *State.Running*) if [ -f "$state/loadgen-done" ]; then echo false; else echo true; fi ;;
  *State.Status*) echo running ;;
  *ExitCode*) echo 3 ;;
  esac ;;
logs) echo loadgen-log ;;
run)
  for a in "$@"; do
    if [ "$next" = convert ]; then
      mkdir -p "$(dirname "$a")"
      printf '%s' '{"tasks":[{"status":"succeeded","evaluation_status":"passed"},{"status":"succeeded","evaluation_status":"failed"}]}' > "$a"
      touch "$state/loadgen-done"; next=; break
    fi
    if [ "$next" = copy ]; then next=dest; continue; fi
    if [ "$next" = dest ]; then mkdir -p "$a"; next=; break; fi
    [ "$a" = tracex-convert ] && next=convert
    [ "$a" = tracex-copy ] && next=copy
  done ;;
esac
exit 0
`

func asbFixture(t *testing.T) (Config, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	deploy := filepath.Join(root, "benchmark", "deployment", "browser-use-webagent")
	scripts := filepath.Join(deploy, "scripts")
	for _, d := range []string{scripts, filepath.Join(root, "benchmark", "deployment", "qa-rag", "scripts"), filepath.Join(root, "agents", "webagent", "config"), filepath.Join(root, "request", "scripts")} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	write := func(path, text string) {
		if e := os.WriteFile(path, []byte(text), 0700); e != nil {
			t.Fatal(e)
		}
	}
	write(filepath.Join(root, ".env"), "AGSBENCH_HOME="+root+"\n")
	write(filepath.Join(root, "agents", "webagent", "config", ".env"), "OPENAI_API_KEY=SECRET_TEST_PROVIDER_CREDENTIAL\nOPENAI_BASE_URL=https://example.invalid/v1\nWEBAGENT_MODEL=deepseek-flash\n")
	write(filepath.Join(deploy, "docker-compose.yaml"), "name: webagent-benchmarking\nservices: {}\n")
	write(filepath.Join(scripts, "1.prepare_data.sh"), "touch \"$FAKE_DOCKER_STATE/prepared\"\n")
	write(filepath.Join(scripts, "2.run.sh"), "echo \"$WEBAGENT_REQUEST_NUM $WEBAGENT_CONCURRENCY\" > \"$FAKE_DOCKER_STATE/run.env\"\n")
	write(filepath.Join(scripts, "3.cleanup.sh"), "touch \"$FAKE_DOCKER_STATE/cleaned\"\n")
	write(filepath.Join(scripts, "4.evaluate.sh"), "echo \"$PREDICTIONS_PATH\" > \"$FAKE_DOCKER_STATE/evaluated\"\n")
	bin := t.TempDir()
	write(filepath.Join(bin, "docker"), fakeDocker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_STATE", state)
	t.Setenv("TRACEX_ASB_POLL", "0.2")
	t.Setenv("TRACEX_ASB_TAIL", "0")
	return Config{Backend: BackendASB, Root: root, Executor: "local", StopTimeout: 20 * time.Second}, state
}

// readyManager configures an ASB runner and, off Linux, lifts the Linux-only
// readiness gate so the real driver can run against the fake docker CLI.
func readyManager(t *testing.T, c Config) *Manager {
	t.Helper()
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	m.catalog.Ready = true
	t.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func TestASBCatalogReadsDeploymentsWithoutCredentials(t *testing.T) {
	c, _ := asbFixture(t)
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	cat := m.Catalog()
	if cat.Backend != BackendASB || len(cat.Profiles) != 2 {
		t.Fatalf("catalog %+v", cat)
	}
	var web, rag Preset
	for _, p := range cat.Profiles {
		switch p.ID {
		case "browser-use-webagent":
			web = p
		case "qa-rag":
			rag = p
		}
	}
	if web.Model != "deepseek-flash" || !web.RequestCount || web.ConcurrencyFixed || !rag.ConcurrencyFixed {
		t.Fatalf("presets %+v %+v", web, rag)
	}
	b, _ := os.ReadFile(filepath.Join(c.Root, "agents", "webagent", "config", ".env"))
	if strings.Contains(web.Model+web.Name, "SECRET") || !strings.Contains(string(b), "SECRET") {
		t.Fatal("credential leaked into catalog")
	}
	if _, e = configured(Config{Backend: BackendASB, Root: t.TempDir(), Executor: "local"}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("non-ASB directory accepted: %v", e)
	}
	if _, e = configured(Config{Backend: "other", Root: c.Root, Executor: "local"}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("unknown backend accepted: %v", e)
	}
}

func TestASBRunDrivesDeploymentAndPublishesProfile(t *testing.T) {
	c, state := asbFixture(t)
	mounted := make(chan string, 4)
	c.OnArtifacts = func(job, path string) (string, error) { mounted <- path; return "run-asb", nil }
	m := readyManager(t, c)
	if _, e := m.Start(Request{ProfileID: "browser-use-webagent", Action: "run", RequestNum: 0, ConfirmModelCalls: true}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("zero requests accepted: %v", e)
	}
	if _, e := m.Start(Request{ProfileID: "browser-use-webagent", Action: "run", RequestNum: 4}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("missing acknowledgment accepted: %v", e)
	}
	j, e := m.Start(Request{ProfileID: "browser-use-webagent", Action: "run", RequestNum: 4, Concurrency: 2, ConfirmModelCalls: true})
	if e != nil {
		t.Fatal(e)
	}
	if j.RequestNum != 4 || j.Concurrency != 2 || j.Backend != BackendASB || j.Model != "deepseek-flash" {
		t.Fatalf("job %+v", j)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "failed" || j.ExitCode == nil || *j.ExitCode != 3 {
		t.Fatalf("loadgen exit code not propagated: %+v", j)
	}
	if j.CleanupStatus != "confirmed" || j.HarnessStatus != "succeeded" || j.EvaluationStatus != "mixed" || j.RunID != "run-asb" {
		t.Fatalf("outcomes %+v", j)
	}
	select {
	case path := <-mounted:
		if filepath.Base(path) != "profile.json" || !strings.Contains(path, "browser-use-webagent-n4-c2") {
			t.Fatalf("mounted %s", path)
		}
	default:
		t.Fatal("profile was never mounted")
	}
	env, _ := os.ReadFile(filepath.Join(state, "run.env"))
	if strings.TrimSpace(string(env)) != "4 2" {
		t.Fatalf("deployment saw %q", env)
	}
	evaluated, _ := os.ReadFile(filepath.Join(state, "evaluated"))
	if !strings.HasSuffix(strings.TrimSpace(string(evaluated)), "benchmark/data/browser-use-webagent/trajectories/preds.json") {
		t.Fatalf("evaluate got %q", evaluated)
	}
	if _, e = os.Stat(filepath.Join(state, "cleaned")); e != nil {
		t.Fatal("3.cleanup.sh did not run")
	}
	calls, _ := os.ReadFile(filepath.Join(state, "calls"))
	if !strings.Contains(string(calls), "--archive") {
		t.Fatal("final conversion did not archive the source tables")
	}
}

func TestASBCancellationRunsCleanup(t *testing.T) {
	c, state := asbFixture(t)
	// Keep the loadgen running: conversion must not mark it done.
	bin := filepath.Dir(lookPathFake(t))
	script := strings.Replace(fakeDocker, `touch "$state/loadgen-done"; `, "", 1)
	if e := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	m := readyManager(t, c)
	j, e := m.Start(Request{ProfileID: "browser-use-webagent", Action: "run", RequestNum: 1, ConfirmModelCalls: true})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, _ := os.ReadFile(filepath.Join(state, "calls")); strings.Contains(string(b), "tracex-convert") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("driver never reached the conversion loop")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, e = m.Stop(j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "cancelled" || j.CleanupStatus != "confirmed" {
		t.Fatalf("cancel %+v", j)
	}
	if _, e = os.Stat(filepath.Join(state, "cleaned")); e != nil {
		t.Fatal("cancellation skipped 3.cleanup.sh")
	}
}

func TestASBPrepareBuildsAndPreparesWithoutModelCalls(t *testing.T) {
	c, state := asbFixture(t)
	for _, d := range []string{filepath.Join(c.Root, "agents", "webagent", "scripts"), filepath.Join(c.Root, "request", "scripts")} {
		os.MkdirAll(d, 0700)
		os.WriteFile(filepath.Join(d, "build_image.sh"), []byte("echo \"$PWD\" >> \"$FAKE_DOCKER_STATE/built\"\n"), 0700)
	}
	m := readyManager(t, c)
	j, e := m.Start(Request{ProfileID: "browser-use-webagent", Action: "prepare"})
	if e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "completed" || j.CleanupStatus != "not_applicable" {
		t.Fatalf("prepare %+v", j)
	}
	built, _ := os.ReadFile(filepath.Join(state, "built"))
	if !strings.Contains(string(built), "/request") || !strings.Contains(string(built), "/agents/webagent") {
		t.Fatalf("built %q", built)
	}
	if _, e = os.Stat(filepath.Join(state, "prepared")); e != nil {
		t.Fatal("1.prepare_data.sh did not run")
	}
}

func lookPathFake(t *testing.T) string {
	t.Helper()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if p := filepath.Join(dir, "docker"); fileExists(p) {
			return p
		}
	}
	t.Fatal("fake docker missing")
	return ""
}

func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
