# ARIES instrumentation integration

TraceX works with existing ARIES runs. Full lifecycle and experiment-condition evidence additionally needs a small ARIES producer change. It preserves the four component contracts, evaluation isolation, and cleanup decisions; it adds observations at existing boundaries.

The producer change is available as [ARIES-browser-bench PR #4](https://github.com/aeilot/ARIES-browser-bench/pull/4), branch `codex/tracex-profiling` (commit `f56edb1`). Check out that branch and rebuild ARIES to collect the additional evidence on future runs; existing completed artifacts remain readable.

## Producer artifacts

- `aries.log`: paired `msg: "task phase"` records containing task occurrence ID, unique phase ID, phase name, start/end event, measured end duration, status, and explicit `depends_on` predecessor.
- `run-result.json`: optional `scheduled_at` captured when application admission is attempted, before waiting for capacity. Compare it with actual `started_at` for admission-to-start queue time. An arrival trace's requested offset is a separate concept.
- `run-profile.json`: a private allowlisted snapshot for comparing benchmark, harness, model, provider, concurrency and explicitly configured resource conditions. It never serializes the entire configuration or native harness settings.

Observed phases are sandbox start, preparation, bridge start, harness start, execution, isolation, evaluation, and cleanup. Attempts that never occur do not receive fabricated spans. Isolation remains authoritative before verification, and cleanup still follows resource ownership. Raw phase errors, instructions, outputs, credentials, and endpoint values are excluded.

The predecessor is recorded by a task-local sequential phase recorder when the prior phase completes. TraceX uses that explicit relationship, rather than sorting timestamps to manufacture causality. Only completed phase endpoints become dependency edges. The task-wide lifecycle envelope does not become an extra weighted predecessor that double-counts all phases.

## Existing runs

Older artifacts can show task duration, native model/tool evidence, and resources, but cannot reconstruct missing preparation/evaluation/cleanup boundaries. OpenClaw model completion usage does not supply a missing request start. Browser timing remains sandbox-produced evidence. The UI keeps these limits visible.

## Validation

The producer is covered by Runner success, preparation failure, cancellation, predecessor order, final cleanup, schedule waiting, and snapshot privacy tests. No benchmark model requests are needed for these tests. See [verification](verification.md) for the local platform limitation on the broader ARIES app package.
