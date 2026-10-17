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
