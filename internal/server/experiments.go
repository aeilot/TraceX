package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aeilot/TraceX/internal/experiment"
)

const experimentBodyLimit = 64 << 10

func (ws *workspace) experiments(w http.ResponseWriter, r *http.Request) {
	read := r.URL.Path == "/api/experiments/catalog" || r.URL.Path == "/api/experiments/jobs" || r.URL.Path == "/api/experiments/runner" || r.URL.Path == "/api/experiments/discover"
	write := r.URL.Path == "/api/experiments/start" || r.URL.Path == "/api/experiments/stop" || r.URL.Path == "/api/experiments/configure" || r.URL.Path == "/api/experiments/check"
	if (read && r.Method != http.MethodGet && r.Method != http.MethodHead) || (write && r.Method != http.MethodPost) || (!read && !write && r.Method != http.MethodGet && r.Method != http.MethodHead) {
		allow := "GET, HEAD"
		if write {
			allow = "POST"
		}
		w.Header().Set("Allow", allow)
		ws.fail(w, r, 405, "method not allowed")
		return
	}
	if !read && !write {
		ws.fail(w, r, 404, "unknown API route")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			ws.fail(w, r, 403, "experiment requests must be same-origin")
			return
		}
	}
	// Runner setup exposes local paths and changes execution authority; keep it local.
	setup := r.URL.Path == "/api/experiments/runner" || r.URL.Path == "/api/experiments/discover" || r.URL.Path == "/api/experiments/configure" || r.URL.Path == "/api/experiments/check"
	if setup {
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		target := r.Host
		if h, _, err := net.SplitHostPort(target); err == nil {
			target = h
		}
		ip := net.ParseIP(target)
		if e != nil || !net.ParseIP(host).IsLoopback() || (target != "localhost" && (ip == nil || !ip.IsLoopback())) {
			ws.fail(w, r, 403, "runner setup requires a loopback connection and host")
			return
		}
	}
	if read {
		if r.URL.Path == "/api/experiments/runner" {
			ws.reply(w, r, ws.experiment.Settings())
			return
		}
		if r.URL.Path == "/api/experiments/discover" {
			ws.reply(w, r, experiment.Discover(r.Context(), ws.experiment.Settings().Root))
			return
		}
		if r.URL.Path == "/api/experiments/catalog" {
			if ws.experiment == nil {
				ws.reply(w, r, experiment.Catalog{Message: "Experiment controls disabled. Restart TraceX with --aries-dir to enable ARIES execution.", Profiles: []experiment.Preset{}})
			} else {
				ws.reply(w, r, ws.experiment.Catalog())
			}
		} else {
			jobs := []experiment.Job{}
			if ws.experiment != nil {
				jobs = ws.experiment.Jobs()
			}
			ws.reply(w, r, map[string]any{"jobs": jobs})
		}
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		ws.fail(w, r, 415, "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, experimentBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request experiment.Request
	var stop struct {
		JobID string `json:"job_id"`
	}
	var settings experiment.Settings
	var check struct{}
	var target any = &request
	if r.URL.Path == "/api/experiments/configure" {
		target = &settings
	}
	if r.URL.Path == "/api/experiments/check" {
		target = &check
	}
	if r.URL.Path == "/api/experiments/stop" {
		target = &stop
	}
	if err = decoder.Decode(target); err == nil {
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			if e == nil {
				err = errors.New("multiple values")
			} else {
				err = e
			}
		}
	}
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			ws.fail(w, r, 413, "experiment request is too large")
		} else {
			ws.fail(w, r, 400, "invalid experiment request JSON")
		}
		return
	}
	if ws.experiment == nil || (!setup && !ws.experiment.Catalog().Enabled) {
		ws.fail(w, r, 409, "experiment controls are disabled; configure the ARIES runner")
		return
	}
	if r.URL.Path == "/api/experiments/check" {
		ws.reply(w, r, map[string]any{"checks": ws.experiment.Check(r.Context())})
		return
	}
	if r.URL.Path == "/api/experiments/configure" {
		err = ws.experiment.Configure(settings, ws.runnerConfigPath)
		if err == nil {
			ws.mu.Lock()
			startWatcher := ws.interval == 0
			if startWatcher {
				ws.interval = time.Second
			}
			ws.mu.Unlock()
			if startWatcher {
				go ws.watch()
			}
			ws.reply(w, r, ws.experiment.Catalog())
			return
		}
		code := 500
		message := "Could not save runner settings"
		if errors.Is(err, experiment.ErrBusy) {
			code = 409
			message = "An experiment is active; stop it before changing the runner"
		}
		if errors.Is(err, experiment.ErrInvalid) {
			code = 400
			message = "Select a valid ARIES directory with profiles and a local or OrbStack target"
		}
		ws.fail(w, r, code, message)
		return
	}
	var job experiment.Job
	if r.URL.Path == "/api/experiments/start" {
		job, err = ws.experiment.Start(request)
	} else {
		job, err = ws.experiment.Stop(stop.JobID)
	}
	if err != nil {
		status := 500
		message := "experiment operation failed"
		switch {
		case errors.Is(err, experiment.ErrInvalid):
			status = 400
			message = "invalid experiment selection or missing model-call confirmation"
		case errors.Is(err, experiment.ErrNotFound):
			status = 404
			message = "unknown experiment or profile"
		case errors.Is(err, experiment.ErrBusy):
			status = 409
			message = "an experiment is already active"
		case errors.Is(err, experiment.ErrNotReady):
			status = 503
			message = "ARIES execution is not ready; inspect catalog readiness"
		}
		ws.fail(w, r, status, message)
		return
	}
	ws.reply(w, r, map[string]any{"job": job})
}
