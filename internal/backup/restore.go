package backup

// restore.go is the PURE, Deps-injected transactional core for `villa restore`
// (BAK-02 / BAK-03). It clones the proven internal/backendswap.Run frame —
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
// the volume. So restore MUST clean-recreate the Open WebUI volume —
// VolumeRm (not-found-tolerant) → ReconcileAndWrite (Quadlet recreate from the
// RESTORED config, the single source of truth) → EnsureVolume (explicit
// `podman volume create`, idempotent) — BEFORE every VolumeImport, on the
// forward apply AND the rollback path, so stale chats/webui.db never leak through.
//
// It links NO inference and NO detect package: the prove sentinel
// (ProveStatusPass) is this package's OWN local value, so the backend-marker seam
// discipline (TestSeamGrepGate) holds. Every host effect is a Deps func field; the
// whole flow is driven from restore_test.go without a live host.

import (
	"bytes"
	"fmt"
	"io"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// RestoreInput is the plain-data drive for the pure Restore orchestrator. The cmd
// tier (liveRestoreDeps + runRestore) gathers everything host-derived — the
// archive opener, the current-install facts for the skew compare (sourced from the
// seam: inference.BackendFor(...).Image() / orchestrate.OpenWebUIImage() — never a
// re-typed literal, D-10), the consent gate + its bypass flag, the podman volume
// name, and the resolved destination paths for the restored data-dir artifacts —
// then Restore() executes the pure transactional ordering over the injected Deps.
type RestoreInput struct {
	// OpenArchive opens the outer .tar for a fresh read pass. Restore calls it TWICE
	// (once to verify per-entry SHA-256, once to extract the verified entries) so the
	// reader need not be seekable; each call yields a fresh stream the core closes.
	OpenArchive func() (io.ReadCloser, error)

	// Current is the current-install snapshot the manifest is compared against
	// (BAK-03). The cmd tier fills it seam-/accessor-sourced; Restore sets
	// Current.ChecksumFailed itself from the verify pass before CompareSkew.
	Current CurrentInstall

	// Consent is the y/N gate invoked once with the assembled WARN text when the skew
	// compare yields warnings (and Bypass is false). It returns true to proceed. The
	// live closure composes stdinIsInteractive + promptConsent (D-08).
	Consent func(prompt string) bool
	// Bypass is the --yes/--force flag: when true, a WARN-only skew is applied without
	// invoking Consent (D-08). It NEVER bypasses a fail-closed BLOCK.
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
}

// Restore performs the guarded, transactional archive apply and returns a typed
// Result, cloning backendswap.Run's frame. Ordering (RESEARCH §Transactional
// Restore):
//
//	(1) READ+VERIFY (pure, zero side effects): open the outer tar, parse
//	    manifest.json, verify each entry's SHA-256 against the manifest. A mismatch
//	    or unreadable/incompatible manifest.schema_version → Refused (D-08).
//	(2) SKEW: CompareSkew(manifest, Current). Block → Refused. WARN-only → require
//	    Consent unless Bypass; a declined gate → Refused. (All still zero side effects.)
//	(3) CAPTURE strictly BEFORE mutation: export the CURRENT owui volume + snapshot
//	    the current config + current usage.json/bench-reports.jsonl. Uncapturable → Refused.
//	(4) QUIESCE: Stop the Open WebUI service.
//	(5) MUTATE (any error → rollback): SaveConfig(restored) → restore data-dir files →
//	    CLEAN-RECREATE owui volume (VolumeRm → ReconcileAndWrite → EnsureVolume) →
//	    VolumeImport(extracted owui tar) → Start.
//	(6) PROVE: switch to success ONLY on ProveStatusPass; any other verdict → rollback.
//
// The rollback path re-applies the captured set through the SAME clean-recreate
// ordering (VolumeRm → ReconcileAndWrite(prior cfg) → EnsureVolume → VolumeImport
// of the captured tar) so a rollback never merge-imports into a live volume either.
func Restore(d Deps, in RestoreInput) Result {
	// (1) READ+VERIFY — pure, zero side effects. A verify failure or an
	// unreadable/incompatible manifest is a fail-closed BLOCK (D-08) BEFORE any mutate.
	ex, verr := readAndVerify(in)
	if verr != nil {
		return Result{Refused: true, FailedStep: "verify", Err: verr,
			Reason: "archive failed integrity verification — refusing to restore a corrupt backup: " + verr.Error()}
	}

	// (2) SKEW (BAK-03 / D-08). A checksum failure is folded into CompareSkew via the
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
	priorCfg, err := d.LoadConfig()
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "cannot snapshot the current config for rollback — refusing to mutate: " + err.Error()}
	}
	if err := d.VolumeExport(in.OpenWebUIVolumeName, in.RollbackVolumeTar); err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "cannot capture the current Open WebUI volume for rollback — refusing to mutate: " + err.Error()}
	}
	priorUsage, priorUsageOK := captureFile(d, in.UsageDestPath)
	priorBench, priorBenchOK := captureFile(d, in.BenchDestPath)

	// Restored config is the archive's config.toml parsed into a VillaConfig (config is
	// the single source of truth — the Quadlet recreate renders from it; D-07).
	restoredCfg, err := config.Parse(ex.config)
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err,
			Reason: "archive config.toml is unreadable — refusing to mutate: " + err.Error()}
	}

	// cleanRecreateThenImport is the load-bearing clean-recreate-before-import
	// sequence (RESEARCH Pitfall 1/2), used on BOTH the forward apply and the rollback:
	// VolumeRm (not-found-tolerant) → ReconcileAndWrite (Quadlet recreate from cfg) →
	// EnsureVolume (explicit create) → VolumeImport. import MERGES + does NOT
	// auto-create, so the volume MUST be rm'd + freshly created first.
	cleanRecreateThenImport := func(cfg config.VillaConfig, srcTar string) error {
		if err := d.VolumeRm(in.OpenWebUIVolumeName); err != nil {
			return fmt.Errorf("volume rm %s: %w", in.OpenWebUIVolumeName, err)
		}
		if _, err := d.ReconcileAndWrite(cfg); err != nil {
			return fmt.Errorf("reconcile/recreate units: %w", err)
		}
		if err := d.EnsureVolume(in.OpenWebUIVolumeName); err != nil {
			return fmt.Errorf("ensure volume %s: %w", in.OpenWebUIVolumeName, err)
		}
		if err := d.VolumeImport(in.OpenWebUIVolumeName, srcTar); err != nil {
			return fmt.Errorf("volume import %s: %w", in.OpenWebUIVolumeName, err)
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
		add(d.SaveConfig(priorCfg), "SaveConfig(prior)")
		if priorUsageOK {
			add(d.WriteFileAtomic(in.UsageDestPath, priorUsage), "restore usage.json")
		}
		if priorBenchOK {
			add(d.WriteFileAtomic(in.BenchDestPath, priorBench), "restore bench-reports.jsonl")
		}
		// Re-import the CAPTURED owui volume through the clean-recreate ordering (prior cfg).
		add(cleanRecreateThenImport(priorCfg, in.RollbackVolumeTar), "restore Open WebUI volume")
		add(d.Start(d.OpenWebUIServiceName), "restart Open WebUI")
		return ok, detail
	}

	// rolledBack assembles a RolledBack Result, folding in an honest
	// rollback-incomplete message when the restore did not fully succeed (Pitfall 5).
	rolledBack := func(failedStep, reason string, origErr error, v ProveVerdict) Result {
		rbOK, rbDetail := rollback()
		r := Result{
			RolledBack: true,
			FailedStep: failedStep,
			Reason:     reason,
			Err:        origErr,
			Prove:      v,
		}
		if !rbOK {
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-openwebui unit"
		}
		return r
	}

	// (4) QUIESCE the Open WebUI service for a clean volume swap (D-05). A stop failure
	// is a pre-mutate error → rollback (which best-effort re-readies).
	if err := d.Stop(d.OpenWebUIServiceName); err != nil {
		return rolledBack("quiesce", "", fmt.Errorf("stop %s: %w", d.OpenWebUIServiceName, err), ProveVerdict{})
	}

	// (5) MUTATE. ANY error here rolls back verbatim from the captured set.
	if err := d.SaveConfig(restoredCfg); err != nil {
		return rolledBack("save", "", fmt.Errorf("save restored config: %w", err), ProveVerdict{})
	}
	if ex.usagePresent {
		if err := d.WriteFileAtomic(in.UsageDestPath, ex.usage); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore usage.json: %w", err), ProveVerdict{})
		}
	}
	if ex.benchPresent {
		if err := d.WriteFileAtomic(in.BenchDestPath, ex.bench); err != nil {
			return rolledBack("data", "", fmt.Errorf("restore bench-reports.jsonl: %w", err), ProveVerdict{})
		}
	}
	// CLEAN-RECREATE then import the RESTORED owui volume (the whole reason for the
	// rm→recreate→ensure→import ordering — never merge into a live volume).
	if err := d.WriteFileAtomic(in.TempVolumeTar, ex.owuiVolume); err != nil {
		return rolledBack("volume", "", fmt.Errorf("stage restored owui volume tar: %w", err), ProveVerdict{})
	}
	if err := cleanRecreateThenImport(restoredCfg, in.TempVolumeTar); err != nil {
		return rolledBack("volume", "", err, ProveVerdict{})
	}
	if err := d.Start(d.OpenWebUIServiceName); err != nil {
		return rolledBack("restart", "", fmt.Errorf("start %s: %w", d.OpenWebUIServiceName, err), ProveVerdict{})
	}

	// (6) PROVE the restored stack offload-honestly. Switch to success ONLY on
	// ProveStatusPass; ANY other verdict (incl. ready+health-200-but-residency-FAIL)
	// rolls back verbatim — is-active/200 alone is NEVER success (D-07).
	v := d.Prove(restoredCfg.Backend)
	if v.Status != ProveStatusPass {
		return rolledBack("prove", v.Detail, nil, v)
	}
	return Result{Restored: true, Prove: v}
}

