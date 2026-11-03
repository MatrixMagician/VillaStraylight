#!/usr/bin/env bash
# PROTOTYPE (#385): fail-closed proxy env on top of the launch contract: HTTPS_PROXY at a dead local port,
# NO_PROXY the model server. Full sessions; the request count proves the model traffic still flows.
cd "$(dirname "$0")"
n() { podman logs proto-llama 2>&1 | grep -c "slot      release"; }
run() { ./seed.sh >/dev/null; b=$(n); python3 -I agent_capture.py "$@" 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tail -1; echo "   model requests: $(( $(n) - b ))"; }
P="HTTPS_PROXY=http://127.0.0.1:1,HTTP_PROXY=http://127.0.0.1:1,NO_PROXY=proto-llama"
run "crush-contract+$P"
run "claude-contract+$P"
echo PROXY-DONE
