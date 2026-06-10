# Phase 23: Surfacing, Backup & Memory-Aware Swap - Context

**Gathered:** 2026-06-10
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Each decision below was auto-selected
> as the recommended option, grounded in ROADMAP Phase 23 (goal + 3 success
> criteria), REQUIREMENTS CTRL-02/04/05, the deferred-item routing from the
> Phase 20/21/22 CONTEXTs, and a codebase scout of `internal/status`,
> `internal/backup`, `internal/modelswap`, `internal/recall`, and
> `internal/dashboard`. Review before planning.

<domain>
## Phase Boundary

The memory stack becomes **observable, recoverable, and swap-safe** — closing
out v1.3 by landing the milestone's single byte-frozen contract evolution
exactly once:

1. **CTRL-02** — `villa status` + the control dashboard surface memory-stack
   health (Qdrant + embeddings service rows, active embedding model) as an
   append-only, schema-bumped contract change (`reportSchemaVersion` 2→3,
   golden re-frozen once); non-GPU services fold health/active into the verdict
   without a spurious offload PASS/FAIL.
2. **CTRL-04** — `villa backup` includes the Qdrant memory volume; `villa
   restore` clean-recreates it before import (no stale-vector leak), with the
   embedding dimension recorded in the manifest so version/dimension skew WARNs
   rather than silently corrupting retrieval.
3. **CTRL-05** — `villa model swap` is memory-aware: it warns/guards when
   changing the embedding model would invalidate existing vectors (dimension
   mismatch / no auto-reindex), and a chat-model swap leaves the embedding
   model and vector collections intact.

**In scope:**
- `status.Report` 2→3: memory service rows + active-embedding-model field(s) +
  recall-index state, append-only, goldens re-frozen ONCE; dashboard API/SPA
  surfacing the same read-model.
- `internal/backup`/`restore` extension: optional Qdrant-volume entry +
  `recall-state.json` entry, manifest dimension fields, clean-recreate-before-
  import, skew WARN/confirm.
- Memory-aware swap guards: chat-swap leaves memory stack intact (asserted);
  embedding-model-change detection + refuse/WARN-with-remediation.
- On-hardware verification on the live Strix Halo box (real backup/restore of
  a populated Qdrant volume, real status/dashboard rows, real swap drill).

**Out of scope (do NOT build here):**
- GPU passthrough for `villa-embed` — backlog (Phase 22 D-04 posture holds).
- Auto-reindex (on swap, on restore, or scheduled) — guard reports/refuses,
  never mutates; re-index stays an explicit `villa recall index` action.
- A new `villa model swap --embedding` verb / embedding-model catalog entries —
  new capability; backlog if ever wanted (see D-10).
- Reranker/hybrid search, multi-user, remote access — v2 (REQUIREMENTS).

</domain>

<decisions>
## Implementation Decisions

### Status/dashboard surfacing shape (CTRL-02)
- **D-01:** `villa-qdrant` + `villa-embed` appear as **`ServiceStatus` rows
  appended to `Report.Services`** using the existing **non-GPU row pattern**
  (the OWUI/dashboard-service rows: health/active folded into the verdict,
  `OffloadApplies=false`, no spurious offload PASS/FAIL — exactly what SC#1
  demands). Rows are emitted **only when `memory_enabled=true`** (opt-in
  discipline: memory-off output stays v1.2-shaped apart from the version
  field, mirroring Phase 19/20/22).
  `[auto] Surfacing — Q: "new Memory section vs reuse ServiceStatus rows?" → Selected: "reuse the existing non-GPU ServiceStatus row pattern, gated on memory_enabled" (recommended; SC#1 wording + composition over re-implementation)`
- **D-02:** A new **append-only memory field/section tail-appended above
  `SchemaVersion`** carries: active embedding model (+ dimension), and the
  **recall-index summary** (indexed count / last-indexed / staleness read from
  `recall-state.json`, typed-Unknown WARN-style when absent/unreadable — never
  silently stale). Including recall state honors the Phase-21 deferral
  ("surfacing recall rows in status/dashboard (status.Report 2→3) — Phase 23")
  under the SAME single bump — there is no second evolution available later.
  Fields are `omitempty`-style so memory-off output stays shape-stable. Exact
  field names/JSON keys are planner's call.
  `[auto] Memory fields — Q: "service rows only vs also embedding model + recall state?" → Selected: "embedding model + recall-index state under the single 2→3 bump" (recommended; CTRL-02 names the active embedding model; 21-CONTEXT routed recall surfacing here)`
