---
phase: 25
slug: coding-mode-render-transactional-swap-verb
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-13
validated: 2026-06-15
---

# Phase 25 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven; `httptest`; byte-for-byte golden fixtures). No third-party assertion/mock lib — seams are injected `func` fields. |
| **Config file** | none (Go convention) |
| **Quick run command** | `go test ./internal/codingmode/... ./internal/inference/... ./internal/orchestrate/... ./internal/config/...` |
| **Full suite command** | `make check` (`go vet ./...` + `go test ./...`) |
| **Estimated runtime** | ~30 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/codingmode/... ./internal/inference/... ./internal/orchestrate/... ./internal/config/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full `make check` green (incl. `TestSeamGrepGate`, all goldens); plus an on-hardware enter→prove→exit smoke on the gfx1151 box (swap residency, real generation + residency proof) as the acceptance checkpoint
- **Max feedback latency:** ~30 seconds (automated suite)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 25-01-03 | 01 | 1 | CMODE-01 | — | Addon-OFF render byte-identical to v1.3 goldens | unit/golden | `go test ./internal/orchestrate/ -run 'TestRenderContainerGolden\|TestRenderCodingModeOffPathUnchanged'` | ✅ off-path goldens + `TestRenderCodingModeOffPathUnchanged` | ✅ green |
| 25-01-02 | 01 | 1 | CMODE-01 | T-25 seam-leak | Seam grep-gate green (no `--jinja`/`--cache-reuse`/sampling leak outside `internal/inference`) | unit | `go test ./internal/inference/ -run 'TestSeamGrepGate\|TestSeamGateForbidsCodingFlagsInCmdFixture'` | ✅ gate extended (`codingModeFlagPattern` in both `patterns`+`cmdPatterns`) | ✅ green |
| 25-01-03 | 01 | 1 | CMODE-01 | — | Addon-ON renders `--jinja` + `-c <agent_ctx>` + sampling; `--cache-reuse` only when `cache_reuse_safe` | unit/golden | `go test ./internal/inference/ -run TestCodingModeArgs`; `go test ./internal/orchestrate/ -run TestRenderCodingMode` (`villa-llama-coding.container.golden`) | ✅ `containerargs_coding_test.go` + new golden | ✅ green |
| 25-01-02 | 01 | 1 | CMODE-01 | T-25 capability-drift | `--cache-reuse` absent when `cache_reuse_safe=false` (fail-closed) | unit | `go test ./internal/inference/ -run TestCacheReuseGate`; `go test ./internal/orchestrate/ -run TestRenderCodingModeFailClosedCacheReuse` | ✅ both backends + render | ✅ green |
| 25-01-01 | 01 | 1 | CMODE-01/02 | — | Config off-path byte-identical on disk (no coding keys when off) | unit | `go test ./internal/config/ -run 'TestCodingModeSaveOmitsKeysWhenDisabled\|TestCodingModeNotSelfHealed'` | ✅ `villaconfig_test.go` (extended) | ✅ green |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Enter → prove-pass → coder model served (swap residency) | unit (Deps-driven) | `go test ./internal/codingmode/ -run 'TestEnter\|TestSharedResidencyRenderDeltaOnly'` | ✅ `codingmode_test.go` | ✅ green |
| 25-02-01 | 02 | 2 | CMODE-02 | T-25 false-green | Prove-FAIL (CPU fallback / residency FAIL) → verbatim rollback, prior unit+config restored | unit | `go test ./internal/codingmode/ -run 'TestProveFailRollback\|TestIdleGreenNotSuccess'` | ✅ `codingmode_test.go` | ✅ green |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Mutate error (save/write/restart) → verbatim rollback with honest rollback-incomplete | unit | `go test ./internal/codingmode/ -run 'TestMutateRollback\|TestRollbackIncompleteReported'` | ✅ `codingmode_test.go` | ✅ green |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Exit → chat model restored under same transactional discipline (symmetric) | unit | `go test ./internal/codingmode/ -run 'TestExitRestoresChat\|TestExitProveFailRollback'` | ✅ `codingmode_test.go` | ✅ green |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Same-state enter/exit is a clean NoOp (zero side effects) | unit | `go test ./internal/codingmode/ -run 'TestNoOpEnterAlreadyCoding\|TestNoOpExitAlreadyChat'` | ✅ `codingmode_test.go` | ✅ green |
| 25-02-02 | 02 | 2 | CMODE-02 | T-25 unintended-mode-change | Mode never auto-flips (explicit verb only) | structural | `go test ./cmd/villa/ -run TestNoAutoFlipStructuralGuard` (walks `cmd/villa`+`internal/`; bool-literal anchored) | ✅ `coding-mode_test.go` | ✅ green |

