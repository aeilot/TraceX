package profile

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDemoRoundTrip(t *testing.T) {
	p := Demo()
	if err := Validate(&p); err != nil {
		t.Fatal(err)
	}
	if len(p.Spans) < 12 || len(p.Edges) < 10 {
		t.Fatal("demo lacks branching evidence")
	}
	if len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "synthetic") {
		t.Fatal("demo must disclose synthetic evidence")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write(t, root, "profile.json", string(b))
	loaded, err := Load(filepath.Join(root, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Edges) != len(p.Edges) {
		t.Fatal("parent edges lost")
	}
}
func TestValidationRejectsInvalidEvidence(t *testing.T) {
	cases := map[string]func(*Profile){
		"duplicate task":    func(p *Profile) { p.Tasks = append(p.Tasks, p.Tasks[0]) },
		"duplicate span":    func(p *Profile) { p.Spans = append(p.Spans, p.Spans[0]) },
		"cycle":             func(p *Profile) { p.Spans[0].ParentID = p.Spans[1].ID },
		"unknown parent":    func(p *Profile) { p.Spans[0].ParentID = "missing" },
		"unknown task":      func(p *Profile) { p.Spans[0].TaskID = "missing" },
		"negative":          func(p *Profile) { p.Spans[0].DurationMS = -1 },
		"nan":               func(p *Profile) { p.DurationMS = math.NaN() },
		"infinite resource": func(p *Profile) { n := math.Inf(1); p.Resources[0].CPUPercent = &n },
		"invented edge": func(p *Profile) {
			p.Edges = append(p.Edges, Edge{Source: "delegate-a", Target: "delegate-b", Kind: "parent"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := Demo()
			change(&p)
			if Validate(&p) == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
func TestARIESPreservesParentsResourcesAndPrivateBoundary(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","name":"fixture","duration":3000000000,"tasks":[{"task_id":"task-001","started_at":"2026-01-01T00:00:00Z","duration":3000000000,"harness":{"status":"succeeded"},"evaluation":{"status":"failed"},"cleanup":{"status":"succeeded"},"error":"verifier failed"}]}`)
	write(t, root, "task-001/harness/telemetry/otel-spans.jsonl", `{"name":"agent","span_id":"root","trace_id":"t","start_time_unix_nano":"1767225600000000000","end_time_unix_nano":"1767225602000000000","attributes":{"prompt":"private-secret","model":"fixture"}}
{"name":"api.chat","span_id":"child","parent_span_id":"root","trace_id":"t","start_time_unix_nano":"1767225600100000000","end_time_unix_nano":"1767225601100000000"}
{"name":"tool.terminal","span_id":"orphan","parent_span_id":"missing","trace_id":"t","start_time_unix_nano":"1767225601100000000","end_time_unix_nano":"1767225601200000000"}
invalid`)
	write(t, root, "task-001/bridge/tool-calls.jsonl", `{"timestamp":"2026-01-01T00:00:02Z","operation_class":"exec","duration_ms":500,"status":"succeeded","command":"private-secret","stdin":"private-secret","exit_code":0}`)
	write(t, root, "task-001/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:01Z","component":"harness","runtime_id":"h","cpu_percent":0,"memory_usage_bytes":1234}
{"time":"2026-01-01T00:00:02Z","component":"harness","runtime_id":"h","cpu_percent":0,"memory_usage_bytes":1234}`)
	write(t, root, "infrastructure/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:02Z","component":"bridge","runtime_id":"shared","cpu_percent":5,"memory_usage_bytes":4567}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Spans) != 5 || len(p.Edges) != 1 {
		t.Fatalf("spans=%d edges=%d", len(p.Spans), len(p.Edges))
	}
	if p.Tasks[0].EvaluationStatus != "failed" || p.Tasks[0].Status != "succeeded" {
		t.Fatal("independent outcomes conflated")
	}
	if len(p.Resources) != 3 {
		t.Fatal("resources missing")
	}
	for _, r := range p.Resources {
		if r.RuntimeID == "h" && r.TimeMS == 2000 && (r.CPUPercent == nil || *r.CPUPercent != 0 || r.GPUPercent != nil) {
			t.Fatal("missing measurement conflated with zero")
		}
		if r.RuntimeID == "h" && r.TimeMS == 1000 && r.CPUPercent != nil {
			t.Fatal("first sample baseline treated as measured zero")
		}
		if r.RuntimeID == "shared" && r.TaskID != "" {
			t.Fatal("shared resource incorrectly assigned")
		}
	}
	for _, s := range p.Spans {
		if s.Source == "ARIES bridge" && s.StartMS != 1500 {
			t.Fatalf("bridge timestamp is completion; start=%v", s.StartMS)
		}
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "private-secret") {
		t.Fatal("private contents leaked")
	}
	if len(p.Warnings) < 2 {
		t.Fatal("missing partial evidence warnings")
	}
}
func TestRepeatedTaskIDsStayDistinctWithoutGuessingArtifacts(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","duration":2000000000,"tasks":[{"task_id":"same","duration":1000000000},{"task_id":"same","duration":1000000000}]}`)
	write(t, root, "same/harness/telemetry/otel-spans.jsonl", `{"name":"agent","start_time_unix_nano":1767225600000000000,"end_time_unix_nano":1767225601000000000}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tasks) != 2 || p.Tasks[0].ID == p.Tasks[1].ID || len(p.Spans) != 0 {
		t.Fatal("occurrence identity guessed")
	}
}
func TestMissingTelemetryAndOpaqueOpenClaw(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","tasks":[{"task_id":"task","duration":1000000}]}`)
	write(t, root, "task/harness/trajectory.json", `{"private":"do not parse"}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range p.Capabilities {
		if c.Name == "openclaw_trajectory" && c.Status == "unsupported" {
			found = true
		}
	}
	if !found || len(p.Spans) != 0 || len(p.Resources) != 0 {
		t.Fatal("unsupported telemetry misrepresented")
	}
}
func TestSymlinkEvidenceNotFollowed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"task","duration":1000000}]}`)
	secret := filepath.Join(t.TempDir(), "external.jsonl")
	if err := os.WriteFile(secret, []byte(`{"private":"secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "task"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "task", "otel-spans.jsonl")); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Spans) != 0 {
		t.Fatal("followed external symlink")
	}
}

func TestMonitorFailureIsNotUnsupported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"task","duration":1000000}]}`)
	write(t, root, "task/monitor/index.json", `{"status":"failed","error":"private failure details"}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range p.Capabilities {
		if c.Name == "resources" && c.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("genuine resource failure misreported")
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "private failure") {
		t.Fatal("raw error leaked")
	}
}
func TestNativeCycleWarnsAndOmitsEdge(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"task","duration":1000000}]}`)
	write(t, root, "task/harness/telemetry/otel-spans.jsonl", `{"name":"a","span_id":"a","parent_span_id":"b","start_time_unix_nano":1767225600000000000,"end_time_unix_nano":1767225601000000000}
{"name":"b","span_id":"b","parent_span_id":"a","start_time_unix_nano":1767225600000000000,"end_time_unix_nano":1767225601000000000}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Edges) != 1 {
		t.Fatal("cycle not broken")
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "cyclic") {
			found = true
		}
	}
	if !found {
		t.Fatal("silent cycle removal")
	}
}

func TestDisabledMonitorAndActualOpenClawFilename(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"task","duration":1000000}]}`)
	write(t, root, "task/monitor/index.json", `{"status":"not_enabled"}`)
	write(t, root, "task/harness/telemetry/run.trajectory.jsonl", `{"private":"opaque data"}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	disabled, opaque := false, false
	for _, c := range p.Capabilities {
		if c.Name == "resources" && c.Status == "not_enabled" {
			disabled = true
		}
		if c.Name == "openclaw_trajectory" && c.Status == "unsupported" {
			opaque = true
		}
	}
	if !disabled || !opaque {
		t.Fatal("monitor or trajectory capability lost")
	}
}
func TestUnattributedSpanIsNotFabricatedAgent(t *testing.T) {
	v := map[string]any{"name": "unclassified.operation", "start_time_unix_nano": json.Number("1767225600000000000"), "end_time_unix_nano": json.Number("1767225601000000000")}
	s, err := parseSpan(v, "task", 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.span.Kind != "other" || s.span.AgentID != "" {
		t.Fatal("unknown event attributed to a fabricated agent")
	}
}
