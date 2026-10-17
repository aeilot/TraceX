package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("TRACEX_EXPERIMENT_HELPER") == "1" {
		helper()
		return
	}
	os.Exit(m.Run())
}
func helper() {
	path := os.Args[len(os.Args)-1]
	b, e := os.ReadFile(path)
	if e != nil {
		os.Exit(2)
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "SECRET_TEST_PROVIDER_CREDENTIAL")
	fmt.Fprintln(os.Stderr, "SECRET_TEST_PROVIDER_CREDENTIAL")
	if len(os.Args) > 2 && os.Args[1] == "setup" {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	dir := filepath.Join(str(raw, "output_dir"), "synthetic-run")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600)
	if str(raw, "name") == "wait" || str(raw, "name") == "slow" {
		<-sig
		if str(raw, "name") == "slow" {
			time.Sleep(300 * time.Millisecond)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "run-result.json"), []byte(`{"tasks":[{"harness":{"status":"failed","error":"SECRET_TEST_PROVIDER_CREDENTIAL"},"evaluation":{"status":"succeeded"},"cleanup":{"status":"succeeded"}}],"infrastructure":{"cleanup":{"status":"succeeded"}}}`), 0600)
}
func fixture(t *testing.T, name string) (Config, string) {
	t.Helper()
	t.Setenv("TRACEX_EXPERIMENT_HELPER", "1")
	root := t.TempDir()
	dir := filepath.Join(root, "profiles", "nested")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	profile := `{"name":"` + name + `","versions_file":"../../configs/versions.json","overrides_file":"override.json","benchmark":{"type":"terminalbench2","root":".cache/data","tasks":["a","b"]},"harness":{"type":"hermes"},"model":{"id":"test","api_key":"SECRET_TEST_PROVIDER_CREDENTIAL"},"runtime":{"backend":"test","config":{"file":"runtime.yaml"}},"execution":{"concurrency":2}}`
	path := filepath.Join(dir, "test.json")
	if e := os.WriteFile(path, []byte(profile), 0600); e != nil {
		t.Fatal(e)
	}
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return Config{Root: root, Binary: binary, StopTimeout: 2 * time.Second}, path
}
func waitFinished(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.Jobs() {
			if j.ID == id && j.FinishedAt != nil {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func waitReady(t *testing.T, m *Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, e := os.Stat(filepath.Join(m.cfg.JobsDir, id, "artifacts", "synthetic-run", "ready"))
		if e == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper did not become ready")
}
func TestProfileSelectionPrivacyAndPaths(t *testing.T) {
	c, _ := fixture(t, "immediate")
	attached := make(chan string, 2)
	c.OnArtifacts = func(id, path string) (string, error) { attached <- path; return "mounted-" + id, nil }
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	cat := m.Catalog()
	if !cat.Ready || len(cat.Profiles) != 1 || cat.Profiles[0].ID != "nested/test.json" {
		t.Fatalf("catalog %#v", cat)
	}
	for _, req := range []Request{{ProfileID: "nested/test.json", Action: "run"}, {ProfileID: "nested/test.json", Action: "prepare", Tasks: []string{"not-declared"}}, {ProfileID: "nested/test.json", Action: "prepare", Tasks: []string{"a", "a"}}, {ProfileID: "nested/test.json", Action: "prepare", Tasks: []string{}}, {ProfileID: "nested/test.json", Action: "prepare", Concurrency: -1}} {
		if _, e = m.Start(req); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted %#v: %v", req, e)
		}
	}
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "run", ConfirmModelCalls: true, Tasks: []string{"b"}, Concurrency: 3})
	if e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "completed" || j.CleanupStatus != "confirmed" || j.HarnessStatus != "failed" || j.EvaluationStatus != "succeeded" || j.RunID == "" {
		t.Fatalf("outcomes %#v", j)
	}
	b, _ := json.Marshal(struct {
		Catalog Catalog
		Jobs    []Job
	}{m.Catalog(), m.Jobs()})
	if strings.Contains(string(b), "SECRET_TEST") || strings.Contains(string(b), c.Root) {
		t.Fatal("public data leaked private values")
	}
	raw, e := readProfile(filepath.Join(m.cfg.JobsDir, j.ID, "profile.json"))
	if e != nil {
		t.Fatal(e)
	}
	if str(raw, "versions_file") != filepath.Join(m.cfg.Root, "configs", "versions.json") || str(obj(raw, "benchmark"), "root") != ".cache/data" || str(obj(obj(raw, "runtime"), "config"), "file") != filepath.Join(m.cfg.Root, "profiles", "nested", "runtime.yaml") {
		t.Fatalf("path semantics %#v", raw)
	}
	for _, name := range []string{"profile.json", "stdout.log", "stderr.log"} {
		info, e := os.Stat(filepath.Join(m.cfg.JobsDir, j.ID, name))
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private file %s %v", name, e)
		}
	}
	select {
	case <-attached:
	default:
		t.Fatal("artifact callback missing")
	}
}
func TestCancellationAndBusyPreserveCleanup(t *testing.T) {
	c, _ := fixture(t, "wait")
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "run", ConfirmModelCalls: true})
	if e != nil {
		t.Fatal(e)
	}
	waitReady(t, m, j.ID)
	if _, e = m.Start(Request{ProfileID: "nested/test.json", Action: "prepare"}); !errors.Is(e, ErrBusy) {
		t.Fatalf("busy error %v", e)
	}
	if _, e = m.Stop(j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "cancelled" || j.CleanupStatus != "confirmed" {
		t.Fatalf("cleanup %#v", j)
	}
}
func TestShutdownTimeoutKeepsOwnership(t *testing.T) {
	c, _ := fixture(t, "slow")
	c.StopTimeout = 20 * time.Millisecond
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "run", ConfirmModelCalls: true})
	if e != nil {
		t.Fatal(e)
	}
	waitReady(t, m, j.ID)
	if e = m.Close(context.Background()); e == nil {
		t.Fatal("expected truthful timeout")
	}
	m.mu.Lock()
	active := m.active != nil
	m.mu.Unlock()
	if !active {
		t.Fatal("ownership discarded while helper cleaning")
	}
	j = waitFinished(t, m, j.ID)
	if j.CleanupStatus != "confirmed" {
		t.Fatalf("cleanup %#v", j)
	}
}
func TestArrivalSelectionAndSymlinks(t *testing.T) {
	c, path := fixture(t, "immediate")
	raw, e := readProfile(path)
	if e != nil {
		t.Fatal(e)
	}
	obj(raw, "execution")["arrivals_file"] = "arrivals.json"
	b, _ := json.Marshal(raw)
	_ = os.WriteFile(path, b, 0600)
	_ = os.Symlink(path, filepath.Join(c.Root, "profiles", "alias.json"))
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	if len(m.Catalog().Profiles) != 1 {
		t.Fatal("followed symlink")
	}
	if _, e = m.Start(Request{ProfileID: "nested/test.json", Action: "prepare", Tasks: []string{"b"}}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("arrival subset %v", e)
	}
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "prepare", Tasks: []string{"a", "b"}})
	if e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.CleanupStatus != "not_applicable" {
		t.Fatalf("prepare cleanup %#v", j)
	}
}
func TestProcessFailureDoesNotLeakOutput(t *testing.T) {
	c, _ := fixture(t, "immediate")
	binary := filepath.Join(c.Root, "bad-binary")
	_ = os.WriteFile(binary, []byte("bad binary"), 0700)
	c.Binary = binary
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "prepare"})
	if e != nil {
		t.Fatal(e)
	}
	if j.Status != "failed" || j.CleanupStatus != "not_started" {
		t.Fatalf("failure %#v", j)
	}
}

