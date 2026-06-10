package backup

// restore_test.go drives the pure transactional Restore() state-machine off-hardware
// with a fakeDeps recorder, asserting the BAK-02/BAK-03 invariants:
//   - a SHA-256 verify failure / incompatible manifest schema → Refused, ZERO mutate calls
//   - a fail-closed BLOCK skew → Refused
//   - a WARN skew with consent denied → Refused (and --yes/Bypass proceeds)
//   - the happy-path clean-recreate-before-import ordering (VolumeRm + ReconcileAndWrite
//     + EnsureVolume BEFORE VolumeImport) on the FORWARD path
//   - a mutate error rolls back and the rollback re-imports the CAPTURED tar through the
//     SAME clean-recreate ordering, reporting RolledBack:true
//   - a rollback-STEP error yields RolledBack:true AND a rollback-incomplete Reason
//   - a non-pass Prove rolls back

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// recDeps is the order-recording fake Deps + the canned outcomes each seam returns.
type recDeps struct {
	calls []string // ordered seam-call log (verb+arg)

	loadCfg     config.VillaConfig
	loadErr     error
	saveErr     error
	saveErrOnce map[int]error // SaveConfig error keyed by call ordinal (1-based)
	saveN       int

	volumeExportErr error
	volumeRmErr     error
	ensureVolErr    error
	volumeImportErr error
	reconcileErr    error
	stopErr         error
	startErr        error
	writeFileErr    error
	writeTempErr    error
	removeFileErr   error
	readFile        map[string][]byte
	readFileErr     map[string]error

	prove ProveVerdict
}

func (r *recDeps) log(s string) { r.calls = append(r.calls, s) }

// deps builds a backup.Deps wired to the recorder.
func (r *recDeps) deps() Deps {
	return Deps{
		OpenWebUIServiceName: "villa-openwebui.service",
		InstallServiceName:   "villa-llama.service",
		QdrantServiceName:    "qdrant.service",
		LoadConfig: func() (config.VillaConfig, error) {
			r.log("LoadConfig")
			return r.loadCfg, r.loadErr
		},
		SaveConfig: func(c config.VillaConfig) error {
			r.saveN++
			r.log("SaveConfig:" + c.Backend)
			if r.saveErrOnce != nil {
				if e, ok := r.saveErrOnce[r.saveN]; ok {
					return e
				}
			}
			return r.saveErr
		},
		VolumeExport: func(name, out string) error {
			r.log("VolumeExport:" + name)
			return r.volumeExportErr
		},
		VolumeRm: func(name string) error {
			r.log("VolumeRm:" + name)
			return r.volumeRmErr
		},
		EnsureVolume: func(name string) error {
			r.log("EnsureVolume:" + name)
			return r.ensureVolErr
		},
		VolumeImport: func(name, src string) error {
			r.log("VolumeImport:" + name + ":" + src)
			return r.volumeImportErr
		},
		ReconcileAndWrite: func(c config.VillaConfig) (bool, error) {
			r.log("ReconcileAndWrite:" + c.Backend)
			return true, r.reconcileErr
		},
		Stop: func(s string) error {
			r.log("Stop:" + s)
			return r.stopErr
		},
		Start: func(s string) error {
			r.log("Start:" + s)
			return r.startErr
		},
		Restart: func(s string) error {
			r.log("Restart:" + s)
			return nil
		},
		ReadFile: func(p string) ([]byte, error) {
			if r.readFileErr != nil {
				if e, ok := r.readFileErr[p]; ok {
					return nil, e
				}
			}
			if r.readFile != nil {
				if b, ok := r.readFile[p]; ok {
					return b, nil
				}
			}
			return nil, errors.New("not found: " + p)
		},
		WriteFileAtomic: func(p string, data []byte) error {
			r.log("WriteFileAtomic:" + p)
			return r.writeFileErr
		},
		WriteTempFile: func(p string, data []byte) error {
			r.log("WriteTempFile:" + p)
			return r.writeTempErr
		},
		RemoveFile: func(p string) error {
			r.log("RemoveFile:" + p)
			return r.removeFileErr
		},
		DaemonReload: func() error { return nil },
		Prove: func(target string) ProveVerdict {
			r.log("Prove:" + target)
			return r.prove
		},
	}
}

