# Phase 23: Surfacing, Backup & Memory-Aware Swap - Pattern Map

**Mapped:** 2026-06-10
**Files analyzed:** 19 new/modified edit sites
**Analogs found:** 19 / 19 (this is a pure brownfield phase — every edit site's analog is an existing in-repo pattern, usually in the same file)

All excerpts below were verified by direct reads this session. Line numbers refer to the current `gsd/phase-22-control-plane-fit-host-gate` branch tip.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog (pattern to clone) | Match Quality |
|-------------------|------|-----------|-----------------------------------|---------------|
| `internal/status/status.go` | pure read-model core | aggregation (probe→Report) | own OWUI/dashboard non-GPU branches (`:364-374`, `:404-424`); own tail-append history (`:106-156`) | exact (in-file) |
| `internal/status/status_test.go` | test | table-driven over fake Deps | existing fixture conventions (`loopbackUnits` pattern, per RESEARCH `cmd/villa/status_test.go:32`) | exact |
| `cmd/villa/status.go` | cmd-tier live wiring | request-response (bounded probes) | own `liveStatusDeps` (`:168-217`), `liveOpenWebUIHealth` (`:341-362`), `liveReadUsage` (`:226-248`) | exact (in-file) |
| `cmd/villa/status_test.go` + `cmd/villa/testdata/status.json.golden` | golden test | byte-frozen contract | own `-update` machinery (`TestStatusJSONGolden`, `status_test.go:285-298`); Phase-15 v1→v2 + Phase-22 recommend 1→2 bump precedent | exact |
| `internal/doctor/doctor.go` | pure diagnostic core | aggregation | own `memoryOffloadDownRanked` (`:375-384`) — goes vestigial; offload-finding gate (`:462-479`) | exact (in-file) |
| `cmd/villa/testdata/doctor-memory*.golden` (3 files) | golden fixtures | byte-frozen | re-freeze with `-update` in the SAME plan as the status classification fix | exact |
| `internal/dashboard/api.go` | HTTP handler | request-response | own `handleStatus` (`api.go:19-22`) — **zero change expected**; fold is automatic | exact |
| `internal/dashboard/assets/dashboard.js` (+ `.html`/`.css`) | SPA component | poll + render | own `renderHealth` XSS-safe DOM idiom (`dashboard.js:146-178`), `readinessClass` gray-badge typed-Unknown (`:185-200`), `renderBackend` panel-append shape (`:210+`) | exact (in-file) |
| `internal/backup/backup.go` | pure orchestrator core | batch (quiesce→export→assemble) | own sources table (`:134-139`), OWUI quiesce frame (`:109-125`), image-skew warning (`:297-303`), `blockOnNewerStore` not-recorded rule (`:322-323`) | exact (in-file) |
| `internal/backup/manifest.go` | pure data contract | serialization | own append-only-above-SchemaVersion layout (`:70-102`), entry consts (`:29-35`) | exact (in-file) |
| `internal/backup/restore.go` | pure transactional core | batch (capture→mutate→prove→rollback) | own `cleanRecreateThenImport` (`:171-185`), capture (`:146-156`), `rollbackRemove` (`:294-299`), `skewPrompt` (`:441-449`), optional-entry mapping (`:429-434`) | exact (in-file) |
| `internal/backup/deps.go` | seam definition | — | own `OpenWebUIServiceName` field (`:122`), Volume* seams (`:61-72`) | exact (in-file) |
| `cmd/villa/backup.go` | cmd-tier wiring | batch | own `runBackup` BackupInput assembly (`:139-157`), temp-file frame (`:128-137`) | exact (in-file) |
| `cmd/villa/restore.go` | cmd-tier wiring | batch | own `liveRestore` (`:131-187`), `liveSkewConsent` (`:193-200`), WR-01 tmpDir cleanup (`:56-76`) | exact (in-file) |
| `cmd/villa/podman_volume.go` | fixed-arg podman seam | exec | own fixed-arg `podmanVolume` helper family (add `volume exists` check here if chosen) | exact (in-file) |
| `internal/modelswap/modelswap_test.go` | invariant test | — | `modelswap.Run` restart scope (`modelswap.go:130-141`): fakeDeps records Restart calls, assert exactly one with `InstallServiceName` | exact |
| `internal/orchestrate` render test (extend) | invariant test | — | render two configs differing only in `Model`/`Quant` → memory unit texts byte-identical (`memory.go:153-205` shows memory views read only memory config fields) | exact |
| `cmd/villa/recall.go` | cmd-tier guard | request-response | own `runRecallIndex` step (4) (`:319-353`) — guard inserts after `readState` (`:322`), before the stamp overwrite (`:343-344`) | exact (in-file) |
| optional pure skew helper (`internal/recall` or `internal/memory`) | utility | pure compare | `recall.Load` fail-closed semantics (`store.go:111-132`) + `blockOnNewerStore` "not recorded ⇒ no alarm" convention (`backup.go:322-323`) | role-match |

## Pattern Assignments

### `internal/status/status.go` — memory ServiceStatus rows + Memory section + 2→3 bump

**Analog: the OWUI non-GPU row branch** (`internal/status/status.go:364-374`) — clone this shape for villa-qdrant and villa-embed, matched BEFORE the generic GPU branch at `:376`:

```go
if svc == d.OWUIService {
    // Open WebUI row (D-12 / CHAT-01 SC#1): health = reachability + a
    // NON-EMPTY upstream model list. It has no GPU offload, so its offload
    // is the N/A representation that does NOT fold into the overall verdict.
    ss.Health = d.OWUIHealth(endpoint)
    ss.Offload = naOffloadVerdict()
    ss.OffloadApplies = false
    ss.OffloadOK = false
    report.Services = append(report.Services, ss)
    continue
}
```

`naOffloadVerdict()` (`:71-77`) already exists — reuse, do not re-roll:

```go
func naOffloadVerdict() inference.Verdict {
    return inference.Verdict{
        Status:     inference.StatusWarn,
        Detail:     "N/A — this service has no GPU offload",
        Provenance: "not an inference service (no llama-server residency to assert)",
    }
}
```

`Aggregate` (`:448-456`) folds offload ONLY when `OffloadApplies` — no Aggregate change needed:

```go
for _, s := range r.Services {
    if s.OffloadApplies {
        bump(s.Offload.Status)
    }
    bump(HealthStatus(s.Health))
    bump(ActiveStatus(s.Active))
}
```

**Deps fields pattern** — clone `OWUIService` (`:245`) / `DashboardService`+`DashboardHealth` (`:254-260`) for the two memory service names + their per-service health seams:

```go
// OWUIService is the villa-openwebui.service unit name the owui-row branch
// targets (D-12). It is a Deps field so internal/status need not import the
// cmd-layer install.go constant (which would create a package-main cycle).
OWUIService string
...
DashboardService string
DashboardAddr    string
DashboardHealth  func(addr string) HealthState
```

**Tail-append + bump pattern** — `Report` (`:129-156`). New fields (memory section pointer) go between `Usage` and `SchemaVersion`; nothing above moves; bump the const and extend its doc-comment history (v1 = Phase 10, v2 = Phase 15, v3 = Phase 23):

```go
Usage *usage.UsageTotals `json:"usage,omitempty"`

// >>> insert here: Memory *MemoryInfo `json:"memory,omitempty"`  (nil when memory off — D-04)

// SchemaVersion is the Report contract self-version (D-07). It MUST stay the
// LAST tagged field (append-only; new tagged fields go above it, the unexported
// err stays after it and never serializes).
SchemaVersion int `json:"schema_version"`
...
const reportSchemaVersion = 2   // → 3
```

**omitempty mechanics (Pitfall 10):** use a POINTER struct, the exact pattern `Usage *usage.UsageTotals` (`:138`) and `GenTokensPerSec *float64` (`:114`) use — a non-pointer struct with omitempty still serializes.

**Nil-seam guard pattern** for the new `ReadRecallState`-style seam — clone the `ReadUsage` fold in `Run` (`:343-345`):

```go
if d.ReadUsage != nil {
    report.Usage = d.ReadUsage()
}
```

---

### `cmd/villa/status.go` — live wiring: memory service names, per-service health probes, recall-state seam

**Service-name derivation (no literals)** — clone `cmd/villa/doctor.go:214-217` + `unitServiceName` (`doctor.go:299-301`) into `liveStatusDeps`:

```go
memServices = []string{
    unitServiceName(orchestrate.QdrantContainerUnitName()),
    unitServiceName(orchestrate.EmbedContainerUnitName()),
}
...
func unitServiceName(containerUnit string) string {
    return strings.TrimSuffix(containerUnit, ".container") + ".service"
}
```

**liveStatusDeps wiring shape** (`cmd/villa/status.go:181-216`) — every new seam added here reaches the dashboard automatically (`cmd/villa/dashboard.go` wires `*liveStatusDeps()` verbatim):

```go
return &status.Deps{
    LoadConfig: config.LoadVilla,
    ...
    OWUIService:      openWebUIServiceName,
    DashboardService: orchestrate.DashboardServiceName,
    DashboardAddr:    liveDashboardAddr(),
    DashboardHealth:  liveDashboardHealth,
    ReadUsage:        liveReadUsage,
}, nil
```

**Health probe for container-DNS-only services** — the proven mechanism is `runProbeCurl` (`cmd/villa/install_memory.go:322-341`): fixed-arg `podman run --rm --network villa --entrypoint curl <orchestrate.EmbedImage()>`:

```go
func runProbeCurl(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, error) {
    args := []string{
        "run", "--rm",
        "--network", memoryProofNetwork,
        "--entrypoint", "curl",
        helperImage,
    }
    args = append(args, curlArgs...)
    cmd := exec.CommandContext(ctx, "podman", args...) // fixed args; no shell
    ...
}
```

Probe targets (existing precedent): Qdrant `curl("-sf", base+"/readyz")` (`install_memory.go:289`); villa-embed llama-server `/health` with the 200→ready / 503→loading mapping of `liveHealthProbe` (`status.go:317-333`):

```go
switch resp.StatusCode {
case http.StatusOK:
    return status.HealthReady
case http.StatusServiceUnavailable:
    return status.HealthLoading
default:
    return status.HealthDown
}
```

**Typed-Unknown probe honesty** — clone `liveOpenWebUIHealth`'s mapping discipline (`status.go:341-362`): transport error → `HealthUnknown` (WARN), reachable-but-not-confirmed → `HealthLoading`, never a fabricated confident FAIL from an unevaluable probe. The probe MUST be context/timeout-bounded (mirror `statusHTTPTimeout` / `residencyRequestTimeout` bounds). Pitfall 2 (dashboard 2.5s poll × podman-run probe cost): the mitigation seam lives HERE in the cmd tier (TTL cache is planner's call).

