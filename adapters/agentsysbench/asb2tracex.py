"""Convert an AgentSysBench (ASB) run into a TraceX normalized profile (schema v2).

Nothing ASB records is dropped. ASB's analysis SDK (sdk/analysis) maps every
setting's raw output onto one schema: requests / latency / io / tokens joined
on request_id. This adapter carries every column of that schema into TraceX:

  requests row -> task; every column as task attribute asb.<column>
  latency row  -> span; parent_call_id -> parent_id; every column as asb.<column>,
                  plus parent_service_name from the raw fast-service CSV
    top-level / e2e -> kind "agent"; LLM -> "model";
    Sandbox/Search/Embedding/VecDB/Model/MCP -> "tool" (tool.name = category)
  trajectory-delta rows (no call id) -> synthetic ids under the request envelope
  tokens row   -> usage (input/output/cache read/cache write/reasoning/cost) and
                  asb.model / asb.total_tokens on the k-th LLM span of the request
  io row       -> asb.io.<direction>_bytes / _tokens on the k-th span with the
                  same function in the request
  preds.json   -> evaluation_status / score, every field as asb.pred.<key>
  extract diagnostics -> warnings
  cgroup v2 samples (sample_cgroups.py) or cAdvisor (Prometheus) -> resources

TraceX keeps only scalars (strings <= 256 chars). Rows that match no span are
summed onto the task and named in a warning; long strings are truncated with an
asb.<key>_truncated flag. With --archive, the four unified tables, diagnostics,
preds and the raw Prometheus series are written verbatim, so the archive plus
the raw benchmark/data directory is the lossless record.

Usage:
  uv run --with pandas --with numpy python -I asb2tracex.py \\
      --asb-home ~/Developer/research/external/agent-sys-bench \\
      --setting browser-use-webagent --data .../benchmark/data/browser-use-webagent \\
      --prometheus http://aries-vm.orb.local:9090 --config request_num=1 \\
      --archive runs/X/asb -o runs/X/profile.json
  # or, from exported tables (no raw data):
  ... --tables benchmark/analysis/examples/qa-rag/tables --setting qa-rag
"""

import argparse
import json
import math
import os
import sys
import urllib.parse
import urllib.request

import pandas as pd

SOURCE = "agent-sys-bench"
NS = "asb."
TOOL_CATEGORIES = {"Sandbox", "Search", "Embedding", "VecDB", "Model", "MCP"}
MAX_STR = 256


def num(v, default=None):
    try:
        f = float(v)
    except (TypeError, ValueError):
        return default
    return f if math.isfinite(f) else default


def blank(v):
    return v is None or (isinstance(v, float) and math.isnan(v)) or str(v) in ("", "None", "nan")


def put(attrs, key, v):
    """Store v under asb.<key> as a TraceX scalar, never silently dropping it."""
    k = NS + key
    if blank(v):
        return
    if not isinstance(v, str) and str(v) in ("True", "False"):  # bool or numpy.bool_
        attrs[k] = str(v) == "True"
        return
    f = num(v)
    if f is not None and not isinstance(v, str):
        if f >= 0:
            attrs[k] = f
        else:  # TraceX numbers are nonnegative; keep the sign as text
            attrs[k] = str(f)
        return
    s = v if isinstance(v, str) else json.dumps(v, default=str)
    if len(s) > MAX_STR:
        attrs[k] = s[:MAX_STR]
        attrs[k + "_truncated"] = True
    else:
        attrs[k] = s


def load(args):
    """Return (agent, agent_meta, requests, latency, io, tokens, diagnostics, raw_trace)."""
    if args.tables:
        read = lambda n: (pd.read_csv(os.path.join(args.tables, n + ".csv"))
                          if os.path.exists(os.path.join(args.tables, n + ".csv")) else pd.DataFrame())
        return (args.agent or args.setting, {}, read("requests"), read("latency"), read("io"),
                read("tokens"), [], {})
    sys.path.insert(0, os.path.join(args.asb_home, "sdk"))
    from analysis import AGENT_META, SETTING_AGENT  # noqa: E402
    from analysis import config as C  # noqa: E402
    from analysis._parse import parse_e2e_csv  # noqa: E402
    from analysis.extract import extract  # noqa: E402
    agent = args.agent or SETTING_AGENT[args.setting]
    data = extract(agent, args.data)
    raw = {}
    e2e = C.AGENT_LAYOUT.get(agent, {}).get("e2e_csv")
    if e2e:
        for r in parse_e2e_csv(os.path.join(args.data, e2e)):
            raw[r["call_id"]] = r
    return (agent, AGENT_META.get(agent, {}), data.requests, data.latency, data.io, data.tokens,
            data.diagnostics, raw)


