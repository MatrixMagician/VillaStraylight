package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// status.go is the thin cobra caller for the offload-asserting `villa status` slice
// (CLI-03/PRIV-01/PRIV-03). The read-model core — aggregation, the worst-wins fold,
// the publish-port privacy parse, and the frozen --json contract — was extracted to
// internal/status (Phase-5 DASH-01) so the dashboard backend calls the SAME logic.
// This file keeps only: the cobra wiring + exit-code mapping, the human table
// renderer (CLI presentation), and the live host wiring (HTTP/journald/GTT probes)
// that constructs status.Deps. status_test.go drives runStatus through a stubbed
// status.Deps and freezes the --json contract byte-for-byte.

// statusHTTPTimeout bounds a single /health or /props probe.
const statusHTTPTimeout = readinessHTTPTimeout

// newStatus builds `villa status`: aggregate unit + container + /health + offload
// Verdict into one table (or --json), assert the loopback/no-telemetry posture, and
// exit 0/2/1. The exit-code mapping lives entirely here (return-not-Exit verb body;
// cobra RunE calls os.Exit) mirroring runInference/runInstall.
func newStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show aggregated health: unit + container + /health + GPU-offload proof, and the loopback/no-telemetry posture",
		Long: "Aggregate, for every service in the generated stack, the systemd unit active-state, the " +
			"container /health, and the running-server GPU-offload Verdict (residency proven from the " +
			"journald load_tensors Vulkan0 line, corroborated by a point-in-time GTT floor) into one " +
			"table. Asserts every published port binds loopback (none on 0.0.0.0) and that there is no " +
			"telemetry. Exits 0 (all PASS), 2 (any WARN), or 1 (any FAIL).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveStatusDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "status: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runStatus(cmd, args, deps))
			return nil
		},
	}
}

// runStatus builds the Report from the injected core and renders it. It RETURNS the
// exit code (no os.Exit) so status_test.go drives it deterministically. All printing
// + exit mapping lives here; the read-model is status.Run/status.Aggregate.
func runStatus(cmd *cobra.Command, _ []string, d *status.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	report := status.Run(*d)
	if err := report.Err(); err != nil {
		fmt.Fprintf(errOut, "status: %v\n", err)
		return exitBlocked
	}

	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		renderStatusTable(out, report, verbose)
	}

	switch status.Aggregate(report) {
	case inference.StatusPass:
		return exitPass
	case inference.StatusWarn:
		return exitWarn
	default:
		return exitBlocked
	}
}

