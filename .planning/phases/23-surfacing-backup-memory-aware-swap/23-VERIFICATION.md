---
phase: 23-surfacing-backup-memory-aware-swap
verified: 2026-06-10T23:59:00Z
status: passed
score: 24/24 must-haves verified
overrides_applied: 0
---

# Phase 23: Surfacing, Backup & Memory-Aware Swap — Verification Report

**Phase Goal:** The memory stack becomes observable, recoverable, and swap-safe — health rows appear in `status` + the dashboard, `backup`/`restore` cover the Qdrant volume safely, and `villa model swap` guards the dimension-mismatch hazard — landing the milestone's single byte-frozen contract evolution exactly once.
**Verified:** 2026-06-10T23:59:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

#### Roadmap Success Criteria (contract)

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| SC1 | `villa status` + dashboard show memory-stack health (Qdrant + embeddings rows, active embedding model) as append-only schema-bumped contract (v2→3, golden re-frozen once); non-GPU services fold health without spurious offload PASS/FAIL | ✓ VERIFIED | `internal/status/status.go:171` `reportSchemaVersion = 3`; `MemoryInfo` struct (:183) with `embedding_model`/`embedding_dim`/`recall_state`/`embedding_skew`; `Report.Memory *MemoryInfo` (:149, append-only above SchemaVersion); goldens `status.json.golden` + `status-memory.json.golden` both `"schema_version": 3`; memory golden shows `villa-qdrant.service`/`villa-embed.service` rows with own `health` and `offload_applies: false` ("N/A — this service has no GPU offload"); dashboard `renderMemory` reads `report.memory` off the existing `/api/status` poll (`dashboard.js:299,758`); on-hardware drill: stopped villa-embed → `health=down`, overall FAIL (23-05-SUMMARY, T-23-01 closed) |
| SC2 | `villa backup` includes the Qdrant volume; `villa restore` clean-recreates before import; embedding dimension in manifest; skew WARNs not silently corrupts | ✓ VERIFIED | `internal/backup/manifest.go:33` `backupSchemaVersion = 2`, `EntryQdrantVolume`/`EntryRecallState` (:47-48), `embedding_model`/`embedding_dim`/`recall_schema_version` fields (:119-124); `backup.go:172-184` qdrant quiesce (Stop → VolumeExport → restart frame); `restore.go:239-250` `cleanRecreateThenImport` (VolumeRm → ReconcileAndWrite → EnsureVolume → VolumeImport) for BOTH volumes forward and rollback; skew WARN+confirm tests `TestRestoreWarnSkewConsentDeniedRefuses`/`TestRestoreWarnSkewBypassProceeds`; live drill: real volume backed up + restored, retrieval intact, skew WARN fired on provocation (23-05-SUMMARY) |
| SC3 | `villa model swap` memory-aware: embedding-change guard (no auto-reindex), chat swap leaves embedding model + vectors intact | ✓ VERIFIED | `cmd/villa/recall.go:372` D-10 refusal: `!rebuild && recall.EmbeddingSkew(...) == SkewMismatch` → refuse-with-remediation + `exitBlocked`, placed after fail-closed `readState` and BEFORE the stamp overwrite; `--rebuild` sanctioned bypass; `cmd/villa/install_memory.go:159-170` read-only WARN, never mutates; `internal/orchestrate/memory_test.go:174` `TestRenderChatSwapLeavesMemoryUnitsByteIdentical`; `modelswap_test.go:176` `TestSwapDepsSurfaceRestartIsOnlyServiceMutator`; live drill: memory units sha256-identical across chat swap, refusal + --rebuild proven (OQ4 closed) |

#### Plan-Level Must-Have Truths

