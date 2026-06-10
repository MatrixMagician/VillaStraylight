package main

// restore.go wires `villa restore <archive>` (BAK-02 / BAK-03): the transactional
// apply of a backup archive — read+verify -> skew WARN-and-confirm -> capture ->
// quiesce -> clean-recreate-before-import -> restart -> offload-asserting prove ->
// rollback-on-failure. The pure transactional state-machine lives in internal/backup
// (Restore); this file is the thin cobra caller + liveRestoreDeps / liveRestoreInput
// wiring: config restored via config.SaveVilla (atomic 0600/0700, traversal-guarded —
// NEVER hand-write), the Open WebUI volume rm/import via the shared cmd-tier fixed-arg
// podman volume seam (podman_volume.go, D-02), the Quadlet recreate via
// orchestrate.Render->Reconcile->WriteUnits->DaemonReload (config is the single source
// of truth, D-07), EnsureVolume via an explicit `podman volume create` so import never
// hits "no volume with name" (RESEARCH OQ2-RESOLVED), and an offload-asserting Prove
// composing preflight + a status residency assert (a ready+health-200-but-residency-
// FAIL maps to a NON-pass -> rollback; D-07). runRestore RETURNS the exit code (the
// RunE wrapper calls os.Exit), mirroring runBackup/runUninstall. --json is intentionally
// NOT implemented this phase (D-13, deferred).

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/benchstore"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// newRestore builds `villa restore <archive>`: the transactional restore. The archive
// path is a positional arg; --yes/--force bypass the skew confirmation gate (D-08).
func newRestore() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <archive>",
		Short: "Restore the workspace from a backup .tar transactionally (capture -> swap -> prove -> rollback)",
		Long: "Restore a `villa backup` archive: verify its per-entry SHA-256 checksums (a corrupt archive or an " +
			"incompatible manifest is a fail-closed BLOCK with zero side effects), warn-and-confirm on version/digest/" +
			"store-schema skew (bypass with --yes/--force), capture the current state for rollback, briefly stop Open " +
			"WebUI, restore config.toml + the usage/bench stores, clean-recreate the Open WebUI data volume (remove -> " +
			"regenerate the Quadlet unit from the restored config -> create -> import, so stale data never leaks " +
			"through a merge), restart, and PROVE the restored stack (preflight + GPU-residency-honest status). Any " +
			"mutate error or a non-pass proof rolls back verbatim — a failed restore leaves the running stack intact. " +
			"Strictly local — no data leaves the box.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in, d, tmpDir, code := liveRestore(cmd, args[0], yes)
			// WR-01: clean up the restore temp dir (it holds the exported Open WebUI
			// volume tar incl. webui.db) on BOTH the success and error paths. os.Exit
			// skips defers, so remove it explicitly before every exit. tmpDir is empty
			// when liveRestore returned before creating it (a pre-MkdirTemp error).
			cleanup := func() {
				if tmpDir != "" {
					_ = os.RemoveAll(tmpDir)
				}
			}
			if code != exitPass {
				cleanup()
				os.Exit(code)
				return nil
			}
			rc, preserveTmp := runRestore(cmd, args[0], in, d, tmpDir)
			// CR-01: when the rollback did NOT fully complete, tmpDir holds the ONLY
			// copies of the prior volume data (rollback-owui.tar / rollback-qdrant.tar)
			// — deleting it would permanently lose the prior chat database. runRestore
			// already printed the preservation notice + recovery hint.
			if !preserveTmp {
				cleanup() // must survive until AFTER backup.Restore returns
			}
			os.Exit(rc)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "bypass the skew confirmation prompt (apply despite version/digest/schema skew)")
	// --force is the inherited global persistent flag (root.go); it also bypasses the
	// skew confirmation (read via the global `force` in liveRestore). Not re-registered
	// here to avoid a duplicate-flag panic.
	return cmd
}

