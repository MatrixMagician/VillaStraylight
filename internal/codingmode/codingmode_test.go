package codingmode

import (
	"context"
	"errors"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// The transaction's ordering, restart set and rollback are asserted once, on the
// frame (internal/stackapply/transact_test.go). These tests cover what coding mode
// decides inside it: the no-op, the coder resolution and pull, the residency, and
// the config fields enter writes and exit clears.

type modeFake struct {
	cfg         config.VillaConfig
	coder       CoderTarget
	resolveOK   bool
	resolveWhy  string
	resolveCall int
	pulled      []string
	pullErr     error
	captured    bool
	saved       []config.VillaConfig
	proveStatus string
}

func swapCoder() CoderTarget {
	return CoderTarget{Model: "coder-30b", Quant: "Q4_K_M", AgentCtx: 65536, Residency: ResidencySwap, Downloaded: true}
}

func newModeFake(coding bool) *modeFake {
	cfg := config.VillaConfig{Model: "chat-model", Ctx: 8192, Backend: "vulkan"}
	if coding {
		cfg.CodingMode, cfg.CoderModel, cfg.CoderQuant, cfg.CoderAgentCtx = true, "coder-30b", "Q4_K_M", 65536
	}
	return &modeFake{cfg: cfg, coder: swapCoder(), resolveOK: true}
}

func (f *modeFake) deps() Deps {
	return Deps{
		Tx: stackapply.TxDeps{
			Lock:       func() (*stacklock.Lock, error) { return nil, nil },
			LoadConfig: func() (config.VillaConfig, error) { return f.cfg, nil },
			SaveConfig: func(c config.VillaConfig) error { f.saved = append(f.saved, c); return nil },
			Capture: func(config.VillaConfig) (map[string]string, error) {
				f.captured = true
				return map[string]string{"villa-llama.container": "PRIOR"}, nil
			},
			Apply: func(config.VillaConfig) ([]orchestrate.Unit, error) {
				return []orchestrate.Unit{{Name: "villa-llama.container"}}, nil
			},
			Restore:      func(map[string]string) error { return nil },
			DaemonReload: func() error { return nil },
			IsActive:     func(string) (string, error) { return "active", nil },
			Restart:      func(string) error { return nil },
			Prove: func(context.Context, string) prove.Verdict {
				if f.proveStatus != "" {
					return prove.Verdict{Status: f.proveStatus}
				}
				return prove.Verdict{Status: prove.StatusPass}
			},
			Service: "villa-llama.service",
		},
		ResolveCoder: func(config.VillaConfig) (CoderTarget, bool, string) {
			f.resolveCall++
			return f.coder, f.resolveOK, f.resolveWhy
		},
		Pull: func(t CoderTarget) error {
			f.pulled = append(f.pulled, t.Model)
			return f.pullErr
		},
	}
}

// TestEnter: a swap-residency enter persists the coder fields AT ENTER and leaves
// cfg.Model as the durable chat model, so exit is a config-derived restore.
func TestEnter(t *testing.T) {
	f := newModeFake(false)
	res := Run(f.deps(), Enter)
	if !res.Switched || res.FromModel != "chat-model" || res.ToModel != "coder-30b" || res.Residency != ResidencySwap {
		t.Fatalf("expected a swap-residency enter chat→coder, got %+v", res)
	}
	s := f.saved[0]
	if !s.CodingMode || s.CoderModel != "coder-30b" || s.CoderQuant != "Q4_K_M" || s.CoderAgentCtx != 65536 || s.Model != "chat-model" {
		t.Errorf("saved %+v, want the coder fields set and the chat model untouched", s)
	}
	if len(f.pulled) != 0 {
		t.Errorf("downloaded coder weights must not be pulled again: %v", f.pulled)
	}
}

// TestEnterAutoPullsAbsentCoder: absent coder weights are pulled before capture;
// a failed pull stops with nothing captured or saved.
func TestEnterAutoPullsAbsentCoder(t *testing.T) {
	f := newModeFake(false)
	f.coder.Downloaded = false
	if res := Run(f.deps(), Enter); !res.Switched || len(f.pulled) != 1 {
		t.Fatalf("expected a pull then a switch, got %+v pulled %v", res, f.pulled)
	}

	f = newModeFake(false)
	f.coder.Downloaded = false
	f.pullErr = errors.New("offline")
	res := Run(f.deps(), Enter)
	if res.FailedStep != "pull" || res.Err == nil || res.Refused || f.captured || len(f.saved) != 0 {
		t.Fatalf("a failed pull must stop before capture as an error, got %+v captured %v", res, f.captured)
	}
}

// TestSharedResidencyRenderDeltaOnly: shared residency records only the agent ctx;
// coder_model stays empty so the chat endpoint serves, and the residency is surfaced.
func TestSharedResidencyRenderDeltaOnly(t *testing.T) {
	f := newModeFake(false)
	f.coder = CoderTarget{AgentCtx: 16384, Residency: ResidencyShared}
	res := Run(f.deps(), Enter)
	if !res.Switched || res.Residency != ResidencyShared || res.ToModel != "chat-model" {
		t.Fatalf("expected a shared-residency enter on the chat model, got %+v", res)
	}
	if s := f.saved[0]; !s.CodingMode || s.CoderModel != "" || s.CoderAgentCtx != 16384 {
		t.Errorf("saved %+v, want only coding mode and the agent ctx", s)
	}
}

// TestExitRestoresChat: exit clears every coder field under the same transaction.
func TestExitRestoresChat(t *testing.T) {
	f := newModeFake(true)
	res := Run(f.deps(), Exit)
	if !res.Switched || res.ToModel != "chat-model" {
		t.Fatalf("expected an exit back to the chat model, got %+v", res)
	}
	if s := f.saved[0]; s.CodingMode || s.CoderModel != "" || s.CoderQuant != "" || s.CoderAgentCtx != 0 || s.Model != "chat-model" {
		t.Errorf("saved %+v, want every coder field cleared", s)
	}
	if f.resolveCall != 0 {
		t.Errorf("exit must not resolve a coder")
	}
}

// TestNoOpSameState: enter while coding and exit while chatting are clean no-ops.
func TestNoOpSameState(t *testing.T) {
	for _, tc := range []struct {
		coding bool
		dir    Direction
	}{{true, Enter}, {false, Exit}} {
		f := newModeFake(tc.coding)
		res := Run(f.deps(), tc.dir)
		if !res.NoOp || f.captured || len(f.saved) != 0 {
			t.Errorf("%s while already there: expected a clean NoOp, got %+v", tc.dir, res)
		}
	}
}

// TestRefuseFitGuard: a coder that does not fit at the agent ctx refuses with the
// remediation before anything is pulled or captured.
func TestRefuseFitGuard(t *testing.T) {
	f := newModeFake(false)
	f.resolveOK, f.resolveWhy = false, "no coder model fits"
	f.coder.Downloaded = false
	res := Run(f.deps(), Enter)
	if !res.Refused || res.Reason != "no coder model fits" || f.captured || len(f.pulled) != 0 {
		t.Fatalf("expected a refusal before pull and capture, got %+v", res)
	}
}

// TestRollbackKeepsTheModels: a failed proof rolls back and the Result still names
// the chat model and the coder it tried.
func TestRollbackKeepsTheModels(t *testing.T) {
	f := newModeFake(false)
	f.proveStatus = prove.StatusFail
	res := Run(f.deps(), Enter)
	if !res.RolledBack || res.FromModel != "chat-model" || res.ToModel != "coder-30b" || res.Direction != Enter {
		t.Fatalf("expected a rollback naming both models, got %+v", res)
	}
}
