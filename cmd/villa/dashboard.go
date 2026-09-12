package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/dashboard"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/taskrun"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// dashboard.go is the thin cobra caller for `villa dashboard`: it
// loads the loopback dashboard/chat ports from config, composes the SHARED
// internal/status read-model seam (the same status.Deps `villa status` uses — never a
// fork), and serves the loopback-only HTTP dashboard. The server (stdlib mux, /api,
// embedded UI, same-origin guard) lives in internal/dashboard; this file keeps only
// the cobra wiring + the live host composition. dashboard_test.go drives runDashboard
// through a stubbed serve dep.

// dashboardDeps are the injectable seams runDashboard drives, so the test can stub the
// config load and the serve call without binding a real socket.
type dashboardDeps struct {
	// LoadConfig loads the villa config (DashboardAddr/DashboardPort/ChatPort).
	LoadConfig func() (config.VillaConfig, error)
	// StatusDeps is the composed SHARED status read-model seam the dashboard folds
	// (the same wiring villa status uses). It is a value so the server holds a copy.
	StatusDeps status.Deps
	// Serve runs the constructed server until it errors or the context is cancelled.
	// Stubbed in tests so no real listener is bound; the live wiring calls
	// (*dashboard.Server).Serve, which shuts down gracefully on cancellation.
	Serve func(context.Context, *dashboard.Server) error

	// Performance + GPU collector seams the dashboard folds into
	// /api/metrics + /api/gpu. Live wiring scrapes the inference endpoint and reads
	// amdgpu sysfs; nil seams default (in dashboard.NewServer) to honest "unavailable".
	Metrics     func() (metrics.PerfSnapshot, bool)
	Slots       func() ([]metrics.Slot, bool)
	MemUsed     func() detect.Bytes
	MemEnvelope func() detect.Bytes
	GPUBusy     func() detect.Int

	// Models lists the catalog marked loaded/on-disk/catalog-only with a per-row fit
	// flag. The live wiring reuses the SAME catalog+config+recommend.Pick
	// fit-math the CLI does; the bool is the availability flag (false on a catalog-load
	// failure → "No models in catalog").
	Models func() ([]dashboard.ModelView, bool)

	// SwapDeps is the SHARED guarded swap core the POST /api/models/switch handler folds
	// The live wiring is liveSwapDeps — the IDENTICAL deps `villa model swap`
	// uses, so the dashboard switch routes through the same security contract.
	SwapDeps modelswap.Deps

	// Cumulative-usage writer seams. The dashboard /api/metrics scrape
	// is the SOLE writer of usage.json: ReadUsage loads the fold's prior, WriteUsage
	// atomically persists the folded store, ModelID supplies the per-model key (cfg.Model),
	// and CounterSample scrapes the two monotonic _total counters from the SAME endpoint
	// already scraped for live tok/s (no new outbound). Nil seams default (in
	// dashboard.NewServer) to honest no-ops that never write.
	ReadUsage     func() usage.Totals
	WriteUsage    func(usage.Totals) error
	ModelID       func() string
	CounterSample func() (metrics.CounterSample, bool)

	// Tasks is the workspace agent's runner (spec v1.11 §5), hosted in this
	// service because it is already the long-lived villa process. nil when
	// workspace_agent is off, in which case the task routes answer 503.
	Tasks *taskrun.Runner

	// Pins folds pinresolve.Resolver.All() over the compiled-in pins.Table() and
	// this host's pinstate.State into the Update·Pins read-model.
	Pins func() dashboard.PinsView
	// Journal tails the rendered stack's rootless user journal into the Journal
	// panel read-model.
	Journal func() dashboard.JournalView
}

// newDashboard builds `villa dashboard`: serve the loopback-only control dashboard
// (read-only health + the chat link) on 127.0.0.1:<dashboard_port>. The exit-code
// mapping lives in runDashboard (return-not-Exit body; cobra RunE calls os.Exit),
// mirroring newStatus.
func newDashboard() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Serve the loopback-only control dashboard (read-only health + chat link)",
		Long: "Serve the VillaStraylight control dashboard on 127.0.0.1:<dashboard_port> (loopback only, " +
			"never all interfaces). The dashboard folds the SAME internal/status read-model " +
			"`villa status` uses (not a fork) and links to Open WebUI on the configured chat port. " +
			"Strictly local, zero telemetry.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveDashboardDeps(cmdContext(cmd))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "dashboard: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runDashboard(cmd, args, deps))
			return nil
		},
	}
}

