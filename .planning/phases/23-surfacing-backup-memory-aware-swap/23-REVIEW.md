---
phase: 23-surfacing-backup-memory-aware-swap
reviewed: 2026-06-10T22:01:09Z
depth: deep
files_reviewed: 35
files_reviewed_list:
  - cmd/villa/backup.go
  - cmd/villa/doctor.go
  - cmd/villa/doctor_test.go
  - cmd/villa/install.go
  - cmd/villa/install_memory.go
  - cmd/villa/install_memory_test.go
  - cmd/villa/podman_volume.go
  - cmd/villa/podman_volume_test.go
  - cmd/villa/recall.go
  - cmd/villa/recall_test.go
  - cmd/villa/restore.go
  - cmd/villa/restore_test.go
  - cmd/villa/status.go
  - cmd/villa/status_test.go
  - cmd/villa/testdata/status.json.golden
  - cmd/villa/testdata/status-memory.json.golden
  - internal/backup/backup.go
  - internal/backup/backup_test.go
  - internal/backup/deps.go
  - internal/backup/manifest.go
  - internal/backup/manifest_test.go
  - internal/backup/restore.go
  - internal/backup/restore_test.go
  - internal/dashboard/api_test.go
  - internal/dashboard/assets/dashboard.html
  - internal/dashboard/assets/dashboard.js
  - internal/doctor/doctor.go
  - internal/doctor/doctor_test.go
  - internal/modelswap/modelswap_test.go
  - internal/orchestrate/memory_test.go
  - internal/recall/skew.go
  - internal/recall/skew_test.go
  - internal/recall/staleness.go
  - internal/status/status.go
  - internal/status/status_test.go
findings:
  critical: 1
  warning: 6
  info: 8
  total: 15
status: issues_found
---

# Phase 23: Code Review Report

**Reviewed:** 2026-06-10T22:01:09Z
**Depth:** deep
**Files Reviewed:** 35
**Status:** issues_found

## Summary

Deep review of the Phase-23 surface: status.Report v2→3 memory surfacing (internal/status + cmd/villa/status.go + goldens), memory-aware backup/restore of the Qdrant volume (internal/backup + cmd/villa/backup.go/restore.go/podman_volume.go), embedding-skew guards (internal/recall/skew.go, recall.go, install_memory.go), the doctor memory fold, and the dashboard memory panel.

Conventions held up well under cross-file tracing: no backend/image/volume literal leaks outside the seams (every identity flows through `orchestrate.QdrantVolumeName()` / `orchestrate.EmbedImage()` / `inference.BackendFor`), every host command is fixed-arg `exec.Command`, the v3 JSON contract is a clean tail-append refreeze (memory-off delta is `schema_version` only, verified against both goldens), and dashboard.js renders all server values via `createElement`+`textContent`. The typed-Unknown discipline is consistently applied in the status/doctor/recall read paths.

The serious problems live in the **transactional restore's failure paths** and in **gate ordering before destructive operations**: the rollback path cannot actually complete on a live host for a prove-triggered rollback (running services hold the volumes), and the cmd tier then deletes the only rollback copy of the prior data — a real data-loss path in exactly the scenario rollback exists for. Several other guards (recall single-operator guard, restore's fail-soft volume-exists gate, backup output truncation) run after, not before, the mutation they should protect.

## Critical Issues

### CR-01: Prove-fail rollback cannot complete on a live host (volumes in use), and the cmd tier then deletes the only rollback copy of prior data

