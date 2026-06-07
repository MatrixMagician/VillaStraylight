---
phase: 12-rocm-6-4-4-alternate-backend
plan: 03
subsystem: bench
tags: [bench, ab, option-a, sc3, fail-closed, back-compat]
status: partial-awaiting-on-hardware
requires:
  - "internal/inference.BackendFor (rocm-6.4.4 / rocm-6.4.4-rocwmma cases — 12-01)"
  - "internal/backendswap.Run (LOCKED transactional switch — Phase 8)"
provides:
  - "BenchSpec.ABTarget — explicit arbitrary-pair --ab flip target (Option A, SC#3)"
  - "villa bench --ab-target <backend> flag with fail-closed BackendFor validation (D-03)"
affects:
  - "cmd/villa bench noun (additive flag; existing behavior + golden unchanged)"
tech-stack:
  added: []
  patterns:
    - "pure-core + cmd-tier seam: target is a config VALUE in the core; fail-closed resolve in cmd"
    - "package-level benchRun seam isolates os.Exit for off-hardware flag-plumbing tests"
key-files:
  created: []
  modified:
    - internal/bench/bench.go
    - internal/bench/bench_test.go
    - cmd/villa/bench.go
    - cmd/villa/bench_test.go
decisions:
  - "D-03 honored: an unknown --ab-target is fail-closed via inference.BackendFor BEFORE any switch — a typo is an actionable error, never a silent flip (T-12-07)."
  - "--ab-target without --ab is a usage error (clearer UX than silently ignoring the explicit target)."
  - "Unset --ab-target preserves the v1.1 other(orig) vulkan<->rocm 2-value swap exactly; existing bench.json.golden byte-unchanged (D-09)."
  - "Pure bench core stays inference-free: target resolution/validation lives only in the cmd tier (seam boundary preserved; TestSeamGrepGate green)."
metrics:
  duration: ~20 min
  completed: "2026-06-07"
  tasks_completed: 3 of 4 (Task 4 on-hardware checkpoint pending)
  files_modified: 4
---

# Phase 12 Plan 03: Arbitrary-Pair `bench --ab` (Option A) + On-Hardware UAT Summary

Added `BenchSpec.ABTarget` and a `villa bench --ab-target <backend>` flag so `--ab` can
measure an arbitrary backend pair (e.g. `rocm-6.4.4` vs `rocm-7.2.4`) instead of only the
hardcoded v1.1 `vulkan↔rocm` swap — closing the one genuine SC#3 design gap. The explicit
target is fail-closed validated in the cmd tier (`inference.BackendFor`) before any switch;
unset preserves the v1.1 behavior byte-for-byte. The on-hardware UAT (Task 4) is a
`checkpoint:human-verify` and is **pending operator action** (see "On-Hardware Checkpoint").

## What Was Built (autonomous, off-hardware)

### Task 1 — `BenchSpec.ABTarget` + effective-target threading (pure core) — `9fcd5d3`
- New `BenchSpec.ABTarget string` field (doc-commented as the Option A explicit target;
  empty = v1.1 `other(orig)` default).
- `Run` computes the effective target **once** after loading the original backend
  (`target := spec.ABTarget; if target == "" { target = other(orig) }`) and threads it into
  both the `d.Switch(ctx, target)` call and `abResult`.
- `abResult(orig, target, a, b)` so `ABResult.To` reflects the **actual** pair benchmarked;
  `From` always equals the loaded original; `DeltaPP`/`DeltaTG` stay B−A per metric (never blended).
- `other()` / `vulkan` / `rocm` consts retained unchanged as the unset default; **no
  `internal/inference` import** (pure-core boundary preserved).
- Tests: unset-default back-compat (flips to `rocm`, `To==rocm`), explicit-target flip
  (`rocm-6.4.4` → `rocm-7.2.4`, `To==rocm-7.2.4`), deltas-never-blended.

### Task 2 — `--ab-target` cobra flag + fail-closed validation (cmd tier) — `c049e31`
- New `--ab-target <backend>` string flag (default `""`) plumbed into `BenchSpec.ABTarget`.
- Fail-closed `inference.BackendFor(abTarget)` validation BEFORE constructing Deps / running
  (D-03 / T-12-07) — `--ab-target bogus` returns an actionable error and never reaches the run.
