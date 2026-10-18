/* Shareable investigation state. Run identities refer to source IDs, not mount order. */
(function (root) {
  "use strict";
  const pages = ["experiments", "summary", "insights", "profiler", "tasks", "configuration", "evidence", "compare", "sources"];
  function parse(hash) {
    const p = new URLSearchParams(hash.replace(/^#/, ""));
    const number = (key, fallback, min, max) => {
      if (!p.has(key)) return fallback;
      const n = Number(p.get(key));
      return Number.isFinite(n) && n >= min && n <= max ? n : fallback;
    };
    const start = number("from", 0, 0, Number.MAX_SAFE_INTEGER);
    const end = number("to", null, 0, Number.MAX_SAFE_INTEGER);
    return {
      page: pages.includes(p.get("page")) ? p.get("page") : "experiments",
      run: p.get("run") || "", task: p.get("task") || "all",
      kind: p.get("kind") || "all", query: p.get("q") || "",
      runtime: p.get("runtime") || "all", selected: p.get("span") || null,
      view: ["split", "dag", "timeline"].includes(p.get("view")) ? p.get("view") : "timeline",
      rangeStart: end != null && end <= start ? 0 : start,
      rangeEnd: end != null && end <= start ? null : end,
      zoom: number("zoom", 1, 1, 8),
      pageOffset: Math.floor(number("offset", 0, 0, Number.MAX_SAFE_INTEGER)),
      pageLimit: [50, 250, 500].includes(Number(p.get("limit"))) ? Number(p.get("limit")) : 250,
      baseline: p.get("baseline") || "", candidate: p.get("candidate") || "",
    };
  }
  function serialize(s) {
    const p = new URLSearchParams({page: s.page});
    const add = (key, value, fallback) => {
      if (value != null && value !== "" && value !== fallback) p.set(key, String(value));
    };
    add("run", s.run); add("task", s.task, "all"); add("kind", s.kind, "all");
    add("q", s.query); add("runtime", s.runtime, "all"); add("span", s.selected);
    add("view", s.view, "timeline"); add("from", s.rangeStart, 0); add("to", s.rangeEnd);
    add("zoom", s.zoom, 1); add("offset", s.pageOffset, 0); add("limit", s.pageLimit, 250);
    add("baseline", s.baseline); add("candidate", s.candidate);
    return "#" + p.toString();
  }
  function matches(span, query) {
    const q = (query || "").trim().toLowerCase();
    return !!q && [span.name, span.id, span.agent_id, span.task_id, span.kind].join(" ").toLowerCase().includes(q);
  }
  function find(spans, opts) {
    const context = spans.filter(s => (!opts.task || opts.task === "all" || s.task_id === opts.task)
      && (!opts.kind || opts.kind === "all" || s.kind === opts.kind)
      && s.start_ms + s.duration_ms >= (opts.rangeStart || 0)
      && s.start_ms <= (opts.rangeEnd ?? Infinity))
      .sort((a,b) => a.start_ms - b.start_ms || a.id.localeCompare(b.id));
    let total = 0, result = {total:0, index:0, span_id:"", offset:0};
    context.forEach((s, offset) => {
      if (!matches(s, opts.query)) return;
      if (total === 0 || total === opts.searchIndex) result = {index:total, span_id:s.id, offset};
      total++;
    });
    return {...result, total};
  }
  const api = {parse, serialize, matches, find};
  if (typeof module !== "undefined") module.exports = api;
  else root.TraceXWorkspace = api;
})(globalThis);
