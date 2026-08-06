package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// code_test.go exercises the `villa code` cmd-tier mapping of agent.Run's Result to
// exit codes + messages, driving runCode through a fake *agent.Deps without a live
// host: the lockdown env, the first-run config-absent render-then-launch, the
// binary-absent + present-but-differs drift remediation, and the
// coding-mode-OFF WARN that STILL launches. The no-auto-flip structural guard +
// the seam grep gate (asserting code.go holds no CodingMode literal and no backend
// marker) live in coding-mode_test.go / internal/inference and are run alongside.

// codeRecorder records the side-effecting agent.Deps seam calls so the exit-mapping
// tests can assert what runCode drove agent.Run to do (and not do).
type codeRecorder struct {
	cfg           config.VillaConfig
	binSHA        string
	binPresent    bool
	onDisk        []byte
	configPresent bool
	lookFound     map[string]bool

	writeCalls  [][]byte
	launchCalls [][]string
}

// deps builds a fake *agent.Deps from the recorder.
func (r *codeRecorder) deps() *agent.Deps {
	return &agent.Deps{
		LoadConfig: func() (config.VillaConfig, error) { return r.cfg, nil },
		LookPath: func(bin string) (string, bool) {
			if r.lookFound[bin] {
				return "/usr/bin/" + bin, true
			}
			return "", false
		},
		ReadConfig:  func() ([]byte, bool, error) { return r.onDisk, r.configPresent, nil },
		HashBinary:  func() (string, bool, error) { return r.binSHA, r.binPresent, nil },
		WriteConfig: func(b []byte) error { r.writeCalls = append(r.writeCalls, b); return nil },
		Launch:      func(env []string) error { r.launchCalls = append(r.launchCalls, env); return nil },
	}
}

// renderRef renders the reference crush.json the way agent.Run does, so a test can
// seed a matching (non-drifting) on-disk config.
func renderRef(t *testing.T, cfg config.VillaConfig) []byte {
	t.Helper()
	b, _, err := agent.Render(cfg, nil)
	if err != nil {
		t.Fatalf("render reference: %v", err)
	}
	return b
}

// TestCrushProviderPortMatchesInferenceServerPort is the cross-seam drift guard
// (v1.4-AUDIT-WARN-crush-port-drift): it ties the rendered crush.json provider
// baseURL port to inference.ServerPort(), failing if the two desync.
//
// WHY this lives in cmd/villa and NOT in internal/agent: the agent core deliberately
// imports NEITHER internal/inference NOR internal/detect (LOCKED seam invariant,
// agent.go:17-24); its loopback `http://127.0.0.1:8080/v1` literal in render.go is
// SANCTIONED but cannot source inference.ServerPort() without violating that seam.
// cmd/villa is the single package that legitimately imports BOTH cores, so it is the
// correct place to bind render.go's rendered provider port to inference.ServerPort().
// This mirrors the orchestrate.LlamaInNetworkEndpoint() precedent
// (internal/orchestrate/endpoint.go), which composes inference.ServerPort() because
// orchestrate MAY import inference — agent may not, so the coupling is asserted from
// the test tier instead.
//
// Contract: this guard FAILS if someone changes inference.serverPort (or render.go's
// providerBaseURL) without updating the other. Both are 8080 today, so it PASSES.
func TestCrushProviderPortMatchesInferenceServerPort(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true}
	rendered, _, err := agent.Render(cfg, nil)
	if err != nil {
		t.Fatalf("render crush.json: %v", err)
	}
	want := fmt.Sprintf("127.0.0.1:%d/v1", inference.ServerPort())
	if !bytes.Contains(rendered, []byte(want)) {
		t.Errorf("rendered crush.json provider baseURL does not embed inference.ServerPort()=%d (want substring %q); "+
			"render.go providerBaseURL desynced from the served inference port — update one to match the other",
			inference.ServerPort(), want)
	}
}

// newCodeCmd returns a cobra.Command with captured out/err buffers for runCode.
func newCodeCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "code"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// pinnedPolicyBinSHA is the REAL extracted-binary SHA-256 pinned in
// internal/agent/crush-policy.json (Plan 03, on-hardware). agent.Run loads the
// embedded policy, so a fake installed binary must carry this exact hash to be
// non-drifting; any other value is now a confident binary-drift signal.
const pinnedPolicyBinSHA = "4fd811f68c05da6c8d11fd1d5b6298a75ecc38a6c105a342b74e080cce8342b4"

// TestCodeLockdownEnv — on the clean path a fake Launch captures the env; it MUST
// contain the three lockdown vars.
func TestCodeLockdownEnv(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true}
	rec := &codeRecorder{cfg: cfg, binPresent: true, binSHA: pinnedPolicyBinSHA, configPresent: true, onDisk: renderRef(t, cfg)}
	cmd, _, _ := newCodeCmd()
	code := runCode(cmd, rec.deps())
	if code != exitPass {
		t.Fatalf("runCode exit = %d, want %d", code, exitPass)
	}
	if len(rec.launchCalls) != 1 {
		t.Fatalf("Launch called %d times; want 1", len(rec.launchCalls))
	}
	env := rec.launchCalls[0]
	for _, want := range []string{"CRUSH_DISABLE_METRICS=1", "DO_NOT_TRACK=1", "CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1"} {
		if !envHas(env, want) {
			t.Errorf("launch env missing lockdown var %q", want)
		}
	}
}

