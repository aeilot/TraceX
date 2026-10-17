# Verification

The P0–P2 implementation was verified locally on macOS/Apple M4 using Go 1.26.4 for TraceX, Go 1.26.5 for the ARIES producer, and Node.js 22.21.1. Local checks are recorded here; this does not assert that remote CI has completed.

## TraceX checks

`make check` covers Go unit tests, JavaScript syntax and 25 model/analysis/experiment tests, Go race tests, vet, and a binary build. Coverage includes schema v1 migration, v2 multi-parent joins and whole-graph cycles, explicit precedence critical paths, unknown timing, nested concurrency, interval unions, usage coverage/zero values, private-field filtering, resource context, overlapping range queries, stable filtered pages, forward references, SSE replay/cancellation, partial lines, malformed snapshots, truncation/rotation, and native incremental decoding.

Browser verification covered:

- DAG/timeline/split views, three-way joins, critical-path highlighting and navigation.
- Model usage and interval resource statistics in the inspector.
- Linked range selection, agent/type filters, page navigation and page-size changes.
- Baseline/candidate comparison with configuration warnings and computed changes.
- A 630-span imported profile: off-page spans remained searchable and full export contained all 630.
- Cycle rejection preserved the active profile.
- A 390-pixel viewport had no page overflow and could switch runs.
- Synthetic live events updated via SSE; pause/resume, last-valid retention after malformed append, and file-replacement recovery were exercised.

No browser page errors were observed in the main smoke test. The repeatable script is [scripts/browser-smoke.cjs](../scripts/browser-smoke.cjs); it expects the default two-demo workspace and a Playwright installation. The runtime app itself does not require Playwright or Node.

```sh
# With Playwright available in Node's module resolution:
make run
node scripts/browser-smoke.cjs
# Override TRACEX_URL when the demo server uses another address.
```

## Native evidence checks

Private retained data was read locally without printing message bodies or publishing artifacts:

| Source | Tasks | Spans | Resource samples | Observation |
| --- | ---: | ---: | ---: | --- |
| OpenClaw terminal task | 1 | 84 | 317 | Session + model completion usage + bridge + task lifecycle; unknown model duration stayed unavailable |
| Hermes BrowserBench sweep | 8 | 33,524 | 8,297 | Includes 31,313 network records; browser verified network pages 1–250 and 251–500 |

These counts describe retained artifact coverage, not benchmark accuracy or complete instrumentation. Real artifacts are not committed. Screenshots contain only synthetic demos.

## Scale checks

A synthetic server fixture contains 50,000 spans and 200,000 samples. Tests verify bounded pages, range indexes, resource peak/gap preservation, and incremental updates. A separate 45-runtime case verifies that a resource-budget warning does not block span browsing and that selecting one runtime restores its curve.

Indicative Apple M4 Go benchmark, three iterations (not a latency guarantee): indexed narrow `/api/view` about 1.0 ms/op; complete snapshot/index rebuild about 24.9 ms/op. Derived analysis still processes retained data; incremental file reads do not promise constant-time snapshot rebuilding.

```sh
go test ./internal/server -run TestLargeIndexedViewAndStreamUpdate
go test ./internal/server -run '^$' -bench 'LargeIndexedView|LargeSnapshotBuild' -benchtime=3x
```

## ARIES producer checks

Using ARIES's declared Go 1.26.5 toolchain, Runner/core unit tests, Runner race checks, and the changed scheduling/artifact/snapshot app-file tests and race checks passed. The entire app test package cross-compiled for Linux.

The complete ARIES app test package cannot build natively on this macOS host because existing SGLang process code uses Linux-only `unix.Waitid`, `P_PID`, and `Siginfo` without a Darwin implementation. This pre-existing issue was not changed as part of profiler instrumentation. No paid model runs or new benchmark containers were started for this work.

## Experiment launcher checks

The launcher passes `make check`, including the race detector and configured HTTP API tests. Tests use harmless helper processes rather than ARIES model workloads. Coverage includes private configuration copies, original profile path resolution, task subsets and arrival sequence preservation, model-call acknowledgment, single-job ownership, cancellation, independent outcomes, dynamic source mounts and browser-origin rejection.

An opt-in `TestOrbLifecycle` was exercised against the existing `aries-vm` with a synthetic Python process. It verified shared paths, Linux PID identity checking, actual child SIGINT delivery, a nonzero cancellation exit code and cleanup evidence. The updated ARIES CLI and bridge binaries also built successfully inside that VM. Neither check starts a paid model benchmark.

```sh
# Optional: requires an existing accessible OrbStack machine and Python 3 there.
TRACEX_TEST_ORB=aries-vm go test ./internal/experiment -run TestOrbLifecycle -count=1
```

An isolated Chrome UI check with API fixtures verified selected tasks and concurrency, acknowledgment reset, runner ownership after `stop_failed`, graceful-stop retry, arrival/loop notices and a 390-pixel layout without horizontal overflow. No page errors were observed.

A second browser check exercised the real HTTP server and a harmless local CLI helper without intercepting APIs: workload filtering, two declared tasks reduced to one, concurrency three, prepare completion, run acknowledgment, automatic `run-3` attachment, live DAG/timeline updates, graceful cancellation and confirmed cleanup. Its repeatable browser script is [scripts/launcher-smoke.cjs](../scripts/launcher-smoke.cjs); it refuses catalogs without the explicit synthetic fixture identity. It is not a benchmark invocation script.