**Recall-state read seam** — clone `liveReadUsage` (`status.go:226-248`) exactly, swapping `usage.Load` for `recall.Load`:

```go
func liveReadUsage() *usage.UsageTotals {
    path := usage.UsagePath()
    deps := usage.Deps{
        ReadAll: func() ([]byte, error) {
            b, err := os.ReadFile(path)
            if os.IsNotExist(err) {
                return nil, nil // absent store ⇒ fail-closed-to-empty in usage.Load
            }
            ...
        },
    }
    totals, err := usage.Load(deps)
    if err != nil {
        return nil // unreadable store → typed-Unknown (omitted), never a fabricated 0
    }
    if len(totals.Models) == 0 {
        return nil // empty store ⇒ omit the key
    }
    return &totals
}
```

`recall.Load` already fails closed to empty State on absent/corrupt/future-schema (`internal/recall/store.go:111-132`); `recall.RecallStatePath()` (`store.go:154-156`) resolves the path. `recall.State` fields for the summary (`store.go:67-76`): `EmbeddingModel`, `EmbeddingDim`, `LastIndexStartedAt`, `LastIndexCompletedAt`, `len(Chats)`.

---

### `internal/doctor/doctor.go` + doctor goldens — ripple from the N/A reclassification

Doctor creates `offload:<svc>` findings via `offloadFinding` for every row, but the down-rank exists only because of the carried false-classification. After the status fix flips memory rows to `OffloadApplies=false`, the `memoryOffloadDownRanked` predicate (`internal/doctor/doctor.go:375-384`) never matches — its own comment names this phase: "the status-side N/A fix is Phase 23" (`:363`):

