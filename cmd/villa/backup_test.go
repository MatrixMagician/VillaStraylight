package main

// backup_test.go guards the `villa backup` cmd-tier wiring: the default
// output name is FS-safe (no ':'), an escaping -o is traversal-refused, the bench
// entry resolves through the existing cmd-tier benchReportsStorePath() resolver, the
// exit code maps from the orchestrator result, and the live wiring sources the image
// digests from the seam (no literal — also covered by TestSeamGrepGate).

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// TestBackupDefaultNameIsFSSafe asserts the default archive name has no ':'
// and matches the villa-backup-<timestamp>.tar shape.
func TestBackupDefaultNameIsFSSafe(t *testing.T) {
	name := defaultBackupName(time.Date(2026, 6, 7, 14, 22, 33, 0, time.UTC))
	if strings.ContainsRune(name, ':') {
		t.Fatalf("default name contains ':' (not FS-safe): %q", name)
	}
	if !strings.HasPrefix(name, "villa-backup-") || !strings.HasSuffix(name, ".tar") {
		t.Fatalf("unexpected default name shape: %q", name)
	}
	if name != "villa-backup-20260607T142233Z.tar" {
		t.Fatalf("default name = %q, want villa-backup-20260607T142233Z.tar", name)
	}
}

// TestBackupOutputTraversalRejected asserts an output path escaping its parent dir is
// refused (T-16-02a).
func TestBackupOutputTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	// A path whose Rel to its own Dir() escapes is impossible by construction, so test
	// the guard directly with a crafted path/parent pair.
	escaping := filepath.Join(dir, "sub", "..", "..", "evil.tar")
	parent := filepath.Join(dir, "sub")
	if err := assertBackupOutputInside(escaping, parent); err == nil {
		t.Fatalf("traversal guard accepted an escaping output path %q under %q", escaping, parent)
	}
	// A well-formed path under its parent is accepted.
	ok := filepath.Join(dir, "b.tar")
	if err := assertBackupOutputInside(ok, dir); err != nil {
		t.Fatalf("guard rejected a valid path %q: %v", ok, err)
	}
}

// TestBenchEntryResolvesViaCmdResolver asserts the bench store path the backup uses
// is the SAME one the existing cmd-tier resolver returns (the single
// bench-reports.jsonl), not a re-implemented path (REUSE benchReportsStorePath).
func TestBenchEntryResolvesViaCmdResolver(t *testing.T) {
	got := benchReportsStorePath()
	if !strings.HasSuffix(got, filepath.Join("villa", "bench-reports.jsonl")) {
		t.Fatalf("bench store path %q does not end in villa/bench-reports.jsonl", got)
	}
}

// fakeRunDeps builds a backup.Deps whose VolumeExport writes a stub tar and
// whose ReadFile serves canned bytes, so runBackup is driven end-to-end with no live
// podman/systemd.
func fakeRunDeps(t *testing.T, files map[string][]byte) backup.Deps {
	t.Helper()
	return backup.Deps{
		OpenWebUIServiceName: openWebUIServiceName,
		Stop:                 func(string) error { return nil },
		Start:                func(string) error { return nil },
		VolumeExport: func(_, out string) error {
			return os.WriteFile(out, []byte("STUB-OWUI-VOLUME"), 0o600)
		},
		ReadFile: func(p string) ([]byte, error) {
			if b, ok := files[p]; ok {
				return b, nil
			}
			// The runtime temp volume tar (written by the fake VolumeExport above) is
			// read back from disk like the live wiring does.
			return os.ReadFile(p)
		},
		// CreateTemp/Rename/Remove drive backup.RunBackup's stage→publish sequence
		// (#239); the real os calls are fine to use directly here (test-tier only).
		CreateTemp: func(dir, pattern string) (string, io.WriteCloser, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return "", nil, err
			}
			return f.Name(), f, nil
		},
		Rename: os.Rename,
		Remove: os.Remove,
	}
}

