package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aeilot/TraceX/internal/experiment"
	"github.com/aeilot/TraceX/internal/profile"
)

func TestRunnerCanBeConfiguredWithoutCLI(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "profiles"), 0700)
	os.Mkdir(filepath.Join(root, "bin"), 0700)
	os.WriteFile(filepath.Join(root, "profiles", "smoke.json"), []byte(`{"name":"Smoke","benchmark":{"type":"fixture","tasks":["a"]},"harness":{"type":"fixture"},"runtime":{"backend":"fixture"}}`), 0600)
	os.WriteFile(filepath.Join(root, "bin", "aries"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := profile.Demo()
	save := filepath.Join(t.TempDir(), "runner.json")
	h, err := NewWorkspace(ctx, []Source{{Profile: &p}}, Options{RunnerConfigPath: save})
	if err != nil {
		t.Fatal(err)
	}
	defer h.(*workspace).Close(context.Background())
	body, _ := json.Marshal(experiment.Settings{Root: root, Executor: "local"})
	for _, tc := range []struct {
		method, path, body, remote, host, origin string
		want                                     int
	}{
		{"GET", "runner", "", "127.0.0.1:1234", "127.0.0.1:4318", "", 200},
		{"POST", "configure", string(body), "192.0.2.1:1234", "127.0.0.1:4318", "", 403},
		{"POST", "configure", string(body), "127.0.0.1:1234", "evil.example", "", 403},
		{"POST", "configure", string(body), "127.0.0.1:1234", "127.0.0.1:4318", "https://evil.example", 403},
		{"GET", "configure", "", "127.0.0.1:1234", "127.0.0.1:4318", "", 405},
		{"POST", "configure", `{"root":"relative","executor":"local"}`, "127.0.0.1:1234", "127.0.0.1:4318", "", 400},
		{"POST", "configure", string(body), "127.0.0.1:1234", "127.0.0.1:4318", "", 200},
		{"GET", "runner", "", "[::1]:1234", "[::1]:4318", "", 200},
	} {
		r := httptest.NewRequest(tc.method, "/api/experiments/"+tc.path, strings.NewReader(tc.body))
		r.RemoteAddr = tc.remote
		r.Host = tc.host
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	data, err := os.ReadFile(save)
	if err != nil {
		t.Fatal(err)
	}
	var settings experiment.Settings
	json.Unmarshal(data, &settings)
	restored, err := experiment.New(ctx, experiment.Config{Root: settings.Root, Binary: settings.Binary, Executor: settings.Executor, Machine: settings.Machine})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(context.Background())
	if c := restored.Catalog(); !c.Ready || len(c.Profiles) != 1 {
		t.Fatalf("restore: %+v", c)
	}
	if !h.(*workspace).experiment.Catalog().Ready || h.(*workspace).interval == 0 {
		t.Fatal("runner not live after configuration")
	}
	session, err := NewWorkspace(ctx, []Source{{Profile: &p}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.(*workspace).Close(context.Background())
	r := httptest.NewRequest("POST", "http://127.0.0.1:4318/api/experiments/configure", strings.NewReader(string(body)))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	session.ServeHTTP(w, r)
	if w.Code != 200 || session.(*workspace).interval == 0 {
		t.Fatalf("session-only runner did not enable watching: %d %s", w.Code, w.Body.String())
	}

}
