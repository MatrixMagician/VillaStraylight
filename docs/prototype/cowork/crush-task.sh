#!/usr/bin/env bash
# PROTOTYPE — the chosen harness (Crush v0.76.0, villa's pinned binary) on the document
# tasks, inside the libkrun microVM, on an INTERNAL podman network that also carries the
# :8081 probe server, one rw folder grant, no GPU, the office sandbox image. Per task:
# transcript, wall time, prompt/generated token deltas from the server's /metrics, and the
# workspace file-list diff. `crush run` auto-approves every tool call (the approval gate is
# the bridge's job, not this run's); nothing here touches the live stack.
#   crush-task.sh [T1 T2 ...]   (default: all)
set -uo pipefail
cd "$(dirname "$0")"
IMG=${IMG:-localhost/villa-proto-sandbox:office}
CRUSH=$HOME/.local/share/villa/bin/crush
NET=proto-internal; SRV=villa-proto-cowork
podman network exists $NET || podman network create --internal $NET >/dev/null
podman network connect $NET $SRV 2>/dev/null || true
mkdir -p results crushdata
metric() { curl -s http://127.0.0.1:8081/metrics | awk -v k="$1" '$1==k{print $2}'; }
run() { podman run --rm -i --runtime=krun --network $NET --memory 8g --cpus 4 \
  -v "$PWD/workspace:/workspace:Z" -v "$CRUSH:/usr/local/bin/crush:ro,z" -v "$PWD/crushcfg:/crushcfg:ro,z" \
  -v "$PWD/crushdata:/crushdata:Z" -w /workspace \
  -e HOME=/tmp -e CRUSH_GLOBAL_CONFIG=/crushcfg -e CRUSH_GLOBAL_DATA=/crushdata \
  -e CRUSH_DISABLE_METRICS=1 -e DO_NOT_TRACK=1 -e CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1 -e PROMPT "$IMG" "$@"; }
tasks=("$@"); [ ${#tasks[@]} -eq 0 ] && tasks=(T1 T2 T3 T4 T5)
for t in "${tasks[@]}"; do
  f=$(ls tasks/$t-*.txt); log=results/crush-$t.log
  (cd workspace && find . -type f | sort) > results/crush-$t.before
  p0=$(metric llamacpp:prompt_tokens_total); g0=$(metric llamacpp:tokens_predicted_total)
  {
  echo "== crush $t $(date -Is)"
  start=$(date +%s)
  export PROMPT="$(cat "$f")"
  run sh -c 'exec timeout 1200 crush run -q --cwd /workspace "$PROMPT"' </dev/null 2>&1
  echo "-- exit=$? wall=$(( $(date +%s) - start ))s prompt_tokens=$(( $(metric llamacpp:prompt_tokens_total) - p0 )) generated=$(( $(metric llamacpp:tokens_predicted_total) - g0 ))"
  (cd workspace && find . -type f | sort) > results/crush-$t.after
  diff results/crush-$t.before results/crush-$t.after
  } 2>&1 | tee "$log"
done
