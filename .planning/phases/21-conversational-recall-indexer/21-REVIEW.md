---
phase: 21-conversational-recall-indexer
reviewed: 2026-06-10T00:00:00Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/recall/recall.go
  - internal/recall/staleness.go
  - internal/recall/store.go
  - internal/recall/transcript.go
  - cmd/villa/recall.go
  - cmd/villa/recall_live.go
  - cmd/villa/root.go
  - cmd/villa/verify_memory.go
findings:
  critical: 1
  warning: 6
  info: 4
  total: 11
status: issues_found
---

# Phase 21: Code Review Report

**Reviewed:** 2026-06-10
**Depth:** deep
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Phase 21 implements the conversational-recall indexer: a pure `internal/recall`
core (Plan diff D-05, transcript renderer D-04, staleness classifier D-06,
state store) plus the `cmd/villa` live REST drive over loopback Open WebUI. The
seam discipline is sound — no shell interpolation (all `exec.Command` fixed-arg,
JSON bodies via `json.Marshal`, transcript content via stdin multipart), the
state store mirrors the usage clone (0600/0700, atomic temp+rename,
traversal-guarded, fail-closed Load), and staleness honesty (typed-Unknown vs
fabricated stale=0) is correctly preserved through `Classify`.

The review found one BLOCKER: a partial-pass run that re-indexes an item which
becomes unrenderable on retry loses honesty discipline because the started/
completed stamps interact with `delete` in a way that can mark a partial pass
as complete. Several WARNINGs concern the privilege/identity scope of the
admin chat-read path, an attach-state false-positive, the upload→add ordering
leaving an orphaned (un-added) file on partial failure, and counter
miscounting. The cross-user KB exposure is documented as a single-operator
assumption but deserves an explicit guard rail.

## Critical Issues

### CR-01: `recall index` stamps `last_index_completed_at` even when an Update's underlying chat became unrenderable, but the `Deletes`/`Updates`/`Adds` execution can leave the index in a state the "complete full pass" stamp misrepresents

**File:** `cmd/villa/recall.go:367-417`
**Issue:**
The honesty contract (D-06 / Pitfall 8, documented in `staleness.go:7-9` and
`recall.go:17-22`) is that `last_index_completed_at` is stamped ONLY on a clean
full pass, and a partial run must remain structurally distinguishable. The
index body stamps `LastIndexCompletedAt` (line 411) whenever the
Deletes/Updates/Adds loops all return without short-circuiting. But those loops
treat an unrenderable chat as a *successful* outcome via `persist()` returning
`true`:

```go
text, renderable := recall.RenderTranscript(doc)
if !renderable {
    delete(state.Chats, ref.ID)
    skipped++
    return persist()   // returns true → loop continues, run "completes"
}
```

This is correct for a genuinely empty chat. The problem is the interaction with
`Plan` recomputation across runs combined with the started-stamp logic in
`Classify`:

`r.CompleteRun = state.LastIndexCompletedAt != "" && state.LastIndexCompletedAt >= state.LastIndexStartedAt`
(`staleness.go:65`). Because `LastIndexStartedAt` and `LastIndexCompletedAt` are
both produced by `deps.now().UTC().Format(time.RFC3339)` (`recall.go:301, 411`),
RFC3339 has **second** resolution. An index run that starts and completes within
the same wall-clock second produces `completed == started`, which the
`>=` comparison accepts — fine. But a run whose started stamp is written, then
the machine clock is adjusted backward (NTP step, DST is UTC-immune but manual
set is not), can produce `completed < started` and silently report a *clean*
run as PARTIAL, or the inverse. More importantly: the started stamp is written
once at step (4) and never re-written, so if step (4) `writeState` succeeds but
a later step partially completes and the operator re-runs, the SECOND run
overwrites `LastIndexStartedAt` with a NEW timestamp and clears
`LastIndexCompletedAt` — but any chats already deleted/updated in the prior
aborted run are gone from `state.Chats`, so `Plan` no longer sees them as
work. The combination means a run that "completes" on its second invocation
stamps `completed` while the KB may still be missing transcripts that were
removed (clean-replace `removeKnowledgeFile`) but never re-uploaded in the first
aborted run — see CR-01's sibling WR-01.

