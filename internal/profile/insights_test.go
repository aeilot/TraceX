package profile

import "testing"

func TestInsightLeafCallsAndMissingEvidence(t *testing.T) {
	p := Demo()
	p.Spans = []Span{
		{ID: "outer", Kind: "model", Name: "envelope", StartMS: 0, DurationMS: 20},
		{ID: "inner", ParentID: "outer", Kind: "model", Name: "api", StartMS: 0, DurationMS: 10, Usage: &Usage{InputTokens: numInsight(0)}},
		{ID: "unknown", Kind: "model", Name: "api", StartMS: 2, DurationMS: 0, Attributes: map[string]any{"time_quality": "completion_only"}, Usage: &Usage{OutputTokens: numInsight(4)}},
		{ID: "overlap", Kind: "model", Name: "api", StartMS: 5, DurationMS: 10, Status: "failed"},
	}
	p.Edges = nil
	p.Resources = nil
	r := Diagnose(p, Analysis{})
	if len(r.Groups) != 1 {
		t.Fatalf("envelope counted: %+v", r.Groups)
	}
	g := r.Groups[0]
	if g.Count != 3 || g.Timed != 2 || g.SumMS != 20 || g.UnionMS != 15 || g.InputKnown != 1 || g.InputTokens != 0 || g.OutputKnown != 1 || g.OutputTokens != 4 {
		t.Fatalf("incorrect group: %+v", g)
	}
	if g.P50MS == nil || *g.P50MS != 10 {
		t.Fatal("missing timing quantile")
	}
	for _, f := range r.Findings {
		if f.Rule == "usage_gap" && f.Count != 3 {
			t.Fatalf("missing input/output sets must not be collapsed: %+v", f)
		}
	}
}
func numInsight(v float64) *float64 { return &v }
func TestInsightTaskPairingAndThrottlingReset(t *testing.T) {
	p := Demo()
	p.Resources = []Resource{
		{TimeMS: 0, RuntimeID: "r", CPUThrottling: &CPUThrottling{Periods: 10, ThrottledPeriods: 2}},
		{TimeMS: 1, RuntimeID: "r", CPUThrottling: &CPUThrottling{Periods: 20, ThrottledPeriods: 7}},
		{TimeMS: 2, RuntimeID: "r", CPUThrottling: &CPUThrottling{Periods: 1, ThrottledPeriods: 0}},
	}
	r := Diagnose(p, Analyze(p))
	found := false
	for _, f := range r.Findings {
		if f.Rule == "throttling" {
			found = true
			if f.Value != .5 {
				t.Fatal(f.Value)
			}
		}
	}
	if !found {
		t.Fatal("missing throttling")
	}
	pairs := PairTasks([]TaskEvidence{{ID: "a1", Key: "task-a"}, {ID: "a2", Key: "task-a"}}, []TaskEvidence{{ID: "b1", Key: "task-a"}, {ID: "b2", Key: "task-b"}})
	if len(pairs) != 2 || len(pairs[0].Baseline) != 2 || len(pairs[0].Candidate) != 1 || len(pairs[1].Baseline) != 0 {
		t.Fatalf("bad pairs: %+v", pairs)
	}
}
