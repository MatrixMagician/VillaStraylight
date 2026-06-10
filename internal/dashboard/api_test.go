package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// stubStatusDeps builds a fully-stubbed status.Deps that renders the real stack
// and reports every service healthy/active, so status.Run returns a deterministic
// Report the dashboard handler can serialize. It mirrors cmd/villa/status_test.go's
// newStatusDeps but trimmed to what the dashboard needs.
func stubStatusDeps(t *testing.T) status.Deps {
	t.Helper()
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"},
		ModelFile: "qwen3.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return status.Deps{
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"}, nil
		},
		ModelFile:  func(config.VillaConfig) (string, error) { return "qwen3.gguf", nil },
		ModelsDir:  func() string { return "/home/villa/.local/share/villa/models" },
		Render:     func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil },
		IsActive:   func(string) (string, error) { return "active", nil },
		Health:     func(string) status.HealthState { return status.HealthReady },
		OWUIHealth: func(string) status.HealthState { return status.HealthReady },
		JournalText: func(string) (string, bool) {
			return "load_tensors:      Vulkan0 model buffer size = 21504.49 MiB\n", true
		},
		Props: func(string) *inference.PropsInfo {
			return &inference.PropsInfo{ModelPath: "/models/qwen3.gguf", NCtx: 131072}
		},
		GTTUsed:     func() detect.Bytes { return detect.UnknownBytes("stub", "") },
		WeightBytes: func(config.VillaConfig) uint64 { return 0 },
		Endpoint:    func() string { return "http://127.0.0.1:8080" },
		OWUIService: "villa-openwebui.service",
	}
}

