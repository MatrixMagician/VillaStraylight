// Package modelswap holds the guarded `villa model swap` change the CLI and the
// dashboard's POST /api/models/switch both run, not a fork: resolve the model through
// the catalog, refuse a non-fitting target before any side effect, pull absent
// weights, then write the model to config. It is a stackapply.Change run through the
// one swap transaction frame (ADR-0015), which owns the stack lock (blocking for the
// CLI, non-blocking for the dashboard), the capture, the apply, the restart of every
// changed running unit, the proof and the rollback (#237).
//
// Run returns a typed Result, not an exit code, so the dashboard handler can branch
// on it. All host-touching actions are injected via Deps; the live wiring lives in
// cmd/villa.
package modelswap

import (
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
)

// Deps are the swap's guards, plus the frame.
type Deps struct {
	// Tx is the swap transaction frame's host.
	Tx             stackapply.TxDeps
	ResolveCatalog func(name string) (catalog.Model, bool)
	// Fits reports whether m fits the usable envelope (reuse recommend fit-math)
	// and a human reason when it does not — never a silent OOM at container start.
	Fits func(m catalog.Model) (bool, string)
	// IsDownloaded reports whether the model's weights are already on disk.
	IsDownloaded func(m catalog.Model) bool
	// Pull auto-downloads the verified weights (reuse download.PullModel).
	Pull func(m catalog.Model) error
}

// Result is the typed outcome of a swap.
type Result struct {
	stackapply.Outcome
	// Unknown is true when the model id did not resolve through the catalog (a
	// refusal sub-case the caller surfaces with an "unknown model" hint).
	Unknown bool
	// Pulled is true when the target weights were auto-downloaded during the swap.
	Pulled bool
	// FromModel / ToModel are the previous and new model ids.
	FromModel string
	ToModel   string
}

// Run performs the guarded swap. Ordering is the security contract: (1) resolve
// through the catalog, never as a path; (2) fit-guard refuse; (3) auto-pull if
// absent; (4) write the model and its quant to config. The frame then persists,
// applies, restarts, proves and, on any failure, rolls back.
func Run(d Deps, name string) Result {
	r := Result{ToModel: name}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.FromModel = cfg.Model
		m, ok := d.ResolveCatalog(name)
		if !ok {
			r.Unknown = true
			return cfg, &stackapply.Outcome{Refused: true, Reason: "unknown model"}
		}
		r.ToModel = m.ID
		if fits, reason := d.Fits(m); !fits {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		if !d.IsDownloaded(m) {
			if err := d.Pull(m); err != nil {
				return cfg, &stackapply.Outcome{FailedStep: "pull", Err: err}
			}
			r.Pulled = true
		}
		cfg.Model = m.ID
		if m.Quant != "" {
			cfg.Quant = m.Quant
		}
		return cfg, nil
	})
	return r
}