def load_preds(data_dir):
    if not data_dir:
        return {}, None
    for rel in ("trajectories/preds.json", "preds.json"):
        p = os.path.join(data_dir, rel)
        if os.path.exists(p):
            with open(p) as f:
                raw = json.load(f)
            items = raw.items() if isinstance(raw, dict) else ((r.get("task_id") or r.get("instance_id"), r) for r in raw)
            return {str(k): v for k, v in items if k is not None}, p
    return {}, None


def kind_for(row):
    if str(row.get("is_top_level")) == "True" or row.get("category") == "e2e":
        return "agent"
    cat = row.get("category")
    if cat == "LLM":
        return "model"
    if cat in TOOL_CATEGORIES:
        return "tool"
    return "other"


def build(args):
    agent, meta, requests, latency, io, tokens, diags, raw = load(args)
    warnings = [f"asb extract: {d}" for d in diags]
    preds, preds_path = load_preds(args.data)
    if latency.empty and requests.empty:
        raise SystemExit("no ASB data: " + "; ".join(diags))

    origin = num(latency["start_time"].min(), 0.0) if not latency.empty else 0.0
    ms = lambda t: max(0.0, (num(t, origin) - origin) * 1000.0)

    # ── spans from latency ──────────────────────────────────────────
    spans, by_id = [], {}
    top_by_req, llm_by_req, by_fn = {}, {}, {}
    if not latency.empty:
        latency = latency.sort_values(["request_id", "start_time"], kind="stable")
        traj_seq = 0
        for _, r in latency.iterrows():
            rid = str(r["request_id"])
            sid = None if blank(r.get("call_id")) else str(r["call_id"])
            if sid is None:
                traj_seq += 1
                sid = f"{rid}:traj-{traj_seq}"
            if sid in by_id:
                warnings.append(f"duplicate latency call_id {sid}; kept the first row")
                continue
            k = kind_for(r)
            fn = str(r.get("function_name") or "")
            attrs = {"harness": agent}
            for col in latency.columns:
                put(attrs, col, r[col])
            if sid in raw:
                put(attrs, "parent_service_name", raw[sid]["parent_service_name"])
            if k == "tool":
                attrs["tool.name"] = str(r.get("category"))
            if r.get("delay_type") == "traj":
                attrs["time_quality"] = "unknown"
                attrs["synthetic"] = True
            span = {"id": sid, "task_id": rid, "agent_id": agent, "name": fn.split(".")[-1] or k,
                    "kind": k, "status": "succeeded", "start_ms": ms(r["start_time"]),
                    "duration_ms": max(0.0, num(r.get("delay"), 0.0) * 1000.0),
                    "source": SOURCE, "attributes": attrs}
            if not blank(r.get("parent_call_id")):
                span["parent_id"] = str(r["parent_call_id"])
            if k == "agent" and str(r.get("is_top_level")) == "True":
                top_by_req.setdefault(rid, sid)
            if k == "model":
                llm_by_req.setdefault(rid, []).append(span)
            by_fn.setdefault((rid, fn), []).append(span)
            spans.append(span)
            by_id[sid] = span
        dangling = 0
        for s in spans:
            if s.get("parent_id") and s["parent_id"] not in by_id:
                del s["parent_id"]  # original id stays in asb.parent_call_id
                dangling += 1
            if not s.get("parent_id") and s["kind"] != "agent" and s["task_id"] in top_by_req:
                s["parent_id"] = top_by_req[s["task_id"]]
        if dangling:
            warnings.append(f"{dangling} spans referenced a parent call absent from the trace; "
                            "reattached to the request envelope (original id kept in asb.parent_call_id)")

    task_extra = {}  # rid -> attrs summed from rows that matched no span

    def add_extra(rid, key, v):
        a = task_extra.setdefault(rid, {})
        a[NS + key] = a.get(NS + key, 0.0) + (num(v, 0.0) or 0.0)

    # ── tokens -> usage on the k-th LLM span ───────────────────────
    model_name, matched, unmatched = "", 0, 0
    if not tokens.empty:
        for (rid, cat), g in tokens.groupby(["request_id", "category"], sort=False):
            rid = str(rid)
            targets = llm_by_req.get(rid, []) if cat == "LLM" else []
            for i, (_, t) in enumerate(g.iterrows()):
                if not blank(t.get("model")):
                    model_name = model_name or str(t["model"])
                tgt = targets[i] if i < len(targets) else None
                if tgt is None and not blank(t.get("call_id")):
                    tgt = by_id.get(str(t["call_id"]))
                if tgt is None:
                    unmatched += 1
                    pre = f"unmatched_tokens.{cat}."
                    add_extra(rid, pre + "calls", 1)
                    for col in ("input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens",
                                "reasoning_tokens", "total_tokens", "cost_usd"):
                        add_extra(rid, pre + col, t.get(col))
                    continue
                matched += 1
                tgt["usage"] = {k: num(t.get(k), 0.0) for k in
                                ("input_tokens", "output_tokens", "cache_read_tokens",
                                 "cache_write_tokens", "reasoning_tokens", "cost_usd")}
                for col in ("model", "total_tokens", "function_name", "category"):
                    put(tgt["attributes"], "token." + col, t.get(col))
                if not blank(t.get("model")):
                    tgt["attributes"]["model"] = str(t["model"])[:MAX_STR]
        warnings.append(f"token rows attached to LLM spans by per-request order ({matched} matched; "
                        f"{unmatched} without a traced call, summed on the task as asb.unmatched_tokens.*)")

    # ── io -> k-th span of the same function ───────────────────────
    if not io.empty:
        io_matched = io_unmatched = 0
        for (rid, fn, direction), g in io.groupby(["request_id", "function_name", "direction"], sort=False):
            rid, fn = str(rid), str(fn)
            targets = by_fn.get((rid, fn), [])
            for i, (_, row) in enumerate(g.iterrows()):
                tgt = targets[i] if i < len(targets) else None
                if tgt is None:
                    io_unmatched += 1
                    short = fn.split(".")[-1]
                    add_extra(rid, f"io.{short}.{direction}_bytes", row.get("bytes"))
                    add_extra(rid, f"io.{short}.{direction}_tokens", row.get("tokens"))
                    add_extra(rid, f"io.{short}.{direction}_rows", 1)
                    continue
                io_matched += 1
                put(tgt["attributes"], f"io.{direction}_bytes", row.get("bytes"))
                put(tgt["attributes"], f"io.{direction}_tokens", row.get("tokens"))
        warnings.append(f"io rows attached to spans by (request, function, order) ({io_matched} matched; "
                        f"{io_unmatched} summed on the task as asb.io.<function>.*)")

    # ── tasks from requests ────────────────────────────────────────
    tasks = []
    req_rows = {str(q["request_id"]): q for _, q in requests.iterrows()} if not requests.empty else {}
    all_rids = list(req_rows) + sorted({s["task_id"] for s in spans} - set(req_rows))
    for rid in all_rids:
        q = req_rows.get(rid)
        mine = [s for s in spans if s["task_id"] == rid]
        top = by_id.get(top_by_req.get(rid, ""))
        if top:
            start, dur, tq = top["start_ms"], top["duration_ms"], ""
        elif mine:
            start = min(s["start_ms"] for s in mine)
            dur, tq = max(s["start_ms"] + s["duration_ms"] for s in mine) - start, ""
        else:
            start, dur, tq = 0.0, num(q.get("e2e_latency"), 0.0) * 1000.0 if q is not None else 0.0, "unknown"
        attrs = {}
        if q is None:
            status = "unknown"
            put(attrs, "in_valid_task_set", False)
        else:
            for col in requests.columns:
                put(attrs, col, q[col])
            status = "succeeded" if str(q.get("success")).lower() in ("true", "1") else "failed"
        name = rid
        if isinstance(preds.get(rid), dict):  # e.g. browsergym/miniwob.click-link -> click-link
            bench_task = str(preds[rid].get("task_id") or "").rsplit("/", 1)[-1].rsplit(".", 1)[-1]
            name = bench_task or rid
        task = {"id": rid, "name": name, "status": status, "start_ms": start, "duration_ms": dur,
                "evaluation_status": "unknown", "cleanup_status": "unknown"}
        if tq:
            task["time_quality"] = tq
        if q is not None and status == "failed" and not blank(q.get("error")):
            task["end_reason"] = "error"
        rec = preds.get(rid)
        if isinstance(rec, dict):
            for k, v in rec.items():
                put(attrs, "pred." + k, v)
            r = next((num(rec[k]) for k in ("reward", "score", "resolved") if k in rec), None)
            if r is not None:
                task["score"] = min(1.0, max(0.0, r))
                task["evaluation_status"] = "passed" if r > 0 else "failed"
        elif rec is not None:
            put(attrs, "pred", rec)
        attrs.update(task_extra.get(rid, {}))
        task["attributes"] = attrs
        tasks.append(task)
    orphan = [t for t in tasks if t["status"] == "unknown"]
    if orphan:
        warnings.append(f"{len(orphan)} traced requests are not in ASB's valid-task set; status unknown")
    extra_preds = sorted(set(preds) - {t["id"] for t in tasks})
    if extra_preds:
        warnings.append(f"{len(extra_preds)} preds.json entries have no traced request: {', '.join(extra_preds[:10])}")

    # ── resources ──────────────────────────────────────────────────
    resources, prom_raw = [], {}
    if args.cgroup_samples and spans:
        t1 = origin + end_of(spans) / 1000.0
        resources = cgroup_resources(args.cgroup_samples, origin, t1 + 10, warnings)
        harness = sorted({r["runtime_id"] for r in resources if r["component"] == "harness"})
        if len(harness) == 1:  # every fast-service call runs inside the one agent container
            for sp in spans:
                sp["runtime_id"] = harness[0]
        elif harness:
            warnings.append(f"{len(harness)} harness containers; spans carry no runtime_id")
    if args.prometheus and spans and not resources:
        t1 = origin + end_of(spans) / 1000.0
        resources, prom_raw = scrape_prometheus(args.prometheus, origin - 10, t1 + 10, origin, warnings)
    caps = [
        {"name": "resources", "status": "supported" if resources else "unsupported",
         "reason": ("cgroup v2 counters per container (sample_cgroups.py)" if args.cgroup_samples
                    else "cAdvisor via the deployment's Prometheus") if resources
         else "No container samples (pass --cgroup-samples, or --prometheus while the deployment is up)"},
        {"name": "queue", "status": "unsupported", "reason": "ASB loadgen does not record admission or queue time"},
        {"name": "lifecycle_phases", "status": "unsupported",
         "reason": "fast-service traces agent calls only, not container start or evaluation"},
    ]

    config = {"benchmark": f"agent-sys-bench/{args.setting}", "harness": agent, "task_count": len(tasks)}
    if args.model or model_name:
        config["model"] = args.model or model_name
    if args.provider:
        config["provider"] = args.provider
    if args.concurrency:
        config["concurrency"] = args.concurrency
    put(config, "setting", args.setting)
    put(config, "agent", agent)
    for k, v in meta.items():
        put(config, "agent_" + k, v)
    for kv in args.config or []:
        k, _, v = kv.partition("=")
        put(config, k, num(v) if num(v) is not None and v.strip() else v)
    if preds_path:
        put(config, "preds_file", os.path.relpath(preds_path, args.data))

    end = max([t["start_ms"] + t["duration_ms"] for t in tasks] +
              [s["start_ms"] + s["duration_ms"] for s in spans] + [0])
    prof = {"schema_version": 2, "run_id": args.run_id or f"asb-{args.setting}",
            "name": args.name or f"AgentSysBench {args.setting}", "duration_ms": end,
            "configuration": config, "tasks": tasks, "spans": spans, "edges": [],
            "resources": resources, "capabilities": caps, "warnings": warnings}
    if args.archive:
        archive(args.archive, requests, latency, io, tokens, diags, preds, prom_raw, origin)
        if args.cgroup_samples:
            import shutil
            shutil.copy(args.cgroup_samples, os.path.join(args.archive, "cgroup_samples.jsonl"))
    return prof


