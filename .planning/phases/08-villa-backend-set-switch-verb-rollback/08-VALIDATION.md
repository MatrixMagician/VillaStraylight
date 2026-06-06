---
phase: 8
slug: villa-backend-set-switch-verb-rollback
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-06
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib testing + table-driven, matching existing `cmd/villa` and `internal/*` suites) |
| **Config file** | none — `go.mod` at repo root; no separate test config |
| **Quick run command** | `go test ./internal/backendswap/... ./internal/inference/... ./cmd/villa/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/backendswap/... ./internal/inference/... ./cmd/villa/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

> Filled by the planner against the final task IDs. Every task that creates the rollback state-machine, the cutover gate, or the fit-guard MUST map to an automated `go test` command exercising injected `Deps` seams (no live hardware in unit tests). On-hardware behaviors (real ROCm offload, `load_tensors` hang) are listed under Manual-Only.
>
> Reconciled to the final structure: 5 tasks across 2 plans / 2 waves. Plan 01 (Wave 1) holds the transactional core (08-01-01/02) plus the exported running-server prove wrappers + cmd/villa seam-gate extension (08-01-03). Plan 02 (Wave 2) holds the live `liveProve` composition (08-02-01) and the cobra noun + exit mapping + live Deps + tests (08-02-02). The fit-guard and cutover-gate behaviors are unit-proven in the Plan-01 backendswap state-machine suite; the live wiring + command surface are proven in the Plan-02 cmd/villa suite.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 08-01-01 | 01 | 1 | BSET-01, BSET-02 | T-8-01 | capture verbatim prior unit+config bytes before any mutation; fit-guard FIRST; no-op same-backend | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-01-02 | 01 | 1 | BSET-01, BSET-02 | T-8-02 / T-8-03 / T-8-05 | any prove/cutover failure restores verbatim captured unit+config (no-op to running stack); cutover only on ProveStatusPass (is-active never sufficient); rollback-incomplete reported honestly; fit/preflight refuse with zero side effects | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-01-03 | 01 | 1 | BSET-02 | T-8-04 | export non-container running-server prove primitives (PollHealth/GenerationProbe) so cmd/villa can compose the cutover prove; extend TestSeamGrepGate to walk cmd/villa (committed literal-free regression) | unit | `go test ./internal/inference/ -run 'TestSeamGrepGate\|TestROCmMarkerPresence'` | ❌ W0 | ⬜ pending |
| 08-02-01 | 02 | 2 | BSET-02 | T-8-06 / T-8-07 | liveProve composes inference.PollHealth + inference.GenerationProbe + RunningOffloadVerdict (fed liveWeightBytes/liveModelFile), samples gpu_busy DURING the decode (D-07), bounded by proveTimeout; only inference.StatusPass → ProveStatusPass | build+vet | `go build ./cmd/villa/... && go vet ./cmd/villa/...` | ❌ W0 | ⬜ pending |
| 08-02-02 | 02 | 2 | BSET-01, BSET-03 | T-8-08 / T-8-09 / T-8-11 | `villa backend set/show`; `--dry-run` previews without mutating config/units; Result→exit mapping; live Deps wired to traversal-guarded seams; backend.go literal-free | unit | `go test ./cmd/villa/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/backendswap/backendswap_test.go` — table-driven tests for capture/prove/rollback over injected `Deps` seams (BSET-01, BSET-02)
- [ ] `internal/inference/seam_test.go` — `TestSeamGrepGate` extended to walk `cmd/villa` (committed literal-free regression for the cmd tier)
- [ ] `cmd/villa/backend_test.go` — `villa backend show` / `set` / `--dry-run` command tests (BSET-01, BSET-03)
- [ ] Shared fakes for `orchestrate`, the exported `inference.PollHealth`/`inference.GenerationProbe` probes, `preflight.RunROCm`, and `detect.GPUBusyPercent` (mirror existing `cmd/villa/*_test.go` fake patterns)

*Existing `go test` infrastructure covers the framework; new test files above stub the new package + command surface. The exported `inference.PollHealth`/`inference.GenerationProbe` wrappers (08-01-03) are thin delegations over the already-tested private `pollHealth`/`chatProbe`.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ROCm offload residency (`ROCm0` buffer line, gpu_busy>0, no `Memory access fault`) | BSET-02 | Requires gfx1151 Strix Halo hardware + ROCm 7.2.4 image | On hardware: `villa backend set rocm`, confirm cutover succeeds and `villa backend show` reports rocm with offloaded N/N layers |
| `load_tensors` hang → bounded-timeout → auto-rollback | BSET-02 | Hang only reproduces against a real degraded ROCm bring-up | On hardware: induce a known-bad ROCm config, run `villa backend set rocm`, confirm stack rolls back verbatim within timeout and inference unit re-readies |
| Silent CPU fallback detection via live `detect.GPUBusyPercent()` | BSET-02 | Live sysfs gpu_busy read only meaningful during real generation | On hardware: confirm a CPU-fallback bring-up is classified FAIL and rolled back |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** signed-off (reconciled to final task IDs 08-01-01/02/03, 08-02-01/02 across waves 1–2)
</content>
