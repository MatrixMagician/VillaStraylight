#!/usr/bin/env bash
# PROTOTYPE — read-only second llama-server on port 8081 with --jinja (tool calls).
# The live villa-llama unit on 8080 is untouched; config.toml and units are never read
# or written. Same image, same model, same flags as the unit, minus the vision projector
# and with a smaller ctx. Usage: serve.sh start|stop|status
set -euo pipefail
IMG="docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89"
MODEL=${MODEL:-Qwen3.6-35B-A3B-UD-Q4_K_M.gguf}
MODELS=$HOME/.local/share/villa/models
NAME=villa-proto-cowork
PORT=8081
CTX=${CTX:-131072}
case ${1:-start} in
stop)   podman rm -f -i $NAME >/dev/null 2>&1 || true; echo stopped ;;
status) curl -sf http://127.0.0.1:$PORT/health && echo && podman ps --filter name=$NAME ;;
start)
  podman rm -f -i $NAME >/dev/null 2>&1 || true
  podman run -d --name $NAME --rm --network villa --device /dev/kfd --device /dev/dri --group-add keep-groups \
    -v "$MODELS:/models:ro,z" --publish 127.0.0.1:$PORT:$PORT \
    --env HSA_OVERRIDE_GFX_VERSION=11.5.1 --env ROCBLAS_USE_HIPBLASLT=1 --security-opt seccomp=unconfined \
    "$IMG" llama-server -m "/models/$MODEL" -c "$CTX" --host 0.0.0.0 --port $PORT \
    -ngl 999 -fa 1 --no-mmap -lv 4 --metrics --spec-type ngram-mod --jinja "${@:2}" >/dev/null
  for i in $(seq 1 180); do
    curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1 && { echo "ready on :$PORT"; exit 0; }
    podman container exists $NAME || { echo "server died"; podman logs $NAME 2>&1 | tail -20; exit 1; }
    sleep 2
  done
  echo "never healthy"; exit 1 ;;
esac
