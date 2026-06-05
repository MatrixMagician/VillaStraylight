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
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
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
