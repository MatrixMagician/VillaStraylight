package backup

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"testing"
)

// baseManifest / baseCurrent are a fully-matching manifest/current pair; tests
// mutate one field to exercise a single classification.
func baseManifest() Manifest {
	return BuildManifest(ManifestInput{
		VillaVersion:        "v1.2.0",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		InferenceImage:      "inf@sha256:aaa",
		OpenWebUIImage:      "owui@sha256:bbb",
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  1,
		BenchSchemaVersion:  1,
	})
}

func baseCurrent() CurrentInstall {
	return CurrentInstall{
		VillaVersion:        "v1.2.0",
		InferenceImage:      "inf@sha256:aaa",
		OpenWebUIImage:      "owui@sha256:bbb",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  1,
		BenchSchemaVersion:  1,
	}
}

// TestSkewClassification is the table-driven BAK-03 / D-08 classifier test.
func TestSkewClassification(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(m *Manifest, c *CurrentInstall)
		wantBlock bool
		wantWarnN int
		wantField string // a warning Field that MUST be present (when wantWarnN>0)
	}{
		{
			name:   "fully matching: no findings",
			mutate: func(m *Manifest, c *CurrentInstall) {},
		},
		{
			name:      "villa version mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.VillaVersion = "v9.9.9" },
			wantWarnN: 1,
			wantField: "villa_version",
		},
		{
			name:      "inference digest mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.InferenceImage = "inf@sha256:zzz" },
			wantWarnN: 1,
			wantField: "inference_image",
		},
		{
			name:      "owui digest mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.OpenWebUIImage = "owui@sha256:zzz" },
			wantWarnN: 1,
			wantField: "openwebui_image",
		},
		{
			name:      "host fingerprint mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.Host.Kernel = "6.99.0" },
			wantWarnN: 1,
			wantField: "host",
		},
		{
			name:      "older usage store schema -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.UsageSchemaVersion = 2 },
			wantWarnN: 1,
			wantField: "usage_schema_version",
		},
		{
			name:      "checksum failed -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.ChecksumFailed = true },
			wantBlock: true,
		},
		{
			name:      "newer manifest schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.SchemaVersion = backupSchemaVersion + 1 },
			wantBlock: true,
		},
		{
			name:      "unreadable manifest schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.SchemaVersion = 0 },
			wantBlock: true,
		},
		{
			name:      "newer config store schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.ConfigSchemaVersion = 5; c.ConfigSchemaVersion = 1 },
			wantBlock: true,
		},
		{
			name:      "newer bench store schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.BenchSchemaVersion = 5; c.BenchSchemaVersion = 1 },
			wantBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := baseManifest()
			c := baseCurrent()
			tt.mutate(&m, &c)
			v := CompareSkew(m, c)

			if v.Block != tt.wantBlock {
				t.Fatalf("Block = %v, want %v (reason=%q)", v.Block, tt.wantBlock, v.BlockReason)
			}
			if tt.wantBlock {
				if v.BlockReason == "" {
					t.Errorf("Block=true but BlockReason is empty")
				}
				// A BLOCK short-circuits — no warnings should accumulate.
				if len(v.Warnings) != 0 {
					t.Errorf("Block=true but got %d warnings", len(v.Warnings))
				}
				return
			}
			if len(v.Warnings) != tt.wantWarnN {
				t.Fatalf("got %d warnings, want %d: %+v", len(v.Warnings), tt.wantWarnN, v.Warnings)
			}
			if tt.wantField != "" {
				found := false
				for _, w := range v.Warnings {
					if w.Field == tt.wantField {
						found = true
						if w.Remediation == "" {
							t.Errorf("warning %q has empty remediation", w.Field)
						}
					}
				}
				if !found {
					t.Errorf("no warning with Field=%q in %+v", tt.wantField, v.Warnings)
				}
			}
		})
	}
}

// TestSkewMatchingNoFindings asserts a fully-matching manifest yields the zero
// verdict (no Block, no Warnings) — the happy path.
func TestSkewMatchingNoFindings(t *testing.T) {
	v := CompareSkew(baseManifest(), baseCurrent())
	if v.Block || len(v.Warnings) != 0 {
		t.Fatalf("matching manifest produced findings: %+v", v)
	}
}

