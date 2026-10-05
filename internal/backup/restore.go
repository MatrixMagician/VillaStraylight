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
// The file entries (usage, bench, recall, crush, searxng settings, ...) are the
// registry's KindFile rows (registry.go, ADR-0020): capture, forward write and
// rollback each loop over them. The two volumes and config.toml are explicit code
// below, because their quiesce, tri-state refusal and clean-recreate are different
// in kind.
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

	// Dests maps a file entry's name (the Entry* constants of the registry's KindFile
	// rows) to the resolved path it is restored to. The cmd tier's liveRestoreDests
	// owns the gates: usage.json and bench-reports.jsonl are always wired,
	// recall-state.json only when memory is on, crush.json only when the coding agent
	// is on, and searxng-settings.yml only when web search is on. A missing or empty
	// destination means the entry is NOT applied: an archive that carries it reports
	// it as Skipped, never a false "restored", and a restore onto an install with the
	// subsystem off makes ZERO writes for it. config.toml has no destination here — it
	// goes through Deps.SaveConfig.
	//
	// Every destination is written through the one Deps.WriteFile seam (the live
	// wiring picks the containment guard by path) and captured through Deps.ReadFile
	// for the rollback set. crush.json and the SearXNG settings live OUTSIDE the villa
	// data-store root; the latter holds the rendered SEARXNG_SECRET, so its write is
	// 0600-preserving and NEVER widens the mode.
	Dests map[string]string

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

	// EvalKeysOf names the baselines an eval-baselines.json document holds, one label
	// per baseline (the cmd tier parses it with evalstore, so this package imports
	// nothing from eval). A document it cannot read for keys (corrupt, or a newer
	// schema) is an error saying why, never an empty list: that would pass for a store
	// with no baselines. Restore replaces the document verbatim, so it applies
	// EvalKeysOf to the archive's document and to the file it captured before
	// mutating, and reports every captured baseline the archive lacks in
	// Result.EvalDropped; a current file it cannot read is asked about at the skew
	// confirmation instead (#281). Nil disables both.
	EvalKeysOf func(doc []byte) ([]string, error)
}

// restoreTxn is one Restore's state: the verified archive, the prior state captured
// before any mutation, and the config being restored. Its methods are the steps of
// the transaction, small enough that each is its own function.
type restoreTxn struct {
	d  RestoreDeps
	in RestoreInput
	ex extracted
	// priorCfg is the snapshot of the current config; restoredCfg is the archive's.
	priorCfg, restoredCfg config.VillaConfig
	// prior holds the captured bytes of each file entry that EXISTS now, keyed by
	// entry name. An absent key means the file was not there to begin with.
	prior map[string][]byte
	// evalUnreadable says why the current eval-baselines.json this restore replaces
	// cannot be read for keys; empty when it can, or when nothing is replaced.
	evalUnreadable string
}

// Restore performs the guarded, transactional archive apply and returns a typed
// Result, cloning backendswap.Run's frame. Ordering (RESEARCH §Transactional
// Restore):
//
//	(1) READ+VERIFY (pure, zero side effects): open the outer tar, parse
//	    manifest.json, verify each entry's SHA-256 against the manifest. A mismatch
//	    or unreadable/incompatible manifest.schema_version → Refused.
//	(2) SKEW: CompareSkew(manifest, Current). Block → Refused. WARN-only, or a
//	    current eval-baselines.json the archive would replace but EvalKeysOf cannot
//	    read → require Consent unless Bypass; a declined gate → Refused. (All still
//	    zero side effects.)
//	(3) CAPTURE strictly BEFORE mutation: export the CURRENT owui volume + snapshot
//	    the current config + the current file entries. Uncapturable → Refused.
//	(4) QUIESCE: Stop the Open WebUI service.
//	(5) MUTATE (any error → rollback): SaveConfig(restored) → restore the file
//	    entries → CLEAN-RECREATE owui volume (VolumeRm → ReconcileAndWrite →
//	    EnsureVolume) → VolumeImport(extracted owui tar) → Start.
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
	t := &restoreTxn{d: d, in: in, ex: ex, prior: map[string][]byte{}}
	if refused := t.skewGate(); refused != nil {
		return *refused
	}
	if refused := t.capture(); refused != nil {
		return *refused
	}
	return t.apply()
}

