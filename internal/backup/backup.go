package backup

// backup.go holds the PURE backup orchestrator (Backup) and the PURE skew
// comparison (CompareSkew): it compares a backup
// Manifest against the CURRENT install and classifies each difference as either a
// WARN-and-confirm finding (legitimate skew that does NOT block — e.g. a newer
// villa restoring an older backup) or a fail-closed BLOCK (corruption /
// incompatible-future schema that cannot be safely applied). No host I/O — the
// caller supplies the current-install facts as plain data (CurrentInstall) and
// the recomputed checksum verdict as a flag.
//
// Which entries an archive holds is the registry's business (registry.go,
// ADR-0020): Backup walks it in tar order, and the cmd tier names the source path
// of each entry in Input.Sources.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Input is the plain-data drive for the pure Backup orchestrator. The cmd
// tier (liveDeps) gathers everything host-derived — the seam-sourced image
// digests (inference.BackendFor(cfg.Backend).Image() / orchestrate.OpenWebUIImage()
// — NEVER a literal), the accessor-sourced store schema versions
// (usage.SchemaVersion() / benchstore.SavedReportSchemaVersion()), the resolved
// data-dir artifact paths, the build-stamped villa version, the flattened host
// facts, and the excluded-model identities — then Backup() executes the pure
// quiesce→export→assemble→restart ordering over the injected Deps. Backup imports
// NEITHER inference NOR detect NOR any image literal, so TestSeamGrepGate stays
// green.
type Input struct {
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

	// Sources maps an entry name (the Entry* constants) to the resolved source path
	// of that entry: config.toml, usage.json, bench-reports.jsonl and, only when its
	// subsystem is on, recall-state.json (memory), crush.json (coding agent) and
	// searxng-settings.yml (web search). The cmd tier's liveBackupSources owns those
	// gates. A missing or empty path means the entry is never offered, so an
	// archive made with a subsystem off stays layout-identical to one made before
	// the entry existed — otherwise an orphan file left by a previously-enabled
	// subsystem would ship without the manifest field that gates it on restore. An
	// absent file at a non-empty path is skipped via FileMissing. The two volume
	// tars are NOT here: their staging paths are TempVolumeTar / TempQdrantTar.
	Sources map[string]string

	// ExcludedModels are the identities of the excluded model weights,
	// recorded in the manifest for re-pull. Identity only.
	ExcludedModels []ExcludedModel

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

	// EmbeddingModel / EmbeddingDim / RecallSchemaVersion are the Phase-23 manifest
	// fields: config-sourced embedding identity + dimension and the
	// accessor-sourced recall store schema version. The cmd tier sets them ONLY on
	// a memory-on backup; zero values are omitted from the manifest ("not
	// recorded" — never a fabricated claim).
	EmbeddingModel      string
	EmbeddingDim        int
	RecallSchemaVersion int

	// FileMissing classifies a ReadFile error as a tolerable absent-file (skip the
	// entry) vs a hard error. The cmd layer wires os.IsNotExist; the pure core stays
	// free of os. When nil, any ReadFile error is treated as hard.
	FileMissing func(error) bool
}

// sourceFor is the resolved source path of a registry row: the staged export tar
// for a volume, the cmd tier's named path for everything else. "" means the entry
// is not offered.
func (in Input) sourceFor(r Row) string {
	switch r.Name {
	case EntryOpenWebUIVolume:
		return in.TempVolumeTar
	case EntryQdrantVolume:
		return in.TempQdrantTar
	}
	return in.Sources[r.Name]
}

// tolerates reports whether err on an OPTIONAL row is a tolerable absent file.
func (in Input) tolerates(r Row, err error) bool {
	return !r.Required && in.FileMissing != nil && in.FileMissing(err)
}

// excludedAgent is the EXCLUDED coding-agent binary identity, agent-on ONLY (clone
// of the ExcludedModels weights exclusion). It is nil unless the cmd tier supplied
// an identity (any of the three fields non-empty), so an agent-off backup's
// manifest omits the key. The binary bytes are NEVER added to the archive (there
// is no EntryCrushBinary).
func (in Input) excludedAgent() *ExcludedAgent {
	if in.AgentBinarySHA256 == "" && in.AgentVersion == "" && in.AgentPinSHA256 == "" {
		return nil
	}
	return &ExcludedAgent{SHA256: in.AgentBinarySHA256, Version: in.AgentVersion, PinSHA256: in.AgentPinSHA256}
}