// runRestore drives the pure backup.Restore over the live (or test) Deps + input and
// maps the typed Result to an exit code + messages (clone of runBackendSet's mapping):
// Restored -> exitPass; Refused -> exitBlocked (a clean fail-closed/decline, zero side
// effects); RolledBack -> exitBlocked with the honest rollback reason; a bare Err ->
// exitBlocked. The body RETURNS the int (no os.Exit) so tests assert output + code.
// preserveTmp reports whether the caller must PRESERVE the restore temp dir (CR-01):
// it is true exactly when the rollback did not fully complete — tmpDir then holds the
// ONLY copies of the prior volume data and deleting it would lose the prior chats.
func runRestore(cmd *cobra.Command, archivePath string, in backup.RestoreInput, d backup.Deps, tmpDir string) (code int, preserveTmp bool) {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	res := backup.Restore(d, in)
	switch {
	case res.Refused:
		if res.Reason != "" {
			fmt.Fprintf(errOut, "restore: refusing to apply %s — %s\n", archivePath, res.Reason)
		} else if res.Err != nil {
			fmt.Fprintf(errOut, "restore: refusing to apply %s — %s failed: %v\n", archivePath, res.FailedStep, res.Err)
		} else {
			fmt.Fprintf(errOut, "restore: refusing to apply %s\n", archivePath)
		}
		return exitBlocked, false
	case res.RolledBack:
		fmt.Fprintf(errOut, "restore: applying %s failed at %q — rolled back; prior stack restored\n", archivePath, res.FailedStep)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		if res.RollbackIncomplete {
			// CR-01: an incomplete rollback means the captured prior state was NOT
			// fully re-applied — the rollback tars in tmpDir are the only copies of
			// the prior Open WebUI (webui.db) / Qdrant volume data. Preserve them
			// and tell the operator how to recover instead of silently deleting.
			if tmpDir != "" {
				fmt.Fprintf(errOut, "restore: PRESERVING %s — the rollback did not fully complete and this directory holds the ONLY copies of the prior volume data (rollback-owui.tar / rollback-qdrant.tar).\n", tmpDir)
				fmt.Fprintf(errOut, "  recover: fix the cause above, stop the affected service, then `podman volume import <volume> <rollback tar>`; remove the directory once the prior data is safe.\n")
			}
			return exitBlocked, true
		}
		return exitBlocked, false
	case res.Err != nil:
		fmt.Fprintf(errOut, "restore: applying %s failed at %q: %v\n", archivePath, res.FailedStep, res.Err)
		return exitBlocked, false
	default: // Restored
		fmt.Fprintf(out, "restored %s — config + Open WebUI data + usage/bench stores applied, cutover proven\n", archivePath)
		fmt.Fprintf(out, "note: model weights are not in the backup; if inference fails to start, re-pull with `villa model pull <id>`\n")
		// Honest Phase-23 memory reporting (D-07/OQ1: report, never extend Prove —
		// the memory stack is NOT covered by the cutover proof; verify it explicitly).
		if res.QdrantRestored {
			fmt.Fprintf(out, "memory: Qdrant volume restored — verify with `villa doctor`; if a dimension skew was confirmed at restore, run `villa recall index --rebuild`\n")
		} else {
			fmt.Fprintf(out, "memory volume not present in this backup — existing Qdrant data left untouched\n")
		}
		if res.RecallStateRestored {
			fmt.Fprintf(out, "memory: recall state restored\n")
		} else {
			fmt.Fprintf(out, "memory: recall state not present in this backup — left untouched\n")
		}
		// The restored config is the single source of truth for the stack shape
		// (Pitfall 5). Stale memory unit files are NOT removed by the reconcile
		// (it writes changed units only) — existing behavior, reported not "fixed".
		posture := "disabled"
		if res.RestoredMemoryEnabled {
			posture = "enabled"
		}
		fmt.Fprintf(out, "memory stack: %s (restored config); note: a reconcile does not remove stale unit files — bring services up with `villa up`\n", posture)
		return exitPass, false
	}
}

