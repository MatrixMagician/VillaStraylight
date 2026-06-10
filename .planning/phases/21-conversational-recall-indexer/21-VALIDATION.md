---
phase: 21
slug: conversational-recall-indexer
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-10
---

# Phase 21 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + fake Deps seams — no third-party assert/mock) |
| **Config file** | none — `Makefile` targets exist |
| **Quick run command** | `go test ./internal/recall/... ./cmd/villa/...` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/recall/... ./cmd/villa/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner from PLAN.md task breakdown) | | | RECALL-01/02/03 | | | unit | `go test ./...` | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `go test` + the established
fake-Deps seam pattern (`fake*Deps` doubles, injected probes per `cmd/villa/verify_memory_test.go`)
need no new framework. The planner fills the per-task map; the pure `internal/recall`
core must be fully unit-testable off-hardware with injected chat lists/state.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Semantic retrieval of past-chat content in a NEW chat (SC#2) | RECALL-02 | Needs the live OWUI + villa-embed + Qdrant stack on the gfx1151 box with real chats | On-hardware wave: run `villa recall index`, open a new chat, ask about a fact only present in an old chat, confirm retrieval-by-meaning (not keyword); detach/delete checks per RESEARCH.md Validation Architecture |
| Bulk-index throughput/timeouts (A1–A4) | RECALL-01 | Embed throughput on gfx1151 is hardware-dependent | On-hardware wave: index the real chat history; record durations; tune poll timeouts |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
