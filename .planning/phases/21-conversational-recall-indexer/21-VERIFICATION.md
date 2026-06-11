---
phase: 21-conversational-recall-indexer
verified: 2026-06-10T00:00:00Z
status: passed
score: 3/3 success criteria verified (truth-level 7/7)
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: none
---

# Phase 21: Conversational Recall Indexer Verification Report

**Phase Goal:** A `villa`-orchestrated, strictly-local indexer turns the user's past Open WebUI chat history into a searchable Knowledge collection (chats → Knowledge) so the assistant can recall relevant past conversations by meaning, and the index can be kept current under explicit `villa` control with honest staleness reporting (never silently stale).
**Verified:** 2026-06-10
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | SC#1/RECALL-01 — a `villa` command semantically indexes past conversations into the vector store (chats → Knowledge), entirely locally (no new outbound) | ✓ VERIFIED | `villa recall index` exists, registered in `cmd/villa/root.go:36`, runnable (`/tmp/villa-verify recall --help` shows index/status). Pure diff `recall.Plan` implements the D-05 algebra (`internal/recall/recall.go:40-64`). Pipeline gate→reachability→token→KB→list→Plan→render/upload→attach in `runRecallIndex` (`recall.go:293-518`). On-hardware 21-03 Task 1: operator's real history indexed into KB `23e667e7-…` via OWUI chunk→embed→Qdrant, ~1.3 s/chat, status green; zero new outbound (loopback REST + container-DNS, Phase-20 posture held). |
| 2 | SC#2/RECALL-02 — in a NEW chat, the assistant retrieves relevant past-chat content BY MEANING (semantic, not keyword) | ✓ VERIFIED | Idempotent read-merge-write attach into the served model's `meta.knowledge` (`attachKnowledgeRow` `recall_live.go:410-455`) with foreign-key preservation AND a re-GET verification (WR-04) that fails honestly on a silent detach. On-hardware 21-03 Task 2 (human-approved): NEW chat, zero-keyword paraphrase "What secret name did I give my local AI box project?" → answer **"OBSIDIAN-LYNX"** with visible `sources` citation `villa-recall-14…8bec21.txt`; REST + real UI agree; negative control (never-discussed topic) returned no fabricated content/citation. |
| 3 | SC#3/RECALL-03 — incremental update/re-index under explicit `villa` control with honest indexed-count/last-indexed/staleness; never silently stale | ✓ VERIFIED | `recall.Classify` typed-Unknown logic (`internal/recall/staleness.go:54-87`): unevaluable live list ⇒ `StaleKnown=false` + reason, never stale=0; `CompleteRun` distinguishes partial from clean pass; `last_index_completed_at` stamped ONLY on a reconciled clean pass (`recall.go:498-514`, CR-01 fix). Clean-replace = remove-then-re-add; `--rebuild` uses id-preserving `knowledge/{id}/reset`, never delete (`recall_live.go:17-20`). On-hardware 21-03 Task 3 (human-approved drill): edit→stale=1 changed→old file HTTP 404, new file mapped; delete→removed, file 404; OWUI down→"Unknown — could not evaluate" WARN never stale=0; recovery green; idempotent final no-op 0/0/0. |

**Score:** 3/3 success criteria verified (7/7 plan must-have truths)

### Plan-Level Truths (21-01 / 21-02)

