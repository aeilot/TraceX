/* Scheduling projection for browser imports: a port of profile.Schedule. */
(function (root) {
  "use strict";
  const occurrenceSuffix = /-\d{3,}$/;
  const failed = (s) => ["failed", "ERROR", "error", "canceled"].includes(s);
  const sameTask = (t) => (t.logical_id || t.id).replace(occurrenceSuffix, "");
  function clip(s, e, lo, hi) {
    s = Math.max(s, lo);
    e = Math.min(e, hi);
    return e > s ? [s, e] : null;
  }
  function union(xs, gap) {
    xs = [...xs].sort((a, b) => a.start_ms - b.start_ms);
    const out = [];
    for (const x of xs) {
      const last = out[out.length - 1];
      if (last && last.kind === x.kind && x.start_ms <= last.start_ms + last.duration_ms + gap) {
        last.duration_ms = Math.max(last.start_ms + last.duration_ms, x.start_ms + x.duration_ms) - last.start_ms;
        last.count += x.count;
      } else out.push({ ...x });
    }
    return out;
  }
  const total = (xs) => xs.reduce((a, x) => a + x.duration_ms, 0);
  function subtractOne(xs, cut) {
    const out = [], cs = cut.start_ms, ce = cut.start_ms + cut.duration_ms;
    for (const x of xs) {
      const s = x.start_ms, e = x.start_ms + x.duration_ms;
      if (ce <= s || cs >= e) { out.push(x); continue; }
      if (cs > s) out.push({ ...x, duration_ms: cs - s });
      if (ce < e) out.push({ ...x, start_ms: ce, duration_ms: e - ce });
    }
    return out;
  }
  function median(xs) {
    const s = [...xs].sort((a, b) => a - b), k = (s.length - 1) * 0.5, i = Math.floor(k);
    return s[i] + (s[Math.min(i + 1, s.length - 1)] - s[i]) * (k - i);
  }
  function concurrency(name, xs, lo, binMS, bins) {
    const sum = new Array(bins).fill(0), peak = new Array(bins).fill(0), events = [];
    for (const x of xs) {
      events.push([x.start_ms, 1], [x.start_ms + x.duration_ms, -1]);
      for (let b = Math.floor((x.start_ms - lo) / binMS); b < bins && b >= 0; b++) {
        const c = clip(x.start_ms, x.start_ms + x.duration_ms, lo + b * binMS, lo + (b + 1) * binMS);
        if (c) sum[b] += (c[1] - c[0]) / binMS;
        else if (x.start_ms + x.duration_ms <= lo + b * binMS) break;
      }
    }
    events.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
    let level = 0;
    events.forEach(([t, d], i) => {
      level += d;
      const next = i + 1 < events.length ? events[i + 1][0] : t;
      if (next <= t) return;
      for (let b = Math.floor(Math.max(0, (t - lo) / binMS)); b < bins && lo + b * binMS < next; b++) peak[b] = Math.max(peak[b], level);
    });
    return { name, unit: "count", group: "concurrency", mean: sum, max: peak };
  }

  function schedule(p, q = {}) {
    let lo = q.start_ms ?? 0, hi = q.end_ms ?? p.duration_ms;
    if (!(hi > lo)) hi = Math.max(lo + 1, p.duration_ms);
    const bins = q.bins > 0 && q.bins <= 2000 ? q.bins : 400, width = q.width > 0 ? q.width : 1000;
    const gap = (hi - lo) / width, binMS = (hi - lo) / bins, pinned = new Set(q.pinned || []);
    const view = { start_ms: lo, end_ms: hi, bin_ms: binMS, lanes: [], lanes_total: 0, series: [], impact: [], queue_recorded: false, host_recorded: false,
      notes: ["Lanes are task occurrences: traces record no agent instance identity."] };
    const byTask = new Map();
    for (const s of p.spans) if (s.task_id) (byTask.get(s.task_id) || byTask.set(s.task_id, []).get(s.task_id)).push(s);
    const completed = new Map();
    for (const t of p.tasks)
      if (!failed(t.status) && t.status !== "running" && t.time_quality === "measured" && t.duration_ms > 0)
        (completed.get(sameTask(t)) || completed.set(sameTask(t), []).get(sameTask(t))).push(t);
    const tasks = [];
    for (const t of p.tasks) {
      const spans = byTask.get(t.id) || [];
      let start = t.start_ms, end = t.start_ms + t.duration_ms, envelope = false;
      for (const s of spans) if (s.kind === "lifecycle" && s.attributes?.phase === "task") { start = s.start_ms; end = s.start_ms + s.duration_ms; envelope = true; }
      for (const s of spans) if (s.kind === "lifecycle" && s.attributes?.phase === "queue") start = Math.min(start, s.start_ms);
      if (!clip(start, end, lo, hi) && t.time_quality !== "open") continue;
      const pt = { lane: { task_id: t.id, status: t.status, start_ms: t.start_ms, duration_ms: t.duration_ms, pinned: pinned.has(t.id), lifecycle: [], work: [] },
        queue: [], model: [], tool: [], paused: [], active: [] };
      const phases = [];
      for (const s of spans) {
        const c = clip(s.start_ms, s.start_ms + s.duration_ms, lo, hi);
        if (s.kind === "lifecycle") {
          let phase = s.attributes?.phase;
          if (phase === "task") continue;
          if (s.name === "Queue") phase = "queue";
          phase = phase || s.name;
          if (c) {
            const x = { start_ms: c[0], duration_ms: c[1] - c[0], kind: phase, count: 1, span_id: s.id };
            phases.push(x);
            if (phase === "queue") { pt.queue.push(x); view.queue_recorded = true; }
          }
          continue;
        }
        const kind = ["model", "tool", "suspended"].includes(s.kind) ? s.kind : "";
        if (!kind || !c) continue;
        const x = { start_ms: c[0], duration_ms: c[1] - c[0], kind, count: 1, span_id: s.id };
        ({ model: pt.model, tool: pt.tool, suspended: pt.paused })[kind].push(x);
      }
      const unknown = [];
      const w = clip(start, end, lo, hi);
      if (w && (envelope || t.time_quality === "measured" || t.time_quality === "open")) {
        let cursor = w[0];
        for (const c of union(phases, 0)) {
          if (c.start_ms > cursor) unknown.push({ start_ms: cursor, duration_ms: Math.min(c.start_ms, w[1]) - cursor, kind: "unknown", count: 1 });
          cursor = Math.max(cursor, c.start_ms + c.duration_ms);
        }
        if (w[1] > cursor) unknown.push({ start_ms: cursor, duration_ms: w[1] - cursor, kind: "unknown", count: 1 });
        pt.active = [{ start_ms: w[0], duration_ms: w[1] - w[0], kind: "active", count: 1 }];
        for (const x of union(pt.queue, 0)) pt.active = subtractOne(pt.active, x);
      }
      pt.model = union(pt.model, 0);
      pt.tool = union(pt.tool, 0);
      pt.paused = union(pt.paused, 0);
      pt.lane.lifecycle = union([...phases, ...unknown], gap);
      pt.lane.work = [...union(pt.model, gap), ...union(pt.tool, gap), ...union(pt.paused, gap)];
      tasks.push(pt);
      const row = { task_id: t.id, status: t.status, score: t.score, overlap_ms: w ? w[1] - w[0] : 0, queue_ms: null,
        model_ms: total(pt.model), tool_ms: total(pt.tool), paused_ms: total(pt.paused), unknown_ms: null,
        cpu_core_s: null, resource_coverage: null, throttled_share: null, failures: 0,
        duration_ms: null, baseline_ms: null, baseline_n: 0, slowdown: null, evidence: {} };
      if (pt.queue.length) { row.queue_ms = total(union(pt.queue, 0)); row.evidence.queue = pt.queue[0].span_id; }
      if (unknown.length || phases.length) row.unknown_ms = total(unknown);
      for (const [key, xs] of [["model", pt.model], ["tool", pt.tool], ["paused", pt.paused]]) if (xs.length) row.evidence[key] = xs[0].span_id;
      for (const s of spans)
        if (failed(s.status) && clip(s.start_ms, s.start_ms + Math.max(s.duration_ms, 0.001), lo, hi)) {
          row.failures++;
          row.evidence.failure ??= s.id;
        }
      if (t.time_quality === "measured" && t.duration_ms > 0) {
        row.duration_ms = t.duration_ms;
        const others = (completed.get(sameTask(t)) || []).filter((o) => o.id !== t.id).map((o) => o.duration_ms);
        row.baseline_n = others.length;
        if (others.length >= 3 && !failed(t.status)) {
          row.baseline_ms = median(others);
          row.slowdown = t.duration_ms / row.baseline_ms;
        }
      }
      view.impact.push(row);
    }
    tasks.sort((a, b) => (a.lane.pinned !== b.lane.pinned ? (a.lane.pinned ? -1 : 1) : a.lane.start_ms - b.lane.start_ms));
    view.lanes_total = tasks.length;
    view.lanes = tasks.slice(0, q.lanes > 0 ? q.lanes : 200).map((t) => t.lane);
    const pick = (f) => tasks.flatMap(f);
    view.series.push(
      concurrency("Admitted occurrences", pick((t) => t.active), lo, binMS, bins),
      concurrency("Occurrences in a model call", pick((t) => t.model), lo, binMS, bins),
      concurrency("Occurrences in a tool call", pick((t) => t.tool), lo, binMS, bins),
      concurrency("Paused sandboxes", pick((t) => t.paused), lo, binMS, bins));
    if (view.queue_recorded) view.series.push(concurrency("Queued occurrences", pick((t) => t.queue), lo, binMS, bins));
    else view.notes.push("No admission queue spans are recorded; queue depth is unknown, not zero.");
    resources(view, p, lo, hi, binMS, bins);
    return view;
  }

  function resources(view, p, lo, hi, binMS, bins) {
    const runtimes = new Map(), impact = new Map(view.impact.map((r) => [r.task_id, r]));
    for (const r of p.resources || []) {
      const key = `${r.task_id || ""}\0${r.component}\0${r.runtime_id}`;
      (runtimes.get(key) || runtimes.set(key, []).get(key)).push(r);
    }
    const acc = () => ({ sum: new Array(bins).fill(0), seen: new Array(bins).fill(0) });
    const groups = { task: { cpu: acc(), memory: acc() }, shared: { cpu: acc(), memory: acc() } };
    const throttled = new Array(bins).fill(0), periods = new Array(bins).fill(0), perTask = new Map();
    for (const rs of runtimes.values()) {
      rs.sort((a, b) => a.time_ms - b.time_ms);
      const g = groups[rs[0].task_id ? "task" : "shared"];
      const cpu = new Array(bins).fill(0), cpuN = new Array(bins).fill(0), mem = new Array(bins).fill(0), memN = new Array(bins).fill(0);
      rs.forEach((r, k) => {
        if (r.time_ms < lo || r.time_ms > hi) return;
        const b = Math.min(bins - 1, Math.floor((r.time_ms - lo) / binMS));
        if (Number.isFinite(r.cpu_percent)) { cpu[b] += r.cpu_percent / 100; cpuN[b]++; }
        if (Number.isFinite(r.memory_bytes)) { mem[b] += r.memory_bytes; memN[b]++; }
        const prev = rs[k - 1];
        const dp = prev && r.cpu_throttling && prev.cpu_throttling ? r.cpu_throttling.periods - prev.cpu_throttling.periods : 0;
        const dt = prev && r.cpu_throttling && prev.cpu_throttling ? r.cpu_throttling.throttled_periods - prev.cpu_throttling.throttled_periods : -1;
        if (dp > 0 && dt >= 0) { periods[b] += dp; throttled[b] += dt; }
        if (r.task_id && impact.has(r.task_id) && prev) {
          const tr = perTask.get(r.task_id) || perTask.set(r.task_id, { core: 0, covered: 0, thr: 0, per: 0 }).get(r.task_id);
          const step = r.time_ms - prev.time_ms;
          if (step > 0 && step < 10000) {
            tr.covered += step;
            tr.sampled = true;
            if (Number.isFinite(r.cpu_percent)) tr.core += (r.cpu_percent / 100) * step / 1000;
          }
          if (dp > 0 && dt >= 0) { tr.per += dp; tr.thr += dt; }
        }
      });
      for (let b = 0; b < bins; b++) {
        if (cpuN[b]) { g.cpu.sum[b] += cpu[b] / cpuN[b]; g.cpu.seen[b]++; }
        if (memN[b]) { g.memory.sum[b] += mem[b] / memN[b]; g.memory.seen[b]++; }
      }
    }
    const add = (name, unit, a) => {
      if (!a.seen.some(Boolean)) return;
      const values = a.sum.map((v, b) => (a.seen[b] ? v : null));
      view.series.push({ name, unit, group: "resources", mean: values, max: values, coverage: a.seen });
    };
    add("Task runtime CPU", "cores", groups.task.cpu);
    add("Shared runtime CPU", "cores", groups.shared.cpu);
    add("Task runtime memory", "bytes", groups.task.memory);
    add("Shared runtime memory", "bytes", groups.shared.memory);
    if (periods.some((x) => x > 0)) {
      const share = periods.map((x, b) => (x > 0 ? throttled[b] / x : null));
      view.series.push({ name: "Throttled period share", unit: "share", group: "resources", mean: share, max: share });
    }
    for (const [id, tr] of perTask) {
      const row = impact.get(id);
      if (!tr.sampled || row.overlap_ms <= 0) continue;
      row.cpu_core_s = tr.core;
      row.resource_coverage = Math.min(1, tr.covered / row.overlap_ms);
      if (tr.per > 0) row.throttled_share = tr.thr / tr.per;
    }
    view.notes.push("No host or resource-pool identity is recorded; runtimes are grouped as task-owned or shared, not by host.");
  }

  const api = Object.freeze({ schedule });
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.TraceXScheduling = api;
})(globalThis);
