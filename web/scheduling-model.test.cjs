const { test } = require("node:test");
const assert = require("node:assert/strict");
const { schedule } = require("./scheduling-model.js");

// The same fixture and expectations as internal/profile/scheduling_test.go.
function fixture() {
  const task = (id, start, dur) => ({ id, logical_id: id, name: id, status: "succeeded", time_quality: "measured", start_ms: start, duration_ms: dur });
  const life = (task, phase, s, d) => ({ id: `${task}/${phase}`, task_id: task, name: phase, kind: "lifecycle", start_ms: s, duration_ms: d, status: "completed", attributes: { phase } });
  const work = (id, task, kind, s, d, status = "completed") => ({ id, task_id: task, name: kind, kind, start_ms: s, duration_ms: d, status });
  const tasks = [task("t-001", 0, 100), task("t-002", 100, 100), task("t-003", 200, 100), task("t-004", 300, 400), task("other-001", 0, 900)];
  const spans = tasks.map((t) => life(t.id, "task", t.start_ms, t.duration_ms));
  spans.push({ id: "t-004/queue", task_id: "t-004", name: "Queue", kind: "lifecycle", start_ms: 250, duration_ms: 50, status: "completed", attributes: { phase: "queue" } });
  spans.push(life("t-004", "execute", 300, 200));
  spans.push(work("m1", "t-004", "model", 310, 100), work("m2", "t-004", "model", 350, 100), work("x1", "t-004", "tool", 460, 20),
    work("p1", "t-004", "suspended", 480, 15), work("bad", "t-004", "tool", 600, 10, "failed"));
  const resources = [];
  for (let i = 0; i < 10; i++) {
    const t = 300 + i * 40, reset = i >= 5;
    resources.push(
      { task_id: "t-004", component: "sandbox", runtime_id: "s4", time_ms: t, cpu_percent: 50, memory_bytes: 2 ** 30,
        cpu_throttling: { periods: reset ? (i - 5) * 10 : 100 + i * 10, throttled_periods: reset ? (i - 5) * 5 : i * 5 } },
      { component: "bridge", runtime_id: "b", time_ms: t, cpu_percent: 10, memory_bytes: 2 ** 20 });
  }
  return { duration_ms: 1000, tasks, spans, resources };
}

test("lanes, queue, unknown time and work unions match the server projection", () => {
  const v = schedule(fixture(), { start_ms: 0, end_ms: 1000, bins: 100, width: 1000, pinned: ["t-004"] });
  assert.equal(v.lanes_total, 5);
  assert.equal(v.lanes[0].task_id, "t-004");
  assert.ok(v.queue_recorded);
  const row = v.impact.find((r) => r.task_id === "t-004");
  assert.equal(row.queue_ms, 50);
  assert.equal(row.unknown_ms, 200);
  assert.equal(row.model_ms, 140);
  assert.equal(row.tool_ms, 30);
  assert.equal(row.paused_ms, 15);
  assert.equal(row.failures, 1);
  assert.equal(row.evidence.failure, "bad");
  assert.ok(v.lanes[0].lifecycle.some((x) => x.kind === "unknown" && x.start_ms === 500 && x.duration_ms === 200));
});

test("slowdown pairs only occurrences of the same task", () => {
  const v = schedule(fixture(), { start_ms: 0, end_ms: 1000, bins: 100 });
  const rows = Object.fromEntries(v.impact.map((r) => [r.task_id, r]));
  assert.equal(rows["t-004"].slowdown, 4);
  assert.equal(rows["t-004"].baseline_n, 3);
  assert.equal(rows["other-001"].slowdown, null);
});

test("concurrency and resources keep missing values and shared runtimes distinct", () => {
  const v = schedule(fixture(), { start_ms: 0, end_ms: 1000, bins: 10 });
  const s = Object.fromEntries(v.series.map((x) => [x.name, x]));
  assert.equal(s["Admitted occurrences"].max[0], 2);
  assert.equal(s["Admitted occurrences"].mean[0], 2);
  assert.equal(s["Queued occurrences"].max[2], 1);
  assert.equal(s["Task runtime CPU"].mean[0], null);
  assert.equal(s["Task runtime CPU"].mean[3], 0.5);
  assert.ok(Math.abs(s["Shared runtime CPU"].mean[3] - 0.1) < 1e-9);
  for (const x of s["Throttled period share"].mean) if (x != null) assert.ok(x >= 0 && x <= 1);
  assert.ok(v.notes.join(" ").includes("host"));
  const unsampled = v.impact.find((r) => r.task_id === "t-001");
  assert.equal(unsampled.cpu_core_s, null);
  assert.equal(unsampled.resource_coverage, null);
});

test("a window clips intervals and counts every overlapping lane", () => {
  const v = schedule(fixture(), { start_ms: 320, end_ms: 340, bins: 4, lanes: 1 });
  assert.equal(v.lanes_total, 2);
  assert.equal(v.lanes.length, 1);
  for (const r of v.impact) assert.ok(r.overlap_ms <= 20 && r.model_ms <= 20);
});