// buildArchive assembles a valid in-memory archive (manifest FIRST + correct
// per-entry SHA-256) the same way Backup does, so the restore verify pass passes.
// owui/usage/bench are optional (nil entry omitted). corrupt flips one byte of the
// owui entry AFTER its checksum is recorded, to drive a verify mismatch.
func buildArchive(t *testing.T, m Manifest, cfgTOML, owui, usage, bench []byte, corrupt bool) []byte {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	var data []e
	data = append(data, e{EntryConfig, cfgTOML})
	data = append(data, e{EntryOpenWebUIVolume, owui})
	if usage != nil {
		data = append(data, e{EntryUsage, usage})
	}
	if bench != nil {
		data = append(data, e{EntryBenchReports, bench})
	}
	var sums []EntryChecksum
	for _, d := range data {
		s, err := sum(bytes.NewReader(d.data))
		if err != nil {
			t.Fatalf("sum: %v", err)
		}
		sums = append(sums, EntryChecksum{Name: d.name, SHA256: s})
	}
	m.Entries = sums
	if m.SchemaVersion == 0 {
		m.SchemaVersion = backupSchemaVersion
	}
	mj, err := marshalManifest(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	entries := []archiveEntry{{name: EntryManifest, data: mj}}
	for i, d := range data {
		payload := d.data
		if corrupt && d.name == EntryOpenWebUIVolume {
			payload = append([]byte("X"), d.data...) // mismatch vs recorded sum
		}
		_ = i
		entries = append(entries, archiveEntry{name: d.name, data: payload})
	}
	var buf bytes.Buffer
	if err := writeArchive(&buf, entries); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	return buf.Bytes()
}

// opener returns an OpenArchive func yielding a fresh reader over b on each call.
func opener(b []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
}

// validCfgTOML is a minimal config.toml the restore parses into a VillaConfig.
var validCfgTOML = []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 4096\n")

func passVerdict() ProveVerdict { return ProveVerdict{Status: ProveStatusPass} }

// baseInput builds a RestoreInput over the archive bytes with a matching Current
// (no skew) and a pass prove, plus a recorder.
func baseInput(t *testing.T, arch []byte) (*recDeps, RestoreInput) {
	t.Helper()
	r := &recDeps{
		loadCfg:  config.VillaConfig{Backend: "vulkan", Model: "m"},
		prove:    passVerdict(),
		readFile: map[string][]byte{},
	}
	in := RestoreInput{
		OpenArchive:         opener(arch),
		Current:             baseCurrent(),
		Consent:             func(string) bool { return true },
		OpenWebUIVolumeName: "villa-openwebui",
		TempVolumeTar:       "/tmp/restore-owui.tar",
		RollbackVolumeTar:   "/tmp/rollback-owui.tar",
		UsageDestPath:       "/data/usage.json",
		BenchDestPath:       "/data/bench-reports.jsonl",
	}
	return r, in
}

// indexOf returns the first index of a call matching prefix, or -1.
func indexOf(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// hasMutate reports whether any mutating seam was called (used to assert zero side
// effects on a Refused path).
func hasMutate(calls []string) bool {
	for _, c := range calls {
		for _, m := range []string{"SaveConfig", "VolumeRm", "EnsureVolume", "VolumeImport", "ReconcileAndWrite", "WriteFileAtomic", "WriteTempFile", "RemoveFile", "Stop", "Start"} {
			if strings.HasPrefix(c, m) {
				return true
			}
		}
	}
	return false
}

// rawMultiTar assembles a tar from explicit (name, data) members in the GIVEN
// order, bypassing buildArchive's manifest-first/checksum discipline so the
// read-side WR-02/WR-03 guards (duplicate / extra / out-of-order entries) are
// exercised directly.
func rawMultiTar(t *testing.T, members []archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := writeArchive(&buf, members); err != nil {
		t.Fatalf("rawMultiTar writeArchive: %v", err)
	}
	return buf.Bytes()
}

// manifestJSONFor builds a manifest.json listing exactly the given entries with
// correct checksums (schema = backupSchemaVersion), for the raw-tar guard tests.
func manifestJSONFor(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	m := baseManifest()
	m.SchemaVersion = backupSchemaVersion
	var sums []EntryChecksum
	for _, e := range entries {
		s, err := sum(bytes.NewReader(e.data))
		if err != nil {
			t.Fatalf("sum: %v", err)
		}
		sums = append(sums, EntryChecksum{Name: e.name, SHA256: s})
	}
	m.Entries = sums
	mj, err := marshalManifest(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return mj
}

// TestRestoreDuplicateEntryRefuses asserts a duplicate non-manifest entry name is
// refused at verify with ZERO side effects (WR-02).
func TestRestoreDuplicateEntryRefuses(t *testing.T) {
	cfg := validCfgTOML
	owui := []byte("owui-data")
	dataEntries := []archiveEntry{{EntryConfig, cfg}, {EntryOpenWebUIVolume, owui}}
	mj := manifestJSONFor(t, dataEntries)
	// Two config.toml members (duplicate name) after the manifest.
	arch := rawMultiTar(t, []archiveEntry{
		{EntryManifest, mj},
		{EntryConfig, cfg},
		{EntryConfig, []byte("model = \"other\"\n")},
		{EntryOpenWebUIVolume, owui},
	})
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "verify" {
		t.Fatalf("want Refused at verify on a duplicate entry, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("duplicate-entry refusal must have ZERO mutate side effects, got %v", r.calls)
	}
}

// TestRestoreExtraEntryRefuses asserts an entry NOT listed in the manifest is
// refused at verify with ZERO side effects (WR-02 exact-set).
func TestRestoreExtraEntryRefuses(t *testing.T) {
	cfg := validCfgTOML
	owui := []byte("owui-data")
	dataEntries := []archiveEntry{{EntryConfig, cfg}, {EntryOpenWebUIVolume, owui}}
	mj := manifestJSONFor(t, dataEntries)
	arch := rawMultiTar(t, []archiveEntry{
		{EntryManifest, mj},
		{EntryConfig, cfg},
		{EntryOpenWebUIVolume, owui},
		{"unexpected.txt", []byte("stowaway")}, // not in the manifest
	})
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "verify" {
		t.Fatalf("want Refused at verify on an unexpected entry, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("extra-entry refusal must have ZERO mutate side effects, got %v", r.calls)
	}
}

// TestRestoreManifestNotFirstRefuses asserts an archive whose first member is NOT
// manifest.json is refused at verify with ZERO side effects (WR-03).
func TestRestoreManifestNotFirstRefuses(t *testing.T) {
	cfg := validCfgTOML
	owui := []byte("owui-data")
	dataEntries := []archiveEntry{{EntryConfig, cfg}, {EntryOpenWebUIVolume, owui}}
	mj := manifestJSONFor(t, dataEntries)
	// Data entry BEFORE the manifest.
	arch := rawMultiTar(t, []archiveEntry{
		{EntryConfig, cfg},
		{EntryManifest, mj},
		{EntryOpenWebUIVolume, owui},
	})
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "verify" {
		t.Fatalf("want Refused at verify on a non-first manifest, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("manifest-not-first refusal must have ZERO mutate side effects, got %v", r.calls)
	}
}

func TestRestoreVerifyMismatchRefusesZeroSideEffects(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, true /*corrupt*/)
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "verify" {
		t.Fatalf("want Refused at verify, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("verify-fail must have ZERO mutate side effects, got calls %v", r.calls)
	}
}

func TestRestoreIncompatibleSchemaRefuses(t *testing.T) {
	m := baseManifest()
	m.SchemaVersion = backupSchemaVersion + 1
	arch := buildArchive(t, m, validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "verify" {
		t.Fatalf("want Refused at verify for future schema, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("incompatible-schema must have ZERO mutate side effects, got %v", r.calls)
	}
}

func TestRestoreBlockSkewRefuses(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	// Drive a BLOCK: current usage schema OLDER than the manifest's (future store).
	in.Current.UsageSchemaVersion = 0 // manifest has 1 → newer than current → BLOCK
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "skew" {
		t.Fatalf("want Refused at skew BLOCK, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("BLOCK skew must have ZERO mutate side effects, got %v", r.calls)
	}
}

func TestRestoreWarnSkewConsentDeniedRefuses(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	in.Current.VillaVersion = "v9.9.9" // WARN-only skew
	in.Consent = func(string) bool { return false }
	res := Restore(r.deps(), in)
	if !res.Refused || res.FailedStep != "skew" {
		t.Fatalf("want Refused at skew on declined consent, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("declined-consent must have ZERO mutate side effects, got %v", r.calls)
	}
}

func TestRestoreWarnSkewBypassProceeds(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	in.Current.VillaVersion = "v9.9.9" // WARN-only skew
	in.Consent = func(string) bool { t.Fatalf("Consent must NOT be called when Bypass=true"); return false }
	in.Bypass = true
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored with Bypass over a WARN skew, got %+v", res)
	}
}

func TestRestoreHappyPathCleanRecreateBeforeImport(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("usage"), []byte("bench"), false)
	r, in := baseInput(t, arch)
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	// CAPTURE strictly before mutate: VolumeExport precedes the first SaveConfig.
	if iExp, iSave := indexOf(r.calls, "VolumeExport"), indexOf(r.calls, "SaveConfig"); iExp == -1 || iExp > iSave {
		t.Fatalf("capture (VolumeExport) must precede mutate (SaveConfig); calls %v", r.calls)
	}
	// Clean-recreate-before-import ordering on the FORWARD path: VolumeRm <
	// ReconcileAndWrite < EnsureVolume < VolumeImport.
	iRm := indexOf(r.calls, "VolumeRm")
	iRec := indexOf(r.calls, "ReconcileAndWrite")
	iEns := indexOf(r.calls, "EnsureVolume")
	iImp := indexOf(r.calls, "VolumeImport")
	if iRm == -1 || iRec == -1 || iEns == -1 || iImp == -1 {
		t.Fatalf("missing a clean-recreate seam call: %v", r.calls)
	}
	if !(iRm < iRec && iRec < iEns && iEns < iImp) {
		t.Fatalf("want VolumeRm<ReconcileAndWrite<EnsureVolume<VolumeImport, got rm=%d rec=%d ens=%d imp=%d (%v)",
			iRm, iRec, iEns, iImp, r.calls)
	}
}

func TestRestoreMutateErrorRollsBackAndReImportsCaptured(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	// Forward import fails → rollback. The rollback must re-import the CAPTURED tar
	// through the SAME clean-recreate ordering.
	r.volumeImportErr = errors.New("import boom")
	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack on a mutate error, got %+v", res)
	}
	// Two VolumeImport attempts: forward (TempVolumeTar) then rollback (RollbackVolumeTar).
	var imports []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "VolumeImport:") {
			imports = append(imports, c)
		}
	}
	if len(imports) != 2 {
		t.Fatalf("want forward+rollback VolumeImport (2), got %v (all %v)", imports, r.calls)
	}
	if !strings.HasSuffix(imports[1], in.RollbackVolumeTar) {
		t.Fatalf("rollback import must use the CAPTURED tar %q, got %q", in.RollbackVolumeTar, imports[1])
	}
	// Rollback re-import also goes through clean-recreate (a second VolumeRm precedes it).
	rmCount := 0
	for _, c := range r.calls {
		if strings.HasPrefix(c, "VolumeRm") {
			rmCount++
		}
	}
	if rmCount != 2 {
		t.Fatalf("rollback must clean-recreate too (2 VolumeRm), got %d (%v)", rmCount, r.calls)
	}
}

// TestRestoreTempVolumeStagingFailureRollsBack is the on-hardware WR-05 regression:
// staging the extracted OWUI volume tar must go through the UNguarded WriteTempFile
// seam (a /tmp path outside the data store), NOT the store-root-guarded
// WriteFileAtomic — the latter rejected the legitimate /tmp write and failed every
// restore at the "volume" stage. Here WriteTempFile errs: restore must roll back at
// "volume" with the prior stack intact and must NEVER reach the forward VolumeImport.
func TestRestoreTempVolumeStagingFailureRollsBack(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	r.writeTempErr = errors.New("stage temp boom")

	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack on temp-staging failure, got %+v (calls %v)", res, r.calls)
	}
	if res.FailedStep != "volume" {
		t.Fatalf("want FailedStep \"volume\", got %q (calls %v)", res.FailedStep, r.calls)
	}
	// The forward path must NOT have reached its VolumeImport (staging failed first).
	// Only the rollback re-import of the captured tar may run.
	for _, c := range r.calls {
		if strings.HasPrefix(c, "VolumeImport:") && strings.HasSuffix(c, in.TempVolumeTar) {
			t.Fatalf("forward VolumeImport of the restored tar must not run after staging failed; calls %v", r.calls)
		}
	}
}

// TestRestoreRollbackRemovesForwardCreatedDataArtifacts is the CR-01 regression:
// the prior install has NO usage.json / bench-reports.jsonl, the archive CARRIES
// both, a post-write step fails (volume import), and rollback must REMOVE the
// forward-created data-dir artifacts to restore the prior (absent) state verbatim —
// never leave restored-from-archive data on disk after a "rollback".
func TestRestoreRollbackRemovesForwardCreatedDataArtifacts(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("usage-from-archive"), []byte("bench-from-archive"), false)
	r, in := baseInput(t, arch)
	// Prior install has NO usage.json / bench-reports.jsonl: capture (ReadFile) fails
	// for both dest paths, so priorUsageOK/priorBenchOK are false.
	r.readFileErr = map[string]error{
		in.UsageDestPath: errors.New("not found"),
		in.BenchDestPath: errors.New("not found"),
	}
	// Force a post-data-write failure via a NON-PASS prove so rollback runs AFTER the
	// forward path wrote the archive's usage.json/bench-reports.jsonl, WITHOUT breaking
	// the rollback path itself (a volumeImportErr would also fail the rollback re-import
	// and mask the clean-remove assertion).
	r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}

	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack, got %+v (calls %v)", res, r.calls)
	}
	// Forward path wrote both data artifacts...
	if indexOf(r.calls, "WriteFileAtomic:"+in.UsageDestPath) == -1 {
		t.Fatalf("forward path must have written usage.json; calls %v", r.calls)
	}
	// ...and rollback must REMOVE both (no prior to restore).
	if indexOf(r.calls, "RemoveFile:"+in.UsageDestPath) == -1 {
		t.Fatalf("rollback must RemoveFile the forward-created usage.json; calls %v", r.calls)
	}
	if indexOf(r.calls, "RemoveFile:"+in.BenchDestPath) == -1 {
		t.Fatalf("rollback must RemoveFile the forward-created bench-reports.jsonl; calls %v", r.calls)
	}
	// The remove must come AFTER the forward write (verbatim restore of absent state).
	if iW, iR := indexOf(r.calls, "WriteFileAtomic:"+in.UsageDestPath), indexOf(r.calls, "RemoveFile:"+in.UsageDestPath); iW > iR {
		t.Fatalf("RemoveFile must follow the forward WriteFileAtomic; calls %v", r.calls)
	}
	// A clean (complete) rollback: no rollback-incomplete reason.
	if strings.Contains(res.Reason, "did not fully complete") {
		t.Fatalf("rollback should be COMPLETE (RemoveFile succeeded), got %q", res.Reason)
	}
}

// TestRestoreRollbackRemoveFailureReportsIncomplete asserts a FAILED RemoveFile
// during rollback is counted as rollback-incomplete (honest reporting, CR-01).
func TestRestoreRollbackRemoveFailureReportsIncomplete(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("usage-from-archive"), nil, false)
	r, in := baseInput(t, arch)
	r.readFileErr = map[string]error{in.UsageDestPath: errors.New("not found")}
	r.volumeImportErr = errors.New("import boom")
	r.removeFileErr = errors.New("permission denied")

	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack:true, got %+v", res)
	}
	if !strings.Contains(res.Reason, "did not fully complete") {
		t.Fatalf("a failed RemoveFile must report rollback-incomplete, got %q", res.Reason)
	}
}

