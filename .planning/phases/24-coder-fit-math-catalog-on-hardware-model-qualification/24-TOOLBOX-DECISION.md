# 24-TOOLBOX-DECISION — D-11 Toolbox Keep/Re-Pin Decision Record

**Decision ID:** D-13 (ratifies the D-11 toolbox keep/re-pin question with on-hardware evidence)
**Phase:** 24 — Coder Fit Math, Catalog & On-Hardware Model Qualification
**Plan:** 24-04 (reconciliation + freeze)
**Date:** 2026-06-13 (gfx1151 Strix Halo dev box, Fedora 44)
**Status:** Recorded BEFORE the catalog freeze (SC#4 / CODER-03 gate)

---

## Decision

Decision: KEEP the pinned digest `sha256:9a74e555…` (llama.cpp build 9496, commit 94a220cd6) — no re-pin.

The pinned `vulkan-radv` toolbox digest qualified all three coder entries on hardware
with full Qwen3-Next/DeltaNet arch support and a working Qwen3-Coder tool-call parser.
The pre-identified fallback re-pin (the drifted tag's build 9579, `sha256:9df33843…`,
locally present) is **NOT** needed — no 9496-specific parser/arch failure surfaced.
The pin is unchanged; nothing lands at the `internal/inference` seam in Phase 25 on
account of this decision. (Had this been a re-pin, the digest would have been recorded
here and applied at the inference seam in Phase 25 — digest-pinned, never floating/nightly,
per the v1.1 standing decision; no Go-side image literal lives in Phase 24, TestSeamGrepGate.)

---

## D-11 Required Checks — static + functional evidence

D-11 requires verifying three things against the pinned digest before the freeze: (1)
Qwen3-Next/DeltaNet arch support, (2) tool-call parser vintage, and (3) per-model
`--cache-reuse` semantics. Each is closed by BOTH static binary-string evidence and a
functional on-hardware proof — the binary strings alone never close D-11; the agent-loop
run is the closure (24-RESEARCH §Toolbox Vintage Findings recommended verdict).

### Pinned-digest proof (T-24-07 — every qualification ran by digest, never the tag)

```
version: 9496 (94a220cd6)
built with GNU 15.2.1 for Linux x86_64
```

Served BY DIGEST `sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad`
(the drifted `vulkan-radv` tag resolves to build 9579 — explicitly avoided). API
responses carry `system_fingerprint: b9496-94a220cd6`. The local tag-vs-digest drift is
documented in 24-RESEARCH §Toolbox Vintage Findings (Tag drift row) and Pitfall 1.

- **Evidence:** `qualification/qwen3-coder-30b-a3b/server-version.txt`,
  `qualification/qwen3-coder-next-q4/server-version.txt`,
  `qualification/qwen3-coder-next-q3/server-version.txt`,
  and the `b9496-94a220cd6` fingerprint in each entry's `cache-reuse.txt`.

### Check 1 — Qwen3-Next / DeltaNet arch support (llama.cpp PR #16095)

- **Static:** `libllama.so` contains the arch strings `qwen3next`, `qwen3moe`, `qwen35moe`,
  `qwen3vl` (binary grep in the pinned image — 24-RESEARCH §Toolbox Vintage Findings table).
- **Functional:** both Next entries loaded with `general.architecture = qwen3next` and
  GENERATED on hardware under build 9496 — full 49/49 offload, KV log confirms the
  hybrid encoding (`131072 cells, 12 layers, K 1536 + V 1536 MiB`) plus the DeltaNet
  recurrent-state buffer (`llama_memory_recurrent: Vulkan0 RS buffer size = 301.50 MiB`).
- **Evidence:** `qualification/qwen3-coder-next-q4/verdict.md` (§Arch support),
  `qualification/qwen3-coder-next-q4/kv-gtt.txt` (hybrid-arch detail lines),
  `qualification/qwen3-coder-next-q3/verdict.md`, `qualification/qwen3-coder-next-q3/kv-gtt.txt`.
- **Result:** PASS — the hybrid arch is fully supported on the pinned image.

### Check 2 — Tool-call parser vintage (Feb-2026 Qwen3-Coder fixes)

- **Static:** `libllama-common.so` contains the `Qwen3-Coder-` chat-format string plus the
  `<tool_call>` and `<function=` XML markers; the `--jinja` tools guard
  (`tools param requires --jinja`) is present in `libllama-server-impl.so`
  (24-RESEARCH §Toolbox Vintage Findings). Builds 9496/9579 are June-2026 master builds,
  ~4 months after the Feb-2026 fixes.
- **Functional:** for every entry the tool-call smoke (cheap disqualifier, ran before the
  agent loop) returned HTTP 200 with well-formed `tool_calls` carrying STRING `arguments`
  (`{"city":"Berlin"}`) — no llama.cpp #20198-class failure — and the no-`properties`
  variant returned HTTP 200 (must-not-500). The full agent loops then executed real
  multi-step tool use: ≥3 distinct tool types actually executed (`bash`/`view`/`edit`,
  and `glob` on the Next runs), the agent fixed the seeded `i < n` → `i <= n` off-by-one,
  the final `go test ./...` was green, zero 5xx while tools were present, and no narrated
  raw-XML tool calls leaked as prose.
- **Evidence:** `qualification/qwen3-coder-30b-a3b/{smoke.json,crush-transcript.txt,server.log,verdict.md}`,
  `qualification/qwen3-coder-next-q4/{smoke.json,crush-transcript.txt,verdict.md}`,
  `qualification/qwen3-coder-next-q3/{smoke.json,crush-transcript.txt,verdict.md}`.
- **Result:** PASS — the Qwen3-Coder tool-call path works functionally on the pinned image.

### Check 3 — Per-model `--cache-reuse` semantics → catalog `cache_reuse_safe`

- **Static:** the `--cache-reuse` flag is present in `--help`; both degrade strings
  (`cache reuse is not supported - ignoring n_cache_reuse` and
  `cache_reuse is not supported by this context`) are present in the binary — an
  incompatible model IGNORES cache-reuse with a journal warning, it does not crash
  (24-RESEARCH §Toolbox Vintage Findings + §Cache-Reuse Compatibility).
- **Functional (per-entry probe, protocol step 9 — verdict true requires the warning
  ABSENT AND turn-2 `cache_n > 0`):**
  - `qwen3-coder-30b-a3b` @65536: WARNING_PRESENT=false, turn-2 `cache_n = 135` → **true**.
    Standard full-attention GQA (`Qwen3MoeForCausalLM`) — matches the expected outcome.
  - `qwen3-coder-next-q4` @131072: WARNING_PRESENT=false, turn-2 `cache_n = 58` → **true**.
  - `qwen3-coder-next-q3` @131072: WARNING_PRESENT=false, turn-2 `cache_n = 58` → **true**.
- **FINDING A3 — result differs from the pre-probe expectation, and the mechanism nuance
  is the point:** RESEARCH §Cache-Reuse Compatibility expected the Next hybrids to come
  back `false` (DeltaNet/recurrent caches do not support KV shifting). The probe instead
  returned **true** for both Next entries: build 9496 serves the hybrid with
  **recurrent-state context checkpoints** (`context checkpoints enabled, max = 32,
  min spacing = 256`; `restored context checkpoint … size = 75.376 MiB` — exactly the
  per-seq DeltaNet state), NOT `n_cache_reuse` chunk reuse. Turn-2 prefix reuse worked
  through checkpoint restore, no incompatibility warning was emitted, and the server
  stayed healthy with `--cache-reuse 256` present. The flag is therefore demonstrably
  HARMLESS on build 9496 for all three entries.
- **Reconciliation call (24-04, Task 1):** the catalog `cache_reuse_safe` value is the
  literal probe verdict — all three set to `true`. The honest meaning of the flag is
  "rendering `--cache-reuse 256` (Phase 25) is safe on this entry under build 9496," which
  is exactly what the evidence shows; the reuse mechanism for the Next entries is
  checkpointing, recorded here so Phase 25 / future re-pins know the claim's basis is not
  GQA KV shifting. If a future toolbox re-pin removes context-checkpoint support, this
  claim must be re-probed (the claim is build-9496-scoped).
- **Evidence:** `qualification/qwen3-coder-30b-a3b/cache-reuse.txt`,
  `qualification/qwen3-coder-next-q4/cache-reuse.txt`,
  `qualification/qwen3-coder-next-q3/cache-reuse.txt`.
- **Result:** PASS (all three `cache_reuse_safe: true`, mechanism nuance recorded).

---

## D-12 ship gate (Qwen3-Coder-Next)

D-12: the Next entries ship ONLY if the pinned image proves the arch + tool-call path on
hardware; otherwise the 96/128 GB tiers ship 30B-A3B only (deletion over hope). Both Next
entries PASSED arch (Check 1) and tool-call (Check 2) on the pinned digest → **both Next
entries SHIP.** The deletion contingency was not triggered.

## D-10 disposition (failed entries deleted/re-pinned)

D-10: entries that fail qualification are deleted or re-pinned, never shipped on hope.
**No entry failed** — all three returned `VERDICT: PASS`. No deletion or re-pin was
required; this is recorded explicitly because delete-over-hope was the contingency, not
the outcome.

---

## Provenance

- D-11/D-12 decision text: `24-CONTEXT.md` §Toolbox re-pin decision.
- Static binary-string evidence: `24-RESEARCH.md` §Toolbox Vintage Findings (gathered on
  this box 2026-06-12) and §Cache-Reuse Compatibility.
- Functional on-hardware evidence: `qualification/qwen3-coder-*/` (verdicts, transcripts,
  smoke, server logs, KV/GTT, cache-reuse probes) — operator-approved in 24-03
  (`qualification/REVIEW.md`).
- Per-entry summary table: `24-QUALIFICATION-EVIDENCE.md`.

---

## Freeze Ratification (Task 3 — SC#1/SC#4 blocking checkpoint)

**Status: APPROVED — 2026-06-13.** The operator reviewed the freeze evidence chain at the
blocking human-verify checkpoint and ratified the catalog freeze:

- Reviewed the `internal/catalog/seed.json` diff against the plan 24-01 state — the only
  reconciliation change is `cache_reuse_safe: true` added to the two hybrid Next entries
  (exactly 2 lines; commit `e18ce7b`). No FAIL entry shipped; no deletion/re-pin required.
- Reviewed this decision record (`24-TOOLBOX-DECISION.md`) — accepted **Decision: KEEP**
  the pinned digest `sha256:9a74e555…` (build 9496), no re-pin; nothing lands at the
  `internal/inference` seam in Phase 25 on account of D-11.
- Reviewed `24-QUALIFICATION-EVIDENCE.md` — confirmed every entry's disposition traces to
  its `qualification/*/verdict.md` (all three PASS).
- Explicitly accepted the **build-9496-scoped** `cache_reuse_safe: true` truth-up on the two
  Next entries, with the mechanism = DeltaNet recurrent-state context checkpoints (Check 3 /
  FINDING A3), to be re-probed if a future toolbox re-pin removes context-checkpoint support.
- Confirmed the recommend golden (`cmd/villa/testdata/recommend.golden.json`) is untouched
  since its single 24-02 re-freeze (`be8ee0e`) and `make check` is green incl.
  `TestSeamGrepGate`.

The catalog is **FROZEN**. This closes the SC#1 freeze and the SC#4 decision-before-freeze
gate; CODER-01 and CODER-03 are satisfied. Phase 24 plans are complete.
