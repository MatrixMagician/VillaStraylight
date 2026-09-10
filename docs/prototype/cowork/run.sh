#!/usr/bin/env bash
# PROTOTYPE — runs each task headlessly with Claude Code against the :8081 server, using
# the SAME env `villa code --agent claude` sets (internal/agent/claude.go claudeEnv), only
# the port differs. Records transcript, wall time, and the workspace file-list diff per task.
# Usage: run.sh [T1 T2 ...]   (default: all)   Set MAXTURNS to cap the agent loop.
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p results
export ANTHROPIC_BASE_URL=http://127.0.0.1:8081 ANTHROPIC_AUTH_TOKEN=local \
  ANTHROPIC_MODEL=qwen3.6-35b-a3b ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3.6-35b-a3b \
  CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 DISABLE_TELEMETRY=1 DISABLE_ERROR_REPORTING=1 \
  DISABLE_AUTOUPDATER=1 DO_NOT_TRACK=1 CLAUDE_CODE_MAX_CONTEXT_TOKENS=131072
tasks=("$@"); [ ${#tasks[@]} -eq 0 ] && tasks=(T1 T2 T3 T4 T5)
for t in "${tasks[@]}"; do
  f=$(ls tasks/$t-*.txt); log=results/$t.log
  (cd workspace && find . -type f | sort) > results/$t.before
  echo "== $t  $(date -Is)" | tee "$log"
  start=$(date +%s)
  (cd workspace && timeout ${TIMEOUT:-1200} claude -p "$(cat ../$f)" \
      --dangerously-skip-permissions --max-turns ${MAXTURNS:-40} --output-format text) 2>&1 | tee -a "$log"
  rc=${PIPESTATUS[0]}
  echo "-- exit=$rc  wall=$(( $(date +%s) - start ))s" | tee -a "$log"
  (cd workspace && find . -type f | sort) > results/$t.after
  diff results/$t.before results/$t.after | tee -a "$log" || true
done
