package main

// restore_test.go guards the `villa restore` cmd-tier wiring: the
// positional archive arg is required, --yes bypasses the skew consent gate, a declined
// consent / a non-pass prove map to the right exit codes, and a Restored result exits
// 0. The full transactional ordering invariants (clean-recreate-before-import, capture-
// before-mutate, rollback) are covered by the PURE internal/backup restore_test.go;
// this file asserts the cobra surface + Result->exit mapping over a fake backup.RestoreDeps.

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/eval"
	"github.com/MatrixMagician/VillaStraylight/internal/evalstore"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
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

// fakeRestoreDeps returns a backup.RestoreDeps whose seams are all no-op successes, plus the
// canned prove verdict. The capture LoadConfig returns a vulkan config so rollback has
// a prior to restore.
func fakeRestoreDeps(verdict prove.Verdict) backup.RestoreDeps {
	return backup.RestoreDeps{
		OpenWebUIServiceName: openWebUIServiceName,
		LoadConfig:           func() (config.VillaConfig, error) { return config.VillaConfig{Backend: "vulkan", Model: "m"}, nil },
		SaveConfig:           func(config.VillaConfig) error { return nil },
		VolumeExport:         func(_, _ string) error { return nil },
		VolumeImport:         func(_, _ string) error { return nil },
		VolumeRm:             func(string) error { return nil },
		EnsureVolume:         func(string) error { return nil },
		ReconcileAndWrite:    func(config.VillaConfig) (bool, error) { return true, nil },
		Stop:                 func(string) error { return nil },
		Start:                func(string) error { return nil },
		ReadFile:             func(string) ([]byte, error) { return nil, os.ErrNotExist },
		WriteFile:            func(string, []byte) error { return nil },
		Prove:                func(string) prove.Verdict { return verdict },
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
		Dests: map[string]string{
			backup.EntryUsage:        filepath.Join(tmp, "usage.json"),
			backup.EntryBenchReports: filepath.Join(tmp, "bench-reports.jsonl"),
		},
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
	code, _ := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
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
	code, _ := runRestore(cmd, arch, in, fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
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
	code, _ := runRestore(cmd, arch, in, fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
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
	code, preserve := runRestore(cmd, arch, baseRestoreInput(t, arch),
		fakeRestoreDeps(prove.Verdict{Status: prove.StatusFail, Detail: "residency FAIL (CPU fallback)"}), "")
	if code != exitBlocked {
		t.Fatalf("offload-FAIL prove: runRestore = %d, want %d", code, exitBlocked)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("rolled back")) {
		t.Fatalf("expected a rollback message, got %q", errOut.String())
	}
	// A CLEAN rollback must not ask the caller to preserve the temp dir.
	if preserve {
		t.Fatalf("a complete rollback must not preserve the restore temp dir")
	}
}

// TestRestoreRollbackIncompletePreservesTmpDir is the cmd-tier regression:
// when the rollback did NOT fully complete (here: every VolumeRm fails "in use"),
// runRestore must report preserveTmp=true and print the preservation notice naming
// the temp dir — the rollback tars it holds are the ONLY copies of the prior
// webui.db / Qdrant vectors, and the old unconditional cleanup deleted them.
func TestRestoreRollbackIncompletePreservesTmpDir(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	d := fakeRestoreDeps(prove.Verdict{Status: prove.StatusFail, Detail: "residency FAIL"})
	d.VolumeRm = func(string) error { return errors.New("volume is being used") }

	tmpDir := t.TempDir()
	cmd, _, errOut := newRestoreTestCmd()
	code, preserve := runRestore(cmd, arch, baseRestoreInput(t, arch), d, tmpDir)
	if code != exitBlocked {
		t.Fatalf("rollback-incomplete: runRestore = %d, want %d", code, exitBlocked)
	}
	if !preserve {
		t.Fatalf("an INCOMPLETE rollback must preserve the restore temp dir")
	}
	if !bytes.Contains(errOut.Bytes(), []byte("PRESERVING "+tmpDir)) {
		t.Fatalf("expected the preservation notice naming %q, got %q", tmpDir, errOut.String())
	}
	if !bytes.Contains(errOut.Bytes(), []byte("podman volume import")) {
		t.Fatalf("expected a recovery hint, got %q", errOut.String())
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

// TestRestoreOutputMemoryNotPresent asserts the honest reporting on a
// memory-FREE backup: the not-present-left-untouched line and the restored-config
// memory-posture line (Pitfall 5) both print.
func TestRestoreOutputMemoryNotPresent(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "b.tar")
	writeTestArchive(t, arch, matchingManifestInput(), restoreCfgTOML, []byte("owui"))

	cmd, out, errOut := newRestoreTestCmd()
	code, _ := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
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
	in.Dests[backup.EntryRecallState] = filepath.Join(tmp, "recall-state.json")

	cmd, out, errOut := newRestoreTestCmd()
	code, _ := runRestore(cmd, arch, in, fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
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

// TestRestoreWritesWebsafeSecretEnv is the regression: Phase 31 makes the restored
// OWUI + villa-websafe units carry EnvironmentFile={websafe.env} whenever the restored
// config has web search on. restore only restores config.toml (the 0600 env file lives
// outside the archive), so the reconcile/start path MUST write that 0600 websafe.env from
// the restored config's bearer BEFORE starting OWUI — otherwise `systemctl start
// villa-openwebui.service` fails on the absent EnvironmentFile. restoreWriteWebsafeSecretEnv
// is the gate the live ReconcileAndWrite closure calls; this drives it over a fake writer.
func TestRestoreWritesWebsafeSecretEnv(t *testing.T) {
	t.Run("web-search-on-with-secret-writes-env", func(t *testing.T) {
		var wrote int
		var gotName, gotText string
		writeEnv := func(name, text string) error {
			wrote++
			gotName, gotText = name, text
			return nil
		}
		cfg := config.VillaConfig{WebSearchEnabled: true, WebLoaderSecret: "deadbeefcafe"}
		if err := restoreWriteWebsafeSecretEnv(cfg, writeEnv); err != nil {
			t.Fatalf("restoreWriteWebsafeSecretEnv = %v, want nil", err)
		}
		if wrote != 1 {
			t.Fatalf("websafe.env must be written exactly once on a web-search restore, got %d writes", wrote)
		}
		if gotName == "" || gotText == "" {
			t.Fatalf("websafe.env render produced empty name/text: name=%q text=%q", gotName, gotText)
		}
		if !bytes.Contains([]byte(gotText), []byte("deadbeefcafe")) {
			t.Errorf("websafe.env body must carry the restored bearer, got %q", gotText)
		}
	})

	t.Run("web-search-on-without-secret-fails-closed", func(t *testing.T) {
		writeEnv := func(string, string) error {
			t.Fatalf("must NOT write the env file when the restored config has no bearer")
			return nil
		}
		cfg := config.VillaConfig{WebSearchEnabled: true, WebLoaderSecret: ""}
		err := restoreWriteWebsafeSecretEnv(cfg, writeEnv)
		if err == nil {
			t.Fatalf("a web-search restore with no bearer must fail closed, got nil error")
		}
		if !bytes.Contains([]byte(err.Error()), []byte("no web loader secret")) {
			t.Errorf("error must explain the missing bearer with remediation, got %v", err)
		}
	})

	t.Run("web-search-off-writes-nothing", func(t *testing.T) {
		writeEnv := func(string, string) error {
			t.Fatalf("a web-search-off restore must write no websafe.env")
			return nil
		}
		cfg := config.VillaConfig{WebSearchEnabled: false}
		if err := restoreWriteWebsafeSecretEnv(cfg, writeEnv); err != nil {
			t.Fatalf("web-search-off restore = %v, want nil (no-op)", err)
		}
	})
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
	code, _ := runRestore(cmd, arch, baseRestoreInput(t, arch), fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass}), "")
	if code != exitBlocked {
		t.Fatalf("corrupt archive: runRestore = %d, want %d", code, exitBlocked)
	}
}

// stubPodmanVolume swaps the podman runner for fn for the test's lifetime.
func stubPodmanVolume(t *testing.T, fn func(args []string) (string, error)) {
	t.Helper()
	prev := podmanVolume
	podmanVolume = fn
	t.Cleanup(func() { podmanVolume = prev })
}

// TestLiveRestoreAssemblesTheInput: liveRestore resolves the archive, the current
// install and the temp dir into a RestoreInput whose destinations are exactly the
// ones the current (agent-off, web-search-off) install wires, and whose qdrant
// existence is the tri-state check's answer.
func TestLiveRestoreAssemblesTheInput(t *testing.T) {
	scratchVillaHome(t, "backend = \"vulkan\"\n")
	stubPodmanVolume(t, func([]string) (string, error) { return "", nil }) // volume exists
	arch := filepath.Join(t.TempDir(), "b.tar")
	if err := os.WriteFile(arch, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd, _, errOut := newRestoreTestCmd()
	in, _, tmpDir, code := liveRestore(cmd, arch, true)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	if code != exitPass {
		t.Fatalf("liveRestore = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if want := []string{backup.EntryBenchReports, backup.EntryEvalBaselines, backup.EntryRecallState, backup.EntryUsage}; !slices.Equal(sortedKeys(in.Dests), want) {
		t.Fatalf("dests = %v, want %v", sortedKeys(in.Dests), want)
	}
	if !in.Bypass || !in.QdrantVolumeExists || in.QdrantVolumeUnknown {
		t.Fatalf("want bypass + existing known qdrant volume, got %+v", in)
	}
	if filepath.Dir(in.TempVolumeTar) != tmpDir || filepath.Dir(in.RollbackQdrantTar) != tmpDir {
		t.Fatalf("volume tars must live in the restore temp dir %q, got %q / %q", tmpDir, in.TempVolumeTar, in.RollbackQdrantTar)
	}
	if rc, err := in.OpenArchive(); err != nil {
		t.Fatalf("OpenArchive: %v", err)
	} else {
		_ = rc.Close()
	}
}

// TestLiveRestoreRefusesBeforeAnyEffect: an unreadable archive, an unreadable config
// and an unwritable temp dir each exit blocked with a message and an EMPTY tmpDir —
// the caller has nothing to clean up, because nothing was created.
func TestLiveRestoreRefusesBeforeAnyEffect(t *testing.T) {
	good := filepath.Join(t.TempDir(), "b.tar")
	if err := os.WriteFile(good, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		cfgTOML string
		archive string
		tmpdir  string
		want    string
	}{
		{"missing archive", "", filepath.Join(t.TempDir(), "absent.tar"), "", "restore: cannot read archive"},
		{"unreadable config", "model = [unterminated\n", good, "", "restore: load config"},
		{"unwritable temp dir", "", good, filepath.Join(t.TempDir(), "no-such-dir"), "restore: temp dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scratchVillaHome(t, tt.cfgTOML)
			stubPodmanVolume(t, func([]string) (string, error) { return "", nil })
			if tt.tmpdir != "" {
				t.Setenv("TMPDIR", tt.tmpdir)
			}
			cmd, _, errOut := newRestoreTestCmd()
			_, _, tmpDir, code := liveRestore(cmd, tt.archive, false)
			if code != exitBlocked || tmpDir != "" {
				t.Fatalf("liveRestore = %d tmpDir %q, want blocked and no temp dir; stderr=%q", code, tmpDir, errOut.String())
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("stderr %q does not contain %q", errOut.String(), tt.want)
			}
		})
	}
}

// TestLiveCurrentInstallRefusesAnUnknownBackend: the skew compare needs the backend
// image digest from the seam, so a backend the seam does not know is a refusal that
// names it, not a silent default.
func TestLiveCurrentInstallRefusesAnUnknownBackend(t *testing.T) {
	_, err := liveCurrentInstall(config.VillaConfig{Backend: "bogus"})
	if err == nil || !strings.Contains(err.Error(), `resolve backend "bogus"`) {
		t.Fatalf("want a refusal naming the backend, got %v", err)
	}
}

// TestNarrateRestoredReportsEachFileOutcome: every optional file entry is reported
// as restored, skipped (carried but this install has no destination — a subsystem is
// off) or absent, and a skip is never reported as a restore.
func TestNarrateRestoredReportsEachFileOutcome(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]backup.FileOutcome
		want  []string
	}{
		{"nothing carried", nil, []string{
			recallRestoreNarration.absent, crushRestoreNarration.absent, searxngRestoreNarration.absent,
		}},
		{"all restored", map[string]backup.FileOutcome{
			backup.EntryRecallState:     {Restored: true},
			backup.EntryCrushConfig:     {Restored: true},
			backup.EntrySearxngSettings: {Restored: true},
		}, []string{
			recallRestoreNarration.restored, crushRestoreNarration.restored, searxngRestoreNarration.restored,
		}},
		{"carried but unwired", map[string]backup.FileOutcome{
			backup.EntryCrushConfig:     {Skipped: true},
			backup.EntrySearxngSettings: {Skipped: true},
		}, []string{crushRestoreNarration.skipped, searxngRestoreNarration.skipped}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			narrateRestored(&out, "b.tar", backup.Result{Restored: true, Files: tt.files, ExcludedAgent: &backup.ExcludedAgent{Version: "v0.76.0"}})
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("report missing %q, got %q", want, out.String())
				}
			}
			if !strings.Contains(out.String(), "(pinned v0.76.0)") {
				t.Errorf("the excluded agent identity must be reported, got %q", out.String())
			}
		})
	}
}