**File:** `internal/backup/restore.go:241-295` (rollback closure), `internal/backup/restore.go:372-390` (forward starts before Prove), `cmd/villa/restore.go:57-77` (unconditional tmpDir cleanup)
**Issue:** The forward restore path starts Open WebUI (and the qdrant service, when applicable) at step (5) **before** the Prove gate at step (6). When Prove returns non-pass (the exact CPU-fallback scenario rollback exists for), `rollback()` runs `cleanRecreateThenImport(priorCfg, OpenWebUIVolumeName, RollbackVolumeTar)` — whose first step is `VolumeRm` — **without ever stopping the now-running services**. On a real host, `podman volume rm` fails with "volume is in use" while the Quadlet container holds the volume, so the rollback re-import of the captured prior volume fails for BOTH the OWUI and qdrant volumes. The Result honestly reports rollback-incomplete, but the running stack keeps the **archive's** data, not the prior data. The cmd tier (`cmd/villa/restore.go` RunE) then runs `cleanup()` unconditionally, deleting `tmpDir` — which contains `rollback-owui.tar` / `rollback-qdrant.tar`, the **only copies of the prior webui.db and qdrant vectors**. The prior chat database is permanently lost. The same hole exists on the qdrant-Start-failure path (OWUI is already running when its volume rollback fires). The entire test suite (`TestRestoreNonPassProveRollsBack`, `TestRestoreQdrantForwardFailureRollsBackBothVolumes`) uses no-op fakes whose `VolumeRm` always succeeds, so the in-use failure mode is structurally invisible to the tests.
**Fix:**
```go
// internal/backup/restore.go — rollback() must quiesce before re-importing,
// mirroring the forward path's own quiesce:
rollback := func() (ok bool, detail string) {
    ok = true
    add := func(e error, what string) { /* unchanged */ }
    // QUIESCE FIRST: the forward path may have started these before Prove ran;
    // a running container holds its volume and VolumeRm would fail in-use.
    add(d.Stop(d.OpenWebUIServiceName), "stop Open WebUI for rollback")
    if ex.qdrantPresent {
        add(d.Stop(d.QdrantServiceName), "stop Qdrant for rollback")
    }
    add(d.SaveConfig(priorCfg), "SaveConfig(prior)")
    // ... rest unchanged ...
}
```
And in `cmd/villa/restore.go`, preserve the rollback tars when the rollback did not fully complete (detectable from the printed rollback-incomplete Reason / a new Result flag), printing the tmpDir path with a recovery hint instead of `os.RemoveAll`. Add a fake-deps test in `internal/backup/restore_test.go` whose `VolumeRm` fails while the service is "running" to lock the quiesce-before-rollback ordering.

## Warnings

### WR-01: `villa recall index` mutates (KB reset, KB create, state-stamp persist) BEFORE the single-operator privacy guard refuses

**File:** `cmd/villa/recall.go:351-398`
**Issue:** The WR-05 multi-human guard (step 5a) runs **after** step 4: on `--rebuild`, `resetKnowledge` has already destroyed the recall collection's content; on every run, `ensureKnowledge` has already created the KB and the state file has been persisted with `LastIndexStartedAt` stamped, `LastIndexCompletedAt` cleared, and `EmbeddingModel`/`EmbeddingDim` overwritten with the **configured** identity. A run that is then refused at the guard ("found N human users… REFUSING") has already wiped the index (`--rebuild`) and re-stamped the embedding identity for an index pass that never happened — on a pre-Phase-21 store (empty stamp) this records the new config identity over content built under a different embedder, defeating the future skew comparison. A refusal should be side-effect-free. (The index content is recoverable by re-running with the ack flag, which is why this is a Warning, not a Blocker.)
**Fix:** Hoist `listUsers` + the `recallHumanUsers`/ack-flag refusal to between step (3) token mint and step (4) state/KB work (the token is the only dependency). Reuse the already-fetched `users` for `recallChatsForUsers` at step (5). Add a test asserting a multi-human refusal with `--rebuild` fires zero `reset:`/`ensureKB`/`persist` calls.

### WR-02: Restore gates Qdrant capture/quiesce on the fail-soft `volumeExists` — a transient podman error can route a destructive `VolumeRm` past the rollback capture

