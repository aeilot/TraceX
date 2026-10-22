# Normalized profile schema v2

Schema v2 preserves explicit multi-parent relationships, native timing quality, model usage, runtime context, and experiment configuration. Version 1 inputs are accepted and normalized to v2; v1 edges remain restricted to parent relationships.

All timing uses finite, nonnegative milliseconds relative to one run origin. Sizes use bytes; utilization uses percent. An omitted/null metric is unavailable; zero is an observed zero.

| Object | Fields |
| --- | --- |
| Profile | `schema_version: 2`, `run_id`, `name`, `duration_ms`, `tasks`, `spans`, `edges`, `resources`, `capabilities`, `warnings`, optional `configuration` |
| Task | `id`, optional `logical_id`, `name`, `status`, `start_ms`, `duration_ms`, optional `time_quality`, separate `evaluation_status` and `cleanup_status`, optional `score` (continuous accuracy in [0, 1]), `end_reason` (lowercase enum), `network` (NetworkSummary), `flags` (`duration_outlier`, `sparse_monitor`), `attributes` (producer-namespaced scalars only) |
| NetworkSummary | optional `requests`, `failed`, `unsent`, `response_bytes`, `active_ms`, `navigations`, `by_resource_type` (allowlisted browser resource type → count; unknown types fold into `other`) |
| Span | `id`, `task_id`, `agent_id`, optional `parent_id` and `runtime_id`, `name`, `kind`, `status`, `start_ms`, `duration_ms`, `source`, optional `attributes`, `usage`, `incomplete` |
| Usage | optional `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`, `cost_usd`; missing and measured zero differ |
| Edge | `source`, `target`, `kind` |
| Resource | optional `task_id`, `component`, `runtime_id`, `time_ms`, optional `cpu_percent`, `memory_bytes`, `gpu_percent`, `memory_limit_bytes`, `memory_peak_bytes`, `pids`, `network_rx_bytes`, `network_tx_bytes` (cumulative per runtime), `cpu_throttling` (CPUThrottling) |
| CPUThrottling | `periods`, `throttled_periods`, `throttled_us`: cumulative cgroup `cpu.stat` counters per runtime; `throttled_periods` ≤ `periods`. Omitted means throttling was not measured, never zero. Shares derive from consecutive samples of one runtime; a counter decrease is a reset and yields no share |
| Capability | `name`, `status`, optional `reason` |

IDs are unique within each object class. Span/resource task references must resolve. The whole graph, including every edge kind, must be acyclic. A span's `parent_id` derives a parent edge. Repeated edges are deduplicated; explicit edge endpoints must exist.

## Relationships

| Kind | Meaning |
| --- | --- |
| `parent` | Containment of a child operation; must agree with its `parent_id` |
| `spawn` | Explicitly recorded agent/task creation |
| `call` | Explicit invocation relationship |
| `return` | Explicit result-return relationship |
| `dependency` | Recorded prerequisite work |
| `join` | Recorded branch completion prerequisite; several sources can target the same join |

A return relationship targets a distinct continuation/result span rather than pointing back to its containing invocation, which would create a cycle.

Timestamp order and overlap never create edges. Producer declarations are evidence, not an automatic guarantee of finish-before-start semantics. The completion critical-path analysis uses only dependency/join/return edges whose source finishes before the target starts. If these edges contradict timing or include incomplete work, the path is unavailable. It sums recorded durations and excludes waits not represented by spans; it is not the run's elapsed time.

## Timing quality and outcomes

Native records may set `attributes.time_quality` to `completion_only` or `unknown`. Such markers retain their timestamp and usage but do not count as a measured zero-duration model call. `incomplete: true` identifies an operation without a recorded end. A live producer replaces that span with its completed record under the same ID.

Task `time_quality: unknown` means its timeline placement is not known. Coverage is unavailable for that task. Empty quality on older normalized inputs preserves their declared interval semantics.

Lifecycle spans use `kind: lifecycle` and `attributes.phase` (`queue`, `sandbox_start`, `prepare`, `bridge_start`, `harness_start`, `execute`, `isolation`, `evaluate`, `cleanup`, or a producer-declared phase). Lifecycle/agent envelopes are excluded from observed leaf-work coverage and concurrency. Completed task latency quantiles use completed outcomes; a current running task is not a finished latency sample.

`evaluation_status` is the benchmark's binary pass/fail; `score` is its continuous accuracy when recorded, so a 0.99 answer can carry `evaluation_status: failed`. Analysis reports `score_mean` over the `score_known` tasks that recorded a score; tasks without one are excluded rather than counted as zero.

## Metadata

Configuration and attributes use explicit scalar allowlists. Configuration supports experiment conditions such as benchmark, harness, model, provider, concurrency, and independent runtime resource limits. Missing fields are unknown, not confirmed equality. See [ARIES integration](aries-integration.md) for the producer snapshot.

Attributes retain selected model, usage, tool, phase, timing-quality, provider, and browser resource-type metadata.

A producer may also keep its own scalar fields under a registered namespace prefix on span attributes, task attributes and configuration. The registered prefixes are `asb.` ([AgentSysBench](https://github.com/open-agent-infra/agent-sys-bench): unified-schema request, latency, io and token fields) and `tracelab.` ([TraceLab](https://github.com/uw-syfi/TraceLab): session, LLM-round, timing-event and tool fields). Namespaced values pass the same filter: strings up to 256 characters, finite nonnegative numbers, booleans; keys up to 128 characters. Unregistered keys are still dropped. Arbitrary fields, raw errors, URLs, prompts, arguments, and outputs are not part of the export contract. Browser normalization also removes unknown object fields.

## Branch-and-join example

```json
{
  "schema_version": 2,
  "run_id": "example",
  "name": "Explicit parallel work",
  "duration_ms": 1000,
  "configuration": {"harness": "custom", "concurrency": 2},
  "tasks": [{"id": "attempt-1", "name": "Research", "status": "succeeded", "start_ms": 0, "duration_ms": 1000, "evaluation_status": "unknown", "cleanup_status": "unknown"}],
  "spans": [
    {"id": "a", "task_id": "attempt-1", "agent_id": "researcher-a", "name": "Branch A", "kind": "tool", "status": "succeeded", "start_ms": 0, "duration_ms": 400, "source": "custom"},
    {"id": "b", "task_id": "attempt-1", "agent_id": "researcher-b", "name": "Branch B", "kind": "tool", "status": "succeeded", "start_ms": 0, "duration_ms": 500, "source": "custom"},
    {"id": "join", "task_id": "attempt-1", "agent_id": "coordinator", "name": "Synthesis", "kind": "model", "status": "succeeded", "start_ms": 600, "duration_ms": 300, "source": "custom", "usage": {"input_tokens": 100, "output_tokens": 20}}
  ],
  "edges": [{"source": "a", "target": "join", "kind": "join"}, {"source": "b", "target": "join", "kind": "join"}],
  "resources": [],
  "capabilities": [{"name": "resources", "status": "unsupported", "reason": "Not recorded"}],
  "warnings": []
}
```

Profiles are bounded to one million combined tasks, spans, resources, and edges. Normalized file and browser import size limits are described in [inputs](inputs.md); server pages do not change or truncate the underlying profile.