def end_of(spans):
    return max(s["start_ms"] + s["duration_ms"] for s in spans)


def cgroup_resources(path, origin, t_end, warnings):
    """TraceX resources from sample_cgroups.py JSONL; CPU% from usage_usec deltas."""
    out, prev = [], {}
    with open(path) as f:
        rows = [json.loads(l) for l in f if l.strip()]
    for r in sorted(rows, key=lambda x: x["t"]):
        name, t, cpu = r["name"], r["t"], r.get("cpu", {})
        p = prev.get(name)
        prev[name] = r
        if t < origin or t > t_end:
            continue
        rec = {"component": component_for(r.get("service") or name), "runtime_id": name,
               "time_ms": (t - origin) * 1000.0}
        if p and t > p["t"] and "usage_usec" in cpu and cpu["usage_usec"] >= p.get("cpu", {}).get("usage_usec", 0):
            rec["cpu_percent"] = (cpu["usage_usec"] - p["cpu"]["usage_usec"]) / ((t - p["t"]) * 1e6) * 100.0
        for field, key in (("memory_bytes", "memory_current"), ("memory_peak_bytes", "memory_peak"),
                           ("memory_limit_bytes", "memory_max"), ("pids", "pids_current"),
                           ("network_rx_bytes", "net_rx"), ("network_tx_bytes", "net_tx")):
            if r.get(key) is not None:
                rec[field] = float(r[key])
        if {"nr_periods", "nr_throttled", "throttled_usec"} <= cpu.keys():
            rec["cpu_throttling"] = {"periods": float(cpu["nr_periods"]), "throttled_periods": float(cpu["nr_throttled"]),
                                     "throttled_us": float(cpu["throttled_usec"])}
        out.append(rec)
    if not out:
        warnings.append(f"no cgroup samples inside the run window in {os.path.basename(path)}")
    else:
        warnings.append("cgroup samples: memory.stat, pids.peak and io.stat are kept in the archive "
                        "(cgroup_samples.jsonl), not in TraceX resources")
    return out


