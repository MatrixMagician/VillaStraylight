// Package backup is the PURE, Deps-injected core for `villa backup` /
// `villa restore` (Phase 16). It builds the
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
// This mirrors the proven pure-core + injectable-seam pattern of
// internal/backendswap, internal/usage, and internal/status.
package backup

import (
	"io"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

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
// but-residency-FAIL verdict is NEVER success (offload-asserting).
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
	// SaveConfig persists a config to config.toml via config.SaveVilla (atomic
	// temp+rename, 0600/0700, traversal-guarded — NEVER hand-write TOML).
	// The atomicity matters most here: a torn write during a restore would destroy
	// both the config being restored and the one already on disk.
	SaveConfig func(c config.VillaConfig) error

	// VolumeExport exports the named podman volume to the file at out via the
	// cmd-tier fixed-arg `podman volume export` seam. Used to capture the
	// current Open WebUI volume (backup + rollback-capture).
	VolumeExport func(name, out string) error
	// VolumeImport imports the tar at src into the named (already-recreated, clean)
	// podman volume via `podman volume import`. import MERGES, so the volume
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
	// OpenFile opens a source file for STREAMING reads (reader + size), used by
	// Backup for the LARGE volume-tar entries (openwebui-volume.tar /
	// qdrant-volume.tar — the one entry that realistically grows to many GiB of
	// vectors) so their bytes are never buffered whole in memory (review):
	// the checksum pass streams via io.Copy and the tar assembly streams a fresh
	// reader per entry. OPTIONAL: when nil, Backup falls back to ReadFile (the
	// in-memory path) — existing fakes and small entries are unaffected. The live
	// wiring is os.Open + Stat in cmd/villa.
	//
	// It stays separate from ReadFile because it is a genuinely different operation:
	// a stream rather than a buffer. Collapsing the two would force the multi-GiB
	// volume entry through a []byte.
	OpenFile func(path string) (rc io.ReadCloser, size int64, err error)
	// WriteFile writes data to path at 0600 under a 0700 directory, atomically.
	//
	// This was five fields — one per destination — which enumerated the caller's
	// filenames in the seam: the data-store artifacts, the restore temp dir, the
	// coding-agent config and the metasearch settings. The seam did not need to know
	// any of that. What actually differs between those destinations is which
	// containment root applies, and that is the live wiring's business: the data-store
	// destinations are guarded against escaping $XDG_DATA_HOME/villa, while the
	// config-dir and temp-dir destinations are guarded within their own parent
	// (routing those through the store-root guard rejected the legitimate write and
	// broke restore on a real host).
	//
	// The mode is 0600 for every one of them, and must stay 0600: two of these files
	// hold generated secrets (crush.json's provider key, settings.yml's rendered
	// SEARXNG_SECRET), so the write must never widen the mode.
	WriteFile func(path string, data []byte) error
	// RemoveFile deletes the file at path, TOLERATING an already-absent file (the
	// live wiring maps os.Remove + os.IsNotExist). It is the verbatim-rollback seam
	// for a data-dir artifact the FORWARD path newly created where none existed
	// before: restoring the prior (absent) state means removing it. A
	// genuine remove failure (e.g. permissions) counts as rollback-incomplete.
	RemoveFile func(path string) error

	// Stop / Start / Restart drive ONLY the named user systemd service
	// (orchestrate.Systemd seam). Quiesce stops villa-openwebui.service before a
	// volume export and restarts it after.
	Stop    func(service string) error
	Start   func(service string) error
	Restart func(service string) error

	// ReconcileAndWrite renders Quadlet units from the persisted config and writes
	// the changed unit(s); the live closure performs the daemon-reload internally.
	// It re-establishes the Open WebUI volume unit from restored config — config is
	// the single source of truth. Reports whether anything changed.
	ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
	// DaemonReload reloads the user systemd manager.
	DaemonReload func() error

	// Prove is the injected, offload-asserting restore-cutover gate: it re-runs
	// preflight + asserts status residency on the already-running stack and returns
	// a verdict. The core switches to success ONLY on ProveStatusPass. All backend
	// markers live behind this seam — never in this package.
	Prove func(target string) ProveVerdict

	// OpenWebUIServiceName / InstallServiceName are the service identities the flow
	// quiesces/restarts. Deps fields so the pure core need not import cmd-layer
	// constants (mirrors backendswap.InstallServiceName).
	OpenWebUIServiceName string
	InstallServiceName   string
	// QdrantServiceName is the qdrant service identity the Phase-23 memory-on
	// backup quiesces around its volume export (Stop → export → deferred Start;
	// Pitfall 3 torn-RocksDB/WAL guard). Seam-sourced by the cmd tier (derived
	// from orchestrate.QdrantContainerUnitName()) — never a literal here, so the
	// core stays free of service-name literals (mirrors OpenWebUIServiceName).
	QdrantServiceName string
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
	// RollbackIncomplete is true when RolledBack is set but one or more rollback
	// steps themselves errored. The cmd tier MUST then PRESERVE the
	// restore temp dir instead of deleting it — the captured rollback tars
	// (rollback-owui.tar / rollback-qdrant.tar) it holds are the ONLY copies of
	// the prior Open WebUI / Qdrant volume data, and deleting them on an
	// incomplete rollback would permanently lose the prior chat database.
	RollbackIncomplete bool
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
	// restart of Open WebUI failed. The backup itself still succeeded (the
	// archive was written); this only flags that the service is likely DOWN and the
	// user should run `villa up`. Empty on a clean restart.
	RestartWarning string
	// QdrantRestored / RecallStateRestored report whether the OPTIONAL Phase-23
	// memory entries were present in the archive and applied (valid on a Restored
	// result). False means "not present in this backup" — the caller reports it
	// honestly and existing Qdrant data was left untouched (OQ1: report,
	// never extend Prove).
	QdrantRestored      bool
	RecallStateRestored bool
	// RestoredMemoryEnabled is the RESTORED config's memory posture (Pitfall 5):
	// the reconcile renders units from the restored config, so the stack shape may
	// have changed — the caller prints "memory stack: enabled/disabled (restored
	// config)". Valid on a Restored result.
	RestoredMemoryEnabled bool
	// CrushConfigRestored reports whether the OPTIONAL Phase-28 crush.json entry was
	// present in the archive AND actually written (Phase 28).
	// It reflects the ACTUAL write (entry present AND a destination wired), not mere
	// presence — an agent-on archive restored onto an agent-off current install has
	// no destination wired, so the entry is skipped and this stays false (see
	// CrushConfigSkipped). Valid on a Restored result.
	CrushConfigRestored bool
	// CrushConfigSkipped is true when the archive CARRIED a crush.json entry but it
	// was NOT applied because no destination was wired — i.e. the current install is
	// agent-off. This is the honest signal that the restored config.toml may
	// believe the agent is enabled while its crush.json was never restored; the cmd
	// tier warns the operator to re-run `villa install --coding-agent` then restore.
	// Mutually exclusive with CrushConfigRestored. Valid on a Restored result.
	CrushConfigSkipped bool
	// SearxngSettingsRestored reports whether the OPTIONAL Phase-34 settings.yml entry
	// was present in the archive AND actually written. It reflects the ACTUAL
	// write (entry present AND a destination wired), not mere presence — a web-search-on
	// archive restored onto a web-search-off current install has no destination wired, so
	// the entry is skipped and this stays false (see SearxngSettingsSkipped). Valid on a
	// Restored result.
	SearxngSettingsRestored bool
	// SearxngSettingsSkipped is true when the archive CARRIED a settings.yml entry but it
	// was NOT applied because no destination was wired — i.e. the current install is
	// web-search-off. This is the honest signal that the restored config.toml may believe
	// web search is enabled while its settings.yml was never restored; the cmd tier warns
	// the operator to re-run `villa install` (web search) then restore. Mutually exclusive
	// with SearxngSettingsRestored. Valid on a Restored result.
	SearxngSettingsSkipped bool
	// ExcludedAgent is the EXCLUDED coding-agent binary identity recorded in the
	// restored manifest, surfaced for the operator to RE-STAGE the
	// binary (re-download the pinned release) — exactly the ExcludedModels re-pull
	// report. Nil when the backup recorded no agent identity (agent-off backup). The
	// binary bytes were never in the archive; restore re-stages it like model
	// weights, and the identity verify is fail-closed on drift.
	ExcludedAgent *ExcludedAgent
}
