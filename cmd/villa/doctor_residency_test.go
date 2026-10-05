package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// TestSearchResidencyDriveOutlastsSettle pins #286: the drive body must keep
// villa-llama decoding past searchResidencySettle, or the in-flight sample is never
// taken and the check is a permanent WARN.
//
// A 16-token completion finished in ~0.38 s on the dev host, before the 750 ms settle,
// so every round was joined unsampled. ignore_eos stops the model ending early on its
// own, and max_tokens at the FASTEST decode rate must still outlast the settle with
// margin; at the SLOWEST rate every round the proof may drive must fit agentProofBudget.
func TestSearchResidencyDriveOutlastsSettle(t *testing.T) {
	raw, err := searchResidencyDriveBody("qwen3")
	if err != nil {
		t.Fatalf("searchResidencyDriveBody: %v", err)
	}
	var body struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		IgnoreEOS bool   `json:"ignore_eos"`
		Stream    bool   `json:"stream"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("drive body is not JSON: %v", err)
	}
	if !body.IgnoreEOS {
		t.Errorf("ignore_eos = false: the model may emit EOS early and finish before the settle (#286)")
	}
	if body.Stream {
		t.Errorf("stream = true: the round must stay a single bounded request")
	}
	if body.Model != "qwen3" {
		t.Errorf("model = %q, want it JSON-marshaled through", body.Model)
	}

	const margin = 2 // the fastest decode must outlast the settle by at least this factor
	fastest := time.Duration(float64(body.MaxTokens) / searchResidencyDecodeRateMax * float64(time.Second))
	if fastest < margin*searchResidencySettle {
		t.Errorf("max_tokens %d at %d tok/s ends in %v, want >= %dx the %v settle", body.MaxTokens, searchResidencyDecodeRateMax, fastest, margin, searchResidencySettle)
	}
	slowest := time.Duration(float64(searchResidencyDriveRounds*body.MaxTokens) / searchResidencyDecodeRateMin * float64(time.Second))
	if slowest > agentProofBudget {
		t.Errorf("%d rounds x %d tokens at %d tok/s takes %v, over agentProofBudget %v", searchResidencyDriveRounds, body.MaxTokens, searchResidencyDecodeRateMin, slowest, agentProofBudget)
	}
}

// stubSleepingPodman puts a `podman` on PATH that sleeps for the given seconds and
// exits 0: a drive round that stays in flight that long.
func stubSleepingPodman(t *testing.T, seconds string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep " + seconds + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o700); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestSearchResidencySamplesARoundThatOutlastsTheSettle pins #286 end to end over the
// real proof with fake host deps: a drive round still running at the settle is
// sampled (the stub stack's residency evidence folds to PASS), while a round that
// finishes before the settle is joined unsampled and degrades to the typed-Unknown
// WARN, never an idle-sampled verdict.
func TestSearchResidencySamplesARoundThatOutlastsTheSettle(t *testing.T) {
	cfg := config.VillaConfig{Backend: "vulkan", Model: "qwen3", Ctx: 131072, WebSearchEnabled: true}

	t.Run("a round outlasting the settle is sampled", func(t *testing.T) {
		stubSleepingPodman(t, "1.5")
		sd, err := status.StubDeps(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("status.StubDeps: %v", err)
		}
		v := runSearchResidencyUnderLoad(t.Context(), cfg, &sd)
		if v.Status != inference.StatusPass {
			t.Fatalf("Status = %v (%s), want StatusPass: the round was in flight at the settle, so residency should have been sampled", v.Status, v.Detail)
		}
	})

	t.Run("a round finishing before the settle is not sampled", func(t *testing.T) {
		stubSleepingPodman(t, "0")
		sd, err := status.StubDeps(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("status.StubDeps: %v", err)
		}
		v := runSearchResidencyUnderLoad(t.Context(), cfg, &sd)
		if v.Status != inference.StatusWarn || !strings.Contains(v.Detail, "no chat round stayed in flight") {
			t.Fatalf("verdict = %v %q, want the typed-Unknown WARN for an unsampled run", v.Status, v.Detail)
		}
	})
}
