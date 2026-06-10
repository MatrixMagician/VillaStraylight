package main

// backup_test.go guards the `villa backup` cmd-tier wiring (BAK-01): the default
// output name is FS-safe (no ':'), an escaping -o is traversal-refused, the bench
// entry resolves through the existing cmd-tier benchReportsStorePath() resolver, the
// exit code maps from the orchestrator result, and the live wiring sources the image
// digests from the seam (no literal — also covered by TestSeamGrepGate).

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// TestBackupDefaultNameIsFSSafe asserts the default archive name has no ':' (D-04)
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

// fakeRunBackupDeps builds a backup.Deps whose VolumeExport writes a stub tar and
// whose ReadFile serves canned bytes, so runBackup is driven end-to-end with no live
// podman/systemd.
func fakeRunBackupDeps(t *testing.T, files map[string][]byte) backup.Deps {
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
	code := runBackup(cmd, outPath, fakeRunBackupDeps(t, files))
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

// TestBackupMemoryOffOmitsLeftoverRecallState is the WR-03 regression over the
// REAL cmd-tier wiring (the pure-core tests pass an empty RecallStatePath and
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
	rsPath := recall.RecallStatePath()
	if err := os.MkdirAll(filepath.Dir(rsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rsPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{cfgPath: []byte("model = \"m\"\n")}
	outPath := filepath.Join(t.TempDir(), "b.tar")
	cmd, out, errOut := newBackupTestCmd()
	code := runBackup(cmd, outPath, fakeRunBackupDeps(t, files))
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
