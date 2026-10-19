package profile

import (
	"math"
	"regexp"
	"sort"
)

// ScheduleQuery selects the window and resolution of a scheduling projection.
type ScheduleQuery struct {
	StartMS, EndMS float64
	// Bins is the number of time buckets for the concurrency and resource
	// series; WidthPX bounds interval detail (narrower gaps are merged).
	Bins    int
	WidthPX float64
	// Pinned task occurrences are listed first; nothing else is hidden.
	Pinned []string
	// MaxLanes bounds the lanes returned; the remainder is counted.
	MaxLanes int
}

// ScheduleInterval is a recorded interval of one layer of a lane, clipped to
// the window. Merged intervals keep the count and one span as evidence.
type ScheduleInterval struct {
	StartMS    float64 `json:"start_ms"`
	DurationMS float64 `json:"duration_ms"`
	Kind       string  `json:"kind"`
	Count      int     `json:"count"`
	SpanID     string  `json:"span_id,omitempty"`
}

// ScheduleLane is one task occurrence. Agent instances are not recorded by
// current traces, so the occurrence is the visible unit.
type ScheduleLane struct {
	TaskID     string             `json:"task_id"`
	Status     string             `json:"status"`
	StartMS    float64            `json:"start_ms"`
	DurationMS float64            `json:"duration_ms"`
	Pinned     bool               `json:"pinned,omitempty"`
	Lifecycle  []ScheduleInterval `json:"lifecycle"`
	Work       []ScheduleInterval `json:"work"`
}

// ScheduleSeries is a per-bin series; nil entries are unmeasured.
type ScheduleSeries struct {
	Name     string     `json:"name"`
	Unit     string     `json:"unit"`
	Group    string     `json:"group"`
	Mean     []*float64 `json:"mean"`
	Max      []*float64 `json:"max"`
	Coverage []int      `json:"coverage,omitempty"`
}

// ImpactRow summarizes one task occurrence within the window. Pointer fields
// are nil when the evidence was not recorded; durations "in window" are
// clipped, DurationMS is the whole task.
type ImpactRow struct {
	TaskID           string            `json:"task_id"`
	Status           string            `json:"status"`
	Score            *float64          `json:"score,omitempty"`
	OverlapMS        float64           `json:"overlap_ms"`
	QueueMS          *float64          `json:"queue_ms"`
	ModelMS          float64           `json:"model_ms"`
	ToolMS           float64           `json:"tool_ms"`
	PausedMS         float64           `json:"paused_ms"`
	UnknownMS        *float64          `json:"unknown_ms"`
	CPUCoreS         *float64          `json:"cpu_core_s"`
	ResourceCoverage *float64          `json:"resource_coverage"`
	ThrottledShare   *float64          `json:"throttled_share"`
	Failures         int               `json:"failures"`
	DurationMS       *float64          `json:"duration_ms"`
	BaselineMS       *float64          `json:"baseline_ms"`
	BaselineN        int               `json:"baseline_n"`
	Slowdown         *float64          `json:"slowdown"`
	Evidence         map[string]string `json:"evidence"`
}

type ScheduleView struct {
	StartMS      float64          `json:"start_ms"`
	EndMS        float64          `json:"end_ms"`
	BinMS        float64          `json:"bin_ms"`
	Lanes        []ScheduleLane   `json:"lanes"`
	LanesTotal   int              `json:"lanes_total"`
	Series       []ScheduleSeries `json:"series"`
	Impact       []ImpactRow      `json:"impact"`
	QueueOK      bool             `json:"queue_recorded"`
	HostRecorded bool             `json:"host_recorded"`
	Notes        []string         `json:"notes"`
}

var occurrenceSuffix = regexp.MustCompile(`-\d{3,}$`)

// sameTask groups occurrences of one logical task: the logical ID without
// ARIES's numeric occurrence suffix.
func sameTask(t Task) string {
	id := t.LogicalID
	if id == "" {
		id = t.ID
	}
	return occurrenceSuffix.ReplaceAllString(id, "")
}