// TestRunRestoreReportsRefusalAndRollback: the Result maps to the right messages —
// a refusal names its reason, else its failed step, else just the archive; a
// rollback that did not complete preserves the temp dir and says how to recover.
func TestRunRestoreReportsRefusalAndRollback(t *testing.T) {
	var out bytes.Buffer
	reportRefused(&out, "b.tar", backup.Result{Refused: true, Reason: "because"})
	reportRefused(&out, "b.tar", backup.Result{Refused: true, FailedStep: "capture", Err: errors.New("boom")})
	reportRefused(&out, "b.tar", backup.Result{Refused: true})
	for _, want := range []string{
		"refusing to apply b.tar — because",
		"refusing to apply b.tar — capture failed: boom",
		"refusing to apply b.tar\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("refusal report missing %q, got %q", want, out.String())
		}
	}

	out.Reset()
	incomplete := backup.Result{RolledBack: true, RollbackIncomplete: true, FailedStep: "volume", Reason: "r", Err: errors.New("e")}
	if !reportRolledBack(&out, "b.tar", "/tmp/keep", incomplete) {
		t.Fatalf("an incomplete rollback must ask the caller to preserve the temp dir")
	}
	for _, want := range []string{"rolled back", "detail: r", "error:  e", "PRESERVING /tmp/keep"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("rollback report missing %q, got %q", want, out.String())
		}
	}
	if reportRolledBack(io.Discard, "b.tar", "/tmp/keep", backup.Result{RolledBack: true}) {
		t.Fatalf("a complete rollback must not preserve the temp dir")
	}
}

