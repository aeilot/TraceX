#!/bin/sh
# AgentSysBench job driver, started and owned by the TraceX experiment runner.
#
#   driver.sh prepare ASB_ROOT SETTING ARTIFACTS AGENT_DIR
#   driver.sh run     ASB_ROOT SETTING ARTIFACTS AGENT_DIR REQ_VAR N CONC_VAR C JOB_DIR RUN_ID
#
# prepare: build the loadgen and agent images, then 1.prepare_data.sh. No model calls.
# run:     sample cgroups, 2.run.sh, convert to ARTIFACTS/RUN_ID/profile.json every
#          10 s while the loadgen runs, then 3.cleanup.sh, 4.evaluate.sh (if any) and a
#          final lossless conversion with a verbatim archive.
# SIGINT/SIGTERM stops the deployment with 3.cleanup.sh before exiting.
# Every argument is data; nothing here is evaluated as shell code.
set -u
umask 077
action=$1 root=$2 setting=$3 artifacts=$4 agent=$5
deploy="$root/benchmark/deployment/$setting"
image=agsbench-request:v1
poll=${TRACEX_ASB_POLL:-10}  # seconds between live conversions
log() { printf '%s tracex-asb: %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() { log "error: $*"; exit 1; }

[ -d "$deploy/scripts" ] || die "deployment $setting not found under $root"
[ -f "$root/.env" ] || die "run scripts/setup.sh and scripts/configure-api.sh in $root first"

if [ "$action" = prepare ]; then
  log "building $image"
  (cd "$root/request" && bash scripts/build_image.sh) || die "loadgen image build failed"
  for a in $agent; do
    log "building agent image: $a"
    (cd "$root/agents/$a" && bash scripts/build_image.sh) || die "agent image build failed: $a"
  done
  log "preparing data"
  (cd "$deploy" && bash scripts/1.prepare_data.sh) || die "1.prepare_data.sh failed"
  log "prepared"
  exit 0
fi

[ "$action" = run ] || die "unknown action $action"
req_var=$6 n=$7 conc_var=$8 c=$9
shift 9
job=$1 run_id=$2
run="$artifacts/$run_id"
data="$root/benchmark/data/$setting"
project=$(sed -n 's/^name:[[:space:]]*//p' "$deploy/docker-compose.yaml" | head -1 | tr -d '"'"'")
[ -n "$project" ] || die "compose project name not found"
docker image inspect "$image" >/dev/null 2>&1 || die "$image is missing; prepare the workload first"
mkdir -p "$run" || die "cannot create $run"
uid=$(id -u) gid=$(id -g)

sampler= runner= stopping=0
cleanup() {
  [ -n "$sampler" ] && kill "$sampler" 2>/dev/null
  log "stopping deployment (3.cleanup.sh)"
  if (cd "$deploy" && bash scripts/3.cleanup.sh) >"$run/cleanup.log" 2>&1; then
    echo succeeded >"$job/cleanup.status"
  else
    echo failed >"$job/cleanup.status"
  fi
  [ -n "$runner" ] && wait "$runner" 2>/dev/null
}
on_signal() { stopping=1; log "cancellation requested"; cleanup; exit 130; }
trap on_signal INT TERM
pause() { sleep "$1" & wait $!; }

# Convert inside the loadgen image (it ships pandas); runs as root to read the
# deployment's root-owned outputs, then hands the result to the job owner.
convert() {
  docker run --rm --entrypoint sh -v "$root:$root:ro" -v "$job:$job" "$image" -c '
    set -e; out=$1; owner=$2; shift 2
    python -I "$@" -o "$out.tmp" 2>>"$(dirname "$out")/convert.log"
    mv "$out.tmp" "$out"; chown -R "$owner" "$(dirname "$out")"' tracex-convert \
    "$run/profile.json" "$uid:$gid" "$job/asb2tracex.py" \
    --asb-home "$root" --setting "$setting" --data "$data" \
    --cgroup-samples "$run/cgroup_samples.jsonl" --concurrency "$c" \
    --config "request_num=$n" --config runner=tracex --config "compose_project=$project" \
    --run-id "$run_id" --name "ASB $setting n=$n c=$c" "$@" >/dev/null 2>&1
}

log "sampling cgroups of compose project $project"
python3 "$job/sample_cgroups.py" --project "$project" --out "$run/cgroup_samples.jsonl" >"$run/sampler.log" 2>&1 &
sampler=$!

log "starting $setting with $req_var=$n${conc_var:+ $conc_var=$c}"
export "$req_var=$n"
[ -n "$conc_var" ] && export "$conc_var=$c"
(cd "$deploy" && exec bash scripts/2.run.sh) >"$run/compose.log" 2>&1 &
runner=$!

loadgen= waited=0 runner_ok=1
while [ -z "$loadgen" ]; do
  loadgen=$(docker ps -a --filter "label=com.docker.compose.project=$project" \
    --format '{{.ID}} {{.Label "com.docker.compose.service"}}' | awk '$2 ~ /loadgen/ {print $1; exit}')
  [ -n "$loadgen" ] && break
  # 2.run.sh may return at once (up -d) or keep running (foreground up).
  if [ "$runner_ok" = 1 ] && ! kill -0 "$runner" 2>/dev/null; then
    wait "$runner" || { log "2.run.sh failed before the loadgen started"; cleanup; exit 1; }
    runner_ok=0 runner=
  fi
  waited=$((waited + 2)); [ "$waited" -gt 3600 ] && { log "loadgen did not start within an hour"; cleanup; exit 1; }
  pause 2
done
log "loadgen container $loadgen; converting every $poll s"
while [ "$(docker inspect -f '{{.State.Running}}' "$loadgen" 2>/dev/null)" != false ]; do
  [ "$(docker inspect -f '{{.State.Status}}' "$loadgen" 2>/dev/null)" = created ] || convert
  pause "$poll"
done
code=$(docker inspect -f '{{.State.ExitCode}}' "$loadgen" 2>/dev/null || echo 1)
docker logs "$loadgen" >"$run/loadgen.log" 2>&1
log "loadgen exited with $code"
pause "${TRACEX_ASB_TAIL:-3}"  # a few idle samples after the last request
kill "$sampler" 2>/dev/null; sampler=
trap '' INT TERM  # past this point the deployment is being torn down anyway
cleanup
if [ -f "$deploy/scripts/4.evaluate.sh" ]; then
  log "evaluating (4.evaluate.sh)"
  if (cd "$deploy" && PREDICTIONS_PATH="$data/trajectories/preds.json" bash scripts/4.evaluate.sh) >"$run/evaluate.log" 2>&1; then
    echo succeeded >"$job/evaluate.status"
  else
    echo failed >"$job/evaluate.status"
  fi
fi
log "final conversion with archive"
convert --archive "$run/asb"
docker run --rm --entrypoint sh -v "$data:$data:ro" -v "$job:$job" "$image" -c \
  'cp -R "$1" "$2" && chown -R "$3" "$2"' tracex-copy "$data" "$run/data" "$uid:$gid" >/dev/null 2>&1
log "done"
exit "$code"
