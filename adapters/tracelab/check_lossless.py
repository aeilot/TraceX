"""Check that TraceX exports retain every field of the TraceLab trace.

Reads the profiles as TraceX itself exports them (after its allowlist
filtering) and the source .jsonl.gz, then compares, for every user present in
the exports (all shards of a user must be passed together):
  session  -> one task; provider/session_id/user/project/store attributes,
              round, tool and user_message counts
  row      -> span s<n>/r<round_index>: every scalar field (task fields may be
              omitted only when they equal the task's, trace_key only when it
              equals provider:session_id:round_id), model,
              usage, and the per-type/per-source event aggregates recomputed
              from timing_events (counts, first/last times, char sums, errors)
  tool     -> span s<n>/r<i>/t<tool_index>: every field, start = emitted_at,
              end = result_at
Times are compared in absolute milliseconds via configuration
tracelab.origin_unix_ms. Lists are compared as JSON strings, negative numbers
as strings, strings over 256 characters by prefix plus the _truncated flag.

  tracex --run STREAM.events.jsonl --export EXPORT.json
  python3 -I check_lossless.py syfi_coding_trace.jsonl.gz EXPORT.json [EXPORT2.json ...]
Exit status 1 on any mismatch.
"""

import gzip
import json
import math
import sys
from collections import Counter, defaultdict
from datetime import datetime

NS = "tracelab."
TASK_FIELDS = ("provider", "session_id", "user", "project", "store")
NOT_LLM_INPUT = ("event_msg.exec_command_end", "event_msg.patch_apply_end")
TOL_MS = 0.01


