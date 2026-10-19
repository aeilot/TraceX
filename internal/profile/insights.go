package profile

import (
	"fmt"
	"sort"
	"strings"
)

type Finding struct {
	Rule     string   `json:"rule"`
	Severity string   `json:"severity"`
	Value    float64  `json:"value"`
	Count    int      `json:"count"`
	SpanIDs  []string `json:"span_ids"`
	TaskIDs  []string `json:"task_ids"`
	// Details holds rule-specific measurements; an absent key was not measured.
	Details map[string]float64 `json:"details,omitempty"`
}
type CallGroup struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Count        int      `json:"count"`
	Timed        int      `json:"timed"`
	Failed       int      `json:"failed"`
	SumMS        float64  `json:"sum_ms"`
	UnionMS      float64  `json:"union_ms"`
	P50MS        *float64 `json:"p50_ms"`
	P95MS        *float64 `json:"p95_ms"`
	InputTokens  float64  `json:"input_tokens"`
	OutputTokens float64  `json:"output_tokens"`
	InputKnown   int      `json:"input_known"`
	OutputKnown  int      `json:"output_known"`
	SpanIDs      []string `json:"span_ids"`
}
type TaskEvidence struct {
	ID         string   `json:"id"`
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	DurationMS *float64 `json:"duration_ms"`
	Score      *float64 `json:"score"`
}
type Insights struct {
	RunID      string         `json:"run_id"`
	Findings   []Finding      `json:"findings"`
	Groups     []CallGroup    `json:"groups"`
	GroupCount int            `json:"group_count"`
	Tasks      []TaskEvidence `json:"tasks"`
	SpanCount  int            `json:"span_count"`
}

func measuredSpan(s Span) bool {
	return !s.Incomplete && s.Attributes["time_quality"] != "unknown" && s.Attributes["time_quality"] != "completion_only"
}
func finishedTask(t Task) bool {
	return (t.Status == "succeeded" || t.Status == "success" || t.Status == "completed" || t.Status == "failed" || t.Status == "canceled") && t.TimeQuality != "unknown" && t.TimeQuality != "completion_only"
}
func badStatus(status string) bool {
	s := strings.ToLower(status)
	return s == "failed" || s == "error" || s == "canceled"
}
func boundedIDs(ids []string, id string) []string {
	if len(ids) < 5 {
		return append(ids, id)
	}
	return ids
}

