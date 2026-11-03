#!/usr/bin/env bash
# PROTOTYPE (#373): `qwen serve` start + idle 30 s under the lockdown env, capture off-model traffic.
cd "$(dirname "$0")"; IP=10.99.7.240; LLAMA=10.99.7.10
set -a; . ~/.config/villa/inference/llama.env; set +a
mkdir -p results/interactive; rm -rf "$QHOME"/* "$QHOME"/.qwen; mkdir -p "$QHOME/.qwen"
echo '{"privacy":{"usageStatisticsEnabled":false},"model":{"catalogRefresh":false},"general":{"enableAutoUpdate":false},"ui":{"enableUserFeedback":false},"telemetry":{"enabled":false}}' > "$QHOME/.qwen/settings.json"
podman unshare --rootless-netns tcpdump -i any -n -U -w "$PWD/results/interactive/serve.pcap" "src host $IP and not dst host $LLAMA" 2>/dev/null & TD=$!
sleep 2
podman rm -f -i proto-qwen-serve >/dev/null 2>&1
timeout 40 podman run --rm --name proto-qwen-serve --network proto-qwen --ip $IP -v "$PREFIX:/opt/qwen:ro,z" -v "$QHOME:/home/node:Z" -v "$PWD/workspace:/workspace:Z" -w /workspace \
  -e HOME=/home/node -e QWEN_USAGE_STATISTICS_ENABLED=false -e QWEN_CODE_MODELS_DEV_REFRESH=off docker.io/library/node:22-slim /opt/qwen/bin/qwen serve > results/interactive/serve.out 2>&1
podman rm -f -i proto-qwen-serve >/dev/null 2>&1; sleep 2; pkill -x tcpdump; wait $TD 2>/dev/null
echo "serve: $(tcpdump -nr results/interactive/serve.pcap 2>/dev/null | awk '!/ In  /' | grep -vc ARP) packets off-model"