func TestRestoreRollbackStepErrorReportsIncomplete(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	// Forward fails at import → rollback; make the rollback SaveConfig(prior) error so
	// the rollback is incomplete. saveErrOnce: forward SaveConfig (call 1) ok, rollback
	// SaveConfig (call 2) errors.
	r.volumeImportErr = errors.New("import boom")
	r.saveErrOnce = map[int]error{2: errors.New("save prior boom")}
	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack:true even on an incomplete rollback, got %+v", res)
	}
	if !strings.Contains(res.Reason, "did not fully complete") {
		t.Fatalf("want an honest rollback-incomplete Reason, got %q", res.Reason)
	}
}

// ---------------------------------------------------------------------------
// Phase-23 qdrant volume + recall-state restore (D-07/D-08): the MANDATORY 2×2
// {entry present/absent} × {current volume present/absent} matrix, rollback
// symmetry by failure injection, and the recall-state.json forward/rollback rows.
// ---------------------------------------------------------------------------

// buildArchiveMem clones buildArchive with the two OPTIONAL Phase-23 memory
// entries (qdrant-volume.tar / recall-state.json); nil omits an entry.
func buildArchiveMem(t *testing.T, m Manifest, cfgTOML, owui, qdrant, recallState []byte) []byte {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	var data []e
	data = append(data, e{EntryConfig, cfgTOML})
	data = append(data, e{EntryOpenWebUIVolume, owui})
	if qdrant != nil {
		data = append(data, e{EntryQdrantVolume, qdrant})
	}
	if recallState != nil {
		data = append(data, e{EntryRecallState, recallState})
	}
	var sums []EntryChecksum
	for _, d := range data {
		s, err := sum(bytes.NewReader(d.data))
		if err != nil {
			t.Fatalf("sum: %v", err)
		}
		sums = append(sums, EntryChecksum{Name: d.name, SHA256: s})
	}
	m.Entries = sums
	if m.SchemaVersion == 0 {
		m.SchemaVersion = backupSchemaVersion
	}
	mj, err := marshalManifest(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	entries := []archiveEntry{{name: EntryManifest, data: mj}}
	for _, d := range data {
		entries = append(entries, archiveEntry{name: d.name, data: d.data})
	}
	var buf bytes.Buffer
	if err := writeArchive(&buf, entries); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	return buf.Bytes()
}

// memInput extends baseInput with the qdrant volume + recall-state destinations.
// Names deliberately avoid the real volume literal — they are seam-sourced by the
// cmd tier (D-05).
func memInput(t *testing.T, arch []byte, volExists bool) (*recDeps, RestoreInput) {
	t.Helper()
	r, in := baseInput(t, arch)
	in.QdrantVolumeName = "qdrant-vol"
	in.TempQdrantTar = "/tmp/restore-qdrant.tar"
	in.RollbackQdrantTar = "/tmp/rollback-qdrant.tar"
	in.QdrantVolumeExists = volExists
	in.RecallDestPath = "/data/recall-state.json"
	return r, in
}

// qdrantCalls filters the recorded seam calls down to anything naming the qdrant
// volume or service (the D-07 zero-touch assertion filter).
func qdrantCalls(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.Contains(c, "qdrant") {
			out = append(out, c)
		}
	}
	return out
}

