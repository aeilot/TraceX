"""Convert the public TraceLab trace into TraceX normalized profiles (schema v2).

TraceLab (https://github.com/uw-syfi/TraceLab, CC BY 4.0) records real Claude
Code and Codex sessions: one row per LLM round with timing_events and tools[].
Message text and tool inputs/outputs are already removed upstream; only counts
and character lengths remain.

One TraceX event stream (JSONL, schema_version 1 events carrying schema v2
task/span/edge payloads) per user. Event streams are used rather than one
normalized JSON file because TraceX reads normalized JSON up to 32 MiB but event
streams up to 256 MiB. A user whose stream would exceed 224 MiB or 900,000
records is split by session into shards (configuration tracelab.shard).

  session           -> task "s<n>" (status "completed"; TraceLab records no
                       outcome); provider, session_id, user, project and store
                       as tracelab.* task attributes, repeated on a round only
                       where the round's value differs
  session envelope  -> span kind "agent"
  turn              -> span kind "agent": from a human message to the last agent
                       event before the next human message
  human wait        -> span kind "lifecycle", phase "human_wait": from the last
                       agent event before a human message to that message
  LLM round         -> span kind "model" "s<n>/r<round_index>"; every other
                       scalar row field as tracelab.<key> (trace_key only when
                       it is not provider:session_id:round_id);
                       usage from the token fields; per event type and source,
                       tracelab.events.* counts, first/last times and char sums
  tools[] entry     -> span kind "tool" "s<n>/r<i>/t<tool_index>" from
                       emitted_at to result_at; every field as tracelab.tool.<key>
  round -> its tools                      edge "call"
  tool  -> round that receives its result edge "dependency"

Human messages are user_message events. A user_message less than 1 s after the
previous one, with no agent event in between, joins the same human message
(skill expansions and command caveats arrive in bursts).

A round's start is not recorded: it is taken as its last input event
(user_message or tool_result) before its first output event, including inputs
the previous row recorded after its own output (Codex rows end at token_count,
so a fast tool's result can land in the row that issued it), or else the
previous round's end; its end is its last output event (tracelab.timing says
which). Such a same-row result links to the next round. Codex exec_command_end /
patch_apply_end notices are tool completions, not input, and never link. TraceX keeps scalars only:
lists become JSON strings, negative numbers become strings, and strings over
256 characters are cut with a tracelab.<key>_truncated flag. Conversion
counts (inferred timing, truncation, tools without results) are recorded as
configuration tracelab.warn.<name>. The source
.jsonl.gz is the verbatim archive; check_lossless.py compares the export
against it.

Usage (stdlib only):
  python3 -I tracelab2tracex.py syfi_coding_trace.jsonl.gz -o OUT_DIR [--user user_x]
"""

import argparse
import bisect
import gzip
import hashlib
import json
import math
import os
import shutil
import sys
import tempfile
from collections import Counter, defaultdict
from datetime import datetime

SOURCE = "TraceLab"
NS = "tracelab."
MAX_STR = 256
MAX_RECORDS = 900_000  # TraceX bounds a profile at 1,000,000; keep headroom
MAX_BYTES = 224 << 20  # TraceX reads event streams up to 256 MiB; the size estimate runs slightly low
QUEUED_S = 1.0
INPUT_TYPES = ("user_message", "tool_result")
# Codex tool completion notices: recorded with the round that issued the tool,
# after its output, so they are neither LLM input nor evidence of consumption.
NOT_LLM_INPUT = ("event_msg.exec_command_end", "event_msg.patch_apply_end")
OUTPUT_TYPES = ("text", "reasoning", "tool_call", "usage_report")


def ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


