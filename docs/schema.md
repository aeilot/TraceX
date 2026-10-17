# Normalized profile schema v1

All times are finite, nonnegative milliseconds relative to the run origin. Sizes use bytes; resource utilization uses percent. JSON uses snake_case. A normalized file can be loaded with `--run` or produced with `--export`.

| Object | Fields |
| --- | --- |
| Profile | `schema_version: 1`, `run_id`, `name`, `duration_ms`, `tasks`, `spans`, `edges`, `resources`, `capabilities`, `warnings` |
| Task | `id`, `name`, `status`, `start_ms`, `duration_ms`, `evaluation_status`, `cleanup_status` |
| Span | `id`, `task_id`, `agent_id`, optional `parent_id`, `name`, `kind`, `status`, `start_ms`, `duration_ms`, `source`, optional `attributes` |
| Edge | `source`, `target`, `kind: "parent"` |
| Resource | optional `task_id`, `component`, `runtime_id`, `time_ms`, optional `cpu_percent`, `memory_bytes`, `gpu_percent` |
| Capability | `name`, `status`, optional `reason` |

IDs are unique within each object class. A span must reference an existing task. A resource's optional task ID must resolve. Parent IDs must resolve to existing spans and form an acyclic graph. Explicit parent IDs determine graph edges; speculative edges are rejected. Arrays can be empty.

An absent resource value means no reading; a numeric zero means a measured zero. Task execution, evaluation, and cleanup statuses are separate so success at one stage cannot hide failure at another. Source labels and capability warnings let viewers distinguish observed detail from missing coverage.

Example:

```json
{
  "schema_version": 1,
  "run_id": "example",
  "name": "Example backend",
  "duration_ms": 1000,
  "tasks": [{
    "id": "task-1-attempt-1", "name": "Inspect repository", "status": "success",
    "start_ms": 0, "duration_ms": 1000,
    "evaluation_status": "unknown", "cleanup_status": "unknown"
  }],
  "spans": [
    {"id": "agent", "task_id": "task-1-attempt-1", "agent_id": "researcher",
     "name": "Research", "kind": "agent", "status": "success",
     "start_ms": 0, "duration_ms": 1000, "source": "custom-backend"},
    {"id": "tool", "parent_id": "agent", "task_id": "task-1-attempt-1", "agent_id": "researcher",
     "name": "Read files", "kind": "tool", "status": "success",
     "start_ms": 100, "duration_ms": 200, "source": "custom-backend"}
  ],
  "edges": [{"source": "agent", "target": "tool", "kind": "parent"}],
  "resources": [],
  "capabilities": [{"name": "resources", "status": "unsupported", "reason": "Not recorded by this backend"}],
  "warnings": []
}
```

Profile validation limits total task, span, and resource records to 100,000. Span attributes are restricted to selected model, usage, tool, exit-code, and synthetic metadata. Prompts, tool arguments, and outputs are not part of this schema contract.
