// Package server serves indexed run snapshots, the profiler UI, and optional experiment controls.
package server

import (
	"context"
	"encoding/json"
	"github.com/aeilot/TraceX/internal/profile"
	"github.com/aeilot/TraceX/web"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

func New(p profile.Profile) (http.Handler, error) {
	return NewWorkspace(context.Background(), []Source{{Profile: &p}}, Options{})
}
func (ws *workspace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
	}
	if strings.HasPrefix(r.URL.Path, "/api/experiments/") {
		ws.experiments(w, r)
		return
	}
	if r.URL.Path == "/api/insights/ai" {
		ws.insightAI(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		ws.fail(w, r, 405, "method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/health":
			ws.reply(w, r, map[string]string{"status": "ok"})
		case "/api/runs":
			ws.runs(w, r)
		case "/api/events":
			ws.events(w, r)
		case "/api/profile", "/api/view", "/api/timeline", "/api/span", "/api/analysis", "/api/sweep", "/api/find", "/api/insights":
			ws.query(w, r)
		default:
			ws.fail(w, r, http.StatusNotFound, "unknown API route")
		}
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if name != "index.html" && name != "i18n.js" && name != "app.js" && name != "profile.js" && name != "analytics.js" && name != "styles.css" && name != "lab.css" && name != "experiments.js" && name != "experiments.css" && name != "experiment-model.js" && name != "workspace.js" && name != "workspace-model.js" && name != "workspace.css" && name != "motion.css" && name != "motion.js" && name != "insight-model.js" && name != "insights.js" && name != "insights.css" {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(web.Files, name); err != nil {
		http.NotFound(w, r)
		return
	}
	http.FileServer(http.FS(web.Files)).ServeHTTP(w, r)
}
func (ws *workspace) reply(w http.ResponseWriter, r *http.Request, v any) {
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func (ws *workspace) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.WriteHeader(status)
	ws.reply(w, r, map[string]string{"error": msg})
}
func HTTP(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}
