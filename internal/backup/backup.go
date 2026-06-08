package backup

// backup.go holds the PURE skew comparison (BAK-03 / D-08): it compares a backup
// Manifest against the CURRENT install and classifies each difference as either a
// WARN-and-confirm finding (legitimate skew that does NOT block — e.g. a newer
// villa restoring an older backup) or a fail-closed BLOCK (corruption /
// incompatible-future schema that cannot be safely applied). No host I/O — the
// caller supplies the current-install facts as plain data (CurrentInstall) and
// the recomputed checksum verdict as a flag.

import (
	"bytes"
	"fmt"
	"io"
)

// BackupInput is the plain-data drive for the pure Backup orchestrator. The cmd
// tier (liveBackupDeps) gathers everything host-derived — the seam-sourced image
// digests (inference.BackendFor(cfg.Backend).Image() / orchestrate.OpenWebUIImage()
// — NEVER a literal, D-10), the accessor-sourced store schema versions
// (usage.SchemaVersion() / benchstore.SavedReportSchemaVersion()), the resolved
// data-dir artifact paths, the build-stamped villa version, the flattened host
// facts, and the excluded-model identities — then Backup() executes the pure
// quiesce→export→assemble→restart ordering over the injected Deps. Backup imports
// NEITHER inference NOR detect NOR any image literal, so TestSeamGrepGate stays
// green.
type BackupInput struct {
	// CreatedAt is the RFC3339 backup timestamp (caller-supplied so the pure core
	// performs no clock I/O).
	CreatedAt string
	// VillaVersion is the build-stamped binary version (cmd/villa version.go).
	VillaVersion string
	// Host is the flattened host fingerprint (arch / iGPU / kernel).
	Host HostFingerprint

	// InferenceImage / OpenWebUIImage are the seam-sourced digest-pinned images. The
	// caller sources them from the seam; Backup carries them through to the manifest
	// (never a re-typed literal — D-10).
	InferenceImage string
	OpenWebUIImage string

	// ConfigSchemaVersion / UsageSchemaVersion / BenchSchemaVersion are the store
	// schema versions (config from config; usage/bench from the Plan-02 accessors).
	ConfigSchemaVersion int
	UsageSchemaVersion  int
	BenchSchemaVersion  int

	// OutputPath is the traversal-guarded destination archive path the caller has
	// already validated; Backup writes the assembled tar to OutputWriter (the caller
	// opened it 0600). OutputPath is carried for the Result/messages only.
	OutputPath string
	// OutputWriter is the 0600 destination the cmd layer opened (the archive is
	// written here). Kept as a seam so the pure core owns no file handle.
	OutputWriter io.Writer

	// OpenWebUIVolumeName is the podman NAMED volume to export (seam-sourced from
	// orchestrate.OpenWebUIVolumeName()). The villa-models volume is NEVER named here.
	OpenWebUIVolumeName string
	// TempVolumeTar is the temp path the cmd layer chose for the volume-export output;
	// Backup asks Deps.VolumeExport to write here, then reads it back for assembly.
	TempVolumeTar string

	// ConfigPath / UsagePath / BenchReportsPath are the resolved source paths for the
	// archive's config.toml / usage.json / single bench-reports.jsonl entries. A path
	// whose file is absent (ReadFile returns a not-exist error) is skipped, not fatal.
	ConfigPath       string
	UsagePath        string
	BenchReportsPath string

	// ExcludedModels are the identities of the excluded model weights (BAK-01),
	// recorded in the manifest for re-pull. Identity only.
	ExcludedModels []ExcludedModel

	// FileMissing classifies a ReadFile error as a tolerable absent-file (skip the
	// entry) vs a hard error. The cmd layer wires os.IsNotExist; the pure core stays
	// free of os. When nil, any ReadFile error is treated as hard.
	FileMissing func(error) bool
}

