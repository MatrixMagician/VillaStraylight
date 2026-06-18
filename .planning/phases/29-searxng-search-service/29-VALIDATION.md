---
phase: 29
slug: searxng-search-service
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-18
---

# Phase 29 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard `testing`; table-driven + byte-frozen golden fixtures) |
| **Config file** | none — Go toolchain (`go.mod`); `make` targets wrap it |
| **Quick run command** | `go test ./internal/orchestrate/... ./internal/config/...` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/orchestrate/... ./internal/config/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 29-01-01 | 01 | 1 | SRCH-01 | — | settings.yml + Quadlet units render deterministically from config (golden) | unit | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 29-01-02 | 01 | 1 | SRCH-04 | T-29-01 | rendered settings.yml restricts engines to the vetted `keep_only` subset (no full default set) | unit | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 29-01-03 | 01 | 1 | SRCH-01 | — | with web search not configured, render is byte-identical to v1.4 (golden diff = 0) | unit | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |

*The planner expands this map per task; the table above seeds the four success criteria. Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Rendered-unit + settings.yml golden fixtures under `internal/orchestrate/testdata/` — for SRCH-01/SRCH-04
- [ ] Byte-identical-when-disabled golden assertion (extends the existing v1.4 render goldens)

*Existing `go test` infrastructure covers the framework; new golden fixtures are the Wave 0 work.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real `format=json` query returns parseable JSON with ≥1 result | SRCH-01 | Requires the live SearXNG container running on `villa.network` (on-hardware; reachable upstream engines) | On the gfx1151 host: bring the unit up, then run the `runProbeCurl`-based readiness probe against the in-network SearXNG `format=json` endpoint and confirm `results[]` parses with ≥1 entry |

*Readiness is proven by the actual query, never a health-200 — the automated golden tests cover render determinism; the live JSON-query proof is on-hardware.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
