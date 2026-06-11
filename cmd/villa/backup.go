package main

// backup.go wires `villa backup` (BAK-01): a single self-describing local .tar of
// the recreatable workspace state — config.toml + the Open WebUI data volume +
// usage.json + the single bench-reports.jsonl + a manifest of versions, both image
// digests (seam-sourced — never a literal, D-10), store schema versions
// (accessor-sourced), per-entry SHA-256 checksums, and the excluded model
// identities for re-pull. Model WEIGHTS are excluded (re-pullable; BAK-01).
//
// The pure quiesce→export→assemble→restart orchestration lives in internal/backup
// (Backup); this file is the thin cobra caller + liveBackupDeps wiring: podman
// volume export via the shared cmd-tier fixed-arg seam (podman_volume.go, D-02),
// service stop/start via orchestrate.NewSystemd, file reads via os.ReadFile, and
// the 0600 traversal-guarded output file the archive is written to. runBackup
// RETURNS the exit code (the RunE wrapper calls os.Exit), mirroring runUninstall.
// --json is intentionally NOT implemented this phase (D-13, deferred).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/benchstore"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// backupTimestamp returns an FS-safe timestamp for the default archive name (D-04):
// no ':' (which is illegal on some filesystems and confuses shells) — RFC3339-basic
// with colons replaced, e.g. 20260607T142233Z.
func backupTimestamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// defaultBackupName is the default output file `villa-backup-<timestamp>.tar` in CWD
// (D-04).
func defaultBackupName(t time.Time) string {
	return fmt.Sprintf("villa-backup-%s.tar", backupTimestamp(t))
}

// newBackup builds `villa backup`: produce the single self-describing .tar (BAK-01).
func newBackup() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up the workspace to a single local .tar (config + Open WebUI data + usage/bench stores)",
		Long: "Produce a single self-describing .tar archive of the recreatable workspace state: config.toml, the " +
			"Open WebUI data volume (exported with the service briefly stopped for a clean SQLite copy), the usage " +
			"store, and the saved bench reports, plus a manifest of versions, image digests, store schema versions, " +
			"per-entry SHA-256 checksums, and the identities of the EXCLUDED model weights (re-pullable, recorded for " +
			"re-pull). Model weights themselves are not backed up. Default output is villa-backup-<timestamp>.tar in " +
			"the current directory; override with -o/--output. Strictly local — no data leaves the box.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runBackup(cmd, output, liveBackupDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output archive path (default villa-backup-<timestamp>.tar in CWD)")
	return cmd
}

