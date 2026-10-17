"use strict";
const { validate, union, maximum } = globalThis.TraceXProfile;
const $ = (s) => document.querySelector(s);
const NS = "http://www.w3.org/2000/svg";
const colors = {
  agent: "#218b80",
  model: "#7075d8",
  tool: "#d79a45",
  browser: "#4998bf",
  evaluation: "#87978a",
  task: "#628d89",
  other: "#8b9697",
};
const state = {
  profile: null,
  task: "all",
  kind: "all",
  query: "",
  selected: null,
  view: "split",
  zoom: 1,
  runtime: "all",
  imported: false,
};
let toastTimer;
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
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)} s`;
  return `${Math.floor(ms / 60000)}m ${((ms % 60000) / 1000).toFixed(1)}s`;
}
function bytes(n) {
  if (n === null || n === undefined) return "Unavailable";
  if (n >= 1073741824) return `${(n / 1073741824).toFixed(2)} GiB`;
  return `${(n / 1048576).toFixed(1)} MiB`;
}
function kind(s) {
  return s.kind || "other";
}
function color(s) {
  return colors[kind(s)] || colors.other;
}
function short(s, n = 26) {
  s = String(s || "");
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}
function visible() {
  return state.profile.spans.filter(
    (s) =>
      (state.task === "all" || s.task_id === state.task) &&
      (state.kind === "all" || kind(s) === state.kind) &&
      (!state.query ||
        [s.name, s.agent_id, s.id, s.task_id]
          .join(" ")
          .toLowerCase()
          .includes(state.query)),
  );
}
function edges() {
  const p = state.profile;
  return (
    p.edges ||
    p.spans
      .filter((s) => s.parent_id)
      .map((s) => ({ source: s.parent_id, target: s.id, kind: "parent" }))
  );
}
function setProfile(p, imported = false) {
  validate(p);
  state.profile = p;
  state.imported = imported;
  state.task = "all";
  state.kind = "all";
  state.query = "";
  state.selected = null;
  state.zoom = 1;
  state.runtime = "all";
  $("#search").value = "";
  $("#zoom").value = 1;
  closeInspector();
  $("#notice").hidden = true;
  render();
}
async function loadSource() {
  try {
    const res = await fetch("/api/profile");
    if (!res.ok) throw Error(`Source returned HTTP ${res.status}`);
    setProfile(await res.json());
  } catch (e) {
    $("#notice").textContent = `Could not load profile: ${e.message}`;
    $("#notice").hidden = false;
    $("#run-name").textContent = "Profile unavailable";
    $("#run-state").textContent = "Load failed";
  }
}
function render() {
  const p = state.profile;
  $("#run-name").textContent = p.name || "Untitled workload";
  $("#run-id").textContent = short(p.run_id || "Imported profile", 65);
  const synthetic = (p.name + " " + p.run_id + " " + p.warnings.join(" "))
    .toLowerCase()
    .includes("synthetic");
  $("#dataset-badge").textContent = synthetic
    ? "Synthetic demo"
    : state.imported
      ? "Imported profile"
      : "ARIES artifacts";
  $("#run-state").textContent = synthetic
    ? "Demo dataset"
    : "Recorded evidence";
  $("#metric-duration").textContent = format(p.duration_ms);
  const agentCount = new Set(p.spans.map((s) => s.agent_id).filter(Boolean))
    .size;
  $("#metric-agents").textContent = agentCount || "—";
  $("#metric-spans").textContent = p.spans.length.toLocaleString();
  $("#metric-span-note").textContent =
    `Across ${p.tasks.length} task occurrence${p.tasks.length === 1 ? "" : "s"}`;
  const kindTime = (k) => {
    const spans = p.spans.filter((s) => s.kind === k);
    return spans.length ? format(union(spans)) : "—";
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
    html("span", "task-name", "All task occurrences"),
  );
  all.onclick = () => setTask("all");
  list.append(all);
  for (const t of p.tasks) {
    const b = html(
      "button",
      `task-button ${state.task === t.id ? "selected" : ""}`,
    );
    b.title = t.id;
    b.setAttribute("aria-pressed", String(state.task === t.id));
    b.append(
      html("span", `status-dot ${t.status === "failed" ? "failed" : ""}`),
      html("span", "task-name", t.name || t.id),
      html("span", "task-duration", format(t.duration_ms)),
    );
    b.onclick = () => setTask(t.id);
    list.append(b);
  }
  const select = $("#kind-filter");
  select.replaceChildren(new Option("All span types", "all"));
  for (const k of [...new Set(p.spans.map(kind))].sort())
    select.add(new Option(k[0].toUpperCase() + k.slice(1), k));
  select.value = state.kind;
  const caps = $("#capability-list");
  caps.replaceChildren();
  const mainCapabilities = p.capabilities.filter(
    (c) => !c.name.startsWith("monitor/"),
  );
  for (const c of mainCapabilities.slice(0, 4)) {
    const a = html(
      "div",
      `capability ${["unsupported", "not_enabled", "failed", "unavailable"].includes(c.status) ? "unavailable" : ""}`,
    );
    a.append(html("span", "status-dot"), html("span", "", c.name));
    a.title = `${c.status}${c.reason ? ": " + c.reason : ""}`;
    caps.append(a);
  }
  const runtime = $("#resource-filter");
  runtime.replaceChildren(new Option("All runtimes", "all"));
  const runtimeNames = new Map();
  for (const r of p.resources)
    runtimeNames.set(
      r.runtime_id || r.component,
      r.component + (r.runtime_id ? " · " + short(r.runtime_id, 14) : ""),
    );
  for (const [k, v] of runtimeNames) runtime.add(new Option(v, k));
  runtime.value = state.runtime;
  const cov = $("#coverage");
  cov.replaceChildren();
  const intro = html("p");
  intro.append(
    html("strong", "", synthetic ? "Synthetic demo. " : "Evidence coverage. "),
    document.createTextNode(
      synthetic
        ? "These agents, dependencies, and measurements are generated to demonstrate the profiler."
        : "Graph edges express recorded parent relationships. Timing overlap alone does not establish a dependency.",
    ),
  );
  cov.append(intro);
  for (const w of p.warnings) {
    if (synthetic && String(w).toLowerCase().includes("synthetic")) continue;
    cov.append(html("p", "", String(w)));
  }
  if (p.capabilities.length) {
    const details = html("details", "coverage-details");
    details.append(
      html(
        "summary",
        "",
        `Inspect ${p.capabilities.length} source coverage reports`,
      ),
    );
    for (const c of p.capabilities)
      details.append(
        html(
          "p",
          "",
          `${c.name}: ${c.status}${c.reason ? " · " + c.reason : ""}`,
        ),
      );
    cov.append(details);
  }
  renderViews();
}
function setTask(id) {
  state.task = id;
  state.selected = null;
  closeInspector();
  render();
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
  const displayed = spans.slice(0, 350),
    ids = new Set(displayed.map((s) => s.id));
  const es = edges().filter((e) => ids.has(e.source) && ids.has(e.target));
  $("#dag-note").textContent =
    spans.length > 350
      ? "Showing first 350 nodes; filter to explore"
      : es.length
        ? `${es.length} explicit relationship${es.length === 1 ? "" : "s"}`
        : "No recorded dependencies · isolated spans";
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
  marker.append(node("path", { d: "M 0 0 L 10 5 L 0 10 z", fill: "#a8bbaa" }));
  defs.append(marker);
  svg.append(defs);
  for (const [d] of columns)
    svg.append(
      node(
        "text",
        { x: 28 + d * 225, y: 20, class: "graph-depth" },
        d === 0 ? "ROOT / UNLINKED" : `DEPTH ${d}`,
      ),
    );
  for (const e of es) {
    const a = pos.get(e.source),
      b = pos.get(e.target);
    const sx = a.x + 190,
      sy = a.y + 34,
      tx = b.x,
      ty = b.y + 34;
    svg.append(
      node("path", {
        d: `M${sx},${sy} C${sx + 20},${sy} ${tx - 20},${ty} ${tx - 6},${ty}`,
        class: `dag-edge ${state.selected === e.source || state.selected === e.target ? "focused" : ""}`,
        "marker-end": "url(#arrow)",
      }),
    );
  }
  for (const s of displayed) {
    const { x, y } = pos.get(s.id);
    const g = node("g", {
      class: `dag-node ${s.status === "failed" || s.status === "ERROR" ? "failed" : ""} ${state.selected === s.id ? "selected" : ""}`,
      transform: `translate(${x},${y})`,
      tabindex: 0,
      role: "button",
      "aria-label": `${s.agent_id || "Agent"}: ${s.name}, ${format(s.duration_ms)}`,
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
        short(s.agent_id || s.task_id || "Unassigned", 26),
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
        format(s.duration_ms),
      ),
      node(
        "title",
        {},
        `${s.name}\n${s.agent_id || s.task_id || ""}\n${format(s.duration_ms)} · ${s.status || "unknown"}`,
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
function timeBounds() {
  const p = state.profile;
  return Math.max(
    p.duration_ms,
    maximum(p.spans.map((s) => s.start_ms + s.duration_ms)),
    maximum(p.tasks.map((t) => t.start_ms + t.duration_ms)),
    maximum(p.resources.map((r) => r.time_ms)),
    1,
  );
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
    total = timeBounds();
  const width = labelWidth + plotWidth + 25;
  const lanes = new Map();
  for (const s of spans.slice(0, 2500)) {
    const key = `${s.task_id || "run"}\0${s.agent_id || "unassigned"}`;
    if (!lanes.has(key))
      lanes.set(key, {
        name: s.agent_id || "Unassigned lane",
        task: s.task_id || "Run scope",
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
        format(t),
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
        fill: "#fff",
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
        stroke: "#f1f4ed",
      }),
    );
  }
  for (const { s, y } of rows) {
    const x = labelWidth + (s.start_ms / total) * plotWidth,
      w = Math.max(3, (s.duration_ms / total) * plotWidth);
    const bar = node("rect", {
      x,
      y,
      width: w,
      height: 20,
      rx: 3,
      fill: color(s),
      class: `timeline-bar ${s.status === "failed" || s.status === "ERROR" ? "failed" : ""} ${state.selected === s.id ? "selected" : ""}`,
      tabindex: 0,
      role: "button",
      "aria-label": `${s.name}, ${format(s.start_ms)} to ${format(s.start_ms + s.duration_ms)}`,
    });
    bar.append(
      node(
        "title",
        {},
        `${s.name}\n${format(s.start_ms)} → ${format(s.start_ms + s.duration_ms)}\nDuration: ${format(s.duration_ms)}`,
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
        "No matching execution spans.",
      ),
    );
  $("#time-window").textContent =
    `0 → ${format(total)}${spans.length > 2500 ? " · first 2,500 spans; filter to explore" : ""}`;
}
function drawResources() {
  const total = timeBounds();
  const selected = state.profile.resources.filter(
    (r) =>
      (state.task === "all" || r.task_id === state.task || !r.task_id) &&
      (state.runtime === "all" ||
        (r.runtime_id || r.component) === state.runtime),
  );
  drawChart($("#cpu-chart"), selected, "cpu_percent", total, "#438f7c");
  drawChart($("#memory-chart"), selected, "memory_bytes", total, "#8b91c6");
  const cpu = selected.map((r) => r.cpu_percent).filter(Number.isFinite),
    mem = selected.map((r) => r.memory_bytes).filter(Number.isFinite);
  $("#cpu-peak").textContent = cpu.length
    ? `${maximum(cpu).toFixed(1)}% peak`
    : "Not measured";
  $("#memory-peak").textContent = mem.length
    ? `${bytes(maximum(mem))} peak`
    : "Not measured";
}
function drawChart(svg, samples, key, total, tint) {
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
        "No measurements available",
      ),
    );
    return;
  }
  const max =
    Math.max(
      maximum(valid.map((r) => r[key])),
      key === "cpu_percent" ? 1 : 1048576,
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
        stroke: "#edf1e6",
        "stroke-dasharray": "3 3",
      }),
      node(
        "text",
        { x: left - 6, y: y + 3, "text-anchor": "end", class: "chart-axis" },
        key === "cpu_percent"
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
        format((total * i) / 4),
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
      const x = left + (r.time_ms / total) * (w - left - right),
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
          cx: left + (rs[0].time_ms / total) * (w - left - right),
          cy: h - bottom - (rs[0][key] / max) * (h - bottom - top),
          r: 2,
          fill: tint,
        }),
      );
  }
}
function selectSpan(id) {
  const s = state.profile.spans.find((s) => s.id === id);
  if (!s) return;
  state.selected = id;
  $("#inspector").hidden = false;
  document.body.classList.add("inspecting");
  const content = $("#inspector-content");
  content.replaceChildren();
  const k = html("div", "detail-kind", kind(s));
  k.style.color = color(s);
  content.append(k, html("h2", "detail-title", s.name));
  const metric = html("div", "detail-metric", format(s.duration_ms));
  metric.append(html("small", "", "duration"));
  content.append(metric);
  const dl = html("dl");
  const task = state.profile.tasks.find((t) => t.id === s.task_id);
  for (const [a, b] of [
    ["Agent", s.agent_id || "Unknown"],
    ["Task", s.task_id || "Run scope"],
    ["Status", s.status || "Unknown"],
    ["Start", format(s.start_ms)],
    ["End", format(s.start_ms + s.duration_ms)],
    ["Source", s.source || "Unspecified"],
    ["Span ID", s.id],
    ["Parent", s.parent_id || "No recorded parent"],
    ...(task
      ? [
          ["Evaluation", task.evaluation_status || "Unknown"],
          ["Cleanup", task.cleanup_status || "Unknown"],
        ]
      : []),
  ]) {
    const row = html("div", "detail-row");
    row.append(html("dt", "", a), html("dd", "", b));
    dl.append(row);
  }
  content.append(dl);
  if (s.attributes && Object.keys(s.attributes).length) {
    const title = html("p", "detail-note", "Recorded attributes");
    content.append(title);
    for (const [a, b] of Object.entries(s.attributes)) {
      const row = html("div", "detail-row");
      row.append(
        html("dt", "", a),
        html("dd", "", typeof b === "object" ? JSON.stringify(b) : String(b)),
      );
      content.append(row);
    }
  }
  content.append(
    html(
      "p",
      "detail-note",
      "DAG relationships come from explicit span references. Concurrent execution does not imply causality.",
    ),
  );
  const focus = html("button", "button detail-button", "Focus this agent");
  focus.onclick = () => {
    state.query = (s.agent_id || s.name).toLowerCase();
    $("#search").value = s.agent_id || s.name;
    renderViews();
  };
  content.append(focus);
  requestAnimationFrame(renderViews);
}
function closeInspector() {
  state.selected = null;
  $("#inspector").hidden = true;
  document.body.classList.remove("inspecting");
  if (state.profile) requestAnimationFrame(renderViews);
}
for (const b of document.querySelectorAll("[data-view]"))
  b.onclick = () => {
    state.view = b.dataset.view;
    renderViews();
  };
$("#search").oninput = (e) => {
  state.query = e.target.value.trim().toLowerCase();
  renderViews();
};
$("#kind-filter").onchange = (e) => {
  state.kind = e.target.value;
  renderViews();
};
$("#resource-filter").onchange = (e) => {
  state.runtime = e.target.value;
  drawResources();
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
$("#fit-graph").onclick = () => {
  const svg = $("#dag"),
    w = svg.viewBox.baseVal.width,
    h = svg.viewBox.baseVal.height;
  const viewport = $("#dag-scroll");
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
  state.task = "all";
  state.query = "";
  state.kind = "all";
  $("#search").value = "";
  closeInspector();
  render();
};
$("#close-inspector").onclick = closeInspector;
$("#load-source").onclick = loadSource;
$("#import-button").onclick = () => $("#import-file").click();
$("#import-file").onchange = async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    if (file.size > 32 * 1024 * 1024)
      throw Error("Profile file exceeds 32 MiB.");
    setProfile(JSON.parse(await file.text()), true);
    notify("Profile imported locally.");
  } catch (err) {
    notify(`Import failed: ${err.message}`);
  } finally {
    e.target.value = "";
  }
};
$("#export-button").onclick = () => {
  if (!state.profile) return;
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(state.profile, null, 2) + "\n"], {
      type: "application/json",
    }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = "tracex-profile.json";
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  notify("Normalized profile exported.");
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
loadSource();
