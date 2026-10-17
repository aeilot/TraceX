package server

import (
	"context"
	"fmt"
	"github.com/aeilot/TraceX/internal/profile"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Source struct {
	Path    string
	Name    string
	Profile *profile.Profile
}
type Options struct{ WatchInterval time.Duration }
type snapshot struct {
	p         profile.Profile
	analysis  any
	byID      map[string]profile.Span
	prefixEnd []float64
	revision  uint64
}
type mounted struct {
	source         Source
	current        *snapshot
	signature      string
	lastError      string
	events         *eventTail
	otherSignature string
	loader         *profile.Loader
	identities     map[string]os.FileInfo
	reloadRequired bool
}
type notice struct {
	RunID    string `json:"run_id"`
	Revision uint64 `json:"revision"`
	Error    string `json:"error,omitempty"`
	Sequence uint64 `json:"-"`
}
type workspace struct {
	mu        sync.RWMutex
	mounts    []*mounted
	interval  time.Duration
	seq       uint64
	history   []notice
	listeners map[chan notice]bool
	ctx       context.Context
}

func NewWorkspace(ctx context.Context, sources []Source, opts Options) (http.Handler, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one source required")
	}
	if opts.WatchInterval < 0 {
		return nil, fmt.Errorf("negative watch interval")
	}
	ws := &workspace{interval: opts.WatchInterval, listeners: map[chan notice]bool{}, ctx: ctx}
	for _, src := range sources {
		loader := profile.NewLoader(src.Path)
		p := profile.Profile{}
		var err error
		if src.Profile != nil {
			p = *src.Profile
		} else {
			p, err = loader.Load()
		}
		if err != nil {
			return nil, fmt.Errorf("load source %d: %w", len(ws.mounts)+1, err)
		}
		if err = profile.Validate(&p); err != nil {
			return nil, err
		}
		if src.Name != "" {
			p.Name = src.Name
		}
		sig, identities := fingerprint(src.Path)
		m := &mounted{source: src, current: build(p, 1), signature: sig, otherSignature: signatureExceptEvents(src.Path), identities: identities}
		m.loader = loader
		m.events = newEventTail(eventPath(src.Path))
		ws.mounts = append(ws.mounts, m)
	}
	if opts.WatchInterval > 0 {
		go ws.watch()
	}
	return ws, nil
}
func build(p profile.Profile, rev uint64) *snapshot {
	// Own the slices: queries never mutate the published snapshot.
	p.Spans = append([]profile.Span{}, p.Spans...)
	p.Resources = append([]profile.Resource{}, p.Resources...)
	sort.Slice(p.Spans, func(i, j int) bool {
		a, b := p.Spans[i], p.Spans[j]
		if a.StartMS == b.StartMS {
			return a.ID < b.ID
		}
		return a.StartMS < b.StartMS
	})
	sort.SliceStable(p.Resources, func(i, j int) bool { return p.Resources[i].TimeMS < p.Resources[j].TimeMS })
	s := &snapshot{p: p, revision: rev, byID: map[string]profile.Span{}, prefixEnd: make([]float64, len(p.Spans)), analysis: profile.Analyze(p)}
	end := 0.0
	for i, v := range p.Spans {
		s.byID[v.ID] = v
		if v.StartMS+v.DurationMS > end {
			end = v.StartMS + v.DurationMS
		}
		s.prefixEnd[i] = end
	}
	return s
}
func fingerprint(path string) (string, map[string]os.FileInfo) {
	identities := map[string]os.FileInfo{}
	if path == "" {
		return "", identities
	}
	var b strings.Builder
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		i, e := d.Info()
		if e == nil {
			identities[p] = i
			fmt.Fprintf(&b, "%s:%d:%d;", p, i.Size(), i.ModTime().UnixNano())
		}
		return nil
	})
	return b.String(), identities
}
func sameIdentities(a, b map[string]os.FileInfo, exclude string) bool {
	for path, old := range a {
		if path == exclude {
			continue
		}
		next, ok := b[path]
		if !ok || !os.SameFile(old, next) {
			return false
		}
	}
	for path := range b {
		if path == exclude {
			continue
		}
		if _, ok := a[path]; !ok {
			return false
		}
	}
	return true
}
func (ws *workspace) watch() {
	t := time.NewTicker(ws.interval)
	defer t.Stop()
	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-t.C:
			ws.refresh()
		}
	}
}
func (ws *workspace) refresh() {
	for i, m := range ws.mounts {
		if m.source.Path == "" {
			continue
		}
		sig, identities := fingerprint(m.source.Path)
		if sig == m.signature && sameIdentities(m.identities, identities, "") {
			continue
		}
		other := signatureExceptEvents(m.source.Path)
		var p profile.Profile
		var err error
		fullLoad := false
		if !m.reloadRequired && other == m.otherSignature && m.events != nil && sameIdentities(m.identities, identities, eventPath(m.source.Path)) {
			var reset bool
			p, reset, err = m.events.read(m.current.p)
			if reset {
				fullLoad = true
				p, err = m.loader.Load()
			}
		} else {
			fullLoad = true
			p, err = m.loader.Load()
		}
		m.identities = identities
		m.signature = sig
		m.otherSignature = other
		if err == nil {
			err = profile.Validate(&p)
		}
		if fullLoad {
			m.reloadRequired = err != nil
			if err == nil {
				m.events = newEventTail(eventPath(m.source.Path))
			}
		}
		var next *snapshot
		if err == nil {
			if m.source.Name != "" {
				p.Name = m.source.Name
			}
			next = build(p, m.current.revision+1)
		}
		ws.mu.Lock()
		oldError := m.lastError
		if err != nil {
			m.lastError = "Source refresh failed; last valid snapshot retained"
		} else {
			m.current = next
			m.lastError = ""
		}
		if err == nil || oldError != m.lastError {
			ws.notifyLocked(notice{RunID: fmt.Sprintf("run-%d", i+1), Revision: m.current.revision, Error: m.lastError})
		}
		ws.mu.Unlock()
	}
}
func (ws *workspace) notifyLocked(n notice) {
	ws.seq++
	n.Sequence = ws.seq
	ws.history = append(ws.history, n)
	if len(ws.history) > 256 {
		ws.history = ws.history[len(ws.history)-256:]
	}
	for ch := range ws.listeners {
		select {
		case ch <- n:
		default:
		}
	}
}
func (ws *workspace) runs(w http.ResponseWriter, r *http.Request) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	rows := []any{}
	for i, m := range ws.mounts {
		s := m.current
		p := s.p
		rows = append(rows, map[string]any{"id": fmt.Sprintf("run-%d", i+1), "name": p.Name, "run_id": p.RunID, "duration_ms": p.DurationMS, "span_count": len(p.Spans), "resource_count": len(p.Resources), "revision": s.revision, "live": ws.interval > 0 && m.source.Path != "", "last_error": m.lastError, "configuration": p.Configuration, "capabilities": p.Capabilities})
	}
	ws.reply(w, r, map[string]any{"runs": rows, "watch_interval_ms": ws.interval.Milliseconds()})
}
func (ws *workspace) selected(id string) (*snapshot, string) {
	if id == "" {
		id = "run-1"
	}
	n, e := strconv.Atoi(strings.TrimPrefix(id, "run-"))
	if e != nil || n < 1 || n > len(ws.mounts) || id != fmt.Sprintf("run-%d", n) {
		return nil, id
	}
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.mounts[n-1].current, id
}
func (ws *workspace) query(w http.ResponseWriter, r *http.Request) {
	s, id := ws.selected(r.URL.Query().Get("run"))
	if s == nil {
		ws.fail(w, r, 404, "unknown run")
		return
	}
	switch r.URL.Path {
	case "/api/profile":
		ws.reply(w, r, s.p)
	case "/api/analysis":
		ws.reply(w, r, s.analysis)
	case "/api/span":
		span, ok := s.byID[r.URL.Query().Get("id")]
		if !ok {
			ws.fail(w, r, 404, "unknown span")
			return
		}
		edges := []profile.Edge{}
		for _, e := range s.p.Edges {
			if e.Source == span.ID || e.Target == span.ID {
				edges = append(edges, e)
			}
		}
		stats, err := profile.WindowStats(s.p, span.ID)
		if err != nil {
			ws.fail(w, r, 500, "resource statistics unavailable")
			return
		}
		ws.reply(w, r, map[string]any{"span": span, "edges": edges, "resource_stats": stats})
	case "/api/view":
		ws.view(w, r, s, id)
	}
}