// Opt-in transport test uses a synthetic Python child; it never starts ARIES,
// containers, downloads, a model runtime, or any provider request.
func TestOrbLifecycle(t *testing.T) {
	machine := os.Getenv("TRACEX_TEST_ORB")
	if machine == "" {
		t.Skip("set TRACEX_TEST_ORB to test an existing OrbStack machine")
	}
	c, _ := fixture(t, "wait")
	binary := filepath.Join(c.Root, "fake-aries")
	script := `#!/usr/bin/python3
import json,sys,os,signal,time
if sys.argv[1]=='setup': sys.exit(0)
with open(sys.argv[-1]) as f: cfg=json.load(f)
d=os.path.join(cfg['output_dir'],'synthetic-run')
os.makedirs(d,mode=0o700)
def stop(sig,frame):
    with open(os.path.join(d,'run-result.json'),'w') as f:
        json.dump({'tasks':[{'harness':{'status':'failed'},'evaluation':{'status':'not_run'},'cleanup':{'status':'succeeded'}}],'infrastructure':{'cleanup':{'status':'succeeded'}}},f)
    sys.exit(7)
signal.signal(signal.SIGINT,stop)
with open(os.path.join(d,'ready'),'w') as f:f.write('ready')
while True:time.sleep(0.05)
`
	if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	c.Binary = binary
	c.Executor = "orb"
	c.Machine = machine
	c.StopTimeout = 5 * time.Second
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	j, e := m.Start(Request{ProfileID: "nested/test.json", Action: "run", ConfirmModelCalls: true})
	if e != nil {
		t.Fatal(e)
	}
	waitReady(t, m, j.ID)
	if _, e = m.Stop(j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitFinished(t, m, j.ID)
	if j.Status != "cancelled" || j.CleanupStatus != "confirmed" || j.ExitCode == nil || *j.ExitCode != 7 {
		t.Fatalf("orb cleanup %#v", j)
	}
}

func TestMixedOutcomesAndMissingSharedCleanup(t *testing.T) {
	for _, order := range [][]string{{"succeeded", "not_run"}, {"not_run", "succeeded"}} {
		j := Job{CleanupStatus: "unconfirmed"}
		raw := map[string]any{"infrastructure": map[string]any{"cleanup": map[string]any{"status": "succeeded"}}}
		items := []any{}
		for _, status := range order {
			items = append(items, map[string]any{"harness": map[string]any{"status": status}, "evaluation": map[string]any{"status": status}, "cleanup": map[string]any{"status": "succeeded"}})
		}
		raw["tasks"] = items
		summarizeResult(&j, raw)
		if j.HarnessStatus != "mixed" || j.EvaluationStatus != "mixed" || j.CleanupStatus != "confirmed" {
			t.Fatalf("mixed results %#v", j)
		}
		delete(raw, "infrastructure")
		j.CleanupStatus = "unconfirmed"
		summarizeResult(&j, raw)
		if j.CleanupStatus == "confirmed" {
			t.Fatal("missing shared cleanup was treated as confirmed")
		}
	}
}
func TestCatalogBoundsAndLateSymlinkEscape(t *testing.T) {
	c, path := fixture(t, "immediate")
	_ = os.WriteFile(filepath.Join(c.Root, "profiles", "metadata.json"), []byte(`{"name":"versions"}`), 0600)
	oversized := filepath.Join(c.Root, "profiles", "large.json")
	f, e := os.Create(oversized)
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(maxProfileBytes + 1)
	f.Close()
	m, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	if len(m.Catalog().Profiles) != 1 {
		t.Fatalf("included non-profile or oversized file %#v", m.Catalog())
	}
	external := filepath.Join(t.TempDir(), "outside.json")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(external, b, 0600)
	_ = os.Remove(path)
	_ = os.Symlink(external, path)
	if _, e = m.Start(Request{ProfileID: "nested/test.json", Action: "prepare"}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("late symlink escape %v", e)
	}
}
