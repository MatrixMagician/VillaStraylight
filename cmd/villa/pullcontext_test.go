package main

import (
	"context"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/codingmode"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// TestEveryWeightPullObservesItsCommandContext pins that a Ctrl-C can interrupt a
// weight download from EVERY command that starts one, not just `villa model pull`.
//
// Threading the context through `model pull` alone left five other entry points
// passing context.Background() to the very same downloader: `model swap`,
// `model resident add`, `coding-mode enter`, and install's three pulls (the main
// model, the embed GGUF, the coder shard). Each is a multi-GB transfer, so on each
// of those paths the SIGINT/SIGTERM-cancelled context main installs never reached
// the transfer and the first Ctrl-C was swallowed.
//
// Aborting a pull is safe: download.PullModel keeps the partially-written ".part"
// file on a stream error and seeds the hash from it next run, so an interrupted
// transfer resumes via HTTP Range rather than restarting. Only a size/checksum
// MISMATCH deletes the partial.
//
// The test drives each LIVE deps constructor (not a stub) with an already-cancelled
// context and asserts the downloader saw that cancellation. Driving the live wiring
// is the point: a pull path that quietly reverts to Background would still pass a
// test written against a stub.
func TestEveryWeightPullObservesItsCommandContext(t *testing.T) {
	// The catalog's first model is a real id the resolve steps inside these closures
	// accept, so the pull seam is actually reached rather than short-circuited by an
	// unknown-model error.
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	modelID := cat.Models[0].ID

	// Cancelled up front: the downloader stub records the context it was handed, and
	// a context that observes this cancellation is one a real Ctrl-C would reach.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pulls := []struct {
		name string
		// call invokes one live pull entry point, returning the context the
		// downloader seam received.
		call func(t *testing.T, ctx context.Context) error
	}{
		{"model swap", func(_ *testing.T, ctx context.Context) error {
			return liveSwapDeps(ctx).Pull(catalog.Model{ID: modelID})
		}},
		{"model resident add", func(_ *testing.T, ctx context.Context) error {
			return liveResidentDeps(ctx).pull(catalog.Model{ID: modelID})
		}},
		{"coding-mode enter", func(_ *testing.T, ctx context.Context) error {
			return liveCodingModeDeps(ctx).Pull(codingmode.CoderTarget{Model: modelID})
		}},
		{"install embed GGUF", func(t *testing.T, ctx context.Context) error {
			return liveEnsureEmbedModel(ctx, t.TempDir())
		}},
		{"install coder shard", func(t *testing.T, ctx context.Context) error {
			return liveEnsureCoderModel(ctx, t.TempDir(), catalog.Shard{})
		}},
		{"install main model", func(t *testing.T, ctx context.Context) error {
			d, err := liveInstallDeps(ctx)
			if err != nil {
				return err
			}
			return d.EnsureModel(recommend.Recommendation{Model: modelID})
		}},
	}

	for _, p := range pulls {
		t.Run(p.name, func(t *testing.T) {
			// Each subtest gets its own data root so a closure that creates the models
			// dir cannot leak into the developer's real ~/.local/share/villa.
			t.Setenv("XDG_DATA_HOME", t.TempDir())

			origPull := pullFn
			t.Cleanup(func() { pullFn = origPull })

			var gotCtx context.Context
			pullFn = func(ctx context.Context, _ catalog.Model, _ string) error {
				gotCtx = ctx
				return nil
			}

			if err := p.call(t, ctx); err != nil {
				t.Fatalf("%s pull: %v", p.name, err)
			}
			if gotCtx == nil {
				t.Fatalf("%s never reached the downloader seam, so this test proves nothing "+
					"— fix the test setup", p.name)
			}
			if gotCtx.Err() == nil {
				t.Errorf("%s handed the downloader a context that ignores the command's "+
					"cancellation — a Ctrl-C cannot interrupt this multi-GB transfer", p.name)
			}
		})
	}
}