// runDashboard loads config, constructs the dashboard.Server composing the shared
// status seam + chat port + loopback bind addr, prints the live loopback URL, and
// serves. It RETURNS the exit code (no os.Exit in the body) so dashboard_test.go drives
// it deterministically with a stubbed Serve.
func runDashboard(cmd *cobra.Command, _ []string, d *dashboardDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.LoadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "dashboard: %v\n", err)
		return exitBlocked
	}

	srv, err := dashboard.NewServer(dashboard.Config{
		StatusDeps:    d.StatusDeps,
		ChatPort:      cfg.ChatPort,
		DashboardAddr: config.DashboardAddr,
		DashboardPort: cfg.DashboardPort,
		Metrics:       d.Metrics,
		Slots:         d.Slots,
		MemUsed:       d.MemUsed,
		MemEnvelope:   d.MemEnvelope,
		GPUBusy:       d.GPUBusy,
		Models:        d.Models,
		SwapDeps:      d.SwapDeps,
		ReadUsage:     d.ReadUsage,
		WriteUsage:    d.WriteUsage,
		ModelID:       d.ModelID,
		CounterSample: d.CounterSample,
		Tasks:         d.Tasks,
		Pins:          d.Pins,
		Journal:       d.Journal,
	})
	if err != nil {
		fmt.Fprintf(errOut, "dashboard: %v\n", err)
		return exitBlocked
	}

	fmt.Fprintf(out, "villa dashboard listening on http://%s\n", srv.Addr())

	if err := d.Serve(cmdContext(cmd), srv); err != nil {
		fmt.Fprintf(errOut, "dashboard: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveDashboardDeps wires dashboardDeps to the real host: config.LoadVilla, the live
// status read-model seam (reusing liveStatusDeps so the dashboard and the CLI fold the
// IDENTICAL core), and a Serve that binds the loopback socket.
//
// ctx is the process-lifetime context: it stops Serve on SIGTERM, and it also
// bounds the multi-GB pull a POST /api/models/switch can start, so a stop signal
// does not leave that transfer running past the graceful-shutdown window. Aborting
// a pull is safe and does not corrupt state — the partial ".part" file is kept and
// resumed via HTTP Range, and the config save happens only AFTER the pull succeeds.
func liveDashboardDeps(ctx context.Context) (*dashboardDeps, error) {
	// The inference endpoint is the SAME loopback URL the status seam probes (derived
	// from the config-resolved backend's container runner, never hard-coded), so
	// /api/metrics scrapes the exact server villa status reports on. liveStatusDeps is
	// the SINGLE backend-resolution point (fail-closed) — reuse its Endpoint
	// rather than resolve the backend a second time.
	statusDeps, err := liveStatusDeps()
	if err != nil {
		return nil, err
	}
	endpoint := statusDeps.Endpoint()

	// The runner exists only when the workspace agent is on (fail-soft config
	// load, like the sandbox gate). Recover runs HERE, before Serve binds, so a
	// record left running by the previous service instance is interrupted before
	// any client can read it. A recovery error is reported and does not stop the
	// dashboard: one corrupt task record must not take the whole service down.
	var tasks *taskrun.Runner
	if cfg, err := config.LoadVilla(); err == nil && subsystem.SandboxOn(cfg) {
		tasks = taskrun.New(liveTaskRunDeps(ctx, endpoint))
		if err := tasks.Recover(); err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: task recovery: %v\n", err)
		}
	}

	return &dashboardDeps{
		Tasks:      tasks,
		LoadConfig: config.LoadVilla,
		StatusDeps: *statusDeps,
		Serve:      func(ctx context.Context, s *dashboard.Server) error { return s.Serve(ctx) },

		// Performance: bounded /metrics + /slots scrapes of the inference endpoint.
		Metrics: func() (metrics.PerfSnapshot, bool) { return metrics.ScrapeMetrics(endpoint) },
		Slots:   func() ([]metrics.Slot, bool) { return metrics.ScrapeSlots(endpoint) },

		// GPU & Memory (memory-first): the GTT-used headline + the usable unified-memory
		// envelope (from the authoritative HostProfile envelope, never MemTotal) + the
		// best-effort iGPU busy% (typed-Unknown → "unavailable" when absent).
		MemUsed:     detect.GTTUsedBytes,
		MemEnvelope: func() detect.Bytes { return detect.Probe().UsableEnvelopeBytes },
		GPUBusy:     detect.GPUBusyPercent,

		// Models: the catalog-vs-config list + the SHARED fit-math.
		Models: liveModelsView,

		// Swap: the IDENTICAL guarded swap deps `villa model swap` uses, so the
		// dashboard POST routes through the same resolve→fit→pull→save→regenerate→restart
		// security contract — never a fork.
		SwapDeps: *liveSwapDeps(ctx),

		// Cumulative usage: the dashboard /api/metrics scrape is the SOLE
		// writer of usage.json. ReadUsage loads the fold's prior via usage.Load over
		// usage.Path(); WriteUsage persists the folded store via the atomic temp+rename
		// usage.WriteFileAtomic over the SAME path. ModelID re-reads cfg.Model from config at
		// scrape time (config is the single source of truth; the dashboard server reads it
		// inside the usageMu section so the per-model key cannot drift — Pitfall 2). The
		// counter scrape reuses the SAME loopback `endpoint` already scraped for live tok/s
		// no new outbound.
		ReadUsage:     liveReadUsageTotals,
		WriteUsage:    liveWriteUsage,
		ModelID:       liveModelID,
		CounterSample: func() (metrics.CounterSample, bool) { return metrics.ScrapeCounters(endpoint) },

		// Pins: liveResolver (cmd/villa/pins.go) already joins the compiled-in
		// table to this host's pinstate.State the SAME way every render does — no
		// second resolver.
		Pins: livePinsView,
		// Journal: the rendered stack's own service set (the SAME renderStack +
		// serviceUnits `villa logs` uses), tailed and reduced by the shared
		// JournalTail + ParseJournalJSON pair — no hard-coded unit list.
		Journal: liveJournalView,
	}, nil
}

// journalTailLines is the Journal panel's line cap (contract: "at most 10
// lines"). It lives here, not in the pure parser, because the cap is a decision
// about how much of the wire the panel wants — the parser just takes a max.
const journalTailLines = 10

// livePinsView folds pinresolve.Resolver.All() (liveResolver, the SAME resolver
// every render uses) into the dashboard's PinsView. There is nothing to degrade
// here: an unreadable pinstate store already resolves to vetted-pins-with-
// FromStore-false inside liveResolver, which is the honest answer, not a failure
// this seam needs to catch.
func livePinsView() dashboard.PinsView {
	resolved := liveResolver().All()
	rows := make([]dashboard.PinRow, 0, len(resolved))
	for _, r := range resolved {
		rows = append(rows, dashboard.PinRow{
			Component: string(r.Component),
			Subsystem: r.Subsystem.String(),
			Vetted:    r.Vetted.Ref,
			Effective: r.Current.Ref,
			FromStore: r.FromStore,
			Diverged:  r.Diverged(),
		})
	}
	return dashboard.PinsView{Serial: pins.Serial(), Components: rows}
}

// villaUnitGlob scopes the Journal panel to villa's own user units. systemd matches
// a wildcard `-u` pattern against the units it knows, so one pattern covers the
// whole stack including the units a resident model or an optional subsystem adds.
//
// It is deliberately NOT the rendered unit set. Deriving the set from renderStack
// would make the panel depend on the config loading and the model file resolving,
// so a host whose weights had gone missing would lose the logs that say so — and
// logs are exactly what an operator wants when the stack is broken.
const villaUnitGlob = "villa-*"

// liveJournalView tails villa's user journal and reduces it via the pure
// ParseJournalJSON. An unavailable or empty journalctl read degrades to the zero
// (unavailable) JournalView rather than an error the dashboard has nowhere to put.
func liveJournalView() dashboard.JournalView {
	text, ok := orchestrate.NewSystemd().JournalTail([]string{villaUnitGlob}, journalTailLines)
	if !ok {
		return dashboard.JournalView{}
	}
	return dashboard.ParseJournalJSON(text, journalTailLines)
}

// liveUsageDeps builds the usage byte-I/O seam over the live store path: ReadAll reads
// usage.Path() ((nil,nil) when absent so Load fails closed to empty), and WriteAll
// is the atomic temp+rename usage.WriteFileAtomic. The dashboard is the SOLE writer
// so this WriteAll seam is wired ONLY here (never in the status read path).
func liveUsageDeps() usage.Deps {
	path := usage.Path()
	return usage.Deps{
		ReadAll: func() ([]byte, error) {
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return nil, nil // absent store ⇒ Load fails closed to empty (typed-Unknown)
			}
			return data, err
		},
		WriteAll: func(data []byte) error { return usage.WriteFileAtomic(path, data) },
	}
}

