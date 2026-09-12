package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandlePinsFoldsInjectedSeam asserts GET /api/pins serializes whatever the
// injected Pins seam returns, unmodified — the handler adds no pin logic.
func TestHandlePinsFoldsInjectedSeam(t *testing.T) {
	want := PinsView{
		Serial: 2,
		Components: []PinRow{
			{Component: "backend-rocm-7.2.4", Subsystem: "inference", Vetted: "docker.io/x@sha256:aaa", Effective: "docker.io/x@sha256:aaa", FromStore: false, Diverged: false},
			{Component: "backend-vulkan-radv", Subsystem: "inference", Vetted: "docker.io/y@sha256:bbb", Effective: "docker.io/y@sha256:ccc", FromStore: true, Diverged: true},
		},
	}
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		Pins:          func() PinsView { return want },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/pins", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("pins code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got PinsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if got.Serial != want.Serial || len(got.Components) != len(want.Components) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.Components[1] != want.Components[1] {
		t.Fatalf("row[1] = %+v, want %+v", got.Components[1], want.Components[1])
	}
}

// TestHandlePinsNilSeamDefaultsToEmpty proves a Server built without Config.Pins
// renders the honest empty view (the panel shows unavailable) rather than
// nil-panicking.
func TestHandlePinsNilSeamDefaultsToEmpty(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/pins", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("pins code = %d, want 200", rec.Code)
	}
	var got PinsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if got.Serial != 0 || len(got.Components) != 0 {
		t.Fatalf("nil seam should yield an empty view, got %+v", got)
	}
}