```go
memoryOffloadDownRanked := func(f Finding) bool {
    if !d.MemoryEnabled || f.Status != statusWarn {
        return false
    }
    svc, ok := strings.CutPrefix(f.ID, "offload:")
    if !ok {
        return false
    }
    return slices.Contains(d.MemoryServices, svc)
}
```

Keep-vs-delete is planner's call; either way re-freeze `cmd/villa/testdata/doctor-memory.json.golden`, `doctor-memory-pass.golden`, `doctor-memory-residency-fail.golden` in the SAME plan as the status classification change (Pitfall 7). Note: whether doctor emits offload findings for memory rows at all may change — verify against `internal/doctor` finding-construction (research cites an `OffloadApplies=true`-only gate at doctor.go:465; the read this session shows `offloadFinding` at `:462-479` — confirm the gating call site during planning).

---

### `internal/dashboard/assets/dashboard.js` — memory panel

`handleStatus` (`internal/dashboard/api.go:19-22`) serializes the same `status.Run` Report — **no API change**:

```go
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
    report := status.Run(s.statusDeps)
    writeJSON(w, http.StatusOK, report)
}
```

Memory service rows auto-render via `renderHealth` (no JS change for rows). The additive memory panel clones the XSS-safe DOM idiom (`dashboard.js:146-178`) — `createElement` + `textContent`, NEVER innerHTML interpolation:

