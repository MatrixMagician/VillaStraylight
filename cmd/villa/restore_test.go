package main

// restore_test.go guards the `villa restore` cmd-tier wiring (BAK-02/BAK-03): the
// positional archive arg is required, --yes bypasses the skew consent gate, a declined
// consent / a non-pass prove map to the right exit codes, and a Restored result exits
// 0. The full transactional ordering invariants (clean-recreate-before-import, capture-
// before-mutate, rollback) are covered by the PURE internal/backup restore_test.go;
// this file asserts the cobra surface + Result->exit mapping over a fake backup.Deps.

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// writeTestArchive builds a valid backup .tar on disk (manifest FIRST + correct
// per-entry SHA-256) using the EXPORTED backup builders, so the restore verify pass
// passes. m's version/digests/schema fields control the skew classification.
func writeTestArchive(t *testing.T, path string, m backup.ManifestInput, cfgTOML, owui []byte) {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	data := []e{{backup.EntryConfig, cfgTOML}, {backup.EntryOpenWebUIVolume, owui}}
	var sums []backup.EntryChecksum
	for _, d := range data {
		h := sha256.Sum256(d.data)
		sums = append(sums, backup.EntryChecksum{Name: d.name, SHA256: hex.EncodeToString(h[:])})
	}
	m.Entries = sums
	man := backup.BuildManifest(m)
	mj, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	all := []e{{backup.EntryManifest, mj}}
	all = append(all, data...)
	for _, d := range all {
		if err := tw.WriteHeader(&tar.Header{Name: d.name, Mode: 0o600, Size: int64(len(d.data)), Format: tar.FormatPAX}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(d.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeRestoreDeps returns a backup.Deps whose seams are all no-op successes, plus the
// canned prove verdict. The capture LoadConfig returns a vulkan config so rollback has
// a prior to restore.
func fakeRestoreDeps(prove backup.ProveVerdict) backup.Deps {
	return backup.Deps{
		OpenWebUIServiceName: openWebUIServiceName,
		InstallServiceName:   installServiceName,
		LoadConfig:           func() (config.VillaConfig, error) { return config.VillaConfig{Backend: "vulkan", Model: "m"}, nil },
		SaveConfig:           func(config.VillaConfig) error { return nil },
		VolumeExport:         func(_, _ string) error { return nil },
		VolumeImport:         func(_, _ string) error { return nil },
		VolumeRm:             func(string) error { return nil },
		EnsureVolume:         func(string) error { return nil },
		ReconcileAndWrite:    func(config.VillaConfig) (bool, error) { return true, nil },
		Stop:                 func(string) error { return nil },
		Start:                func(string) error { return nil },
		Restart:              func(string) error { return nil },
		ReadFile:             func(string) ([]byte, error) { return nil, os.ErrNotExist },
		WriteFileAtomic:      func(string, []byte) error { return nil },
		WriteTempFile:        func(string, []byte) error { return nil },
		DaemonReload:         func() error { return nil },
		Prove:                func(string) backup.ProveVerdict { return prove },
	}
}

// matchingCurrent returns a CurrentInstall that EXACTLY matches the test archive's
// manifest fields, so no skew is detected by default.
func matchingCurrent() backup.CurrentInstall {
	return backup.CurrentInstall{
		VillaVersion:       "v1.0.0",
		InferenceImage:     "inf@sha256:aaa",
		OpenWebUIImage:     "owui@sha256:bbb",
		UsageSchemaVersion: 1,
		BenchSchemaVersion: 1,
	}
}

func matchingManifestInput() backup.ManifestInput {
	return backup.ManifestInput{
		VillaVersion:       "v1.0.0",
		InferenceImage:     "inf@sha256:aaa",
		OpenWebUIImage:     "owui@sha256:bbb",
		UsageSchemaVersion: 1,
		BenchSchemaVersion: 1,
	}
}

func newRestoreTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := newRestore()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// baseRestoreInput builds a RestoreInput over the archive at path with a matching
// Current (no skew), consent granted, into a temp data dir.
func baseRestoreInput(t *testing.T, path string) backup.RestoreInput {
	t.Helper()
	tmp := t.TempDir()
	return backup.RestoreInput{
		OpenArchive:         func() (io.ReadCloser, error) { return os.Open(path) },
		Current:             matchingCurrent(),
		Consent:             func(string) bool { return true },
		OpenWebUIVolumeName: "villa-openwebui",
		TempVolumeTar:       filepath.Join(tmp, "restore-owui.tar"),
		RollbackVolumeTar:   filepath.Join(tmp, "rollback-owui.tar"),
		UsageDestPath:       filepath.Join(tmp, "usage.json"),
		BenchDestPath:       filepath.Join(tmp, "bench-reports.jsonl"),
	}
}

var restoreCfgTOML = []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 4096\n")

// TestRestoreRequiresPositionalArchive asserts the command requires exactly one
// positional archive arg.
func TestRestoreRequiresPositionalArchive(t *testing.T) {
	cmd := newRestore()
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatalf("restore must require a positional archive arg (got nil error for zero args)")
	}
	if err := cmd.Args(cmd, []string{"a.tar"}); err != nil {
		t.Fatalf("restore must accept exactly one archive arg, got %v", err)
	}
}

// TestRestoreHappyPathExitsPass: a valid archive + matching current + pass prove ->
// Restored -> exitPass.
func TestRestoreHappyPathExitsPass(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	cmd, out, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitPass {
		t.Fatalf("runRestore = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("restored")) {
		t.Fatalf("expected a success message, got %q", out.String())
	}
}

// TestRestoreConsentDeniedExitsBlocked: a WARN skew + consent denied -> Refused ->
// exitBlocked.
func TestRestoreConsentDeniedExitsBlocked(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	in := baseRestoreInput(t, arch)
	in.Current.VillaVersion = "v9.9.9" // WARN-only skew
	in.Consent = func(string) bool { return false }

	cmd, _, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, in, fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitBlocked {
		t.Fatalf("declined consent: runRestore = %d, want %d", code, exitBlocked)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("refusing")) {
		t.Fatalf("expected a refusal message, got %q", errOut.String())
	}
}

// TestRestoreYesBypassesConsent: a WARN skew + Bypass=true applies WITHOUT calling
// Consent -> exitPass.
func TestRestoreYesBypassesConsent(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	in := baseRestoreInput(t, arch)
	in.Current.VillaVersion = "v9.9.9" // WARN-only skew
	in.Consent = func(string) bool { t.Fatalf("Consent must NOT be called when --yes/Bypass is set"); return false }
	in.Bypass = true

	cmd, _, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, in, fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitPass {
		t.Fatalf("--yes over WARN skew: runRestore = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
}

// TestRestoreOffloadFailRollsBack: a non-pass prove (residency FAIL) -> RolledBack ->
// exitBlocked (never a false-green).
func TestRestoreOffloadFailRollsBack(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	cmd, _, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, baseRestoreInput(t, arch),
		fakeRestoreDeps(backup.ProveVerdict{Status: "fail", Detail: "residency FAIL (CPU fallback)"}))
	if code != exitBlocked {
		t.Fatalf("offload-FAIL prove: runRestore = %d, want %d", code, exitBlocked)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("rolled back")) {
		t.Fatalf("expected a rollback message, got %q", errOut.String())
	}
}

// writeTestArchiveMem clones writeTestArchive with the OPTIONAL Phase-23 memory
// entries (qdrant-volume.tar / recall-state.json); nil omits an entry.
func writeTestArchiveMem(t *testing.T, path string, m backup.ManifestInput, cfgTOML, owui, qdrant, recallState []byte) {
	t.Helper()
	type e struct {
		name string
		data []byte
	}
	data := []e{{backup.EntryConfig, cfgTOML}, {backup.EntryOpenWebUIVolume, owui}}
	if qdrant != nil {
		data = append(data, e{backup.EntryQdrantVolume, qdrant})
	}
	if recallState != nil {
		data = append(data, e{backup.EntryRecallState, recallState})
	}
	var sums []backup.EntryChecksum
	for _, d := range data {
		h := sha256.Sum256(d.data)
		sums = append(sums, backup.EntryChecksum{Name: d.name, SHA256: hex.EncodeToString(h[:])})
	}
	m.Entries = sums
	man := backup.BuildManifest(m)
	mj, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	all := []e{{backup.EntryManifest, mj}}
	all = append(all, data...)
	for _, d := range all {
		if err := tw.WriteHeader(&tar.Header{Name: d.name, Mode: 0o600, Size: int64(len(d.data)), Format: tar.FormatPAX}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(d.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreOutputMemoryNotPresent asserts the honest D-07 reporting on a
// memory-FREE backup: the not-present-left-untouched line and the restored-config
// memory-posture line (Pitfall 5) both print.
func TestRestoreOutputMemoryNotPresent(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	cmd, out, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitPass {
		t.Fatalf("runRestore = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("memory volume not present in this backup — existing Qdrant data left untouched")) {
		t.Fatalf("memory-free restore must print the not-present-left-untouched line, got %q", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("memory stack: disabled (restored config)")) {
		t.Fatalf("restore must print the restored-config memory posture, got %q", out.String())
	}
}

// TestRestoreOutputMemoryRestored asserts the memory-bearing success output: the
// restored lines, the ENABLED posture from the restored config, and the
// verify/re-index remediation note (OQ1: honest report, no Prove extension).
func TestRestoreOutputMemoryRestored(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	memTOML := []byte("model = \"m\"\nbackend = \"vulkan\"\nctx = 4096\nmemory_enabled = true\n")
	writeTestArchiveMem(t, arch, matchingManifestInput(), memTOML, []byte("owui"), []byte("qdrant"), []byte("recall"))

	in := baseRestoreInput(t, arch)
	tmp := t.TempDir()
	in.QdrantVolumeName = "qdrant-vol"
	in.TempQdrantTar = filepath.Join(tmp, "restore-qdrant.tar")
	in.RollbackQdrantTar = filepath.Join(tmp, "rollback-qdrant.tar")
	in.QdrantVolumeExists = true
	in.RecallDestPath = filepath.Join(tmp, "recall-state.json")

	cmd, out, errOut := newRestoreTestCmd()
	code := runRestore(cmd, arch, in, fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitPass {
		t.Fatalf("runRestore = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	for _, want := range []string{
		"memory stack: enabled (restored config)",
		"villa doctor",
		"villa recall index --rebuild",
	} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Fatalf("memory-restored output must contain %q, got %q", want, out.String())
		}
	}
}

// TestRestoreCorruptArchiveBlocks: a tampered entry (checksum mismatch) is a fail-closed
// BLOCK -> exitBlocked with zero side effects.
func TestRestoreCorruptArchiveBlocks(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))
	// Corrupt the archive bytes on disk so a recorded checksum no longer matches.
	raw, err := os.ReadFile(arch)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the owui payload region (the tail, past the manifest/config).
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(arch, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd, _, _ := newRestoreTestCmd()
	code := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(backup.ProveVerdict{Status: backup.ProveStatusPass}))
	if code != exitBlocked {
		t.Fatalf("corrupt archive: runRestore = %d, want %d", code, exitBlocked)
	}
}
