package residency

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// fakeDeps records what the protocol drove and returns canned signals. Every field
// has a working default, so a test overrides only the seam it is about.
type fakeDeps struct {
	ready        detect.Bool
	chat         inference.ChatResult
	busy         []detect.Int // consumed one per sample; the last value repeats
	busyIdx      atomic.Int32
	gtt          detect.Bytes
	journal      string
	journalFound bool
	verdict      inference.Verdict

	// chatDelay holds the generation probe open so the sampler ticks during it.
	chatDelay time.Duration

	folded    inference.RunningOffloadInput
	foldCalls int
	polled    string
	generated string
	journaled string
}

func newFakeDeps() *fakeDeps {
	return &fakeDeps{
		ready:        detect.KnownBool(true, "test"),
		chat:         inference.ChatResult{OK: true, Tokens: 7},
		busy:         []detect.Int{detect.KnownInt(84, "test")},
		gtt:          detect.KnownBytes(20<<30, "test"),
		journal:      "load_tensors: Vulkan0 model buffer size = 21504.49 MiB",
		journalFound: true,
		verdict:      inference.Verdict{Status: inference.StatusPass, Detail: "offload proven"},
	}
}

func (f *fakeDeps) deps() Deps {
	return Deps{
		PollHealth: func(_ context.Context, endpoint string, _ time.Duration) detect.Bool {
			f.polled = endpoint
			return f.ready
		},
		Generate: func(ctx context.Context, _, modelID string) inference.ChatResult {
			f.generated = modelID
			if f.chatDelay > 0 {
				select {
				case <-time.After(f.chatDelay):
				case <-ctx.Done():
					return inference.ChatResult{}
				}
			}
			return f.chat
		},
		GPUBusy: func() detect.Int {
			i := int(f.busyIdx.Add(1)) - 1
			if i >= len(f.busy) {
				i = len(f.busy) - 1
			}
			return f.busy[i]
		},
		GTTUsed: func() detect.Bytes { return f.gtt },
		Journal: func(service string) (string, bool) {
			f.journaled = service
			return f.journal, f.journalFound
		},
		Fold: func(in inference.RunningOffloadInput) inference.Verdict {
			f.folded = in
			f.foldCalls++
			return f.verdict
		},
	}
}

func target() Target {
	return Target{
		Endpoint:       "http://127.0.0.1:8080",
		Service:        "villa-llama.service",
		ModelID:        "qwen3-30b",
		ModelFile:      "qwen3-30b-Q4_K_M.gguf",
		ContextLen:     8192,
		WeightBytes:    21 << 30,
		SampleInterval: time.Millisecond,
	}
}

// TestUnevaluableSignalWarnsAndContradictedFails is the honesty invariant ADR-0001
// turns on, and the reason Prove returns a tri-state rather than a boolean: an
// unevaluable signal warns, a contradicted signal fails, and neither is manufactured
// from the other. Prove returns the fold's verdict verbatim, so a WARN is never
// hardened into a FAIL on the way out.
func TestUnevaluableSignalWarnsAndContradictedFails(t *testing.T) {
	t.Run("unevaluable signal yields Warn", func(t *testing.T) {
		f := newFakeDeps()
		f.verdict = inference.Verdict{Status: inference.StatusWarn, Detail: "journal carried no load_tensors line"}

		v := Prove(t.Context(), f.deps(), target())

		if v.Status != inference.StatusWarn {
			t.Fatalf("an unevaluable residency signal must WARN, got %v (%q)", v.Status, v.Detail)
		}
		if v.Detail != "journal carried no load_tensors line" {
			t.Errorf("the fold's detail must survive verbatim, got %q", v.Detail)
		}
	})

	t.Run("contradicted signal yields Fail", func(t *testing.T) {
		f := newFakeDeps()
		f.verdict = inference.Verdict{Status: inference.StatusFail, Detail: "CPU buffer only — silent CPU fallback"}

		v := Prove(t.Context(), f.deps(), target())

		if v.Status != inference.StatusFail {
			t.Fatalf("a contradicted residency signal must FAIL, got %v (%q)", v.Status, v.Detail)
		}
	})

	t.Run("both are a non-pass cutover, and the distinction survives in the detail", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			in   inference.Verdict
		}{
			{"warn", inference.Verdict{Status: inference.StatusWarn, Detail: "could not evaluate"}},
			{"fail", inference.Verdict{Status: inference.StatusFail, Detail: "contradicted"}},
		} {
			got := Cutover(tc.in)
			if got.Pass() {
				t.Errorf("%s: a non-pass residency verdict must never commit a cutover", tc.name)
			}
			if got.Detail != tc.in.Detail {
				t.Errorf("%s: cutover erased the detail (%q -> %q)", tc.name, tc.in.Detail, got.Detail)
			}
		}
		if !Cutover(inference.Verdict{Status: inference.StatusPass}).Pass() {
			t.Error("a true residency PASS must commit the cutover")
		}
	})
}