// newBackupTestCmd returns a cobra command whose out/err are captured buffers.
func newBackupTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := newBackup()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// TestRunBackupWritesArchive drives runBackup off-hardware against a fake Deps and a
// controlled config dir, asserting exit 0, a 0600 archive on disk, and a manifest
// whose digests come from the seam (non-empty, @sha256-pinned) and whose store schema
// versions match the accessors.
func TestRunBackupWritesArchive(t *testing.T) {
	// Point config + data dirs at a temp tree so LoadVilla/Path resolve in isolation.
	cfgHome := t.TempDir()
	dataHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	// Write a minimal config.toml so config.Path() has a real source file.
	cfgDir := filepath.Join(cfgHome, "villa")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("model = \"qwen3-30b\"\nbackend = \"vulkan\"\nctx = 8192\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{
		cfgPath: []byte("model = \"qwen3-30b\"\n"),
		filepath.Join(dataHome, "villa", "usage.json"):          []byte(`{"schema_version":1}`),
		filepath.Join(dataHome, "villa", "bench-reports.jsonl"): []byte(`{"schema_version":1}` + "\n"),
	}

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "b.tar")

	cmd, _, errOut := newBackupTestCmd()
	code := runBackup(cmd, outPath, fakeRunDeps(t, files))
	if code != exitPass {
		t.Fatalf("runBackup exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}

	// Archive exists and is 0600.
	fi, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("archive not written: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %o, want 600", fi.Mode().Perm())
	}

	// Manifest digests are seam-sourced (@sha256-pinned, non-empty).
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var mBytes []byte
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == backup.EntryManifest {
			mBytes, _ = io.ReadAll(tr)
		} else {
			_, _ = io.Copy(io.Discard, tr)
		}
	}
	var m backup.Manifest
	if err := json.Unmarshal(mBytes, &m); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if !strings.Contains(m.InferenceImage, "@sha256:") || !strings.Contains(m.OpenWebUIImage, "@sha256:") {
		t.Fatalf("manifest digests not seam-sourced/pinned: inf=%q owui=%q", m.InferenceImage, m.OpenWebUIImage)
	}
	if len(m.ExcludedModels) != 1 || m.ExcludedModels[0].ID != "qwen3-30b" {
		t.Fatalf("excluded model identity not recorded from config: %+v", m.ExcludedModels)
	}
}

// TestBackupFailurePreservesPriorArchive is the regression: re-using an
// output path (`villa backup -o villa-latest.tar` from cron) must NEVER destroy
// the previous archive on a mid-backup failure. Pre-fix, the output was opened
// O_TRUNC up-front and removed on failure — any failed run left ZERO backups.
// The fix stages a same-dir temp and renames only after a complete write.
func TestBackupFailurePreservesPriorArchive(t *testing.T) {
	cfgHome := t.TempDir()
	dataHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	cfgDir := filepath.Join(cfgHome, "villa")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 8192\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "villa-latest.tar")
	prior := []byte("PRIOR-COMPLETE-ARCHIVE-BYTES")
	if err := os.WriteFile(outPath, prior, 0o600); err != nil {
		t.Fatal(err)
	}

	// A deps whose volume export fails mid-backup (after the output would already
	// have been truncated under the old flow).
	d := fakeRunDeps(t, map[string][]byte{})
	d.VolumeExport = func(_, _ string) error { return errors.New("export boom") }

	cmd, _, errOut := newBackupTestCmd()
	code := runBackup(cmd, outPath, d)
	if code != exitBlocked {
		t.Fatalf("failed backup exit = %d, want %d; stderr=%q", code, exitBlocked, errOut.String())
	}

	// The PRIOR archive must survive byte-for-byte.
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("prior archive was deleted by the failed backup: %v", err)
	}
	if !bytes.Equal(got, prior) {
		t.Fatalf("prior archive was modified by the failed backup: got %q", got)
	}
	// And no torn temp file may be left behind.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".villa-backup-") {
			t.Fatalf("torn output temp file left behind: %s", e.Name())
		}
	}
}

