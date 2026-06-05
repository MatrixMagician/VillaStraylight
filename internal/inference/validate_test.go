package inference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// fakeRunner is a Runner test double for the validate orchestrator. It replays a
// scripted stderr + health and points the chat probe at a local httptest endpoint.
// It records Start/Stop so the test asserts lifecycle.
type fakeRunner struct {
	stderr    string
	health    detect.Bool
	endpoint  string
	startErr  error
	started   bool
	stopped   bool
	startSpec RunSpec // captured spec the orchestrator started us with
}

func (f *fakeRunner) Start(spec RunSpec) error { f.started = true; f.startSpec = spec; return f.startErr }
func (f *fakeRunner) Stop() error              { f.stopped = true; return nil }
func (f *fakeRunner) Health() detect.Bool      { return f.health }
func (f *fakeRunner) Endpoint() string         { return f.endpoint }
func (f *fakeRunner) Logs() (string, bool)     { return f.stderr, f.stderr != "" }

// healthyChatServer is an httptest server emulating a ready llama-server: /health
// returns 200 and /v1/chat/completions streams tokens.
func healthyChatServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/chat/completions":
			sseChatHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testModel is a small fixture catalog model (0.5B-ish) for the orchestrator math.
func testModel() catalog.CatalogModel {
	return catalog.CatalogModel{
		ID:             "qwen2.5-0.5b",
		WeightBytes:    491400032,
		NLayers:        24,
		NKVHeads:       2,
		HeadDim:        64,
		KVBytesPerElem: 2,
		DefaultCtx:     4096,
	}
}

// gttSeam builds a GTT-used reader that returns `before` on the first call and
// `after` on every subsequent call, so the orchestrator sees a delta across Start.
func gttSeam(before, after detect.Bytes) func() detect.Bytes {
	calls := 0
	return func() detect.Bytes {
		calls++
		if calls == 1 {
			return before
		}
		return after
	}
}

// baseInput assembles a ValidateInput wired to a healthy chat server and a passing
// ceiling Runner, leaving the offload signals (stderr + GTT delta) for each test to
// set via the fakeRunner + gttSeam.
func baseInput(t *testing.T, fr *fakeRunner) ValidateInput {
	srv := healthyChatServer(t)
	fr.endpoint = srv.URL
	if !fr.health.Known {
		fr.health = detect.KnownBool(true, "/health")
	}
	return ValidateInput{
		Model:         testModel(),
		ContextLen:    4096,
		WeightBytes:   491400032,
		KVCacheBytes:  100 << 20,
		HeadroomBytes: 7 << 30,
		EnvelopeBytes: 62 << 30,
		Runner:        fr,
		NewCeilingRunner: func(stress RunSpec) Runner {
			return &fakeProbeRunner{health: detect.KnownBool(true, "/health")}
		},
		ReadGTTUsed:  gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after")),
		ReadyTimeout: 200 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	}
}

// TestValidatePass: radv_pass stderr + a positive sysfs delta + a healthy chat + a
// clearing ceiling → Verdict PASS.
func TestValidatePass(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "radv_pass.stderr")}
	in := baseInput(t, fr)
	// A delta well above 0.5×weight (491400032) → sysfs PASS.
	in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after"))

	v := Validate(context.Background(), in)
	if v.Status != StatusPass {
		t.Fatalf("Validate: Status=%v, want PASS (detail=%q)", v.Status, v.Detail)
	}
	if !fr.started || !fr.stopped {
		t.Errorf("Validate must Start and Stop the runner (started=%v stopped=%v)", fr.started, fr.stopped)
	}
	if v.GTTDeltaBytes == 0 {
		t.Errorf("Validate: GTTDeltaBytes=0, want the observed delta recorded for A1 calibration")
	}
}

// TestValidatePassesModelsDirToRunner guards the production wiring gap that the
// fixture suite previously missed and on-hardware validation caught: ValidateInput
// must propagate ModelsDir into the RunSpec the Runner is started with. Without it,
// the backend renders an invalid `-v :/models:ro,z` bind (empty host source), podman
// exits immediately, and readiness times out into a misleading WARN. A fake Runner
// renders no ContainerArgs, so only an explicit passthrough assertion catches this.
func TestValidatePassesModelsDirToRunner(t *testing.T) {
	const modelsDir = "/home/u/.local/share/villa/models"
	fr := &fakeRunner{stderr: readFixture(t, "radv_pass.stderr")}
	in := baseInput(t, fr)
	in.ModelsDir = modelsDir

	_ = Validate(context.Background(), in)

	if !fr.started {
		t.Fatal("Validate must Start the runner")
	}
	if fr.startSpec.ModelsDir != modelsDir {
		t.Errorf("Runner started with ModelsDir=%q, want %q — ValidateInput.ModelsDir must reach the RunSpec (else the model bind is empty and the container never becomes ready)", fr.startSpec.ModelsDir, modelsDir)
	}
}

