package server

import (
	"context"
	"fmt"
	"github.com/aeilot/TraceX/internal/profile"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func largeProfile() profile.Profile {
	p := profile.Profile{SchemaVersion: profile.SchemaVersion, RunID: "synthetic-scale", Name: "Synthetic scale", DurationMS: 100000, Tasks: []profile.Task{{ID: "t", Name: "Synthetic task", DurationMS: 100000}}}
	for i := 0; i < 50000; i++ {
		p.Spans = append(p.Spans, profile.Span{ID: fmt.Sprintf("s-%06d", i), TaskID: "t", Name: "Synthetic tool", Kind: "tool", StartMS: float64(i * 2), DurationMS: 10})
	}
	for i := 0; i < 200000; i++ {
		cpu := float64(i % 101)
		memory := float64(i%1000) * 1048576
		p.Resources = append(p.Resources, profile.Resource{TaskID: "t", RuntimeID: fmt.Sprintf("r-%d", i%4), TimeMS: float64(i) / 2, CPUPercent: &cpu, MemoryBytes: &memory})
	}
	return p
}
func TestLargeIndexedViewAndStreamUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("large synthetic scale fixture")
	}
	p := largeProfile()
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	for _, offset := range []int{0, 250, 49750} {
		v := requestJSON(t, ws, fmt.Sprintf("/api/view?offset=%d&limit=250&points=1000", offset))
		if v["total_spans"].(float64) != 50000 {
			t.Fatal("span count")
		}
		pp := v["profile"].(map[string]any)
		if len(pp["spans"].([]any)) != 250 || len(pp["resources"].([]any)) > 1000 {
			t.Fatal("unbounded response")
		}
	}
	window := requestJSON(t, ws, "/api/view?start_ms=99950&end_ms=99999&points=300")
	if window["total_resources"].(float64) != 99 {
		t.Fatal("indexed sample interval mismatch", window["total_resources"])
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	initial := eventBytes(t, profile.Event{Type: "metadata", Metadata: &profile.Metadata{RunID: p.RunID, DurationMS: p.DurationMS}})
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	tail := newEventTail(path)
	oldOffset := tail.offset
	next := eventBytes(t, profile.Event{Type: "span", Span: &profile.Span{ID: "added", TaskID: "t", Kind: "tool", Name: "Append", StartMS: 100000, DurationMS: 1}})
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.Write(next)
	_ = f.Close()
	updated, reset, e := tail.read(p)
	if e != nil || reset {
		t.Fatal(e)
	}
	if len(updated.Spans) != 50001 || tail.offset-oldOffset != int64(len(next)) {
		t.Fatal("append offset or record count")
	}
	if len(p.Spans) != 50000 {
		t.Fatal("base mutated")
	}
}
func BenchmarkLargeIndexedView(b *testing.B) {
	p := largeProfile()
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
	if e != nil {
		b.Fatal(e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/view?offset=250&limit=250&start_ms=1000&end_ms=2000&points=300", nil))
		if r.Code != 200 {
			b.Fatal(r.Code)
		}
	}
}
func BenchmarkLargeSnapshotBuild(b *testing.B) {
	p := largeProfile()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := build(p, uint64(i))
		if len(s.byID) != 50000 {
			b.Fatal("index")
		}
	}
}