// TestHandleStatusFoldsSharedCore asserts GET /api/status returns 200 with a body
// byte-equal to json.Marshal(status.Run(stubbedDeps)) — the SAME frozen Report the
// CLI serializes (DASH-01, not a forked serialization).
func TestHandleStatusFoldsSharedCore(t *testing.T) {
	deps := stubStatusDeps(t)
	srv := mustNewServer(t, Config{StatusDeps: deps, ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	want, err := json.Marshal(status.Run(deps))
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	// The handler may pretty-print; compare semantically by decoding both.
	var got, exp status.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if err := json.Unmarshal(want, &exp); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	gotB, _ := json.Marshal(got)
	expB, _ := json.Marshal(exp)
	if string(gotB) != string(expB) {
		t.Fatalf("status body does not match shared core\n got=%s\nwant=%s", gotB, expB)
	}
}

// TestHandleStatusCarriesBackendIdentity asserts GET /api/status carries the Phase-10
// backend-identity fields (backend, image, rocm_readiness) that handleStatus serves
// VERBATIM from the shared status.Report — they ride for free the moment Plan 10-01 lands
// them on Report (DASH-06 / D-01: backend identity lives on /api/status, not /api/metrics).
// The dashboard JS composes report.backend (label) with the /api/metrics tok/s (number).
func TestHandleStatusCarriesBackendIdentity(t *testing.T) {
	deps := stubStatusDeps(t)
	srv := mustNewServer(t, Config{StatusDeps: deps, ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The new fields must be present as raw JSON keys (the JS reads report.backend /
	// report.image / report.rocm_readiness by these exact names).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	for _, key := range []string{"backend", "image", "rocm_readiness"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("/api/status body missing %q key (D-01 backend identity); body=%s", key, rec.Body.String())
		}
	}

	// And they must equal the shared core's values (handleStatus serves Report verbatim).
	want := status.Run(deps)
	var got status.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode Report: %v", err)
	}
	if got.Backend != want.Backend {
		t.Fatalf("backend = %q, want %q (resolved backend, not a literal)", got.Backend, want.Backend)
	}
	if got.Backend == "" {
		t.Fatalf("backend should be the resolved name (vulkan stub), got empty")
	}
	if got.Image != want.Image {
		t.Fatalf("image = %q, want %q", got.Image, want.Image)
	}
	if got.ROCmReadiness != want.ROCmReadiness {
		t.Fatalf("rocm_readiness = %q, want %q", got.ROCmReadiness, want.ROCmReadiness)
	}
	// With no ROCmReadiness seam wired, the honest off-hardware default is "unknown"
	// (no-false-green, D-04) — the JS renders the gray "ROCm readiness unknown" badge.
	if got.ROCmReadiness != status.ROCmUnknown {
		t.Fatalf("rocm_readiness with no seam = %q, want %q (no-false-green default)", got.ROCmReadiness, status.ROCmUnknown)
	}
}

// stubMemoryStatusDeps builds the memory-ON stub set: stubStatusDeps re-based on a
// MemoryEnabled config whose render output actually contains the villa-qdrant /
// villa-embed .container units (Pitfall 8 coherence — mirrors the Plan 23-01
// memory-on fixture in internal/status/status_test.go), plus healthy per-service
// memory seams and a complete-run recall state.
func stubMemoryStatusDeps(t *testing.T) status.Deps {
	t.Helper()
	cfg := config.DefaultVillaConfig()
	cfg.Model = "qwen3"
	cfg.Quant = "Q4"
	cfg.Ctx = 131072
	cfg.MemoryEnabled = true
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       cfg,
		ModelFile: "qwen3.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	})
	if err != nil {
		t.Fatalf("render memory-on: %v", err)
	}
	d := stubStatusDeps(t)
	d.LoadConfig = func() (config.VillaConfig, error) { return cfg, nil }
	d.Render = func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil }
	d.QdrantService = "villa-qdrant.service"
	d.EmbedService = "villa-embed.service"
	d.QdrantHealth = func(string, int) status.HealthState { return status.HealthReady }
	d.EmbedHealth = func(string, int) status.HealthState { return status.HealthReady }
	d.ReadRecallState = func() *recall.State {
		return &recall.State{
			EmbeddingModel:       "nomic-embed-text-v1.5",
			EmbeddingDim:         768,
			LastIndexStartedAt:   "2026-06-09T10:00:00Z",
			LastIndexCompletedAt: "2026-06-09T10:05:00Z",
			Chats: map[string]recall.ChatState{
				"chat-1": {}, "chat-2": {},
			},
		}
	}
	return d
}

// TestHandleStatusMemoryPassthrough (CTRL-02 / D-03) asserts handleStatus passes the
// v3 memory field through UNTOUCHED from the shared status core — the dashboard JS
// reads report.memory off the existing /api/status poll, no new endpoint or fork:
//   - memory-ON deps → the body carries a "memory" object with the exact JSON keys
//     renderMemory reads (embedding_model / embedding_dim / recall_state);
//   - default memory-OFF deps → the body carries NO "memory" key at all
//     (*MemoryInfo omitempty-nil — the panel stays hidden, pixel-identical to v1.2)
//     while schema_version reads 3 (the v3 contract from Plan 23-01).
func TestHandleStatusMemoryPassthrough(t *testing.T) {
	t.Run("memory-on serves the memory object", func(t *testing.T) {
		srv := mustNewServer(t, Config{StatusDeps: stubMemoryStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
		}
		memBlob, ok := raw["memory"]
		if !ok {
			t.Fatalf("/api/status memory-on body missing \"memory\" key (CTRL-02 passthrough); body=%s", rec.Body.String())
		}
		var mem map[string]json.RawMessage
		if err := json.Unmarshal(memBlob, &mem); err != nil {
			t.Fatalf("decode memory object: %v\n%s", err, string(memBlob))
		}
		// The exact keys renderMemory reads off report.memory (the v3 contract).
		for _, key := range []string{"embedding_model", "embedding_dim", "recall_state"} {
			if _, present := mem[key]; !present {
				t.Errorf("memory object missing %q key; memory=%s", key, string(memBlob))
			}
		}
	})

	t.Run("memory-off omits the memory key", func(t *testing.T) {
		srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
		}
		if _, present := raw["memory"]; present {
			t.Fatalf("memory-off /api/status body must OMIT the \"memory\" key (omitempty-nil, D-04); body=%s", rec.Body.String())
		}
		var schemaVersion int
		if err := json.Unmarshal(raw["schema_version"], &schemaVersion); err != nil {
			t.Fatalf("decode schema_version: %v", err)
		}
		if schemaVersion != 3 {
			t.Fatalf("schema_version = %d, want 3 (the Plan 23-01 v3 contract)", schemaVersion)
		}
	})
}

// TestHandleMetricsShapeUnchanged asserts GET /api/metrics serializes ONLY the
// metricsView read-model and carries NO backend-identity key (D-01: identity lives on
// /api/status, the number on /api/metrics; the UI composes them). This pins the metrics
// surface so Phase 10 cannot leak backend identity into it. With no live scrape the view
// is the typed-Unknown {"available":false,...} shape, which still must expose exactly the
// metricsView keys and never "backend"/"image"/"rocm_readiness".
func TestHandleMetricsShapeUnchanged(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}

	// No backend identity may appear on the metrics surface (D-01 / T-10-10).
	for _, forbidden := range []string{"backend", "image", "rocm_readiness", "schema_version"} {
		if _, present := raw[forbidden]; present {
			t.Fatalf("/api/metrics must NOT carry %q (identity belongs on /api/status, D-01); body=%s", forbidden, rec.Body.String())
		}
	}

	// The metricsView shape is the frozen DASH-02 contract — the keys the dashboard JS
	// renderPerformance reads. Assert the present keys are a subset of this set (latency_ms
	// is omitempty, so it may be absent in the unavailable view).
	allowed := map[string]bool{
		"gen_tokens_per_sec":    true,
		"prompt_tokens_per_sec": true,
		"latency_ms":            true,
		"active_slots":          true,
		"slots_known":           true,
		"idle":                  true,
		"activity_known":        true,
		"available":             true,
	}
	for key := range raw {
		if !allowed[key] {
			t.Fatalf("/api/metrics carries unexpected key %q — metricsView shape changed; body=%s", key, rec.Body.String())
		}
	}
	// The unavailable-scrape view must still decode into metricsView with available=false
	// (no fabricated zeros surfaced as real, D-11) — proving the shape is intact.
	var view metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("body does not decode into metricsView (shape changed): %v\n%s", err, rec.Body.String())
	}
	if view.Available {
		t.Fatalf("with no live scrape, metricsView.Available should be false, got true; body=%s", rec.Body.String())
	}
}