// manifestInput is the seam/accessor-sourced manifest drive for the entries read.
func (in Input) manifestInput(sums []EntryChecksum) ManifestInput {
	return ManifestInput{
		CreatedAt:           in.CreatedAt,
		VillaVersion:        in.VillaVersion,
		Host:                in.Host,
		InferenceImage:      in.InferenceImage,
		OpenWebUIImage:      in.OpenWebUIImage,
		ConfigSchemaVersion: in.ConfigSchemaVersion,
		UsageSchemaVersion:  in.UsageSchemaVersion,
		BenchSchemaVersion:  in.BenchSchemaVersion,
		Entries:             sums,
		ExcludedModels:      in.ExcludedModels,
		EmbeddingModel:      in.EmbeddingModel,
		EmbeddingDim:        in.EmbeddingDim,
		RecallSchemaVersion: in.RecallSchemaVersion,
		ExcludedAgent:       in.excludedAgent(),
	}
}

// stepError tags an error with the Result.FailedStep it belongs to. It exists so
// the small step helpers below can return a plain error and Backup can still name
// the step.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

func stepFail(step string, err error) error { return &stepError{step: step, err: err} }

// failResult is the failed-Backup Result and error for err. An untagged error is a
// "write" failure.
func failResult(err error) (Result, error) {
	step := "write"
	var se *stepError
	if errors.As(err, &se) {
		step, err = se.step, se.err
	}
	return Result{Err: err, FailedStep: step}, err
}

// quiesce stops a service before its volume is exported and remembers it, so
// restart can bring every one back — even when a later step fails. The restart is
// best-effort: it NEVER turns a successful backup into a failure, but a failed
// restart is SURFACED through Result.RestartWarning so the cmd tier can tell the
// user to run `villa up`.
type quiesce struct {
	d       Deps
	stopped []string
}

// exportVolume stops service (clean SQLite copy / no live RocksDB WAL mid-write —
// Pitfall 3 torn-snapshot guard) and exports volume to out.
func (q *quiesce) exportVolume(service, volume, out string) error {
	if err := q.d.Stop(service); err != nil {
		return stepFail("stop", fmt.Errorf("backup: stop %s: %w", service, err))
	}
	q.stopped = append(q.stopped, service)
	if err := q.d.VolumeExport(volume, out); err != nil {
		return stepFail("volume", fmt.Errorf("backup: volume export %s: %w", volume, err))
	}
	return nil
}

// restart starts every stopped service, last stopped first, and returns the
// failures folded into one warning ("" when every start succeeded).
func (q *quiesce) restart() string {
	var warns []string
	for i := len(q.stopped) - 1; i >= 0; i-- {
		if err := q.d.Start(q.stopped[i]); err != nil {
			warns = append(warns, fmt.Sprintf("backup written, but failed to restart %s (%v) — run `villa up`", q.stopped[i], err))
		}
	}
	return strings.Join(warns, "; ")
}

// Backup is the PURE backup orchestrator over the injected Deps. It
// executes the quiesce ordering RESEARCH §OWUI Quiesce mandates and assembles the
// single plain .tar:
//
//  1. Stop the Open WebUI service (clean SQLite copy) and restart it when Backup
//     returns, even on a mid-backup error (best-effort).
//  2. podman volume export the Open WebUI data volume to the temp tar (Deps seam),
//     and the qdrant volume likewise when the caller asked for it.
//  3. Read each registry entry the caller offered (the exported volume tars,
//     config.toml, and the file entries); an absent optional file is skipped.
//  4. Compute a lowercase-hex SHA-256 per entry.
//  5. BuildManifest with the seam-sourced digests + accessor-sourced store schema
//     versions + excluded-model identities injected.
//  6. writeArchive (manifest.json FIRST) to the 0600 OutputWriter the caller opened.
//
// The villa-models volume is NEVER exported. Backup runs no subprocess (links the
// exec package NOT at all) and carries no image literal — every effect is a Deps
// func field.
func Backup(d Deps, in Input) (res Result, err error) {
	if in.OutputWriter == nil {
		return failResult(fmt.Errorf("backup: nil output writer"))
	}
	q := &quiesce{d: d}
	defer func() { res.RestartWarning = q.restart() }()
	return backupRun(d, in, q)
}

// backupRun is steps (2) to (6) of Backup, over a quiesce the caller restarts.
func backupRun(d Deps, in Input, q *quiesce) (Result, error) {
	if err := exportVolumes(d, in, q); err != nil {
		return failResult(err)
	}
	entries, sums, err := readSources(d, in)
	if err != nil {
		return failResult(err)
	}
	if err := assembleArchive(in, entries, sums); err != nil {
		return failResult(err)
	}
	return Result{Reason: fmt.Sprintf("backup written to %s", in.OutputPath)}, nil
}