def ts_ms(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp() * 1000


def expect(v):
    """The value TraceX should hold for a source value; None means absent."""
    if v is None:
        return None
    if isinstance(v, (list, dict)):
        v = json.dumps(v, ensure_ascii=False, separators=(",", ":"))
    if isinstance(v, bool):
        return v
    if isinstance(v, (int, float)):
        return str(v) if (not math.isfinite(v) or v < 0) else float(v)
    return str(v)


def same(attrs, key, v):
    want, got = expect(v), attrs.get(key)
    if want is None:
        return got is None
    if isinstance(want, str) and len(want) > 256:
        return got == want[:256] and attrs.get(key + "_truncated") is True
    if isinstance(want, float):
        return isinstance(got, (int, float)) and not isinstance(got, bool) and math.isclose(got, want, rel_tol=1e-12, abs_tol=1e-9)
    return got == want


def near(a, b):
    return a is not None and b is not None and abs(a - b) <= TOL_MS


def main(trace, exports):
    errors, checked = [], Counter()
    tasks, spans, users = {}, {}, set()
    for path in exports:
        p = json.load(open(path))
        origin = p["configuration"][NS + "origin_unix_ms"]
        users.add(p["configuration"][NS + "user"])
        for t in p["tasks"]:
            tasks[t["attributes"][NS + "session_id"]] = (t, origin)
        for s in p["spans"]:
            spans[s["id"]] = (s, origin)
    rows = defaultdict(list)
    with gzip.open(trace, "rt") as f:
        for line in f:
            r = json.loads(line)
            if r["user"] in users:
                rows[r["session_id"]].append(r)
    if set(rows) != set(tasks):
        errors.append(f"sessions: {len(set(rows) - set(tasks))} missing, {len(set(tasks) - set(rows))} extra")

    def err(msg):
        if len(errors) < 50:
            errors.append(msg)
        checked["errors"] += 1

    for sid, rs in rows.items():
        if sid not in tasks:
            continue
        rs.sort(key=lambda r: r["round_index"])
        task, origin = tasks[sid]
        ta, tid = task["attributes"], task["id"]
        for k in TASK_FIELDS:
            if not same(ta, NS + k, rs[0].get(k)):
                err(f"{tid}: task {k}")
        n_tools = sum(len(r["tools"]) for r in rs)
        n_um = sum(1 for r in rs for e in r["timing_events"] if e["event_type"] == "user_message")
        if ta.get(NS + "rounds") != len(rs) or ta.get(NS + "tools") != n_tools or ta.get(NS + "user_message_events") != n_um:
            err(f"{tid}: task counts")
        turn_um = sum(s["attributes"].get(NS + "turn.user_message_events", 0)
                      for i, (s, _) in ((i, spans[f"{tid}/turn{i}"]) for i in range(ta.get(NS + "human_messages", 0)))
                      if f"{tid}/turn{i}" in spans)
        if turn_um != n_um:
            err(f"{tid}: turns hold {turn_um} of {n_um} user_message events")
        checked["sessions"] += 1
        for r in rs:
            key = f"{tid}/r{r['round_index']}"
            if key not in spans:
                err(f"{key}: round span missing")
                continue
            s, _ = spans[key]
            a = s.get("attributes") or {}
            for k, v in r.items():
                if k in ("timing_events", "tools"):
                    continue
                if k in TASK_FIELDS and NS + k not in a:
                    if not same(ta, NS + k, v):
                        err(f"{key}: {k} differs from the task but is not on the round")
                    checked["row_fields"] += 1
                    continue
                if k == "trace_key" and v == f"{r['provider']}:{r['session_id']}:{r['round_id']}" and NS + k not in a:
                    continue
                if not same(a, NS + k, v):
                    err(f"{key}: {k} {v!r} != {a.get(NS + k)!r}")
                checked["row_fields"] += 1
            if r.get("model") and a.get("model") != r["model"]:
                err(f"{key}: model")
            u = s.get("usage") or {}
            want = {"input_tokens": r.get("input_tokens_total"), "output_tokens": r.get("output_tokens"),
                    "reasoning_tokens": r.get("reasoning_output_tokens"),
                    "cache_read_tokens": r.get("claude_cache_read_input_tokens") if r["provider"] == "claude" else r.get("prefix_tokens")}
            if r["provider"] == "claude":
                want["cache_write_tokens"] = r.get("claude_cache_creation_input_tokens")
            for k, v in want.items():
                if (u.get(k) if v is not None else None) != (float(v) if v is not None else None):
                    err(f"{key}: usage {k}")
            by_type, by_src = defaultdict(list), Counter()
            for e in r["timing_events"]:
                by_type[e["event_type"]].append(e)
                by_src[e["source"]] += 1
                checked["events"] += 1
            for typ, items in by_type.items():
                p = f"{NS}events.{typ}."
                times = [ts_ms(e["timestamp"]) - origin for e in items]
                if a.get(p + "count") != len(items) or not near(a.get(p + "first_ms"), min(times)) or not near(a.get(p + "last_ms"), max(times)):
                    err(f"{key}: events.{typ} count/times")
                for c in ("content_chars", "result_chars"):
                    vals = [e[c] for e in items if e.get(c) is not None]
                    if (a.get(p + c) if vals else None) != (sum(vals) if vals else None):
                        err(f"{key}: events.{typ}.{c}")
                errs = [e["is_error"] for e in items if e.get("is_error") is not None]
                if errs and (a.get(p + "is_error_count") != sum(1 for x in errs if x) or a.get(p + "is_error_known") != len(errs)):
                    err(f"{key}: events.{typ}.is_error")
            for src, c in by_src.items():
                if a.get(f"{NS}events.source.{src}.count") != c:
                    err(f"{key}: events.source.{src}")
            seen = set()
            for tool in r["tools"]:
                idx = tool.get("tool_index", len(seen))
                while idx in seen:
                    idx = f"{idx}x"
                seen.add(idx)
                tkey = f"{key}/t{idx}"
                if tkey not in spans:
                    err(f"{tkey}: tool span missing")
                    continue
                ts_, _ = spans[tkey]
                ta2 = ts_.get("attributes") or {}
                for k, v in tool.items():
                    if not same(ta2, f"{NS}tool.{k}", v):
                        err(f"{tkey}: tool.{k}")
                    checked["tool_fields"] += 1
                if tool.get("emitted_at") and not near(ts_["start_ms"], ts_ms(tool["emitted_at"]) - origin):
                    err(f"{tkey}: start")
                if tool.get("result_at") and not near(ts_["start_ms"] + ts_["duration_ms"], ts_ms(tool["result_at"]) - origin):
                    err(f"{tkey}: end")
                checked["tools"] += 1
            checked["rounds"] += 1
    print(json.dumps({"users": sorted(users), **checked}, indent=1))
    for e in errors:
        print("MISMATCH", e, file=sys.stderr)
    return 1 if errors or checked["errors"] else 0


if __name__ == "__main__":
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    sys.exit(main(sys.argv[1], sys.argv[2:]))