// evalDoc is an eval-baselines.json document holding one baseline per model.
func evalDoc(t *testing.T, models ...string) []byte {
	t.Helper()
	doc := evalstore.Document{SchemaVersion: evalstore.SchemaVersion()}
	for _, m := range models {
		doc.Baselines = append(doc.Baselines, eval.Baseline{Key: eval.Key{Model: m, Quant: "Q4", SuiteVersion: 1}})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// realWriteRestoreDeps is the live restore wiring minus podman, systemd and the
// proof: config and files go through the real seams onto the scratch XDG roots.
func realWriteRestoreDeps() backup.RestoreDeps {
	d := fakeRestoreDeps(prove.Verdict{Status: prove.StatusPass})
	d.LoadConfig = config.LoadVilla
	d.SaveConfig = config.SaveVilla
	d.ReadFile = os.ReadFile
	d.WriteFile = liveRestoreWriteFile
	d.RemoveFile = func(p string) error {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return d
}

// TestBackupRestoreRoundTripsEvalBaselines is #275's acceptance over the REAL
// cmd-tier wiring on a scratch XDG root: back up an install with eval baselines,
// let the store drift, restore, and eval-baselines.json is the backed-up bytes
// again — byte for byte. A baseline recorded after the backup is replaced, and the
// restore WARNS naming it, because it cannot be re-recorded; a restore that loses
// nothing says nothing.
func TestBackupRestoreRoundTripsEvalBaselines(t *testing.T) {
	tests := []struct {
		name     string
		drifted  []string
		wantWarn string
	}{
		{"a baseline recorded after the backup is named", []string{"m1", "later"}, "later Q4 (suite v1)"},
		{"nothing lost, nothing said", []string{"m1"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scratchVillaHome(t, "backend = \"vulkan\"\n")
			stubPodmanVolume(t, func([]string) (string, error) { return "", nil })
			store := evalstore.Path()
			if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
				t.Fatal(err)
			}
			backedUp := evalDoc(t, "m1", "m2")
			if err := os.WriteFile(store, backedUp, 0o600); err != nil {
				t.Fatal(err)
			}

			archive := filepath.Join(t.TempDir(), "villa-backup.tar")
			bcmd, _, berr := newBackupTestCmd()
			if code := runBackup(bcmd, archive, fakeRunDeps(t, nil)); code != exitPass {
				t.Fatalf("runBackup = %d; stderr=%q", code, berr.String())
			}

			if err := os.WriteFile(store, evalDoc(t, tt.drifted...), 0o600); err != nil {
				t.Fatal(err)
			}

			rcmd, out, rerr := newRestoreTestCmd()
			in, _, tmpDir, code := liveRestore(rcmd, archive, true)
			if code != exitPass {
				t.Fatalf("liveRestore = %d; stderr=%q", code, rerr.String())
			}
			t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
			if code, _ := runRestore(rcmd, archive, in, realWriteRestoreDeps(), tmpDir); code != exitPass {
				t.Fatalf("runRestore = %d; stderr=%q", code, rerr.String())
			}

			got, err := os.ReadFile(store)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, backedUp) {
				t.Fatalf("eval-baselines.json must be restored byte for byte:\n got %s\nwant %s", got, backedUp)
			}
			warned := strings.Contains(out.String(), "warning: eval baselines")
			if tt.wantWarn == "" {
				if warned {
					t.Fatalf("a restore that loses no baseline must not warn, got %q", out.String())
				}
				return
			}
			if !warned || !strings.Contains(out.String(), tt.wantWarn) {
				t.Fatalf("the restore must warn naming %q, got %q", tt.wantWarn, out.String())
			}
		})
	}
}

// TestLiveEvalKeysNamesEachBaselineAndFailsClosed: the parser the restore core is
// handed reads a document the way the store does — each baseline's key as a label —
// and a document this villa cannot read names none (the store would treat it as
// empty too).
func TestLiveEvalKeysNamesEachBaselineAndFailsClosed(t *testing.T) {
	got := liveEvalKeysOf(evalDoc(t, "m1", "m2"))
	if want := []string{"m1 Q4 (suite v1)", "m2 Q4 (suite v1)"}; !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if keys := liveEvalKeysOf([]byte("not json")); len(keys) != 0 {
		t.Fatalf("an unreadable document names no baseline, got %v", keys)
	}
}