| # | Plan | Truth | Status | Evidence |
| --- | ---- | ----- | ------ | -------- |
| 1 | 23-01 | status --json reports schema_version 3; memory off differs from v2 only in version field | ✓ VERIFIED | Both goldens at v3; `status.json.golden` carries no `memory` key (omitempty); golden test discipline byte-freezes both |
| 2 | 23-01 | Memory rows show OWN per-service health + N/A offload not folded into verdict | ✓ VERIFIED | `status.go:448-463` `QdrantHealth`/`EmbedHealth` per-service branches; golden rows `offload_applies: false`; drill negative control: stopped embed never ready |
| 3 | 23-01 | Report carries memory section: model+dim, recall summary, skew only on confident mismatch | ✓ VERIFIED | `MemoryInfo` + `status.go:410` `memoryInfo(cfg, d.ReadRecallState)`; `:559` skew set only on `SkewMismatch` |
| 4 | 23-01 | doctor no longer emits offload:villa-qdrant/villa-embed findings; doctor goldens re-frozen | ✓ VERIFIED | `memoryOffloadDownRanked` absent from `internal/doctor/` + `cmd/villa/` (grep: not found); `doctor-memory*.golden` fixtures present |
| 5 | 23-02 | backup quiesces villa-qdrant, exports volume as optional tar entry, records model+dim+recall schema in manifest | ✓ VERIFIED | `backup.go:172-184` quiesce frame; `cmd/villa/backup.go:193` `in.QdrantVolumeName = orchestrate.QdrantVolumeName()` (seam-sourced, D-05); manifest fields above SchemaVersion |
| 6 | 23-02 | restore clean-recreates Qdrant volume before import; rollback covers second volume with same ordering | ✓ VERIFIED | `restore.go:239-250` generalized helper; `:312,319` rollback re-imports both volumes; `TestRestoreQdrantForwardFailureRollsBackBothVolumes` |
| 7 | 23-02 | Memory-free backup restores cleanly, never touches existing Qdrant volume; 2×2 matrix per D-07 | ✓ VERIFIED | `TestRestoreQdrantMatrix` (restore_test.go:684) covers all four cells; `restore.go:85` memory-free archives never touch Qdrant data |
| 8 | 23-02 | Confident model/dim mismatch at restore WARNs + requires confirm; old manifests no false alarm | ✓ VERIFIED | `CompareSkew` embedding warning in backup.go; `TestRestoreWarnSkew*` pair; omitempty fields → empty stamp = typed-Unknown, no alarm |
| 9 | 23-02 | v1 backups remain restorable after bump to 2 | ✓ VERIFIED | Gate `m.SchemaVersion <= backupSchemaVersion` (`restore.go:505`); `TestRestoreV1ManifestStillRestores` (restore_test.go:958) |
| 10 | 23-03 | Dashboard shows memory health rows (auto-rendered) + Memory panel with model/dim/chats/last-indexed/state badge | ✓ VERIFIED | Rows free via existing renderHealth over v3 `report.services`; `dashboard.html:64` `id="memory-panel" hidden`; `renderMemory` (dashboard.js:299); operator visually approved in drill |
| 11 | 23-03 | Memory off → dashboard pixel-identical (panel ships hidden, stays hidden) | ✓ VERIFIED | Panel `hidden` in static shell; `renderMemory` re-hides when `report.memory` absent (dashboard.js:301) |
| 12 | 23-03 | Typed-Unknown gray badges; skew amber mismatch badge only on confident mismatch | ✓ VERIFIED | `dashboard.js:345-349`: skew row rendered ONLY when `embedding_skew === "mismatch"`, badge class "warn" (amber, not red) |
| 13 | 23-03 | No new fetch/endpoint/probe — reads report.memory from existing /api/status poll (D-03) | ✓ VERIFIED | `renderMemory(report)` called from existing poll handler (dashboard.js:755-758); `api_test.go` memory passthrough test (memory-ON → field present untouched; OFF → absent) |
| 14 | 23-04 | Chat swap leaves memory stack intact — byte-identity + single-restart proofs | ✓ VERIFIED | `TestRenderChatSwapLeavesMemoryUnitsByteIdentical` (memory_test.go:174); `TestSwapDepsSurfaceRestartIsOnlyServiceMutator` (modelswap_test.go:176) |
| 15 | 23-04 | recall index REFUSES (exitBlocked) on confident skew; --rebuild is sanctioned bypass | ✓ VERIFIED | `recall.go:372-376` refusal naming both identities + consequence + fixes, returns `exitBlocked`; drill: refusal observed live (exit 1), --rebuild completed + re-stamped |
| 16 | 23-04 | No recorded stamp raises no alarm; no guard ever mutates (no auto-reindex anywhere) | ✓ VERIFIED | `EmbeddingSkew`: empty `st.EmbeddingModel` → `SkewUnknown` (skew.go:32-33); refusal placed before all state/KB mutation; WR-01 fix made multi-human refusal side-effect-free too |
| 17 | 23-04 | install memory flow WARNs read-only on same confident mismatch | ✓ VERIFIED | `warnRecallEmbeddingSkew` (install_memory.go:159): single-implementation comparison, WARN with remediation, never blocks/mutates |
| 18 | 23-05 | Live box: schema 3, real per-service health (stopped embed NOT PASS), dashboard Memory panel renders | ✓ VERIFIED | Drill transcripts in 23-05-SUMMARY (2026-06-10 21:36–21:45 UTC); operator checkpoint:human-verify APPROVED |
| 19 | 23-05 | Real backup of populated Qdrant volume restores through clean-recreate; retrieval works; skew WARN fires | ✓ VERIFIED | Drill: safety + drill archives, manifest v2 with qdrant-volume.tar + nomic-embed-text-v1.5/768; restore + retrieval spot-check; skew WARN+confirm transcript captured |
| 20 | 23-05 | Live swap drill: chat swap leaves memory intact; recall index refuses on skew; --rebuild proceeds (OQ4) | ✓ VERIFIED | Drill: memory units sha256-identical; refusal exit 1 = exitBlocked; --rebuild bypassed refusal, KB id-preserving clean-replace (id 23e667e7… unchanged), fresh stamp |
| 21 | 23-05 | Box returned to prior state (backend, real data, no drill artifacts) | ✓ VERIFIED | Drill: config sha256 byte-identical post-drill, backend rocm untouched, drill weights removed, doctor PASS |

