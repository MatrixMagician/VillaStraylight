// Package modelswap is the extracted `villa model swap` guarded orchestration core
// (DASH-04): the ordering-is-the-security-contract sequence that resolves a model
// through the catalog, refuses a non-fitting target BEFORE any side effect, auto-pulls
// absent weights, persists config BEFORE unit work, regenerates units, and restarts
// ONLY the inference service.
//
// It was moved VERBATIM out of cmd/villa/model.go runModelSwap (STATE.md [03-05]: the
// swap ordering IS the security contract, D-09) so the dashboard's POST
// /api/models/switch handler can call the SAME path the CLI does, not a fork. Run
// returns a typed Result (not an exit code) so the dashboard handler can branch on it
// (RESEARCH: "Deps + Run() returning a typed result, not an exit code").
//
// All host-touching actions are injected via Deps; modelswap itself stays free of
// catalog-load/download/systemd coupling (the live wiring lives in cmd/villa).
package modelswap

import (
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// Deps is the injectable seam set for the swap core. Every host-touching action is a
// field so modelswap_test.go drives the whole flow (and asserts ordering) without a
// live host. The live wiring (liveSwapDeps) stays in cmd/villa.
type Deps struct {
	LoadConfig     func() (config.VillaConfig, error)
	ResolveCatalog func(name string) (catalog.CatalogModel, bool)
	// Fits reports whether m fits the usable envelope (reuse recommend fit-math)
	// and a human reason when it does not — never a silent OOM at container start.
	Fits func(m catalog.CatalogModel) (bool, string)
	// IsDownloaded reports whether the model's weights are already on disk.
	IsDownloaded func(m catalog.CatalogModel) bool
	// Pull auto-downloads the verified weights (reuse download.PullModel).
	Pull func(m catalog.CatalogModel) error
	// SaveConfig persists the new model to config.toml (the source of truth).
	SaveConfig func(c config.VillaConfig) error
	// ReconcileAndWrite renders units from the persisted config and writes only the
	// changed unit(s). It reports whether anything actually changed so the caller can
	// skip a needless restart on a no-op swap (WR-06).
	ReconcileAndWrite func(c config.VillaConfig) (bool, error)
	// Restart restarts ONLY the named service (the inference unit).
	Restart func(service string) error
	// InstallServiceName is the inference service the swap restarts (and ONLY that
	// service). It is a Deps field so modelswap need not import the cmd-layer
	// install.go constant (no package-main cycle).
	InstallServiceName string
}

// Result is the typed outcome of a swap, replacing the old exit code so the dashboard
// handler can branch on it (and the cobra caller maps it to an exit code + messages).
type Result struct {
	// Refused is true when the swap was rejected with zero side effects (unknown model
	// or a non-fitting target). Reason carries the human explanation.
	Refused bool
	// Reason is the refusal/error explanation (empty on success).
	Reason string
	// Unknown is true when the model id did not resolve through the catalog (a refusal
	// sub-case the caller surfaces with an "unknown model" hint).
	Unknown bool
	// Err is a non-refusal failure (config load, save, pull, reconcile, restart). It is
	// distinct from a Refused (which is a clean policy rejection, not an error).
	Err error
	// FailedStep names the step Err occurred at ("pull"/"load config"/"persist config"/
	// "regenerate units"/"restart") so the caller prints the same message it used to.
	FailedStep string
	// Pulled is true when the target weights were auto-downloaded during the swap.
	Pulled bool
	// Switched is true when the swap persisted config AND restarted the inference unit.
	Switched bool
	// NoOp is true when config was persisted but the units were already up to date, so
	// no restart was needed (WR-06).
	NoOp bool
	// FromModel / ToModel are the previous and new model ids (ToModel set on any
	// resolved target; FromModel from the loaded config).
	FromModel string
	ToModel   string
}

// Run performs the guarded swap and returns a typed Result. Ordering is the security
// contract (D-09), preserved VERBATIM from the old runModelSwap:
// (1) resolve through catalog, (2) fit-guard refuse, (3) auto-pull if absent,
// (4) SaveVilla BEFORE unit work, (5) reconcileAndWrite, (6) restart ONLY the
// inference service, skipping the restart on a no-op (WR-06). A failure at any step
// short-circuits with no further side effects.
func Run(d Deps, name string) Result {
	// (1) Resolve the name THROUGH the catalog — never as a filesystem path
	// (T-03-18 command-injection / path-traversal guard). Unknown → refuse, zero
	// side effects.
	m, ok := d.ResolveCatalog(name)
	if !ok {
		return Result{Refused: true, Unknown: true, Reason: "unknown model", ToModel: name}
	}

	// (2) Fit-guard (D-09.1 / T-03-19): refuse a target that won't fit the envelope
	// BEFORE any pull/persist/restart — never a silent OOM at container start.
	if fits, reason := d.Fits(m); !fits {
		return Result{Refused: true, Reason: reason, ToModel: m.ID}
	}

	// (3) Auto-pull the verified weights if absent (D-09.2). Reuses the same
	// verified/resumable downloader as `model pull`.
	pulled := false
	if !d.IsDownloaded(m) {
		if err := d.Pull(m); err != nil {
			return Result{Err: err, FailedStep: "pull", ToModel: m.ID}
		}
		pulled = true
	}

	// (4) Persist the new model to config.toml BEFORE any unit work (D-09.3 /
	// T-03-21): config is the single source of truth, so it must never lag the
	// running unit.
	cfg, err := d.LoadConfig()
	if err != nil {
		return Result{Err: err, FailedStep: "load config", Pulled: pulled, ToModel: m.ID}
	}
	fromModel := cfg.Model
	cfg.Model = m.ID
	if m.Quant != "" {
		cfg.Quant = m.Quant
	}
	if err := d.SaveConfig(cfg); err != nil {
		return Result{Err: err, FailedStep: "persist config", Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}

	// (5) Regenerate the inference unit args from the persisted config and (6) restart
	// ONLY the inference service — but only when the regenerate actually changed a
	// unit, so a no-op swap does not trigger a multi-second model reload for nothing
	// (WR-06).
	changed, err := d.ReconcileAndWrite(cfg)
	if err != nil {
		return Result{Err: err, FailedStep: "regenerate units", Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}
	if !changed {
		return Result{NoOp: true, Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}
	if err := d.Restart(d.InstallServiceName); err != nil {
		return Result{Err: err, FailedStep: "restart", Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}

	return Result{Switched: true, Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
}
