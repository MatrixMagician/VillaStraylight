package inference

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// sseChatHandler emulates an OpenAI-compatible streaming /v1/chat/completions:
// two content deltas then [DONE].
func sseChatHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

// TestChatProbe: against an httptest server serving a streaming completion, the
// probe (via the reused internal/llm OpenAIClient) collects non-empty token deltas
// and reports success; a non-200 chat response is reported as a failure detail, not
// a panic.
func TestChatProbe(t *testing.T) {
	t.Run("streams tokens", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/chat/completions" {
				sseChatHandler(w, r)
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		res := chatProbe(context.Background(), srv.URL, "qwen2.5-0.5b")
		if !res.OK {
			t.Fatalf("chatProbe: OK=false, want true (detail=%q)", res.Detail)
		}
		if res.Tokens == 0 {
			t.Errorf("chatProbe: Tokens=0, want >0")
		}
		if res.Text == "" {
			t.Errorf("chatProbe: empty assembled text, want non-empty")
		}
	})

	t.Run("non-200 is a failure detail not a panic", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "model not loaded", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		res := chatProbe(context.Background(), srv.URL, "qwen2.5-0.5b")
		if res.OK {
			t.Errorf("chatProbe: OK=true on a non-200 chat, want false")
		}
		if res.Detail == "" {
			t.Errorf("chatProbe: empty Detail on failure, want a reported reason")
		}
	})
}