| Truth | Status | Evidence |
|-------|--------|----------|
| Pure Plan diff (D-05 algebra, no I/O) | ✓ VERIFIED | `recall.Plan` (`recall.go:40`); `TestPlan`, `TestPlanDeterministicOrder`, `TestPlanDoesNotMutateInputs` green |
| RenderTranscript chain-walk + cycle guard + reasoning strip + skip-not-drop | ✓ VERIFIED | `transcript.go:54-136`; `TestRenderTranscriptChainWalk/CycleGuard/StripsReasoning/Skips/SkipsNonChatRoles` green |
| Typed-Unknown staleness, partial-run distinguishable | ✓ VERIFIED | `staleness.go:54-87`; `TestStaleness`, `TestStalenessUnknownIsNotZero` green |
| recall-state.json fail-closed Load, atomic 0600/0700 + traversal guard, ids/timestamps only | ✓ VERIFIED | `store.go:111-231`; `TestStoreLoadFailsClosed`, `TestStoreWriteFileAtomic`, `TestStoreStateHasNoContentKeys` (banned title/content/message keys), `TestStoreRecallStatePathXDG` green. On-hw: real file mode 0600, content-free |
| Memory-off ⇒ both verbs exitBlocked (never honest no-op) | ✓ VERIFIED | `recallGate` (`recall.go:209-221`); `TestRecallGate` green |
| TestSeamGrepGate stays green (no os/exec or image/backend literal in internal/recall) | ✓ VERIFIED | `go test ./internal/inference/ -run TestSeamGrepGate` → 1 passed |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/recall/recall.go` | Plan diff (D-05) | ✓ VERIFIED | `func Plan` real algebra, sorted output, no mutation |
| `internal/recall/transcript.go` | RenderTranscript chain-walk | ✓ VERIFIED | cycle guard + reasoning strip + skip-not-drop |
| `internal/recall/staleness.go` | Classify typed-Unknown | ✓ VERIFIED | Unknown ≠ 0, CompleteRun, attachment fold |
| `internal/recall/store.go` | fail-closed Load + atomic write | ✓ VERIFIED | 0600/0700, traversal guard, ids/timestamps only |
| `cmd/villa/recall.go` | verbs + seam + gate + pipeline | ✓ VERIFIED | 28.4K, runRecallIndex/runRecallStatus return-not-Exit |
| `cmd/villa/recall_live.go` | live REST curl seam | ✓ VERIFIED | 23.1K, fixed-arg curl, stdin multipart `-F file=@-`, reset-not-delete, read-merge-write attach |
| `cmd/villa/root.go` | newRecall() registered | ✓ VERIFIED | `root.go:36` |
| `docs/MEMORY.md` | recall operator section | ✓ VERIFIED | 10 `villa recall` mentions |
| `21-03-SUMMARY.md` | on-hardware SC#1-3 + A1-A4 | ✓ VERIFIED | All three SCs signed off; A1-A4 resolved |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `cmd/villa/recall.go` | `internal/recall` | Plan/RenderTranscript/Classify/Load/Save | ✓ WIRED | 8 `recall.*` call-sites |
| `cmd/villa/recall.go` | `internal/memory.Decide` | enablement gate | ✓ WIRED | 3 `memory.Decide` call-sites; gate returns exitBlocked |
| `cmd/villa/recall_live.go` | `runLoopbackCurl(Stdin)` | Phase-20 fixed-arg curl seam | ✓ WIRED | 16 call-sites; no os/exec leak |
| model `meta.knowledge` attach | new-chat semantic retrieval | server-side knowledge injection | ✓ WIRED | on-hw: `sources` citation in NEW chat (OBSIDIAN-LYNX) |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| recall index | live chat universe | OWUI admin chats list endpoints (curl) | Yes — real operator history indexed on-hw | ✓ FLOWING |
| recall status | staleness report | `recall.Classify(live, liveKnown, attachment, state)` | Yes — diff counts from real Plan; Unknown when unevaluable | ✓ FLOWING |
| model attach | meta.knowledge | read-merge-write + re-GET verify | Yes — KB id confirmed persisted before Attached | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| recall+cmd test suites | `go test ./internal/recall/... ./cmd/villa/...` | 374 passed | ✓ PASS |
| seam grep gate | `go test ./internal/inference/ -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| binary builds + tree wired | `go build ./cmd/villa && villa recall --help` | index + status subcommands shown | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| RECALL-01 | 21-01, 21-02, 21-03 | indexer semantically indexes past chats locally | ✓ SATISFIED | SC#1 verified; REQUIREMENTS.md marked Complete |
| RECALL-02 | 21-02, 21-03 | retrieve past-chat content by meaning | ✓ SATISFIED | SC#2 verified (on-hw citation); marked Complete |
| RECALL-03 | 21-01, 21-02, 21-03 | incremental re-index + honest staleness | ✓ SATISFIED | SC#3 verified (on-hw drill); marked Complete |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TODO/FIXME/XXX/TBD/HACK/placeholder in `recall.go`, `recall_live.go`, `internal/recall/*.go` | — | Clean |

### Code-Review Closure

The 21-REVIEW found 1 BLOCKER (CR-01) + 6 WARNINGs (WR-01..06), all fixed with tests:
- **CR-01** (false "complete" stamp on partial pass) → fixed at `173bfb4`; verified: `recall.go:505-510` reconciles `done == expected` before stamping complete.
- **WR-01/WR-02** (clean-replace state clear; typed outcome counting) → `173bfb4`; verified `recall.go:413-419`, `chatOutcome` enum.
- **WR-05** (single-operator fail-closed guard) → `693de5c`; verified `recall.go:370-374` (>1 human user refuses without `--i-understand-shared-recall`). Box has 1 human user, so on-hardware sign-off stands.
- **WR-06** (status state-read error ⇒ exitBlocked, not warn) → `693de5c`; verified `recall.go:539-548`.
- **WR-03/WR-04** (orphan-file cleanup on add failure; attach re-GET verification) → `440c4d4`; verified `attachKnowledgeRow` re-GET + `rowHasKnowledgeID` (`recall_live.go:444-473`).

### Human Verification Required

None outstanding. The two checkpoints that required human sign-off (SC#2 semantic-quality, SC#3 incremental drill) were performed and APPROVED on the live gfx1151 box during 21-03 (this IS the dev host; on-hardware tasks run for real). The on-hardware wave is the milestone's dominant verification path (STATE.md), and the recorded evidence (OBSIDIAN-LYNX citation, 404-on-replace, typed-Unknown on OWUI-down) is concrete and reproducible.

### Deferred Items

One out-of-scope discovery in `deferred-items.md`: pre-existing gofmt drift in `cmd/villa/bench_compare.go` and `cmd/villa/verify_memory_test.go`, NOT caused by Phase 21 — a future `make fmt` sweep. This does not touch the phase goal and is not a gap.

The full residency-under-embedding-load gate is explicitly Phase 22 scope (T-21-17 accepted); throughput baseline (~1.3 s/chat) handed forward. Not a Phase-21 gap.

### Gaps Summary

No gaps. All three success criteria are observably true: the `villa recall` command tree exists, builds, and is wired through the pure `internal/recall` core into the Phase-20 loopback REST seam; the pure-core diff/transcript/staleness/store logic is real (not stubbed) and exhaustively unit-tested (374 cmd+recall tests + seam gate green); and the on-hardware 21-03 wave proved the three behavioral properties that only a live embed+retrieve round-trip can establish — local semantic indexing (SC#1), retrieval-by-meaning with citations in a new chat (SC#2), and villa-controlled incrementality with honest typed-Unknown staleness (SC#3). The lone code-review blocker and all six warnings were fixed with tests. State file is mode 0600 and content-free. Zero-outbound posture held.

---

_Verified: 2026-06-10_
_Verifier: Claude (gsd-verifier)_
