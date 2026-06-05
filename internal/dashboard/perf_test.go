package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
)

// metricsConfig builds a Config with the status seam stubbed plus the new metrics/GPU
// seams overridden per-test, so the Performance + GPU panels are driven deterministically
// without any live llama-server or sysfs.
func metricsConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	}
}

// TestHandleMetricsLiveGeneration asserts GET /api/metrics returns the gen/prompt
// tok/s + latency + active-slot fields from a generating snapshot, with available=true
// and idle=false.
func TestHandleMetricsLiveGeneration(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.Metrics = func() (metrics.PerfSnapshot, bool) {
		return metrics.PerfSnapshot{
			PromptTokensPerSec: 200,
			GenTokensPerSec:    40,
			RequestsProcessing: 1,
		}, true
	}
	cfg.Slots = func() ([]metrics.Slot, bool) {
		s := []metrics.Slot{{ID: 0, NCtx: 65536, IsProcessing: true}}
		s[0].NextToken.NDecoded = 128
		return s, true
	}
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var v metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if !v.Available {
		t.Errorf("Available=false on a 200 metrics scrape, want true")
	}
	if v.Idle {
		t.Errorf("Idle=true with a processing slot, want false")
	}
	if v.GenTokensPerSec != 40 || v.PromptTokensPerSec != 200 {
		t.Errorf("tok/s = gen %v / prompt %v, want 40 / 200", v.GenTokensPerSec, v.PromptTokensPerSec)
	}
	if v.ActiveSlots != 1 {
		t.Errorf("ActiveSlots=%d, want 1", v.ActiveSlots)
	}
	// Latency (A5) = prompt-eval ms/token = 1000/prompt_tokens_seconds = 5ms.
	if v.LatencyMS == nil || *v.LatencyMS != 5 {
		t.Errorf("LatencyMS=%v, want 5 (1000/200)", v.LatencyMS)
	}
}

// TestHandleMetricsIdle asserts an idle snapshot (no processing slot, requests_processing=0)
// sets Idle=true (the UI renders "Idle — no active generation.") and never presents the
// stale gauges as a live rate.
func TestHandleMetricsIdle(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.Metrics = func() (metrics.PerfSnapshot, bool) {
		return metrics.PerfSnapshot{PromptTokensPerSec: 200, GenTokensPerSec: 40, RequestsProcessing: 0}, true
	}
	cfg.Slots = func() ([]metrics.Slot, bool) {
		return []metrics.Slot{{ID: 0, NCtx: 65536, IsProcessing: false}}, true
	}
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))

	var v metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.Available {
		t.Errorf("Available=false, want true (scrape succeeded)")
	}
	if !v.Idle {
		t.Errorf("Idle=false on a no-processing snapshot, want true")
	}
}

// TestHandleMetricsSlotsFailedActivityUnknown is the WR-01 guard: when /metrics succeeds
// but /slots fails AND requests_processing==0, the view must NOT claim a confident "Idle".
// ActivityKnown is false (the UI renders "Activity unknown") and Idle stays false, because
// the snapshot cannot distinguish idle from generating-between-requests.
func TestHandleMetricsSlotsFailedActivityUnknown(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.Metrics = func() (metrics.PerfSnapshot, bool) {
		return metrics.PerfSnapshot{PromptTokensPerSec: 200, GenTokensPerSec: 40, RequestsProcessing: 0}, true
	}
	cfg.Slots = func() ([]metrics.Slot, bool) { return nil, false } // /slots scrape failed
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))

	var v metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.Available {
		t.Errorf("Available=false, want true (/metrics scrape succeeded)")
	}
	if v.SlotsKnown {
		t.Errorf("SlotsKnown=true on a failed /slots scrape, want false")
	}
	if v.ActivityKnown {
		t.Errorf("ActivityKnown=true with /slots unavailable and requests_processing==0, want false (WR-01)")
	}
	if v.Idle {
		t.Errorf("Idle=true asserted without slot corroboration — must degrade to Unknown, not a confident Idle (WR-01)")
	}
}