// TestPollHealth: /health returning 503 a few times then 200 is treated as ready
// (poll-until-200 with timeout); never-200-before-timeout is a readiness failure
// (Unknown), not a crash.
func TestPollHealth(t *testing.T) {
	t.Run("503 then 200 is ready", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				http.Error(w, `{"status":"loading model"}`, http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		ready := pollHealth(context.Background(), srv.Client(), srv.URL, 3*time.Second, 10*time.Millisecond)
		if !ready.Known || !ready.Value {
			t.Fatalf("pollHealth: ready=%+v, want Known+true after 503→200", ready)
		}
	})

	t.Run("never-200 before timeout is Unknown, not a crash", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"status":"loading model"}`, http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		ready := pollHealth(context.Background(), srv.Client(), srv.URL, 150*time.Millisecond, 10*time.Millisecond)
		if ready.Known {
			t.Errorf("pollHealth: Known=true on a never-ready server, want Unknown (could not evaluate readiness)")
		}
	})
}

// fakeProbeRunner is a Runner test double for the context-ceiling probe: it scripts
// Start, Health, Logs, and records whether Stop (teardown) was invoked.
type fakeProbeRunner struct {
	startErr error
	health   detect.Bool
	logs     string
	endpoint string
	stopped  int32
}

func (f *fakeProbeRunner) Start(spec RunSpec) error { return f.startErr }
func (f *fakeProbeRunner) Stop() error              { atomic.AddInt32(&f.stopped, 1); return nil }
func (f *fakeProbeRunner) Health() detect.Bool      { return f.health }
func (f *fakeProbeRunner) Endpoint() string         { return f.endpoint }
func (f *fakeProbeRunner) Logs() (string, bool)     { return f.logs, f.logs != "" }
func (f *fakeProbeRunner) didStop() bool            { return atomic.LoadInt32(&f.stopped) > 0 }

// TestContextProbe: a ceiling container that "fails to allocate" (stderr marker) or
// never reaches /health before timeout is CLASSIFIED as an OOM/hang finding (typed
// result), teardown is invoked, and no error/panic propagates.
func TestContextProbe(t *testing.T) {
	stressSpec := RunSpec{ContainerName: "villa-inf-ceiling", ModelFile: "m.gguf", ModelsDir: "/models", ContextLen: 131072}

	t.Run("allocation failure is an OOM cliff finding with teardown", func(t *testing.T) {
		fr := &fakeProbeRunner{
			health: detect.UnknownBool("never became ready", ""),
			logs:   "ggml_vulkan: Device memory allocation of 12884901888 bytes failed\nfailed to allocate buffer",
		}
		res := contextCeilingProbe(context.Background(), fr, stressSpec, 100*time.Millisecond, 10*time.Millisecond)
		if res.Status != StatusWarn {
			t.Errorf("ceiling OOM: Status=%v, want WARN (classified finding, not crash/PASS)", res.Status)
		}
		if !res.CliffFound {
			t.Errorf("ceiling OOM: CliffFound=false, want true")
		}
		if !fr.didStop() {
			t.Errorf("ceiling probe must tear down the stress container")
		}
	})

	t.Run("hang (never ready, no marker) is a finding with teardown", func(t *testing.T) {
		fr := &fakeProbeRunner{
			health: detect.UnknownBool("never became ready", ""),
			logs:   "loading model...\nstill loading",
		}
		res := contextCeilingProbe(context.Background(), fr, stressSpec, 100*time.Millisecond, 10*time.Millisecond)
		if res.Status != StatusWarn {
			t.Errorf("ceiling hang: Status=%v, want WARN", res.Status)
		}
		if !res.CliffFound {
			t.Errorf("ceiling hang: CliffFound=false, want true (hang is a reported cliff)")
		}
		if !fr.didStop() {
			t.Errorf("ceiling probe must tear down on hang")
		}
	})

	t.Run("clears the ceiling when healthy", func(t *testing.T) {
		fr := &fakeProbeRunner{health: detect.KnownBool(true, "/health")}
		res := contextCeilingProbe(context.Background(), fr, stressSpec, 100*time.Millisecond, 10*time.Millisecond)
		if res.Status != StatusPass {
			t.Errorf("ceiling clears: Status=%v, want PASS", res.Status)
		}
		if res.CliffFound {
			t.Errorf("ceiling clears: CliffFound=true, want false (it became ready)")
		}
		if !fr.didStop() {
			t.Errorf("ceiling probe must always tear down")
		}
	})

	t.Run("start failure is classified, not propagated as a crash", func(t *testing.T) {
		fr := &fakeProbeRunner{startErr: fmt.Errorf("podman: no such image")}
		res := contextCeilingProbe(context.Background(), fr, stressSpec, 100*time.Millisecond, 10*time.Millisecond)
		if res.Status != StatusWarn {
			t.Errorf("ceiling start-fail: Status=%v, want WARN (classified)", res.Status)
		}
		if !fr.didStop() {
			t.Errorf("ceiling probe must tear down even when Start failed")
		}
	})
}

// stressContextFor must derive a stress ctx near the envelope ceiling from the
// recommend-computed fit terms without exceeding the envelope's reach, and must be
// strictly greater than the recommend ctx (it pushes toward the ceiling).
func TestStressContextFor(t *testing.T) {
	// recommend ctx 4096, weight 491400032, kv@4096 ~ 100MiB, headroom ~ 7GiB,
	// envelope 62 GiB → there is headroom to push ctx well above 4096.
	const (
		recCtx     = 4096
		weight     = uint64(491400032)
		kvAtRecCtx = uint64(100 << 20)
		headroom   = uint64(7 << 30)
		envelope   = uint64(62 << 30)
	)
	// maxCtx 0 = uncapped, isolating the memory-bound math this case exercises.
	stress := stressContextFor(recCtx, weight, kvAtRecCtx, headroom, envelope, 0)
	if stress <= recCtx {
		t.Errorf("stressContextFor = %d, want > recommend ctx %d (must push toward the ceiling)", stress, recCtx)
	}
	// The stress ctx's modelled total must not blow past the envelope by construction.
	kvAtStress := kvAtRecCtx * uint64(stress) / uint64(recCtx)
	total := weight + kvAtStress + headroom
	if total > envelope {
		t.Errorf("stressContextFor: modelled total %d exceeds envelope %d (must approach, not exceed)", total, envelope)
	}
}

// TestStressContextForCapsAtModelMax guards the live on-hardware finding: a small
// model's tiny per-token KV makes the memory-bound ceiling millions of tokens, but
// the probe must never exceed the model's trained max context (default_ctx) — else
// it reports a multi-million-token "ceiling cleared" the model never actually ran
// (llama.cpp silently clamps a ctx above n_ctx_train).
func TestStressContextForCapsAtModelMax(t *testing.T) {
	const (
		weight   = uint64(491400032)
		headroom = uint64(7 << 30)
		envelope = uint64(62 << 30)
		modelMax = 32768
	)
	// recCtx already at the model max + huge envelope → must stay at the max, not
	// balloon into the millions.
	atMax := stressContextFor(modelMax, weight, uint64(400<<20), headroom, envelope, modelMax)
	if atMax != modelMax {
		t.Errorf("stressContextFor(recCtx==modelMax) = %d, want %d (capped at the rated max, not a memory-bound millions)", atMax, modelMax)
	}

	// Room to grow below the cap: recCtx 8192, model max 32768 → grow toward the cap
	// but never past it.
	grow := stressContextFor(8192, weight, uint64(100<<20), headroom, envelope, modelMax)
	if grow > modelMax {
		t.Errorf("stressContextFor = %d, want <= model max %d", grow, modelMax)
	}
	if grow <= 8192 {
		t.Errorf("stressContextFor = %d, want > recCtx 8192 (push toward the capped ceiling)", grow)
	}
}
