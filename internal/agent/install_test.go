package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// install_test.go drives the Plan-02 live half of the agent core: agent.Run's
// absent/first-run-render/drift/clean flow (AGENT-03/AGENT-04) and the
// checksum-before-extract install seam (AGENT-01). Every host effect is an
// injected Deps func field / an in-memory reader, so the whole flow is driven
// off-hardware.

// pinnedPolicyBinSHA is the REAL extracted-binary SHA-256 pinned in crush-policy.json
// (Plan 03, on-hardware). Run() loads the embedded policy, so a fake installed binary
// must carry this exact hash to be treated as non-drifting; any other value is now a
// confident BinaryDrift (the sentinel-WARN era ended when the hash was pinned).
const pinnedPolicyBinSHA = "e59e1baecfc810027d424a8ef2a23d6ed091ca93ac36f5f32e95ce6788c5fbf9"

// runRecorder captures the side-effecting seam calls so the flow tests can assert
// what Run did (and did NOT do) without a live host.
type runRecorder struct {
	cfg           config.VillaConfig
	loadErr       error
	binSHA        string
	binPresent    bool
	binErr        error
	onDisk        []byte
	configPresent bool
	readErr       error
	lookFound     map[string]bool
	writeCalls    [][]byte
	writeErr      error
	backupCalls   [][]byte
	backupErr     error
	launchCalls   [][]string
	launchErr     error
	// order records the config-writing seams in call order ("backup", "write").
	order []string
}

// deps wires the recorder into an agent.Deps with sensible defaults (gopls found).
func (r *runRecorder) deps() Deps {
	return Deps{
		LoadConfig: func() (config.VillaConfig, error) { return r.cfg, r.loadErr },
		LookPath: func(bin string) (string, bool) {
			if r.lookFound[bin] {
				return "/usr/bin/" + bin, true
			}
			return "", false
		},
		ReadConfig: func() ([]byte, bool, error) { return r.onDisk, r.configPresent, r.readErr },
		HashBinary: func() (string, bool, error) { return r.binSHA, r.binPresent, r.binErr },
		WriteConfig: func(b []byte) error {
			r.order = append(r.order, "write")
			r.writeCalls = append(r.writeCalls, b)
			return r.writeErr
		},
		BackupConfig: func(b []byte) error {
			r.order = append(r.order, "backup")
			r.backupCalls = append(r.backupCalls, b)
			return r.backupErr
		},
		Launch: func(env []string) error { r.launchCalls = append(r.launchCalls, env); return r.launchErr },
	}
}

// renderedRef renders the reference crush.json for a config the way Run does, so a
// test can seed a matching (non-drifting) on-disk config.
func renderedRef(t *testing.T, cfg config.VillaConfig) []byte {
	t.Helper()
	b, _, err := Render(cfg, nil)
	if err != nil {
		t.Fatalf("render reference: %v", err)
	}
	return b
}

// TestRunBinaryAbsent — a not-present binary yields BinaryAbsent + a Phase-27 install
// remediation Reason; NO Launch, NO WriteConfig.
func TestRunBinaryAbsent(t *testing.T) {
	rec := &runRecorder{
		cfg:        config.VillaConfig{Model: "qwen3", CodingMode: true},
		binPresent: false,
	}
	res := Run(rec.deps())
	if !res.BinaryAbsent {
		t.Fatalf("BinaryAbsent = false, want true; res=%+v", res)
	}
	if res.ReadyToLaunch {
		t.Errorf("ReadyToLaunch = true on binary-absent; must not launch")
	}
	if len(rec.writeCalls) != 0 {
		t.Errorf("WriteConfig called %d times on binary-absent; want 0", len(rec.writeCalls))
	}
	if !strings.Contains(res.Reason, "install") {
		t.Errorf("Reason %q does not mention the install remediation", res.Reason)
	}
}