*Task IDs bound to concrete `NN-NN-NN` (plan 25-01 = Wave 1 / CMODE-01; plan 25-02 = Wave 2 / CMODE-02).*
*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] `internal/codingmode/codingmode_test.go` — covers CMODE-02 (enter/exit/rollback/NoOp), clone of `backendswap_test.go` — **15 Deps-driven tests** (incl. idle-green-not-success, capture-failure, rollback-incomplete, fit-guard refusal, shared-residency)
- [x] `internal/inference/containerargs_coding_test.go` — covers CMODE-01 on-path flags + `cache_reuse_safe` gate (both backends) + cmd-tier seam-leak fixture
- [x] `internal/orchestrate/testdata/villa-llama-coding.container.golden` — NEW append-only on-path golden (CMODE-01); no existing golden mutated
- [x] `internal/config/villaconfig_test.go` — marshal-omit-when-off extension (`TestCodingModeSaveOmitsKeysWhenDisabled` + `TestCodingModeNotSelfHealed`)
- [x] Structural guard: `TestNoAutoFlipStructuralGuard` — no `CodingMode` bool toggle mutated outside `codingmode.Run`
- [x] Framework install: none — `go test` is already the suite

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Enter→prove→exit on real hardware (swap residency, real generation + residency proof) | CMODE-02 | Requires the live gfx1151 box + pinned toolbox image + a downloaded coder GGUF; the under-load residency proof can only be exercised against a running llama-server | On the dev box: `villa coding-mode enter` → confirm coder served + residency PASS under load → `villa coding-mode exit` → confirm chat model restored. A forced CPU-fallback must FAIL the prove and roll back. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references — all Wave-0 tests written during execution
- [x] No watch-mode flags
- [x] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-06-15 (all 11 map rows green; on-hardware CMODE-02 acceptance PASSED, recorded in 25-02-SUMMARY)

---

## Validation Audit 2026-06-15

State A audit of the planning-time VALIDATION contract against the executed phase. Every
Wave-0 test was authored during execution (plans 25-01 / 25-02), so no `gsd-nyquist-auditor`
spawn was required. All 11 per-task map rows verified green; suggested test names in the
original map were reconciled to the actual implemented test function names.

| Metric | Count |
|--------|-------|
| Gaps found | 0 |
| Resolved | 0 (all covered at execution time) |
| Escalated | 0 |
| Map rows verified green | 11 / 11 |
| Manual-only items | 1 (on-hardware enter→prove→exit — PASSED, see 25-02-SUMMARY) |

**Verification commands run (all green):**
`go test ./internal/codingmode/... ./internal/inference/... ./internal/orchestrate/... ./internal/config/... ./cmd/villa/...`

Notable coverage beyond the original plan: `TestIdleGreenNotSuccess` (idle WARN ≠ false PASS),
`TestRollbackIncompleteReported` (honest rollback-incomplete), `TestCaptureFailureRefuses`,
`TestRefuseFitGuard`, `TestSeamGateForbidsCodingFlagsInCmdFixture` (cmd-tier seam-leak fixture).