func failed(status string) bool {
	return status == "failed" || status == "ERROR" || status == "error" || status == "canceled"
}

// clip intersects [s, e) with the window; ok is false when they do not overlap.
func clip(s, e, lo, hi float64) (float64, float64, bool) {
	s, e = math.Max(s, lo), math.Min(e, hi)
	return s, e, e > s
}

// unionIntervals merges overlapping or touching intervals, keeping counts.
func unionIntervals(xs []ScheduleInterval, gap float64) []ScheduleInterval {
	sort.Slice(xs, func(i, j int) bool { return xs[i].StartMS < xs[j].StartMS })
	out := []ScheduleInterval{}
	for _, x := range xs {
		if n := len(out); n > 0 && out[n-1].Kind == x.Kind && x.StartMS <= out[n-1].StartMS+out[n-1].DurationMS+gap {
			last := &out[n-1]
			last.DurationMS = math.Max(last.StartMS+last.DurationMS, x.StartMS+x.DurationMS) - last.StartMS
			last.Count += x.Count
			continue
		}
		out = append(out, x)
	}
	return out
}

func totalMS(xs []ScheduleInterval) float64 {
	t := 0.0
	for _, x := range xs {
		t += x.DurationMS
	}
	return t
}

// workKind maps a span to a concurrent-work layer, or "" when it is not one.
// Network requests and harness envelopes are not work intervals here.
func workKind(s Span) string {
	switch s.Kind {
	case "model", "tool", "suspended":
		return s.Kind
	}
	return ""
}