- `--ab-target` without `--ab` → usage error (the explicit target is only meaningful for the flip).
- Introduced the package-level `benchRun` seam (default runs `runBench` + `os.Exit`; test
  override captures the spec and returns) so flag-plumbing and no-switch-on-bad-target are
  asserted off-hardware without a subprocess.
- Existing `cmd/villa/testdata/bench.json.golden` is **byte-unchanged** (`git diff --stat` clean).

### Task 3 — full off-hardware gate — (verification-only, no commit)
- `make check` (`go vet ./...` + `go test ./...`) green across all 17 packages.
- `TestSeamGrepGate` + `TestROCmMarkerPresence` green (no marker leak into bench).
- Digest-leak guard: `grep -rn 'rocm-6.4.4@sha256' internal cmd | grep -v '_test.go'`
  returns only `internal/inference/backend_rocm.go` (source) + the orchestrate golden fixture
  (expected — goldens carry the rendered digest).

## On-Hardware Checkpoint (Task 4 — PENDING)

`checkpoint:human-verify` / `gate="blocking-human"`. **Not auto-performed** — running
`villa backend set rocm-6.4.4` would pull a multi-GB ROCm image and restart the live
inference stack on the gfx1151 host. ROCM-ALT-01 must remain OPEN until the operator runs:

1. **Digest currency** (rolling-tag drift guard):
   `skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4 | grep Digest`
   and the `-rocwmma` variant — confirm they match the pinned `sha256:c81f30a7…150ec62` /
   `sha256:9a97129a…3c0141`.