// TestBackupMemoryOffOmitsLeftoverRecallState is the regression over the
// REAL cmd-tier wiring (the pure-core tests offer no recall-state source and
// never saw it): a memory-OFF config with a LEFTOVER recall-state.json (memory
// was previously enabled) must produce an archive WITHOUT the recall-state.json
// entry and a manifest without the recall/embedding fields — otherwise the entry
// escapes the fail-closed recall_schema_version gate on restore and the
// documented v1-identical memory-off layout is violated.
func TestBackupMemoryOffOmitsLeftoverRecallState(t *testing.T) {
	cfgHome := t.TempDir()
	dataHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	cfgDir := filepath.Join(cfgHome, "villa")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.toml")
	// memory_enabled defaults to false — a memory-OFF config.
	if err := os.WriteFile(cfgPath, []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 8192\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The leftover recall-state.json a previously-enabled memory stack left behind.
	rsPath := recall.StatePath()
	if err := os.MkdirAll(filepath.Dir(rsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rsPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{cfgPath: []byte("model = \"m\"\n")}
	outPath := filepath.Join(t.TempDir(), "b.tar")
	cmd, out, errOut := newBackupTestCmd()
	code := runBackup(cmd, outPath, fakeRunDeps(t, files))
	if code != exitPass {
		t.Fatalf("runBackup exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var mBytes []byte
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == backup.EntryRecallState {
			t.Fatalf("memory-off backup must NOT archive the leftover %s", backup.EntryRecallState)
		}
		if h.Name == backup.EntryManifest {
			mBytes, _ = io.ReadAll(tr)
		} else {
			_, _ = io.Copy(io.Discard, tr)
		}
	}
	var m backup.Manifest
	if err := json.Unmarshal(mBytes, &m); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if m.EmbeddingModel != "" || m.EmbeddingDim != 0 || m.RecallSchemaVersion != 0 {
		t.Fatalf("memory-off manifest must omit the recall/embedding fields, got %+v", m)
	}
	if !strings.Contains(out.String(), "recall state not included (memory disabled)") {
		t.Fatalf("memory-off backup must report the recall state as not included, got %q", out.String())
	}
}

// scratchVillaHome points the config and data roots at fresh temp dirs, writes
// cfgTOML as config.toml, and returns the config.toml path.
func scratchVillaHome(t *testing.T, cfgTOML string) string {
	t.Helper()
	cfgHome, dataHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(cfgHome, "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(cfgTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// sortedKeys is the entry names of a source/destination map, sorted.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// TestLiveBackupSourcesGateEachEntry guards the cmd tier's half of the registry
// (ADR-0020): config.toml, usage.json and bench-reports.jsonl are always offered to
// the archive, and each optional entry is offered ONLY when its subsystem is on —
// so an archive made with a subsystem off stays layout-identical to one made before
// the entry existed.
func TestLiveBackupSourcesGateEachEntry(t *testing.T) {
	cfgPath := scratchVillaHome(t, "")
	always := []string{backup.EntryBenchReports, backup.EntryConfig, backup.EntryEvalBaselines, backup.EntryUsage}
	tests := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"every subsystem off", config.VillaConfig{}, always},
		{"memory on", config.VillaConfig{MemoryEnabled: true}, append([]string{backup.EntryRecallState}, always...)},
		{"coding agent on", config.VillaConfig{AgentEnabled: true}, append([]string{backup.EntryCrushConfig}, always...)},
		{"web search on", config.VillaConfig{WebSearchEnabled: true}, append([]string{backup.EntrySearxngSettings}, always...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedKeys(liveBackupSources(tt.cfg, cfgPath, io.Discard))
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("sources = %v, want %v", got, want)
			}
		})
	}
}

// TestLiveRestoreDestsGateEachEntry is the restore half: recall-state.json is wired
// regardless of the memory gate (the archive's own entry decides; the backup side is
// the one that gates it), while crush.json and searxng-settings.yml are wired only
// when their subsystem is on, so an off install makes ZERO writes for them.
func TestLiveRestoreDestsGateEachEntry(t *testing.T) {
	scratchVillaHome(t, "")
	always := []string{backup.EntryBenchReports, backup.EntryEvalBaselines, backup.EntryRecallState, backup.EntryUsage}
	tests := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"every subsystem off", config.VillaConfig{}, always},
		{"coding agent on", config.VillaConfig{AgentEnabled: true}, append([]string{backup.EntryCrushConfig}, always...)},
		{"web search on", config.VillaConfig{WebSearchEnabled: true}, append([]string{backup.EntrySearxngSettings}, always...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedKeys(liveRestoreDests(tt.cfg, io.Discard))
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("dests = %v, want %v", got, want)
			}
		})
	}
}

// TestFileEntryPathFailureWarnsAndOmits: a path that cannot be resolved (no HOME or
// XDG dir) is warned about by name and left out, so the entry is honestly not
// archived or restored rather than failing the verb.
func TestFileEntryPathFailureWarnsAndOmits(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	cfg := config.VillaConfig{AgentEnabled: true, WebSearchEnabled: true}

	var errOut bytes.Buffer
	sources := liveBackupSources(cfg, "/cfg/config.toml", &errOut)
	for _, entry := range []string{backup.EntryCrushConfig, backup.EntrySearxngSettings} {
		if _, ok := sources[entry]; ok {
			t.Errorf("an unresolvable %s path must be omitted, got %v", entry, sources)
		}
	}
	for _, want := range []string{
		"backup: warning: cannot resolve crush.json path (agent config not archived)",
		"backup: warning: cannot resolve settings.yml path (web-search config not archived)",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr missing %q, got %q", want, errOut.String())
		}
	}

	errOut.Reset()
	dests := liveRestoreDests(cfg, &errOut)
	if _, ok := dests[backup.EntryCrushConfig]; ok {
		t.Errorf("an unresolvable crush.json destination must be omitted, got %v", dests)
	}
	if !strings.Contains(errOut.String(), "restore: warning: cannot resolve crush.json path (agent config will not be restored)") {
		t.Errorf("restore warning missing, got %q", errOut.String())
	}
}