// Schedule projects a profile onto a scheduling window: per-occurrence lanes,
// concurrency and resource series, and an interval impact table. It reads the
// whole profile, never a DAG page, so totals do not depend on pagination.
func Schedule(p Profile, q ScheduleQuery) ScheduleView {
	lo, hi := q.StartMS, q.EndMS
	if hi <= lo {
		hi = math.Max(lo+1, p.DurationMS)
	}
	bins := q.Bins
	if bins <= 0 || bins > 2000 {
		bins = 400
	}
	width := q.WidthPX
	if width <= 0 {
		width = 1000
	}
	gap := (hi - lo) / width // one pixel
	binMS := (hi - lo) / float64(bins)
	view := ScheduleView{StartMS: lo, EndMS: hi, BinMS: binMS, Lanes: []ScheduleLane{}, Series: []ScheduleSeries{}, Impact: []ImpactRow{},
		Notes: []string{"Lanes are task occurrences: traces record no agent instance identity."}}

	byTask := map[string][]Span{}
	for _, s := range p.Spans {
		if s.TaskID != "" {
			byTask[s.TaskID] = append(byTask[s.TaskID], s)
		}
	}
	pinned := map[string]bool{}
	for _, id := range q.Pinned {
		pinned[id] = true
	}

	// Baselines: other completed occurrences of the same logical task.
	completed := map[string][]Task{}
	for _, t := range p.Tasks {
		if !failed(t.Status) && t.Status != "running" && t.TimeQuality == "measured" && t.DurationMS > 0 {
			completed[sameTask(t)] = append(completed[sameTask(t)], t)
		}
	}

	type perTask struct {
		lane                               ScheduleLane
		queue, model, tool, paused, active []ScheduleInterval
	}
	tasks := []perTask{}
	for _, t := range p.Tasks {
		spans := byTask[t.ID]
		start, end := t.StartMS, t.StartMS+t.DurationMS
		envelope := false
		for _, s := range spans {
			if s.Kind == "lifecycle" && s.Attributes["phase"] == "task" {
				start, end, envelope = s.StartMS, s.StartMS+s.DurationMS, true
			}
		}
		for _, s := range spans {
			if s.Kind == "lifecycle" && s.Attributes["phase"] == "queue" {
				start = math.Min(start, s.StartMS)
			}
		}
		if _, _, ok := clip(start, end, lo, hi); !ok && t.TimeQuality != "open" {
			continue
		}
		pt := perTask{lane: ScheduleLane{TaskID: t.ID, Status: t.Status, StartMS: t.StartMS, DurationMS: t.DurationMS, Pinned: pinned[t.ID],
			Lifecycle: []ScheduleInterval{}, Work: []ScheduleInterval{}}}
		phases := []ScheduleInterval{}
		for _, s := range spans {
			if s.Kind == "lifecycle" {
				phase, _ := s.Attributes["phase"].(string)
				if phase == "task" {
					continue
				}
				if s.Name == "Queue" {
					phase = "queue"
				}
				if phase == "" {
					phase = s.Name
				}
				if a, b, ok := clip(s.StartMS, s.StartMS+s.DurationMS, lo, hi); ok {
					x := ScheduleInterval{StartMS: a, DurationMS: b - a, Kind: phase, Count: 1, SpanID: s.ID}
					phases = append(phases, x)
					if phase == "queue" {
						pt.queue = append(pt.queue, x)
						view.QueueOK = true
					}
				}
				continue
			}
			kind := workKind(s)
			if kind == "" {
				continue
			}
			if a, b, ok := clip(s.StartMS, s.StartMS+s.DurationMS, lo, hi); ok {
				x := ScheduleInterval{StartMS: a, DurationMS: b - a, Kind: kind, Count: 1, SpanID: s.ID}
				switch kind {
				case "model":
					pt.model = append(pt.model, x)
				case "tool":
					pt.tool = append(pt.tool, x)
				case "suspended":
					pt.paused = append(pt.paused, x)
				}
			}
		}
		// Unknown: task time inside the window that no recorded phase covers.
		var unknown []ScheduleInterval
		if a, b, ok := clip(start, end, lo, hi); ok && (envelope || t.TimeQuality == "measured" || t.TimeQuality == "open") {
			covered := unionIntervals(append([]ScheduleInterval(nil), phases...), 0)
			cursor := a
			for _, c := range covered {
				if c.StartMS > cursor {
					unknown = append(unknown, ScheduleInterval{StartMS: cursor, DurationMS: math.Min(c.StartMS, b) - cursor, Kind: "unknown", Count: 1})
				}
				cursor = math.Max(cursor, c.StartMS+c.DurationMS)
			}
			if b > cursor {
				unknown = append(unknown, ScheduleInterval{StartMS: cursor, DurationMS: b - cursor, Kind: "unknown", Count: 1})
			}
			pt.active = []ScheduleInterval{{StartMS: a, DurationMS: b - a, Kind: "active", Count: 1}}
			// A queued occurrence is admitted only after its queue span ends.
			for _, x := range unionIntervals(append([]ScheduleInterval(nil), pt.queue...), 0) {
				pt.active = subtractOne(pt.active, x)
			}
		}
		pt.model = unionIntervals(pt.model, 0)
		pt.tool = unionIntervals(pt.tool, 0)
		pt.paused = unionIntervals(pt.paused, 0)
		lifecycle := append(phases, unknown...)
		pt.lane.Lifecycle = unionIntervals(lifecycle, gap)
		work := append(append(append([]ScheduleInterval{}, pt.model...), pt.tool...), pt.paused...)
		pt.lane.Work = []ScheduleInterval{}
		for _, kind := range []string{"model", "tool", "suspended"} {
			var xs []ScheduleInterval
			for _, x := range work {
				if x.Kind == kind {
					xs = append(xs, x)
				}
			}
			pt.lane.Work = append(pt.lane.Work, unionIntervals(xs, gap)...)
		}
		tasks = append(tasks, pt)

		// Impact row.
		row := ImpactRow{TaskID: t.ID, Status: t.Status, Score: t.Score, ModelMS: totalMS(pt.model), ToolMS: totalMS(pt.tool),
			PausedMS: totalMS(pt.paused), Evidence: map[string]string{}}
		if a, b, ok := clip(start, end, lo, hi); ok {
			row.OverlapMS = b - a
		}
		if len(pt.queue) > 0 {
			v := totalMS(unionIntervals(append([]ScheduleInterval(nil), pt.queue...), 0))
			row.QueueMS = &v
			row.Evidence["queue"] = pt.queue[0].SpanID
		}
		if len(unknown) > 0 || len(phases) > 0 {
			v := totalMS(unknown)
			row.UnknownMS = &v
		}
		for key, xs := range map[string][]ScheduleInterval{"model": pt.model, "tool": pt.tool, "paused": pt.paused} {
			if len(xs) > 0 {
				row.Evidence[key] = xs[0].SpanID
			}
		}
		for _, s := range spans {
			if failed(s.Status) {
				if _, _, ok := clip(s.StartMS, s.StartMS+math.Max(s.DurationMS, 0.001), lo, hi); ok {
					row.Failures++
					if row.Evidence["failure"] == "" {
						row.Evidence["failure"] = s.ID
					}
				}
			}
		}
		if t.TimeQuality == "measured" && t.DurationMS > 0 {
			d := t.DurationMS
			row.DurationMS = &d
			var others []float64
			for _, o := range completed[sameTask(t)] {
				if o.ID != t.ID {
					others = append(others, o.DurationMS)
				}
			}
			row.BaselineN = len(others)
			if len(others) >= 3 && !failed(t.Status) {
				sort.Float64s(others)
				median := quantile(others, 0.5)
				ratio := d / median
				row.BaselineMS, row.Slowdown = &median, &ratio
			}
		}
		view.Impact = append(view.Impact, row)
	}

	// Lanes: pinned first, then by start; the rest are counted, not hidden.
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].lane.Pinned != tasks[j].lane.Pinned {
			return tasks[i].lane.Pinned
		}
		return tasks[i].lane.StartMS < tasks[j].lane.StartMS
	})
	view.LanesTotal = len(tasks)
	limit := q.MaxLanes
	if limit <= 0 {
		limit = 200
	}
	for i, pt := range tasks {
		if i < limit {
			view.Lanes = append(view.Lanes, pt.lane)
		}
	}

	// Concurrency: how many occurrences were in each state, per bin.
	count := func(name string, pick func(perTask) []ScheduleInterval) ScheduleSeries {
		var xs []ScheduleInterval
		for _, pt := range tasks {
			xs = append(xs, pick(pt)...)
		}
		return concurrency(name, xs, lo, binMS, bins)
	}
	view.Series = append(view.Series,
		count("Admitted occurrences", func(pt perTask) []ScheduleInterval { return pt.active }),
		count("Occurrences in a model call", func(pt perTask) []ScheduleInterval { return pt.model }),
		count("Occurrences in a tool call", func(pt perTask) []ScheduleInterval { return pt.tool }),
		count("Paused sandboxes", func(pt perTask) []ScheduleInterval { return pt.paused }))
	if view.QueueOK {
		view.Series = append(view.Series, count("Queued occurrences", func(pt perTask) []ScheduleInterval { return pt.queue }))
	} else {
		view.Notes = append(view.Notes, "No admission queue spans are recorded; queue depth is unknown, not zero.")
	}

	resources(&view, p, lo, hi, binMS, bins)
	return view
}

