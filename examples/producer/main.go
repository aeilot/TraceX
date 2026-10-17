// Command producer demonstrates the backend-neutral TraceX event contract.
// All names, metrics and timings are synthetic. It does not run an agent.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aeilot/TraceX/internal/profile"
)

func main() {
	interval := flag.Duration("interval", 300*time.Millisecond, "wall-clock delay between synthetic events (0 disables)")
	flag.Parse()
	sequence := uint64(0)
	emit := func(e profile.Event) {
		sequence++
		n := sequence
		e.Sequence = &n
		if err := profile.AppendEvent(os.Stdout, e); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if *interval > 0 {
			time.Sleep(*interval)
		}
	}
	emit(profile.Event{Type: "metadata", Metadata: &profile.Metadata{RunID: "synthetic-live-example", Name: "Synthetic branching agent producer", DurationMS: 0, Configuration: map[string]any{"harness": "synthetic", "model": "synthetic-model", "concurrency": 2}}})
	task := profile.Task{ID: "synthetic-task-1", LogicalID: "synthetic-task", Name: "Synthetic branching task", Status: "running", TimeQuality: "measured", EvaluationStatus: "not_started", CleanupStatus: "not_started"}
	emit(profile.Event{Type: "task", Task: &task})
	phase := func(id, name string, start, duration float64) {
		s := profile.Span{ID: id, TaskID: task.ID, Kind: "lifecycle", Name: name, Status: "succeeded", StartMS: start, DurationMS: duration, Source: "Synthetic event producer", Attributes: map[string]any{"phase": id, "synthetic": true}}
		emit(profile.Event{Type: "span", Span: &s})
	}
	phase("queue", "Queued", 0, 100)
	phase("prepare", "Prepare environment", 100, 200)
	phase("launch", "Launch harness", 300, 100)
	agent := profile.Span{ID: "agent", TaskID: task.ID, AgentID: "synthetic-coordinator", Kind: "agent", Name: "Synthetic coordinator", StartMS: 400, Incomplete: true, Status: "running", Source: "Synthetic event producer"}
	emit(profile.Event{Type: "span", Span: &agent})
	span := func(id, parent, agentID, kind string, start, duration float64) {
		s := profile.Span{ID: id, TaskID: task.ID, ParentID: parent, AgentID: agentID, RuntimeID: "synthetic-runtime", Name: "Synthetic " + id, Kind: kind, Status: "running", StartMS: start, Incomplete: true, Source: "Synthetic event producer"}
		emit(profile.Event{Type: "span", Span: &s})
		s.Incomplete = false
		s.DurationMS = duration
		s.Status = "succeeded"
		if kind == "model" {
			input, output := 100.0, 20.0
			s.Usage = &profile.Usage{InputTokens: &input, OutputTokens: &output}
		}
		emit(profile.Event{Type: "span", Span: &s})
	}
	span("plan", "agent", "synthetic-coordinator", "model", 400, 200)
	span("worker-a", "agent", "synthetic-worker-a", "tool", 650, 400)
	span("worker-b", "agent", "synthetic-worker-b", "tool", 650, 600)
	span("join", "agent", "synthetic-coordinator", "model", 1300, 300)
	for _, edge := range []profile.Edge{{Source: "plan", Target: "worker-a", Kind: "spawn"}, {Source: "plan", Target: "worker-b", Kind: "call"}, {Source: "worker-a", Target: "join", Kind: "join"}, {Source: "worker-b", Target: "join", Kind: "join"}} {
		emit(profile.Event{Type: "edge", Edge: &edge})
	}
	for i := 0; i <= 8; i++ {
		cpu, memory, gpu := float64(i*9), float64((128+i*10)*1024*1024), float64(i*11)
		r := profile.Resource{TaskID: task.ID, Component: "harness", RuntimeID: "synthetic-runtime", TimeMS: float64(i * 200), CPUPercent: &cpu, MemoryBytes: &memory, GPUPercent: &gpu}
		emit(profile.Event{Type: "resource", Resource: &r})
	}
	agent.Incomplete = false
	agent.Status = "succeeded"
	agent.DurationMS = 1200
	emit(profile.Event{Type: "span", Span: &agent})
	phase("execution", "Harness execution", 400, 1200)
	phase("evaluation", "Evaluate", 1600, 200)
	phase("cleanup", "Cleanup", 1800, 100)
	task.Status = "succeeded"
	task.DurationMS = 1900
	task.EvaluationStatus = "succeeded"
	task.CleanupStatus = "succeeded"
	emit(profile.Event{Type: "task", Task: &task})
	emit(profile.Event{Type: "metadata", Metadata: &profile.Metadata{RunID: "synthetic-live-example", Name: "Synthetic branching agent producer", DurationMS: 1900}})
}