```js
services.forEach(function (svc) {
    var row = document.createElement("div");
    row.className = "health-row";
    var dot = document.createElement("span");
    dot.className = "status-dot " + healthClass(svc.health);
    row.appendChild(dot);
    var name = document.createElement("span");
    name.className = "health-service";
    name.textContent = svc.service;
    ...
});
```

For the typed-Unknown badge convention, clone `readinessClass` (`dashboard.js:185-191`) — absent/unexpected → gray "unknown", never a fabricated state:

```js
function readinessClass(state) {
    switch (state) {
        case "ready": return "ready";
        case "not-ready": return "warn";
        default: return "unknown"; // absent or anything unexpected → typed-Unknown
    }
}
```

Panel placement: mirror `renderBackend` (`dashboard.js:210+`), which appends extra rows after `renderHealth` output, reading `report.memory` fields the same way it reads `report.backend`/`report.rocm_readiness`. Verification gotcha: `systemctl --user restart villa-dashboard.service` after `make build`.

---

### `internal/backup/backup.go` — Qdrant volume entry, quiesce, dimension-skew warning

**Optional-entry sources table** (`backup.go:134-139`) — the exact extension point; an empty optional path is skipped (`:144-150`) so the cmd tier gates by passing `""`:

```go
sources := []src{
    {EntryOpenWebUIVolume, in.TempVolumeTar, true},
    {EntryConfig, in.ConfigPath, true},
    {EntryUsage, in.UsagePath, false},
    {EntryBenchReports, in.BenchReportsPath, false},
    // Phase 23: {EntryQdrantVolume, in.TempQdrantTar, false}
    // Phase 23: {EntryRecallState, in.RecallStatePath, false}
}
```

**Quiesce frame** — clone the OWUI stop/deferred-start with surfaced `RestartWarning` (`backup.go:109-125`) for `villa-qdrant.service` around its volume export (torn-RocksDB hazard, Pitfall 3):