// skewGate is step (2). A checksum failure is folded into CompareSkew via the
// ChecksumFailed flag (always false here — a real mismatch already Refused in the
// verify pass), so CompareSkew classifies schema/version/digest/host skew. Block →
// Refused; a WARN-only verdict, or an unreadable eval-baselines.json this restore
// would replace (evalWarnings), requires consent unless Bypass. nil means proceed.
func (t *restoreTxn) skewGate() *Result {
	cur := t.in.Current
	cur.ChecksumFailed = false
	skew := CompareSkew(t.ex.manifest, cur)
	if skew.Block {
		return &Result{Refused: true, FailedStep: "skew", Reason: skew.BlockReason}
	}
	if declined(t.in, append(skew.Warnings, t.evalWarnings()...)) {
		return &Result{Refused: true, FailedStep: "skew",
			Reason: "restore declined at the skew confirmation (re-run with --yes/--force to bypass)"}
	}
	return nil
}

// evalWarnings is the skew warning for a current eval-baselines.json this restore
// would replace but cannot read for keys: corrupt, or written by a newer villa
// (#281). Replacing it loses every baseline it holds with none named in
// EvalDropped, and evalstore itself refuses that overwrite, so the operator
// confirms it first. An absent file is not this case: there is nothing to lose.
// Reading the file is not a mutation, so the gate keeps zero side effects.
func (t *restoreTxn) evalWarnings() []SkewWarning {
	current, ok := t.replacedEval()
	if !ok {
		return nil
	}
	_, err := t.in.EvalKeysOf(current)
	if err == nil {
		return nil
	}
	t.evalUnreadable = err.Error()
	return []SkewWarning{{
		Field: "eval_baselines",
		Detail: fmt.Sprintf("%s is unreadable (%s) — this restore will replace it with the backup's copy, "+
			"and the eval baselines it holds cannot be named, so any the backup lacks are lost (ADR-0018)",
			t.in.Dests[EntryEvalBaselines], err),
		Remediation: "to keep it, move it aside before restoring (a newer villa can still read a newer schema); " +
			"confirm, or re-run with --yes/--force, to replace it",
	}}
}

// replacedEval is the current eval-baselines.json this restore would replace: the
// archive carries the entry, a parser and a destination are wired, and a file is
// there now.
func (t *restoreTxn) replacedEval() ([]byte, bool) {
	if _, carried := t.ex.files[EntryEvalBaselines]; !carried || t.in.EvalKeysOf == nil {
		return nil, false
	}
	return captureFile(t.d, t.in.Dests[EntryEvalBaselines])
}

// declined reports whether the operator refused the skew warnings: there are some,
// Bypass is off, and Consent is absent or said no.
func declined(in RestoreInput, ws []SkewWarning) bool {
	if len(ws) == 0 || in.Bypass {
		return false
	}
	return in.Consent == nil || !in.Consent(skewPrompt(ws))
}

// capture is step (3): the verbatim rollback set, taken STRICTLY before any
// mutation (RESEARCH Pitfall 4). An uncapturable current state must NOT be
// mutated — refuse with zero side effects. nil means every capture succeeded.
func (t *restoreTxn) capture() *Result {
	steps := []func() *Result{
		t.checkQdrantKnown, t.captureConfig, t.captureOwui, t.captureQdrant, t.captureFiles, t.parseRestored,
	}
	for _, step := range steps {
		if refused := step(); refused != nil {
			return refused
		}
	}
	return nil
}

// qdrantLive reports whether the archive carries a qdrant entry AND the current
// host has the volume: the cell where qdrant is captured, quiesced, rolled back
// and restarted. Entry-present + volume-absent records prior-absent (rollback then
// REMOVES the forward-created volume); entry-absent makes ZERO qdrant calls.
func (t *restoreTxn) qdrantLive() bool {
	return t.ex.qdrantPresent && t.in.QdrantVolumeExists
}

