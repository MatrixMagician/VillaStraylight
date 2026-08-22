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

// TestSkewClassification is the table-driven classifier test.
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
			// a confident embedding model/dim mismatch is exactly ONE
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
			// model — "not recorded" must NOT raise a false alarm.
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
// Backup orchestrator tests. A fakeDeps records the call
// ordering and serves canned bytes so the pure quiesce→export→assemble flow is
// driven with no live host.
// ---------------------------------------------------------------------------

// fakeDeps records the seam call order and serves canned file bytes.
type fakeDeps struct {
	calls       []string          // ordered seam-call log
	files       map[string][]byte // path -> bytes for ReadFile
	exportErr   error             // injected VolumeExport failure
	exportWrote bool              // whether VolumeExport "wrote" the temp tar
	startErrs   map[string]error  // injected Start failure keyed by service name
}

func newFakeDeps() *fakeDeps {
	return &fakeDeps{files: map[string][]byte{}}
}

func (f *fakeDeps) deps() Deps {
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
	f := newFakeDeps()
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

// TestBackupAgentOnAddsCrushConfigAndExcludedAgent asserts an agent-on backup
// adds the crush.json entry INTO the archive (checksummed like
// every member) and records the EXCLUDED agent binary IDENTITY in the manifest
// (sha256 + version + pin) — while the agent binary BYTES are NEVER an archive
// entry (mirroring the excluded model weights).
func TestBackupAgentOnAddsCrushConfigAndExcludedAgent(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/crush/crush.json"] = []byte(`{"provider":"local"}`)

	in := baseBackupInput(nil)
	var out bytes.Buffer
	in.OutputWriter = &out
	in.CrushConfigPath = "/crush/crush.json"
	in.AgentBinarySHA256 = "sha-of-on-disk-binary"
	in.AgentVersion = "v0.76.0"
	in.AgentPinSHA256 = "sha-of-pinned-binary"

	res, err := Backup(f.deps(), in)
	if err != nil {
		t.Fatalf("Backup err: %v (%+v)", err, res)
	}

	names := archiveNames(t, out.Bytes())
	gotCrush := false
	for _, n := range names {
		gotCrush = gotCrush || n == EntryCrushConfig
		if n == "crush" || n == "crush-binary" {
			t.Fatalf("agent BINARY bytes must NEVER be archived; found %q", n)
		}
	}
	if !gotCrush {
		t.Fatalf("agent-on backup missing %q entry: %v", EntryCrushConfig, names)
	}

	m := manifestFromArchive(t, out.Bytes())
	if m.ExcludedAgent == nil {
		t.Fatalf("agent-on backup must record ExcludedAgent identity; manifest=%+v", m)
	}
	if m.ExcludedAgent.SHA256 != "sha-of-on-disk-binary" ||
		m.ExcludedAgent.Version != "v0.76.0" ||
		m.ExcludedAgent.PinSHA256 != "sha-of-pinned-binary" {
		t.Fatalf("ExcludedAgent identity not recorded: %+v", m.ExcludedAgent)
	}
	// crush.json carries a checksum like every other member.
	csum := map[string]bool{}
	for _, e := range m.Entries {
		csum[e.Name] = e.SHA256 != ""
	}
	if !csum[EntryCrushConfig] {
		t.Fatalf("crush.json entry has no checksum: %+v", m.Entries)
	}
}

// TestBackupAgentOffIsLayoutIdentical asserts an agent-off backup (no
// CrushConfigPath, no agent identity) records ZERO agent entries and a nil
// ExcludedAgent — the archive layout is identical to the pre-Phase-28 v2 backup
// (the bump only widens the contract for an agent-ON backup).
func TestBackupAgentOffIsLayoutIdentical(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/data/usage.json"] = []byte(`{"schema_version":3}`)
	f.files["/data/bench-reports.jsonl"] = []byte(`{"schema_version":4}` + "\n")

	var out bytes.Buffer
	if _, err := Backup(f.deps(), baseBackupInput(&out)); err != nil {
		t.Fatalf("Backup err: %v", err)
	}

	for _, n := range archiveNames(t, out.Bytes()) {
		if n == EntryCrushConfig {
			t.Fatalf("agent-off backup must NOT include %q: %v", EntryCrushConfig, archiveNames(t, out.Bytes()))
		}
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.ExcludedAgent != nil {
		t.Fatalf("agent-off backup must record NO ExcludedAgent, got %+v", m.ExcludedAgent)
	}
}

// TestBackupAgentOnSkipsAbsentCrushConfig asserts an agent-on backup whose
// rendered crush.json is absent on disk SKIPS the entry (via FileMissing) rather
// than failing the backup — mirroring the qdrant/recall optional skip-when-absent
// shape. The ExcludedAgent identity is still recorded (the binary identity is
// independent of the config file's presence).
func TestBackupAgentOnSkipsAbsentCrushConfig(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	// NOTE: /crush/crush.json deliberately NOT seeded — ReadFile returns ErrNotExist.

	in := baseBackupInput(nil)
	var out bytes.Buffer
	in.OutputWriter = &out
	in.CrushConfigPath = "/crush/crush.json"
	in.AgentBinarySHA256 = "sha-of-on-disk-binary"
	in.AgentVersion = "v0.76.0"
	in.AgentPinSHA256 = "sha-of-pinned-binary"

	res, err := Backup(f.deps(), in)
	if err != nil {
		t.Fatalf("absent crush.json must be skipped, not fatal: %v (%+v)", err, res)
	}
	for _, n := range archiveNames(t, out.Bytes()) {
		if n == EntryCrushConfig {
			t.Fatalf("absent crush.json must be skipped, but %q is present", EntryCrushConfig)
		}
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.ExcludedAgent == nil || m.ExcludedAgent.SHA256 != "sha-of-on-disk-binary" {
		t.Fatalf("ExcludedAgent identity must still be recorded when crush.json is absent: %+v", m.ExcludedAgent)
	}
}

// TestBackupSearxngSettings asserts the OPTIONAL Phase-34 web-search settings.yml
// provenance entry behaves exactly like the crush.json optional entry:
// present-when-set, absent-when-empty (web off), FileMissing-skipped-when-absent,
// and the manifest stamps its OWN schema version 4 in every case (the manifest
// stamps its own version, never a caller-supplied one).
func TestBackupSearxngSettings(t *testing.T) {
	t.Run("present when set", func(t *testing.T) {
		f := newFakeDeps()
		f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
		f.files["/searxng/settings.yml"] = []byte("use_default_settings: true\n")

		in := baseBackupInput(nil)
		var out bytes.Buffer
		in.OutputWriter = &out
		in.SearxngSettingsPath = "/searxng/settings.yml"

		res, err := Backup(f.deps(), in)
		if err != nil {
			t.Fatalf("Backup err: %v (%+v)", err, res)
		}
		gotSettings := false
		for _, n := range archiveNames(t, out.Bytes()) {
			gotSettings = gotSettings || n == EntrySearxngSettings
		}
		if !gotSettings {
			t.Fatalf("web-search-on backup missing %q entry: %v", EntrySearxngSettings, archiveNames(t, out.Bytes()))
		}
		m := manifestFromArchive(t, out.Bytes())
		// settings.yml carries a checksum like every other member.
		csum := map[string]bool{}
		for _, e := range m.Entries {
			csum[e.Name] = e.SHA256 != ""
		}
		if !csum[EntrySearxngSettings] {
			t.Fatalf("settings.yml entry has no checksum: %+v", m.Entries)
		}
		// The manifest stamps its OWN backupSchemaVersion (4), never a caller value.
		if m.SchemaVersion != backupSchemaVersion {
			t.Fatalf("manifest must stamp schema %d, got %d", backupSchemaVersion, m.SchemaVersion)
		}
		if backupSchemaVersion != 4 {
			t.Fatalf("Phase-34 backup contract is schema 4, got %d", backupSchemaVersion)
		}
	})

	t.Run("skipped when empty (web off)", func(t *testing.T) {
		f := newFakeDeps()
		f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")

		in := baseBackupInput(nil)
		var out bytes.Buffer
		in.OutputWriter = &out
		in.SearxngSettingsPath = "" // web search off

		res, err := Backup(f.deps(), in)
		if err != nil {
			t.Fatalf("web-off backup must not error: %v (%+v)", err, res)
		}
		for _, n := range archiveNames(t, out.Bytes()) {
			if n == EntrySearxngSettings {
				t.Fatalf("web-off backup must NOT include %q: %v", EntrySearxngSettings, archiveNames(t, out.Bytes()))
			}
		}
		// Even with the v4 bump, a web-off backup still stamps schema 4 (the
		// contract widened; the LAYOUT is identical to a v3 archive sans entry).
		if m := manifestFromArchive(t, out.Bytes()); m.SchemaVersion != backupSchemaVersion {
			t.Fatalf("manifest must stamp schema %d even when web off, got %d", backupSchemaVersion, m.SchemaVersion)
		}
	})

	t.Run("FileMissing-skipped when absent", func(t *testing.T) {
		f := newFakeDeps()
		f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
		// NOTE: /searxng/settings.yml deliberately NOT seeded — ReadFile returns ErrNotExist.

		in := baseBackupInput(nil)
		var out bytes.Buffer
		in.OutputWriter = &out
		in.SearxngSettingsPath = "/searxng/settings.yml"

		res, err := Backup(f.deps(), in)
		if err != nil {
			t.Fatalf("absent settings.yml must be skipped, not fatal: %v (%+v)", err, res)
		}
		for _, n := range archiveNames(t, out.Bytes()) {
			if n == EntrySearxngSettings {
				t.Fatalf("absent settings.yml must be skipped, but %q is present", EntrySearxngSettings)
			}
		}
	})
}

// TestBackupExcludesEphemeral is the NEGATIVE assertion guarding:
// a web-search-on backup archives ONLY the rendered settings.yml CONFIG provenance
// and NEVER any fetched/ephemeral web content — no query log, no fetched-URL log,
// no per-page content key. The backup core exposes exactly one web-search source
// field (SearxngSettingsPath); there is no input or archive entry for ephemeral
// content, by construction.
func TestBackupExcludesEphemeral(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/searxng/settings.yml"] = []byte("use_default_settings: true\n")

	in := baseBackupInput(nil)
	var out bytes.Buffer
	in.OutputWriter = &out
	in.SearxngSettingsPath = "/searxng/settings.yml"

	res, err := Backup(f.deps(), in)
	if err != nil {
		t.Fatalf("Backup err: %v (%+v)", err, res)
	}
	// No archive entry may reference a fetched-URL / query log / per-page ephemeral
	// content key. The ONLY sanctioned web-search member is the settings.yml CONFIG.
	bannedSubstrings := []string{"query", "queries", "fetched", "fetch-log", "url-log", "urls", "page_content", "ephemeral", "web-content", "search-log", "history"}
	for _, n := range archiveNames(t, out.Bytes()) {
		if n == EntrySearxngSettings {
			continue // the sanctioned CONFIG provenance entry
		}
		low := strings.ToLower(n)
		for _, b := range bannedSubstrings {
			if strings.Contains(low, b) {
				t.Fatalf("SURF-07/T-34-06: archive entry %q looks like ephemeral web content — only the settings.yml CONFIG may cross", n)
			}
		}
	}
	// And the manifest carries no ephemeral-content checksum either.
	m := manifestFromArchive(t, out.Bytes())
	for _, e := range m.Entries {
		if e.Name == EntrySearxngSettings {
			continue
		}
		low := strings.ToLower(e.Name)
		for _, b := range bannedSubstrings {
			if strings.Contains(low, b) {
				t.Fatalf("SURF-07/T-34-06: manifest lists %q — no ephemeral web content may be archived", e.Name)
			}
		}
	}
}

// TestBackupDeferredRestartFiresOnExportError asserts the OWUI service is restarted
// (best-effort defer) even when the volume export fails mid-backup.
func TestBackupDeferredRestartFiresOnExportError(t *testing.T) {
	f := newFakeDeps()
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
// embedding_model/embedding_dim in config.toml) — refuse-with-remediation.
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
// fields. Names deliberately avoid the real service/volume literals
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
// (Pitfall 3): Stop(qdrant) strictly before VolumeExport(qdrant
// volume) strictly before Start(qdrant) — a live export of a running Qdrant can
// tear RocksDB/WAL state — and that the archive carries the two optional entries
// with checksums plus the manifest embedding fields.
func TestBackupQdrantQuiesceOrderingAndEntries(t *testing.T) {
	f := newFakeDeps()
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
// RestartWarning (the OWUI convention extended to the second quiesce frame).
func TestBackupQdrantRestartFailureFoldsIntoWarning(t *testing.T) {
	f := newFakeDeps()
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
// omitted (zero-touch discipline on the backup side).
func TestBackupMemoryOffZeroQdrantCalls(t *testing.T) {
	f := newFakeDeps()
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

// TestBackupStreamsVolumeTars is the regression for the backup side: with
// the OpenFile seam wired (the live cmd-tier shape), the two volume tar entries
// are STREAMED — ReadFile is never called for their paths, so a multi-GiB qdrant
// export is never buffered whole in memory — and the assembled archive still
// carries manifest checksums that VERIFY against the streamed bodies (the
// restore-side fail-closed gate stays sound).
func TestBackupStreamsVolumeTars(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files["/data/recall-state.json"] = []byte(`{"schema_version":1}`)

	d := f.deps()
	d.OpenFile = func(p string) (io.ReadCloser, int64, error) {
		f.calls = append(f.calls, "open:"+p)
		b, ok := f.files[p]
		if !ok {
			return nil, 0, os.ErrNotExist
		}
		return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
	}

	var out bytes.Buffer
	in := memoryBackupInput(&out)
	if _, err := Backup(d, in); err != nil {
		t.Fatalf("Backup err: %v", err)
	}

	// The volume tars must STREAM via OpenFile — never a whole-file ReadFile.
	for _, c := range f.calls {
		if c == "read:"+in.TempVolumeTar || c == "read:"+in.TempQdrantTar {
			t.Fatalf("volume tars must stream via OpenFile, not ReadFile; calls %v", f.calls)
		}
	}
	for _, want := range []string{"open:" + in.TempVolumeTar, "open:" + in.TempQdrantTar} {
		found := false
		for _, c := range f.calls {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing streaming open %q; calls %v", want, f.calls)
		}
	}

	// Every streamed entry's manifest SHA-256 must verify against the archived
	// body (byte-identical to the in-memory path).
	m := manifestFromArchive(t, out.Bytes())
	wantSum := map[string]string{}
	for _, e := range m.Entries {
		wantSum[e.Name] = e.SHA256
	}
	tr := tar.NewReader(bytes.NewReader(out.Bytes()))
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == EntryManifest {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got, err := sum(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if got != wantSum[h.Name] {
			t.Fatalf("entry %q checksum mismatch after streaming: archive %s, manifest %s", h.Name, got, wantSum[h.Name])
		}
		seen[h.Name] = true
	}
	if !seen[EntryOpenWebUIVolume] || !seen[EntryQdrantVolume] {
		t.Fatalf("streamed archive missing a volume entry: %v", seen)
	}
}

// TestBackupSkipsAbsentDataDirArtifacts asserts an absent usage.json / bench file
// is skipped (not fatal): the archive still assembles with the present entries.
func TestBackupSkipsAbsentDataDirArtifacts(t *testing.T) {
	f := newFakeDeps()
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