```go
if err := d.Stop(d.OpenWebUIServiceName); err != nil {
    return Result{Err: ..., FailedStep: "stop"}, ...
}
defer func() {
    if serr := d.Start(d.OpenWebUIServiceName); serr != nil {
        retRes.RestartWarning = fmt.Sprintf(
            "backup written, but failed to restart %s (%v) — run `villa up`",
            d.OpenWebUIServiceName, serr)
    }
}()
```

**Dimension-skew WARN** — clone the OWUI image-skew warning shape (`backup.go:297-303`) into `CompareSkew`, gated by the `manifestVer <= 0 ⇒ "not recorded" ⇒ no alarm` convention from `blockOnNewerStore` (`:322-323`):

```go
if m.OpenWebUIImage != cur.OpenWebUIImage {
    v.Warnings = append(v.Warnings, SkewWarning{
        Field:       "openwebui_image",
        Detail:      fmt.Sprintf("backup Open WebUI image %q differs from current %q", ...),
        Remediation: "the restored Open WebUI data volume was produced by a different image; confirm to proceed ...",
    })
}
...
// not-recorded convention:
if manifestVer > 0 && manifestVer > currentVer { ... }
```

Phase-23 shape (typed-Unknown for old manifests — empty `m.EmbeddingModel` ⇒ no warning):

```go
if m.EmbeddingModel != "" &&
    (m.EmbeddingModel != cur.EmbeddingModel || m.EmbeddingDim != cur.EmbeddingDim) {
    v.Warnings = append(v.Warnings, SkewWarning{
        Field:  "embedding",
        Detail: fmt.Sprintf("backup vectors were built with %s (dim %d); current config is %s (dim %d)", ...),
        Remediation: "restored vectors will be unusable for retrieval until re-indexed — run `villa recall index --rebuild` after restore, or align embedding_model/embedding_dim in config.toml",
    })
}
```

`CurrentInstall` (`:203-214`) gains `EmbeddingModel string` / `EmbeddingDim int` filled by the cmd tier from cfg. Recall-schema-version compare rides the existing `blockOnNewerStore`/`warnOnOlderStore` pair (`:318-341`) with `recall.SchemaVersion()` (`internal/recall/store.go:40` — pre-laid accessor).

---

### `internal/backup/manifest.go` — append-only fields + new entry consts

Entry consts pattern (`:29-35`):

```go
const (
    EntryManifest        = "manifest.json"
    EntryConfig          = "config.toml"
    EntryOpenWebUIVolume = "openwebui-volume.tar"
    EntryUsage           = "usage.json"
    EntryBenchReports    = "bench-reports.jsonl"
    // Phase 23: EntryQdrantVolume = "qdrant-volume.tar"; EntryRecallState = "recall-state.json"
)
```

Append-only field placement — new fields go ABOVE `SchemaVersion` (`:95-102`), mirroring the existing layout; `BuildManifest` (`:125-139`) copies them through and stamps `backupSchemaVersion` itself. The bump decision (`backupSchemaVersion = 1` at `:23`, research recommends → 2) is planner's call; old (v1) backups stay restorable either way because the gate is `m.SchemaVersion <= backupSchemaVersion`:

```go
ExcludedModels []ExcludedModel `json:"excluded_models,omitempty"`
// >>> insert Phase-23 fields here (embedding_model, embedding_dim, recall_schema_version, omitempty for old shape)
// SchemaVersion is the manifest's own self-version. APPEND-ONLY: this stays the
// LAST field; new fields go ABOVE it (D-09).
SchemaVersion int `json:"schema_version"`
```

---

### `internal/backup/restore.go` — second volume through the proven clean-recreate mechanism

**`cleanRecreateThenImport`** (`:171-185`) — generalize the closure to take `(volumeName, srcTar)`; the ordering is the load-bearing contract (import MERGES and does NOT auto-create — [16-03] lesson):

