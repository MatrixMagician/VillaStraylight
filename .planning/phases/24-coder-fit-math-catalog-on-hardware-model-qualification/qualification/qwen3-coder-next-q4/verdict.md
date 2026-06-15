# Qualification Verdict: qwen3-coder-next-q4 @ agent_ctx 131072

**Entry:** `qwen3-coder-next-q4` — Qwen3-Coder-Next UD-Q4_K_XL (128 GB tier; D-12 ship gate)
**Date:** 2026-06-12 (gfx1151 Strix Halo dev box, Fedora 44, rootless podman 5.8.2)
**Protocol:** 24-RESEARCH Pattern 4 steps 2–9 via `qualification/qualify.sh`

## Server version (pinned-digest proof, T-24-07)

```
version: 9496 (94a220cd6)
built with GNU 15.2.1 for Linux x86_64
```

Served BY DIGEST `sha256:9a74e555…`; API responses carry `system_fingerprint: b9496-94a220cd6`.

## Arch support (D-12 gate input)

`general.architecture = qwen3next` loaded and generated under build 9496 — the
Qwen3-Next/DeltaNet arch (PR #16095) works on the PINNED image. Tightest-fit load
(45.89 GiB weights into the 62.54 GiB envelope) succeeded with villa-llama quiesced.

## Measured vs computed (D-09) — incl. the A2 DeltaNet constant

| Quantity | Computed (24-RESEARCH) | Measured | Delta |
|----------|------------------------|----------|-------|
| KV @131072 (f16, n_layers:12 encoding) | 3.00 GiB (24 KiB/token x 131072) | `Vulkan0 KV buffer size = 3072.00 MiB` = 3.00 GiB; log confirms `131072 cells, 12 layers, K 1536 + V 1536 MiB` | **exact match — the n_layers:12 hybrid encoding is correct (Pitfall 2 ruled out: NOT ~12 GiB)** |
| Model weights | 46.20 GiB | `Vulkan0 model buffer size = 46989.32 MiB` = 45.89 GiB | -0.31 GiB |
| DeltaNet recurrent state (A2) | est. ~75–150 MiB, ctx-INdependent | `llama_memory_recurrent: Vulkan0 RS buffer size = 301.50 MiB` (4 cells/seqs x 48 layers; ~75.4 MiB per seq) | **measured constant: 301.50 MiB at default 4 slots — ctx-independent, fold into min_envelope_bytes if material (24-04 call); per-slot 75.4 MiB matches the estimate** |
| Compute buffers | (unmodeled) | `Vulkan0 compute buffer = 216.38 MiB` + host 136.02 MiB | — |
| Total GTT footprint | ~49.2 GiB (weights+KV) / ~56.7 incl. headroom claim | GTT delta 51030 MiB = 49.83 GiB (baseline 1851 MiB -> 52881 MiB) | +0.64 GiB over weights+KV = RS (301.5) + compute (216.4) + misc; well inside the 62.54 GiB envelope |

Evidence: `kv-gtt.txt` (incl. appended hybrid-arch detail lines).

## Residency (offload-asserting)

```
load_tensors: offloaded 49/49 layers to GPU
load_tensors: Vulkan0 model buffer size = 46989.32 MiB
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

Probe conditions (protocol step 9): verdict true requires BOTH (a) neither
incompatibility warning string in the journal AND (b) turn-2 `timings.cache_n > 0`.

- Journal scan: **NO** `cache reuse is not supported - ignoring n_cache_reuse` /
  `cache_reuse is not supported by this context` warning (WARNING_PRESENT=false).
- Turn-2 `timings.cache_n = 58` (> 0).
- **Probe verdict by the protocol's stated criteria: true.**

**Honesty nuance for 24-04 (A3 expected false — evidence decided otherwise):** the
hybrid does NOT do GQA KV shifting; build 9496 serves it with **recurrent-state
context checkpoints** (`context checkpoints enabled, max = 32, min spacing = 256`;
`restored context checkpoint … size = 75.376 MiB` — exactly the per-seq DeltaNet
state). Turn-2 prefix reuse worked through checkpoint restore, no warning was
emitted, and the server stayed healthy with `--cache-reuse 256` present. Whether
the catalog should claim `cache_reuse_safe: true` for the Next entries (i.e. let
Phase 25 render `--cache-reuse 256`) is plan 24-04's reconciliation call — the
flag is demonstrably harmless on build 9496, but the reuse mechanism is
checkpointing, not n_cache_reuse chunk reuse. Evidence: `cache-reuse.txt`
(probe output + appended mechanism lines).

cache-reuse: true
VERDICT: PASS
