package profile

import (
	"sort"
	"strings"
)

// SweepTask holds per-task samples for comparing runs across one recorded
// configuration field. Aggregation (windows, quantiles, indexing) happens in
// the browser so imported and served runs share one implementation.
type SweepTask struct {
	TaskID  string  `json:"task_id"`
	StartMS float64 `json:"start_ms"`
	// DurationMS is set only for a finished outcome with a measured start.
	DurationMS *float64 `json:"duration_ms"`
	// ModelMS are completed model calls with no nested model span, so turn
	// envelopes that contain an API call are not counted twice.
	ModelMS []float64 `json:"model_ms"`
	// BrowserLocalMS is each browser tool call's duration minus its overlap
	// with the union of the task's browser network request intervals.
	BrowserLocalMS []float64 `json:"browser_local_ms"`
	// Sandbox throttling counter increases summed over consecutive samples of
	// each sandbox runtime; nil when no sandbox throttling was measured.
	Periods          *float64 `json:"periods,omitempty"`
	ThrottledPeriods *float64 `json:"throttled_periods,omitempty"`
}

func finished(t Task) bool {
	switch t.Status {
	case "succeeded", "success", "completed", "failed", "canceled":
		return t.TimeQuality != "unknown" && t.TimeQuality != "completion_only"
	}
	return false
}

// browserToolCall identifies an MCP browser tool request. Harness wrappers
// around the same call (e.g. tool.* spans) are excluded to avoid counting a
// call twice; browser network and page records have other kinds or names.
func browserToolCall(s Span) bool {
	return s.Kind == "browser" && strings.Contains(s.Name, "tools/call") && !incomplete(s)
}

// ThrottleDeltas sums counter increases between consecutive samples of each
// runtime series. A decrease is a counter reset and contributes nothing.
func ThrottleDeltas(rs []Resource) (periods, throttled float64, measured bool) {
	series := map[string][]Resource{}
	for _, r := range rs {
		if r.CPUThrottling == nil {
			continue
		}
		measured = true
		k := r.TaskID + "\x00" + r.Component + "\x00" + r.RuntimeID
		series[k] = append(series[k], r)
	}
	for _, v := range series {
		sort.SliceStable(v, func(i, j int) bool { return v[i].TimeMS < v[j].TimeMS })
		for i := 1; i < len(v); i++ {
			a, b := v[i-1].CPUThrottling, v[i].CPUThrottling
			dp, dt := b.Periods-a.Periods, b.ThrottledPeriods-a.ThrottledPeriods
			if dp < 0 || dt < 0 || dt > dp {
				continue
			}
			periods += dp
			throttled += dt
		}
	}
	return periods, throttled, measured
}

func SweepSamples(p Profile) []SweepTask {
	out := make([]SweepTask, 0, len(p.Tasks))
	index := map[string]int{}
	for _, t := range p.Tasks {
		s := SweepTask{TaskID: t.ID, StartMS: t.StartMS, ModelMS: []float64{}, BrowserLocalMS: []float64{}}
		if finished(t) {
			d := t.DurationMS
			s.DurationMS = &d
		}
		index[t.ID] = len(out)
		out = append(out, s)
	}
	nestedModel := map[string]bool{}
	for _, s := range p.Spans {
		if s.Kind == "model" && s.ParentID != "" {
			nestedModel[s.ParentID] = true
		}
	}
	network := map[string][]interval{}
	for _, s := range p.Spans {
		if s.Kind == "network" && !incomplete(s) {
			network[s.TaskID] = append(network[s.TaskID], interval{s.StartMS, s.StartMS + s.DurationMS})
		}
	}
	merged := map[string][]interval{}
	for task, xs := range network {
		merged[task] = mergeIntervals(xs)
	}
	for _, s := range p.Spans {
		i, ok := index[s.TaskID]
		if !ok || incomplete(s) {
			continue
		}
		switch {
		case s.Kind == "model" && !nestedModel[s.ID] && !unknownTiming(s):
			out[i].ModelMS = append(out[i].ModelMS, s.DurationMS)
		case browserToolCall(s):
			remote := overlapMS(interval{s.StartMS, s.StartMS + s.DurationMS}, merged[s.TaskID])
			out[i].BrowserLocalMS = append(out[i].BrowserLocalMS, max(0, s.DurationMS-remote))
		}
	}
	byTask := map[string][]Resource{}
	for _, r := range p.Resources {
		if r.Component == "sandbox" && r.TaskID != "" {
			byTask[r.TaskID] = append(byTask[r.TaskID], r)
		}
	}
	for task, rs := range byTask {
		i, ok := index[task]
		if !ok {
			continue
		}
		if periods, throttled, measured := ThrottleDeltas(rs); measured {
			out[i].Periods, out[i].ThrottledPeriods = &periods, &throttled
		}
	}
	return out
}

// unknownTiming excludes producer markers whose duration is not measured.
func unknownTiming(s Span) bool { return s.Attributes["time_quality"] == "unknown" }

func mergeIntervals(xs []interval) []interval {
	sort.Slice(xs, func(i, j int) bool { return xs[i].start < xs[j].start })
	out := []interval{}
	for _, x := range xs {
		if x.end <= x.start {
			continue
		}
		if n := len(out); n > 0 && x.start <= out[n-1].end {
			out[n-1].end = max(out[n-1].end, x.end)
		} else {
			out = append(out, x)
		}
	}
	return out
}

func overlapMS(x interval, merged []interval) float64 {
	i := sort.Search(len(merged), func(i int) bool { return merged[i].end > x.start })
	total := 0.0
	for ; i < len(merged) && merged[i].start < x.end; i++ {
		if d := min(x.end, merged[i].end) - max(x.start, merged[i].start); d > 0 {
			total += d
		}
	}
	return total
}
