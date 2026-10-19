# Multi-agent scheduling analysis plan

## Product objective

Make it possible to select an interval and determine which agent/task instances were admitted, active, paused or waiting; which recorded resource pools they shared; which instances slowed down; and which measurements support each proposed explanation.

The primary surface should be a Scheduling workspace. Keep the DAG as a local relationship inspector. Do not use containment edges as scheduler causality or treat model-call wall time as local CPU execution.

## Existing evidence and gaps

TraceX already supports task occurrence IDs and logical task IDs, optional agent/runtime IDs, queue lifecycle spans when recorded, model/tool spans, paired sandbox pause intervals, runtime resource samples and cumulative CPU throttling counters. The schema deliberately preserves missing measurements.

Relevant implementation: `internal/profile/profile.go`, `internal/profile/suspend.go`; contracts: `docs/schema.md`, `docs/aries-integration.md`.

Missing evidence includes a dependable mapping from logical agent to instance/task/runtime/host/resource pool; general admission and resource-pool acquisition events; provider queue/service phases; and operating-system runnable/CPU-wait attribution. Many current traces have no explicit agent identity. Use task occurrence as the visible fallback and label it accordingly.

## Workspace

### One shared analysis window

The scheduler overview, task/agent lanes, resource pools and impact table share one time window. Brushing any panel updates all panels. Support previous window, reset, linked/unlinked zoom, pinned instances and comparison intervals. Selecting an instance highlights it while leaving competitors visible; filtering competitors away requires an explicit action.

### Aligned lanes

Provide three separately labeled layers:

1. Lifecycle/admission: recorded admission queue, initialization, execution envelope, evaluation and cleanup, plus unknown intervals.
2. Concurrent work: model requests, tool/browser work and explicit sandbox pauses. These intervals may overlap; do not force them into a single exclusive state or sum nested envelopes.
3. OS/resource evidence, when instrumented: CPU running/runnable wait, pressure and pool queues. Application execution does not supply these states by itself.

Use one lane per explicit agent instance, with task occurrences nested below it. Until that mapping exists, use task occurrence lanes. Collapse child spans by default and preserve failure/phase markers. Sort by start, measured queue duration, duration/slowdown, or resource pool; allow a few lanes to be pinned. Aggregate offscreen rows with visible counts rather than hiding other active instances on selection.

### Shared capacity and demand

Above the lanes, show admitted/in-flight task counts, recorded queue depth, model request concurrency, and tool/browser concurrency. In-flight requests describe outstanding operations, not simultaneous model execution.

Group resource charts by recorded host and pool. Show system CPU and capacity/quota when measured, memory/limit, throttling, and recorded pressure/queue metrics. Keep shared/device series separate from task/runtime-attributed series; do not duplicate shared utilization across agents or invent co-location when host identity is absent. Queue depth requires recorded entry and exit boundaries; active envelopes alone do not reveal runnable demand.

### Interval impact table

Rows retain agent/task identity and show overlapping duration, known admission queue time, model/tool work union, explicit paused duration, resource sample coverage, CPU/throttling evidence, failures and paired baseline latency where available. Unknown columns remain unknown. Sum-of-work and wall time have distinct labels; multithreaded CPU-wait totals cannot be displayed as a mutually exclusive portion of agent wall time.

For a controlled baseline with the same logical task and compatible inputs/conditions, show slowdown as candidate completed task latency divided by baseline completed task latency. With repeated trials, report sample counts and distributions. Different task IDs or incompatible/missing conditions are not automatically paired into a slowdown estimate.

### Evidence-backed explanations

Selecting an interval can surface a statement such as: several task instances overlapped, recorded CPU pressure rose, and selected task latency exceeded a matched baseline. Each clause links to the relevant spans, resource series, queue events or baseline tasks. Label a suspected mechanism separately.

CPU throttling supports quota enforcement, while confirming host competition requires additional host/runtime evidence. A slow model call can include provider queueing, network, inference or local handling; identify these components only when measured. LLM Insights can summarize this evidence but is not the measurement or attribution mechanism.

