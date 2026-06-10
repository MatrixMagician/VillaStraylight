---
phase: 23
slug: surfacing-backup-memory-aware-swap
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-10
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + golden fixtures, no third-party assert) |
| **Config file** | none — `Makefile` targets exist |
| **Quick run command** | `go test ./internal/status ./internal/backup ./internal/modelswap ./internal/recall ./cmd/villa` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command (affected packages)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | CTRL-02/04/05 | | | unit/golden | `go test ./...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `internal/status`,
`internal/backup`, `internal/modelswap`, `internal/recall`, and `cmd/villa`
already have table-driven test files and golden fixtures (`testdata/*.golden*`)
that the new tests extend. Golden re-freezes happen intentionally with
`go test … -update` exactly once per evolved contract.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live status/dashboard memory rows on real stack | CTRL-02 | Needs running villa-qdrant/villa-embed + dashboard service | `make build`, restart `villa-dashboard.service`, check `villa status --json` + dashboard panel on the Strix Halo box |
| Real backup/restore of populated Qdrant volume | CTRL-04 | Needs a populated volume (post-Phase-21 index) + podman | `villa backup` → `villa restore` drill; verify clean-recreate + skew WARN path |
| Embedding-dimension swap drill | CTRL-05 | Needs live recall state + OWUI KB | Edit config embedding model, observe `recall index` refusal + remediation; `--rebuild` path |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
