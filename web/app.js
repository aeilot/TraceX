"use strict";
const {
  validate,
  union,
  maximum,
  analyze,
  windowStats,
  page: localPage,
} = globalThis.TraceXProfile;
const t9n = globalThis.TraceXI18n.t;
const $ = (s) => document.querySelector(s),
  NS = "http://www.w3.org/2000/svg";
const colors = {
  agent: "#0f6b5f",
  model: "#4a46c4",
  tool: "#c46a00",
  browser: "#1f78b4",
  network: "#5f97b0",
  evaluation: "#6b7d2a",
  lifecycle: "#8b887c",
  task: "#5d7f7a",
  other: "#9a968a",
};
const state = {
  profile: null,
  analysis: null,
  runs: [],
  runKey: "",
  imports: new Map(),
  task: "all",
  kind: "all",
  query: "",
  selected: null,
  view: "split",
  zoom: 1,
  runtime: "all",
  imported: false,
  pageOffset: 0,
  pageLimit: 250,
  totalSpans: 0,
  hiddenEdges: 0,
  rangeStart: 0,
  rangeEnd: null,
  revision: 0,
  paused: false,
  critical: false,
  stream: null,
  watchInterval: 0,
  runtimeIDs: [],
  resourceError: "",
  resourceReturned: 0,
  resourceTotal: 0,
  request: 0,
};
let toastTimer, searchTimer;
function notify(message) {
  $("#toast").textContent = message;
  $("#toast").hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => ($("#toast").hidden = true), 4500);
}
function node(tag, attrs = {}, text) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  if (text !== undefined) e.textContent = text;
  return e;
}
function html(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}
function format(ms) {
  if (ms == null || !Number.isFinite(ms)) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)} s`;
  if (ms < 3600000)
    return `${Math.floor(ms / 60000)}m ${((ms % 60000) / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 3600000)}h ${Math.floor((ms % 3600000) / 60000)}m`;
}
function rate(perSecond) {
  if (!Number.isFinite(perSecond)) return "—";
  if (perSecond >= 0.1) return `${perSecond.toFixed(3)} / s`;
  if (perSecond * 60 >= 0.1) return `${(perSecond * 60).toFixed(2)} / min`;
  return `${(perSecond * 3600).toFixed(2)} / h`;
}
function bytes(n) {
  if (n == null) return t9n("Unavailable");
  return n >= 1073741824
    ? `${(n / 1073741824).toFixed(2)} GiB`
    : `${(n / 1048576).toFixed(1)} MiB`;
}
function score(v) {
  return Number.isFinite(v) ? v.toFixed(2) : "—";
}
function failedTask(t) {
  return ["failed", "canceled"].includes(t.status);
}
function flagText(t) {
  const out = [];
  if (t.flags?.includes("duration_outlier")) out.push(t9n("duration outlier"));
  if (t.flags?.includes("sparse_monitor")) out.push(t9n("sparse monitor"));
  return out.join(" · ");
}
function networkText(n) {
  if (!n) return "—";
  const parts = [t9n("{count} req", { count: (n.requests ?? 0).toLocaleString() })];
  if (n.failed) parts.push(t9n("{count} failed", { count: n.failed.toLocaleString() }));
  if (n.response_bytes != null) parts.push(bytes(n.response_bytes));
  return parts.join(" · ");
}
function spanDuration(s) {
  return s.incomplete
    ? t9n("In progress")
    : ["completion_only", "unknown"].includes(s.attributes?.time_quality)
      ? t9n("Timing unavailable")
      : format(s.duration_ms);
}
function short(s, n = 26) {
  s = String(s || "");
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}
function kind(s) {
  return s.kind || "other";
}
function color(s) {
  return colors[kind(s)] || colors.other;
}
function visible() {
  return state.profile?.spans || [];
}
function edges() {
  return state.profile?.edges || [];
}
function bounds() {
  const end = state.rangeEnd ?? state.profile?.duration_ms ?? 1;
  return {
    start: state.rangeStart,
    end: Math.max(state.rangeStart + 0.001, end),
    duration: Math.max(0.001, end - state.rangeStart),
  };
}
async function json(url) {
  const response = await fetch(url);
  if (!response.ok) {
    let msg = `HTTP ${response.status}`;
    try {
      const error = await response.json();
      msg = error.error || msg;
    } catch {}
    throw Error(msg);
  }
  return response.json();
}
function allRuns() {
  return [
    ...state.runs,
    ...[...state.imports].map(([id, p]) => ({
      id,
      name: p.name,
      run_id: p.run_id,
      duration_ms: p.duration_ms,
      span_count: p.spans.length,
      configuration: p.configuration,
      live: false,
    })),
  ];
}
function updateRunPickers() {
  const runs = allRuns();
  for (const id of [
    "run-picker",
    "run-picker-mobile",
    "compare-left",
    "compare-right",
  ]) {
    const select = $("#" + id),
      old = select.value;
    select.replaceChildren();
    const label = (run) => run.name || run.run_id || run.id,
      seen = new Map();
    for (const run of runs) seen.set(label(run), (seen.get(label(run)) || 0) + 1);
    for (const run of runs) {
      // Repeated experiment names are told apart by their run timestamp.
      const stamp = /^\d{8}T\d{6}/.exec(run.run_id || "")?.[0];
      select.add(
        new Option(
          seen.get(label(run)) > 1 && stamp ? `${label(run)} · ${stamp}` : label(run),
          run.id,
        ),
      );
    }
    if (id === "run-picker" || id === "run-picker-mobile")
      select.value = state.runKey;
    else
      select.value = runs.some((r) => r.id === old)
        ? old
        : id === "compare-left"
          ? state.runKey
          : runs.find((r) => r.id !== state.runKey)?.id || state.runKey;
  }
  $(".nav-count").textContent = runs.length;
}
function resetFilters() {
  state.task = "all";
  state.kind = "all";
  state.query = "";
  state.selected = null;
  state.zoom = 1;
  state.runtime = "all";
  state.pageOffset = 0;
  state.rangeStart = 0;
  state.rangeEnd = null;
  $("#search").value = "";
  $("#zoom").value = 1;
  closeInspector();
}
async function loadSource() {
  try {
    const data = await json("/api/runs");
    state.runs = data.runs || [];
    state.watchInterval = data.watch_interval_ms || 0;
    if (!state.runKey || !allRuns().some((r) => r.id === state.runKey))
      state.runKey = state.runs[0]?.id || "";
    updateRunPickers();
    await loadView();
    connectEvents();
  } catch (e) {
    showError(e);
  }
}
function showError(e) {
  $("#notice").textContent =
    t9n("Source update failed: {error}. The last valid view is retained.", { error: e.message });
  $("#notice").hidden = false;
}
async function loadView() {
  if (!state.runKey) return;
  const request = ++state.request;
  try {
    let data;
    const local = state.imports.get(state.runKey);
    state.imported = !!local;
    if (local) {
      data = localPage(local, {
        task: state.task,
        kind: state.kind,
        q: state.query,
        runtime: state.runtime,
        offset: state.pageOffset,
        limit: state.pageLimit,
        start_ms: state.rangeStart,
        end_ms: state.rangeEnd,
      });
    } else {
      const params = new URLSearchParams({
        run: state.runKey,
        offset: state.pageOffset,
        limit: state.pageLimit,
        points: 300,
      });
      for (const [k, v] of Object.entries({
        task: state.task,
        kind: state.kind,
        q: state.query,
        runtime: state.runtime,
        start_ms: state.rangeStart,
        end_ms: state.rangeEnd,
      }))
        if (v != null && v !== "" && v !== "all") params.set(k, v);
      data = await json("/api/view?" + params);
    }
    if (request !== state.request) return;
    state.profile = data.profile;
    state.runtimeIDs = data.runtime_ids || [
      ...new Set(data.profile.resources.map((r) => r.runtime_id)),
    ];
    state.resourceError = data.resource_error || "";
    state.resourceReturned = data.profile.resources.length;
    state.resourceTotal = data.total_resources ?? data.profile.resources.length;
    state.analysis = data.analysis || analyze(local || data.profile);
    state.totalSpans = data.total_spans;
    state.hiddenEdges = data.hidden_edges || 0;
    state.revision = data.revision || state.revision;
    if (state.pageOffset >= state.totalSpans && state.pageOffset > 0) {
      state.pageOffset = Math.max(
        0,
        Math.floor((state.totalSpans - 1) / state.pageLimit) * state.pageLimit,
      );
      return loadView();
    }
    $("#notice").hidden = true;
    render();
    setLiveStatus();
    if (state.selected) selectSpan(state.selected);
  } catch (e) {
    if (request === state.request) showError(e);
  }
}
function setLiveStatus(label) {
  const run = state.runs.find((r) => r.id === state.runKey);
  $("#live-status").textContent =
    label ||
    (state.imported
      ? t9n("Local import")
      : state.paused
        ? t9n("Updates paused")
        : run?.last_error
          ? t9n("Source error")
          : run?.live
            ? t9n("Watching · rev {revision}", { revision: state.revision })
            : t9n("Snapshot"));
  $("#pause-live").disabled = state.imported || !state.watchInterval;
  $("#pause-live").textContent = state.paused ? t9n("Resume live") : t9n("Pause live");
  $("#pause-live").setAttribute("aria-pressed", String(state.paused));
}
function connectEvents() {
  if (state.stream || !state.watchInterval) return;
  const stream = new EventSource("/api/events");
  state.stream = stream;
  stream.addEventListener("revision", async (event) => {
    try {
      const update = JSON.parse(event.data);
      if (state.paused) return;
      if (update.run_id === state.runKey) {
        if (update.error) {
          showError(Error(update.error));
          setLiveStatus(t9n("Source error"));
        } else await loadView();
      }
      const data = await json("/api/runs");
      state.runs = data.runs || state.runs;
      updateRunPickers();
      setLiveStatus();
    } catch {
      setLiveStatus(t9n("Reconnecting"));
    }
  });
  stream.onerror = () => setLiveStatus(t9n("Reconnecting"));
  stream.onopen = () => {
    setLiveStatus();
    // Reconcile after every subscription: notice history is deliberately bounded.
    if (!state.paused) loadSource();
  };
}
function render() {
  const p = state.profile,
    a = state.analysis;
  $("#run-name").textContent = p.name || t9n("Untitled workload");
  $("#run-id").textContent = short(p.run_id || t9n("Imported profile"), 65);
  const synthetic = (p.name + " " + p.run_id + " " + p.warnings.join(" "))
    .toLowerCase()
    .includes("synthetic");
  $("#dataset-badge").textContent = synthetic
    ? t9n("Synthetic demo")
    : state.imported
      ? t9n("Imported profile")
      : t9n("Recorded artifacts");
  $("#run-state").textContent = synthetic
    ? t9n("Demo dataset")
    : state.runs.find((r) => r.id === state.runKey)?.live
      ? t9n("Live source")
      : t9n("Recorded evidence");
  $("#metric-duration").textContent = format(p.duration_ms);
  $("#metric-agents").textContent = a.agent_count || "—";
  $("#metric-spans").textContent = (
    a.span_count ?? state.totalSpans
  ).toLocaleString();
  $("#metric-span-note").textContent =
    t9n("Across {count} task occurrences · full run", { count: p.tasks.length });
  const kindTime = (k) => {
    const item = a.kinds.find((x) => x.kind === k);
    return item && item.time_quality !== "completion_only"
      ? format(item.union_ms)
      : "—";
  };
  $("#metric-active").textContent =
    `${kindTime("model")} / ${kindTime("tool")}`;
  $("#task-count").textContent = p.tasks.length;
  const list = $("#task-list");
  list.replaceChildren();
  const all = html(
    "button",
    `task-button ${state.task === "all" ? "selected" : ""}`,
  );
  all.append(
    html("span", "status-dot"),
    html("span", "task-name", t9n("All task occurrences")),
  );
  all.onclick = () => setTask("all");
  list.append(all);
  for (const t of p.tasks) {
    const b = html(
      "button",
      `task-button ${state.task === t.id ? "selected" : ""}`,
    );
    const flags = flagText(t);
    b.title = [
      t.id,
      Number.isFinite(t.score) ? t9n("accuracy {score}", { score: t.score.toFixed(3) }) : "",
      t.end_reason ? t9n("ended: {reason}", { reason: t9n(t.end_reason) }) : "",
      flags,
    ]
      .filter(Boolean)
      .join("\n");
    b.setAttribute("aria-pressed", String(state.task === t.id));
    b.append(
      html("span", `status-dot ${failedTask(t) ? "failed" : ""}`),
      html("span", "task-name", (flags ? "⚠ " : "") + (t.name || t.id)),
    );
    if (Number.isFinite(t.score)) {
      const sc = html("span", "task-score", score(t.score));
      sc.style.setProperty("--score", t.score);
      b.append(sc);
    }
    b.append(html("span", "task-duration", format(t.duration_ms)));
    b.onclick = () => setTask(t.id);
    list.append(b);
  }
  const select = $("#kind-filter");
  select.replaceChildren(new Option(t9n("All span types"), "all"));
  for (const k of a.kinds.map((k) => k.kind).sort())
    select.add(new Option(k[0].toUpperCase() + k.slice(1), k));
  select.value = state.kind;
  const caps = $("#capability-list");
  caps.replaceChildren();
  for (const c of p.capabilities
    .filter((c) => !c.name.startsWith("monitor/"))
    .slice(0, 4)) {
    const item = html(
      "div",
      `capability ${["unsupported", "not_enabled", "failed", "unavailable"].includes(c.status) ? "unavailable" : ""}`,
    );
    item.append(html("span", "status-dot"), html("span", "", c.name));
    item.title = `${t9n(c.status)}${c.reason ? ": " + t9n(c.reason) : ""}`;
    caps.append(item);
  }
  const runtime = $("#resource-filter"),
    oldOptions = [...runtime.options].filter((o) => o.value !== "all");
  const names = new Map(oldOptions.map((o) => [o.value, o.text]));
  for (const id of state.runtimeIDs)
    if (!names.has(id)) names.set(id, short(id, 30));
  for (const r of p.resources)
    names.set(
      r.runtime_id || r.component,
      r.component + (r.runtime_id ? " · " + short(r.runtime_id, 16) : ""),
    );
  runtime.replaceChildren(new Option(t9n("All runtimes"), "all"));
  for (const [k, v] of names) runtime.add(new Option(v, k));
  runtime.value = state.runtime;
  $("#resource-message").hidden = !state.resourceError;
  $("#resource-message").textContent = t9n(state.resourceError);
  $(".resource-controls .tag").textContent =
    t9n("{returned} / {total} samples", { returned: state.resourceReturned.toLocaleString(), total: state.resourceTotal.toLocaleString() });
  const cov = $("#coverage");
  cov.replaceChildren();
  cov.append(
    html(
      "p",
      "",
      synthetic
        ? t9n("Synthetic demo. Agent relationships, timing, usage and resource signals are authored example data.")
        : t9n("Evidence coverage. Relationships require explicit references; overlapping timing does not establish causality."),
    ),
  );
  for (const w of p.warnings)
    if (!synthetic || !w.toLowerCase().includes("synthetic"))
      cov.append(html("p", "", t9n(w)));
  if (p.capabilities.length) {
    const details = html("details", "coverage-details");
    details.append(
      html(
        "summary",
        "",
        t9n("Inspect {count} source coverage reports", { count: p.capabilities.length }),
      ),
    );
    for (const c of p.capabilities)
      details.append(
        html(
          "p",
          "",
          `${c.name}: ${t9n(c.status)}${c.reason ? " · " + t9n(c.reason) : ""}`,
        ),
      );
    cov.append(details);
  }
  const b = bounds();
  $("#range-start").value = Number((b.start / 1000).toFixed(3));
  $("#range-end").value = Number((b.end / 1000).toFixed(3));
  $("#page-label").textContent = state.totalSpans
    ? t9n("{first}–{last} of {total} matching spans", { first: state.pageOffset + 1, last: Math.min(state.pageOffset + state.pageLimit, state.totalSpans), total: state.totalSpans.toLocaleString() })
    : t9n("No matching spans");
  $("#edge-coverage").textContent = state.hiddenEdges
    ? t9n("{count} relationships cross this page; inspect a node to follow neighbors.", { count: state.hiddenEdges })
    : t9n("All relationships between visible spans are shown.");
  $("#page-prev").disabled = state.pageOffset === 0;
  $("#page-next").disabled =
    state.pageOffset + state.pageLimit >= state.totalSpans;
  renderAnalysis();
  renderViews();
}
function renderAnalysis() {
  const a = state.analysis,
    p = state.profile;
  const metrics = $("#analysis-metrics");
  metrics.replaceChildren();
  const done = a.task_succeeded + a.task_failed;
  for (const [label, value, note] of [
    [
      t9n("P50 / P95 TASK LATENCY"),
      done ? `${format(a.task_p50_ms)} / ${format(a.task_p95_ms)}` : "—",
      t9n("Completed task occurrences"),
    ],
    [
      t9n("MEAN ACCURACY"),
      a.score_known ? score(a.score_mean) : "—",
      a.score_known
        ? t9n("Recorded score.json · {known}/{total} tasks", { known: a.score_known, total: a.task_count })
        : t9n("No continuous scores recorded"),
    ],
    [t9n("PEAK WORK CONCURRENCY"), a.max_concurrency, t9n("Nested envelopes excluded")],
    [
      t9n("THROUGHPUT"),
      rate(a.throughput_per_second),
      t9n("{succeeded} succeeded · {failed} failed", { succeeded: a.task_succeeded, failed: a.task_failed }),
    ],
    [
      t9n("MODEL TOKENS"),
      `${a.usage.input_tokens?.toLocaleString() ?? "—"} / ${a.usage.output_tokens?.toLocaleString() ?? "—"}`,
      t9n("Input / output · {known}/{calls} input usage known", { known: a.usage.input_known, calls: a.usage.model_calls }),
    ],
  ]) {
    const c = html("div");
    c.append(
      html("span", "metric-label", label),
      html("strong", "", String(value)),
      html("small", "", note),
    );
    metrics.append(c);
  }
  const breakdown = $("#kind-breakdown");
  breakdown.replaceChildren();
  for (const k of a.kinds) {
    const row = html("div", "breakdown-row"),
      title = html("div", "breakdown-title");
    title.append(
      html("span", "", `${k.kind} · ${k.count}`),
      html(
        "strong",
        "",
        k.time_quality === "completion_only"
          ? t9n("Timing unavailable")
          : format(k.union_ms),
      ),
    );
    const track = html("div", "breakdown-track"),
      bar = html("i");
    bar.style.width =
      Math.min(100, p.duration_ms ? (k.union_ms / p.duration_ms) * 100 : 0) +
      "%";
    bar.style.background = colors[k.kind] || colors.other;
    track.append(bar);
    row.append(
      title,
      track,
      html(
        "small",
        "",
        k.incomplete_count
          ? t9n("{duration} summed calls · {count} incomplete or completion-only", { duration: format(k.summed_ms), count: k.incomplete_count })
          : t9n("{duration} summed calls", { duration: format(k.summed_ms) }),
      ),
    );
    breakdown.append(row);
  }
  const critical = a.critical_path;
  $("#critical-duration").textContent =
    critical.status === "available"
      ? format(critical.duration_ms)
      : t9n("Unavailable");
  $("#critical-reason").textContent = t9n(critical.reason || "");
  $("#highlight-critical").disabled = critical.status !== "available";
  const nodes = $("#critical-nodes");
  nodes.replaceChildren();
  for (const id of critical.span_ids || []) {
    const label = pathLabel(id),
      button = html("button", "path-node");
    button.append(
      html("span", "path-name", label.name),
      html("span", "path-task", label.task),
      html("span", "path-duration", label.duration),
    );
    button.title = id;
    button.onclick = () => jumpToSpan(id);
    nodes.append(button);
  }
  const rows = p.tasks.map((t) => {
    const c = a.coverage.find((c) => c.task_id === t.id);
    const flags = flagText(t);
    return [
      (flags ? "⚠ " : "") + (t.name || t.id),
      t9n(t.status || "unknown"),
      t9n(t.end_reason || "—"),
      t9n(t.evaluation_status || "unknown"),
      score(t.score),
      t9n(t.cleanup_status || "unknown"),
      format(t.duration_ms) + (flags ? ` (${flags})` : ""),
      networkText(t.network),
      c && c.status !== "unavailable"
        ? `${(c.ratio * 100).toFixed(1)}%`
        : t9n("Unavailable"),
      c && c.status !== "unavailable"
        ? format(c.unattributed_ms)
        : t9n("Unavailable"),
    ];
  });
  table(
    $("#coverage-table"),
    [
      t9n("Task"),
      t9n("Harness"),
      t9n("End reason"),
      t9n("Evaluation"),
      t9n("Accuracy"),
      t9n("Cleanup"),
      t9n("Duration"),
      t9n("Browser network"),
      t9n("Observed work"),
      t9n("Unattributed"),
    ],
    rows,
  );
}
// pathLabel names a critical-path span readably. IDs share long task
// prefixes, so the span name (from the page, or the ID's last meaningful
// segment) and the occurrence number carry the difference.
function pathLabel(id) {
  const span = visible().find((s) => s.id === id),
    parts = id.split("/"),
    occurrence = /-(\d{3})$/.exec(parts[0])?.[1];
  let name = span?.name;
  if (!name) {
    const last = parts.at(-1);
    name = (/^\d+$/.test(last) && parts.length > 1 ? parts.at(-2) : last).replaceAll("_", " ");
    if (name.length > 28) name = "…" + name.slice(-12);
  }
  return {
    name,
    task: occurrence ? "#" + occurrence : parts.length > 1 ? short(parts[0], 14) : "",
    duration: span && !span.incomplete ? format(span.duration_ms) : "",
  };
}
function table(target, headers, rows) {
  const t = html("table");
  const head = html("thead"),
    tr = html("tr");
  for (const h of headers) tr.append(html("th", "", h));
  head.append(tr);
  const body = html("tbody");
  for (const row of rows) {
    const tr = html("tr");
    for (const cell of row) tr.append(html("td", "", String(cell)));
    body.append(tr);
  }
  t.append(head, body);
  target.replaceChildren(t);
}
async function compareRuns() {
  const left = $("#compare-left").value,
    right = $("#compare-right").value;
  if (!left || !right) return;
  try {
    const get = async (id) => {
      if (state.imports.has(id)) {
        const p = state.imports.get(id);
        return { profile: p, analysis: analyze(p) };
      }
      const metadata = allRuns().find((r) => r.id === id);
      const analysis = await json(
        "/api/analysis?run=" + encodeURIComponent(id),
      );
      return { profile: metadata, analysis: analysis.analysis || analysis };
    };
    const [l, r] = await Promise.all([get(left), get(right)]);
    const configKeys = new Set([
      ...Object.keys(l.profile.configuration || {}),
      ...Object.keys(r.profile.configuration || {}),
    ]);
    const differences = [...configKeys].filter(
      (k) =>
        JSON.stringify(l.profile.configuration?.[k]) !==
        JSON.stringify(r.profile.configuration?.[k]),
    );
    const items = [
      [t9n("Duration"), l.profile.duration_ms, r.profile.duration_ms, format],
      [
        t9n("Mean accuracy"),
        l.analysis.score_mean,
        r.analysis.score_mean,
        (v) => (v == null ? t9n("Unavailable") : v.toFixed(3)),
      ],
      [t9n("Task P50"), l.analysis.task_p50_ms, r.analysis.task_p50_ms, format],
      [t9n("Task P95"), l.analysis.task_p95_ms, r.analysis.task_p95_ms, format],
      [
        t9n("Throughput / s"),
        l.analysis.throughput_per_second,
        r.analysis.throughput_per_second,
        (v) => v.toFixed(3),
      ],
      [
        t9n("Peak concurrency"),
        l.analysis.max_concurrency,
        r.analysis.max_concurrency,
        String,
      ],
      [
        t9n("Input tokens"),
        l.analysis.usage.input_tokens,
        r.analysis.usage.input_tokens,
        (v) => (v == null ? t9n("Unavailable") : v.toLocaleString()),
      ],
      [
        t9n("Output tokens"),
        l.analysis.usage.output_tokens,
        r.analysis.usage.output_tokens,
        (v) => (v == null ? t9n("Unavailable") : v.toLocaleString()),
      ],
    ];
    const target = $("#comparison-result");
    table(
      target,
      [t9n("Metric"), l.profile.name || left, r.profile.name || right, t9n("Change")],
      items.map(([name, a, b, format]) => [
        name,
        format(a),
        format(b),
        a == null || b == null
          ? t9n("Unavailable")
          : a === 0
            ? b === 0
              ? "0%"
              : t9n("No zero baseline")
            : `${b >= a ? "+" : ""}${(((b - a) / a) * 100).toFixed(1)}%`,
      ]),
    );
    const note = html(
      "p",
      "comparison-note",
      differences.length
        ? t9n(
            "Experiment conditions differ: {fields}. Changes are descriptive, not a controlled causal comparison.",
            { fields: differences.join(", ") },
          )
        : configKeys.size
          ? t9n("Recorded configuration fields match. Unrecorded conditions and incomplete measurements still limit comparability.")
          : t9n("Configuration metadata is unavailable. Experiment comparability is unknown."),
    );
    target.prepend(note);
    if (configKeys.size) {
      const section = html("div", "configuration-table");
      table(
        section,
        [t9n("Condition"), t9n("Baseline"), t9n("Candidate")],
        [...configKeys].map((k) => [
          k,
          String(l.profile.configuration?.[k] ?? t9n("Unknown")),
          String(r.profile.configuration?.[k] ?? t9n("Unknown")),
        ]),
      );
      target.append(section);
    }
    const coverage = html("div", "comparison-coverage");
    for (const [label, item] of [
      [t9n("Baseline"), l],
      [t9n("Candidate"), r],
    ]) {
      const known = item.analysis.coverage.filter(
        (c) => c.status !== "unavailable",
      );
      const average = known.length
        ? known.reduce((n, c) => n + c.ratio, 0) / known.length
        : null;
      coverage.append(
        html(
          "p",
          "",
          t9n("{label}: {known}/{total} tasks have usable timing; mean observed-work coverage {coverage}; token input known for {inputs}/{calls} model records.", { label, known: known.length, total: item.analysis.coverage.length, coverage: average == null ? t9n("unavailable") : (average * 100).toFixed(1) + "%", inputs: item.analysis.usage.input_known, calls: item.analysis.usage.model_calls }),
        ),
      );
    }
    target.append(coverage);
  } catch (e) {
    notify(t9n("Comparison failed: {error}", { error: e.message }));
  }
}
function setTask(id) {
  state.task = id;
  state.pageOffset = 0;
  closeInspector();
  loadView();
}
function renderViews() {
  if (!state.profile) return;
  const spans = visible();
  $("#dag-count").textContent = spans.length;
  $("#timeline-count").textContent = spans.length;
  $("#dag-panel").hidden = state.view === "timeline";
  $("#timeline-panel").hidden = state.view === "dag";
  for (const b of document.querySelectorAll("[data-view]")) {
    b.classList.toggle("selected", b.dataset.view === state.view);
    b.setAttribute("aria-pressed", String(b.dataset.view === state.view));
  }
  drawDAG(spans);
  drawTimeline(spans);
  drawResources();
}
function drawDAG(spans) {
  const svg = $("#dag");
  svg.replaceChildren();
  $("#dag-empty").hidden = spans.length !== 0;
  if (!spans.length) {
    svg.setAttribute("width", "100%");
    svg.setAttribute("height", "0");
    return;
  }
  const displayed = spans,
    ids = new Set(displayed.map((s) => s.id));
  const es = edges().filter((e) => ids.has(e.source) && ids.has(e.target));
  $("#dag-note").textContent =
    state.hiddenEdges > 0
      ? t9n("{visible} visible edges · {hidden} cross this page", { visible: es.length, hidden: state.hiddenEdges })
      : es.length
        ? t9n(es.length === 1 ? "{count} recorded relationship" : "{count} recorded relationships", { count: es.length })
        : t9n("No recorded dependencies · isolated spans");
  const parents = new Map(),
    adj = new Map(),
    indeg = new Map();
  for (const s of displayed) {
    parents.set(s.id, []);
    adj.set(s.id, []);
    indeg.set(s.id, 0);
  }
  for (const e of es) {
    parents.get(e.target).push(e.source);
    adj.get(e.source).push(e.target);
    indeg.set(e.target, indeg.get(e.target) + 1);
  }
  const depth = new Map(),
    q = displayed.filter((s) => !indeg.get(s.id)).map((s) => s.id);
  for (const id of q) depth.set(id, 0);
  for (let i = 0; i < q.length; i++) {
    for (const id of adj.get(q[i])) {
      depth.set(id, Math.max(depth.get(id) || 0, (depth.get(q[i]) || 0) + 1));
      indeg.set(id, indeg.get(id) - 1);
      if (!indeg.get(id)) q.push(id);
    }
  }
  const columns = new Map();
  for (const s of displayed) {
    const d = depth.get(s.id) || 0;
    if (!columns.has(d)) columns.set(d, []);
    columns.get(d).push(s);
  }
  const pos = new Map();
  let maxRows = 0;
  for (const [d, col] of columns) {
    col.sort((a, b) => a.start_ms - b.start_ms);
    maxRows = Math.max(maxRows, col.length);
    for (let i = 0; i < col.length; i++)
      pos.set(col[i].id, { x: 28 + d * 225, y: 39 + i * 94 });
  }
  const width = Math.max($("#dag-scroll").clientWidth, columns.size * 225 + 36),
    height = Math.max(280, maxRows * 94 + 55);
  svg.setAttribute("width", width);
  svg.setAttribute("height", height);
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  const defs = node("defs"),
    marker = node("marker", {
      id: "arrow",
      viewBox: "0 0 10 10",
      refX: 9,
      refY: 5,
      markerWidth: 5,
      markerHeight: 5,
      orient: "auto-start-reverse",
    });
  marker.append(node("path", { d: "M 0 0 L 10 5 L 0 10 z", fill: "#8b887c" }));
  defs.append(marker);
  svg.append(defs);
  for (const [d] of columns)
    svg.append(
      node(
        "text",
        { x: 28 + d * 225, y: 20, class: "graph-depth" },
        d === 0 ? t9n("ROOT / UNLINKED") : t9n("DEPTH {depth}", { depth: d }),
      ),
    );
  for (const e of es) {
    const a = pos.get(e.source),
      b = pos.get(e.target);
    const sx = a.x + 190,
      sy = a.y + 34,
      tx = b.x,
      ty = b.y + 34;
    const edgePath = node("path", {
      d: `M${sx},${sy} C${sx + 20},${sy} ${tx - 20},${ty} ${tx - 6},${ty}`,
      class: `dag-edge edge-${e.kind} ${state.selected === e.source || state.selected === e.target ? "focused" : ""} ${state.critical && state.analysis.critical_path.span_ids.includes(e.source) && state.analysis.critical_path.span_ids.includes(e.target) ? "critical" : ""}`,
      "marker-end": "url(#arrow)",
    });
    edgePath.append(node("title", {}, `${e.kind}: ${e.source} → ${e.target}`));
    svg.append(edgePath);
  }
  for (const s of displayed) {
    const { x, y } = pos.get(s.id);
    const g = node("g", {
      class: `dag-node ${s.status === "failed" || s.status === "ERROR" ? "failed" : ""} ${state.selected === s.id ? "selected" : ""} ${state.critical && state.analysis.critical_path.span_ids.includes(s.id) ? "critical" : ""}`,
      transform: `translate(${x},${y})`,
      tabindex: 0,
      role: "button",
      "aria-label": `${s.agent_id || t9n("Agent")}: ${s.name}, ${spanDuration(s)}`,
    });
    g.append(
      node("rect", { width: 190, height: 70, rx: 7 }),
      node("rect", {
        x: 0,
        y: 15,
        width: 3,
        height: 39,
        rx: 1,
        style: `fill:${color(s)};stroke:none`,
      }),
      node(
        "text",
        { x: 13, y: 17, class: "node-agent" },
        short(s.agent_id || s.task_id || t9n("Unassigned"), 26),
      ),
      node("text", { x: 13, y: 35, class: "node-title" }, short(s.name, 28)),
      node(
        "text",
        { x: 13, y: 55, class: "node-kind", style: `fill:${color(s)}` },
        kind(s).toUpperCase(),
      ),
      node(
        "text",
        { x: 178, y: 55, class: "node-duration", "text-anchor": "end" },
        spanDuration(s),
      ),
      node(
        "title",
        {},
        `${s.name}\n${s.agent_id || s.task_id || ""}\n${spanDuration(s)} · ${s.status || "unknown"}`,
      ),
    );
    g.onclick = () => selectSpan(s.id);
    g.onkeydown = (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectSpan(s.id);
      }
    };
    svg.append(g);
  }
}
function drawTimeline(spans) {
  const svg = $("#timeline");
  svg.replaceChildren();
  const labelWidth = 180,
    baseWidth = Math.max(
      500,
      $("#timeline-scroll").clientWidth - labelWidth - 25,
    ),
    plotWidth = baseWidth * state.zoom,
    total = bounds().duration;
  const width = labelWidth + plotWidth + 25;
  const lanes = new Map();
  for (const s of spans) {
    const key = `${s.task_id || "run"}\0${s.agent_id || "unassigned"}`;
    if (!lanes.has(key))
      lanes.set(key, {
        name: s.agent_id || t9n("Unassigned lane"),
        task: s.task_id || t9n("Run scope"),
        spans: [],
      });
    lanes.get(key).spans.push(s);
  }
  let y = 38;
  const rows = [];
  for (const lane of lanes.values()) {
    const ends = [];
    lane.spans.sort((a, b) => a.start_ms - b.start_ms);
    for (const s of lane.spans) {
      let i = ends.findIndex((end) => end <= s.start_ms);
      if (i < 0) i = ends.length;
      ends[i] = s.start_ms + s.duration_ms;
      rows.push({ s, y: y + i * 27 });
    }
    lane.y = y;
    lane.height = Math.max(1, ends.length) * 27 + 20;
    y += lane.height;
  }
  const height = Math.max(130, y + 15);
  svg.setAttribute("width", width);
  svg.setAttribute("height", height);
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  for (let i = 0; i <= 8 * state.zoom; i++) {
    const t = (total * i) / (8 * state.zoom),
      x = labelWidth + (plotWidth * t) / total;
    svg.append(
      node("line", {
        x1: x,
        x2: x,
        y1: 27,
        y2: height,
        class: "timeline-grid",
      }),
      node(
        "text",
        {
          x,
          y: 16,
          "text-anchor": i === 0 ? "start" : "middle",
          class: "timeline-label",
        },
        format(t + bounds().start),
      ),
    );
  }
  for (const lane of lanes.values()) {
    svg.append(
      node("rect", {
        x: 0,
        y: lane.y - 8,
        width: labelWidth - 12,
        height: lane.height,
        fill: "#fcfbf7",
      }),
      node(
        "text",
        { x: 18, y: lane.y + 9, class: "timeline-agent" },
        short(lane.name, 22),
      ),
      node(
        "text",
        { x: 18, y: lane.y + 23, class: "timeline-label" },
        short(lane.task, 22),
      ),
      node("line", {
        x1: 0,
        x2: width,
        y1: lane.y + lane.height - 11,
        y2: lane.y + lane.height - 11,
        stroke: "#e6e2d6",
      }),
    );
  }
  for (const { s, y } of rows) {
    const clippedStart = Math.max(s.start_ms, bounds().start),
      clippedEnd = Math.min(s.start_ms + s.duration_ms, bounds().end);
    const x =
        labelWidth + ((clippedStart - bounds().start) / total) * plotWidth,
      w = Math.max(
        3,
        (Math.max(0, clippedEnd - clippedStart) / total) * plotWidth,
      );
    const bar = node("rect", {
      x,
      y,
      width: w,
      height: 20,
      rx: 3,
      fill: color(s),
      class: `timeline-bar ${s.incomplete ? "incomplete" : ""} ${s.status === "failed" || s.status === "ERROR" ? "failed" : ""} ${state.selected === s.id ? "selected" : ""} ${state.critical && state.analysis.critical_path.span_ids.includes(s.id) ? "critical" : ""}`,
      tabindex: 0,
      role: "button",
      "aria-label": `${s.name}, ${format(s.start_ms)} to ${format(s.start_ms + s.duration_ms)}`,
    });
    bar.append(
      node(
        "title",
        {},
        `${s.name}\n${format(s.start_ms)} → ${format(s.start_ms + s.duration_ms)}\n${t9n("Duration: {duration}", { duration: spanDuration(s) })}`,
      ),
    );
    bar.onclick = () => selectSpan(s.id);
    bar.onkeydown = (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectSpan(s.id);
      }
    };
    svg.append(bar);
    if (w > 52)
      svg.append(
        node(
          "text",
          { x: x + 7, y: y + 13, class: "timeline-bar-label" },
          short(s.name, Math.floor((w - 14) / 5.4)),
        ),
      );
  }
  if (!spans.length)
    svg.append(
      node(
        "text",
        { x: 30, y: 70, class: "timeline-label" },
        t9n("No matching execution spans."),
      ),
    );
  svg.onpointerdown = (event) => {
    if (event.target.closest(".timeline-bar")) return;
    const rect = svg.getBoundingClientRect();
    const px = event.clientX - rect.left;
    if (px < labelWidth) return;
    const initial = Math.max(0, Math.min(plotWidth, px - labelWidth));
    const overlay = node("rect", {
      x: initial + labelWidth,
      y: 27,
      width: 1,
      height: height - 27,
      fill: "#e03a1e14",
      stroke: "#e03a1e",
      "pointer-events": "none",
    });
    svg.append(overlay);
    svg.setPointerCapture(event.pointerId);
    svg.onpointermove = (move) => {
      const next = Math.max(
        0,
        Math.min(plotWidth, move.clientX - rect.left - labelWidth),
      );
      overlay.setAttribute("x", Math.min(initial, next) + labelWidth);
      overlay.setAttribute("width", Math.abs(next - initial));
    };
    svg.onpointerup = (up) => {
      const next = Math.max(
        0,
        Math.min(plotWidth, up.clientX - rect.left - labelWidth),
      );
      overlay.remove();
      svg.onpointermove = null;
      svg.onpointerup = null;
      if (svg.hasPointerCapture(up.pointerId))
        svg.releasePointerCapture(up.pointerId);
      if (Math.abs(next - initial) > 8)
        applyRange(
          bounds().start + (Math.min(initial, next) / plotWidth) * total,
          bounds().start + (Math.max(initial, next) / plotWidth) * total,
        );
    };
    svg.onpointercancel = () => {
      overlay.remove();
      svg.onpointermove = null;
      svg.onpointerup = null;
    };
  };
  $("#time-window").textContent =
    t9n("{start} → {end} · {count} matching spans", { start: format(bounds().start), end: format(bounds().end), count: state.totalSpans });
}
function drawResources() {
  const total = bounds().duration;
  const selected = state.profile.resources.filter(
    (r) =>
      (state.task === "all" || r.task_id === state.task || !r.task_id) &&
      (state.runtime === "all" ||
        (r.runtime_id || r.component) === state.runtime),
  );
  drawChart($("#cpu-chart"), selected, "cpu_percent", total, colors.agent);
  drawChart($("#memory-chart"), selected, "memory_bytes", total, colors.model);
  drawChart($("#gpu-chart"), selected, "gpu_percent", total, colors.tool);
  const net = networkRates(selected);
  drawChart(
    $("#network-chart"),
    net,
    "network_rate",
    total,
    colors.browser,
    (v) =>
      v >= 1048576
        ? (v / 1048576).toFixed(0) + "M"
        : v >= 1024
          ? (v / 1024).toFixed(0) + "K"
          : v.toFixed(0),
  );
  const rates = net.map((r) => r.network_rate).filter(Number.isFinite);
  $("#network-peak").textContent = rates.length
    ? t9n("{value} peak", { value: `${bytes(maximum(rates))}/s` })
    : t9n("Not measured");
  const gpu = selected.map((r) => r.gpu_percent).filter(Number.isFinite);
  $("#gpu-peak").textContent = gpu.length
    ? t9n("{value} peak", { value: `${maximum(gpu).toFixed(1)}%` })
    : t9n("Not measured");
  const cpu = selected.map((r) => r.cpu_percent).filter(Number.isFinite),
    mem = selected.map((r) => r.memory_bytes).filter(Number.isFinite);
  $("#cpu-peak").textContent = cpu.length
    ? t9n("{value} peak", { value: `${maximum(cpu).toFixed(1)}%` })
    : t9n("Not measured");
  // Percent of the runtime's own memory limit; series without a limit are
  // excluded rather than compared against another runtime's limit.
  const pct = selected
    .filter(
      (r) => Number.isFinite(r.memory_bytes) && r.memory_limit_bytes > 0,
    )
    .map((r) => (r.memory_bytes / r.memory_limit_bytes) * 100);
  $("#memory-peak").textContent = mem.length
    ? t9n("{value} peak", { value: bytes(maximum(mem)) }) +
      (pct.length ? " · " + t9n("{percent} of limit", { percent: `${maximum(pct).toFixed(1)}%` }) : "")
    : t9n("Not measured");
}
// Monitor network counters are cumulative per runtime; rates come from
// consecutive samples of one series. Counter resets yield no rate.
function networkRates(samples) {
  const series = new Map(),
    out = [];
  for (const r of samples) {
    if (!Number.isFinite(r.network_rx_bytes) && !Number.isFinite(r.network_tx_bytes))
      continue;
    const id = r.runtime_id || `${r.task_id}/${r.component}`;
    if (!series.has(id)) series.set(id, []);
    series.get(id).push(r);
  }
  for (const rs of series.values()) {
    rs.sort((a, b) => a.time_ms - b.time_ms);
    for (let i = 1; i < rs.length; i++) {
      const a = rs[i - 1],
        b = rs[i],
        dt = (b.time_ms - a.time_ms) / 1000,
        d =
          (b.network_rx_bytes ?? 0) -
          (a.network_rx_bytes ?? 0) +
          (b.network_tx_bytes ?? 0) -
          (a.network_tx_bytes ?? 0);
      out.push({
        task_id: b.task_id,
        component: b.component,
        runtime_id: b.runtime_id,
        time_ms: b.time_ms,
        network_rate: dt > 0 && d >= 0 ? d / dt : null,
      });
    }
  }
  return out;
}
function drawChart(svg, samples, key, total, tint, axis) {
  svg.replaceChildren();
  const w = 500,
    h = 112,
    left = 37,
    right = 10,
    bottom = 22,
    top = 10;
  svg.setAttribute("viewBox", `0 0 ${w} ${h}`);
  const valid = samples.filter((r) => Number.isFinite(r[key]));
  if (!valid.length) {
    svg.append(
      node(
        "text",
        { x: w / 2, y: 55, "text-anchor": "middle", class: "chart-axis" },
        t9n("No measurements available"),
      ),
    );
    return;
  }
  const max =
    Math.max(
      maximum(valid.map((r) => r[key])),
      key.endsWith("percent") ? 1 : axis ? 1024 : 1048576,
    ) * 1.15;
  for (let i = 0; i <= 2; i++) {
    const value = (max * i) / 2,
      y = h - bottom - ((h - bottom - top) * i) / 2;
    svg.append(
      node("line", {
        x1: left,
        x2: w - right,
        y1: y,
        y2: y,
        stroke: "#d9d5c8",
        "stroke-dasharray": "2 3",
      }),
      node(
        "text",
        { x: left - 6, y: y + 3, "text-anchor": "end", class: "chart-axis" },
        axis
          ? axis(value)
          : key.endsWith("percent")
            ? value.toFixed(0) + "%"
            : (value / 1048576).toFixed(0) + "M",
      ),
    );
  }
  for (let i = 0; i <= 4; i++)
    svg.append(
      node(
        "text",
        {
          x: left + ((w - right - left) * i) / 4,
          y: h - 4,
          "text-anchor": "middle",
          class: "chart-axis",
        },
        format(bounds().start + (total * i) / 4),
      ),
    );
  const series = new Map();
  for (const r of samples) {
    const id = r.runtime_id || `${r.task_id}/${r.component}`;
    if (!series.has(id)) series.set(id, []);
    series.get(id).push(r);
  }
  let idx = 0;
  for (const [id, rs] of series) {
    rs.sort((a, b) => a.time_ms - b.time_ms);
    const deltas = [];
    for (let i = 1; i < rs.length; i++)
      if (rs[i].time_ms > rs[i - 1].time_ms)
        deltas.push(rs[i].time_ms - rs[i - 1].time_ms);
    deltas.sort((a, b) => a - b);
    const interval = deltas.length
      ? deltas[Math.floor(deltas.length / 2)]
      : Infinity;
    let path = "",
      drawing = false,
      prev = -Infinity;
    for (const r of rs) {
      if (!Number.isFinite(r[key])) {
        drawing = false;
        continue;
      }
      const x =
          left + ((r.time_ms - bounds().start) / total) * (w - left - right),
        y = h - bottom - (r[key] / max) * (h - bottom - top);
      path += `${drawing && r.time_ms - prev <= interval * 2.5 ? "L" : "M"}${x.toFixed(2)},${y.toFixed(2)} `;
      drawing = true;
      prev = r.time_ms;
    }
    const line = node("path", {
      d: path,
      fill: "none",
      stroke: idx++ % 2 ? tint + "99" : tint,
      "stroke-width": 1.6,
      "stroke-linecap": "round",
    });
    line.append(node("title", {}, `${id} · ${rs[0]?.component || "runtime"}`));
    svg.append(line);
    if (rs.length === 1 && Number.isFinite(rs[0][key]))
      svg.append(
        node("circle", {
          cx:
            left +
            ((rs[0].time_ms - bounds().start) / total) * (w - left - right),
          cy: h - bottom - (rs[0][key] / max) * (h - bottom - top),
          r: 2,
          fill: tint,
        }),
      );
  }
}
async function selectSpan(id) {
  let s = state.profile.spans.find((s) => s.id === id),
    related = [],
    stats;
  try {
    if (state.imported) {
      const p = state.imports.get(state.runKey);
      s = p.spans.find((s) => s.id === id);
      related = p.edges.filter((e) => e.source === id || e.target === id);
      stats = windowStats(p, id);
    } else {
      const result = await json(
        "/api/span?" + new URLSearchParams({ run: state.runKey, id }),
      );
      s = result.span;
      related = result.edges || [];
      stats = result.resource_stats || result.window_stats;
    }
  } catch (e) {
    notify(t9n("Span details unavailable: {error}", { error: e.message }));
    return;
  }
  if (!s) return;
  state.selected = id;
  $("#inspector").hidden = false;
  document.body.classList.add("inspecting");
  const content = $("#inspector-content");
  content.replaceChildren();
  const k = html("div", "detail-kind", kind(s));
  k.style.color = color(s);
  content.append(k, html("h2", "detail-title", s.name));
  const durationUnknown =
    s.incomplete ||
    ["completion_only", "unknown"].includes(s.attributes?.time_quality);
  const metric = html(
    "div",
    "detail-metric",
    durationUnknown ? t9n("Unavailable") : format(s.duration_ms),
  );
  metric.append(html("small", "", s.incomplete ? t9n("in progress") : t9n("duration")));
  content.append(metric);
  const dl = html("dl");
  const task = state.profile.tasks.find((t) => t.id === s.task_id);
  for (const [a, b] of [
    [t9n("Agent"), s.agent_id || t9n("Not recorded")],
    [t9n("Task"), s.task_id || t9n("Run scope")],
    [t9n("Status"), t9n(s.status || "Unknown")],
    [t9n("Time quality"), s.attributes?.time_quality || t9n("Recorded interval")],
    [t9n("Start"), format(s.start_ms)],
    [
      t9n("End"),
      s.incomplete ? t9n("Not yet recorded") : format(s.start_ms + s.duration_ms),
    ],
    [t9n("Source"), s.source || t9n("Unspecified")],
    [t9n("Span ID"), s.id],
    [t9n("Runtime"), s.runtime_id || t9n("Not recorded")],
    ...(task
      ? [
          [t9n("Evaluation"), t9n(task.evaluation_status || "Unknown")],
          [
            t9n("Accuracy"),
            Number.isFinite(task.score)
              ? task.score.toFixed(3)
              : t9n("Not recorded"),
          ],
          [t9n("End reason"), t9n(task.end_reason || "Not recorded")],
          [t9n("Cleanup"), t9n(task.cleanup_status || "Unknown")],
          ...(task.flags?.length ? [[t9n("Evidence flags"), flagText(task)]] : []),
        ]
      : []),
    ...(s.usage
      ? [
          [t9n("Input tokens"), s.usage.input_tokens ?? t9n("Unavailable")],
          [t9n("Output tokens"), s.usage.output_tokens ?? t9n("Unavailable")],
        ]
      : []),
  ]) {
    const row = html("div", "detail-row");
    row.append(html("dt", "", a), html("dd", "", String(b)));
    dl.append(row);
  }
  content.append(dl);
  if (task?.network) {
    const n = task.network;
    content.append(
      html("h3", "detail-section-title", t9n("Task browser network")),
    );
    const card = html("div", "window-resource");
    card.append(
      html("strong", "", networkText(n)),
      html(
        "small",
        "",
        [
          n.unsent ? t9n("{count} unsent", { count: n.unsent }) : "",
          n.navigations ? t9n("{count} navigations", { count: n.navigations }) : "",
          Number.isFinite(n.active_ms) ? t9n("{duration} active", { duration: format(n.active_ms) }) : "",
        ]
          .filter(Boolean)
          .join(" · ") || t9n("Aggregate counters"),
      ),
    );
    const types = Object.entries(n.by_resource_type || {}).sort(
      (a, b) => b[1] - a[1],
    );
    if (types.length)
      card.append(
        html(
          "p",
          "",
          types.map(([k, v]) => `${k} ${v.toLocaleString()}`).join(" · "),
        ),
      );
    content.append(card);
  }
  if (related.length) {
    content.append(
      html("h3", "detail-section-title", t9n("Recorded relationships")),
    );
    for (const e of related) {
      const neighbor = e.source === id ? e.target : e.source;
      const button = html(
        "button",
        "relationship-link",
        `${e.source === id ? "→" : "←"} ${e.kind} · ${short(neighbor, 36)}`,
      );
      button.title = neighbor;
      button.onclick = () => jumpToSpan(neighbor);
      content.append(button);
    }
  }
  content.append(
    html("h3", "detail-section-title", t9n("Resources during this interval")),
  );
  if (!stats?.resources?.length)
    content.append(
      html("p", "detail-note", t9n("No samples recorded within this interval.")),
    );
  else
    for (const r of stats.resources) {
      const card = html("div", "window-resource");
      card.append(
        html("strong", "", `${r.component} · ${short(r.runtime_id, 24)}`),
        html(
          "small",
          "",
          t9n("{attribution} · {count} samples", { attribution: r.attribution.replaceAll("_", " "), count: r.sample_count }),
        ),
      );
      for (const [key, label, fmt] of [
        ["cpu_percent", "CPU", (v) => v.toFixed(1) + "%"],
        ["memory_bytes", t9n("Memory"), bytes],
        ["gpu_percent", "GPU", (v) => v.toFixed(1) + "%"],
      ]) {
        const m = r[key];
        if (m?.sample_count)
          card.append(
            html(
              "p",
              "",
              t9n("{label}: {min} – {max} · mean {mean}", { label, min: fmt(m.min), max: fmt(m.max), mean: fmt(m.mean) }),
            ),
          );
      }
      content.append(card);
    }
  content.append(
    html(
      "p",
      "detail-note",
      t9n("Sample statistics describe concurrent runtime or task context, not resource cost caused by this span. Shared/device samples remain separate."),
    ),
  );
  if (s.attributes && Object.keys(s.attributes).length) {
    const details = html("details", "detail-attributes");
    details.append(html("summary", "", t9n("Recorded metadata")));
    for (const [a, b] of Object.entries(s.attributes)) {
      const row = html("div", "detail-row");
      row.append(html("dt", "", a), html("dd", "", String(b)));
      details.append(row);
    }
    content.append(details);
  }
  const focus = html(
    "button",
    "button detail-button",
    t9n("Focus this time interval"),
  );
  focus.disabled = durationUnknown;
  focus.onclick = () => {
    state.rangeStart = s.start_ms;
    state.rangeEnd = s.start_ms + s.duration_ms;
    state.pageOffset = 0;
    loadView();
  };
  content.append(focus);
  requestAnimationFrame(renderViews);
}
async function jumpToSpan(id) {
  state.task = "all";
  state.kind = "all";
  state.query = id.toLowerCase();
  $("#search").value = id;
  state.pageOffset = 0;
  state.rangeStart = 0;
  state.rangeEnd = null;
  await loadView();
  selectSpan(id);
}
function closeInspector() {
  state.selected = null;
  $("#inspector").hidden = true;
  document.body.classList.remove("inspecting");
  if (state.profile) requestAnimationFrame(renderViews);
}
function applyRange(start, end) {
  if (
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    start < 0 ||
    end <= start
  ) {
    notify(t9n("Choose a non-negative time window with end after start."));
    return;
  }
  state.rangeStart = start;
  state.rangeEnd = end;
  state.pageOffset = 0;
  state.zoom = 1;
  $("#zoom").value = 1;
  loadView();
}
for (const b of document.querySelectorAll("[data-view]"))
  b.onclick = () => {
    state.view = b.dataset.view;
    renderViews();
  };
$("#run-picker").onchange = async (e) => {
  state.runKey = e.target.value;
  resetFilters();
  $("#resource-filter").replaceChildren(new Option(t9n("All runtimes"), "all"));
  updateRunPickers();
  await loadView();
};
$("#run-picker-mobile").onchange = $("#run-picker").onchange;
$("#search").oninput = (e) => {
  state.query = e.target.value.trim().toLowerCase();
  state.pageOffset = 0;
  clearTimeout(searchTimer);
  searchTimer = setTimeout(loadView, 160);
};
$("#kind-filter").onchange = (e) => {
  state.kind = e.target.value;
  state.pageOffset = 0;
  loadView();
};
$("#resource-filter").onchange = (e) => {
  state.runtime = e.target.value;
  loadView();
};
$("#zoom").oninput = (e) => {
  state.zoom = Number(e.target.value);
  renderViews();
};
$("#reset-time").onclick = () => {
  state.zoom = 1;
  $("#zoom").value = 1;
  $("#timeline-scroll").scrollLeft = 0;
  renderViews();
};
$("#apply-range").onclick = () =>
  applyRange(
    Number($("#range-start").value) * 1000,
    Number($("#range-end").value) * 1000,
  );
$("#full-range").onclick = () => {
  state.rangeStart = 0;
  state.rangeEnd = null;
  state.pageOffset = 0;
  loadView();
};
$("#page-prev").onclick = () => {
  state.pageOffset = Math.max(0, state.pageOffset - state.pageLimit);
  loadView();
};
$("#page-next").onclick = () => {
  state.pageOffset += state.pageLimit;
  loadView();
};
$("#page-size").onchange = (e) => {
  state.pageLimit = Number(e.target.value);
  state.pageOffset = 0;
  loadView();
};
$("#highlight-critical").onclick = () => {
  state.critical = !state.critical;
  $("#highlight-critical").setAttribute("aria-pressed", String(state.critical));
  renderViews();
};
$("#compare-button").onclick = compareRuns;
$("#fit-graph").onclick = () => {
  const svg = $("#dag"),
    w = svg.viewBox.baseVal.width,
    h = svg.viewBox.baseVal.height,
    viewport = $("#dag-scroll");
  const scale = Math.min(
    1,
    viewport.clientWidth / w,
    viewport.clientHeight / h,
  );
  svg.setAttribute("width", Math.max(1, w * scale));
  svg.setAttribute("height", Math.max(1, h * scale));
  viewport.scrollTo(0, 0);
};
$("#overview").onclick = () => {
  if (!state.profile) return;
  resetFilters();
  loadView();
};
$("#close-inspector").onclick = closeInspector;
$("#load-source").onclick = loadSource;
$("#pause-live").onclick = () => {
  state.paused = !state.paused;
  setLiveStatus();
  if (!state.paused) loadSource();
};
$("#import-button").onclick = () => $("#import-file").click();
$("#import-file").onchange = async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    if (file.size > 32 * 1024 * 1024)
      throw Error(
        t9n("Profile exceeds browser import limit of 32 MiB. Use --run for a larger source."),
      );
    const p = validate(JSON.parse(await file.text()));
    const id = "import-" + Date.now();
    state.imports.set(id, p);
    state.runKey = id;
    resetFilters();
    updateRunPickers();
    await loadView();
    notify(t9n("Profile imported locally and available for comparison."));
  } catch (err) {
    notify(t9n("Import failed: {error}", { error: err.message }));
  } finally {
    e.target.value = "";
  }
};
$("#export-button").onclick = async () => {
  if (!state.profile) return;
  try {
    const p =
      state.imports.get(state.runKey) ||
      (await json("/api/profile?run=" + encodeURIComponent(state.runKey)));
    const url = URL.createObjectURL(
        new Blob([JSON.stringify(p, null, 2) + "\n"], {
          type: "application/json",
        }),
      ),
      link = document.createElement("a");
    link.href = url;
    link.download = "tracex-profile.json";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    notify(
      t9n("Complete profile exported, including spans outside the current page."),
    );
  } catch (e) {
    notify(t9n("Export failed: {error}", { error: e.message }));
  }
};
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closeInspector();
  if (
    e.key === "/" &&
    !["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement.tagName)
  ) {
    e.preventDefault();
    $("#search").focus();
  }
});
let resizeTimer;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(renderViews, 150);
});
window.addEventListener("beforeunload", () => state.stream?.close());
window.addEventListener("tracex:open-run", async (event) => {
  const id = event.detail?.run_id;
  if (!id) return;
  try {
    const data = await json("/api/runs");
    state.runs = data.runs || [];
    state.watchInterval = data.watch_interval_ms || 0;
    if (!state.runs.some((run) => run.id === id)) return;
    state.runKey = id;
    resetFilters();
    $("#resource-filter").replaceChildren(new Option(t9n("All runtimes"), "all"));
    updateRunPickers();
    await loadView();
    connectEvents();
  } catch (error) {
    showError(error);
  }
});
{
  const i18n = globalThis.TraceXI18n;
  for (const [code, name, label] of i18n.languages) {
    const button = html("button", code === i18n.language ? "selected" : "", label);
    button.lang = code;
    button.title = name;
    button.setAttribute("aria-label", name);
    button.setAttribute("aria-pressed", String(code === i18n.language));
    button.onclick = () => code !== i18n.language && i18n.setLanguage(code);
    $("#language-switch").append(button);
  }
}
loadSource();
