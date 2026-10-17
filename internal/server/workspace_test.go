package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aeilot/TraceX/internal/profile"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requestJSON(t *testing.T, w *workspace, path string) map[string]any {
	t.Helper()
	r := httptest.NewRecorder()
	w.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
	if r.Code != 200 {
		t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
	}
	var v map[string]any
	if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestIndexedPaginationFilters(t *testing.T) {
	p := profile.Demo()
	p.Spans = nil
	p.Edges = nil
	for i := 0; i < 700; i++ {
		p.Spans = append(p.Spans, profile.Span{ID: fmt.Sprintf("s%04d", i), TaskID: p.Tasks[0].ID, Kind: "tool", Name: "needle", StartMS: float64(i), DurationMS: 1})
	}
	p.Spans = append(p.Spans, profile.Span{ID: "long", TaskID: p.Tasks[0].ID, Kind: "model", Name: "long", StartMS: 0, DurationMS: 1000})
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}, {Profile: &p}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	seen := map[string]bool{}
	for off := 0; off < 700; off += 127 {
		v := requestJSON(t, ws, fmt.Sprintf("/api/view?run=run-2&kind=tool&q=needle&offset=%d&limit=127", off))
		if v["total_spans"].(float64) != 700 {
			t.Fatal(v)
		}
		pp := v["profile"].(map[string]any)
		for _, s := range pp["spans"].([]any) {
			id := s.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("duplicate page", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 700 {
		t.Fatal(len(seen))
	}
	v := requestJSON(t, ws, "/api/view?start_ms=900&end_ms=950")
	if v["total_spans"].(float64) != 1 {
		t.Fatal("lost earlier overlapping span")
	}
	r := requestJSON(t, ws, "/api/runs")
	if len(r["runs"].([]any)) != 2 {
		t.Fatal(r)
	}
}
func TestDownsamplePreservesRuntimePeaksAndGaps(t *testing.T) {
	in := []profile.Resource{}
	for _, id := range []string{"a", "b"} {
		for i := 0; i < 1000; i++ {
			v := float64(i % 9)
			if i == 551 {
				v = 999
			}
			r := profile.Resource{RuntimeID: id, TimeMS: float64(i), CPUPercent: &v}
			if i == 700 {
				r.CPUPercent = nil
			}
			in = append(in, r)
		}
	}
	out := downsample(in, 100)
	peaks, gaps := map[string]bool{}, map[string]bool{}
	for _, r := range out {
		if r.CPUPercent != nil && *r.CPUPercent == 999 {
			peaks[r.RuntimeID] = true
		}
		if r.TimeMS == 700 && r.CPUPercent == nil {
			gaps[r.RuntimeID] = true
		}
	}
	if len(peaks) != 2 || len(gaps) != 2 || len(out) >= len(in) {
		t.Fatalf("peaks %v gaps %v count %d", peaks, gaps, len(out))
	}
}
func eventBytes(t *testing.T, events ...profile.Event) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, e := range events {
		if err := profile.AppendEvent(&b, e); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}
func TestIncrementalEventsPartialRotationAndFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	base := eventBytes(t, profile.Event{Type: "metadata", Metadata: &profile.Metadata{RunID: "live", Name: "Live"}}, profile.Event{Type: "task", Task: &profile.Task{ID: "t", Name: "t", DurationMS: 10}})
	if e := os.WriteFile(path, base, 0600); e != nil {
		t.Fatal(e)
	}
	h, e := NewWorkspace(context.Background(), []Source{{Path: dir}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	offset := ws.mounts[0].events.offset
	next := eventBytes(t, profile.Event{Type: "span", Span: &profile.Span{ID: "s", TaskID: "t", Kind: "tool", DurationMS: 5}})
	appendFile := func(b []byte) {
		f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = f.Write(b)
		_ = f.Close()
		ws.refresh()
	}
	appendFile(next[:len(next)-2])
	if len(ws.mounts[0].current.p.Spans) != 0 {
		t.Fatal("partial committed")
	}
	appendFile(next[len(next)-2:])
	if len(ws.mounts[0].current.p.Spans) != 1 || ws.mounts[0].events.offset <= offset {
		t.Fatal("append not consumed")
	}
	rev := ws.mounts[0].current.revision
	appendFile([]byte("garbage\n"))
	if ws.mounts[0].current.revision != rev || ws.mounts[0].lastError == "" {
		t.Fatal("malformed replaced snapshot")
	}
	if e := os.Rename(path, path+".old"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, base, 0600); e != nil {
		t.Fatal(e)
	}
	ws.refresh()
	if len(ws.mounts[0].current.p.Spans) != 0 || ws.mounts[0].lastError != "" {
		t.Fatal("rotation did not rebuild")
	}
	if e := os.WriteFile(path, base[:len(base)-1], 0600); e != nil {
		t.Fatal(e)
	}
	ws.refresh()
	if ws.mounts[0].events.offset != int64(len(base)-1) {
		t.Fatal("truncation did not reset offset")
	}
}
func TestSSEReplayAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h, e := NewWorkspace(ctx, []Source{{Profile: ptrDemo()}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	ws.mu.Lock()
	ws.notifyLocked(notice{RunID: "run-1", Revision: 2})
	ws.notifyLocked(notice{RunID: "run-1", Revision: 3})
	ws.mu.Unlock()
	r := httptest.NewRequest("GET", "/api/events", nil)
	r.Header.Set("Last-Event-ID", "1")
	out := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ws.ServeHTTP(out, r); close(done) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream leaked")
	}
	if !strings.Contains(out.Body.String(), "id: 2\nevent: revision") {
		t.Fatal(out.Body.String())
	}
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	if len(ws.listeners) != 0 {
		t.Fatal("subscriber leaked")
	}
}
func ptrDemo() *profile.Profile { p := profile.Demo(); return &p }

func TestIncrementalPendingReferencesAndSnapshotIsolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	initial := eventBytes(t, profile.Event{Type: "metadata", Metadata: &profile.Metadata{RunID: "pending"}}, profile.Event{Type: "span", Span: &profile.Span{ID: "child", TaskID: "future-task", ParentID: "parent", Kind: "tool", DurationMS: 3}})
	if e := os.WriteFile(path, initial, 0600); e != nil {
		t.Fatal(e)
	}
	h, e := NewWorkspace(context.Background(), []Source{{Path: dir}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	before := ws.mounts[0].current
	more := eventBytes(t, profile.Event{Type: "task", Task: &profile.Task{ID: "future-task", Name: "Future"}}, profile.Event{Type: "span", Span: &profile.Span{ID: "parent", TaskID: "future-task", Kind: "agent", DurationMS: 4}})
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.Write(more)
	_ = f.Close()
	ws.refresh()
	p := ws.mounts[0].current.p
	if len(p.Spans) != 2 || len(p.Edges) != 1 {
		t.Fatalf("pending reference lost: %+v", p)
	}
	if len(before.p.Spans) != 0 {
		t.Fatal("published snapshot mutated")
	}
}
func TestConcurrentRefreshAndQueries(t *testing.T) {
	p := profile.Demo()
	path := filepath.Join(t.TempDir(), "profile.json")
	b, _ := json.Marshal(p)
	_ = os.WriteFile(path, b, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, e := NewWorkspace(ctx, []Source{{Path: path}}, Options{WatchInterval: time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	for i := 0; i < 30; i++ {
		requestJSON(t, ws, "/api/view?points=1000")
		requestJSON(t, ws, "/api/runs")
	}
}

func TestManyRuntimesPreservesViewAndSelector(t *testing.T) {
	p := profile.Demo()
	p.Resources = nil
	for runtime := 0; runtime < 45; runtime++ {
		for sample := 0; sample < 30; sample++ {
			cpu := float64(sample)
			p.Resources = append(p.Resources, profile.Resource{RuntimeID: fmt.Sprintf("runtime-%02d", runtime), TaskID: p.Tasks[0].ID, TimeMS: float64(sample), CPUPercent: &cpu})
		}
	}
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	v := requestJSON(t, ws, "/api/view?points=300")
	if v["resource_error"] == "" || len(v["runtime_ids"].([]any)) != 45 || v["total_resources"].(float64) != 1350 || v["resources_sampled"] != false {
		t.Fatal("missing resource overflow disclosure", v)
	}
	pp := v["profile"].(map[string]any)
	if len(pp["resources"].([]any)) != 0 || len(pp["spans"].([]any)) == 0 {
		t.Fatal("overflow discarded spans or returned unbounded resources")
	}
	v = requestJSON(t, ws, "/api/view?runtime=runtime-03&points=300")
	if v["resource_error"] != "" || len(v["runtime_ids"].([]any)) != 45 {
		t.Fatal("runtime selector did not persist", v)
	}
	pp = v["profile"].(map[string]any)
	if len(pp["resources"].([]any)) != 30 {
		t.Fatal("runtime narrowing did not recover signal")
	}
}

func TestSameMetadataAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	p := profile.Demo()
	p.Name = "Alpha"
	data, _ := json.Marshal(p)
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	h, e := NewWorkspace(context.Background(), []Source{{Path: path}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	p.Name = "Bravo"
	replacement, _ := json.Marshal(p)
	if len(replacement) != len(data) {
		t.Fatal("fixture must retain size")
	}
	temp := filepath.Join(dir, "replacement")
	if e = os.WriteFile(temp, replacement, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(temp, info.ModTime(), info.ModTime()); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(temp, path); e != nil {
		t.Fatal(e)
	}
	ws.refresh()
	if ws.mounts[0].current.p.Name != "Bravo" || ws.mounts[0].current.revision != 2 {
		t.Fatal("same-metadata replacement was missed")
	}
}
func TestFailedFullReloadCannotSkipMalformedEventPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	initial := eventBytes(t, profile.Event{Type: "task", Task: &profile.Task{ID: "t", Name: "Task"}})
	if e := os.WriteFile(path, initial, 0600); e != nil {
		t.Fatal(e)
	}
	h, e := NewWorkspace(context.Background(), []Source{{Path: dir}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	tail := ws.mounts[0].events
	revision := ws.mounts[0].current.revision
	appendToStream := func(b []byte) {
		t.Helper()
		f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(b); e != nil {
			t.Fatal(e)
		}
		_ = f.Close()
	}
	appendToStream([]byte("malformed\n"))
	if e = os.WriteFile(filepath.Join(dir, "run-result.json"), []byte("invalid"), 0600); e != nil {
		t.Fatal(e)
	}
	ws.refresh()
	if ws.mounts[0].events != tail || !ws.mounts[0].reloadRequired {
		t.Fatal("failed full reload replaced event checkpoint")
	}
	if newEventTail(path) != nil {
		t.Fatal("malformed event prefix accepted")
	}
	appendToStream(eventBytes(t, profile.Event{Type: "span", Span: &profile.Span{ID: "s", TaskID: "t", Kind: "tool", DurationMS: 1}}))
	ws.refresh()
	if ws.mounts[0].current.revision != revision || ws.mounts[0].lastError == "" || len(ws.mounts[0].current.p.Spans) != 0 {
		t.Fatal("later append skipped invalid evidence and recovered incorrectly")
	}
	if e = os.Remove(filepath.Join(dir, "run-result.json")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, initial, 0600); e != nil {
		t.Fatal(e)
	}
	ws.refresh()
	if ws.mounts[0].lastError != "" || ws.mounts[0].reloadRequired {
		t.Fatal("valid repair did not recover")
	}
}
