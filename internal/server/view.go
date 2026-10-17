package server

import (
	"fmt"
	"github.com/aeilot/TraceX/internal/profile"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func numeric(r *http.Request, key string, def float64) (float64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	x, e := strconv.ParseFloat(v, 64)
	if e != nil || math.IsNaN(x) || math.IsInf(x, 0) || x < 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return x, nil
}
func (ws *workspace) view(w http.ResponseWriter, r *http.Request, s *snapshot, id string) {
	start, e := numeric(r, "start_ms", 0)
	if e != nil {
		ws.fail(w, r, 400, e.Error())
		return
	}
	end, e := numeric(r, "end_ms", math.MaxFloat64)
	if e != nil || end < start {
		ws.fail(w, r, 400, "invalid interval")
		return
	}
	params := map[string]int{}
	for k, d := range map[string]int{"offset": 0, "limit": 250, "points": 300} {
		v, err := numeric(r, k, float64(d))
		if err != nil || math.Trunc(v) != v || v > float64(math.MaxInt32) {
			ws.fail(w, r, 400, "invalid "+k)
			return
		}
		params[k] = int(v)
	}
	offset, limit, points := params["offset"], params["limit"], params["points"]
	if limit < 1 || points < 1 {
		ws.fail(w, r, 400, "limit and points must be positive")
		return
	}
	if limit > 1000 {
		limit = 1000
	}
	if points > 1000 {
		points = 1000
	}
	task, kind, q := r.URL.Query().Get("task"), r.URL.Query().Get("kind"), strings.ToLower(r.URL.Query().Get("q"))
	first := sort.Search(len(s.prefixEnd), func(i int) bool { return s.prefixEnd[i] >= start })
	last := sort.Search(len(s.p.Spans), func(i int) bool { return s.p.Spans[i].StartMS > end })
	matches := make([]profile.Span, 0, limit)
	total := 0
	for _, v := range s.p.Spans[first:last] {
		if v.StartMS+v.DurationMS < start || (task != "" && v.TaskID != task) || (kind != "" && v.Kind != kind) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.ID+" "+v.AgentID+" "+v.TaskID+" "+v.Kind), q) {
			continue
		}
		if total >= offset && len(matches) < limit {
			matches = append(matches, v)
		}
		total++
	}
	hi := offset + len(matches)
	if hi > total {
		hi = total
	}
	p := s.p
	p.Spans = matches
	ids := map[string]bool{}
	for _, v := range p.Spans {
		ids[v.ID] = true
	}
	p.Edges = []profile.Edge{}
	hidden := 0
	for _, v := range s.p.Edges {
		if ids[v.Source] && ids[v.Target] {
			p.Edges = append(p.Edges, v)
		} else if ids[v.Source] || ids[v.Target] {
			hidden++
		}
	}
	// Parent IDs remain evidence but missing parents are explicitly identified by hidden_edges.
	resources := []profile.Resource{}
	runtimes := map[string]bool{}
	runtimeFilter := r.URL.Query().Get("runtime")
	resourceFirst := sort.Search(len(s.p.Resources), func(i int) bool { return s.p.Resources[i].TimeMS >= start })
	resourceLast := sort.Search(len(s.p.Resources), func(i int) bool { return s.p.Resources[i].TimeMS > end })
	for _, v := range s.p.Resources[resourceFirst:resourceLast] {
		if task != "" && v.TaskID != "" && v.TaskID != task {
			continue
		}
		runtimes[v.RuntimeID] = true
		if runtimeFilter == "" || v.RuntimeID == runtimeFilter {
			resources = append(resources, v)
		}
	}
	series := map[string]bool{}
	for _, v := range resources {
		series[v.TaskID+"\x00"+v.Component+"\x00"+v.RuntimeID] = true
	}
	budget := points
	if len(series) > 0 {
		budget = points / len(series)
	}
	resourceError := ""
	if len(resources) > points && budget < 8 {
		resourceError = "Resource series exceed the points budget; select a runtime or narrower interval."
	} else {
		p.Resources = downsample(resources, budget)
		if len(p.Resources) > points {
			resourceError = "Resource gaps exceed the points budget; select a narrower interval or increase points."
		}
	}
	if resourceError != "" {
		p.Resources = []profile.Resource{}
	}
	runtimeIDs := []string{}
	for runtime := range runtimes {
		runtimeIDs = append(runtimeIDs, runtime)
	}
	sort.Strings(runtimeIDs)
	ws.reply(w, r, map[string]any{"profile": p, "total_spans": total, "offset": offset, "limit": limit, "has_more": hi < total, "revision": s.revision, "analysis": s.analysis, "total_resources": len(resources), "run_id": id, "hidden_edges": hidden, "returned_resources": len(p.Resources), "runtime_ids": runtimeIDs, "resource_points_limit": points, "resources_sampled": resourceError == "" && len(p.Resources) < len(resources), "resource_error": resourceError})
}

// Each runtime/component/task series receives its own envelope. Budget is per
// series, so shared and task resources can never collapse into one curve.
func downsample(in []profile.Resource, points int) []profile.Resource {
	groups := map[string][]profile.Resource{}
	keys := []string{}
	for _, v := range in {
		k := v.TaskID + "\x00" + v.Component + "\x00" + v.RuntimeID
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], v)
	}
	out := []profile.Resource{}
	sort.Strings(keys)
	for _, k := range keys {
		v := groups[k]
		if len(v) <= points {
			out = append(out, v...)
			continue
		}
		if points < 8 {
			points = 8
		}
		buckets := (points - 2) / 8
		if buckets < 1 {
			buckets = 1
		}
		selected := map[int]bool{0: true, len(v) - 1: true}
		for b := 0; b < buckets; b++ {
			lo := b * len(v) / buckets
			hi := (b + 1) * len(v) / buckets
			mins, maxs := [3]int{-1, -1, -1}, [3]int{-1, -1, -1}
			for j := lo; j < hi; j++ {
				vals := []*float64{v[j].CPUPercent, v[j].MemoryBytes, v[j].GPUPercent}
				for m, x := range vals {
					if x == nil {
						if j == lo || j == hi-1 || metric(v[j-1], m) != nil {
							selected[j] = true
						}
						continue
					}
					if j > lo && metric(v[j-1], m) == nil {
						selected[j] = true
					}
					if mins[m] < 0 || *x < *metric(v[mins[m]], m) {
						mins[m] = j
					}
					if maxs[m] < 0 || *x > *metric(v[maxs[m]], m) {
						maxs[m] = j
					}
				}
			}
			selected[lo] = true
			selected[hi-1] = true
			for m := 0; m < 3; m++ {
				if mins[m] >= 0 {
					selected[mins[m]] = true
					selected[maxs[m]] = true
				}
			}
		}
		indices := []int{}
		for j := range selected {
			indices = append(indices, j)
		}
		sort.Ints(indices)
		for _, j := range indices {
			out = append(out, v[j])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TimeMS < out[j].TimeMS })
	return out
}
func metric(r profile.Resource, m int) *float64 {
	switch m {
	case 0:
		return r.CPUPercent
	case 1:
		return r.MemoryBytes
	default:
		return r.GPUPercent
	}
}