// Diagnose uses complete normalized evidence. Summed work, interval unions and
// task wall time are distinct; missing measurements never become measured zero.
func Diagnose(p Profile, a Analysis) Insights {
	out := Insights{RunID: p.RunID, Findings: []Finding{}, Groups: []CallGroup{}, Tasks: []TaskEvidence{}, SpanCount: len(p.Spans)}
	byID := map[string]Span{}
	nested := map[string]bool{}
	for _, s := range p.Spans {
		byID[s.ID] = s
	}
	// Exclude enclosing same-kind call envelopes so nested API records count once.
	for _, s := range p.Spans {
		if s.Kind != "model" && s.Kind != "tool" && s.Kind != "browser" {
			continue
		}
		for id, seen := s.ParentID, map[string]bool{}; id != "" && !seen[id]; {
			seen[id] = true
			parent, ok := byID[id]
			if !ok {
				break
			}
			if parent.Kind == s.Kind {
				nested[id] = true
			}
			id = parent.ParentID
		}
	}
	groups := map[string]*CallGroup{}
	durations := map[string][]float64{}
	intervals := map[string][]interval{}
	failed := Finding{Rule: "failed_spans", Severity: "warning", SpanIDs: []string{}, TaskIDs: []string{}}
	usageGap := Finding{Rule: "usage_gap", Severity: "info", SpanIDs: []string{}, TaskIDs: []string{}}
	for _, s := range p.Spans {
		if badStatus(s.Status) {
			failed.Count++
			failed.SpanIDs = boundedIDs(failed.SpanIDs, s.ID)
		}
		if (s.Kind != "model" && s.Kind != "tool" && s.Kind != "browser") || nested[s.ID] {
			continue
		}
		name := s.Name
		keys := []string{"tool.name"}
		if s.Kind == "model" {
			name = "Model identity not recorded"
			keys = []string{"gen_ai.request.model", "model"}
		}
		for _, key := range keys {
			if v, ok := s.Attributes[key].(string); ok && v != "" {
				name = v
				break
			}
		}
		key := s.Kind + "\x00" + name
		g := groups[key]
		if g == nil {
			g = &CallGroup{Kind: s.Kind, Name: name, SpanIDs: []string{}}
			groups[key] = g
		}
		g.Count++
		g.SpanIDs = boundedIDs(g.SpanIDs, s.ID)
		if badStatus(s.Status) {
			g.Failed++
		}
		if measuredSpan(s) {
			g.Timed++
			g.SumMS += s.DurationMS
			durations[key] = append(durations[key], s.DurationMS)
			intervals[key] = append(intervals[key], interval{s.StartMS, s.StartMS + s.DurationMS})
		}
		if s.Kind == "model" {
			if usageValue(s, true) == nil || usageValue(s, false) == nil {
				usageGap.Count++
				usageGap.SpanIDs = boundedIDs(usageGap.SpanIDs, s.ID)
			}
			if v := usageValue(s, true); v != nil {
				g.InputKnown++
				g.InputTokens += *v
			}
			if v := usageValue(s, false); v != nil {
				g.OutputKnown++
				g.OutputTokens += *v
			}
		}
	}
	if failed.Count > 0 {
		out.Findings = append(out.Findings, failed)
	}
	for key, g := range groups {
		xs := durations[key]
		sort.Float64s(xs)
		if len(xs) > 0 {
			p50, p95 := quantile(xs, .5), quantile(xs, .95)
			g.P50MS = &p50
			g.P95MS = &p95
		}
		g.UnionMS = unionMS(intervals[key])
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		l, r := out.Groups[i], out.Groups[j]
		if l.SumMS != r.SumMS {
			return l.SumMS > r.SumMS
		}
		return l.Kind+"\x00"+l.Name < r.Kind+"\x00"+r.Name
	})
	out.GroupCount = len(out.Groups)
	if len(out.Groups) > 200 {
		out.Groups = out.Groups[:200]
	}
	if a.CriticalPath.Status == "available" {
		var longest Span
		for _, id := range a.CriticalPath.SpanIDs {
			s := byID[id]
			if measuredSpan(s) && s.DurationMS > longest.DurationMS {
				longest = s
			}
		}
		if longest.ID != "" {
			out.Findings = append(out.Findings, Finding{Rule: "critical_span", Severity: "info", Value: longest.DurationMS, SpanIDs: []string{longest.ID}, TaskIDs: []string{}})
		}
	}
	known := []float64{}
	for _, t := range p.Tasks {
		row := TaskEvidence{ID: t.ID, Key: t.LogicalID, Name: t.Name, Status: t.Status, Score: t.Score}
		if row.Key == "" {
			row.Key = t.ID
		}
		if finishedTask(t) {
			d := t.DurationMS
			row.DurationMS = &d
			known = append(known, d)
		}
		out.Tasks = append(out.Tasks, row)
	}
	sort.Float64s(known)
	median := quantile(known, .5)
	if len(known) >= 5 && median > 0 {
		f := Finding{Rule: "slow_tasks", Severity: "warning", SpanIDs: []string{}, TaskIDs: []string{}}
		for _, t := range out.Tasks {
			if t.DurationMS != nil && *t.DurationMS > median*2 {
				f.Count++
				f.TaskIDs = boundedIDs(f.TaskIDs, t.ID)
				f.Value = max(f.Value, *t.DurationMS/median)
			}
		}
		if f.Count > 0 {
			out.Findings = append(out.Findings, f)
		}
	}
	gap := Finding{Rule: "coverage_gap", Severity: "info", SpanIDs: []string{}, TaskIDs: []string{}}
	for _, c := range a.Coverage {
		if c.Status == "available" && c.Ratio < .7 {
			gap.Count++
			gap.TaskIDs = boundedIDs(gap.TaskIDs, c.TaskID)
			gap.Value += c.UnattributedMS
		}
	}
	if gap.Count > 0 {
		out.Findings = append(out.Findings, gap)
	}
	if usageGap.Count > 0 {
		out.Findings = append(out.Findings, usageGap)
	}
	// Counter deltas are grouped by runtime/task/component; resets are excluded.
	previous := map[string]Resource{}
	var periods, throttled float64
	resources := append([]Resource(nil), p.Resources...)
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].TimeMS < resources[j].TimeMS })
	for _, r := range resources {
		if r.CPUThrottling == nil {
			continue
		}
		key := r.TaskID + "\x00" + r.RuntimeID + "\x00" + r.Component
		if prev, ok := previous[key]; ok && r.TimeMS > prev.TimeMS {
			dp := r.CPUThrottling.Periods - prev.CPUThrottling.Periods
			dt := r.CPUThrottling.ThrottledPeriods - prev.CPUThrottling.ThrottledPeriods
			if dp > 0 && dt >= 0 && dt <= dp {
				periods += dp
				throttled += dt
			}
		}
		previous[key] = r
	}
	if periods > 0 && throttled/periods >= .1 {
		out.Findings = append(out.Findings, Finding{Rule: "throttling", Severity: "warning", Value: throttled / periods, SpanIDs: []string{}, TaskIDs: []string{}})
	}
	if f, ok := suspension(p); ok {
		out.Findings = append(out.Findings, f)
	}
	return out
}

