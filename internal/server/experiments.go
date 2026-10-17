package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/aeilot/TraceX/internal/experiment"
)

const experimentBodyLimit = 64 << 10

func (ws *workspace) experiments(w http.ResponseWriter, r *http.Request) {
	read := r.URL.Path == "/api/experiments/catalog" || r.URL.Path == "/api/experiments/jobs"
	write := r.URL.Path == "/api/experiments/start" || r.URL.Path == "/api/experiments/stop"
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
	if read {
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
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			ws.fail(w, r, 403, "experiment requests must be same-origin")
			return
		}
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
	var target any = &request
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
	if ws.experiment == nil {
		ws.fail(w, r, 409, "experiment controls are disabled; configure --aries-dir")
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