// liveRestore resolves + traversal-validates the archive path, gathers the
// seam-/accessor-sourced current-install facts for the skew compare, and assembles the
// live RestoreInput + liveRestoreDeps. It RETURNS exitPass on success, or a non-pass
// code (with a stderr message already printed) when the archive path or config cannot
// be resolved BEFORE any side effect. The returned tmpDir is the restore temp dir
// (holding the extracted/rollback volume tars); the caller MUST remove it after
// backup.Restore returns (WR-01). tmpDir is "" on every pre-MkdirTemp error path.
func liveRestore(cmd *cobra.Command, archivePath string, bypass bool) (backup.RestoreInput, backup.Deps, string, int) {
	errOut := cmd.ErrOrStderr()

	absArchive, err := filepath.Abs(filepath.Clean(archivePath))
	if err != nil {
		fmt.Fprintf(errOut, "restore: bad archive path %q: %v\n", archivePath, err)
		return backup.RestoreInput{}, backup.Deps{}, "", exitBlocked
	}
	if _, err := os.Stat(absArchive); err != nil {
		fmt.Fprintf(errOut, "restore: cannot read archive %q: %v\n", absArchive, err)
		return backup.RestoreInput{}, backup.Deps{}, "", exitBlocked
	}

	// Current install facts (BAK-03): seam-sourced digests (never re-typed — D-10),
	// accessor-sourced store schema versions, flattened host fingerprint.
	cfg, err := config.LoadVilla()
	if err != nil {
		fmt.Fprintf(errOut, "restore: load config: %v\n", err)
		return backup.RestoreInput{}, backup.Deps{}, "", exitBlocked
	}
	be, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		fmt.Fprintf(errOut, "restore: resolve backend %q: %v\n", cfg.Backend, err)
		return backup.RestoreInput{}, backup.Deps{}, "", exitBlocked
	}
	cur := backup.CurrentInstall{
		VillaVersion:        villaVersion(),
		InferenceImage:      be.Image(),
		OpenWebUIImage:      orchestrate.OpenWebUIImage(),
		Host:                liveHostFingerprint(),
		ConfigSchemaVersion: 0, // VillaConfig carries no schema_version field (not recorded).
		UsageSchemaVersion:  usage.SchemaVersion(),
		BenchSchemaVersion:  benchstore.SavedReportSchemaVersion(),
		// Embedding identity for the Phase-23 dimension-skew compare (D-08): config
		// is the single source of truth; the recall schema comes from its accessor.
		EmbeddingModel:      cfg.EmbeddingModel,
		EmbeddingDim:        cfg.EmbeddingDim,
		RecallSchemaVersion: recall.SchemaVersion(),
	}

	// Temp dir (same data-home parent) for the extracted + rollback volume tars. The
	// caller removes it after backup.Restore returns (WR-01) — it holds the exported
	// Open WebUI volume tar, which contains the user's chat database (webui.db).
	tmpDir, err := os.MkdirTemp("", "villa-restore-*")
	if err != nil {
		fmt.Fprintf(errOut, "restore: temp dir: %v\n", err)
		return backup.RestoreInput{}, backup.Deps{}, "", exitBlocked
	}

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
		// Phase-23 qdrant volume + recall-state wiring (D-05/D-07): identities are
		// seam-sourced; the qdrant tars live in the SAME WR-01-cleaned tmpDir (they
		// hold chat-derived vectors, same sensitivity as webui.db); the existence
		// check is the fail-soft cmd-tier `podman volume exists` helper.
		QdrantVolumeName:   orchestrate.QdrantVolumeName(),
		TempQdrantTar:      filepath.Join(tmpDir, "restore-qdrant.tar"),
		RollbackQdrantTar:  filepath.Join(tmpDir, "rollback-qdrant.tar"),
		QdrantVolumeExists: volumeExists(orchestrate.QdrantVolumeName(), errOut),
		RecallDestPath:     recall.RecallStatePath(),
	}
	return in, liveRestoreDeps(), tmpDir, exitPass
}

// liveSkewConsent prints the assembled skew WARN+remediation prompt and reads a y/N
// answer. A non-interactive session declines (consent is opt-IN — the user must pass
// --yes/--force to apply over skew non-interactively, D-08). Clones the uninstall.go
// stdinIsInteractive + promptConsent gate.
func liveSkewConsent(prompt string) bool {
	if !stdinIsInteractive() {
		fmt.Fprint(os.Stderr, prompt)
		fmt.Fprintln(os.Stderr, "\nnot interactive — declining (re-run with --yes/--force to apply over skew)")
		return false
	}
	return promptConsent(prompt)
}