// assembleArchive builds the seam/accessor-sourced manifest and writes the archive:
// manifest.json FIRST, then the data entries in registry order.
func assembleArchive(in Input, entries []archiveEntry, sums []EntryChecksum) error {
	manifestJSON, err := marshalManifest(BuildManifest(in.manifestInput(sums)))
	if err != nil {
		return err
	}
	all := append([]archiveEntry{{name: EntryManifest, data: manifestJSON}}, entries...)
	return writeArchive(in.OutputWriter, all)
}

// exportVolumes exports the Open WebUI volume (model weights excluded), then the
// OPTIONAL qdrant volume. The qdrant export is gated on BOTH QdrantVolumeName and
// TempQdrantTar being non-empty (memory on AND volume present — decided by the cmd
// tier); empty means ZERO qdrant Deps calls.
func exportVolumes(d Deps, in Input, q *quiesce) error {
	if err := q.exportVolume(d.OpenWebUIServiceName, in.OpenWebUIVolumeName, in.TempVolumeTar); err != nil {
		return err
	}
	if in.QdrantVolumeName == "" || in.TempQdrantTar == "" {
		return nil
	}
	return q.exportVolume(d.QdrantServiceName, in.QdrantVolumeName, in.TempQdrantTar)
}

// source is one entry read for the archive. A zero source (no entry name) is an
// optional entry that was not offered or is tolerably absent.
type source struct {
	entry archiveEntry
	sum   EntryChecksum
}

func (s source) present() bool { return s.entry.name != "" }

// readSources reads every registry entry in tar order, with its SHA-256.
func readSources(d Deps, in Input) ([]archiveEntry, []EntryChecksum, error) {
	var entries []archiveEntry
	var sums []EntryChecksum
	for _, row := range registry {
		s, err := readSource(d, in, row)
		if err != nil {
			return nil, nil, err
		}
		if s.present() {
			entries = append(entries, s.entry)
			sums = append(sums, s.sum)
		}
	}
	return entries, sums, nil
}

// readSource reads one registry row. The VOLUME TARS are the only members that
// realistically grow to many GiB (a populated Qdrant store on a memory-tight
// host), so when the OpenFile seam is wired they are checksummed in a streaming
// pass and tar-copied from a fresh reader at assembly — they never sit whole in
// memory. The seam is OPTIONAL: nil (existing fakes) and the small data-dir
// entries keep the ReadFile path.
func readSource(d Deps, in Input, row Row) (source, error) {
	path := in.sourceFor(row)
	switch {
	case path == "":
		return absentSource(row)
	case d.OpenFile != nil && row.Kind == KindVolume:
		return streamSource(d, in, row, path)
	}
	return bufferSource(d, in, row, path)
}

// absentSource is a row with no source path: fatal when required, else skipped.
func absentSource(row Row) (source, error) {
	if row.Required {
		return source{}, stepFail("read", fmt.Errorf("backup: missing required source path for %s", row.Name))
	}
	return source{}, nil
}

// streamSource checksums a volume tar by streaming it, and registers a streaming
// archive entry that re-opens it at assembly.
func streamSource(d Deps, in Input, row Row, path string) (source, error) {
	rc, size, err := d.OpenFile(path)
	if err != nil {
		if in.tolerates(row, err) {
			return source{}, nil // tolerable absent optional entry (mirrors the ReadFile row)
		}
		return source{}, stepFail("read", fmt.Errorf("backup: open %s (%s): %w", row.Name, path, err))
	}
	csum, err := checksumStream(rc, row.Name, path)
	if err != nil {
		return source{}, err
	}
	reopen := func() (io.ReadCloser, error) {
		r, _, oerr := d.OpenFile(path)
		return r, oerr
	}
	return source{
		entry: archiveEntry{name: row.Name, size: size, open: reopen},
		sum:   EntryChecksum{Name: row.Name, SHA256: csum},
	}, nil
}

// checksumStream SHA-256s rc and closes it; a close failure is a checksum failure
// too, because the stream may not have been fully read.
func checksumStream(rc io.ReadCloser, name, path string) (string, error) {
	csum, sumErr := sum(rc)
	closeErr := rc.Close()
	if sumErr != nil {
		return "", stepFail("checksum", fmt.Errorf("backup: checksum %s (%s): %w", name, path, sumErr))
	}
	if closeErr != nil {
		return "", stepFail("checksum", fmt.Errorf("backup: close %s (%s): %w", name, path, closeErr))
	}
	return csum, nil
}

