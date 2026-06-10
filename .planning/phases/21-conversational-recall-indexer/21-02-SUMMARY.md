---
phase: 21-conversational-recall-indexer
plan: 02
subsystem: recall
tags: [go, cobra, rest, open-webui, curl, tdd, docs]

# Dependency graph
requires:
  - phase: 21-conversational-recall-indexer
    plan: 01
    provides: internal/recall pure core (Plan/RenderTranscript/Classify/Load/Save/State/AttachmentState)
  - phase: 20-open-webui-memory-rag-wiring-offline-lockdown
    provides: loopback REST primitives (mintAdminToken, runLoopbackCurl(Stdin), pollFileProcessed, discoverChatModel)
  - phase: 18-memory-spine
    provides: memory.Decide fail-closed enablement gate + liveLoadedConfig/liveLoadedMemoryEnabled seams
provides:
  - villa recall index [--rebuild] / villa recall status cobra verbs (D-07, registered in root)
  - recallDeps 17-field injectable seam + liveRecallDeps live wiring
  - cmd/villa/recall_live.go ten fixed-arg curl drives over the loopback PublishPort (D-01)
  - attachKnowledgeRow idempotent read-merge-write model attachment core (D-03, RECALL-02)
  - pollFileProcessed(timeout) parameterized poll (verify-memory behavior unchanged)
  - docs/MEMORY.md operator section for villa recall