// TestValidatePassNewLogFormat reproduces the live on-hardware case: the auto-fit
// llama.cpp build emits only a "device_info: - Vulkan0 : … (RADV GFX1151)" line and
// NO "offloaded N/N" line, yet the iGPU offload genuinely happened (a positive GTT
// delta). The dual assert must now PASS (log signal recognizes the real RADV device,
// sysfs proves residency) rather than degrading to WARN on the missing legacy line.
func TestValidatePassNewLogFormat(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "radv_devinfo_pass.stderr")}
	in := baseInput(t, fr)
	in.ModelsDir = "/models-host"
	// A delta ~2× weight (observed live: 983621632 vs 491400032) → sysfs PASS.
	in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+983621632, "after"))

	v := Validate(context.Background(), in)
	if v.Status != StatusPass {
		t.Fatalf("Validate (new device_info log fmt + real GTT delta): Status=%v, want PASS (detail=%q)", v.Status, v.Detail)
	}
	if !v.LogOffload.Known || !v.LogOffload.Value {
		t.Errorf("log offload signal: %+v, want Known/true from the device_info Vulkan device line", v.LogOffload)
	}
}

// TestValidateStopsPrimaryBeforeCeiling guards CR-01: the ceiling probe runs a
// second container that binds the SAME loopback port as the primary, so the primary
// MUST be torn down before the ceiling runner is created — otherwise the ceiling's
// readiness poll hits the still-live primary and false-clears (the bug on-hardware
// reported as "ceiling cleared at ctx 4768884").
func TestValidateStopsPrimaryBeforeCeiling(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "radv_devinfo_pass.stderr")}
	in := baseInput(t, fr)
	in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+983621632, "after"))

	ceilingRan := false
	in.NewCeilingRunner = func(stress RunSpec) Runner {
		if !fr.stopped {
			t.Errorf("ceiling runner created while the primary is still running — they share the loopback port (CR-01)")
		}
		ceilingRan = true
		return &fakeProbeRunner{health: detect.KnownBool(true, "/health")}
	}

	v := Validate(context.Background(), in)
	if !ceilingRan {
		t.Fatal("ceiling probe never ran")
	}
	if v.Status != StatusPass {
		t.Fatalf("Validate: Status=%v, want PASS (detail=%q)", v.Status, v.Detail)
	}
}

// TestValidateCPUFallbackFails: llvmpipe stderr (CPU fallback) with a 200/healthy
// server → FAIL (D-11: responds-but-on-CPU is a FAIL, not PASS).
func TestValidateCPUFallbackFails(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "llvmpipe_fail.stderr")}
	in := baseInput(t, fr)

	v := Validate(context.Background(), in)
	if v.Status != StatusFail {
		t.Fatalf("Validate (CPU fallback, healthy server): Status=%v, want FAIL (D-11)", v.Status)
	}
}

// TestValidateUnknownOffloadWarns: an unreadable offload signal (empty stderr →
// Unknown) with an otherwise-healthy run → WARN, distinct from FAIL.
func TestValidateUnknownOffloadWarns(t *testing.T) {
	fr := &fakeRunner{stderr: ""} // empty stderr → log scrape Unknown
	in := baseInput(t, fr)
	// Keep sysfs evaluable+passing so the WARN comes from the Unknown log signal.
	in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after"))

	v := Validate(context.Background(), in)
	if v.Status != StatusWarn {
		t.Fatalf("Validate (Unknown offload signal): Status=%v, want WARN", v.Status)
	}
	if v.Status == StatusFail {
		t.Errorf("Unknown signal must not read as FAIL")
	}
}

// TestValidateCeilingCliffWarns: offload passes + chat OK, but the ceiling probe
// reports an OOM cliff → WARN with the cliff reported (not a crash, D-10).
func TestValidateCeilingCliffWarns(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "radv_pass.stderr")}
	in := baseInput(t, fr)
	in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after"))
	// Ceiling Runner hits an OOM cliff (never ready + alloc-failure marker).
	in.NewCeilingRunner = func(stress RunSpec) Runner {
		return &fakeProbeRunner{
			health: detect.UnknownBool("never ready", ""),
			logs:   "ggml_vulkan: Device memory allocation of 99999999999 bytes failed",
		}
	}

	v := Validate(context.Background(), in)
	if v.Status != StatusWarn {
		t.Fatalf("Validate (ceiling cliff): Status=%v, want WARN (offload+chat fine, ceiling cliff is a finding)", v.Status)
	}
}

// TestValidateChatFailWarns: offload passes but the chat completion fails (server
// readiness ok yet no tokens) → the run is not a clean PASS. A confirmed-offload run
// whose chat could not return tokens degrades to WARN (uncertain liveness), never a
// silent PASS.
func TestValidateChatFailWarns(t *testing.T) {
	// Server that is healthy but errors the chat endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "no model", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	fr := &fakeRunner{stderr: readFixture(t, "radv_pass.stderr"), health: detect.KnownBool(true, "/health"), endpoint: srv.URL}
	in := ValidateInput{
		Model:            testModel(),
		ContextLen:       4096,
		WeightBytes:      491400032,
		KVCacheBytes:     100 << 20,
		HeadroomBytes:    7 << 30,
		EnvelopeBytes:    62 << 30,
		Runner:           fr,
		NewCeilingRunner: func(stress RunSpec) Runner { return &fakeProbeRunner{health: detect.KnownBool(true, "/health")} },
		ReadGTTUsed:      gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after")),
		ReadyTimeout:     200 * time.Millisecond,
		PollInterval:     10 * time.Millisecond,
	}

	v := Validate(context.Background(), in)
	if v.Status == StatusPass {
		t.Fatalf("Validate (offload ok but chat failed): Status=PASS, want non-PASS (a PASS requires real tokens, D-11)")
	}
}
