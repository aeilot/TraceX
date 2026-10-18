package server

import (
	"math"
	"net/http"
	"sort"

	"github.com/aeilot/TraceX/internal/profile"
)

// timeline lays out every span matching the window and filters for one plot
// width. Unlike /api/view it is not paginated: spans too small to tell apart
// at that width are merged into clusters, which bounds the response by the
// plot size rather than by the run's span count. from_ms and to_ms return
// only the items near a zoomed-in viewport, with the full window's lanes.
func (ws *workspace) timeline(w http.ResponseWriter, r *http.Request, s *snapshot, id string) {
	start, e := numeric(r, "start_ms", 0)
	if e != nil {
		ws.fail(w, r, 400, e.Error())
		return
	}
	end, e := numeric(r, "end_ms", s.p.DurationMS)
	if e != nil || end < start {
		ws.fail(w, r, 400, "invalid interval")
		return
	}
	width, e := numeric(r, "width", 1000)
	if e != nil || width < 1 || math.IsInf(width, 0) {
		ws.fail(w, r, 400, "invalid width")
		return
	}
	// 64x zoom of a wide screen; only the requested slice is returned.
	width = min(width, 500000)
	from, e := numeric(r, "from_ms", start)
	if e != nil {
		ws.fail(w, r, 400, e.Error())
		return
	}
	to, e := numeric(r, "to_ms", end)
	if e != nil || to < from {
		ws.fail(w, r, 400, "invalid slice")
		return
	}
	task, kind := r.URL.Query().Get("task"), r.URL.Query().Get("kind")
	first := sort.Search(len(s.prefixEnd), func(i int) bool { return s.prefixEnd[i] >= start })
	last := sort.Search(len(s.p.Spans), func(i int) bool { return s.p.Spans[i].StartMS > end })
	matches := make([]profile.Span, 0, max(0, last-first))
	for _, v := range s.p.Spans[first:last] {
		if v.StartMS+v.DurationMS < start || (task != "" && v.TaskID != task) || (kind != "" && v.Kind != kind) {
			continue
		}
		matches = append(matches, v)
	}
	layout := profile.LayoutTimeline(matches, start, end, width).Slice(from, to)
	ws.reply(w, r, map[string]any{"run_id": id, "revision": s.revision, "start_ms": start, "end_ms": end, "width": width, "from_ms": from, "to_ms": to, "layout": layout})
}
