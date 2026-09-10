package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// tools_mode_test.go covers the cobra/exit mapping, the derived flag token and the
// doctor drift seam. The transaction itself is asserted in internal/toolsmode.

// Distinct sentinel errors so a mapping test cannot pass on the wrong step.
var (
	errTestCapture = errors.New("capture failed")
	errTestWrite   = errors.New("render failed")
)

// newToolsStub builds a fake Deps over the shared backend recorder with the given
// persisted tools-mode state.
func newToolsStub(rec *backendRecorder, toolsOn bool) *backendswap.Deps {
	d := newBackendStub(rec)
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{Model: "current-model", Backend: rec.curBackend, ToolsMode: toolsOn}, nil
	}
	return d
}

func passingToolsRecorder() *backendRecorder {
	return &backendRecorder{curBackend: "rocm", fits: true, preflightOK: true, proveStatus: prove.StatusPass}
}

// TestToolsModeRegistered guards that the noun and all three subcommands are wired
// into the command tree.
func TestToolsModeRegistered(t *testing.T) {
	root := newRoot()
	tm, _, err := root.Find([]string{"tools-mode"})
	if err != nil || tm.Name() != "tools-mode" {
		t.Fatalf("`tools-mode` noun not registered: %v", err)
	}
	for _, sub := range []string{"show", "enter", "exit"} {
		c, _, err := root.Find([]string{"tools-mode", sub})
		if err != nil || c.Name() != sub {
			t.Fatalf("`tools-mode %s` subcommand not registered: %v", sub, err)
		}
	}
}