def scalar(attrs, key, v):
    """Store v under key as a TraceX scalar; returns False if it had to be cut."""
    if v is None:
        return True
    if isinstance(v, (list, dict)):
        v = json.dumps(v, ensure_ascii=False, separators=(",", ":"))
    if isinstance(v, bool):
        attrs[key] = v
    elif isinstance(v, (int, float)):
        if not math.isfinite(v):
            attrs[key] = str(v)
        elif v < 0:
            attrs[key] = str(v)
        else:
            attrs[key] = v
    else:
        v = str(v)
        if len(v) > MAX_STR:
            attrs[key] = v[:MAX_STR]
            attrs[key + "_truncated"] = True
            return False
        attrs[key] = v
    return True


ROW_SKIP = ("timing_events", "tools")
TASK_FIELDS = ("provider", "session_id", "user", "project", "store")


def split_by_user(path, tmp):
    """Stream the trace once into one uncompressed JSONL file per user."""
    files, sha = {}, hashlib.sha256()
    with open(path, "rb") as raw:
        for chunk in iter(lambda: raw.read(1 << 20), b""):
            sha.update(chunk)
    with gzip.open(path, "rt") as f:
        for line in f:
            user = json.loads(line)["user"]
            if user not in files:
                files[user] = open(os.path.join(tmp, f"{len(files)}.jsonl"), "w")
            files[user].write(line)
    for fh in files.values():
        fh.close()
    return {u: fh.name for u, fh in files.items()}, sha.hexdigest()


def human_messages(events):
    """Merge user_message bursts; returns [(time, n_events, chars)] and agent times."""
    humans, agent = [], []
    agent_since = True
    for t, e in events:
        if e["event_type"] == "user_message":
            chars = e.get("content_chars") or 0
            if humans and not agent_since and t - humans[-1][0] <= QUEUED_S:
                last = humans[-1]
                humans[-1] = (t, last[1] + 1, last[2] + chars, last[3])
            else:
                humans.append((t, 1, chars, t))
            agent_since = False
        else:
            agent.append(t)
            agent_since = True
    return humans, agent


def trace_key(r):
    return f"{r['provider']}:{r['session_id']}:{r['round_id']}"


