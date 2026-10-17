package profile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// streamRecords keeps memory proportional to one record. An incomplete final
// JSON record is pending while a producer is appending, never malformed evidence.
func streamRecords(path string, consume func(map[string]any, int)) (invalid int, pending bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false, err
	}
	if !info.Mode().IsRegular() {
		return 0, false, fmt.Errorf("not a regular evidence file")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(io.LimitReader(f, maxRunBytes+1), 64<<10)
	line := 0
	total := 0
	for {
		b, e := r.ReadBytes('\n')
		total += len(b)
		if total > maxRunBytes {
			return invalid, pending, fmt.Errorf("evidence exceeds streaming limit")
		}
		line++
		if len(bytes.TrimSpace(b)) > 0 {
			var v map[string]any
			if len(b) > 4<<20 {
				invalid++
			} else if decode(b, &v) != nil {
				if e == io.EOF {
					pending = true
				} else {
					invalid++
				}
			} else {
				consume(v, line)
			}
		}
		if e == io.EOF {
			return invalid, pending, nil
		}
		if e != nil {
			return invalid, pending, e
		}
	}
}

type trajectoryState struct {
	starts    map[string]timedSpan
	agent     string
	supported bool
}

func (s *trajectoryState) parse(v map[string]any, task string, line int) []timedSpan {
	if s.starts == nil {
		s.starts = map[string]timedSpan{}
	}
	typ := text(v["type"])
	at := timestamp(v["ts"])
	if at == 0 {
		at = timestamp(v["timestamp"])
	}
	if at <= 0 {
		return nil
	}
	data, _ := v["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	session := text(v["sessionId"])
	if session == "" {
		session = "session"
	}
	base := func(id, name, kind string) timedSpan {
		return timedSpan{span: Span{ID: task + "/openclaw/" + id, TaskID: task, AgentID: s.agent, Name: name, Kind: kind, Status: "unknown", Source: "OpenClaw trajectory", Incomplete: true}, start: at, end: at}
	}
	switch typ {
	case "session.started":
		s.supported = true
		s.agent = text(data["agentId"])
		x := base("session/"+session, "OpenClaw session", "agent")
		x.span.AgentID = s.agent
		s.starts["session/"+session] = x
	case "session.ended":
		s.supported = true
		key := "session/" + session
		if x, ok := s.starts[key]; ok && at >= x.start {
			x.end = at
			x.span.DurationMS = float64(at-x.start) / 1e6
			x.span.Incomplete = false
			x.span.Status = text(data["status"])
			delete(s.starts, key)
			return []timedSpan{x}
		}
	case "model.completed":
		s.supported = true
		x := base(fmt.Sprintf("model/%s/%d", session, line), "Model completion (duration unavailable)", "model")
		u, _ := data["usage"].(map[string]any)
		if u != nil {
			x.span.Usage = &Usage{InputTokens: optionalNumber(u, "input"), OutputTokens: optionalNumber(u, "output")}
		}
		return []timedSpan{x}
	case "tool.started", "tool.completed":
		s.supported = true
		id := text(data["toolCallId"])
		if id == "" {
			return nil
		}
		key := "tool/" + session + "/" + id
		if typ == "tool.started" {
			s.starts[key] = base(key, "Tool call", "tool")
		} else if x, ok := s.starts[key]; ok && at >= x.start {
			x.end = at
			x.span.DurationMS = float64(at-x.start) / 1e6
			x.span.Incomplete = false
			x.span.Status = "completed"
			delete(s.starts, key)
			return []timedSpan{x}
		}
	case "message":
		m, _ := v["message"].(map[string]any)
		if m == nil {
			return nil
		}
		s.supported = true
		if text(m["role"]) == "assistant" {
			content, _ := m["content"].([]any)
			for _, c := range content {
				call, _ := c.(map[string]any)
				if text(call["type"]) != "toolCall" {
					continue
				}
				id := text(call["id"])
				if id != "" {
					s.starts["message-tool/"+id] = base("message-tool/"+id, "Tool call", "tool")
				}
			}
		}
		if text(m["role"]) == "toolResult" {
			id := text(m["toolCallId"])
			key := "message-tool/" + id
			if x, ok := s.starts[key]; id != "" && ok && at >= x.start {
				x.end = at
				x.span.DurationMS = float64(at-x.start) / 1e6
				x.span.Incomplete = false
				x.span.Status = "completed"
				delete(s.starts, key)
				return []timedSpan{x}
			}
		}
	}
	return nil
}

func parseBrowser(v map[string]any, task string, line int) (timedSpan, bool) {
	kind := text(v["type"])
	start, end := number(v["start_ms"]), number(v["end_ms"])
	name := "Browser request"
	spanKind := "network"
	if kind == "request" {
		if timing, ok := v["timing"].(map[string]any); ok {
			if response := optionalNumber(timing, "response_end"); response != nil && start > 0 {
				end = start + *response
			}
		}
		// Resource types are an enum, never URLs or user-authored names.
		rt := text(v["resource_type"])
		switch rt {
		case "document", "stylesheet", "image", "media", "font", "script", "texttrack", "xhr", "fetch", "eventsource", "websocket", "manifest", "other":
			name += " · " + rt
		}
	} else if kind == "load" {
		nav, _ := v["navigation_timing"].(map[string]any)
		start = number(nav["time_origin"])
		duration := optionalNumber(nav, "load_end")
		if duration == nil {
			return timedSpan{}, false
		}
		end = start + *duration
		name = "Page navigation"
		spanKind = "browser"
	} else {
		return timedSpan{}, false
	}
	if start <= 0 || !validTime(start) || !validTime(end) || end < start || end > float64(math.MaxInt64)/1e6 {
		return timedSpan{}, false
	}
	status := "unknown"
	if number(v["status"]) >= 100 {
		status = fmt.Sprintf("HTTP %.0f", number(v["status"]))
	}
	if text(v["failure"]) != "" {
		status = "failed"
	}
	return timedSpan{span: Span{ID: fmt.Sprintf("%s/browser/%d", task, line), TaskID: task, Name: name, Kind: spanKind, Status: status, DurationMS: end - start, Source: "BrowserBench sandbox evidence"}, start: int64(start * 1e6), end: int64(end * 1e6)}, true
}

func sourceIdentity(v map[string]any, attrs map[string]any) string {
	if id := text(attrs["agent.id"]); id != "" {
		return id
	}
	r, _ := v["resource"].(map[string]any)
	a, _ := r["attributes"].(map[string]any)
	for _, k := range []string{"agent.id", "gen_ai.agent.id"} {
		if id := text(a[k]); id != "" {
			return id
		}
	}
	return ""
}

func safeConfiguration(root string) map[string]any {
	// ARIES currently does not promise a resolved profile artifact. Accept only
	// explicit profile snapshots; never read harness config or credentials.
	for _, name := range []string{"profile.json", "run-profile.json"} {
		b, e := readBounded(root + string(os.PathSeparator) + name)
		if e != nil {
			continue
		}
		var v map[string]any
		if decode(b, &v) != nil {
			continue
		}
		out := map[string]any{}
		for _, k := range []string{"benchmark", "harness"} {
			if m, ok := v[k].(map[string]any); ok {
				if s := text(m["type"]); len(s) <= 100 && s != "" {
					out[k] = s
				}
			}
		}
		if m, ok := v["model"].(map[string]any); ok {
			for _, k := range []string{"id", "provider"} {
				if s := text(m[k]); s != "" && len(s) <= 256 {
					out[map[string]string{"id": "model", "provider": "provider"}[k]] = s
				}
			}
		}
		for _, k := range []string{"concurrency", "cpu_limit", "memory_limit_mb", "harness_cpu_limit", "harness_memory_limit_mb", "sandbox_cpu_limit", "sandbox_memory_limit_mb", "temperature", "max_tokens", "context_length"} {
			if n := optionalNumber(v, k); n != nil {
				out[k] = *n
			}
		}
		if value := text(v["reasoning_effort"]); value != "" && len(value) <= 256 {
			out["reasoning_effort"] = value
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func trajectoryName(name string) bool {
	return strings.HasSuffix(name, ".trajectory.jsonl") || name == "trajectory.jsonl"
}

func lifecycleSpans(root string, tasks map[string]string, readRecords func(string, func(map[string]any, int)) (int, bool, error)) ([]timedSpan, error) {
	starts := map[string]timedSpan{}
	out := []timedSpan{}
	_, _, err := readRecords(root+string(os.PathSeparator)+"aries.log", func(v map[string]any, line int) {
		if text(v["msg"]) != "task phase" {
			return
		}
		task := tasks[text(v["task_id"])]
		at := timestamp(v["time"])
		phase := text(v["phase"])
		id := text(v["phase_id"])
		switch phase {
		case "sandbox_start", "prepare", "bridge_start", "harness_start", "execute", "isolation", "evaluate", "cleanup":
		default:
			return
		}
		if task == "" || at <= 0 || id == "" {
			return
		}
		key := task + "/phase/" + id
		switch text(v["event"]) {
		case "start":
			starts[key] = timedSpan{span: Span{ID: key, TaskID: task, Name: strings.ReplaceAll(phase, "_", " "), Kind: "lifecycle", Source: "ARIES phase events", Incomplete: true, Status: "running", Attributes: map[string]any{"phase": phase}}, start: at, end: at}
			if id := text(v["depends_on"]); id != "" {
				x := starts[key]
				x.dependency = task + "/phase/" + id
				starts[key] = x
			}
		case "end":
			if x, ok := starts[key]; ok && at >= x.start && text(x.span.Attributes["phase"]) == phase {
				x.end = at
				x.span.DurationMS = float64(at-x.start) / 1e6
				if measured := optionalNumber(v, "duration_ms"); measured != nil {
					x.span.DurationMS = *measured
					x.end = x.start + int64(*measured*1e6)
				}
				x.span.Status = text(v["status"])
				x.span.Incomplete = false
				out = append(out, x)
				delete(starts, key)
			}
		}
	})
	for _, x := range starts {
		out = append(out, x)
	}
	return out, err
}