// TestHandleModelsListsCatalogWithFit asserts GET /api/models returns the full catalog
// with each entry marked loaded/on-disk/catalog-only and a fits flag from the SHARED
// fit seam — a fitting+loaded entry, a non-fitting entry (fits=false so the UI can
// disable Switch, D-08), serialized via the injected Models seam (not re-implemented).
func TestHandleModelsListsCatalogWithFit(t *testing.T) {
	want := []ModelView{
		{ID: "qwen3", Quant: "Q4", Loaded: true, OnDisk: true, Fits: true, FitDetail: "Fits: 21.0 GiB ≤ 120.0 GiB — 99.0 GiB headroom at 131072 context."},
		{ID: "deepseek-70b", Quant: "Q4", Loaded: false, OnDisk: false, Fits: false, FitDetail: "needs 80.0 GiB vs 60.0 GiB usable"},
	}
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		Models:        func() ([]ModelView, bool) { return want, true },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("models code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var got []ModelView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d: %s", len(got), len(want), rec.Body.String())
	}
	if !got[0].Loaded || !got[0].OnDisk || !got[0].Fits {
		t.Fatalf("entry[0] should be loaded+on-disk+fits, got %+v", got[0])
	}
	if got[1].Fits {
		t.Fatalf("entry[1] should be non-fitting (fits=false) so UI disables Switch, got %+v", got[1])
	}
	if got[1].FitDetail == "" {
		t.Fatalf("non-fitting entry should carry a fit-detail reason, got %+v", got[1])
	}
}

// TestHandleModelsEmptyCatalog asserts an empty catalog → an empty JSON list (the UI
// renders the "No models in catalog" empty state), never a null/500.
func TestHandleModelsEmptyCatalog(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		Models:        func() ([]ModelView, bool) { return []ModelView{}, true },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("models code = %d, want 200", rec.Code)
	}
	var got []ModelView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 0 {
		t.Fatalf("empty catalog should yield empty list, got %d", len(got))
	}
}