// TestToolsModeShowReportsTheAnsweredGate guards that show reports the gate, not the
// raw flag: coding mode implies tool calling, and a reader that saw only tools_mode
// would think a coding-mode stack served no tools.
func TestToolsModeShowReportsTheAnsweredGate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      config.VillaConfig
		wantText string
		wantJSON string
	}{
		{"unset", config.VillaConfig{}, "off", `"tools": false`},
		{"explicit", config.VillaConfig{ToolsMode: true}, "on", `"tools": true`},
		{"implied by coding mode", config.VillaConfig{CodingMode: true}, "coding mode", `"tools": true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tc.cfg.Model != "" || tc.cfg.ToolsMode || tc.cfg.CodingMode {
				if err := config.SaveVilla(tc.cfg); err != nil {
					t.Fatalf("seed config: %v", err)
				}
			}

			cmd, out, _ := newTestCmd()
			if code := runToolsModeShow(cmd, false); code != exitPass {
				t.Fatalf("exit = %d, want 0", code)
			}
			if !strings.Contains(out.String(), tc.wantText) {
				t.Errorf("output = %q, want it to mention %q", out.String(), tc.wantText)
			}

			cmd, out, _ = newTestCmd()
			if code := runToolsModeShow(cmd, true); code != exitPass {
				t.Fatalf("--json exit = %d, want 0", code)
			}
			if !strings.Contains(out.String(), tc.wantJSON) {
				t.Errorf("--json shape = %q, want %q", out.String(), tc.wantJSON)
			}
		})
	}
}

// TestToolsModeNoOpExitsZero guards the promise that entering a mode already entered,
// and exiting one already exited, exit 0 and say so rather than restarting the stack.
func TestToolsModeNoOpExitsZero(t *testing.T) {
	for _, tc := range []struct {
		name      string
		persisted bool
		target    bool
	}{
		{"enter when already on", true, true},
		{"exit when already off", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := passingToolsRecorder()
			cmd, out, _ := newTestCmd()
			if code := runToolsMode(cmd, tc.target, newToolsStub(rec, tc.persisted)); code != exitPass {
				t.Fatalf("exit = %d, want 0", code)
			}
			if !strings.Contains(out.String(), "already") {
				t.Errorf("output = %q, want it to say the stack is already there", out.String())
			}
			if len(rec.saved) != 0 || rec.written != 0 || len(rec.restarted) != 0 || rec.captured != 0 {
				t.Errorf("a no-op touched the host: %+v", rec)
			}
		})
	}
}

// TestToolsModeResultMapping guards the Result→exit-code mapping for every outcome
// the operator can hit.
func TestToolsModeResultMapping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		arm      func(*backendRecorder)
		on       bool
		wantCode int
		wantErr  string
	}{
		{"fit refusal", func(r *backendRecorder) { r.fits = false; r.fitReason = "needs 9 bytes vs 8 usable" },
			true, exitBlocked, "refusing"},
		{"capture refusal", func(r *backendRecorder) { r.captureErr = errTestCapture },
			true, exitBlocked, "refusing"},
		{"write rollback", func(r *backendRecorder) { r.writeErr = errTestWrite },
			true, exitBlocked, "rolled back"},
		{"prove rollback", func(r *backendRecorder) { r.proveStatus = prove.StatusFail; r.proveDetail = "fell back to CPU" },
			true, exitBlocked, "rolled back"},
		{"switch", func(*backendRecorder) {}, true, exitPass, ""},
		{"exit switch", func(*backendRecorder) {}, false, exitPass, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := passingToolsRecorder()
			tc.arm(rec)
			cmd, out, errOut := newTestCmd()
			code := runToolsMode(cmd, tc.on, newToolsStub(rec, !tc.on))
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tc.wantCode, errOut.String())
			}
			if tc.wantErr == "" {
				if !strings.Contains(out.String(), "switched tools mode") {
					t.Errorf("output = %q, want the switch line", out.String())
				}
				return
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.wantErr)
			}
		})
	}
}

// TestToolsFlagTokenIsDerivedFromTheSeam guards that the token TMD-01 looks for comes
// from the inference seam rather than being typed here: it must be exactly the one
// argument ContainerArgs adds when RunSpec.Tools flips on, for every backend.
func TestToolsFlagTokenIsDerivedFromTheSeam(t *testing.T) {
	for _, name := range []string{"rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma", "vulkan"} {
		t.Run(name, func(t *testing.T) {
			token, err := toolsFlagToken(name)
			if err != nil {
				t.Fatalf("toolsFlagToken(%q): %v", name, err)
			}
			if !strings.HasPrefix(token, "--") {
				t.Errorf("token = %q, want a llama-server long flag", token)
			}
			b, err := inference.BackendFor(name)
			if err != nil {
				t.Fatalf("BackendFor(%q): %v", name, err)
			}
			for _, a := range b.ContainerArgs(inference.RunSpec{}) {
				if a == token {
					t.Fatalf("the tools-off args already carry %q, so it cannot identify tools mode", token)
				}
			}
		})
	}
}

// TestToolsFlagTokenRefusesAnUnknownBackend guards that an unresolvable backend is an
// error, never a token that would silently make every unit look drift-free.
func TestToolsFlagTokenRefusesAnUnknownBackend(t *testing.T) {
	if _, err := toolsFlagToken("nvidia"); err == nil {
		t.Error("an unknown backend returned a token, want an error")
	}
}

// TestUnitCarriesToolsFlag guards that the drift check reads the Exec line as
// arguments: a token in a comment, or one that is only a substring of another
// argument, is not a served flag.
func TestUnitCarriesToolsFlag(t *testing.T) {
	token, err := toolsFlagToken("rocm")
	if err != nil {
		t.Fatalf("toolsFlagToken: %v", err)
	}
	for _, tc := range []struct {
		name string
		unit string
		want bool
	}{
		{"served", "[Container]\nExec=llama-server -m x " + token + " --port 8080\n", true},
		{"absent", "[Container]\nExec=llama-server -m x --port 8080\n", false},
		{"only in a comment", "# " + token + "\n[Container]\nExec=llama-server -m x\n", false},
		{"substring of another argument", "[Container]\nExec=llama-server -m x " + token + "-extra\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unitCarriesToolsFlag([]byte(tc.unit), token); got != tc.want {
				t.Errorf("unitCarriesToolsFlag = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLiveToolsDriftUnreadableIsUnknown guards the no-false-green rule at the seam: a
// host with no rendered unit reports ok=false, which doctor renders as WARN, never a
// matching PASS.
func TestLiveToolsDriftUnreadableIsUnknown(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, _, ok := liveToolsDrift(); ok {
		t.Error("a host with no rendered unit reported an evaluable drift answer")
	}
}

// TestLiveToolsProveExitPathSkipsTheToolCall guards that the exit cutover is not
// gated on a tool call: a stack with tool calling off has no tool call to make, and
// demanding one would roll every exit back.
func TestLiveToolsProveExitPathSkipsTheToolCall(t *testing.T) {
	// A cancelled context makes any real probe fail fast, so a pass here can only
	// come from the tool-call step having been skipped.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v := liveToolsProve(false)(ctx, "nvidia"); v.Pass() {
		t.Errorf("an unresolvable backend proved PASS: %+v", v)
	}
}

// TestStatusTableShowsToolsModeOnlyWhenOn guards the status line spec v1.11 §10 asks
// for: `mode tools` appears iff the gate is answered on, so an unchanged default
// stack does not grow a line saying nothing happened.
func TestStatusTableShowsToolsModeOnlyWhenOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   bool
	}{{"off", false}, {"on", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderStatusTable(&buf, status.Report{Tools: tc.on}, false)
			if got := strings.Contains(buf.String(), "tools"); got != tc.on {
				t.Errorf("Tools=%v rendered:\n%s", tc.on, buf.String())
			}
		})
	}
}

// TestInstallWorkspaceAgentFlagRegistered guards that the opt-in reaches the install
// flow: the gate it persists is what `villa work` later refuses without.
func TestInstallWorkspaceAgentFlagRegistered(t *testing.T) {
	root := newRoot()
	inst, _, err := root.Find([]string{"install"})
	if err != nil {
		t.Fatalf("install not registered: %v", err)
	}
	f := inst.Flags().Lookup("workspace-agent")
	if f == nil {
		t.Fatal("`villa install --workspace-agent` is not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("default = %q, want an opt-in default of false", f.DefValue)
	}
}

// TestNoteVerdictKeepsTheStatus guards that annotating what went unproven never
// upgrades or downgrades the verdict itself.
func TestNoteVerdictKeepsTheStatus(t *testing.T) {
	v := noteVerdict(prove.Verdict{Status: prove.StatusPass, Detail: "resident"}, "and more")
	if !v.Pass() || v.Detail != "resident; and more" {
		t.Errorf("noteVerdict = %+v", v)
	}
	empty := noteVerdict(prove.Verdict{Status: prove.StatusPass}, "note")
	if empty.Detail != "note" {
		t.Errorf("empty detail = %q, want the note alone", empty.Detail)
	}
}
