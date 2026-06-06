---
status: complete
phase: 09-villa-bench-honest-a-b
source: [09-VERIFICATION.md, 09-01-SUMMARY.md, 09-02-SUMMARY.md, 09-03-SUMMARY.md]
started: 2026-06-06T00:00:00Z
updated: 2026-06-06T18:25:00Z
---

# Phase 9: `villa bench` (Honest A/B) — User Acceptance Testing

**Phase goal:** A user can prove, on their own loaded model, whether ROCm is actually
faster than Vulkan — `villa bench` runs an honest A/B over the running endpoint,
reporting prompt-processing (pp) and token-generation (tg) throughput SEPARATELY (never
a single blended number), over residency-checked runs only. The per-metric throughput
delta is the milestone's proof-of-value.

**On-hardware UAT executed on the live gfx1151 host.** This machine is the AMD Strix Halo
target: `AMD Radeon 8060S Graphics (RADV STRIX_HALO)`, `/dev/kfd` present, user in
`render`+`video` groups, `villa-llama.service` active with OFFLOAD **PASS** and a model
loaded. So the on-hardware proofs that `09-VERIFICATION.md` routed to `human_needed`
were exercised directly (not recorded as blocked).

## Current Test

[testing complete]

## Tests

### 1. `villa bench` on a real loaded model (single backend)
expected: pp and tg reported as two SEPARATE figures (median ± stddev); Kept>0 and Void shown; exit 0; live `/v1` carries the `timings` block (else VOID via ErrNoTimings → /completion fallback per A1).
result: pass
evidence: |
  Rebuilt `./villa` from ./cmd/villa first (prebuilt binary predated Phase-9 bench code).
  `villa bench`  → pp 112.51 ± 2.19, tg 60.52 ± 0.12, kept=5 void=0, exit 0.
  `villa bench --json` → "prompt_per_sec":112.86 / "predicted_per_sec":60.70 as separate
  keys (no blended key), "kept":5 "void":0, "mode":"single", exit 0.
  kept=5/void=0 confirms the live llama-server `/v1` response carries `timings` — no
  ErrNoTimings VOID, so the /completion fallback (A1) was not needed.

### 2. `villa bench --ab` flipping Vulkan↔ROCm on the live host
expected: Identical spec both sides; ROCm side reaches GPU residency (KEPT not VOID); per-metric Δpp and Δtg delta with noise band produced (the proof-of-value); original backend restored (`villa backend show`).
result: pass
evidence: |
  User-approved invasive flip. `villa bench --ab`, exit 0. Identical conditions both sides
  (warmup=1 reps=5 n_predict=128 seed=42 temp=0):
    A (vulkan): pp 113.49 ± 1.61, tg 60.29 ± 0.17, kept=5 void=0
    B (rocm):   pp 118.34 ± 4.42, tg 49.13 ± 0.06, kept=5 void=0
    delta (vulkan → rocm): Δpp +4.84, Δtg −11.15   ← per-metric, never blended
  Both sides labeled by backend name (the single-mode empty-label gap does NOT affect --ab).
  Backend RESTORED to vulkan post-run (`villa backend show` → vulkan @ same digest);
  `villa status` overall PASS, villa-llama OFFLOAD PASS.
  PROOF-OF-VALUE + honesty constraint confirmed on real hardware: ROCm WINS prompt-
  processing (Δpp +4.84) but REGRESSES token-generation (Δtg −11.15). The win is
  pp-weighted and tg regresses — reported as two separate figures, exactly as the
  milestone honesty constraint requires. A blended single number would have hidden this.

### 3. ROCm `--ab` side residency on the real host
expected: `RunningOffloadVerdict` returns StatusPass for ROCm runs (ResidencyProof markers, gpu_busy sampled during decode); a CPU-fallback run is correctly VOIDed, not folded as a slow pass.
result: pass
evidence: |
  Observed during Test 2's ROCm (B) side: kept=5 void=0 → all 5 ROCm runs passed the
  residency void-gate (RunningOffloadVerdict StatusPass; gpu_busy sampled during decode),
  i.e. genuine GPU residency on gfx1151 — not folded as a CPU pass. The negative case
  (forcing a CPU-fallback to confirm it VOIDs) was not artificially induced; the positive
  residency proof — the ROADMAP on-hardware item — is satisfied. CPU-fallback-VOID logic
  is covered off-hardware by TestVoidNonResident (09-VERIFICATION.md truth #2).

## Summary

total: 3
passed: 3
issues: 1
pending: 0
skipped: 0
blocked: 0

## Gaps

- truth: "`villa bench` (single mode) states which backend it measured"
  status: resolved
  resolved_by: "quick task 260606-p3a (fix 8aa9c90) — runBench now sets res.Backend = benchConfiguredBackend() on the !ab path; single-mode human header shows `backend (vulkan):` and --json single.backend == \"vulkan\" (verified live). internal/bench core unchanged; --ab + pp/tg-separate contract + golden no-blended-key invariant intact; 171 cmd+bench tests green."
  reason: "Found during Test 1: human-readable header is `backend ():` and --json `single.backend` is `\"\"` — the measured backend (vulkan) is never labeled in single mode. --ab mode labels sides correctly."
  severity: minor
  test: 1
  root_cause: "internal/bench/bench.go Run() single-backend path returns Result{Single: st} and never sets Result.Backend; cmd/villa/bench.go:495 benchEntryFromResult reads the empty res.Backend for the single side. cfg.Backend is available at runBench (cmd/villa/bench.go:215 liveMeasure(ctx, cfg.Backend, spec)) but is not threaded into the rendered/JSON result."
  artifacts:
    - path: "internal/bench/bench.go"
      issue: "Run() single path does not populate Result.Backend"
    - path: "cmd/villa/bench.go"
      issue: "benchEntryFromResult:495 reads empty res.Backend for single-mode side label"
  missing:
    - "Thread cfg.Backend into the single-mode result so the rendered header and --json single.backend name the measured backend (cosmetic/honesty completeness; does not affect the pp/tg-separate contract)"