// stubSwapDeps builds a modelswap.Deps whose every host-touching action is a stub, so
// the switch handler exercises modelswap.Run end-to-end without a live host. `known` is
// the catalog id that resolves; `fits` controls the fit-guard; the side-effect funcs
// record that they were called so a refusal can assert NO side effect.
func stubSwapDeps(known string, fits bool, called *[]string) modelswap.Deps {
	rec := func(step string) { *called = append(*called, step) }
	return modelswap.Deps{
		InstallServiceName: "villa-llama.service",
		ResolveCatalog: func(name string) (catalog.CatalogModel, bool) {
			if name == known {
				return catalog.CatalogModel{ID: name, Quant: "Q4"}, true
			}
			return catalog.CatalogModel{}, false
		},
		Fits: func(catalog.CatalogModel) (bool, string) {
			if fits {
				return true, ""
			}
			return false, "needs 80.0 GiB vs 60.0 GiB usable"
		},
		IsDownloaded: func(catalog.CatalogModel) bool { return true },
		Pull:         func(catalog.CatalogModel) error { rec("pull"); return nil },
		LoadConfig:   func() (config.VillaConfig, error) { return config.VillaConfig{Model: "old"}, nil },
		SaveConfig:   func(config.VillaConfig) error { rec("save"); return nil },
		ReconcileAndWrite: func(config.VillaConfig) (bool, error) {
			rec("reconcile")
			return true, nil
		},
		Restart: func(string) error { rec("restart"); return nil },
	}
}