// indexAfter returns the first index >= from of a call matching prefix, or -1.
func indexAfter(calls []string, prefix string, from int) int {
	for i := from; i < len(calls); i++ {
		if strings.HasPrefix(calls[i], prefix) {
			return i
		}
	}
	return -1
}

// memCfgTOML is a restored config with the memory stack ENABLED, for the
// posture-reporting assertions (Pitfall 5).
var memCfgTOML = []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 4096\nmemory_enabled = true\n")

// TestRestoreQdrantMatrix drives ALL FOUR {entry present/absent} ×
// {current volume present/absent} cells (D-07, Pitfall 4) on the happy path and
// asserts the per-cell seam-call contracts:
//   - present+exists: capture(qdrant) BEFORE mutate; forward clean-recreate
//     ordering VolumeRm < ReconcileAndWrite < EnsureVolume < VolumeImport on the
//     qdrant name; quiesce Stop(qdrant) before the rm, Start(qdrant) after import
//   - present+absent: NO capture export, NO Stop (nothing running); the volume is
//     created (EnsureVolume → Import)
//   - absent+exists / absent+absent: ZERO calls naming the qdrant volume/service
func TestRestoreQdrantMatrix(t *testing.T) {
	tests := []struct {
		name      string
		entry     bool
		volExists bool
	}{
		{"entry present + volume exists", true, true},
		{"entry present + volume absent", true, false},
		{"entry absent + volume exists", false, true},
		{"entry absent + volume absent", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var qdrant []byte
			if tt.entry {
				qdrant = []byte("qdrant-volume-data")
			}
			arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), qdrant, nil)
			r, in := memInput(t, arch, tt.volExists)
			res := Restore(r.deps(), in)
			if !res.Restored {
				t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
			}
			if res.QdrantRestored != tt.entry {
				t.Fatalf("QdrantRestored = %v, want %v", res.QdrantRestored, tt.entry)
			}

			if !tt.entry {
				// D-07 zero-touch: a memory-free backup NEVER touches qdrant state
				// regardless of what exists on the host (T-23-09).
				if qc := qdrantCalls(r.calls); len(qc) != 0 {
					t.Fatalf("entry-absent cell must make ZERO qdrant calls, got %v", qc)
				}
				return
			}

			iRm := indexOf(r.calls, "VolumeRm:qdrant-vol")
			iEns := indexOf(r.calls, "EnsureVolume:qdrant-vol")
			iImp := indexOf(r.calls, "VolumeImport:qdrant-vol:"+in.TempQdrantTar)
			if iRm == -1 || iEns == -1 || iImp == -1 {
				t.Fatalf("missing a qdrant clean-recreate call: %v", r.calls)
			}
			iRec := indexAfter(r.calls, "ReconcileAndWrite", iRm)
			if !(iRm < iRec && iRec < iEns && iEns < iImp) {
				t.Fatalf("want VolumeRm<ReconcileAndWrite<EnsureVolume<VolumeImport on qdrant, got rm=%d rec=%d ens=%d imp=%d (%v)",
					iRm, iRec, iEns, iImp, r.calls)
			}

			if tt.volExists {
				// Capture strictly before mutate.
				iExp := indexOf(r.calls, "VolumeExport:qdrant-vol")
				iSave := indexOf(r.calls, "SaveConfig")
				if iExp == -1 || iExp > iSave {
					t.Fatalf("capture VolumeExport(qdrant) must precede mutate; calls %v", r.calls)
				}
				// Quiesce: the running qdrant service is stopped before its volume is
				// swapped and started after the import.
				iStop := indexOf(r.calls, "Stop:qdrant.service")
				iStart := indexOf(r.calls, "Start:qdrant.service")
				if iStop == -1 || iStop > iRm {
					t.Fatalf("Stop(qdrant) must precede the qdrant VolumeRm; calls %v", r.calls)
				}
				if iStart == -1 || iStart < iImp {
					t.Fatalf("Start(qdrant) must follow the qdrant import; calls %v", r.calls)
				}
			} else {
				// Prior-absent: nothing to capture, nothing to stop.
				if indexOf(r.calls, "VolumeExport:qdrant-vol") != -1 {
					t.Fatalf("prior-absent cell must NOT capture-export the qdrant volume; calls %v", r.calls)
				}
				if indexOf(r.calls, "Stop:qdrant.service") != -1 {
					t.Fatalf("prior-absent cell must NOT stop a non-running qdrant service; calls %v", r.calls)
				}
			}
		})
	}
}

