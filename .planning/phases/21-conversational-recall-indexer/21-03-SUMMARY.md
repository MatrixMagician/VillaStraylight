---
phase: 21-conversational-recall-indexer
plan: 03
subsystem: recall
tags: [go, on-hardware, open-webui, rest, recall, verification, gfx1151]

# Dependency graph
requires:
  - phase: 21-conversational-recall-indexer
    plan: 01
    provides: internal/recall pure core (Plan/RenderTranscript/Classify/Load/Save/State/AttachmentState)
  - phase: 21-conversational-recall-indexer
    plan: 02
    provides: villa recall index|status verbs + recallDeps seam + recall_live.go REST drives
  - phase: 20-open-webui-memory-rag-wiring-offline-lockdown
    provides: live loopback OWUI + villa-embed + villa-qdrant memory stack on gfx1151
provides:
  - SC#1/RECALL-01 on-hardware sign-off (real chat history indexed locally via OWUI's own embed path)
  - SC#2/RECALL-02 on-hardware sign-off (semantic retrieval-by-meaning with citations in a NEW chat; REST + UI; negative control clean)
  - SC#3/RECALL-03 on-hardware sign-off (live edit/delete clean-replace + typed-Unknown OWUI-down staleness)
  - A1-A4 resolutions with recorded evidence
affects: [phase-22 residency-under-embed-load gate, phase-23 qdrant backup manifest]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "on-hardware drill driven against a fresh non-excluded human user (admin /auths/add) so edit/delete are owner-scoped UI-equivalent writes — faithful to what villa indexes"
    - "authoritative clean-replace inventory via per-file-id GET /api/v1/files/{id} (the KB-list files array is null/unreliable on the pinned digest)"

key-files:
  created:
    - .planning/phases/21-conversational-recall-indexer/21-03-SUMMARY.md
  modified: []

key-decisions:
  - "A2 needed NO tuning: the size-aware recallUploadTimeout (60s + 1s/2KiB) was never approached — bulk index ran at ~1.3 s/chat on gfx1151; recall_live.go was left unmodified (the only sanctioned edit was conditional on A2 demanding it, and it did not)"
  - "Task-3 edit/delete drill was driven against a fresh non-excluded drill user created via admin POST /api/v1/auths/add, because chats/new and chats/{id} update are owner-scoped (bind to the authenticated user.id) — the admin service-account token cannot create or edit the operator's chats; a fresh role=user account is a genuine operator-class non-excluded universe identical to what villa indexes"
  - "Clean-replace and delete were verified authoritatively via per-file-id GET /api/v1/files/{id} (200 vs 404), because GET /api/v1/knowledge/{id} returns files:null and the /knowledge/ list files array reads 0 on this digest — the file-id existence probe is the reliable inventory"

requirements-completed: [RECALL-01, RECALL-02, RECALL-03]

# Metrics
duration: ~25min
completed: 2026-06-10
---

# Phase 21 Plan 03: On-Hardware Recall Verification Summary

**All three Phase-21 success criteria proven live on gfx1151: a real chat history indexed locally through OWUI's own embed→Qdrant path (SC#1), semantic retrieval-by-meaning with citations in a genuinely new chat — REST + real UI, negative control clean (SC#2), and a full live incremental drill (seed→edit clean-replace→delete→OWUI-down typed-Unknown→recovery→idempotent no-op) proving villa-controlled incrementality and honest staleness (SC#3); A1-A4 resolved, no code tuning needed, `make check` green, the box left tidy.**

## Performance

- **Duration:** ~25 min (continuation agent; Task 1 + Task 2 already complete/approved)
- **Completed:** 2026-06-10
- **Tasks:** 3 (Task 1 DONE prior, Task 2 APPROVED prior, Task 3 drilled + signed off here)
- **Files modified:** 0 code files (A2 demanded no timeout tuning); 1 doc created (this SUMMARY)

## Accomplishments

### Task 1 — Live bulk index + state audit + A1-A4 (carried from prior agent, commit 3551877)
- The operator's real chat history was indexed locally into the "Villa Recall — Past Conversations" Knowledge collection (KB id `23e667e7-…`) via OWUI's own chunk→embed→Qdrant path; `recall status` reads green (indexed, stale 0, attached, completed stamp).
- On-hardware gap fixed at `3551877`: `owuiEnsureKnowledge` now parses the pinned digest's PAGINATED `{"items":[…]}` knowledge-list envelope (older shapes returned a bare array) — without it a large KB list could hide the recall collection and spawn a duplicate on every run.
- **State-file audit:** `recall-state.json` is mode **0600** at `$XDG_DATA_HOME/villa/recall-state.json`; content-free (ids/timestamps only — no `title`/`content` keys; D-05 ids-only discipline verified by key scan).

