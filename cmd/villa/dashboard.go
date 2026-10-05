package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/dashboard"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
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
// through a stubbed serve func.

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
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := liveDashboardDeps(cmdContext(cmd))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "dashboard: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runDashboard(cmd, c, func(ctx context.Context, s *dashboard.Server) error { return s.Serve(ctx) }))
			return nil
		},
	}
}

// runDashboard constructs the dashboard.Server from the composed Config, prints the
// live loopback URL, and serves until serve returns. It RETURNS the exit code (no
// os.Exit in the body) so dashboard_test.go drives it deterministically with a
// stubbed serve that binds no socket; the live serve is (*dashboard.Server).Serve,
// which shuts down gracefully on cancellation.
func runDashboard(cmd *cobra.Command, c dashboard.Config, serve func(context.Context, *dashboard.Server) error) int {
	errOut := cmd.ErrOrStderr()

	srv, err := dashboard.NewServer(c)
	if err != nil {
		fmt.Fprintf(errOut, "dashboard: %v\n", err)
		return exitBlocked
	}

	fmt.Fprintf(cmd.OutOrStdout(), "villa dashboard listening on http://%s\n", srv.Addr())

	if err := serve(cmdContext(cmd), srv); err != nil {
		fmt.Fprintf(errOut, "dashboard: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveDashboardDeps composes the dashboard's Config from the real host: the ports
// from config.LoadVilla, the live status read-model seam (reusing liveStatusDeps so
// the dashboard and the CLI fold the IDENTICAL core), and the panel collectors.
//
// The service runs for days, so nothing here may answer a question once for the life
// of the process that config can change (#253). The status seams gate on the config
// each run loads (ADR-0016), and the /api/metrics reads build their inference client
// per scrape, so an api key written after startup (`villa up` heals a missing one)
// is the key they send.
//
// ctx is the process-lifetime context: it stops Serve on SIGTERM, and it also
// bounds the multi-GB pull a POST /api/models/switch can start, so a stop signal
// does not leave that transfer running past the graceful-shutdown window. Aborting
// a pull is safe and does not corrupt state — the partial ".part" file is kept and
// resumed via HTTP Range, and the config save happens only AFTER the pull succeeds.
func liveDashboardDeps(ctx context.Context) (dashboard.Config, error) {
	cfg, err := config.LoadVilla()
	if err != nil {
		return dashboard.Config{}, fmt.Errorf("load config: %w", err)
	}
	// liveStatusDeps is the SINGLE backend-resolution point (fail-closed), so the
	// dashboard's status panel reports on exactly what `villa status` does.
	statusDeps, err := liveStatusDeps()
	if err != nil {
		return dashboard.Config{}, err
	}

	// The runner exists only when the workspace agent is on at startup. Recover
	// runs HERE, before Serve binds, so a record left running by the previous
	// service instance is interrupted before any client can read it. A recovery
	// error is reported and does not stop the dashboard: one corrupt task record
	// must not take the whole service down. The grounding audit reaches the SAME
	// server villa status reports on, through the authenticated client (ADR-0014).
	var tasks *taskrun.Runner
	if subsystem.SandboxOn(cfg) {
		tasks = taskrun.New(liveTaskRunDeps(ctx, inferenceClient(cfg)))
		if err := tasks.Recover(); err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: task recovery: %v\n", err)
		}
	}

	perfRead, slotsRead, counterRead := newScrapeClient().reads(ctx)

	return dashboard.Config{
		Tasks:         tasks,
		StatusDeps:    *statusDeps,
		ChatPort:      cfg.ChatPort,
		DashboardAddr: config.DashboardAddr,
		DashboardPort: cfg.DashboardPort,

		// Performance: bounded /metrics + /slots scrapes of the inference endpoint.
		Metrics: perfRead,
		Slots:   slotsRead,

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
		// counter scrape reads the SAME /metrics route the live tok/s reads — no new
		// outbound.
		ReadUsage:     liveReadUsageTotals,
		WriteUsage:    liveWriteUsage,
		ModelID:       liveModelID,
		CounterSample: counterRead,

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

// scrapeClientTTL is how long one config load serves the dashboard's inference reads.
// One /api/metrics scrape makes three of them (the usage counter fold, the gauges and
// the slots), so they share a load; it is well under the UI's 2.5 s poll, so the next
// scrape reads config again and a key written by `villa up` reaches it (#253).
const scrapeClientTTL = time.Second

// scrapeClient serves the inference client for the dashboard's per-scrape reads (#253)
// from the config as it is now, not as it was at startup. A config that no longer loads
// is the zero config, so the client is keyless and the unit answers a 401 on every keyed
// route: the panel reads unavailable, never a crash and never a stale key.
//
// The three reads of one scrape reuse one client for scrapeClientTTL rather than
// loading config.toml each: the server calls them separately, so a scrape has no
// boundary to build the client at.
type scrapeClient struct {
	load  func() (config.VillaConfig, error)
	build func(config.VillaConfig) inference.Client
	now   func() time.Time

	mu      sync.Mutex
	builtAt time.Time
	client  inference.Client
	built   bool
}

func newScrapeClient() *scrapeClient {
	return &scrapeClient{load: config.LoadVilla, build: inferenceClient, now: time.Now}
}

// current returns the client for the config as of this scrape.
func (s *scrapeClient) current() inference.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now := s.now(); !s.built || now.Sub(s.builtAt) >= scrapeClientTTL {
		cfg, _ := s.load()
		s.client, s.builtAt, s.built = s.build(cfg), now, true
	}
	return s.client
}

// reads returns the dashboard's three inference reads (gauges, slots and the usage
// counters), each taking the client for the scrape it belongs to.
func (s *scrapeClient) reads(ctx context.Context) (
	perf func() (metrics.PerfSnapshot, bool),
	slots func() ([]metrics.Slot, bool),
	counters func() (metrics.CounterSample, bool),
) {
	return func() (metrics.PerfSnapshot, bool) { return s.current().Perf(ctx) },
		func() ([]metrics.Slot, bool) { return s.current().Slots(ctx) },
		func() (metrics.CounterSample, bool) { return s.current().Counters(ctx) }
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
		ReadAll:  storeReader(path), // absent store ⇒ Load fails closed to empty (typed-Unknown)
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
// marks loaded (== cfg.Model) / on-disk (every file present) / catalog-only and computes
// the per-row fit verdict through swapFit and modelswap.Size — the SAME fit and ctx rule
// `villa model swap` runs, never re-implemented. It returns (nil, false) on a catalog-load
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
	fits := swapFit(profile, cat, mem, webSearchInputsFrom(cfg))
	for _, m := range cat.Models {
		// The same fit and ctx rule a switch to this entry would run (#301), so the
		// column shows what the switch would do.
		target := cfg
		target.Model = m.ID
		fit, _ := modelswap.Size(m, target, fits)
		views = append(views, dashboard.ModelView{
			ID:        m.ID,
			Quant:     m.Quant,
			Loaded:    m.ID == cfg.Model,
			OnDisk:    modelOnDisk(m),
			Fits:      fit.OK,
			FitDetail: fit.Detail,
		})
	}
	return views, true
}

// modelOnDisk reports whether every file of a catalog model, its projector and
// draft sidecars included, is downloaded. It is also `model swap`'s IsDownloaded,
// so the dashboard and the swap agree, and a swap that turns vision on pulls a
// projector the weights were fetched without (#299).
func modelOnDisk(m catalog.Model) bool {
	for _, sh := range m.AllShards() {
		if _, err := os.Stat(filepath.Join(modelsDir(), sh.Filename)); err != nil {
			return false
		}
	}
	return true
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