- **D-03:** The dashboard **folds the same `status` core** (composition over
  re-implementation): the API exposes the new fields; the embedded SPA renders
  memory rows/panel. No parallel probe logic in `internal/dashboard`. The
  dashboard-restart-after-rebuild gotcha applies to verification.
  `[auto] Dashboard — Q: "dashboard-specific probes vs fold status core?" → Selected: "fold the shared status read-model" (recommended; existing dashboard pattern)`

### Single contract evolution mechanics (CTRL-02)
- **D-04:** Exactly ONE `status.Report` evolution: `reportSchemaVersion` 2→3,
  **unconditional** (version reflects the contract, not the config); all new
  fields tail-appended above `SchemaVersion` (append-only — nothing above
  moves); `status --json` + dashboard goldens re-frozen **once**, intentionally,
  with `-update`. The ROADMAP's "single byte-frozen contract evolution" line
  refers to THIS contract; the backup manifest's append-only growth (D-06)
  follows the manifest's own self-version discipline and is not a second
  status-contract evolution.
  `[auto] Bump — Q: "bump only when memory on vs unconditional?" → Selected: "unconditional 2→3; memory fields omitempty" (recommended; a schema version must describe the contract shape, not runtime state)`

### Backup/restore of the Qdrant volume (CTRL-04)
- **D-05:** **Extend the existing `internal/backup` core** (no parallel path):
  the Qdrant volume name enters `Deps`/Input **seam-sourced from `orchestrate`**
  (never a literal — same rule as `OpenWebUIVolumeName`), exported as an
  additional **optional** tar entry when `memory_enabled=true` and the volume
  exists. `villa-models` stays NEVER exported. **`recall-state.json` is added
  as an optional source entry** (same shape as usage.json/bench-reports.jsonl),
  per the Phase-21 deferral ("backup/restore of recall-state.json + the Qdrant
  volume — Phase 23").
  `[auto] Backup shape — Q: "extend existing core vs parallel memory-backup path?" → Selected: "extend internal/backup; optional entries; seam-sourced volume name" (recommended; composition + BAK-01 precedent)`
- **D-06:** The **manifest gains append-only fields above its `SchemaVersion`**:
  embedding model id + **embedding dimension** (SC#2's skew key), plus whatever
  presence marker restore needs to distinguish memory-bearing backups. Exact
  names planner's call. Whether `backupSchemaVersion` bumps is planner's call
  per the manifest's own rule (append-only = non-breaking; bump on breaking
  change) — old backups MUST remain restorable either way.
  `[auto] Manifest — Q: "dimension in manifest vs probe collection at restore?" → Selected: "record model + dimension in the manifest at backup time" (recommended; SC#2 wording — the manifest is the authority for what the vectors were built with)`
- **D-07:** Restore **clean-recreates the Qdrant volume before import** via the
  existing `cleanRecreateThenImport` mechanism (VolumeRm → ReconcileAndWrite →
  EnsureVolume → VolumeImport) — no stale-vector leak; the rollback path covers
  the new volume with the SAME ordering. Backups **without** the memory entry
  restore cleanly and do NOT touch any existing Qdrant volume (entry optional,
  honest report of what was/wasn't restored).
  `[auto] Restore — Q: "merge-import vs clean-recreate-before-import?" → Selected: "clean-recreate via the proven Phase-16 mechanism" (recommended; SC#2 mandates it; restore.go already documents the pitfall)`
- **D-08:** **Dimension/version skew at restore:** manifest embedding model/dim
  vs the current config → confident mismatch **WARNs + requires confirm**
  (mirror the existing OWUI image-skew confirm pattern in `restore.go`), with
  remediation text naming the consequence (retrieval corrupt until re-index)
  and the fix (`villa recall index` re-index after restore / align config).
  Never silent, never auto-reindex. Missing/unparseable manifest fields (old
  backups) → typed-Unknown WARN, not a hard block.
  `[auto] Skew — Q: "hard refuse vs WARN+confirm vs silent?" → Selected: "WARN + explicit confirm, mirroring the image-skew precedent" (recommended; refuse-with-remediation posture, operator stays in control)`

### Memory-aware model swap (CTRL-05)
- **D-09:** **Chat-model swap leaves the memory stack intact** — `villa model
  swap` (and the dashboard swap path, which shares `internal/modelswap`) must
  not touch the embedding model, the `villa-qdrant`/`villa-embed` units, or the
  vector collections. This is asserted by tests on the swap's render/reconcile
  scope (memory units byte-identical across a chat swap) — an invariant proof,
  not a new behavior.
  `[auto] Chat swap — Q: "restart memory services on swap vs leave untouched + assert?" → Selected: "leave untouched, assert by test" (recommended; SC#3 second clause)`
- **D-10:** The **embedding-model-change guard fires where config changes take
  effect, NOT via a new swap verb.** Today the embedding model changes only by
  editing `config.toml` (it is not a catalog model `model swap` can resolve).
  The guard compares configured `EmbeddingModel`/dimension against the
  **recorded state** — the `recall-state.json` stamp laid down for exactly this
  purpose (`cmd/villa/recall.go:343`, "Phase-23 skew guards (D-05)") and the
  Phase-18 `internal/memory` pin — and on confident mismatch:
  `villa recall index` **refuses** (fail-closed: indexing into a
  mismatched-dimension collection corrupts retrieval), while install/up/status
  paths **WARN with remediation** (no auto-reindex; remediation = explicit
  re-index or revert the config). Typed-Unknown (no recorded state yet) → no
  false alarm. The exact set of guarded verbs beyond `recall index` is
  planner's call within this posture.
  `[auto] Guard surface — Q: "new embedding-swap verb vs guard config-applied paths?" → Selected: "guard where config takes effect using the Phase-21 stamp; no new verb" (recommended; a swap verb is a new capability — out of scope; the stamp exists precisely for this)`
- **D-11:** **No auto-reindex anywhere** — guards report/refuse, never mutate
  (mirrors Phase 22 D-10: diagnose, don't mutate). Re-indexing stays an
  explicit, operator-driven `villa recall index` action.
  `[auto] Auto-remediation — Q: "auto-reindex on mismatch vs refuse/warn only?" → Selected: "never auto-reindex" (recommended; SC#3 says 'no auto-reindex'; index rebuild is expensive and operator-owned)`

### Claude's Discretion
- Exact new `Report`/manifest field names and JSON keys, golden fixture layout,
  dashboard panel layout/placement for the memory rows.
- Whether `backupSchemaVersion` bumps for the append-only manifest growth.
- The exact verb set carrying the D-10 WARN (beyond the `recall index` refusal)
  and the remediation strings.
- Whether the recall-index summary in status reads `recall-state.json` via a
  new injected Deps func or reuses the `internal/recall` store loader directly
  (pure-core rule: no direct file I/O inside `status.Aggregate` — keep it
  behind the Deps seam either way).
- Plan sequencing — status surfacing (CTRL-02), backup (CTRL-04), and swap
  guards (CTRL-05) are largely independent; on-hardware verification lands
  last (v1.x discipline).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → **Phase 23** section — goal + 3 success criteria
  (SC#1 schema 2→3 + non-GPU fold; SC#2 clean-recreate + dimension-in-manifest;
  SC#3 swap guards + chat-swap-leaves-memory-intact).
- `.planning/REQUIREMENTS.md` — **CTRL-02, CTRL-04, CTRL-05** definitions;
  v2 deferrals and Out of Scope table.
- `.planning/PROJECT.md` — v1.3 milestone goal; "runs healthy after install"
  bar; integration-first constraint.

### Prior-phase contracts this phase builds on
- `.planning/phases/22-control-plane-fit-host-gate/22-CONTEXT.md` — the
  schema-bump precedent (recommend 1→2), the deferred-to-23 routing, the
  non-GPU down-rank-but-visible doctor posture Phase 22 landed.
- `.planning/phases/21-conversational-recall-indexer/21-CONTEXT.md` — recall
  surfacing + recall-state.json backup deferrals routed HERE; recall D-05 skew
  stamp rationale.
- `.planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md`
  — D-08 pin: `nomic-embed-text-v1.5`, **768-dim**, Q8_0 — the dimension the
  manifest records and the guards compare against.
- `.planning/phases/16-backup-restore/` (v1.2 archive, if present) — original
  backup/restore decisions (BAK-01 model exclusion, clean-recreate pitfalls).

### Code touchpoints (reuse/extend; primary edit sites)
- `internal/status/status.go` — `ServiceStatus` (:46), `Report.Services` (:91),
  `reportSchemaVersion = 2` (:156), the tail-append comments (:127–:156), the
  OWUI/dashboard non-GPU row branches (:364, :404) D-01 mirrors.
- `internal/dashboard/api.go` + `internal/dashboard/assets/` — API read-models
  + SPA; fold the status core (D-03).
- `internal/backup/manifest.go` — `Manifest` (:70, append-only above
  `SchemaVersion`), `ManifestInput`/`BuildManifest` — D-06 edit site.
- `internal/backup/backup.go` — `OpenWebUIVolumeName` seam rule (:56),
  optional-entry reads (:127) — the pattern D-05 extends.
- `internal/backup/restore.go` — `cleanRecreateThenImport` (:166), the
  image-skew confirm precedent (:301) D-08 mirrors; rollback ordering (:113).
- `internal/backup/deps.go` — `VolumeImport`/`VolumeRm` seams (:62–:68).
- `internal/modelswap/modelswap.go` — guarded swap ordering core (`Run` :85)
  — D-09 invariant site.
- `internal/recall/store.go` — `State` (:62), `recall-state.json` path (:152)
  — recall summary source (D-02) + backup entry (D-05).
- `cmd/villa/recall.go:343` — `state.EmbeddingModel = cfg.EmbeddingModel
  // Phase-23 skew guards (D-05)` — the pre-laid stamp D-10 consumes.
- `internal/memory/footprint.go` + `internal/config/villaconfig.go` —
  `EmbeddingModel`/`EmbeddingDim` config fields + pinned defaults.
- `internal/orchestrate/` — Qdrant volume name accessor (seam source for D-05;
  same category as `OpenWebUIVolumeName()`).
- `cmd/villa/testdata/*.golden*` + `internal/status` / dashboard goldens — the
  byte-frozen fixtures to re-freeze ONCE with `-update` (D-04).
- `internal/inference/seam_test.go` — `TestSeamGrepGate` must stay green: no
  image/volume literals leak into `status`/`backup`/`modelswap` cores.

### External
- None — all integration surfaces (podman volume export/import, Qdrant volume,
  recall state) are already in-repo behind proven seams. If research needs
  Qdrant collection-dimension probing semantics, that would be new ground —
  D-06 deliberately avoids it by recording the dimension in the manifest.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `status.Aggregate`'s OWUI/dashboard-service row branches — the exact non-GPU
  fold (health/active into verdict, no offload row) D-01 reuses.
- `backup.cleanRecreateThenImport` + `VolumeRm`/`VolumeImport` Deps seams — the
  whole CTRL-04 restore mechanism exists; Phase 23 points it at a second volume.
- Manifest append-only assembly (`BuildManifest`) — D-06 adds fields, no new
  machinery.
- The restore image-skew confirm flow — the template for the dimension-skew
  WARN+confirm (D-08).
- `internal/recall` store loader + typed-Unknown staleness classification —
  the recall summary (D-02) and the guard comparison (D-10) read it as-is.
- `internal/modelswap` Deps-injected ordering core shared by CLI + dashboard —
  one edit covers both swap surfaces (D-09).

### Established Patterns
- **Append-only + schema-bump golden evolution** — fields above `SchemaVersion`
  never move; goldens re-frozen once with `-update`.
- **Seam-sourced literals** — volume/unit/image names come from `orchestrate`/
  `inference` accessors, never re-typed (grep-gate enforced).
- **Typed-Unknown degradation** — absent recall state / old manifests → WARN
  with provenance, never false-green, never false-alarm.
- **Opt-in discipline** — `memory_enabled=false` ⇒ no memory rows/entries.
- **Refuse-with-remediation** — every guard carries actionable text.
- **Pure core + injectable Deps** — status/backup/modelswap cores stay host-free.

### Integration Points
- config (`MemoryEnabled`/`EmbeddingModel`/`EmbeddingDim`) → status rows,
  manifest stamping, swap guards.
- `recall-state.json` → status recall summary + backup entry + D-10 comparison.
- `orchestrate` volume-name accessor → backup/restore Deps input.
- Dashboard SPA → status core via the existing API fold; restart-after-rebuild
  gotcha applies during verification.

</code_context>

<specifics>
## Specific Ideas

- ROADMAP phrasing is the contract: "landing the milestone's single byte-frozen
  contract evolution exactly once" — one 2→3 bump, one golden re-freeze; any
  design that needs a second status evolution is wrong.
- The live Strix Halo box IS the dev/test host — backup/restore of a REAL
  populated Qdrant volume (post-Phase-21 index), live status/dashboard rows,
  and the swap drill run for real during verification (standing convention).
- Known gotcha to respect during verification: `villa install` re-runs
  recommend and reverts a ROCm backend choice to Vulkan — restore
  `villa backend set rocm` afterward if the box was on ROCm.
- `villa-dashboard.service` is long-lived — after `make build`, restart it
  before checking dashboard changes.

</specifics>

<deferred>
## Deferred Ideas

- `villa model swap --embedding <id>` sanctioned embedding-swap verb (with
  guided re-index) — new capability; backlog.
- Auto-reindex on schedule / on swap / on restore — backlog (guards never
  mutate in v1.3).
- GPU passthrough for `villa-embed` + re-measured footprint — backlog
  (carried from Phase 22).
- Reranker/hybrid search (RAG-Q-01), SearXNG, multi-user/remote — v2 per
  REQUIREMENTS.

</deferred>

---

*Phase: 23-surfacing-backup-memory-aware-swap*
*Context gathered: 2026-06-10*
