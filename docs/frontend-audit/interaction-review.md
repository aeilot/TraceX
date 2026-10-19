# Profiler interaction review

The review used the existing local TraceX UI and a normalized snapshot of `suspend-c4-off` containing 66,019 spans. The original service on port 4318 was retained; the updated binary is previewed separately on port 4320 with that snapshot. No experiment or external model call was started.

## 1. Open a task and inspect its timeline — improved

The starting view was task occurrence 020 at about 51× zoom. Its core evidence was readable, but the canvas depended on hidden modifier gestures. The source list repeated per-task capabilities and displaced the task list; long task hashes shared an indistinguishable prefix.

Changes: explicit Pan / Zoom to region controls, keyboard controls, position slider, previous/next window actions, disabled boundary actions and an explicit boundary label. Fit task restores the task interval without clearing filters. Hash-based occurrence labels show their distinct suffix, while the full identifier remains in the tooltip. Capability summaries group repeated source names; detailed reports remain available in Evidence.

Evidence: [starting view](15-timeline-before.png).

## 2. Pan, select a region and inspect evidence — verified

Pointer handlers previously belonged to each drawing pass and could be replaced during a gesture. Pointer-down did not prevent text selection. Panning wrote the view URL on every input event; replacement data could arrive for an obsolete context. Failed loading could immediately schedule another request.

Changes:

- Gesture listeners stay attached to the persistent SVG. Only primary input starts a gesture. A five-pixel threshold distinguishes a click from a drag; dragging a bar cannot accidentally open its inspector. Shift-drag and the explicit region mode both zoom the view, without changing the analysis filter. Escape cancels an active gesture, and canceled selection does not apply.
- The canvas blocks text selection and owns horizontal overscroll. Horizontal wheel events are consumed even at the ends; ordinary vertical scrolling remains available. Touch permits native vertical scrolling and reserves horizontal movement for the timeline.
- View movement coalesces into animation frames. Existing buffered bars move immediately, while two view-widths of neighboring evidence on each side are prefetched. Rebuilds are bounded to an 80 ms cadence, and selection overlays wait until their gesture ends. Axis labels update with the current view. Hidden DAG DOM is no longer rebuilt for timeline-only rendering.
- The loader deduplicates covered requests, aborts obsolete work, captures run/filter context before requesting, and rejects late results. Failed loads stop and expose Retry; existing same-context evidence remains. A changed run/filter never presents another context's bars as its own. View URL updates settle after 180 ms rather than blocking each wheel event.
- Long span labels anchor inside the visible region. Focus survives replacement drawings. Late span-inspector responses cannot replace a newer selection or reopen a closed panel.

Actual browser checks: drag changed the view without selecting text; explicit region mode narrowed the view without Shift; Home and End moved to the correct endpoints; native horizontal scrolling at both endpoints kept the current page; Fit task restored approximately 5m 52.4s–8m 37.2s; clicking a recorded model span opened its matching inspector. Browser error/warning inspection returned no entries.

Evidence: [updated high-zoom timeline](16-timeline-after.png).

## 3. Narrow-screen controls and evidence — verified at 390 px

The earlier minimum plot width exceeded a narrow container. Lane labels consumed too much space and eight timestamps overlapped.

Changes: responsive lane-label/plot allocation, adaptive timestamp count, wrapping control groups and internal scrolling for long tables. At 390 px viewport, document width was 390 px and the timeline SVG matched its 356 px container, with three timestamp labels. Keyboard focus and labeled controls remain available.

Evidence: [narrow-screen timeline](17-timeline-mobile.png).

## Verification and limits

`make check` passed: Go tests, 58 JavaScript tests, race detector, vet and embedded binary build. New tests cover obsolete and canceled requests, deduplication, explicit retries, drag threshold, click suppression, region mode, Shift selection, cancellation and Escape rollback.

The browser check used the in-app Chromium surface, including native drag and horizontal-scroll input. Shift handling and delayed-response races were additionally tested deterministically in JavaScript. This is not a frame-rate benchmark or a claim about every browser/OS navigation gesture. Native mobile touch and screen-reader behavior were not manually exercised. The review covers the profiler entry, navigation/gesture/inspection flow and narrow-screen layout; it does not claim a full audit of experiment execution or every settings flow.

## DAG follow-up: structure, expansion and navigation

1. **Open the DAG — improved.** The checked page previously drew 500 nodes, 51 recorded page-local edges and 58 cross-page relationships. Of its nodes, 447 had no page-local edge. The initial structure now shows four nodes and their recorded edges, with explicit shown/hidden counts, a checkbox for the 447 nodes without page-local links and the original pagination warning. These nodes can have relationships on another page; they are not classified as globally independent. [Before](18-dag-before.png), [structure overview](19-dag-overview.png).
2. **Expand and inspect — verified.** Expand a branch in batches of eight direct successors, show how many remain, or explicitly expand the page. The model branch with 46 direct successors first expanded the diagram from four to twelve nodes; the remaining 38 stayed available. Full page expansion plus the unlinked-node checkbox restored all 500 nodes and 51 edges. Shared successors preserve every recorded edge between visible endpoints. Folding adds no synthetic dependency or causal relationship. Search reveals matches and their ancestor paths; the checked `browser_navigate` search revealed 16 matches among 26 displayed nodes while preserving the surrounding overview.
3. **Navigate the graph — verified.** Added pointer-centered Ctrl/Command-wheel zoom, zoom buttons, original size, fit, background dragging and adjacent-edge highlighting. Four ordering passes reduce crossings within graph columns; columns still reflect recorded directed relationships. Node headers use readable task occurrence labels when no agent identity is recorded. The detail drawer has a top-level neighborhood action that hides the drawer, retains selection and shows exact incoming/outgoing neighbors. Search refresh no longer reopens the drawer afterward. Returning to structure, 125%/100% zoom, dragging without text selection and a 390 px document-width check passed in the browser. Warnings/errors were empty.

DAG projection tests cover compact and expanded branches, batched expansion, ancestor reveal, shared joins, exact neighborhood edges, isolated/cyclic inputs, deterministic non-overlapping columns and cross-page references. The full check passed with 66 JavaScript tests, Go tests, race detector, vet and build; final drawer and label changes also passed the JavaScript suite and binary build. This remains a page-local graph: neither search nor overview implies that all 66,019 spans or all relationships are drawn together. Browser imports use the same projection code.
