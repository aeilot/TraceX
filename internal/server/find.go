package server

import (
	"math"
	"net/http"
	"strings"
)

// find locates a match in the filtered execution context without filtering the
// context itself. Offsets use the same stable order and predicates as view.
func (ws *workspace) find(w http.ResponseWriter, r *http.Request, s *snapshot) {
	start, err := numeric(r, "start_ms", 0)
	if err != nil {
		ws.fail(w, r, 400, "invalid interval")
		return
	}
	end, err := numeric(r, "end_ms", math.MaxFloat64)
	if err != nil || end < start {
		ws.fail(w, r, 400, "invalid interval")
		return
	}
	index, err := numeric(r, "index", 0)
	if err != nil || math.Trunc(index) != index || index > math.MaxInt32 {
		ws.fail(w, r, 400, "invalid match index")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	task, kind := r.URL.Query().Get("task"), r.URL.Query().Get("kind")
	total, offset, targetOffset, actualIndex := 0, 0, 0, 0
	id := ""
	for _, span := range s.p.Spans {
		if span.StartMS > end || span.StartMS+span.DurationMS < start || task != "" && task != span.TaskID || kind != "" && kind != span.Kind {
			continue
		}
		if q != "" && strings.Contains(strings.ToLower(span.Name+" "+span.ID+" "+span.AgentID+" "+span.TaskID+" "+span.Kind), q) {
			if total == 0 || total == int(index) {
				id, targetOffset, actualIndex = span.ID, offset, total
			}
			total++
		}
		offset++
	}
	ws.reply(w, r, map[string]any{"total": total, "index": actualIndex, "span_id": id, "offset": targetOffset})
}
