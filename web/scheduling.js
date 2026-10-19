/* Scheduling workspace: occurrence lanes, concurrency, resources and an
   interval impact table that share one analysis window. */
(() => {
  "use strict";
  const LABEL = 190, RIGHT = 16, ROW = 14, LANE_GAP = 8;
  const phaseColors = {
    queue: "#d9822b", "sandbox start": "#9aa7b0", prepare: "#b7c0c6", "bridge start": "#b7c0c6", "harness start": "#b7c0c6",
    execute: "#5b7f95", isolation: "#b7c0c6", evaluate: "#6b7d2a", cleanup: "#b7c0c6", unknown: "#e7e3d8",
  };
  const workColors = { model: colors.model, tool: colors.tool, suspended: colors.suspended || "#b9b3e6" };
  const seriesColors = {
    "Admitted occurrences": "#147b73", "Occurrences in a model call": colors.model, "Occurrences in a tool call": colors.tool,
    "Paused sandboxes": "#9d93d8", "Queued occurrences": "#d9822b", "Task runtime CPU": "#147b73", "Shared runtime CPU": "#8b887c",
    "Task runtime memory": "#147b73", "Shared runtime memory": "#8b887c", "Throttled period share": "#c0392b",
  };
  const view = { window: null, history: [], pinned: new Set(), selected: null, sort: "start", data: null, request: 0, controller: null };
  const $s = (sel) => document.querySelector("#scheduling-workspace " + sel);
  const runID = () => allRuns().find((r) => r.id === state.runKey)?.run_id;
  const jump = (detail) => window.dispatchEvent(new CustomEvent("tracex:evidence", { detail }));
  const visible = () => !$("#scheduling-workspace").hidden;
  const fmtCount = (v) => (v == null ? "—" : Number.isInteger(v) ? String(v) : v.toFixed(1));
  const pct = (v) => (v == null ? t9n("unknown") : (100 * v).toFixed(1) + "%");
  const dur = (v) => (v == null ? t9n("unknown") : format(v));
  // Occurrence IDs share long task prefixes; keep the distinguishing tail.
  const tail = (id, n = 16) => (id.length > n ? "…" + id.slice(-(n - 1)) : id);

  function geometry() {
    const width = Math.max(600, $s(".scheduling-body").clientWidth - 2);
    return { width, plot: width - LABEL - RIGHT };
  }
  function bounds() {
    const total = state.profile?.duration_ms || 1;
    return view.window || [0, total];
  }
  const xOf = (t, [lo, hi], plot) => LABEL + ((t - lo) / (hi - lo)) * plot;

  async function load() {
    if (!visible() || !state.runKey || !state.profile) return;
    const [lo, hi] = bounds(), { plot } = geometry(), request = ++view.request;
    const bins = Math.max(20, Math.min(400, Math.floor(plot / 3)));
    view.controller?.abort();
    $s(".scheduling-status").textContent = t9n("Loading…");
    try {
      let data;
      const local = state.imports.get(state.runKey);
      if (local) {
        data = TraceXScheduling.schedule(local, { start_ms: lo, end_ms: hi, bins, width: plot, pinned: [...view.pinned] });
      } else {
        view.controller = new AbortController();
        const params = new URLSearchParams({ run: state.runKey, start_ms: lo, end_ms: hi, bins, width: Math.round(plot) });
        if (view.pinned.size) params.set("pinned", [...view.pinned].join(","));
        const response = await fetch("/api/scheduling?" + params, { signal: view.controller.signal });
        if (!response.ok) throw new Error((await response.json()).error || response.statusText);
        data = (await response.json()).schedule;
      }
      if (request !== view.request) return;
      view.data = data;
      render();
    } catch (e) {
      if (e.name !== "AbortError" && request === view.request) $s(".scheduling-status").textContent = t9n("Scheduling projection unavailable: {error}", { error: e.message });
    }
  }

  function setWindow(lo, hi, remember = true) {
    const total = state.profile?.duration_ms || 1;
    lo = Math.max(0, lo);
    hi = Math.min(total, hi);
    if (!(hi - lo > 0.001)) return;
    if (remember) view.history.push(view.window);
    view.window = lo <= 0 && hi >= total ? null : [lo, hi];
    load();
  }

  // Dragging across any chart selects the shared window for every panel.
  function brush(svg, height) {
    svg.onpointerdown = (event) => {
      if (event.target.closest("[data-task]") && event.target.closest(".lane-label")) return;
      const rect = svg.getBoundingClientRect(), { plot } = geometry(), win = bounds();
      const x0 = Math.min(LABEL + plot, Math.max(LABEL, event.clientX - rect.left));
      const band = node("rect", { x: x0, y: 0, width: 1, height, class: "scheduling-brush", "pointer-events": "none" });
      svg.append(band);
      svg.setPointerCapture(event.pointerId);
      svg.onpointermove = (m) => {
        const x = Math.min(LABEL + plot, Math.max(LABEL, m.clientX - rect.left));
        band.setAttribute("x", Math.min(x0, x));
        band.setAttribute("width", Math.abs(x - x0));
      };
      svg.onpointerup = (u) => {
        const x = Math.min(LABEL + plot, Math.max(LABEL, u.clientX - rect.left));
        band.remove();
        svg.onpointermove = svg.onpointerup = null;
        if (Math.abs(x - x0) < 6) return;
        const t = (px) => win[0] + ((px - LABEL) / plot) * (win[1] - win[0]);
        setWindow(t(Math.min(x0, x)), t(Math.max(x0, x)));
      };
    };
  }

  function axis(svg, win, plot, y) {
    for (let i = 0; i <= 6; i++) {
      const t = win[0] + ((win[1] - win[0]) * i) / 6, x = xOf(t, win, plot);
      svg.append(node("line", { x1: x, x2: x, y1: 0, y2: y, class: "scheduling-grid" }),
        node("text", { x, y: y + 12, "text-anchor": i === 0 ? "start" : i === 6 ? "end" : "middle", class: "scheduling-tick" }, format(t)));
    }
  }

  function chart(target, series, { unit, height = 120 }) {
    target.replaceChildren();
    if (!series.length) {
      target.append(html("p", "workspace-empty", t9n("Not recorded for this run.")));
      return;
    }
    const { width, plot } = geometry(), win = [view.data.start_ms, view.data.end_ms], bins = series[0].mean.length;
    const scale = unit === "bytes" ? 2 ** 30 : 1;
    let top = 0;
    for (const s of series) for (const v of s.max) if (v != null) top = Math.max(top, v / scale);
    top = unit === "share" ? 1 : Math.max(top, unit === "count" ? 1 : 0.1) * 1.1;
    const h = height, svg = node("svg", { width, height: h + 18, viewBox: `0 0 ${width} ${h + 18}`, class: "scheduling-chart" });
    axis(svg, win, plot, h);
    const y = (v) => h - (v / scale / top) * (h - 6);
    svg.append(node("text", { x: LABEL - 8, y: 10, "text-anchor": "end", class: "scheduling-tick" },
      unit === "bytes" ? `${top.toFixed(1)} GiB` : unit === "share" ? "100%" : unit === "cores" ? `${top.toFixed(1)} ${t9n("cores")}` : fmtCount(top)));
    const legend = html("div", "scheduling-legend");
    for (const s of series) {
      const color = seriesColors[s.name] || colors.other;
      // Mean as a line, the per-bin peak as a faint band; gaps stay gaps.
      let d = "", peak = "";
      s.mean.forEach((v, b) => {
        if (v == null) return;
        const x = LABEL + ((b + 0.5) / bins) * plot, prev = b > 0 && s.mean[b - 1] != null;
        d += `${prev ? "L" : "M"}${x.toFixed(1)},${y(v).toFixed(1)}`;
        if (s.max[b] != null && s.max[b] !== v) peak += `M${x.toFixed(1)},${y(v).toFixed(1)}L${x.toFixed(1)},${y(s.max[b]).toFixed(1)}`;
      });
      if (peak) svg.append(node("path", { d: peak, stroke: color, "stroke-opacity": 0.25, "stroke-width": Math.max(1, plot / bins - 1) }));
      svg.append(node("path", { d, fill: "none", stroke: color, "stroke-width": 1.6 }));
      const item = html("span", "scheduling-legend-item");
      item.append(html("i", ""), t9n(s.name));
      item.firstChild.style.background = color;
      legend.append(item);
    }
    brush(svg, h);
    target.append(legend, svg);
  }

  function lanes(target) {
    target.replaceChildren();
    const data = view.data, { width, plot } = geometry(), win = [data.start_ms, data.end_ms];
    const rows = [...data.lanes];
    const impact = new Map(data.impact.map((r) => [r.task_id, r]));
    const key = { start: (l) => l.start_ms, queue: (l) => -(impact.get(l.task_id)?.queue_ms ?? -1), duration: (l) => -(l.duration_ms || 0),
      slowdown: (l) => -(impact.get(l.task_id)?.slowdown ?? -1) }[view.sort];
    rows.sort((a, b) => (a.pinned !== b.pinned ? (a.pinned ? -1 : 1) : key(a) - key(b)));
    const laneH = ROW * 2 + LANE_GAP, h = rows.length * laneH + 4;
    const svg = node("svg", { width, height: h + 18, viewBox: `0 0 ${width} ${h + 18}`, class: "scheduling-lanes" });
    axis(svg, win, plot, h);
    rows.forEach((lane, i) => {
      const y = i * laneH + 2, selected = view.selected === lane.task_id;
      const g = node("g", { "data-task": lane.task_id, class: `scheduling-lane ${selected ? "selected" : ""} ${view.selected && !selected ? "dimmed" : ""}` });
      g.append(node("rect", { x: 0, y: y - 2, width, height: laneH, class: "lane-background" }));
      const label = node("g", { class: "lane-label", role: "button", tabindex: 0, "aria-label": t9n("Select {name}", { name: lane.task_id }) });
      label.append(node("text", { x: 8, y: y + 11, class: "scheduling-lane-name" }, (lane.pinned ? "📌 " : "") + tail(lane.task_id, 24)),
        node("text", { x: 8, y: y + 24, class: "scheduling-tick" }, t9n(lane.status || "unknown")));
      label.onclick = () => select(lane.task_id);
      label.onkeydown = (e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(lane.task_id));
      g.append(label);
      const bar = (x, layer, color, title, extra = {}) => {
        const x0 = xOf(x.start_ms, win, plot), w = Math.max(1.5, (x.duration_ms / (win[1] - win[0])) * plot);
        const r = node("rect", { x: x0, y: y + layer * ROW, width: w, height: ROW - 3, rx: 2, fill: color, ...extra });
        r.append(node("title", {}, title));
        if (x.span_id) r.onclick = (e) => { e.stopPropagation(); jump({ run_id: runID(), span_id: x.span_id }); };
        g.append(r);
      };
      for (const x of lane.lifecycle)
        bar(x, 0, phaseColors[x.kind] || "#b7c0c6", `${t9n(x.kind)} · ${format(x.duration_ms)}${x.kind === "unknown" ? "\n" + t9n("No recorded phase covers this time.") : ""}`,
          x.kind === "unknown" ? { class: "unknown-phase" } : {});
      for (const x of lane.work)
        bar(x, 1, workColors[x.kind] || colors.other, `${t9n(x.kind === "suspended" ? "Sandbox paused" : x.kind)} · ${format(x.duration_ms)}${x.count > 1 ? " · " + t9n("{count} spans", { count: x.count }) : ""}`);
      svg.append(g);
    });
    brush(svg, h);
    target.append(svg);
    if (data.lanes_total > data.lanes.length)
      target.append(html("p", "scheduling-note", t9n("{shown} of {total} overlapping occurrences shown; narrow the window to see the rest.", { shown: data.lanes.length, total: data.lanes_total })));
  }

  function select(id) {
    view.selected = view.selected === id ? null : id;
    render();
  }
  function togglePin(id) {
    if (view.pinned.has(id)) view.pinned.delete(id);
    else if (view.pinned.size < 20) view.pinned.add(id);
    load();
  }

  function table(target) {
    target.replaceChildren();
    const rows = [...view.data.impact];
    const key = { start: (r) => 0, queue: (r) => -(r.queue_ms ?? -1), duration: (r) => -(r.duration_ms ?? -1), slowdown: (r) => -(r.slowdown ?? -1) }[view.sort];
    rows.sort((a, b) => key(a) - key(b));
    const t = html("table", "scheduling-table"), head = html("thead"), tr = html("tr"), body = html("tbody");
    for (const h of ["Occurrence", "Status", "In window", "Queue", "Model", "Tool", "Paused", "Unknown", "CPU core-s", "Resource coverage", "Throttled", "Failures", "Task duration", "vs same-task median", "Evidence"]) {
      const th = html("th", "", t9n(h));
      th.scope = "col";
      tr.append(th);
    }
    head.append(tr);
    for (const r of rows) {
      const row = html("tr", view.selected === r.task_id ? "selected" : "");
      const name = html("button", "link-button", tail(r.task_id, 20));
      name.title = r.task_id;
      name.onclick = () => select(r.task_id);
      const pin = html("button", "button small subtle", view.pinned.has(r.task_id) ? t9n("Unpin") : t9n("Pin"));
      pin.onclick = () => togglePin(r.task_id);
      const evidence = html("div", "scheduling-evidence");
      for (const [kind, label] of [["queue", "Queue"], ["model", "Model"], ["tool", "Tool"], ["paused", "Pause"], ["failure", "Failure"]]) {
        if (!r.evidence?.[kind]) continue;
        const b = html("button", "button small", t9n(label));
        b.onclick = () => jump({ run_id: runID(), span_id: r.evidence[kind] });
        evidence.append(b);
      }
      const task = html("button", "button small", t9n("Task"));
      task.onclick = () => jump({ run_id: runID(), task_id: r.task_id });
      evidence.append(task);
      const slowdown = r.slowdown != null ? `${r.slowdown.toFixed(2)}× (n=${r.baseline_n})` : r.baseline_n < 3 ? t9n("too few peers (n={n})", { n: r.baseline_n }) : t9n("not comparable");
      const cells = [null, t9n(r.status || "unknown"), format(r.overlap_ms), dur(r.queue_ms), format(r.model_ms), format(r.tool_ms), format(r.paused_ms),
        dur(r.unknown_ms), r.cpu_core_s == null ? t9n("unknown") : r.cpu_core_s.toFixed(2), pct(r.resource_coverage), pct(r.throttled_share),
        String(r.failures), dur(r.duration_ms), slowdown, evidence];
      cells.forEach((value, i) => {
        const td = html("td");
        if (i === 0) td.append(name, pin);
        else if (value instanceof Node) td.append(value);
        else td.textContent = value;
        row.append(td);
      });
      body.append(row);
    }
    t.append(head, body);
    target.append(t);
  }

  function render() {
    if (!view.data) return;
    const d = view.data;
    $s(".scheduling-status").textContent = t9n("{start} → {end} · {count} overlapping occurrences", { start: format(d.start_ms), end: format(d.end_ms), count: d.lanes_total });
    $s(".scheduling-back").disabled = !view.history.length;
    $s(".scheduling-notes").replaceChildren(...d.notes.map((n) => html("li", "", t9n(n))));
    chart($s(".scheduling-concurrency"), d.series.filter((s) => s.group === "concurrency"), { unit: "count" });
    lanes($s(".scheduling-lane-panel"));
    chart($s(".scheduling-cpu"), d.series.filter((s) => s.unit === "cores"), { unit: "cores", height: 90 });
    chart($s(".scheduling-memory"), d.series.filter((s) => s.unit === "bytes"), { unit: "bytes", height: 80 });
    chart($s(".scheduling-throttle"), d.series.filter((s) => s.unit === "share"), { unit: "share", height: 60 });
    table($s(".scheduling-impact"));
  }

  $s(".scheduling-back").onclick = () => {
    if (!view.history.length) return;
    view.window = view.history.pop();
    load();
  };
  $s(".scheduling-reset").onclick = () => setWindow(-Infinity, Infinity);
  $s(".scheduling-sort").onchange = (e) => {
    view.sort = e.target.value;
    render();
  };
  window.addEventListener("tracex:navigated", (e) => e.detail?.page === "scheduling" && load());
  // A new run starts from the whole run; live revisions refresh in place.
  let shownRun = null;
  window.addEventListener("tracex:render", () => {
    if (state.runKey !== shownRun) {
      shownRun = state.runKey;
      Object.assign(view, { window: null, history: [], pinned: new Set(), selected: null, data: null });
    }
    load();
  });
  let resizeTimer;
  window.addEventListener("resize", () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => visible() && load(), 200);
  });
})();