// TestCodeFirstRunRenders — config-absent renders the reference via WriteConfig AND
// still launches (first-run render-then-launch, no drift error surfaced).
func TestCodeFirstRunRenders(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true}
	rec := &codeRecorder{cfg: cfg, binPresent: true, binSHA: pinnedPolicyBinSHA, configPresent: false}
	cmd, _, _ := newCodeCmd()
	code := runCode(cmd, rec.deps())
	if code != exitPass {
		t.Fatalf("runCode exit = %d, want %d (first-run render-then-launch)", code, exitPass)
	}
	if len(rec.writeCalls) != 1 {
		t.Fatalf("WriteConfig called %d times; want 1 (first-run render)", len(rec.writeCalls))
	}
	if !bytes.Equal(rec.writeCalls[0], renderRef(t, cfg)) {
		t.Errorf("first-run WriteConfig bytes != freshly-rendered reference")
	}
	if len(rec.launchCalls) != 1 {
		t.Errorf("Launch called %d times; want 1 after first-run render", len(rec.launchCalls))
	}
}

// TestCodeBinaryAbsent — a not-present binary exits blocked with stderr remediation
// mentioning the install addon; Launch + WriteConfig NOT called.
func TestCodeBinaryAbsent(t *testing.T) {
	rec := &codeRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: true}, binPresent: false}
	cmd, _, errOut := newCodeCmd()
	code := runCode(cmd, rec.deps())
	if code != exitBlocked {
		t.Fatalf("runCode exit = %d, want %d on binary-absent", code, exitBlocked)
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("Launch must NOT be called on binary-absent")
	}
	if len(rec.writeCalls) != 0 {
		t.Errorf("WriteConfig must NOT be called on binary-absent")
	}
	if !strings.Contains(errOut.String(), "install") {
		t.Errorf("stderr %q does not mention the install remediation", errOut.String())
	}
}

// TestCodeDriftSurfaced — a present-but-differs config exits blocked with remediation;
// Launch + WriteConfig NOT called.
func TestCodeDriftSurfaced(t *testing.T) {
	rec := &codeRecorder{
		cfg:           config.VillaConfig{Model: "qwen3", CodingMode: true},
		binPresent:    true,
		binSHA:        pinnedPolicyBinSHA,
		configPresent: true,
		onDisk:        []byte(`{"$schema":"hand-edited","options":{"disable_metrics":false}}`),
	}
	cmd, _, errOut := newCodeCmd()
	code := runCode(cmd, rec.deps())
	if code != exitBlocked {
		t.Fatalf("runCode exit = %d, want %d on config drift", code, exitBlocked)
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("Launch must NOT be called on config drift")
	}
	if len(rec.writeCalls) != 0 {
		t.Errorf("WriteConfig must NOT be called on config drift (never auto-correct)")
	}
	if !strings.Contains(errOut.String(), "refusing") {
		t.Errorf("stderr %q does not surface the drift refusal", errOut.String())
	}
}

// TestCodeCodingModeOffWarns — cfg.CodingMode=false emits a stderr WARN pointing at
// `villa coding-mode enter` AND still launches.
func TestCodeCodingModeOffWarns(t *testing.T) {
	cfg := config.VillaConfig{Model: "qwen3", CodingMode: false}
	rec := &codeRecorder{cfg: cfg, binPresent: true, binSHA: pinnedPolicyBinSHA, configPresent: true, onDisk: renderRef(t, cfg)}
	cmd, _, errOut := newCodeCmd()
	code := runCode(cmd, rec.deps())
	if code != exitPass {
		t.Fatalf("runCode exit = %d, want %d (coding-off still launches)", code, exitPass)
	}
	if len(rec.launchCalls) != 1 {
		t.Errorf("Launch must still be called on coding-mode-off")
	}
	if !strings.Contains(errOut.String(), "coding-mode enter") {
		t.Errorf("stderr %q does not point at `villa coding-mode enter`", errOut.String())
	}
}

// TestCodeRegistered — `villa code` is registered on the root tree as a NoArgs verb,
// a sibling of `coding-mode` (not a subcommand).
func TestCodeRegistered(t *testing.T) {
	root := newRoot()
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "code" {
			found = c
		}
	}
	if found == nil {
		t.Fatalf("`villa code` is not registered on the root command")
	}
	if found.Args == nil {
		t.Errorf("`villa code` must use cobra.NoArgs")
	} else if err := found.Args(found, []string{"extra"}); err == nil {
		t.Errorf("`villa code` accepted a positional arg; want NoArgs")
	}
}

func envHas(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
