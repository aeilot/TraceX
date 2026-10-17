# Run ARIES experiments

TraceX can launch the real ARIES CLI as well as inspect existing evidence. Execution is opt-in: without `--aries-dir`, the experiment panel explains how to configure a runner and cannot start processes.

## Configure a runner

On a Linux host with a built ARIES checkout:

```sh
./bin/tracex --aries-dir /absolute/path/to/ARIES
```

On macOS, use an existing OrbStack Linux machine with the checkout shared into it:

```sh
./bin/tracex --aries-dir /Users/you/Developer/ARIES \
  --aries-executor orb --aries-machine aries-vm
```

Build ARIES and configure its Docker, workload prerequisites, model credentials and backend on that execution host first. TraceX does not create machines, provision GPUs, replace ARIES profiles, or infer credentials. The default executable is `ARIES_DIR/bin/aries`; `--aries-bin` overrides it. Orb uses the shared `/mnt/mac` mount and the selected machine's execution environment. The catalog checks binary availability and host compatibility; readiness does not assert that Docker, datasets, credentials, or the model endpoint are healthy.

## Select and start

Open **New experiment**, filter by workload, and choose a profile. Each preset identifies its harness, model and backend. The task checklist uses tasks declared in that profile; it does not enumerate an unprepared dataset. Choose a subset and concurrency. Arrival profiles retain their entire declared task sequence. Loop duration and arrival scheduling remain those of the selected preset.

**Prepare workload** invokes `aries setup PROFILE.json`: this may download datasets or build images, but does not run the model benchmark. **Run experiment** invokes `aries PROFILE.json` and requires the model-call acknowledgment for each submission. It can incur provider charges and consume local compute. Opening the panel or selecting a profile never starts a process.

The server validates the selection again, allows one active job, and never automatically retries a start. It copies the selected configuration into a private job directory, preserves profile-relative configuration paths, and overrides only selected tasks, concurrency and the output location. The original profile is unchanged. Private copies and logs can contain sensitive configuration; they are not returned by the HTTP API.

## Observe and stop

As ARIES creates its output directory, TraceX mounts it automatically and opens its live DAG and timeline. The experiment list shows process state and exit code separately from harness, evaluation and cleanup outcomes. Process completion alone does not establish benchmark success or successful cleanup. Missing cleanup evidence remains unconfirmed.

**Stop gracefully** sends an interrupt to the owned ARIES process. ARIES retains responsibility for containers, isolation and cleanup. On Orb, cancellation validates the Linux process's unique generated profile argument before signaling it; it does not merely kill the macOS transport. TraceX waits up to `--stop-timeout` (default two minutes). It never force kills ARIES after this deadline. A stop failure keeps the job owned and blocks another start; inspect the private logs and retry stopping or recover the execution host. Closing a browser tab does not stop the job. Stopping the TraceX service requests graceful ARIES shutdown.

Job state is held in memory. Generated configurations, private stdout/stderr and artifacts remain under `ARIES_DIR/.cache/tracex/jobs` (override with `--jobs-dir`). After restarting TraceX, inspect retained outputs with `--run JOB_DIR/artifacts/RUN_ID`; do not assume an interrupted service has cleaned an execution host. Keep experiment controls on loopback unless you intentionally grant other clients access to the execution host.

Experiment-enabled serving requires watching; `--watch 0` becomes one second. Existing `--run` inputs and the two default synthetic demonstrations remain available alongside real runs. Export mode cannot be combined with `--aries-dir`.