2. **Transactional switch + residency proof (SC#1):** `villa backend set rocm-6.4.4` → expect
   a transactional switch + residency-proof PASS (a silent/partial CPU fallback MUST FAIL and
   roll back verbatim — never a health-200 false-green). Verify `villa status` shows the active
   backend + 6.4.4 image + an honest OFFLOAD verdict.
3. Repeat for `villa backend set rocm-6.4.4-rocwmma` (residency PASS).
4. **Δtg recovery (SC#3):** `villa bench --ab --ab-target rocm-7.2.4` from a `rocm-6.4.4` config
   (and/or `--ab-target vulkan`) → confirm pp and tg deltas print SEPARATELY and identify which
   digest recovers the v1.1 Δtg −11.15 (record the user's keep choice — the bench-decided D-04).
5. **Restore default:** `villa backend set vulkan`.

**Resume signal:** "on-hardware verified" with the residency-proof result + bench Δtg figures,
or describe any failure (e.g. residency FAIL → A1/A2 needs a per-backend marker/env — re-open 12-01).

## Deviations from Plan

None — plan executed exactly as written for the autonomous tasks. The on-hardware Task 4 was
intentionally not auto-performed (it is a `gate="blocking-human"` checkpoint requiring the
gfx1151 host and a multi-GB image pull); returned as a structured checkpoint per directive.

## Authentication Gates

None.

## Known Stubs

None.

## Threat Flags

None — no new security surface beyond the threat-modeled `--ab-target` input (mitigated by the
fail-closed `BackendFor` validation, T-12-07) and the existing digest-pinned image pulls.

## Verification

- `go test ./internal/bench/ -count=1` → 10 passed.
- `go test ./cmd/villa/ -run Bench -count=1` → 20 passed.
- `make check` → green (vet clean + all 17 packages pass).
- `TestSeamGrepGate` / `TestROCmMarkerPresence` → green.
- `cmd/villa/testdata/bench.json.golden` → byte-unchanged.
- On-hardware (SC#1/SC#3 host-only legs) → **COMPLETED 2026-06-07 on the gfx1151 host** (see "On-Hardware UAT Results" below).

## On-Hardware UAT Results (Task 4 — COMPLETED 2026-06-07, gfx1151 Strix Halo host)

Operator ran the full on-hardware checkpoint on the live host (model `qwen3.6-35b-a3b`).
All four success criteria PASS as engineering deliverables; the **performance
hypothesis is DISPROVEN** by the very A/B the phase built.

**SC#1 — transactional switch + residency proof:** PASS.
- `villa backend set rocm-6.4.4` → switched + cutover **proven** (residency PASS); `villa status` = backend `rocm-6.4.4`, OFFLOAD PASS, image `@sha256:c81f30a7…`. Proven twice (initial switch + post-rollback restore).
- `villa backend set rocm-6.4.4-rocwmma` → **residency FAILED** ("not ready before timeout — possible load_tensors hang or CPU-fallback stall") and **rolled back verbatim** to rocm-6.4.4, which then recovered to OFFLOAD PASS. Offload-asserting FAIL path + transactional rollback both working as designed — a real honest FAIL, never a false-green.
- `villa backend set rocm` (restore) → switched + cutover proven (4th proven cutover).

**SC#2 — digest-pin + policy gate / fail-closed:** PASS.
- `--dry-run` → fit PASS + preflight PASS before any mutation.
- **Rolling-tag drift caught & survived:** `skopeo inspect` showed the live `rocm-6.4.4` tag re-pushed to `sha256:44f115e0…` (≠ pinned `c81f30a7…`) within the same day; the pinned digest still resolves & pulls (content-addressed), so the switch used the exact reproducible image. Pinning worked exactly as Pitfall 12 intended. (`-rocwmma` tag digest `9a97129a…` still matches.)
- `--ab-target rocm-7.2.4` → **fail-closed rejected** with named remediation (valid identifier is `rocm` = 7.2.4); zero side effects (D-03 validated).

**SC#3 — `bench --ab` arbitrary-pair, pp/tg separate:** PASS (mechanism). `--ab-target` works; pp/tg separate; all runs residency-checked (kept=5 void=0); auto-restore after each A/B. Conditions: warmup=1 reps=5 n_predict=128 seed=42 temp=0.

| A/B (Δ = A→B) | A pp | A tg | B pp | B tg | Δpp | Δtg |
|---|---|---|---|---|---|---|
| rocm-6.4.4 → rocm (7.2.4) | 118.68 | 49.28 | 122.50 | 50.36 | +3.82 | +1.08 |
| rocm-6.4.4 → vulkan | 118.25 | 49.11 | 116.54 | **60.79** | −1.72 | **+11.68** |

**Δtg verdict (the bench-decided D-04 outcome): rocm-6.4.4 does NOT recover the regression.**
Vulkan still leads tg by **~11.68 tok/s** over rocm-6.4.4 — essentially identical to v1.1's rocm-7.2.4 Δtg −11.15. rocm-6.4.4 ≈ rocm-7.2.4 (marginally slower on both). `-rocwmma` is non-functional on this host/model (residency FAIL). ROCm wins pp slightly; **Vulkan remains the tg winner and the correct default** — never auto-switched, as designed.

**SC#4 — seam grep-gate:** PASS (off-hardware; `TestSeamGrepGate` green).

**Net:** the engineering shipped correctly and safely (selectable, gated, residency-proven, honestly benchable alternate backend). The hypothesis it was built to test — that a TG-tuned rocm-6.4.4 image recovers the Δtg loss — is **disproven on this host/model**. The honest-A/B did exactly its job: prove, don't assume.

**Follow-ups surfaced (not blocking the capability):**
1. `rocm-6.4.4-rocwmma` residency FAIL — investigate whether it's a bounded-timeout tuning issue (older 8.04 GB / 4-month image) or a genuine gfx1151 incompatibility; it ships selectable but does not come up here.
2. Rolling-tag drift — the live `rocm-6.4.4` tag now points to a newer build (`44f115e0…`); the pin is still valid/reproducible, but a future re-pin could capture the newer build if desired.
3. Doc nit — the checkpoint instructions said `--ab-target rocm-7.2.4`; the valid backend identifier is `rocm`.

## Self-Check: PASSED

- Files: `internal/bench/bench.go`, `cmd/villa/bench.go`, `12-03-SUMMARY.md` all FOUND.
- Commits: `9fcd5d3` (Task 1), `c049e31` (Task 2) both FOUND in history.