// jsonSwitchReq builds a same-origin JSON POST to /api/models/switch (the headers the
// requireSameOrigin guard requires) for the given model id.
func jsonSwitchReq(model string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/models/switch",
		strings.NewReader(`{"model":`+strconv.Quote(model)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

// TestHandleSwitchFittingRoutesThroughModelswap asserts a same-origin JSON POST for a
// fit-passing model calls modelswap.Run (the SHARED guarded path) and returns the typed
// Result as switched=true — the handler adds zero swap logic, it only decodes + folds the
// core + serializes.
func TestHandleSwitchFittingRoutesThroughModelswap(t *testing.T) {
	var called []string
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      stubSwapDeps("qwen3", true, &called),
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, jsonSwitchReq("qwen3"))

	if rec.Code != http.StatusOK {
		t.Fatalf("switch code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Switched bool   `json:"switched"`
		To       string `json:"to"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if !resp.Switched {
		t.Fatalf("expected switched=true, got %s", rec.Body.String())
	}
	if resp.To != "qwen3" {
		t.Fatalf("expected to=qwen3, got %q", resp.To)
	}
	// The guarded core ran its side effects (save → reconcile → restart).
	joined := strings.Join(called, ",")
	if !strings.Contains(joined, "save") || !strings.Contains(joined, "restart") {
		t.Fatalf("modelswap.Run side effects not observed: %v", called)
	}
}

// TestHandleSwitchConcurrentRefusedWith409 asserts that while one swap is in flight, a
// second concurrent POST is refused with 409 Conflict (CR-02) rather than allowed to
// interleave the non-atomic modelswap.Run read-modify-write. The first request's Restart
// blocks on a gate so the second request provably races it while the swap mutex is held.
func TestHandleSwitchConcurrentRefusedWith409(t *testing.T) {
	var called []string
	var mu sync.Mutex
	rec := func(step string) { mu.Lock(); called = append(called, step); mu.Unlock() }

	inFlight := make(chan struct{}) // closed once the first swap is provably inside Restart
	release := make(chan struct{})  // closed by the test to let the first swap finish

	deps := modelswap.Deps{
		InstallServiceName: "villa-llama.service",
		ResolveCatalog: func(name string) (catalog.CatalogModel, bool) {
			return catalog.CatalogModel{ID: name, Quant: "Q4"}, true
		},
		Fits:         func(catalog.CatalogModel) (bool, string) { return true, "" },
		IsDownloaded: func(catalog.CatalogModel) bool { return true },
		Pull:         func(catalog.CatalogModel) error { return nil },
		LoadConfig:   func() (config.VillaConfig, error) { return config.VillaConfig{Model: "old"}, nil },
		SaveConfig:   func(config.VillaConfig) error { rec("save"); return nil },
		ReconcileAndWrite: func(config.VillaConfig) (bool, error) {
			rec("reconcile")
			return true, nil
		},
		Restart: func(string) error {
			rec("restart")
			close(inFlight) // signal the first swap is holding the swap mutex
			<-release       // block here until the test releases it
			return nil
		},
	}

	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      deps,
	})

	// Fire the first switch in a goroutine; it will block inside Restart holding the lock.
	firstDone := make(chan int, 1)
	go func() {
		rec1 := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec1, jsonSwitchReq("qwen3"))
		firstDone <- rec1.Code
	}()

	// Wait until the first swap is provably in flight (mutex held).
	select {
	case <-inFlight:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("first swap never reached Restart")
	}

	// Second concurrent switch must be refused 409 while the first holds the lock.
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, jsonSwitchReq("qwen3"))
	if rec2.Code != http.StatusConflict {
		close(release)
		t.Fatalf("concurrent switch should be 409 Conflict, got %d; body=%s", rec2.Code, rec2.Body.String())
	}
	var resp switchResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		close(release)
		t.Fatalf("decode 409 body: %v\n%s", err, rec2.Body.String())
	}
	if !resp.Refused || resp.Reason != "a model switch is already in progress" {
		close(release)
		t.Fatalf("409 body should be a refusal with the in-progress reason, got %+v", resp)
	}

	// Let the first swap complete and assert it succeeded.
	close(release)
	if code := <-firstDone; code != http.StatusOK {
		t.Fatalf("first swap should have succeeded with 200, got %d", code)
	}
}

// TestHandleSwitchUnknownRefusesNoSideEffect asserts a POST for an unknown id is refused
// by modelswap.Run's resolve-through-catalog guard (Security V5) BEFORE any side effect,
// and the handler returns a 4xx refusal with no save/restart fired.
func TestHandleSwitchUnknownRefusesNoSideEffect(t *testing.T) {
	var called []string
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      stubSwapDeps("qwen3", true, &called),
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, jsonSwitchReq("../etc/passwd"))

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("unknown id should be a 4xx refusal, got %d; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Refused  bool `json:"refused"`
		Switched bool `json:"switched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if !resp.Refused || resp.Switched {
		t.Fatalf("expected refused=true switched=false, got %s", rec.Body.String())
	}
	if len(called) != 0 {
		t.Fatalf("unknown id must fire NO side effect, got %v", called)
	}
}

// TestHandleSwitchNonFittingRefuses asserts a POST for a non-fitting model is refused by
// the fit-guard (D-08) with no side effect — the dashboard never fires a swap the core
// would reject.
func TestHandleSwitchNonFittingRefuses(t *testing.T) {
	var called []string
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      stubSwapDeps("toobig", false, &called),
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, jsonSwitchReq("toobig"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-fitting should be 422, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if len(called) != 0 {
		t.Fatalf("non-fitting refusal must fire NO side effect, got %v", called)
	}
}

// TestHandleSwitchCrossOriginBlocked asserts a cross-origin POST is rejected 403 by the
// middleware and NEVER reaches modelswap.Run (no side effect) — T-05-11 CSRF guard.
func TestHandleSwitchCrossOriginBlocked(t *testing.T) {
	var called []string
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      stubSwapDeps("qwen3", true, &called),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/models/switch", strings.NewReader(`{"model":"qwen3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST should be 403, got %d", rec.Code)
	}
	if len(called) != 0 {
		t.Fatalf("cross-origin POST must never reach modelswap.Run, got %v", called)
	}
}

// TestHandleSwitchGetRejected asserts a GET to /api/models/switch is method-not-allowed
// (the route is POST-only).
func TestHandleSwitchGetRejected(t *testing.T) {
	var called []string
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		SwapDeps:      stubSwapDeps("qwen3", true, &called),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/models/switch", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/models/switch should be 405, got %d", rec.Code)
	}
	if len(called) != 0 {
		t.Fatalf("GET must not invoke swap, got %v", called)
	}
}

