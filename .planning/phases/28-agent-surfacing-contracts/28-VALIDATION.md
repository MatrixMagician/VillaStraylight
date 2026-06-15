---
phase: 28
slug: agent-surfacing-contracts
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-15
reconstructed: 2026-06-15
---

> **Reconstructed (State B):** Phase 28 executed without a VALIDATION.md (test-first was folded into every plan — 28-01/02/03 each wrote tests before/with implementation). This contract was reconstructed from the three SUMMARYs + VERIFICATION on 2026-06-15: every phase requirement has a mapped automated test that exists on disk and runs green; the only manual-only item is the dashboard Agent **panel's visual rendering** (no JS test toolchain — the SPA is a no-build `go:embed` asset, per CLAUDE.md). `nyquist_compliant: true`, `wave_0_complete: true`.

# Phase 28 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Reconstructed retroactively from phase artifacts (SUMMARY 28-01/02/03, 28-VERIFICATION.md).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven, `httptest`, byte-frozen golden fixtures) — the only framework (CLAUDE.md) |
| **Config file** | none (Go toolchain) |
| **Quick run command** | `go test ./internal/status/ ./internal/doctor/ ./internal/backup/ ./internal/metrics/ -count=1` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/status/ ./internal/doctor/ ./internal/backup/ ./internal/metrics/ ./cmd/villa/ -count=1`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite green + golden append-only diff confirmed (status `schema_version` 3→4 single-line diff) + dashboard panel inspected on a live box (manual)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Req ID | Plan | Behavior | Test Type | Automated Command | Status |
|--------|------|----------|-----------|-------------------|--------|
| SURF-02 | 28-01 | doctor folds agent tool-call + under-load residency; confident agent FAIL DOMINATES a healthy HTTP-200 (D-07) | unit | `go test ./internal/doctor/ -run 'TestAgentToolCallFolded|TestAgentResidencyFolded' -count=1` | ✅ green |
| SURF-02 | 28-01 | agent binary/version + config drift mappers; ConfigAbsent-alone → no finding; WARN-only never auto-corrected (D-14) | unit | `go test ./internal/doctor/ -run 'TestAgentDriftFindingsMatrix|TestAgentCleanDriftPasses' -count=1` | ✅ green |
| SURF-02 | 28-01 | agent seams nil-safe + gated on `cfg.AgentEnabled`; doctor schema 1→2 append-only; agent-off byte-identical | unit+golden | `go test ./cmd/villa/ -run 'TestLiveDoctorDepsWiresAgentSeams|TestDoctorAgentRender' -count=1` | ✅ green (`doctor-agent.json.golden`) |
| SURF-03 | 28-02 | backup adds crush.json + identity-only `ExcludedAgent` (binary bytes excluded); agent-off layout-identical | unit | `go test ./internal/backup/ -run 'TestBackupAgentOn|TestBackupAgentOff|TestExcludedAgentHasNoContentFields|TestManifestExcludedAgent' -count=1` | ✅ green |
| SURF-03 | 28-02 | restore re-stages crush.json with fail-closed identity verify; drift → Refused; agent-off no-op | unit | `go test ./internal/backup/ -run 'TestRestoreAgent' -count=1` | ✅ green |
| USAGE-04 | 28-02 | `cache_n`/`prompt_n` typed-Unknown counter primitive (no fabricated 0; oversized body refuses) | unit | `go test ./internal/metrics/ -run 'TestScrapeCacheCounters|TestCacheSampleRejectsNonFinite' -count=1` | ✅ green |
| USAGE-04 | 28-03 | cache pct surfaced ONLY when both-Known AND prompt_n>0 — never a fabricated 0% | unit | `go test ./internal/status/ -run TestRunCodingCacheGate -count=1` | ✅ green |
| USAGE-03 | 28-03 | per-model agent token usage keyed on the coder model id via the v1.2 usage core | unit | `go test ./internal/status/ -run TestRunCodingSection -count=1` | ✅ green |
| SURF-01 | 28-03 | `status.Report` `Coding *CodingInfo` block (enabled/version/pin_match tri-state/model/mode/residency/cache); `reportSchemaVersion` 3→4 EXACTLY ONCE; residency derived (typed-Unknown when unevaluable); pin tri-state | unit | `go test ./internal/status/ -run 'TestRunCodingOffReport|TestRunCodingPinTriState|TestRunCodingResidencyTypedUnknown' -count=1` | ✅ green |
| SURF-01 | 28-03 | coding-off golden byte-identical except `schema_version` 3→4; coding-on fixture carries full block | golden | `go test ./cmd/villa/ -run 'TestStatus|TestStatusJSONGoldenCodingOn' -count=1` | ✅ green (`status.json.golden` 1-line diff; `status-coding.json.golden`) |
| SURF-01 | 28-03 | dashboard `/api/status` passthrough carries `schema_version` 4 (single-bump propagation) | unit | `go test ./internal/dashboard/ -count=1` | ✅ green (`api_test.go`) |
| SURF-01 | 28-03 | dashboard Agent panel **visual** (hidden-until-data, tri-state pin badge, residency/usage/cache rows) | manual | — (no JS test toolchain; see Manual-Only) | ⬜ manual |
| all | all | seam-gate green (no leaked backend/image/host literal in cmd/villa or surfacing cores) | unit | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ green |

*Status: ⬜ pending/manual · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

*Existing infrastructure covers all phase requirements.* No new framework — the Go testing toolchain (table-driven + `httptest` + golden fixtures) covers every phase-28 requirement. All tests were written test-first during execution (28-01/02/03, each plan's Task 1 TDD) and confirmed green by the 2026-06-15 reconstruction audit. No dangling MISSING reference.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dashboard Agent panel renders correctly (appears only when `report.coding` present; version/model/mode/residency rows; tri-state policy-pin badge match→green / mismatch→amber / unknown→gray; per-model coder usage or honest "No usage recorded yet"; cache pct+raw or gray Unknown badge) | SURF-01 | The dashboard SPA is a no-build `go:embed` asset — there is **no JS test toolchain/bundler** in the `villa` path (CLAUDE.md). The data contract feeding the panel IS automated (status golden + `api_test.go` schema passthrough); only the rendered visual is manual. Also subject to the dashboard-binary trap. | On a live box with the agent enabled: `make build` → `systemctl --user restart villa-dashboard.service` → open the dashboard → confirm the Agent panel appears with the version/pin/model/mode/residency rows, per-model coder usage, and the cache-effectiveness row (pct+raw or gray Unknown badge); with the agent OFF, confirm the panel stays hidden. (Recorded in 28-VERIFICATION Human-Verification + 28-03 GOTCHA.) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (test-first folded into every plan; no Wave 0 needed)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — existing infra covers all requirements)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-06-15 (reconstructed State B)

---

## Validation Audit 2026-06-15

State B reconstruction of a completed, VERIFICATION-passed phase (28-01/02/03; VERIFICATION 5/5). Every mapped requirement test confirmed to exist on disk and run green across all 7 affected packages. The single manual-only item (dashboard panel visual) is an architectural constraint (no JS toolchain), not a coverage gap. No auditor spawn needed (zero MISSING gaps).

| Metric | Count |
|--------|-------|
| Requirements mapped | 5 (SURF-01, SURF-02, SURF-03, USAGE-03, USAGE-04) |
| Gaps found | 0 |
| Resolved | 0 (none needed — tests pre-existing, written test-first during execution) |
| Escalated | 0 |
| Manual-only | 1 (dashboard panel visual — no JS test toolchain) |

**Commands re-run green (2026-06-15):**
- `go test ./internal/doctor/ -run 'TestAgent|TestResidency|TestDrift' -count=1` → ok
- `go test ./internal/backup/ -run 'TestBackupAgent|TestRestoreAgent|TestExcludedAgent' -count=1` → ok
- `go test ./internal/metrics/ -run 'TestScrapeCache|TestCacheSample|TestCounter' -count=1` → ok
- `go test ./internal/status/ -run TestRunCoding -count=1` → ok
- `go test ./internal/dashboard/ -count=1` → ok
- `go test ./cmd/villa/ -run 'TestDoctor|TestStatus|TestBackup|TestRestore' -count=1` → ok
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` → ok

**Verdict:** Phase 28 is NYQUIST-COMPLIANT. No test files generated by this audit (all coverage pre-existing).