def convert_session(sid, rows, warn):
    """Build spans/edges for one session with absolute-second times; returns (task, spans, edges)."""
    rows.sort(key=lambda r: r["round_index"])
    events = sorted(((ts(e["timestamp"]), e) for r in rows for e in r["timing_events"]), key=lambda x: x[0])
    humans, agent_times = human_messages(events)
    tool_times = []
    for r in rows:
        for tool in r["tools"]:
            for k in ("emitted_at", "result_at"):
                if tool.get(k):
                    tool_times.append(ts(tool[k]))
    agent_all = sorted(agent_times + tool_times)
    all_times = [t for t, _ in events] + tool_times
    s_start, s_end = min(all_times), max(all_times)
    provider = rows[0]["provider"]
    spans, edges = [], []

    def span(id_, name, kind, start, end, parent, attrs=None, status="completed", **extra):
        s = {"id": id_, "task_id": sid, "agent_id": provider, "name": name, "kind": kind, "status": status,
             "start_ms": start, "duration_ms": max(0.0, end - start), "source": SOURCE}
        if parent:
            s["parent_id"] = parent
        if attrs:
            s["attributes"] = attrs
        s.update(extra)
        spans.append(s)
        return s

    env = span(f"{sid}/session", "Session", "agent", s_start, s_end, None)["id"]
    h_times = [h[0] for h in humans]
    turn_ids = []
    for i, (t_h, n_ev, chars, first_t) in enumerate(humans):
        nxt = h_times[i + 1] if i + 1 < len(humans) else math.inf
        lo, hi = bisect.bisect_right(agent_all, t_h), bisect.bisect_left(agent_all, nxt)
        end = agent_all[hi - 1] if hi > lo else t_h
        turn_ids.append(span(f"{sid}/turn{i}", f"Turn {i + 1}", "agent", first_t, end, env,
                             {NS + "turn.index": i, NS + "turn.user_message_events": n_ev,
                              NS + "turn.user_message_chars": chars})["id"])
        before = bisect.bisect_left(agent_all, first_t)
        if i > 0 and before > 0 and agent_all[before - 1] < first_t:
            gap_start = agent_all[before - 1]
            span(f"{sid}/wait{i}", "Waiting for human", "lifecycle", gap_start, first_t, env,
                 {"phase": "human_wait", NS + "wait.mid_turn": first_t - gap_start <= QUEUED_S})

    def parent_at(t):
        i = bisect.bisect_right(h_times, t) - 1
        return turn_ids[i] if i >= 0 else env

    result_round = {}
    for r in rows:
        for t, e in ((ts(e["timestamp"]), e) for e in r["timing_events"]):
            if e["event_type"] == "tool_result" and e.get("tool_call_id") and e["source"] not in NOT_LLM_INPUT:
                result_round.setdefault(e["tool_call_id"], []).append(r["round_index"])
    round_span = {}
    next_round = {a["round_index"]: b["round_index"] for a, b in zip(rows, rows[1:])}
    prev_end = None
    carry = []  # inputs recorded in the previous row after its output (Codex rows end at token_count)
    n_tools = 0
    for r in rows:
        evs = [(ts(e["timestamp"]), e) for e in r["timing_events"]]
        outs = [t for t, e in evs if e["event_type"] in OUTPUT_TYPES]
        first_out = min(outs) if outs else math.inf
        llm_in = [t for t, e in evs if e["event_type"] in INPUT_TYPES and e["source"] not in NOT_LLM_INPUT]
        own = [t for t in llm_in if t <= first_out]
        carried = [t for t in carry if t <= first_out]
        ins = own + carried
        carry = [t for t in llm_in if t > first_out]
        attrs = {"model": r["model"]} if r.get("model") else {}
        for k, v in r.items():
            if k in ROW_SKIP or (k in TASK_FIELDS and v == rows[0].get(k)) or (k == "trace_key" and v == trace_key(r)):
                continue
            if k == "trace_key":
                warn["trace_key_not_derived"] += 1
            if not scalar(attrs, NS + k, v):
                warn["truncated:" + k] += 1
        by_type, by_source = defaultdict(list), Counter()
        for t, e in evs:
            by_type[e["event_type"]].append((t, e))
            by_source[e["source"]] += 1
        for typ, items in by_type.items():
            p = f"{NS}events.{typ}."
            attrs[p + "count"] = len(items)
            attrs[p + "first_s"] = min(t for t, _ in items)
            attrs[p + "last_s"] = max(t for t, _ in items)
            for k in ("content_chars", "result_chars"):
                vals = [e[k] for _, e in items if e.get(k) is not None]
                if vals:
                    attrs[p + k] = sum(vals)
            errs = [e["is_error"] for _, e in items if e.get("is_error") is not None]
            if errs:
                attrs[p + "is_error_count"] = sum(1 for x in errs if x)
                attrs[p + "is_error_known"] = len(errs)
        for src, c in by_source.items():
            attrs[f"{NS}events.source.{src}.count"] = c
        end = max(outs) if outs else None
        start = max(ins) if ins else prev_end
        if end is None and start is None:
            start = end = min(t for t, _ in evs) if evs else s_start
            attrs["time_quality"], attrs[NS + "timing"] = "unknown", "no_events"
        elif end is None:
            end = start
            attrs["time_quality"], attrs[NS + "timing"] = "completion_only", "no_output_event"
        elif start is None or start > first_out:
            attrs[NS + "timing"] = "output_only" if start is None else "previous_round_after_output"
            start = end
            attrs["time_quality"] = "completion_only"
        else:
            if not ins:
                attrs[NS + "timing"] = "previous_round_to_last_output"
            elif carried and max(carried) >= max(own, default=-math.inf):
                attrs[NS + "timing"] = "previous_row_input_to_last_output"
            else:
                attrs[NS + "timing"] = "last_input_to_last_output"
        if attrs.get("time_quality"):
            warn["round_timing:" + attrs[NS + "timing"]] += 1
        usage = {"input_tokens": r.get("input_tokens_total"), "output_tokens": r.get("output_tokens"),
                 "reasoning_tokens": r.get("reasoning_output_tokens")}
        if r["provider"] == "claude":
            usage["cache_read_tokens"] = r.get("claude_cache_read_input_tokens")
            usage["cache_write_tokens"] = r.get("claude_cache_creation_input_tokens")
        else:
            usage["cache_read_tokens"] = r.get("prefix_tokens")
        usage = {k: float(v) for k, v in usage.items() if isinstance(v, (int, float)) and v >= 0}
        rid = f"{sid}/r{r['round_index']}"
        span(rid, r.get("model") or "LLM round", "model", start, end, parent_at(end), attrs,
             **({"usage": usage} if usage else {}))
        round_span[r["round_index"]] = (rid, start)
        prev_end = end
        seen = set()
        for tool in r["tools"]:
            idx = tool.get("tool_index", len(seen))
            while idx in seen:
                idx = f"{idx}x"
            seen.add(idx)
            n_tools += 1
            tattrs = {"tool.name": str(tool.get("tool_name", ""))[:MAX_STR]}
            for k, v in tool.items():
                if not scalar(tattrs, f"{NS}tool.{k}", v):
                    warn["truncated:tool." + k] += 1
            t0 = ts(tool["emitted_at"]) if tool.get("emitted_at") else end
            extra = {}
            if tool.get("result_at"):
                t1 = ts(tool["result_at"])
            else:
                t1, extra["incomplete"] = t0, True
                warn["tool_without_result"] += 1
            status = "failed" if tool.get("is_error") else ("succeeded" if tool.get("is_error") is False else "completed")
            tid = f"{rid}/t{idx}"
            span(tid, str(tool.get("tool_name", "tool"))[:MAX_STR], "tool", t0, t1, parent_at(t0), tattrs,
                 status=status, **extra)
            edges.append({"source": rid, "target": tid, "kind": "call"})
            seen_in = result_round.get(tool.get("tool_call_id"), [])
            later = [i for i in seen_in if i > r["round_index"]]
            if later:
                edges.append({"source": tid, "target": ("round", min(later)), "kind": "dependency"})
            elif r["round_index"] in seen_in and r["round_index"] in next_round:
                # Result recorded after this row's output, before its token_count: the next request reads it.
                edges.append({"source": tid, "target": ("round", next_round[r["round_index"]]), "kind": "dependency"})
                warn["tool_result_read_by_next_round"] += 1
            else:
                warn["tool_result_not_recorded_as_input"] += 1
    for e in edges:
        if isinstance(e["target"], tuple):
            e["target"] = round_span[e["target"][1]][0]
    first = rows[0]
    tattrs = {}
    for k in TASK_FIELDS:
        scalar(tattrs, NS + k, first.get(k))
        if any(r.get(k) != first.get(k) for r in rows):
            warn["task_field_varies:" + k] += 1
    tattrs[NS + "rounds"] = len(rows)
    tattrs[NS + "tools"] = n_tools
    tattrs[NS + "human_messages"] = len(humans)
    tattrs[NS + "user_message_events"] = sum(h[1] for h in humans)
    task = {"id": sid, "name": f"{provider} session", "status": "completed", "start_ms": s_start,
            "duration_ms": s_end - s_start, "evaluation_status": "unknown", "cleanup_status": "unknown",
            "attributes": tattrs}
    return task, spans, edges


