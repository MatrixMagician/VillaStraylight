---
phase: 22
slug: control-plane-fit-host-gate
status: planned
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-10
---

# Phase 22 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library; table-driven + golden fixtures) |
| **Config file** | none — Makefile targets (`make test`, `make check`) |
| **Quick run command** | `go test ./internal/recommend/ ./internal/preflight/ ./internal/doctor/ ./internal/memory/` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/recommend/ ./internal/preflight/ ./internal/doctor/ ./internal/memory/` (plus `./cmd/villa/` when cmd-tier files change)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 22-01-T1 | 22-01 | 1 | CTRL-01 | T-22-01/02 | conservative reservation on typed-Unknown (never 0); no uint64 wrap | unit (table) | `go test ./internal/recommend/ ./internal/memory/ -count=1` | ✅ recommend_test.go / footprint_test.go | ⬜ pending |
| 22-01-T2 | 22-01 | 1 | CTRL-01 | T-22-03/04 | memory-aware install pick; single targeted golden re-freeze | golden + full suite | `go test ./cmd/villa/ -count=1 && make check` | ✅ cmd/villa goldens | ⬜ pending |
| 22-02-T1 | 22-02 | 2 | CTRL-06 | T-22-05/06/07 | fixed-arg podman resolver; BLOCK on confident shortage, WARN on Unknown, remediation non-empty | unit (table) | `go test ./internal/preflight/ -count=1` | ❌→created in-plan (checks_memory_test.go) | ⬜ pending |
| 22-02-T2 | 22-02 | 2 | CTRL-06 | T-22-08 | fail-soft config gate; off-path byte-identical (frozen render goldens) | golden + full suite | `go test ./cmd/villa/ -count=1 && make check` | ✅ preflight goldens | ⬜ pending |
| 22-03-T1 | 22-03 | 3 | CTRL-03 | T-22-10 | proof FAIL/WARN/PASS semantics; offload down-rank only on Status==WARN (FAIL never suppressed) | unit (fake Deps) | `go test ./internal/doctor/ -count=1` | ✅ doctor_test.go | ⬜ pending |
| 22-03-T2 | 22-03 | 3 | CTRL-03 | T-22-09/11/12/13 | fixed-arg bounded embed drive; no state mutation; seam gate green | golden + grep gate + full suite | `go test ./cmd/villa/ -count=1 && go test ./internal/inference/ -run TestSeamGrepGate -count=1 && make check` | ✅ doctor render goldens (+ new additive fixtures) | ⬜ pending |
| 22-04-T1 | 22-04 | 4 | CTRL-01/03/06 | T-22-14/15/16 | on-hardware SC#1-3 + D-05 measurement + negative controls (manual-only, justified) | manual + full suite | `make check` (+ manual checklist) | n/a | ⬜ pending |
| 22-04-T2 | 22-04 | 4 | CTRL-01/03/06 | T-22-15 | blocking operator sign-off | manual (checkpoint) | — | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `internal/recommend`,
`internal/preflight`, `internal/doctor`, `internal/memory`, and `cmd/villa`
all have established `_test.go` suites, fake-Deps doubles, and golden-fixture
machinery (`-update` flag pattern). No new framework or harness needed.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Doctor residency proof under real embedding load | CTRL-03 | Needs the live gfx1151 box: real villa-llama + villa-embed, real GTT scrape under `/v1/embeddings` load | On the dev box with the stack up: `./villa doctor` (and `--json`); assert chat-model residency finding is PASS while embed requests run; confirm typed-Unknown WARN (not PASS) when stack is down |
| Footprint measurement (D-05) | CTRL-01 | On-hardware cgroup/GTT observation | Record villa-embed MemoryCurrent/MemoryPeak under load in the phase summary; raise the 512 MiB constant only if it under-reserves |
| Preflight vector-disk gate on real volume root | CTRL-06 | statfs target is the live rootless podman volume path | `./villa preflight` with memory on: disk/headroom rows present with remediation; with `memory_enabled=false`: rows absent (byte-identical output) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
