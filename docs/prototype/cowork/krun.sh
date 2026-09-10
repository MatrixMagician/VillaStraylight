#!/usr/bin/env bash
# PROTOTYPE — does a libkrun microVM (podman --runtime=krun) give the Cowork sandbox
# topology? Five probes, each printed with a verdict. Never touches config or units;
# only reads the live villa-llama /health over the villa network.
#   krun.sh [crun|krun]      default: both, side by side
set -uo pipefail
cd "$(dirname "$0")"
IMG=registry.fedoraproject.org/fedora:44
CLAUDE=$(readlink -f "$(command -v claude)")
mkdir -p krun-workspace results
probe() {  # probe <runtime>
  local rt=$1 base=(podman run --rm --runtime="$rt" --network villa)
  echo "== runtime=$rt"
  local t0=$(date +%s.%N)
  local kern; kern=$("${base[@]}" "$IMG" uname -r 2>&1); local rc=$?
  printf '   start+uname: %.2fs rc=%s kernel=%s (host %s)\n' "$(echo "$(date +%s.%N) - $t0" | bc)" "$rc" "$kern" "$(uname -r)"
  [ $rc -ne 0 ] && { echo "   FAIL: container did not start"; return 1; }
  "${base[@]}" "$IMG" ls /dev/kfd /dev/dri >/dev/null 2>&1 && echo "   gpu: EXPOSED (bad)" || echo "   gpu: absent (good)"
  "${base[@]}" -v "$PWD/krun-workspace:/workspace:Z" "$IMG" sh -c "echo hello-from-$rt > /workspace/probe-$rt.txt" \
    && [ -f "krun-workspace/probe-$rt.txt" ] && echo "   mount rw: ok ($(cat krun-workspace/probe-$rt.txt))" || echo "   mount rw: FAIL"
  local h; h=$("${base[@]}" "$IMG" curl -sf -m 5 http://villa-llama:8080/health 2>&1) && echo "   llama over villa net: ok $h" || echo "   llama over villa net: FAIL $h"
  "${base[@]}" "$IMG" curl -sf -m 5 https://registry.fedoraproject.org >/dev/null 2>&1 && echo "   egress: OPEN (villa net is not internal)" || echo "   egress: blocked"
  local v; v=$("${base[@]}" -v "$CLAUDE:/usr/local/bin/claude:ro,z" "$IMG" claude --version 2>&1) && echo "   claude inside: ok $v" || echo "   claude inside: FAIL $v"
}
for rt in "${@:-crun krun}"; do probe "$rt"; done 2>&1 | tee -a results/krun.log
