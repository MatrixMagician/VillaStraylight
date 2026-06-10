package backup

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
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
			// D-08: a confident embedding model/dim mismatch is exactly ONE
			// WARN-and-confirm finding (never silent, never auto-reindex).
			name: "embedding model+dim mismatch -> 1 embedding WARN",
			mutate: func(m *Manifest, c *CurrentInstall) {
				m.EmbeddingModel, m.EmbeddingDim = "nomic-embed-text-v1.5", 768
				c.EmbeddingModel, c.EmbeddingDim = "other-model", 512
			},
			wantWarnN: 1,
			wantField: "embedding",
		},
		{
			name: "embedding dim-only mismatch -> 1 embedding WARN",
			mutate: func(m *Manifest, c *CurrentInstall) {
				m.EmbeddingModel, m.EmbeddingDim = "nomic-embed-text-v1.5", 768
				c.EmbeddingModel, c.EmbeddingDim = "nomic-embed-text-v1.5", 512
			},
			wantWarnN: 1,
			wantField: "embedding",
		},
		{
			// Typed-Unknown: an old/memory-off backup never recorded an embedding
			// model — "not recorded" must NOT raise a false alarm (D-08).
			name: "old backup without embedding fields -> NO warning",
			mutate: func(m *Manifest, c *CurrentInstall) {
				c.EmbeddingModel, c.EmbeddingDim = "nomic-embed-text-v1.5", 768
			},
		},
		{
			name: "newer recall store schema -> BLOCK",
			mutate: func(m *Manifest, c *CurrentInstall) {
				m.RecallSchemaVersion, c.RecallSchemaVersion = 5, 1
			},
			wantBlock: true,
		},
		{
			name: "older recall store schema -> WARN",
			mutate: func(m *Manifest, c *CurrentInstall) {
				m.RecallSchemaVersion, c.RecallSchemaVersion = 1, 2
			},
			wantWarnN: 1,
			wantField: "recall_schema_version",
		},
		{
			// v1 backups stay restorable after the bump to 2: the gate is
			// m.SchemaVersion <= backupSchemaVersion.
			name:   "v1 manifest passes the version gate after the bump to 2",
			mutate: func(m *Manifest, c *CurrentInstall) { m.SchemaVersion = 1 },
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
	startErrs   map[string]error  // injected Start failure keyed by service name
}

func newFakeBackupDeps() *fakeBackupDeps {
	return &fakeBackupDeps{files: map[string][]byte{}}
}