// TestRestoreQdrantForwardFailureRollsBackBothVolumes injects a non-pass prove
// AFTER the full forward apply (both volumes imported) and asserts the rollback
// restores BOTH volumes through the SAME clean-recreate ordering from their
// rollback tars (D-07 rollback symmetry).
func TestRestoreQdrantForwardFailureRollsBackBothVolumes(t *testing.T) {
	arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("qdrant-data"), nil)
	r, in := memInput(t, arch, true)
	r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}

	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack, got %+v", res)
	}
	// Four imports: forward owui+qdrant from the restore tars, rollback owui+qdrant
	// from the CAPTURED rollback tars.
	var imports []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "VolumeImport:") {
			imports = append(imports, c)
		}
	}
	if len(imports) != 4 {
		t.Fatalf("want 4 VolumeImports (forward+rollback × both volumes), got %v", imports)
	}
	wantRollback := map[string]bool{
		"VolumeImport:villa-openwebui:" + in.RollbackVolumeTar: false,
		"VolumeImport:qdrant-vol:" + in.RollbackQdrantTar:      false,
	}
	for _, c := range imports {
		if _, ok := wantRollback[c]; ok {
			wantRollback[c] = true
		}
	}
	for c, seen := range wantRollback {
		if !seen {
			t.Fatalf("rollback must re-import %q; imports %v", c, imports)
		}
	}
	// Rollback symmetry: each volume goes through clean-recreate TWICE (forward +
	// rollback) — two VolumeRm per volume.
	rmOwui, rmQdrant := 0, 0
	for _, c := range r.calls {
		switch c {
		case "VolumeRm:villa-openwebui":
			rmOwui++
		case "VolumeRm:qdrant-vol":
			rmQdrant++
		}
	}
	if rmOwui != 2 || rmQdrant != 2 {
		t.Fatalf("want 2 VolumeRm per volume (forward+rollback), got owui=%d qdrant=%d (%v)", rmOwui, rmQdrant, r.calls)
	}
	// The rollback qdrant import must itself follow the SAME ordering: the second
	// VolumeRm:qdrant-vol precedes the rollback import.
	iFirstRm := indexOf(r.calls, "VolumeRm:qdrant-vol")
	iSecondRm := indexAfter(r.calls, "VolumeRm:qdrant-vol", iFirstRm+1)
	iRbImp := indexOf(r.calls, "VolumeImport:qdrant-vol:"+in.RollbackQdrantTar)
	if !(iSecondRm != -1 && iSecondRm < iRbImp) {
		t.Fatalf("rollback qdrant import must be preceded by its own clean-recreate VolumeRm; calls %v", r.calls)
	}
}