**File:** `cmd/villa/restore.go:218`, `cmd/villa/podman_volume.go:52-76`, `internal/backup/restore.go:192-197, 324-328`
**Issue:** `in.QdrantVolumeExists = volumeExists(...)` is fail-soft by design (any non-exit-1 error ⇒ `false` with a warning). For **backup** that is the right direction (the entry is honestly omitted). For **restore** it selects the destructive branch shape: `false` means no capture export, no `Stop(qdrant)`, and rollback becomes "remove the forward-created volume". If the exists check fails transiently while the volume actually exists (podman hiccup, socket race), the forward path runs `VolumeRm` on a real, uncaptured qdrant volume — if it succeeds (qdrant not currently running), the existing vectors are destroyed with no rollback copy; the "rollback" then removes whatever the import created. A warning line on stderr is the only signal, and the restore proceeds without confirmation. Destructive paths should fail closed on an unevaluable signal (the project's own typed-Unknown doctrine — here an Unknown is silently collapsed into a confident "absent").
**Fix:** Make the existence check tri-state for restore (exists / absent / unknown). On `unknown`, refuse the restore before any mutation with a remediation ("could not determine whether the Qdrant volume exists — check podman, then re-run"), or at minimum require `--yes`/interactive consent to proceed treating it as absent. `classifyVolumeExists` already distinguishes the cases (`warn==true` is exactly the unknown cell); thread that through instead of flattening to `false`.

### WR-03: A memory-OFF backup still archives `recall-state.json` while the manifest omits `recall_schema_version` — the entry escapes the fail-closed schema gate and contradicts the documented v1-identical layout

**File:** `cmd/villa/backup.go:179-200`, `internal/backup/manifest.go:38-40`, `internal/backup/backup.go:183-199`
**Issue:** `RecallStatePath` is passed unconditionally ("no presence gate is needed here — D-06"), so a host with `memory_enabled=false` but a leftover `recall-state.json` (user previously had memory on) produces an archive **containing** the `recall-state.json` entry — but the manifest's `EmbeddingModel`/`EmbeddingDim`/`RecallSchemaVersion` fields are gated on `cfg.MemoryEnabled` and are all omitted. Consequences: (a) `manifest.go`'s documented contract ("the two Phase-23 memory entries… are OPTIONAL: present only in a memory-on backup, D-05/D-06") and the core's "archive identical to the v1 layout" claim are violated; (b) on restore, `blockOnNewerStore("recall", 0, cur)` cannot block a future-schema recall state because the version was never recorded — the fail-closed protection that exists for usage/bench/recall is silently bypassed for exactly this entry (mitigated only by `recall.Load`'s own fail-close-to-empty, which silently discards the restored state instead of refusing). The memory-off backup tests (`TestBackupMemoryOffZeroQdrantCalls`) pass an empty `RecallStatePath` and so never exercise the real cmd-tier wiring.
**Fix:** Gate `in.RecallStatePath` on `cfg.MemoryEnabled` (mirroring the qdrant entry), or — if including the orphan state file is intentional — also record `RecallSchemaVersion` unconditionally so the schema gate applies, and update the manifest/core doc comments to match. Either way, add a cmd-tier test with memory off + an existing recall-state.json.

### WR-04: `villa backup -o <existing.tar>` truncates the existing archive up-front and deletes it on failure — a failed backup destroys the previous backup

**File:** `cmd/villa/backup.go:119-125, 202-211`
**Issue:** The output is opened `O_CREATE|O_WRONLY|O_TRUNC` before any quiesce/export work, so an existing archive at the `-o` path is destroyed immediately; every subsequent failure path (`temp volume file`, `Backup()` error, close error) runs `os.Remove(absOut)`. Net effect: re-using a backup path (a natural pattern, e.g. `villa backup -o ~/backups/villa-latest.tar` from cron) means any mid-backup failure leaves the operator with **zero** backups — the old archive is gone and the new one was never completed. For a backup tool this is the wrong atomicity direction.
**Fix:** Write to a same-directory temp file (`os.CreateTemp(parent, ".villa-backup-*.tar")`) and `os.Rename` onto `absOut` only after `backup.Backup` and `Close` succeed; remove only the temp file on failure. This is the same temp+rename discipline `config.SaveVilla`/`WriteFileAtomic` already use.

