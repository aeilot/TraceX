package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aeilot/TraceX/internal/profile"
)

func TestInsightsUseFullRunAndScope(t *testing.T) {
	h, err := New(profile.Demo())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/insights?run=run-1&q=absent&limit=1", "/api/insights?run=run-1&span=plan"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		var out struct {
			Report profile.Insights `json:"report"`
			Scoped bool             `json:"scoped"`
		}
		if json.Unmarshal(r.Body.Bytes(), &out) != nil {
			t.Fatal("bad json")
		}
		if strings.Contains(path, "q=") && out.Report.SpanCount != 18 {
			t.Fatal("diagnosis was page filtered")
		}
		if strings.Contains(path, "span=") && (!out.Scoped || out.Report.SpanCount >= 18) {
			t.Fatal("span scope was ignored")
		}
	}
}
func TestAIExplicitSendValidationAndReferences(t *testing.T) {
	var calls atomic.Int32
	var invalid atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			t.Error("missing auth")
		}
		var request map[string]any
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("bad request")
		}
		if request["model"] != "fixture-model" {
			t.Error("wrong model")
		}
		id := "run/span/s1"
		if invalid.Load() {
			id = "invented"
		}
		content := fmt.Sprintf(`{"observations":[{"fact":"A measured interval","evidence_ids":[%q],"hypothesis":"Unknown cause","next_experiment":"Compare a controlled configuration"}],"limitations":"Fixture only"}`, id)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
	}))
	defer upstream.Close()
	p := profile.Demo()
	h, e := NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{Insight: &InsightConfig{Endpoint: upstream.URL, Model: "fixture-model", APIKey: "fixture-only"}})
	if e != nil {
		t.Fatal(e)
	}
	packet := InsightPacket{Scope: "run", RunID: "run", Version: "1", Groups: []profile.CallGroup{{Kind: "model", Name: "model", SpanIDs: []string{"run/span/s1"}}}}
	request := func(confirm bool, origin string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"confirm": confirm, "evidence": packet})
		r := httptest.NewRequest("POST", "http://local/api/insights/ai", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if r := request(false, "http://local"); r.Code != 400 {
		t.Fatal(r.Code)
	}
	if r := request(true, "http://other"); r.Code != 403 {
		t.Fatal(r.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("model was called without authorization")
	}
	if r := request(true, "http://local"); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	invalid.Store(true)
	if r := request(true, "http://local"); r.Code != 502 {
		t.Fatal("invented citation accepted")
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/insights/ai", nil))
	if strings.Contains(r.Body.String(), "fixture-only") {
		t.Fatal("credential exposed")
	}
}
