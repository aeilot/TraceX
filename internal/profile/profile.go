// Package profile normalizes evidence without inferring causal relationships.
package profile

import (
	"encoding/json"
	"fmt"
	"math"
)

const SchemaVersion = 2
const maxRecords = 1000000

type Profile struct {
	SchemaVersion int            `json:"schema_version"`
	RunID         string         `json:"run_id"`
	Name          string         `json:"name"`
	DurationMS    float64        `json:"duration_ms"`
	Tasks         []Task         `json:"tasks"`
	Spans         []Span         `json:"spans"`
	Edges         []Edge         `json:"edges"`
	Resources     []Resource     `json:"resources"`
	Capabilities  []Capability   `json:"capabilities"`
	Warnings      []string       `json:"warnings"`
	Configuration map[string]any `json:"configuration,omitempty"`
}
type Task struct {
	ID               string  `json:"id"`
	LogicalID        string  `json:"logical_id,omitempty"`
	TimeQuality      string  `json:"time_quality,omitempty"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	StartMS          float64 `json:"start_ms"`
	DurationMS       float64 `json:"duration_ms"`
	EvaluationStatus string  `json:"evaluation_status"`
	CleanupStatus    string  `json:"cleanup_status"`
	// Score is the benchmark's continuous accuracy in [0,1] when recorded.
	Score     *float64        `json:"score,omitempty"`
	EndReason string          `json:"end_reason,omitempty"`
	Network   *NetworkSummary `json:"network,omitempty"`
	// Flags are loader-derived evidence-quality markers, never outcomes.
	Flags []string `json:"flags,omitempty"`
}

// NetworkSummary keeps aggregate browser counters only; URLs are never retained.
type NetworkSummary struct {
	Requests       *float64           `json:"requests,omitempty"`
	Failed         *float64           `json:"failed,omitempty"`
	Unsent         *float64           `json:"unsent,omitempty"`
	ResponseBytes  *float64           `json:"response_bytes,omitempty"`
	ActiveMS       *float64           `json:"active_ms,omitempty"`
	Navigations    int                `json:"navigations,omitempty"`
	ByResourceType map[string]float64 `json:"by_resource_type,omitempty"`
}
type Span struct {
	ID         string         `json:"id"`
	TaskID     string         `json:"task_id"`
	AgentID    string         `json:"agent_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Status     string         `json:"status"`
	StartMS    float64        `json:"start_ms"`
	DurationMS float64        `json:"duration_ms"`
	Source     string         `json:"source"`
	Attributes map[string]any `json:"attributes,omitempty"`
	RuntimeID  string         `json:"runtime_id,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	Incomplete bool           `json:"incomplete,omitempty"`
}
type Usage struct {
	InputTokens  *float64 `json:"input_tokens,omitempty"`
	OutputTokens *float64 `json:"output_tokens,omitempty"`
}
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}
type Resource struct {
	TaskID      string   `json:"task_id,omitempty"`
	Component   string   `json:"component"`
	RuntimeID   string   `json:"runtime_id"`
	TimeMS      float64  `json:"time_ms"`
	CPUPercent  *float64 `json:"cpu_percent,omitempty"`
	MemoryBytes *float64 `json:"memory_bytes,omitempty"`
	GPUPercent  *float64 `json:"gpu_percent,omitempty"`
	// Network counters are cumulative bytes per runtime as reported by ARIES.
	NetworkRxBytes   *float64 `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes   *float64 `json:"network_tx_bytes,omitempty"`
	PIDs             *float64 `json:"pids,omitempty"`
	MemoryLimitBytes *float64 `json:"memory_limit_bytes,omitempty"`
	MemoryPeakBytes  *float64 `json:"memory_peak_bytes,omitempty"`
	// CPUThrottling is the cumulative cgroup cpu.stat bandwidth counters for
	// the runtime; nil means throttling was not measured, not that it was zero.
	CPUThrottling *CPUThrottling `json:"cpu_throttling,omitempty"`
}