def archive(d, requests, latency, io, tokens, diags, preds, prom_raw, origin):
    """Verbatim copy of everything the adapter read, next to the profile."""
    os.makedirs(d, exist_ok=True)
    for name, df in (("requests", requests), ("latency", latency), ("io", io), ("tokens", tokens)):
        df.to_csv(os.path.join(d, name + ".csv"), index=False)
    with open(os.path.join(d, "diagnostics.txt"), "w") as f:
        f.write("\n".join(diags) + "\n")
    with open(os.path.join(d, "preds.json"), "w") as f:
        json.dump(preds, f, indent=1, default=str)
    if prom_raw:
        with open(os.path.join(d, "prometheus.json"), "w") as f:
            json.dump({"origin_epoch_s": origin, "series": prom_raw}, f)


def prom_range(base, query, t0, t1, step):
    qs = urllib.parse.urlencode({"query": query, "start": t0, "end": t1, "step": step})
    with urllib.request.urlopen(f"{base.rstrip('/')}/api/v1/query_range?{qs}", timeout=30) as r:
        body = json.load(r)
    return body.get("data", {}).get("result", []) if body.get("status") == "success" else []


# TraceX resource field <- PromQL over cAdvisor container series.
PROM_FIELDS = {
    "cpu_percent": 'sum by (name) (rate(container_cpu_usage_seconds_total{name!=""}[15s])) * 100',
    "memory_bytes": 'container_memory_working_set_bytes{name!=""}',
    "memory_peak_bytes": 'container_memory_max_usage_bytes{name!=""}',
    "memory_limit_bytes": 'container_spec_memory_limit_bytes{name!=""} > 0',
    "network_rx_bytes": 'sum by (name) (container_network_receive_bytes_total{name!=""})',
    "network_tx_bytes": 'sum by (name) (container_network_transmit_bytes_total{name!=""})',
    "pids": 'container_processes{name!=""}',
    "throttle.periods": 'container_cpu_cfs_periods_total{name!=""}',
    "throttle.throttled_periods": 'container_cpu_cfs_throttled_periods_total{name!=""}',
    "throttle.throttled_seconds": 'container_cpu_cfs_throttled_seconds_total{name!=""}',
}