// checkQdrantKnown is the fail-closed gate: when the archive carries a qdrant entry
// but the current volume's existence could NOT be evaluated, REFUSE before any
// mutation. An Unknown collapsed into "absent" would skip the capture export AND
// the quiesce, then run the destructive VolumeRm on a possibly-real, uncaptured
// qdrant volume — destroying existing vectors with no rollback copy. The
// typed-Unknown doctrine: Unknown is never a confident negative.
func (t *restoreTxn) checkQdrantKnown() *Result {
	if !t.ex.qdrantPresent || !t.in.QdrantVolumeUnknown {
		return nil
	}
	return &Result{Refused: true, FailedStep: "capture",
		Reason: "could not determine whether the Qdrant volume " + t.in.QdrantVolumeName +
			" exists — an unknown current state cannot be safely captured for rollback; " +
			"check podman (`podman volume exists " + t.in.QdrantVolumeName + "`), then re-run"}
}

func refuseCapture(err error, what string) *Result {
	return &Result{Refused: true, FailedStep: "capture", Err: err, Reason: what + " — refusing to mutate: " + err.Error()}
}

func (t *restoreTxn) captureConfig() *Result {
	cfg, err := t.d.LoadConfig()
	if err != nil {
		return refuseCapture(err, "cannot snapshot the current config for rollback")
	}
	t.priorCfg = cfg
	return nil
}

func (t *restoreTxn) captureOwui() *Result {
	if err := t.d.VolumeExport(t.in.OpenWebUIVolumeName, t.in.RollbackVolumeTar); err != nil {
		return refuseCapture(err, "cannot capture the current Open WebUI volume for rollback")
	}
	return nil
}

func (t *restoreTxn) captureQdrant() *Result {
	if !t.qdrantLive() {
		return nil
	}
	if err := t.d.VolumeExport(t.in.QdrantVolumeName, t.in.RollbackQdrantTar); err != nil {
		return refuseCapture(err, "cannot capture the current Qdrant volume for rollback")
	}
	return nil
}

// captureFiles snapshots each file entry's current bytes through Deps.ReadFile. An
// absent or unreadable file is simply not captured: rollback then does not restore
// it (it was not there to begin with).
func (t *restoreTxn) captureFiles() *Result {
	for _, row := range fileRows {
		if b, ok := captureFile(t.d, t.in.Dests[row.Name]); ok {
			t.prior[row.Name] = b
		}
	}
	return nil
}

// parseRestored parses the archive's config.toml into a VillaConfig (config is the
// single source of truth — the Quadlet recreate renders from it).
func (t *restoreTxn) parseRestored() *Result {
	cfg, err := config.Parse(t.ex.config)
	if err != nil {
		return refuseCapture(err, "archive config.toml is unreadable")
	}
	t.restoredCfg = cfg
	return nil
}

// forwardStep is one step of the MUTATE phase; name is the Result.FailedStep an
// error in it reports.
type forwardStep struct {
	name string
	run  func() error
}

// apply is steps (4) to (6): quiesce, mutate, restart, then PROVE the restored
// stack offload-honestly. Switch to success ONLY on prove.StatusPass; ANY other
// verdict (incl. ready+health-200-but-residency-FAIL) rolls back verbatim —
// is-active/200 alone is NEVER success. ANY step error also rolls back, from the
// captured set.
func (t *restoreTxn) apply() Result {
	steps := []forwardStep{
		{"quiesce", t.quiesce}, {"save", t.saveConfig}, {"data", t.writeFiles}, {"volume", t.swapVolumes}, {"restart", t.restart},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			return t.rolledBack(s.name, "", err, prove.Verdict{})
		}
	}
	v := t.d.Prove(t.restoredCfg.Backend)
	if !v.Pass() {
		return t.rolledBack("prove", v.Detail, nil, v)
	}
	return t.restored(v)
}

func (t *restoreTxn) stop(service string) error {
	if err := t.d.Stop(service); err != nil {
		return fmt.Errorf("stop %s: %w", service, err)
	}
	return nil
}

func (t *restoreTxn) start(service string) error {
	if err := t.d.Start(service); err != nil {
		return fmt.Errorf("start %s: %w", service, err)
	}
	return nil
}