// Backup is the PURE backup orchestrator over the injected Deps (BAK-01, D-05). It
// executes the quiesce ordering RESEARCH §OWUI Quiesce mandates and assembles the
// single plain .tar:
//
//  1. Stop the Open WebUI service (clean SQLite copy) and DEFER its restart so the
//     service is brought back even on a mid-backup error (best-effort).
//  2. podman volume export the Open WebUI data volume to the temp tar (Deps seam).
//  3. Read the source entries (the exported volume tar + config.toml + usage.json +
//     the single bench-reports.jsonl); an absent optional data-dir file is skipped.
//  4. Compute a lowercase-hex SHA-256 per entry.
//  5. BuildManifest with the seam-sourced digests + accessor-sourced store schema
//     versions + excluded-model identities injected.
//  6. writeArchive (manifest.json FIRST) to the 0600 OutputWriter the caller opened.
//
// The villa-models volume is NEVER exported. Backup runs no subprocess (links the
// exec package NOT at all) and carries no image literal — every effect is a Deps
// func field.
func Backup(d Deps, in BackupInput) (retRes Result, retErr error) {
	if in.OutputWriter == nil {
		return Result{Err: fmt.Errorf("backup: nil output writer"), FailedStep: "write"}, fmt.Errorf("backup: nil output writer")
	}

	// (1) Quiesce: stop OWUI for a clean SQLite copy, defer best-effort restart so the
	// service is restored even if a later step errors (D-05). The restart stays
	// best-effort (it NEVER fails the backup), but a failed restart is now SURFACED
	// via retRes.RestartWarning so the cmd tier can warn the user to run `villa up`
	// (IN-01). The named return retRes is what every `return` below populates, so the
	// defer (which runs ONLY after a successful Stop) annotates whichever Result is
	// actually returned — without ever turning a successful backup into a failure.
	if err := d.Stop(d.OpenWebUIServiceName); err != nil {
		return Result{Err: fmt.Errorf("backup: stop %s: %w", d.OpenWebUIServiceName, err), FailedStep: "stop"},
			fmt.Errorf("backup: stop %s: %w", d.OpenWebUIServiceName, err)
	}
	defer func() {
		if serr := d.Start(d.OpenWebUIServiceName); serr != nil {
			retRes.RestartWarning = fmt.Sprintf(
				"backup written, but failed to restart %s (%v) — run `villa up`",
				d.OpenWebUIServiceName, serr)
		}
	}()

	// (2) Export ONLY the Open WebUI volume (model weights excluded — BAK-01).
	if err := d.VolumeExport(in.OpenWebUIVolumeName, in.TempVolumeTar); err != nil {
		return Result{Err: fmt.Errorf("backup: volume export %s: %w", in.OpenWebUIVolumeName, err), FailedStep: "volume"},
			fmt.Errorf("backup: volume export %s: %w", in.OpenWebUIVolumeName, err)
	}

	// (3) Read entries. The OWUI volume tar is REQUIRED; config/usage/bench are read
	// from their resolved paths, an absent optional data-dir file being skipped.
	type src struct {
		entry    string
		path     string
		required bool
	}
	sources := []src{
		{EntryOpenWebUIVolume, in.TempVolumeTar, true},
		{EntryConfig, in.ConfigPath, true},
		{EntryUsage, in.UsagePath, false},
		{EntryBenchReports, in.BenchReportsPath, false},
	}

	var entries []archiveEntry
	var checksums []EntryChecksum
	for _, s := range sources {
		if s.path == "" {
			if s.required {
				err := fmt.Errorf("backup: missing required source path for %s", s.entry)
				return Result{Err: err, FailedStep: "read"}, err
			}
			continue
		}
		data, err := d.ReadFile(s.path)
		if err != nil {
			if !s.required && in.FileMissing != nil && in.FileMissing(err) {
				continue // tolerable absent data-dir artifact
			}
			rerr := fmt.Errorf("backup: read %s (%s): %w", s.entry, s.path, err)
			return Result{Err: rerr, FailedStep: "read"}, rerr
		}
		// (4) per-entry SHA-256.
		csum, err := sum(bytes.NewReader(data))
		if err != nil {
			return Result{Err: err, FailedStep: "checksum"}, err
		}
		entries = append(entries, archiveEntry{name: s.entry, data: data})
		checksums = append(checksums, EntryChecksum{Name: s.entry, SHA256: csum})
	}

	// (5) Build the seam/accessor-sourced manifest.
	m := BuildManifest(ManifestInput{
		CreatedAt:           in.CreatedAt,
		VillaVersion:        in.VillaVersion,
		Host:                in.Host,
		InferenceImage:      in.InferenceImage,
		OpenWebUIImage:      in.OpenWebUIImage,
		ConfigSchemaVersion: in.ConfigSchemaVersion,
		UsageSchemaVersion:  in.UsageSchemaVersion,
		BenchSchemaVersion:  in.BenchSchemaVersion,
		Entries:             checksums,
		ExcludedModels:      in.ExcludedModels,
	})
	manifestJSON, err := marshalManifest(m)
	if err != nil {
		return Result{Err: err, FailedStep: "write"}, err
	}

	// (6) Assemble: manifest.json FIRST, then the data entries in deterministic order.
	all := append([]archiveEntry{{name: EntryManifest, data: manifestJSON}}, entries...)
	if err := writeArchive(in.OutputWriter, all); err != nil {
		return Result{Err: err, FailedStep: "write"}, err
	}

	return Result{Reason: fmt.Sprintf("backup written to %s", in.OutputPath)}, nil
}