def scrape_prometheus(base, t0, t1, origin, warnings):
    """Per-container resources, plus every cAdvisor container series verbatim."""
    step = "5s"
    series, raw = {}, {}
    for field, q in PROM_FIELDS.items():
        try:
            res = prom_range(base, q, t0, t1, step)
        except OSError as e:
            warnings.append(f"prometheus query failed ({field}): {type(e).__name__}")
            return [], raw
        raw[field] = {"query": q, "result": res}
        for s in res:
            name = s["metric"].get("name", "")
            for ts, v in s["values"]:
                series.setdefault((name, float(ts)), {})[field] = num(v)
    try:  # every container_* series the deployment scraped, for the archive
        names = prom_range(base, 'count by (__name__) ({__name__=~"container_.+", name!=""})', t0, t1, "60s")
        for n in sorted({s["metric"]["__name__"] for s in names}):
            raw.setdefault("all", {})[n] = prom_range(base, f'{n}{{name!=""}}', t0, t1, step)
    except OSError as e:
        warnings.append(f"prometheus full-series archive failed: {type(e).__name__}")
    out = []
    for (name, ts), vals in sorted(series.items()):
        if ts < origin:
            continue
        rec = {"component": component_for(name), "runtime_id": name, "time_ms": (ts - origin) * 1000.0}
        thr = {k.split(".", 1)[1]: v for k, v in vals.items() if k.startswith("throttle.") and v is not None}
        rec.update({k: v for k, v in vals.items() if not k.startswith("throttle.") and v is not None})
        if {"periods", "throttled_periods", "throttled_seconds"} <= thr.keys() and thr["throttled_periods"] <= thr["periods"]:
            rec["cpu_throttling"] = {"periods": thr["periods"], "throttled_periods": thr["throttled_periods"],
                                     "throttled_us": thr["throttled_seconds"] * 1e6}
        out.append(rec)
    if not out:
        warnings.append("prometheus returned no cAdvisor container samples for the run window")
    return out, raw