// quiesce stops Open WebUI for a clean volume swap, and qdrant too (Pitfall 3): a
// RUNNING qdrant holds its volume (the live VolumeRm would fail in-use) and could
// write mid-swap. The qdrant stop is gated on a prior volume actually existing — on
// a memory-off host there is no running qdrant service to stop.
func (t *restoreTxn) quiesce() error {
	if err := t.stop(t.d.OpenWebUIServiceName); err != nil {
		return err
	}
	if t.qdrantLive() {
		return t.stop(t.d.QdrantServiceName)
	}
	return nil
}

func (t *restoreTxn) saveConfig() error {
	if err := t.d.SaveConfig(t.restoredCfg); err != nil {
		return fmt.Errorf("save restored config: %w", err)
	}
	return nil
}

// willWrite reports whether a file row is actually applied: the archive carries it
// AND the cmd tier wired a destination for it.
func (t *restoreTxn) willWrite(row Row) bool {
	_, present := t.ex.files[row.Name]
	return present && t.in.Dests[row.Name] != ""
}

// writeFiles restores each file entry through the one WriteFile seam. Any error
// rolls back verbatim like the other data rows. The agent BINARY is NOT restored
// here — it is re-staged separately (re-download the pinned release; the
// ExcludedAgent identity is surfaced on the Result for that fail-closed re-stage).
func (t *restoreTxn) writeFiles() error {
	for _, row := range fileRows {
		if !t.willWrite(row) {
			continue
		}
		if err := t.writeRow(row, t.ex.files[row.Name]); err != nil {
			return fmt.Errorf("restore %s: %w", row.Label, err)
		}
	}
	return nil
}

// writeRow writes data to a file row's destination. A nil seam is a
// restore-incomplete condition surfaced honestly (mirrors rollbackRemove's
// nil-seam contract) rather than a silent skip; the message names the artifact the
// operator is missing.
func (t *restoreTxn) writeRow(row Row, data []byte) error {
	path := t.in.Dests[row.Name]
	if t.d.WriteFile == nil {
		return fmt.Errorf("no WriteFile seam wired — cannot restore %s to %q", row.Label, path)
	}
	return t.d.WriteFile(path, data)
}

// swapVolumes CLEAN-RECREATES then imports the RESTORED owui volume (the whole
// reason for the rm→recreate→ensure→import ordering — never merge into a live
// volume), then the qdrant volume through the SAME ordering when the archive
// carries it. VolumeRm tolerates an absent prior volume (the seam contract), so the
// prior-absent cell flows through the same sequence.
func (t *restoreTxn) swapVolumes() error {
	if err := t.importVolume("owui", t.in.OpenWebUIVolumeName, t.in.TempVolumeTar, t.ex.owuiVolume); err != nil {
		return err
	}
	if !t.ex.qdrantPresent {
		return nil
	}
	return t.importVolume("qdrant", t.in.QdrantVolumeName, t.in.TempQdrantTar, t.ex.qdrantVolume)
}

func (t *restoreTxn) importVolume(kind, volume, stagePath string, data []byte) error {
	if err := t.d.WriteFile(stagePath, data); err != nil {
		return fmt.Errorf("stage restored %s volume tar: %w", kind, err)
	}
	return t.cleanRecreateThenImport(t.restoredCfg, volume, stagePath)
}

// cleanRecreateThenImport is the load-bearing clean-recreate-before-import
// sequence (RESEARCH Pitfall 1/2), used on BOTH the forward apply and the
// rollback, for BOTH volumes: VolumeRm (not-found-tolerant) → ReconcileAndWrite
// (Quadlet recreate from cfg) → EnsureVolume (explicit create) → VolumeImport.
// import MERGES + does NOT auto-create, so the volume MUST be rm'd + freshly
// created first. When both volumes restore, ReconcileAndWrite runs once per call —
// the second invocation is an idempotent no-op by construction (Reconcile is a pure
// content-hash compare; WriteUnits writes only Changed), tolerated rather than
// restructured.
func (t *restoreTxn) cleanRecreateThenImport(cfg config.VillaConfig, volumeName, srcTar string) error {
	if err := t.d.VolumeRm(volumeName); err != nil {
		return fmt.Errorf("volume rm %s: %w", volumeName, err)
	}
	if _, err := t.d.ReconcileAndWrite(cfg); err != nil {
		return fmt.Errorf("reconcile/recreate units: %w", err)
	}
	if err := t.d.EnsureVolume(volumeName); err != nil {
		return fmt.Errorf("ensure volume %s: %w", volumeName, err)
	}
	if err := t.d.VolumeImport(volumeName, srcTar); err != nil {
		return fmt.Errorf("volume import %s: %w", volumeName, err)
	}
	return nil
}