// subtractOne removes cut from each interval of xs.
func subtractOne(xs []ScheduleInterval, cut ScheduleInterval) []ScheduleInterval {
	out := []ScheduleInterval{}
	cs, ce := cut.StartMS, cut.StartMS+cut.DurationMS
	for _, x := range xs {
		s, e := x.StartMS, x.StartMS+x.DurationMS
		if ce <= s || cs >= e {
			out = append(out, x)
			continue
		}
		if cs > s {
			out = append(out, ScheduleInterval{StartMS: s, DurationMS: cs - s, Kind: x.Kind, Count: x.Count})
		}
		if ce < e {
			out = append(out, ScheduleInterval{StartMS: ce, DurationMS: e - ce, Kind: x.Kind, Count: x.Count})
		}
	}
	return out
}

// concurrency returns the time-weighted mean and the peak number of
// overlapping intervals per bin.
func concurrency(name string, xs []ScheduleInterval, lo, binMS float64, bins int) ScheduleSeries {
	type event struct {
		t     float64
		delta int
	}
	events := make([]event, 0, 2*len(xs))
	sum := make([]float64, bins)
	for _, x := range xs {
		events = append(events, event{x.StartMS, 1}, event{x.StartMS + x.DurationMS, -1})
		for b := int((x.StartMS - lo) / binMS); b < bins && b >= 0; b++ {
			bs, be := lo+float64(b)*binMS, lo+float64(b+1)*binMS
			if s, e, ok := clip(x.StartMS, x.StartMS+x.DurationMS, bs, be); ok {
				sum[b] += (e - s) / binMS
			} else if x.StartMS+x.DurationMS <= bs {
				break
			}
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].t == events[j].t {
			return events[i].delta < events[j].delta
		}
		return events[i].t < events[j].t
	})
	peak := make([]float64, bins)
	level := 0
	for i, e := range events {
		level += e.delta
		next := e.t
		if i+1 < len(events) {
			next = events[i+1].t
		}
		if next <= e.t {
			continue
		}
		for b := int(math.Max(0, (e.t-lo)/binMS)); b < bins && lo+float64(b)*binMS < next; b++ {
			peak[b] = math.Max(peak[b], float64(level))
		}
	}
	s := ScheduleSeries{Name: name, Unit: "count", Group: "concurrency", Mean: make([]*float64, bins), Max: make([]*float64, bins)}
	for b := 0; b < bins; b++ {
		m, p := sum[b], peak[b]
		s.Mean[b], s.Max[b] = &m, &p
	}
	return s
}