// liveRestoreDeps wires the pure backup.Restore seam to the real host: config
// load/save (SaveVilla — atomic 0600/0700, traversal-guarded), the shared fixed-arg
// podman volume export/import/rm + an explicit ensure-create, the Quadlet recreate
// (Render->Reconcile->WriteUnits->DaemonReload from the restored config), the systemd
// stop/start/restart seam, atomic data-dir writes via usage.WriteFileAtomic, and the
// offload-asserting liveRestoreProve as the cutover gate.
func liveRestoreDeps() backup.Deps {
	sys := orchestrate.NewSystemd()
	return backup.Deps{
		OpenWebUIServiceName: openWebUIServiceName,
		InstallServiceName:   installServiceName,
		// QdrantServiceName mirrors liveBackupDeps: derived from the orchestrate
		// unit-name accessor, never a re-typed literal (D-05).
		QdrantServiceName: unitServiceName(orchestrate.QdrantContainerUnitName()),
		LoadConfig:        config.LoadVilla,
		SaveConfig:        config.SaveVilla, // atomic 0600/0700, traversal-guarded — NEVER hand-write
		VolumeExport: func(name, outPath string) error {
			if err := requirePodman(); err != nil {
				return err
			}
			stderr, err := podmanVolume(volumeExportArgs(name, outPath))
			if err != nil {
				return fmt.Errorf("podman volume export %s: %w: %s", name, err, stderr)
			}
			return nil
		},
		VolumeImport: func(name, src string) error {
			if err := requirePodman(); err != nil {
				return err
			}
			stderr, err := podmanVolume(volumeImportArgs(name, src))
			if err != nil {
				return fmt.Errorf("podman volume import %s: %w: %s", name, err, stderr)
			}
			return nil
		},
		VolumeRm: func(name string) error {
			if err := requirePodman(); err != nil {
				return err
			}
			// Tolerate an already-absent volume (clean-recreate is idempotent) by
			// inspecting the not-found stderr — `podman volume rm` has no tolerance flag
			// (mirrors removeVolumesLive).
			stderr, err := podmanVolume(volumeRmArgs(name))
			if err == nil {
				return nil
			}
			if isVolumeNotFound(stderr) {
				return nil
			}
			return fmt.Errorf("podman volume rm %s: %w: %s", name, err, stderr)
		},
		EnsureVolume: func(name string) error {
			if err := requirePodman(); err != nil {
				return err
			}
			// Explicit idempotent `podman volume create` so the subsequent import has a
			// target — import does NOT auto-create (RESEARCH OQ2-RESOLVED). An
			// already-exists is a harmless no-op.
			stderr, err := podmanVolume([]string{"volume", "create", name})
			if err == nil {
				return nil
			}
			if isVolumeAlreadyExists(stderr) {
				return nil
			}
			return fmt.Errorf("podman volume create %s: %w: %s", name, err, stderr)
		},
		ReconcileAndWrite: func(c config.VillaConfig) (bool, error) {
			dir, err := quadletUnitDir()
			if err != nil {
				return false, err
			}
			modelFile, err := liveModelFile(c)
			if err != nil {
				return false, err
			}
			backend, err := inference.BackendFor(c.Backend)
			if err != nil {
				return false, err
			}
			units, err := orchestrate.Render(orchestrate.RenderInput{
				Backend:   backend,
				Cfg:       c,
				ModelFile: modelFile,
				ModelsDir: modelsDir(),
			})
			if err != nil {
				return false, err
			}
			plan, err := orchestrate.Reconcile(units, dir)
			if err != nil {
				return false, err
			}
			if len(plan.Changed) == 0 {
				return false, nil
			}
			if err := orchestrate.WriteUnits(plan, dir); err != nil {
				return false, err
			}
			if err := sys.DaemonReload(); err != nil {
				return false, err
			}
			return true, nil
		},
		DaemonReload:    sys.DaemonReload,
		Stop:            sys.Stop,
		Start:           sys.Start,
		Restart:         sys.Restart,
		ReadFile:        os.ReadFile,
		WriteFileAtomic: usage.WriteFileAtomic,
		// WriteTempFile stages the extracted OWUI volume tar in the restore temp dir
		// (an os.MkdirTemp dir OUTSIDE the villa data store), so it must NOT use the
		// store-root-guarded usage.WriteFileAtomic (that guard rejects /tmp paths and
		// broke restore on a real host). MkdirTemp made the dir 0700; the tar is 0600.
		WriteTempFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
		// RemoveFile (CR-01): delete a data-dir artifact the forward path created
		// where none existed before, to restore the prior (absent) state verbatim on
		// rollback. Tolerate an already-absent file (it is the goal state).
		RemoveFile: func(path string) error {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		},
		Prove: liveRestoreProve,
	}
}