// TestRunFirstRunRendersThenLaunches — a present, non-drifting binary with the config
// ABSENT renders the reference via WriteConfig exactly once, then Launches exactly
// once; it does NOT report a config-drift error (config-absent is a clean render path).
func TestRunFirstRunRendersThenLaunches(t *testing.T) {
	rec := &runRecorder{
		cfg:           config.VillaConfig{Model: "qwen3", CodingMode: true},
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: false, // first run — no crush.json yet
	}
	res := Run(rec.deps())
	if res.ConfigDrift {
		t.Fatalf("ConfigDrift = true on first run; absent must NOT be drift; res=%+v", res)
	}
	if !res.ConfigAbsent {
		t.Errorf("ConfigAbsent = false on first run; want true")
	}
	if len(rec.writeCalls) != 1 {
		t.Fatalf("WriteConfig called %d times; want exactly 1 (first-run render)", len(rec.writeCalls))
	}
	wantRef := renderedRef(t, rec.cfg)
	if !bytes.Equal(rec.writeCalls[0], wantRef) {
		t.Errorf("first-run WriteConfig bytes != freshly-rendered reference")
	}
	if !res.ReadyToLaunch {
		t.Errorf("ReadyToLaunch = false after first-run render; want true (render-then-launch)")
	}
	// Run resolves the env but does NOT exec — the caller (runCode) is the single
	// launch point, so it can surface warnings before the process is replaced.
	if len(rec.launchCalls) != 0 {
		t.Errorf("Run called Launch %d times; want 0 (Run never execs — caller does)", len(rec.launchCalls))
	}
}

// TestRunDriftSurfaced — a PRESENT-but-differing config surfaces ConfigDrift +
// remediation; Launch NOT called; WriteConfig NOT called (present-but-differs
// is never auto-corrected).
func TestRunDriftSurfaced(t *testing.T) {
	rec := &runRecorder{
		cfg:           config.VillaConfig{Model: "qwen3", CodingMode: true},
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: true,
		onDisk:        []byte(`{"$schema":"hand-edited","options":{"disable_metrics":false}}`),
	}
	res := Run(rec.deps())
	if !res.ConfigDrift {
		t.Fatalf("ConfigDrift = false on present-but-differs config; want true; res=%+v", res)
	}
	if res.ReadyToLaunch || len(rec.launchCalls) != 0 {
		t.Errorf("must NOT be ReadyToLaunch on config drift")
	}
	if len(rec.writeCalls) != 0 {
		t.Errorf("WriteConfig must NOT be called on config drift (never auto-correct)")
	}
	if res.Reason == "" {
		t.Errorf("config drift carried no remediation Reason")
	}
	if len(rec.backupCalls) != 0 {
		t.Errorf("BackupConfig called on an operator edit; a refused drift writes nothing")
	}
}

// keyOnlyRecorder stages a host whose crush.json is the pre-v1.14 rendering of the
// current config: identical but for api_key, which holds the placeholder `local`
// (#277). The config carries a fake 64-hex secret.
func keyOnlyRecorder(t *testing.T) *runRecorder {
	t.Helper()
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true, InferenceSecret: "local"}
	stale := renderedRef(t, cfg)
	cfg.InferenceSecret = testInferenceKey
	return &runRecorder{
		cfg:           cfg,
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: true,
		onDisk:        stale,
	}
}