// CurrentInstall is the plain-data snapshot of the running install that a backup
// Manifest is compared against (BAK-03). The cmd tier gathers these: the current
// villa version (build-stamped), the current inference + OWUI image digests
// (seam-sourced via inference.BackendFor(...).Image() / orchestrate.OpenWebUIImage()
// — never re-typed), the current host fingerprint (from detect), the current
// config/usage/bench store schema versions (usage/bench via the Plan-02
// accessors), and ChecksumFailed (set true when archive verify failed — a
// fail-closed BLOCK trigger).
type CurrentInstall struct {
	VillaVersion        string
	InferenceImage      string
	OpenWebUIImage      string
	Host                HostFingerprint
	ConfigSchemaVersion int
	UsageSchemaVersion  int
	BenchSchemaVersion  int
	// ChecksumFailed is set by the caller when a per-entry SHA-256 verify failed
	// (archive corruption) — CompareSkew turns it into a fail-closed BLOCK (D-08).
	ChecksumFailed bool
}

// SkewWarning is one WARN-and-confirm finding: the field that differs, a
// human-readable detail, and named remediation text (D-08). It does NOT block —
// the caller prints it and requires explicit y/N confirmation (--yes bypass).
type SkewWarning struct {
	Field       string
	Detail      string
	Remediation string
}

// SkewVerdict is the classified outcome of CompareSkew. Block (with BlockReason)
// is a fail-closed refusal with zero side effects; Warnings are surfaced and
// require confirmation but do NOT block. A fully-matching manifest yields neither.
type SkewVerdict struct {
	Block       bool
	BlockReason string
	Warnings    []SkewWarning
}