// runBackup resolves the output path (traversal-guarded against its parent dir),
// gathers the seam-/accessor-sourced BackupInput, drives the pure Backup orchestrator
// over liveBackupDeps, and RETURNS the exit code. The archive is assembled in a
// same-dir temp file and renamed onto the destination only after a fully-successful
// write (WR-04): a mid-backup failure removes only the temp — a pre-existing archive
// at the output path is never truncated or deleted.
func runBackup(cmd *cobra.Command, output string, d backup.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Resolve + traversal-guard the output path against its parent dir.
	if output == "" {
		output = defaultBackupName(time.Now())
	}
	absOut, err := filepath.Abs(filepath.Clean(output))
	if err != nil {
		fmt.Fprintf(errOut, "backup: bad output path %q: %v\n", output, err)
		return exitBlocked
	}
	parent := filepath.Dir(absOut)
	if err := assertBackupOutputInside(absOut, parent); err != nil {
		fmt.Fprintf(errOut, "backup: refusing output path: %v\n", err)
		return exitBlocked
	}

	// Load config (the single source of truth) for backend selection + the data the
	// manifest records.
	cfg, err := config.LoadVilla()
	if err != nil {
		fmt.Fprintf(errOut, "backup: load config: %v\n", err)
		return exitBlocked
	}

	// Resolve the inference image digest from the SEAM (never a literal — D-10).
	be, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		fmt.Fprintf(errOut, "backup: %v\n", err)
		return exitBlocked
	}

	// Resolve the config.toml source path (the archive's config entry).
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(errOut, "backup: resolve config path: %v\n", err)
		return exitBlocked
	}

	// Stage the archive in a SAME-DIR temp file and os.Rename it onto absOut only
	// after a fully-successful assembly (review WR-04): re-using an output path
	// (e.g. a cron'd `villa backup -o ~/backups/villa-latest.tar`) must NEVER
	// destroy the PREVIOUS archive on a mid-backup failure — the old
	// O_TRUNC-then-remove flow left the operator with ZERO backups whenever any
	// later step failed. Same temp+rename discipline as config.SaveVilla /
	// usage.WriteFileAtomic; CreateTemp creates the file 0600 (owner-only).
	// Created here so a failure short-circuits before any service quiesce.
	f, err := os.CreateTemp(parent, ".villa-backup-*.tar")
	if err != nil {
		fmt.Fprintf(errOut, "backup: open output temp file in %q: %v\n", parent, err)
		return exitBlocked
	}
	tmpOutPath := f.Name()

	// Temp file for the volume export; same dir as the output so the rename/cleanup
	// stays on one filesystem. Removed after assembly.
	tmpVol, err := os.CreateTemp(parent, ".villa-owui-vol-*.tar")
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmpOutPath) // the prior archive at absOut stays untouched (WR-04)
		fmt.Fprintf(errOut, "backup: temp volume file: %v\n", err)
		return exitBlocked
	}
	tmpVolPath := tmpVol.Name()
	_ = tmpVol.Close()
	defer func() { _ = os.Remove(tmpVolPath) }()

	// Optional Phase-23 qdrant volume entry (D-05): gated on cfg.MemoryEnabled AND a
	// fail-soft existence check over the podmanVolume seam — memory off or volume
	// absent means the entry is honestly omitted (and the core makes ZERO qdrant
	// Deps calls). The temp tar clones the OWUI same-dir frame above; it holds
	// chat-derived vectors, so it is removed on every exit path.
	includeQdrant := cfg.MemoryEnabled && volumeExists(orchestrate.QdrantVolumeName(), errOut)
	tmpQdrantPath := ""
	if includeQdrant {
		// Temp-name pattern deliberately avoids the volume literal (the seam-grep
		// acceptance gate): the identity comes from orchestrate.QdrantVolumeName().
		tmpQdrant, err := os.CreateTemp(parent, ".villa-memory-vol-*.tar")
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmpOutPath) // the prior archive at absOut stays untouched (WR-04)
			fmt.Fprintf(errOut, "backup: temp qdrant volume file: %v\n", err)
			return exitBlocked
		}
		tmpQdrantPath = tmpQdrant.Name()
		_ = tmpQdrant.Close()
		defer func() { _ = os.Remove(tmpQdrantPath) }()
	}

	in := backup.BackupInput{
		CreatedAt:           time.Now().UTC().Format(time.RFC3339),
		VillaVersion:        villaVersion(),
		Host:                liveHostFingerprint(),
		InferenceImage:      be.Image(),
		OpenWebUIImage:      orchestrate.OpenWebUIImage(),
		ConfigSchemaVersion: 0, // VillaConfig carries no schema_version field (not recorded).
		UsageSchemaVersion:  usage.SchemaVersion(),
		BenchSchemaVersion:  benchstore.SavedReportSchemaVersion(),
		OutputPath:          absOut,
		OutputWriter:        f,
		OpenWebUIVolumeName: orchestrate.OpenWebUIVolumeName(),
		TempVolumeTar:       tmpVolPath,
		ConfigPath:          cfgPath,
		UsagePath:           usage.UsagePath(),
		BenchReportsPath:    benchReportsStorePath(),
		ExcludedModels:      excludedModelIdentities(cfg),
		FileMissing:         os.IsNotExist,
	}
	if includeQdrant {
		// Seam-sourced volume identity — NEVER a literal here (D-05).
		in.QdrantVolumeName = orchestrate.QdrantVolumeName()
		in.TempQdrantTar = tmpQdrantPath
	}
	if cfg.MemoryEnabled {
		// RecallStatePath is gated on cfg.MemoryEnabled (review WR-03), mirroring
		// the qdrant entry: a memory-OFF backup must produce an archive IDENTICAL
		// to the v1 layout even when a leftover recall-state.json exists from a
		// previously-enabled memory stack. Including the orphan entry while the
		// manifest omits recall_schema_version would let it escape the fail-closed
		// blockOnNewerStore gate on restore. An absent file is still skipped by the
		// core's optional-entry FileMissing logic (D-06).
		in.RecallStatePath = recall.RecallStatePath()
		// Manifest embedding identity + recall store schema, recorded ONLY on a
		// memory-on backup (D-06/D-08): config is the single source of truth for the
		// embedding model/dim; the recall schema comes from its accessor. A
		// memory-off backup omits all three (the "not recorded" convention) — note
		// cfg self-heals embedding defaults even when memory is off, so this gate is
		// what keeps memory-off manifests claim-free.
		in.EmbeddingModel = cfg.EmbeddingModel
		in.EmbeddingDim = cfg.EmbeddingDim
		in.RecallSchemaVersion = recall.SchemaVersion()
	}

	res, rerr := backup.Backup(d, in)
	if cerr := f.Close(); cerr != nil && rerr == nil {
		rerr = cerr
		res.Err = cerr
		res.FailedStep = "write"
	}
	if rerr != nil {
		// Remove only the torn TEMP file — a pre-existing archive at absOut is
		// preserved (WR-04): a failed backup must never destroy the previous one.
		_ = os.Remove(tmpOutPath)
		fmt.Fprintf(errOut, "backup: failed at %s: %v\n", res.FailedStep, rerr)
		return exitBlocked
	}
	// Publish atomically: rename the fully-written, closed temp onto the
	// destination (same dir ⇒ same filesystem). Only now is a prior archive at
	// absOut replaced — and only by a complete, verified-write archive.
	if err := os.Rename(tmpOutPath, absOut); err != nil {
		_ = os.Remove(tmpOutPath)
		fmt.Fprintf(errOut, "backup: publish output %q: %v\n", absOut, err)
		return exitBlocked
	}

	fmt.Fprintf(out, "backup written to %s\n", absOut)
	// Surface a failed best-effort service restart (IN-01): the backup succeeded,
	// but a service is likely down — warn rather than exit 0 silently.
	if res.RestartWarning != "" {
		fmt.Fprintf(errOut, "warning: %s\n", res.RestartWarning)
	}
	// Honest memory-entry reporting (Phase 23): state whether the qdrant volume and
	// recall state made it into the archive — never leave the operator guessing.
	if includeQdrant {
		fmt.Fprintf(out, "memory: Qdrant volume included (%s)\n", backup.EntryQdrantVolume)
	} else {
		fmt.Fprintf(out, "memory: Qdrant volume not included (memory disabled or volume absent)\n")
	}
	switch {
	case in.RecallStatePath == "":
		// Memory off ⇒ the entry was never offered to the core (WR-03 gate).
		fmt.Fprintf(out, "memory: recall state not included (memory disabled)\n")
	default:
		if _, serr := os.Stat(in.RecallStatePath); serr == nil {
			fmt.Fprintf(out, "memory: recall state included (%s)\n", backup.EntryRecallState)
		} else {
			fmt.Fprintf(out, "memory: recall state not included (no recall-state.json)\n")
		}
	}
	if len(in.ExcludedModels) > 0 {
		fmt.Fprintf(out, "excluded model weights (re-pullable, recorded in manifest for re-pull):\n")
		for _, m := range in.ExcludedModels {
			fmt.Fprintf(out, "  - %s (quant %s, ctx %s)\n", m.ID, m.Quant, m.Ctx)
		}
	}
	return exitPass
}

