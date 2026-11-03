#!/usr/bin/env bash
# PROTOTYPE (#385): Claude Code start-up bisect. Short sessions; the model-request count per run proves the
# session reached the model server.
cd "$(dirname "$0")"
n() { podman logs proto-llama 2>&1 | grep -c "slot      release"; }
run() { ./seed.sh >/dev/null; b=$(n); python3 -I agent_capture.py "$@" --short 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tail -1; echo "   model requests: $(( $(n) - b ))"; }
run claude-contract
run claude-contract --keep-home
run "claude-contract+DISABLE_UPDATES=1"
run "claude-contract+CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1"
run "claude-contract+DISABLE_UPDATES=1,CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1,DISABLE_GROWTHBOOK=1"
run "claude-contract+DISABLE_UPDATES=1,CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1,DISABLE_GROWTHBOOK=1" --keep-home
echo BISECT-DONE
