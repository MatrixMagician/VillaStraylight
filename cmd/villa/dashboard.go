package main

import (
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
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// dashboard.go is the thin cobra caller for `villa dashboard` (DASH-01/DASH-05): it
// loads the loopback dashboard/chat ports from config, composes the SHARED
// internal/status read-model seam (the same status.Deps `villa status` uses — never a
// fork), and serves the loopback-only HTTP dashboard. The server (chi router, /api,
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
	// Serve runs the constructed server until it errors. Stubbed in tests so no real
	// listener is bound; the live wiring calls (*dashboard.Server).ListenAndServe.
	Serve func(*dashboard.Server) error

	// Performance (DASH-02) + GPU (DASH-03) collector seams the dashboard folds into
	// /api/metrics + /api/gpu. Live wiring scrapes the inference endpoint and reads
	// amdgpu sysfs; nil seams default (in dashboard.NewServer) to honest "unavailable".
	Metrics     func() (metrics.PerfSnapshot, bool)
	Slots       func() ([]metrics.Slot, bool)
	MemUsed     func() detect.Bytes
	MemEnvelope func() detect.Bytes
	GPUBusy     func() detect.Int

	// Models lists the catalog marked loaded/on-disk/catalog-only with a per-row fit
	// flag (DASH-04). The live wiring reuses the SAME catalog+config+recommend.Pick
	// fit-math the CLI does; the bool is the availability flag (false on a catalog-load
	// failure → "No models in catalog").
	Models func() ([]dashboard.ModelView, bool)

	// SwapDeps is the SHARED guarded swap core the POST /api/models/switch handler folds
	// (DASH-04). The live wiring is liveSwapDeps() — the IDENTICAL deps `villa model swap`
	// uses, so the dashboard switch routes through the same security contract.
	SwapDeps modelswap.Deps

	// Cumulative-usage writer seams (USAGE-02 / D-07). The dashboard /api/metrics scrape
	// is the SOLE writer of usage.json: ReadUsage loads the fold's prior, WriteUsage
	// atomically persists the folded store, ModelID supplies the per-model key (cfg.Model),
	// and CounterSample scrapes the two monotonic _total counters from the SAME endpoint
	// already scraped for live tok/s (no new outbound, D-12). Nil seams default (in
	// dashboard.NewServer) to honest no-ops that never write.
	ReadUsage     func() usage.UsageTotals
	WriteUsage    func(usage.UsageTotals) error
	ModelID       func() string
	CounterSample func() (metrics.CounterSample, bool)
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
			"never all interfaces — PRIV-01). The dashboard folds the SAME internal/status read-model " +
			"`villa status` uses (not a fork) and links to Open WebUI on the configured chat port. " +
			"Strictly local, zero telemetry.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveDashboardDeps()
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
		DashboardAddr: cfg.DashboardAddr,
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
	})
	if err != nil {
		fmt.Fprintf(errOut, "dashboard: %v\n", err)
		return exitBlocked
	}

	fmt.Fprintf(out, "villa dashboard listening on http://%s\n", srv.Addr())

	if err := d.Serve(srv); err != nil {
		fmt.Fprintf(errOut, "dashboard: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveDashboardDeps wires dashboardDeps to the real host: config.LoadVilla, the live
// status read-model seam (reusing liveStatusDeps so the dashboard and the CLI fold the
// IDENTICAL core), and a Serve that binds the loopback socket.
func liveDashboardDeps() (*dashboardDeps, error) {
	// The inference endpoint is the SAME loopback URL the status seam probes (derived
	// from the config-resolved backend's container runner, never hard-coded), so
	// /api/metrics scrapes the exact server villa status reports on. liveStatusDeps is
	// the SINGLE backend-resolution point (D-03/D-02 fail-closed) — reuse its Endpoint
	// rather than resolve the backend a second time.
	statusDeps, err := liveStatusDeps()
	if err != nil {
		return nil, err
	}
	endpoint := statusDeps.Endpoint()
	return &dashboardDeps{
		LoadConfig: config.LoadVilla,
		StatusDeps: *statusDeps,
		Serve:      func(s *dashboard.Server) error { return s.ListenAndServe() },

		// Performance: bounded /metrics + /slots scrapes of the inference endpoint.
		Metrics: func() (metrics.PerfSnapshot, bool) { return metrics.ScrapeMetrics(endpoint) },
		Slots:   func() ([]metrics.Slot, bool) { return metrics.ScrapeSlots(endpoint) },

		// GPU & Memory (memory-first): the GTT-used headline + the usable unified-memory
		// envelope (from the authoritative HostProfile envelope, never MemTotal) + the
		// best-effort iGPU busy% (typed-Unknown → "unavailable" when absent, D-06).
		MemUsed:     detect.GTTUsedBytes,
		MemEnvelope: func() detect.Bytes { return detect.Probe().UsableEnvelopeBytes },
		GPUBusy:     detect.GPUBusyPercent,

		// Models (DASH-04): the catalog-vs-config list + the SHARED fit-math.
		Models: liveModelsView,

		// Swap (DASH-04): the IDENTICAL guarded swap deps `villa model swap` uses, so the
		// dashboard POST routes through the same resolve→fit→pull→save→regenerate→restart
		// security contract — never a fork.
		SwapDeps: *liveSwapDeps(),

		// Cumulative usage (USAGE-02 / D-07): the dashboard /api/metrics scrape is the SOLE
		// writer of usage.json. ReadUsage loads the fold's prior via usage.Load over
		// usage.UsagePath(); WriteUsage persists the folded store via the atomic temp+rename
		// usage.WriteFileAtomic over the SAME path. ModelID re-reads cfg.Model from config at
		// scrape time (config is the single source of truth; the dashboard server reads it
		// inside the usageMu section so the per-model key cannot drift — Pitfall 2). The
		// counter scrape reuses the SAME loopback `endpoint` already scraped for live tok/s —
		// no new outbound (D-12).
		ReadUsage:     liveReadUsageTotals,
		WriteUsage:    liveWriteUsage,
		ModelID:       liveModelID,
		CounterSample: func() (metrics.CounterSample, bool) { return metrics.ScrapeCounters(endpoint) },
	}, nil
}

// liveUsageDeps builds the usage byte-I/O seam over the live store path: ReadAll reads
// usage.UsagePath() ((nil,nil) when absent so Load fails closed to empty), and WriteAll
// is the atomic temp+rename usage.WriteFileAtomic. The dashboard is the SOLE writer
// (D-07), so this WriteAll seam is wired ONLY here (never in the status read path).
func liveUsageDeps() usage.Deps {
	path := usage.UsagePath()
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
// closed to an empty UsageTotals on an absent/corrupt/schema-skew store (Plan 01), so a
// read failure degrades the fold to a fresh accumulation rather than panicking.
func liveReadUsageTotals() usage.UsageTotals {
	t, err := usage.Load(liveUsageDeps())
	if err != nil {
		return usage.UsageTotals{}
	}
	return t
}

// liveWriteUsage atomically persists the folded store via usage.Save (full-file replace,
// temp+rename, 0600/0700, traversal-guarded). The dashboard server calls this inside the
// usageMu critical section (T-15-13) and treats a returned error as loud-but-non-fatal
// (T-15-17).
func liveWriteUsage(t usage.UsageTotals) error {
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

// liveModelsView composes the Models read-model (DASH-04): it loads the catalog and the
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
	views := make([]dashboard.ModelView, 0, len(cat.Models))
	for _, m := range cat.Models {
		// Reuse recommend.Pick fit-math by overriding to this entry (the same override
		// path liveSwapDeps.Fits uses, recommend.go / D-07) — never new envelope math.
		rec := recommend.Pick(profile, cat, recommend.Overrides{Model: m.ID})
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
func modelOnDisk(m catalog.CatalogModel) bool {
	path := filepath.Join(modelsDir(), primaryModelFile(m))
	_, err := os.Stat(path)
	return err == nil
}

// fitDetail renders the confirm-dialog fit-verdict line from a Recommendation: the
// fitting form ("Fits: {total} ≤ {envelope} — {headroom} headroom at {ctx} context.") or
// the won't-fit reason ("needs {total} vs {envelope} usable"). It reuses recommend's
// already-computed fit terms (D-06) — no new math.
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