// ---------------------------------------------------------------------------
// Backup() orchestrator tests (BAK-01, D-05). A fakeBackupDeps records the call
// ordering and serves canned bytes so the pure quiesce→export→assemble flow is
// driven with no live host.
// ---------------------------------------------------------------------------

// fakeBackupDeps records the seam call order and serves canned file bytes.
type fakeBackupDeps struct {
	calls       []string          // ordered seam-call log
	files       map[string][]byte // path -> bytes for ReadFile
	exportErr   error             // injected VolumeExport failure
	exportWrote bool              // whether VolumeExport "wrote" the temp tar
}

func newFakeBackupDeps() *fakeBackupDeps {
	return &fakeBackupDeps{files: map[string][]byte{}}
}

func (f *fakeBackupDeps) deps() Deps {
	return Deps{
		OpenWebUIServiceName: "villa-openwebui.service",
		Stop: func(s string) error {
			f.calls = append(f.calls, "stop:"+s)
			return nil
		},
		Start: func(s string) error {
			f.calls = append(f.calls, "start:"+s)
			return nil
		},
		VolumeExport: func(name, out string) error {
			f.calls = append(f.calls, "export:"+name)
			if f.exportErr != nil {
				return f.exportErr
			}
			f.exportWrote = true
			f.files[out] = []byte("OWUI-VOLUME-TAR-BYTES")
			return nil
		},
		ReadFile: func(p string) ([]byte, error) {
			f.calls = append(f.calls, "read:"+p)
			b, ok := f.files[p]
			if !ok {
				return nil, os.ErrNotExist
			}
			return b, nil
		},
	}
}

func baseBackupInput(w io.Writer) BackupInput {
	return BackupInput{
		CreatedAt:           "2026-06-07T00:00:00Z",
		VillaVersion:        "v1.2.0",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		InferenceImage:      "inf@sha256:aaa",
		OpenWebUIImage:      "owui@sha256:bbb",
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  3,
		BenchSchemaVersion:  4,
		OutputPath:          "/tmp/villa-backup.tar",
		OutputWriter:        w,
		OpenWebUIVolumeName: "villa-openwebui",
		TempVolumeTar:       "/tmp/owui-vol.tar",
		ConfigPath:          "/cfg/config.toml",
		UsagePath:           "/data/usage.json",
		BenchReportsPath:    "/data/bench-reports.jsonl",
		ExcludedModels: []ExcludedModel{
			{ID: "qwen3-30b", Quant: "Q4_K_M", Ctx: "8192", Source: "catalog"},
		},
		FileMissing: os.IsNotExist,
	}
}

// archiveNames reads back the assembled tar and returns its entry names in order.
func archiveNames(t *testing.T, b []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(b))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		names = append(names, h.Name)
		if _, err := io.Copy(io.Discard, tr); err != nil {
			t.Fatalf("drain tar body: %v", err)
		}
	}
	return names
}

