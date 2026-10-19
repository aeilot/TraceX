/* Workspace navigation and run/task discovery, sharing the existing profiler model. */
(() => {
  "use strict";
  const model = globalThis.TraceXWorkspace;
  let restored = model.parse(location.hash);
  let page = restored.page, restoring = true;
  const updateRunTabs = TraceXMotion.tabs($("#run-tabs"), "[data-page]");
  const updateViewTabs = TraceXMotion.tabs($(".explorer-toolbar .view-tabs"), "[data-view]");
  const selectedRuns = new Set();
  const runPages = ["summary", "insights", "scheduling", "profiler", "tasks", "configuration", "evidence"];
  const names = {
    experiments: "Experiments", insights: "Insights", scheduling: "Scheduling", summary: "Overview", profiler: "Profiler",
    tasks: "Tasks", configuration: "Configuration", evidence: "Evidence",
    compare: "Compare", sources: "Sources & runner",
  };
  const descriptions = {
    insights: "Inspect evidence, compare tasks and record your conclusions.",
    scheduling: "Find which occurrences competed in a window and which slowed down.",
    experiments: "Open a run to investigate, or select two runs to compare.",
    summary: "Run metrics, work breakdown and recorded critical path.",
    profiler: "Explore execution and resource signals in one workspace.",
    tasks: "Find slow or failed tasks, then inspect their execution.",
    configuration: "Only conditions recorded by the source are shown.",
    evidence: "Inspect measurement coverage and source limitations.",
    compare: "Compare recorded conditions and performance across runs.",
    sources: "Inspect mounted sources and the configured execution target.",
  };
  const show = (selector, allowed) => {
    const el = $(selector), wasHidden = el.hidden;
    el.hidden = !allowed.includes(page);
    if (wasHidden && !el.hidden && !restoring) TraceXMotion.enter(el);
  };
  function writeURL() {
    if (restoring) return;
    const runs = allRuns(), current = runs.find(r => r.id === state.runKey);
    const baseline = runs.find(r => r.id === $("#compare-left").value)?.run_id;
    const candidate = runs.find(r => r.id === $("#compare-right").value)?.run_id;
    const hash = model.serialize({...state, page, run: current?.run_id,
      baseline: page === "compare" ? baseline : "", candidate: page === "compare" ? candidate : ""});
    if (hash !== location.hash) history.replaceState(null, "", hash);
  }
  function navigate(next, {historyEntry = true} = {}) {
    const changed = page !== next;
    page = next;
    document.body.dataset.workspace = page;
    $("#workspace-title").textContent = t9n(names[page]);
    $(".page-description").textContent = t9n(descriptions[page]);
    $(".breadcrumb strong").textContent = t9n(names[page]);
    $(".eyebrow").textContent = t9n(runPages.includes(page) ? "EXPERIMENT RECORD" : "AGENT WORKLOAD PROFILER");
    show("#experiments-workspace", ["experiments"]);
    show("#run-tabs", runPages);
    show(".run-card", runPages);
    show(".analysis-card", ["summary"]);
    show("#insight-overview", ["summary"]);
    show("#insights-workspace", ["insights"]);
    show("#scheduling-workspace", ["scheduling"]);
    show(".comparison-card", ["compare"]);
    show(".explorer", ["profiler"]);
    show(".resources-card", ["profiler"]);
    show("#coverage", ["evidence"]);
    show("#tasks-workspace", ["tasks"]);
    show("#configuration-workspace", ["configuration"]);
    show("#sources-workspace", ["sources"]);
    if (page !== "experiments") {
      TraceXMotion.visible($("#experiment-panel"), false, {immediate:true});
      $("#new-experiment").setAttribute("aria-expanded", "false");
    }
    if (page !== "profiler" && state.selected) closeInspector();
    for (const b of document.querySelectorAll("[data-page]")) {
      const active = b.dataset.page === page || (b.id === "overview" && runPages.includes(page));
      b.classList.toggle("active", active);
      if (active) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
    }
    if (page === "compare") $("#comparison-details").open = true;
    if (state.profile && page === "profiler") requestAnimationFrame(renderViews);
    if (page === "sources") checkRunner();
    if (changed && !restoring) {
      TraceXMotion.enter($(".page-heading"));
      window.scrollTo({top:0, behavior:"instant"});
    }
    if (changed && historyEntry && !restoring) history.pushState(null, "", model.serialize({page}));
    writeURL();
    updateRunTabs(!restoring);
    window.dispatchEvent(new CustomEvent("tracex:navigated",{detail:{page}}));
  }
  function button(text, action, label) {
    const b = html("button", "button small", t9n(text));
    if (label) b.setAttribute("aria-label", label);
    b.onclick = action;
    return b;
  }
  function table(target, headings, rows) {
    const el = $(target); el.replaceChildren();
    if (!rows.length) { el.append(html("p", "workspace-empty", t9n("No matching records."))); return; }
    const t = html("table"), head = html("thead"), tr = html("tr");
    for (const label of headings) { const th = html("th", "", t9n(label)); th.scope = "col"; tr.append(th); }
    head.append(tr); const body = html("tbody");
    for (const row of rows) {
      const r = html("tr");
      for (const value of row) {
        const td = html("td");
        if (value instanceof Node) td.append(value); else td.textContent = String(value ?? "—");
        r.append(td);
      }
      body.append(r);
    }
    t.append(head, body); el.append(t);
  }
  async function openRun(id, next = "profiler", task = "all") {
    state.runKey = id; resetFilters(); state.task = task;
    $("#resource-filter").replaceChildren(new Option(t9n("All runtimes"), "all"));
    updateRunPickers(); navigate(next); await loadView();
  }
  function updateSelection() {
    $("#library-selection").textContent = t9n("{count} selected · select exactly two to compare", {count: selectedRuns.size});
    $("#library-compare").disabled = selectedRuns.size !== 2;
  }
  function renderLibrary() {
    const runs = allRuns(), q = $("#experiment-search").value.toLowerCase(), status = $("#experiment-state").value;
    for (const id of selectedRuns) if (!runs.some(r => r.id === id)) selectedRuns.delete(id);
    const shown = runs.filter(r => `${r.name} ${r.run_id} ${Object.values(r.configuration || {}).join(" ")}`.toLowerCase().includes(q)
      && (status === "all" || status === "error" && r.last_error || status === "live" && r.live || status === "snapshot" && !r.live));
    $("#library-count").textContent = t9n("{count} runs", {count: runs.length});
    updateSelection();
    table("#experiment-library", ["Select", "Run", "Source state", "Model", "Concurrency", "Duration", "Spans", "Action"], shown.map(r => {
      const box = html("input"); box.type = "checkbox"; box.checked = selectedRuns.has(r.id);
      box.setAttribute("aria-label", t9n("Select {name}", {name: r.name || r.run_id}));
      box.onchange = () => { if (box.checked) selectedRuns.add(r.id); else selectedRuns.delete(r.id); updateSelection(); };
      const identity = html("div", "record-identity"); identity.append(html("strong", "", r.name || r.run_id), html("small", "", r.run_id));
      return [box, identity, t9n(r.last_error ? "Source error" : r.live ? "Live source" : "Snapshot"), r.configuration?.model,
        r.configuration?.concurrency, format(r.duration_ms), r.span_count?.toLocaleString(), button("Open profiler", () => openRun(r.id), t9n("Open {name}", {name:r.name || r.run_id}))];
    }));
    table("#source-results", ["Run", "Source state", "Revision", "Source message", "Action"], runs.map(r => [r.name || r.run_id,
      t9n(r.last_error ? "Source error" : r.live ? "Live source" : "Snapshot"), r.revision ?? "—", r.last_error || t9n("No source errors reported"), button("Inspect evidence", () => openRun(r.id, "evidence"))]));
  }
  function renderTasks() {
    const q = $("#task-search").value.toLowerCase(), status = $("#task-status").value, sort = $("#task-sort").value;
    const tasks = [...(state.profile?.tasks || [])].filter(t => `${t.name} ${t.id}`.toLowerCase().includes(q)
      && (status === "all" || status === "failed" && failedTask(t) || status === "succeeded" && t.status === "succeeded"));
    tasks.sort((a,b) => sort === "name" ? (a.name || a.id).localeCompare(b.name || b.id)
      : sort === "score" ? (a.score ?? Infinity) - (b.score ?? Infinity) : (b.duration_ms ?? -1) - (a.duration_ms ?? -1));
    table("#task-results", ["Task", "Status", "Duration", "Task accuracy", "Task evaluation", "Action"], tasks.map(t => {
      const name = html("div", "record-identity"); name.append(html("strong", "", t.name || t.id));
      if (t.name && t.name !== t.id) name.append(html("small", "", t.id));
      return [name, t9n(t.status || "Unknown"), taskDuration(t), Number.isFinite(t.score) ? t.score.toFixed(3) : "—",
        t9n(t.evaluation_status || "Unknown"), button("Inspect task", () => { navigate("profiler"); resetFilters(); setTask(t.id); })];
    }));
    table("#run-configuration", ["Condition", "Recorded value"], Object.entries(state.profile?.configuration || {}).map(([k,v]) => [k, String(v)]));
  }
  let checkingRunner = false;
  async function checkRunner() {
    if (checkingRunner) return; checkingRunner = true;
    const target = $("#runner-summary"); target.textContent = t9n("Checking runner…");
    try {
      const c = await json("/api/experiments/catalog"); target.replaceChildren();
      target.append(html("strong", "", t9n(c.enabled && c.ready ? "Runner ready" : "Runner unavailable")),
        html("p", "", t9n(c.message || (c.enabled && c.ready ? "Select a configured workload to start an experiment." : "Restart TraceX with --aries-dir to enable execution."))),
        html("p", "", `${t9n("Execution target")}: ${c.executor || "—"}${c.machine ? " · " + c.machine : ""}`));
      if (c.enabled && c.ready) target.append(button("New experiment", () => $("#new-experiment").click()));
      else { const code = html("pre", "runner-command", "tracex --aries-dir /absolute/path/to/ARIES"); target.append(code); }
      target.append(html("small", "", t9n("Runner readiness does not verify Docker, datasets, credentials or model endpoints.")));
    } catch (e) { target.textContent = t9n("Runner check failed: {error}", {error:e.message}); }
    finally { checkingRunner = false; }
  }
  for (const b of document.querySelectorAll("[data-page]")) b.onclick = () => navigate(b.dataset.page);
  $("#new-experiment").addEventListener("click", () => navigate("experiments"));
  $("#experiment-search").oninput = renderLibrary;
  $("#experiment-state").onchange = renderLibrary;
  $("#task-search").oninput = renderTasks;
  $("#task-status").onchange = renderTasks;
  $("#task-sort").onchange = renderTasks;
  $("#refresh-runner").onclick = checkRunner;
  $("#library-compare").onclick = () => {
    const [left,right] = [...selectedRuns]; if (!left || !right || selectedRuns.size !== 2) return;
    $("#compare-left").value = left; $("#compare-right").value = right;
    navigate("compare"); compareRuns();
  };
  $("#copy-view-link").onclick = async () => {
    writeURL();
    try { await navigator.clipboard.writeText(location.href); notify(t9n(state.imported ? "View link copied. Reimport the same profile to restore its data." : "View link copied.")); }
    catch { notify(t9n("Copy the view URL from the address bar.")); }
  };
  $("#run-picker").addEventListener("change", () => navigate("profiler"));
  $("#run-picker-mobile").addEventListener("change", () => navigate("profiler"));
  $("#import-file").addEventListener("change", () => navigate("profiler"));
  window.addEventListener("tracex:open-run", () => navigate("profiler"));
  window.addEventListener("tracex:selection", () => {
    if (state.selected && page !== "profiler") navigate("profiler");
    queueMicrotask(writeURL);
  });
  $("#task-list").addEventListener("click", () => navigate("profiler"));
  document.addEventListener("click", e => {
    if (e.target.closest("[data-view]")) { writeURL(); updateViewTabs(); }
  });
  window.addEventListener("tracex:view", writeURL);
  $("#reset-time").addEventListener("click", writeURL);
  $("#compare-left").addEventListener("change", writeURL);
  $("#compare-right").addEventListener("change", writeURL);
  window.addEventListener("tracex:render", () => {
    renderLibrary(); renderTasks();
    updateViewTabs(false);
    if (restoring) {
      restoring = false;
      for (const [key,id] of [["baseline","compare-left"],["candidate","compare-right"]]) {
        const run = allRuns().find(r => r.run_id === restored[key]);
        if (run) $("#"+id).value = run.id;
      }
      if (page === "compare") compareRuns();
    }
    writeURL();
  });
  function restore(s) {
    restored = s;
    restoring = true;
    Object.assign(state, {task:s.task, kind:s.kind, query:s.query, runtime:s.runtime, selected:s.selected,
      searchIndex:0, searchLocate:true, searchResult:null, view:s.view, rangeStart:s.rangeStart, rangeEnd:s.rangeEnd, viewStart:s.viewStart, viewEnd:s.viewEnd, pageOffset:s.pageOffset, restoreRun:s.run});
    $("#search").value = s.query; showZoom();
    navigate(s.page, {historyEntry:false});
  }
  window.addEventListener("popstate", () => { const s = model.parse(location.hash); restore(s); loadSource(); });
  window.addEventListener("tracex:navigate", event => navigate(event.detail.page));
  window.addEventListener("tracex:evidence", async event => {
    const ref=event.detail, run=allRuns().find(r=>r.run_id===ref.run_id);
    if(!run) {notify(t9n("Linked run is unavailable. Mount the source or reimport the profile."));return;}
    await openRun(run.id,"profiler",ref.task_id || "all");
    if(ref.span_id) await jumpToSpan(ref.span_id);
  });
  restore(restored);
  loadSource();
})();
