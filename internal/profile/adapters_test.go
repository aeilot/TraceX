package profile

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeTrajectoryBrowserAndPhases(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","tasks":[{"task_id":"task","scheduled_at":"2026-01-01T00:00:00Z","started_at":"2026-01-01T00:00:01Z","duration":5000000000,"harness":{"status":"succeeded"}}]}`)
	write(t, root, "task/harness/telemetry/run.trajectory.jsonl", `{"type":"session.started","ts":"2026-01-01T00:00:01Z","sessionId":"s","data":{"agentId":"main","prompt":"SECRET"}}
{"type":"model.completed","ts":"2026-01-01T00:00:02Z","sessionId":"s","data":{"usage":{"input":100,"output":20},"assistantTexts":["SECRET"]}}
{"type":"message","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","arguments":{"command":"SECRET"}}]}}
{"type":"message","timestamp":"2026-01-01T00:00:03Z","message":{"role":"toolResult","toolCallId":"c1","content":"SECRET"}}
{"type":"session.ended","ts":"2026-01-01T00:00:05Z","sessionId":"s","data":{"status":"completed"}}
`)
	write(t, root, "task/evaluation/browser-network.jsonl", `{"type":"request","start_ms":1767225602000,"end_ms":1767225602900,"timing":{"response_end":200},"url":"https://example.com/SECRET","resource_type":"script","status":200}
{"type":"load","navigation_timing":{"time_origin":1767225602000,"load_end":500}}
`)
	write(t, root, "aries.log", `{"msg":"task phase","task_id":"task","phase":"prepare","phase_id":"p1","event":"start","time":"2026-01-01T00:00:01Z"}
{"msg":"task phase","task_id":"task","phase":"prepare","phase_id":"p1","event":"end","time":"2026-01-01T00:00:02Z","status":"succeeded"}
{"msg":"task phase","task_id":"missing","phase":"execute","phase_id":"p1","event":"start","time":"2026-01-01T00:00:01Z"}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Span{}
	for _, s := range p.Spans {
		found[s.Name] = s
	}
	if s := found["Model completion (duration unavailable)"]; !s.Incomplete || s.DurationMS != 0 || s.Usage == nil || *s.Usage.InputTokens != 100 {
		t.Fatalf("model measurement %+v", s)
	}
	if s := found["Tool call"]; s.DurationMS != 1000 || s.ParentID != "" {
		t.Fatalf("tool call %+v", s)
	}
	if s := found["Browser request · script"]; s.DurationMS != 200 {
		t.Fatalf("network duration %+v", s)
	}
	if s := found["OpenClaw session"]; s.DurationMS != 4000 || s.AgentID != "main" {
		t.Fatalf("session %+v", s)
	}
	if s := found["prepare"]; s.DurationMS != 1000 {
		t.Fatalf("phase %+v", s)
	}
	if s := found["Queue"]; s.DurationMS != 1000 || s.StartMS != 0 {
		t.Fatalf("queue %+v", s)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("private payload leaked")
	}
}

func TestStreamingPendingTailAndLiveStartup(t *testing.T) {
	root := t.TempDir()
	path := "task/monitor/resources.jsonl"
	write(t, root, path, "{\"time\":\"2026-01-01T00:00:01Z\",\"memory_usage_bytes\":10}\ninvalid\n{\"time\":")
	n := 0
	bad, pending, err := streamRecords(filepath.Join(root, path), func(v map[string]any, line int) { n++ })
	if err != nil || bad != 1 || !pending || n != 1 {
		t.Fatalf("n=%d bad=%d pending=%v err=%v", n, bad, pending, err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tasks) != 1 || len(p.Resources) != 1 {
		t.Fatalf("live snapshot %+v", p)
	}
	if len(p.Spans) != 0 {
		t.Fatal("live startup fabricated lifecycle timing")
	}
}

func TestOpenClawNeverJoinsUnmatchedCalls(t *testing.T) {
	s := trajectoryState{}
	_ = s.parse(map[string]any{"type": "tool.started", "ts": "2026-01-01T00:00:00Z", "data": map[string]any{"toolCallId": "a"}}, "t", 1)
	got := s.parse(map[string]any{"type": "tool.completed", "ts": "2026-01-01T00:00:01Z", "data": map[string]any{"toolCallId": "b"}}, "t", 2)
	if len(got) != 0 || len(s.starts) != 1 {
		t.Fatal("different tool call IDs joined")
	}
}

func TestNativeExplicitLifecycleDependenciesAndComparisonSnapshot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"run_id":"r","tasks":[{"task_id":"t","started_at":"2026-01-01T00:00:00Z","duration":3000000000}]}`)
	write(t, root, "run-profile.json", `{"benchmark":{"type":"browserbench"},"harness":{"type":"hermes"},"model":{"id":"m","provider":"openai","base_url":"SECRET"},"concurrency":4,"cpu_limit":2,"memory_limit_mb":1024,"task_instruction":"SECRET"}`)
	write(t, root, "aries.log", `{"msg":"task phase","task_id":"t","phase":"prepare","phase_id":"p1","event":"start","time":"2026-01-01T00:00:00Z","depends_on":""}
{"msg":"task phase","task_id":"t","phase":"prepare","phase_id":"p1","event":"end","time":"2026-01-01T00:00:01Z","duration_ms":1000,"status":"succeeded","depends_on":""}
{"msg":"task phase","task_id":"t","phase":"execute","phase_id":"p2","event":"start","time":"2026-01-01T00:00:01Z","depends_on":"p1"}
{"msg":"task phase","task_id":"t","phase":"execute","phase_id":"p2","event":"end","time":"2026-01-01T00:00:02Z","duration_ms":900,"status":"failed","depends_on":"p1"}
{"msg":"task phase","task_id":"t","phase":"cleanup","phase_id":"p3","event":"start","time":"2026-01-01T00:00:02Z","depends_on":"p2"}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Edges) != 1 || p.Edges[0] != (Edge{Source: "t/phase/p1", Target: "t/phase/p2", Kind: "dependency"}) {
		t.Fatalf("explicit phases %+v", p.Edges)
	}
	for _, s := range p.Spans {
		if s.ID == "t/phase/p2" && s.DurationMS != 900 {
			t.Fatal("monotonic phase duration ignored")
		}
	}
	if p.Configuration["model"] != "m" || p.Configuration["concurrency"] != float64(4) || p.Configuration["benchmark"] != "browserbench" {
		t.Fatalf("comparison metadata %+v", p.Configuration)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("snapshot leaked private fields")
	}
}

func TestLiveTaskDiscoveryRequiresEvidence(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sglang/server.log", "unrelated runtime logs")
	write(t, root, "notes/readme.txt", "unrelated directory")
	write(t, root, "real/harness/telemetry/agent-result.json", `{}`)
	write(t, root, "aries.log", `{"msg":"task started","task_id":"log-only","time":"2026-01-01T00:00:00Z"}
{"msg":"task finished","task_id":"log-only","time":"2026-01-01T00:00:01Z","status":"succeeded"}
{"msg":"something else","task_id":"untrusted"}
`)
	p, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]Task{}
	for _, task := range p.Tasks {
		ids[task.ID] = task
	}
	if len(ids) != 2 || ids["real"].ID == "" || ids["log-only"].ID == "" {
		t.Fatalf("fabricated or missed tasks: %+v", ids)
	}
	if ids["log-only"].Status != "unknown" {
		t.Fatal("log-only identity inferred an outcome")
	}
}
func TestConfigurationPreservesIndependentResourceOverrides(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-profile.json", `{"harness_cpu_limit":0,"harness_memory_limit_mb":2048,"sandbox_cpu_limit":4,"sandbox_memory_limit_mb":4096,"temperature":0,"max_tokens":512,"context_length":8192,"reasoning_effort":"high","agent_timeout_seconds":600,"sandbox_shm_size_mb":256,"task_count":8,"harness_image":"hermes:v1","sandbox_image":"mcp:v1","endpoint":"PRIVATE"}`)
	config := safeConfiguration(root)
	if config["harness_cpu_limit"] != float64(0) || config["sandbox_cpu_limit"] != float64(4) || config["temperature"] != float64(0) || config["reasoning_effort"] != "high" || config["agent_timeout_seconds"] != float64(600) || config["sandbox_shm_size_mb"] != float64(256) || config["task_count"] != float64(8) || config["harness_image"] != "hermes:v1" || config["sandbox_image"] != "mcp:v1" {
		t.Fatal(config)
	}
	if _, ok := config["cpu_limit"]; ok {
		t.Fatal("fabricated effective limit")
	}
	if _, ok := config["endpoint"]; ok {
		t.Fatal("private endpoint retained")
	}
	p := testProfile()
	p.Configuration = config
	if e := Validate(&p); e != nil {
		t.Fatal(e)
	}
	if len(p.Configuration) != 13 {
		t.Fatal(p.Configuration)
	}
}
func TestLifecycleDependenciesParticipateInRecordedCompletionPath(t *testing.T) {
	p := testProfile()
	p.Spans = []Span{{ID: "prepare", TaskID: "t", Kind: "lifecycle", DurationMS: 10}, {ID: "execute", TaskID: "t", Kind: "lifecycle", StartMS: 10, DurationMS: 70}, {ID: "umbrella", TaskID: "t", Kind: "lifecycle", DurationMS: 100}}
	p.Edges = []Edge{{Source: "prepare", Target: "execute", Kind: "dependency"}}
	a := Analyze(p)
	if a.CriticalPath.Status != "available" || a.CriticalPath.DurationMS != 80 {
		t.Fatal(a.CriticalPath)
	}
	if a.Coverage[0].ObservedMS != 0 {
		t.Fatal("lifecycle counted as observed work")
	}
}

func TestBrowserMissingMeasurementsNeverBecomeMeasuredZero(t *testing.T) {
	base := map[string]any{"type": "request", "start_ms": 1767225602000.0, "end_ms": 1767225602900.0, "timing": map[string]any{}}
	got, ok := parseBrowser(base, "t", 1)
	if !ok || got.span.DurationMS != 900 {
		t.Fatal("missing response_end replaced valid end timestamp")
	}
	base["timing"] = map[string]any{"response_end": 0.0}
	got, ok = parseBrowser(base, "t", 1)
	if !ok || got.span.DurationMS != 0 {
		t.Fatal("explicit measured zero lost")
	}
	for _, raw := range []map[string]any{{"type": "request", "start_ms": 1767225602000.0}, {"type": "load", "navigation_timing": map[string]any{"time_origin": 1767225602000.0}}} {
		if _, ok := parseBrowser(raw, "t", 1); ok {
			t.Fatal("missing end measurement accepted")
		}
	}
}
func TestBridgeDurationRequiredButExplicitZeroRetained(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"t","started_at":"2026-01-01T00:00:00Z","duration":1000000000}]}`)
	write(t, root, "t/bridge/tool-calls.jsonl", `{"timestamp":"2026-01-01T00:00:00Z","operation_class":"exec"}
{"timestamp":"2026-01-01T00:00:00Z","operation_class":"exec","duration_ms":0}
`)
	p, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, s := range p.Spans {
		if s.Source == "ARIES bridge" {
			n++
			if s.DurationMS != 0 {
				t.Fatal(s)
			}
		}
	}
	if n != 1 {
		t.Fatal("missing duration fabricated measurement", n)
	}
}