def component_for(container):
    """Role of a compose service (or container name) in the deployment."""
    n = container.lower()
    if "loadgen" in n:
        return "loadgen"
    if any(k in n for k in ("cadvisor", "prometheus", "grafana")):
        return "monitoring"
    if "proxy" in n:  # LLM / search / API proxies in front of the agent
        return "proxy"
    if any(k in n for k in ("milvus", "shopping-admin", "grader", "models-server")):
        return "service"  # vector DB, target website, grader, local model server
    return "harness"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--data", help="raw benchmark/data/<setting>/ directory")
    src.add_argument("--tables", help="exported tables/ directory")
    ap.add_argument("--setting", required=True, help="ASB setting, e.g. browser-use-webagent")
    ap.add_argument("--asb-home", default=os.environ.get("AGSBENCH_HOME", ""), help="ASB repo root (for --data)")
    ap.add_argument("--agent", help="ASB agent key (default: derived from --setting)")
    ap.add_argument("--cgroup-samples", help="JSONL from sample_cgroups.py (preferred resource source)")
    ap.add_argument("--prometheus", help="Prometheus base URL of the running deployment (fallback)")
    ap.add_argument("--config", action="append", metavar="KEY=VALUE",
                    help="run condition recorded as configuration asb.KEY (repeatable)")
    ap.add_argument("--archive", help="directory for the verbatim tables, diagnostics, preds and Prometheus series")
    ap.add_argument("--model")
    ap.add_argument("--provider")
    ap.add_argument("--concurrency", type=int)
    ap.add_argument("--run-id")
    ap.add_argument("--name")
    ap.add_argument("-o", "--output", default="-")
    args = ap.parse_args()
    if args.data and not args.asb_home:
        ap.error("--data needs --asb-home (or AGSBENCH_HOME)")
    prof = build(args)
    text = json.dumps(prof, indent=1)
    if args.output == "-":
        print(text)
    else:
        with open(args.output, "w") as f:
            f.write(text)
        print(f"wrote {args.output}: {len(prof['tasks'])} tasks, {len(prof['spans'])} spans, "
              f"{len(prof['resources'])} resource samples", file=sys.stderr)


if __name__ == "__main__":
    main()
