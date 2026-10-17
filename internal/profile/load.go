package profile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxFileBytes = 32 << 20
const maxRunBytes = 256 << 20

func readBounded(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular file")
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("input exceeds 32 MiB limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if len(b) > maxFileBytes {
		return nil, fmt.Errorf("input exceeds 32 MiB limit")
	}
	return b, err
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}

// Load reads a normalized TraceX JSON file or an ARIES run directory. It never
// executes evidence or exposes raw command, prompt, response, or environment data.
func Load(path string) (Profile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Profile{}, err
	}
	if info.IsDir() {
		return loadARIES(path)
	}
	b, err := readBounded(path)
	if err != nil {
		return Profile{}, err
	}
	p := empty()
	if err := decode(b, &p); err != nil {
		return Profile{}, fmt.Errorf("decode profile: %w", err)
	}
	if err := Validate(&p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

type nativeTask struct {
	TaskID    string    `json:"task_id"`
	StartedAt time.Time `json:"started_at"`
	Duration  int64     `json:"duration"`
	Error     string    `json:"error"`
	Harness   struct {
		Status string `json:"status"`
	} `json:"harness"`
	Evaluation struct {
		Status string `json:"status"`
	} `json:"evaluation"`
	Cleanup struct {
		Status string `json:"status"`
	} `json:"cleanup"`
}
type nativeRun struct {
	RunID    string       `json:"run_id"`
	Name     string       `json:"name"`
	Duration int64        `json:"duration"`
	Tasks    []nativeTask `json:"tasks"`
}
type artifact struct{ path, task, kind string }
type timedSpan struct {
	span                    Span
	start                   int64
	end                     int64
	rawID, rawParent, trace string
}
type timedResource struct {
	r  Resource
	at int64
}

func loadARIES(root string) (Profile, error) {
	b, err := readBounded(filepath.Join(root, "run-result.json"))
	if err != nil {
		return Profile{}, fmt.Errorf("read ARIES run-result.json: %w", err)
	}
	var run nativeRun
	if err := decode(b, &run); err != nil {
		return Profile{}, fmt.Errorf("decode ARIES result: %w", err)
	}
	if len(run.Tasks) == 0 {
		return Profile{}, fmt.Errorf("ARIES result has no task occurrences")
	}
	p := empty()
	p.RunID = run.RunID
	p.Name = run.Name
	p.DurationMS = float64(run.Duration) / 1e6
	if p.RunID == "" {
		p.RunID = filepath.Base(root)
	}
	if p.Name == "" {
		p.Name = p.RunID
	}
	taskDirs := map[string]string{}
	counts := map[string]int{}
	origin := int64(0)
	observe := func(at int64) {
		if at > 0 && (origin == 0 || at < origin) {
			origin = at
		}
	}
	for _, t := range run.Tasks {
		counts[t.TaskID]++
	}
	seen := map[string]int{}
	for _, t := range run.Tasks {
		seen[t.TaskID]++
		id := t.TaskID
		if counts[t.TaskID] > 1 {
			id = fmt.Sprintf("%s#%d", t.TaskID, seen[t.TaskID])
		}
		if t.TaskID == "" {
			return Profile{}, fmt.Errorf("ARIES task missing task_id")
		}
		status := t.Harness.Status
		if status == "" {
			status = "unknown"
		}
		p.Tasks = append(p.Tasks, Task{ID: id, Name: t.TaskID, Status: status, DurationMS: float64(t.Duration) / 1e6, EvaluationStatus: t.Evaluation.Status, CleanupStatus: t.Cleanup.Status})
		if !t.StartedAt.IsZero() {
			observe(t.StartedAt.UnixNano())
		} else {
			p.Warnings = appendUnique(p.Warnings, "Task start timestamp unavailable; its timeline start is unknown and shown at zero.")
		}
		if counts[t.TaskID] == 1 {
			taskDirs[t.TaskID] = id
		}
	}
	if len(run.Tasks) > maxRecords {
		return Profile{}, fmt.Errorf("too many task occurrences")
	}
	var artifacts []artifact
	var size int64
	visited := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 20000 {
			return fmt.Errorf("run exceeds 20000 filesystem entries")
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if d.IsDir() {
			if len(parts) > 8 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		kind := ""
		switch d.Name() {
		case "otel-spans.jsonl":
			kind = "otel"
		case "tool-calls.jsonl":
			kind = "bridge"
		case "resources.jsonl":
			kind = "resource"
		case "index.json":
			if filepath.Base(filepath.Dir(path)) != "monitor" {
				return nil
			}
			kind = "monitor-index"
		case "trajectory.json", "trajectory.jsonl", "agent-result.json":
			kind = "opaque"
		default:
			if strings.Contains(d.Name(), ".trajectory.") {
				kind = "opaque"
			} else {
				return nil
			}
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		size += info.Size()
		if size > maxRunBytes {
			return fmt.Errorf("run evidence exceeds 256 MiB limit")
		}
		task := ""
		if len(parts) > 1 {
			task = taskDirs[parts[0]]
		}
		if task == "" && parts[0] != "infrastructure" {
			p.Warnings = appendUnique(p.Warnings, "Some evidence could not be assigned to a unique task occurrence and was skipped.")
			return nil
		}
		artifacts = append(artifacts, artifact{path, task, kind})
		return nil
	})
	if err != nil {
		return Profile{}, err
	}
	var spans []timedSpan
	var resources []timedResource
	invalid := 0
	opaque := false
	monitorFailed := false
	monitorUnsupported := false
	monitorDisabled := false
	resourceSeen := map[string]bool{}
	for _, a := range artifacts {
		if a.kind == "opaque" {
			opaque = true
			continue
		}
		data, e := readBounded(a.path)
		if e != nil {
			p.Warnings = appendUnique(p.Warnings, "Some evidence files were unavailable or exceeded the per-file limit.")
			continue
		}
		if a.kind == "monitor-index" {
			var index struct {
				Status string `json:"status"`
				Reason string `json:"reason"`
			}
			if decode(data, &index) != nil {
				invalid++
				continue
			}
			status := index.Status
			switch status {
			case "succeeded", "complete", "completed", "unsupported", "failed", "not_started", "not_enabled", "disabled", "canceled":
			default:
				status = "unknown"
			}
			scope := a.task
			if scope == "" {
				scope = "infrastructure"
			}
			p.Capabilities = append(p.Capabilities, Capability{"monitor/" + scope, status, "Status reported by ARIES monitor index; raw errors are omitted."})
			if status == "failed" {
				monitorFailed = true
				p.Warnings = appendUnique(p.Warnings, "ARIES reports a resource observation failure; retained samples provide partial coverage.")
			}
			if status == "unsupported" {
				monitorUnsupported = true
			}
			if status == "not_enabled" || status == "disabled" {
				monitorDisabled = true
			}
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		line := 0
		for scanner.Scan() {
			line++
			if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
				continue
			}
			if len(spans)+len(resources) >= maxRecords {
				return Profile{}, fmt.Errorf("run exceeds %d evidence records", maxRecords)
			}
			var v map[string]any
			if decode(scanner.Bytes(), &v) != nil {
				invalid++
				continue
			}
			switch a.kind {
			case "otel":
				s, e := parseSpan(v, a.task, line)
				if e != nil {
					invalid++
					continue
				}
				if a.task == "" {
					invalid++
					continue
				}
				if s.span.AgentID == "" {
					p.Warnings = appendUnique(p.Warnings, "Some spans lack an explicit agent identity; task-level unassigned lanes do not identify measured agents.")
				}
				observe(s.start)
				spans = append(spans, s)
			case "bridge":
				start := timestamp(v["timestamp"])
				duration := number(v["duration_ms"])
				if start <= 0 || !validTime(duration) || duration > float64(start)/1e6 {
					invalid++
					continue
				}
				start -= int64(duration * 1e6)
				if start <= 0 {
					invalid++
					continue
				}
				op := text(v["operation_class"])
				if op == "" {
					op = "tool"
				}
				if len(op) > 80 {
					op = "tool"
				}
				if a.task == "" {
					invalid++
					continue
				}
				s := Span{ID: fmt.Sprintf("%s/bridge/%d", a.task, line), TaskID: a.task, AgentID: "", Name: "Bridge · " + op, Kind: "tool", Status: text(v["status"]), Source: "ARIES bridge", DurationMS: duration, Attributes: filterAttributes(map[string]any{"exit_code": v["exit_code"]})}
				spans = append(spans, timedSpan{span: s, start: start, end: start + int64(duration*1e6)})
				observe(start)
			case "resource":
				at := timestamp(v["time"])
				if at <= 0 {
					invalid++
					continue
				}
				r := Resource{TaskID: a.task, Component: text(v["component"]), RuntimeID: text(v["runtime_id"])}
				r.CPUPercent = optionalNumber(v, "cpu_percent")
				key := a.task + "/" + r.Component + "/" + r.RuntimeID
				if !resourceSeen[key] && r.CPUPercent != nil {
					r.CPUPercent = nil
					p.Warnings = appendUnique(p.Warnings, "The first ARIES CPU sample per runtime is a counter baseline, not a measured zero; it is omitted from CPU values.")
				}
				resourceSeen[key] = true
				r.MemoryBytes = optionalNumber(v, "memory_usage_bytes")
				if gpu, ok := v["gpu"].(map[string]any); ok {
					r.GPUPercent = optionalNumber(gpu, "utilization_percent")
					r.CPUPercent = nil
					r.MemoryBytes = nil
				}
				if r.CPUPercent == nil && r.MemoryBytes == nil && r.GPUPercent == nil {
					invalid++
					continue
				}
				resources = append(resources, timedResource{r, at})
				observe(at)
			}
		}
		if scanner.Err() != nil {
			invalid++
			p.Warnings = appendUnique(p.Warnings, "An evidence file contains an oversized or unreadable record; later records were not loaded.")
		}
	}
	if origin == 0 {
		p.Warnings = appendUnique(p.Warnings, "No absolute timestamps available; only task durations are known.")
	}
	for i, t := range run.Tasks {
		if !t.StartedAt.IsZero() {
			p.Tasks[i].StartMS = float64(t.StartedAt.UnixNano()-origin) / 1e6
		}
		p.DurationMS = max(p.DurationMS, p.Tasks[i].StartMS+p.Tasks[i].DurationMS)
	}
	keys := map[string]string{}
	duplicate := map[string]bool{}
	for _, s := range spans {
		if s.rawID != "" {
			key := s.span.TaskID + "/" + s.trace + "/" + s.rawID
			if keys[key] != "" {
				duplicate[key] = true
			}
			keys[key] = s.span.ID
		}
	}
	ids := map[string]bool{}
	for _, s := range spans {
		if ids[s.span.ID] {
			invalid++
			continue
		}
		ids[s.span.ID] = true
		s.span.StartMS = float64(s.start-origin) / 1e6
		if s.rawParent != "" {
			key := s.span.TaskID + "/" + s.trace + "/" + s.rawParent
			if keys[key] != "" && !duplicate[key] {
				s.span.ParentID = keys[key]
			} else {
				p.Warnings = appendUnique(p.Warnings, "Some span parents are unresolved or ambiguous; those edges are omitted.")
			}
		}
		p.Spans = append(p.Spans, s.span)
		p.DurationMS = max(p.DurationMS, s.span.StartMS+s.span.DurationMS)
	}
	// Native cycles are malformed evidence: drop cyclic links with a warning.
	byID := map[string]*Span{}
	for i := range p.Spans {
		byID[p.Spans[i].ID] = &p.Spans[i]
	}
	states := map[string]int{}
	var resolve func(*Span)
	resolve = func(s *Span) {
		if states[s.ID] == 2 {
			return
		}
		states[s.ID] = 1
		if s.ParentID != "" {
			parent := byID[s.ParentID]
			if parent == nil || states[s.ParentID] == 1 {
				s.ParentID = ""
				p.Warnings = appendUnique(p.Warnings, "Malformed cyclic or unresolved parent links were omitted.")
			} else {
				resolve(parent)
			}
		}
		states[s.ID] = 2
	}
	for i := range p.Spans {
		resolve(&p.Spans[i])
	}

	for _, r := range resources {
		r.r.TimeMS = float64(r.at-origin) / 1e6
		p.Resources = append(p.Resources, r.r)
		p.DurationMS = max(p.DurationMS, r.r.TimeMS)
	}
	sort.SliceStable(p.Spans, func(i, j int) bool { return p.Spans[i].StartMS < p.Spans[j].StartMS })
	if invalid > 0 {
		p.Warnings = appendUnique(p.Warnings, fmt.Sprintf("Skipped %d invalid or unsupported evidence records.", invalid))
	}
	spanStatus, resourceStatus := "unavailable", "unsupported"
	if len(p.Spans) > 0 {
		spanStatus = "partial"
	}
	if len(p.Resources) > 0 {
		resourceStatus = "available"
	}
	if monitorFailed {
		resourceStatus = "partial"
		if len(p.Resources) == 0 {
			resourceStatus = "failed"
		}
	} else if len(p.Resources) > 0 && (monitorUnsupported || monitorDisabled) {
		resourceStatus = "partial"
	} else if len(p.Resources) == 0 {
		if monitorDisabled {
			resourceStatus = "not_enabled"
		} else if !monitorUnsupported {
			resourceStatus = "unavailable"
		}
	}
	p.Capabilities = append(p.Capabilities, Capability{"spans", spanStatus, "Coverage depends on retained harness and bridge telemetry; absent events are not zero-duration work."}, Capability{"resources", resourceStatus, "Shared infrastructure is kept separate from task resources."}, Capability{"agent_dag", "partial", "Only explicit span parent relationships are shown; parallel timing does not imply causality."})
	if opaque {
		p.Capabilities = append(p.Capabilities, Capability{"openclaw_trajectory", "unsupported", "Opaque trajectory data has no supported span timing contract; use bridge evidence or normalized spans."})
	}
	p.Warnings = appendUnique(p.Warnings, "Timeline origin is the earliest retained task or observation timestamp; cross-process clock synchronization is not verified.")
	if err := Validate(&p); err != nil {
		return Profile{}, fmt.Errorf("invalid ARIES profile: %w", err)
	}
	return p, nil
}
func appendUnique(v []string, s string) []string {
	for _, x := range v {
		if x == s {
			return v
		}
	}
	return append(v, s)
}
func text(v any) string { s, _ := v.(string); return s }
func number(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		n, e := x.Float64()
		if e == nil {
			return n
		}
	case float64:
		return x
	case string:
		n, e := strconv.ParseFloat(x, 64)
		if e == nil {
			return n
		}
	}
	return -1
}
func optionalNumber(v map[string]any, k string) *float64 {
	x, ok := v[k]
	if !ok || x == nil {
		return nil
	}
	n := number(x)
	if !validTime(n) {
		return nil
	}
	return &n
}
func timestamp(v any) int64 {
	if s, ok := v.(string); ok {
		if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
			return t.UnixNano()
		}
		if n, e := strconv.ParseInt(s, 10, 64); e == nil {
			return n
		}
	}
	if n, ok := v.(json.Number); ok {
		i, _ := n.Int64()
		return i
	}
	return 0
}
func parseSpan(v map[string]any, task string, line int) (timedSpan, error) {
	start := timestamp(v["start_time_unix_nano"])
	end := timestamp(v["end_time_unix_nano"])
	if start == 0 {
		start = timestamp(v["start_time"])
	}
	if end == 0 {
		end = timestamp(v["end_time"])
	}
	if start <= 0 || end < start {
		return timedSpan{}, errors.New("invalid span timing")
	}
	id := text(v["span_id"])
	parent := text(v["parent_span_id"])
	trace := text(v["trace_id"])
	if context, ok := v["context"].(map[string]any); ok {
		if id == "" {
			id = text(context["span_id"])
		}
		if trace == "" {
			trace = text(context["trace_id"])
		}
	}
	if parent == "" {
		parent = text(v["parent_id"])
	}
	if parent == "0000000000000000" {
		parent = ""
	}
	nativeID := id
	if nativeID == "" {
		nativeID = fmt.Sprintf("record-%d", line)
	}
	name := text(v["name"])
	if len(name) > 256 {
		name = name[:256]
	}
	if name == "" {
		name = "Unnamed span"
	}
	kind := "other"
	lower := strings.ToLower(name)
	switch {
	case lower == "agent" || strings.HasPrefix(lower, "agent."):
		kind = "agent"
	case strings.HasPrefix(lower, "api."), strings.Contains(lower, "chat"), strings.Contains(lower, "llm"):
		kind = "model"
	case strings.Contains(lower, "browser"):
		kind = "browser"
	case strings.HasPrefix(lower, "tool."), strings.Contains(lower, "tools/call"):
		kind = "tool"
	}
	attrs, _ := v["attributes"].(map[string]any)
	agent := text(v["agent_id"])
	if agent == "" {
		agent = text(attrs["agent.id"])
	}
	attrs = filterAttributes(attrs)
	status := text(v["status"])
	if status == "" {
		if st, ok := v["status"].(map[string]any); ok {
			status = text(st["status_code"])
			if status == "" {
				status = text(st["code"])
			}
		}
	}
	if status == "" {
		status = "unknown"
	}
	return timedSpan{span: Span{ID: task + "/otel/" + trace + "/" + nativeID, TaskID: task, AgentID: agent, Name: name, Kind: kind, Status: status, DurationMS: float64(end-start) / 1e6, Source: "OpenTelemetry", Attributes: attrs}, start: start, end: end, rawID: id, rawParent: parent, trace: trace}, nil
}