// TestNeverReadyFailsBeforeProbing locks the load_tensors-hang guard: a server that
// does not become ready is a FAIL, and the protocol never goes on to probe or fold
// (which is what would turn a hang into an infinite wait).
func TestNeverReadyFailsBeforeProbing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ready detect.Bool
	}{
		{"unknown readiness", detect.UnknownBool("health endpoint unreachable", "")},
		{"negative readiness", detect.KnownBool(false, "test")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.ready = tc.ready

			v := Prove(t.Context(), f.deps(), target())

			if v.Status != inference.StatusFail {
				t.Fatalf("a never-ready server must FAIL, got %v", v.Status)
			}
			if f.generated != "" {
				t.Error("a never-ready server must not be probed")
			}
			if f.foldCalls != 0 {
				t.Error("a never-ready server must not reach the residency fold")
			}
		})
	}
}

// TestTokenlessProbeFailsAndNeverFolds: ready plus health-200 is not offload. A
// probe that streams no tokens is a FAIL, and it must not reach the fold, where a
// stale journal could otherwise produce a PASS on a stack that generated nothing.
func TestTokenlessProbeFailsAndNeverFolds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		chat       inference.ChatResult
		wantDetail string
	}{
		{"zero tokens", inference.ChatResult{OK: true, Tokens: 0}, "generation probe returned no tokens"},
		{"probe error", inference.ChatResult{OK: false, Detail: "connection refused"}, "generation probe failed: connection refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.chat = tc.chat

			v := Prove(t.Context(), f.deps(), target())

			if v.Status != inference.StatusFail {
				t.Fatalf("a tokenless probe must FAIL, got %v", v.Status)
			}
			if v.Detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", v.Detail, tc.wantDetail)
			}
			if f.foldCalls != 0 {
				t.Error("a tokenless probe must never reach the residency fold — a stale journal would read as PASS")
			}
		})
	}
}

// TestSamplesBusyDuringTheDecodeAndKeepsMax is the reason the sampler exists: a
// single post-probe read can miss a short decode's busy window. The probe is held
// open while the ticker fires, and the MAXIMUM reading — not the last one, which
// here is the idle zero — reaches the fold.
func TestSamplesBusyDuringTheDecodeAndKeepsMax(t *testing.T) {
	f := newFakeDeps()
	f.chatDelay = 40 * time.Millisecond
	f.busy = []detect.Int{
		detect.KnownInt(0, "pre-decode idle"),
		detect.KnownInt(97, "mid-decode"),
		detect.KnownInt(0, "post-decode idle"),
	}

	Prove(t.Context(), f.deps(), target())

	if f.foldCalls != 1 {
		t.Fatalf("fold called %d times, want 1", f.foldCalls)
	}
	got := f.folded.GPUBusyPercent
	if !got.Known || got.Value != 97 {
		t.Errorf("the fold must receive the MAX in-flight busy reading (97), got %+v", got)
	}
	if f.busyIdx.Load() < 3 {
		t.Errorf("busy was sampled %d times, want at least 3 (up-front, on-tick, at-completion)", f.busyIdx.Load())
	}
}

// TestUnreadableJournalIsUnknownNotNegative: a journal that cannot be read is an
// absent signal, not evidence of CPU fallback. The protocol passes empty text to the
// fold, which types it as Unknown, rather than short-circuiting to a FAIL of its own.
func TestUnreadableJournalIsUnknownNotNegative(t *testing.T) {
	f := newFakeDeps()
	f.journal = ""
	f.journalFound = false
	f.verdict = inference.Verdict{Status: inference.StatusWarn, Detail: "residency could not be evaluated"}

	v := Prove(t.Context(), f.deps(), target())

	if f.foldCalls != 1 {
		t.Fatalf("an unreadable journal must still reach the fold, called %d times", f.foldCalls)
	}
	if f.folded.JournalText != "" {
		t.Errorf("JournalText = %q, want empty (the fold types absence as Unknown)", f.folded.JournalText)
	}
	if v.Status != inference.StatusWarn {
		t.Errorf("an unreadable journal must not be reported as a contradicted signal, got %v", v.Status)
	}
}