// suspension summarizes recorded sandbox pause intervals. Value is the paused
// share of agent time, counted only in tasks that have both pause intervals
// and agent spans. Resource details use sandbox samples taken inside a pause:
// pausing stops CPU but keeps memory, which the held-memory mean makes visible.
func suspension(p Profile) (Finding, bool) {
	f := Finding{Rule: "sandbox_suspension", Severity: "info", SpanIDs: []string{}, TaskIDs: []string{}, Details: map[string]float64{}}
	paused := map[string][]interval{}
	agent := map[string][]interval{}
	unpause := []float64{}
	for _, s := range p.Spans {
		switch {
		case s.Kind == "suspended" && !s.Incomplete:
			f.Count++
			f.SpanIDs = boundedIDs(f.SpanIDs, s.ID)
			paused[s.TaskID] = append(paused[s.TaskID], interval{s.StartMS, s.StartMS + s.DurationMS})
			if v, ok := s.Attributes["unpause_latency_ms"].(float64); ok && validTime(v) {
				unpause = append(unpause, v)
			}
			if badStatus(s.Status) {
				f.Details["failed_unpauses"]++
			}
		case s.Kind == "agent" && measuredSpan(s):
			agent[s.TaskID] = append(agent[s.TaskID], interval{s.StartMS, s.StartMS + s.DurationMS})
		}
	}
	if f.Count == 0 {
		return f, false
	}
	var pausedMS, agentMS float64
	tasks := make([]string, 0, len(paused))
	for task, xs := range paused {
		tasks = append(tasks, task)
		if len(agent[task]) > 0 {
			pausedMS += unionMS(xs)
			agentMS += unionMS(agent[task])
		}
	}
	sort.Strings(tasks)
	for _, task := range tasks {
		f.TaskIDs = boundedIDs(f.TaskIDs, task)
	}
	if agentMS > 0 {
		f.Value = pausedMS / agentMS
		f.Details["paused_ms"], f.Details["agent_ms"] = pausedMS, agentMS
	}
	if len(unpause) > 0 {
		sort.Float64s(unpause)
		f.Details["unpause_p50_ms"], f.Details["unpause_p99_ms"] = quantile(unpause, .5), quantile(unpause, .99)
	}
	// Memory is a point reading; a CPU sample covers the interval since the
	// series' previous sample, so it counts only when that whole interval lies
	// inside one pause (otherwise work just before the pause leaks in).
	within := func(task string, t float64) int {
		for i, x := range paused[task] {
			if t >= x.start && t <= x.end {
				return i
			}
		}
		return -1
	}
	resources := append([]Resource(nil), p.Resources...)
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].TimeMS < resources[j].TimeMS })
	previous := map[string]float64{}
	var memory, cpu []float64
	for _, r := range resources {
		if r.Component != "sandbox" {
			continue
		}
		key := r.TaskID + "\x00" + r.RuntimeID
		prev, seen := previous[key]
		previous[key] = r.TimeMS
		i := within(r.TaskID, r.TimeMS)
		if i < 0 {
			continue
		}
		if r.MemoryBytes != nil {
			memory = append(memory, *r.MemoryBytes)
		}
		if r.CPUPercent != nil && seen && within(r.TaskID, prev) == i {
			cpu = append(cpu, *r.CPUPercent)
		}
	}
	if len(memory) > 0 {
		f.Details["paused_memory_bytes"] = mean(memory)
	}
	if len(cpu) > 0 {
		f.Details["paused_cpu_percent"] = mean(cpu)
	}
	return f, true
}