// resources adds CPU, memory and throttling series grouped into task
// runtimes and shared infrastructure, and fills the impact rows' resource
// columns. Counter resets never produce negative throttling.
func resources(view *ScheduleView, p Profile, lo, hi, binMS float64, bins int) {
	type acc struct {
		sum, peak []float64
		seen      []int
	}
	newAcc := func() *acc { return &acc{make([]float64, bins), make([]float64, bins), make([]int, bins)} }
	runtimes := map[string][]Resource{}
	for _, r := range p.Resources {
		runtimes[r.TaskID+"\x00"+r.Component+"\x00"+r.RuntimeID] = append(runtimes[r.TaskID+"\x00"+r.Component+"\x00"+r.RuntimeID], r)
	}
	groups := map[string]map[string]*acc{"task": {"cpu": newAcc(), "memory": newAcc()}, "shared": {"cpu": newAcc(), "memory": newAcc()}}
	throttled, periods := make([]float64, bins), make([]float64, bins)
	impact := map[string]*ImpactRow{}
	for i := range view.Impact {
		impact[view.Impact[i].TaskID] = &view.Impact[i]
	}
	type taskRes struct {
		coreS, covered, thr, per float64
		sampled                  bool
	}
	perTaskRes := map[string]*taskRes{}
	for _, rs := range runtimes {
		sort.Slice(rs, func(i, j int) bool { return rs[i].TimeMS < rs[j].TimeMS })
		group := "task"
		if rs[0].TaskID == "" {
			group = "shared"
		}
		// Per-runtime mean of each bin, summed across runtimes below.
		cpuSum, cpuN := make([]float64, bins), make([]int, bins)
		memSum, memN := make([]float64, bins), make([]int, bins)
		for k, r := range rs {
			if r.TimeMS < lo || r.TimeMS > hi {
				continue
			}
			b := int((r.TimeMS - lo) / binMS)
			if b >= bins {
				b = bins - 1
			}
			if r.CPUPercent != nil {
				cpuSum[b] += *r.CPUPercent / 100
				cpuN[b]++
			}
			if r.MemoryBytes != nil {
				memSum[b] += *r.MemoryBytes
				memN[b]++
			}
			if k > 0 && r.CPUThrottling != nil && rs[k-1].CPUThrottling != nil {
				dp := r.CPUThrottling.Periods - rs[k-1].CPUThrottling.Periods
				dt := r.CPUThrottling.ThrottledPeriods - rs[k-1].CPUThrottling.ThrottledPeriods
				if dp > 0 && dt >= 0 { // a reset makes the delta negative: skip it
					periods[b] += dp
					throttled[b] += dt
				}
			}
			if r.TaskID != "" && impact[r.TaskID] != nil && k > 0 {
				tr := perTaskRes[r.TaskID]
				if tr == nil {
					tr = &taskRes{}
					perTaskRes[r.TaskID] = tr
				}
				step := r.TimeMS - rs[k-1].TimeMS
				if step > 0 && step < 10000 {
					tr.covered += step
					tr.sampled = true
					if r.CPUPercent != nil {
						tr.coreS += *r.CPUPercent / 100 * step / 1000
					}
				}
				if r.CPUThrottling != nil && rs[k-1].CPUThrottling != nil {
					dp := r.CPUThrottling.Periods - rs[k-1].CPUThrottling.Periods
					dt := r.CPUThrottling.ThrottledPeriods - rs[k-1].CPUThrottling.ThrottledPeriods
					if dp > 0 && dt >= 0 {
						tr.per += dp
						tr.thr += dt
					}
				}
			}
		}
		g := groups[group]
		for b := 0; b < bins; b++ {
			if cpuN[b] > 0 {
				v := cpuSum[b] / float64(cpuN[b])
				g["cpu"].sum[b] += v
				g["cpu"].peak[b] += v
				g["cpu"].seen[b]++
			}
			if memN[b] > 0 {
				v := memSum[b] / float64(memN[b])
				g["memory"].sum[b] += v
				g["memory"].peak[b] += v
				g["memory"].seen[b]++
			}
		}
	}
	add := func(name, unit, group string, a *acc) {
		s := ScheduleSeries{Name: name, Unit: unit, Group: group, Mean: make([]*float64, bins), Max: make([]*float64, bins), Coverage: a.seen}
		measured := false
		for b := 0; b < bins; b++ {
			if a.seen[b] > 0 {
				m, p := a.sum[b], a.peak[b]
				s.Mean[b], s.Max[b] = &m, &p
				measured = true
			}
		}
		if measured {
			view.Series = append(view.Series, s)
		}
	}
	add("Task runtime CPU", "cores", "resources", groups["task"]["cpu"])
	add("Shared runtime CPU", "cores", "resources", groups["shared"]["cpu"])
	add("Task runtime memory", "bytes", "resources", groups["task"]["memory"])
	add("Shared runtime memory", "bytes", "resources", groups["shared"]["memory"])
	ts := ScheduleSeries{Name: "Throttled period share", Unit: "share", Group: "resources", Mean: make([]*float64, bins), Max: make([]*float64, bins)}
	anyThrottle := false
	for b := 0; b < bins; b++ {
		if periods[b] > 0 {
			v := throttled[b] / periods[b]
			ts.Mean[b], ts.Max[b] = &v, &v
			anyThrottle = true
		}
	}
	if anyThrottle {
		view.Series = append(view.Series, ts)
	}
	for id, tr := range perTaskRes {
		row := impact[id]
		if !tr.sampled || row.OverlapMS <= 0 {
			continue
		}
		core, cov := tr.coreS, math.Min(1, tr.covered/row.OverlapMS)
		row.CPUCoreS, row.ResourceCoverage = &core, &cov
		if tr.per > 0 {
			share := tr.thr / tr.per
			row.ThrottledShare = &share
		}
	}
	view.Notes = append(view.Notes, "No host or resource-pool identity is recorded; runtimes are grouped as task-owned or shared, not by host.")
}