**Fix:** Stamp run boundaries with a monotonic-safe, sub-second-or-counter
identity rather than relying on `>=` over second-resolution RFC3339, and treat
"completed without re-uploading every removed file" as not-complete. Concretely,
gate the completed stamp on a reconciliation assertion that `len(plan.Adds) +
len(plan.Updates)` items were each either uploaded or recorded-as-skipped in
THIS run, not merely that no loop returned early:

```go
// after the execute loops:
expected := len(plan.Adds) + len(plan.Updates)
done := added + updated + skipped
if done != expected {
    fmt.Fprintf(errOut, "recall index: incomplete pass (%d/%d) — not stamping complete; re-run.\n", done, expected)
    return exitBlocked
}
state.LastIndexCompletedAt = deps.now().UTC().Format(time.RFC3339)
```

Also store started/completed run identity as a counter or include the
nanosecond, and compare runs by that identity instead of lexical timestamp
`>=`.

## Warnings

### WR-01: Clean-replace removes the old transcript BEFORE upload — a mid-step failure leaves the chat with NO indexed content while state still records it as indexed

**File:** `cmd/villa/recall.go:335-365`
**Issue:**
`indexChat` for an Update removes the old file first (line 337), then fetches,
renders, and uploads. If `getChat`, `RenderTranscript`-skip, or
`uploadTranscript` fails AFTER the remove succeeded, the function returns
`false` and the run aborts — but `state.Chats[ref.ID]` still holds the OLD
`ChatState` (with the now-deleted `FileID`). On the next run, `Plan` sees the
same `OWUIUpdatedAt` and, if the live `UpdatedAt` is unchanged, the chat is NO
LONGER an Update (it is neither Add nor Update) — so the removed transcript is
never re-uploaded. The chat is silently absent from retrieval while
`recall status` reports it as indexed. This contradicts the documented
invariant "completed work is never lost" (`recall.go:323`).

**Fix:** On a remove-old-then-fail path, drop the stale state entry so the next
run re-Adds it, OR record the FileID as cleared so the chat re-qualifies:

```go
if oldFileID != "" {
    if err := deps.removeKnowledgeFile(...); err != nil { ... return false }
    // old file is gone; if anything below fails, the chat must re-index next run
    cur := state.Chats[ref.ID]
    cur.FileID = ""
    cur.OWUIUpdatedAt = 0 // force re-Update/Add on next Plan
    state.Chats[ref.ID] = cur
    _ = persist()
}
```

### WR-02: The `updated`/`added` counters are read from `state.Chats` AFTER an unrenderable skip has deleted the entry — undercounting and a misleading summary

**File:** `cmd/villa/recall.go:380-395`
**Issue:**
After `indexChat` returns `true` for an Update whose chat turned out
unrenderable, the entry was `delete`d from `state.Chats` (line 349). The caller
then checks `if _, ok := state.Chats[ref.ID]; ok { updated++ }` (line 384) —
the key is absent, so `updated` is NOT incremented, but `skipped` was already
incremented inside `indexChat`. That is arguably correct for the Update case.
But for the Adds loop (line 392) the same pattern means a freshly-skipped Add is
counted as `skipped` (good). The real defect: a SUCCESSFUL update increments
both — no — re-reading, a successful update records the entry then `updated++`
fires; a skipped update fires `skipped++` only. The counts are internally
consistent but `updated` is derived by presence-after-the-fact rather than from
the actual operation outcome, which is fragile: any future change that mutates
`state.Chats[ref.ID]` for an unrelated reason flips the count. The summary line
(line 415) is the only operator-facing record of what happened.

**Fix:** Have `indexChat` return a typed outcome (`uploaded` | `skipped`)
instead of a bare `ok bool`, and increment counters from that outcome directly:

```go
type chatOutcome int
const (outcomeFail chatOutcome = iota; outcomeUploaded; outcomeSkipped)
// caller:
switch indexChat(ref, ...) {
case outcomeUploaded: updated++
case outcomeSkipped:  skipped++
case outcomeFail:     return exitBlocked
}
```

### WR-03: `owuiUploadTranscript` can leave an orphaned, processed-but-unattached file on a `knowledge/file/add` failure

