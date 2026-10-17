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

var allowedAttributes = map[string]bool{"model": true, "gen_ai.request.model": true, "gen_ai.usage.input_tokens": true, "gen_ai.usage.output_tokens": true, "tool.name": true, "exit_code": true, "synthetic": true, "phase": true, "time_quality": true, "backend": true, "harness": true, "provider": true, "resource_type": true, "http_status": true, "page": true, "agent.id": true}

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
		for _, v := range []*float64{r.CPUPercent, r.MemoryBytes, r.GPUPercent} {
			if v != nil && !validTime(*v) {
				return fmt.Errorf("invalid resource reading")
			}
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

var configurationKeys = map[string]bool{"benchmark": true, "harness": true, "model": true, "provider": true, "concurrency": true, "cpu_limit": true, "memory_limit_mb": true, "harness_cpu_limit": true, "harness_memory_limit_mb": true, "sandbox_cpu_limit": true, "sandbox_memory_limit_mb": true, "temperature": true, "max_tokens": true, "context_length": true, "reasoning_effort": true}

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
