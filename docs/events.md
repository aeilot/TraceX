# Backend-neutral live events

TraceX accepts append-only UTF-8 JSONL. Each newline commits one event. A partial final line is pending and retried after append. Complete malformed events retain the last valid server snapshot and expose a refresh error.

Events use `schema_version: 1` independently of profile schema v2. A record has exactly one payload matching its `type`:

| Type | Payload | Semantics |
| --- | --- | --- |
| `metadata` | `metadata: {run_id, name, duration_ms, configuration?}` | Update run metadata |
| `task` | `task: Task` | Insert or replace the complete task record by ID |
| `span` | `span: Span` | Insert or replace the complete span record by ID |
| `edge` | `edge: Edge` | Add an explicit relationship; duplicate triples are ignored |
| `resource` | `resource: Resource` | Insert or replace by task/component/runtime/timestamp |

Use [profile schema](schema.md) payloads, not arbitrary native objects. Optional `sequence` values must increase within a stream. Updates are complete replacements, not patches. A running span uses `incomplete: true`; its later completion keeps the same ID and sets `incomplete: false` with a measured duration. All events share a stable producer-defined run origin; do not mix absolute and relative timestamps.

```jsonl
{"schema_version":1,"sequence":1,"type":"metadata","metadata":{"run_id":"synthetic","name":"Synthetic producer","duration_ms":0}}
{"schema_version":1,"sequence":2,"type":"task","task":{"id":"t","name":"Example","status":"running","start_ms":0,"duration_ms":0,"evaluation_status":"not_run","cleanup_status":"not_started"}}
{"schema_version":1,"sequence":3,"type":"span","span":{"id":"model-1","task_id":"t","agent_id":"agent-1","name":"Model request","kind":"model","status":"running","start_ms":10,"duration_ms":0,"source":"custom","incomplete":true}}
{"schema_version":1,"sequence":4,"type":"span","span":{"id":"model-1","task_id":"t","agent_id":"agent-1","name":"Model request","kind":"model","status":"succeeded","start_ms":10,"duration_ms":200,"source":"custom","usage":{"input_tokens":120,"output_tokens":30}}}
```

Write task/endpoint records before dependent spans and links where possible. A live stream can publish forward references: unresolved records are withheld with warnings and recovered when later endpoints arrive. Do not infer a dependency from when an event happened to be appended.

Use `profile.AppendEvent` in Go, or one JSON object plus a newline in another language. [examples/producer/main.go](../examples/producer/main.go) demonstrates tasks, lifecycle intervals, concurrent branches, joins, usage, and resource samples; all values are synthetic.

```sh
mkdir -p .cache/live
: > .cache/live/events.jsonl
go run ./examples/producer --interval 300ms >> .cache/live/events.jsonl
# In another terminal:
go run ./cmd/tracex --run .cache/live/events.jsonl --watch 100ms
```

Mounted event streams maintain identity, byte offsets, pending tails, parsed records, and sequence state. Only appended bytes are read; truncation or replacement resets the source. Native ARIES JSONL also caches parsed records and offsets. Normalized snapshots and derived indexes are rebuilt from retained evidence, so incrementally reading a file does not imply constant-time analysis of the whole run.

## ARIES phase producer

ARIES's structured phase logs are adapted separately. Their absolute timestamps are normalized with other run evidence, and their explicit `depends_on` predecessor becomes a dependency edge only when both phases completed. See [integration](aries-integration.md). Existing logs without phase boundaries stay partial.
