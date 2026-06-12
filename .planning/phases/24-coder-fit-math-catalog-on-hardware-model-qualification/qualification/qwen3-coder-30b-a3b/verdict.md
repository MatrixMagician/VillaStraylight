# Qualification Verdict: qwen3-coder-30b-a3b @ agent_ctx 65536

**Entry:** `qwen3-coder-30b-a3b` — Qwen3-Coder-30B-A3B-Instruct UD-Q4_K_XL
**Date:** 2026-06-12 (gfx1151 Strix Halo dev box, Fedora 44, rootless podman 5.8.2)
**Protocol:** 24-RESEARCH Pattern 4 steps 2–9 via `qualification/qualify.sh`

## Server version (pinned-digest proof, T-24-07)

```
version: 9496 (94a220cd6)
built with GNU 15.2.1 for Linux x86_64
```

Served BY DIGEST `sha256:9a74e555…` (never the drifted `vulkan-radv` tag, which is
build 9579). Evidence: `server-version.txt`; the cache-reuse probe API response also
carries `system_fingerprint: b9496-94a220cd6`.

## Measured vs computed (D-09)

| Quantity | Computed (24-RESEARCH) | Measured | Delta |
|----------|------------------------|----------|-------|
| KV @65536 (f16) | 6.00 GiB (96 KiB/token x 65536) | `Vulkan0 KV buffer size = 6144.00 MiB` = 6.00 GiB | **exact match** |
| Model weights | 16.45 GiB | `Vulkan0 model buffer size = 16674.36 MiB` = 16.28 GiB | -0.17 GiB (host/CPU-side buffers excluded) |
| Total GTT footprint | ~22.5 GiB (weights+KV, pre-headroom; ~26.2 incl. 12% headroom) | GTT delta 23211 MiB = 22.67 GiB (baseline 1840 MiB -> 25052 MiB) | +0.17 GiB over weights+KV (compute buffers); well inside the ~26.2 GiB fit-math claim |

Evidence: `kv-gtt.txt`.

## Residency (offload-asserting, never liveness)

```
load_tensors: offloaded 49/49 layers to GPU
load_tensors: Vulkan0 model buffer size = 16674.36 MiB
```

FULL offload (49/49). PASS.

## PASS criteria (RESEARCH Pattern 4 step 7 — all required)

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | >=3 distinct tool types actually executed | **PASS** — `bash` (3x), `view` (2x), `edit` (1x) | `crush-transcript.txt` (tool_call extraction from /tmp/qual-repo/.crush/crush.db) |
| 2 | Final `go test ./...` in /tmp/qual-repo green | **PASS** — `ok qualrepo`, go-test-exit=0; agent fixed the seeded `i < n` -> `i <= n` off-by-one | `crush-transcript.txt` (post-run section + git diff) |
| 3 | Zero 5xx on /v1/chat/completions while tools present | **PASS** — 10 chat/completions requests in serve log, no 5xx lines | `server.log` (savelog 5xx scan) |
| 4 | No narrated tool calls (raw `<tool_call>`/`<function=` XML as prose) | **PASS** — 0 matches in transcript | `crush-transcript.txt` grep |
| 5 | No `ggml_vulkan: Device memory allocation` failure | **PASS** — 0 matches | `server.log` |
| 6 | Session self-terminates | **PASS** — `crush run` exited on its own after confirming green tests | `crush-transcript.txt` |

Tool-call smoke (cheap disqualifier, ran before the agent loop):
- standard variant: HTTP 200, well-formed `tool_calls`, STRING `arguments` (`{"city":"Berlin"}`) — no llama.cpp #20198 class failure. Evidence: `smoke.json`.
- no-`properties` variant: HTTP 200 (must-not-500) — PASS. Evidence: `smoke.json`.

## Cache-reuse probe (D-09 / feeds `cache_reuse_safe`)

- Restarted with `--cache-reuse 256`.
- Journal scan: NO `cache reuse is not supported - ignoring n_cache_reuse` / `cache_reuse is not supported by this context` warning (WARNING_PRESENT=false).
- Turn-2 `timings.cache_n = 135` (> 0; turn-1 cache_n = 0 as expected for a cold slot).
- Both conditions met -> probe verdict **true** (matches the expected value licensed into the seed entry's `cache_reuse_safe: true`).

Evidence: `cache-reuse.txt`.

## Harness deviation (recorded for 24-04)

Crush v0.76.0 rejects `--yolo` when the `run` subcommand is present (root-local cobra
flag; plan/RESEARCH assumption A5). Auto-accept for the non-interactive loop was
implemented via the config `permissions.allowed_tools` list (view/ls/grep/glob/edit/
write/bash) in the qual `crush.json` — strictly TIGHTER than `--yolo` (no agent/
fetch/download auto-accept; T-24-09 unchanged). Model fault vs harness fault
isolation held: the smoke test passed before any agent attempt.

cache-reuse: true
VERDICT: PASS
