# Architecture Research

**Domain:** Operability features layered onto a shipped Go CLI control plane (pure-core + injectable-seam)
**Researched:** 2026-06-07
**Confidence:** HIGH (grounded in the actual v1.1 codebase — seam_test.go, status.go, bench.go, preflight.go, config.go, systemd.go, install.go, uninstall.go all read directly)

> This is an INTEGRATION study, not an ecosystem study. The architecture is fixed
> (pure cores in `internal/*`, host effects behind `Deps` func-field seams, the
> `orchestrate` module the only intentionally-impure one). The question for every
> v1.2 feature is: *where does it slot in without violating an invariant?* Each
> section below gives concrete integration points (exact files), new-vs-modified,
> golden-contract impact, and the invariant it must respect.

## Standard Architecture

### System Overview (current, with v1.2 additions marked ⊕)

```
┌──────────────────────────────────────────────────────────────────────────┐
│  COMMAND TIER  cmd/villa/*.go  (thin cobra; live*Deps wiring; exit codes)  │
│  detect recommend preflight install up/down/restart logs status model      │
│  backend bench dashboard uninstall                                          │
│  ⊕ doctor.go   ⊕ backup.go   ⊕ bench.go(--compare)   ⊕ install.go(TUI)      │
├──────────────────────────────────────────────────────────────────────────┤
│  PURE CORE TIER  internal/*  (no host I/O; behind a Deps struct of funcs)   │
│  detect  recommend  catalog  preflight  inference  backendswap  bench       │
│  status  metrics  modelswap  download  config                               │
│  ⊕ doctor (compose preflight+status)   ⊕ backup (plan/verify, pure)         │
│  ⊕ benchstore (frozen saved-report codec)   ⊕ usage (cumulative fold)       │
├──────────────────────────────────────────────────────────────────────────┤
│  IMPURE EDGE  internal/orchestrate  (Render/Reconcile PURE; systemd.go +    │
│  WriteUnits touch host) — the ONLY intentionally-impure first-party module  │
│  ⊕ host seams for: podman volume export/import (backup), benchstore + usage │
│     file read/write — modeled on systemd.go runTool / uninstall podmanVolumeRm│
├──────────────────────────────────────────────────────────────────────────┤
│  PERSISTENT STATE   config.toml (XDG, single source of truth)               │
│  Quadlet units (regenerated, never authoritative)  on-disk model weights    │
│  ⊕ ~/.local/share/villa/bench/*.json  (saved reports)                       │
│  ⊕ ~/.local/share/villa/usage.json   (cumulative usage)                     │
│  ⊕ backup archive (tar.gz of config.toml + OpenWebUI volume export)         │
├──────────────────────────────────────────────────────────────────────────┤
│  MANAGED OSS CONTAINERS  villa-llama  villa-openwebui  + villa-dashboard svc │
└──────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities (v1.2 additions)

| Component | Responsibility | Implementation |
|-----------|----------------|----------------|
| `internal/doctor` | Pure diagnostics core: compose `preflight.RunWithResources` + `status.Run` into one `Report` of typed findings + remediation | NEW pure core; imports preflight + status, no host I/O |
| `cmd/villa/doctor.go` | `villa doctor` cobra surface; live wiring (reuse `liveStatusDeps` + preflight live deps); exit mapping; `--json` | NEW thin caller |
| `internal/backup` | Pure backup *plan/manifest* + restore *verification* (what to capture, version stamp, integrity check) — NO tar/podman I/O | NEW pure core |
| backup host seam | `podman volume export/import` + tar of config.toml | NEW funcs in `internal/orchestrate` (the sanctioned impure edge), modeled on `podmanVolumeRm` |
| `cmd/villa/backup.go` | `villa backup` / `villa restore` cobra surface; live seam wiring; consent on destructive restore | NEW thin caller |
| `internal/benchstore` | Frozen, versioned codec for saved bench reports (marshal/unmarshal + dir listing + compare-load) | NEW pure core |
| `cmd/villa/bench.go` | extend with `--compare` (load saved reports) + save-after-run | MODIFIED |
| `internal/usage` | Pure cumulative-usage fold: read prior totals + a new sample → new totals | NEW pure core |
| usage writer | who persists `usage.json` — recommend a poller seam invoked by the dashboard server's existing scrape loop OR a `villa usage` snapshot command | NEW seam |
| `internal/inference/backend_rocm_alt.go` | the `rocm-6.4.4` image literal + its `ContainerArgs`/`ResidencyProof` — behind the seam | NEW seam file (or a variant inside backend_rocm.go) |

## Recommended Project Structure

```
internal/
├── doctor/                 # ⊕ NEW pure core: compose preflight + status
│   ├── doctor.go           #   Report{Checks []preflight.CheckResult; Services []status.ServiceStatus; drift findings}
│   └── doctor_test.go      #   off-host: stub the two injected cores
├── backup/                 # ⊕ NEW pure core: manifest + version stamp + restore verify
│   ├── backup.go           #   Plan(), Manifest, VerifyRestore() — NO tar/podman here
│   └── backup_test.go
├── benchstore/             # ⊕ NEW pure core: frozen saved-report codec
│   ├── benchstore.go       #   SavedReport{schema_version; ...}; Marshal/Unmarshal/List/Compare
│   └── benchstore_test.go  #   golden test freezes the on-disk JSON shape
├── usage/                  # ⊕ NEW pure core: cumulative fold
│   ├── usage.go            #   Totals; Fold(prior, sample) -> Totals; frozen JSON
│   └── usage_test.go
├── inference/
│   ├── backend_rocm.go     # existing rocm-7.2.4
│   ├── backend_rocm_alt.go # ⊕ NEW: rocm-6.4.4 literal + args + markers (seam-locked)
│   └── seam_test.go        # MODIFIED: add rocm-6\.4\.4 to the image-literal pattern
├── orchestrate/
│   ├── systemd.go          # existing impure edge
│   └── volume_io.go        # ⊕ NEW: podman volume export/import seam (fixed-arg exec)
└── preflight/
    └── rocm-policy.json     # MODIFIED: allow/deny rocm-6.4.4 in image policy