### WR-05: dashboard renderModels conflates an /api/models fetch failure with an empty catalog — fabricates the "No models in catalog" empty state

**File:** `internal/dashboard/assets/dashboard.js:523-528, 605-613, 770-773`
**Issue:** `getJSON` returns `null` on any failure (non-200, network error, parse error), and `renderModels` treats `null` and `[]` identically: `if (!models || models.length === 0) { renderModelsEmpty(); }`. On a transient API failure the panel confidently tells the operator "No models in catalog — No models are available to switch to. Pull one with `villa model pull <id>`…" — a fabricated confident-empty for an unevaluable signal, violating the typed-Unknown rendering doctrine this very file applies everywhere else (renderPerformance/renderGPU render "Unavailable" on `null`; renderMemory uses the gray badge). It also discards the last-good model rows that the stale-dimming convention says should be kept.
**Fix:**
```js
getJSON("/api/models").then(function (models) {
  if (models === null) {            // fetch failed → typed-Unknown, keep last-good rows
    return;                          // (or render a muted "Models unavailable" if body empty)
  }
  clearSwitchIfLoaded(models);
  renderModels(models);
});
```
with `renderModels` reserving `renderModelsEmpty()` for a genuine `[]`.

### WR-06: Backup and restore buffer entire volume tars (and the whole archive) in memory

