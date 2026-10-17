package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/aeilot/TraceX/internal/profile"
	"github.com/aeilot/TraceX/web"
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
		{"/i18n.js", 200, "javascript"},
		{"/app.js", 200, "javascript"},
		{"/profile.js", 200, "javascript"},
		{"/styles.css", 200, "text/css"},
		{"/lab.css", 200, "text/css"},
		{"/api/profile", 200, "application/json"},
		{"/api/health", 200, "application/json"},
		{"/api/missing", 404, "application/json"},
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

// TestPageAssetsServed fetches every script and stylesheet the page names, so
// a new asset cannot be embedded yet missing from the served allowlist.
func TestPageAssetsServed(t *testing.T) {
	handler, err := New(profile.Demo())
	if err != nil {
		t.Fatal(err)
	}
	page, err := web.Files.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	assets := regexp.MustCompile(`(?:src|href)="(/[^"/]+\.(?:js|css))"`).FindAllStringSubmatch(string(page), -1)
	if len(assets) == 0 {
		t.Fatal("no page assets found")
	}
	for _, asset := range assets {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, asset[1], nil))
		if r.Code != http.StatusOK {
			t.Errorf("%s: status %d", asset[1], r.Code)
		}
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
