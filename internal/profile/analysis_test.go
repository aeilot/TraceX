package profile

import "testing"

func testProfile() Profile {
	return Profile{SchemaVersion: SchemaVersion, DurationMS: 100, Tasks: []Task{{ID: "t", Status: "succeeded", DurationMS: 100}}, Spans: []Span{{ID: "a", TaskID: "t", Kind: "model", DurationMS: 10}, {ID: "b", TaskID: "t", Kind: "tool", StartMS: 10, DurationMS: 30}, {ID: "c", TaskID: "t", Kind: "tool", StartMS: 10, DurationMS: 20}, {ID: "d", TaskID: "t", Kind: "model", StartMS: 40, DurationMS: 10}}, Edges: []Edge{{"a", "b", "dependency"}, {"a", "c", "dependency"}, {"b", "d", "join"}, {"c", "d", "join"}}}
}
func TestAnalysisBranchJoinCoverage(t *testing.T) {
	p := testProfile()
	if e := Validate(&p); e != nil {
		t.Fatal(e)
	}
	a := Analyze(p)
	if a.CriticalPath.Status != "available" || a.CriticalPath.DurationMS != 50 {
		t.Fatalf("critical: %+v", a.CriticalPath)
	}
	if len(a.CriticalPath.SpanIDs) != 3 || a.CriticalPath.SpanIDs[1] != "b" {
		t.Fatal(a.CriticalPath)
	}
	if a.Coverage[0].ObservedMS != 50 || a.Coverage[0].UnattributedMS != 50 || a.MaxConcurrency != 2 {
		t.Fatalf("coverage: %+v", a)
	}
	if a.Usage.InputTokens != nil || a.Usage.OutputTokens != nil {
		t.Fatal("missing usage became zero")
	}
	if a.TaskSucceeded != 1 || a.SpanCount != 4 || a.TaskCount != 1 {
		t.Fatal("counts")
	}
}
func TestAnalysisRejectsOverlapForCriticalPath(t *testing.T) {
	p := testProfile()
	p.Spans[3].StartMS = 30
	a := Analyze(p)
	if a.CriticalPath.Status != "unavailable" {
		t.Fatal("overlap counted as precedence")
	}
}
func TestContainmentNotCriticalAndNotObservedCompute(t *testing.T) {
	p := testProfile()
	p.Spans = append(p.Spans, Span{ID: "agent", TaskID: "t", Kind: "agent", DurationMS: 100}, Span{ID: "lifecycle", TaskID: "t", Kind: "lifecycle", DurationMS: 100})
	p.Edges = nil
	a := Analyze(p)
	if a.Coverage[0].ObservedMS != 50 || a.CriticalPath.Status != "unavailable" {
		t.Fatal(a)
	}
}
func TestUnknownCoverageCompletionUsageAndZero(t *testing.T) {
	p := testProfile()
	p.Tasks[0].TimeQuality = "unknown"
	zero := 0.0
	p.Spans[0].Usage = &Usage{InputTokens: &zero}
	p.Spans[0].Incomplete = true
	p.Spans[0].DurationMS = 0
	a := Analyze(p)
	if a.Coverage[0].Status != "unavailable" || a.Usage.InputTokens == nil || *a.Usage.InputTokens != 0 || a.Usage.InputKnown != 1 {
		t.Fatalf("missing vs zero: %+v", a)
	}
	for _, k := range a.Kinds {
		if k.Kind == "model" && (k.TimeQuality != "mixed" || k.IncompleteCount != 1) {
			t.Fatal(k)
		}
	}
}
func TestWindowResourcesPreserveSharedAndMissing(t *testing.T) {
	p := testProfile()
	p.Spans[0].RuntimeID = "h"
	zero, twenty := 0.0, 20.0
	p.Resources = []Resource{{TaskID: "t", RuntimeID: "h", TimeMS: 0}, {TaskID: "t", RuntimeID: "h", TimeMS: 5, CPUPercent: &zero, GPUPercent: &twenty}, {RuntimeID: "shared", TimeMS: 5, CPUPercent: &twenty}, {TaskID: "t", RuntimeID: "other", TimeMS: 5, CPUPercent: &twenty}}
	w, e := WindowStats(p, "a")
	if e != nil {
		t.Fatal(e)
	}
	if len(w.Resources) != 2 {
		t.Fatal(w)
	}
	for _, r := range w.Resources {
		if r.RuntimeID == "h" {
			if r.Attribution != "runtime_context" || r.SampleCount != 2 || r.CPU.SampleCount != 1 || r.CPU.Mean == nil || *r.CPU.Mean != 0 || r.GPU.Mean == nil {
				t.Fatal(r)
			}
		} else if r.Attribution != "shared_context" {
			t.Fatal(r)
		}
	}
	if _, e = WindowStats(p, "missing"); e == nil {
		t.Fatal("unknown span accepted")
	}
}
func TestSchemaMigrationAndGraphCycle(t *testing.T) {
	p := testProfile()
	p.SchemaVersion = 1
	if Validate(&p) == nil {
		t.Fatal("v1 extended edges accepted")
	}
	p.Edges = nil
	p.Spans[1].ParentID = "a"
	if e := Validate(&p); e != nil {
		t.Fatal(e)
	}
	if p.SchemaVersion != 2 || len(p.Edges) != 1 {
		t.Fatal("v1 failed migration")
	}
	p.Edges = append(p.Edges, Edge{"b", "a", "dependency"})
	if Validate(&p) == nil {
		t.Fatal("mixed containment/dependency cycle accepted")
	}
}
func TestConfigurationPrivacy(t *testing.T) {
	p := testProfile()
	p.Configuration = map[string]any{"model": "m", "secret": "private", "cpu_limit": 2, "prompt": "private"}
	if e := Validate(&p); e != nil {
		t.Fatal(e)
	}
	if len(p.Configuration) != 2 {
		t.Fatal(p.Configuration)
	}
}
