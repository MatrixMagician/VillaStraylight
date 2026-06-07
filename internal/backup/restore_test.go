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
		for _, m := range []string{"SaveConfig", "VolumeRm", "EnsureVolume", "VolumeImport", "ReconcileAndWrite", "WriteFileAtomic", "Stop", "Start"} {
			if strings.HasPrefix(c, m) {
				return true
			}
		}
	}
	return false
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