// TestBackupAssemblesArchive asserts the happy-path: stop-before-export ordering,
// the exact entry-name set (manifest FIRST, single bench-reports.jsonl, NO models
// volume), and that the manifest carries the injected seam digests + accessor-
// sourced store schema versions + excluded-model identities.
func TestBackupAssemblesArchive(t *testing.T) {
	f := newFakeBackupDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/data/usage.json"] = []byte(`{"schema_version":3}`)
	f.files["/data/bench-reports.jsonl"] = []byte(`{"schema_version":4}` + "\n")

	var out bytes.Buffer
	res, err := Backup(f.deps(), baseBackupInput(&out))
	if err != nil {
		t.Fatalf("Backup returned err: %v (result %+v)", err, res)
	}

	// Ordering: stop MUST come before export; export before any read; start (the
	// deferred restart) fires last.
	stopIdx, exportIdx, startIdx := -1, -1, -1
	for i, c := range f.calls {
		switch {
		case c == "stop:villa-openwebui.service":
			stopIdx = i
		case c == "export:villa-openwebui":
			exportIdx = i
		case c == "start:villa-openwebui.service":
			startIdx = i
		}
	}
	if !(stopIdx >= 0 && exportIdx > stopIdx) {
		t.Fatalf("expected stop before export, calls=%v", f.calls)
	}
	if !(startIdx > exportIdx) {
		t.Fatalf("expected deferred restart (start) after export, calls=%v", f.calls)
	}

	// Entry set: manifest FIRST, exactly the 5 expected names, NO models volume.
	names := archiveNames(t, out.Bytes())
	if len(names) == 0 || names[0] != EntryManifest {
		t.Fatalf("manifest.json must be first; got %v", names)
	}
	want := map[string]bool{
		EntryManifest:        true,
		EntryConfig:          true,
		EntryOpenWebUIVolume: true,
		EntryUsage:           true,
		EntryBenchReports:    true,
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
		if n == "models-volume.tar" || n == "villa-models.tar" {
			t.Fatalf("models volume must be excluded; found %q", n)
		}
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("missing expected entry %q in %v", n, names)
		}
	}
	if len(names) != len(want) {
		t.Fatalf("unexpected entry count: got %v", names)
	}

	// Manifest carries injected seam digests + accessor-sourced store schema versions.
	var mBytes []byte
	tr := tar.NewReader(bytes.NewReader(out.Bytes()))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == EntryManifest {
			mBytes, _ = io.ReadAll(tr)
		} else {
			_, _ = io.Copy(io.Discard, tr)
		}
	}
	var m Manifest
	if err := json.Unmarshal(mBytes, &m); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if m.InferenceImage != "inf@sha256:aaa" || m.OpenWebUIImage != "owui@sha256:bbb" {
		t.Fatalf("manifest digests not seam-sourced: %+v", m)
	}
	if m.UsageSchemaVersion != 3 || m.BenchSchemaVersion != 4 {
		t.Fatalf("manifest store schema versions not accessor-sourced: %+v", m)
	}
	if len(m.ExcludedModels) != 1 || m.ExcludedModels[0].ID != "qwen3-30b" {
		t.Fatalf("excluded-model identity not recorded: %+v", m.ExcludedModels)
	}
	// Per-entry checksums recorded for every non-manifest entry.
	csum := map[string]bool{}
	for _, e := range m.Entries {
		csum[e.Name] = true
		if e.SHA256 == "" {
			t.Fatalf("empty checksum for %q", e.Name)
		}
	}
	for _, n := range []string{EntryConfig, EntryOpenWebUIVolume, EntryUsage, EntryBenchReports} {
		if !csum[n] {
			t.Fatalf("missing checksum for %q", n)
		}
	}
	if res.Refused || res.RolledBack {
		t.Fatalf("unexpected non-success result: %+v", res)
	}
}

// TestBackupDeferredRestartFiresOnExportError asserts the OWUI service is restarted
// (best-effort defer) even when the volume export fails mid-backup (D-05).
func TestBackupDeferredRestartFiresOnExportError(t *testing.T) {
	f := newFakeBackupDeps()
	f.exportErr = errors.New("export boom")

	var out bytes.Buffer
	_, err := Backup(f.deps(), baseBackupInput(&out))
	if err == nil {
		t.Fatal("expected export error to propagate")
	}
	// The deferred restart MUST still have fired.
	sawStart := false
	for _, c := range f.calls {
		if c == "start:villa-openwebui.service" {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatalf("deferred restart did not fire on export error, calls=%v", f.calls)
	}
}

// TestBackupSkipsAbsentDataDirArtifacts asserts an absent usage.json / bench file
// is skipped (not fatal): the archive still assembles with the present entries.
func TestBackupSkipsAbsentDataDirArtifacts(t *testing.T) {
	f := newFakeBackupDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	// usage.json and bench-reports.jsonl deliberately absent.

	var out bytes.Buffer
	if _, err := Backup(f.deps(), baseBackupInput(&out)); err != nil {
		t.Fatalf("Backup err with absent optional files: %v", err)
	}
	names := archiveNames(t, out.Bytes())
	sort.Strings(names)
	wantSet := []string{EntryConfig, EntryManifest, EntryOpenWebUIVolume}
	sort.Strings(wantSet)
	if len(names) != len(wantSet) {
		t.Fatalf("expected only present entries %v, got %v", wantSet, names)
	}
	for i := range wantSet {
		if names[i] != wantSet[i] {
			t.Fatalf("entry mismatch: got %v want %v", names, wantSet)
		}
	}
}
