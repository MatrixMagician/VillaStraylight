---
status: testing
phase: 09-villa-bench-honest-a-b
source: [09-VERIFICATION.md]
started: 2026-06-06T00:00:00Z
updated: 2026-06-06T00:00:00Z
---

# Phase 9: `villa bench` (Honest A/B) — User Acceptance Testing

**Phase goal:** A user can prove, on their own loaded model, whether ROCm is actually
faster than Vulkan — `villa bench` runs an honest A/B over the running endpoint,
reporting prompt-processing (pp) and token-generation (tg) throughput SEPARATELY (never
a single blended number), over residency-checked runs only. The per-metric throughput
delta is the milestone's proof-of-value.

**Off-hardware status:** 7/7 must-haves verified (see `09-VERIFICATION.md`). The honest-
methodology machinery is fully built and green; full suite passes; zero new deps. The
items below are the ROADMAP-flagged **on-hardware** proofs that only the AMD Strix Halo
(gfx1151) host can exercise.

## Gaps

_None in code._ All open items are on-hardware UAT (GPU-only proofs), not code gaps.

## Tests

### 1. `villa bench` on a real loaded model (single backend)
- **Run:** with `villa-llama` up and a model loaded, run `villa bench` (and `villa bench --json`).
- **Expected:**
  - pp and tg tok/s reported as **two separate figures** (median ± stddev), never one blended number.
  - `Kept > 0` and `Void` counts shown; exit 0.
  - The live llama-server `/v1` response actually carries the `timings` block. If it does
    not, runs VOID honestly via `ErrNoTimings` — in that case fall back to `/completion`
    (research Assumption A1) and re-test.
- **Why human:** requires the GPU + a loaded model; `/v1` timings presence and live
  throughput cannot be observed off-hardware. ROADMAP on-hardware research flag.

### 2. `villa bench --ab` flipping Vulkan↔ROCm on the live host
- **Run:** `villa bench --ab` on the gfx1151 host with ROCm available.
- **Expected:**
  - Identical spec applied to both sides.
  - The ROCm side reaches GPU residency (SELinux `/dev/kfd` / `container_use_devices`
    correct) so its runs are **KEPT, not VOID**.
  - A per-metric **Δpp and Δtg** delta with noise band is produced (the milestone's
    proof-of-value).
  - The original backend is **restored** afterward — confirm with `villa backend show`.
- **Why human:** needs the live backend switch + ROCm container device access (SELinux
  `/dev/kfd`), exercisable only on the GPU host. The delta magnitude is the ROADMAP-
  flagged on-hardware item.

### 3. ROCm `--ab` side residency on the real host
- **Run:** during the `--ab` ROCm side, observe the residency verdict.
- **Expected:** `RunningOffloadVerdict` returns `StatusPass` for ROCm runs (markers via
  `ResidencyProof`, `gpu_busy` sampled during decode); a CPU-fallback run is correctly
  **VOIDed**, not folded as a slow pass.
- **Why human:** the residency verdict over real journal/GTT/`gpu_busy` signals can only
  be exercised against a live ROCm container on gfx1151.

---

## How to record results

After running on-hardware, update each test's status and re-run verification:

```
/gsd-verify-work 9
```

When all three pass, the phase verification re-runs as `passed` and Phase 9 closes.
