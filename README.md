# TraceX

A local, offline agent workload profiler for [ARIES](https://github.com/hyscale-lab/ARIES). Inspect an agent DAG alongside a linear timeline, correlate resource samples, and inspect the evidence behind each span. TraceX is an independent visualization extension; BrowserBench remains a workload extension.

The default view uses **synthetic demo data**. It does not represent a measured benchmark result.

![TraceX synthetic demo with an agent DAG, linear timeline, and resource signals](docs/images/explorer.png)

## Run

Go 1.26 or later is required. The application uses the Go standard library and embedded HTML, JavaScript, and CSS; no Node installation or remote UI assets are needed.

```sh
git clone https://github.com/aeilot/TraceX.git
cd TraceX
make run
```

Open [http://127.0.0.1:4318](http://127.0.0.1:4318). To load your own data:

```sh
go run ./cmd/tracex --run /path/to/aries/runs/RUN_ID
go run ./cmd/tracex --run /path/to/profile.json --addr 127.0.0.1:4319
```

The server reads one snapshot at startup. Restart it to refresh a changed run. It binds to loopback by default; setting a public address makes the profile accessible on that interface. Stop with Ctrl-C.

Export a portable normalized snapshot without starting the server:

```sh
go run ./cmd/tracex --run /path/to/aries/runs/RUN_ID --export profile.json
go run ./cmd/tracex --export - > demo-profile.json
make build
./bin/tracex --help
```

## Explore

- Switch between **Agent DAG**, **Timeline**, and **Split view**. Parent edges never come from timestamp order.
- Filter by task occurrence, span type, or agent/name search (`/` focuses search).
- Select a DAG node or timeline bar to inspect timing, source, IDs, and outcomes. `Escape` closes details.
- Zoom the timeline or fit the DAG; filter resource charts by runtime. CPU and memory signals use the same run origin.
- Import normalized JSON directly in the browser or export the current snapshot. Browser imports remain local.

## Evidence and backend compatibility

TraceX separates the display model from data ingestion. ARIES task lifecycle and resource artifacts, recognized harness telemetry, and normalized TraceX JSON map into one profile. A backend can produce normalized JSON without taking a dependency on ARIES or on the UI. See [input formats and measurement limits](docs/inputs.md) and the [normalized schema](docs/schema.md).

DAG edges require an explicit parent ID. Timestamp order or overlap never establishes a dependency. Missing parent relationships produce disconnected roots. The timeline displays elapsed time; overlapping spans do not add up to wall time. Missing resource samples remain missing, rather than becoming measured zero.

This first version provides offline snapshots, not live collection, distributed clock correction, or arbitrary backend auto-detection. OpenClaw native trajectories are unsupported unless a recognized OpenTelemetry artifact is available. Browser evidence is optional and is not required to inspect a standard ARIES run.

## Local data

The service exposes only normalized profile JSON and bundled assets. It does not serve the input directory or raw logs, call model APIs, or upload runs. Input names and selected metadata still appear in profiles: review exported data before sharing. Private run directories and exports belong outside version control; `runs/` and `exports/` are ignored.

## Develop

```sh
make check
```

Node.js 22 or later is needed only for frontend verification. This runs Go unit tests, browser-model tests using Node’s built-in test runner, JavaScript syntax checks, the race detector, Go vet, and a binary build. Go caches live under `.cache/`. CLI tests cover export/reload, invalid inputs, occupied ports, and cancellation; HTTP tests cover assets, MIME types, API JSON, read-only methods, and raw-file isolation. See [architecture](docs/architecture.md) and [verification evidence](docs/verification.md).