def records(spans, edges):
    return 1 + len(spans) + len(edges) + sum(1 for s in spans if s.get("parent_id"))


def to_ms(origin, task, spans):
    task["start_ms"] = (task["start_ms"] - origin) * 1000
    task["duration_ms"] *= 1000
    for s in spans:
        s["start_ms"] = (s["start_ms"] - origin) * 1000
        s["duration_ms"] *= 1000
        for k, v in list((s.get("attributes") or {}).items()):
            if k.endswith(("first_s", "last_s")) and k.startswith(NS + "events."):
                s["attributes"][k[:-2] + "_ms"] = (v - origin) * 1000
                del s["attributes"][k]


def event(seq, kind, payload):
    return json.dumps({"schema_version": 1, "sequence": seq, "type": kind, kind: payload},
                      ensure_ascii=False, separators=(",", ":")) + "\n"


def write_streams(user, path, out_dir, release, sha):
    by_session = defaultdict(list)
    with open(path) as f:
        for line in f:
            r = json.loads(line)
            by_session[r["session_id"]].append(r)
    first_seen = {sid: min(ts(e["timestamp"]) for r in rows for e in r["timing_events"]) if any(
        r["timing_events"] for r in rows) else math.inf for sid, rows in by_session.items()}
    warn = Counter()
    built = []
    for n, sid in enumerate(sorted(by_session, key=lambda x: (first_seen[x], x))):
        task, spans, edges = convert_session(f"s{n}", by_session[sid], warn)
        size = sum(len(json.dumps(x, ensure_ascii=False, separators=(",", ":"))) + 64 for x in [task] + spans + edges)
        built.append((task, spans, edges, records(spans, edges), size))
    shards, cur, n_rec, n_bytes = [], [], 0, 0
    for b in built:
        if cur and (n_rec + b[3] > MAX_RECORDS or n_bytes + b[4] > MAX_BYTES):
            shards.append(cur)
            cur, n_rec, n_bytes = [], 0, 0
        cur.append(b)
        n_rec += b[3]
        n_bytes += b[4]
    shards.append(cur)
    outs = []
    for i, shard in enumerate(shards):
        origin = min(b[0]["start_ms"] for b in shard)
        for task, spans, _, _, _ in shard:
            to_ms(origin, task, spans)
        suffix = f"-part{i + 1}" if len(shards) > 1 else ""
        run_id = f"tracelab-{release}-{user}{suffix}"
        providers = sorted({b[0]["attributes"][NS + "provider"] for b in shard})
        config = {"benchmark": "TraceLab", "harness": "+".join(providers), NS + "release": release,
                  NS + "user": user, NS + "source_sha256": sha, NS + "shard": i + 1, NS + "shards": len(shards),
                  NS + "sessions": len(shard), NS + "user_sessions": len(built),
                  NS + "origin_unix_ms": origin * 1000}
        for k, v in warn.items():
            config[f"{NS}warn.{k}"[:128]] = v
        duration = max(s["start_ms"] + s["duration_ms"] for b in shard for s in b[1])
        out = os.path.join(out_dir, run_id + ".events.jsonl")
        seq = 0
        with open(out, "w") as f:
            seq += 1
            f.write(event(seq, "metadata", {"run_id": run_id, "name": f"TraceLab {user}{suffix}",
                                            "duration_ms": duration, "configuration": config}))
            for task, spans, edges, _, _ in shard:
                for kind, items in (("task", [task]), ("span", spans), ("edge", edges)):
                    for x in items:
                        seq += 1
                        f.write(event(seq, kind, x))
        outs.append((out, len(shard), sum(len(b[1]) for b in shard), sum(len(b[2]) for b in shard),
                     os.path.getsize(out)))
    return outs


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("trace", help="syfi_coding_trace.jsonl.gz from a TraceLab release")
    ap.add_argument("-o", "--out-dir", required=True)
    ap.add_argument("--release", default="v0.0.2", help="TraceLab release tag recorded in configuration")
    ap.add_argument("--user", action="append", help="only convert these pseudonymous users (repeatable)")
    args = ap.parse_args()
    os.makedirs(args.out_dir, exist_ok=True)
    tmp = tempfile.mkdtemp(prefix="tracelab-")
    try:
        users, sha = split_by_user(args.trace, tmp)
        for user in sorted(users):
            if args.user and user not in args.user:
                continue
            for out, nt, ns, ne, nb in write_streams(user, users[user], args.out_dir, args.release, sha):
                print(f"{out}\t{nt} sessions\t{ns} spans\t{ne} edges\t{nb / 2**20:.0f} MiB", file=sys.stderr)
    finally:
        shutil.rmtree(tmp)


if __name__ == "__main__":
    main()
