# Frontend workspace

The default screen is the experiment library. These rows are mounted/imported run records, not a persistent experiment database. Source state describes watching or source errors, not benchmark success.

## Navigation

- **Experiments** searches run names, source IDs and recorded configuration. Select exactly two runs to compare; selection order determines baseline then candidate. Selections remain selected when hidden by a filter and the selected count remains visible.
- **Profiler** opens the current run. Run tabs expose Overview, Profiler, Tasks, Configuration and Evidence. The default visualization is the timeline; DAG and split remain available. Summary, analysis and comparison no longer precede the execution view.
- **Tasks** searches names/IDs and filters statuses. Duration sorting defaults to slowest first; score sorting places missing scores last. Inspecting a task clears previous execution filters before selecting that task.
- **Compare** reuses the existing full-run metrics and configuration sweep. Opening it from library selection immediately compares the chosen pair. This does not execute a sweep or establish causality.
- **Sources & runner** lists source states, revisions and errors, and reads runner catalog readiness. A ready catalog does not establish that Docker, datasets, credentials or endpoints work.

Opening New experiment navigates to the library and expands the existing launcher. Unavailable runners show editable ARIES runner settings and job history without an empty execution form. The settings panel discovers existing directories and OrbStack machines, saves the selected target without restarting, and reports host prerequisites. External-runtime profiles offer per-job model overrides. See [setup and configuration](experiments.md#configure-a-runner). Existing prepare/start/stop behavior and model-call acknowledgment are retained. Active jobs can be inspected or stopped by reopening New experiment while browsing a profile.

Span details label span status separately from task evaluation, accuracy, end reason and cleanup. The inspector overlays the workspace rather than shrinking/reflowing its charts. Escape and the close control dismiss it.

Resource empty labels distinguish a response omitted because of a reported resource budget/error from absent measurements. An empty filtered sample response is labeled as no samples in the selection; a metric missing from returned samples remains unmeasured. Existing explanatory server messages remain visible.

## Investigation links

View state is stored in the URL fragment: page, source `run_id`, task, span, kind, query, runtime, time window, timeline view window (`vfrom`/`vto`), visualization, DAG offset and comparison identities. Stable source IDs allow restoration even when mount order changes. Browser navigation restores the linked view. Only supported pages, view modes, limits and finite nonnegative numeric ranges are accepted.

Links do not contain profile contents. They require the same source to be available locally. Imported profiles live in browser memory; reimport after refreshing. A missing linked run produces an explicit notice. This release does not persist jobs or imported files.

## Find spans without filtering away context

Profiler search highlights matches and centers the current match in the DAG/timeline scroll area. It preserves surrounding spans, lanes, edges and resource signals. Task, type and time-window controls remain explicit filters. Previous/next buttons and Enter / Shift+Enter cycle matches inside that filtered context. A search with no matches leaves the context visible.

`GET /api/find` returns a bounded locator (match count, match index, span ID and offset within context). The client loads the target's normal context page rather than a page containing only search hits. Browser imports use the same matching and context ordering locally. The timeline is not paginated; the DAG draws the first 500 spans of the window, moves to the slice holding a found span, and warns about edges that cross it.

Verification: a 630-span server fixture finds span 629 at context offset 629 while the target page retains its 129 neighboring spans; JavaScript tests cover the corresponding browser-import behavior and context filters. Browser verification shows searching `plan` retains all 18 demo spans with one highlight; searching `source` retains 18 spans with nine highlights, and Next advances to the second match. `make check` passed with 44 JavaScript tests plus Go, race, vet and build checks.

## Verification · 2026-10-17

`make check` passed: Go suites, 38 JavaScript model/translation tests, race detector, vet and binary build. Shared-state tests cover Unicode/reserved characters, invalid ranges and comparison identities. Existing server asset checks verify that all new embedded scripts/styles are served.

The in-app browser was used against a fresh demo server on port 4320 to verify:

1. Experiment library renders and name search narrows the result.
2. Library → Profiler → Tasks → inspect task selects the correct task and timeline.
3. Clicking the failed retry span shows separate span/task status labels; refreshing restores both task and selected span.
4. Selecting both demo runs opens a comparison with the expected +25.0% duration change; refreshing retains baseline/candidate and results.
5. Sources & runner shows the unavailable runner; New experiment omits the empty execution form.
6. Chinese navigation/table labels render correctly.
7. At 390px viewport width, library and Profiler document width remains 390px; tables/visualizations use internal scrolling. The temporary viewport override was reset.
8. Browser error/warning log inspection returned no entries during the checked flow.

Final screenshots: [library](frontend-audit/06-library-after.png), [profiler](frontend-audit/07-profiler-after.png), [mobile library](frontend-audit/08-mobile-after.png).

Optional Playwright smoke scripts were updated for the new navigation, but were not executed in this run. Real model execution was not launched. Persistent experiment metadata, automatic sweep execution, hierarchical trace aggregation remain separate future work. Task-paired regression diagnosis is now available in Insights.

## Motion and interaction feedback

Navigation reveals newly visible workspace sections with a short fade/rise. Switching the DAG/timeline layout reveals the incoming visualization; live snapshot updates do not replay these animations. Run tabs and layout controls use a measured sliding selection background that snaps on initial layout, wrapping and resize.

The inspector slides from the right; the experiment panel fades/rises into place; toasts enter and leave with a short fade. Exit transitions make content inert and remove it from the accessibility tree immediately, then set `hidden` when the exit finishes. Reopening cancels pending dismissal. Navigation away hides the experiment panel immediately to avoid retaining layout space in another workspace.

Motion durations are CSS variables in `web/motion.css`. The implementation uses local CSS and the Web Animations API, with no added dependency. `prefers-reduced-motion` disables transitions and entrance effects; changing that preference also cancels active entrance animations and finishes pending dismissals. Explicit experiment-panel scrolling follows the same preference.

Verification: `make check` passed with 42 JavaScript tests, including panel close/reopen races, immediate exits, reduced-motion preference changes, and interrupted toast dismissal. In-app browser checks confirmed navigation, inspector close/reopen, zero-offset selection backgrounds for both tab groups, and no browser warnings/errors. Panel/tab/toast primitives are adapted from the local transitions-dev skill. Screenshot: [Profiler with transitions](frontend-audit/10-motion-profiler.png).

## Insights and navigation verification

Insights adds evidence-linked diagnostic cards, full-run model/tool aggregates, task-paired comparison, scoped optional model interpretation and a persistent browser notebook. See [definitions and configuration](insights.md). The compact header keeps the brand and navigation in one row, with internal navigation scrolling on narrow screens.

The browser integration check used a clearly labeled local synthetic model fixture, with no real model calls. It verified explicit consent, structured output and evidence references, saving and reloading interpretation notes, editing, removal with Undo, and returning to the selected span. The default preview leaves interpretation disabled until the server is configured.

## Profiler gesture and loading update

The timeline now has explicit pan/region modes, a position slider, previous/next windows, Fit task, keyboard navigation and boundary feedback. Dragging and region selection do not select text. Horizontal gestures are contained at the ends. Panning and zooming change the visible window; the analysis-range fields and Apply remain explicit evidence filters.

Movement uses frame-coalesced previews with neighboring buffers, cancellation of obsolete requests and bounded redraws. Failed loads retain the prior evidence and offer Retry. Long span labels remain visible, task occurrences with long common hashes use distinct suffixes, and repeated source capabilities no longer displace the task list. Narrow timelines fit their container and reduce timestamp density.

See the [interaction review and screenshots](frontend-audit/interaction-review.md) for the checked flow, verification and limits. `make check` passed with 58 JavaScript tests plus Go, race, vet and build checks.

## DAG structure view

DAG defaults to a compact structure overview of the current page. Branches expand eight successors at a time; Expand page reveals the connected page structure, and the separate nodes-without-page-links checkbox restores those other page nodes. Counts distinguish displayed nodes, folded/outside nodes and relationships crossing the page. Search and selection automatically reveal ancestor paths. Neighborhood mode shows only the selected node and its direct recorded neighbors, with a return action.

The canvas supports background dragging, pointer-centered modifier-wheel zoom, zoom buttons, original size and fit. Hover/focus emphasizes adjacent edges. The inspector offers a neighborhood action near its title, and a search refresh keeps the dismissed inspector closed. See the [DAG follow-up and before/after evidence](frontend-audit/interaction-review.md#dag-follow-up-structure-expansion-and-navigation).

## Resource window zoom

Resource charts have an independent shared visible window. Drag across any chart to zoom all five signals; repeated selections support nested investigation with Previous resource window and Reset resource zoom. Modifier-wheel zoom anchors at the pointer; horizontal scrolling or arrow keys pans, and +/− zooms. Use timeline window explicitly copies the timeline view. These actions do not change the DAG page, analysis filters or timeline window. Changing a resource runtime preserves the resource window; changing task/run/analysis bounds resets it. This local zoom history is not encoded in the shared view URL.

Each window requests fresh evidence through the existing `/api/view` route with a one-span payload and up to 1,000 resource points. Imported profiles filter their complete local resource array instead. The server's existing envelope/error rules and returned/total sample counts remain visible. Peaks and memory-limit percentages use the displayed window, and all five charts share its axis. No samples and missing metrics remain distinct from measured zeros. Network rates separate task/component/runtime series and reject a reset in either counter direction. Rates and throttling shares require consecutive observed samples; a window containing only one sample cannot supply an interval rate. Downsampled counter rates describe the returned sample intervals.

Requests coalesce for 70 ms and cancel obsolete windows. Preview samples remain visible while detail loads; failure exposes Retry. Returning to a cached window cancels any stale in-flight detail request. Live refresh preserves the current resource window within unchanged task/run/analysis bounds.

Verification: the complete check passed with 71 JavaScript tests, Go tests, race detector, vet and build. Native browser dragging narrowed 4m 30.2s–13m 30.7s to 6m 19.1s–9m 26.3s, with matching CPU/memory axes and no text selection. Back, Reset, timeline alignment and runtime switching were checked; 390 px viewport retained a 390 px document. Browser warnings/errors were empty. [Resource window screenshot](frontend-audit/20-resource-window.png).

## Timeline drag mode

Timeline dragging is now fixed to region zoom. The Pan / Zoom to region switch has been removed; a static label and crosshair indicate the behavior. No Shift modifier is required. Explicit time navigation, wheel navigation and zoom/reset controls remain available. Resource-chart region selection and DAG background dragging retain their own behavior.

Verification: direct native dragging narrowed the timeline from 4m 30.2s–13m 30.7s to 6m 5.6s–8m 24.4s without selecting text; no mode-switch controls remain. All 71 JavaScript tests and the embedded binary build passed.