```go
cleanRecreateThenImport := func(cfg config.VillaConfig, srcTar string) error {
    if err := d.VolumeRm(in.OpenWebUIVolumeName); err != nil {
        return fmt.Errorf("volume rm %s: %w", in.OpenWebUIVolumeName, err)
    }
    if _, err := d.ReconcileAndWrite(cfg); err != nil {
        return fmt.Errorf("reconcile/recreate units: %w", err)
    }
    if err := d.EnsureVolume(in.OpenWebUIVolumeName); err != nil {
        return fmt.Errorf("ensure volume %s: %w", in.OpenWebUIVolumeName, err)
    }
    if err := d.VolumeImport(in.OpenWebUIVolumeName, srcTar); err != nil {
        return fmt.Errorf("volume import %s: %w", in.OpenWebUIVolumeName, err)
    }
    return nil
}
```

**Capture-before-mutate** (`:146-156`) — same shape for the qdrant volume, with the prior-absent case (memory-bearing backup onto memory-off host) handled like `rollbackRemove` (`:294-299`) handles a forward-created data file:

```go
if err := d.VolumeExport(in.OpenWebUIVolumeName, in.RollbackVolumeTar); err != nil {
    return Result{Refused: true, FailedStep: "capture", ...}
}
...
func rollbackRemove(d Deps, path string) error {
    if d.RemoveFile == nil {
        return fmt.Errorf("no RemoveFile seam wired — cannot remove forward-created %q", path)
    }
    return d.RemoveFile(path)
}
```

**Optional-entry extraction** — clone the usage/bench `present` flag mapping (`:429-434`) for `qdrantVolume`/`recallState` in the `extracted` struct (`:86-94`):

```go
if b, ok := collect[EntryUsage]; ok {
    ex.usage, ex.usagePresent = b, true
}
if b, ok := collect[EntryBenchReports]; ok {
    ex.bench, ex.benchPresent = b, true
}
```

Gate EVERY qdrant mutation on `ex.qdrantPresent` (D-07: a memory-free backup never touches an existing qdrant volume). **recall-state.json restore** is identical to the usage/bench rows (`:258-267` forward, `:212-223` rollback) with `RecallDestPath = recall.RecallStatePath()`; the store-root guard in `Deps.WriteFileAtomic` already covers it.

**Skew confirm UX** — unchanged machinery: `skewPrompt` (`:441-449`) assembles Field/Detail/Remediation + y/N; the gate at `:135-140` honors `Bypass`. The new `SkewWarning` rides it with zero prompt-flow changes.

---

### `internal/backup/deps.go` — new seam fields

Clone the service-name field convention (`:122-123`):

```go
// OpenWebUIServiceName / InstallServiceName are the service identities the flow
// quiesces/restarts. Deps fields so the pure core need not import cmd-layer
// constants (mirrors backendswap.InstallServiceName).
OpenWebUIServiceName string
InstallServiceName   string
// Phase 23: QdrantServiceName string  (same convention)
```

The Volume* seam doc-comments (`:61-72`) are the template for any new seam (e.g. a `VolumeExists` check, if the cmd-tier `podman volume exists` route is chosen instead).

---

### `cmd/villa/backup.go` / `cmd/villa/restore.go` — wiring deltas

**BackupInput assembly** (`backup.go:139-157`) — add `QdrantVolumeName: orchestrate.QdrantVolumeName()` (pre-laid accessor, `internal/orchestrate/memory.go:106`), `RecallStatePath: recall.RecallStatePath()`, embedding model/dim from cfg, `RecallSchemaVersion: recall.SchemaVersion()`:

```go
in := backup.BackupInput{
    ...
    InferenceImage:      be.Image(),
    OpenWebUIImage:      orchestrate.OpenWebUIImage(),
    UsageSchemaVersion:  usage.SchemaVersion(),
    BenchSchemaVersion:  benchstore.SavedReportSchemaVersion(),
    OpenWebUIVolumeName: orchestrate.OpenWebUIVolumeName(),
    TempVolumeTar:       tmpVolPath,
    ...
    FileMissing:         os.IsNotExist,
}
```

Temp-file frame (`backup.go:128-137`): same-dir `os.CreateTemp` + deferred remove — clone for the second qdrant temp tar.

