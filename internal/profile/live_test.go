package profile

import "testing"

// Before run-result.json exists, task times come from the lifecycle log. A
// finished occurrence gets its measured duration and outcomes; a running one
// is open, elapsed to the newest evidence, and kept out of duration statistics.
func TestLiveARIESTasksUseLifecycleLogTimes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "aries.log", `{"msg":"task started","task_id":"task-001","time":"2026-01-01T00:00:00Z"}
{"msg":"task started","task_id":"task-002","time":"2026-01-01T00:00:01Z"}
{"msg":"task finished","task_id":"task-001","harness_status":"succeeded","evaluation_status":"failed","cleanup_status":"succeeded","time":"2026-01-01T00:00:04Z"}
{"msg":"task phase","task_id":"task-002","event":"start","phase":"execute","time":"2026-01-01T00:00:02Z"}`)
	write(t, root, "task-002/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:05Z","component":"sandbox","runtime_id":"s","cpu_percent":0,"memory_usage_bytes":1}
{"time":"2026-01-01T00:00:07Z","component":"sandbox","runtime_id":"s","cpu_percent":0,"memory_usage_bytes":1}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tasks := map[string]Task{}
	for _, x := range p.Tasks {
		tasks[x.ID] = x
	}
	done, open := tasks["task-001"], tasks["task-002"]
	if done.Status != "succeeded" || done.EvaluationStatus != "failed" || done.CleanupStatus != "succeeded" || done.DurationMS != 4000 || done.StartMS != 0 || done.TimeQuality != "measured" {
		t.Fatalf("finished task = %+v", done)
	}
	// Started at 1 s; the newest evidence is the resource sample at 7 s.
	if open.Status != "running" || open.TimeQuality != "open" || open.StartMS != 1000 || open.DurationMS != 6000 {
		t.Fatalf("running task = %+v", open)
	}
	for _, s := range p.Spans {
		if s.ID == "task-002/lifecycle/task" && !s.Incomplete {
			t.Fatal("a running task's lifecycle span must be incomplete")
		}
	}
	a := Analyze(p)
	if a.TaskP50MS != 4000 || a.TaskSucceeded != 1 {
		t.Fatalf("a running task must not enter duration statistics: p50=%v succeeded=%d", a.TaskP50MS, a.TaskSucceeded)
	}
	for _, c := range a.Coverage {
		if c.TaskID == "task-002" && c.Status != "unavailable" {
			t.Fatalf("coverage of a running task = %+v", c)
		}
	}
}