func TestTaskScoreEndReasonAndNetworkSummary(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"a","started_at":"2026-01-01T00:00:00Z","duration":1000000000,"harness":{"status":"succeeded"},"evaluation":{"status":"failed"}},{"task_id":"b","started_at":"2026-01-01T00:00:00Z","duration":1000000000}]}`)
	write(t, root, "a/evaluation/score.json", `{"prediction":"SECRET-PRED","gold":"SECRET-GOLD","accuracy":0.99}`)
	write(t, root, "a/harness/session-outcome.json", `{"status":"canceled","end_reason":"deadline_exceeded","run_id":"x"}`)
	write(t, root, "b/harness/session-outcome.json", `{"end_reason":"failed: SECRET error text"}`)
	write(t, root, "b/evaluation/score.json", `{"accuracy":7}`)
	write(t, root, "a/evaluation/network-summary.json", `{"requests":10,"failed":2,"unsent":1,"response_bytes":4096,"network_active_ms":120.5,"by_resource_type":{"script":6,"image":3,"https://SECRET":1},"navigations":[{"url":"https://SECRET.example/"},{"url":"https://SECRET.example/2"}]}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a, b := p.Tasks[0], p.Tasks[1]
	if a.Score == nil || *a.Score != 0.99 || a.EndReason != "deadline_exceeded" || a.EvaluationStatus != "failed" {
		t.Fatalf("task a %+v", a)
	}
	if b.Score != nil || b.EndReason != "" {
		t.Fatalf("out-of-range score or free-text reason kept %+v", b)
	}
	n := a.Network
	if n == nil || *n.Requests != 10 || *n.Failed != 2 || *n.Unsent != 1 || *n.ResponseBytes != 4096 || *n.ActiveMS != 120.5 || n.Navigations != 2 {
		t.Fatalf("network %+v", n)
	}
	if n.ByResourceType["script"] != 6 || n.ByResourceType["other"] != 1 || len(n.ByResourceType) != 3 {
		t.Fatalf("resource types %+v", n.ByResourceType)
	}
	an := Analyze(p)
	if an.ScoreKnown != 1 || an.ScoreMean == nil || *an.ScoreMean != 0.99 {
		t.Fatalf("score mean %+v %v", an.ScoreKnown, an.ScoreMean)
	}
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "SECRET") {
		t.Fatal("private payload leaked", string(body))
	}
}

