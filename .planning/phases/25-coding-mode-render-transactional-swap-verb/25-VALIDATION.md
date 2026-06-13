---
phase: 25
slug: coding-mode-render-transactional-swap-verb
status: bound
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-13
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
| 25-01-03 | 01 | 1 | CMODE-01 | — | Addon-OFF render byte-identical to v1.3 goldens | unit/golden | `go test ./internal/orchestrate/ -run TestRender` | ✅ existing off-path goldens | ⬜ pending |
| 25-01-02 | 01 | 1 | CMODE-01 | T-25 seam-leak | Seam grep-gate green (no `--jinja`/`--cache-reuse`/sampling leak outside `internal/inference`) | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ existing gate | ⬜ pending |
| 25-01-03 | 01 | 1 | CMODE-01 | — | Addon-ON renders `--jinja` + `-c <agent_ctx>` + sampling; `--cache-reuse` only when `cache_reuse_safe` | unit/golden | `go test ./internal/inference/ -run TestContainerArgs`; new `villa-llama-coding.container.golden` | ❌ W0 | ⬜ pending |
| 25-01-02 | 01 | 1 | CMODE-01 | T-25 capability-drift | `--cache-reuse` absent when `cache_reuse_safe=false` (fail-closed) | unit | `go test ./internal/inference/ -run TestCacheReuseGate` | ❌ W0 | ⬜ pending |
| 25-01-01 | 01 | 1 | CMODE-01/02 | — | Config off-path byte-identical on disk (no coding keys when off) | unit | `go test ./internal/config/ -run TestMarshalOmitWhenOff` | ❌ W0 (extend memory-omit test) | ⬜ pending |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Enter → prove-pass → coder model served (swap residency) | unit (Deps-driven) | `go test ./internal/codingmode/ -run TestEnter` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | CMODE-02 | T-25 false-green | Prove-FAIL (CPU fallback / residency FAIL) → verbatim rollback, prior unit+config restored | unit | `go test ./internal/codingmode/ -run TestProveFailRollback` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Mutate error (save/write/restart) → verbatim rollback with honest rollback-incomplete | unit | `go test ./internal/codingmode/ -run TestMutateRollback` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Exit → chat model restored under same transactional discipline (symmetric) | unit | `go test ./internal/codingmode/ -run TestExitRestoresChat` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | CMODE-02 | — | Same-state enter/exit is a clean NoOp (zero side effects) | unit | `go test ./internal/codingmode/ -run TestNoOp` | ❌ W0 | ⬜ pending |
| 25-02-02 | 02 | 2 | CMODE-02 | T-25 unintended-mode-change | Mode never auto-flips (explicit verb only) | structural | grep/structural: no caller mutates `coding_mode` outside `codingmode.Run` | ❌ W0 | ⬜ pending |

*Task IDs bound to concrete `NN-NN-NN` (plan 25-01 = Wave 1 / CMODE-01; plan 25-02 = Wave 2 / CMODE-02).*
*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/codingmode/codingmode_test.go` — covers CMODE-02 (enter/exit/rollback/NoOp), clone of `backendswap_test.go`
- [ ] `internal/inference/` ContainerArgs coding-mode cases — covers CMODE-01 on-path flags + `cache_reuse_safe` gate
- [ ] `internal/orchestrate/testdata/villa-llama-coding.container.golden` — NEW append-only on-path golden (CMODE-01)
- [ ] `internal/config/` marshal-omit-when-off extension — covers byte-identical-on-disk for coding fields
- [ ] Structural guard: no `coding_mode` mutation outside `codingmode.Run` (explicit-verb-only invariant)
- [ ] Framework install: none — `go test` is already the suite

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Enter→prove→exit on real hardware (swap residency, real generation + residency proof) | CMODE-02 | Requires the live gfx1151 box + pinned toolbox image + a downloaded coder GGUF; the under-load residency proof can only be exercised against a running llama-server | On the dev box: `villa coding-mode enter` → confirm coder served + residency PASS under load → `villa coding-mode exit` → confirm chat model restored. A forced CPU-fallback must FAIL the prove and roll back. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
