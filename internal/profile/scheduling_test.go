package profile

import (
	"math"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

// scheduleFixture: four occurrences of one task (one queued, one slow), one
// occurrence of another task, task and shared runtimes with a throttling
// counter reset.
func scheduleFixture() Profile {
	task := func(id string, start, dur float64) Task {
		return Task{ID: id, LogicalID: id, Name: id, Status: "succeeded", TimeQuality: "measured", StartMS: start, DurationMS: dur}
	}
	life := func(task, phase string, s, d float64) Span {
		return Span{ID: task + "/" + phase, TaskID: task, Name: phase, Kind: "lifecycle", StartMS: s, DurationMS: d, Status: "completed", Attributes: map[string]any{"phase": phase}}
	}
	work := func(id, task, kind string, s, d float64) Span {
		return Span{ID: id, TaskID: task, Name: kind, Kind: kind, StartMS: s, DurationMS: d, Status: "completed"}
	}
	p := Profile{SchemaVersion: SchemaVersion, RunID: "r", Name: "r", DurationMS: 1000}
	p.Tasks = []Task{task("t-001", 0, 100), task("t-002", 100, 100), task("t-003", 200, 100), task("t-004", 300, 400), task("other-001", 0, 900)}
	for _, t := range p.Tasks {
		p.Spans = append(p.Spans, life(t.ID, "task", t.StartMS, t.DurationMS))
	}
	// t-004 waited 50 ms in the admission queue before starting at 300.
	p.Spans = append(p.Spans, Span{ID: "t-004/queue", TaskID: "t-004", Name: "Queue", Kind: "lifecycle", StartMS: 250, DurationMS: 50, Status: "completed", Attributes: map[string]any{"phase": "queue"}})
	// t-004 phases cover 300-500; 500-700 has no recorded phase.
	p.Spans = append(p.Spans, life("t-004", "execute", 300, 200))
	p.Spans = append(p.Spans,
		work("m1", "t-004", "model", 310, 100), work("m2", "t-004", "model", 350, 100), // nested/overlapping: one union
		work("x1", "t-004", "tool", 460, 20), work("p1", "t-004", "suspended", 480, 15),
		work("bad", "t-004", "tool", 600, 10))
	p.Spans[len(p.Spans)-1].Status = "failed"
	cpu := func(v float64) *float64 { return f(v) }
	thr := func(periods, throttled float64) *CPUThrottling {
		return &CPUThrottling{Periods: periods, ThrottledPeriods: throttled}
	}
	for i := 0; i < 10; i++ {
		t := 300 + float64(i)*40
		// The counter resets at the sixth sample.
		periods, throttled := float64(100+i*10), float64(i*5)
		if i >= 5 {
			periods, throttled = float64((i-5)*10), float64((i-5)*5)
		}
		p.Resources = append(p.Resources,
			Resource{TaskID: "t-004", Component: "sandbox", RuntimeID: "s4", TimeMS: t, CPUPercent: cpu(50), MemoryBytes: f(1 << 30), CPUThrottling: thr(periods, throttled)},
			Resource{Component: "bridge", RuntimeID: "b", TimeMS: t, CPUPercent: cpu(10), MemoryBytes: f(1 << 20)})
	}
	return p
}

func TestScheduleLanesQueueUnknownAndWork(t *testing.T) {
	v := Schedule(scheduleFixture(), ScheduleQuery{StartMS: 0, EndMS: 1000, Bins: 100, WidthPX: 1000, Pinned: []string{"t-004"}})
	if v.LanesTotal != 5 || len(v.Lanes) != 5 || v.Lanes[0].TaskID != "t-004" || !v.Lanes[0].Pinned {
		t.Fatalf("lanes: total %d, first %+v", v.LanesTotal, v.Lanes[0])
	}
	if !v.QueueOK {
		t.Fatal("a recorded queue span must enable queue depth")
	}
	var row ImpactRow
	for _, r := range v.Impact {
		if r.TaskID == "t-004" {
			row = r
		}
	}
	if row.QueueMS == nil || *row.QueueMS != 50 || row.Evidence["queue"] != "t-004/queue" {
		t.Fatalf("queue = %v evidence %v", row.QueueMS, row.Evidence)
	}
	// Execute covers 300-500; the queue covers 250-300; 500-700 is unknown.
	if row.UnknownMS == nil || *row.UnknownMS != 200 {
		t.Fatalf("unknown = %v", row.UnknownMS)
	}
	// Overlapping model calls count once: union 310-450.
	if row.ModelMS != 140 || row.ToolMS != 30 || row.PausedMS != 15 || row.Failures != 1 || row.Evidence["failure"] != "bad" {
		t.Fatalf("work = %+v", row)
	}
	var unknown bool
	for _, x := range v.Lanes[0].Lifecycle {
		if x.Kind == "unknown" && x.StartMS == 500 && x.DurationMS == 200 {
			unknown = true
		}
	}
	if !unknown {
		t.Fatalf("lifecycle = %+v", v.Lanes[0].Lifecycle)
	}
}

func TestScheduleBaselineOnlyForTheSameTask(t *testing.T) {
	v := Schedule(scheduleFixture(), ScheduleQuery{StartMS: 0, EndMS: 1000, Bins: 100})
	rows := map[string]ImpactRow{}
	for _, r := range v.Impact {
		rows[r.TaskID] = r
	}
	// t-004 (400 ms) against the median of t-001..t-003 (100 ms).
	if r := rows["t-004"]; r.Slowdown == nil || *r.Slowdown != 4 || r.BaselineN != 3 {
		t.Fatalf("t-004 slowdown = %v n %d", r.Slowdown, r.BaselineN)
	}
	// Another task, and occurrences with fewer than three peers, get none.
	if r := rows["other-001"]; r.Slowdown != nil {
		t.Fatalf("other task paired: %+v", r)
	}
}

func TestScheduleConcurrencyAndResourcesKeepMissingDistinct(t *testing.T) {
	v := Schedule(scheduleFixture(), ScheduleQuery{StartMS: 0, EndMS: 1000, Bins: 10})
	series := map[string]ScheduleSeries{}
	for _, s := range v.Series {
		series[s.Name] = s
	}
	admitted := series["Admitted occurrences"]
	// 0-100 ms: t-001 and other-001 are admitted.
	if *admitted.Max[0] != 2 || *admitted.Mean[0] != 2 {
		t.Fatalf("bin 0 admitted = %v/%v", *admitted.Mean[0], *admitted.Max[0])
	}
	queued := series["Queued occurrences"]
	if *queued.Max[2] != 1 || *queued.Max[0] != 0 {
		t.Fatalf("queued = %v %v", *queued.Max[2], *queued.Max[0])
	}
	// Bins without samples are unmeasured, not zero.
	cpu := series["Task runtime CPU"]
	if cpu.Mean[0] != nil || cpu.Mean[3] == nil || *cpu.Mean[3] != 0.5 {
		t.Fatalf("task cpu = %v %v", cpu.Mean[0], cpu.Mean[3])
	}
	if shared := series["Shared runtime CPU"]; shared.Mean[3] == nil || math.Abs(*shared.Mean[3]-0.1) > 1e-9 {
		t.Fatal("shared runtime must stay a separate series")
	}
	// A counter reset never yields a negative or above-one share.
	for b, x := range series["Throttled period share"].Mean {
		if x != nil && (*x < 0 || *x > 1) {
			t.Fatalf("bin %d throttled share %v", b, *x)
		}
	}
	if !strings.Contains(strings.Join(v.Notes, " "), "host") {
		t.Fatal("missing host identity must be stated")
	}
	// Model calls and pauses never become CPU: CPU comes only from samples.
	for _, r := range v.Impact {
		if r.TaskID == "t-001" && (r.CPUCoreS != nil || r.ResourceCoverage != nil) {
			t.Fatalf("unsampled task has resource values: %+v", r)
		}
	}
}

func TestScheduleWindowClipsAndCountsAllOverlappingLanes(t *testing.T) {
	v := Schedule(scheduleFixture(), ScheduleQuery{StartMS: 320, EndMS: 340, Bins: 4, MaxLanes: 1})
	if v.LanesTotal != 2 || len(v.Lanes) != 1 {
		t.Fatalf("lanes %d of %d", len(v.Lanes), v.LanesTotal)
	}
	for _, r := range v.Impact {
		if r.OverlapMS > 20 || r.ModelMS > 20 {
			t.Fatalf("window not clipped: %+v", r)
		}
	}
}
