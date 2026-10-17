"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const P = require("./profile.js");
function fixture() {
  return {
    schema_version: 2,
    run_id: "synthetic-test",
    name: "Synthetic test",
    duration_ms: 100,
    tasks: [{ id: "t", start_ms: 0, duration_ms: 100, status: "succeeded" }],
    spans: [
      {
        id: "a",
        task_id: "t",
        agent_id: "coordinator",
        kind: "model",
        start_ms: 0,
        duration_ms: 10,
      },
      {
        id: "b",
        task_id: "t",
        agent_id: "worker-a",
        kind: "tool",
        start_ms: 10,
        duration_ms: 30,
      },
      {
        id: "c",
        task_id: "t",
        agent_id: "worker-b",
        kind: "tool",
        start_ms: 10,
        duration_ms: 20,
      },
      {
        id: "d",
        task_id: "t",
        agent_id: "coordinator",
        kind: "model",
        start_ms: 40,
        duration_ms: 10,
      },
    ],
    edges: [
      { source: "a", target: "b", kind: "dependency" },
      { source: "a", target: "c", kind: "spawn" },
      { source: "b", target: "d", kind: "join" },
      { source: "c", target: "d", kind: "join" },
    ],
    resources: [],
    warnings: [],
    capabilities: [],
  };
}
test("schema v2 preserves explicit multi-parent join and validates whole graph", () => {
  let p = P.validate(fixture());
  assert.equal(p.edges.filter((e) => e.target === "d").length, 2);
  p.edges.push({ source: "d", target: "a", kind: "return" });
  assert.throws(() => P.validate(p), /cycle/i);
});
test("v1 migrates containment only and rejects v2 precedence", () => {
  const p = fixture();
  p.schema_version = 1;
  p.edges = [];
  p.spans[1].parent_id = "a";
  assert.equal(P.validate(p).schema_version, 2);
  assert.deepEqual(p.edges, [{ source: "a", target: "b", kind: "parent" }]);
  const invalid = fixture();
  invalid.schema_version = 1;
  assert.throws(() => P.validate(invalid));
});
test("analysis uses interval union and explicit completion critical path", () => {
  const a = P.analyze(P.validate(fixture()));
  assert.equal(a.critical_path.status, "available");
  assert.equal(a.critical_path.duration_ms, 50);
  assert.deepEqual(a.critical_path.span_ids, ["a", "b", "d"]);
  assert.equal(a.max_concurrency, 2);
  assert.equal(a.coverage[0].observed_ms, 50);
  assert.equal(a.coverage[0].unattributed_ms, 50);
  assert.equal(a.coverage[0].status, "available");
});
test("overlap cannot establish completion critical path", () => {
  const p = P.validate(fixture());
  p.spans[3].start_ms = 30;
  assert.equal(P.analyze(p).critical_path.status, "unavailable");
  p.edges = [];
  assert.equal(P.analyze(p).critical_path.status, "unavailable");
});
test("model missing usage remains null and measured zero is known", () => {
  const p = P.validate(fixture());
  let a = P.analyze(p);
  assert.equal(a.usage.input_tokens, null);
  p.spans[0].usage = { input_tokens: 0 };
  p.spans[3].incomplete = true;
  p.spans[3].duration_ms = 0;
  p.spans[3].usage = { output_tokens: 12 };
  a = P.analyze(p);
  assert.equal(a.usage.input_tokens, 0);
  assert.equal(a.usage.input_known, 1);
  assert.equal(a.usage.output_tokens, 12);
  const k = a.kinds.find((k) => k.kind === "model");
  assert.equal(k.incomplete_count, 1);
  assert.equal(k.time_quality, "mixed");
  assert.equal(k.summed_ms, 10);
});
test("unknown task placement cannot imply observation coverage", () => {
  const p = P.validate(fixture());
  p.tasks[0].time_quality = "unknown";
  assert.equal(P.analyze(p).coverage[0].status, "unavailable");
});
test("nested work does not double-count parallelism", () => {
  const p = fixture();
  p.edges = [];
  p.spans = [
    { id: "outer", task_id: "t", kind: "tool", start_ms: 0, duration_ms: 50 },
    {
      id: "inner",
      task_id: "t",
      kind: "tool",
      parent_id: "outer",
      start_ms: 10,
      duration_ms: 20,
    },
  ];
  const a = P.analyze(P.validate(p));
  assert.equal(a.max_concurrency, 1);
  assert.equal(a.coverage[0].observed_ms, 50);
});
test("paging and filtering preserve every matching record with full analysis", () => {
  const p = P.validate(fixture());
  let ids = [];
  for (let offset = 0; offset < 4; offset += 2) {
    const result = P.page(p, { offset, limit: 2 });
    assert.equal(result.total_spans, 4);
    assert.equal(result.analysis.span_count, 4);
    ids.push(...result.profile.spans.map((s) => s.id));
    assert.equal(result.has_more, offset === 0);
  }
  assert.equal(new Set(ids).size, 4);
  const filtered = P.page(p, { kind: "tool", q: "worker", limit: 1 });
  assert.equal(filtered.total_spans, 2);
  assert.equal(filtered.has_more, true);
  assert.equal(filtered.profile.spans.length, 1);
  assert.equal(p.spans.length, 4);
});
test("normalization strips unknown config and span attributes", () => {
  const p = fixture();
  p.configuration = {
    model: "synthetic",
    concurrency: 2,
    api_key: "private",
    prompt: "private",
  };
  p.spans[0].attributes = {
    model: "synthetic",
    prompt: "private",
    command: "private",
  };
  const result = P.validate(p);
  assert.deepEqual(result.configuration, {
    model: "synthetic",
    concurrency: 2,
  });
  assert.deepEqual(result.spans[0].attributes, { model: "synthetic" });
  assert.ok(!JSON.stringify(result).includes("private"));
});
test("resource windows separate shared and task context and preserve missing readings", () => {
  const p = P.validate(fixture());
  p.spans[0].runtime_id = "h";
  p.resources = [
    { task_id: "t", runtime_id: "h", component: "harness", time_ms: 1 },
    {
      task_id: "t",
      runtime_id: "h",
      component: "harness",
      time_ms: 2,
      cpu_percent: 0,
      gpu_percent: 25,
    },
    {
      task_id: "t",
      runtime_id: "other",
      component: "harness",
      time_ms: 2,
      cpu_percent: 90,
    },
    { runtime_id: "shared", component: "bridge", time_ms: 3, cpu_percent: 5 },
  ];
  const r = P.windowStats(p, "a").resources;
  assert.equal(r.length, 2);
  const harness = r.find((x) => x.runtime_id === "h");
  assert.equal(harness.attribution, "runtime_context");
  assert.equal(harness.sample_count, 2);
  assert.equal(harness.cpu_percent.sample_count, 1);
  assert.equal(harness.cpu_percent.mean, 0);
  assert.equal(harness.gpu_percent.mean, 25);
  assert.equal(
    r.find((x) => x.runtime_id === "shared").attribution,
    "shared_context",
  );
  delete p.spans[0].runtime_id;
  assert.ok(
    P.windowStats(p, "a").resources.some(
      (x) => x.attribution === "task_context",
    ),
  );
});
test("independent resource overrides and explicit request values survive normalization", () => {
  const p = fixture();
  p.configuration = {
    harness_cpu_limit: 0,
    harness_memory_limit_mb: 2048,
    sandbox_cpu_limit: 4,
    sandbox_memory_limit_mb: 4096,
    temperature: 0,
    max_tokens: 512,
    context_length: 8192,
    reasoning_effort: "high",
  };
  const original = { ...p.configuration };
  assert.deepEqual(P.validate(p).configuration, original);
  assert.ok(!Object.hasOwn(p.configuration, "cpu_limit"));
});
test("explicit lifecycle precedence is critical evidence while umbrella is not observed work", () => {
  const p = fixture();
  p.spans = [
    {
      id: "prepare",
      task_id: "t",
      kind: "lifecycle",
      start_ms: 0,
      duration_ms: 10,
    },
    {
      id: "execute",
      task_id: "t",
      kind: "lifecycle",
      start_ms: 10,
      duration_ms: 70,
    },
    {
      id: "umbrella",
      task_id: "t",
      kind: "lifecycle",
      start_ms: 0,
      duration_ms: 100,
    },
  ];
  p.edges = [{ source: "prepare", target: "execute", kind: "dependency" }];
  const a = P.analyze(P.validate(p));
  assert.equal(a.critical_path.duration_ms, 80);
  assert.equal(a.coverage[0].observed_ms, 0);
});