func TestResourceNetworkMemoryLimitAndPIDs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"t","started_at":"2026-01-01T00:00:00Z","duration":3000000000}]}`)
	write(t, root, "t/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:01Z","component":"sandbox","runtime_id":"r","cpu_percent":0,"memory_usage_bytes":100,"memory_limit_bytes":1000,"memory_peak_bytes":150,"pids":4,"network_rx_bytes":10,"network_tx_bytes":5}
{"time":"2026-01-01T00:00:02Z","component":"sandbox","runtime_id":"r","memory_usage_bytes":200,"memory_limit_bytes":0,"network_rx_bytes":30,"network_tx_bytes":9}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 {
		t.Fatalf("resources %+v", p.Resources)
	}
	r := p.Resources[0]
	if *r.NetworkRxBytes != 10 || *r.NetworkTxBytes != 5 || *r.PIDs != 4 || *r.MemoryLimitBytes != 1000 || *r.MemoryPeakBytes != 150 {
		t.Fatalf("first sample %+v", r)
	}
	if r2 := p.Resources[1]; r2.MemoryLimitBytes != nil || r2.PIDs != nil || *r2.NetworkRxBytes != 30 {
		t.Fatalf("zero limit must be unknown, missing pids stay missing %+v", r2)
	}
}

func TestBridgeBookkeepingIsNotToolWork(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[{"task_id":"t","started_at":"2026-01-01T00:00:00Z","duration":1000000000}]}`)
	write(t, root, "t/bridge/tool-calls.jsonl", `{"timestamp":"2026-01-01T00:00:00.5Z","operation_class":"unknown","status":"unsupported","duration_ms":0,"error":"SECRET"}
{"timestamp":"2026-01-01T00:00:00.5Z","operation_class":"sync","status":"denied","duration_ms":0}
{"timestamp":"2026-01-01T00:00:00.5Z","operation_class":"agent","status":"completed","duration_ms":200,"exit_code":0}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, s := range p.Spans {
		if s.Source != "ARIES bridge" {
			continue
		}
		kinds[s.Kind]++
		if s.Kind == "other" && s.Attributes["bridge_role"] != "bookkeeping" {
			t.Fatalf("bookkeeping span unlabeled %+v", s)
		}
	}
	if kinds["tool"] != 1 || kinds["other"] != 2 {
		t.Fatalf("bridge kinds %v", kinds)
	}
	for _, k := range Analyze(p).Kinds {
		if k.Kind == "tool" && (k.Count != 1 || k.SummedMS != 200) {
			t.Fatalf("tool stats %+v", k)
		}
	}
}

func TestDurationOutlierWithSparseMonitorWarns(t *testing.T) {
	root := t.TempDir()
	write(t, root, "run-result.json", `{"tasks":[
{"task_id":"a","started_at":"2026-01-01T00:00:00Z","duration":10000000000},
{"task_id":"b","started_at":"2026-01-01T00:00:00Z","duration":12000000000},
{"task_id":"c","started_at":"2026-01-01T00:00:00Z","duration":11000000000},
{"task_id":"slow","started_at":"2026-01-01T00:00:00Z","duration":600000000000}]}`)
	write(t, root, "slow/monitor/index.json", `{"status":"succeeded","interval_milliseconds":1000}`)
	write(t, root, "slow/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:01Z","component":"sandbox","runtime_id":"r","memory_usage_bytes":1}
{"time":"2026-01-01T00:00:02Z","component":"sandbox","runtime_id":"r","memory_usage_bytes":1}
`)
	write(t, root, "a/monitor/resources.jsonl", `{"time":"2026-01-01T00:00:01Z","component":"sandbox","runtime_id":"q","memory_usage_bytes":1}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range p.Tasks {
		want := task.ID == "slow"
		if got := len(task.Flags) == 2 && task.Flags[0] == "duration_outlier" && task.Flags[1] == "sparse_monitor"; got != want {
			t.Fatalf("flags %s %v", task.ID, task.Flags)
		}
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "Task slow ran 52× the run median") && strings.Contains(w, "sparse monitor coverage") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing outlier warning %v", p.Warnings)
	}
}
