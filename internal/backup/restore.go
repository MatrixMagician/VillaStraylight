package backup

// restore.go is the PURE, Deps-injected transactional core for `villa restore`
// It clones the proven internal/backendswap.Run frame
// capture STRICTLY before mutate, gate the cutover on an offload-asserting Prove
// verdict, and roll back verbatim (with honest rollback-complete/incomplete
// reporting) on ANY mutate error or non-pass prove — and wraps it around the
// archive-apply ordering RESEARCH §Transactional Restore mandates:
//
//	read+verify → skew (WARN-and-confirm / fail-closed BLOCK) → capture →
//	quiesce → MUTATE (config + data-dir + CLEAN-RECREATE owui volume + import +
//	start) → prove → rollback-on-failure.
//
// THE load-bearing fact (RESEARCH §Podman Volume Mechanics, HIGH confidence):
// `podman volume import` MERGES into existing contents and does NOT auto-create
// the volume. So restore MUST clean-recreate the Open WebUI volume
// VolumeRm (not-found-tolerant) → ReconcileAndWrite (Quadlet recreate from the
// RESTORED config, the single source of truth) → EnsureVolume (explicit
// `podman volume create`, idempotent) — BEFORE every VolumeImport, on the
// forward apply AND the rollback path, so stale chats/webui.db never leak through.
//
// It links NO inference and NO detect package: the prove sentinel
// (prove.StatusPass) is this package's OWN local value, so the backend-marker seam
// discipline (TestSeamGrepGate) holds. Every host effect is a Deps func field; the
// whole flow is driven from restore_test.go without a live host.

