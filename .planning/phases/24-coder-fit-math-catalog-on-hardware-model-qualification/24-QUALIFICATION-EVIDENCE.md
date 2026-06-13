# 24-QUALIFICATION-EVIDENCE — On-Hardware Coder Qualification Summary

**Phase:** 24 — Coder Fit Math, Catalog & On-Hardware Model Qualification
**Plan:** 24-04 (reconciliation + freeze)
**Box:** gfx1151 Strix Halo dev host, Fedora 44, rootless podman 5.8.2
**Server:** llama.cpp build 9496 (94a220cd6) served BY DIGEST `sha256:9a74e555…` (D-13: KEEP)
**Source evidence:** `qualification/qwen3-coder-*/` (operator-approved in 24-03, `qualification/REVIEW.md`)

This consolidates the on-hardware qualification (24-03) into a per-entry table and records
the reconciliation dispositions applied to the frozen catalog (24-04, Task 1).

---

## Per-entry summary

| Entry | quant | tier | agent_ctx | Verdict | Offload | Measured KV vs computed | Measured weights | Total GTT footprint (envelope) | cache-reuse probe → `cache_reuse_safe` | Disposition (D-10/D-12) |
|-------|-------|------|-----------|---------|---------|--------------------------|------------------|--------------------------------|----------------------------------------|--------------------------|
| `qwen3-coder-30b-a3b` | UD-Q4_K_XL | 64 | 65536 | **PASS** | 49/49 | 6.00 GiB measured = 6.00 GiB computed — **exact** | 16.28 GiB (-0.17 vs 16.45 computed) | 22.67 GiB (in ~26.2 GiB fit claim; floor 28 GB) | true (warn absent, turn-2 `cache_n=135`) → **true** | **KEEP / ship** |
| `qwen3-coder-next-q4` | UD-Q4_K_XL | 128 | 131072 | **PASS** | 49/49 | 3.00 GiB measured = 3.00 GiB computed — **exact** (n_layers:12 hybrid encoding validated) | 45.89 GiB (-0.31 vs 46.20 computed) | 49.83 GiB (in 62.54 GiB envelope; floor 60 GB) | true via context checkpoints (warn absent, turn-2 `cache_n=58`) → **true** (A3) | **KEEP / ship** (D-12 gate passed) |
| `qwen3-coder-next-q3` | UD-Q3_K_XL | 96 | 131072 | **PASS** | 49/49 | 3.00 GiB measured = 3.00 GiB computed — **exact** (n_layers:12 hybrid encoding validated) | 33.48 GiB (-0.31 vs 33.79 computed) | 37.42 GiB (floor 45 GB) | true via context checkpoints (warn absent, turn-2 `cache_n=58`) → **true** (A3) | **KEEP / ship** (D-12 gate passed) |

Evidence files per entry: `qualification/<id>/{verdict.md, kv-gtt.txt, cache-reuse.txt, server-version.txt, smoke.json, crush-transcript.txt, server.log}`.

---

## D-09 measured-vs-computed reconciliation (what changed in the frozen catalog)

- **KV @agent_ctx:** exact match for all three (6.00 / 3.00 / 3.00 GiB). The Next
  `n_layers:12` hybrid encoding (full-attention layer count of `Qwen3NextForCausalLM`,
  48 / `full_attention_interval` 4) is **validated** — Pitfall 2 (the ~12 GiB 4× overcount)
  is ruled out. No KV-dimension edit was needed; no value was silently changed.
- **Measured weights** are slightly LESS than computed (host/CPU-side buffers excluded:
  -0.17 GiB for 30B, -0.31 GiB for each Next). Measured-below-computed never forces a fold.
- **A2 DeltaNet recurrent-state constant:** `llama_memory_recurrent: Vulkan0 RS buffer
  size = 301.50 MiB` at the default 4 slots (4 cells × 48 layers; ~75.4 MiB per seq),
  measured **identically** on both Next runs — confirmed ctx- AND quant-independent, as
  modeled. Per RESEARCH Pattern 3 this constant belongs in the floor, never the
  ctx-proportional KV dims.
- **min_envelope_bytes fold check:** each entry's total measured GTT footprint
  (incl. RS + compute buffers) plus 12% headroom sits comfortably INSIDE its existing
  `min_envelope_bytes` floor:
  - 30B: 22.67 GiB × 1.12 ≈ 25.4 GiB < 28 GB floor (26.07 GiB). OK.
  - Next-q4: 49.83 GiB × 1.12 ≈ 55.8 GiB < 60 GB floor (55.88 GiB). OK.
  - Next-q3: 37.42 GiB × 1.12 ≈ 41.9 GiB < 45 GB floor (41.91 GiB). OK.
  → **No `min_envelope_bytes` fold applied** — the 24-01 floors already absorb the
  measured reality including the A2 constant. Recorded as an explicit no-op so the
  decision is traceable.

## cache_reuse_safe truth-up (A3) — the one catalog value that changed

The on-hardware probe returned `true` for all three entries. RESEARCH expected the Next
hybrids to be `false` (no KV shifting); they came back `true` because build 9496 reuses
prefix via **DeltaNet recurrent-state context checkpoints** (75.376 MiB snapshots), not
`n_cache_reuse` chunk reuse — `--cache-reuse 256` is harmless (no degrade warning, healthy
server, turn-2 `cache_n>0`). Per Task 1, the catalog value is the literal probe verdict:

- `qwen3-coder-30b-a3b`: already `true` (24-01) — confirmed, unchanged.
- `qwen3-coder-next-q4`: `cache_reuse_safe` **added: true** (was absent ⇒ false).
- `qwen3-coder-next-q3`: `cache_reuse_safe` **added: true** (was absent ⇒ false).

The mechanism nuance (checkpointing, not KV shifting) is recorded in
`24-TOOLBOX-DECISION.md` Check 3 — the claim is build-9496-scoped and must be re-probed if
a future toolbox re-pin removes context-checkpoint support. `internal/catalog/catalog_test.go`
expected-value map tracks `true/true/true`. Committed in `e18ce7b`.

## D-10 / D-12 dispositions

- **D-10:** no entry returned a FAIL verdict → no entry deleted or re-pinned. All three
  ship. (Delete-over-hope was the contingency, not the outcome.)
- **D-12:** both Next entries proved arch (`qwen3next` loaded + generated) and the
  tool-call path on the pinned digest → both ship; the 96/128 GB tiers are NOT reduced to
  30B-A3B-only. Gate passed.

## Frozen catalog state (post-reconciliation)

Three surviving `role:"coder"` entries, all `cache_reuse_safe: true`, all
revision-pinned GGUF shards (no `/resolve/main/`), schema 3:
`['qwen3-coder-30b-a3b', 'qwen3-coder-next-q4', 'qwen3-coder-next-q3']`.
`make check` green; recommend golden byte-identical to its 24-02 freeze (last touched by
commit `be8ee0e`, untouched by 24-04).
