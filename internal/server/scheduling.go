package server

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/aeilot/TraceX/internal/profile"
)

// scheduleCache holds recent projections per immutable snapshot and query.
// A new revision produces new keys; the map is bounded by clearing it.
var scheduleCache = struct {
	sync.Mutex
	entries map[string]profile.ScheduleView
}{entries: map[string]profile.ScheduleView{}}

const scheduleCacheSize = 64

// scheduling projects the whole run (never a DAG page) onto one window:
// occurrence lanes, concurrency and resource series and the impact table.
func (ws *workspace) scheduling(w http.ResponseWriter, r *http.Request, s *snapshot, id string) {
	start, e := numeric(r, "start_ms", 0)
	if e != nil {
		ws.fail(w, r, 400, e.Error())
		return
	}
	end, e := numeric(r, "end_ms", s.p.DurationMS)
	if e != nil || end <= start {
		ws.fail(w, r, 400, "invalid interval")
		return
	}
	bins, e := numeric(r, "bins", 400)
	if e != nil || bins < 1 || bins > 2000 || math.Trunc(bins) != bins {
		ws.fail(w, r, 400, "invalid bins")
		return
	}
	width, e := numeric(r, "width", 1000)
	if e != nil || width < 1 || math.IsInf(width, 0) {
		ws.fail(w, r, 400, "invalid width")
		return
	}
	lanes, e := numeric(r, "lanes", 200)
	if e != nil || lanes < 1 || lanes > 2000 || math.Trunc(lanes) != lanes {
		ws.fail(w, r, 400, "invalid lanes")
		return
	}
	var pinned []string
	if raw := r.URL.Query().Get("pinned"); raw != "" {
		pinned = strings.Split(raw, ",")
		if len(pinned) > 20 {
			ws.fail(w, r, 400, "at most 20 pinned occurrences")
			return
		}
	}
	key := fmt.Sprintf("%s|%d|%g|%g|%g|%g|%g|%s", id, s.revision, start, end, bins, math.Min(width, 20000), lanes, strings.Join(pinned, ","))
	scheduleCache.Lock()
	view, ok := scheduleCache.entries[key]
	scheduleCache.Unlock()
	if !ok {
		view = profile.Schedule(s.p, profile.ScheduleQuery{StartMS: start, EndMS: end, Bins: int(bins), WidthPX: math.Min(width, 20000), Pinned: pinned, MaxLanes: int(lanes)})
		scheduleCache.Lock()
		if len(scheduleCache.entries) >= scheduleCacheSize {
			scheduleCache.entries = map[string]profile.ScheduleView{}
		}
		scheduleCache.entries[key] = view
		scheduleCache.Unlock()
	}
	ws.reply(w, r, map[string]any{"run_id": id, "revision": s.revision, "schedule": view})
}
