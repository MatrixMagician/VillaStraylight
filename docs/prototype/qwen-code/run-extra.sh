#!/usr/bin/env bash
cd "$(dirname "$0")"; set -a; . ~/.config/villa/inference/llama.env; set +a
for m in project-reenable project-reenable+env; do ./seed.sh >/dev/null; python3 -I interactive.py "$m" 2>&1 | tail -1; done
podman logs proto-sink > results/interactive/sink-all.log 2>&1; echo EXTRA-DONE