// captureFile reads a current data-dir artifact for the rollback set via Deps.ReadFile.
// An absent/unreadable file yields ok=false (the rollback then simply does not restore
// it — it was not there to begin with), never a hard failure.
func captureFile(d Deps, path string) (data []byte, ok bool) {
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
	// guard to every entry name before handing it to fn (D-11), so a malicious
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
			// Manifest-first on READ (WR-03): the manifest MUST be the FIRST tar member
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
			// Schema-gate the manifest BEFORE reading any further entry body (WR-03):
			// fail-closed BLOCK on an unreadable/incompatible schema (D-08), mirroring
			// usage.Load's fail-closed-on-future discipline.
			if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
				return fmt.Errorf("manifest schema_version %d is unreadable or newer than this villa supports (%d)",
					m.SchemaVersion, backupSchemaVersion)
			}
			return nil
		}

		// Every non-manifest entry arrives AFTER the manifest (WR-03): if the manifest
		// was not the first member, the idx!=0 check above already refused it; a data
		// entry at idx 0 means there was no leading manifest.
		if !manifestSeen {
			return fmt.Errorf("archive %s must be the FIRST entry — entry %q precedes it", EntryManifest, name)
		}
		// Reject duplicate entry names explicitly (WR-02): the prior `collect[name]=data`
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

	// Reject any collected entry NOT listed in the manifest (WR-02): the archive must
	// contain EXACTLY the manifest-described members — an extra/unexpected entry was
	// previously accepted-and-ignored, which is not what the manifest claims.
	for name := range collect {
		if _, listed := want[name]; !listed {
			return ex, fmt.Errorf("archive contains entry %q not listed in the manifest", name)
		}
	}

	// Verify every manifest-listed entry's SHA-256 against the collected bytes. A
	// missing required entry or a mismatch is archive corruption (D-08).
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
	return ex, nil
}

// skewPrompt assembles the WARN-and-confirm prompt text from the skew warnings:
// each finding's Field, Detail, and named Remediation, plus a final y/N question
// (D-08). The cmd-tier Consent closure prints this and reads the answer.
func skewPrompt(ws []SkewWarning) string {
	var b bytes.Buffer
	b.WriteString("restore detected skew between the backup and the current install:\n")
	for _, w := range ws {
		fmt.Fprintf(&b, "  - %s: %s\n      remediation: %s\n", w.Field, w.Detail, w.Remediation)
	}
	b.WriteString("proceed with restore? [y/N]: ")
	return b.String()
}
