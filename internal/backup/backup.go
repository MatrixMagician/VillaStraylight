package backup

// backup.go holds the PURE skew comparison: it compares a backup
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
// tier (liveDeps) gathers everything host-derived — the seam-sourced image
// digests (inference.BackendFor(cfg.Backend).Image() / orchestrate.OpenWebUIImage()
// — NEVER a literal), the accessor-sourced store schema versions
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
	// (never a re-typed literal).
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

	// ExcludedModels are the identities of the excluded model weights,
	// recorded in the manifest for re-pull. Identity only.
	ExcludedModels []ExcludedModel

	// CrushConfigPath is the resolved source path for the OPTIONAL Phase-28
	// crush.json archive entry: the RENDERED coding-agent config.
	// The cmd tier sets it ONLY on an agent-on backup (crushConfigPath()); empty
	// means agent off → the entry is never offered to the core (archive stays
	// v2-layout-identical). An absent file at a non-empty path is skipped via
	// FileMissing exactly like the other optional entries.
	CrushConfigPath string
	// AgentBinarySHA256 / AgentVersion / AgentPinSHA256 are the IDENTITY of the
	// EXCLUDED coding-agent binary, supplied by the cmd tier
	// (hashFileSHA256(agentBinPath()) + the pinned policy version + the policy's
	// pinned binary SHA-256). Identity only — the binary bytes are NEVER archived.
	// Set ONLY on an agent-on backup; when all three are empty the manifest records
	// no ExcludedAgent.
	AgentBinarySHA256 string
	AgentVersion      string
	AgentPinSHA256    string

	// QdrantVolumeName / TempQdrantTar drive the OPTIONAL Phase-23 qdrant volume
	// export: when BOTH are non-empty, Backup quiesces Deps.QdrantServiceName
	// around VolumeExport(QdrantVolumeName, TempQdrantTar) and appends the
	// qdrant-volume.tar entry. The cmd tier gates them on cfg.MemoryEnabled AND a
	// fail-soft `podman volume exists` check; empty means memory off / volume
	// absent — ZERO qdrant Deps calls, archive identical to the v1 layout.
	// QdrantVolumeName is seam-sourced (orchestrate.QdrantVolumeName()) — never a
	// literal.
	QdrantVolumeName string
	TempQdrantTar    string
	// RecallStatePath is the resolved recall-state.json source path (the OPTIONAL
	// recall-state.json entry,; recall.RecallStatePath at the cmd tier). An
	// absent file is skipped via FileMissing like the other optional entries. The
	// cmd tier gates it on cfg.MemoryEnabled (review), mirroring the qdrant
	// pair: empty means memory off, so a memory-off archive stays v1-identical
	// even when a leftover recall-state.json exists on disk — otherwise the entry
	// would ship without a manifest recall_schema_version and escape the
	// fail-closed schema gate on restore.
	RecallStatePath string

	// EmbeddingModel / EmbeddingDim / RecallSchemaVersion are the Phase-23 manifest
	// fields: config-sourced embedding identity + dimension and the
	// accessor-sourced recall store schema version. The cmd tier sets them ONLY on
	// a memory-on backup; zero values are omitted from the manifest ("not
	// recorded" — never a fabricated claim).
	EmbeddingModel      string
	EmbeddingDim        int
	RecallSchemaVersion int

	// SearxngSettingsPath is the resolved source path for the OPTIONAL Phase-34
	// web-search settings.yml archive entry: the RENDERED SearXNG
	// provenance ($XDG_CONFIG_HOME/villa/searxng/settings.yml). The cmd tier sets it
	// ONLY on a web-search-on backup (gated on cfg.WebSearchEnabled); empty means web
	// search off, so the entry is never offered to the core and the archive stays
	// v3-layout-identical. An absent file at a non-empty path is skipped via
	// FileMissing exactly like the other optional entries. Fetched EPHEMERAL web
	// content is NEVER a source here — only this CONFIG provenance crosses.
	SearxngSettingsPath string

	// FileMissing classifies a ReadFile error as a tolerable absent-file (skip the
	// entry) vs a hard error. The cmd layer wires os.IsNotExist; the pure core stays
	// free of os. When nil, any ReadFile error is treated as hard.
	FileMissing func(error) bool
}

