"use strict";
// The browser import contract matches the Go model; all analysis uses recorded intervals.
(function (root) {
  const attributeKeys = new Set([
    "model",
    "gen_ai.request.model",
    "gen_ai.usage.input_tokens",
    "gen_ai.usage.output_tokens",
    "tool.name",
    "exit_code",
    "synthetic",
    "phase",
    "time_quality",
    "backend",
    "harness",
    "resource_type",
    "http_status",
    "page",
    "agent.id",
    "provider",
    "bridge_role",
  ]);
  const taskFlags = new Set(["duration_outlier", "sparse_monitor"]);
  const networkKeys = new Set([
    "requests",
    "failed",
    "unsent",
    "response_bytes",
    "active_ms",
    "navigations",
  ]);
  const resourceKeys = [
    "cpu_percent",
    "memory_bytes",
    "gpu_percent",
    "network_rx_bytes",
    "network_tx_bytes",
    "pids",
    "memory_limit_bytes",
    "memory_peak_bytes",
  ];
  const configKeys = new Set([
    "benchmark",
    "harness",
    "model",
    "provider",
    "concurrency",
    "cpu_limit",
    "memory_limit_mb",
    "harness_cpu_limit",
    "harness_memory_limit_mb",
    "sandbox_cpu_limit",
    "sandbox_memory_limit_mb",
    "temperature",
    "max_tokens",
    "context_length",
    "reasoning_effort",
    "agent_timeout_seconds",
    "sandbox_shm_size_mb",
    "task_count",
    "harness_image",
    "sandbox_image",
  ]);
  const edgeKinds = new Set([
    "parent",
    "spawn",
    "call",
    "return",
    "dependency",
    "join",
  ]);
  const maximum = (values) => values.reduce((a, b) => Math.max(a, b), 0);
  function union(spans) {
    const xs = spans
      .map((s) => [s.start_ms, s.start_ms + s.duration_ms])
      .sort((a, b) => a[0] - b[0]);
    let sum = 0,
      end = -Infinity;
    for (const [a, b] of xs) {
      sum += Math.max(0, b - Math.max(a, end));
      end = Math.max(end, b);
    }
    return sum;
  }
  function scalars(value, keys) {
    const out = {};
    if (!value || typeof value !== "object") return out;
    for (const [k, v] of Object.entries(value))
      if (
        keys.has(k) &&
        ((typeof v === "string" && v.length <= 256) ||
          (typeof v === "number" && Number.isFinite(v) && v >= 0) ||
          typeof v === "boolean")
      )
        out[k] = v;
    return out;
  }
  function retain(value, keys) {
    for (const k of Object.keys(value)) if (!keys.includes(k)) delete value[k];
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
        "Expected normalized TraceX JSON with tasks and spans arrays. Use --run to open an ARIES directory.",
      );
    if (p.schema_version !== 1 && p.schema_version !== 2)
      throw Error("Unsupported profile schema_version; expected 1 or 2.");
    const version = p.schema_version;
    for (const key of ["run_id", "name"])
      if (p[key] != null && typeof p[key] !== "string")
        throw Error(`${key} must be a string.`);
    for (const key of ["edges", "resources", "capabilities", "warnings"])
      if (p[key] != null && !Array.isArray(p[key]))
        throw Error(`${key} must be an array.`);
    if (!Number.isFinite(p.duration_ms) || p.duration_ms < 0)
      throw Error("Invalid run duration.");
    if (
      p.tasks.length +
        p.spans.length +
        (p.resources || []).length +
        (p.edges || []).length >
      1000000
    )
      throw Error(
        "Profile exceeds one million records; open the source through --run.",
      );
    const ids = new Map(),
      taskIDs = new Set();
    const timing = (v, label) => {
      if (!v || typeof v !== "object") throw Error(`Invalid ${label}.`);
      for (const k of ["start_ms", "duration_ms"])
        if (!Number.isFinite(v[k]) || v[k] < 0)
          throw Error(`Invalid ${label} ${k}.`);
      if (!Number.isFinite(v.start_ms + v.duration_ms))
        throw Error(`Invalid ${label} end.`);
    };
    for (const t of p.tasks) {
      timing(t, "task");
      if (typeof t.id !== "string" || !t.id || taskIDs.has(t.id))
        throw Error("Tasks require unique IDs.");
      taskIDs.add(t.id);
      if (
        t.score != null &&
        (!Number.isFinite(t.score) || t.score < 0 || t.score > 1)
      )
        throw Error("Invalid task score.");
      if (t.end_reason != null && !/^[a-z0-9_-]{1,64}$/.test(t.end_reason))
        delete t.end_reason;
      if (t.network != null) {
        if (typeof t.network !== "object") throw Error("Invalid task network.");
        const types = scalars(t.network.by_resource_type, {
          has: (k) => /^[a-z]{1,20}$/.test(k),
        });
        t.network = scalars(t.network, networkKeys);
        if (Object.keys(types).length) t.network.by_resource_type = types;
      }
      if (t.flags != null)
        t.flags = Array.isArray(t.flags)
          ? t.flags.filter((f) => taskFlags.has(f))
          : [];
    }
    for (const s of p.spans) {
      timing(s, "span");
      if (typeof s.id !== "string" || !s.id || ids.has(s.id))
        throw Error("Spans require unique IDs.");
      ids.set(s.id, s);
      if (!taskIDs.has(s.task_id))
        throw Error("Span references an unknown task.");
      for (const k of [
        "name",
        "agent_id",
        "kind",
        "status",
        "source",
        "parent_id",
        "runtime_id",
      ])
        if (s[k] != null && typeof s[k] !== "string")
          throw Error(`Span ${k} must be a string.`);
      if (s.incomplete != null && typeof s.incomplete !== "boolean")
        throw Error("Invalid incomplete flag.");
      if (s.usage) {
        for (const k of ["input_tokens", "output_tokens"])
          if (
            s.usage[k] != null &&
            (!Number.isFinite(s.usage[k]) || s.usage[k] < 0)
          )
            throw Error("Invalid token usage.");
        retain(s.usage, ["input_tokens", "output_tokens"]);
      }
    }
    const es = [],
      seen = new Set();
    const add = (e) => {
      if (
        !e ||
        !ids.has(e.source) ||
        !ids.has(e.target) ||
        !edgeKinds.has(e.kind)
      )
        throw Error("Invalid explicit edge relationship.");
      if (
        (version === 1 || e.kind === "parent") &&
        ids.get(e.target).parent_id !== e.source
      )
        throw Error("Edges must describe explicit parent relationships.");
      if (version === 1 && e.kind !== "parent")
        throw Error("Schema v1 supports parent edges only.");
      const key = e.source + "\0" + e.target + "\0" + e.kind;
      if (!seen.has(key)) {
        es.push({ source: e.source, target: e.target, kind: e.kind });
        seen.add(key);
      }
    };
    for (const e of p.edges || []) add(e);
    for (const s of p.spans)
      if (s.parent_id)
        add({ source: s.parent_id, target: s.id, kind: "parent" });
    const degree = new Map([...ids.keys()].map((id) => [id, 0])),
      adj = new Map([...ids.keys()].map((id) => [id, []]));
    for (const e of es) {
      degree.set(e.target, degree.get(e.target) + 1);
      adj.get(e.source).push(e.target);
    }
    const queue = [...ids.keys()].filter((id) => !degree.get(id));
    let count = 0;
    for (let i = 0; i < queue.length; i++) {
      count++;
      for (const id of adj.get(queue[i])) {
        degree.set(id, degree.get(id) - 1);
        if (!degree.get(id)) queue.push(id);
      }
    }
    if (count !== ids.size) throw Error("Execution graph contains a cycle.");
    for (const r of p.resources || []) {
      if (!r || !Number.isFinite(r.time_ms) || r.time_ms < 0)
        throw Error("Invalid resource timestamp.");
      if (r.task_id && !taskIDs.has(r.task_id))
        throw Error("Resource references unknown task.");
      for (const k of ["component", "runtime_id"])
        if (r[k] != null && typeof r[k] !== "string")
          throw Error("Invalid resource identity.");
      for (const k of resourceKeys)
        if (r[k] != null && (!Number.isFinite(r[k]) || r[k] < 0))
          throw Error(`Invalid ${k} reading.`);
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
    for (const t of p.tasks)
      retain(t, [
        "id",
        "logical_id",
        "name",
        "status",
        "start_ms",
        "duration_ms",
        "time_quality",
        "evaluation_status",
        "cleanup_status",
        "score",
        "end_reason",
        "network",
        "flags",
      ]);
    for (const s of p.spans) {
      s.attributes = scalars(s.attributes, attributeKeys);
      retain(s, [
        "id",
        "task_id",
        "agent_id",
        "parent_id",
        "runtime_id",
        "name",
        "kind",
        "status",
        "start_ms",
        "duration_ms",
        "source",
        "attributes",
        "usage",
        "incomplete",
      ]);
    }
    for (const r of p.resources || [])
      retain(r, [
        "task_id",
        "component",
        "runtime_id",
        "time_ms",
        ...resourceKeys,
      ]);
    for (const c of p.capabilities || [])
      retain(c, ["name", "status", "reason"]);
    p.configuration = scalars(p.configuration, configKeys);
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
      "configuration",
    ]);
    p.schema_version = 2;
    p.edges = es;
    p.resources = p.resources || [];
    p.capabilities = p.capabilities || [];
    p.warnings = p.warnings || [];
    return p;
  }
  const incomplete = (s) =>
    !!s.incomplete || s.attributes?.time_quality === "completion_only";
  const work = (s) =>
    !["agent", "lifecycle"].includes(s.kind) && !incomplete(s);
  function critical(p) {
    const unavailable = {
      status: "unavailable",
      reason:
        "No recorded finish-before-start dependency, join, or return relationships.",
      span_ids: [],
      duration_ms: 0,
    };
    const spans = new Map(p.spans.map((s) => [s.id, s])),
      es = p.edges.filter((e) =>
        ["dependency", "join", "return"].includes(e.kind),
      );
    if (!es.length) return unavailable;
    const adj = new Map(),
      degree = new Map(),
      dist = new Map(),
      prev = new Map();
    for (const e of es) {
      const a = spans.get(e.source),
        b = spans.get(e.target);
      if (
        !a ||
        !b ||
        incomplete(a) ||
        incomplete(b) ||
        a.start_ms + a.duration_ms > b.start_ms
      )
        return {
          ...unavailable,
          reason:
            "Recorded precedence contains overlapping or incomplete spans; a completion critical path is unavailable.",
        };
      if (!adj.has(e.source)) adj.set(e.source, []);
      adj.get(e.source).push(e.target);
      degree.set(e.target, (degree.get(e.target) || 0) + 1);
      if (!degree.has(e.source)) degree.set(e.source, 0);
    }
    for (const xs of adj.values()) xs.sort();
    const q = [...degree]
      .filter(([, d]) => !d)
      .map(([id]) => id)
      .sort();
    for (const id of degree.keys()) dist.set(id, spans.get(id).duration_ms);
    let best = "";
    for (let i = 0; i < q.length; i++) {
      const id = q[i];
      if (!best || dist.get(id) > dist.get(best)) best = id;
      for (const next of adj.get(id) || []) {
        const v = dist.get(id) + spans.get(next).duration_ms;
        if (v > dist.get(next)) {
          dist.set(next, v);
          prev.set(next, id);
        }
        degree.set(next, degree.get(next) - 1);
        if (!degree.get(next)) q.push(next);
      }
    }
    if (q.length !== degree.size)
      return { ...unavailable, reason: "Recorded precedence is cyclic." };
    if (!best) return unavailable;
    const path = [];
    for (let id = best; id; id = prev.get(id)) path.unshift(id);
    return {
      status: "available",
      reason:
        "Longest sum of recorded span durations along finish-before-start precedence; excludes unrecorded waits and containment.",
      span_ids: path,
      duration_ms: dist.get(best),
    };
  }
  function subtract(base, nested) {
    const out = [];
    let cursor = base.start_ms,
      end = cursor + base.duration_ms;
    for (const s of [...nested].sort((a, b) => a.start_ms - b.start_ms)) {
      const finish = s.start_ms + s.duration_ms;
      if (finish <= cursor || s.start_ms >= end) continue;
      if (s.start_ms > cursor) out.push([cursor, Math.min(s.start_ms, end)]);
      cursor = Math.max(cursor, finish);
      if (cursor >= end) break;
    }
    if (cursor < end) out.push([cursor, end]);
    return out;
  }
  function analyze(p) {
    const kinds = new Map(),
      usage = {
        input_tokens: null,
        output_tokens: null,
        model_calls: 0,
        input_known: 0,
        output_known: 0,
      };
    const windows = new Map(),
      points = [],
      byID = new Map(p.spans.map((s) => [s.id, s])),
      parentCoverage = new Map();
    for (const s of p.spans) {
      if (!work(s)) continue;
      const seen = new Set();
      for (let id = s.parent_id; id && !seen.has(id); ) {
        seen.add(id);
        const parent = byID.get(id);
        if (!parent) break;
        if (work(parent)) {
          if (!parentCoverage.has(id)) parentCoverage.set(id, []);
          parentCoverage.get(id).push(s);
        }
        id = parent.parent_id;
      }
    }
    for (const s of p.spans) {
      if (!kinds.has(s.kind)) kinds.set(s.kind, []);
      kinds.get(s.kind).push(s);
      if (work(s)) {
        if (!windows.has(s.task_id)) windows.set(s.task_id, []);
        windows.get(s.task_id).push(s);
        for (const [start, end] of subtract(s, parentCoverage.get(s.id) || []))
          points.push([start, 1], [end, -1]);
      }
      if (s.kind === "model") {
        usage.model_calls++;
        for (const [k, n] of [
          ["input_tokens", "input_known"],
          ["output_tokens", "output_known"],
        ]) {
          const v = s.usage?.[k] ?? s.attributes?.["gen_ai.usage." + k];
          if (Number.isFinite(v)) {
            usage[k] = (usage[k] ?? 0) + v;
            usage[n]++;
          }
        }
      }
    }
    const coverage = p.tasks.map((t) => {
      if (["unknown", "completion_only"].includes(t.time_quality))
        return {
          task_id: t.id,
          status: "unavailable",
          time_quality: t.time_quality,
          observed_ms: 0,
          unattributed_ms: 0,
          ratio: 0,
        };
      const xs = (windows.get(t.id) || []).map((s) => ({
        start_ms: Math.max(s.start_ms, t.start_ms),
        duration_ms: Math.max(
          0,
          Math.min(s.start_ms + s.duration_ms, t.start_ms + t.duration_ms) -
            Math.max(s.start_ms, t.start_ms),
        ),
      }));
      const observed = union(xs);
      return {
        task_id: t.id,
        status: "available",
        time_quality: t.time_quality,
        observed_ms: observed,
        unattributed_ms: Math.max(0, t.duration_ms - observed),
        ratio: t.duration_ms ? observed / t.duration_ms : 0,
      };
    });
    let active = 0,
      max = 0;
    points.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
    for (const [, d] of points) {
      active += d;
      max = Math.max(max, active);
    }
    const success = (t) =>
        ["succeeded", "success", "completed"].includes(t.status),
      failure = (t) => ["failed", "canceled"].includes(t.status);
    const ds = p.tasks
        .filter((t) => success(t) || failure(t))
        .map((t) => t.duration_ms)
        .sort((a, b) => a - b),
      quantile = (q) => {
        if (!ds.length) return 0;
        const i = (ds.length - 1) * q;
        return (
          ds[Math.floor(i)] +
          (ds[Math.ceil(i)] - ds[Math.floor(i)]) * (i - Math.floor(i))
        );
      };
    const succeeded = p.tasks.filter(success).length,
      failed = p.tasks.filter(failure).length,
      scores = p.tasks.map((t) => t.score).filter(Number.isFinite);
    return {
      agent_count: new Set(p.spans.map((s) => s.agent_id).filter(Boolean)).size,
      span_count: p.spans.length,
      task_count: p.tasks.length,
      task_succeeded: succeeded,
      task_failed: failed,
      resources_count: p.resources.length,
      duration_ms: p.duration_ms,
      kinds: [...kinds]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([kind, xs]) => {
          const count = xs.filter(incomplete).length,
            measured = xs.filter((s) => !incomplete(s));
          return {
            kind,
            count: xs.length,
            union_ms: union(measured),
            summed_ms: measured.reduce((n, s) => n + s.duration_ms, 0),
            incomplete_count: count,
            time_quality:
              count === xs.length
                ? "completion_only"
                : count
                  ? "mixed"
                  : "measured",
          };
        }),
      coverage,
      max_concurrency: max,
      usage,
      task_p50_ms: quantile(0.5),
      task_p95_ms: quantile(0.95),
      throughput_per_second: p.duration_ms
        ? ((succeeded + failed) * 1000) / p.duration_ms
        : 0,
      critical_path: critical(p),
      score_mean: scores.length
        ? scores.reduce((a, b) => a + b, 0) / scores.length
        : null,
      score_known: scores.length,
    };
  }
  function windowStats(p, id) {
    const s = p.spans.find((s) => s.id === id);
    if (!s) return null;
    const groups = new Map();
    for (const r of p.resources) {
      if (r.time_ms < s.start_ms || r.time_ms > s.start_ms + s.duration_ms)
        continue;
      if (r.task_id && r.task_id !== s.task_id) continue;
      if (r.task_id && s.runtime_id && r.runtime_id !== s.runtime_id) continue;
      const k = r.runtime_id + "\0" + r.component;
      if (!groups.has(k)) groups.set(k, []);
      groups.get(k).push(r);
    }
    const stats = (xs) => {
      const v = xs.filter(Number.isFinite);
      return {
        sample_count: v.length,
        min: v.length ? v.reduce((a, b) => Math.min(a, b), Infinity) : null,
        max: v.length ? maximum(v) : null,
        mean: v.length ? v.reduce((a, b) => a + b, 0) / v.length : null,
      };
    };
    return {
      span_id: id,
      start_ms: s.start_ms,
      end_ms: s.start_ms + s.duration_ms,
      resources: [...groups]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([, rs]) => ({
          runtime_id: rs[0].runtime_id,
          component: rs[0].component,
          attribution: !rs[0].task_id
            ? "shared_context"
            : s.runtime_id
              ? "runtime_context"
              : "task_context",
          sample_count: rs.length,
          cpu_percent: stats(rs.map((r) => r.cpu_percent)),
          memory_bytes: stats(rs.map((r) => r.memory_bytes)),
          gpu_percent: stats(rs.map((r) => r.gpu_percent)),
        })),
    };
  }
  function page(p, opts = {}) {
    const xs = p.spans
      .filter(
        (s) =>
          (!opts.task || opts.task === "all" || s.task_id === opts.task) &&
          (!opts.kind || opts.kind === "all" || s.kind === opts.kind) &&
          (!opts.q ||
            [s.id, s.name, s.task_id, s.agent_id]
              .join(" ")
              .toLowerCase()
              .includes(opts.q.toLowerCase())) &&
          (opts.start_ms == null ||
            s.start_ms + s.duration_ms >= opts.start_ms) &&
          (opts.end_ms == null || s.start_ms <= opts.end_ms),
      )
      .sort((a, b) => a.start_ms - b.start_ms || a.id.localeCompare(b.id));
    const offset = opts.offset || 0,
      limit = opts.limit || 250,
      spans = xs.slice(offset, offset + limit),
      ids = new Set(spans.map((s) => s.id));
    const es = p.edges.filter((e) => ids.has(e.source) && ids.has(e.target));
    return {
      profile: {
        ...p,
        spans,
        edges: es,
        resources: p.resources.filter(
          (r) =>
            (!opts.task ||
              opts.task === "all" ||
              !r.task_id ||
              r.task_id === opts.task) &&
            (!opts.runtime ||
              opts.runtime === "all" ||
              r.runtime_id === opts.runtime) &&
            (opts.start_ms == null || r.time_ms >= opts.start_ms) &&
            (opts.end_ms == null || r.time_ms <= opts.end_ms),
        ),
      },
      total_spans: xs.length,
      offset,
      limit,
      has_more: offset + limit < xs.length,
      hidden_edges: p.edges.filter(
        (e) => ids.has(e.source) !== ids.has(e.target),
      ).length,
      analysis: analyze(p),
    };
  }
  const api = Object.freeze({
    validate,
    union,
    maximum,
    analyze,
    windowStats,
    page,
  });
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.TraceXProfile = api;
})(globalThis);