**File:** `cmd/villa/recall_live.go:275-308`
**Issue:**
The upload pipeline is files/upload → poll → knowledge/file/add. If
`knowledge/file/add` fails (line 300), the function returns an error and the
file id is never returned to the caller, so it is never recorded in
`state.Chats`. The uploaded+embedded file now exists in OWUI's file table (and
its vectors in Qdrant) with no KB membership and no villa-side record — it is
unreachable by villa's clean-replace/delete (which keys off recorded FileIDs)
and will accumulate on every retry of a chat that repeatedly fails at the add
step. This is an orphaned-resource leak that also slightly widens the
content-on-disk surface the T-21-01 store discipline is trying to minimize.

**Fix:** On `knowledge/file/add` failure, best-effort delete the just-uploaded
file before returning the error (the remove primitive already exists), or return
the file id alongside the error so the caller can record it for cleanup:

```go
if _, err := runLoopbackCurl(ctx, ... "/file/add" ...); err != nil {
    _ = owuiRemoveKnowledgeFile(ctx, base, token, kbID, fResp.ID) // best-effort; file not in KB yet
    return "", fmt.Errorf("knowledge/file/add: %w", err)
}
```
(If `file/remove` requires KB membership, delete via the files DELETE endpoint
instead.)

### WR-04: `owuiAttachKnowledge` reports `AttachmentAttached` without verifying the merge persisted — a no-op update is treated as success

**File:** `cmd/villa/recall_live.go:387-416, 443-454`
**Issue:**
`attachKnowledgeRow` returns `AttachmentAttached` as soon as `updateRow` (or
`createRow`) returns nil. `updateRow` issues `models/model/update` with `-sf` and
only checks curl's exit status — it never re-reads the row to confirm the recall
KB id actually landed in `meta.knowledge`. OWUI's update endpoint can return 200
while silently dropping or reshaping `meta.knowledge` (the legacy vs modern
collection shapes are a known hazard, noted at `recall_live.go:346-348`). The
index run then prints "retrieval attached" and stamps the run complete while
retrieval is in fact OFF — the exact Pitfall 2 silent-detach the design is meant
to catch. `recall status` would later contradict the index summary.

**Fix:** After update/create, re-GET the row and confirm the kbID is present in
`meta.knowledge` before returning `AttachmentAttached`; otherwise return
`AttachmentMissing`/`Unknown` with an error:

```go
if err := updateRow(merged); err != nil { ... }
return verifyAttached(getRow, kbID) // re-read; Attached only if id present
```

### WR-05: Admin token reads EVERY user's full chat content into one shared KB attached to the served model — cross-user disclosure on any multi-user OWUI

