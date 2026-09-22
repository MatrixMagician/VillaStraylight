package backup

// run_test.go covers RunBackup's decisions with a fake Deps: the moved-out
// traversal guard, the same-directory stage→publish sequence, and that a
// mid-Backup failure removes only its own temp files (#239, ADR-0012). The
// happy-path assemble/entry-selection behavior is already covered by
// TestBackupAssemblesArchive et al. against Backup directly; these tests focus
// on what RunBackup adds around it.

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// wireOSTemp sets Deps.CreateTemp/Rename/Remove to the real os calls, scoped to
// whatever dir the caller passes to CreateTemp (tests always pass a t.TempDir()).
func wireOSTemp(d Deps) Deps {
	d.CreateTemp = func(dir, pattern string) (string, io.WriteCloser, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return "", nil, err
		}
		return f.Name(), f, nil
	}
	d.Rename = os.Rename
	d.Remove = os.Remove
	return d
}

// TestRunBackupPublishesAndCleansScratch asserts the happy path: the archive
// lands at OutputPath, and every same-dir scratch temp (output stage + OWUI
// volume export) is gone afterward — nothing leaks into the destination
// directory.
func TestRunBackupPublishesAndCleansScratch(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	d := wireOSTemp(f.deps())

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "b.tar")
	in := baseBackupInput(nil)
	in.OutputPath = outPath

	res, err := RunBackup(d, in)
	if err != nil {
		t.Fatalf("RunBackup failed at %s: %v", res.FailedStep, err)
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		t.Fatalf("archive not published at %q: %v", outPath, statErr)
	}
	entries, rdErr := os.ReadDir(outDir)
	if rdErr != nil {
		t.Fatal(rdErr)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(outPath) {
			t.Fatalf("leftover scratch file in output dir: %s", e.Name())
		}
	}
}

// TestRunBackupFailurePreservesPriorArchiveAndCleansTemp mirrors the cmd-tier
// regression (now driven at the core directly): a mid-Backup failure removes
// ONLY its own staging temp — a pre-existing archive at OutputPath is never
// truncated or deleted.
func TestRunBackupFailurePreservesPriorArchiveAndCleansTemp(t *testing.T) {
	f := newFakeDeps()
	f.exportErr = os.ErrInvalid // fails inside Backup at the volume-export step
	d := wireOSTemp(f.deps())

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "b.tar")
	prior := []byte("PRIOR-ARCHIVE")
	if err := os.WriteFile(outPath, prior, 0o600); err != nil {
		t.Fatal(err)
	}

	in := baseBackupInput(nil)
	in.OutputPath = outPath

	res, err := RunBackup(d, in)
	if err == nil {
		t.Fatalf("RunBackup succeeded despite the injected export failure")
	}
	if res.FailedStep != "volume" {
		t.Fatalf("FailedStep = %q, want %q", res.FailedStep, "volume")
	}
	got, rerr := os.ReadFile(outPath)
	if rerr != nil || string(got) != string(prior) {
		t.Fatalf("prior archive not preserved byte-for-byte: err=%v got=%q", rerr, got)
	}
	entries, rdErr := os.ReadDir(outDir)
	if rdErr != nil {
		t.Fatal(rdErr)
	}
	if len(entries) != 1 {
		t.Fatalf("leftover temp file(s) in output dir: %v", entries)
	}
}

// TestRunBackupQdrantScratchOnlyWhenRequested asserts RunBackup stages a THIRD
// temp (the qdrant scratch) only when the caller set QdrantVolumeName — the
// caller's entry-selection decision, which RunBackup itself makes no call about.
func TestRunBackupQdrantScratchOnlyWhenRequested(t *testing.T) {
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	var patterns []string
	d := wireOSTemp(f.deps())
	d.CreateTemp = func(dir, pattern string) (string, io.WriteCloser, error) {
		patterns = append(patterns, pattern)
		fh, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return "", nil, err
		}
		return fh.Name(), fh, nil
	}

	outPath := filepath.Join(t.TempDir(), "b.tar")
	in := baseBackupInput(nil)
	in.OutputPath = outPath

	if _, err := RunBackup(d, in); err != nil {
		t.Fatalf("RunBackup (no qdrant) failed: %v", err)
	}
	for _, p := range patterns {
		if p == qdrantTempPattern {
			t.Fatalf("qdrant scratch created without QdrantVolumeName set: patterns = %v", patterns)
		}
	}

	patterns = nil
	f2 := newFakeDeps()
	f2.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	d2 := wireOSTemp(f2.deps())
	d2.CreateTemp = d.CreateTemp
	in2 := baseBackupInput(nil)
	in2.OutputPath = filepath.Join(t.TempDir(), "b2.tar")
	in2.QdrantVolumeName = "villa-memory"

	if _, err := RunBackup(d2, in2); err != nil {
		t.Fatalf("RunBackup (with qdrant) failed: %v", err)
	}
	found := false
	for _, p := range patterns {
		if p == qdrantTempPattern {
			found = true
		}
	}
	if !found {
		t.Fatalf("qdrant scratch NOT created despite QdrantVolumeName set: patterns = %v", patterns)
	}
}
