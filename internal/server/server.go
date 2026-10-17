// Package server serves a read-only snapshot and the embedded profiler UI.
package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/aeilot/TraceX/internal/profile"
	"github.com/aeilot/TraceX/web"
)

// New returns a handler for a fixed snapshot. No run directories or raw logs are served.
func New(p profile.Profile) (http.Handler, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode profile: %w", err)
	}
	assets := http.FileServer(http.FS(web.Files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/api/profile":
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if r.Method != http.MethodHead {
				_, _ = w.Write(data)
			}
		case "/api/health":
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
			}
		default:
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			// An explicit allowlist also prevents FileServer directory listings.
			if name != "index.html" && name != "app.js" && name != "profile.js" && name != "styles.css" {
				http.NotFound(w, r)
				return
			}
			if _, err := fs.Stat(web.Files, name); err != nil {
				http.NotFound(w, r)
				return
			}
			assets.ServeHTTP(w, r)
		}
	}), nil
}

// HTTP configures bounded headers and idle connections for a local server.
func HTTP(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}
