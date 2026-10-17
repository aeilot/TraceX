"use strict";
// Shared browser/Node logic. No DOM, backend dependency, or temporal inference.
(function (root) {
  const attributeKeys = new Set([
    "model",
    "gen_ai.request.model",
    "gen_ai.usage.input_tokens",
    "gen_ai.usage.output_tokens",
    "tool.name",
    "exit_code",
    "synthetic",
  ]);
  function maximum(values) {
    return values.reduce((a, b) => Math.max(a, b), 0);
  }
  function union(spans) {
    const intervals = spans
      .map((s) => [s.start_ms, s.start_ms + s.duration_ms])
      .sort((a, b) => a[0] - b[0]);
    let sum = 0,
      end = -Infinity;
    for (const [start, stop] of intervals) {
      sum += Math.max(0, stop - Math.max(start, end));
      end = Math.max(end, stop);
    }
    return sum;
  }
  function validate(p) {
    if (
      !p ||
      typeof p !== "object" ||
      Array.isArray(p) ||
      !Array.isArray(p.spans) ||
      !Array.isArray(p.tasks)
    )
      throw Error(
        "Expected a normalized TraceX profile with tasks and spans arrays. Use --run to load an ARIES directory.",
      );
    if (p.schema_version !== 1)
      throw Error("Unsupported profile schema_version; expected 1.");
    for (const key of ["run_id", "name"])
      if (p[key] != null && typeof p[key] !== "string")
        throw Error(`${key} must be a string.`);
    for (const key of ["edges", "resources", "capabilities", "warnings"])
      if (p[key] != null && !Array.isArray(p[key]))
        throw Error(`${key} must be an array.`);
    if (!Number.isFinite(p.duration_ms) || p.duration_ms < 0)
      throw Error("Profile duration_ms must be a non-negative number.");
    if (
      p.spans.length > 20000 ||
      (p.resources || []).length > 100000 ||
      p.tasks.length + p.spans.length + (p.resources || []).length > 100000
    )
      throw Error(
        "Profile exceeds the browser limit (20,000 spans / 100,000 total records).",
      );
    const ids = new Map(),
      taskIDs = new Set();
    const timing = (value, label) => {
      if (!value || typeof value !== "object") throw Error(`Invalid ${label}.`);
      for (const key of ["start_ms", "duration_ms"])
        if (!Number.isFinite(value[key]) || value[key] < 0)
          throw Error(`Invalid ${label} ${key}.`);
      if (!Number.isFinite(value.start_ms + value.duration_ms))
        throw Error(`Invalid ${label} end time.`);
    };
    for (const t of p.tasks) {
      timing(t, "task");
      if (typeof t.id !== "string" || !t.id || taskIDs.has(t.id))
        throw Error("Tasks require unique nonempty IDs.");
      taskIDs.add(t.id);
    }
    for (const s of p.spans) {
      timing(s, "span");
      if (typeof s.id !== "string" || !s.id || ids.has(s.id))
        throw Error("Spans require unique nonempty IDs.");
      ids.set(s.id, s);
      if (!taskIDs.has(s.task_id))
        throw Error("Span references an unknown task.");
      for (const key of [
        "name",
        "agent_id",
        "kind",
        "status",
        "source",
        "parent_id",
      ])
        if (s[key] != null && typeof s[key] !== "string")
          throw Error(`Span ${key} must be a string.`);
    }
    // Match the Go import contract: edges are exactly the explicit parent links.
    const es = p.spans
      .filter((s) => s.parent_id)
      .map((s) => ({ source: s.parent_id, target: s.id, kind: "parent" }));
    for (const e of p.edges || [])
      if (
        !e ||
        !ids.has(e.source) ||
        !ids.has(e.target) ||
        ids.get(e.target).parent_id !== e.source ||
        e.kind !== "parent"
      )
        throw Error("Edges must describe explicit parent relationships.");
    const degree = new Map([...ids.keys()].map((id) => [id, 0])),
      adj = new Map([...ids.keys()].map((id) => [id, []]));
    for (const e of es) {
      if (!ids.has(e.source)) throw Error("Span references an unknown parent.");
      degree.set(e.target, degree.get(e.target) + 1);
      adj.get(e.source).push(e.target);
    }
    const queue = [...ids.keys()].filter((id) => !degree.get(id));
    let seen = 0;
    for (let i = 0; i < queue.length; i++) {
      seen++;
      for (const id of adj.get(queue[i])) {
        degree.set(id, degree.get(id) - 1);
        if (!degree.get(id)) queue.push(id);
      }
    }
    if (seen !== ids.size) throw Error("Execution graph contains a cycle.");
    for (const r of p.resources || []) {
      if (!r || !Number.isFinite(r.time_ms) || r.time_ms < 0)
        throw Error("Invalid resource timestamp.");
      if (r.task_id && !taskIDs.has(r.task_id))
        throw Error("Resource references an unknown task.");
      for (const key of ["component", "runtime_id"])
        if (r[key] != null && typeof r[key] !== "string")
          throw Error(`Resource ${key} must be a string.`);
      for (const key of ["cpu_percent", "memory_bytes", "gpu_percent"])
        if (r[key] != null && (!Number.isFinite(r[key]) || r[key] < 0))
          throw Error(`Invalid ${key} measurement.`);
    }
    for (const c of p.capabilities || [])
      if (
        !c ||
        typeof c.name !== "string" ||
        typeof c.status !== "string" ||
        (c.reason != null && typeof c.reason !== "string")
      )
        throw Error("Invalid capability.");
    for (const w of p.warnings || [])
      if (typeof w !== "string") throw Error("Warnings must be strings.");
    // Names/IDs remain evidence. Only selected scalar metadata is retained.
    for (const s of p.spans)
      if (s.attributes) {
        const filtered = {};
        for (const [k, v] of Object.entries(s.attributes))
          if (
            attributeKeys.has(k) &&
            ((typeof v === "string" && v.length <= 256) ||
              (typeof v === "number" && Number.isFinite(v) && v >= 0) ||
              typeof v === "boolean")
          )
            filtered[k] = v;
        s.attributes = filtered;
      }
    // Match Go's typed decoding: unknown fields do not survive export.
    const retain = (value, keys) => {
      for (const key of Object.keys(value))
        if (!keys.includes(key)) delete value[key];
    };
    for (const t of p.tasks)
      retain(t, [
        "id",
        "name",
        "status",
        "start_ms",
        "duration_ms",
        "evaluation_status",
        "cleanup_status",
      ]);
    for (const s of p.spans)
      retain(s, [
        "id",
        "task_id",
        "agent_id",
        "parent_id",
        "name",
        "kind",
        "status",
        "start_ms",
        "duration_ms",
        "source",
        "attributes",
      ]);
    for (const r of p.resources || [])
      retain(r, [
        "task_id",
        "component",
        "runtime_id",
        "time_ms",
        "cpu_percent",
        "memory_bytes",
        "gpu_percent",
      ]);
    for (const c of p.capabilities || [])
      retain(c, ["name", "status", "reason"]);
    retain(p, [
      "schema_version",
      "run_id",
      "name",
      "duration_ms",
      "tasks",
      "spans",
      "edges",
      "resources",
      "capabilities",
      "warnings",
    ]);
    p.edges = es;
    p.resources = p.resources || [];
    p.capabilities = p.capabilities || [];
    p.warnings = p.warnings || [];
    return p;
  }
  const api = Object.freeze({ validate, union, maximum });
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.TraceXProfile = api;
})(globalThis);
