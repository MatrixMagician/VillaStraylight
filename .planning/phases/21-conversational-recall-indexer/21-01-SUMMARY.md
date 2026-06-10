---
phase: 21-conversational-recall-indexer
plan: 01
subsystem: recall
tags: [go, pure-core, tdd, open-webui, rag, xdg-store, json]

# Dependency graph
requires:
  - phase: 15-cumulative-usage
    provides: internal/usage atomic XDG store discipline (cloned, not imported)
  - phase: 18-memory-stack
    provides: internal/memory Decide typed-verdict shape mirrored by Classify
provides:
  - internal/recall pure core (the ONE new package of phase 21, D-08)
  - recall-state.json schema v1 store (fail-closed Load, atomic Save, ids/timestamps only)
  - Plan(live, state) D-05 diff algebra (Adds/Updates/Deletes, sorted, copy-not-mutate)
  - RenderTranscript D-04 chain-walk renderer + TranscriptFilename
  - Classify typed-Unknown staleness Report + AttachmentState enum (D-06)
affects: [21-02 cmd tier recall verbs, 21-03 on-hardware wave, phase-23 backup manifest]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - clone-don't-import store discipline (usage.go:243 rule applied to internal/recall/store.go)
    - typed-Unknown staleness (StaleKnown bool pairing, mirrors memory.Decide)
    - currentId→parentId chain walk with visited-set cycle guard (OWUI get_message_list semantics)

key-files:
  created:
    - internal/recall/store.go
    - internal/recall/store_test.go
    - internal/recall/recall.go
    - internal/recall/recall_test.go
    - internal/recall/transcript.go
    - internal/recall/transcript_test.go
    - internal/recall/staleness.go
    - internal/recall/staleness_test.go
  modified: []

key-decisions:
  - "CompleteRun = completed!=\"\" && completed>=started (lexicographic RFC3339 UTC compare) — catches a partial SECOND run leaving an older completed stamp, stricter than the plan's minimum"
  - "stripReasoning trims surrounding whitespace after block removal so reasoning-only assistant turns render as an empty (not whitespace) content"
  - "State JSON tags emit all scalar fields (no omitempty except chats map) for a deterministic, explicit on-disk document"

patterns-established:
  - "internal/recall purity: stdlib-only imports, no os/exec, no image/backend literals — TestSeamGrepGate green with zero allowlist edits"
  - "Classify reuses Plan for the diff counts — the D-05 algebra has exactly one implementation"

requirements-completed: [RECALL-01, RECALL-03]

# Metrics
duration: 10min
completed: 2026-06-10
---

# Phase 21 Plan 01: Pure Recall Core Summary

**`internal/recall` pure core landed: D-05 Plan diff algebra, D-04 currentId-chain transcript renderer with reasoning-strip, D-06 typed-Unknown staleness Classify, and a fail-closed atomic recall-state.json store cloned from internal/usage — 38 off-hardware tests green, seam gate untouched**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-06-10T11:04:48Z
- **Completed:** 2026-06-10T11:14:30Z
- **Tasks:** 3 (all TDD: RED test commit → GREEN feat commit each)
- **Files modified:** 8 created

## Accomplishments
- The ONE new pure package of the phase (D-08): 4 source files + 4 test files, only stdlib imports, `TestSeamGrepGate` green with no allowlist edit
- recall-state.json schema v1 (D-05): fail-closed `Load` (absent/corrupt/future-schema ⇒ empty state, never a fabricated index), version-stamping `Save`, `WriteFileAtomic` 0600/0700 + traversal guard vs the fixed XDG root, and a JSON-key content-denylist test proving the file carries ids/timestamps only (T-21-01)
- `Plan(live, state)` implements exactly the D-05 algebra (new = L∖S, changed = updated_at > owui_updated_at, deleted = S∖L) with sorted deterministic output and proven input purity
- `RenderTranscript` walks history.currentId → parentId with a visited-set cycle guard (never the stale flat chat.messages view), strips `<details type="reasoning">` blocks (single/multiple/unclosed), and signals skip via ok=false — never a silent drop (D-04, T-21-05)
- `Classify` makes Unknown structurally distinct from zero (`StaleKnown=false` + named reason) while Indexed/LastIndex* still report from villa-side state; partial runs distinguishable via `CompleteRun` (D-06, T-21-04)