**File:** `internal/backup/backup.go:183-226` (ReadFile of exported volume tars), `internal/backup/restore.go:432-557` (`collect` map holds every entry's full bytes), `cmd/villa/restore.go` (extracted tars re-staged via `WriteTempFile([]byte)`)
**Issue:** The OWUI volume tar and — new in Phase 23 — the **Qdrant volume tar** are read fully into memory for checksumming and archive assembly, and `readAndVerify` collects every archive entry's bytes into a map before extraction. A populated Qdrant store is the one entry in this archive that realistically grows to many GiB of vectors; on a memory-tight host (the product's own MEM-PRE-headroom gate exists because hosts run near the envelope) a backup/restore of a large index risks OOM-killing the process mid-quiesce — for backup that leaves services stopped until the deferred restarts never run (the process is killed, defers don't fire). This is a robustness/correctness-under-load gap, not a micro-optimization.
**Fix:** Stream: compute SHA-256 with `io.Copy(hash, file)` and `tar`-copy entries from the source files (`io.Copy(tw, f)`) instead of `ReadFile`; on restore, stream the verified volume entries straight to their temp tar paths during the (second) pass instead of holding them in `extracted`. The `OpenArchive func() (io.ReadCloser, error)` seam was explicitly designed for a two-pass read (see IN-03) — use it.

## Info

### IN-01: assertBackupOutputInside is a vacuous guard

**File:** `cmd/villa/backup.go:91-95, 241-260`
**Issue:** The "traversal guard" validates `absOut` against `parent := filepath.Dir(absOut)` — a path is by construction inside its own parent, so `rel` is always the basename and the guard can never fire (the user legitimately controls `-o` anyway). Dead code that lends false assurance via its T-16-02a comment.
**Fix:** Remove it, or guard against something real (e.g. refuse writing inside the villa data store / unit dirs).

### IN-02: Post-backup "recall state included" line is a TOCTOU re-stat, not the core's actual decision

**File:** `cmd/villa/backup.go:227-231`
**Issue:** Inclusion is reported from a fresh `os.Stat(in.RecallStatePath)` after `Backup()` returns; the core decided inclusion via its own `ReadFile`/`FileMissing` during assembly. A file created/removed between the two reads makes the printed claim disagree with the archive.
**Fix:** Have `backup.Result` carry a `RecallStateIncluded bool` (the core knows), mirroring `QdrantRestored` on the restore side.

### IN-03: Stale doc — `OpenArchive` claims Restore reads the archive twice; the implementation is single-pass

**File:** `internal/backup/restore.go:44-47, 432-460`
**Issue:** The field comment ("Restore calls it TWICE… once to verify… once to extract") no longer matches `readAndVerify`, which opens once and buffers everything (see WR-06).
**Fix:** Update the comment, or restore the two-pass design as part of the WR-06 streaming fix.

### IN-04: Residency-under-load sample is not actually "verifiably IN FLIGHT"

**File:** `cmd/villa/doctor.go:451-483`
**Issue:** The sample branch launches the drive request asynchronously and immediately samples journal/GTT; nothing verifies the request is still executing at sample time (a fast embed response can complete before the sample runs), and the sample can also fire after `residencySampleAfter` **failed** requests (`completed` counts errors), i.e. with no demonstrated load. The `driveErrs>0 && PASS → WARN` downgrade catches the second case, but the comment's "verifiably IN FLIGHT" claim overstates the first.
**Fix:** Soften the comment, or gate the sample on `residencySampleAfter` *successful* completions and add a small post-launch delay/handshake before sampling.

### IN-05: recallGate takes an ad-hoc writer interface instead of io.Writer

**File:** `cmd/villa/recall.go:219`
**Issue:** `errOut interface{ Write([]byte) (int, error) }` is a structural re-spelling of `io.Writer` (which the file does not import here despite other files in the package using it).
**Fix:** `errOut io.Writer`.

### IN-06: `fmt.Sprintf("%s", s.Offload.Status)` instead of calling String()

**File:** `cmd/villa/status.go:146`
**Issue:** Redundant Sprintf of a Stringer (staticcheck S1025 territory; `staticcheck` is enabled in `.golangci.yml`).
**Fix:** `offloadCell = s.Offload.Status.String()`.

### IN-07: Always-true condition in liveEmbedModelPresent

**File:** `cmd/villa/install_memory.go:101`
**Issue:** `fi.Size() >= 0 && uint64(fi.Size()) == nomicEmbedShard.SizeBytes` — the first conjunct is always true for `os.FileInfo.Size()` of a stat'd path. Also no `fi.Mode().IsRegular()` check (a directory could theoretically satisfy the size compare).
**Fix:** `return fi.Mode().IsRegular() && uint64(fi.Size()) == nomicEmbedShard.SizeBytes`.

### IN-08: Memory-health cache refresh holds the mutex through up to two 10s podman probes; cold-cache `villa status` always spawns a container pair

**File:** `cmd/villa/status.go:276-364`
**Issue:** `memoryHealthSnapshot` holds `memoryHealthMu` while running both `podman run --rm` probes sequentially (worst case ~2×`memoryProbeTimeout` = 20s with both services down), stalling every concurrent `/api/status` request in the dashboard for that window; the one-shot `villa status`/`villa doctor` CLI always starts with a cold cache, so each invocation pays the two container spawns. Bounded and documented (OQ2), so informational — but the lock could be released during the probes (probe outside the lock, write-back under it) for free.
**Fix:** Snapshot-and-release: read the cache under the lock, run the probe pair unlocked (with a single-flight guard), write back under the lock.

### (Note) status.Run memory-row fallthrough

`internal/status/status.go:442-470`: if a caller renders memory-on units but leaves `QdrantService`/`EmbedService` empty, the qdrant/embed rows fall through to the GPU branch (generic chat-endpoint health + `RunningOffloadVerdict`) — the exact Phase-22 misclassification, resurrected for any future wiring that forgets the two fields. Current callers (liveStatusDeps, dashboard via liveStatusDeps) always set them, so this is a latent-only hazard; consider deriving the match from the unit name (`strings.HasPrefix(svc, ...)` is forbidden by the literal rule, so thread the names through RenderInput/units instead) or documenting the invariant on Deps.

---

_Reviewed: 2026-06-10T22:01:09Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
