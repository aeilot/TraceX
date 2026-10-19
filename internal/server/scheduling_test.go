package server

import (
	"context"
	"testing"
)

// The projection covers every task of a large run, independent of DAG pages,
// and rejects invalid windows.
func TestSchedulingCoversTheWholeRun(t *testing.T) {
	if testing.Short() {
		t.Skip("large synthetic scale fixture")
	}
	p := largeProfile()
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	ws := h.(*workspace)
	v := requestJSON(t, ws, "/api/scheduling?bins=50")["schedule"].(map[string]any)
	if v["lanes_total"].(float64) != 1 || len(v["series"].([]any)) == 0 {
		t.Fatalf("schedule = %v lanes, %d series", v["lanes_total"], len(v["series"].([]any)))
	}
	// All 50,000 tool spans of the one task are in its work layer's union.
	impact := v["impact"].([]any)[0].(map[string]any)
	if impact["tool_ms"].(float64) < 99000 {
		t.Fatalf("tool union = %v", impact["tool_ms"])
	}
	again := requestJSON(t, ws, "/api/scheduling?bins=50")["schedule"].(map[string]any)
	if again["lanes_total"] != v["lanes_total"] {
		t.Fatal("cached projection differs")
	}
	for _, bad := range []string{"/api/scheduling?start_ms=10&end_ms=5", "/api/scheduling?bins=0", "/api/scheduling?bins=2.5", "/api/scheduling?lanes=0"} {
		if code := requestStatus(ws, bad); code != 400 {
			t.Fatalf("%s: %d", bad, code)
		}
	}
}
