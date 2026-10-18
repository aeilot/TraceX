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

func TestSuspendRecordRejectsUnpairedEvents(t *testing.T) {
	var s suspendState
	if _, _, _, invalid := s.parse(map[string]any{"time": "2026-01-01T00:00:01Z", "event": "unpause", "latency_ms": 1.0}, "task", 1); !invalid {
		t.Fatal("an unpause without a pause must be invalid")
	}
	if _, _, _, invalid := s.parse(map[string]any{"time": "2026-01-01T00:00:01Z", "event": "pause"}, "task", 2); !invalid {
		t.Fatal("a record without latency must be invalid")
	}
}