func mean(xs []float64) float64 {
	total := 0.0
	for _, x := range xs {
		total += x
	}
	return total / float64(len(xs))
}

type TaskPair struct {
	Key       string         `json:"key"`
	Baseline  []TaskEvidence `json:"baseline"`
	Candidate []TaskEvidence `json:"candidate"`
}

// PairTasks never guesses correspondence from display names. Duplicate logical
// IDs are retained as repeated occurrences and summarized by the UI explicitly.
func PairTasks(left, right []TaskEvidence) []TaskPair {
	rows := map[string]*TaskPair{}
	for side, tasks := range [][]TaskEvidence{left, right} {
		for _, t := range tasks {
			r := rows[t.Key]
			if r == nil {
				r = &TaskPair{Key: t.Key, Baseline: []TaskEvidence{}, Candidate: []TaskEvidence{}}
				rows[t.Key] = r
			}
			if side == 0 {
				r.Baseline = append(r.Baseline, t)
			} else {
				r.Candidate = append(r.Candidate, t)
			}
		}
	}
	out := []TaskPair{}
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ScopedProfile retains only overlapping evidence. Span durations are unchanged,
// so callers must label these as full intervals, not time attributed to a window.
func ScopedProfile(p Profile, task string, start, end float64) Profile {
	out := p
	out.Spans = []Span{}
	out.Resources = []Resource{}
	out.Tasks = []Task{}
	out.Edges = []Edge{}
	ids := map[string]bool{}
	for _, s := range p.Spans {
		if (task == "" || task == s.TaskID) && s.StartMS <= end && s.StartMS+s.DurationMS >= start {
			out.Spans = append(out.Spans, s)
			ids[s.ID] = true
		}
	}
	for _, r := range p.Resources {
		if (task == "" || r.TaskID == task || r.TaskID == "") && r.TimeMS >= start && r.TimeMS <= end {
			out.Resources = append(out.Resources, r)
		}
	}
	for _, t := range p.Tasks {
		if (task == "" || t.ID == task) && t.StartMS <= end && t.StartMS+t.DurationMS >= start {
			out.Tasks = append(out.Tasks, t)
		}
	}
	for _, e := range p.Edges {
		if ids[e.Source] && ids[e.Target] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

func InsightVersion(revision uint64, scope string) string {
	return fmt.Sprintf("%d:%s", revision, scope)
}