// liveReadUsageTotals loads the persisted store as the fold's prior. usage.Load fails
// closed to an empty usage.Totals on an absent/corrupt/schema-skew store (Plan 01), so a
// read failure degrades the fold to a fresh accumulation rather than panicking.
func liveReadUsageTotals() usage.Totals {
	t, err := usage.Load(liveUsageDeps())
	if err != nil {
		return usage.Totals{}
	}
	return t
}

// liveWriteUsage atomically persists the folded store via usage.Save (full-file replace,
// temp+rename, 0600/0700, traversal-guarded). The dashboard server calls this inside the
// usageMu critical section and treats a returned error as loud-but-non-fatal
// (T-15-17).
func liveWriteUsage(t usage.Totals) error {
	return usage.Save(liveUsageDeps(), t)
}

// liveModelID re-reads cfg.Model from config at scrape time (config is the single source
// of truth, Pitfall 2). The dashboard server invokes it INSIDE the usageMu section so the
// per-model fold key reflects the model the scrape is actually observing; an unreadable
// config yields "" (Fold keys an empty entry it discards when no counter is Known).
func liveModelID() string {
	cfg, err := config.LoadVilla()
	if err != nil {
		return ""
	}
	return cfg.Model
}

// liveModelsView composes the Models read-model: it loads the catalog and the
// persisted config (the source of truth for the loaded model), then for each catalog entry
// marks loaded (== cfg.Model) / on-disk (weights present) / catalog-only and computes the
// per-row fit verdict by reusing recommend.Pick over the entry — the SAME fit-math
// `villa model swap` uses, never re-implemented. It returns (nil, false) on a catalog-load
// failure so the dashboard renders the "No models in catalog" empty state honestly.
func liveModelsView() ([]dashboard.ModelView, bool) {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return nil, false
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		// Config is the loaded-model source of truth; without it we still list the
		// catalog (nothing marked loaded) rather than fail the whole panel.
		cfg = config.VillaConfig{}
	}

	profile := detect.Probe()
	// Memory inputs from the fail-soft cfg load above: the dashboard's
	// per-model fit column reflects the same shrunken envelope recommend uses
	// (a load error left cfg zero-valued — memory off).
	mem := recommend.MemoryInputs{Enabled: cfg.MemoryEnabled, EmbeddingModel: cfg.EmbeddingModel}
	views := make([]dashboard.ModelView, 0, len(cat.Models))
	for _, m := range cat.Models {
		// Reuse recommend.Pick fit-math by overriding to this entry (the same override
		// path liveSwapDeps.Fits uses, recommend.go) — never new envelope math.
		rec := recommend.Pick(profile, cat, recommend.Overrides{Model: m.ID}, mem, webSearchInputsFrom(cfg))
		views = append(views, dashboard.ModelView{
			ID:        m.ID,
			Quant:     m.Quant,
			Loaded:    m.ID == cfg.Model,
			OnDisk:    modelOnDisk(m),
			Fits:      rec.Fits,
			FitDetail: fitDetail(rec),
		})
	}
	return views, true
}