// TestRunHealsAKeyOnlyDrift guards ADR-0019 in `villa code`: a crush.json whose only
// difference is villa's own inference key is backed up, rewritten from the
// reference, reported as a warning, and launched as clean. It is not a refusal.
func TestRunHealsAKeyOnlyDrift(t *testing.T) {
	rec := keyOnlyRecorder(t)
	stale := rec.onDisk
	res := Run(rec.deps())
	if res.Err != nil || res.ConfigDrift {
		t.Fatalf("key-only drift: Err=%v ConfigDrift=%v, want a heal, not a refusal (res=%+v)", res.Err, res.ConfigDrift, res)
	}
	if !res.ReadyToLaunch {
		t.Errorf("ReadyToLaunch = false after the heal; want the clean launch")
	}
	if want := []string{"backup", "write"}; strings.Join(rec.order, ",") != strings.Join(want, ",") {
		t.Fatalf("config seams = %v, want %v (the old file is kept before it is overwritten)", rec.order, want)
	}
	if !bytes.Equal(rec.backupCalls[0], stale) {
		t.Errorf("BackupConfig got %q, want the on-disk bytes verbatim", rec.backupCalls[0])
	}
	if !bytes.Equal(rec.writeCalls[0], renderedRef(t, rec.cfg)) {
		t.Errorf("WriteConfig bytes != the freshly rendered reference")
	}
	var healed *Warning
	for i := range res.Warnings {
		if res.Warnings[i].Code == "config_key_healed" {
			healed = &res.Warnings[i]
		}
	}
	if healed == nil {
		t.Fatalf("no config_key_healed warning; got %+v", res.Warnings)
	}
	if !strings.Contains(healed.Msg, "crush.json.bak") || strings.Contains(healed.Msg, testInferenceKey) {
		t.Errorf("heal warning %q must name the backup and must not carry the key", healed.Msg)
	}
}

// TestRunKeyHealFailureLaunchesNothing: an I/O failure during the heal is an error,
// never a launch against a crush.json villa could not repair. A failed backup
// writes nothing over the operator's file.
func TestRunKeyHealFailureLaunchesNothing(t *testing.T) {
	cases := map[string]struct {
		backupErr, writeErr error
		wantOrder           string
	}{
		"backup fails": {backupErr: os.ErrPermission, wantOrder: "backup"},
		"write fails":  {writeErr: os.ErrPermission, wantOrder: "backup,write"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := keyOnlyRecorder(t)
			rec.backupErr, rec.writeErr = tc.backupErr, tc.writeErr
			res := Run(rec.deps())
			if !errors.Is(res.Err, os.ErrPermission) || res.ReadyToLaunch {
				t.Fatalf("Err=%v ReadyToLaunch=%v, want the wrapped I/O error and no launch", res.Err, res.ReadyToLaunch)
			}
			if got := strings.Join(rec.order, ","); got != tc.wantOrder {
				t.Errorf("config seams = %q, want %q", got, tc.wantOrder)
			}
		})
	}
}

// TestRunLaunchesClean — a present, non-drifting binary + a present, MATCHING config
// calls Launch exactly once with the three lockdown vars, does NOT WriteConfig, and
// returns Launched.
func TestRunLaunchesClean(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true}
	rec := &runRecorder{
		cfg:           cfg,
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: true,
		onDisk:        renderedRef(t, cfg),
	}
	res := Run(rec.deps())
	if res.Err != nil {
		t.Fatalf("clean Run returned Err: %v (res=%+v)", res.Err, res)
	}
	if !res.ReadyToLaunch {
		t.Fatalf("ReadyToLaunch = false on the clean path; res=%+v", res)
	}
	if len(rec.writeCalls) != 0 {
		t.Errorf("WriteConfig called on the clean (config-present-matching) path; want 0")
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("Run called Launch %d times; want 0 (Run resolves env only — caller execs)", len(rec.launchCalls))
	}
	// The resolved lockdown env is returned on the Result for the caller to exec with.
	for _, want := range []string{envCrushDisableMetrics, envDoNotTrack, envCrushDisableAutoUpdate} {
		if !containsEnv(res.LaunchEnv, want) {
			t.Errorf("LaunchEnv missing lockdown var %q; env=%v", want, res.LaunchEnv)
		}
	}
}