// renderStatusTable writes the aggregated report as an aligned human table: the
// overall verdict, each service's active/health/offload row, and the privacy
// posture (loopback-only + the no-telemetry statement). With -v it adds each
// offload Verdict's detail/provenance. This is CLI presentation (not the
// read-model), so it stays in cmd/villa.
func renderStatusTable(w io.Writer, r status.Report, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "overall\t%s\n", r.Overall)

	// Active-backend surface (D-01): backend name always; the digest-pinned image tag
	// is verbose-only to keep the default table compact (gated behind -v like the
	// offload provenance). The image string is a value from the resolved backend
	// (Report.Image), never a literal in this renderer.
	fmt.Fprintf(tw, "backend\t%s\n", r.Backend)
	if withProvenance {
		fmt.Fprintf(tw, "image\t%s\n", r.Image)
	}
	// Live tok/s (D-03): rendered ONLY when present — an idle/unavailable reading is
	// omitted (the seam returned nil), never a fabricated 0. Labeled by the active
	// backend so the user sees which backend produced the rate.
	if r.GenTokensPerSec != nil {
		fmt.Fprintf(tw, "gen tok/s\t%.1f (%s)\n", *r.GenTokensPerSec, r.Backend)
	}
	// ROCm-readiness tri-state (D-04): the folded indicator (ready/not-ready/unknown).
	fmt.Fprintf(tw, "rocm-readiness\t%s\n", r.ROCmReadiness)

	// Cumulative usage (D-09): rendered ONLY when present — an absent/empty store is
	// omitted (the read-only seam returned nil), never fabricated 0s. Prints the
	// per-model cumulative prompt/generated token totals.
	if r.Usage != nil {
		for _, m := range r.Usage.Models {
			fmt.Fprintf(tw, "usage %s\tprompt %d / generated %d (cumulative)\n",
				m.Model, m.Prompt.Cumulative, m.Predicted.Cumulative)
		}
	}

	fmt.Fprintf(tw, "\nSERVICE\tACTIVE\tHEALTH\tOFFLOAD\n")
	for _, s := range r.Services {
		// A service with no GPU offload (OffloadApplies=false, e.g. Open WebUI)
		// carries a typed N/A Verdict that is EXCLUDED from the worst-wins fold
		// (D-12). Render it as "N/A" rather than leaking the underlying WARN-typed
		// verdict. The --json contract keeps the full Verdict + OffloadApplies.
		offloadCell := "N/A"
		if s.OffloadApplies {
			offloadCell = fmt.Sprintf("%s", s.Offload.Status)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Service, s.Active, s.Health, offloadCell)
	}
	fmt.Fprintf(tw, "\nloopback-only\t%s\n", strconv.FormatBool(r.LoopbackOnly))
	for _, p := range r.Ports {
		mark := "loopback"
		if !p.Loopback {
			mark = "EXPOSED (not loopback)"
		}
		fmt.Fprintf(tw, "  port %s\t%s:%s\n", p.ContainerPort, p.HostAddr, mark)
	}
	fmt.Fprintf(tw, "telemetry\t%s\n", r.NoTelemetry)
	if withProvenance {
		for _, s := range r.Services {
			fmt.Fprintf(tw, "\n%s offload\t%s\n", s.Service, s.Offload.Detail)
			fmt.Fprintf(tw, "%s provenance\t%s\n", s.Service, s.Offload.Provenance)
			if s.Offload.Remediation != "" {
				fmt.Fprintf(tw, "%s remediation\t%s\n", s.Service, s.Offload.Remediation)
			}
		}
	}
	_ = tw.Flush()
}

// liveStatusDeps wires status.Deps to the real host: config.LoadVilla, the
// orchestrate render + systemd is-active/journald seam, the live /health + /props
// HTTP probes (bounded), the live GTT reader, and the recommend-derived weight
// footprint. It is replaced wholesale by stubs in status_test.go.
func liveStatusDeps() (*status.Deps, error) {
	sys := orchestrate.NewSystemd()
	// Resolve the backend from config (fail-closed, D-02): the inference endpoint is
	// derived from the resolved backend's container runner, never a hardcoded literal.
	cfg, err := config.LoadVilla()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return nil, fmt.Errorf("resolve backend: %w", err)
	}
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()
	return &status.Deps{
		LoadConfig: config.LoadVilla,
		ModelFile:  liveModelFile,
		ModelsDir:  modelsDir,
		Render:     orchestrate.Render,
		IsActive:   sys.IsActive,
		Health:     liveHealthProbe,
		OWUIHealth: liveOpenWebUIHealth,
		// ResidencyJournal (not JournalText) — the offload assert needs the CURRENT
		// invocation's startup, where the load_tensors residency line lives; the
		// whole-unit journal's oldest bytes are stale prior-start output (F-3).
		JournalText: sys.ResidencyJournal,
		Props:       liveProps,
		GTTUsed:     detect.GTTUsedBytes,
		WeightBytes: liveWeightBytes,
		Endpoint:    func() string { return endpoint },
		OWUIService: openWebUIServiceName,
		// Dashboard self-row (Plan 05-05 / D-04): the dashboard is a managed, observable
		// member of the stack. Its addr is the config'd loopback DashboardAddr:Port (read
		// from config, never hard-coded — D-13); DashboardHealth is a bounded GET to its
		// own /api/healthz (benign self-recursion, Pitfall 6).
		DashboardService: orchestrate.DashboardServiceName,
		DashboardAddr:    liveDashboardAddr(),
		DashboardHealth:  liveDashboardHealth,
		// Live tok/s (D-03): REUSE the dashboard-proven metrics collector — no new
		// scraper. nil on a failed/absent /metrics scrape or an idle server, so the
		// figure is omitted (typed-Unknown), NEVER a fabricated 0.
		GenTokensPerSec: liveGenTokensPerSec,
		// ROCm-readiness (D-04): CONSUME the already-computed detect rocm_readiness
		// sub-tree; internal/status folds it. Never recompute the signals here.
		ROCmReadiness: func() detect.ROCmReadiness { return detect.Probe().ROCmReadiness },
		// Cumulative usage (D-07/D-09): READ-ONLY load of usage.json. The CLI is
		// one-shot and NEVER writes the store (the dashboard, Plan 04, is the sole
		// writer); nil on an absent/empty store so the figure is omitted.
		ReadUsage: liveReadUsage,
	}, nil
}

