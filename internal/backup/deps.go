// Package backup is the PURE, Deps-injected core for `villa backup` /
// `villa restore` (Phase 16, BAK-01/BAK-02/BAK-03). It builds the
// self-describing manifest, computes and verifies per-entry SHA-256 checksums,
// assembles + extracts the outer single POSIX .tar (with a tar-slip guard), and
// performs the pure version/digest/host skew comparison.
//
// It is deliberately literal-free of any container IMAGE digest and imports
// NEITHER the exec package NOR podman NOR internal/inference / internal/detect: every
// host-touching action (podman volume export/import, file r/w, service
// stop/start, Quadlet recreate, the offload-asserting prove) is an injected
// `Deps` func field, wired by `live*Deps` closures in cmd/villa (later plans).
// It runs NO subprocess and links NO podman bindings — every effect is a seam.
// Image digests reach the manifest ONLY through the seam accessors
// (orchestrate.OpenWebUIImage() / inference.BackendFor(name).Image()) — never as
// a re-typed literal — so internal/inference's TestSeamGrepGate stays green
// (D-01/D-10). This mirrors the proven pure-core + injectable-seam pattern of
// internal/backendswap, internal/usage, and internal/status.
package backup

import "github.com/MatrixMagician/VillaStraylight/internal/config"

// ProveStatusPass is this package's OWN success sentinel for a restore-cutover
// prove verdict. The cmd layer (later plans) sets ProveVerdict.Status to this
// constant when — and ONLY when — a real generation probe AND a positive
// residency proof both pass. Keeping the success marker here (rather than
// importing inference.StatusPass) is exactly what keeps backup free of
// inference/detect imports and of backend literals (mirrors
// backendswap.ProveStatusPass).
const ProveStatusPass = "pass"

// ProveVerdict is the LOCAL prove outcome the restore cutover gates on. It is
// defined here (not imported from inference) so backup imports neither inference
// nor detect and stays literal-free of backend markers. The cmd layer composes
// the real verdict (preflight + status residency) and maps it into this value,
// setting Status to ProveStatusPass only on a true pass — a ready+health-200-
// but-residency-FAIL verdict is NEVER success (D-07, offload-asserting).
type ProveVerdict struct {
	// Status is the prove outcome. The cutover succeeds ONLY when Status equals
	// ProveStatusPass; any other value triggers rollback. A silent CPU fallback
	// MUST map to a non-pass status.
	Status string
	// Detail is the human explanation carried into the Result on a non-pass verdict.
	Detail string
}

