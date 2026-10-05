// Package modelswap holds the guarded `villa model swap` change the CLI and the
// dashboard's POST /api/models/switch both run, not a fork: resolve the model through
// the catalog, refuse a non-fitting target before any side effect, pull absent
// weights, then write the model, the ctx it is served at and its vision answer to
// config. It is a stackapply.Change run through the
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
	// Fits sizes m the way the render would serve cfg, the config the swap would
	// write (reuse recommend fit-math) — never a silent OOM at container start.
	Fits func(m catalog.Model, cfg config.VillaConfig) Fit
	// IsDownloaded reports whether the model's weights are already on disk.
	IsDownloaded func(m catalog.Model) bool
	// Pull auto-downloads the verified weights (reuse download.PullModel).
	Pull func(m catalog.Model) error
	// OnPull, when set, is told just before a pull starts, so a caller can say a
	// multi-GB download is under way. Optional.
	OnPull func(m catalog.Model)
}

// Fit is the fit guard's answer for a swap target served at one config.
type Fit struct {
	OK bool
	// OverEnvelope is true when the target does not fit the memory envelope: the
	// one refusal a smaller ctx can cure. A speculation refusal is not one.
	OverEnvelope bool
	// Detail is the verdict line: the fit and its headroom, the shortfall, or the
	// speculation refusal.
	Detail string
	// Vision is recommend's vision answer for the target: true only when the entry
	// ships a projector AND it fits beside the model. The swap writes it, so vision
	// follows the served model rather than outliving it (#299, ADR-0023).
	Vision bool
}

// Result is the typed outcome of a swap.
type Result struct {
	stackapply.Outcome
	// Unknown is true when the model id did not resolve through the catalog (a
	// refusal sub-case the caller surfaces with an "unknown model" hint).
	Unknown bool
	// OverEnvelope is true on a refusal that is a memory shortfall, so the caller
	// can say "won't fit" only when that is what happened.
	OverEnvelope bool
	// Pulled is true when the target weights were auto-downloaded during the swap.
	Pulled bool
	// FromModel / ToModel are the previous and new model ids.
	FromModel string
	ToModel   string
	// FromVision / ToVision are the vision decision before and after. They differ
	// only when the swap changed it; a refusal leaves both at the prior value.
	FromVision bool
	ToVision   bool
	// FromCtx / ToCtx are the configured ctx before and after. They differ only
	// when the target did not fit at the configured ctx and the swap fell back to
	// its default_ctx (#301); a refusal leaves both at the prior value.
	FromCtx int
	ToCtx   int
}

// Size applies the swap's ctx rule (#301) to cfg, the config the swap would write:
// the target is sized at the configured ctx (unset means its default_ctx), and when
// it is over the envelope there it is sized again at its default_ctx, which the
// returned config then carries. Only a memory shortfall above the default is
// retried: a smaller configured ctx would only grow, and a speculation refusal is
// not cured by any ctx. A target that fits at neither returns the default's
// refusing Fit. The dashboard's fit column runs the same rule, so it shows what a
// switch would do.
func Size(m catalog.Model, cfg config.VillaConfig, fits func(catalog.Model, config.VillaConfig) Fit) (Fit, config.VillaConfig) {
	fit := fits(m, cfg)
	if fit.OK || !fit.OverEnvelope || cfg.Ctx <= m.DefaultCtx {
		return fit, cfg
	}
	cfg.Ctx = m.DefaultCtx
	return fits(m, cfg), cfg
}

// Run performs the guarded swap. Ordering is the security contract: (1) resolve
// through the catalog, never as a path; (2) fit-guard refuse; (3) auto-pull if
// absent; (4) write the model, its quant, the ctx Size chose and its vision answer
// to config. The frame then persists,
// applies, restarts, proves and, on any failure, rolls back.
func Run(d Deps, name string) Result {
	r := Result{ToModel: name}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.FromModel = cfg.Model
		r.FromVision, r.ToVision = cfg.Vision, cfg.Vision
		r.FromCtx, r.ToCtx = cfg.Ctx, cfg.Ctx
		m, ok := d.ResolveCatalog(name)
		if !ok {
			r.Unknown = true
			return cfg, &stackapply.Outcome{Refused: true, Reason: "unknown model"}
		}
		r.ToModel = m.ID
		target := cfg
		target.Model = m.ID
		if m.Quant != "" {
			target.Quant = m.Quant
		}
		fit, sized := Size(m, target, d.Fits)
		if !fit.OK {
			r.OverEnvelope = fit.OverEnvelope
			return cfg, &stackapply.Outcome{Refused: true, Reason: fit.Detail}
		}
		if !d.IsDownloaded(m) {
			if d.OnPull != nil {
				d.OnPull(m)
			}
			if err := d.Pull(m); err != nil {
				return cfg, &stackapply.Outcome{FailedStep: "pull", Err: err}
			}
			r.Pulled = true
		}
		sized.Vision = fit.Vision
		r.ToVision, r.ToCtx = sized.Vision, sized.Ctx
		return sized, nil
	})
	return r
}
