---
phase: 10
slug: backend-tok-s-surfacing
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 10 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `10-RESEARCH.md` §"Validation Architecture". This phase is a
> read/surfacing capstone — almost all verification is automated Go unit/golden
> tests; the only manual items are on-hardware (live ROCm residency + live tok/s),
> deferred to UAT exactly as Phases 8/9 did.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table tests + golden files) + `cobra` command harness |
| **Config file** | none — `go test` |
| **Quick run command** | `go test ./internal/status/ ./internal/recommend/ ./internal/dashboard/ ./cmd/villa/ && go test ./internal/inference/ -run TestSeamGrepGate` |
| **Full suite command** | `go build ./... && go vet ./... && go test ./...` |
| **Estimated runtime** | ~30 seconds (full suite; quick run < 10s) |

---

## Sampling Rate

- **After every task commit:** Run the quick run command above.
- **After every plan wave:** Run the full suite command.
- **Before `/gsd-verify-work`:** Full suite green **AND** `git diff cmd/villa/testdata/detect.golden.json` empty (detect must stay byte-identical) **AND** the two re-frozen goldens (`status.json.golden`, `recommend.golden.json`) reviewed as pure-addition diffs.
- **Max feedback latency:** ~30 seconds.

---

## Per-Task Verification Map

> Plan/task IDs are filled by the planner; this map fixes the requirement → test binding the planner must honor.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 10-XX | — | — | DASH-06 | T-10-* / — | status `Report` carries backend+image from the resolved backend (not a literal) | unit | `go test ./cmd/villa/ -run TestStatus` | ✅ extend status_test.go | ⬜ pending |
| 10-XX | — | — | DASH-06 | — | ROCm-config residency verdict keys on `ROCm0` markers (SC#1 proof; no Vulkan default) | unit | `go test ./internal/status/ -run ROCm` | ❌ W0 | ⬜ pending |
| 10-XX | — | — | DASH-06 | — | live tok/s typed-optional, omitted when idle/unavailable (never a fabricated 0) | unit | `go test ./cmd/villa/ -run TestStatus` | ❌ W0 | ⬜ pending |
| 10-XX | — | — | DASH-06 | — | ROCm-readiness tri-state fold; unknown wins over not-ready (no-false-green) | unit | `go test ./internal/status/ -run Readiness` | ❌ W0 | ⬜ pending |
| 10-XX | — | — | DASH-06 | — | status `--json` golden re-frozen as a pure-addition diff | golden | `go test ./cmd/villa/ -run TestStatusJSONGolden` | ✅ status_test.go | ⬜ pending |
| 10-XX | — | — | REC-05 | — | `ROCmAdvice` derived purely from `rocm_readiness`; recommended `Backend` stays `vulkan` | unit | `go test ./internal/recommend/` | ✅ extend recommend_test.go | ⬜ pending |
| 10-XX | — | — | REC-05 | — | advice Note never promises a speed-up (no "faster"/"guaranteed"; contains "bench") | unit | `go test ./internal/recommend/ -run Advice` | ❌ W0 | ⬜ pending |
| 10-XX | — | — | REC-05 | — | recommend golden re-frozen as a pure-addition diff | golden | `go test ./cmd/villa/ -run <recommend golden>` | ✅ recommend_test.go | ⬜ pending |
| 10-XX | — | — | DASH-06/REC-05 | — | `detect.golden.json` stays byte-identical (no detect change) | golden | `go test ./cmd/villa/ -run TestDetect` | ✅ detect_test.go (green w/o `-update`) | ⬜ pending |
| 10-XX | — | — | DASH-06 | — | dashboard `/api/status` serves new fields; `metricsView` shape unchanged | unit | `go test ./internal/dashboard/` | ✅ extend api_test.go | ⬜ pending |
| 10-XX | — | — | DASH-06/REC-05 | — | seam gate: no backend marker literals (`ROCm0`, image tags, fault strings) leak into surfacing code | regression | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ seam_test.go (stays green) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/status/status_test.go` — ROCm-config residency test (`cfg.Backend="rocm"` → verdict keys on `ROCm0` markers, exercisable off-hardware via the `ResidencyProof()` markers); readiness-fold table (all-unset → `unknown`, all-Known-good → `ready`, one-Known-bad → `not-ready`, any-unknown → `unknown`).
- [ ] `cmd/villa/status_test.go` — tok/s `Deps` stub cases (generating → value rendered + labeled by backend, idle → omitted, scrape-unavailable → omitted; never a fabricated 0).
- [ ] `internal/recommend/recommend_test.go` — advice derivation table (ready / worth-trying / verify-with-bench) + `Backend` stays `vulkan` + Note-honesty assertion (no "faster"/"guaranteed", contains "bench").
- [ ] `internal/dashboard/*_test.go` — assert `/api/status` carries backend/image/rocm-readiness; assert `metricsView` JSON shape is UNCHANGED.
- [ ] Golden re-freeze mechanism: `go test ./cmd/villa/ -run <GoldenTest> -update` (the `-update` flag is defined once in `cmd/villa/detect_test.go` and shared across the package test binary). Confirm the exact recommend-golden test name via `grep "func Test" cmd/villa/recommend_test.go` before scripting `-update`.
- [ ] Framework install: **none** — existing Go test infra covers all phase requirements.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live ROCm residency surfaced correctly in `villa status` / dashboard on a real ROCm install | DASH-06 (SC#1) | Requires a real gfx1151 host with a ROCm backend actually loaded | On a ROCm-config host: `villa backend set rocm`, then `villa status` shows backend=rocm + image tag + a `ROCm0`-proven offload; dashboard Health panel shows the same. |
| Live token-generation tok/s rendered + labeled by backend under real generation | DASH-06 (SC#1) | Requires a model actively generating on the host | With a model loaded and a chat in flight: `villa status` and the dashboard Performance panel show a non-zero gen tok/s labeled by the active backend; idle shows the honest "Idle" state, not a 0. |

*Off-hardware coverage is complete; the two items above mirror the Phase 8/9 on-hardware UAT deferral.*

---

## Validation Sign-Off

- [ ] All tasks have automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
