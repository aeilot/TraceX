# AgentSysBench adapter

Brings [AgentSysBench](https://github.com/open-agent-infra/agent-sys-bench) (ASB) runs into TraceX without dropping any field ASB records. The experiment runner embeds these files and copies them into each private job directory; they also work standalone.

| File | Role |
| --- | --- |
| `asb2tracex.py` | ASB run → TraceX schema v2 profile, with an optional verbatim archive |
| `sample_cgroups.py` | 1 s cgroup v2 counters for every container of a compose project (stdlib only) |
| `check_lossless.py` | Cell-by-cell check of TraceX's own export against ASB's unified tables |
| `driver.sh` | Job driver used by the runner: prepare, or run with live conversion and cleanup |

## Mapping

`asb2tracex.py` reads ASB's unified schema through its own `sdk/analysis.extract`, so one mapping covers all ten settings:

| ASB | TraceX |
| --- | --- |
| request row | task; every column as task attribute `asb.<column>`; name is the benchmark task (e.g. `click-link`) |
| latency row (fast-service call) | span; `parent_call_id` → `parent_id`; every column as `asb.<column>` plus `asb.parent_service_name` |
| top-level / `e2e` | kind `agent` |
| `LLM` | kind `model` |
| `Sandbox/Search/Embedding/VecDB/Model/MCP` | kind `tool`, `tool.name` = ASB category |
| trajectory-delta row (no call id) | synthetic id under the request envelope, `time_quality: unknown` |
| token row | `usage` (input, output, cache read/write, reasoning, cost) on the k-th LLM span of the request |
| io row | `asb.io.<direction>_bytes/_tokens` on the k-th span of the same function |
| `preds.json` | `evaluation_status`, `score`; every field as `asb.pred.<key>` |
| extract diagnostics | `warnings` |
| cgroup samples | `resources`; components `harness`, `loadgen`, `proxy`, `service`, `monitoring` |

ASB token and io rows carry no call id, so they are matched by order within a request. Rows that match no span are summed onto the task (`asb.unmatched_tokens.*`, `asb.io.<function>.*`), and each profile's warnings give the counts. ASB's own semantics are kept as recorded; for example, DeepSeek's `prompt_cache_miss_tokens` arrive as `cache_write_tokens`.

## Runner

With `--asb-dir` (or **New experiment → Runner settings → AgentSysBench**), the runner lists the deployments under `benchmark/deployment/`. It sends a request count rather than a task selection, since the loadgen sends the first *N* manifest requests. Three settings fix their own concurrency (`qa-rag`, `multimodal-hugginggpt`, `ai-search-deep-research`).

- **Prepare** builds `agsbench-request:v1` and the setting's agent image, then runs `1.prepare_data.sh`. It makes no model calls.
- **Run** samples cgroups, starts `2.run.sh` with the setting's request/concurrency variables, and writes `artifacts/<run>/profile.json` every 10 s while the loadgen runs; TraceX mounts it as a live source. Afterwards it runs `3.cleanup.sh`, `4.evaluate.sh` where present, and a final conversion that archives the unified tables, diagnostics, preds and samples next to the profile, plus a copy of `benchmark/data/<setting>/`.
- **Stop** interrupts the driver, which runs `3.cleanup.sh` before exiting.

Conversion runs inside `agsbench-request:v1`, which ships pandas, so the execution host needs Docker, Compose v2 and a stdlib `python3`. Configure models with ASB's `scripts/configure-api.sh` on that host; TraceX never handles the keys.

## Standalone

```sh
python3 asb2tracex.py --asb-home ASB --setting browser-use-webagent \
  --data ASB/benchmark/data/browser-use-webagent \
  --cgroup-samples samples.jsonl --archive out/asb -o out/profile.json
tracex --run out/profile.json --export out/export.json
python3 check_lossless.py out/export.json out/asb
```

`--tables DIR` reads ASB's exported `tables/` instead of raw data (e.g. `benchmark/analysis/examples/<setting>/tables`).

## Known upstream issues (ASB d653ab2)

- The deployments' cAdvisor v0.49.1 sees only the root cgroup under Docker 29, and the Prometheus keep-regex never matches compose container names, so container metrics are empty; the runner samples cgroups directly.
- Several images default to aliyun/ghfast mirrors; outside China, run `PIP_INDEX_URL=https://pypi.org/simple/ GITHUB_PROXY= bash scripts/setup.sh` and pass `--build-arg APT_MIRROR=deb.debian.org` where a Dockerfile uses one.
