---
phase: 34
slug: web-search-surfacing-lands-last
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-21
---

# Phase 34 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + golden fixtures) |
| **Config file** | none — toolchain built in (`go.mod`, Go 1.26) |
| **Quick run command** | `go test ./internal/status/ ./internal/backup/ ./internal/doctor/` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------------|-----------|-------------------|-------------|--------|
| T-34-01 | 34-01 | 0 | SURF-04 | verify-state fail-closed Load (absent/corrupt/future-schema → empty State, never fabricated PASS); atomic 0600 Save | unit | `go test ./internal/verifystate -run TestStore -v` | ✅ `internal/verifystate/store_test.go` | ✅ green |
| T-34-02 | 34-01 | 0 | SURF-04 | `villa verify search` persists verdict+timestamp; persist failure never changes exit code; nil seam safe | unit | `go test ./cmd/villa -run TestVerifySearchPersists -v` | ✅ `cmd/villa/verify_search_test.go` | ✅ green |
| T-34-03 | 34-01/03 | 0 | SURF-04 | status `web_search` block 4→5 append-only; outbound-bounded derives from cached verify (PASS+fresh→bounded, non-PASS→not-bounded, stale/future/absent→unknown), NEVER cfg bool | unit + golden | `go test ./internal/status -run TestWebSearchOutboundBounded -v` | ✅ `internal/status/status_test.go` (T-34-08, 9 subtests incl. future-dated) | ✅ green |
| T-34-04 | 34-03 | 0 | SURF-04 | web-OFF status output byte-identical to v4 except schema_version; dedicated searxng/websafe seams (not generic Health) | golden + unit | `go test ./cmd/villa -run TestRunWebSearch ./internal/status -run TestRunSearxngWebsafeRows` | ✅ `cmd/villa/testdata/status.json.golden`, `internal/status/status_test.go` | ✅ green |
| T-34-05 | 34-04 | 0 | SURF-06 | doctor web-search fold on own schema 2→3; egress-proof tri-state (ready/degraded/typed-Unknown); freshness incl. future-dated re-prove | unit | `go test ./internal/doctor -run 'TestSearchEgressFinding\|TestDoctorSchemaVersionIsThree' ./cmd/villa -run TestLiveSearchEgressProofFreshness` | ✅ `internal/doctor/doctor_test.go`, `cmd/villa/doctor_test.go` | ✅ green |
| T-34-06 | 34-04 | 0 | SURF-06 | residency-under-search-load offload-asserting (CPU fallback=FAIL, not-in-flight=typed-Unknown); every non-PASS finding carries Remediation | unit | `go test ./internal/doctor -run 'TestSearchResidencyFinding\|TestWebSearchFindingsHaveRemediation' ./cmd/villa -run TestRunSearchResidencyUnderLoadPreconditionGate` | ✅ `internal/doctor/doctor_test.go`, `cmd/villa/doctor_test.go` | ✅ green |
| T-34-07 | 34-02 | 0 | SURF-07 | backup adds settings.yml provenance entry only when web-on + present; FileMissing skip when off/absent (web-OFF schema 3→4 only) | unit | `go test ./internal/backup -run 'TestBackupSearxngSettings\|TestManifestSchemaVersionIsV4'` | ✅ `internal/backup/backup_test.go`, `manifest_test.go` | ✅ green |
| T-34-08 | 34-02 | 0 | SURF-07 | restore re-writes settings.yml forcing 0600 (never widening); tampered→fail-closed; ephemeral web content NEVER archived | unit | `go test ./internal/backup -run 'TestRestoreSearxngSettings\|TestBackupExcludesEphemeral'` | ✅ `internal/backup/restore_test.go`, `backup_test.go` | ✅ green |
| T-34-09 | 34-05 | 0 | SURF-05 | dashboard hidden-until-data Web Search panel; rides existing /api/status poll (no new fetch); web-OFF schema-only delta | golden (status contract) | `make check` (panel data path; visual sign-off → manual) | ✅ `internal/dashboard/assets/dashboard.js` | ✅ green (visual → manual) |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing infrastructure covers all phase requirements (stdlib `go test` + golden fixtures; no framework install needed).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Offload-asserting chat-model GPU-residency under search load | SURF-06 | Requires live gfx1151 host + running stack under a real search workload | On the Strix Halo host: enable web search, drive a search query, run `villa doctor` and confirm the residency-under-load check PASSes (no silent CPU fallback) |
| `villa verify search` egress proof (rootless-netns nft block) | SURF-04 (indicator source) | Requires on-hardware netns + nft | On-hardware `villa verify search` PASS produces the cached result the indicator reads |

*Automated coverage handles the JSON/golden/backup/doctor-schema contracts; the two host-only behaviors above are manual-on-hardware.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (T-34-01..09 each map to a passing test)
- [x] Wave 0 covers all MISSING references (existing stdlib `go test` + golden infra; no install needed)
- [x] No watch-mode flags
- [x] Feedback latency < 60s (quick-run ~60s; individual subtests <1s)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated — SURF-04..07 all carry non-vacuous automated coverage; `make check` green (vet + race, all packages). Only inherently host-only/visual behaviors remain manual (justified below).
