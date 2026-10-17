# Architecture

TraceX is a standalone consumer of run evidence. It does not control ARIES execution, evaluator lifecycle, runtime cleanup, or model calls.

```text
ARIES run / harness telemetry / normalized JSON
                       |
                internal/profile
              adapters + validation
                       |
                immutable snapshot
                       |
                internal/server
                       |
            embedded web application
                  DAG + timeline
```

`cmd/tracex` selects input and configures the local server or JSON export. `internal/profile` owns ingestion, normalization, validation, and synthetic fixtures. `internal/server` serves a fixed profile and an allowlist of embedded assets. `web` contains the offline client, with no CDN or build step.

## HTTP interface

| Route | Response |
| --- | --- |
| `GET /api/profile` | Normalized profile JSON, `Cache-Control: no-store` |
| `GET /api/health` | `{"status":"ok"}` |
| `GET /` | Embedded application |
| `GET /app.js`, `GET /profile.js`, `GET /styles.css` | Embedded assets |

All routes support HEAD. Other methods return 405. Unknown paths return 404. No file browsing, run-directory access, upload API, or server-side mutation is provided. Every response carries `X-Content-Type-Options: nosniff`. The process shuts down on SIGINT or SIGTERM.

To add a backend, translate its recorded evidence into `profile.Profile` and validate it. Retain source labels and capability reasons, use unique task occurrence IDs, and only record a parent relationship when the source provides it. Resource samples may be shared at component/runtime scope; do not distribute shared usage across tasks without evidence.
