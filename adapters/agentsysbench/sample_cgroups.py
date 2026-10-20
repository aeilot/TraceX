"""Per-container cgroup v2 sampler for a Docker Compose project (runs in the VM).

Every --interval seconds, for each running container of the compose project,
writes one JSON line with raw counters read directly from cgroup v2 and the
container's network namespace:

  t (epoch s), name, service, id,
  cpu.stat (all fields), memory.current, memory.peak, memory.max,
  memory.stat (anon, file, shmem, file_mapped, kernel), pids.current, pids.peak,
  io.stat rbytes/wbytes (summed over devices), net rx/tx bytes (non-lo)

Used instead of the deployment's cAdvisor, which on Docker 29 sees only the root
cgroup. Stops on SIGTERM/SIGINT.

  python3 sample_cgroups.py --project webagent-benchmarking --out samples.jsonl
"""

import argparse
import json
import signal
import subprocess
import time

MEM_STAT_KEYS = ("anon", "file", "shmem", "file_mapped", "kernel")


def read(path):
    try:
        with open(path) as f:
            return f.read()
    except OSError:
        return None


def kv(text):
    out = {}
    for line in (text or "").splitlines():
        parts = line.split()
        if len(parts) == 2:
            try:
                out[parts[0]] = int(parts[1])
            except ValueError:
                pass
    return out


def scalar(text):
    if text is None:
        return None
    t = text.strip()
    return None if t == "max" else int(t) if t.isdigit() else None


def containers(project):
    """{id: (name, service, pid, cgroup_dir)} for the project's running containers."""
    ids = subprocess.run(["docker", "ps", "-q", "--no-trunc", "--filter", f"label=com.docker.compose.project={project}"],
                         capture_output=True, text=True).stdout.split()
    out = {}
    if not ids:
        return out
    fmt = '{{.Id}} {{.Name}} {{.State.Pid}} {{index .Config.Labels "com.docker.compose.service"}}'
    for line in subprocess.run(["docker", "inspect", "-f", fmt, *ids], capture_output=True, text=True).stdout.splitlines():
        cid, name, pid, service = (line.split() + [""] * 4)[:4]
        cg = read(f"/proc/{pid}/cgroup")
        if not cg or pid == "0":
            continue
        rel = cg.strip().split("::", 1)[-1]
        out[cid] = (name.lstrip("/"), service, pid, "/sys/fs/cgroup" + rel)
    return out


def sample(name, service, cid, pid, cg, t):
    rec = {"t": t, "name": name, "service": service, "id": cid[:12]}
    rec["cpu"] = kv(read(f"{cg}/cpu.stat"))
    rec["memory_current"] = scalar(read(f"{cg}/memory.current"))
    rec["memory_peak"] = scalar(read(f"{cg}/memory.peak"))
    rec["memory_max"] = scalar(read(f"{cg}/memory.max"))
    ms = kv(read(f"{cg}/memory.stat"))
    rec["memory_stat"] = {k: ms[k] for k in MEM_STAT_KEYS if k in ms}
    rec["pids_current"] = scalar(read(f"{cg}/pids.current"))
    rec["pids_peak"] = scalar(read(f"{cg}/pids.peak"))
    rb = wb = 0
    for line in (read(f"{cg}/io.stat") or "").splitlines():
        for tok in line.split()[1:]:
            k, _, v = tok.partition("=")
            if k == "rbytes":
                rb += int(v)
            elif k == "wbytes":
                wb += int(v)
    rec["io_rbytes"], rec["io_wbytes"] = rb, wb
    rx = tx = 0
    for line in (read(f"/proc/{pid}/net/dev") or "").splitlines()[2:]:
        iface, _, rest = line.partition(":")
        f = rest.split()
        if iface.strip() != "lo" and len(f) >= 9:
            rx += int(f[0])
            tx += int(f[8])
    rec["net_rx"], rec["net_tx"] = rx, tx
    return rec


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--project", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--interval", type=float, default=1.0)
    ap.add_argument("--rescan", type=float, default=3.0, help="seconds between container list refreshes")
    args = ap.parse_args()
    stop = []
    signal.signal(signal.SIGTERM, lambda *_: stop.append(1))
    signal.signal(signal.SIGINT, lambda *_: stop.append(1))
    known, last_scan = {}, 0.0
    with open(args.out, "a", buffering=1) as f:
        while not stop:
            now = time.time()
            if now - last_scan >= args.rescan:
                known, last_scan = containers(args.project), now
            for cid, (name, service, pid, cg) in list(known.items()):
                if read(f"{cg}/cpu.stat") is None:  # container exited
                    known.pop(cid)
                    continue
                f.write(json.dumps(sample(name, service, cid, pid, cg, now)) + "\n")
            time.sleep(max(0.0, args.interval - (time.time() - now)))


if __name__ == "__main__":
    main()
