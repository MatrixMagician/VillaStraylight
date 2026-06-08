# Phase 12: `rocm-6.4.4` Alternate Backend - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-07
**Phase:** 12-rocm-6-4-4-alternate-backend
**Mode:** `--auto` — all gray areas auto-selected; recommended option chosen per area (no interactive prompts).
**Areas discussed:** Backend identity & selection, rocwmma variant strategy, Relationship to rocm-7.2.4, Backend delta reuse, Policy gating & preflight applicability, Surfacing & seam gate

---

## Backend identity & selection

| Option | Description | Selected |
|--------|-------------|----------|
| Distinct `BackendFor` strings | `rocm-6.4.4` (+ `-rocwmma`) as new resolver cases; matches ROADMAP SC#1 `villa backend set rocm-6.4.4` | ✓ |
| Sub-flag / `--image` variant of `rocm` | Keep one `rocm` backend, select image via flag/config sub-field | |

**Auto choice:** Distinct backend strings (recommended).
**Notes:** SC#1 literally specifies `villa backend set rocm-6.4.4`; the single-polymorphism `BackendFor` seam makes new cases zero-churn for the 8 call sites.

---

## rocwmma variant strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Ship BOTH variants selectable | plain + `-rocwmma`, both digest-pinned/seam-locked/policy-gated; `bench --ab` decides at runtime | ✓ |
| Ship only plain `rocm-6.4.4` | Defer `-rocwmma` to a later phase | |

**Auto choice:** Ship both (recommended).
**Notes:** "ship the one the A/B proves" requires the A/B to be runnable — shipping both is the honest reading; we cannot bench at plan time.

---

## Relationship to rocm-7.2.4

| Option | Description | Selected |
|--------|-------------|----------|
| Coexist | `rocm` stays = 7.2.4; new strings are additive | ✓ |
| Replace | Repoint `rocm` to 6.4.4 | |

**Auto choice:** Coexist (recommended).
**Notes:** SC#3 requires `bench --ab` to measure the new image **against** rocm-7.2.4, so 7.2.4 must remain selectable.

---

## Backend delta reuse

| Option | Description | Selected |
|--------|-------------|----------|
| Parameterize existing ROCm delta by image | Reuse kfd+dri/keep-groups/HSA/hipBLASLt/ResidencyProof; only digest differs | ✓ |
| New backend type per digest | Fork a fresh struct per image | |

**Auto choice:** Parameterize (recommended).
**Notes:** Research must confirm whether 6.4.4 needs a different HSA override / hipBLASLt setting (default assumption: identical to 7.2.4).

---

## Policy gating & preflight applicability

| Option | Description | Selected |
|--------|-------------|----------|
| Keep deny-list+floors; generalize ROCm predicate | Pin digests in seam; ROCm preflight fires for ALL ROCm-family backends | ✓ |
| Add an explicit image allowlist | Switch policy to allow-list gating | |

**Auto choice:** Keep deny-list + generalize predicate (recommended).
**Notes:** The live `PreflightROCm` closure gates on `cfg.Backend != "rocm"` — must widen to a ROCm-family predicate or the new backends skip the gate.

---

## Surfacing & seam gate

| Option | Description | Selected |
|--------|-------------|----------|
| No new surfacing; extend seam regex SAME commit | status/dashboard auto-reflect image tag; recommend stays generic; `seam_test.go` regex += `rocm-6.4.4` | ✓ |
| Add per-image recommend/status logic | New surfacing for the new image | |

**Auto choice:** No new surfacing + extend regex (recommended).
**Notes:** SC#4 — the new literal must fail `TestSeamGrepGate` if it leaks; regex extended in the introducing commit.

---

## Claude's Discretion

- Exact resolver string for the rocwmma variant (`rocm-6.4.4-rocwmma` assumed).
- Where the ROCm-family predicate lives (`internal/inference` preferred, keeps literals behind the seam).
- One image-parameterized struct vs thin sibling structs — planner picks lowest-churn that keeps the grep-gate green.

## Deferred Ideas

- ROCm perf-tuning knobs (hipBLASLt/rocWMMA-FA/batch) — `ROCM-TUNE-01`, deferred beyond v1.2.
- Per-image `recommend` advice — kept image-agnostic for honesty.
- Retiring rocm-7.2.4 once 6.4.4 proves a universal win — not this phase (needed as A/B baseline).