// TestTargetDrivesTheProtocol proves Target is the whole of what varies between the
// five former copies: each field lands where the fold and the probe expect it.
// Coding mode is the case that matters — it proves the SERVED coder model at the
// resolved agent ctx, not the chat model at the chat ctx.
func TestTargetDrivesTheProtocol(t *testing.T) {
	f := newFakeDeps()
	tgt := target()
	tgt.ModelID = "qwen3-coder-30b"
	tgt.ModelFile = "qwen3-coder-30b-Q4_K_M.gguf"
	tgt.ContextLen = 32768
	tgt.WeightBytes = 17 << 30
	tgt.Service = "villa-llama.service"
	tgt.Markers = inference.ResidencyMarkers{DeviceToken: "TEST0", FaultString: "TEST-ABORT"}

	Prove(t.Context(), f.deps(), tgt)

	if f.polled != tgt.Endpoint {
		t.Errorf("polled %q, want the target endpoint %q", f.polled, tgt.Endpoint)
	}
	if f.generated != tgt.ModelID {
		t.Errorf("probed model %q, want the SERVED model %q", f.generated, tgt.ModelID)
	}
	if f.journaled != tgt.Service {
		t.Errorf("journaled %q, want %q", f.journaled, tgt.Service)
	}
	if f.folded.ConfigModel != tgt.ModelFile {
		t.Errorf("ConfigModel = %q, want the resolved GGUF FILENAME %q — the id makes the drift overlay misfire", f.folded.ConfigModel, tgt.ModelFile)
	}
	if f.folded.ConfigContext != tgt.ContextLen {
		t.Errorf("ConfigContext = %d, want the SERVED ctx %d", f.folded.ConfigContext, tgt.ContextLen)
	}
	if f.folded.WeightBytes != tgt.WeightBytes {
		t.Errorf("WeightBytes = %d, want %d", f.folded.WeightBytes, tgt.WeightBytes)
	}
	if f.folded.Markers != tgt.Markers {
		t.Errorf("Markers = %+v, want the backend-owned markers %+v", f.folded.Markers, tgt.Markers)
	}
	if f.folded.GTTUsedBytes != f.gtt {
		t.Errorf("GTTUsedBytes = %+v, want the sampled floor %+v", f.folded.GTTUsedBytes, f.gtt)
	}
}

// TestDeadlineFailsRatherThanHanging: the whole proof shares one deadline, so a
// probe that never returns is a bounded FAIL rather than an infinite wait.
func TestDeadlineFailsRatherThanHanging(t *testing.T) {
	f := newFakeDeps()
	f.chatDelay = time.Hour

	tgt := target()
	tgt.ReadyTimeout = 30 * time.Millisecond

	start := time.Now()
	v := Prove(t.Context(), f.deps(), tgt)
	elapsed := time.Since(start)

	if v.Status != inference.StatusFail {
		t.Fatalf("a probe that never returns must FAIL, got %v", v.Status)
	}
	if elapsed > 5*time.Second {
		t.Errorf("the proof waited %v; it must be bounded by ReadyTimeout", elapsed)
	}
	if f.foldCalls != 0 {
		t.Error("a timed-out probe must never reach the residency fold")
	}
}

// TestProveCutoverIsProveThenMap pins the one-call entry point the three
// transactional callers use.
func TestProveCutoverIsProveThenMap(t *testing.T) {
	f := newFakeDeps()
	if got := ProveCutover(t.Context(), f.deps(), target()); got.Status != prove.StatusPass {
		t.Errorf("a proven stack must commit the cutover, got %+v", got)
	}

	f = newFakeDeps()
	f.verdict = inference.Verdict{Status: inference.StatusWarn, Detail: "unevaluable"}
	if got := ProveCutover(t.Context(), f.deps(), target()); got.Pass() {
		t.Error("an unevaluable residency verdict must not commit the cutover")
	}
}
