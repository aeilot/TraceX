package server

import (
	"context"
	"net/http/httptest"
	"testing"
)

// The timeline is not paginated: every matching span is counted, and spans
// too small to tell apart are merged, so the response stays bounded.
func TestTimelineCoversEverySpanWithoutPages(t *testing.T) {
	if testing.Short() {
		t.Skip("large synthetic scale fixture")
	}
	p := largeProfile()
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	layout := func(path string) (map[string]any, []any) {
		v := requestJSON(t, ws, path)
		l := v["layout"].(map[string]any)
		return l, l["items"].([]any)
	}
	l, items := layout("/api/timeline?width=1000")
	if l["spans"].(float64) != 50000 {
		t.Fatalf("spans = %v", l["spans"])
	}
	counted := 0.0
	for _, x := range items {
		counted += x.(map[string]any)["count"].(float64)
	}
	if counted != 50000 || len(items) > 50 {
		t.Fatalf("items = %d covering %v spans", len(items), counted)
	}
	// 100 ms across 1000 px: every 10 ms span is 100 px wide and its own bar.
	l, items = layout("/api/timeline?start_ms=0&end_ms=100&width=1000")
	if len(items) != int(l["spans"].(float64)) || len(items) == 0 {
		t.Fatalf("zoomed in: %d items for %v spans", len(items), l["spans"])
	}
	for _, x := range items {
		if x.(map[string]any)["span"] == nil {
			t.Fatal("a resolvable span was merged")
		}
	}
	if l, _ = layout("/api/timeline?kind=model&width=1000"); l["spans"].(float64) != 0 {
		t.Fatal("kind filter ignored")
	}
	for _, bad := range []string{"/api/timeline?width=0", "/api/timeline?start_ms=10&end_ms=5", "/api/timeline?width=x"} {
		r := requestStatus(ws, bad)
		if r != 400 {
			t.Fatalf("%s: status %d", bad, r)
		}
	}
}

func requestStatus(w *workspace, path string) int {
	r := httptest.NewRecorder()
	w.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
	return r.Code
}
