#!/usr/bin/env bash
# PROTOTYPE (#373): every interactive mode in turn, one fresh HOME each.
cd "$(dirname "$0")"
set -a; . ~/.config/villa/inference/llama.env; set +a
for m in default env-only setting-only all-optout env-beats-settings optout+refresh-env optout+refresh-setting lockdown; do
  ./seed.sh >/dev/null
  python3 -I interactive.py "$m" 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tail -1
done
podman logs proto-sink > results/interactive/sink.log 2>&1
echo ALL-MODES-DONE
