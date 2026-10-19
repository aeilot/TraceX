package profile

import (
	"strings"
	"testing"
)

func TestARIESSuspendRecordBecomesPausedIntervals(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","name":"fixture","duration":10000000000,"tasks":[{"task_id":"task-001","started_at":"2026-01-01T00:00:00Z","duration":10000000000,"harness":{"status":"succeeded"}}]}`)
	write(t, root, "task-001/harness/telemetry/otel-spans.jsonl", `{"name":"tool.mcp__playwright__browser_navigate","span_id":"a","trace_id":"t","start_time_unix_nano":"1767225600000000000","end_time_unix_nano":"1767225601000000000"}`)
	// A 2 s pause between calls, a pause that ARIES could not apply, and a
	// trailing pause that has no unpause yet.
	write(t, root, "task-001/bridge/suspend.jsonl", `{"time":"2026-01-01T00:00:01Z","event":"pause","reason":"idle","latency_ms":20}
{"time":"2026-01-01T00:00:03.02Z","event":"unpause","reason":"call","latency_ms":15,"paused_ms":2000}
{"time":"2026-01-01T00:00:04Z","event":"pause","reason":"idle","latency_ms":5,"error":"refused"}
{"time":"2026-01-01T00:00:04Z","event":"disabled","reason":"error","latency_ms":0}
{"time":"2026-01-01T00:00:05Z","event":"pause","reason":"idle","latency_ms":10}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var paused []Span
	for _, s := range p.Spans {
		if s.Kind == "suspended" {
			paused = append(paused, s)
		}
	}
	if len(paused) != 1 {
		t.Fatalf("paused spans = %+v", paused)
	}
	s := paused[0]
	// The interval starts when the pause took effect (1 s + 20 ms) and ends
	// when the next call requested the unpause.
	if s.StartMS != 1020 || s.DurationMS != 2000 || s.TaskID != "task-001" || s.Attributes["suspend_reason"] != "call" || s.Attributes["unpause_latency_ms"] != 15.0 {
		t.Fatalf("span = %+v", s)
	}
	if p.Configuration["observed_sandbox_suspend"] != "pause" {
		t.Fatalf("configuration = %+v", p.Configuration)
	}
	warnings := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"pause failed", "no recorded unpause"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("missing warning %q in %q", want, warnings)
		}
	}
	a := Analyze(p)
	found := false
	for _, k := range a.Kinds {
		if k.Kind == "suspended" {
			found = true
			if k.UnionMS != 2000 {
				t.Fatalf("suspended union = %v", k.UnionMS)
			}
		}
	}
	if !found {
		t.Fatalf("analysis lacks the suspended kind: %+v", a.Kinds)
	}
	// Frozen time is not work: coverage counts only the 1 s tool call.
	found = false
	for _, c := range a.Coverage {
		if c.TaskID == "task-001" {
			found = true
			if c.ObservedMS != 1000 {
				t.Fatalf("coverage counted paused time as work: %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("analysis lacks coverage for the task: %+v", a.Coverage)
	}
}

func TestARIESSuspendSettingIsAComparisonCondition(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","name":"fixture","duration":1000000000,"tasks":[{"task_id":"task-001","started_at":"2026-01-01T00:00:00Z","duration":1000000000,"harness":{"status":"succeeded"}}]}`)
	write(t, root, "task-001/harness/telemetry/otel-spans.jsonl", `{"name":"agent","span_id":"a","trace_id":"t","start_time_unix_nano":"1767225600000000000","end_time_unix_nano":"1767225601000000000"}`)
	write(t, root, "run-profile.json", `{"concurrency":4,"bridge_suspend_mode":"proxy","bridge_suspend_grace_ms":1000}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Configuration["bridge_suspend_mode"] != "proxy" || p.Configuration["bridge_suspend_grace_ms"] != 1000.0 {
		t.Fatalf("configuration = %+v", p.Configuration)
	}
	// Without pause intervals nothing is inferred: absence cannot separate
	// "off", "proxy" and builds that never suspend.
	if _, ok := p.Configuration["observed_sandbox_suspend"]; ok {
		t.Fatalf("suspension inferred without evidence: %+v", p.Configuration)
	}
}

func TestDiagnoseSummarizesSandboxSuspension(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	p := Profile{RunID: "r", Tasks: []Task{{ID: "a"}, {ID: "b"}}, Spans: []Span{
		{ID: "agent-a", TaskID: "a", Kind: "agent", StartMS: 0, DurationMS: 10000, Status: "completed"},
		{ID: "pause-1", TaskID: "a", Kind: "suspended", StartMS: 1000, DurationMS: 3000, Status: "completed", Attributes: map[string]any{"unpause_latency_ms": 10.0}},
		{ID: "pause-2", TaskID: "a", Kind: "suspended", StartMS: 6000, DurationMS: 2000, Status: "failed", Attributes: map[string]any{"unpause_latency_ms": 30.0}},
		// Task b has a pause but no agent span: counted, but outside the share.
		{ID: "pause-3", TaskID: "b", Kind: "suspended", StartMS: 0, DurationMS: 5000, Status: "completed"},
	}, Resources: []Resource{
		// The first sample in each pause covers time before it (50% and 70%
		// CPU) and must not count as paused CPU; its memory reading does.
		{TaskID: "a", Component: "sandbox", TimeMS: 1500, MemoryBytes: f64(400), CPUPercent: f64(50)},
		{TaskID: "a", Component: "sandbox", TimeMS: 2500, MemoryBytes: f64(400), CPUPercent: f64(0)},
		{TaskID: "a", Component: "sandbox", TimeMS: 3500, MemoryBytes: f64(400), CPUPercent: f64(3)},
		{TaskID: "a", Component: "sandbox", TimeMS: 5000, MemoryBytes: f64(9000), CPUPercent: f64(80)}, // between pauses
		{TaskID: "a", Component: "sandbox", TimeMS: 6500, MemoryBytes: f64(700), CPUPercent: f64(70)},
		{TaskID: "a", Component: "sandbox", TimeMS: 7500, MemoryBytes: f64(700), CPUPercent: f64(0)},
		{TaskID: "a", Component: "harness", TimeMS: 2000, MemoryBytes: f64(9000)},
	}}
	var f *Finding
	for _, x := range Diagnose(p, Analysis{}).Findings {
		if x.Rule == "sandbox_suspension" {
			f = &x
		}
	}
	if f == nil {
		t.Fatal("missing sandbox_suspension finding")
	}
	d := f.Details
	if f.Count != 3 || f.Value != 0.5 || d["paused_ms"] != 5000 || d["agent_ms"] != 10000 || d["failed_unpauses"] != 1 {
		t.Fatalf("finding = %+v", *f)
	}
	if d["unpause_p50_ms"] != 20 || d["paused_memory_bytes"] != 520 || d["paused_cpu_percent"] != 1 {
		t.Fatalf("details = %+v", d)
	}
	if got := Diagnose(Profile{RunID: "r"}, Analysis{}); len(got.Findings) != 0 {
		t.Fatalf("finding without pauses: %+v", got.Findings)
	}
}

func TestSuspendRecordRejectsUnpairedEvents(t *testing.T) {
	var s suspendState
	if _, _, _, invalid := s.parse(map[string]any{"time": "2026-01-01T00:00:01Z", "event": "unpause", "latency_ms": 1.0}, "task", 1); !invalid {
		t.Fatal("an unpause without a pause must be invalid")
	}
	if _, _, _, invalid := s.parse(map[string]any{"time": "2026-01-01T00:00:01Z", "event": "pause"}, "task", 2); !invalid {
		t.Fatal("a record without latency must be invalid")
	}
}