import (
	"bytes"
	"fmt"
	"io"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// RestoreInput is the plain-data drive for the pure Restore orchestrator. The cmd
// tier (liveRestoreDeps + runRestore) gathers everything host-derived — the
// archive opener, the current-install facts for the skew compare (sourced from the
// seam: inference.BackendFor(...).Image() / orchestrate.OpenWebUIImage() — never a
// re-typed literal), the consent gate + its bypass flag, the podman volume
// name, and the resolved destination paths for the restored data-dir artifacts
// then Restore() executes the pure transactional ordering over the injected Deps.
type RestoreInput struct {
	// OpenArchive opens the outer .tar for a fresh read pass. Restore calls it TWICE
	// (once to verify per-entry SHA-256, once to extract the verified entries) so the
	// reader need not be seekable; each call yields a fresh stream the core closes.
	OpenArchive func() (io.ReadCloser, error)

	// Current is the current-install snapshot the manifest is compared against
	// The cmd tier fills it seam-/accessor-sourced; Restore sets
	// Current.ChecksumFailed itself from the verify pass before CompareSkew.
	Current CurrentInstall

	// Consent is the y/N gate invoked once with the assembled WARN text when the skew
	// compare yields warnings (and Bypass is false). It returns true to proceed. The
	// live closure composes stdinIsInteractive + promptConsent.
	Consent func(prompt string) bool
	// Bypass is the --yes/--force flag: when true, a WARN-only skew is applied without
	// invoking Consent. It NEVER bypasses a fail-closed BLOCK.
	Bypass bool

	// OpenWebUIVolumeName is the podman NAMED volume to clean-recreate + import into
	// (seam-sourced from orchestrate.OpenWebUIVolumeName()). The villa-models volume is
	// NEVER named here.
	OpenWebUIVolumeName string
	// TempVolumeTar is the cmd-chosen temp path Restore writes the EXTRACTED
	// openwebui-volume.tar entry to, then asks Deps.VolumeImport to import from. It is
	// also reused (overwritten) for the rollback re-import of the captured volume.
	TempVolumeTar string
	// RollbackVolumeTar is the cmd-chosen temp path the CAPTURE step exports the
	// CURRENT Open WebUI volume to (the verbatim rollback set). Restore imports from it
	// on the rollback path.
	RollbackVolumeTar string

	// ConfigDestPath is unused by the pure core for config (config goes through
	// Deps.SaveConfig); it is documented here only to make the data-dir destination set
	// explicit. UsageDestPath / BenchDestPath are the resolved destinations the
	// extracted usage.json / bench-reports.jsonl entries are written to atomically.
	UsageDestPath string
	BenchDestPath string

	// QdrantVolumeName is the podman NAMED qdrant storage volume (seam-sourced from
	// orchestrate.QdrantVolumeName — never a literal) the OPTIONAL
	// qdrant-volume.tar entry is clean-recreated + imported into. Every
	// qdrant mutation is gated on the entry actually being present in the archive
	// (ex.qdrantPresent) — a memory-free backup NEVER touches existing Qdrant data
	// (T-23-09).
	QdrantVolumeName string
	// TempQdrantTar / RollbackQdrantTar mirror TempVolumeTar / RollbackVolumeTar
	// for the qdrant volume: the cmd-chosen staging path for the EXTRACTED entry
	// and the capture destination for the CURRENT volume's rollback tar (both in
	// the -cleaned restore temp dir — the qdrant tar holds chat-derived
	// vectors, same sensitivity as webui.db).
	TempQdrantTar     string
	RollbackQdrantTar string
	// QdrantVolumeExists reports whether the CURRENT host has the qdrant volume
	// (the cmd tier's tri-state `podman volume exists` check). It selects the
	// Pitfall-4 capture/rollback shape: existing ⇒ capture-export + rollback
	// re-import; absent ⇒ no capture, and rollback REMOVES the forward-created
	// volume (the volume analog of rollbackRemove).
	QdrantVolumeExists bool
	// QdrantVolumeUnknown is true when the existence check could NOT be evaluated
	// (podman missing/failed — the tri-state check's unknown cell, review).
	// When the archive carries a qdrant entry, an Unknown current state is a
	// fail-closed REFUSAL before any mutation: treating Unknown as absent would
	// run the destructive VolumeRm on a possibly-real, UNCAPTURED qdrant volume.
	// A memory-free archive ignores it (zero qdrant calls either way).
	QdrantVolumeUnknown bool
	// RecallDestPath is the resolved recall-state.json destination
	// (recall.RecallStatePath() at the cmd tier) for the OPTIONAL recall-state
	// entry — restored through the same WriteFileAtomic/rollbackRemove rows as
	// usage/bench (the file lives directly under the villa data root, so the
	// store-root guard covers it).
	RecallDestPath string

	// CrushConfigDestPath is the resolved crush.json destination for the OPTIONAL
	// Phase-28 coding-agent config entry (crushConfigPath at the cmd
	// tier — ~/.config/crush/crush.json, OUTSIDE the villa data-store root). It is
	// restored through the dedicated WriteCrushConfig / RemoveFile seams (NOT
	// WriteFileAtomic, whose store-root guard would reject a path outside
	// $XDG_DATA_HOME/villa). Empty means the cmd tier supplied no destination — the
	// crush.json entry, if present, is reported as re-stageable but not written.
	CrushConfigDestPath string

	// SearxngSettingsDestPath is the resolved settings.yml destination for the OPTIONAL
	// Phase-34 web-search config entry (orchestrate.SearXNGSettingsFilePath at
	// the cmd tier — $XDG_CONFIG_HOME/villa/searxng/settings.yml, OUTSIDE the villa
	// data-STORE root). It is restored through the dedicated WriteSearxngSettings /
	// RemoveFile seams (NOT WriteFileAtomic, whose data-store-root guard would reject a
	// path under $XDG_CONFIG_HOME). The file holds the rendered SEARXNG_SECRET, so the
	// write is 0600-preserving and NEVER widens the mode. Empty means the cmd
	// tier supplied no destination (web search off) — the settings.yml entry, if present,
	// is reported as re-writable but not written.
	SearxngSettingsDestPath string
}

// extracted holds the verified, tar-slip-guarded archive payload after the read
// pass: the parsed manifest, the raw config.toml bytes, the openwebui-volume.tar
// bytes, and the optional data-dir artifact bytes (present flag distinguishes an
// absent optional entry from an empty one).
type extracted struct {
	manifest     Manifest
	config       []byte
	owuiVolume   []byte
	usage        []byte
	usagePresent bool
	bench        []byte
	benchPresent bool
	// qdrantVolume / recallState are the OPTIONAL Phase-23 memory entries
	// the present flags gate EVERY qdrant/recall mutation downstream.
	qdrantVolume  []byte
	qdrantPresent bool
	recallState   []byte
	recallPresent bool
	// crushConfig is the OPTIONAL Phase-28 coding-agent config entry;
	// crushPresent gates its restore. It is SHA-256-verified through the SAME
	// readAndVerify pass as every other entry (no parallel reader). The manifest's
	// ExcludedAgent (the EXCLUDED binary identity) rides alongside it for the
	// re-stage report.
	crushConfig  []byte
	crushPresent bool
	// searxngSettings is the OPTIONAL Phase-34 web-search config entry;
	// searxngSettingsPresent gates its restore. It is SHA-256-verified through the SAME
	// readAndVerify pass as every other entry (no parallel reader).
	searxngSettings        []byte
	searxngSettingsPresent bool
}

// Restore performs the guarded, transactional archive apply and returns a typed
// Result, cloning backendswap.Run's frame. Ordering (RESEARCH §Transactional
// Restore):
//
//	(1) READ+VERIFY (pure, zero side effects): open the outer tar, parse
//	    manifest.json, verify each entry's SHA-256 against the manifest. A mismatch
//
// or unreadable/incompatible manifest.schema_version → Refused.
//
//	(2) SKEW: CompareSkew(manifest, Current). Block → Refused. WARN-only → require
//	    Consent unless Bypass; a declined gate → Refused. (All still zero side effects.)
//	(3) CAPTURE strictly BEFORE mutation: export the CURRENT owui volume + snapshot
//	    the current config + current usage.json/bench-reports.jsonl. Uncapturable → Refused.
//	(4) QUIESCE: Stop the Open WebUI service.
//	(5) MUTATE (any error → rollback): SaveConfig(restored) → restore data-dir files →
//	    CLEAN-RECREATE owui volume (VolumeRm → ReconcileAndWrite → EnsureVolume) →
//	    VolumeImport(extracted owui tar) → Start.
//	(6) PROVE: switch to success ONLY on prove.StatusPass; any other verdict → rollback.
//
// The rollback path re-applies the captured set through the SAME clean-recreate
// ordering (VolumeRm → ReconcileAndWrite(prior cfg) → EnsureVolume → VolumeImport
// of the captured tar) so a rollback never merge-imports into a live volume either.
func Restore(d RestoreDeps, in RestoreInput) Result {
	// (1) READ+VERIFY — pure, zero side effects. A verify failure or an
	// unreadable/incompatible manifest is a fail-closed BLOCK BEFORE any mutate.
	ex, verr := readAndVerify(in)
	if verr != nil {
		return Result{Refused: true, FailedStep: "verify", Err: verr,
			Reason: "archive failed integrity verification — refusing to restore a corrupt backup: " + verr.Error()}
	}

	// (2) SKEW. A checksum failure is folded into CompareSkew via the
	// ChecksumFailed flag (always false here — a real mismatch already Refused above),
	// so CompareSkew classifies schema/version/digest/host skew. Block → Refused; a
	// WARN-only verdict requires consent unless Bypass.
	cur := in.Current
	cur.ChecksumFailed = false
	skew := CompareSkew(ex.manifest, cur)
	if skew.Block {
		return Result{Refused: true, FailedStep: "skew", Reason: skew.BlockReason}
	}
	if len(skew.Warnings) > 0 && !in.Bypass {
		if in.Consent == nil || !in.Consent(skewPrompt(skew.Warnings)) {
			return Result{Refused: true, FailedStep: "skew",
				Reason: "restore declined at the skew confirmation (re-run with --yes/--force to bypass)"}
		}
	}

	// (3) CAPTURE strictly BEFORE any mutation (RESEARCH Pitfall 4). The verbatim
	// rollback set: the CURRENT owui volume tar, a snapshot of the current config, and
	// the current data-dir artifacts. An uncapturable current state must NOT be
	// mutated — refuse with zero side effects.
	//
	// fail-closed gate: when the archive carries a qdrant entry but the
	// current volume's existence could NOT be evaluated, REFUSE before any
	// mutation. An Unknown collapsed into "absent" would skip the capture export
	// AND the quiesce, then run the destructive VolumeRm on a possibly-real,
	// uncaptured qdrant volume — destroying existing vectors with no rollback
	// copy. The typed-Unknown doctrine: Unknown is never a confident negative.
	if ex.qdrantPresent && in.QdrantVolumeUnknown {
		return Result{Refused: true, FailedStep: "capture",
			Reason: "could not determine whether the Qdrant volume " + in.QdrantVolumeName +
				" exists — an unknown current state cannot be safely captured for rollback; " +
				"check podman (`podman volume exists " + in.QdrantVolumeName + "`), then re-run"}
	}
	priorCfg, err := d.LoadConfig()
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "cannot snapshot the current config for rollback — refusing to mutate: " + err.Error()}
	}
	if err := d.VolumeExport(in.OpenWebUIVolumeName, in.RollbackVolumeTar); err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "cannot capture the current Open WebUI volume for rollback — refusing to mutate: " + err.Error()}
	}
	// Qdrant capture (Phase 23, /Pitfall 4): ONLY when the archive carries the
	// entry AND the current host actually has the volume. Entry-present +
	// volume-absent records prior-absent (rollback then REMOVES the
	// forward-created volume); entry-absent makes ZERO qdrant calls of any kind.
	if ex.qdrantPresent && in.QdrantVolumeExists {
		if err := d.VolumeExport(in.QdrantVolumeName, in.RollbackQdrantTar); err != nil {
			return Result{Refused: true, FailedStep: "capture", Err: err,
				Reason: "cannot capture the current Qdrant volume for rollback — refusing to mutate: " + err.Error()}
		}
	}
	priorUsage, priorUsageOK := captureFile(d, in.UsageDestPath)
	priorBench, priorBenchOK := captureFile(d, in.BenchDestPath)
	priorRecall, priorRecallOK := captureFile(d, in.RecallDestPath)
	// Capture the current crush.json for verbatim rollback (Phase 28).
	// It lives OUTSIDE the data-store root, so it is captured via the same ReadFile
	// seam as the others but restored/rolled-back through the dedicated
	// WriteCrushConfig / RemoveFile seams below.
	priorCrush, priorCrushOK := captureFile(d, in.CrushConfigDestPath)
	// Capture the current settings.yml for verbatim rollback (Phase 34). Like
	// crush.json it lives OUTSIDE the data-store root, so it is captured via the same
	// ReadFile seam but restored/rolled-back through the dedicated WriteSearxngSettings /
	// RemoveFile seams below.
	priorSearxngSettings, priorSearxngSettingsOK := captureFile(d, in.SearxngSettingsDestPath)

	// Restored config is the archive's config.toml parsed into a VillaConfig (config is
	// the single source of truth — the Quadlet recreate renders from it).
	restoredCfg, err := config.Parse(ex.config)
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "archive config.toml is unreadable — refusing to mutate: " + err.Error()}
	}

	// cleanRecreateThenImport is the load-bearing clean-recreate-before-import
	// sequence (RESEARCH Pitfall 1/2), used on BOTH the forward apply and the
	// rollback, for BOTH volumes (Phase 23 generalized it over volumeName):
	// VolumeRm (not-found-tolerant) → ReconcileAndWrite (Quadlet recreate from cfg) →
	// EnsureVolume (explicit create) → VolumeImport. import MERGES + does NOT
	// auto-create, so the volume MUST be rm'd + freshly created first. When both
	// volumes restore, ReconcileAndWrite runs once per call — the second invocation
	// is an idempotent no-op by construction (Reconcile is a pure content-hash
	// compare; WriteUnits writes only Changed), tolerated rather than restructured.
	cleanRecreateThenImport := func(cfg config.VillaConfig, volumeName, srcTar string) error {
		if err := d.VolumeRm(volumeName); err != nil {
			return fmt.Errorf("volume rm %s: %w", volumeName, err)
		}
		if _, err := d.ReconcileAndWrite(cfg); err != nil {
			return fmt.Errorf("reconcile/recreate units: %w", err)
		}
		if err := d.EnsureVolume(volumeName); err != nil {
			return fmt.Errorf("ensure volume %s: %w", volumeName, err)
		}
		if err := d.VolumeImport(volumeName, srcTar); err != nil {
			return fmt.Errorf("volume import %s: %w", volumeName, err)
		}
		return nil
	}

	// rollback re-applies the captured prior state verbatim and re-readies the stack,
	// best-effort: it accumulates errors across ALL steps rather than aborting on the
	// first, and reports whether EVERY step succeeded. Per RESEARCH Pitfall 5 an
	// incomplete rollback is flagged honestly — never claim a clean no-op when a
	// restore step errored. It uses the SAME clean-recreate ordering so the rollback
	// re-import never merges into a live volume either.
	rollback := func() (ok bool, detail string) {
		ok = true
		add := func(e error, what string) {
			if e != nil {
				ok = false
				if detail != "" {
					detail += "; "
				}
				detail += what + ": " + e.Error()
			}
		}
		// QUIESCE FIRST: the forward path starts Open WebUI (and Qdrant)
		// at step (5) BEFORE the Prove gate at step (6), so a prove-triggered
		// rollback arrives with the services RUNNING — and a running container
		// holds its volume, making the clean-recreate VolumeRm below fail in-use
		// on a live host. Mirror the forward path's own quiesce before any volume
		// work; Stop on an already-stopped unit is an idempotent no-op. The qdrant
		// stop mirrors the forward Start gate (entry present AND a prior volume
		// existed) — on the prior-absent cell nothing was ever started.
		add(d.Stop(d.OpenWebUIServiceName), "stop Open WebUI for rollback")
		if ex.qdrantPresent && in.QdrantVolumeExists {
			add(d.Stop(d.QdrantServiceName), "stop Qdrant for rollback")
		}
		add(d.SaveConfig(priorCfg), "SaveConfig(prior)")
		// Restore each data-dir artifact VERBATIM. For each path:
		//   - prior existed → rewrite the captured prior bytes (the prior behavior);
		//   - prior absent BUT the forward path created it (ex.*Present) → REMOVE it,
		//     so the rolled-back state matches the prior (absent) state. Without this,
		//     a restored-from-archive usage.json/bench-reports.jsonl was left on disk
		//     after a "rollback", leaking backup chat/usage data into a supposedly
		//     prior-restored install. A failed RemoveFile counts as rollback-incomplete.
		switch {
		case priorUsageOK:
			add(d.WriteFile(in.UsageDestPath, priorUsage), "restore usage.json")
		case ex.usagePresent && in.UsageDestPath != "":
			add(rollbackRemove(d, in.UsageDestPath), "remove restored usage.json")
		}
		switch {
		case priorBenchOK:
			add(d.WriteFile(in.BenchDestPath, priorBench), "restore bench-reports.jsonl")
		case ex.benchPresent && in.BenchDestPath != "":
			add(rollbackRemove(d, in.BenchDestPath), "remove restored bench-reports.jsonl")
		}
		// recall-state.json follows the same verbatim rows (Phase 23).
		switch {
		case priorRecallOK:
			add(d.WriteFile(in.RecallDestPath, priorRecall), "restore recall-state.json")
		case ex.recallPresent && in.RecallDestPath != "":
			add(rollbackRemove(d, in.RecallDestPath), "remove restored recall-state.json")
		}
		// crush.json follows the same verbatim rows (Phase 28),
		// but through the dedicated out-of-store-root seams: WriteCrushConfig to
		// restore the prior bytes, RemoveFile to undo a forward-created file.
		switch {
		case priorCrushOK:
			add(writeCrushConfig(d, in.CrushConfigDestPath, priorCrush), "restore crush.json")
		case ex.crushPresent && in.CrushConfigDestPath != "":
			add(rollbackRemove(d, in.CrushConfigDestPath), "remove restored crush.json")
		}
		// settings.yml follows the same verbatim rows (Phase 34),
		// through the dedicated out-of-store-root seams: WriteSearxngSettings to restore
		// the prior bytes (0600-preserving), RemoveFile to undo a forward-created file.
		switch {
		case priorSearxngSettingsOK:
			add(writeSearxngSettings(d, in.SearxngSettingsDestPath, priorSearxngSettings), "restore settings.yml")
		case ex.searxngSettingsPresent && in.SearxngSettingsDestPath != "":
			add(rollbackRemove(d, in.SearxngSettingsDestPath), "remove restored settings.yml")
		}
		// Re-import the CAPTURED owui volume through the clean-recreate ordering (prior cfg).
		add(cleanRecreateThenImport(priorCfg, in.OpenWebUIVolumeName, in.RollbackVolumeTar), "restore Open WebUI volume")
		// Qdrant rollback (Phase 23, /Pitfall 4): same clean-recreate ordering
		// from the CAPTURED rollback tar when a prior volume existed; when the prior
		// state was ABSENT, restore it verbatim by REMOVING the forward-created
		// volume (the volume analog of rollbackRemove). Entry-absent ⇒ zero calls.
		if ex.qdrantPresent {
			if in.QdrantVolumeExists {
				add(cleanRecreateThenImport(priorCfg, in.QdrantVolumeName, in.RollbackQdrantTar), "restore Qdrant volume")
				add(d.Start(d.QdrantServiceName), "restart Qdrant")
			} else {
				add(d.VolumeRm(in.QdrantVolumeName), "remove forward-created Qdrant volume")
			}
		}
		add(d.Start(d.OpenWebUIServiceName), "restart Open WebUI")
		return ok, detail
	}

	// rolledBack assembles a RolledBack Result, folding in an honest
	// rollback-incomplete message when the restore did not fully succeed (Pitfall 5).
	rolledBack := func(failedStep, reason string, origErr error, v prove.Verdict) Result {
		rbOK, rbDetail := rollback()
		r := Result{
			RolledBack: true,
			FailedStep: failedStep,
			Reason:     reason,
			Err:        origErr,
			Prove:      v,
		}
		if !rbOK {
			r.RollbackIncomplete = true
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-openwebui unit"
		}
		return r
	}

	// (4) QUIESCE the Open WebUI service for a clean volume swap. A stop failure
	// is a pre-mutate error → rollback (which best-effort re-readies).
	if err := d.Stop(d.OpenWebUIServiceName); err != nil {
		return rolledBack("quiesce", "", fmt.Errorf("stop %s: %w", d.OpenWebUIServiceName, err), prove.Verdict{})
	}
	// Quiesce the qdrant service too (Phase 23, Pitfall 3): a RUNNING qdrant holds
	// its volume (the live VolumeRm would fail in-use) and could write mid-swap.
	// Gated on a prior volume actually existing — on a memory-off host there is no
	// running qdrant service to stop (its unit may not even exist).
	if ex.qdrantPresent && in.QdrantVolumeExists {
		if err := d.Stop(d.QdrantServiceName); err != nil {
			return rolledBack("quiesce", "", fmt.Errorf("stop %s: %w", d.QdrantServiceName, err), prove.Verdict{})
		}
	}

	// (5) MUTATE. ANY error here rolls back verbatim from the captured set.
	if err := d.SaveConfig(restoredCfg); err != nil {
		return rolledBack("save", "", fmt.Errorf("save restored config: %w", err), prove.Verdict{})
	}
	if ex.usagePresent {
		if err := d.WriteFile(in.UsageDestPath, ex.usage); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore usage.json: %w", err), prove.Verdict{})
		}
	}
	if ex.benchPresent {
		if err := d.WriteFile(in.BenchDestPath, ex.bench); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore bench-reports.jsonl: %w", err), prove.Verdict{})
		}
	}
	// recall-state.json restores like the usage/bench rows (Phase 23): the
	// store-root-guarded atomic write covers it (it lives directly under the villa
	// data root).
	if ex.recallPresent {
		if err := d.WriteFile(in.RecallDestPath, ex.recallState); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore recall-state.json: %w", err), prove.Verdict{})
		}
	}
	// Restore the OPTIONAL crush.json (Phase 28) through the dedicated
	// out-of-store-root seam (it lives at ~/.config/crush/, not under the villa data
	// root). Gated on the entry being present AND a destination wired; any error
	// rolls back verbatim like the other data rows. The agent BINARY is NOT restored
	// here — it is re-staged separately (re-download the pinned release; the
	// ExcludedAgent identity is surfaced on the Result for that fail-closed re-stage).
	// crushWritten records the ACTUAL write (entry present AND a destination wired),
	// NOT mere presence. crushSkipped flags an archive that CARRIED a
	// crush.json entry but had no destination wired — i.e. the current install is
	// agent-off — so the cmd tier reports the skip honestly instead of a false
	// "crush.json restored".
	crushWritten := ex.crushPresent && in.CrushConfigDestPath != ""
	crushSkipped := ex.crushPresent && in.CrushConfigDestPath == ""
	if crushWritten {
		if err := writeCrushConfig(d, in.CrushConfigDestPath, ex.crushConfig); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore crush.json: %w", err), prove.Verdict{})
		}
	}
	// Restore the OPTIONAL settings.yml (Phase 34) through the dedicated
	// out-of-store-root seam (it lives at $XDG_CONFIG_HOME/villa/searxng/, not under the
	// villa data root). Gated on the entry being present AND a destination wired; any
	// error rolls back verbatim like the other data rows. The write FORCES 0600 (the
	// entry holds the rendered SEARXNG_SECRET — never widen the mode).
	// searxngSettingsWritten records the ACTUAL write; searxngSettingsSkipped flags an
	// archive that CARRIED a settings.yml entry but had no destination wired (web-off
	// current install) so the cmd tier reports the skip honestly (mirror crush).
	searxngSettingsWritten := ex.searxngSettingsPresent && in.SearxngSettingsDestPath != ""
	searxngSettingsSkipped := ex.searxngSettingsPresent && in.SearxngSettingsDestPath == ""
	if searxngSettingsWritten {
		if err := writeSearxngSettings(d, in.SearxngSettingsDestPath, ex.searxngSettings); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore settings.yml: %w", err), prove.Verdict{})
		}
	}
	// CLEAN-RECREATE then import the RESTORED owui volume (the whole reason for the
	// rm→recreate→ensure→import ordering — never merge into a live volume).
	if err := d.WriteFile(in.TempVolumeTar, ex.owuiVolume); err != nil {
		return rolledBack("volume", "", fmt.Errorf("stage restored owui volume tar: %w", err), prove.Verdict{})
	}
	if err := cleanRecreateThenImport(restoredCfg, in.OpenWebUIVolumeName, in.TempVolumeTar); err != nil {
		return rolledBack("volume", "", err, prove.Verdict{})
	}
	// Forward qdrant apply (Phase 23): SAME clean-recreate ordering for the
	// second volume — never a merge-import. Gated on the entry being present;
	// VolumeRm tolerates an absent prior volume (the seam contract), so the
	// prior-absent cell flows through the same sequence.
	if ex.qdrantPresent {
		if err := d.WriteFile(in.TempQdrantTar, ex.qdrantVolume); err != nil {
			return rolledBack("volume", "", fmt.Errorf("stage restored qdrant volume tar: %w", err), prove.Verdict{})
		}
		if err := cleanRecreateThenImport(restoredCfg, in.QdrantVolumeName, in.TempQdrantTar); err != nil {
			return rolledBack("volume", "", err, prove.Verdict{})
		}
	}
	if err := d.Start(d.OpenWebUIServiceName); err != nil {
		return rolledBack("restart", "", fmt.Errorf("start %s: %w", d.OpenWebUIServiceName, err), prove.Verdict{})
	}
	// Restart the qdrant service we quiesced (symmetric with its Stop gate). On the
	// prior-absent cell nothing was stopped — the operator brings the (possibly
	// newly-rendered) memory stack up via `villa up`, reported honestly by the
	// caller.
	if ex.qdrantPresent && in.QdrantVolumeExists {
		if err := d.Start(d.QdrantServiceName); err != nil {
			return rolledBack("restart", "", fmt.Errorf("start %s: %w", d.QdrantServiceName, err), prove.Verdict{})
		}
	}

	// (6) PROVE the restored stack offload-honestly. Switch to success ONLY on
	// prove.StatusPass; ANY other verdict (incl. ready+health-200-but-residency-FAIL)
	// rolls back verbatim — is-active/200 alone is NEVER success.
	v := d.Prove(restoredCfg.Backend)
	if !v.Pass() {
		return rolledBack("prove", v.Detail, nil, v)
	}
	return Result{
		Restored:                true,
		Prove:                   v,
		QdrantRestored:          ex.qdrantPresent,
		RecallStateRestored:     ex.recallPresent,
		RestoredMemoryEnabled:   restoredCfg.MemoryEnabled,
		CrushConfigRestored:     crushWritten,
		CrushConfigSkipped:      crushSkipped,
		SearxngSettingsRestored: searxngSettingsWritten,
		SearxngSettingsSkipped:  searxngSettingsSkipped,
		// Surface the EXCLUDED agent binary identity for the operator to RE-STAGE
		// (re-download the pinned release) — the binary bytes were never in the
		// archive, exactly like model weights. Nil on an agent-off
		// backup (the manifest recorded no ExcludedAgent).
		ExcludedAgent: ex.manifest.ExcludedAgent,
	}
}

