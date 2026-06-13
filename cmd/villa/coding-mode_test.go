package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/codingmode"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// coding-mode_test.go exercises the cobra caller's mapping of codingmode.Result to exit
// codes + messages (the transactional ORDERING + rollback are asserted in
// internal/codingmode): refused→1, switched→0, rolled-back→1, no-op→0, plus the
// swap-vs-shared success surfacing (D-10). It also holds the structural no-auto-flip guard
// (D-06): nothing mutates coding_mode outside internal/codingmode.Run.

// codingRecorder records the side-effecting seam calls so the exit-mapping tests can
// drive runCodingMode through a fake *codingmode.Deps without a live host.
type codingRecorder struct {
	curCoding   bool
	chatModel   string
	coder       codingmode.CoderTarget
	resolveOK   bool
	resolveWhy  string
	downloaded  bool
	saved       []config.VillaConfig
	restarted   []string
	proved      int
	proveStatus string

	proveDetai string
}

func newCodingStub(rec *codingRecorder) *codingmode.Deps {
	if rec.chatModel == "" {
		rec.chatModel = "chat-model"
	}
	return &codingmode.Deps{
		InstallServiceName: installServiceName,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: rec.chatModel, CodingMode: rec.curCoding}, nil
		},
		ResolveCoder: func(_ config.VillaConfig) (codingmode.CoderTarget, bool, string) {
			t := rec.coder
			t.Downloaded = rec.downloaded
			return t, rec.resolveOK, rec.resolveWhy
		},
		Pull:        func(_ codingmode.CoderTarget) error { return nil },
		CaptureUnit: func() ([]byte, error) { return []byte("[Container]\nImage=prior\n"), nil },
		SaveConfig: func(c config.VillaConfig) error {
			rec.saved = append(rec.saved, c)
			return nil
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) { return true, nil },
		RestoreUnit:       func(_ []byte) error { return nil },
		DaemonReload:      func() error { return nil },
		Restart: func(service string) error {
			rec.restarted = append(rec.restarted, service)
			return nil
		},
		Prove: func(_ context.Context, _ codingmode.Direction) codingmode.ProveVerdict {
			rec.proved++
			status := rec.proveStatus
			if status == "" {
				status = codingmode.ProveStatusPass
			}
			return codingmode.ProveVerdict{Status: status, Detail: rec.proveDetai}
		},
	}
}

func swapCoder() codingmode.CoderTarget {
	return codingmode.CoderTarget{Model: "coder-model", Quant: "Q4", AgentCtx: 65536, Residency: codingmode.ResidencySwap}
}

// TestCodingModeEnterSwapExit0 — a proven swap enter exits 0 and surfaces the swap
// residency (chat → coder) in the success line (D-10).
func TestCodingModeEnterSwapExit0(t *testing.T) {
	rec := &codingRecorder{resolveOK: true, downloaded: true, coder: swapCoder()}
	cmd, out, _ := newTestCmd()
	code := runCodingMode(cmd, codingmode.Enter, newCodingStub(rec))
	if code != exitPass {
		t.Fatalf("a proven enter must exit 0, got %d", code)
	}
	if len(rec.saved) == 0 || len(rec.restarted) == 0 || rec.proved == 0 {
		t.Errorf("an enter must save+restart+prove, got saved=%v restarted=%v proved=%d", rec.saved, rec.restarted, rec.proved)
	}
	s := out.String()
	if !strings.Contains(s, "entered coding mode") || !strings.Contains(s, "swap") {
		t.Errorf("enter success must surface the swap residency, got %q", s)
	}
	if !strings.Contains(s, "coder-model") {
		t.Errorf("enter success must name the coder model, got %q", s)
	}
}

