package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (ws *workspace) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		ws.fail(w, r, 500, "streaming unsupported")
		return
	}
	ch := make(chan notice, 32)
	ws.mu.Lock()
	ws.listeners[ch] = true
	last, _ := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
	history := append([]notice{}, ws.history...)
	seq := ws.seq
	ws.mu.Unlock()
	defer func() { ws.mu.Lock(); delete(ws.listeners, ch); ws.mu.Unlock() }()
	send := func(n notice) bool {
		data, _ := json.Marshal(n)
		_, e := fmt.Fprintf(w, "id: %d\nevent: revision\ndata: %s\n\n", n.Sequence, data)
		f.Flush()
		return e == nil
	}
	if last > 0 {
		for _, n := range history {
			if n.Sequence > last {
				if !send(n) {
					return
				}
			}
		}
	}
	fmt.Fprintf(w, ": connected %d\n\n", seq)
	f.Flush()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ws.ctx.Done():
			return
		case n := <-ch:
			if !send(n) {
				return
			}
		case <-t.C:
			if _, e := fmt.Fprint(w, ": heartbeat\n\n"); e != nil {
				return
			}
			f.Flush()
		}
	}
}
