package recommend

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// TestSlidingWindowCacheIsCounted guards the promise that the fit reserves the
// sliding-window layers' cache as well as the global layers' (ADR-0029). On the
// embedded seed, Gemma 4 31B at ctx 16384 on a 33 GB envelope carries 1342177280
// bytes of global KV and 3774873600 bytes of sliding-window KV, and the second
// term is what pushes it over: before the term existed the pick was admitted with
// about 0.29 GB of slack and then loaded 3.6 GiB it had never reserved. The
// checkpoint cap (ADR-0034) adds 3355443200 bytes on top.
func TestSlidingWindowCacheIsCounted(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	rec := Pick(profileWithEnvelope(33_000_000_000), cat, Overrides{Model: "gemma-4-31b", Ctx: 16384}, nil)
	if rec.Model != "gemma-4-31b" {
		t.Fatalf("Model = %q, want gemma-4-31b (notes: %v)", rec.Model, rec.Notes)
	}
	const wantKV uint64 = 1342177280 + 3774873600 + 3355443200
	if rec.KVCacheBytes != wantKV {
		t.Errorf("KVCacheBytes = %d, want %d (global + sliding-window + checkpoints)", rec.KVCacheBytes, wantKV)
	}
	if rec.Fits {
		t.Errorf("Fits = true with %d bytes needed on a %d envelope, want false", rec.TotalBytes, rec.UsableEnvelopeBytes)
	}
}

// TestSlidingWindowCheckpointsAreCounted guards ADR-0034's fit term on the
// embedded seed: Gemma 4 31B at ctx 16384 fit a 40 GB envelope while the fit
// counted only its two caches, and the 3355443200 bytes the rendered checkpoint
// cap can hold are what tip it over.
func TestSlidingWindowCheckpointsAreCounted(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	rec := Pick(profileWithEnvelope(40_000_000_000), cat, Overrides{Model: "gemma-4-31b", Ctx: 16384}, nil)
	if rec.Model != "gemma-4-31b" {
		t.Fatalf("Model = %q, want gemma-4-31b (notes: %v)", rec.Model, rec.Notes)
	}
	const wantKV uint64 = 1342177280 + 3774873600 + 3355443200
	if rec.KVCacheBytes != wantKV {
		t.Errorf("KVCacheBytes = %d, want %d (global + sliding-window + checkpoints)", rec.KVCacheBytes, wantKV)
	}
	if rec.Fits {
		t.Errorf("Fits = true with %d bytes needed on a %d envelope, want false", rec.TotalBytes, rec.UsableEnvelopeBytes)
	}
}

// TestSeedPicksAt50GB pins the coder default #315 changed on 46 to 54 GB
// envelopes under the largest-total rule: the chat pick stays the Qwen MoE and the
// coder pick is GLM-4.7-Flash. A later seed entry that moves either does so
// knowingly, by editing this test.
func TestSeedPicksAt50GB(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	rec := Pick(profileWithEnvelope(50_000_000_000), cat, Overrides{}, nil)
	if rec.Model != "qwen3.6-35b-a3b" {
		t.Errorf("chat Model = %q, want qwen3.6-35b-a3b", rec.Model)
	}
	if rec.Coder.Model != "glm-4.7-flash" {
		t.Errorf("Coder.Model = %q, want glm-4.7-flash", rec.Coder.Model)
	}
	if !rec.Coder.Fits {
		t.Errorf("Coder.Fits = false, want true")
	}
}
