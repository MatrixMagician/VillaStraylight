# Phase 16: Backup / Restore - Pattern Map

**Mapped:** 2026-06-07
**Files analyzed:** 11 (8 new, 3 modified)
**Analogs found:** 11 / 11 (every file has a strong in-tree analog — this phase is ~80% composition)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/backup/backup.go` (new) | service (pure core) | file-I/O / transform | `internal/usage/usage.go` (pure core + Deps) + `archive/tar` | role-match |
| `internal/backup/restore.go` (new) | service (pure core) | event-driven (state-machine) | `internal/backendswap/backendswap.go` (`Run`) | exact |
| `internal/backup/manifest.go` (new) | model + transform | transform (skew compare) | `internal/usage/usage.go` (schema-versioned doc) + `internal/benchstore` | role-match |
| `internal/backup/checksum.go` (new) | utility | transform | `crypto/sha256` (stdlib; RESEARCH §Code Examples) | stdlib-pattern |
| `internal/backup/tarutil.go` (new) | utility | file-I/O | `config.assertInsideDir` (`villaconfig.go:223`) + `usage.WriteFileAtomic` | role-match |
| `internal/backup/deps.go` (new) | service (seam) | — | `backendswap.Deps` (`backendswap.go:56`) | exact |
| `cmd/villa/backup.go` (new) | controller (cobra) | request-response | `cmd/villa/uninstall.go` (podman var + Deps + live wiring) | exact |
| `cmd/villa/restore.go` (new) | controller (cobra) | request-response | `cmd/villa/backend.go` (`runBackendSet` + `liveBackendSwapDeps`) | exact |
| `internal/orchestrate/openwebui.go` (modify) | config (accessor add) | request-response | existing `openWebUIImage` const (`openwebui.go:20`) | exact |
| `cmd/villa/root.go` / new `version.go` (modify/new) | config | — | NONE (no version constant exists — OQ1) | no-analog |
| `internal/backup/*_test.go` (new) | test | — | `usage_test.go` + `metrics/llamacpp_test.go` (structural narrow-field) | exact |

---

## Pattern Assignments

### `cmd/villa/backup.go` + `cmd/villa/restore.go` — podman volume export/import seam (controller)

**Analog:** `cmd/villa/uninstall.go` lines 344–398 (`volumeRmArgs` + `podmanVolumeRm` + `removeVolumesLive`)

**This is the seam-gate-proven template (D-02).** Clone the pure arg-builder + package-level fixed-arg `podman` var. The arg-builder is pure (testable); the var is swappable for a fake in `*_test.go`.

Pure arg-builders to add (mirror `volumeRmArgs` at `uninstall.go:352`):
```go
func volumeExportArgs(name, out string) []string {
	return []string{"volume", "export", name, "--output", out} // fixed args, no shell
}
func volumeImportArgs(name, src string) []string {
	return []string{"volume", "import", name, src}
}
```

Fixed-arg exec var (clone of `podmanVolumeRm`, `uninstall.go:361-367`):
```go
var podmanVolume = func(args []string) (stderr string, err error) {
	var buf bytes.Buffer
	cmd := exec.Command("podman", args...) // fixed args (T-03-25: never a shell)
	cmd.Stderr = &buf
	err = cmd.Run()
	return strings.TrimSpace(buf.String()), err
}
```

**Not-found tolerance pattern** for the clean-recreate `podman volume rm` on restore (clone `removeVolumesLive`, `uninstall.go:377-398`) — inspect trimmed stderr for `"no such volume"` / `"no volume with name"` and treat as success rather than relying on an unsupported flag. This is load-bearing for the clean-recreate-before-import step (RESEARCH Pitfall 1/2).

**ErrToolNotFound guard** (`uninstall.go:381-383`): `exec.LookPath("podman")` → `orchestrate.ErrToolNotFound{Tool:"podman"}` before any volume op.

**cobra command shell + exit-code-return** (`uninstall.go:89-110`): `newBackup()`/`newRestore()` build the `*cobra.Command`; `RunE` calls `run*` which RETURNS the exit code, then `os.Exit(code)` in the closure (mirrors `runUninstall`/`runBackendSet`). Use the existing `exitPass`/`exitBlocked` constants.

**Interactive consent for restore skew** (`uninstall.go:244-256`, `resolveModelChoice`): `d.interactive()` (TTY check) + `d.consent(prompt)` for the `y/N` gate; `--yes`/`--force` flag bypasses (D-08). Reuse `stdinIsInteractive` / `promptConsent` (already wired in `liveUninstallDeps`, `uninstall.go:303-304`).

---

### `cmd/villa/restore.go` — live Deps wiring (controller)

**Analog:** `cmd/villa/backend.go` lines 378–481 (`liveBackendSwapDeps`) — the closure-per-seam wiring pattern.

Concrete reuse for the restore live deps (each is a `func` field on the new `backup.Deps`):
- **Config restore:** `SaveConfig: config.SaveVilla` (atomic 0600/0700, traversal-guarded — NEVER hand-write; `backend.go:383`).
- **Volume recreate via Quadlet** (D-07): clone the `ReconcileAndWrite` closure (`backend.go:430-466`) — `orchestrate.Render(RenderInput{Backend, Cfg, ModelFile, ModelsDir})` → `orchestrate.Reconcile` → `orchestrate.WriteUnits(plan, dir)` → `sys.DaemonReload()`. Then materialize+import the OWUI volume (OQ2: may need explicit `podman volume create` or one `sys.Start` cycle before import).
- **Quiesce:** `sys := orchestrate.NewSystemd()`; `Stop: sys.Stop`, `Start: sys.Start`, `Restart: sys.Restart` (`systemd.go:99,125,131`). Stop `villa-openwebui.service` before export/import; restart in a defer-style cleanup (RESEARCH §Quiesce).
- **Prove (offload-asserting):** clone `Prove: liveProve` + the `PreflightROCm` closure (`backend.go:402-418`, `479`) — re-run `preflight.RunROCmForImage(detect.Probe(), b.Image())` and assert `status` residency. Switch-to-success ONLY on a true pass (mirror `ProveStatusPass`, see restore.go below).

---

### `internal/backup/restore.go` — transactional state-machine (pure core)

**Analog:** `internal/backendswap/backendswap.go` lines 145–253 (`Run`) — **clone the frame wholesale (D-06).**

**Result type** (clone `backendswap.Result`, `backendswap.go:97-127`): fields `Refused / Restored / RolledBack / NoOp / Reason / Err / FailedStep / Prove`. Keep the doc-comment discipline that `RolledBack` stays true even when a rollback STEP errored (Pitfall 5).

**Prove sentinel** (clone `ProveStatusPass`, `backendswap.go:36` + `ProveVerdict`, `:43-50`): define the success constant LOCALLY in `internal/backup` so the package imports neither `inference` nor `detect` and stays literal-free. Cutover succeeds ONLY on `v.Status == ProveStatusPass`; a ready+health-200-but-residency-FAIL verdict triggers rollback.

**Capture-strictly-before-mutation** (`backendswap.go:176-183`): capture the CURRENT `villa-openwebui` volume export + `config.toml` + `usage.json` + bench reports to a temp rollback set BEFORE any mutation. An uncapturable current state → `Refused` with zero side effects (Pitfall 4).

**rollback() / rolledBack() closures** (`backendswap.go:191-231`): clone verbatim — `rollback()` accumulates errors across ALL restore steps (never aborts on first), returns `(ok bool, detail string)`; `rolledBack()` folds an honest "rolled back, but the restore did not fully complete (...)" message when `!ok` (Pitfall 5 — never claim a clean no-op when rollback errored).

**Ordered sequence** (RESEARCH §Transactional Restore, mirroring `backendswap.go:145-253`):
1. read+verify archive (SHA-256, fail-closed BLOCK on mismatch/incompatible schema)
2. skew check + WARN-and-confirm (legitimate skew does NOT block)
3. CAPTURE current state
4. QUIESCE (`Stop` OWUI)
5. MUTATE (`SaveConfig` → restore data-dir files → clean-recreate volume + `import` → `Start`) — ANY error → `rolledBack(...)`
6. PROVE (preflight + status residency) — non-pass → `rolledBack("prove", v.Detail, nil, v)`

---

### `internal/backup/deps.go` — injectable seam (service)

**Analog:** `backendswap.Deps` (`backendswap.go:52-93`).

Every host-touching action is a `func` field with a doc comment (RESEARCH §Responsibility Map for the field list): `LoadConfig`, `SaveConfig`, volume export/import, file r/w, `Stop`/`Start`/`Restart`, `ReconcileAndWrite` (Quadlet recreate), `DaemonReload`, `Prove`. Include service-name fields (e.g. `OpenWebUIServiceName`) as Deps fields so the pure core need not import cmd-layer constants (mirror `InstallServiceName`, `backendswap.go:92`).

---

### `internal/backup/tarutil.go` — tar-slip guard + atomic write (utility)

**Analog A (traversal guard):** `config.assertInsideDir` (`villaconfig.go:223-240`) — the `filepath.Rel`-based escape check. Also cloned (not imported) in `usage.go:223` and `uninstall.go:325` (`assertUnitInsideDir`). Clone this shape per-archive-entry on extraction (D-11) AND on the archive output path on write:
```go
rel, err := filepath.Rel(absDir, absPath)
if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
	return fmt.Errorf("backup: refusing %q outside %q", absPath, absDir)
}
```

**Analog B (atomic temp+rename):** `usage.WriteFileAtomic` (`usage.go:248-287`) — same-dir `os.CreateTemp` → `Chmod 0600` → write → `Close` → `os.Rename` → chmod-tighten, with `cleanup()` on every error path. Reuse this discipline for the restored data-dir artifacts and the archive output (`0600` files, `0700` dirs — `storeFileMode`/`storeDirMode`, `usage.go:42-43`).

**Tar assembly** (RESEARCH §Code Examples): `archive/tar` with `tar.FormatPAX`, deterministic entry order (manifest.json FIRST), `Mode: 0o600`.

---

### `internal/backup/manifest.go` — schema-versioned doc + skew compare (model)

**Analog:** `usage.UsageTotals` (`usage.go:64-70`) and `benchstore` `savedReportSchemaVersion` (`benchstore.go:32`).

**Schema-version-field-LAST, append-only** (`usage.go:67-70`): `Manifest` carries `SchemaVersion int json:"schema_version"` as the LAST field; new fields append ABOVE it; bump the const on a breaking change. Use a backup-owned const (mirror `usageSchemaVersion = 1`, `usage.go:36`).

**XDG data-dir resolver** for locating the artifacts to back up: `usage.UsagePath()` (`usage.go:209-218`) → `$XDG_DATA_HOME/villa/usage.json` (fallback `~/.local/share/villa`, then `/var/tmp/villa`); and `benchstore.benchReportsPath()` (`benchstore.go:300`) for bench reports. Record `usageSchemaVersion` and `savedReportSchemaVersion` in the manifest (D-09).

**Skew compare** is pure (table in RESEARCH §Skew Detection): WARN-and-confirm on version/digest/host mismatch; fail-closed BLOCK only on SHA-256 mismatch or unreadable/newer `manifest.schema_version` (mirror `usage.Load`'s future-fail-closed at `benchstore.go:277`).

---

### `internal/backup/checksum.go` — SHA-256 (utility)

**Analog:** stdlib `crypto/sha256` (RESEARCH §Code Examples, no in-tree analog needed):
```go
func sum(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil { return "", err }
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

---

### `internal/orchestrate/openwebui.go` (MODIFY) — exported OWUI image accessor

**Analog:** existing `openWebUIImage` const (`openwebui.go:20`) and the inference `Backend.Image()` method (`inference.go:58`, impl `backend_vulkan.go:69`).

**The ONE new seam-resident addition (D-10).** Add an exported getter so the manifest sources the OWUI digest without re-typing a literal:
```go
// OpenWebUIImage returns the digest-pinned Open WebUI image so the manifest can
// record it WITHOUT re-typing the literal (D-10). The literal stays behind the
// orchestrate seam (managed-service constant, not an inference-backend token).
func OpenWebUIImage() string { return openWebUIImage }
```
`internal/backup` + `cmd/villa` then read the digest via this accessor — never as a literal. The inference image digest comes from `inference.BackendFor(cfg.Backend).Image()` (`backend.go:24` + `inference.go:58`), as already done at `status.go:323`.

---

## Shared Patterns

### Path-traversal guard (tar-slip + output)
**Source:** `config.assertInsideDir` (`villaconfig.go:223-240`) — cloned 3× already (`usage.go:223`, `uninstall.go:325`).
**Apply to:** every archive entry on extraction AND the archive output path on write (`tarutil.go`). `filepath.Rel`-based — never ad-hoc `strings.HasPrefix`.

### Atomic write + owner-only perms
**Source:** `usage.WriteFileAtomic` (`usage.go:248-287`); `config.SaveVilla` (`villaconfig.go:150-177`).
**Apply to:** restored data-dir artifacts and archive output. Files `0600`, dirs `0700`. Config restore goes through `config.SaveVilla` (NEVER hand-written TOML).

### Image digest sourced from the seam (never re-typed)
**Source:** `inference.BackendFor(name).Image()` (`backend.go:24`); new `orchestrate.OpenWebUIImage()`.
**Apply to:** `internal/backup/manifest.go` + `cmd/villa` wiring. Keeps `TestSeamGrepGate` green (`inference/seam_test.go:34`). NOTE: the gate's `container image literal` regex (`seam_test.go:54`) matches `kyuz0|docker.io/|:rocm-...` — it does NOT match `ghcr.io/open-webui`, but the accessor is still correct discipline (Pitfall 6).

### Fixed-arg podman exec (no shell)
**Source:** `podmanVolumeRm` (`uninstall.go:361`).
**Apply to:** all `podman volume export/import/rm/create` ops. `exec.Command("podman", args...)` — never a shell; volume/model names are config/catalog-resolved (D-02, T-03-25).

### Transactional capture→prove→rollback with honest reporting
**Source:** `backendswap.Run` (`backendswap.go:145-253`).
**Apply to:** `internal/backup/restore.go`. Capture before mutate; prove on a true pass only; rollback accumulates errors and reports complete vs incomplete.

### Structural narrow-field / no-content security test
**Source:** `metrics.TestParseSlotsReadsOnlyNarrowFields` (`llamacpp_test.go:268-277`) + `usage.TestUsageTotalsHasNoContentFields` (`usage_test.go:138-196`).
**Apply to:** any new manifest/identity struct that must carry counts/identity only — `reflect.TypeOf(T{})` over an allow-set of field names + a marshaled-JSON denylist (`"prompt_text","response","content","text","messages"`). Use for the `Manifest`'s excluded-model-identity records to assert no prompt/content leaks into the archive.

### cobra command returns exit code; live*Deps wiring; fake*Deps test double
**Source:** `runUninstall` + `liveUninstallDeps` (`uninstall.go:115,284`); `runBackendSet` + `liveBackendSwapDeps` (`backend.go:296,378`).
**Apply to:** `cmd/villa/backup.go` / `restore.go`. Thin cobra caller → `run*` returns exit code → `os.Exit` in `RunE`; `live*Deps()` closures wire the seams; `fake*Deps` in `*_test.go` drives the whole flow off-hardware and asserts ordering.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/villa/version.go` (or `root.go` `Version:`) | config | — | **No villa version constant exists** (OQ1 / A1): `cobra.Version` is unset, no build stamp. Manifest `villa_version` (D-09) has no source. RESEARCH recommends `var version = "dev"` in `cmd/villa` + `-ldflags "-X main.version=..."` in the Makefile. New pattern — no in-tree analog. If deferred, manifest records `"dev"`/`"unknown"` and version-skew is WARN-only. |

---

## Metadata

**Analog search scope:** `cmd/villa/` (uninstall.go, backend.go, root.go), `internal/backendswap/`, `internal/config/`, `internal/orchestrate/` (openwebui.go, render.go, systemd.go, reconcile.go), `internal/inference/` (backend.go, inference.go, backend_vulkan.go, seam_test.go), `internal/usage/` (usage.go, usage_test.go), `internal/benchstore/`, `internal/metrics/` (llamacpp_test.go), `internal/status/status.go`.
**Files scanned:** ~18 source + 3 test.
**Pattern extraction date:** 2026-06-07