// liveReadUsage loads the cumulative-usage store READ-ONLY (D-07): it wires a
// usage.Deps whose ReadAll reads usage.UsagePath() via os.ReadFile (returning
// (nil,nil) on a not-yet-created store so usage.Load fails closed to empty) and
// supplies NO WriteAll seam — the CLI status path can never write usage.json (the
// dashboard, Plan 04, is the sole writer). It returns a *usage.UsageTotals only when
// the store holds at least one model entry; an absent/empty/corrupt store yields nil
// so the Report omits the usage key (typed-Unknown, never a fabricated 0 — D-09).
func liveReadUsage() *usage.UsageTotals {
	path := usage.UsagePath()
	deps := usage.Deps{
		ReadAll: func() ([]byte, error) {
			b, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return nil, nil // absent store ⇒ fail-closed-to-empty in usage.Load
			}
			if err != nil {
				return nil, err
			}
			return b, nil
		},
	}
	totals, err := usage.Load(deps)
	if err != nil {
		return nil // unreadable store → typed-Unknown (omitted), never a fabricated 0
	}
	if len(totals.Models) == 0 {
		return nil // empty store ⇒ omit the usage key (D-09)
	}
	return &totals
}

// liveGenTokensPerSec reads the live token-generation throughput by REUSING the
// dashboard's bounded metrics collector (D-03): metrics.ScrapeMetrics +
// metrics.IsGenerating. It returns nil — a typed-Unknown the Report omits — on a
// failed/absent /metrics scrape (404/transport) OR when the server is idle (the
// gauges are stale snapshots when !IsGenerating), so the surface NEVER shows a
// fabricated 0 tok/s. The scrape inherits the collector's 2s timeout + 64 KiB
// io.LimitReader bounds (no new attack surface). Mirrors liveProps' nil-on-failure
// discipline.
func liveGenTokensPerSec(endpoint string) *float64 {
	snap, ok := metrics.ScrapeMetrics(endpoint)
	if !ok {
		return nil // /metrics 404 or transport error → typed-Unknown (omitted)
	}
	slots, _ := metrics.ScrapeSlots(endpoint)
	if !metrics.IsGenerating(snap, slots) {
		return nil // idle: gauges are stale snapshots → omit, never a fabricated 0 (D-03)
	}
	v := snap.GenTokensPerSec
	return &v
}

// liveDashboardAddr resolves the dashboard's loopback base URL from config
// (DashboardAddr:DashboardPort, default 127.0.0.1:8888 — D-13). It reads config so the
// probe target is never hard-coded; a config-load failure falls back to the typed
// defaults so the status row still probes the conventional loopback address.
func liveDashboardAddr() string {
	cfg, err := config.LoadVilla()
	if err != nil {
		return "http://127.0.0.1:8888"
	}
	addr := cfg.DashboardAddr
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := cfg.DashboardPort
	if port == 0 {
		port = 8888
	}
	return "http://" + net.JoinHostPort(addr, strconv.Itoa(port))
}

