package profile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventUpdatesIncompleteSpanAndPrivateFields(t *testing.T) {
	var out bytes.Buffer
	task := Task{ID: "t", DurationMS: 100}
	start := Span{ID: "s", TaskID: "t", Kind: "model", Incomplete: true}
	end := start
	end.Incomplete = false
	end.DurationMS = 70
	end.Attributes = map[string]any{"prompt": "private", "model": "m"}
	for _, e := range []Event{{Type: "task", Task: &task}, {Type: "span", Span: &start}, {Type: "span", Span: &end}} {
		if err := AppendEvent(&out, e); err != nil {
			t.Fatal(err)
		}
	}
	p, e := ApplyEvents(empty(), &out)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Spans) != 1 || p.Spans[0].Incomplete || p.Spans[0].DurationMS != 70 {
		t.Fatal(p)
	}
	if _, found := p.Spans[0].Attributes["prompt"]; found {
		t.Fatal("private attribute retained")
	}
}
func TestEventPartialAndPendingEdges(t *testing.T) {
	var out bytes.Buffer
	task := Task{ID: "t"}
	edge := Edge{"future", "also-future", "dependency"}
	_ = AppendEvent(&out, Event{Type: "task", Task: &task})
	_ = AppendEvent(&out, Event{Type: "edge", Edge: &edge})
	out.WriteString(`{"schema_version":1`)
	p, e := ApplyEvents(empty(), &out)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Edges) != 0 || len(p.Warnings) < 2 {
		t.Fatal("pending links or partial writes undisclosed")
	}
}
func TestEventRejectsWrongTypeAndNonMonotonic(t *testing.T) {
	for _, raw := range []string{"{\"schema_version\":1,\"type\":\"span\",\"task\":{\"id\":\"t\"}}\n", "{\"schema_version\":1,\"type\":\"task\",\"sequence\":2,\"task\":{\"id\":\"t\"}}\n{\"schema_version\":1,\"type\":\"task\",\"sequence\":1,\"task\":{\"id\":\"t\"}}\n"} {
		if _, e := ApplyEvents(empty(), strings.NewReader(raw)); e == nil {
			t.Fatal("invalid events accepted")
		}
	}
}
func TestEventResourceReplacement(t *testing.T) {
	task := Task{ID: "t"}
	r := Resource{TaskID: "t", RuntimeID: "h", TimeMS: 1}
	z := 0.0
	r2 := r
	r2.CPUPercent = &z
	p, e := ApplyEventBatch(empty(), []Event{{SchemaVersion: 1, Type: "task", Task: &task}, {SchemaVersion: 1, Type: "resource", Resource: &r}, {SchemaVersion: 1, Type: "resource", Resource: &r2}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Resources) != 1 || p.Resources[0].CPUPercent == nil {
		t.Fatal(p.Resources)
	}
}

func TestSyntheticProducerFixture(t *testing.T) {
	f, e := os.Open("../../examples/events.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	p, e := ApplyEvents(empty(), f)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Tasks) != 1 || p.Tasks[0].Status != "succeeded" || p.DurationMS != 1900 {
		t.Fatal(p)
	}
	phases := map[string]bool{}
	joins := 0
	for _, s := range p.Spans {
		if s.Kind == "lifecycle" {
			phases[s.Attributes["phase"].(string)] = true
		}
		if s.Incomplete {
			t.Fatal("completed producer retains incomplete marker")
		}
	}
	for _, e := range p.Edges {
		if e.Kind == "join" {
			joins++
		}
	}
	if len(phases) != 6 || joins != 2 {
		t.Fatal("producer missing lifecycle or multi-parent join")
	}
	if a := Analyze(p); a.CriticalPath.Status != "available" || a.Usage.InputKnown != 2 {
		t.Fatal(a)
	}
}

func TestEventResourceExtendsRunHorizon(t *testing.T) {
	r := Resource{RuntimeID: "shared", TimeMS: 900}
	p, e := ApplyEventBatch(empty(), []Event{{SchemaVersion: 1, Type: "resource", Resource: &r}})
	if e != nil {
		t.Fatal(e)
	}
	if p.DurationMS != 900 {
		t.Fatal("resource horizon dropped", p.DurationMS)
	}
}

func TestLoadDirectEventStreamAndEmptyStartup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "custom.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil || len(p.Spans) != 0 {
		t.Fatalf("empty startup: %v", err)
	}
	fixture, err := os.ReadFile("../../examples/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	p, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Spans) < 10 || len(p.Edges) < 2 {
		t.Fatal("stream not loaded")
	}
}

func TestEventReplayRecomputesTransientWarnings(t *testing.T) {
	task := Task{ID: "t"}
	child := Span{ID: "child", TaskID: "t", ParentID: "parent"}
	parent := Span{ID: "parent", TaskID: "t"}
	edge := Edge{Source: "parent", Target: "child", Kind: "dependency"}
	events := []Event{{SchemaVersion: 1, Type: "span", Span: &child}, {SchemaVersion: 1, Type: "edge", Edge: &edge}, {SchemaVersion: 1, Type: "resource", Resource: &Resource{TaskID: "t", RuntimeID: "h", TimeMS: 1}}}
	p := empty()
	p.Warnings = []string{"Native evidence remains partial."}
	p, e := ApplyEventBatch(p, events)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Warnings) < 3 {
		t.Fatal("pending warnings missing")
	}
	p.Warnings = append(p.Warnings, "Unterminated final event is pending; retry after producer commits its newline.")
	// Replay the retained prefix plus newly committed endpoint records.
	events = append(events, Event{SchemaVersion: 1, Type: "task", Task: &task}, Event{SchemaVersion: 1, Type: "span", Span: &parent})
	next, e := ApplyEventBatch(p, events)
	if e != nil {
		t.Fatal(e)
	}
	if len(next.Warnings) != 1 || next.Warnings[0] != "Native evidence remains partial." {
		t.Fatal("resolved event warning retained", next.Warnings)
	}
	if len(next.Spans) != 2 || len(next.Edges) != 2 || len(next.Resources) != 1 {
		t.Fatal("pending records failed to resolve")
	}
}
func TestEventPendingWarningsDeduplicate(t *testing.T) {
	p, e := ApplyEventBatch(empty(), []Event{{SchemaVersion: 1, Type: "span", Span: &Span{ID: "a", TaskID: "missing"}}, {SchemaVersion: 1, Type: "span", Span: &Span{ID: "b", TaskID: "missing"}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Warnings) != 1 {
		t.Fatal(p.Warnings)
	}
}
