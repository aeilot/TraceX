package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aeilot/TraceX/internal/experiment"
	"github.com/aeilot/TraceX/internal/profile"
)

func TestExperimentDisabledAndRequestBoundary(t *testing.T) {
	h, err := New(profile.Demo())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/experiments/catalog", "/api/experiments/jobs"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/api/experiments/catalog" && !strings.Contains(w.Body.String(), `"enabled":false`) {
			t.Fatal(w.Body.String())
		}
	}
	for _, tc := range []struct {
		path, origin, ctype, body string
		status                    int
	}{
		{"/api/experiments/start", "", "application/json", `{"profile_id":"safe","action":"run"}`, 409},
		{"/api/experiments/stop", "", "application/json", `{"job_id":"1"}`, 409},
		{"/api/experiments/start", "http://other.example", "application/json", `{}`, 403},
		{"/api/experiments/start", "http://example.com/path", "application/json", `{}`, 403},
		{"/api/experiments/start", "http://example.com", "text/plain", `{}`, 415},
		{"/api/experiments/start", "http://example.com", "application/json", `{"command":"private"}`, 400},
		{"/api/experiments/start", "", "application/json", `{} {}`, 400},
		{"/api/experiments/start", "", "application/json", `{"profile_id":"` + strings.Repeat("x", experimentBodyLimit) + `"}`, 413},
		{"/api/experiments/unknown", "", "application/json", `{}`, 405},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.ctype)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s origin %s: %d want %d: %s", tc.path, tc.origin, w.Code, tc.status, w.Body.String())
		}
	}
}
func TestDynamicExperimentSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := profile.Demo()
	h, err := NewWorkspace(ctx, []Source{{Profile: &p}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ws := h.(*workspace)
	ch := make(chan notice, 2)
	ws.mu.Lock()
	ws.listeners[ch] = true
	ws.mu.Unlock()
	path := filepath.Join(t.TempDir(), "profile.json")
	data, _ := json.Marshal(profile.DemoVariant())
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	id, err := ws.addSource("job-1", path)
	if err != nil || id != "run-2" {
		t.Fatalf("%s %v", id, err)
	}
	if next, e := ws.addSource("job-1", path); e != nil || next != id {
		t.Fatalf("duplicate %s %v", next, e)
	}
	select {
	case event := <-ch:
		if event.RunID != id {
			t.Fatal(event)
		}
	default:
		t.Fatal("missing source notification")
	}
	if next, e := ws.addSource("job-1", path+".other"); e != nil || next != id {
		t.Fatalf("job source changed: %s %v", next, e)
	}
	if len(ch) != 0 {
		t.Fatal("duplicate source notification")
	}
	w := httptest.NewRecorder()
	ws.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/profile?run="+id, nil))
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = ws.addSource("job-2", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing artifacts published")
	}
}

func TestConfiguredExperimentAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", "smoke.json"), []byte(`{"name":"Smoke","benchmark":{"type":"fixture","tasks":["a"]},"harness":{"type":"fixture"},"runtime":{"backend":"fixture"},"execution":{"concurrency":1}}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "fake-aries")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := profile.Demo()
	h, err := NewWorkspace(ctx, []Source{{Profile: &p}}, Options{Experiment: &experiment.Config{Root: root, Binary: binary}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.(*workspace).Close(context.Background())
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"profile_id":"smoke.json","action":"run"}`, 400},
		{`{"profile_id":"unknown.json","action":"prepare"}`, 404},
		{`{"profile_id":"smoke.json","action":"prepare","tasks":["missing"]}`, 400},
		{`{"profile_id":"smoke.json","action":"prepare"}`, 200},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/experiments/start", strings.NewReader(tc.body))
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s status %d: %s", tc.body, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), root) {
			t.Fatal("private job path leaked")
		}
	}
	if h.(*workspace).interval <= 0 {
		t.Fatal("experiment run would not refresh with --watch 0")
	}
}