## Implementation phases

### P0: make current traces useful

Status: implemented on `feat/scheduling-workspace` (Scheduling run tab, `/api/scheduling`, `profile.Schedule`, `web/scheduling-model.js`). Comparison intervals and linked/unlinked zoom with the profiler are not yet implemented.

- Add Scheduling workspace, shared brush, task-instance lanes, pin/highlight and recorded runtime grouping.
- Compute admission queue intervals only when queue spans exist; show model/tool concurrency, explicit pauses and existing throttling alongside resources.
- Add full-scope interval impact and evidence links. Do not derive global concurrency or queue counts from the current 500-span DAG page.
- Add an indexed `/api/scheduling` projection with run/revision, time window, selected identities and resolution. Return bounded interval/series envelopes, counts, coverage and stable evidence references. Cache per immutable snapshot/filter/resolution; cancel obsolete requests. Browser imports use the corresponding pure projection.
- Distinguish interval-clipped display metrics from full-call/full-task measurements. Preserve gaps, peaks and missing data during downsampling.

This phase supports overlap and pressure investigation, with explicit limits on causal attribution.

### P1: instrument the scheduling decisions

- Record `agent_instance_id`, task occurrence, runtime, component, host and resource-pool mapping. One agent can use several runtimes; several agents can share a process or pool.
- Emit admission/resource acquisition entry, grant, release and cancellation events with resource type, configured capacity, request/decision ID and structured reason. Keep admission-to-start queueing distinct from each pool's wait.
- Record clock domain, timestamp precision and synchronized origins. Incomplete event pairs stay incomplete. Do not serialize prompts, credentials or arbitrary raw event payloads.
- Add host CPU/memory pressure, runqueue and effective capacity/quota telemetry. Collect per-process/thread CPU scheduling delay where supported and map it explicitly to runtimes. Keep host-level indicators distinct from per-agent attribution.
- For model endpoints, instrument local request slots and provider-reported queue/prefill/decode phases when available. A provider lacking those fields retains an undivided measured call interval.
- Version new normalized fields/events and update producer/browser allowlists together; older runs remain readable with unknown fields.

### P2: controlled comparisons and policies

Run the same selected tasks with concurrency 1/2/4/8, and compare suspension/resource policies with repeated trials. Hold host, workload inputs, model/provider, cache conditions and resource limits fixed or disclose differences. Report throughput with a defined observation interval, completion/failure counts, queue P50/P95, completed-task latency P50/P95, matched-task slowdown, accuracy and policy transition overhead. Mark unfinished tasks and do not quietly exclude them from failure/censoring counts.

Compare fairness only within a defined comparable workload cohort and service-share definition; heterogeneous task durations alone do not establish unfair scheduling. Use the measurements to decide whether concurrency, admission, CPU limits, model-slot limits or pause policy should change.

## Acceptance scenarios

1. Brushing a slowdown interval keeps all overlapping task/agent lanes visible and aligns resource axes and table metrics.
2. A recorded admission wait appears as queue time; a missing phase appears unknown. LLM time and sandbox pause never become inferred CPU wait.
3. Resource pooling requires a recorded identity mapping. Shared host/device samples appear once, with coverage and units.
4. Changing the DAG page, collapsing nodes or searching does not alter full-scope scheduling totals.
5. A one-runtime counter reset does not create a utilization/throttling spike. Unmeasured, zero and incomplete intervals remain distinct.
6. Selecting a candidate task can open its matched baseline and recorded conditions; unmatched tasks remain visible without a fabricated slowdown ratio.
7. Older traces, missing host identity and provider telemetry remain usable, with unavailable columns rather than guessed causes.

## Recommended sequence

Implement P0 first. Its central deliverable is the linked Scheduling timeline and interval impact table, backed by full-scope aggregation. P1 turns pressure correlations into stronger explanations. P2 evaluates scheduler and suspension policies against controlled baselines.
