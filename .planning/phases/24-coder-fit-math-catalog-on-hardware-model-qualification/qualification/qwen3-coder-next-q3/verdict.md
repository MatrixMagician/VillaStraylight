# Qualification Verdict: qwen3-coder-next-q3 @ agent_ctx 131072

**Entry:** `qwen3-coder-next-q3` — Qwen3-Coder-Next UD-Q3_K_XL (96 GB tier; D-12 ship gate)
**Date:** 2026-06-13 (gfx1151 Strix Halo dev box, Fedora 44, rootless podman 5.8.2)
**Protocol:** 24-RESEARCH Pattern 4 steps 2–9 via `qualification/qualify.sh`

## Server version (pinned-digest proof, T-24-07)

```
version: 9496 (94a220cd6)
built with GNU 15.2.1 for Linux x86_64
```

Served BY DIGEST `sha256:9a74e555…`; API responses carry `system_fingerprint: b9496-94a220cd6`.

## Arch support (D-12 gate input)

`general.architecture = qwen3next` loaded and generated under build 9496 (same arch
proof as the Q4 run, independent load).

## Measured vs computed (D-09) — incl. the A2 DeltaNet constant

| Quantity | Computed (24-RESEARCH) | Measured | Delta |
|----------|------------------------|----------|-------|
| KV @131072 (f16, n_layers:12 encoding) | 3.00 GiB | `Vulkan0 KV buffer size = 3072.00 MiB`; log confirms `131072 cells, 12 layers, K 1536 + V 1536 MiB` | **exact match — n_layers:12 hybrid encoding correct (Pitfall 2 ruled out)** |
| Model weights | 33.79 GiB | `Vulkan0 model buffer size = 34280.86 MiB` = 33.48 GiB | -0.31 GiB |
| DeltaNet recurrent state (A2) | est. ~75–150 MiB, ctx-INdependent | `llama_memory_recurrent: Vulkan0 RS buffer size = 301.50 MiB` (4 cells/seqs x 48 layers; ~75.4 MiB per seq) | **identical constant to the Q4 run (301.50 MiB at default 4 slots) — ctx- and quant-independent, as modeled** |
| Compute buffers | (unmodeled) | `Vulkan0 compute buffer = 216.38 MiB` + host 136.02 MiB | — |
| Total GTT footprint | ~36.8 GiB (weights+KV) / ~42.4 incl. headroom claim | GTT delta 38319 MiB = 37.42 GiB (baseline 1851 MiB -> 40170 MiB) | +0.94 GiB over weights+KV = RS (301.5) + compute (216.4) + misc; comfortably inside the envelope |

Evidence: `kv-gtt.txt` (incl. appended hybrid-arch detail lines).

## Residency (offload-asserting)

```
load_tensors: offloaded 49/49 layers to GPU
load_tensors: Vulkan0 model buffer size = 34280.86 MiB
```

FULL offload (49/49). PASS.

## PASS criteria (RESEARCH Pattern 4 step 7 — all required)

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | >=3 distinct tool types actually executed | **PASS** — `bash` (2x), `glob` (1x), `view` (2x), `edit` (1x) = 4 distinct | `crush-transcript.txt` (tool_call extraction) |
| 2 | Final `go test ./...` green | **PASS** — `ok qualrepo`, go-test-exit=0; agent fixed `i < n` -> `i <= n` | `crush-transcript.txt` |
| 3 | Zero 5xx on /v1/chat/completions while tools present | **PASS** — no 5xx lines in serve log | `server.log` |
| 4 | No narrated tool calls (raw XML as prose) | **PASS** — 0 matches | `crush-transcript.txt` grep |
| 5 | No `ggml_vulkan: Device memory allocation` failure | **PASS** — 0 matches | `server.log` |
| 6 | Session self-terminates | **PASS** — `crush run` exited on its own | `crush-transcript.txt` |

Tool-call smoke (before agent loop): standard variant HTTP 200 with STRING
`arguments` (`{"city":"Berlin"}`); no-`properties` variant HTTP 200 (must-not-500)
— both PASS. Evidence: `smoke.json`.

## Cache-reuse probe (D-09 / feeds `cache_reuse_safe`) — RESULT DIFFERS FROM EXPECTATION

Same outcome and nuance as the Q4 run (independent probe):

- Journal scan: **NO** incompatibility warning (WARNING_PRESENT=false; neither
  build-9496 warning string appeared).
- Turn-2 `timings.cache_n = 58` (> 0).
- **Probe verdict by the protocol's stated criteria: true.**

**Honesty nuance for 24-04 (A3 expected false — evidence decided otherwise):**
reuse is achieved via recurrent-state context checkpoints (75.376 MiB per
checkpoint, restored on turn 2), not GQA KV shifting; `--cache-reuse 256` was
present and harmless on build 9496. Whether the catalog claims
`cache_reuse_safe: true` for the Next entries is plan 24-04's reconciliation
call. Evidence: `cache-reuse.txt` (probe output + appended mechanism lines).

cache-reuse: true
VERDICT: PASS