// Backup is the PURE backup orchestrator over the injected Deps. It
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
	// service is restored even if a later step errors. The restart stays
	// best-effort (it NEVER fails the backup), but a failed restart is now SURFACED
	// via retRes.RestartWarning so the cmd tier can warn the user to run `villa up`
	// The named return retRes is what every `return` below populates, so the
	// defer (which runs ONLY after a successful Stop) annotates whichever Result is
	// actually returned — without ever turning a successful backup into a failure.
	if err := d.Stop(d.OpenWebUIServiceName); err != nil {
		return Result{Err: fmt.Errorf("backup: stop %s: %w", d.OpenWebUIServiceName, err), FailedStep: "stop"},
			fmt.Errorf("backup: stop %s: %w", d.OpenWebUIServiceName, err)
	}
	// foldRestartWarning APPENDS a failed best-effort restart message to the named
	// return's RestartWarning so BOTH deferred restarts (OWUI + qdrant) surface
	// instead of the last defer clobbering the first.
	foldRestartWarning := func(service string, serr error) {
		warn := fmt.Sprintf("backup written, but failed to restart %s (%v) — run `villa up`", service, serr)
		if retRes.RestartWarning != "" {
			retRes.RestartWarning += "; " + warn
			return
		}
		retRes.RestartWarning = warn
	}
	defer func() {
		if serr := d.Start(d.OpenWebUIServiceName); serr != nil {
			foldRestartWarning(d.OpenWebUIServiceName, serr)
		}
	}()

	// (2) Export ONLY the Open WebUI volume (model weights excluded).
	if err := d.VolumeExport(in.OpenWebUIVolumeName, in.TempVolumeTar); err != nil {
		return Result{Err: fmt.Errorf("backup: volume export %s: %w", in.OpenWebUIVolumeName, err), FailedStep: "volume"},
			fmt.Errorf("backup: volume export %s: %w", in.OpenWebUIVolumeName, err)
	}

	// (2b) OPTIONAL qdrant volume export (Phase 23). Gated on BOTH
	// QdrantVolumeName and TempQdrantTar being non-empty (memory on AND volume
	// present — decided by the cmd tier; empty ⇒ ZERO qdrant Deps calls). Clone of
	// the OWUI quiesce frame: Stop the qdrant service so the export never copies a
	// live RocksDB/WAL mid-write (Pitfall 3 torn-snapshot guard), export, and DEFER
	// a best-effort Start folding into RestartWarning — a failed restart NEVER
	// fails the backup.
	if in.QdrantVolumeName != "" && in.TempQdrantTar != "" {
		if err := d.Stop(d.QdrantServiceName); err != nil {
			return Result{Err: fmt.Errorf("backup: stop %s: %w", d.QdrantServiceName, err), FailedStep: "stop"},
				fmt.Errorf("backup: stop %s: %w", d.QdrantServiceName, err)
		}
		defer func() {
			if serr := d.Start(d.QdrantServiceName); serr != nil {
				foldRestartWarning(d.QdrantServiceName, serr)
			}
		}()
		if err := d.VolumeExport(in.QdrantVolumeName, in.TempQdrantTar); err != nil {
			return Result{Err: fmt.Errorf("backup: volume export %s: %w", in.QdrantVolumeName, err), FailedStep: "volume"},
				fmt.Errorf("backup: volume export %s: %w", in.QdrantVolumeName, err)
		}
	}

	// (3) Read entries. The OWUI volume tar is REQUIRED; config/usage/bench are read
	// from their resolved paths, an absent optional data-dir file being skipped. The
	// two Phase-23 memory entries are OPTIONAL: an empty path (memory off / volume
	// absent — the cmd tier gates by passing "") skips the row entirely.
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
		{EntryQdrantVolume, in.TempQdrantTar, false},
		{EntryRecallState, in.RecallStatePath, false},
		// The OPTIONAL Phase-28 coding-agent config: present only on
		// an agent-on backup (the cmd tier passes CrushConfigPath=""  when the agent
		// is off, skipping the row). An absent file at a non-empty path is tolerated
		// via FileMissing like the other optional entries — agent-on but no rendered
		// crush.json on disk skips the entry rather than failing the backup. The
		// agent BINARY is NEVER an entry — only its identity (ExcludedAgent) below.
		{EntryCrushConfig, in.CrushConfigPath, false},
		// The OPTIONAL Phase-34 web-search settings.yml provenance:
		// present only on a web-search-on backup (the cmd tier passes
		// SearxngSettingsPath="" when web search is off, skipping the row). An absent
		// file at a non-empty path is tolerated via FileMissing exactly like the
		// other optional entries. NO entry is ever added for fetched/ephemeral web
		// content — only this rendered CONFIG provenance crosses.
		{EntrySearxngSettings, in.SearxngSettingsPath, false},
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
		// streaming path: the two VOLUME TAR entries are the only members
		// that realistically grow to many GiB (a populated Qdrant store on a
		// memory-tight host). When the OpenFile seam is wired, checksum them via
		// a streaming io.Copy pass and register a streaming archiveEntry that
		// tar-copies from a fresh reader at assembly — the tar bytes never sit
		// whole in memory. The seam is OPTIONAL: nil (existing fakes) and the
		// small data-dir entries keep the ReadFile path below.
		if d.OpenFile != nil && (s.entry == EntryOpenWebUIVolume || s.entry == EntryQdrantVolume) {
			srcPath := s.path
			rc, size, err := d.OpenFile(srcPath)
			if err != nil {
				if !s.required && in.FileMissing != nil && in.FileMissing(err) {
					continue // tolerable absent optional entry (mirrors the ReadFile row)
				}
				rerr := fmt.Errorf("backup: open %s (%s): %w", s.entry, srcPath, err)
				return Result{Err: rerr, FailedStep: "read"}, rerr
			}
			csum, sumErr := sum(rc)
			closeErr := rc.Close()
			if sumErr != nil {
				rerr := fmt.Errorf("backup: checksum %s (%s): %w", s.entry, srcPath, sumErr)
				return Result{Err: rerr, FailedStep: "checksum"}, rerr
			}
			if closeErr != nil {
				rerr := fmt.Errorf("backup: close %s (%s): %w", s.entry, srcPath, closeErr)
				return Result{Err: rerr, FailedStep: "checksum"}, rerr
			}
			entries = append(entries, archiveEntry{
				name: s.entry,
				size: size,
				open: func() (io.ReadCloser, error) {
					r, _, oerr := d.OpenFile(srcPath)
					return r, oerr
				},
			})
			checksums = append(checksums, EntryChecksum{Name: s.entry, SHA256: csum})
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

	// Record the EXCLUDED coding-agent binary IDENTITY, agent-on
	// ONLY (clone of the ExcludedModels weights exclusion). It is gated on the cmd
	// tier having supplied an identity (any of the three fields non-empty); an
	// agent-off backup leaves all three empty so excludedAgent stays nil and the
	// manifest omits the key — keeping the archive v2-layout-identical. The binary
	// bytes are NEVER added to the archive (there is no EntryCrushBinary).
	var excludedAgent *ExcludedAgent
	if in.AgentBinarySHA256 != "" || in.AgentVersion != "" || in.AgentPinSHA256 != "" {
		excludedAgent = &ExcludedAgent{
			SHA256:    in.AgentBinarySHA256,
			Version:   in.AgentVersion,
			PinSHA256: in.AgentPinSHA256,
		}
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
		EmbeddingModel:      in.EmbeddingModel,
		EmbeddingDim:        in.EmbeddingDim,
		RecallSchemaVersion: in.RecallSchemaVersion,
		ExcludedAgent:       excludedAgent,
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
// Manifest is compared against. The cmd tier gathers these: the current
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
	// EmbeddingModel / EmbeddingDim are the CURRENT install's embedding identity
	// (cfg.EmbeddingModel / cfg.EmbeddingDim — config is the single source of
	// truth) for the Phase-23 dimension-skew compare. Plain values so this
	// core stays free of config-field coupling beyond the caller's snapshot.
	EmbeddingModel string
	EmbeddingDim   int
	// RecallSchemaVersion is the CURRENT recall store schema version
	// (recall.SchemaVersion(), accessor-sourced at the cmd tier — this core
	// imports no recall, mirroring the usage/bench plain-int convention).
	RecallSchemaVersion int
	// ChecksumFailed is set by the caller when a per-entry SHA-256 verify failed
	// (archive corruption) — CompareSkew turns it into a fail-closed BLOCK.
	ChecksumFailed bool
}

// SkewWarning is one WARN-and-confirm finding: the field that differs, a
// human-readable detail, and named remediation text. It does NOT block
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
// current install cur (pure), per the RESEARCH §Skew Detection
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

	// --- fail-closed BLOCK checks ------------------------------------
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
	if blocked, reason := blockOnNewerStore("recall", m.RecallSchemaVersion, cur.RecallSchemaVersion); blocked {
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
	// Embedding model/dimension skew (Phase 23): a CONFIDENT mismatch between
	// the manifest-recorded embedding identity and the current install means the
	// backup's vectors were embedded under a different model/dimension — retrieval
	// is silently corrupt after restore until a re-index. Exactly ONE warning for
	// the model+dim pair, guarded on m.EmbeddingModel != "": an old/memory-off
	// backup never recorded one, and "not recorded" must raise NO false alarm (the
	// typed-Unknown convention, mirroring blockOnNewerStore's <=0 rule). Never
	// silent, never an auto-reindex — WARN-and-confirm only.
	if m.EmbeddingModel != "" && (m.EmbeddingModel != cur.EmbeddingModel || m.EmbeddingDim != cur.EmbeddingDim) {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field: "embedding",
			Detail: fmt.Sprintf("backup vectors were embedded with %s (dim %d); this install is configured for %s (dim %d)",
				m.EmbeddingModel, m.EmbeddingDim, cur.EmbeddingModel, cur.EmbeddingDim),
			Remediation: "restored vectors will not match the current embedder — retrieval stays corrupt until a re-index: " +
				"run `villa recall index --rebuild` after restore, or align embedding_model/embedding_dim in config.toml " +
				"with the backup before restoring",
		})
	}
	warnOnOlderStore(&v, "config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion)
	warnOnOlderStore(&v, "usage", m.UsageSchemaVersion, cur.UsageSchemaVersion)
	warnOnOlderStore(&v, "bench", m.BenchSchemaVersion, cur.BenchSchemaVersion)
	warnOnOlderStore(&v, "recall", m.RecallSchemaVersion, cur.RecallSchemaVersion)

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
