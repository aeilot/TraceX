# Architecture

TraceX interprets evidence and optionally launches an explicitly configured ARIES CLI. ARIES retains ownership of agent execution, evaluator isolation, model calls and cleanup.

```text
ARIES native artifacts / normalized profiles / JSONL events
                            |
                     internal/profile
             adapters, validation, incremental decode
                            |
               immutable snapshot + analysis + indexes
                            |
                     internal/server
             filtered pages, envelopes, revisions (SSE)
                            |
                   embedded web application
       DAG / timeline / GPU+CPU+memory / comparison / details
```

`cmd/tracex` selects sources, startup discovery, watching, JSON export, optional experiment configuration, and the local listener. `internal/experiment` owns profile selection, private job configuration, child process lifetime and graceful cancellation; it delegates workload behavior to ARIES. `internal/profile` owns evidence interpretation, schema migration, graph validation, analysis, and producer contracts. `internal/server` owns mounted snapshots, indexes, bounded queries, resource envelopes, and revision subscriptions. The browser uses the same validation/analysis semantics for local imports. All runtime dependencies are in the Go standard library; the UI has no CDN or build step.

## HTTP interface

| Route | Response |
| --- | --- |
| `GET /api/runs` | Workspace runs, stable mount IDs, revision, watching state, configuration and coverage |
| `GET /api/profile?run=run-1` | Complete normalized snapshot; export includes data outside current filters/pages |
| `GET /api/view?run=run-1` | Current filtered page, resource envelope, full-run analysis, counts and revision |
| `GET /api/span?run=run-1&id=SPAN_ID` | Span, all incident recorded edges, interval resource statistics |
| `GET /api/analysis?run=run-1` | Full-run analysis |
| `GET /api/events` | Reconnectable `revision` server-sent events with event IDs and heartbeats |
| `GET /api/health` | `{"status":"ok"}` |
| `GET /api/experiments/catalog` | Allowlisted profile metadata and runner readiness |
| `GET /api/experiments/jobs` | In-memory jobs, process state, run mount and outcome summaries |
| `POST /api/experiments/start` | Explicit prepare/run submission; requires configured runner |
| `POST /api/experiments/stop` | Graceful interrupt of the owned job |
| `GET /`, bundled JS and CSS paths | Allowlisted bundled assets |

Read-only JSON and static routes support HEAD; the SSE route requires a streaming GET. Experiment mutations require bounded JSON POST and reject a mismatched browser Origin. Other methods return 405. Unknown paths return 404. API responses use no-store and nosniff. No raw file browsing, upload endpoint, or model API is provided. Experiment commands use fixed CLI arguments, selected local presets and private generated profiles; request bodies cannot supply executable paths or shell commands.

### View query

`task`, `kind`, `q`, `runtime`, `start_ms`, and `end_ms` filter evidence. Time matching includes spans that overlap the selected window, even if they started before it. `offset` and `limit` page stable `(start_ms,id)` ordering after filtering. Default limit is 250, maximum 1,000. The UI offers pages of 50, 250, or 500; it has no inaccessible tail behind a display cutoff.

`points` bounds returned resource observations (default 300, maximum 1,000). Each task/component/runtime series stays separate. Envelopes preserve first/last, per-metric extrema, and missing-value transitions. If that evidence cannot fit, `resource_error` explains the omission while spans and runtime choices still load. Narrow the interval or choose a runtime. A missing curve in that response must not be described as no measurements.

Full-run analysis is not recomputed from the current page. A graph page returns edges whose endpoints are visible and reports `hidden_edges` for cross-page relationships. Span details can navigate to those neighbors.

### Watching and ownership

The watcher fingerprints sources, incrementally decodes changed JSONL, and publishes a new immutable snapshot/index only after validation. A malformed refresh preserves the previous revision and reports an error. SSE reconnects replay retained revision notices; each notice causes the browser to query the latest view. Source reading and derived snapshot construction remain bounded; this is a local in-memory profiler, not a distributed warehouse.

Cancellation stops watcher and subscriber work. HTTP shutdown uses a bounded context. A source is read-only; source updates belong to the original producer. `--runs-dir` discovery runs at startup, while all mounted sources remain watchable afterward. Experiment outputs attach dynamically through an idempotent job-to-mount mapping and publish normal revision events. The mount collection is synchronized; each reader uses immutable snapshots. See [experiment ownership and shutdown](experiments.md).
