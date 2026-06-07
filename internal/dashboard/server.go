// Package dashboard is the VillaStraylight control dashboard backend (Phase-5
// DASH-01/DASH-05): a loopback-only chi HTTP server that serves a read-model JSON
// API by folding the SHARED internal/status core (never a fork) plus an embedded
// no-build single-page UI.
//
// The server binds 127.0.0.1 explicitly via net.JoinHostPort (Pitfall 6 / PRIV-01,
// T-05-03) — never ":8888" / "0.0.0.0". The /api routes are read-only GETs in this
// slice; the single future mutation (POST /api/models/switch, Plan 04) is guarded by
// construction by the requireSameOrigin middleware on the /api subrouter (T-05-04).
package dashboard

import (
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// defaultDashboardAddr is the loopback default applied when Config.DashboardAddr is
// empty, so an unset bind address can never accidentally become the all-interfaces
// empty host (Pitfall 6 / PRIV-01).
const defaultDashboardAddr = "127.0.0.1"

// Config is the composed input NewServer needs: the SHARED status read-model seam
// (the same status.Deps the CLI wires), the chat link target port, and the
// loopback bind address/port.
type Config struct {
	// StatusDeps is the injected status read-model seam. handleStatus folds
	// status.Run(StatusDeps) — the SAME core villa status uses (DASH-01).
	StatusDeps status.Deps
	// ChatPort is the Open WebUI host port the header chat link targets (DASH-05/D-12),
	// read from config, never hard-coded.
	ChatPort int
	// DashboardAddr is the loopback bind address (default 127.0.0.1, PRIV-01).
	DashboardAddr string
	// DashboardPort is the host port the dashboard listens on (default 8888).
	DashboardPort int

	// --- Performance (DASH-02) seams ---------------------------------------
	// These are injected func fields (the project's collector seam pattern) so
	// api_test stubs the metrics scrape without a live llama-server. When nil, the
	// live wiring fills them in liveDashboardDeps (scraping the inference endpoint).

	// Metrics scrapes llama-server /metrics into a PerfSnapshot; the bool is the
	// typed-Unknown availability flag (false on a 404/transport error — D-11).
	Metrics func() (metrics.PerfSnapshot, bool)
	// Slots scrapes llama-server /slots into the narrow []Slot view (bool=available).
	Slots func() ([]metrics.Slot, bool)

	// --- GPU & Memory (DASH-03) seams --------------------------------------
	// Memory-first headline + best-effort busy%, each a typed value so the panel
	// degrades to "unavailable" rather than a fabricated number (D-06).

	// MemUsed is the unified-memory (GTT) bytes currently in use (the headline used).
	MemUsed func() detect.Bytes
	// MemEnvelope is the usable unified-memory ceiling (the headline total/envelope).
	MemEnvelope func() detect.Bytes
	// GPUBusy is the best-effort iGPU utilization 0..100 (typed-Unknown when absent).
	GPUBusy func() detect.Int

	// --- Models (DASH-04) seam ---------------------------------------------
	// Models lists the full catalog marked loaded/on-disk/catalog-only with a per-row
	// Fits flag + fit-detail (the SHARED fit-math, not re-implemented here). The bool is
	// the availability flag (false on a catalog-load failure → empty list / "No models in
	// catalog"). The live wiring (cmd/villa) composes runModelList's shape + recommend.Pick.
	Models func() ([]ModelView, bool)

	// SwapDeps is the SHARED modelswap.Deps the POST /api/models/switch handler folds:
	// handleSwitch calls modelswap.Run(SwapDeps, id) VERBATIM — the SAME guarded
	// resolve→fit→pull→save→regenerate→restart path `villa model swap` uses (DASH-04).
	// The live wiring (cmd/villa liveSwapDeps) stays in cmd; the dashboard adds zero swap
	// logic. A zero value yields an always-refuse swap (no resolver) so a mis-wired Server
	// never silently fires a swap.
	SwapDeps modelswap.Deps

	// --- Cumulative usage (USAGE-02 / D-07) writer seams -------------------
	// The dashboard's /api/metrics scrape path is the SOLE, usageMu-guarded writer of
	// usage.json (D-07): on each scrape it folds the _total counters + the configured
	// model into the store and atomically writes. These func fields are the injected
	// store/identity seams so api_test exercises the fold+write off-hardware; the live
	// wiring (liveDashboardDeps) fills them with usage.Load / usage.WriteFileAtomic over
	// usage.UsagePath and cfg.Model. When nil, NewServer defaults each to an honest
	// no-op so a partially-wired Server never writes and never panics.

	// ReadUsage loads the persisted store (the fold's prior). nil → empty UsageTotals.
	ReadUsage func() usage.UsageTotals
	// WriteUsage atomically writes the folded store. nil → a no-op (never writes).
	WriteUsage func(usage.UsageTotals) error
	// ModelID returns the configured model id (cfg.Model) used as the per-model fold key;
	// it is read INSIDE the usageMu critical section so the key cannot drift mid-write
	// (Pitfall 2 / T-15-14). nil → "" (Fold then keys an empty-id entry — honest no-op).
	ModelID func() string

	// CounterSample scrapes the two monotonic _total counters (the fold's source) from
	// the SAME loopback endpoint already scraped for live tok/s — no new outbound (D-12).
	// nil → a typed-Unknown (unavailable) sample, so no counter folds.
	CounterSample func() (metrics.CounterSample, bool)
}

// Server holds the composed dashboard configuration and exposes the chi handler and
// the loopback bind address. It performs no I/O until ListenAndServe.
type Server struct {
	statusDeps    status.Deps
	chatPort      int
	dashboardAddr string
	port          int
	router        http.Handler
	assets        fs.FS
	shell         *template.Template

	// Performance + GPU collector seams (Plan 03). Filled from Config; defaulted to
	// always-unavailable no-ops when Config leaves them nil, so a partially-wired
	// Server still degrades honestly (every panel → "unavailable") rather than panicking.
	scrapeMetrics func() (metrics.PerfSnapshot, bool)
	scrapeSlots   func() ([]metrics.Slot, bool)
	memUsed       func() detect.Bytes
	memEnvelope   func() detect.Bytes
	gpuBusy       func() detect.Int

	// Models seam (Plan 04). Defaulted to an always-unavailable empty list when Config
	// leaves it nil, so a partially-wired Server renders the "No models in catalog"
	// empty state honestly rather than nil-panicking.
	listModels func() ([]ModelView, bool)

	// swapDeps is the SHARED modelswap.Deps handleSwitch folds (Plan 04 mutation).
	swapDeps modelswap.Deps

	// swapMu serializes POST /api/models/switch so two near-simultaneous swaps can never
	// interleave the non-atomic modelswap.Run read-modify-write (LoadConfig → SaveConfig →
	// ReconcileAndWrite → Restart) and corrupt the config↔units source-of-truth invariant
	// (CR-02). handleSwitch holds it via TryLock and refuses a concurrent switch with 409.
	swapMu sync.Mutex

	// Cumulative-usage writer seams (USAGE-02 / D-07). Filled from Config; defaulted to
	// honest no-ops when Config leaves them nil so a partially-wired Server never writes
	// usage.json and never nil-panics in handleMetrics.
	readUsage     func() usage.UsageTotals
	writeUsage    func(usage.UsageTotals) error
	modelID       func() string
	counterSample func() (metrics.CounterSample, bool)

	// usageMu serializes the whole read-modify-write of usage.json in handleMetrics so two
	// concurrent /api/metrics scrapes can never interleave the non-atomic
	// ReadUsage → Fold → WriteUsage and tear the store (T-15-13). It is a sibling to swapMu
	// (a dedicated mutex, not shared) so a usage fold never blocks a model switch and vice
	// versa. The model identity (modelID) is captured INSIDE this section so the per-model
	// fold key cannot drift if config changes mid-write (Pitfall 2 / T-15-14). Unlike
	// swapMu's TryLock-then-409 (a swap may be safely refused), the usage write uses
	// Lock/defer-Unlock: a scrape's fold must NOT be silently skipped under contention.
	usageMu sync.Mutex
}

// isLoopbackAddr reports whether a configured DashboardAddr denotes the loopback
// interface — the only bind the dashboard's PRIV-01 posture permits. An empty value is
// the caller's signal to apply defaultDashboardAddr and is treated as loopback.
func isLoopbackAddr(addr string) bool {
	switch addr {
	case "", "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

// NewServer constructs a Server from Config, defaulting the bind address to loopback
// when unset and building the chi router (route table + middleware chain + embedded
// UI). It never binds until ListenAndServe is called. A non-empty, non-loopback
// DashboardAddr (e.g. "0.0.0.0") is REFUSED with an error so the PRIV-01 loopback
// posture is enforced by construction, not merely by the empty-string default (IN-03):
// a tampered config can never make the dashboard bind all interfaces.
func NewServer(cfg Config) (*Server, error) {
	if !isLoopbackAddr(cfg.DashboardAddr) {
		return nil, fmt.Errorf("dashboard: refusing non-loopback bind address %q (PRIV-01: only 127.0.0.1, ::1, localhost, or empty are allowed)", cfg.DashboardAddr)
	}
	addr := cfg.DashboardAddr
	if addr == "" {
		addr = defaultDashboardAddr
	}
	s := &Server{
		statusDeps:    cfg.StatusDeps,
		chatPort:      cfg.ChatPort,
		dashboardAddr: addr,
		port:          cfg.DashboardPort,
		scrapeMetrics: cfg.Metrics,
		scrapeSlots:   cfg.Slots,
		memUsed:       cfg.MemUsed,
		memEnvelope:   cfg.MemEnvelope,
		gpuBusy:       cfg.GPUBusy,
		listModels:    cfg.Models,
		swapDeps:      cfg.SwapDeps,
		readUsage:     cfg.ReadUsage,
		writeUsage:    cfg.WriteUsage,
		modelID:       cfg.ModelID,
		counterSample: cfg.CounterSample,
	}

	// Default any unset collector seam to an always-unavailable / typed-Unknown no-op,
	// so a Server constructed without the Plan-03 seams still degrades honestly
	// (panels render "unavailable") instead of nil-panicking in a handler (D-11).
	if s.scrapeMetrics == nil {
		s.scrapeMetrics = func() (metrics.PerfSnapshot, bool) { return metrics.PerfSnapshot{}, false }
	}
	if s.scrapeSlots == nil {
		s.scrapeSlots = func() ([]metrics.Slot, bool) { return nil, false }
	}
	if s.memUsed == nil {
		s.memUsed = func() detect.Bytes { return detect.UnknownBytes("memory reader not wired", "") }
	}
	if s.memEnvelope == nil {
		s.memEnvelope = func() detect.Bytes { return detect.UnknownBytes("envelope reader not wired", "") }
	}
	if s.gpuBusy == nil {
		s.gpuBusy = func() detect.Int { return detect.UnknownInt("busy% reader not wired", "") }
	}
	if s.listModels == nil {
		s.listModels = func() ([]ModelView, bool) { return []ModelView{}, false }
	}

	// Default the cumulative-usage writer seams to honest no-ops so a Server constructed
	// without the USAGE-02 wiring never writes usage.json and never nil-panics in
	// handleMetrics: ReadUsage → empty store (no prior), WriteUsage → silent no-op (never
	// writes, D-07), ModelID → "" (Fold keys an empty entry it then discards on no Known
	// counter), CounterSample → typed-Unknown unavailable (no counter folds, D-05).
	if s.readUsage == nil {
		s.readUsage = func() usage.UsageTotals { return usage.UsageTotals{} }
	}
	if s.writeUsage == nil {
		s.writeUsage = func(usage.UsageTotals) error { return nil }
	}
	if s.modelID == nil {
		s.modelID = func() string { return "" }
	}
	if s.counterSample == nil {
		s.counterSample = func() (metrics.CounterSample, bool) { return metrics.CounterSample{}, false }
	}

	// Parse the embedded shell + sub the assets FS once at construction so a parse
	// failure surfaces here (and is panicked, since the embed is compiled-in and
	// cannot legitimately fail) rather than per-request.
	sub, err := Assets()
	if err != nil {
		panic("dashboard: embedded assets unavailable: " + err.Error())
	}
	s.assets = sub
	tmpl, err := template.ParseFS(sub, "dashboard.html")
	if err != nil {
		panic("dashboard: parse embedded shell: " + err.Error())
	}
	s.shell = tmpl

	s.router = s.routes()
	return s, nil
}

// routes builds the chi router mirroring internal/server's middleware chain but with
// a READ-ONLY route table (no chat/SSE route, no AllowedOrigins CORS block). The /api
// subrouter carries the requireSameOrigin guard so any future non-GET (Plan 04's POST)
// is rejected cross-origin by construction; GET routes are unaffected.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		// requireSameOrigin is a no-op for GET/HEAD and rejects cross-origin
		// non-GET (the Plan 04 mutation) with 403 (T-05-04 / Pitfall 7).
		r.Use(requireSameOrigin)
		r.Get("/status", s.handleStatus)
		r.Get("/healthz", s.handleHealthz)
		// Performance + GPU read-models (Plan 03, DASH-02/DASH-03). Still GET-only,
		// behind the same read path; requireSameOrigin only gates non-GET.
		r.Get("/metrics", s.handleMetrics)
		r.Get("/gpu", s.handleGPU)
		// Models read-model (Plan 04, DASH-04): the full catalog marked
		// loaded/on-disk/catalog-only with a per-row fit flag. GET-only here.
		r.Get("/models", s.handleModels)
		// The ONE sanctioned mutation (DASH-04): POST /models/switch routes through the
		// SHARED modelswap.Run core. requireSameOrigin (above) already gates this non-GET
		// (JSON content-type + same-origin), so a cross-origin POST never reaches the
		// handler (T-05-11).
		r.Post("/models/switch", s.handleSwitch)
	})

	// Embedded single-page UI (built in Task 2). Mirrors spaHandler: fs.Sub +
	// http.FileServer with a fallback to the html/template shell.
	r.Handle("/*", s.staticHandler())
	return r
}