// TestCodingModeEnterSharedSurfaced — a shared-residency enter exits 0 and EXPLICITLY
// surfaces that no model swap happened (never silently degrade swap→shared, D-10).
func TestCodingModeEnterSharedSurfaced(t *testing.T) {
	rec := &codingRecorder{resolveOK: true, coder: codingmode.CoderTarget{AgentCtx: 32768, Residency: codingmode.ResidencyShared}}
	cmd, out, _ := newTestCmd()
	code := runCodingMode(cmd, codingmode.Enter, newCodingStub(rec))
	if code != exitPass {
		t.Fatalf("a shared enter must exit 0, got %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "shared") || !strings.Contains(s, "no model swap") {
		t.Errorf("shared enter must explicitly surface render-delta-only (no swap), got %q", s)
	}
}

// TestCodingModeRefusedExit1 — a fit-guard refusal exits 1 with zero mutate seams and a
// refusal message.
func TestCodingModeRefusedExit1(t *testing.T) {
	rec := &codingRecorder{resolveOK: false, resolveWhy: "coder needs 70 GiB vs 64 GiB usable at agent_ctx"}
	cmd, _, errOut := newTestCmd()
	code := runCodingMode(cmd, codingmode.Enter, newCodingStub(rec))
	if code != exitBlocked {
		t.Fatalf("a refused enter must exit 1, got %d", code)
	}
	if len(rec.saved) != 0 || len(rec.restarted) != 0 {
		t.Errorf("a refusal must fire zero mutate seams, got saved=%v restarted=%v", rec.saved, rec.restarted)
	}
	if !strings.Contains(errOut.String(), "refusing") {
		t.Errorf("expected a refusal message, got %q", errOut.String())
	}
}

// TestCodingModeRollbackExit1 — a non-pass prove rolls back and exits 1 with a rollback
// message.
func TestCodingModeRollbackExit1(t *testing.T) {
	rec := &codingRecorder{resolveOK: true, downloaded: true, coder: swapCoder(), proveStatus: "fail", proveDetai: "residency FAIL"}
	cmd, _, errOut := newTestCmd()
	code := runCodingMode(cmd, codingmode.Enter, newCodingStub(rec))
	if code != exitBlocked {
		t.Fatalf("a rolled-back enter must exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "rolled back") {
		t.Errorf("expected a rollback message, got %q", errOut.String())
	}
}

// TestCodingModeNoOpEnterExit0 — enter while already coding is a clean no-op, exit 0.
func TestCodingModeNoOpEnterExit0(t *testing.T) {
	rec := &codingRecorder{curCoding: true, resolveOK: true, coder: swapCoder()}
	cmd, out, _ := newTestCmd()
	code := runCodingMode(cmd, codingmode.Enter, newCodingStub(rec))
	if code != exitPass {
		t.Fatalf("a no-op enter must exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "already in coding mode") {
		t.Errorf("expected an already-coding no-op message, got %q", out.String())
	}
}

// TestCodingModeExitExit0 — a proven exit restores the chat model and exits 0.
func TestCodingModeExitExit0(t *testing.T) {
	rec := &codingRecorder{curCoding: true}
	cmd, out, _ := newTestCmd()
	code := runCodingMode(cmd, codingmode.Exit, newCodingStub(rec))
	if code != exitPass {
		t.Fatalf("a proven exit must exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "exited coding mode") {
		t.Errorf("expected an exit success message, got %q", out.String())
	}
}

// TestCodingModeNounRegistered — the `coding-mode` noun with enter/exit is wired into the
// root command tree (D-06).
func TestCodingModeNounRegistered(t *testing.T) {
	root := newRoot()
	var cm *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "coding-mode" {
			cm = c
			break
		}
	}
	if cm == nil {
		t.Fatal("coding-mode noun must be registered on root")
	}
	have := map[string]bool{}
	for _, sub := range cm.Commands() {
		have[sub.Name()] = true
	}
	if !have["enter"] || !have["exit"] {
		t.Errorf("coding-mode must have enter AND exit subcommands, got %v", have)
	}
	// D-06: `villa code` (the Phase-26 agent launcher) is a SIBLING of coding-mode on
	// the root tree, NOT a coding-mode subcommand — the launcher and the mode-flip verb
	// are distinct surfaces. (Phase 26 ships `villa code`; before Phase 26 it did not
	// exist. The invariant that survives is the separation, asserted here.)
	if have["code"] {
		t.Errorf("`villa code` must NOT be a coding-mode subcommand — it is a root-level sibling (D-06)")
	}
}

// codingModeMutationPattern matches a write to the VillaConfig coding_mode TOGGLE: a
// boolean assignment `(.)CodingMode = true|false` or a bool struct-literal field
// `CodingMode: true|false`. It is deliberately anchored to the boolean literals so it
// targets the config toggle (the auto-flip surface) and does NOT false-match the unrelated
// render-descriptor pointer field of the same name (orchestrate.RenderInput.CodingMode /
// inference.RunSpec.CodingMode, which are assigned a *CodingModeSpec, never a bool). The
// structural guard asserts the toggle is mutated NOWHERE outside codingmode.Run — proving
// the mode never auto-flips (D-06 / T-25-09).
var codingModeMutationPattern = regexp.MustCompile(`CodingMode\s*=\s*(true|false)\b|CodingMode:\s*(true|false)\b`)

// TestNoAutoFlipStructuralGuard walks cmd/villa + internal/ and asserts that
// coding_mode/CodingMode is mutated NOWHERE outside internal/codingmode/codingmode.Run.
// This is the explicit-verb-only invariant (D-06 / T-25-09): the mode must change ONLY via
// the verb, never auto-flipped by a caller. Allowed sites: codingmode.go (the core that
// owns the mutation), the config field declaration/marshal (villaconfig.go), and test
// files (which legitimately CONSTRUCT configs with CodingMode set).
func TestNoAutoFlipStructuralGuard(t *testing.T) {
	// Roots relative to cmd/villa (this test's package dir).
	roots := []string{".", "../../internal"}
	// Allow-list: files that legitimately reference/mutate the field.
	allowed := func(rel string) bool {
		base := filepath.Base(rel)
		switch {
		case strings.HasSuffix(rel, "internal/codingmode/codingmode.go"):
			return true // the core that OWNS the mutation (codingmode.Run)
		case strings.HasSuffix(rel, "internal/config/villaconfig.go"):
			return true // the field declaration + marshal omit-when-off path
		case strings.HasSuffix(base, "_test.go"):
			return true // tests construct configs with CodingMode set
		}
		return false
	}

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if allowed(path) {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if codingModeMutationPattern.Match(b) {
				t.Errorf("coding_mode auto-flip guard (D-06): %s mutates CodingMode outside codingmode.Run — the mode must change ONLY via the explicit verb", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}
