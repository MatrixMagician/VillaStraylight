#!/usr/bin/env bash
# PROTOTYPE (issue #372) — Qwen Code headless against the LIVE villa-llama over the `villa`
# network, in a throwaway node:22 container with an isolated HOME and npm prefix. Per
# (mode, task): transcript, wall time, token deltas, workspace file-list diff, and a
# tcpdump of everything the container sends off the villa subnet (rootless netns).
#   run.sh <optout|default> [T1 T2 ...]      (default tasks: T1..T5; T6 needs the office image)
# Env: PREFIX (npm prefix with qwen), QHOME (isolated HOME), MAXTURNS, TIMEOUT.
set -uo pipefail
cd "$(dirname "$0")"
MODE=${1:?optout|default}; shift
IP=10.89.2.240; SUBNET=10.89.2.0/24; NAME=proto-qwen
PREFIX=${PREFIX:?}; QHOME=${QHOME:?}
MODEL=/models/Qwen3.6-35B-A3B-Abliterated-Heretic-Q6_K.gguf
mkdir -p results "$QHOME/.qwen"
common='"modelProviders":{"openai":[{"id":"'$MODEL'","name":"villa","envKey":"LLAMA_API_KEY","baseUrl":"http://villa-llama:8080/v1"}]},"security":{"auth":{"selectedType":"openai"}},"model":{"name":"'$MODEL'"}'
if [ "$MODE" = optout ]; then
  echo "{$common,\"privacy\":{\"usageStatisticsEnabled\":false},\"general\":{\"enableAutoUpdate\":false},\"ui\":{\"enableUserFeedback\":false},\"telemetry\":{\"enabled\":false}}" > "$QHOME/.qwen/settings.json"
  OPTENV=(-e QWEN_USAGE_STATISTICS_ENABLED=false)
else
  echo "{$common}" > "$QHOME/.qwen/settings.json"; OPTENV=()
fi
set -a; . ~/.config/villa/inference/llama.env; set +a
metric() { curl -s -H "Authorization: Bearer $LLAMA_API_KEY" http://127.0.0.1:8080/metrics | awk -v k="$1" '$1==k{print $2}'; }
tasks=("$@"); [ ${#tasks[@]} -eq 0 ] && tasks=(T1 T2 T3 T4 T5)
PCAP=results/$MODE.pcap
podman unshare --rootless-netns sh -c "exec tcpdump -i any -n -U -w $PWD/$PCAP \"src host $IP and (not dst net $SUBNET or dst port 53)\"" 2>results/$MODE.tcpdump.err & TD=$!
sleep 2
for t in "${tasks[@]}"; do
  f=$(ls ../cowork/tasks/$t-*.txt); log=results/$MODE-$t.log
  (cd workspace && find . -type f | sort) > results/$MODE-$t.before
  p0=$(metric llamacpp:prompt_tokens_total); g0=$(metric llamacpp:tokens_predicted_total)
  {
  echo "== qwen[$MODE] $t $(date -Is)"; start=$(date +%s)
  export PROMPT="$(cat "$f")"
  podman rm -f -i $NAME >/dev/null 2>&1
  podman run --rm -i --name $NAME --network villa --ip $IP -v "$PREFIX:/opt/qwen:ro,z" -v "$QHOME:/home/node:Z" \
    -v "$PWD/workspace:/workspace:Z" -w /workspace -e HOME=/home/node --env-file ~/.config/villa/inference/llama.env \
    "${OPTENV[@]}" -e PROMPT docker.io/library/node:22-slim \
    sh -c 'exec timeout ${TIMEOUT:-1200} /opt/qwen/bin/qwen -p "$PROMPT" --approval-mode yolo' </dev/null 2>&1
  echo "-- exit=$? wall=$(( $(date +%s) - start ))s prompt_tokens=$(( $(metric llamacpp:prompt_tokens_total) - p0 )) generated=$(( $(metric llamacpp:tokens_predicted_total) - g0 ))"
  (cd workspace && find . -type f | sort) > results/$MODE-$t.after
  diff results/$MODE-$t.before results/$MODE-$t.after
  } 2>&1 | tee "$log"
done
sleep 2; pkill -x tcpdump; wait $TD 2>/dev/null
echo "== capture ($MODE): $(tcpdump -nr $PCAP 2>/dev/null | wc -l) packets off-subnet from $IP"
tcpdump -nr $PCAP 2>/dev/null | head -40