// liveRestoreProve is the offload-asserting restore-cutover gate (backup.Deps.Prove),
// composing preflight + a status residency assert (D-07). It FIRST re-runs the ROCm
// preflight gate for a ROCm-family target (the host-prep gate the restored config must
// still satisfy); a BLOCK there is a prove FAIL -> rollback. It then reuses the proven
// liveProve composition (bounded readiness + a REAL generation probe + GPU-residency
// proof with gpu_busy sampled DURING the decode) and maps its verdict into a
// backup.ProveVerdict, mapping ONLY a true pass to ProveStatusPass — a
// ready+health-200-but-residency-FAIL or a silent CPU fallback is a NON-pass -> the
// core rolls back. All backend markers stay behind the inference seam; this function
// re-types none. It adds NO new outbound (status no_telemetry preserved — D-12).
func liveRestoreProve(target string) backup.ProveVerdict {
	// (preflight) For a ROCm-family target, the restored host must still pass the ROCm
	// preflight against the resolved image digest; a BLOCK is a prove FAIL.
	if inference.IsROCmFamily(target) {
		be, err := inference.BackendFor(target)
		if err != nil {
			return backup.ProveVerdict{Status: "fail", Detail: err.Error()}
		}
		for _, c := range preflight.RunROCmForImage(detect.Probe(), be.Image()) {
			// Fail the prove on the BLOCK tier using the EXACT canonical predicate
			// `villa preflight`/install gates an install on (renderPreflight,
			// cmd/villa/preflight.go: r.Status == StatusFail && r.Tier == TierBlock).
			// In the preflight enum (internal/preflight/preflight.go) BLOCK is a TIER,
			// not a distinct Status: StatusFail is "a confident known-bad" and is
			// "only meaningful on TierBlock checks" — a BLOCK-tier check that cannot be
			// EVALUATED downgrades to StatusWarn (D-15), never StatusFail. So this pairs
			// the two conjuncts to mirror the install gate verbatim and stay auditable;
			// a WARN-tier or could-not-verify result is intentionally NOT a prove fail.
			if c.Status == preflight.StatusFail && c.Tier == preflight.TierBlock {
				return backup.ProveVerdict{Status: "fail", Detail: "ROCm preflight: " + c.Detail}
			}
		}
	}

	// (status residency) Bounded readiness + real generation probe + GPU-residency proof.
	v := liveProve(context.Background(), target)
	if v.Status == backendswap.ProveStatusPass { // a true offload-honest pass
		return backup.ProveVerdict{Status: backup.ProveStatusPass, Detail: v.Detail}
	}
	return backup.ProveVerdict{Status: "fail", Detail: v.Detail}
}

// isVolumeNotFound recognises the `podman volume rm` not-found stderr so the
// clean-recreate stays idempotent (mirrors removeVolumesLive's inspection).
func isVolumeNotFound(stderr string) bool {
	low := strings.ToLower(stderr)
	return strings.Contains(low, "no such volume") || strings.Contains(low, "no volume with name")
}

// isVolumeAlreadyExists recognises the `podman volume create` already-exists stderr so
// the explicit ensure-create is a harmless no-op when the Quadlet generator already
// materialized the volume (RESEARCH OQ2-RESOLVED).
func isVolumeAlreadyExists(stderr string) bool {
	low := strings.ToLower(stderr)
	return strings.Contains(low, "already exists") || strings.Contains(low, "already in use")
}