affects: [21-03 on-hardware wave, phase-23 backup manifest, phase-23 status surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - gated-verb D-07 delta (memory off => exitBlocked refuse-with-remediation, never exit-0 no-op)
    - attach choreography extracted as injectable attachKnowledgeRow so the read-merge-write is fake-testable
    - per-chat incremental persist with deep-copying fake writeState (aliasing-proof test rig)

key-files:
  created:
    - cmd/villa/recall_live.go
    - cmd/villa/recall.go
    - cmd/villa/recall_test.go
  modified:
    - cmd/villa/verify_memory.go
    - cmd/villa/root.go
    - docs/MEMORY.md

key-decisions:
  - "attachKnowledgeRow extracted as an injectable choreography core (getRow/updateRow/createRow funcs) so TestRecallAttach proves foreign-meta preservation and dedupe off-hardware without faking curl"
  - "owuiListUsers terminates pagination on empty page OR no-new-ids (dedupe guard) — robust against a server that ignores ?page; chats list uses the documented 60/page short-page termination"
  - "owuiAttachmentState maps a failed model-row GET AFTER a successful discoverChatModel to Missing (reachable-but-absent), any earlier failure to Unknown — typed-Unknown without misreading transport errors"
  - "recall status with no KnowledgeID recorded but a minted token reports attachment Missing (confidently: no recall KB can be attached) rather than Unknown"
  - "status state-read hard error returns exitWarn with an unevaluable message (exitBlocked stays gate-only per plan); fake writeState deep-copies Chats so incremental-persist assertions cannot be satisfied by map aliasing"

patterns-established:
  - "size-aware processing timeout: recallUploadTimeout = 60s + 1s per 2 KiB of transcript, passed through the parameterized pollFileProcessed"
  - "raw response bodies embedded in error details are truncated at 512 bytes (truncateBody) — diagnosable without dumping content-bearing chat JSON to stderr"

requirements-completed: [RECALL-01, RECALL-02, RECALL-03]

# Metrics
duration: 15min
completed: 2026-06-10
---

# Phase 21 Plan 02: Recall Command Surface Summary

**`villa recall index|status` landed: ten fixed-arg curl drives over the Phase-20 loopback primitives, a 17-field recallDeps seam choreographing the gate→reachability→KB→diff→sequential-clean-replace→attach→honest-stamp pipeline with per-chat incremental persist, typed-Unknown status, the D-07 exitBlocked gate delta, and the operator doc — 21 new fake-Deps tests, 328 package tests, seam gate and make check green**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-10T11:16:56Z
- **Completed:** 2026-06-10T11:32:20Z
- **Tasks:** 3 (Task 2 TDD: RED test commit → GREEN feat commit)
- **Files modified:** 3 created, 3 modified

## Accomplishments
- **Live REST seam (D-01):** `cmd/villa/recall_live.go` adds ten drives — users/chats listing (admin archived-inclusive endpoint, Pitfall 1), chat get (history-only, never the stale flat messages view), knowledge find-or-create / file-remove(`delete_file=true`) / id-preserving reset, transcript upload via stdin multipart + parameterized poll + file/add, model-row attach, attachment-state probe, and a `/health` reachability gate — all fixed-arg curl reusing the Phase-20 primitives, zero image literals (`TestSeamGrepGate` green, no allowlist edit)
- **Idempotent attach (D-03, RECALL-02):** `attachKnowledgeRow` does GET → merge-into-`meta.knowledge` (dedupe by KB id, every foreign meta/params key preserved — T-21-10) → update, or creates the served-model override row (`base_model_id: null`) when absent — never a blind create (Pitfall 3)
- **Gated verbs (D-07):** memory off OR `memory.Decide`-invalid ⇒ BOTH verbs refuse-with-remediation at `exitBlocked` — the deliberate delta from `verify memory`'s exit-0; spy tests prove no drive runs
- **Honest index pipeline (D-04/D-05/D-06/D-09):** ordered short-circuiting steps; `villa-verify@localhost` excluded from the universe; `recall.Plan` drives sequential execute with state persisted after EVERY chat; Updates remove-old-then-re-upload; unrenderable chats are RECORDED skips; `last_index_completed_at` stamped only on the clean full pass; attach runs strictly after the loop
- **Honest status (D-06):** live-list failure renders the literal "Unknown — could not evaluate" at `exitWarn` (never stale=0); partial runs render PARTIAL; attachment folds in with the post-model-swap re-attach hint; `exitPass` only when known-zero stale AND attached
- **Docs:** `docs/MEMORY.md` gained the full operator section (verbs, `--rebuild`, status semantics, model-swap gotcha, prerequisite, widened-admin-read privacy note, non-admin caveat) with the auto-extraction default-off guidance untouched

## Task Commits

1. **Task 1: live REST seam (recall_live.go) + pollFileProcessed timeout param** - `021bbcc` (feat)
2. **Task 2: recall verbs + tests + root registration (TDD)** - `6c18ef0` (test, RED), `eee30a1` (feat, GREEN)
3. **Task 3: docs/MEMORY.md recall section + full-suite green** - `ab330cd` (docs)

## Files Created/Modified
- `cmd/villa/recall_live.go` - ten loopback REST drives, `recallKnowledgeName` const, `recallUploadTimeout` size-aware allowance, `attachKnowledgeRow`/`mergeKnowledgeIntoRow`, `truncateBody`
- `cmd/villa/recall.go` - `newRecall`/`newRecallIndex`/`newRecallStatus`, `recallDeps` (17 fields) + `liveRecallDeps`, `runRecallIndex`/`runRecallStatus` return-not-Exit bodies, shared `recallGate`, `recallLiveChats`
- `cmd/villa/recall_test.go` - `TestRecallGate`/`TestRecallIndexOrdering`/`TestRecallCleanReplace`/`TestRecallAttach`/`TestRecallStatus`/`TestRecallRebuild` over an aliasing-proof fake env (21 tests)
- `cmd/villa/verify_memory.go` - `pollFileProcessed` gained a `timeout time.Duration` parameter (verify-memory caller passes the unchanged `ragSmokeProcessTimeout`; pure refactor, tests green)
- `cmd/villa/root.go` - `newRecall()` registered
- `docs/MEMORY.md` - `## Indexing past conversations with villa recall` operator section

## Decisions Made
- `attachKnowledgeRow` extracted as an injectable core so the read-merge-write contract is provable off-hardware (the alternative — faking the curl layer — would have widened the seam for no gain)
- `owuiListUsers` terminates on empty page OR a page contributing no new ids (defensive against a server ignoring `?page`); chats listing uses the documented 60/page short-page rule
- `owuiAttachmentState`: GET-row failure AFTER a successful `discoverChatModel` ⇒ `Missing` (reachability + auth just proven; the digest 401s NOT_FOUND for absent rows); any earlier failure ⇒ `Unknown`
- `recall status` with a minted token but no recorded `KnowledgeID` reports attachment `Missing` (no recall KB exists to be attached; first `index` run fixes it)
- A hard `readState` I/O error in `status` returns `exitWarn` with an "unevaluable" message — `exitBlocked` stays gate-only per the plan's exit contract

## Deviations from Plan

None - plan executed exactly as written. (One scope note: `go fmt ./cmd/villa/` surfaced pre-existing gofmt drift in `cmd/villa/bench_compare.go` and `cmd/villa/verify_memory_test.go` — unrelated to this plan; the formatter's rewrites were reverted and the finding logged to `deferred-items.md`.)

## Issues Encountered
- None blocking. The fake test rig needed deep-copying `writeState`/`readState` closures so the incremental-persist assertions could not be vacuously satisfied by the run body and the persisted snapshot sharing one `Chats` map — caught while designing the RED tests.

## Known Stubs

None — every drive and run body is fully implemented. The live REST drives are exercised for real on-hardware in Plan 03 (by design: this plan's scope is the off-hardware-testable choreography).

## User Setup Required

None - no external service configuration required. (Plan 03 drives the live index on the gfx1151 box.)

## Next Phase Readiness
- Plan 03 (on-hardware wave) can run `villa recall index` / `status` live: A1/A2 (throughput + size-aware timeout tuning), A3 (attach merge verified against a UI-customized model row), and the RECALL-02 no-files-param retrieval probe
- `recall-state.json` now reaches disk via `recall.WriteFileAtomic` at `$XDG_DATA_HOME/villa/recall-state.json` — the Phase-23 backup manifest can read `recall.SchemaVersion()`
- Verification gates green: `go test ./cmd/villa/ -run TestRecall -count=1` (21 tests), 328 package tests, `TestSeamGrepGate`, `villa recall --help`/`index --help`/`status --help` reachable, `make check` exit 0

## TDD Gate Compliance

Task 2 (`tdd="true"`): RED commit `6c18ef0` (tests failed to compile against the missing implementation — verified before commit) → GREEN commit `eee30a1` (all 21 tests pass). No refactor commit needed.

---
*Phase: 21-conversational-recall-indexer*
*Completed: 2026-06-10*

## Self-Check: PASSED

All 3 created files + 3 modified files exist on disk; all 4 task commits (021bbcc, 6c18ef0, eee30a1, ab330cd) present in git log; TDD gate sequence verified (test 6c18ef0 → feat eee30a1); `newRecall()` registered in root.go.