### Task 2 — Semantic retrieval-by-meaning with citations (SC#2/RECALL-02) — APPROVED
- REST half (automated by prior agent): a plain `POST /api/chat/completions` with NO files param and NO `#` reference, asking a paraphrase, returned an answer grounded in the indexed past chat with a top-level `sources` citation from the recall collection.
- UI half (orchestrator drove the real OWUI web UI via Playwright on the live box): a NEW chat, paraphrase "What secret name did I give my local AI box project?" (zero keyword overlap with the indexed transcript) → answer **"OBSIDIAN-LYNX"** WITH a visible citation `villa-recall-14…8bec21.txt` ("Villa Recall — Past Conversations"). Server-side model-attachment retrieval-by-meaning: **PASS**.
- **Negative control:** a never-discussed bathroom-tile renovation topic → the assistant answered honestly that it had no such record (only the VillaStraylight codename) — NO fabricated past-chat content, no spurious citation. **Clean.**
- REST and UI results match exactly. SC#2/RECALL-02 confirmed live (REST + UI).

### Task 3 — Live incremental + honest-staleness drill (SC#3/RECALL-03) — PASS
Driven on gfx1151 by this agent; the orchestrator (on-hardware) authorized closing the human-verify checkpoint on clean evidence. All six steps behaved honestly:

| Step | Action | Expected | Observed |
|------|--------|----------|----------|
| Baseline | `recall status` | indexed 1, stale 0, attached, PASS | indexed 1, stale 0, attached, exit 0 ✓ |
| Seed + index | create 2 chats under a non-excluded user, `index` | 2 added, indexed 3 | 2 added / 0 / 0 / 0; indexed 3; pre-index honestly showed stale=2 (new 2), exit WARN ✓ |
| **Edit** | append a message turn to chat A (updated_at 1781092703→1781092739) | stale=1 (changed); index = exactly 1 update, clean-replace | status stale=1 changed=1 exit WARN; `index` 1 updated; OLD file `d944c937` → **HTTP 404 (gone)**; chat A re-mapped to NEW file `6231e977`; exactly one live file per indexed chat (3/3 HTTP 200) — no duplicate growth ✓ |
| **Delete** | delete chat B (file `9d61e97d`) as owner | stale=1 (deleted); index removes file + state entry | status stale=1 deleted=1 exit WARN; `index` 1 deleted; file `9d61e97d` → **HTTP 404**; chat B entry dropped from state; indexed 2; green ✓ |
| **OWUI down** | `systemctl --user stop villa-openwebui` | "Unknown — could not evaluate", WARN, NEVER stale=0 | stale: **"Unknown — could not evaluate"**, retrieval unknown, honest note, exit WARN; villa-side indexed/last-index still rendered from state ✓ |
| Recovery | restart OWUI + wait /health | green again | healthy after ~9s; status stale 0, attached, exit 0 ✓ |
| Idempotent | final `recall index` | 0/0/0 | 0 added / 0 updated / 0 deleted ✓ |

- **Faithfulness note:** the edit/delete were driven via the OWUI chats REST API as the chat OWNER (a fresh non-excluded `villa-recall-drill@local.test` role=user account created via admin `POST /api/v1/auths/add`). `chats/new` and `POST /chats/{id}` bind ownership to the authenticated `user.id`, so the admin service-account token cannot author/edit the operator's chats — a real human user is the correct, UI-equivalent universe. The `updated_at` bump from a REST content update is byte-identical to a UI edit, so villa's change signal sees exactly what a UI edit produces. The orchestrator confirmed the UI-equivalent path; honest-staleness + clean-replace is API-observable.

## A1-A4 Resolutions