## Task Commits

Each task was committed atomically (TDD: test commit then feat commit):

1. **Task 1: recall-state.json store** - `15da473` (test, RED), `921840b` (feat, GREEN)
2. **Task 2: Plan diff + transcript renderer** - `5813725` (test, RED), `3846728` (feat, GREEN)
3. **Task 3: typed-Unknown staleness + seam gate proof** - `12a38f8` (test, RED), `566db9e` (feat, GREEN)

## Files Created/Modified
- `internal/recall/store.go` - State/ChatState schema v1, SchemaVersion(), fail-closed Load, stamping Save, RecallStatePath, WriteFileAtomic + assertInsideDir (clone of internal/usage)
- `internal/recall/store_test.go` - fail-closed table, round-trip, atomic-write/0600/0700/temp-cleanup, traversal guard, JSON-key content denylist
- `internal/recall/recall.go` - ChatRef, PlanResult, pure Plan diff (D-05)
- `internal/recall/recall_test.go` - algebra table, deterministic order, input-purity proof
- `internal/recall/transcript.go` - ChatDoc/ChatHistory/ChatMsg, linearThread, stripReasoning, RenderTranscript, TranscriptFilename (D-04)
- `internal/recall/transcript_test.go` - chain walk, cycle guard, reasoning strip (single/multiple/unclosed), skip cases, role filtering, filename
- `internal/recall/staleness.go` - AttachmentState enum, Report, Classify (D-06)
- `internal/recall/staleness_test.go` - typed-Unknown + partial-run tables, Unknown≠0 structural proof

## Decisions Made
- `CompleteRun` compares `LastIndexCompletedAt >= LastIndexStartedAt` (RFC3339 UTC strings compare lexicographically) rather than only checking non-empty — a partial second run that leaves an OLDER completed stamp is honestly reported as incomplete
- `stripReasoning` trims whitespace after removing blocks, so a reasoning-only assistant message renders as an empty-content turn (still a turn — the message exists)
- State scalar JSON fields emit unconditionally (no omitempty) for a deterministic, self-describing on-disk document; only the `chats` map is omitempty

## Deviations from Plan

None - plan executed exactly as written. (One test-authoring correction during Task 2 GREEN: the expected UTC rendering of epoch 1781040998 in the chain-walk test was miscalculated in the RED commit and corrected to the implementation's correct output — not a code deviation.)

## Issues Encountered
- `Report` carries a `Reasons []string` field, so the table test's struct comparison needed `reflect.DeepEqual` instead of `!=` — caught before the RED commit

## Known Stubs

None — every function in `internal/recall` is fully implemented and covered; the cmd tier wiring is intentionally Plan 02's scope.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Plan 02 (cmd tier) can now be a thin choreography layer: `recallDeps` wires curl drives + `WriteFileAtomic`/`RecallStatePath` as the live byte-I/O seam; all decision logic is already covered off-hardware
- `SchemaVersion()` is exported as the Phase-23 backup-manifest reader-of-record
- Verification gates green: `go test ./internal/recall/ -count=1` (38 tests), `TestSeamGrepGate`, `go build ./...`, `make check` exit 0

---
*Phase: 21-conversational-recall-indexer*
*Completed: 2026-06-10*

## Self-Check: PASSED

All 8 created files exist on disk; all 6 task commits (15da473, 921840b, 5813725, 3846728, 12a38f8, 566db9e) present in git log; TDD gate sequence verified (test→feat per task).