// TestRestoreQdrantPriorAbsentRollbackRemovesForwardCreatedVolume is the
// Pitfall-4(a) cell: a memory-bearing backup restored onto a host with NO qdrant
// volume. A forward failure must roll back by REMOVING the forward-created
// volume (the volume analog of rollbackRemove) — never by importing a rollback
// tar that was never captured.
func TestRestoreQdrantPriorAbsentRollbackRemovesForwardCreatedVolume(t *testing.T) {
	arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("qdrant-data"), nil)
	r, in := memInput(t, arch, false /* volume absent */)
	r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}

	res := Restore(r.deps(), in)
	if !res.RolledBack {
		t.Fatalf("want RolledBack, got %+v", res)
	}
	// Never a capture export, never a rollback-tar import for the qdrant volume.
	if indexOf(r.calls, "VolumeExport:qdrant-vol") != -1 {
		t.Fatalf("prior-absent: no qdrant capture export may run; calls %v", r.calls)
	}
	if indexOf(r.calls, "VolumeImport:qdrant-vol:"+in.RollbackQdrantTar) != -1 {
		t.Fatalf("prior-absent: rollback must NOT import a never-captured tar; calls %v", r.calls)
	}
	// Rollback removes the forward-created volume: a VolumeRm:qdrant-vol AFTER the
	// forward import.
	iImp := indexOf(r.calls, "VolumeImport:qdrant-vol:"+in.TempQdrantTar)
	if iImp == -1 {
		t.Fatalf("forward qdrant import must have run; calls %v", r.calls)
	}
	if indexAfter(r.calls, "VolumeRm:qdrant-vol", iImp+1) == -1 {
		t.Fatalf("rollback must VolumeRm the forward-created qdrant volume; calls %v", r.calls)
	}
}

