---
phase: 18
slug: memory-spine-config-core-embeddings-wiring-research-spike
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-09
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`, table-driven + golden — the only test framework in repo) |
| **Config file** | none — Go modules; tests live beside source as `*_test.go` |
| **Quick run command** | `go test ./internal/memory/ ./internal/config/` |
| **Full suite command** | `make check` (go vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/memory/ ./internal/config/`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** `make check` must be green (includes `TestSeamGrepGate`)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 18-01-01 | 01 | 1 | INFRA-04 | T-18-01 | New `memory_*` fields default to `memory_enabled=false`; absent file → typed defaults | unit | `go test ./internal/config/ -run TestLoadVilla` | ❌ W0 | ⬜ pending |
| 18-01-02 | 01 | 1 | INFRA-04 | T-18-01 | Existing v1.2 config (no memory keys) loads byte-identical; non-opted-in install never rewritten | unit/golden | `go test ./internal/config/ -run TestVilla` | ❌ W0 | ⬜ pending |
| 18-01-03 | 01 | 1 | INFRA-04 | T-18-02 | `normalizeVilla` self-heals zeroed memory fields from `defaultConfig()` single source; never widens a bind | unit | `go test ./internal/config/ -run TestNormalize` | ❌ W0 | ⬜ pending |
| 18-02-01 | 02 | 2 | INFRA-04 | — | `memory.Footprint(model)` returns `detect.Bytes` (typed-Unknown on catalog miss, never bare 0) | unit | `go test ./internal/memory/ -run TestFootprint` | ❌ W0 | ⬜ pending |
| 18-02-02 | 02 | 2 | INFRA-04 | T-18-03 | Enablement/fields-valid gate is fail-closed: invalid/missing fields → typed refuse-with-reason, not silent default | unit | `go test ./internal/memory/ -run TestDecide` | ❌ W0 | ⬜ pending |
| 18-02-03 | 02 | 2 | INFRA-04 | — | RenderView carries resolved values only — no image literal; `TestSeamGrepGate` stays green over `internal/memory` | unit/grep | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ | ⬜ pending |
| 18-01-03 | 01 | 1 | INFRA-04 | — | Spike decisions (D-07/D-08/D-09) recorded with evidence in 18-DECISIONS.md; ENABLE_QDRANT_MULTITENANCY_MODE marked "choice pending — Phase 20" | manual | n/a (doc review) | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/memory/footprint_test.go` — stubs for INFRA-04 footprint math (typed-Unknown on miss)
- [ ] `internal/memory/decide_test.go` — stubs for the fail-closed enablement/fields-valid gate
- [ ] `internal/config/villaconfig_test.go` — extend existing tests for new `memory_*` fields + byte-identical / self-heal invariants
- [ ] No framework install needed — `go test` already in use

*Existing `internal/inference/seam_test.go::TestSeamGrepGate` already covers the no-leaked-literal invariant — `internal/memory` must keep it green (no new test needed, but it must run).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Spike decision currency (OWUI env keys vs pinned digest; `villa-embed` `/v1/embeddings` feasibility; embedding footprint) | INFRA-04 (SC#3) | Verification of external/version-sensitive facts, not code behavior | Review `18-RESEARCH.md` "Spike Decisions (PINNED)" — confirm `ENABLE_PERSISTENT_CONFIG=False` mandatory, 768-dim pinned, ~512 MiB reservation, sources cited |

*Pure-core decisions (footprint, gate, byte-identical config) all have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