// staticHandler serves the embedded no-build single-page UI (D-01). The index "/"
// renders the dashboard.html html/template shell with the config'd ChatPort injected
// into the header chat link (DASH-05/D-12, never hard-coded); html/template
// auto-escapes the value. Every other path (dashboard.css, dashboard.js) is served
// verbatim from the embedded assets via http.FileServer (mirroring spaHandler).
func (s *Server) staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			s.renderShell(w, r)
			return
		}
		fileServer := http.FileServer(http.FS(s.assets))
		fileServer.ServeHTTP(w, r)
	})
}

// renderShell executes the parsed dashboard.html template with the loopback chat port
// injected. On a (compile-time-impossible) execute error it 500s; the template is
// parsed once at NewServer so a parse failure surfaces at startup, not per-request.
func (s *Server) renderShell(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.shell.Execute(w, shellData{ChatPort: s.chatPort}); err != nil {
		http.Error(w, "dashboard: render error", http.StatusInternalServerError)
	}
}

// shellData is the html/template view model for dashboard.html.
type shellData struct {
	// ChatPort is the Open WebUI host port the header chat link targets (D-12),
	// read from config so the link is never a hard-coded 3000.
	ChatPort int
}

// Handler returns the composed chi handler (for httptest and ListenAndServe).
func (s *Server) Handler() http.Handler { return s.router }

// Addr returns the loopback bind address, built via net.JoinHostPort so it is always
// host:port with an explicit 127.0.0.1 host — never ":8888"/"0.0.0.0" (Pitfall 6 /
// PRIV-01, asserted by server_test).
func (s *Server) Addr() string {
	return net.JoinHostPort(s.dashboardAddr, strconv.Itoa(s.port))
}

// ListenAndServe binds the loopback address and serves the dashboard until the
// listener errors. The Addr is loopback-only by construction (Addr()).
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:    s.Addr(),
		Handler: s.router,
	}
	return srv.ListenAndServe()
}