// CompareSkew classifies the difference between a backup Manifest m and the
// current install cur (pure; BAK-03 / D-08), per the RESEARCH §Skew Detection
// table:
//
//	BLOCK (fail-closed, no apply):
//	  - cur.ChecksumFailed (archive corruption)
//	  - m.SchemaVersion unreadable (<= 0) or NEWER than backupSchemaVersion
//	    (incompatible-future manifest)
//	  - any store schema version (config/usage/bench) in the manifest NEWER than
//	    the current value (future schema can't be safely applied — mirrors
//	    usage.Load's fail-closed-on-future)
//
//	WARN-and-confirm (legitimate skew, does NOT block):
//	  - villa version mismatch
//	  - inference / OWUI image digest mismatch (re-pull remediation)
//	  - host fingerprint mismatch (cross-host caveat)
//	  - any store schema version OLDER in the manifest than current
//
// A fully-matching manifest returns the zero SkewVerdict (no Block, no Warnings).
func CompareSkew(m Manifest, cur CurrentInstall) SkewVerdict {
	var v SkewVerdict

	// --- fail-closed BLOCK checks (D-08) ------------------------------------
	if cur.ChecksumFailed {
		v.Block = true
		v.BlockReason = "archive integrity check failed (SHA-256 mismatch) — refusing to restore a corrupt backup"
		return v
	}
	if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
		v.Block = true
		v.BlockReason = fmt.Sprintf(
			"manifest schema_version %d is unreadable or newer than this villa supports (%d) — cannot safely restore an incompatible manifest",
			m.SchemaVersion, backupSchemaVersion)
		return v
	}
	if blocked, reason := blockOnNewerStore("config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}
	if blocked, reason := blockOnNewerStore("usage", m.UsageSchemaVersion, cur.UsageSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}
	if blocked, reason := blockOnNewerStore("bench", m.BenchSchemaVersion, cur.BenchSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}

	// --- WARN-and-confirm findings (legitimate skew) ------------------------
	if m.VillaVersion != cur.VillaVersion {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "villa_version",
			Detail:      fmt.Sprintf("backup was made by villa %q; this is villa %q", m.VillaVersion, cur.VillaVersion),
			Remediation: "version skew is usually fine; confirm to proceed, or rebuild/reinstall the matching villa version if a behaviour change is suspected",
		})
	}
	if m.InferenceImage != cur.InferenceImage {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "inference_image",
			Detail:      fmt.Sprintf("backup inference image %q differs from current %q", m.InferenceImage, cur.InferenceImage),
			Remediation: "after restore, re-pull the inference image/model weights with `villa model pull <id>` if inference fails to start",
		})
	}
	if m.OpenWebUIImage != cur.OpenWebUIImage {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "openwebui_image",
			Detail:      fmt.Sprintf("backup Open WebUI image %q differs from current %q", m.OpenWebUIImage, cur.OpenWebUIImage),
			Remediation: "the restored Open WebUI data volume was produced by a different image; confirm to proceed (Open WebUI migrates its DB forward on start)",
		})
	}
	if m.Host != cur.Host {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "host",
			Detail:      fmt.Sprintf("backup host %+v differs from current %+v", m.Host, cur.Host),
			Remediation: "backed up on a different host — if Open WebUI cannot read its data after restore, run `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` and ensure the :Z relabel",
		})
	}
	warnOnOlderStore(&v, "config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion)
	warnOnOlderStore(&v, "usage", m.UsageSchemaVersion, cur.UsageSchemaVersion)
	warnOnOlderStore(&v, "bench", m.BenchSchemaVersion, cur.BenchSchemaVersion)

	return v
}

// blockOnNewerStore reports a fail-closed BLOCK when the manifest's store schema
// version is NEWER than the current value — a future schema this villa cannot
// safely apply (mirrors usage.Load's fail-closed-on-future). A zero/absent
// manifest value (<= 0) is treated as "not recorded" and does NOT block.
func blockOnNewerStore(name string, manifestVer, currentVer int) (bool, string) {
	if manifestVer > 0 && manifestVer > currentVer {
		return true, fmt.Sprintf(
			"%s store schema_version %d in the backup is newer than this villa supports (%d) — a future schema cannot be safely applied",
			name, manifestVer, currentVer)
	}
	return false, ""
}

// warnOnOlderStore appends a WARN when the manifest's store schema version is
// OLDER than current (a legitimate older backup; the store migrates forward).
func warnOnOlderStore(v *SkewVerdict, name string, manifestVer, currentVer int) {
	if manifestVer > 0 && manifestVer < currentVer {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       name + "_schema_version",
			Detail:      fmt.Sprintf("%s store schema_version %d in the backup is older than current %d", name, manifestVer, currentVer),
			Remediation: "older store schema; confirm to proceed — the restored store will be read/migrated forward by the current villa",
		})
	}
}