cmd/villa/
├── doctor.go               # ⊕ NEW
├── backup.go               # ⊕ NEW (backup + restore subcommands)
├── usage.go                # ⊕ NEW (or fold into status; see USAGE-01)
├── install.go              # MODIFIED: TUI front-end calls the SAME runInstall pipeline
└── bench.go                # MODIFIED: --compare + save
```

### Structure Rationale

- **One new `internal/*` pure core per feature that has decision logic** (doctor, backup, benchstore, usage). This mirrors the existing split exactly: `bench`, `backendswap`, `modelswap`, `status` are all pure cores with a thin cobra caller. A feature with *no* host I/O of its own (doctor composes other cores) is the cleanest fit.
- **Every host effect lands in `internal/orchestrate`** (the sanctioned impure module) or behind a `Deps` func field, never inside a new pure core. Backup's `podman volume export` is the textbook case — it belongs next to `systemd.go`'s fixed-arg exec pattern and `uninstall.go`'s `podmanVolumeRm`, never inside `internal/backup`.
- **Persisted state goes under XDG data dir** (`~/.local/share/villa/`), parallel to how `config` owns the XDG *config* dir. Do NOT co-mingle saved reports / usage with `config.toml` — config is the single source of truth for the *running stack*; reports and usage are append-only history, a different lifecycle (and `uninstall` deliberately preserves config but may want to preserve/wipe history separately).

## Architectural Patterns

### Pattern 1: Compose existing cores, never fork (DOCTOR-01)

**What:** `villa doctor` is a *read-only superset* of `preflight` + `status`. It must NOT re-run a parallel set of checks. `internal/preflight`'s own package doc already names this exact use: *"so Phase 3 `install` and a future `villa doctor` can reuse the exact same checks."*

**Recommendation:** New `internal/doctor` pure core that calls `preflight.RunWithResources(profile, req)` and `status.Run(statusDeps)` and folds both into a single `doctor.Report`. Doctor adds the *drift* dimension that neither core has alone: preflight answers "is the host still prep-ready?" and status answers "is the running stack healthy?" — doctor cross-checks them (e.g. config says backend=rocm but the running unit's residency proof is Vulkan; or a unit is active but preflight now FAILs the GPU check → hardware/driver regressed since install).

**Do NOT extend `status.Report`.** status is byte-frozen by a golden and folded by the dashboard; adding diagnostic/remediation fields there would bloat the dashboard contract and force a schema bump for a CLI-only feature. Keep doctor's richer output in its own `internal/doctor` type with its own (new, therefore unconstrained) golden.

**Where remediation strings live:** they already exist. Every `preflight.CheckResult` carries `Remediation` + `Provenance` (preflight.go), and `status` verdicts carry `Detail`/`Provenance`. Doctor surfaces those verbatim — it does NOT invent a second remediation vocabulary. Any *new* doctor-only finding (drift) gets its remediation string in `internal/doctor`, co-located with the check that emits it (matching the convention that a check owns its remediation).

```go
// internal/doctor/doctor.go (pure; no os.Exit, no print)
type Report struct {
    Checks    []preflight.CheckResult `json:"checks"`
    Services  []status.ServiceStatus  `json:"services"`
    Drift     []Finding               `json:"drift"`          // doctor-only cross-checks
    Overall   string                  `json:"overall"`        // worst-wins, reuse the vocabulary
    Schema    int                     `json:"schema_version"`
}
type Deps struct {
    RunPreflight func() []preflight.CheckResult
    RunStatus    func() status.Report
}
```

### Pattern 2: Impure edge for backup, modeled on the existing volume seam (BAK-01)

**What:** Back up = `config.toml` (a file villa already owns) + the Open WebUI data volume (chats/settings, a podman-managed volume). Restore reverses it.

**Recommendation — two-layer split exactly like orchestrate:**
- `internal/backup` (PURE): builds a `Manifest` (what files/volumes, a version stamp, the source villa version, a checksum plan), and a pure `VerifyRestore` that decides whether an archive is restorable into this install (version compatibility, presence of expected members). No tar, no podman, no filesystem.
- Host seam (IMPURE, in `internal/orchestrate` `volume_io.go`): `VolumeExport(name, w io.Writer)` → `podman volume export <name>` and `VolumeImport(name, r io.Reader)` → `podman volume import <name>`, both FIXED-ARG `exec.Command("podman", ...)` with `exec.LookPath` degradation, byte-for-byte the shape of `uninstall.go`'s `podmanVolumeRm` and `systemd.go`'s `runTool`. This keeps the `podman` invocation behind the seam gate's blessed zones.

**Why orchestrate and not a new impure module:** the invariant is "orchestrate is the ONLY intentionally-impure first-party module." A standalone `internal/backup` that shelled to podman would *create a second impure module* and violate that. The pure `internal/backup` core + an orchestrate-resident host seam preserves it. (The seam gate already permits `podman` invocations in `internal/inference` and `cmd/villa`; for the backup seam, place the exec in `internal/orchestrate` and confirm the gate — see Anti-Pattern below — or place the fixed-arg exec in `cmd/villa/backup.go` like `uninstall.go` already does for `podman volume rm`. The cmd-tier placement is the *lowest-risk* option because `uninstall.go` proves it passes the gate today.)

**Restore is destructive → consent.** Restore overwrites live chat data. Reuse the existing `consent`/`interactive` seam pattern (install/uninstall) and the `--force` posture. Stop the OpenWebUI service before `volume import`, restore, restart — the same ordered-lifecycle discipline uninstall encodes.

```go
// host seam (cmd/villa/backup.go OR internal/orchestrate/volume_io.go) — fixed-arg, like podmanVolumeRm
func volumeExport(name string, w io.Writer) error // podman volume export <name>
func volumeImport(name string, r io.Reader) error // podman volume import <name>
```

### Pattern 3: Frozen, versioned saved-report format (BENCH-03)

**What:** Persist each `bench.Result` so `villa bench --compare` can load and diff runs over time / across models.

**Recommendation:** NEW `internal/benchstore` pure core that owns a `SavedReport` struct with its OWN `schema_version` and frozen JSON shape (golden test in `internal/benchstore`). It wraps `bench.Result` plus provenance the live run already collects (the cmd-tier `benchEntry`/`benchConditions`/`benchAB` shapes in `bench.go` — model, quant, ctx, backend, image, timestamp, host). Do NOT persist the raw in-memory `bench.Result` — that type is a *return value*, not a contract; freezing it on disk would couple the file format to an internal type that may evolve. The store's `SavedReport` is the disk contract; `bench.Result` maps into it.

**Storage location:** `~/.local/share/villa/bench/<timestamp>-<backend>-<model>.json` (XDG data dir). Listing + compare-load + the worst-wins/delta math stay PURE in `benchstore`; only the directory read/write is a `Deps` seam (or a small live wiring in `cmd/villa/bench.go`).

**`--compare` semantics:** the existing `bench --ab` compares two *live* backends in one run. `--compare` instead loads N *saved* reports and tables their deltas — it performs NO live measurement and NO backend switching, so it cannot regress the honest-A/B invariants (those only apply to live runs). Keep them as distinct code paths.

**Honesty carries over:** a saved report MUST persist the `VoidExhausted`/`Reason` fields so a low-confidence historical run is never silently compared as authoritative. The `benchstore` golden freezes those fields in.

### Pattern 4: TUI as a pure front-end over the existing pipeline (INSTALL-01)

**What:** An interactive terminal UI for first-run install, alongside the flag-driven `villa install`.

**Recommendation — the TUI is a *presentation seam*, not a second pipeline.** `runInstall(cmd, opts, deps)` already returns an exit code and drives every host effect through `installDeps` func fields; it already supports an interactive `consent`/`interactive` seam. The TUI must:
1. Drive the SAME `detect.Probe` → `recommend.Pick` → `preflight.RunWithResources` → `orchestrate.Render/Reconcile/WriteUnits` → systemd pipeline by calling the SAME cores (or `runInstall` itself), never duplicating the detect/recommend/preflight logic.
2. Inject its `consent` and progress as `Deps` func fields (the install flow already prompts via `d.consent` and announces via `fmt.Fprintf(out,...)`; bench already has an `OnSideStart` observational seam — the same pattern gives the TUI live progress without the core knowing it's a TUI).
3. Keep ALL terminal/render I/O in `cmd/villa` (or a `cmd/villa/tui` subpackage). NO pure `internal/*` core may import a TUI library — that would leak presentation/host I/O into a pure core and break off-host testability.

**Dependency decision (flag for roadmap):** there is NO TUI library in `go.mod` today (verified — only cobra/chi/ghw/toml). INSTALL-01 introduces the project's first interactive-UI dependency. Options: `charmbracelet/bubbletea` (de-facto Go TUI standard, larger dep tree) vs. a minimal hand-rolled prompt loop over `golang.org/x/term` (keeps the single-static-binary/minimal-dep posture). This is a STACK decision the roadmap must make; architecturally either fits as long as it lives only in the command tier. Given the project's stated minimal-dependency, single-binary value, a thin prompt loop or a small TUI lib is preferable to a heavy framework.

### Pattern 5: Second ROCm image strictly behind the seam (ROCM-ALT-01)

**What:** Add `rocm-6.4.4` as an alternate ROCm image tuned for TG-heavy models (the v1.1 Δtg −11.15 regression motivates it).

**Recommendation:** This is the *cleanest* of the six — the seam was explicitly built for it. The image literal, device args, HSA env, and residency markers live in `internal/inference` and ONLY there. Two viable shapes:
- A new `backend_rocm_alt.go` sibling exposing a third `Backend` implementation, selected via a new `BackendFor` case (e.g. `"rocm-6.4.4"` or a `backend = "rocm"` + a `rocm_image` config knob).
- OR parameterize `backendROCm` by image while keeping markers shared (6.4.4 and 7.2.4 likely share the `ROCm0`/HSA markers).

**Three invariants to honor, all mechanically enforced:**
1. **`TestSeamGrepGate` (seam_test.go) hardcodes the image pattern `rocm-7\.2\.4|rocm7-nightlies`.** Adding a `rocm-6.4.4` literal anywhere will NOT be caught as a leak by the *current* pattern — meaning a leak outside the seam would pass CI silently. You MUST extend the pattern to `rocm-7\.2\.4|rocm-6\.4\.4|rocm7-nightlies` so a 6.4.4 leak in a caller fails the build. This is a one-line edit to `seam_test.go` (MODIFIED) and is itself the regression guard for the new image.
2. **`rocm-policy.json` is the allow/deny authority.** The `imageDeny` list governs forbidden tags (today `rocm7-nightlies`). Adding 6.4.4 as an *allowed* alt may require an explicit allow concept or simply NOT being on the denylist; preflight's `checkROCm` reads this policy. The policy file is embedded (`go:embed`) so the change is build-time, not runtime input.
3. **`TestROCmMarkerPresence`** asserts `ROCm0`/`HSA_OVERRIDE_GFX_VERSION`/`/dev/kfd` stay in `backend_rocm.go`. If 6.4.4 lives in a new file, ensure the positive presence test covers it too (or that the shared markers remain in the original file).

**Golden impact:** a new ROCm-alt backend that can be rendered produces a new Quadlet container golden (`villa-llama-rocm-alt.container.golden`) — additive, freeze once with `-update`. `status`/`detect --json` may surface the chosen image; if so, that is an append-only + schema-bump change (see below).

### Pattern 6: Append-only golden evolution for any surfaced state (USAGE-01, and all surfacing)

**What:** Cumulative usage (tokens/throughput over time) surfaced in `status`/dashboard, not just live.

**Recommendation — separate the *store* from the *surfacing*:**
- **Store:** NEW `internal/usage` pure core owning `Totals` (cumulative tokens, total generation time, run count, per-backend breakdown) with its OWN frozen JSON (`usage.json`, schema-versioned) under the XDG data dir. `Fold(prior, sample) -> Totals` is pure.
- **Who writes it (key decision):** the live tok/s already flows through `internal/metrics.ScrapeMetrics` (llama.cpp `/metrics`: `predicted_tokens_seconds`, etc.). The dashboard server already runs a scrape loop. RECOMMENDATION: a small *usage poller seam* invoked by the dashboard server's existing poll (it is the only long-lived first-party process — the CLI is one-shot). The poller reads the metrics counter, folds a delta into `usage.json` via `internal/usage`, guarded by a mutex (the dashboard already guards its one cached value with a `sync` mutex). The CLI `villa status`/a new `villa usage` *reads* the file; it does not write it (a one-shot CLI can't accumulate). Note llama.cpp `/metrics` exposes monotonic *counters* (e.g. `prompt_tokens_total`/`tokens_predicted_total`) as well as rate gauges — fold from the counters for cumulative totals, not the rate gauges (which are last-window snapshots, per metrics.go's Pitfall-3 note). **Confirm the exact counter names against the running llama-server `/metrics` before wiring (MEDIUM confidence on names; HIGH on the pattern).**
- **No telemetry invariant:** usage.json is strictly local, never transmitted — consistent with the no-telemetry posture. Flag in PITFALLS that usage tracking must not become an outbound signal.

**Surfacing = append-only golden change.** This is the most invariant-sensitive part. Both `status.Report` (`internal/status`, frozen by `cmd/villa/testdata/status.json.golden`) and the dashboard read-models (`internal/dashboard/api.go`) are byte-frozen. Adding cumulative usage to `status --json`:
- Add the new field(s) **above** `SchemaVersion` (which `status.go` documents as *"MUST stay the LAST tagged field"*), as a `*float64`/typed-optional with `omitempty` where a missing reading must be omitted (mirroring `GenTokensPerSec`).
- Bump `reportSchemaVersion` (currently `1`).
- Re-freeze `status.json.golden` exactly ONCE with `go test ... -update`, as a pure-addition diff (the Key-Decision precedent: *"--json/goldens re-freeze exactly once ... append-only, schema-bumped, never reordered"*).
- The dashboard `metricsView`/`gpuView` are SEPARATE JSON contracts; a new cumulative-usage panel is a new read-model (`usageView`) or appended fields — same append-only rule, but those views are not golden-frozen in cmd/villa today (they're served by handlers), so the constraint is the documented byte-frozen-contract convention rather than a specific golden file. Treat additively regardless.

## Data Flow

### villa doctor (read-only diagnosis)

```
villa doctor
   ↓ liveStatusDeps + preflight live deps
detect.Probe ─┬→ preflight.RunWithResources(profile, req) ──┐
              └→ status.Run(statusDeps) ────────────────────┤
                                                            ↓
                              doctor.Run(deps) folds both + drift cross-checks
                                                            ↓
                              doctor.Report → cmd maps to exit 0/2/1 + table/--json
```

### Backup / restore

```
villa backup
   ↓
backup.Plan(cfg) (pure: manifest + version stamp)
   ↓
[host seam] read config.toml  +  podman volume export villa-openwebui-data
   ↓
tar.gz archive  (at user-chosen path)

villa restore <archive>
   ↓
backup.VerifyRestore(manifest, thisInstall) (pure: version/member compat) → refuse-with-reason if incompatible
   ↓ consent (destructive)
[host seam] systemctl --user stop villa-openwebui → podman volume import → write config.toml → restart
```

### bench --compare (no live measurement)

```
villa bench --compare
   ↓
[seam] list ~/.local/share/villa/bench/*.json
   ↓
benchstore.Load(...) → []SavedReport (carries VoidExhausted/Reason honesty flags)
   ↓
benchstore.Compare(reports) (pure deltas, never blended pp/tg) → table/--json
```

### Cumulative usage (write path = dashboard poller; read path = CLI/dashboard)

```
dashboard poll loop ──(existing)──→ metrics.ScrapeMetrics(/metrics counters)
   ↓ (mutex-guarded)
usage.Fold(prior, sample) → Totals → write usage.json   [the ONLY writer]

villa status / villa usage / dashboard usageView ──→ READ usage.json (never write)
```

## Anti-Patterns

### Anti-Pattern 1: A new impure module for backup

**What people do:** put `exec.Command("podman", "volume", "export", ...)` inside `internal/backup`.
**Why it's wrong:** violates the *"orchestrate is the ONLY intentionally-impure first-party module"* constraint and adds a second place host I/O hides. It also re-introduces a `podman` literal in a non-blessed package — the seam gate permits `podman` invocations in `internal/inference` and `cmd/villa`, not in arbitrary cores.
**Do this instead:** pure `internal/backup` (manifest + verify) + the fixed-arg podman seam in `internal/orchestrate` or in `cmd/villa/backup.go` (the latter proven to pass the gate by `uninstall.go`'s `podmanVolumeRm`).

### Anti-Pattern 2: Adding a `rocm-6.4.4` literal without extending the seam gate pattern

**What people do:** add the new image const in the seam, ship it, assume the gate covers it.
**Why it's wrong:** `seam_test.go`'s image pattern is the *literal string* `rocm-7\.2\.4|rocm7-nightlies` — it does NOT match `rocm-6.4.4`. A future accidental leak of the 6.4.4 tag into a caller would pass CI silently, eroding the very invariant the gate exists for.
**Do this instead:** extend the regexp to include `rocm-6\.4\.4` in the SAME commit that adds the image. The gate change IS the regression guard.

### Anti-Pattern 3: Extending status.Report with diagnostic/remediation prose

**What people do:** bolt doctor's drift findings + remediation onto `status.Report` so the dashboard "gets it for free."
**Why it's wrong:** `status.Report` is byte-frozen and folded by the dashboard; every addition forces a dashboard schema bump and golden re-freeze for a CLI-only diagnostic concern, and bloats a contract designed to be a lean health read-model.
**Do this instead:** doctor owns its own `internal/doctor.Report` with its own (new, unconstrained) golden; it *composes* status, it does not mutate it.

### Anti-Pattern 4: A TUI that re-implements detect/recommend/preflight

**What people do:** write fresh probe/recommend/gate logic inside the TUI for "tighter control" of the flow.
**Why it's wrong:** forks the pipeline, drifts from the flag-driven path, leaks host I/O into presentation, and double-maintains the most security-sensitive logic (preflight gating, fit math).
**Do this instead:** the TUI calls the SAME cores / `runInstall`, injecting consent + progress as `Deps` func fields (the `OnSideStart`/`consent` precedent). All terminal I/O stays in the command tier.

### Anti-Pattern 5: Reordering or inserting fields before `SchemaVersion`'s required tail position incorrectly

**What people do:** append a usage field after `SchemaVersion`, or reorder existing fields.
**Why it's wrong:** `status.go` documents `SchemaVersion` as the LAST tagged field; new fields go ABOVE it, never after, and existing order is frozen. Reordering breaks the byte-frozen golden semantics even if values are unchanged.
**Do this instead:** new typed-optional field above `SchemaVersion`, bump `reportSchemaVersion`, re-freeze the golden exactly once.

## Integration Points

### External Services / Tools

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| podman volume export/import (backup) | fixed-arg `exec.Command`, `exec.LookPath` degradation | Model on `uninstall.go` `podmanVolumeRm`; `villa-openwebui` data volume is the target |
| llama.cpp `/metrics` counters (usage) | bounded loopback GET via existing `internal/metrics` | Use monotonic *counters* for cumulative totals, NOT the rate gauges (Pitfall-3); confirm counter names on the live server |
| ROCm 6.4.4 image (kyuz0 toolbox) | digest-pin in the inference seam, like rocm-7.2.4 | Resolve digest via `skopeo inspect` before pinning (precedent in backend_rocm.go) |
| TUI library (install) | command-tier only; first interactive dep | STACK decision: minimal prompt loop vs bubbletea — keep out of pure cores |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| doctor ↔ preflight, status | direct import + `Deps` injection | doctor composes; never forks check logic |
| backup(pure) ↔ orchestrate/cmd(impure podman seam) | `Deps` func fields | preserves the single-impure-module invariant |
| bench cmd ↔ benchstore | direct import; dir read/write via seam | `--compare` is read-only, distinct from live `--ab` |
| usage writer ↔ dashboard poll loop | mutex-guarded fold into usage.json | dashboard is the only long-lived writer; CLI reads |
| inference seam ↔ all callers | `BackendFor` + `Backend` interface only | rocm-6.4.4 literal stays behind it; seam gate enforces |

## Golden / Byte-Frozen Contract Impact Summary (for the roadmapper)

| Feature | Contract touched | Change type | Action |
|---------|------------------|-------------|--------|
| DOCTOR-01 | NEW `internal/doctor` golden | new, unconstrained | freeze once on creation |
| BAK-01 | none of the frozen `--json` contracts | new archive format (versioned in `internal/backup` manifest) | own golden in `internal/backup` |
| BENCH-03 | NEW `internal/benchstore` saved-report golden | new, frozen-on-disk format | freeze once; carry VoidExhausted/Reason |
| USAGE-01 | `status.Report` (`status.json.golden`) + dashboard read-models | APPEND-ONLY + schema bump | new optional field above `SchemaVersion`; bump `reportSchemaVersion`; re-freeze once |
| INSTALL-01 | none (presentation only) | — | no golden change |
| ROCM-ALT-01 | `seam_test.go` image pattern (MODIFIED); possibly new Quadlet container golden; possibly `status`/`detect --json` if image surfaced | gate-pattern extension + additive golden + (optional) append-only schema bump | extend regexp same commit; freeze new container golden once |

## Suggested Build Order (dependency-honoring)

1. **ROCM-ALT-01** (lowest risk, self-contained, no dependents). Pure seam addition: new image behind `BackendFor`, extend `seam_test.go` pattern + `rocm-policy.json` + marker-presence test, freeze the new container golden. Independent — can land first. Also delivers the milestone's perf motivation (Δtg regression) early.
2. **DOCTOR-01** (composes existing cores; no new persistence). Depends only on already-shipped `preflight` + `status`. New `internal/doctor` + `cmd/villa/doctor.go`. No frozen-contract risk.
3. **BENCH-03** — *freeze the `benchstore` saved-report format FIRST*, then wire `--compare` + save. Order matters: the on-disk format is a contract; lock it (golden) before persisting any real reports, so early-saved files never need migration. Builds on the shipped honest-A/B bench core.
4. **USAGE-01** (the most invariant-sensitive surfacing). Build the pure `internal/usage` store + the dashboard-poller writer first, then the append-only `status`/dashboard surfacing as a single schema-bump + one-time golden re-freeze. Sequence after BENCH-03 so only one frozen-contract evolution is in flight at a time.
5. **BAK-01** (new impure seam + destructive restore consent). Independent of the others but higher host-I/O risk; sequence after the read-only features so the safer surface is proven first. Pure `internal/backup` + the orchestrate/cmd podman volume seam.
6. **INSTALL-01** (TUI) LAST. It is a front-end over the whole pipeline and ideally over the *final* command surface (so it can also expose doctor/backup if desired). Introduces the first interactive dependency — a STACK decision best made once the rest of the v1.2 surface is settled. Pure presentation; no core changes.

**Ordering rationale:** seam-locked + composition features first (ROCM-ALT, DOCTOR) — zero or trivial contract risk; then the two persistence features (BENCH-03, USAGE-01) with BENCH-03's format frozen before USAGE-01's status surfacing so only one byte-frozen evolution lands at a time; then the destructive BAK-01; then INSTALL-01 as the capstone front-end over the finished surface.

## Sources

- VillaStraylight v1.1 source (read directly): `internal/inference/seam_test.go`, `internal/inference/backend.go`, `internal/inference/backend_rocm.go`, `internal/status/status.go`, `internal/bench/bench.go`, `internal/preflight/preflight.go`, `internal/preflight/floors.go`, `internal/preflight/rocm-policy.json`, `internal/orchestrate/systemd.go`, `internal/metrics/llamacpp.go`, `internal/dashboard/api.go`, `internal/config/villaconfig.go`, `cmd/villa/install.go`, `cmd/villa/uninstall.go`, `cmd/villa/bench.go` (head) — HIGH confidence.
- `.planning/PROJECT.md` (Key Decisions, v1.2 milestone scope), `CLAUDE.md` (Architecture / Conventions / Architectural Constraints / Anti-Patterns) — HIGH confidence; binding invariants.
- `go.mod` — confirms no TUI dependency exists today (INSTALL-01 stack decision).
- llama.cpp `/metrics` counter-vs-gauge distinction — codebase-documented (metrics.go Pitfall-3) HIGH; exact cumulative counter names need live confirmation — MEDIUM.

---
*Architecture research for: VillaStraylight v1.2 Operability feature integration*
*Researched: 2026-06-07*
