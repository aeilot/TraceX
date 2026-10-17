# Inputs and measurement limits

## ARIES run directory

Pass the directory containing `run-result.json` to `--run`. TraceX reads task occurrences and separate harness, evaluation, and cleanup outcomes from that result. It discovers recognized evidence files under task directories and `infrastructure/`:

| Artifact | Used evidence |
| --- | --- |
| `run-result.json` | Run name/ID, task identity, start timestamps, durations and outcomes |
| `otel-spans.jsonl` | Recognized per-line OpenTelemetry span records, explicit parents, selected metadata |
| `tool-calls.jsonl` | Bridge operation class, timestamp, duration and outcome |
| `resources.jsonl` | CPU percent, memory usage bytes, optional GPU utilization |
| `monitor/index.json` | Observation status, including failed, unsupported and disabled scopes |
| `trajectory.json`, `trajectory.jsonl`, `*.trajectory.*`, `agent-result.json` | Reported as unsupported opaque trajectory evidence |

The loader expects ARIES task evidence below a directory named for its task ID. Repeated task IDs get distinct occurrence IDs; evidence that cannot be assigned to a unique occurrence is skipped with a warning. Shared infrastructure samples keep their component/runtime identity and are not attributed to tasks.

Input files are read as data and never executed. Unknown filenames are ignored. Malformed records and unsupported artifact shapes produce warnings rather than fabricated spans. An invalid run-result or normalized profile fails loading. Files must be regular files; symlink files are skipped during discovery.

## Harness telemetry

The recognized `otel-spans.jsonl` format uses one JSON span per line, with `start_time_unix_nano` / `end_time_unix_nano` or RFC3339 `start_time` / `end_time`, `span_id`, optional `parent_span_id` / `parent_id`, and optional `trace_id`. IDs can also be supplied under `context`. Parent relationships resolve within a task and trace. This is a retained artifact adapter, not a general OTLP network receiver.

Recognized span names classify model, tool, browser, or agent work; unknown names stay `other`. Agent identities come only from explicit `agent_id` / `attributes.agent.id`; absent identities use unassigned task lanes and are not counted as observed agents. These classes assist navigation; they do not establish backend execution semantics. Missing/ambiguous parent IDs omit edges and preserve independent roots. Invalid cyclic parent links are omitted with warnings. Bridge calls have no inferred parent link to harness spans, so two telemetry streams can describe overlapping work without being treated as additive wall time.

Native OpenClaw trajectories currently lack a supported timing adapter. A run can still show its task lifecycle, bridge evidence, resources, and any recognized OpenTelemetry evidence. TraceX does not claim complete agent coverage from those partial sources.

BrowserBench remains an optional ARIES workload extension. This release can display browser spans provided through recognized telemetry or normalized JSON. It does not parse arbitrary browser HAR, CDP, or sandbox timing files. No browser evidence is required for the main profiler.

## Other backends

Emit [normalized TraceX JSON](schema.md) and load it with `--run`. Explicit parent IDs support branched agent DAGs, including cross-agent child spans. Task IDs must distinguish execution occurrences. Omitted measurements should carry a capability reason; do not use zero as a substitute for unsupported collection.

## Bounds and interpretation

The loader limits a file to 32 MiB, discovered evidence to 256 MiB, discovery to 20,000 entries and eight directory levels, and JSONL records to 1 MiB. The profile record limit is 100,000. Browser loading additionally limits spans to 20,000, and graph/timeline rendering shows up to 350/2,500 matching spans with an explicit truncation label. Use task, type, or search filters to inspect smaller subsets. A partial read is reported in warnings. These bounds keep a local snapshot manageable; large-run streaming and aggregation are future work.

The first CPU sample for each ARIES runtime is a counter baseline and is omitted from the CPU curve. Later measured zero values are retained.

The timeline origin is the earliest retained task or observation timestamp. Cross-process clock synchronization is not checked. Unobserved gaps cannot be identified as local agent compute, provider queueing, or network wait without additional evidence. Client spans do not reveal server-side prefill/decode time. GPU readings may cover a shared device. Coverage labels and warnings belong to the result and should accompany exported research findings.