**File:** `cmd/villa/recall.go:216-238`, `cmd/villa/recall_live.go:140-166`, `cmd/villa/recall_live.go:349-377`
**Issue:**
`recallLiveChats` enumerates ALL users via the admin endpoint, excludes only
`villa-verify@localhost`, fetches each user's full chat documents with the admin
token, renders them to transcripts, and uploads them into ONE shared Knowledge
collection. `owuiAttachKnowledge` then wires that single KB into the served
model's `meta.knowledge`, which is model-global. The net effect: every user who
chats with the served model can retrieve (with citations) the conversation
content of every other user on the instance. The CLAUDE.md context frames this
as a single-operator box (D-09: "all remaining human users are the operator"),
which makes it acceptable in the target deployment — but nothing in the code
enforces or even checks that assumption. A second human account (a guest, a
family member, a future multi-seat use) silently turns this into a
cross-account data leak, and the privacy posture of the whole product ("zero
data leaving the box") makes within-box cross-user leakage especially
surprising.

**Fix:** Make the single-operator assumption explicit and fail-closed: refuse
(or loudly warn with remediation) when `listUsers` returns more than one
non-service-account user, until a deliberate per-user-scoped KB design lands.
Even a startup check —
"recall index found N>1 human users; recall currently pools all chats into one
shared collection visible to all — re-run with --i-understand-shared-recall or
wait for per-user scoping" — converts a silent leak into an informed choice.

### WR-06: `recall status` returns `exitWarn` (2) for the memory-gate path indirectly, and conflates "could not read state" with a recoverable warning — but the gate returns `exitBlocked` (1); verify the exit-code contract is what callers expect

**File:** `cmd/villa/recall.go:439-443`, `cmd/villa/preflight.go:20-22`
**Issue:**
`exitWarn = 2` and `exitBlocked = 1` (note the non-obvious ordering). On a
state-read failure `runRecallStatus` returns `exitWarn` (line 442) with the
message "status is unevaluable." A genuinely unreadable/corrupt state file is a
hard, unevaluable condition closer to blocked than to a soft warning, yet the
index path treats the SAME `readState` error as `exitBlocked` (line 281). The
two verbs disagree on the severity of an identical failure. Scripts that branch
on exit code (1 vs 2) will treat a corrupt state file as benign under `status`
but fatal under `index`. Note also `recall.Load` itself fail-closes a corrupt
blob to empty state (no error), so `readState` only errors on a real I/O fault
(e.g. permissions) — which is arguably blocked-worthy.

**Fix:** Pick one severity for an unreadable state file across both verbs.
Recommend `exitBlocked` for a real I/O error in both, reserving `exitWarn` for
the live-list-unevaluable (typed-Unknown) case which is the legitimate soft
state:

```go
state, err := deps.readState()
if err != nil {
    fmt.Fprintf(errOut, "recall status: could not read recall-state.json (%v).\n", err)
    return exitBlocked
}
```

## Info

### IN-01: `stripReasoning` only matches the exact literal `<details type="reasoning"` — attribute reordering or single-quotes leaks chain-of-thought into the index

**File:** `internal/recall/transcript.go:75-101`
**Issue:**
The strip is a literal substring match on `<details type="reasoning"`. OWUI (or
a future digest) emitting `<details data-x type="reasoning">`,
`<details type='reasoning'>`, or with leading whitespace inside the tag will not
match, and the reasoning block is indexed verbatim — the Pitfall 5 bloat the
function exists to prevent. The image is pinned, so this is low-risk today.
**Fix:** Match `<details` followed by a `type` attribute whose value is
`reasoning` (small tokenizer or a tolerant regex), or strip any `<details …>…
</details>` whose opening tag contains `reasoning`.

### IN-02: `RenderTranscript` interpolates the chat title verbatim into the transcript header — a title containing newlines distorts the document structure

**File:** `internal/recall/transcript.go:128`
**Issue:**
`fmt.Fprintf(&b, "# %s\n", c.Title)` writes the API-returned title as-is. A
title containing a newline or a leading `user:`/`assistant:` line could confuse
downstream chunking/parsing that keys on the role-prefixed line shape. Not a
security issue (content is embedded, not executed), but it weakens the
deterministic document contract.
**Fix:** Strip/escape newlines in the title before writing the header
(`strings.ReplaceAll(c.Title, "\n", " ")`).

### IN-03: `recallUploadTimeout` integer division silently floors small content to the base timeout

**File:** `cmd/villa/recall_live.go:71-73`
**Issue:**
`time.Duration(len(content)/2048)*time.Second` — content under 2048 bytes adds
0s, which is intended, but the comment says "1s per 2 KiB" while the code is
"1s per completed 2048-byte block." Cosmetic; the floor is harmless because the
base is generous. Worth a one-line comment correction so a future reader does
not "fix" the floor.
**Fix:** Align the comment with the floor behavior, or use ceil if per-partial-
block allowance is desired.

### IN-04: `Plan` recomputes Deletes by iterating `state.Chats` which the execute loops mutate — safe today, but the read-after-Plan coupling is implicit

**File:** `cmd/villa/recall.go:320, 367-395`
**Issue:**
`plan := recall.Plan(live, state)` snapshots the diff, then the execute loops
mutate `state.Chats` (delete/insert) while iterating `plan.*` slices (not the
map), so there is no concurrent-map-iteration hazard. This is correct, but it
relies on Plan having copied the refs out (it has — slices of `ChatRef`/`string`
by value). A future change that made `Plan` return map references would
introduce a mutate-during-iterate bug. Add a test asserting Plan output is
independent of post-Plan state mutation.
**Fix:** Document the snapshot invariant on `PlanResult` and add a regression
test.

---

_Reviewed: 2026-06-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
