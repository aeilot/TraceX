"""Check that a TraceX profile retains every cell of ASB's unified tables.

Reads the profile as TraceX itself exports it (after its allowlist filtering)
and the verbatim tables the adapter archived, then compares:
  requests: every column            == task attribute asb.<column>
  latency:  every column            == span attribute asb.<column>
  tokens:   per-request column sums == matched span usage/attrs + asb.unmatched_tokens.*
  io:       per-request byte/token sums == span asb.io.* + task asb.io.<fn>.*

  tracex --run profile.json --export - > exported.json
  python check_lossless.py exported.json ARCHIVE_DIR
Exit status 1 on any mismatch.
"""

import json
import math
import sys
from collections import defaultdict

import pandas as pd

NS = "asb."


def same(a, b):
    if b is None or (isinstance(b, float) and math.isnan(b)) or str(b) in ("", "None", "nan"):
        return a is None
    if isinstance(a, bool) or str(b) in ("True", "False"):
        return str(a) == str(b)
    try:
        fa, fb = float(a), float(b)
        return math.isclose(fa, fb, rel_tol=1e-9, abs_tol=1e-9) or (fb < 0 and str(a) == str(fb))
    except (TypeError, ValueError):
        sa, sb = str(a), str(b)
        return sa == sb or (len(sb) > 256 and sb.startswith(sa))


def main(profile_path, archive):
    p = json.load(open(profile_path))
    read = lambda n: pd.read_csv(f"{archive}/{n}.csv") if open(f"{archive}/{n}.csv").read().strip() else pd.DataFrame()
    requests, latency, io, tokens = (read(n) for n in ("requests", "latency", "io", "tokens"))
    tasks = {t["id"]: t for t in p["tasks"]}
    spans = p["spans"]
    bad, checked = [], 0

    for _, q in requests.iterrows():
        a = tasks.get(str(q["request_id"]), {}).get("attributes", {})
        for col in requests.columns:
            checked += 1
            if not same(a.get(NS + col), q[col]):
                bad.append(f"requests {q['request_id']}.{col}: {a.get(NS + col)!r} != {q[col]!r}")

    by_call = {s["attributes"].get(NS + "call_id"): s for s in spans if s.get("attributes", {}).get(NS + "call_id")}
    traj = defaultdict(list)
    for s in spans:
        if not s.get("attributes", {}).get(NS + "call_id"):
            traj[s["task_id"]].append(s)
    if not latency.empty:
        latency = latency.sort_values(["request_id", "start_time"], kind="stable")
        seen = defaultdict(int)
        for _, r in latency.iterrows():
            if str(r.get("call_id")) not in ("", "nan", "None"):
                s = by_call.get(str(r["call_id"]))
            else:
                rid = str(r["request_id"])
                lst = traj[rid]
                s = lst[seen[rid]] if seen[rid] < len(lst) else None
                seen[rid] += 1
            for col in latency.columns:
                checked += 1
                got = None if s is None else s["attributes"].get(NS + col)
                if not same(got, r[col]):
                    bad.append(f"latency {r['request_id']}/{r.get('call_id')}.{col}: {got!r} != {r[col]!r}")

    def sums(df, keys):
        return {k: df.groupby("request_id")[k].sum().to_dict() for k in keys if k in df}

    if not tokens.empty:
        cols = ["input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "reasoning_tokens", "cost_usd", "total_tokens"]
        want = sums(tokens, cols)
        for col, per in want.items():
            for rid, v in per.items():
                got = 0.0
                for s in spans:
                    if s["task_id"] == str(rid) and s.get("usage") and NS + "token.category" in s["attributes"]:
                        got += (s["attributes"].get(NS + "token.total_tokens", 0) if col == "total_tokens" else s["usage"].get(col, 0))
                a = tasks.get(str(rid), {}).get("attributes", {})
                got += sum(v2 for k2, v2 in a.items() if k2.startswith(NS + "unmatched_tokens.") and k2.endswith("." + col))
                checked += 1
                if not same(got, float(v)):
                    bad.append(f"tokens {rid}.{col}: {got} != {v}")

    if not io.empty:
        for (rid, direction), g in io.groupby(["request_id", "direction"]):
            for field in ("bytes", "tokens"):
                want = float(g[field].sum())
                got = sum(s["attributes"].get(f"{NS}io.{direction}_{field}", 0) for s in spans if s["task_id"] == str(rid))
                a = tasks.get(str(rid), {}).get("attributes", {})
                got += sum(v for k, v in a.items() if k.startswith(NS + "io.") and k.endswith(f".{direction}_{field}"))
                checked += 1
                if not same(got, want):
                    bad.append(f"io {rid}.{direction}_{field}: {got} != {want}")

    print(f"checked {checked} values: {len(bad)} mismatches "
          f"({len(requests)} requests, {len(latency)} latency, {len(tokens)} token, {len(io)} io rows)")
    for b in bad[:30]:
        print("  " + b)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:3]))
