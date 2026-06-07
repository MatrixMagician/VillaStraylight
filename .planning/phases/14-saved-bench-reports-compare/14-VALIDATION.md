---
phase: 14
slug: saved-bench-reports-compare
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-07
---

# Phase 14 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`; table-driven + byte-for-byte golden fixtures) |
| **Config file** | none — `go test` is config-free; goldens live under `internal/benchstore/testdata/` and `cmd/villa/testdata/` |
| **Quick run command** | `go test ./internal/benchstore/... ./cmd/villa/...` |
| **Full suite command** | `make check` (go vet + `go test ./...`) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/benchstore/... ./cmd/villa/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite (`make check`) must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 14-01-01 | 01 | 1 | BENCH-03 | — | On-disk JSONL record format frozen with `schema_version=1` from day one; golden refuses silent drift | golden | `go test ./internal/benchstore/...` | ❌ W0 | ⬜ pending |
| 14-01-02 | 01 | 1 | BENCH-03 | T-14-01 | XDG path write confined under `$XDG_DATA_HOME/villa/`; 0600 file / 0700 dir; path-traversal guard | unit | `go test ./internal/benchstore/...` | ❌ W0 | ⬜ pending |
| 14-02-01 | 02 | 2 | BENCH-03 | — | `villa bench` persists a report after a run; pp and tg tok/s stored separately (never blended); VoidExhausted/Reason round-trips | unit | `go test ./cmd/villa/... ./internal/benchstore/...` | ❌ W0 | ⬜ pending |
| 14-03-01 | 03 | 3 | BENCH-04 | — | `villa bench --compare` is read-only, lists/views saved reports, prints pp/tg deltas kept structurally separate | unit | `go test ./cmd/villa/...` | ❌ W0 | ⬜ pending |
| 14-03-02 | 03 | 3 | BENCH-04 | T-14-02 | Comparability guard refuses deltas across mismatched model/quant/host fingerprint; UNKNOWN host fact → "not comparable" (never false-equal) | unit | `go test ./internal/benchstore/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · IDs illustrative — planner assigns canonical task IDs*

---

## Wave 0 Requirements

- [ ] `internal/benchstore/testdata/record.golden` — frozen `schema_version=1` saved-report record (BENCH-03 contract)
- [ ] `internal/benchstore/store_test.go` — round-trip, XDG path safety, fingerprint comparability table tests
- [ ] `cmd/villa/testdata/bench-compare.json.golden` — frozen `--compare` output (pp/tg deltas + "not comparable" label)
- [ ] No framework install needed — `go test` already in use repo-wide

*Existing `testing` infrastructure covers all phase requirements; only new fixtures/test files are added.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| First real saved reports prove Phase-12 Δtg recovery (cross-backend compare) | BENCH-04 | Requires live `villa bench --ab-target` runs on gfx1151 hardware producing actual pp/tg numbers | On the Strix Halo host, run two `villa bench` runs (vulkan + rocm), then `villa bench --compare` and confirm pp/tg deltas print and same-model cross-backend pair is comparable |

*All format/guard/round-trip behaviors have automated verification; only the on-hardware proof is manual (UAT).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