// TestHandleMetricsSlotsFailedButGeneratingKnown asserts that when /slots fails but
// requests_processing>0 definitively reports generation, ActivityKnown is true (the
// metric itself is authoritative) and Idle is false.
func TestHandleMetricsSlotsFailedButGeneratingKnown(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.Metrics = func() (metrics.PerfSnapshot, bool) {
		return metrics.PerfSnapshot{PromptTokensPerSec: 200, GenTokensPerSec: 40, RequestsProcessing: 1}, true
	}
	cfg.Slots = func() ([]metrics.Slot, bool) { return nil, false }
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))

	var v metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.ActivityKnown {
		t.Errorf("ActivityKnown=false while requests_processing>0, want true (the metric is authoritative)")
	}
	if v.Idle {
		t.Errorf("Idle=true while requests_processing>0, want false")
	}
}

// TestHandleMetricsUnavailable is the D-11 guard: a 404/transport-error collector marks
// the panel Available=false with NO fabricated zeros presented as a real rate.
func TestHandleMetricsUnavailable(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.Metrics = func() (metrics.PerfSnapshot, bool) { return metrics.PerfSnapshot{}, false }
	cfg.Slots = func() ([]metrics.Slot, bool) { return nil, false }
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, want 200 (panel-level unavailable, not an HTTP error)", rec.Code)
	}

	var v metricsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Available {
		t.Errorf("Available=true on an unavailable collector, want false")
	}
	if v.GenTokensPerSec != 0 || v.LatencyMS != nil {
		t.Errorf("unavailable view leaked a value: gen=%v latency=%v, want zero/nil with Available=false",
			v.GenTokensPerSec, v.LatencyMS)
	}
}

// TestHandleGPUMemoryFirst asserts GET /api/gpu returns the unified-memory used/envelope
// headline (from the injected memory readers) and a busy% that is typed-Unknown
// (Available=false) when the reader returns Unknown — never a fabricated number (D-06).
func TestHandleGPUMemoryFirst(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.MemUsed = func() detect.Bytes { return detect.KnownBytes(21<<30, "test") }
	cfg.MemEnvelope = func() detect.Bytes { return detect.KnownBytes(64<<30, "test") }
	cfg.GPUBusy = func() detect.Int { return detect.UnknownInt("gpu_busy_percent not found", "") }
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/gpu", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var v gpuView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if !v.MemUsedKnown || v.MemUsedBytes != uint64(21<<30) {
		t.Errorf("MemUsed = %d (known=%v), want 21GiB known", v.MemUsedBytes, v.MemUsedKnown)
	}
	if !v.MemEnvelopeKnown || v.MemEnvelopeBytes != uint64(64<<30) {
		t.Errorf("MemEnvelope = %d (known=%v), want 64GiB known", v.MemEnvelopeBytes, v.MemEnvelopeKnown)
	}
	if v.BusyAvailable {
		t.Errorf("BusyAvailable=true on an Unknown busy reader, want false (typed-Unknown)")
	}
}

// TestHandleGPUBusyKnown asserts a Known busy% surfaces as BusyAvailable=true + the value.
func TestHandleGPUBusyKnown(t *testing.T) {
	cfg := metricsConfig(t)
	cfg.MemUsed = func() detect.Bytes { return detect.KnownBytes(10<<30, "test") }
	cfg.MemEnvelope = func() detect.Bytes { return detect.KnownBytes(64<<30, "test") }
	cfg.GPUBusy = func() detect.Int { return detect.KnownInt(42, "test") }
	srv := mustNewServer(t, cfg)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/gpu", nil))

	var v gpuView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.BusyAvailable || v.BusyPercent != 42 {
		t.Errorf("busy = %d (available=%v), want 42 available", v.BusyPercent, v.BusyAvailable)
	}
}

// TestMetricsGPURoutesAreGET asserts the new routes live under the read-only /api block.
func TestMetricsGPURoutesExist(t *testing.T) {
	srv := mustNewServer(t, metricsConfig(t))
	for _, path := range []string{"/api/metrics", "/api/gpu"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("GET %s = 404, want a registered handler", path)
		}
	}
}
