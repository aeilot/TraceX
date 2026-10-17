package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aeilot/TraceX/internal/profile"
)

func TestRoutes(t *testing.T) {
	handler, err := New(profile.Demo())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path        string
		status      int
		contentType string
	}{
		{"/", 200, "text/html"},
		{"/app.js", 200, "javascript"},
		{"/profile.js", 200, "javascript"},
		{"/styles.css", 200, "text/css"},
		{"/api/profile", 200, "application/json"},
		{"/api/health", 200, "application/json"},
		{"/api/missing", 404, "text/plain"},
		{"/runs/private.log", 404, "text/plain"},
		{"/../go.mod", 404, "text/plain"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if r.Code != tc.status || !strings.Contains(r.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("status/type = %d %q", r.Code, r.Header().Get("Content-Type"))
			}
			if r.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
			if strings.HasPrefix(tc.path, "/api/") && tc.status == 200 {
				if !json.Valid(r.Body.Bytes()) || r.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("API must return JSON without caching")
				}
			}
		})
	}
}

func TestReadOnlyAndHead(t *testing.T) {
	handler, err := New(profile.Demo())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/api/profile", "/api/health"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if r.Code != http.StatusMethodNotAllowed || r.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("POST %s: status %d", path, r.Code)
		}
		r = httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodHead, path, nil))
		if r.Code != http.StatusOK || r.Body.Len() != 0 {
			t.Fatalf("HEAD %s: status %d body %q", path, r.Code, r.Body.String())
		}
	}
}
