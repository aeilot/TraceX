package experiment

// Only outcome enums leave the private results directory. Agent errors, final
// responses, paths and provider messages are deliberately never copied.
func outcome(v string) string {
	switch v {
	case "succeeded", "failed", "not_run", "not_started", "not_needed", "blocked", "confirmed", "cancelled":
		return v
	}
	return "unknown"
}
func aggregateOutcome(previous, current string) string {
	if previous == "" {
		return current
	}
	if previous == current {
		return current
	}
	return "mixed"
}
func summarizeResult(job *Job, raw map[string]any) {
	clean, known := true, false
	harness, evaluation := "", ""
	for _, item := range arr(raw["tasks"]) {
		task, ok := item.(map[string]any)
		if !ok {
			clean = false
			continue
		}
		known = true
		c := outcome(str(obj(task, "cleanup"), "status"))
		if c != "succeeded" && c != "not_needed" {
			clean = false
		}
		harness = aggregateOutcome(harness, outcome(str(obj(task, "harness"), "status")))
		evaluation = aggregateOutcome(evaluation, outcome(str(obj(task, "evaluation"), "status")))
	}
	if infrastructure, ok := raw["infrastructure"].(map[string]any); ok {
		known = true
		c := outcome(str(obj(infrastructure, "cleanup"), "status"))
		if c != "succeeded" && c != "not_needed" {
			clean = false
		}
	} else {
		clean = false
	}
	if known {
		if clean {
			job.CleanupStatus = "confirmed"
		} else {
			job.CleanupStatus = "unconfirmed"
		}
	}
	if harness == "" {
		harness = "not_run"
	}
	if evaluation == "" {
		evaluation = "not_run"
	}
	job.HarnessStatus = harness
	job.EvaluationStatus = evaluation
}