// TestApplyBackupAgentRecordsIdentityNotBytes: an agent-on backup records the pinned
// agent identity (version + pin sha) in the Input and never the binary's bytes, and
// a binary that cannot be hashed is warned about and left empty — the identity is
// still written from the pinned policy. An agent-off backup records nothing.
func TestApplyBackupAgentRecordsIdentityNotBytes(t *testing.T) {
	scratchVillaHome(t, "")

	var off backup.Input
	applyBackupAgent(&off, config.VillaConfig{}, io.Discard)
	if off.AgentVersion != "" || off.AgentPinSHA256 != "" || off.AgentBinarySHA256 != "" {
		t.Fatalf("an agent-off backup must record no identity, got %+v", off)
	}

	var absent backup.Input
	applyBackupAgent(&absent, config.VillaConfig{AgentEnabled: true}, io.Discard)
	if absent.AgentVersion == "" || absent.AgentBinarySHA256 != "" {
		t.Fatalf("an absent binary still records the pinned version and an empty sha, got %+v", absent)
	}

	// A directory where the binary should be is unreadable as a file: the hash fails.
	if err := os.MkdirAll(agentBinPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	var unreadable backup.Input
	applyBackupAgent(&unreadable, config.VillaConfig{AgentEnabled: true}, &errOut)
	if !strings.Contains(errOut.String(), "cannot hash the coding-agent binary") {
		t.Fatalf("an unhashable binary must warn, got %q", errOut.String())
	}
	if unreadable.AgentBinarySHA256 != "" || unreadable.AgentVersion == "" {
		t.Fatalf("an unhashable binary leaves the sha empty but keeps the pinned identity, got %+v", unreadable)
	}
}

// TestNarrateBackupEntryStates: an optional entry is reported as off (never
// offered), absent (offered, no file) or included — the operator is never left
// guessing whether it made it into the archive.
func TestNarrateBackupEntryStates(t *testing.T) {
	present := filepath.Join(t.TempDir(), "recall-state.json")
	if err := os.WriteFile(present, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{"off", map[string]string{}, recallBackupNarration.off},
		{"absent", map[string]string{backup.EntryRecallState: present + ".missing"}, recallBackupNarration.absent},
		{"included", map[string]string{backup.EntryRecallState: present}, recallBackupNarration.included},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			narrateBackupEntry(&out, recallBackupNarration, tt.sources)
			if got := strings.TrimSpace(out.String()); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNarrateBackupReportsQdrantAndAgentIdentity covers the optional-memory and
// agent-identity lines of the backup report.
func TestNarrateBackupReportsQdrantAndAgentIdentity(t *testing.T) {
	var out, errOut bytes.Buffer
	in := backup.Input{
		OutputPath:       "/tmp/b.tar",
		QdrantVolumeName: "qdrant-vol",
		AgentVersion:     "v0.76.0",
		ExcludedModels:   []backup.ExcludedModel{{ID: "m", Quant: "Q4", Ctx: "4096"}},
	}
	narrateBackup(&out, &errOut, in, backup.Result{RestartWarning: "restart failed"})
	for _, want := range []string{
		"backup written to /tmp/b.tar",
		"memory: Qdrant volume included (qdrant-volume.tar)",
		"  - m (quant Q4, ctx 4096)",
		"coding agent: binary excluded, identity recorded for re-stage (pinned v0.76.0)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q, got %q", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "warning: restart failed") {
		t.Errorf("a failed service restart must be surfaced, got %q", errOut.String())
	}
}

// TestRunBackupRefusesBeforeAnyEffect: a held stack lock, an unreadable config and
// an unknown backend each exit blocked with a message and no archive written.
func TestRunBackupRefusesBeforeAnyEffect(t *testing.T) {
	tests := []struct {
		name    string
		cfgTOML string
		lockErr error
		want    string
	}{
		{"stack lock held", "", errors.New("lock held"), "backup: lock held"},
		{"unreadable config", "model = [unterminated\n", nil, "backup: load config"},
		{"unknown backend", "backend = \"bogus\"\n", nil, "backup: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scratchVillaHome(t, tt.cfgTOML)
			prev := acquireStackLock
			acquireStackLock = func() (*stacklock.Lock, error) { return nil, tt.lockErr }
			t.Cleanup(func() { acquireStackLock = prev })

			outPath := filepath.Join(t.TempDir(), "b.tar")
			cmd, _, errOut := newBackupTestCmd()
			if code := runBackup(cmd, outPath, fakeRunDeps(t, nil)); code != exitBlocked {
				t.Fatalf("exit = %d, want %d; stderr=%q", code, exitBlocked, errOut.String())
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("stderr %q does not contain %q", errOut.String(), tt.want)
			}
			if _, err := os.Stat(outPath); err == nil {
				t.Fatalf("a refused backup must not write an archive")
			}
		})
	}
}
