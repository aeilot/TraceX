package profile

import (
	"fmt"
	"math"
	"sort"
)

type KindStats struct {
	Kind            string  `json:"kind"`
	IncompleteCount int     `json:"incomplete_count"`
	TimeQuality     string  `json:"time_quality"`
	Count           int     `json:"count"`
	UnionMS         float64 `json:"union_ms"`
	SummedMS        float64 `json:"summed_ms"`
}
type Coverage struct {
	TaskID         string  `json:"task_id"`
	Status         string  `json:"status"`
	ObservedMS     float64 `json:"observed_ms"`
	UnattributedMS float64 `json:"unattributed_ms"`
	Ratio          float64 `json:"ratio"`
	TimeQuality    string  `json:"time_quality,omitempty"`
}
type TokenTotals struct {
	InputTokens  *float64 `json:"input_tokens"`
	OutputTokens *float64 `json:"output_tokens"`
	ModelCalls   int      `json:"model_calls"`
	InputKnown   int      `json:"input_known"`
	OutputKnown  int      `json:"output_known"`
	// Usage detail sums over model calls that recorded them; absent when none did.
	CacheReadTokens  *float64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *float64 `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  *float64 `json:"reasoning_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
}
type CriticalPath struct {
	Status     string   `json:"status"`
	Reason     string   `json:"reason,omitempty"`
	SpanIDs    []string `json:"span_ids"`
	DurationMS float64  `json:"duration_ms"`
}
type Analysis struct {
	AgentCount          int          `json:"agent_count"`
	SpanCount           int          `json:"span_count"`
	TaskCount           int          `json:"task_count"`
	TaskSucceeded       int          `json:"task_succeeded"`
	TaskFailed          int          `json:"task_failed"`
	ResourcesCount      int          `json:"resources_count"`
	DurationMS          float64      `json:"duration_ms"`
	Kinds               []KindStats  `json:"kinds"`
	Coverage            []Coverage   `json:"coverage"`
	MaxConcurrency      int          `json:"max_concurrency"`
	Usage               TokenTotals  `json:"usage"`
	TaskP50MS           float64      `json:"task_p50_ms"`
	TaskP95MS           float64      `json:"task_p95_ms"`
	ThroughputPerSecond float64      `json:"throughput_per_second"`
	CriticalPath        CriticalPath `json:"critical_path"`
	// ScoreMean is the mean recorded accuracy over ScoreKnown tasks; tasks
	// without a score are excluded, never counted as zero.
	ScoreMean  *float64 `json:"score_mean"`
	ScoreKnown int      `json:"score_known"`
}
type interval struct{ start, end float64 }

func unionMS(xs []interval) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].start < xs[j].start })
	start, end, total := xs[0].start, xs[0].end, 0.0
	for _, x := range xs[1:] {
		if x.start > end {
			total += end - start
			start, end = x.start, x.end
		} else if x.end > end {
			end = x.end
		}
	}
	return total + end - start
}
func incomplete(s Span) bool {
	return s.Incomplete || s.Attributes["time_quality"] == "completion_only"
}