**RestoreInput assembly** (`restore.go:175-185`) — add `restore-qdrant.tar`/`rollback-qdrant.tar` in the same tmpDir; the WR-01 cleanup (`:56-76`, explicit cleanup before every `os.Exit` because exits skip defers) covers them:

```go
in := backup.RestoreInput{
    OpenArchive:         func() (io.ReadCloser, error) { return os.Open(absArchive) },
    Current:             cur,
    Consent:             liveSkewConsent,
    Bypass:              bypass || force,
    OpenWebUIVolumeName: orchestrate.OpenWebUIVolumeName(),
    TempVolumeTar:       filepath.Join(tmpDir, "restore-owui.tar"),
    RollbackVolumeTar:   filepath.Join(tmpDir, "rollback-owui.tar"),
    UsageDestPath:       usage.UsagePath(),
    BenchDestPath:       benchReportsStorePath(),
}
```

`liveSkewConsent` (`:193-200`) is unchanged — the dimension-skew confirm rides it.

---

### `internal/modelswap` + `internal/orchestrate` tests — D-09 chat-swap invariant proofs

The invariant is already structurally true; the tests make it permanent. `modelswap.Run` mutates only `cfg.Model`/`cfg.Quant` and restarts ONLY `InstallServiceName` (`modelswap.go:117-141`):

```go
fromModel := cfg.Model
cfg.Model = m.ID
if m.Quant != "" {
    cfg.Quant = m.Quant
}
...
changed, err := d.ReconcileAndWrite(cfg)
...
if err := d.Restart(d.InstallServiceName); err != nil { ... }
```

Memory units render exclusively from memory config fields — `buildQdrantView(qdrantAddr)` / `buildEmbedView(ggufFilename, embedAddr, embedPort)` (`internal/orchestrate/memory.go:153-205`) never read `cfg.Model`. Test design (RESEARCH §CTRL-05):
1. orchestrate: render two memory-on configs differing only in `Model`/`Quant` → assert `villa-qdrant.container`/`.volume`/`villa-embed.container`/`villa-openwebui.*` unit texts byte-identical; only `villa-llama.container` differs.
2. modelswap: fakeDeps records every `Restart` call → assert exactly one call, with `InstallServiceName`. Covers the dashboard too — `handleSwitch` calls `modelswap.Run` verbatim.

---

### `cmd/villa/recall.go` — D-10 embedding-skew refusal

**Insertion point:** `runRecallIndex` step (4), AFTER `deps.readState()` (`:322`) and BEFORE the stamp overwrite (`:343-344`) — placing it after destroys the recorded truth forever (Pitfall 6):

```go
state, err := deps.readState()           // :322 — guard goes immediately after this
...
state.EmbeddingModel = cfg.EmbeddingModel // :343 — Phase-23 skew guards (D-05); stamp overwrite
state.EmbeddingDim = cfg.EmbeddingDim     // :344
```

**Refusal message register** — clone the existing refuse-with-remediation prints in the same function, e.g. the WR-05 single-operator guard (`:371-374`):

```go
if len(humans) > 1 && !sharedRecallAck {
    fmt.Fprintf(errOut, "recall index: REFUSING — found %d human users ... re-run with --i-understand-shared-recall; otherwise wait for per-user-scoped recall.\n", len(humans))
    return exitBlocked
}
```

Guard shape (typed-Unknown: empty `state.EmbeddingModel` = no recorded state ⇒ no alarm; `--rebuild` is the sanctioned bypass — it id-preservingly resets the KB at `:327-335`):

```go
if !rebuild && state.EmbeddingModel != "" &&
    (state.EmbeddingModel != cfg.EmbeddingModel || state.EmbeddingDim != cfg.EmbeddingDim) {
    fmt.Fprintf(errOut, "recall index: REFUSING — the index was built with %s (dim %d) but config now says %s (dim %d); indexing into a mismatched collection corrupts retrieval. Re-run with --rebuild to re-index cleanly, or revert the config.\n", ...)
    return exitBlocked
}
```