// modelOnDisk reports whether a catalog model's primary weight file is already
// downloaded (mirrors liveSwapDeps.IsDownloaded so the dashboard and swap agree).
func modelOnDisk(m catalog.Model) bool {
	path := filepath.Join(modelsDir(), m.PrimaryFile())
	_, err := os.Stat(path)
	return err == nil
}

// fitDetail renders the confirm-dialog fit-verdict line from a Recommendation: the
// fitting form ("Fits: {total} ≤ {envelope} — {headroom} headroom at {ctx} context.") or
// the won't-fit reason ("needs {total} vs {envelope} usable"). It reuses recommend's
// already-computed fit terms — no new math.
func fitDetail(rec recommend.Recommendation) string {
	if rec.Fits {
		return fmt.Sprintf("Fits: %s ≤ %s — %s headroom at %d context.",
			fitGiB(rec.TotalBytes), fitGiB(rec.UsableEnvelopeBytes), fitGiB(rec.HeadroomBytes), rec.ContextLen)
	}
	return fmt.Sprintf("needs %s vs %s usable", fitGiB(rec.TotalBytes), fitGiB(rec.UsableEnvelopeBytes))
}

// fitGiB formats a byte count as GiB with one decimal for the dashboard confirm-dialog
// fit-verdict line (terser than the CLI's gib(), which appends raw bytes for the table).
func fitGiB(b uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(b)/(1024*1024*1024))
}
