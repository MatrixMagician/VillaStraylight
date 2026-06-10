# Phase 23: Surfacing, Backup & Memory-Aware Swap - Research

**Researched:** 2026-06-10
**Domain:** Brownfield Go control-plane integration — status/dashboard contract evolution, podman volume backup/restore, swap guards (no new external technology)
**Confidence:** HIGH (all findings verified by direct codebase reads in this session)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Status/dashboard surfacing shape (CTRL-02)
- **D-01:** `villa-qdrant` + `villa-embed` appear as **`ServiceStatus` rows appended to `Report.Services`** using the existing **non-GPU row pattern** (the OWUI/dashboard-service rows: health/active folded into the verdict, `OffloadApplies=false`, no spurious offload PASS/FAIL — exactly what SC#1 demands). Rows are emitted **only when `memory_enabled=true`** (opt-in discipline: memory-off output stays v1.2-shaped apart from the version field, mirroring Phase 19/20/22).
- **D-02:** A new **append-only memory field/section tail-appended above `SchemaVersion`** carries: active embedding model (+ dimension), and the **recall-index summary** (indexed count / last-indexed / staleness read from `recall-state.json`, typed-Unknown WARN-style when absent/unreadable — never silently stale). Including recall state honors the Phase-21 deferral under the SAME single bump — there is no second evolution available later. Fields are `omitempty`-style so memory-off output stays shape-stable. Exact field names/JSON keys are planner's call.
- **D-03:** The dashboard **folds the same `status` core** (composition over re-implementation): the API exposes the new fields; the embedded SPA renders memory rows/panel. No parallel probe logic in `internal/dashboard`. The dashboard-restart-after-rebuild gotcha applies to verification.

#### Single contract evolution mechanics (CTRL-02)
- **D-04:** Exactly ONE `status.Report` evolution: `reportSchemaVersion` 2→3, **unconditional** (version reflects the contract, not the config); all new fields tail-appended above `SchemaVersion` (append-only — nothing above moves); `status --json` + dashboard goldens re-frozen **once**, intentionally, with `-update`. The ROADMAP's "single byte-frozen contract evolution" line refers to THIS contract; the backup manifest's append-only growth (D-06) follows the manifest's own self-version discipline and is not a second status-contract evolution.

#### Backup/restore of the Qdrant volume (CTRL-04)
- **D-05:** **Extend the existing `internal/backup` core** (no parallel path): the Qdrant volume name enters `Deps`/Input **seam-sourced from `orchestrate`** (never a literal — same rule as `OpenWebUIVolumeName`), exported as an additional **optional** tar entry when `memory_enabled=true` and the volume exists. `villa-models` stays NEVER exported. **`recall-state.json` is added as an optional source entry** (same shape as usage.json/bench-reports.jsonl), per the Phase-21 deferral.
- **D-06:** The **manifest gains append-only fields above its `SchemaVersion`**: embedding model id + **embedding dimension** (SC#2's skew key), plus whatever presence marker restore needs to distinguish memory-bearing backups. Exact names planner's call. Whether `backupSchemaVersion` bumps is planner's call per the manifest's own rule (append-only = non-breaking; bump on breaking change) — old backups MUST remain restorable either way.
- **D-07:** Restore **clean-recreates the Qdrant volume before import** via the existing `cleanRecreateThenImport` mechanism (VolumeRm → ReconcileAndWrite → EnsureVolume → VolumeImport) — no stale-vector leak; the rollback path covers the new volume with the SAME ordering. Backups **without** the memory entry restore cleanly and do NOT touch any existing Qdrant volume (entry optional, honest report of what was/wasn't restored).
- **D-08:** **Dimension/version skew at restore:** manifest embedding model/dim vs the current config → confident mismatch **WARNs + requires confirm** (mirror the existing OWUI image-skew confirm pattern in `restore.go`), with remediation text naming the consequence (retrieval corrupt until re-index) and the fix (`villa recall index` re-index after restore / align config). Never silent, never auto-reindex. Missing/unparseable manifest fields (old backups) → typed-Unknown WARN, not a hard block.

#### Memory-aware model swap (CTRL-05)
- **D-09:** **Chat-model swap leaves the memory stack intact** — `villa model swap` (and the dashboard swap path, which shares `internal/modelswap`) must not touch the embedding model, the `villa-qdrant`/`villa-embed` units, or the vector collections. This is asserted by tests on the swap's render/reconcile scope (memory units byte-identical across a chat swap) — an invariant proof, not a new behavior.
- **D-10:** The **embedding-model-change guard fires where config changes take effect, NOT via a new swap verb.** Today the embedding model changes only by editing `config.toml`. The guard compares configured `EmbeddingModel`/dimension against the **recorded state** — the `recall-state.json` stamp (`cmd/villa/recall.go:343`) and the Phase-18 `internal/memory` pin — and on confident mismatch: `villa recall index` **refuses** (fail-closed), while install/up/status paths **WARN with remediation** (no auto-reindex; remediation = explicit re-index or revert the config). Typed-Unknown (no recorded state yet) → no false alarm. The exact set of guarded verbs beyond `recall index` is planner's call within this posture.
- **D-11:** **No auto-reindex anywhere** — guards report/refuse, never mutate (mirrors Phase 22 D-10: diagnose, don't mutate). Re-indexing stays an explicit, operator-driven `villa recall index` action.

### Claude's Discretion
- Exact new `Report`/manifest field names and JSON keys, golden fixture layout, dashboard panel layout/placement for the memory rows.
- Whether `backupSchemaVersion` bumps for the append-only manifest growth.
- The exact verb set carrying the D-10 WARN (beyond the `recall index` refusal) and the remediation strings.
- Whether the recall-index summary in status reads `recall-state.json` via a new injected Deps func or reuses the `internal/recall` store loader directly (pure-core rule: no direct file I/O inside `status.Aggregate` — keep it behind the Deps seam either way).
- Plan sequencing — status surfacing (CTRL-02), backup (CTRL-04), and swap guards (CTRL-05) are largely independent; on-hardware verification lands last (v1.x discipline).

### Deferred Ideas (OUT OF SCOPE)
- `villa model swap --embedding <id>` sanctioned embedding-swap verb (with guided re-index) — new capability; backlog.
- Auto-reindex on schedule / on swap / on restore — backlog (guards never mutate in v1.3).
- GPU passthrough for `villa-embed` + re-measured footprint — backlog (carried from Phase 22).
- Reranker/hybrid search (RAG-Q-01), SearXNG, multi-user/remote — v2 per REQUIREMENTS.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CTRL-02 | `villa status` + dashboard surface memory-stack health (Qdrant + embeddings rows, active embedding model) as an append-only, schema-bumped contract change (golden re-frozen once) | §CTRL-02 map: exact non-GPU branch sites (`status.go:364`, `:404`), tail-append point (`:140-156`), classification fix for the per-row health false-green (`:376`), probe mechanism precedent (`runProbeCurl`), golden inventory + doctor ripple |
| CTRL-04 | `villa backup`/`restore` cover the Qdrant volume — clean-recreate-before-import, embedding dimension in manifest, version-skew warning | §CTRL-04 map: `BackupInput` sources table extension, `cleanRecreateThenImport` generalization, `CompareSkew` warning + Consent gate reuse, pre-laid `orchestrate.QdrantVolumeName()` + `recall.SchemaVersion()` accessors |
| CTRL-05 | `villa model swap` memory-aware — warns/guards when changing the embedding model would invalidate vectors (dimension mismatch / no auto-reindex) | §CTRL-05 map: `modelswap.Run` restart-scope proof, render byte-identity invariant test design, guard insertion point in `runRecallIndex` before the `recall.go:343` stamp overwrite |
</phase_requirements>

## Summary

This is a pure brownfield integration phase: **zero new dependencies, zero new external surfaces**. Every mechanism Phase 23 needs already exists in-repo behind proven seams, and several were explicitly pre-laid for this phase: `orchestrate.QdrantVolumeName()` (memory.go:106, "so the Phase-23 backup/restore flow reads the resolved volume name"), `recall.SchemaVersion()` (store.go:40, "exposes the recall store's OWN schema version to the Phase-23 backup manifest"), the `recall.State.EmbeddingModel/EmbeddingDim` stamp (`cmd/villa/recall.go:343-344`, comment "Phase-23 skew guards"), and doctor's down-rank comment ("the status-side N/A fix is Phase 23", doctor.go:363).

The single most load-bearing discovery: **memory service rows already flow into `status.Run` today** — `serviceUnits(units)` derives rows from the rendered units, and with `memory_enabled=true` Render appends `villa-qdrant.container`/`villa-embed.container`. The Phase 23 work is *classification*, not row creation: those rows currently fall into the GPU branch at `status.go:376-392`, which (a) probes the **chat** endpoint for their health (the carried false-green — a stopped villa-embed shows `health PASS`) and (b) emits an offload WARN. The fix mirrors the OWUI/dashboard branches: match the two memory service names (Deps-supplied, derived via the established `unitServiceName(orchestrate.QdrantContainerUnitName())` pattern from `cmd/villa/doctor.go:215-217`) and give each a per-service health seam + `naOffloadVerdict()`. This ripples into doctor: doctor only creates `offload:<svc>` findings for `OffloadApplies=true` rows (doctor.go:465), so all three `doctor-memory*` goldens change and the `memoryOffloadDownRanked` predicate (doctor.go:375) goes vestigial — a deliberate, documented consequence, not an accident.

For CTRL-04, the backup core's required/optional source-entry table (`backup.go:134-139`), the `cleanRecreateThenImport` closure (`restore.go:171-185`), the rollback frame, and the `CompareSkew` WARN+Consent gate (`backup.go:283-313` + `restore.go:135-140`) extend directly; the main design care points are quiescing `villa-qdrant.service` before exporting its volume (live RocksDB/WAL ≈ the same torn-copy hazard the OWUI SQLite quiesce exists for), generalizing the single-volume closure to two volumes, and the asymmetric cases (memory-bearing backup onto memory-off install and vice versa). For CTRL-05, the chat-swap invariant is already structurally true (`modelswap.Run` restarts only `InstallServiceName`; memory units derive only from memory config fields) — the work is proving it with byte-identity tests, plus inserting the skew comparison in `runRecallIndex` step 4 *before* the stamp overwrite.

**Primary recommendation:** Plan three largely independent plan-streams (status 2→3 + dashboard; backup/restore; swap guards), land the schema bump + ALL new Report fields in one plan with one `-update` re-freeze, and run the on-hardware drill (real populated-Qdrant backup/restore + status rows + swap) last.

## Project Constraints (from CLAUDE.md)

| Directive | Source | Phase-23 consequence |
|-----------|--------|---------------------|
| Config is the single source of truth; Quadlet units regenerated, never hand-edited | CLAUDE.md Conventions | Restore renders units from the RESTORED config; guards compare config vs recorded state |
| Dashboard binary trap: restart `villa-dashboard.service` after `make build` | CLAUDE.md gotchas | Required during dashboard verification (also CONTEXT Specifics) |
| `TestSeamGrepGate`: backend/image literals stay behind `internal/inference` + `internal/orchestrate`; gate walks `internal/` AND `cmd/villa` | CLAUDE.md gotchas | No image literals or `exec.Command("podman"...)` in status/backup/modelswap cores; names via orchestrate accessors |
| `--json`/dashboard contracts byte-frozen by goldens; evolve append-only + schema-bump; refreeze with `-update` | CLAUDE.md gotchas | The 2→3 bump is the milestone's ONE such evolution |
| Offload is offload-asserting, never liveness; silent CPU fallback = FAIL, never false-green | CLAUDE.md gotchas | Memory rows must be `OffloadApplies=false` (no spurious offload), and their health must be REAL per-service health (fix the false-green) |
| Pure-core + injectable `live*Deps` seams; `orchestrate` is the only intentionally impure module | CLAUDE.md architecture | All new probes/reads enter `status.Deps`/`backup.Deps` as func fields wired in cmd/villa |
| No shell interpolation; fixed-arg `exec.Command` only | CLAUDE.md constraints | Reuse `runProbeCurl`/`podmanVolume` fixed-arg seams |
| Loopback-only binds; no telemetry; single static binary | CLAUDE.md constraints | No new ports, no new outbound; memory services stay container-DNS-only |
| GSD workflow enforcement: file changes via GSD commands | CLAUDE.md | Execution happens via `/gsd-execute-phase` |
| Go 1.26+; `make check` = vet + test as pre-commit gate | CLAUDE.md build | Validation commands below |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Memory-row classification + Report shape (2→3) | Pure core (`internal/status`) | cmd tier (`liveStatusDeps`) supplies seams | Read-model logic is pure; probes/names injected (D-12 precedent) |
| Per-service health probes (qdrant `/readyz`, embed `/health`) | cmd tier (`cmd/villa`, `runProbeCurl` family) | — | Containers are container-DNS-only; probing requires `podman run` (host-touching, seam-locked at cmd tier) |
| Recall summary read (`recall-state.json`) | cmd tier seam → pure `recall.Load` | `internal/status` consumes typed value | Pure-core rule: no file I/O inside `status.Run`; `recall.Load` is already pure-over-Deps |
| Dashboard memory surfacing | `internal/dashboard` (API) + embedded SPA | — | Folds the SAME `status.Report` (D-03); SPA renders `report.memory` + auto-renders new service rows |
| Qdrant volume + recall-state backup entries | Pure core (`internal/backup`) | cmd tier wires volume/service names + paths | Existing required/optional entry pattern; names seam-sourced from `orchestrate`/`recall` |
| Clean-recreate + rollback for second volume | Pure core (`internal/backup/restore.go`) | cmd tier (`podman_volume.go` seams) | `cleanRecreateThenImport` generalizes; podman effects stay behind Deps |
| Dimension-skew WARN+confirm | Pure core (`CompareSkew`) | cmd tier (`liveSkewConsent`) | New `SkewWarning` rides the existing Consent gate unchanged |
| Chat-swap-leaves-memory-intact invariant | Tests over `internal/orchestrate` Render + `internal/modelswap` | — | Invariant proof, not new behavior (D-09) |
| Embedding-skew guard (refuse/WARN) | cmd tier verbs (`recall index`, install/up) + optional pure check helper | `internal/recall` state as the recorded truth | Guard fires where config takes effect (D-10) |

## Standard Stack

### Core

**No new libraries. No new images. No new files outside the existing module.** This phase is constrained to the existing stack [VERIFIED: go.mod read via CLAUDE.md Technology Stack; no install step exists in any decision]:

| Component | Version | Role in this phase |
|-----------|---------|--------------------|
| Go stdlib `testing` + golden fixtures | go 1.26.2 | All validation; `-update` re-freeze flag (`cmd/villa/status_test.go:298`) |
| `internal/status` | in-repo | CTRL-02 edit site (Report, Run, Aggregate) |
| `internal/backup` | in-repo | CTRL-04 edit site (BackupInput, Manifest, Restore, CompareSkew) |
| `internal/modelswap`, `internal/orchestrate`, `internal/recall`, `internal/memory`, `internal/config` | in-repo | CTRL-05 + seam sources |
| Podman CLI (fixed-arg seams) | 5.8.2 on dev box | `volume export/import/rm/create`, `run --rm` probe helper |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `runProbeCurl` helper-container health probes | `podman exec <container> curl localhost:…` | Qdrant's unprivileged image is not guaranteed to ship curl; helper-container path is the proven Phase-20/22 mechanism (doctor.go:462, install_memory.go:241) |
| Per-service health seams on `status.Deps` | `podman healthcheck`/`HealthCmd=` in Quadlet units | Would change unit goldens + add in-container tool assumptions; more moving parts than two Deps func fields |
| Recording dim in manifest (D-06, locked) | Probing the Qdrant collection dimension at restore | Locked OUT by D-06/CONTEXT External note — manifest is the authority; collection probing is new ground |

## Package Legitimacy Audit

**No external packages are installed by this phase.** Zero new Go modules (v1.3 is a zero-new-first-party-library milestone per STATE.md), zero new container images (Qdrant/embed/OWUI images already digest-pinned behind orchestrate seams). Audit table intentionally empty; the Package Legitimacy Gate does not apply.

## Architecture Patterns

### System Architecture Diagram (data flow touched by this phase)

```
                       ┌────────────────────────────────────────────────────┐
                       │ config.toml (single source of truth)               │
                       │ memory_enabled / embedding_model / embedding_dim   │
                       └──────┬─────────────────────┬───────────────────────┘
                              │                     │
              ┌───────────────▼──────┐      ┌───────▼────────────────────────┐
              │ orchestrate.Render   │      │ D-10 guards compare cfg vs     │
              │ (memory-on appends   │      │ recall-state.json stamp        │
              │ villa-qdrant/embed   │      │  - recall index: REFUSE        │
              │ units)               │      │  - install/up/status: WARN     │
              └──────┬───────────────┘      └────────────────────────────────┘
                     │ units
       ┌─────────────▼───────────────────────────────────────────┐
       │ status.Run (pure)                                       │
       │  serviceUnits → rows:                                   │
       │   villa-llama → GPU branch (offload assert)             │
       │   villa-openwebui → non-GPU branch (existing)           │
       │   villa-qdrant / villa-embed → NEW non-GPU branch       │
       │     health ← Deps seam (cmd: runProbeCurl /readyz,      │
       │              /health over villa.network)                │
       │   villa-dashboard → extra row (existing)                │
       │  + NEW Memory section (omitempty, above SchemaVersion)  │
       │     ← cfg.EmbeddingModel/Dim + ReadRecallState seam     │
       │  SchemaVersion: 3                                       │
       └────────┬──────────────────────────────┬─────────────────┘
                │ --json (golden-frozen)        │ same Report
        villa status CLI              dashboard /api/status → SPA
                                      (renderHealth auto-renders rows;
                                       new memory panel reads report.memory)

  villa backup ──quiesce OWUI──export owui vol──┐
        └─quiesce villa-qdrant──export qdrant vol (OPTIONAL entry)─┤
        └─read config/usage/bench/recall-state.json (OPTIONAL)─────┤
                                                                   ▼
                       manifest.json (+embedding_model/dim, +recall schema)
                                                                   │
  villa restore ──verify──CompareSkew (+dim-skew WARN+confirm)──capture──
        quiesce──SaveConfig──data files──cleanRecreateThenImport(owui)──
        cleanRecreateThenImport(qdrant, IF entry present)──start──prove──
        (any failure → rollback BOTH volumes via same clean-recreate order)
```

File-to-responsibility mapping is in the per-requirement maps below.

### CTRL-02 Map — status core, classification fix, tail-append, goldens

**Row production (no new row code needed):** `status.Run` derives rows from `serviceUnits(units)` (`internal/status/status.go:272-280, :348`); Render (stubbed via `Deps.Render`, live = `orchestrate.Render`) appends `villa-qdrant.container` + `villa-embed.container` when `cfg.MemoryEnabled` — so memory rows appear/disappear with config automatically, satisfying D-01's gating with zero extra logic. [VERIFIED: codebase read]

**The branch to mirror:** OWUI row branch `status.go:364-374` and dashboard self-row `:404-424` — both set `ss.Offload = naOffloadVerdict()` (`:71-77`), `OffloadApplies=false`, `OffloadOK=false`. `Aggregate` (`:438-459`) folds health+active for every row but offload only when `OffloadApplies` — the exact "fold health/active into the verdict without spurious offload" SC#1 demands. The human table already renders `N/A` for `OffloadApplies=false` (`cmd/villa/status.go:131-142`); no renderer change needed for the offload cell.

**The false-green to fix (tracked in STATE.md, graphmind 527de579):** `status.go:376` — `ss.Health = d.Health(endpoint)` probes the single **chat** endpoint for every non-OWUI container row. With memory-on, a stopped villa-embed renders `health PASS`. Fix: match memory service names BEFORE the generic branch. Established pattern for the names: `Deps` string fields like `OWUIService`/`DashboardService` (`status.go:245, :254`), wired in `liveStatusDeps` from orchestrate accessors using the `.container → .service` derivation already documented at `cmd/villa/doctor.go:295-301` (`unitServiceName(orchestrate.QdrantContainerUnitName())`, see `doctor.go:214-217`).

**Per-service health probes:** containers are container-DNS-only (no published host port — `internal/orchestrate/memory.go:119-121, :80-86`); the host cannot HTTP-probe them directly. The proven mechanism is `runProbeCurl` (`cmd/villa/install_memory.go:317-341`): fixed-arg `podman run --rm --network villa --entrypoint curl <orchestrate.EmbedImage()> …`, already used by install readiness and doctor (`doctor.go:462, :492`). Probe targets:
- Qdrant: `GET http://<cfg.QdrantAddr>:<cfg.QdrantPort>/readyz` (used at `install_memory.go:289`) → 200 = `HealthReady`.
- villa-embed: llama-server `GET http://<cfg.EmbedAddr>:<cfg.EmbedPort>/health` → 200 = ready, 503 = loading (same mapping as `liveHealthProbe`, `cmd/villa/status.go:317-333`).
Honesty nuance: a `runProbeCurl` failure conflates "podman/helper failed" with "service down"; map a *probe-could-not-run* class to `HealthUnknown` (WARN) and an HTTP-level failure to `HealthDown` where distinguishable; if not distinguishable, prefer `HealthDown` only when systemd `is-active` corroborates DOWN, else `HealthUnknown` — never fabricate a confident FAIL from an unevaluable probe. The exact mapping is planner's call within the typed-Unknown doctrine.

**Tail-append point + bump precedent:** `Report` (`status.go:90-149`): tagged fields end with `Model` (v2, Phase-15), `Usage` (v2, Phase-15), then `SchemaVersion` LAST (`:140-143`); unexported `err` after it never serializes. `reportSchemaVersion = 2` at `:156` with the version history in its doc comment (v1 = Phase-10 Backend/Image/GenTokensPerSec/ROCmReadiness tail-append; v2 = Phase-15 Usage). The Phase-22 `recommend` 1→2 bump followed the identical recipe (STATE.md [22-01]: append-only field + schema bump + ONE golden re-freeze of `recommend.golden.json`). The 2→3 recipe: insert new fields between `Usage` and `SchemaVersion`, bump the const, re-freeze once.

**Memory section shape (D-02; names planner's call):** a pointer struct field (e.g. `Memory *MemoryInfo \`json:"memory,omitempty"\``) above `SchemaVersion` is the cleanest omitempty unit — nil when memory off ⇒ memory-off JSON differs from v2 ONLY in `schema_version` (D-04). Contents: embedding model id + dim (from cfg — the same `LoadConfig` the Run body already has), and the recall summary. Recall summary source: `recall.State` (`internal/recall/store.go:67-76`) — indexed count = `len(state.Chats)`, `LastIndexStartedAt`, `LastIndexCompletedAt`, and the complete-run truth `state.LastIndexCompletedAt != "" && state.LastIndexCompletedAt >= state.LastIndexStartedAt` (the exact expression at `internal/recall/staleness.go:65`; full `Classify` needs a live OWUI chat list — too heavy for status; the villa-side truths are computable from State alone, which is what D-02 names). Seam options (Claude's discretion per CONTEXT): a `ReadRecallState func() *recall.State` Deps field wired in cmd to `recall.Load` over a file-read Deps (mirrors `liveReadUsage`, `cmd/villa/status.go:226-248`) — nil ⇒ typed-Unknown ⇒ omit/`known:false` fields. `recall.Load` fails closed to empty state on absent/corrupt/future-schema (`store.go:111-132`), so "no state yet" is distinguishable as zero-value (empty `KnowledgeID`/timestamps).

**Goldens to re-freeze (the complete inventory):**
| Golden | Test | Changes because |
|--------|------|-----------------|
| `cmd/villa/testdata/status.json.golden` | `TestStatusJSONGolden` (`status_test.go:285`, `-update`) | `schema_version` 2→3 (fixture cfg is memory-off, `newStatusDeps` `status_test.go:49`; only the version line changes if Memory is omitempty-nil) |
| (likely new) memory-on status golden | new test | The memory-on shape needs its own frozen fixture — planner's call on layout |
| `cmd/villa/testdata/doctor-memory.json.golden`, `doctor-memory-pass.golden`, `doctor-memory-residency-fail.golden` | doctor tests | All three contain `offload:villa-qdrant.service` / `offload:villa-embed.service` WARN findings [VERIFIED: grep]; doctor creates offload findings ONLY for `OffloadApplies=true` rows (`internal/doctor/doctor.go:465`), so after the N/A fix those findings disappear; the `health:villa-*` PASS lines also reflect the false-green and will track the new per-service health stubs |
| Orchestrate unit goldens | — | UNCHANGED (no unit rendering touched) |

**Doctor ripple (deliberate, document in plan):** `memoryOffloadDownRanked` (`doctor.go:375-384`) exists solely because the status-side fix was deferred to Phase 23 (comment at `:359-364`). After the fix the predicate never matches. Planner's call: keep as harmless defense-in-depth or remove with its tests; either way the doctor goldens re-freeze is a *consequence* of the status row fix, not a second contract evolution (doctor's own `SchemaVersion` shape is unchanged — findings are data).

**Dashboard fold (D-03):** `handleStatus` serializes the same `status.Run(s.statusDeps)` (`internal/dashboard/api.go:19-22`); the dashboard wires `*liveStatusDeps()` verbatim (`cmd/villa/dashboard.go:152-159`) — so every new seam added to `liveStatusDeps` reaches the dashboard automatically with zero dashboard-side probe logic. SPA: `renderHealth(services)` (`assets/dashboard.js:146-178`) iterates `report.services` generically and is XSS-safe by construction — memory rows appear with **no JS change**; the additive work is a memory panel reading `report.memory` (model/dim/recall summary), following the `readinessClass`/gray-badge typed-Unknown convention (`dashboard.js:184-200`). SPA polls `/api/status` every ~2500 ms (`dashboard.js:2`) — see Pitfall 2 for the probe-cost consequence.

### CTRL-04 Map — backup/restore extension points

**Backup forward path (`internal/backup/backup.go:97-193`):** ordering = Stop(OWUI) + deferred Start → `VolumeExport(OpenWebUIVolumeName, TempVolumeTar)` → read sources table → per-entry SHA-256 → `BuildManifest` → `writeArchive` (manifest FIRST). The sources table (`:134-139`) is the exact extension point:
```go
sources := []src{
    {EntryOpenWebUIVolume, in.TempVolumeTar, true},
    {EntryConfig, in.ConfigPath, true},
    {EntryUsage, in.UsagePath, false},
    {EntryBenchReports, in.BenchReportsPath, false},
    // Phase 23: {EntryQdrantVolume, in.TempQdrantTar, false}  — only when exported
    // Phase 23: {EntryRecallState, in.RecallStatePath, false} — same optional shape
}
```
An empty optional path is skipped (`:144-150`) — so the cmd tier can gate the Qdrant entry by passing `""` when memory is off / the volume doesn't exist, with no new core branching. The Qdrant export itself needs: `in.QdrantVolumeName` (from `orchestrate.QdrantVolumeName()` — accessor EXISTS, `memory.go:103-106`), `in.TempQdrantTar`, and a **quiesce of `villa-qdrant.service`** around the export (Deps already has generic `Stop/Start(service)`; add a `QdrantServiceName` Deps field mirroring `OpenWebUIServiceName`, `deps.go:122`). Rationale: Qdrant persists via RocksDB/WAL under `/qdrant/storage`; exporting a live volume risks a torn snapshot — the same hazard class the OWUI SQLite quiesce exists for (`backup.go:84-85`). [ASSUMED: that a live Qdrant export can be torn — standard RocksDB behavior; the quiesce is cheap and strictly safer] Whether to also stop `villa-embed` is unnecessary (it holds no volume state; `villa-models` is `:ro`).

**Volume-existence gating:** there is no `VolumeExists` seam today. Cheapest: cmd tier decides (memory-enabled config + `podman volume exists` via the existing `podmanVolume` fixed-arg helper in `cmd/villa/podman_volume.go`, or tolerate export failure). Recommend an explicit cmd-tier existence check so the pure core's contract stays "non-empty name ⇒ export must succeed".

**Manifest (`internal/backup/manifest.go`):** append-only fields go above `SchemaVersion` (`:70-102`); `BuildManifest` stamps `backupSchemaVersion` itself (`:125-139`). Additions per D-06: `EmbeddingModel string \`json:"embedding_model,omitempty"\``, `EmbeddingDim int \`json:"embedding_dim,omitempty"\``, and `RecallSchemaVersion int` (sourced via the pre-laid `recall.SchemaVersion()` accessor — `store.go:36-40`; rides the existing `blockOnNewerStore`/`warnOnOlderStore` machinery, `backup.go:318-341`). The presence marker D-06 mentions is already structural: the entry's row in `Manifest.Entries` + membership in the `collect` map at restore (`restore.go:419-435`). New entry-name consts: `EntryQdrantVolume = "qdrant-volume.tar"`, `EntryRecallState = "recall-state.json"` (names planner's call).

**`backupSchemaVersion` bump (Claude's discretion — analysis):** `CompareSkew` fail-closes when manifest version > supported (`backup.go:262-267`), and `BuildManifest` stamps unconditionally. Bumping to 2 means an OLD villa refuses ANY new backup (even memory-free) — fail-closed downgrade. NOT bumping keeps old villas restoring new backups, but an old villa verifies-then-silently-ignores the qdrant/recall entries (its `readAndVerify` maps only known names, `restore.go:419-434`) — a silent partial restore. Recommendation: **bump to 2** — consistent with the project's "version reflects the contract" doctrine (D-04 wording) and the fail-closed-on-future precedent; old backups (v1) remain restorable by new villa either way (`m.SchemaVersion <= backupSchemaVersion` passes).

**Restore (`internal/backup/restore.go:116-288`):** the extension is mechanical but ordering-sensitive:
- `extracted` (`:86-94`) gains `qdrantVolume []byte / qdrantPresent bool` + `recallState []byte / recallPresent bool`; mapping at `:419-435`.
- `cleanRecreateThenImport` (`:171-185`) currently closes over `in.OpenWebUIVolumeName` — generalize to take `(volumeName, srcTar)`; the ordering (VolumeRm → ReconcileAndWrite → EnsureVolume → VolumeImport) is identical for both volumes. NOTE: `ReconcileAndWrite` runs once per call — when both volumes restore, either tolerate the second no-op reconcile or restructure to reconcile once then ensure+import per volume (planner's call; the second reconcile is an idempotent no-op by construction).
- Capture (`:142-156`): when the archive HAS a qdrant entry AND a current qdrant volume exists → `VolumeExport(qdrant, RollbackQdrantTar)`; when it has the entry but NO current volume exists (memory-bearing backup onto memory-off install) → record "prior absent", and rollback must REMOVE the forward-created volume (the volume analog of `rollbackRemove`, `:294-299` — a new `VolumeRm` on rollback). When the archive has NO qdrant entry → never touch any existing qdrant volume (D-07), and report honestly what was/wasn't restored.
- `recall-state.json` restore: identical to usage/bench (`:155-156, :212-223, :258-267`) — `RecallDestPath = recall.RecallStatePath()`; the store-root guard in `Deps.WriteFileAtomic` covers it (recall-state.json lives directly under `$XDG_DATA_HOME/villa`, `store.go:151-156`).
- Temp paths: `liveRestore` already creates `tmpDir` with `restore-owui.tar`/`rollback-owui.tar` (`cmd/villa/restore.go:169-185`); add `restore-qdrant.tar`/`rollback-qdrant.tar` in the same dir — the WR-01 cleanup (`:58-66`) covers them (the qdrant tar contains chat-derived vectors — same sensitivity as webui.db).

**Skew WARN+confirm (D-08):** `CurrentInstall` (`backup.go:203-214`) gains `EmbeddingModel string` / `EmbeddingDim int` (cmd fills from `cfg`); `CompareSkew` gains a warning mirroring the OWUI image-skew warning (`backup.go:297-303` — this is the "image-skew confirm precedent"; the actual y/N gate is `restore.go:135-140` + `skewPrompt` `:441-449` + `liveSkewConsent` `cmd/villa/restore.go:193-200`). Guard semantics: warn only on a *confident* mismatch — manifest fields zero/empty = "not recorded" (old backup) → no warning, mirroring `blockOnNewerStore`'s `manifestVer <= 0` convention (`backup.go:322-323`). Remediation text must name the consequence ("retrieval is corrupt until re-index") and the fix ("run `villa recall index --rebuild` after restore, or align `embedding_model`/`embedding_dim` in config.toml"). Never a BLOCK, never auto-reindex.

**Backup cmd-tier wiring deltas:** `runBackup`/`liveBackupDeps` (`cmd/villa/backup.go:77-184, :252-270`): add the second temp file, `orchestrate.QdrantVolumeName()`, `recall.RecallStatePath()`, manifest fields from cfg, `RecallSchemaVersion: recall.SchemaVersion()`; `liveRestore` (`restore.go:131-187`) symmetrical. `liveBackupDeps` gets `QdrantServiceName` + (if chosen) the volume-exists check.

### CTRL-05 Map — swap invariant + skew guards

**D-09 invariant (already structurally true — prove it):** `modelswap.Run` (`internal/modelswap/modelswap.go:85-142`) mutates only `cfg.Model`/`cfg.Quant` (`:117-121`), calls `ReconcileAndWrite`, and restarts ONLY `d.InstallServiceName` (`:137`). Memory units render exclusively from memory config fields (`memory.RenderView`, `internal/memory/memory.go:95-104`) — `cfg.Model` does not enter `buildQdrantView`/`buildEmbedView` (`orchestrate/memory.go:153-205`). Test design:
1. `internal/orchestrate`: render with two configs differing only in `Model`/`Quant` (memory-on) → assert `villa-qdrant.container`, `villa-qdrant.volume`, `villa-embed.container` (and `villa-openwebui.*`) unit texts byte-identical; only `villa-llama.container` differs.
2. `internal/modelswap`: fakeDeps records every `Restart` call → assert exactly one call with `InstallServiceName` (and zero Stop/Start of anything else — the Deps surface has no other service mutator, which the test makes permanent).
This covers both CLI and dashboard surfaces because `handleSwitch` calls `modelswap.Run` verbatim (`internal/dashboard/api.go:273-330`).

**D-10 guard — recall index refusal (fail-closed):** insertion point is `runRecallIndex` step (4), `cmd/villa/recall.go:319-353`: state is read at `:322`, and the stamp is OVERWRITTEN at `:343-344` (`state.EmbeddingModel = cfg.EmbeddingModel // Phase-23 skew guards (D-05)`). The guard MUST run after `deps.readState()` and BEFORE the overwrite+persist: if `state.EmbeddingModel != ""` and (`state.EmbeddingModel != cfg.EmbeddingModel || state.EmbeddingDim != cfg.EmbeddingDim`) → refuse `exitBlocked` with remediation. Design point for the planner: `--rebuild` resets the KB (id-preserving reset + cleared chats map, `:327-335`) — a rebuild is exactly the sanctioned re-index, so the refusal's remediation should be "re-run with `--rebuild`" and the guard should LET `--rebuild` proceed (it clean-replaces the collection content, resolving the skew; the fresh stamp then records the new model). Typed-Unknown: empty `state.EmbeddingModel` (no recorded state — pre-Phase-21-stamp stores or fresh installs) → no alarm (D-10). A small pure helper (in `internal/recall` or `internal/memory`) returning a typed verdict keeps it table-testable; planner's call.

**D-10 WARN surfaces (verb set = planner's call):** candidates with existing seams: `villa install` memory readiness flow (`cmd/villa/install_memory.go` — already loads cfg and could read state), `villa up` (`cmd/villa/up.go`), and the status Memory section itself (a `skew`/`stamped_model` field under the same 2→3 bump — zero extra verbs, visible in CLI + dashboard). Recommend at minimum: status-surfaced skew indicator + install WARN; all WARN paths read-only (D-11).

### Anti-Patterns to Avoid
- **A second status contract evolution** — every new Report field (memory section, skew indicator, anything) lands in THIS 2→3 bump or not at all this milestone.
- **Probing the chat endpoint for memory rows** — that IS the bug being fixed; never reuse `d.Health(endpoint)` for villa-qdrant/villa-embed.
- **Re-typing `villa-qdrant`/image/volume literals in cores** — use `orchestrate.QdrantVolumeName()/QdrantContainerUnitName()/EmbedContainerUnitName()/QdrantImage()/EmbedImage()`; thread into pure cores via Deps/Input fields.
- **Merge-importing into a live volume** — `podman volume import` MERGES and does NOT auto-create ([16-03] lesson, `restore.go:14-25`); always the clean-recreate ordering, forward AND rollback.
- **Auto-reindex anywhere** (D-11) — guards report/refuse only.
- **Treating a probe failure as confident service-down** — typed-Unknown (`HealthUnknown` → WARN), never a fabricated FAIL from an unevaluable signal; conversely never `HealthReady` without a real 200.
- **Hand-editing goldens** — only `go test ./... -update`-style re-freeze via the tests' own `-update` flag, once, in a deliberate commit.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Probing container-DNS-only services | New socket/proxy/port-publish mechanism | `runProbeCurl` (`cmd/villa/install_memory.go:317`) | Proven, fixed-arg, no host port (T-19-11), seam-sourced helper image |
| Clean volume replace | Custom rm/create/import sequence | `cleanRecreateThenImport` ordering (`restore.go:171-185`) generalized | Encodes the merge/no-auto-create pitfalls + rollback symmetry |
| Skew confirm UX | New prompt flow | `CompareSkew` warning + `Consent`/`skewPrompt`/`liveSkewConsent` | One gate, `--yes/--force` bypass, non-interactive declines already handled |
| recall-state read | New JSON reader | `recall.Load` (fail-closed, schema-gated, `store.go:111`) | Absent/corrupt/future-schema → empty state, never a fabricated index |
| `.container`→`.service` naming | String literals | `unitServiceName(...)` derivation + orchestrate accessors (`doctor.go:295-301`) | Same names the status fold derives; grep-gate-clean |
| Store schema comparisons in manifest | Ad-hoc int compares | `blockOnNewerStore`/`warnOnOlderStore` (`backup.go:318-341`) | Fail-closed-on-future + warn-on-older semantics already encoded |
| Atomic data-file restore | New writer | `Deps.WriteFileAtomic` (store-root-guarded) for recall-state.json | recall-state.json lives in the guarded store root |

**Key insight:** every Phase-23 behavior is a *composition* of an existing proven mechanism pointed at one more target. Any plan task that invents a new mechanism should be challenged.

## Common Pitfalls

### Pitfall 1: Accidental second contract evolution
**What goes wrong:** The skew indicator / recall summary gets "discovered" mid-implementation after the golden was already re-frozen, forcing a second bump.
**How to avoid:** Define the COMPLETE v3 field set in the status plan up front (memory section incl. model, dim, recall summary, and — if chosen — the skew indicator); freeze once at the end of that plan. The plan-checker should reject any later plan touching `Report`'s tagged fields.
**Warning signs:** Any diff to `internal/status/status.go` struct tags in a plan other than the schema-bump plan.

### Pitfall 2: Dashboard poll × podman-run probes = container churn
**What goes wrong:** The SPA polls `/api/status` every ~2500 ms (`dashboard.js:2`); if each `status.Run` spawns two `podman run --rm` probe containers, the dashboard continuously creates/destroys containers (~0.5–2 s each, possibly overlapping), loading the box and slowing the poll.
**Why it happens:** The dashboard reuses `liveStatusDeps` verbatim (`dashboard.go:152`).
**How to avoid:** Options (planner's call): (a) cache the memory-health results in the cmd-tier seam with a short TTL (e.g. 10–30 s — staleness bounded and honest; the dashboard server already has a mutex-guarded-cached-value precedent), (b) lengthen only the memory-row probe cadence dashboard-side, (c) accept the cost after on-hardware measurement. Whatever is chosen, the probe MUST be context/timeout-bounded (mirror `residencyRequestTimeout`-style bounds, `doctor.go:307-321`).
**Warning signs:** `podman ps -a` filling with exited helper containers during dashboard verification; dashboard poll visibly lagging.

### Pitfall 3: Torn Qdrant snapshot from a live export
**What goes wrong:** `podman volume export` of a running Qdrant's storage copies RocksDB/WAL mid-write → a backup that restores to a corrupt/partially-applied collection.
**How to avoid:** Quiesce `villa-qdrant.service` around the export (stop → export → deferred best-effort start with a surfaced `RestartWarning`, exactly the OWUI pattern `backup.go:102-125`). Same on the restore CAPTURE step.
**Warning signs:** Restore "succeeds" but `villa recall status`/retrieval errors on the restored box.

### Pitfall 4: Asymmetric memory-state restores
**What goes wrong:** (a) Memory-bearing backup → memory-off install: capture can't export a nonexistent qdrant volume; naive code refuses or rolls back dirtily. (b) Memory-free backup → memory-on install: code "helpfully" clears the existing qdrant volume, destroying live vectors (violates D-07).
**How to avoid:** (a) Treat "no current qdrant volume" as prior-absent: skip capture, and on rollback REMOVE the forward-created volume (volume analog of `rollbackRemove`, `restore.go:294`). (b) Gate every qdrant mutation on `ex.qdrantPresent`; with no entry, never call VolumeRm/Import on the qdrant volume, and print an honest "memory volume not present in this backup — existing Qdrant data left untouched".
**Warning signs:** Table-driven restore tests missing the 2×2 matrix {backup has/lacks entry} × {host has/lacks volume}.

### Pitfall 5: ReconcileAndWrite from the RESTORED config flips the memory stack
**What goes wrong:** The restored `config.toml` carries its own `memory_enabled`. `cleanRecreateThenImport` reconciles units from the restored cfg — restoring a memory-on backup onto a memory-off host writes qdrant/embed units (correct: config is the source of truth), but the operator may not expect the stack shape to change; conversely a memory-off backup restored onto a memory-on host reconciles toward memory-off units. The volume-touch rules (D-07) and the unit reconcile interact here.
**How to avoid:** Decide and TEST the matrix explicitly; have restore's success output state the resulting memory posture ("memory stack: enabled (restored config)"). Verify what `Reconcile` does with now-obsolete units (it writes changed units — confirm whether it removes stale ones) before relying on it. [ASSUMED: Reconcile's obsolete-unit behavior — not verified this session; check `internal/orchestrate/reconcile.go` during planning]
**Warning signs:** Post-restore `villa status` showing rows that contradict the restored config.

### Pitfall 6: Guard placed AFTER the stamp overwrite
**What goes wrong:** `runRecallIndex` overwrites `state.EmbeddingModel/Dim` from cfg at `recall.go:343-344` and persists at `:350` — if the D-10 comparison runs after this (or after persist), the recorded truth is destroyed and the skew becomes undetectable forever.
**How to avoid:** Compare immediately after `deps.readState()` (`:322`) and before any state mutation; only a clean pass (or `--rebuild`) reaches the stamp.
**Warning signs:** A test where two consecutive `recall index` runs with a changed cfg both succeed without `--rebuild`.

### Pitfall 7: Doctor goldens/tests forgotten in the status-fix wave
**What goes wrong:** Flipping memory rows to `OffloadApplies=false` removes `offload:villa-*` findings from doctor output (`doctor.go:465` gate) — the three `doctor-memory*` goldens and the down-rank tests fail "mysteriously" in a later wave.
**How to avoid:** Re-freeze doctor goldens in the SAME plan as the status classification change; decide keep-vs-remove for `memoryOffloadDownRanked` explicitly.
**Warning signs:** `make check` red on doctor tests after the status change.

### Pitfall 8: Test fixture incoherence (stubbed cfg vs stubbed units)
**What goes wrong:** `status.Run` renders rows from `Deps.Render` output but reads memory fields from `Deps.LoadConfig` — a memory-on test that stubs units WITHOUT memory units (or vice versa) produces a fixture state unreachable in production.
**How to avoid:** Memory-on test fixtures must pair `cfg.MemoryEnabled=true` (+ valid memory fields) WITH render output containing `villa-qdrant.container`/`villa-embed.container` unit names (mirror `loopbackUnits`, `status_test.go:32`).

### Pitfall 9: Verification-drill side effects on the live box
**What goes wrong:** (a) `villa install` re-runs recommend and reverts a ROCm backend choice to Vulkan (CONTEXT Specifics) — the drill leaves the box on the wrong backend. (b) The restore drill replaces the operator's REAL Qdrant vectors/OWUI data. (c) Dashboard changes invisible because `villa-dashboard.service` is long-lived.
**How to avoid:** Verification checklist: take a fresh `villa backup` FIRST and restore it LAST; re-run `villa backend set rocm` if applicable; `systemctl --user restart villa-dashboard.service` after `make build`.

### Pitfall 10: omitempty mechanics
**What goes wrong:** A non-pointer struct Memory field with `omitempty` still serializes (`omitempty` doesn't elide non-empty struct values, and never elides zero-valued structs pre-Go-1.24 semantics differ from expectation) — memory-off output grows fields, breaking D-04's "memory-off differs only in version".
**How to avoid:** Use a pointer (`*MemoryInfo`) + nil-when-off, the exact pattern `Usage *usage.UsageTotals` and `GenTokensPerSec *float64` already use (`status.go:114, :138`).

## Code Examples

All sourced from this repository (the authoritative patterns to mirror).

### Non-GPU row branch to clone for memory services
```go
// internal/status/status.go:364-374 (OWUI branch — clone shape for qdrant/embed)
if svc == d.OWUIService {
    ss.Health = d.OWUIHealth(endpoint)
    ss.Offload = naOffloadVerdict()
    ss.OffloadApplies = false
    ss.OffloadOK = false
    report.Services = append(report.Services, ss)
    continue
}
```

### Service-name derivation (no literals)
```go
// cmd/villa/doctor.go:214-217 — wire the same into liveStatusDeps
memServices = []string{
    unitServiceName(orchestrate.QdrantContainerUnitName()),
    unitServiceName(orchestrate.EmbedContainerUnitName()),
}
```

### Bounded in-network probe (health seam body)
```go
// cmd/villa/install_memory.go:289 (qdrant) — status health probe target
if _, err := curl("-sf", base+"/readyz"); err != nil { /* not ready */ }
// runProbeCurl: podman run --rm --network villa --entrypoint curl <EmbedImage()> ...
```

### Tail-append + bump (the v2→v3 edit shape)
```go
// internal/status/status.go — insert ABOVE SchemaVersion (:140), nothing above moves
Memory *MemoryInfo `json:"memory,omitempty"` // nil when memory off (D-04)
SchemaVersion int  `json:"schema_version"`   // stays LAST
// :156 — const reportSchemaVersion = 3  (bump; extend the doc-comment history)
```

### Optional manifest-field skew warning (typed-Unknown for old backups)
```go
// mirror backup.go:297-303 + the manifestVer<=0 "not recorded" rule (:322)
if m.EmbeddingModel != "" &&
    (m.EmbeddingModel != cur.EmbeddingModel || m.EmbeddingDim != cur.EmbeddingDim) {
    v.Warnings = append(v.Warnings, SkewWarning{
        Field:  "embedding",
        Detail: fmt.Sprintf("backup vectors were built with %s (dim %d); current config is %s (dim %d)",
            m.EmbeddingModel, m.EmbeddingDim, cur.EmbeddingModel, cur.EmbeddingDim),
        Remediation: "restored vectors will be unusable for retrieval until re-indexed — run `villa recall index --rebuild` after restore, or align embedding_model/embedding_dim in config.toml",
    })
}
```

### D-10 refusal placement
```go
// cmd/villa/recall.go — AFTER readState (:322), BEFORE the :343 stamp overwrite
if !rebuild && state.EmbeddingModel != "" &&
    (state.EmbeddingModel != cfg.EmbeddingModel || state.EmbeddingDim != cfg.EmbeddingDim) {
    fmt.Fprintf(errOut, "recall index: REFUSING — the index was built with %s (dim %d) but config now says %s (dim %d); indexing into a mismatched collection corrupts retrieval. Re-run with --rebuild to re-index cleanly, or revert the config.\n", ...)
    return exitBlocked
}
```

## State of the Art (contract-evolution history in this repo)

| Contract | v | Phase | Delta |
|----------|---|-------|-------|
| `status.Report` | 1 | 10 | Backend/Image/GenTokensPerSec/ROCmReadiness tail-append |
| `status.Report` | 2 | 15 | `Model` + `Usage` tail-append (one re-freeze) |
| `recommend` | 2 | 22 | Memory-reservation field tail-append (one re-freeze of `recommend.golden.json`) |
| `status.Report` | **3** | **23 (this)** | Memory section + per-row classification fix (one re-freeze) |
| backup manifest | 1 → (2?) | 16 → 23 | Embedding model/dim + recall schema version (bump = planner's call, recommendation: bump) |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Exporting a RUNNING Qdrant volume can yield a torn/inconsistent snapshot (RocksDB/WAL), so quiesce-before-export is required | CTRL-04 map / Pitfall 3 | If wrong, quiesce is merely unnecessary (still safe); if right and skipped, backups are silently corrupt — quiesce is the no-regret choice |
| A2 | Qdrant `/readyz` returns 200 on the pinned v1.18.2 image (already relied on by `install_memory.go:289` and Plan 19-03 on-hardware) | CTRL-02 probes | Health probe would misreport; on-hardware verification catches it immediately |
| A3 | `orchestrate.Reconcile`'s handling of obsolete units (memory-off restored config on memory-on host) — NOT verified this session | Pitfall 5 | Restore matrix behavior differs from plan; verify `internal/orchestrate/reconcile.go` during planning |
| A4 | `podman volume exists` is available in Podman 5.8.2 for the cmd-tier existence check | CTRL-04 map | Fallback: tolerate export failure or parse `volume inspect` — minor wiring change only |

(No external-package or compliance assumptions — nothing is installed and no new outbound surface exists.)

## Open Questions

1. **Should restore's Prove gate extend to the memory stack when a qdrant entry was restored?**
   - What we know: `Prove` currently composes preflight + chat residency (`restore.go:280-287`); `qdrantWritableProbe` exists and is pure-over-curl (`install_memory.go:288`).
   - What's unclear: whether a failed memory readiness after an otherwise-good restore should roll back EVERYTHING (heavy) or report-with-remediation.
   - Recommendation: keep Prove as-is (chat stack), add an honest post-restore memory note + remediation (`villa recall index --rebuild` / `villa doctor`) — rollback-on-memory-fail is scope creep against D-07's "honest report" wording. Planner decides.
2. **Probe-cost mitigation choice for the dashboard 2.5 s poll** (Pitfall 2 options a/b/c) — needs an on-hardware timing sample of `runProbeCurl` to choose between TTL-cache vs accept-cost.
3. **Keep or delete `memoryOffloadDownRanked`** after it goes vestigial — keep = defense-in-depth, delete = less dead code. Either is safe; pick one and say why in the plan.
4. **`--rebuild` bypass semantics for the D-10 refusal** — recommended yes (rebuild IS the sanctioned re-index); confirm at plan time that `resetKnowledge` clean-replaces the collection content such that dimension change is actually safe through OWUI's KB reset path. The on-hardware swap drill should prove this.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | go1.26.2 linux/amd64 | — |
| Podman (rootless) | volume export/import, probes | ✓ | 5.8.2 | — |
| `villa-dashboard.service` | dashboard verification | ✓ active | — | restart after build (gotcha) |
| Live gfx1151 Strix Halo host | on-hardware verification | ✓ (this IS the dev box, per project memory) | — | — |
| Populated Qdrant volume (post-Phase-21 index) | real backup/restore drill | expected present (Phase 21 UAT indexed live) | — | run `villa recall index` first if absent |

**Missing dependencies with no fallback:** none.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven + golden fixtures; no third-party assert/mock) |
| Config file | none needed (Makefile targets) |
| Quick run command | `go test ./internal/status/... ./internal/backup/... ./internal/modelswap/... ./internal/recall/... ./cmd/villa/...` |
| Full suite command | `make check` (go vet + go test ./...) |
| Golden re-freeze | `go test ./cmd/villa -run TestStatusJSONGolden -update` (and the analogous doctor/new-fixture tests) — ONCE, deliberate commit |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CTRL-02 | Memory rows non-GPU-classified; per-service health; no spurious offload | unit (fake Deps) | `go test ./internal/status -run TestRun` (new cases) | extend `internal/status/status_test.go` + `cmd/villa/status_test.go` |
| CTRL-02 | `--json` v3 contract byte-frozen (memory-off delta = version only; memory-on shape frozen) | golden | `go test ./cmd/villa -run TestStatusJSONGolden` | exists (re-freeze) + new memory-on fixture |
| CTRL-02 | Doctor goldens track the classification fix | golden | `go test ./cmd/villa -run TestDoctor` | exists (re-freeze 3 fixtures) |
| CTRL-02 | Dashboard serves same Report; SPA panel renders | unit + manual | `go test ./internal/dashboard/...` + on-hardware dashboard check | extend existing |
| CTRL-04 | Qdrant entry exported (memory-on), skipped (off/absent); quiesce ordering | unit (fakeDeps ordering assert) | `go test ./internal/backup -run TestBackup` | extend `backup_test.go` |
| CTRL-04 | Restore 2×2 matrix {entry present/absent}×{volume present/absent}; clean-recreate + rollback both volumes | unit (fakeDeps) | `go test ./internal/backup -run TestRestore` | extend `restore_test.go` |
| CTRL-04 | Dim-skew WARN+confirm; old-manifest typed-Unknown; recall schema block/warn | unit (pure CompareSkew) | `go test ./internal/backup -run TestCompareSkew` | extend `backup_test.go` |
| CTRL-05 | Chat swap: memory units byte-identical; only inference restarted | unit | `go test ./internal/orchestrate -run TestRender` + `go test ./internal/modelswap` | extend both |
| CTRL-05 | `recall index` refuses on confident skew; `--rebuild` proceeds; empty stamp = no alarm | unit (recallDeps fake, returns exit code) | `go test ./cmd/villa -run TestRecallIndex` | extend `recall_test.go` |
| SC#1-3 e2e | Live rows/backup-restore/swap drill on gfx1151 | manual-only (on-hardware; standing v1.x convention) | checklist in VERIFICATION; not automatable off-hardware | — |

### Sampling Rate
- **Per task commit:** quick run command above (touched packages)
- **Per wave merge:** `make check`
- **Phase gate:** full suite green + golden re-freeze committed exactly once + on-hardware drill before `/gsd-verify-work`

### Wave 0 Gaps
None — test infrastructure, fixtures, `-update` machinery, and fake-Deps conventions all exist; new test files land with their features.

## Security Domain

### Applicable ASVS Categories (Level 1)

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (loopback-only, single-user posture unchanged) | — |
| V3 Session Management | no | — |
| V4 Access Control | yes (file modes) | 0600/0700 on archives, temp tars, recall-state.json (existing guards) |
| V5 Input Validation | yes | Manifest parse fail-closed; tar-slip guard (`readArchive`, restore.go:328-341); duplicate/extra-entry rejection (WR-02); fixed-arg exec only |
| V6 Cryptography | yes (integrity) | Per-entry SHA-256 verify before any mutate (existing) — extends automatically to new entries |
| V8 Data Protection | yes | Qdrant volume tar contains chat-derived vectors → same sensitivity as webui.db; tmpDir cleanup on ALL exit paths (WR-01 precedent, `cmd/villa/restore.go:58-66`) |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Per-row health false-green (stopped villa-embed shows PASS) — **carried from Phase 22 UAT; STATE.md asks for a threat ID in the Phase 23 register** | Information disclosure / false assurance | Per-service health probes + typed-Unknown degradation (this phase's CTRL-02 fix) |
| Stale-vector leak via merge-import | Tampering / integrity | Clean-recreate-before-import, forward AND rollback (D-07) |
| Silent retrieval corruption on dimension skew | Tampering / integrity | Manifest-recorded dim + WARN+confirm (D-08); recall-index fail-closed refusal (D-10) |
| Torn Qdrant snapshot in backup | Tampering | Quiesce-before-export (Pitfall 3) |
| Chat-content disclosure via backup artifacts | Information disclosure | 0600 archive/temps; tmpDir removal on every exit path; recall-state.json is ids/timestamps-only by construction (T-21-01) |
| Malicious archive (tar-slip, dup entries, future schema) | Tampering / EoP | Existing readAndVerify guards apply unchanged to new entries |
| New outbound surface | — | None added: probes ride villa.network; no new ports; no telemetry |

## Sources

### Primary (HIGH confidence — direct reads this session)
- `internal/status/status.go` (full), `cmd/villa/status.go` (full), `cmd/villa/status_test.go` (golden + fixture sections)
- `internal/backup/{backup.go, manifest.go, restore.go, deps.go}` (full), `cmd/villa/{backup.go, restore.go}` (full/partial), `cmd/villa/podman_volume.go` (signatures)
- `internal/modelswap/modelswap.go` (full), `internal/dashboard/api.go` (full), `cmd/villa/dashboard.go` (wiring lines), `internal/dashboard/assets/dashboard.js` (poll + renderHealth)
- `internal/recall/store.go` (full), `internal/recall/staleness.go` (CompleteRun expression), `cmd/villa/recall.go:280-400`
- `internal/orchestrate/memory.go` (full), `internal/memory/memory.go` (full), `internal/config/villaconfig.go` (memory fields)
- `internal/doctor/doctor.go:340-420 + :465`, `cmd/villa/doctor.go:180-364`, `cmd/villa/install_memory.go:230-342`
- `internal/inference/seam_test.go:34-100` (gate patterns + allowlist)
- Goldens: `cmd/villa/testdata/status.json.golden` (full), doctor-memory golden greps, `internal/orchestrate/testdata/` listing
- `.planning/{REQUIREMENTS.md, STATE.md, ROADMAP.md(Phase 23)}`, `23-CONTEXT.md`

### Secondary / Tertiary
- None needed — CONTEXT.md's External note holds: all integration surfaces are in-repo behind proven seams; no web research performed.

## Metadata

**Confidence breakdown:**
- CTRL-02 map (branch sites, tail-append, goldens, doctor ripple): HIGH — every claim verified at file:line
- CTRL-04 map (extension points, ordering, skew gate): HIGH — full core read; A1/A3/A4 flagged where inference was needed
- CTRL-05 map (invariant, guard placement): HIGH — Run body + stamp site read directly
- Probe-cost question (Pitfall 2): MEDIUM — cost is real but unmeasured; resolve on-hardware

**Research date:** 2026-06-10
**Valid until:** code-coupled — valid while the cited files are unchanged on `main` (re-verify line refs if other work lands first); planning horizon ~30 days