// TestRestoreRecallStateForwardAndRollback asserts the recall-state.json entry
// follows the usage/bench data-artifact discipline: forward atomic write to
// RecallDestPath; prior-absent + rollback ⇒ rollbackRemove; prior-present +
// rollback ⇒ the captured prior bytes are rewritten.
func TestRestoreRecallStateForwardAndRollback(t *testing.T) {
	t.Run("prior absent: rollback removes the forward-created file", func(t *testing.T) {
		arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, []byte("recall-state-from-archive"))
		r, in := memInput(t, arch, false)
		r.readFileErr = map[string]error{in.RecallDestPath: errors.New("not found")}
		r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}

		res := Restore(r.deps(), in)
		if !res.RolledBack {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		iW := indexOf(r.calls, "WriteFileAtomic:"+in.RecallDestPath)
		iR := indexOf(r.calls, "RemoveFile:"+in.RecallDestPath)
		if iW == -1 {
			t.Fatalf("forward path must write recall-state.json; calls %v", r.calls)
		}
		if iR == -1 || iR < iW {
			t.Fatalf("rollback must RemoveFile the forward-created recall-state.json AFTER the write; calls %v", r.calls)
		}
		if strings.Contains(res.Reason, "did not fully complete") {
			t.Fatalf("rollback should be COMPLETE, got %q", res.Reason)
		}
	})
	t.Run("prior present: rollback rewrites the captured prior bytes", func(t *testing.T) {
		arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, []byte("recall-state-from-archive"))
		r, in := memInput(t, arch, false)
		r.readFile[in.RecallDestPath] = []byte("prior-recall-state")
		r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}

		res := Restore(r.deps(), in)
		if !res.RolledBack {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		writes := 0
		for _, c := range r.calls {
			if c == "WriteFileAtomic:"+in.RecallDestPath {
				writes++
			}
		}
		if writes != 2 {
			t.Fatalf("want forward write + rollback rewrite of recall-state.json (2), got %d (%v)", writes, r.calls)
		}
		if indexOf(r.calls, "RemoveFile:"+in.RecallDestPath) != -1 {
			t.Fatalf("prior-present rollback must REWRITE, never remove; calls %v", r.calls)
		}
	})
	t.Run("entry absent: recall-state untouched", func(t *testing.T) {
		arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil)
		r, in := memInput(t, arch, false)
		res := Restore(r.deps(), in)
		if !res.Restored {
			t.Fatalf("want Restored, got %+v", res)
		}
		if res.RecallStateRestored {
			t.Fatalf("RecallStateRestored must be false for an entry-free archive")
		}
		for _, c := range r.calls {
			if strings.Contains(c, in.RecallDestPath) {
				t.Fatalf("entry-absent restore must never touch recall-state.json; calls %v", r.calls)
			}
		}
	})
}