// restart starts Open WebUI, then the qdrant service we quiesced (symmetric with
// its Stop gate). On the prior-absent cell nothing was stopped — the operator
// brings the (possibly newly-rendered) memory stack up via `villa up`, reported
// honestly by the caller.
func (t *restoreTxn) restart() error {
	if err := t.start(t.d.OpenWebUIServiceName); err != nil {
		return err
	}
	if t.qdrantLive() {
		return t.start(t.d.QdrantServiceName)
	}
	return nil
}

// restored is the success Result. Files reports, for each file entry the archive
// carried, the ACTUAL write (a destination was wired) or the skip (none was — the
// subsystem is off on the current install), so the cmd tier never reports a false
// "restored".
func (t *restoreTxn) restored(v prove.Verdict) Result {
	return Result{
		Restored:              true,
		Prove:                 v,
		QdrantRestored:        t.ex.qdrantPresent,
		RestoredMemoryEnabled: t.restoredCfg.MemoryEnabled,
		Files:                 t.fileOutcomes(),
		EvalDropped:           t.evalDropped(),
		EvalUnreadable:        t.evalUnreadable,
		// Surface the EXCLUDED agent binary identity for the operator to RE-STAGE
		// (re-download the pinned release) — the binary bytes were never in the
		// archive, exactly like model weights. Nil on an agent-off backup (the
		// manifest recorded no ExcludedAgent).
		ExcludedAgent: t.ex.manifest.ExcludedAgent,
	}
}

func (t *restoreTxn) fileOutcomes() map[string]FileOutcome {
	out := map[string]FileOutcome{}
	for _, row := range fileRows {
		if _, present := t.ex.files[row.Name]; present {
			out[row.Name] = FileOutcome{Restored: t.willWrite(row), Skipped: !t.willWrite(row)}
		}
	}
	return out
}

// evalDropped names the baselines this restore replaced away: those in the
// eval-baselines.json captured before mutation that the archive's document lacks.
// Restore replaces the whole document verbatim, a baseline cannot be re-recorded
// after the regression it exists to catch (ADR-0018), and so the loss is reported
// rather than merged around. Nothing is dropped when the archive carries no
// document (the current file is left alone) or there was no current file. A current
// file that could not be read names nothing here: the skew gate asked about it and
// Result.EvalUnreadable carries why. An archive document that cannot be read keeps
// none of the current baselines, so each is named.
func (t *restoreTxn) evalDropped() []string {
	archived, carried := t.ex.files[EntryEvalBaselines]
	current, hadCurrent := t.prior[EntryEvalBaselines]
	if !carried || !hadCurrent || t.in.EvalKeysOf == nil {
		return nil
	}
	have, err := t.in.EvalKeysOf(current)
	if err != nil {
		return nil
	}
	keep, _ := t.in.EvalKeysOf(archived)
	return missingFrom(have, keep)
}

// missingFrom is the entries of have that are not in keep, in order.
func missingFrom(have, keep []string) []string {
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	var gone []string
	for _, k := range have {
		if !kept[k] {
			gone = append(gone, k)
		}
	}
	return gone
}

// rolledBack assembles a RolledBack Result, folding in an honest
// rollback-incomplete message when the restore did not fully succeed (Pitfall 5).
func (t *restoreTxn) rolledBack(failedStep, reason string, origErr error, v prove.Verdict) Result {
	rb := t.rollback()
	r := Result{
		RolledBack: true,
		FailedStep: failedStep,
		Reason:     reason,
		Err:        origErr,
		Prove:      v,
	}
	if !rb.ok {
		r.RollbackIncomplete = true
		r.Reason = "rolled back, but the restore did not fully complete (" + rb.detail +
			") — run `villa status` and inspect the villa-openwebui unit"
	}
	return r
}

