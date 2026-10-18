package profile

import "fmt"

// suspendState pairs the pause and unpause events of one ARIES
// bridge/suspend.jsonl into intervals in which the sandbox was frozen. Each
// event records when the bridge sent the request and how long it took; a
// paused interval runs from the pause taking effect to the unpause request.
type suspendState struct {
	open         bool
	start        int64
	pauseLatency float64
}

// parse returns a span for an unpause that closes a pause, and a warning for
// suspension that failed. invalid reports a record that cannot be paired.
func (s *suspendState) parse(v map[string]any, task string, line int) (span timedSpan, ok bool, warning string, invalid bool) {
	at := timestamp(v["time"])
	latency := optionalNumber(v, "latency_ms")
	if at <= 0 || task == "" || latency == nil || !validTime(*latency) {
		return timedSpan{}, false, "", true
	}
	failed := text(v["error"]) != ""
	switch text(v["event"]) {
	case "pause":
		if failed {
			return timedSpan{}, false, "A sandbox pause failed; ARIES disabled suspension for that occurrence and left it running.", false
		}
		if s.open {
			return timedSpan{}, false, "", true
		}
		s.open, s.start, s.pauseLatency = true, at+int64(*latency*1e6), *latency
		return timedSpan{}, false, "", false
	case "unpause":
		if !s.open {
			return timedSpan{}, false, "", true
		}
		s.open = false
		end := at
		if end < s.start {
			end = s.start
		}
		x := timedSpan{start: s.start, end: end, span: Span{
			ID: fmt.Sprintf("%s/suspend/%d", task, line), TaskID: task, Name: "Sandbox paused", Kind: "suspended",
			Status: "completed", Source: "ARIES bridge suspend", DurationMS: float64(end-s.start) / 1e6,
			Attributes: filterAttributes(map[string]any{"suspend_reason": text(v["reason"]), "pause_latency_ms": s.pauseLatency, "unpause_latency_ms": *latency}),
		}}
		if failed {
			x.span.Status = "failed"
			warning = "A sandbox unpause failed; the occurrence may have stayed paused."
		}
		return x, true, warning, false
	case "disabled":
		return timedSpan{}, false, "", false
	}
	return timedSpan{}, false, "", true
}