// TestRunCodingModeOffWarns — cfg.CodingMode=false adds a coding_mode_off WARN but
// STILL launches (Run never mutates the toggle).
func TestRunCodingModeOffWarns(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: false}
	rec := &runRecorder{
		cfg:           cfg,
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: true,
		onDisk:        renderedRef(t, cfg),
	}
	res := Run(rec.deps())
	if !res.ReadyToLaunch {
		t.Fatalf("coding-mode-off must still be ReadyToLaunch; res=%+v", res)
	}
	if !hasWarning(res.Warnings, "coding_mode_off") {
		t.Errorf("coding-mode-off did not carry a coding_mode_off WARN; warnings=%+v", res.Warnings)
	}
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func hasWarning(ws []Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

// makeTarGz builds a gzip-tar containing the given {name: content} entries, for the
// install-seam tests.
func makeTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar body %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// assetFor builds a CrushAsset pinned to the given tarball bytes (matching size +
// checksum), for the verify-pass path.
func assetFor(tarball []byte) CrushAsset {
	return CrushAsset{
		Name:   "crush_test.tar.gz",
		SHA256: sha256Hex(tarball),
		Size:   uint64(len(tarball)),
	}
}

// TestInstallVerifyBeforeExtract — Install refuses (and does NOT extract) on a
// checksum mismatch; on a matching tarball it extracts ONLY the crush entry; a
// traversal entry is rejected.
func TestInstallVerifyBeforeExtract(t *testing.T) {
	t.Run("checksum mismatch refuses without extracting", func(t *testing.T) {
		tarball := makeTarGz(t, map[string]string{"crush": "#!/bin/sh\necho crush\n"})
		asset := assetFor(tarball)
		asset.SHA256 = strings.Repeat("0", 64) // wrong checksum, correct size
		binDir := t.TempDir()
		_, err := Install(asset, bytes.NewReader(tarball), binDir)
		if err == nil {
			t.Fatalf("Install accepted a checksum-mismatched tarball")
		}
		if !strings.Contains(err.Error(), "unverified") && !strings.Contains(err.Error(), "checksum") {
			t.Errorf("error %q does not refuse-with-remediation on checksum mismatch", err)
		}
		if _, statErr := readBin(binDir); statErr == nil {
			t.Errorf("a binary was extracted despite a checksum mismatch (no checksum-before-extract)")
		}
	})

	t.Run("matching tarball extracts only the crush binary", func(t *testing.T) {
		body := "#!/bin/sh\necho crush\n"
		tarball := makeTarGz(t, map[string]string{
			"crush":   body,
			"LICENSE": "MIT",
			"README":  "readme",
		})
		asset := assetFor(tarball)
		binDir := t.TempDir()
		binPath, err := Install(asset, bytes.NewReader(tarball), binDir)
		if err != nil {
			t.Fatalf("Install verify-pass failed: %v", err)
		}
		got, statErr := readBin(binDir)
		if statErr != nil {
			t.Fatalf("crush binary not placed: %v", statErr)
		}
		if string(got) != body {
			t.Errorf("placed binary content = %q, want %q", got, body)
		}
		if !strings.HasSuffix(binPath, "crush") {
			t.Errorf("returned binPath %q does not end in crush", binPath)
		}
	})

	t.Run("traversal entry is rejected", func(t *testing.T) {
		tarball := makeTarGz(t, map[string]string{"../escape": "pwned"})
		asset := assetFor(tarball)
		binDir := t.TempDir()
		_, err := Install(asset, bytes.NewReader(tarball), binDir)
		if err == nil {
			t.Fatalf("Install accepted a traversal tar entry")
		}
		if !strings.Contains(err.Error(), "traversal") && !strings.Contains(err.Error(), "outside") {
			t.Errorf("error %q does not name the traversal guard", err)
		}
	})

	t.Run("size mismatch refuses before extract", func(t *testing.T) {
		tarball := makeTarGz(t, map[string]string{"crush": "x"})
		asset := assetFor(tarball)
		asset.Size = asset.Size + 100 // wrong size
		binDir := t.TempDir()
		_, err := Install(asset, bytes.NewReader(tarball), binDir)
		if err == nil {
			t.Fatalf("Install accepted a size-mismatched tarball")
		}
	})
}

// readBin reads binDir/crush, returning the os error if absent.
func readBin(binDir string) ([]byte, error) {
	return os.ReadFile(binDir + "/" + crushBinaryName)
}