// leafWork excludes containers (agent, lifecycle) and suspended sandbox
// intervals, which record frozen time rather than work.
func leafWork(s Span) bool {
	return s.Kind != "agent" && s.Kind != "lifecycle" && s.Kind != "suspended" && !incomplete(s)
}
func usageValue(s Span, input bool) *float64 {
	if s.Usage != nil {
		if input && s.Usage.InputTokens != nil {
			return s.Usage.InputTokens
		}
		if !input && s.Usage.OutputTokens != nil {
			return s.Usage.OutputTokens
		}
	}
	key := "gen_ai.usage.output_tokens"
	if input {
		key = "gen_ai.usage.input_tokens"
	}
	if v, ok := s.Attributes[key].(float64); ok && validTime(v) {
		return &v
	}
	return nil
}
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	i := q * float64(len(xs)-1)
	lo, hi := int(math.Floor(i)), int(math.Ceil(i))
	return xs[lo] + (xs[hi]-xs[lo])*(i-float64(lo))
}
func Analyze(p Profile) Analysis {
	a := Analysis{Kinds: []KindStats{}, Coverage: []Coverage{}, CriticalPath: CriticalPath{Status: "unavailable", Reason: "No recorded finish-before-start dependency, join, or return relationships.", SpanIDs: []string{}}}
	kinds := map[string]*KindStats{}
	windows := map[string][]interval{}
	kindWindows := map[string][]interval{}
	type point struct {
		t     float64
		delta int
	}
	points := []point{}
	a.SpanCount = len(p.Spans)
	a.TaskCount = len(p.Tasks)
	a.ResourcesCount = len(p.Resources)
	a.DurationMS = p.DurationMS
	agents := map[string]bool{}
	parentCoverage := map[string][]interval{}
	byID := map[string]Span{}
	for _, s := range p.Spans {
		byID[s.ID] = s
		if s.AgentID != "" {
			agents[s.AgentID] = true
		}
	}
	a.AgentCount = len(agents)
	for _, s := range p.Spans {
		if !leafWork(s) {
			continue
		}
		for id, seen := s.ParentID, map[string]bool{}; id != "" && !seen[id]; {
			seen[id] = true
			parent, ok := byID[id]
			if !ok {
				break
			}
			if leafWork(parent) {
				parentCoverage[id] = append(parentCoverage[id], interval{s.StartMS, s.StartMS + s.DurationMS})
			}
			id = parent.ParentID
		}
	}

	for _, s := range p.Spans {
		k := kinds[s.Kind]
		if k == nil {
			k = &KindStats{Kind: s.Kind}
			kinds[s.Kind] = k
		}
		k.Count++
		if incomplete(s) {
			k.IncompleteCount++
		} else {
			k.SummedMS += s.DurationMS
			kindWindows[s.Kind] = append(kindWindows[s.Kind], interval{s.StartMS, s.StartMS + s.DurationMS})
		}
		if leafWork(s) {
			windows[s.TaskID] = append(windows[s.TaskID], interval{s.StartMS, s.StartMS + s.DurationMS})
			for _, part := range subtractIntervals(interval{s.StartMS, s.StartMS + s.DurationMS}, parentCoverage[s.ID]) {
				points = append(points, point{part.start, 1}, point{part.end, -1})
			}
		}
		if s.Kind == "model" {
			a.Usage.ModelCalls++
			if v := usageValue(s, true); v != nil {
				if a.Usage.InputTokens == nil {
					a.Usage.InputTokens = new(float64)
				}
				*a.Usage.InputTokens += *v
				a.Usage.InputKnown++
			}
			if v := usageValue(s, false); v != nil {
				if a.Usage.OutputTokens == nil {
					a.Usage.OutputTokens = new(float64)
				}
				*a.Usage.OutputTokens += *v
				a.Usage.OutputKnown++
			}
			if u := s.Usage; u != nil {
				for _, f := range []struct {
					dst **float64
					v   *float64
				}{
					{&a.Usage.CacheReadTokens, u.CacheReadTokens}, {&a.Usage.CacheWriteTokens, u.CacheWriteTokens},
					{&a.Usage.ReasoningTokens, u.ReasoningTokens}, {&a.Usage.CostUSD, u.CostUSD},
				} {
					if f.v != nil {
						if *f.dst == nil {
							*f.dst = new(float64)
						}
						**f.dst += *f.v
					}
				}
			}
		}

	}
	for kind, k := range kinds {
		k.UnionMS = unionMS(kindWindows[kind])
		k.TimeQuality = "measured"
		if k.IncompleteCount == k.Count {
			k.TimeQuality = "completion_only"
		} else if k.IncompleteCount > 0 {
			k.TimeQuality = "mixed"
		}
		a.Kinds = append(a.Kinds, *k)
	}
	sort.Slice(a.Kinds, func(i, j int) bool { return a.Kinds[i].Kind < a.Kinds[j].Kind })
	durations := []float64{}
	completed := 0
	for _, t := range p.Tasks {
		clipped := []interval{}
		for _, x := range windows[t.ID] {
			x.start = math.Max(x.start, t.StartMS)
			x.end = math.Min(x.end, t.StartMS+t.DurationMS)
			if x.end > x.start {
				clipped = append(clipped, x)
			}
		}
		observed := unionMS(clipped)
		c := Coverage{Status: "available", TaskID: t.ID, ObservedMS: observed, UnattributedMS: math.Max(0, t.DurationMS-observed), TimeQuality: t.TimeQuality}
		if t.DurationMS > 0 {
			c.Ratio = observed / t.DurationMS
		}
		if t.TimeQuality == "unknown" || t.TimeQuality == "completion_only" || t.TimeQuality == "open" {
			c.Status = "unavailable"
			c.ObservedMS = 0
			c.UnattributedMS = 0
			c.Ratio = 0
		}
		a.Coverage = append(a.Coverage, c)
		if t.Score != nil {
			if a.ScoreMean == nil {
				a.ScoreMean = new(float64)
			}
			*a.ScoreMean += *t.Score
			a.ScoreKnown++
		}
		success := t.Status == "succeeded" || t.Status == "success" || t.Status == "completed"
		failed := t.Status == "failed" || t.Status == "canceled"
		if success || failed {
			durations = append(durations, t.DurationMS)

			completed++
			if success {
				a.TaskSucceeded++
			} else {
				a.TaskFailed++
			}
		}
	}
	if a.ScoreKnown > 0 {
		*a.ScoreMean /= float64(a.ScoreKnown)
	}
	sort.Float64s(durations)
	a.TaskP50MS = quantile(durations, .5)
	a.TaskP95MS = quantile(durations, .95)
	if p.DurationMS > 0 {
		a.ThroughputPerSecond = float64(completed) * 1000 / p.DurationMS
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].t == points[j].t {
			return points[i].delta < points[j].delta
		}
		return points[i].t < points[j].t
	})
	active := 0
	for _, pt := range points {
		active += pt.delta
		if active > a.MaxConcurrency {
			a.MaxConcurrency = active
		}
	}
	a.CriticalPath = critical(p)
	return a
}
func critical(p Profile) CriticalPath {
	out := CriticalPath{Status: "unavailable", Reason: "No recorded finish-before-start dependency, join, or return relationships.", SpanIDs: []string{}}
	spans := map[string]Span{}
	for _, s := range p.Spans {
		spans[s.ID] = s
	}
	next := map[string][]string{}
	degree := map[string]int{}
	count := 0
	for _, e := range p.Edges {
		if e.Kind != "dependency" && e.Kind != "join" && e.Kind != "return" {
			continue
		}
		s, ok := spans[e.Source]
		t, ok2 := spans[e.Target]
		if !ok || !ok2 {
			continue
		}
		if incomplete(s) || incomplete(t) || s.StartMS+s.DurationMS > t.StartMS {
			out.Reason = "Recorded precedence contains overlapping or incomplete spans; a completion critical path is unavailable."
			return out
		}
		next[e.Source] = append(next[e.Source], e.Target)
		degree[e.Target]++
		if _, ok := degree[e.Source]; !ok {
			degree[e.Source] = 0
		}
		count++
	}
	if count == 0 {
		return out
	}
	queue := []string{}
	dist := map[string]float64{}
	prev := map[string]string{}
	for id, d := range degree {
		dist[id] = spans[id].DurationMS
		if d == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	for id := range next {
		sort.Strings(next[id])
	}
	visited := 0
	best := ""
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		if best == "" || dist[id] > dist[best] {
			best = id
		}
		for _, n := range next[id] {
			candidate := dist[id] + spans[n].DurationMS
			if candidate > dist[n] {
				dist[n] = candidate
				prev[n] = id
			}
			degree[n]--
			if degree[n] == 0 {
				queue = append(queue, n)
			}
		}
	}
	if visited != len(degree) {
		out.Reason = "Recorded precedence is cyclic."
		return out
	}
	out.Status = "available"
	out.Reason = "Longest sum of recorded span durations along finish-before-start precedence; excludes unrecorded waits and containment."
	out.DurationMS = dist[best]
	for id := best; id != ""; id = prev[id] {
		out.SpanIDs = append(out.SpanIDs, id)
	}
	for i, j := 0, len(out.SpanIDs)-1; i < j; i, j = i+1, j-1 {
		out.SpanIDs[i], out.SpanIDs[j] = out.SpanIDs[j], out.SpanIDs[i]
	}
	return out
}