D-10 WARN surfaces (install/up/status — exact verb set planner's call) reuse the same comparison; a small pure helper returning a typed verdict (in `internal/recall` or `internal/memory`) keeps it table-testable.

## Shared Patterns

### Seam-sourced names — never re-type a literal (grep-gate enforced)
**Sources:** `internal/orchestrate/memory.go:34,47,106,114,117` — all pre-laid for this phase.
**Apply to:** status wiring, backup/restore wiring, doctor.
```go
orchestrate.QdrantVolumeName()        // backup/restore volume identity
orchestrate.QdrantContainerUnitName() // → unitServiceName(...) → "villa-qdrant.service"
orchestrate.EmbedContainerUnitName()  // → "villa-embed.service"
orchestrate.QdrantImage() / orchestrate.EmbedImage() // probe helper image / manifest
recall.SchemaVersion()                // store.go:40 — manifest field
recall.RecallStatePath()              // store.go:154 — backup source + restore dest
```
`TestSeamGrepGate` (`internal/inference/seam_test.go`) walks `internal/` + `cmd/villa` — no image/`podman` literals in status/backup/modelswap cores.

### Typed-Unknown degradation
**Sources:** `naOffloadVerdict` (`status.go:71-77`), `liveReadUsage` nil-on-absent (`cmd/villa/status.go:226-248`), `blockOnNewerStore` not-recorded rule (`backup.go:322-323`), `recall.Load` fail-closed-to-empty (`store.go:111-132`).
**Apply to:** all new probes, the Memory section, the manifest skew compare, the D-10 guard. Absent/unevaluable → WARN/omit/no-alarm; never a fabricated PASS, never a fabricated FAIL.

### Append-only + schema-bump golden evolution
**Sources:** `Report` tail-append history (`status.go:106-156`), Phase-22 recommend 1→2 precedent, `Manifest` layout (`manifest.go:70-102`).
**Apply to:** the 2→3 status bump (ONE evolution, ONE `-update` re-freeze — define the COMPLETE v3 field set up front, Pitfall 1) and the manifest growth (its own self-version discipline).

### Refuse-with-remediation
**Sources:** `SkewWarning{Field, Detail, Remediation}` (`backup.go:219-223`), recall-index refusal prints (`recall.go:308,330,372`).
**Apply to:** dimension-skew confirm, D-10 refusal/WARNs. Every guard names the consequence AND the fix; never silent, never auto-reindex (D-11).

### Pure-core + injectable Deps
**Sources:** `status.Deps` (`status.go:205-261`), `backup.Deps` (`deps.go:51-124`), `modelswap.Deps` (`modelswap.go:25-47`).
**Apply to:** every new probe/read enters as a `func` field wired in a cmd-tier `live*Deps`; no file I/O inside `status.Run`, no exec inside `backup`.

## No Analog Found

None. Every Phase-23 behavior is a composition of an existing proven mechanism pointed at one more target (RESEARCH "Don't Hand-Roll" table). Any plan task inventing a new mechanism should be challenged.

One verify-during-planning item (not a missing analog): `orchestrate.Reconcile`'s obsolete-unit behavior for the memory-off-restored-onto-memory-on matrix (RESEARCH A3 — check `internal/orchestrate/reconcile.go`).

## Metadata

**Analog search scope:** `internal/status`, `internal/backup`, `internal/modelswap`, `internal/recall`, `internal/orchestrate`, `internal/doctor`, `internal/dashboard` (+ assets), `cmd/villa`
**Files read this session:** 14 (status.go, cmd/villa/status.go, backup.go, restore.go, manifest.go, deps.go, modelswap.go, recall.go §280-400, install_memory.go §230-341, store.go, orchestrate/memory.go, api.go §1-60, dashboard.js §130-220, cmd/villa/{backup,restore,doctor}.go targeted, internal/doctor/doctor.go §350-480)
**Pattern extraction date:** 2026-06-10
