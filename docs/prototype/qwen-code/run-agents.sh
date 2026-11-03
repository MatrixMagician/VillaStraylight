#!/usr/bin/env bash
# PROTOTYPE (#385): Crush and Claude Code, each default / contract / project-override.
cd "$(dirname "$0")"
for t in "$@"; do
  ./seed.sh >/dev/null
  python3 -I agent_capture.py "$t" 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tail -1
done
echo AGENTS-DONE