// writeCrushConfig restores the crush.json entry to the out-of-store-root agent
// config destination (Phase 28). A nil seam is a restore-incomplete
// condition surfaced honestly (mirrors rollbackRemove's nil-seam contract) rather
// than a silent skip. The named wrapper survives the seam collapse because the
// honest nil-seam message names the artifact the operator is missing.
func writeCrushConfig(d RestoreDeps, path string, data []byte) error {
	if d.WriteFile == nil {
		return fmt.Errorf("no WriteFile seam wired — cannot restore crush.json to %q", path)
	}
	return d.WriteFile(path, data)
}

// writeSearxngSettings restores the settings.yml entry to the out-of-store-root
// SearXNG config destination (Phase 34). A nil seam is a restore-incomplete
// condition surfaced honestly. The live wiring writes 0600 — the entry holds the
// rendered SEARXNG_SECRET, so the mode must never widen.
func writeSearxngSettings(d RestoreDeps, path string, data []byte) error {
	if d.WriteFile == nil {
		return fmt.Errorf("no WriteFile seam wired — cannot restore settings.yml to %q", path)
	}
	return d.WriteFile(path, data)
}

// rollbackRemove deletes a data-dir artifact the forward path newly created, to
// restore the prior (absent) state verbatim. It requires the RemoveFile
// seam: a nil seam is itself a rollback-incomplete condition (the forward-created
// file cannot be removed), surfaced honestly rather than silently left on disk.
func rollbackRemove(d RestoreDeps, path string) error {
	if d.RemoveFile == nil {
		return fmt.Errorf("no RemoveFile seam wired — cannot remove forward-created %q", path)
	}
	return d.RemoveFile(path)
}