// Deps is the injectable seam set for the transactional backup/restore core.
// Every host-touching action is a func field so the whole capture→quiesce→
// swap→restart→prove→rollback flow is driven from *_test.go with a fakeDeps,
// without a live host. The live wiring (liveBackupDeps / liveRestoreDeps) lives
// in cmd/villa (later plans). Mirrors backendswap.Deps.
type Deps struct {
	// LoadConfig loads the current persisted config (the source of truth).
	LoadConfig func() (config.VillaConfig, error)
	// SaveConfig persists a config to config.toml via config.SaveVilla (atomic,
	// 0600/0700, traversal-guarded — NEVER hand-write TOML; D-07).
	SaveConfig func(c config.VillaConfig) error

	// VolumeExport exports the named podman volume to the file at out via the
	// cmd-tier fixed-arg `podman volume export` seam (D-02). Used to capture the
	// current Open WebUI volume (backup + rollback-capture).
	VolumeExport func(name, out string) error
	// VolumeImport imports the tar at src into the named (already-recreated, clean)
	// podman volume via `podman volume import` (D-02). import MERGES, so the volume
	// MUST be freshly recreated before this call (RESEARCH Pitfall 1).
	VolumeImport func(name, src string) error
	// VolumeRm removes the named podman volume, tolerating not-found, so restore can
	// clean-recreate before import (RESEARCH Pitfall 2).
	VolumeRm func(name string) error
	// EnsureVolume ensures the named volume exists (idempotent `podman volume
	// create`, tolerate already-exists) so the subsequent import has a target
	// (OQ2, resolved option b).
	EnsureVolume func(name string) error

	// ReadFile returns the whole bytes of the file at path (or an error). Used to
	// read config.toml / usage.json / bench-reports.jsonl when assembling the
	// archive and to read captured rollback artifacts.
	ReadFile func(path string) ([]byte, error)
	// WriteFileAtomic writes data to path via a same-dir temp + rename, 0600 file /
	// 0700 dir, traversal-guarded (clone of usage.WriteFileAtomic) — used to
	// restore the data-dir artifacts and to write extracted archive entries.
	WriteFileAtomic func(path string, data []byte) error

	// Stop / Start / Restart drive ONLY the named user systemd service
	// (orchestrate.Systemd seam). Quiesce stops villa-openwebui.service before a
	// volume export and restarts it after (D-05).
	Stop    func(service string) error
	Start   func(service string) error
	Restart func(service string) error

	// ReconcileAndWrite renders Quadlet units from the persisted config and writes
	// the changed unit(s); the live closure performs the daemon-reload internally.
	// It re-establishes the Open WebUI volume unit from restored config — config is
	// the single source of truth (D-07). Reports whether anything changed.
	ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
	// DaemonReload reloads the user systemd manager.
	DaemonReload func() error

	// Prove is the injected, offload-asserting restore-cutover gate: it re-runs
	// preflight + asserts status residency on the already-running stack and returns
	// a verdict. The core switches to success ONLY on ProveStatusPass. All backend
	// markers live behind this seam — never in this package (D-07).
	Prove func(target string) ProveVerdict

	// OpenWebUIServiceName / InstallServiceName are the service identities the flow
	// quiesces/restarts. Deps fields so the pure core need not import cmd-layer
	// constants (mirrors backendswap.InstallServiceName).
	OpenWebUIServiceName string
	InstallServiceName   string
}

// Result is the typed outcome of a backup/restore (not an exit code), so the
// cobra caller (later plans) can branch on it and map it to an exit code +
// messages. Clones backendswap.Result's shape and its honest-rollback contract.
type Result struct {
	// Refused is true when the operation was rejected with ZERO side effects (a
	// fail-closed BLOCK on corruption/incompatible-schema, an uncapturable current
	// state, or a declined skew confirmation).
	Refused bool
	// Restored is true when a restore fully applied AND the Prove verdict was
	// ProveStatusPass.
	Restored bool
	// RolledBack is true when a mutate error or a non-pass Prove verdict triggered
	// a verbatim restore of the captured prior state. It stays TRUE even when a
	// rollback STEP itself errored — Reason/FailedStep then flag rollback-incomplete
	// (never claim a clean no-op when rollback errored; RESEARCH Pitfall 5).
	RolledBack bool
	// NoOp is true for a clean no-op with zero side effects.
	NoOp bool
	// Reason is the human refusal/remediation/rollback explanation (empty on a
	// clean success).
	Reason string
	// Err is a non-refusal failure (capture/save/write/volume/restart). Distinct
	// from a Refused (a clean policy rejection, not an error).
	Err error
	// FailedStep names the step that failed ("verify"/"capture"/"save"/"write"/
	// "volume"/"restart"/"prove") so the caller can print a precise message.
	FailedStep string
	// Prove carries the cutover verdict (on a Restored or a prove-triggered
	// RolledBack result) for the caller to surface.
	Prove ProveVerdict
	// RestartWarning is a non-fatal advisory set when the post-backup best-effort
	// restart of Open WebUI failed (IN-01). The backup itself still succeeded (the
	// archive was written); this only flags that the service is likely DOWN and the
	// user should run `villa up`. Empty on a clean restart.
	RestartWarning string
}