type MetricStats struct {
	SampleCount int      `json:"sample_count"`
	Min         *float64 `json:"min"`
	Max         *float64 `json:"max"`
	Mean        *float64 `json:"mean"`
}
type ResourceWindow struct {
	RuntimeID   string      `json:"runtime_id"`
	Component   string      `json:"component"`
	Attribution string      `json:"attribution"`
	SampleCount int         `json:"sample_count"`
	CPU         MetricStats `json:"cpu_percent"`
	Memory      MetricStats `json:"memory_bytes"`
	GPU         MetricStats `json:"gpu_percent"`
}
type SpanWindowStats struct {
	SpanID    string           `json:"span_id"`
	StartMS   float64          `json:"start_ms"`
	EndMS     float64          `json:"end_ms"`
	Resources []ResourceWindow `json:"resources"`
}

func metric(xs []*float64) MetricStats {
	m := MetricStats{}
	sum := 0.0
	for _, v := range xs {
		if v == nil {
			continue
		}
		if m.Min == nil || *v < *m.Min {
			x := *v
			m.Min = &x
		}
		if m.Max == nil || *v > *m.Max {
			x := *v
			m.Max = &x
		}
		sum += *v
		m.SampleCount++
	}
	if m.SampleCount > 0 {
		x := sum / float64(m.SampleCount)
		m.Mean = &x
	}
	return m
}
func WindowStats(p Profile, spanID string) (SpanWindowStats, error) {
	var selected *Span
	for i := range p.Spans {
		if p.Spans[i].ID == spanID {
			selected = &p.Spans[i]
			break
		}
	}
	if selected == nil {
		return SpanWindowStats{}, fmt.Errorf("unknown span")
	}
	out := SpanWindowStats{SpanID: spanID, StartMS: selected.StartMS, EndMS: selected.StartMS + selected.DurationMS, Resources: []ResourceWindow{}}
	groups := map[string][]Resource{}
	for _, r := range p.Resources {
		if r.TimeMS < out.StartMS || r.TimeMS > out.EndMS {
			continue
		}
		if r.TaskID != "" && r.TaskID != selected.TaskID {
			continue
		}
		if r.TaskID != "" && selected.RuntimeID != "" && r.RuntimeID != selected.RuntimeID {
			continue
		}
		groups[r.RuntimeID+"\x00"+r.Component] = append(groups[r.RuntimeID+"\x00"+r.Component], r)
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rs := groups[k]
		w := ResourceWindow{RuntimeID: rs[0].RuntimeID, Component: rs[0].Component, SampleCount: len(rs), Attribution: "task_context"}
		if rs[0].TaskID == "" {
			w.Attribution = "shared_context"
		} else if selected.RuntimeID != "" {
			w.Attribution = "runtime_context"
		}
		cpu, mem, gpu := []*float64{}, []*float64{}, []*float64{}
		for _, r := range rs {
			cpu = append(cpu, r.CPUPercent)
			mem = append(mem, r.MemoryBytes)
			gpu = append(gpu, r.GPUPercent)
		}
		w.CPU = metric(cpu)
		w.Memory = metric(mem)
		w.GPU = metric(gpu)
		out.Resources = append(out.Resources, w)
	}
	return out, nil
}

// subtractIntervals retains work outside nested child evidence, ensuring an
// enclosing call and its child do not inflate simultaneous work counts.
func subtractIntervals(base interval, nested []interval) []interval {
	out := []interval{}
	cursor := base.start
	sort.Slice(nested, func(i, j int) bool { return nested[i].start < nested[j].start })
	for _, x := range nested {
		if x.end <= cursor || x.start >= base.end {
			continue
		}
		if x.start > cursor {
			out = append(out, interval{cursor, math.Min(x.start, base.end)})
		}
		cursor = math.Max(cursor, x.end)
		if cursor >= base.end {
			break
		}
	}
	if cursor < base.end {
		out = append(out, interval{cursor, base.end})
	}
	return out
}
