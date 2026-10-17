package profile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Event is the backend-neutral append-only producer contract. Updates replace
// task/span records with the same ID; producers must send complete records.
type Event struct {
	SchemaVersion int       `json:"schema_version"`
	Sequence      *uint64   `json:"sequence,omitempty"`
	Type          string    `json:"type"`
	Task          *Task     `json:"task,omitempty"`
	Span          *Span     `json:"span,omitempty"`
	Edge          *Edge     `json:"edge,omitempty"`
	Resource      *Resource `json:"resource,omitempty"`
	Metadata      *Metadata `json:"metadata,omitempty"`
}
type Metadata struct {
	RunID         string         `json:"run_id,omitempty"`
	Name          string         `json:"name,omitempty"`
	DurationMS    float64        `json:"duration_ms"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

func AppendEvent(w io.Writer, e Event) error {
	if e.SchemaVersion == 0 {
		e.SchemaVersion = 1
	}
	if err := checkEvent(e); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(e)
}
func checkEvent(e Event) error {
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported event schema_version")
	}
	n := 0
	for _, ok := range []bool{e.Task != nil, e.Span != nil, e.Edge != nil, e.Resource != nil, e.Metadata != nil} {
		if ok {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("event must contain exactly one record")
	}
	switch e.Type {
	case "task":
		if e.Task != nil {
			return nil
		}
	case "span":
		if e.Span != nil {
			return nil
		}
	case "edge":
		if e.Edge != nil {
			return nil
		}
	case "resource":
		if e.Resource != nil {
			return nil
		}
	case "metadata":
		if e.Metadata != nil {
			return nil
		}
	}
	return fmt.Errorf("event type does not match record")
}

// ApplyEvents reads newline-committed events. A producer's partial final write
// remains uncommitted and is ignored, so readers can safely retry after append.
func ApplyEvents(base Profile, r io.Reader) (Profile, error) {
	reader := bufio.NewReader(io.LimitReader(r, 256*1024*1024+1))
	events := []Event{}
	total := 0
	partial := false
	for {
		line, err := reader.ReadBytes('\n')
		total += len(line)
		if total > 256*1024*1024 {
			return Profile{}, fmt.Errorf("events exceed 256 MiB")
		}
		if len(line) > 8*1024*1024 {
			return Profile{}, fmt.Errorf("event exceeds 8 MiB")
		}
		if err == io.EOF {
			partial = len(bytes.TrimSpace(line)) > 0
			break
		}
		if err != nil {
			return Profile{}, err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&e); err != nil {
			return Profile{}, fmt.Errorf("invalid event at line %d", len(events)+1)
		}
		var tail any
		if dec.Decode(&tail) != io.EOF {
			return Profile{}, fmt.Errorf("multiple values in event")
		}
		events = append(events, e)
		if len(events) > maxRecords {
			return Profile{}, fmt.Errorf("too many events")
		}
	}
	p, err := ApplyEventBatch(base, events)
	if err == nil && partial {
		p.Warnings = append(p.Warnings, "Unterminated final event is pending; retry after producer commits its newline.")
	}
	return p, err
}
func ApplyEventBatch(base Profile, events []Event) (Profile, error) {
	// Clone to keep malformed updates from mutating the published snapshot.
	raw, err := json.Marshal(base)
	if err != nil {
		return Profile{}, err
	}
	p := empty()
	if err = json.Unmarshal(raw, &p); err != nil {
		return Profile{}, err
	}
	p.SchemaVersion = SchemaVersion
	// These warnings describe the previous event snapshot, not immutable source
	// evidence. Recompute them from the full retained event stream on each replay.
	keptWarnings := p.Warnings[:0]
	for _, warning := range p.Warnings {
		if !transientEventWarning(warning) {
			keptWarnings = append(keptWarnings, warning)
		}
	}
	p.Warnings = keptWarnings
	taskIndex := map[string]int{}
	spanIndex := map[string]int{}
	resourceIndex := map[string]int{}
	edgeSet := map[Edge]bool{}
	for i, t := range p.Tasks {
		taskIndex[t.ID] = i
	}
	for i, s := range p.Spans {
		spanIndex[s.ID] = i
	}
	key := func(r Resource) string {
		return fmt.Sprintf("%s\x00%s\x00%s\x00%.17g", r.TaskID, r.Component, r.RuntimeID, r.TimeMS)
	}
	for i, r := range p.Resources {
		resourceIndex[key(r)] = i
	}
	for _, e := range p.Edges {
		edgeSet[e] = true
	}
	var sequence *uint64
	for _, e := range events {
		if err := checkEvent(e); err != nil {
			return Profile{}, err
		}
		if e.Sequence != nil {
			if sequence != nil && *e.Sequence <= *sequence {
				return Profile{}, fmt.Errorf("event sequence is not increasing")
			}
			n := *e.Sequence
			sequence = &n
		}
		switch e.Type {
		case "task":
			t := *e.Task
			if t.ID == "" {
				return Profile{}, fmt.Errorf("event task missing ID")
			}
			if i, ok := taskIndex[t.ID]; ok {
				p.Tasks[i] = t
			} else {
				taskIndex[t.ID] = len(p.Tasks)
				p.Tasks = append(p.Tasks, t)
			}
		case "span":
			s := *e.Span
			if s.ID == "" {
				return Profile{}, fmt.Errorf("event span missing ID")
			}
			if i, ok := spanIndex[s.ID]; ok {
				p.Spans[i] = s
			} else {
				spanIndex[s.ID] = len(p.Spans)
				p.Spans = append(p.Spans, s)
			}
		case "edge":
			if !edgeSet[*e.Edge] {
				p.Edges = append(p.Edges, *e.Edge)
				edgeSet[*e.Edge] = true
			}
		case "resource":
			r := *e.Resource
			if i, ok := resourceIndex[key(r)]; ok {
				p.Resources[i] = r
			} else {
				resourceIndex[key(r)] = len(p.Resources)
				p.Resources = append(p.Resources, r)
			}
		case "metadata":
			m := e.Metadata
			if m.RunID != "" {
				p.RunID = m.RunID
			}
			if m.Name != "" {
				p.Name = m.Name
			}
			p.DurationMS = m.DurationMS
			if m.Configuration != nil {
				p.Configuration = m.Configuration
			}
		}
		if len(p.Tasks)+len(p.Spans)+len(p.Resources)+len(p.Edges) > maxRecords {
			return Profile{}, fmt.Errorf("too many event records")
		}
	}
	// A live producer can write links before their endpoint records. Publish the
	// resolvable subset and disclose pending references instead of inventing nodes.
	kept := p.Spans[:0]
	for _, s := range p.Spans {
		if _, ok := taskIndex[s.TaskID]; !ok {
			p.Warnings = appendUnique(p.Warnings, "Event span pending an unresolved task reference.")
			delete(spanIndex, s.ID)
			continue
		}
		kept = append(kept, s)
	}
	p.Spans = kept
	resources := p.Resources[:0]
	for _, r := range p.Resources {
		if r.TaskID != "" {
			if _, ok := taskIndex[r.TaskID]; !ok {
				p.Warnings = appendUnique(p.Warnings, "Event resource pending an unresolved task reference.")
				continue
			}
		}
		resources = append(resources, r)
	}
	p.Resources = resources
	for i := range p.Spans {
		if parent := p.Spans[i].ParentID; parent != "" {
			if _, ok := spanIndex[parent]; !ok {
				p.Spans[i].ParentID = ""
				p.Warnings = appendUnique(p.Warnings, "Event parent pending an unresolved span reference.")
			}
		}
	}
	spanByID := map[string]Span{}
	for _, s := range p.Spans {
		spanByID[s.ID] = s
	}
	edges := []Edge{}
	for _, e := range p.Edges {
		_, source := spanIndex[e.Source]
		_, target := spanIndex[e.Target]
		if !source || !target {
			p.Warnings = appendUnique(p.Warnings, "Event edge pending unresolved endpoints.")
			continue
		}
		if e.Kind == "parent" && spanByID[e.Target].ParentID != e.Source {
			continue
		}
		edges = append(edges, e)
	}
	p.Edges = edges
	for _, s := range p.Spans {
		if end := s.StartMS + s.DurationMS; end > p.DurationMS {
			p.DurationMS = end
		}
	}
	for _, t := range p.Tasks {
		if end := t.StartMS + t.DurationMS; end > p.DurationMS {
			p.DurationMS = end
		}
	}
	for _, r := range p.Resources {
		if r.TimeMS > p.DurationMS {
			p.DurationMS = r.TimeMS
		}
	}
	if err := Validate(&p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func transientEventWarning(warning string) bool {
	switch warning {
	case "Unterminated final event is pending; retry after producer commits its newline.",
		"Event span pending an unresolved task reference.",
		"Event parent pending an unresolved span reference.",
		"Event edge pending unresolved endpoints.",
		"Event resource pending an unresolved task reference.":
		return true
	}
	return false
}
