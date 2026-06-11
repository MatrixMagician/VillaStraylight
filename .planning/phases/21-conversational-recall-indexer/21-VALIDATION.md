---
phase: 21
slug: conversational-recall-indexer
status: ready
nyquist_compliant: true
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
| store clone (recall-state.json) | 21-01 T1 | 1 | RECALL-03 | T-21-01/02/03 | fail-closed load; 0600 atomic write; traversal guard; content denylist | unit (tdd) | `go test ./internal/recall/ -run 'TestStore' -count=1` | ❌ created in-task (test-first) | ⬜ pending |
| Plan diff + RenderTranscript | 21-01 T2 | 1 | RECALL-01 | T-21-05 | cycle guard; reasoning strip; skip-not-drop; no input mutation | unit (tdd) | `go test ./internal/recall/ -run 'TestPlan\|TestRenderTranscript\|TestTranscriptFilename' -count=1` | ❌ created in-task (test-first) | ⬜ pending |
| Staleness Classify + seam gate | 21-01 T3 | 1 | RECALL-03 | T-21-04 | Unknown ≠ 0; partial-run distinguishable; no literals in internal/recall | unit (tdd) + existing gate | `go test ./internal/recall/ -run 'TestStaleness' -count=1 && go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ❌ / ✅ gate exists | ⬜ pending |
| live REST seam (recall_live.go) | 21-02 T1 | 2 | RECALL-01, RECALL-02 | T-21-06/11/13 | fixed-arg curl; stdin multipart; admin list endpoint; reset-not-delete; no image literals | build + existing tests + gate | `go build ./cmd/villa/... && go vet ./cmd/villa/... && go test ./cmd/villa/ -count=1 && go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ (verify-memory tests guard the refactor) | ⬜ pending |
| recall verbs + run bodies | 21-02 T2 | 2 | RECALL-01, RECALL-02, RECALL-03 | T-21-07/09/10 | memory-off ⇒ exitBlocked; incremental persist; delete-before-re-add; read-merge-write attach; Unknown rendering | unit (tdd, fake Deps) | `go test ./cmd/villa/ -run 'TestRecall' -count=1` | ❌ created in-task (test-first) | ⬜ pending |
| docs/MEMORY.md recall section | 21-02 T3 | 2 | RECALL-02 (enable-path doc) | T-21-07 (scope disclosure) | widened-admin-read documented; swap-gotcha documented | content check + full suite | `grep -q 'villa recall' docs/MEMORY.md && grep -q 'model swap' docs/MEMORY.md && make check` | ✅ doc exists (append) | ⬜ pending |
| live bulk index + A1–A4 | 21-03 T1 | 3 | RECALL-01 | T-21-15/16/17 | 0600 content-free state file on the real artifact; tuning keeps tests green | on-hardware auto | `./villa recall status && stat -c '%a' …/recall-state.json = 600 && go test ./cmd/villa/ -run 'TestRecall' -count=1` | ✅ (built by waves 1-2) | ⬜ pending |
| semantic retrieval (SC#2) | 21-03 T2 | 3 | RECALL-02 | T-21-14 | paraphrase + sources citation + negative control | on-hardware human-check | checkpoint (REST drive + UI confirm) | n/a | ⬜ pending |
| incremental/staleness drill (SC#3) | 21-03 T3 | 3 | RECALL-03 | T-21-09 | edit/delete/OWUI-down honesty; Unknown ≠ 0 live | on-hardware human-check | checkpoint (Claude-driven CLI + UI actions) | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `go test` + the established
fake-Deps seam pattern need no new framework. The RESEARCH "Wave 0 Gaps" test files
(`internal/recall/*_test.go`, `cmd/villa/recall_test.go`) are created test-first INSIDE
their TDD tasks (21-01 T1–T3, 21-02 T2), so no separate Wave 0 plan is needed; the
existing `TestSeamGrepGate` covers the literal-free constraint from day one.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Semantic retrieval of past-chat content in a NEW chat (SC#2) | RECALL-02 | Needs the live OWUI + villa-embed + Qdrant stack on the gfx1151 box with real chats; "by meaning" is a behavioral quality bar | 21-03 Task 2 checkpoint: paraphrase question, `sources` citation, never-discussed negative control, REST + UI |
| Bulk-index throughput/timeouts (A1–A2), attach meta-merge (A3), completion pollution (A4) | RECALL-01 | Hardware/runtime-dependent | 21-03 Task 1: real index run with recorded timings + before/after diffs |
| Incremental edit/delete/OWUI-down honesty (SC#3) | RECALL-03 | Requires real UI chat edits/deletes + service stop/start | 21-03 Task 3 checkpoint drill |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (TDD tasks create their tests first; checkpoints carry `<human-check>`)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (folded into TDD tasks)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner sign-off 2026-06-10 (plans 21-01..03)