// assertBackupOutputInside verifies the resolved output path stays within its parent
// dir (T-16-02a tar/output traversal guard), mirroring the config/usage guard shape.
func assertBackupOutputInside(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("output %q escapes its parent dir %q", absPath, absDir)
	}
	return nil
}

// liveHostFingerprint flattens detect.Probe()'s typed-Unknown HostProfile into the
// plain-string backup.HostFingerprint (keeping internal/backup free of detect — the
// SeamGrepGate-clean discipline). Unknown values become the empty sentinel so the
// manifest records "" rather than a fabricated value.
func liveHostFingerprint() backup.HostFingerprint {
	hp := detect.Probe()
	return backup.HostFingerprint{
		Arch:   knownOrEmpty(hp.Arch.Known, hp.Arch.Value),
		IGPU:   knownOrEmpty(hp.IGPUGfxID.Known, hp.IGPUGfxID.Value),
		Kernel: knownOrEmpty(hp.KernelVersion.Known, hp.KernelVersion.Value),
	}
}

// knownOrEmpty returns v when known, else "" (honest empty sentinel, never a
// fabricated value on an Unknown probe).
func knownOrEmpty(known bool, v string) string {
	if known {
		return v
	}
	return ""
}

// excludedModelIdentities records the IDENTITY of the model weight the backup
// excludes (BAK-01), sourced from config (the single source of truth) so restore can
// report it for re-pull. Identity only — never any content. An empty config model
// yields no record.
func excludedModelIdentities(cfg config.VillaConfig) []backup.ExcludedModel {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	ctx := ""
	if cfg.Ctx > 0 {
		ctx = strconv.Itoa(cfg.Ctx)
	}
	return []backup.ExcludedModel{{
		ID:     cfg.Model,
		Quant:  cfg.Quant,
		Ctx:    ctx,
		Source: "catalog",
	}}
}

// liveBackupDeps wires the pure backup.Backup seam to the real host: service
// stop/start via orchestrate.NewSystemd, podman volume export via the shared
// fixed-arg cmd-tier seam (podman_volume.go), and file reads via os.ReadFile.
func liveBackupDeps() backup.Deps {
	sys := orchestrate.NewSystemd()
	return backup.Deps{
		OpenWebUIServiceName: openWebUIServiceName,
		// QdrantServiceName is derived from the orchestrate unit-name accessor (the
		// same unitServiceName mapping doctor/status use) — never a re-typed literal
		// (D-05).
		QdrantServiceName: unitServiceName(orchestrate.QdrantContainerUnitName()),
		Stop:              sys.Stop,
		Start:             sys.Start,
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
		ReadFile: os.ReadFile,
		// OpenFile is the WR-06 streaming seam: the exported volume tars (the one
		// entry class that can reach many GiB) are checksummed and tar-copied via
		// io.Copy from this reader instead of being buffered whole in memory.
		OpenFile: func(path string) (io.ReadCloser, int64, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, 0, err
			}
			fi, err := f.Stat()
			if err != nil {
				_ = f.Close()
				return nil, 0, err
			}
			return f, fi.Size(), nil
		},
	}
}