// rollbackLog accumulates errors across ALL rollback steps rather than aborting on
// the first, and reports whether EVERY step succeeded. Per RESEARCH Pitfall 5 an
// incomplete rollback is flagged honestly — never claim a clean no-op when a
// restore step errored.
type rollbackLog struct {
	ok     bool
	detail string
}

func (l *rollbackLog) add(e error, what string) {
	if e == nil {
		return
	}
	l.ok = false
	if l.detail != "" {
		l.detail += "; "
	}
	l.detail += what + ": " + e.Error()
}

// rollback re-applies the captured prior state verbatim and re-readies the stack,
// best-effort. It uses the SAME clean-recreate ordering as the forward path so the
// rollback re-import never merges into a live volume either.
func (t *restoreTxn) rollback() *rollbackLog {
	l := &rollbackLog{ok: true}
	t.rollbackQuiesce(l)
	l.add(t.d.SaveConfig(t.priorCfg), "SaveConfig(prior)")
	t.rollbackFiles(l)
	l.add(t.cleanRecreateThenImport(t.priorCfg, t.in.OpenWebUIVolumeName, t.in.RollbackVolumeTar), "restore Open WebUI volume")
	t.rollbackQdrant(l)
	l.add(t.d.Start(t.d.OpenWebUIServiceName), "restart Open WebUI")
	return l
}

// rollbackQuiesce stops the services FIRST: the forward path starts Open WebUI (and
// Qdrant) at the restart step BEFORE the Prove gate, so a prove-triggered rollback
// arrives with the services RUNNING — and a running container holds its volume,
// making the clean-recreate VolumeRm below fail in-use on a live host. Mirror the
// forward path's own quiesce before any volume work; Stop on an already-stopped
// unit is an idempotent no-op. The qdrant stop mirrors the forward Start gate
// (entry present AND a prior volume existed) — on the prior-absent cell nothing was
// ever started.
func (t *restoreTxn) rollbackQuiesce(l *rollbackLog) {
	l.add(t.d.Stop(t.d.OpenWebUIServiceName), "stop Open WebUI for rollback")
	if t.qdrantLive() {
		l.add(t.d.Stop(t.d.QdrantServiceName), "stop Qdrant for rollback")
	}
}

// rollbackFiles restores each file entry VERBATIM. For each:
//   - prior existed → rewrite the captured prior bytes;
//   - prior absent BUT the forward path created it → REMOVE it, so the rolled-back
//     state matches the prior (absent) state. Without this, a restored-from-archive
//     file was left on disk after a "rollback", leaking backup chat/usage data into
//     a supposedly prior-restored install. A failed RemoveFile counts as
//     rollback-incomplete.
func (t *restoreTxn) rollbackFiles(l *rollbackLog) {
	for _, row := range fileRows {
		b, hadPrior := t.prior[row.Name]
		switch {
		case hadPrior:
			l.add(t.writeRow(row, b), "restore "+row.Label)
		case t.willWrite(row):
			l.add(rollbackRemove(t.d, t.in.Dests[row.Name]), "remove restored "+row.Label)
		}
	}
}

// rollbackQdrant: same clean-recreate ordering from the CAPTURED rollback tar when
// a prior volume existed; when the prior state was ABSENT, restore it verbatim by
// REMOVING the forward-created volume (the volume analog of rollbackRemove).
// Entry-absent ⇒ zero calls.
func (t *restoreTxn) rollbackQdrant(l *rollbackLog) {
	if !t.ex.qdrantPresent {
		return
	}
	if !t.in.QdrantVolumeExists {
		l.add(t.d.VolumeRm(t.in.QdrantVolumeName), "remove forward-created Qdrant volume")
		return
	}
	l.add(t.cleanRecreateThenImport(t.priorCfg, t.in.QdrantVolumeName, t.in.RollbackQdrantTar), "restore Qdrant volume")
	l.add(t.d.Start(t.d.QdrantServiceName), "restart Qdrant")
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