// TestHandleHealthz asserts GET /healthz returns 200 with a tiny ok JSON (the D-04
// self-reachability signal Plan 05's status row probes).
func TestHandleHealthz(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz code = %d, want 200", rec.Code)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode healthz: %v\n%s", err, rec.Body.String())
	}
	if !body.OK {
		t.Fatalf("healthz ok = false, want true")
	}
}

// usageProbe records the folded store WriteUsage was last called with, plus a count, so a
// test can assert the SOLE-WRITER fold ran with the expected per-model totals. ReadUsage
// returns the last-written store so successive scrapes accumulate end-to-end through the
// handler (reset-aware continuation is proven across requests, not just in the pure core).
type usageProbe struct {
	mu      sync.Mutex
	store   usage.UsageTotals
	written int
}

func (p *usageProbe) read() usage.UsageTotals {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store
}

func (p *usageProbe) write(t usage.UsageTotals) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store = t
	p.written++
	return nil
}

// getMetrics drives one GET /api/metrics request against the server.
func getMetrics(t *testing.T, srv *Server) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/metrics code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestMetricsWritesUsage asserts the dashboard /api/metrics handler is the SOLE,
// usageMu-guarded writer of the usage store (USAGE-02 / D-07): each scrape folds the two
// monotonic _total counters keyed by the in-section ModelID and atomically writes. It
// proves, end-to-end through the handler:
//   - the fold runs with the stub counters keyed by "m1" (sole-writer);
//   - a SECOND scrape whose raw counter went DOWN (server restart reset) continues the
//     cumulative total reset-aware rather than dropping to the new low raw count (D-04
//     through the handler);
//   - a typed-Unknown counter (Known=false) contributes NO fold (no fabricated 0, D-05);
//   - the live metricsView JSON is byte-identical to the pre-change unavailable shape
//     (the fold is additive — it adds NO field to the live response, D-10).
func TestMetricsWritesUsage(t *testing.T) {
	probe := &usageProbe{}
	// counter is mutated between requests to simulate a counter reset on the 2nd scrape.
	counter := metrics.CounterSample{
		PromptTokensTotal: 100, PromptTokensKnown: true,
		PredictedTokensTotal: 40, PredictedTokensKnown: true,
	}
	counterOK := true

	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		// scrapeMetrics left nil → typed-Unknown unavailable live view (so we also assert the
		// live response is byte-identical to the pre-change unavailable shape below).
		ReadUsage:     probe.read,
		WriteUsage:    probe.write,
		ModelID:       func() string { return "m1" },
		CounterSample: func() (metrics.CounterSample, bool) { return counter, counterOK },
	})

	// --- Request 1: first fold from raw 100/40 ---------------------------------
	getMetrics(t, srv)

	if probe.written != 1 {
		t.Fatalf("after 1 scrape, WriteUsage calls = %d, want 1 (sole writer ran)", probe.written)
	}
	got := probe.read()
	mu, ok := got.Models["m1"]
	if !ok {
		t.Fatalf("store not keyed by in-section ModelID \"m1\"; store=%+v", got)
	}
	if mu.Prompt.Cumulative != 100 || mu.Predicted.Cumulative != 40 {
		t.Fatalf("after scrape 1, cumulative = prompt %d / generated %d, want 100 / 40",
			mu.Prompt.Cumulative, mu.Predicted.Cumulative)
	}

	// --- Request 2: counter RESET (raw drops to 30/10) → reset-aware continuation ---
	counter = metrics.CounterSample{
		PromptTokensTotal: 30, PromptTokensKnown: true,
		PredictedTokensTotal: 10, PredictedTokensKnown: true,
	}
	getMetrics(t, srv)

	if probe.written != 2 {
		t.Fatalf("after 2 scrapes, WriteUsage calls = %d, want 2", probe.written)
	}
	got = probe.read()
	mu = got.Models["m1"]
	// D-04 reset-aware THROUGH the handler: a backward step counts the whole new sample
	// (100+30, 40+10), never a negative delta and never a drop to the low raw count.
	if mu.Prompt.Cumulative != 130 || mu.Predicted.Cumulative != 50 {
		t.Fatalf("after reset scrape, cumulative = prompt %d / generated %d, want 130 / 50 (reset-aware continuation)",
			mu.Prompt.Cumulative, mu.Predicted.Cumulative)
	}

	// --- Request 3: prompt counter typed-Unknown (Known=false) → NOT folded ---------
	counter = metrics.CounterSample{
		PromptTokensTotal: 0, PromptTokensKnown: false, // absent → no fold for prompt (D-05)
		PredictedTokensTotal: 60, PredictedTokensKnown: true,
	}
	getMetrics(t, srv)

	got = probe.read()
	mu = got.Models["m1"]
	// Prompt is unchanged (no fabricated 0, no LastSeenRaw mutation); generated continues
	// reset-aware from last_seen_raw=10 → +60 raw is monotonic growth of (60-10)=50.
	if mu.Prompt.Cumulative != 130 {
		t.Fatalf("typed-Unknown prompt counter must NOT fold; prompt cumulative = %d, want 130 (unchanged)", mu.Prompt.Cumulative)
	}
	if mu.Predicted.Cumulative != 100 {
		t.Fatalf("generated should continue from raw 10→60 (+50) = 100, got %d", mu.Predicted.Cumulative)
	}

	// --- Live metricsView unchanged: the fold adds NO field to the live response (D-10) ---
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	// With scrapeMetrics nil-defaulted unavailable, the live view is the frozen
	// {"available":false,...} metricsView shape — byte-identical to the pre-change handler.
	want, err := json.Marshal(metricsView{Available: false})
	if err != nil {
		t.Fatalf("marshal want view: %v", err)
	}
	if strings.TrimSpace(rec.Body.String()) != string(want) {
		t.Fatalf("live metricsView changed by the usage fold:\n got=%s\nwant=%s", rec.Body.String(), want)
	}

	// --- Counter scrape entirely unavailable → NO fold, NO write (D-05) -------------
	priorWrites := probe.written
	counterOK = false
	getMetrics(t, srv)
	if probe.written != priorWrites {
		t.Fatalf("an unavailable counter scrape must NOT write usage (typed-Unknown); writes = %d, want %d", probe.written, priorWrites)
	}
}