func (f *fakeBackupDeps) deps() Deps {
	return Deps{
		OpenWebUIServiceName: "villa-openwebui.service",
		QdrantServiceName:    "qdrant.service",
		Stop: func(s string) error {
			f.calls = append(f.calls, "stop:"+s)
			return nil
		},
		Start: func(s string) error {
			f.calls = append(f.calls, "start:"+s)
			if f.startErrs != nil {
				return f.startErrs[s]
			}
			return nil
		},
		VolumeExport: func(name, out string) error {
			f.calls = append(f.calls, "export:"+name)
			if f.exportErr != nil {
				return f.exportErr
			}
			f.exportWrote = true
			f.files[out] = []byte("VOL-TAR-BYTES:" + name)
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

// TestSkewEmbeddingRemediationNamesConsequenceAndFix asserts the embedding
// SkewWarning's Remediation names BOTH the consequence (retrieval corrupt until
// re-index) and the fix (`villa recall index --rebuild` after restore, or align
// embedding_model/embedding_dim in config.toml) — D-08 refuse-with-remediation.
func TestSkewEmbeddingRemediationNamesConsequenceAndFix(t *testing.T) {
	m := baseManifest()
	m.EmbeddingModel, m.EmbeddingDim = "nomic-embed-text-v1.5", 768
	c := baseCurrent()
	c.EmbeddingModel, c.EmbeddingDim = "other-model", 512
	v := CompareSkew(m, c)
	if v.Block || len(v.Warnings) != 1 || v.Warnings[0].Field != "embedding" {
		t.Fatalf("want exactly one embedding warning, got %+v", v)
	}
	rem := v.Warnings[0].Remediation
	if !strings.Contains(rem, "villa recall index --rebuild") {
		t.Fatalf("remediation must name the fix `villa recall index --rebuild`, got %q", rem)
	}
	if !strings.Contains(rem, "retriev") {
		t.Fatalf("remediation must name the consequence (corrupt retrieval), got %q", rem)
	}
	if !strings.Contains(rem, "config.toml") {
		t.Fatalf("remediation must name the config alignment alternative, got %q", rem)
	}
}

// memoryBackupInput extends baseBackupInput with the memory-on optional sources:
// the qdrant volume export + recall-state.json entries and the manifest embedding
// fields (D-05/D-06). Names deliberately avoid the real service/volume literals —
// they are seam-sourced by the cmd tier, never typed in this core.
func memoryBackupInput(w io.Writer) BackupInput {
	in := baseBackupInput(w)
	in.QdrantVolumeName = "qdrant-vol"
	in.TempQdrantTar = "/tmp/qdrant-vol.tar"
	in.RecallStatePath = "/data/recall-state.json"
	in.EmbeddingModel = "nomic-embed-text-v1.5"
	in.EmbeddingDim = 768
	in.RecallSchemaVersion = 1
	return in
}

// TestBackupQdrantQuiesceOrderingAndEntries asserts the memory-on forward path
// (D-05/D-06, Pitfall 3): Stop(qdrant) strictly before VolumeExport(qdrant
// volume) strictly before Start(qdrant) — a live export of a running Qdrant can
// tear RocksDB/WAL state — and that the archive carries the two optional entries
// with checksums plus the manifest embedding fields.
func TestBackupQdrantQuiesceOrderingAndEntries(t *testing.T) {
	f := newFakeBackupDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/data/usage.json"] = []byte(`{"schema_version":3}`)
	f.files["/data/bench-reports.jsonl"] = []byte(`{"schema_version":4}` + "\n")
	f.files["/data/recall-state.json"] = []byte(`{"schema_version":1}`)

	var out bytes.Buffer
	res, err := Backup(f.deps(), memoryBackupInput(&out))
	if err != nil {
		t.Fatalf("Backup returned err: %v (result %+v)", err, res)
	}

	// Quiesce ordering by recorded call index: stop(qdrant) < export(qdrant-vol) <
	// start(qdrant).
	stopIdx, exportIdx, startIdx := -1, -1, -1
	for i, c := range f.calls {
		switch c {
		case "stop:qdrant.service":
			stopIdx = i
		case "export:qdrant-vol":
			exportIdx = i
		case "start:qdrant.service":
			startIdx = i
		}
	}
	if !(stopIdx >= 0 && exportIdx > stopIdx) {
		t.Fatalf("expected Stop(qdrant) before VolumeExport(qdrant), calls=%v", f.calls)
	}
	if !(startIdx > exportIdx) {
		t.Fatalf("expected deferred Start(qdrant) after its export, calls=%v", f.calls)
	}

	// Archive carries both optional memory entries; manifest checksums them and
	// records the embedding fields.
	names := archiveNames(t, out.Bytes())
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got[EntryQdrantVolume] || !got[EntryRecallState] {
		t.Fatalf("memory-on archive missing %q/%q: %v", EntryQdrantVolume, EntryRecallState, names)
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.EmbeddingModel != "nomic-embed-text-v1.5" || m.EmbeddingDim != 768 || m.RecallSchemaVersion != 1 {
		t.Fatalf("manifest embedding fields not recorded: %+v", m)
	}
	csum := map[string]bool{}
	for _, e := range m.Entries {
		csum[e.Name] = true
	}
	if !csum[EntryQdrantVolume] || !csum[EntryRecallState] {
		t.Fatalf("manifest missing memory-entry checksums: %+v", m.Entries)
	}
}

// TestBackupQdrantRestartFailureFoldsIntoWarning asserts a failed best-effort
// Start of the qdrant service NEVER fails the backup — it folds into
// RestartWarning (the OWUI IN-01 convention extended to the second quiesce frame).
func TestBackupQdrantRestartFailureFoldsIntoWarning(t *testing.T) {
	f := newFakeBackupDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.startErrs = map[string]error{"qdrant.service": errors.New("qdrant start boom")}

	var out bytes.Buffer
	res, err := Backup(f.deps(), memoryBackupInput(&out))
	if err != nil {
		t.Fatalf("a failed qdrant restart must NOT fail the backup: %v", err)
	}
	if res.RestartWarning == "" {
		t.Fatalf("failed qdrant restart must surface via RestartWarning, got %+v", res)
	}
}

// TestBackupMemoryOffZeroQdrantCalls asserts a memory-off backup (empty
// QdrantVolumeName/TempQdrantTar) makes ZERO qdrant Deps calls and assembles
// exactly the v1.2 entry set — the only delta is the manifest fields, all
// omitted (D-07 zero-touch discipline on the backup side).
func TestBackupMemoryOffZeroQdrantCalls(t *testing.T) {
	f := newFakeBackupDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/data/usage.json"] = []byte(`{"schema_version":3}`)
	f.files["/data/bench-reports.jsonl"] = []byte(`{"schema_version":4}` + "\n")

	var out bytes.Buffer
	if _, err := Backup(f.deps(), baseBackupInput(&out)); err != nil {
		t.Fatalf("Backup err: %v", err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "qdrant") {
			t.Fatalf("memory-off backup must make ZERO qdrant calls, got %v", f.calls)
		}
	}
	names := archiveNames(t, out.Bytes())
	for _, n := range names {
		if n == EntryQdrantVolume || n == EntryRecallState {
			t.Fatalf("memory-off archive must not carry %q: %v", n, names)
		}
	}
	if len(names) != 5 {
		t.Fatalf("memory-off entry set must equal the v1.2 five, got %v", names)
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.EmbeddingModel != "" || m.EmbeddingDim != 0 || m.RecallSchemaVersion != 0 {
		t.Fatalf("memory-off manifest must omit embedding fields, got %+v", m)
	}
}

// manifestFromArchive parses the manifest.json entry back out of an assembled
// archive (test helper).
func manifestFromArchive(t *testing.T, b []byte) Manifest {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == EntryManifest {
			mBytes, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err := json.Unmarshal(mBytes, &m); err != nil {
				t.Fatalf("manifest unmarshal: %v", err)
			}
			return m
		}
		_, _ = io.Copy(io.Discard, tr)
	}
	t.Fatalf("no %s in archive", EntryManifest)
	return Manifest{}
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
