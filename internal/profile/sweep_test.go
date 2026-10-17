package profile

import "testing"

func TestSweepSamples(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	thr := func(p, t float64) *CPUThrottling { return &CPUThrottling{Periods: p, ThrottledPeriods: t} }
	p := Profile{
		Tasks: []Task{
			{ID: "a", Status: "succeeded", TimeQuality: "measured", StartMS: 0, DurationMS: 1000},
			{ID: "b", Status: "running", TimeQuality: "measured", StartMS: 10, DurationMS: 50},
		},
		Spans: []Span{
			{ID: "turn", TaskID: "a", Kind: "model", Name: "llm.x", StartMS: 0, DurationMS: 900},
			{ID: "api", TaskID: "a", ParentID: "turn", Kind: "model", Name: "api.x", StartMS: 0, DurationMS: 300},
			{ID: "marker", TaskID: "a", Kind: "model", Name: "api.y", Attributes: map[string]any{"time_quality": "completion_only"}},
			{ID: "call", TaskID: "a", Kind: "browser", Name: "MCP send tools/call browser_navigate", StartMS: 400, DurationMS: 200},
			{ID: "wrapper", TaskID: "a", Kind: "browser", Name: "tool.mcp__playwright__browser_navigate", StartMS: 399, DurationMS: 202},
			{ID: "r1", TaskID: "a", Kind: "network", Name: "Browser request", StartMS: 350, DurationMS: 100},
			{ID: "r2", TaskID: "a", Kind: "network", Name: "Browser request", StartMS: 420, DurationMS: 60},
			{ID: "r3", TaskID: "a", Kind: "network", Name: "Browser request", StartMS: 550, DurationMS: 10},
			{ID: "page", TaskID: "a", Kind: "browser", Name: "Page navigation", StartMS: 400, DurationMS: 100},
		},
		Resources: []Resource{
			{TaskID: "a", Component: "sandbox", RuntimeID: "s", TimeMS: 0, CPUThrottling: thr(10, 2)},
			{TaskID: "a", Component: "sandbox", RuntimeID: "s", TimeMS: 1, CPUThrottling: thr(20, 7)},
			{TaskID: "a", Component: "sandbox", RuntimeID: "s", TimeMS: 2},
			{TaskID: "a", Component: "sandbox", RuntimeID: "s", TimeMS: 3, CPUThrottling: thr(5, 1)},
			{TaskID: "a", Component: "sandbox", RuntimeID: "s", TimeMS: 4, CPUThrottling: thr(15, 1)},
			{TaskID: "a", Component: "harness", RuntimeID: "h", TimeMS: 0, CPUThrottling: thr(0, 0)},
			{TaskID: "a", Component: "harness", RuntimeID: "h", TimeMS: 4, CPUThrottling: thr(100, 100)},
			{TaskID: "b", Component: "sandbox", RuntimeID: "s2", TimeMS: 0, CPUPercent: f(1)},
		},
	}
	got := SweepSamples(p)
	a, b := got[0], got[1]
	if a.DurationMS == nil || *a.DurationMS != 1000 || b.DurationMS != nil {
		t.Fatalf("durations %+v %+v", a.DurationMS, b.DurationMS)
	}
	if len(a.ModelMS) != 1 || a.ModelMS[0] != 300 {
		t.Fatalf("model calls must exclude envelopes and markers: %v", a.ModelMS)
	}
	// Network union over the call [400,600): [400,480) ∪ [550,560) = 90 ms.
	if len(a.BrowserLocalMS) != 1 || a.BrowserLocalMS[0] != 110 {
		t.Fatalf("browser local %v", a.BrowserLocalMS)
	}
	// 10→20 adds 10/5; reset 20→5 adds nothing; 5→15 adds 10/0. Harness excluded.
	if a.Periods == nil || *a.Periods != 20 || *a.ThrottledPeriods != 5 {
		t.Fatalf("throttling %v %v", a.Periods, a.ThrottledPeriods)
	}
	if b.Periods != nil {
		t.Fatal("unmeasured throttling must stay unmeasured")
	}
}