// TestStatusUsageSurfaced asserts the dashboard surfaces cumulative totals through the
// SAME status.Report.usage field (Plan 03) over the existing /api/status handler — NO new
// endpoint (D-10). A populated ReadUsage seam on status.Deps yields a /api/status body
// carrying the "usage" key; a nil-returning seam omits it (typed-Unknown, never a
// fabricated 0).
func TestStatusUsageSurfaced(t *testing.T) {
	// --- Present: a populated store surfaces the usage key on /api/status -----------
	deps := stubStatusDeps(t)
	deps.ReadUsage = func() *usage.UsageTotals {
		return &usage.UsageTotals{
			SchemaVersion: 1,
			Models: map[string]usage.ModelUsage{
				"qwen3": {
					Model:     "qwen3",
					Prompt:    usage.CounterState{Cumulative: 1284907},
					Predicted: usage.CounterState{Cumulative: 55012},
				},
			},
		}
	}
	srv := mustNewServer(t, Config{StatusDeps: deps, ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/status code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if _, ok := raw["usage"]; !ok {
		t.Fatalf("/api/status must surface the SAME status.Report usage field (D-10, no new endpoint); body=%s", rec.Body.String())
	}

	// --- Absent: a nil-returning seam omits the usage key (typed-Unknown) -----------
	depsNil := stubStatusDeps(t)
	depsNil.ReadUsage = func() *usage.UsageTotals { return nil }
	srvNil := mustNewServer(t, Config{StatusDeps: depsNil, ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	reqNil := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	recNil := httptest.NewRecorder()
	srvNil.Handler().ServeHTTP(recNil, reqNil)

	var rawNil map[string]json.RawMessage
	if err := json.Unmarshal(recNil.Body.Bytes(), &rawNil); err != nil {
		t.Fatalf("decode body: %v\n%s", err, recNil.Body.String())
	}
	if _, ok := rawNil["usage"]; ok {
		t.Fatalf("an absent store must OMIT the usage key (omitempty, no fabricated 0); body=%s", recNil.Body.String())
	}
}