// captureFile reads a current data-dir artifact for the rollback set via Deps.ReadFile.
// An absent/unreadable file yields ok=false (the rollback then simply does not restore
// it — it was not there to begin with), never a hard failure.
func captureFile(d RestoreDeps, path string) (data []byte, ok bool) {
	if path == "" {
		return nil, false
	}
	b, err := d.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// readAndVerify performs the pure read+verify pass (step 1): it parses manifest.json
// (FIRST entry) and verifies every subsequent entry's SHA-256 against the manifest,
// returning the extracted, tar-slip-guarded payload. A manifest whose schema_version
// is unreadable (<=0) or NEWER than this villa supports is a fail-closed BLOCK; a
// per-entry SHA-256 mismatch wraps ErrChecksumMismatch. Zero side effects — it only
// reads the injected archive stream.
func readAndVerify(in RestoreInput) (extracted, error) {
	var ex extracted
	if in.OpenArchive == nil {
		return ex, fmt.Errorf("nil archive opener")
	}

	// First pass: collect entries (manifest FIRST). readArchive applies the tar-slip
	// guard to every entry name before handing it to fn, so a malicious
	// "../escape" / absolute entry is refused here, before any side effect.
	rc, err := in.OpenArchive()
	if err != nil {
		return ex, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = rc.Close() }()

	var (
		manifestSeen bool
		entryIdx     int
	)
	collect := map[string][]byte{}
	err = readArchive(rc, func(name string, data []byte) error {
		idx := entryIdx
		entryIdx++

		if name == EntryManifest {
			// Manifest-first on READ: the manifest MUST be the FIRST tar member
			// so it is parsed + schema-gated before any subsequent body is trusted. An
			// out-of-position manifest is refused (and a second manifest is a duplicate).
			if idx != 0 {
				return fmt.Errorf("archive %s must be the FIRST entry (found at position %d)", EntryManifest, idx)
			}
			m, perr := parseManifest(data)
			if perr != nil {
				return perr
			}
			ex.manifest = m
			manifestSeen = true
			// Schema-gate the manifest BEFORE reading any further entry body:
			// fail-closed BLOCK on an unreadable/incompatible schema, mirroring
			// usage.Load's fail-closed-on-future discipline.
			if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
				return fmt.Errorf("manifest schema_version %d is unreadable or newer than this villa supports (%d)",
					m.SchemaVersion, backupSchemaVersion)
			}
			return nil
		}

		// Every non-manifest entry arrives AFTER the manifest: if the manifest
		// was not the first member, the idx!=0 check above already refused it; a data
		// entry at idx 0 means there was no leading manifest.
		if !manifestSeen {
			return fmt.Errorf("archive %s must be the FIRST entry — entry %q precedes it", EntryManifest, name)
		}
		// Reject duplicate entry names explicitly: the prior `collect[name]=data`
		// silently last-write-won, making verify order-dependent.
		if _, dup := collect[name]; dup {
			return fmt.Errorf("archive contains duplicate entry %q", name)
		}
		collect[name] = data
		return nil
	})
	if err != nil {
		return ex, err
	}
	if !manifestSeen {
		return ex, fmt.Errorf("archive has no %s entry", EntryManifest)
	}

	// Build the manifest-listed name set once (used for both the verify pass and the
	// extra-entry rejection below).
	want := map[string]string{}
	for _, e := range ex.manifest.Entries {
		want[e.Name] = e.SHA256
	}

	// Reject any collected entry NOT listed in the manifest: the archive must
	// contain EXACTLY the manifest-described members — an extra/unexpected entry was
	// previously accepted-and-ignored, which is not what the manifest claims.
	for name := range collect {
		if _, listed := want[name]; !listed {
			return ex, fmt.Errorf("archive contains entry %q not listed in the manifest", name)
		}
	}

	// Verify every manifest-listed entry's SHA-256 against the collected bytes. A
	// missing required entry or a mismatch is archive corruption.
	for name, csum := range want {
		data, ok := collect[name]
		if !ok {
			return ex, fmt.Errorf("manifest lists entry %q but the archive does not contain it", name)
		}
		if verr := verify(bytes.NewReader(data), csum); verr != nil {
			return ex, fmt.Errorf("entry %q: %w", name, verr)
		}
	}

	// Map the verified entries into the typed payload. config.toml + the owui volume
	// tar are REQUIRED; usage.json + bench-reports.jsonl are optional.
	cfgBytes, ok := collect[EntryConfig]
	if !ok {
		return ex, fmt.Errorf("archive is missing the required %s entry", EntryConfig)
	}
	ex.config = cfgBytes
	owuiBytes, ok := collect[EntryOpenWebUIVolume]
	if !ok {
		return ex, fmt.Errorf("archive is missing the required %s entry", EntryOpenWebUIVolume)
	}
	ex.owuiVolume = owuiBytes
	if b, ok := collect[EntryUsage]; ok {
		ex.usage, ex.usagePresent = b, true
	}
	if b, ok := collect[EntryBenchReports]; ok {
		ex.bench, ex.benchPresent = b, true
	}
	// The Phase-23 memory entries are OPTIONAL and flow through the SAME
	// readAndVerify guards as every other entry (SHA-256, tar-slip, duplicate +
	// extra-entry rejection, fail-closed version gate) — no parallel reader
	// (T-23-11).
	if b, ok := collect[EntryQdrantVolume]; ok {
		ex.qdrantVolume, ex.qdrantPresent = b, true
	}
	if b, ok := collect[EntryRecallState]; ok {
		ex.recallState, ex.recallPresent = b, true
	}
	// The Phase-28 coding-agent config entry is OPTIONAL and flows through the SAME
	// readAndVerify guards (SHA-256, tar-slip, duplicate + extra-entry rejection,
	// fail-closed version gate) as every other entry.
	if b, ok := collect[EntryCrushConfig]; ok {
		ex.crushConfig, ex.crushPresent = b, true
	}
	// The Phase-34 web-search settings.yml entry is OPTIONAL and flows through the SAME
	// readAndVerify guards (SHA-256, tar-slip, duplicate + extra-entry rejection,
	// fail-closed version gate) as every other entry.
	if b, ok := collect[EntrySearxngSettings]; ok {
		ex.searxngSettings, ex.searxngSettingsPresent = b, true
	}
	return ex, nil
}

// skewPrompt assembles the WARN-and-confirm prompt text from the skew warnings:
// each finding's Field, Detail, and named Remediation, plus a final y/N question
// The cmd-tier Consent closure prints this and reads the answer.
func skewPrompt(ws []SkewWarning) string {
	var b bytes.Buffer
	b.WriteString("restore detected skew between the backup and the current install:\n")
	for _, w := range ws {
		fmt.Fprintf(&b, "  - %s: %s\n      remediation: %s\n", w.Field, w.Detail, w.Remediation)
	}
	b.WriteString("proceed with restore? [y/N]: ")
	return b.String()
}
