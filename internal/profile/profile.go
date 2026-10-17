// Package profile normalizes evidence without inferring causal relationships.
package profile

import (
	"encoding/json"
	"fmt"
	"math"
)

const SchemaVersion = 1
const maxRecords = 100000

type Profile struct {
	SchemaVersion int          `json:"schema_version"`
	RunID         string       `json:"run_id"`
	Name          string       `json:"name"`
	DurationMS    float64      `json:"duration_ms"`
	Tasks         []Task       `json:"tasks"`
	Spans         []Span       `json:"spans"`
	Edges         []Edge       `json:"edges"`
	Resources     []Resource   `json:"resources"`
	Capabilities  []Capability `json:"capabilities"`
	Warnings      []string     `json:"warnings"`
}
type Task struct {
	ID               string  `json:"id"`
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
	return Profile{SchemaVersion: 1, Tasks: []Task{}, Spans: []Span{}, Edges: []Edge{}, Resources: []Resource{}, Capabilities: []Capability{}, Warnings: []string{}}
}
func validTime(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

var allowedAttributes = map[string]bool{"model": true, "gen_ai.request.model": true, "gen_ai.usage.input_tokens": true, "gen_ai.usage.output_tokens": true, "tool.name": true, "exit_code": true, "synthetic": true}

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

// Validate checks IDs, timing, references, and acyclicity. Edges must agree with
// explicit parent IDs; a temporal overlap is never sufficient evidence.
func Validate(p *Profile) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", p.SchemaVersion)
	}
	if !validTime(p.DurationMS) {
		return fmt.Errorf("invalid run duration")
	}
	if len(p.Tasks)+len(p.Spans)+len(p.Resources) > maxRecords {
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
		s.Attributes = filterAttributes(s.Attributes)
		spans[s.ID] = s
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("span parent cycle")
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		s := spans[id]
		if s.ParentID != "" {
			if spans[s.ParentID] == nil {
				return fmt.Errorf("unresolved span parent")
			}
			if err := visit(s.ParentID); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range spans {
		if err := visit(id); err != nil {
			return err
		}
	}
	for _, e := range p.Edges {
		s := spans[e.Target]
		if spans[e.Source] == nil || s == nil || s.ParentID != e.Source || e.Kind != "parent" {
			return fmt.Errorf("edge must describe an explicit parent relationship")
		}
	}
	// Derive exactly one edge per explicit parent; imports cannot add speculative edges.
	p.Edges = []Edge{}
	for _, s := range p.Spans {
		if s.ParentID != "" {
			p.Edges = append(p.Edges, Edge{s.ParentID, s.ID, "parent"})
		}
	}
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
	return nil
}