// bufferSource reads a small entry whole and SHA-256s it.
func bufferSource(d Deps, in Input, row Row, path string) (source, error) {
	data, err := d.ReadFile(path)
	if err != nil {
		if in.tolerates(row, err) {
			return source{}, nil // tolerable absent data-dir artifact
		}
		return source{}, stepFail("read", fmt.Errorf("backup: read %s (%s): %w", row.Name, path, err))
	}
	csum, err := sum(bytes.NewReader(data))
	if err != nil {
		return source{}, stepFail("checksum", err)
	}
	return source{
		entry: archiveEntry{name: row.Name, data: data},
		sum:   EntryChecksum{Name: row.Name, SHA256: csum},
	}, nil
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
//	  - any store schema version in the manifest NEWER than the current value
//	    (future schema can't be safely applied — mirrors usage.Load's
//	    fail-closed-on-future)
//
//	WARN-and-confirm (legitimate skew, does NOT block):
//	  - villa version mismatch
//	  - inference / OWUI image digest mismatch (re-pull remediation)
//	  - host fingerprint mismatch (cross-host caveat)
//	  - embedding model/dimension mismatch
//	  - any store schema version OLDER in the manifest than current
//
// A fully-matching manifest returns the zero SkewVerdict (no Block, no Warnings).
func CompareSkew(m Manifest, cur CurrentInstall) SkewVerdict {
	if reason := blockReason(m, cur); reason != "" {
		return SkewVerdict{Block: true, BlockReason: reason}
	}
	var v SkewVerdict
	warnOnVillaVersion(&v, m, cur)
	warnOnImages(&v, m, cur)
	warnOnHost(&v, m, cur)
	warnOnEmbedding(&v, m, cur)
	for _, s := range storeVersions(m, cur) {
		warnOnOlderStore(&v, s.name, s.manifest, s.current)
	}
	return v
}

// storeVersion is one store's schema version in the manifest and in the current
// install. The store names are the prefixes of the manifest's *_schema_version
// fields.
type storeVersion struct {
	name              string
	manifest, current int
}

// storeVersions lists every store schema the compare covers, in report order.
func storeVersions(m Manifest, cur CurrentInstall) []storeVersion {
	return []storeVersion{
		{"config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion},
		{"usage", m.UsageSchemaVersion, cur.UsageSchemaVersion},
		{"bench", m.BenchSchemaVersion, cur.BenchSchemaVersion},
		{"recall", m.RecallSchemaVersion, cur.RecallSchemaVersion},
	}
}

// blockReason is the first fail-closed BLOCK reason, or "" when nothing blocks.
func blockReason(m Manifest, cur CurrentInstall) string {
	if reason := manifestBlock(m, cur); reason != "" {
		return reason
	}
	return storeBlock(m, cur)
}

// manifestBlock is the BLOCK reason for corruption or an unreadable/future manifest.
func manifestBlock(m Manifest, cur CurrentInstall) string {
	if cur.ChecksumFailed {
		return "archive integrity check failed (SHA-256 mismatch) — refusing to restore a corrupt backup"
	}
	if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
		return fmt.Sprintf(
			"manifest schema_version %d is unreadable or newer than this villa supports (%d) — cannot safely restore an incompatible manifest",
			m.SchemaVersion, backupSchemaVersion)
	}
	return ""
}

// storeBlock is the BLOCK reason for the first store schema newer than current.
func storeBlock(m Manifest, cur CurrentInstall) string {
	for _, s := range storeVersions(m, cur) {
		if blocked, reason := blockOnNewerStore(s.name, s.manifest, s.current); blocked {
			return reason
		}
	}
	return ""
}

func warnOnVillaVersion(v *SkewVerdict, m Manifest, cur CurrentInstall) {
	if m.VillaVersion != cur.VillaVersion {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "villa_version",
			Detail:      fmt.Sprintf("backup was made by villa %q; this is villa %q", m.VillaVersion, cur.VillaVersion),
			Remediation: "version skew is usually fine; confirm to proceed, or rebuild/reinstall the matching villa version if a behaviour change is suspected",
		})
	}
}

func warnOnImages(v *SkewVerdict, m Manifest, cur CurrentInstall) {
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
}

func warnOnHost(v *SkewVerdict, m Manifest, cur CurrentInstall) {
	if m.Host != cur.Host {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "host",
			Detail:      fmt.Sprintf("backup host %+v differs from current %+v", m.Host, cur.Host),
			Remediation: "backed up on a different host — if Open WebUI cannot read its data after restore, run `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` and ensure the :Z relabel",
		})
	}
}

// warnOnEmbedding: a CONFIDENT mismatch between the manifest-recorded embedding
// identity and the current install means the backup's vectors were embedded under
// a different model/dimension — retrieval is silently corrupt after restore until
// a re-index. Exactly ONE warning for the model+dim pair, guarded on
// m.EmbeddingModel != "": an old/memory-off backup never recorded one, and "not
// recorded" must raise NO false alarm (the typed-Unknown convention, mirroring
// blockOnNewerStore's <=0 rule). Never silent, never an auto-reindex —
// WARN-and-confirm only.
func warnOnEmbedding(v *SkewVerdict, m Manifest, cur CurrentInstall) {
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
