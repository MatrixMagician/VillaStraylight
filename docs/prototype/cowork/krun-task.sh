#!/usr/bin/env bash
# PROTOTYPE — the full sandbox topology in one run: Claude Code inside a libkrun microVM,
# on an INTERNAL podman network (no egress) that also carries the :8081 probe server,
# a single rw folder grant, no GPU. Runs task T2 and diffs the workspace.
#   krun-task.sh [T2]
set -uo pipefail
cd "$(dirname "$0")"
T=${1:-T2}; IMG=registry.fedoraproject.org/fedora:44
CLAUDE=$(readlink -f "$(command -v claude)")
NET=proto-internal; SRV=villa-proto-cowork
podman network exists $NET || podman network create --internal $NET >/dev/null
podman network connect $NET $SRV 2>/dev/null || true
run() { podman run --rm --runtime=krun --network $NET --memory 4g --cpus 4 \
  -v "$PWD/workspace:/workspace:Z" -v "$CLAUDE:/usr/local/bin/claude:ro,z" -w /workspace \
  -e HOME=/tmp -e ANTHROPIC_BASE_URL=http://$SRV:8081 -e ANTHROPIC_AUTH_TOKEN=local \
  -e ANTHROPIC_MODEL=qwen3.6-35b-a3b -e ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3.6-35b-a3b \
  -e CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 -e DISABLE_TELEMETRY=1 -e DISABLE_ERROR_REPORTING=1 \
  -e DISABLE_AUTOUPDATER=1 -e DO_NOT_TRACK=1 -e CLAUDE_CODE_MAX_CONTEXT_TOKENS=131072 -e PROMPT -e CLAUDE_CODE_TMPDIR=/tmp/cc -e IS_SANDBOX=1 "$IMG" "$@"; }
{
echo "== krun-task $T $(date -Is)"
run uname -r | sed 's/^/   vm kernel: /'
run curl -sf -m 5 https://registry.fedoraproject.org >/dev/null 2>&1 && echo "   egress: OPEN (bad)" || echo "   egress: blocked (internal net)"
run curl -sf -m 5 http://$SRV:8081/health | sed 's/^/   llama on internal net: /'
(cd workspace && find . -type f | sort) > results/krun-$T.before
start=$(date +%s)
export PROMPT="$(cat tasks/$T-*.txt)"; run sh -c 'mkdir -p /tmp/cc && exec timeout 1200 claude -p "$PROMPT" --dangerously-skip-permissions --max-turns 40 --output-format text' </dev/null 2>&1 | grep -v "connectors\|model catalog\|unrecognized_model"
echo "-- exit=${PIPESTATUS[0]} wall=$(( $(date +%s) - start ))s"
(cd workspace && find . -type f | sort) > results/krun-$T.after
diff results/krun-$T.before results/krun-$T.after
} 2>&1 | tee results/krun-$T.log
