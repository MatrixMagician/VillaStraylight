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
// over liveBackupDeps, and RETURNS the exit code. A torn output file is removed on a
// failed write so a corrupt partial archive is never left behind.
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

	// Open the destination 0600 (owner-only). Created here so a failure short-circuits
	// before any service quiesce.
	f, err := os.OpenFile(absOut, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(errOut, "backup: open output %q: %v\n", absOut, err)
		return exitBlocked
	}

	// Temp file for the volume export; same dir as the output so the rename/cleanup
	// stays on one filesystem. Removed after assembly.
	tmpVol, err := os.CreateTemp(parent, ".villa-owui-vol-*.tar")
	if err != nil {
		_ = f.Close()
		_ = os.Remove(absOut)
		fmt.Fprintf(errOut, "backup: temp volume file: %v\n", err)
		return exitBlocked
	}
	tmpVolPath := tmpVol.Name()
	_ = tmpVol.Close()
	defer func() { _ = os.Remove(tmpVolPath) }()

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

	res, rerr := backup.Backup(d, in)
	if cerr := f.Close(); cerr != nil && rerr == nil {
		rerr = cerr
		res.Err = cerr
		res.FailedStep = "write"
	}
	if rerr != nil {
		_ = os.Remove(absOut) // never leave a corrupt partial archive
		fmt.Fprintf(errOut, "backup: failed at %s: %v\n", res.FailedStep, rerr)
		return exitBlocked
	}

	fmt.Fprintf(out, "backup written to %s\n", absOut)
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
		Stop:                 sys.Stop,
		Start:                sys.Start,
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
	}
}