// liveDashboardHealth maps a single bounded GET to the dashboard's own /api/healthz to
// a status.HealthState: 200 → ready, any other code → loading (up but not confirmed),
// any transport error / unreachable → down (the wedged-dashboard case). The probe is
// bounded by statusHTTPTimeout + io.LimitReader so a wedged dashboard can NEVER hang
// `villa status` despite the benign self-recursion (Pitfall 6 / T-05-16). Note the path
// is /api/healthz (the route Plan 02 mounted under /api), not a top-level /healthz.
func liveDashboardHealth(addr string) status.HealthState {
	if addr == "" {
		return status.HealthUnknown
	}
	client := &http.Client{Timeout: statusHTTPTimeout}
	resp, err := client.Get(addr + "/api/healthz")
	if err != nil {
		return status.HealthDown
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusOK {
		return status.HealthReady
	}
	return status.HealthLoading
}

// liveHealthProbe maps a single /health GET to a status.HealthState: 200→ready,
// 503→loading (up but not ready — Unknown, never down, WR-07), any transport
// error / other code → down. The body is bounded by io.LimitReader.
func liveHealthProbe(endpoint string) status.HealthState {
	client := &http.Client{Timeout: statusHTTPTimeout}
	resp, err := client.Get(endpoint + "/health")
	if err != nil {
		return status.HealthDown
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	switch resp.StatusCode {
	case http.StatusOK:
		return status.HealthReady
	case http.StatusServiceUnavailable:
		return status.HealthLoading
	default:
		return status.HealthDown
	}
}

// liveOpenWebUIHealth maps the Open WebUI row's health to a status.HealthState by
// probing the UPSTREAM llama-server /v1/models (reachable on the host-published
// 127.0.0.1:8080 — no WEBUI_AUTH friction, RESEARCH A3): a non-empty {"data":[...]}
// model list → HealthReady (CHAT-01 SC#1); an empty list / non-200 → HealthLoading
// (up but not ready → WARN, never a false PASS); any transport error → HealthUnknown
// (typed-Unknown → WARN). The body is bounded by io.LimitReader.
func liveOpenWebUIHealth(endpoint string) status.HealthState {
	client := &http.Client{Timeout: statusHTTPTimeout}
	resp, err := client.Get(endpoint + "/v1/models")
	if err != nil {
		return status.HealthUnknown // transport error / unreachable → typed-Unknown → WARN
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return status.HealthLoading // up but not serving a model list yet → WARN
	}
	var raw struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return status.HealthLoading // reachable but unparseable → not ready → WARN
	}
	if len(raw.Data) == 0 {
		return status.HealthLoading // empty model list → up but not ready → WARN (no false PASS)
	}
	return status.HealthReady // non-empty model list → Open WebUI reaches a model (PASS)
}

// liveProps fetches and parses llama.cpp /props for the config-identity drift
// overlay (D-13 — corroboration only, never the residency proof). A transport
// error / unparseable body yields nil (Unknown), which never produces a false PASS
// or a FAIL in RunningOffloadVerdict. The body is bounded by io.LimitReader.
func liveProps(endpoint string) *inference.PropsInfo {
	client := &http.Client{Timeout: statusHTTPTimeout}
	resp, err := client.Get(endpoint + "/props")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var raw struct {
		ModelPath     string `json:"model_path"`
		DefaultParams struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		NCtx int `json:"n_ctx"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	nctx := raw.NCtx
	if nctx == 0 {
		nctx = raw.DefaultParams.NCtx
	}
	return &inference.PropsInfo{ModelPath: raw.ModelPath, NCtx: nctx}
}

// liveWeightBytes derives the configured model's expected weight footprint from the
// recommend fit math (the GTT-floor reference). It probes the host and picks for the
// config'd model; an undeterminable envelope yields 0 (the GTT floor then degrades
// to a typed-Unknown WARN, never a false PASS).
func liveWeightBytes(cfg config.VillaConfig) uint64 {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return 0
	}
	// Zero-value memory inputs ON PURPOSE: this provably keeps status.json.golden
	// byte-identical — WeightBytes is envelope-independent for overrides (guarded
	// by TestPickOverrideWeightInvariance), so the frozen status path never sees
	// the memory reservation.
	rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model}, recommend.MemoryInputs{})
	return rec.WeightBytes
}