// TestRestoreResultMemoryFlags asserts the honest-reporting fields (OQ1 locked:
// report, never extend Prove): QdrantRestored/RecallStateRestored mirror the
// archive's entries and RestoredMemoryEnabled reflects the RESTORED config's
// memory posture (Pitfall 5).
func TestRestoreResultMemoryFlags(t *testing.T) {
	arch := buildArchiveMem(t, baseManifest(), memCfgTOML, []byte("owui-data"), []byte("qdrant-data"), []byte("recall-state"))
	r, in := memInput(t, arch, true)
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if !res.QdrantRestored || !res.RecallStateRestored {
		t.Fatalf("memory-bearing archive must report both entries restored: %+v", res)
	}
	if !res.RestoredMemoryEnabled {
		t.Fatalf("RestoredMemoryEnabled must reflect the restored config (memory_enabled = true): %+v", res)
	}

	arch = buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r2, in2 := memInput(t, arch, true)
	res = Restore(r2.deps(), in2)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v", res)
	}
	if res.QdrantRestored || res.RecallStateRestored || res.RestoredMemoryEnabled {
		t.Fatalf("memory-free archive must report nothing restored and a disabled posture: %+v", res)
	}
}

// TestRestoreV1ManifestStillRestores is the backward-compat fixture for the
// backupSchemaVersion 1→2 bump (D-04 doctrine): a v1 archive (SchemaVersion 1,
// no memory entries, no embedding fields) must restore cleanly under the v2
// gate (m.SchemaVersion <= backupSchemaVersion) with no false skew alarm and
// zero qdrant calls.
func TestRestoreV1ManifestStillRestores(t *testing.T) {
	m := baseManifest()
	m.SchemaVersion = 1 // a pre-bump v1 manifest
	arch := buildArchive(t, m, validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := memInput(t, arch, true /* memory-on host */)
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("a v1 backup must stay restorable under backupSchemaVersion 2, got %+v", res)
	}
	if res.QdrantRestored || res.RecallStateRestored {
		t.Fatalf("a v1 backup carries no memory entries: %+v", res)
	}
	if qc := qdrantCalls(r.calls); len(qc) != 0 {
		t.Fatalf("a v1 restore on a memory-on host must leave Qdrant untouched, got %v", qc)
	}
}

func TestRestoreNonPassProveRollsBack(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL (CPU fallback)"}
	res := Restore(r.deps(), in)
	if !res.RolledBack || res.FailedStep != "prove" {
		t.Fatalf("want RolledBack at prove on a non-pass verdict, got %+v", res)
	}
	if res.Prove.Status == ProveStatusPass {
		t.Fatalf("prove verdict must be carried through (non-pass), got %+v", res.Prove)
	}
}