// CPUThrottling counters are cumulative per runtime; shares are derived from
// consecutive samples of one series and never across a counter reset.
type CPUThrottling struct {
	Periods          float64 `json:"periods"`
	ThrottledPeriods float64 `json:"throttled_periods"`
	ThrottledUS      float64 `json:"throttled_us"`
}
type Capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func empty() Profile {
	return Profile{SchemaVersion: SchemaVersion, Tasks: []Task{}, Spans: []Span{}, Edges: []Edge{}, Resources: []Resource{}, Capabilities: []Capability{}, Warnings: []string{}}
}
func validTime(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

var allowedAttributes = map[string]bool{"model": true, "gen_ai.request.model": true, "gen_ai.usage.input_tokens": true, "gen_ai.usage.output_tokens": true, "tool.name": true, "exit_code": true, "synthetic": true, "phase": true, "time_quality": true, "backend": true, "harness": true, "provider": true, "resource_type": true, "http_status": true, "page": true, "agent.id": true, "bridge_role": true, "suspend_reason": true, "pause_latency_ms": true, "unpause_latency_ms": true}

func filterAttributes(attrs map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range attrs {
		if !allowedAttributes[k] {
			continue
		}
		switch x := v.(type) {
		case string:
			if len(x) <= 256 {
				out[k] = x
			}
		case float64:
			if validTime(x) {
				out[k] = x
			}
		case bool:
			out[k] = x
		case json.Number:
			if n, e := x.Float64(); e == nil && validTime(n) {
				out[k] = n
			}
		}
	}
	return out
}

// Validate checks IDs, timing, references, and all-edge acyclicity. Parent edges
// agree with containment IDs; other v2 links are explicit producer evidence.
// A temporal overlap is never sufficient evidence of a causal relationship.
func Validate(p *Profile) error {
	if p.SchemaVersion != 1 && p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", p.SchemaVersion)
	}
	if !validTime(p.DurationMS) {
		return fmt.Errorf("invalid run duration")
	}
	if len(p.Tasks)+len(p.Spans)+len(p.Resources)+len(p.Edges) > maxRecords {
		return fmt.Errorf("profile exceeds %d records", maxRecords)
	}
	tasks := map[string]bool{}
	for _, t := range p.Tasks {
		if t.ID == "" || tasks[t.ID] {
			return fmt.Errorf("missing or duplicate task ID")
		}
		tasks[t.ID] = true
		if !validTime(t.StartMS) || !validTime(t.DurationMS) || !validTime(t.StartMS+t.DurationMS) {
			return fmt.Errorf("invalid task timing")
		}
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t.Score != nil && (!validTime(*t.Score) || *t.Score > 1) {
			return fmt.Errorf("invalid task score")
		}
		t.EndReason = enumText(t.EndReason)
		if t.Network != nil {
			n := t.Network
			for _, v := range []*float64{n.Requests, n.Failed, n.Unsent, n.ResponseBytes, n.ActiveMS} {
				if v != nil && !validTime(*v) {
					return fmt.Errorf("invalid network summary")
				}
			}
			n.ByResourceType = resourceTypeCounts(n.ByResourceType)
		}
		flags := t.Flags[:0]
		for _, f := range t.Flags {
			if taskFlags[f] {
				flags = append(flags, f)
			}
		}
		t.Flags = flags
	}
	spans := map[string]*Span{}
	for i := range p.Spans {
		s := &p.Spans[i]
		if s.ID == "" || spans[s.ID] != nil {
			return fmt.Errorf("missing or duplicate span ID")
		}
		if !tasks[s.TaskID] {
			return fmt.Errorf("span references unknown task")
		}
		if !validTime(s.StartMS) || !validTime(s.DurationMS) || !validTime(s.StartMS+s.DurationMS) {
			return fmt.Errorf("invalid span timing")
		}
		if s.Usage != nil {
			for _, v := range []*float64{s.Usage.InputTokens, s.Usage.OutputTokens} {
				if v != nil && !validTime(*v) {
					return fmt.Errorf("invalid token usage")
				}
			}
		}
		s.Attributes = filterAttributes(s.Attributes)
		spans[s.ID] = s
	}
	edges := []Edge{}
	seen := map[Edge]bool{}
	add := func(e Edge) {
		if !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}
	for _, e := range p.Edges {
		source, target := spans[e.Source], spans[e.Target]
		if source == nil || target == nil {
			return fmt.Errorf("unresolved edge reference")
		}
		if e.Kind == "parent" {
			if target.ParentID != e.Source {
				return fmt.Errorf("edge must describe an explicit parent relationship")
			}
		} else {
			if p.SchemaVersion == 1 {
				return fmt.Errorf("v1 supports parent edges only")
			}
			switch e.Kind {
			case "spawn", "call", "return", "dependency", "join":
			default:
				return fmt.Errorf("unsupported edge kind %q", e.Kind)
			}
		}
		add(e)
	}
	for _, s := range p.Spans {
		if s.ParentID != "" {
			if spans[s.ParentID] == nil {
				return fmt.Errorf("unresolved span parent")
			}
			add(Edge{s.ParentID, s.ID, "parent"})
		}
	}
	degree := map[string]int{}
	next := map[string][]string{}
	for id := range spans {
		degree[id] = 0
	}
	for _, e := range edges {
		degree[e.Target]++
		next[e.Source] = append(next[e.Source], e.Target)
	}
	queue := []string{}
	for id, d := range degree {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, n := range next[id] {
			degree[n]--
			if degree[n] == 0 {
				queue = append(queue, n)
			}
		}
	}
	if visited != len(spans) {
		return fmt.Errorf("span graph cycle")
	}
	if len(p.Tasks)+len(p.Spans)+len(p.Resources)+len(edges) > maxRecords {
		return fmt.Errorf("profile exceeds %d records", maxRecords)
	}
	p.Edges = edges
	for _, r := range p.Resources {
		if r.TaskID != "" && !tasks[r.TaskID] {
			return fmt.Errorf("resource references unknown task")
		}
		if !validTime(r.TimeMS) {
			return fmt.Errorf("invalid resource time")
		}
		for _, v := range []*float64{r.CPUPercent, r.MemoryBytes, r.GPUPercent, r.NetworkRxBytes, r.NetworkTxBytes, r.PIDs, r.MemoryLimitBytes, r.MemoryPeakBytes} {
			if v != nil && !validTime(*v) {
				return fmt.Errorf("invalid resource reading")
			}
		}
		if c := r.CPUThrottling; c != nil && (!validTime(c.Periods) || !validTime(c.ThrottledPeriods) || !validTime(c.ThrottledUS) || c.ThrottledPeriods > c.Periods) {
			return fmt.Errorf("invalid CPU throttling reading")
		}
	}
	if p.Tasks == nil {
		p.Tasks = []Task{}
	}
	if p.Spans == nil {
		p.Spans = []Span{}
	}
	if p.Resources == nil {
		p.Resources = []Resource{}
	}
	if p.Capabilities == nil {
		p.Capabilities = []Capability{}
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	p.Configuration = filterConfiguration(p.Configuration)
	p.SchemaVersion = SchemaVersion
	return nil
}

var configurationKeys = map[string]bool{"benchmark": true, "harness": true, "model": true, "provider": true, "concurrency": true, "cpu_limit": true, "memory_limit_mb": true, "harness_cpu_limit": true, "harness_memory_limit_mb": true, "sandbox_cpu_limit": true, "sandbox_memory_limit_mb": true, "temperature": true, "max_tokens": true, "context_length": true, "reasoning_effort": true, "agent_timeout_seconds": true, "sandbox_shm_size_mb": true, "task_count": true, "harness_image": true, "sandbox_image": true, "bridge_suspend_mode": true, "bridge_suspend_grace_ms": true, "observed_sandbox_suspend": true}

func filterConfiguration(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if !configurationKeys[k] {
			continue
		}
		switch x := v.(type) {
		case string:
			if len(x) <= 256 {
				out[k] = x
			}
		case float64:
			if validTime(x) {
				out[k] = x
			}
		case int:
			if x >= 0 {
				out[k] = x
			}
		case json.Number:
			if n, e := x.Float64(); e == nil && validTime(n) {
				out[k] = n
			}
		case bool:
			out[k] = x
		}
	}
	return out
}

var taskFlags = map[string]bool{"duration_outlier": true, "sparse_monitor": true}

// Browser resource types are a closed enum; anything else is folded into "other".
var resourceTypes = map[string]bool{"document": true, "stylesheet": true, "image": true, "media": true, "font": true, "script": true, "texttrack": true, "xhr": true, "fetch": true, "eventsource": true, "websocket": true, "manifest": true, "ping": true, "prefetch": true, "preflight": true, "other": true}

func resourceTypeCounts(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := map[string]float64{}
	for k, v := range in {
		if !validTime(v) {
			continue
		}
		if !resourceTypes[k] {
			k = "other"
		}
		out[k] += v
	}
	return out
}

// enumText keeps short identifier-like producer enums (e.g. end reasons) and
// drops free text, which may carry raw errors.
func enumText(s string) string {
	if len(s) == 0 || len(s) > 64 {
		return ""
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return ""
		}
	}
	return s
}
