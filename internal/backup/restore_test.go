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
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
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
	writeCrushErr   error
	writeSearxngErr error
	removeFileErr   error
	readFile        map[string][]byte
	readFileErr     map[string]error

	// searxngWrites captures the path→bytes the WriteSearxngSettings seam received
	// (Phase 34, SURF-07), so a test can assert the restored content. When
	// searxngRealWriteDir is set, the seam ALSO performs a REAL on-disk write under that
	// dir at 0600 (mirroring the live wiring) so a test can assert the actual file mode
	// (T-34-05) — the pure core can't reach the cmd-tier closure otherwise.
	searxngWrites       map[string][]byte
	searxngRealWriteDir string

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
		// WriteFile is now ONE seam, so the fake routes on the destination to keep the
		// per-artifact call log the assertions read. That mirrors the live wiring, which
		// also picks its containment guard by destination.
		WriteFile: func(p string, data []byte) error {
			switch {
			case strings.HasSuffix(p, "crush.json"):
				r.log("WriteCrushConfig:" + p)
				return r.writeCrushErr
			case strings.HasSuffix(p, "settings.yml"):
				r.log("WriteSearxngSettings:" + p)
				if r.writeSearxngErr != nil {
					return r.writeSearxngErr
				}
				if r.searxngWrites == nil {
					r.searxngWrites = map[string][]byte{}
				}
				r.searxngWrites[p] = append([]byte(nil), data...)
				// Optional REAL on-disk write at 0600 so a test can assert the file mode
				// (T-34-05) — the seam mirrors the live cmd-tier wiring's MkdirAll 0700 +
				// WriteFile 0600 discipline.
				if r.searxngRealWriteDir != "" {
					if err := os.MkdirAll(r.searxngRealWriteDir, 0o700); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(r.searxngRealWriteDir, "settings.yml"), data, 0o600); err != nil {
						return err
					}
				}
				return nil
			case strings.HasPrefix(p, "/tmp/"):
				r.log("WriteTempFile:" + p)
				return r.writeTempErr
			default:
				r.log("WriteFileAtomic:" + p)
				return r.writeFileErr
			}
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
	dataEntries := []archiveEntry{{name: EntryConfig, data: cfg}, {name: EntryOpenWebUIVolume, data: owui}}
	mj := manifestJSONFor(t, dataEntries)
	// Two config.toml members (duplicate name) after the manifest.
	arch := rawMultiTar(t, []archiveEntry{
		{name: EntryManifest, data: mj},
		{name: EntryConfig, data: cfg},
		{name: EntryConfig, data: []byte("model = \"other\"\n")},
		{name: EntryOpenWebUIVolume, data: owui},
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
	dataEntries := []archiveEntry{{name: EntryConfig, data: cfg}, {name: EntryOpenWebUIVolume, data: owui}}
	mj := manifestJSONFor(t, dataEntries)
	arch := rawMultiTar(t, []archiveEntry{
		{name: EntryManifest, data: mj},
		{name: EntryConfig, data: cfg},
		{name: EntryOpenWebUIVolume, data: owui},
		{name: "unexpected.txt", data: []byte("stowaway")}, // not in the manifest
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
	dataEntries := []archiveEntry{{name: EntryConfig, data: cfg}, {name: EntryOpenWebUIVolume, data: owui}}
	mj := manifestJSONFor(t, dataEntries)
	// Data entry BEFORE the manifest.
	arch := rawMultiTar(t, []archiveEntry{
		{name: EntryConfig, data: cfg},
		{name: EntryManifest, data: mj},
		{name: EntryOpenWebUIVolume, data: owui},
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

// TestRestoreProveFailRollbackQuiescesBeforeVolumeRm is the CR-01 regression with
// LIVE-HOST FIDELITY the no-op fakes lacked: on a real host a RUNNING container
// holds its volume, so `podman volume rm` fails "volume is in use" unless the
// owning service is stopped first. The forward path starts Open WebUI (and
// Qdrant) at step (5) BEFORE the Prove gate, so a prove-triggered rollback
// arrives with both services RUNNING — the rollback MUST quiesce before its
// clean-recreate VolumeRm, or the prior data can never be re-imported. This fake
// tracks running state through the Stop/Start seams and fails VolumeRm in-use,
// which made the pre-fix rollback structurally incapable of completing.
func TestRestoreProveFailRollbackQuiescesBeforeVolumeRm(t *testing.T) {
	arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("qdrant-data"), nil)
	r, in := memInput(t, arch, true /* prior qdrant volume exists */)
	r.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL (CPU fallback)"}

	running := map[string]bool{
		"villa-openwebui.service": true,
		"qdrant.service":          true,
	}
	serviceFor := map[string]string{
		"villa-openwebui": "villa-openwebui.service",
		"qdrant-vol":      "qdrant.service",
	}
	d := r.deps()
	origStop, origStart, origRm := d.Stop, d.Start, d.VolumeRm
	d.Stop = func(s string) error { running[s] = false; return origStop(s) }
	d.Start = func(s string) error { running[s] = true; return origStart(s) }
	d.VolumeRm = func(name string) error {
		if running[serviceFor[name]] {
			return errors.New("volume is being used by the following container(s)")
		}
		return origRm(name)
	}

	res := Restore(d, in)
	if !res.RolledBack || res.FailedStep != "prove" {
		t.Fatalf("want RolledBack at prove, got %+v (calls %v)", res, r.calls)
	}
	// The rollback must have quiesced FIRST, so every rollback VolumeRm ran against
	// a stopped service and the rollback completed CLEANLY.
	if res.RollbackIncomplete || strings.Contains(res.Reason, "did not fully complete") {
		t.Fatalf("rollback must quiesce before VolumeRm and complete cleanly, got %+v (calls %v)", res, r.calls)
	}
	// Ordering inside the rollback phase: after Prove, Stop(service) precedes the
	// rollback VolumeRm for BOTH volumes.
	iProve := indexOf(r.calls, "Prove:")
	if iProve == -1 {
		t.Fatalf("missing Prove call: %v", r.calls)
	}
	for _, pair := range [][2]string{
		{"Stop:villa-openwebui.service", "VolumeRm:villa-openwebui"},
		{"Stop:qdrant.service", "VolumeRm:qdrant-vol"},
	} {
		iStop := indexAfter(r.calls, pair[0], iProve)
		iRm := indexAfter(r.calls, pair[1], iProve)
		if iStop == -1 || iRm == -1 || iStop > iRm {
			t.Fatalf("rollback must %s before %s (after Prove at %d); calls %v", pair[0], pair[1], iProve, r.calls)
		}
	}
	// And the rollback re-imports of BOTH captured tars actually ran (the whole
	// point of the quiesce — without it these could never succeed on a live host).
	for _, want := range []string{
		"VolumeImport:villa-openwebui:" + in.RollbackVolumeTar,
		"VolumeImport:qdrant-vol:" + in.RollbackQdrantTar,
	} {
		if indexAfter(r.calls, want, iProve) == -1 {
			t.Fatalf("rollback must re-import %q; calls %v", want, r.calls)
		}
	}
}

// TestRestoreQdrantExistsUnknownFailsClosed is the WR-02 regression: when the
// archive carries a qdrant entry but the current volume's existence could NOT be
// evaluated (the tri-state check's unknown cell — a transient podman failure),
// the restore must REFUSE before any mutation. Pre-fix, the fail-soft check
// collapsed Unknown into "absent", skipping capture + quiesce and routing the
// destructive VolumeRm at a possibly-real, uncaptured qdrant volume. A
// memory-FREE archive stays restorable on the same host (zero qdrant calls).
func TestRestoreQdrantExistsUnknownFailsClosed(t *testing.T) {
	t.Run("qdrant entry present + existence unknown refuses with zero side effects", func(t *testing.T) {
		arch := buildArchiveMem(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("qdrant-data"), nil)
		r, in := memInput(t, arch, false)
		in.QdrantVolumeUnknown = true
		res := Restore(r.deps(), in)
		if !res.Refused || res.FailedStep != "capture" {
			t.Fatalf("want Refused at capture on an unknown qdrant existence, got %+v", res)
		}
		if !strings.Contains(res.Reason, "could not determine") {
			t.Fatalf("refusal must carry the unknown-existence remediation, got %q", res.Reason)
		}
		if hasMutate(r.calls) {
			t.Fatalf("unknown-existence refusal must have ZERO mutate side effects, got %v", r.calls)
		}
	})
	t.Run("memory-free archive restores despite an unknown qdrant existence", func(t *testing.T) {
		arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
		r, in := memInput(t, arch, false)
		in.QdrantVolumeUnknown = true
		res := Restore(r.deps(), in)
		if !res.Restored {
			t.Fatalf("a memory-free archive must restore regardless of the qdrant signal, got %+v", res)
		}
		if qc := qdrantCalls(r.calls); len(qc) != 0 {
			t.Fatalf("memory-free restore must make zero qdrant calls, got %v", qc)
		}
	})
}

// TestRestoreRollbackIncompleteSetsFlag asserts the Result.RollbackIncomplete flag
// (CR-01) mirrors the honest rollback-incomplete Reason, so the cmd tier can
// preserve the rollback tars without string-matching the Reason text.
func TestRestoreRollbackIncompleteSetsFlag(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	r.volumeImportErr = errors.New("import boom")
	r.saveErrOnce = map[int]error{2: errors.New("save prior boom")}
	res := Restore(r.deps(), in)
	if !res.RolledBack || !res.RollbackIncomplete {
		t.Fatalf("an errored rollback step must set RollbackIncomplete, got %+v", res)
	}

	// A clean rollback leaves the flag false.
	arch = buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r2, in2 := baseInput(t, arch)
	r2.prove = ProveVerdict{Status: "fail", Detail: "residency FAIL"}
	res = Restore(r2.deps(), in2)
	if !res.RolledBack || res.RollbackIncomplete {
		t.Fatalf("a clean rollback must leave RollbackIncomplete false, got %+v", res)
	}
}

// buildAgentArchive assembles a valid archive that includes the OPTIONAL Phase-28
// crush.json entry and an ExcludedAgent manifest record (SURF-03/D-08), with the
// manifest-first + correct per-entry SHA-256 discipline so the verify pass passes.
func buildAgentArchive(t *testing.T, m Manifest, cfgTOML, owui, crush []byte) []byte {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	data := []e{{EntryConfig, cfgTOML}, {EntryOpenWebUIVolume, owui}}
	if crush != nil {
		data = append(data, e{EntryCrushConfig, crush})
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

// TestRestoreAgentOnRestoresCrushAndReportsExcludedAgent asserts an agent-on
// archive (SURF-03/D-08) restores the crush.json via the dedicated
// WriteCrushConfig seam AND surfaces the EXCLUDED agent binary identity on the
// Result for the operator to re-stage (re-download the pinned release).
func TestRestoreAgentOnRestoresCrushAndReportsExcludedAgent(t *testing.T) {
	m := baseManifest()
	m.ExcludedAgent = &ExcludedAgent{SHA256: "on-disk-sha", Version: "v0.76.0", PinSHA256: "pin-sha"}
	arch := buildAgentArchive(t, m, validCfgTOML, []byte("owui-data"), []byte(`{"provider":"local"}`))
	r, in := baseInput(t, arch)
	in.CrushConfigDestPath = "/home/u/.config/crush/crush.json"

	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if !res.CrushConfigRestored {
		t.Fatalf("CrushConfigRestored must be true on an agent-on archive, got %+v", res)
	}
	if res.CrushConfigSkipped {
		t.Fatalf("a written crush.json must NOT report CrushConfigSkipped, got %+v", res)
	}
	if indexOf(r.calls, "WriteCrushConfig:") == -1 {
		t.Fatalf("crush.json restore must go through WriteCrushConfig (out-of-store-root), calls=%v", r.calls)
	}
	if res.ExcludedAgent == nil || res.ExcludedAgent.SHA256 != "on-disk-sha" ||
		res.ExcludedAgent.Version != "v0.76.0" || res.ExcludedAgent.PinSHA256 != "pin-sha" {
		t.Fatalf("ExcludedAgent identity must be surfaced for re-stage, got %+v", res.ExcludedAgent)
	}
}

// TestRestoreAgentOffNoCrushNoExcludedAgent asserts an agent-off archive (no
// crush.json entry, nil ExcludedAgent) restores with ZERO WriteCrushConfig calls
// and a nil ExcludedAgent — layout-identical to a v2 restore.
func TestRestoreAgentOffNoCrushNoExcludedAgent(t *testing.T) {
	arch := buildAgentArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil)
	r, in := baseInput(t, arch)
	in.CrushConfigDestPath = "/home/u/.config/crush/crush.json"

	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v", res)
	}
	if res.CrushConfigRestored {
		t.Fatalf("agent-off archive must NOT report CrushConfigRestored, got %+v", res)
	}
	if res.ExcludedAgent != nil {
		t.Fatalf("agent-off archive must surface nil ExcludedAgent, got %+v", res.ExcludedAgent)
	}
	if indexOf(r.calls, "WriteCrushConfig:") != -1 {
		t.Fatalf("agent-off restore must make ZERO WriteCrushConfig calls, calls=%v", r.calls)
	}
}

// TestRestoreAgentOnArchiveOntoAgentOffInstallSkipsCrush asserts WR-02: an
// agent-ON archive (carries crush.json) restored onto an agent-OFF current
// install (no CrushConfigDestPath wired) does NOT write crush.json, reports
// CrushConfigRestored=false (no false-green) AND CrushConfigSkipped=true so the
// cmd tier can warn the operator the entry was present but NOT applied.
func TestRestoreAgentOnArchiveOntoAgentOffInstallSkipsCrush(t *testing.T) {
	m := baseManifest()
	m.ExcludedAgent = &ExcludedAgent{SHA256: "on-disk-sha", Version: "v0.76.0", PinSHA256: "pin-sha"}
	arch := buildAgentArchive(t, m, validCfgTOML, []byte("owui-data"), []byte(`{"provider":"local"}`))
	r, in := baseInput(t, arch)
	// Current install is agent-off → the cmd tier wires NO destination.
	in.CrushConfigDestPath = ""

	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if res.CrushConfigRestored {
		t.Fatalf("WR-02: crush.json was NOT written (no dest) — CrushConfigRestored must be false, got %+v", res)
	}
	if !res.CrushConfigSkipped {
		t.Fatalf("WR-02: archive carried crush.json but it was skipped — CrushConfigSkipped must be true, got %+v", res)
	}
	if indexOf(r.calls, "WriteCrushConfig:") != -1 {
		t.Fatalf("WR-02: no destination wired must make ZERO WriteCrushConfig calls, calls=%v", r.calls)
	}
}

// TestRestoreAgentIdentityDriftFailsClosed asserts a TAMPERED crush.json entry
// (whose bytes do not match the manifest's recorded SHA-256) is a fail-closed
// BLOCK with ZERO side effects (SURF-03/D-08; T-28-02-01) — the same verify gate
// every archive member is held to. The agent identity record is verified by the
// same SHA-256 pass; a drifted entry must never be applied.
func TestRestoreAgentIdentityDriftFailsClosed(t *testing.T) {
	m := baseManifest()
	m.ExcludedAgent = &ExcludedAgent{SHA256: "on-disk-sha", Version: "v0.76.0", PinSHA256: "pin-sha"}
	arch := buildAgentArchive(t, m, validCfgTOML, []byte("owui-data"), []byte(`{"provider":"local"}`))
	// Corrupt the crush.json body AFTER its checksum was recorded by flipping the
	// archive bytes: re-marshal a manifest whose crush.json checksum no longer
	// matches the body we ship.
	tampered := tamperEntry(t, arch, EntryCrushConfig)

	r, in := baseInput(t, tampered)
	in.CrushConfigDestPath = "/home/u/.config/crush/crush.json"
	res := Restore(r.deps(), in)
	if !res.Refused {
		t.Fatalf("a drifted crush.json must be a fail-closed Refused, got %+v", res)
	}
	if hasMutate(r.calls) {
		t.Fatalf("a fail-closed verify refusal must have ZERO side effects, calls=%v", r.calls)
	}
}

// buildSearxngArchive assembles a valid archive that includes the OPTIONAL Phase-34
// settings.yml entry (SURF-07), with the manifest-first + correct per-entry SHA-256
// discipline so the verify pass passes. A nil settings omits the entry (web-off backup).
func buildSearxngArchive(t *testing.T, m Manifest, cfgTOML, owui, settings []byte) []byte {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	data := []e{{EntryConfig, cfgTOML}, {EntryOpenWebUIVolume, owui}}
	if settings != nil {
		data = append(data, e{EntrySearxngSettings, settings})
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

// TestRestoreSearxngSettings asserts the OPTIONAL Phase-34 settings.yml entry (SURF-07)
// behaves exactly like the crush.json optional entry on restore:
//   - present + a destination wired (web-search-on current install) → re-written through
//     the dedicated WriteSearxngSettings seam at mode EXACTLY 0600 (T-34-05, never widened)
//   - present but NO destination wired (web-search-off current install) → NOT applied,
//     reported as SearxngSettingsSkipped (no false-green, WR-02 mirror)
//   - absent from the archive → not-present (ZERO WriteSearxngSettings calls)
//   - the entry is SHA-256-verified through the SAME verify path as every member: a
//     tampered settings.yml is a fail-closed Refused with zero side effects (T-34-07)
func TestRestoreSearxngSettings(t *testing.T) {
	const settingsBody = "use_default_settings: true\nserver:\n  secret_key: rendered-secret\n"

	t.Run("present → written 0600", func(t *testing.T) {
		arch := buildSearxngArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte(settingsBody))
		r, in := baseInput(t, arch)
		dest := filepath.Join(t.TempDir(), "searxng", "settings.yml")
		in.SearxngSettingsDestPath = dest
		r.searxngRealWriteDir = filepath.Dir(dest)

		res := Restore(r.deps(), in)
		if !res.Restored {
			t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
		}
		if !res.SearxngSettingsRestored {
			t.Fatalf("SearxngSettingsRestored must be true on a web-search-on archive, got %+v", res)
		}
		if res.SearxngSettingsSkipped {
			t.Fatalf("a written settings.yml must NOT report SearxngSettingsSkipped, got %+v", res)
		}
		if indexOf(r.calls, "WriteSearxngSettings:") == -1 {
			t.Fatalf("settings.yml restore must go through WriteSearxngSettings (out-of-store-root), calls=%v", r.calls)
		}
		if got := string(r.searxngWrites[dest]); got != settingsBody {
			t.Fatalf("restored settings.yml content mismatch: got %q want %q", got, settingsBody)
		}
		// The load-bearing mode assertion (T-34-05): the restored file is EXACTLY 0600,
		// never widened — it holds the rendered SEARXNG_SECRET.
		fi, err := os.Stat(filepath.Join(r.searxngRealWriteDir, "settings.yml"))
		if err != nil {
			t.Fatalf("stat restored settings.yml: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("T-34-05: restored settings.yml mode must be EXACTLY 0600, got %o", perm)
		}
	})

	t.Run("absent → not present", func(t *testing.T) {
		arch := buildSearxngArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil)
		r, in := baseInput(t, arch)
		in.SearxngSettingsDestPath = filepath.Join(t.TempDir(), "searxng", "settings.yml")

		res := Restore(r.deps(), in)
		if !res.Restored {
			t.Fatalf("want Restored, got %+v", res)
		}
		if res.SearxngSettingsRestored {
			t.Fatalf("web-search-off archive must NOT report SearxngSettingsRestored, got %+v", res)
		}
		if res.SearxngSettingsSkipped {
			t.Fatalf("an archive with NO settings.yml must NOT report SearxngSettingsSkipped, got %+v", res)
		}
		if indexOf(r.calls, "WriteSearxngSettings:") != -1 {
			t.Fatalf("an absent settings.yml must make ZERO WriteSearxngSettings calls, calls=%v", r.calls)
		}
	})

	t.Run("present onto web-off install → skipped", func(t *testing.T) {
		arch := buildSearxngArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte(settingsBody))
		r, in := baseInput(t, arch)
		in.SearxngSettingsDestPath = "" // web-search-off current install: no dest wired

		res := Restore(r.deps(), in)
		if !res.Restored {
			t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
		}
		if res.SearxngSettingsRestored {
			t.Fatalf("no destination wired → SearxngSettingsRestored must be false, got %+v", res)
		}
		if !res.SearxngSettingsSkipped {
			t.Fatalf("archive carried settings.yml but it was skipped → SearxngSettingsSkipped must be true, got %+v", res)
		}
		if indexOf(r.calls, "WriteSearxngSettings:") != -1 {
			t.Fatalf("no destination wired must make ZERO WriteSearxngSettings calls, calls=%v", r.calls)
		}
	})

	t.Run("tampered settings.yml → fail-closed", func(t *testing.T) {
		arch := buildSearxngArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte(settingsBody))
		tampered := tamperEntry(t, arch, EntrySearxngSettings)
		r, in := baseInput(t, tampered)
		in.SearxngSettingsDestPath = filepath.Join(t.TempDir(), "searxng", "settings.yml")

		res := Restore(r.deps(), in)
		if !res.Refused {
			t.Fatalf("a drifted settings.yml must be a fail-closed Refused, got %+v", res)
		}
		if hasMutate(r.calls) {
			t.Fatalf("a fail-closed verify refusal must have ZERO side effects, calls=%v", r.calls)
		}
	})
}

// tamperEntry rewrites the named entry's body to a value that no longer matches
// its manifest-recorded SHA-256, driving the readAndVerify mismatch path. It
// preserves the manifest (and thus the now-stale checksum) and replaces only the
// target entry's bytes.
func tamperEntry(t *testing.T, arch []byte, name string) []byte {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(arch))
	var entries []archiveEntry
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		body, _ := io.ReadAll(tr)
		if h.Name == name {
			body = append([]byte("TAMPER"), body...)
		}
		entries = append(entries, archiveEntry{name: h.Name, data: body})
	}
	var buf bytes.Buffer
	if err := writeArchive(&buf, entries); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	return buf.Bytes()
}