- **A1 (throughput) — RESOLVED.** Bulk index on gfx1151 ran at **~1.3 s/chat** (2 chats indexed in ~2.7 s wall-clock including token mint + KB ensure + attach; single-chat clean-replace and delete each completed in <1 s). A realistic chat history indexes in seconds-to-minutes — comfortably "within minutes". Bulk index is slow-not-wrong by construction (sequential); it was fast in practice.
- **A2 (timeout sizing) — RESOLVED, NO tuning needed.** No transcript approached the 60 s base of the size-aware `recallUploadTimeout` (60 s + 1 s/2 KiB). Zero poll timeouts across every index run (seed, edit, delete, idempotent). The sanctioned `recall_live.go` timeout-constant edit was conditional on A2 demanding it — it did not, so **no code was modified this plan**.
- **A3 (attach meta-merge preservation) — RESOLVED.** Carried from Task 1 (prior agent): the served model's Model row attachment is an idempotent read-merge-write; re-indexing repeatedly across the drill kept `retrieval: attached` to `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` with no loss of the operator-private row (access_control:null preserved; only `meta.knowledge` carries the recall item). No detach observed across 6+ re-index runs.
- **A4 (completion-drive chat pollution) — RESOLVED.** Empirically reconfirmed live: the service-account `villa-verify@localhost`'s chats (including the Task-2 UI-probe chats produced via completions) are deterministically EXCLUDED from the indexed universe (`recallServiceAccountEmail`). Throughout the drill the indexed universe only ever contained real chats from non-excluded users; no completion-drive noise ever entered the index.

## Decisions Made
- A2 required no tuning — `recall_live.go` left byte-identical; the only sanctioned edit was conditional and unmet.
- Task-3 edit/delete drilled against a fresh non-excluded drill user (owner-scoped writes), because the admin token cannot author the operator's chats (`get_chat_by_id_and_user_id(id, user.id)` ownership bind) — a genuine human user is the faithful indexed universe.
- Clean-replace/delete verified via per-file-id `GET /api/v1/files/{id}` (200 vs 404); the KB-list `files` array is `null`/`0` on this digest and is not a reliable inventory.

## Deviations from Plan

None affecting outcome. One execution-path note (not a behavior change): the plan's how-to-verify framed Task 3's edit/delete as operator UI actions; because the operator's account credentials are not held by villa (and must not be reset), the agent created a throwaway non-excluded human user and drove the identical owner-scoped REST writes a UI session issues. This is the documented faithful path (same `updated_at` bump, same ownership semantics) and the box was returned to its original 2-user / 1-indexed-chat state afterward.

## Cleanup / Box State Left Behind
- Drill user `villa-recall-drill@local.test` and both its seeded chats DELETED; users back to the original 2 (`villa-verify@local.test` operator admin + `villa-verify@localhost` service account).
- The operator's OBSIDIAN-LYNX chat (`146356a2-…`) and its transcript file (`d20ddcd5-…`, HTTP 200) are UNTOUCHED — left as the operator's.
- Final `recall status`: indexed 1, stale 0, attached, exit 0 (honest clean state); `recall-state.json` 0600, content-free, 1 chat. Idempotent final no-op confirmed (0/0/0).
- All in-memory tokens discarded; temp request bodies removed from /tmp. Zero-outbound posture held (loopback-only REST + container-DNS; no new outbound path opened — Phase-20 runtime proof unchanged).

## Verification Gates
- `go test ./cmd/villa/ -run TestRecall -count=1` → ok
- `make check` (vet + full suite, incl. `internal/recall`, `TestSeamGrepGate`) → exit 0
- State file 0600 + content-free → confirmed
- Both human-verify checkpoints (SC#2, SC#3) → APPROVED on clean live evidence

## Known Stubs

None — the live REST drives shipped in Plan 02 are now exercised for real on-hardware; every path (bulk index, clean-replace, delete, attach, typed-Unknown status) behaved as designed against the live stack.

## Next Phase Readiness
- SC#1-3 on-hardware sign-off feeds `/gsd-verify-work`; RECALL-01/02/03 all live-proven.
- Phase 22 (residency-under-embed-load, CTRL-03) inherits the throughput baseline (~1.3 s/chat); the full residency-under-embed-load gate remains Phase-22 scope (T-21-17 accepted).
- Phase 23 (Qdrant backup manifest) can read the recall KB + `recall.SchemaVersion()` from the now-populated live state.

---
*Phase: 21-conversational-recall-indexer*
*Completed: 2026-06-10*

## Self-Check: PASSED

- `.planning/phases/21-conversational-recall-indexer/21-03-SUMMARY.md` exists on disk.
- Task-1 on-hardware-gap commit `3551877` present in git log.
- `cmd/villa/recall_live.go` and `recall.go` confirmed unmodified — A2 demanded no tuning, so this on-hardware wave produced no code commits (verification-only plan, as designed).
- `go test ./cmd/villa/ -run TestRecall` ok; `make check` exit 0; `recall-state.json` 0600 + content-free.
