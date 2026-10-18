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
// plot size rather than by the run's span count.
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
	width = min(width, 20000)
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
	layout := profile.LayoutTimeline(matches, start, end, width)
	ws.reply(w, r, map[string]any{"run_id": id, "revision": s.revision, "start_ms": start, "end_ms": end, "width": width, "layout": layout})
}
