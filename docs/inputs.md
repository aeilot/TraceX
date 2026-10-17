# Inputs and measurement limits

## ARIES run directory

Pass a completed or active ARIES run directory to `--run`. Completed results supply authoritative task identities and independent harness, evaluation, and cleanup outcomes. Before `run-result.json` exists, the loader discovers tasks only from known component directories or explicit task lifecycle log IDs, not arbitrary folders such as model-runtime logs.

| Artifact | Used evidence |
| --- | --- |
| `run-result.json` | Task occurrence IDs, actual start, optional admission timestamp, duration, and separate outcomes |
| `run-profile.json` / `profile.json` | Allowlisted experiment configuration only |
| `aries.log` | Explicit paired task-phase records, measured durations, recorded predecessor IDs |
| `otel-spans.jsonl` | Timed spans, explicit parents, selected identity and model metadata |
| `tool-calls.jsonl` | Bridge operation class, completion timestamp, duration, outcome |
| `resources.jsonl` | CPU percent, memory bytes, memory limit and peak, PID count, cumulative network rx/tx bytes, optional cumulative `cpu_throttling` (`periods`, `throttled_periods`, `throttled_us`; kept only when all three are present and consistent), optional device GPU utilization |
| `evaluation/score.json` | Continuous `accuracy` in [0, 1] as the task score; prediction and gold answers are not read into the profile |
| `harness/session-outcome.json` | `end_reason` enum (for example `completed`, `deadline_exceeded`); free text is dropped |
| `evaluation/network-summary.json` | Aggregate request, failed, unsent, response-byte, active-time and per-resource-type counts plus navigation count; navigation URLs are never retained |
| `monitor/index.json` | Observation status including failed, disabled, unsupported and canceled scopes |
| `*.trajectory.jsonl`, `trajectory.jsonl` | Recognized OpenClaw native records |
| `browser-network.jsonl` | BrowserBench request/page intervals without URLs or payloads |
| `events.jsonl` | Optional backend-neutral normalized updates |

Evidence under a task directory must resolve to its exact occurrence. Duplicate legacy task IDs remain distinct, but ambiguous artifacts are skipped with a warning. Shared infrastructure retains run-level scope. Unknown filenames and nonmatching record shapes never become fabricated spans.

Input files are data, never commands. Symlink evidence files are not followed. An invalid metadata/profile fails loading. Invalid native lines produce partial-coverage warnings; partial trailing JSON is retried. A malformed complete live event preserves the previous valid server revision.

## Hermes / OpenTelemetry

One JSON span per line supplies `start_time_unix_nano` / `end_time_unix_nano` or RFC3339 `start_time` / `end_time`, `span_id`, optional `parent_span_id` / `parent_id`, and optional `trace_id`. IDs may also live under `context`. Parents resolve within a task/trace. This is a retained artifact adapter, not an OTLP receiver.

Recognized names classify model, tool, browser, or agent work; unknown kinds remain `other`. Explicit `agent_id` / `attributes.agent.id` identify actors. An unassigned task lane is not a measured agent identity. Selected usage fields are retained when recorded; missing usage remains unavailable. Ambiguous parents and malformed cyclic native links are omitted with warnings.

Bridge records with status `unsupported` (non-exec SSH channel requests such as `env` or `pty-req`) and zero-duration `sync` records are bridge bookkeeping rather than agent tool calls. They are kept as `kind: other` spans named `Bridge bookkeeping · …` with `attributes.bridge_role: bookkeeping`, so tool counts and summed tool time reflect only real calls.

Bridge calls have no guessed parent to a harness call. Both streams can observe the same operation; they must not be added as if they were disjoint wall time. Bridge start is derived from its recorded completion timestamp minus duration, not treated as a completion timestamp on the left edge.

## OpenClaw

The native adapter recognizes `session.started`, `session.ended`, `model.completed`, explicit `tool.started`/`tool.completed` with `toolCallId`, and message tool calls/results with matching explicit IDs. Session intervals and tool pairs are measurable only when both endpoints exist. Unfinished records stay incomplete.

A model completion may supply input/output usage but no request start. It becomes a completion marker with unavailable duration, not a zero-latency model request. Unsupported native formats remain partial/unsupported; the parser does not read arbitrary message content as instructions or render prompts and outputs.

## BrowserBench

The adapter reads sandbox-produced request `start_ms` and completion (`timing.response_end` relative to start, or valid recorded `end_ms`) and page navigation timing. Requests are named only by allowed resource type; URL/query/header/body data is omitted. Failed unsent requests do not become measured network intervals. Browser tool and network intervals can overlap; they remain distinct kinds.

These artifacts are written inside the agent's sandbox. Their integrity is not independently verified, and the warning accompanies the profile. TraceX does not parse arbitrary HAR/CDP dumps or call page-load time CPU compute.

## Other backends

Emit [schema v2 JSON](schema.md) or [newline-committed events](events.md). `--run custom.jsonl` accepts an event file directly, including an initially empty stream. Explicit relationship kinds support branches and multi-parent joins. No provider-specific telemetry is required for the generic event contract.

## Limits

- Normalized JSON and small metadata files: 32 MiB each; browser import: 32 MiB.
- Discovered native evidence / event stream: 256 MiB. Native JSONL records: 4 MiB; event records: 8 MiB.
- Discovery: 20,000 entries and eight nested levels.
- Profile: one million combined tasks, spans, resources and edges. Reaching native evidence limits marks partial coverage; invalid normalized profiles fail validation.
- API pages: 250 spans by default, 1,000 maximum; browser choices 50/250/500.
- Resource envelopes: 300 observations by default, 1,000 maximum, with extrema and gap preservation. An impossible budget returns an explicit `resource_error` while span pages remain usable; choose a runtime or narrower window.

The retained normalized profile and parsed JSONL checkpoints live in memory. Unchanged native files are not decoded again; growing files read appended bytes, while normalized indexes/analysis are rebuilt. This supports large local runs, not unlimited retention or a persistent distributed database. Mount discovery happens at startup; new sibling run directories require restarting `--runs-dir` discovery.

Monitor network counters are cumulative per runtime; the UI derives rx+tx throughput from consecutive samples of one series and shows no rate across a counter reset. Memory is shown against the sample's own `memory_limit_bytes` when the limit is recorded and nonzero; for an unlimited container the reported limit may be host memory.

A task whose duration exceeds five times the run's median task duration is flagged `duration_outlier`. If its monitor series also hold fewer than half the samples expected from the monitor interval (default 1 s), it is flagged `sparse_monitor` and a warning states that its resource profile is incomplete. Flags describe evidence quality, never task outcomes.

The first ARIES CPU observation per runtime is a counter baseline and is omitted from the CPU curve. Later measured zero is retained. Missing metric values and outages remain gaps. Device-level GPU readings and shared bridge resources are not apportioned to tasks.

Run origin is the earliest retained task or observation timestamp. Cross-process clock synchronization is not checked. Missing coverage cannot be classified as local compute, provider queuing, or network wait without evidence. Client model spans do not reveal provider prefill/decode timing.