**Score:** 24/24 (3 roadmap SCs + 21 plan truths; plan truths refine the SCs by design)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/recall/skew.go` | EmbeddingSkew single-implementation D-10 comparison | ✓ VERIFIED | `SkewState` + `SkewUnknown`/`SkewMatch`/`SkewMismatch` consts + `EmbeddingSkew` func; consumed by status (:559), recall.go (:372), install_memory.go (:167) — three consumers, one implementation |
| `internal/status/status.go` | MemoryInfo, Memory field, `reportSchemaVersion = 3`, qdrant/embed branches | ✓ VERIFIED | All present; Deps gains `QdrantService`/`EmbedService`/`QdrantHealth`/`EmbedHealth`/`ReadRecallState` |
| `cmd/villa/status.go` | live probes + TTL cache + recall-state seam | ✓ VERIFIED | `liveQdrantHealth` (:380), `liveEmbedHealth` (:389), `memoryHealthTTL = 15s` (:276, OQ2-confirmed on hardware), `liveReadRecallState` (:395), wired into liveStatusDeps (:230-232) |
| `cmd/villa/testdata/status-memory.json.golden` | byte-frozen memory-on v3 fixture | ✓ VERIFIED | 5 service rows incl. qdrant/embed (health own, offload N/A), memory section, schema_version 3 |
| `internal/backup/manifest.go` | Entry consts, embedding fields, `backupSchemaVersion = 2` | ✓ VERIFIED | All present, fields above SchemaVersion (append-only), bump doc-commented |
| `internal/backup/restore.go` | generalized cleanRecreateThenImport, qdrant capture/rollback, 2×2 matrix | ✓ VERIFIED | Plus CR-01 (rollback quiesce, RollbackIncomplete) and WR-02 (QdrantVolumeUnknown fail-closed refuse) |
| `internal/backup/backup.go` | quiesce+export frame, CompareSkew embedding warning, CurrentInstall fields | ✓ VERIFIED | Plus WR-06 streaming `Deps.OpenFile` seam for volume tars |
| `cmd/villa/podman_volume.go` | volumeExistsArgs pure builder + existence check | ✓ VERIFIED | Fail-soft `volumeExists` for backup (:43-53) + tri-state `volumeExistsTri` for destructive restore (:71, WR-02) |
| `internal/dashboard/assets/dashboard.html` | memory-panel section, hidden by default | ✓ VERIFIED | `:64` exact attributes per plan |
| `internal/dashboard/assets/dashboard.js` | renderMemory, XSS-safe, show/hide on presence | ✓ VERIFIED | `:288-349` createElement/textContent idiom; plus WR-05 null-fetch early return (:524-532) |
| `internal/orchestrate/memory_test.go` | D-09 render byte-identity invariant | ✓ VERIFIED | `TestRenderChatSwapLeavesMemoryUnitsByteIdentical` (:174) |
| `internal/modelswap/modelswap_test.go` | D-09 restart-scope invariant | ✓ VERIFIED | `TestSwapDepsSurfaceRestartIsOnlyServiceMutator` (:176) |
| `cmd/villa/recall.go` | D-10 fail-closed refusal between readState and stamp overwrite | ✓ VERIFIED | `:357-376` — exact placement per plan, Pitfall 6 honored |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| cmd/villa/status.go (liveStatusDeps) | status.Deps.QdrantHealth/EmbedHealth/ReadRecallState | Deps func fields (D-03, same seams reach dashboard) | ✓ WIRED | status.go:230-232 |
| internal/status/status.go (Run) | recall.EmbeddingSkew | skew indicator in MemoryInfo | ✓ WIRED | status.go:559, mismatch-only |
| cmd/villa/backup.go | orchestrate.QdrantVolumeName | seam-sourced, never a literal (D-05) | ✓ WIRED | backup.go:155,193; TestSeamGrepGate green |
| internal/backup/restore.go | Deps.VolumeRm/EnsureVolume/VolumeImport | clean-recreate ordering, forward + rollback (D-07) | ✓ WIRED | restore.go:239-250, 312, 319 |
| cmd/villa/restore.go | recall.RecallStatePath | RecallDestPath for recall-state.json entry | ✓ WIRED | restore.go:240 |
| dashboard.js | /api/status existing poll | report.memory from v3 contract | ✓ WIRED | dashboard.js:301,755-758; no new fetch/endpoint |
| cmd/villa/recall.go (runRecallIndex) | recall.EmbeddingSkew | single-implementation comparison vs recall-state stamp | ✓ WIRED | recall.go:372 |
| cmd/villa/install_memory.go | recall Load + EmbeddingSkew | read-only WARN surface (D-10/D-11) | ✓ WIRED | install_memory.go:159-170 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| dashboard.js renderMemory | report.memory | /api/status → status.Run → memoryInfo(cfg, ReadRecallState) → live recall-state.json | Yes (drill: live values nomic-embed-text-v1.5/768/indexed/1 matched recall-state.json) | ✓ FLOWING |
| status memory rows | ss.Health | liveQdrantHealth/liveEmbedHealth → in-network /readyz + /health probes (TTL 15s) | Yes (drill: stopped embed → down, restart → ready) | ✓ FLOWING |
| backup manifest embedding fields | EmbeddingModel/Dim | config.toml (single source of truth) + recall schema accessor, memory-on only | Yes (live manifest: model + 768 + recall_schema_version 1) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Full workspace gate (vet + tests, incl. TestSeamGrepGate + goldens) | `make check` | All 22 packages ok | ✓ PASS |
| Review-fix commits exist | `git cat-file -t` 98433fc c98dcaa 045642e f9d6a05 b7a46a4 9067868 6a1e190 | All 7 are commits | ✓ PASS |
| Doctor predicate removal | grep memoryOffloadDownRanked internal/ cmd/ | Not found (removed per OQ3) | ✓ PASS |

On-hardware behaviors (live probes, volume export/import, --rebuild) were proven in the Plan 23-05 drill with transcripts recorded in 23-05-SUMMARY.md — not re-run here (drill mutates real operator data by design; re-running is out of verification scope).

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes exist or are declared in this project — N/A. The phase's runnable verification is the Go test suite (run once above) plus the operator-approved on-hardware drill.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| CTRL-02 | 23-01, 23-03, 23-05 | status + dashboard surface memory-stack health, append-only schema-bumped contract, golden re-frozen once | ✓ SATISFIED | SC1 evidence; single 2→3 bump in Plan 23-01 only (review confirms no further schema change in 23-02..05 or fix pass) |
| CTRL-04 | 23-02, 23-05 | backup/restore cover Qdrant volume, clean-recreate-before-import, embedding dim in manifest, skew warning | ✓ SATISFIED | SC2 evidence; live restore drill |
| CTRL-05 | 23-04, 23-05 | model swap memory-aware: warns/guards on embedding change, no auto-reindex | ✓ SATISFIED | SC3 evidence; D-09/D-10/D-11 tests + live drill |

No orphaned requirements: REQUIREMENTS.md maps exactly CTRL-02/04/05 to Phase 23 and all three are claimed by plans.

### Post-Review Fix Regression Check (23-REVIEW-FIX.md)

All 7 fixes (CR-01, WR-01..WR-06) verified present in the codebase:

| Fix | Evidence in code |
| --- | ---------------- |
| CR-01 rollback quiesce + preserve rollback tars | `restore.go:275-281,341` (RollbackIncomplete); `cmd/villa/restore.go:73-123` (preserveTmp, recovery hint); `TestRestoreProveFailRollbackQuiescesBeforeVolumeRm` |
| WR-01 guard before mutation | recall.go: multi-human refusal at step 3a before readState/KB; side-effect-free refusal test present |
| WR-02 tri-state existence for destructive restore | `podman_volume.go:71` volumeExistsTri; `restore.go:101-107,193` QdrantVolumeUnknown fail-closed; `TestRestoreQdrantExistsUnknownFailsClosed` |
| WR-03 RecallStatePath gated on MemoryEnabled | `cmd/villa/backup.go:196-204` with doc comment naming the fix |
| WR-04 temp+rename archive assembly | `cmd/villa/backup.go:122-232` CreateTemp + os.Rename |
| WR-05 dashboard null-fetch != empty catalog | `dashboard.js:524-532` early return on null |
| WR-06 streaming backup seam | `backup.go:223-225` Deps.OpenFile streaming path; `TestBackupStreamsVolumeTars` |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No TBD/FIXME/XXX/placeholder markers in any phase-modified file | — | — |

Informational residuals (documented, not gaps):
- WR-06 restore-side buffering remains (bounded by existing 8 GiB/entry, 16 GiB/archive tarutil caps) — explicitly documented deferred residual in 23-REVIEW-FIX.md with a recommended follow-up task; not a phase must-have.
- IN-01..IN-08 review Info findings deliberately left documented in 23-REVIEW.md.
- Pre-existing gofmt violations in `cmd/villa/bench_compare.go` / `cmd/villa/verify_memory_test.go` (deferred-items.md) — pre-date the phase, untouched by Phase 23.

### Human Verification Required

None outstanding. The phase's only human-verification item (Memory panel renders per 23-UI-SPEC + drill transcript review) was the checkpoint:human-verify task in Plan 23-05 and was completed during execution — operator typed "approved" (23-05-SUMMARY: "Task 2 = checkpoint:human-verify, APPROVED"). The on-hardware drill itself was the live UAT: dashboard visuals, real backup/restore, swap guards, and box restoration were all human-witnessed and signed off.

### Gaps Summary

No gaps. All three roadmap success criteria are observably true in the codebase, all 21 plan-level must-have truths verified against actual code (not SUMMARY claims), all key links wired, the single 2→3 schema bump landed exactly once with goldens re-frozen, all 7 review fixes are present and `make check` is green, and the on-hardware drill closed the live-behavior questions (OQ2 TTL=15s, OQ4 --rebuild) with operator sign-off.

---

_Verified: 2026-06-10T23:59:00Z_
_Verifier: Claude (gsd-verifier)_
