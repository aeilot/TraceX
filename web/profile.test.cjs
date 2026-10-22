const { test } = require("node:test");
const assert = require("node:assert/strict");
const { validate, union, maximum } = require("./profile.js");
function fixture() {
  return {
    schema_version: 1,
    name: "test",
    run_id: "test",
    duration_ms: 100,
    tasks: [{ id: "t", start_ms: 0, duration_ms: 100 }],
    spans: [
      { id: "a", task_id: "t", name: "agent", start_ms: 0, duration_ms: 100 },
      {
        id: "b",
        parent_id: "a",
        task_id: "t",
        name: "tool",
        start_ms: 10,
        duration_ms: 30,
      },
    ],
    resources: [],
  };
}
test("overlapping calls count once, adjacent and disjoint intervals preserved", () => {
  assert.equal(
    union([
      { start_ms: 0, duration_ms: 20 },
      { start_ms: 10, duration_ms: 30 },
      { start_ms: 40, duration_ms: 10 },
      { start_ms: 60, duration_ms: 5 },
    ]),
    55,
  );
  assert.equal(union([]), 0);
});
test("parents derive edges and missing readings stay missing", () => {
  const p = fixture();
  p.resources = [{ time_ms: 0, cpu_percent: 0 }, { time_ms: 5 }];
  validate(p);
  assert.deepEqual(p.edges, [{ source: "a", target: "b", kind: "parent" }]);
  assert.equal(p.resources[0].cpu_percent, 0);
  assert.equal(p.resources[1].cpu_percent, undefined);
});
test("rejects causal cycles even when edges omitted", () => {
  const p = fixture();
  p.spans[0].parent_id = "b";
  assert.throws(() => validate(p), /cycle/);
});
test("rejects arbitrary edges without a matching explicit parent", () => {
  const p = fixture();
  p.edges = [{ source: "b", target: "a", kind: "parent" }];
  assert.throws(() => validate(p), /explicit parent/);
});
test("rejects duplicates, invalid measurements, unknown tasks, and unresolved parents", () => {
  for (const change of [
    (p) => p.spans.push({ ...p.spans[0] }),
    (p) => p.resources.push({ time_ms: 0, cpu_percent: NaN }),
    (p) => (p.spans[1].task_id = "missing"),
    (p) => (p.spans[1].parent_id = "missing"),
    (p) => (p.duration_ms = Infinity),
    (p) => (p.capabilities = [null]),
    (p) => (p.warnings = [{}]),
  ]) {
    const p = fixture();
    change(p);
    assert.throws(() => validate(p));
  }
});
test("metadata allowlist drops private bodies on browser import", () => {
  const p = fixture();
  p.raw_log = "private";
  p.spans[0].prompt = "private";
  p.spans[0].attributes = {
    prompt: "private",
    model: "demo",
    "tool.arguments": "private",
    synthetic: true,
  };
  validate(p);
  assert.equal(p.raw_log, undefined);
  assert.equal(p.spans[0].prompt, undefined);
  assert.deepEqual(p.spans[0].attributes, { model: "demo", synthetic: true });
});
test("large resource series peak calculation does not spread function arguments", () => {
  assert.equal(maximum(Array.from({ length: 100000 }, (_, i) => i)), 99999);
});
test("task score, end reason, network summary and flags survive import with private keys dropped", () => {
  const p = fixture();
  p.tasks[0] = {
    ...p.tasks[0],
    score: 0.77,
    end_reason: "deadline_exceeded",
    network: {
      requests: 5,
      failed: 1,
      url: "https://SECRET",
      by_resource_type: { script: 3, "https://SECRET": 1 },
    },
    flags: ["duration_outlier", "SECRET"],
    answer: "SECRET",
  };
  p.resources = [
    { time_ms: 0, network_rx_bytes: 10, pids: 3, memory_limit_bytes: 100 },
  ];
  validate(p);
  assert.equal(p.tasks[0].score, 0.77);
  assert.equal(p.tasks[0].end_reason, "deadline_exceeded");
  assert.deepEqual(p.tasks[0].flags, ["duration_outlier"]);
  assert.deepEqual(p.tasks[0].network, {
    requests: 5,
    failed: 1,
    by_resource_type: { script: 3 },
  });
  assert.equal(p.resources[0].pids, 3);
  assert.ok(!JSON.stringify(p).includes("SECRET"));
  const bad = fixture();
  bad.tasks[0].score = 2;
  assert.throws(() => validate(bad));
});
test("mean accuracy excludes tasks without a recorded score", () => {
  const { analyze } = require("./profile.js");
  const p = fixture();
  p.tasks.push({ id: "u", start_ms: 0, duration_ms: 10, score: 0.5 });
  p.tasks.push({ id: "v", start_ms: 0, duration_ms: 10, score: 1 });
  const a = analyze(validate(p));
  assert.equal(a.score_mean, 0.75);
  assert.equal(a.score_known, 2);
});
test("cpu throttling is validated, kept, and absent readings stay unmeasured", () => {
  const p = fixture();
  p.resources = [
    { time_ms: 0, cpu_throttling: { periods: 4, throttled_periods: 1, throttled_us: 9, url: "SECRET" } },
    { time_ms: 1, pids: 2 },
  ];
  validate(p);
  assert.deepEqual(p.resources[0].cpu_throttling, { periods: 4, throttled_periods: 1, throttled_us: 9 });
  assert.equal(p.resources[1].cpu_throttling, undefined);
  for (const c of [{ periods: 1, throttled_periods: 2, throttled_us: 0 }, { periods: 1 }, 5]) {
    const bad = fixture();
    bad.resources = [{ time_ms: 0, cpu_throttling: c }];
    assert.throws(() => validate(bad));
  }
});
test("throttle shares use consecutive samples of one series and skip resets", () => {
  const { throttleShares } = require("./profile.js");
  const c = (periods, throttled_periods) => ({ periods, throttled_periods, throttled_us: 0 });
  const out = throttleShares([
    { runtime_id: "a", time_ms: 0, cpu_throttling: c(10, 0) },
    { runtime_id: "b", time_ms: 1, cpu_throttling: c(100, 100) },
    { runtime_id: "a", time_ms: 2, cpu_throttling: c(20, 5) },
    { runtime_id: "a", time_ms: 3 },
    { runtime_id: "a", time_ms: 4, cpu_throttling: c(2, 0) },
    { runtime_id: "a", time_ms: 5, cpu_throttling: c(2, 0) },
  ]);
  assert.deepEqual(
    out.map((r) => r.throttled_percent),
    [50, null, 0],
  );
});
test("sweep samples and metrics reproduce the steady-window analysis", () => {
  const { sweepSamples, sweepMetrics } = require("./profile.js");
  const p = {
    tasks: [
      { id: "a", status: "succeeded", time_quality: "measured", start_ms: 0, duration_ms: 1000 },
      { id: "b", status: "succeeded", time_quality: "measured", start_ms: 2000, duration_ms: 3000 },
      { id: "c", status: "running", time_quality: "measured", start_ms: 2500, duration_ms: 10 },
    ],
    spans: [
      { id: "turn", task_id: "b", kind: "model", name: "llm.x", start_ms: 2000, duration_ms: 900 },
      { id: "api", task_id: "b", parent_id: "turn", kind: "model", name: "api.x", start_ms: 2000, duration_ms: 300 },
      { id: "call", task_id: "b", kind: "browser", name: "MCP send tools/call browser_click", start_ms: 2400, duration_ms: 200 },
      { id: "wrap", task_id: "b", kind: "browser", name: "tool.mcp__playwright__browser_click", start_ms: 2399, duration_ms: 202 },
      { id: "r1", task_id: "b", kind: "network", start_ms: 2350, duration_ms: 100 },
      { id: "r2", task_id: "b", kind: "network", start_ms: 2420, duration_ms: 60 },
      { id: "r3", task_id: "b", kind: "network", start_ms: 2550, duration_ms: 10 },
    ],
    resources: [
      { task_id: "b", component: "sandbox", runtime_id: "s", time_ms: 0, cpu_throttling: { periods: 10, throttled_periods: 2, throttled_us: 0 } },
      { task_id: "b", component: "sandbox", runtime_id: "s", time_ms: 1, cpu_throttling: { periods: 20, throttled_periods: 7, throttled_us: 0 } },
      { task_id: "b", component: "harness", runtime_id: "h", time_ms: 1, cpu_throttling: { periods: 0, throttled_periods: 0, throttled_us: 0 } },
    ],
  };
  const samples = sweepSamples(p);
  assert.deepEqual(samples[1].model_ms, [300]);
  assert.deepEqual(samples[1].browser_local_ms, [110]);
  assert.equal(samples[1].periods, 10);
  assert.equal(samples[1].throttled_periods, 5);
  assert.equal(samples[0].periods, undefined);
  assert.equal(samples[2].duration_ms, null);
  const all = sweepMetrics(samples);
  assert.equal(all.tasks, 3);
  assert.equal(all.finished, 2);
  assert.equal(all.task_p50_ms, 2000);
  assert.equal(all.throttle_known, 1);
  assert.equal(all.throttled_share, 0.5);
  // Window: started ≥1 s after the first task and finished by 6 s.
  const steady = sweepMetrics(samples, { from_s: 1, to_s: 6 });
  assert.equal(steady.tasks, 1);
  assert.equal(steady.task_p90_ms, 3000);
  assert.equal(steady.model_p50_ms, 300);
  assert.equal(steady.browser_local_p90_ms, 110);
  assert.equal(sweepMetrics([], {}).task_p50_ms, null);
});
test("suspended sandbox intervals are reported but never counted as work", () => {
  const { analyze } = require("./profile.js");
  const p = fixture();
  p.spans[0].kind = "agent";
  p.spans[1].kind = "tool";
  p.spans.push({ id: "s", task_id: "t", name: "Sandbox paused", kind: "suspended", start_ms: 50, duration_ms: 40 });
  const a = analyze(validate(p));
  const suspended = a.kinds.find((k) => k.kind === "suspended");
  assert.ok(suspended, "the suspended kind is listed");
  assert.equal(suspended.union_ms, 40);
  const coverage = a.coverage.find((c) => c.task_id === "t");
  assert.equal(coverage.observed_ms, 30, "only the tool call is observed work");
});
test("timeline layout matches the server: narrow neighbors merge, resolvable spans stay", () => {
  const { timelineLayout } = require("./profile.js");
  const sp = (id, task, kind, start, duration) => ({ id, task_id: task, kind, name: id, start_ms: start, duration_ms: duration });
  // Same case as TestLayoutTimelineMergesOnlyIndistinguishableNeighbors: 10 ms per pixel.
  const l = timelineLayout(
    [
      sp("wide-a", "t1", "model", 0, 300),
      sp("tiny-1", "t1", "tool", 300, 5),
      sp("tiny-2", "t1", "browser", 306, 5),
      sp("tiny-3", "t1", "tool", 312, 5),
      sp("wide-b", "t1", "model", 600, 200),
      sp("nested", "t1", "tool", 650, 50),
      sp("other", "t2", "model", 0, 1000),
    ],
    0,
    1000,
    100,
  );
  assert.equal(l.spans, 7);
  assert.deepEqual(l.lanes.map((x) => x.rows), [2, 1]);
  assert.equal(l.items.length, 5);
  const at = (lane, start) => l.items.find((x) => x.lane === lane && x.start_ms === start);
  assert.equal(at(0, 0).span.id, "wide-a");
  const c = at(0, 300);
  assert.equal(c.span, undefined);
  assert.equal(c.count, 3);
  assert.equal(c.kind, "tool");
  assert.equal(c.duration_ms, 17);
  assert.deepEqual(c.kinds, { tool: 2, browser: 1 });
  assert.equal(at(0, 650).row, 1);
  assert.equal(at(1, 0).span.id, "other");
  // 10,000 tiny spans in one row collapse into one cluster; zoomed in they separate.
  const many = Array.from({ length: 10000 }, (_, i) => sp("s" + i, "t", "tool", i, 0.5));
  const collapsed = timelineLayout(many, 0, 10000, 200);
  assert.equal(collapsed.items.length, 1);
  assert.equal(collapsed.items[0].count, 10000);
  assert.equal(timelineLayout(many.slice(0, 3), 0, 3, 1000).items.length, 3);
});
test("timeline layout bounds lane rows with an overflow row, like the server", () => {
  const { timelineLayout } = require("./profile.js");
  const spans = Array.from({ length: 30 }, (_, i) => ({ id: "c" + i, task_id: "t", kind: "browser", name: "c", start_ms: i, duration_ms: 500 }));
  const l = timelineLayout(spans, 0, 1000, 100);
  assert.equal(l.lanes[0].rows, 12);
  const overflow = l.items.filter((x) => !x.span);
  assert.equal(l.items.length - overflow.length, 11);
  assert.equal(overflow.length, 1);
  assert.equal(overflow[0].count, 19);
  assert.equal(overflow[0].overflow, true);
  assert.equal(overflow[0].row, 11);
});
test("producer namespace metadata and usage detail survive import", () => {
  const p = fixture();
  p.schema_version = 2;
  p.configuration = { "asb.setting": "qa-rag", "tracelab.release": "v0.0.2", secret: "x" };
  p.tasks[0].attributes = { "asb.n_steps": 3, "tracelab.provider": "codex", prompt: "hidden" };
  p.spans[1].attributes = { "asb.category": "Sandbox", url: "http://x" };
  p.spans[1].usage = { input_tokens: 5, cache_read_tokens: 2, cost_usd: 0.1, raw: 1 };
  const v = validate(p);
  assert.deepEqual(v.configuration, { "asb.setting": "qa-rag", "tracelab.release": "v0.0.2" });
  assert.deepEqual(v.tasks[0].attributes, { "asb.n_steps": 3, "tracelab.provider": "codex" });
  assert.deepEqual(v.spans[1].attributes, { "asb.category": "Sandbox" });
  assert.deepEqual(v.spans[1].usage, { input_tokens: 5, cache_read_tokens: 2, cost_usd: 0.1 });
});
