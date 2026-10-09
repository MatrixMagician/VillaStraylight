package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// stubAnsweringPodman puts a `podman` on PATH that records every argv and answers
// each `run` with an HTTP 200 code on stdout.
func stubAnsweringPodman(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nd=\"$(dirname \"$0\")\"\necho \"$*\" >> \"$d/calls\"\n" +
		"if [ \"$1\" = run ]; then printf 200; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o700); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// runNetworks returns the --network value of every recorded `podman run`.
func runNetworks(t *testing.T, dir string) []string {
	t.Helper()
	var nets []string
	for _, c := range recordedCalls(t, dir) {
		f := strings.Fields(c)
		if len(f) == 0 || f[0] != "run" {
			continue
		}
		var got []string
		for i, a := range f {
			if a == "--network" && i+1 < len(f) {
				got = append(got, f[i+1])
			}
		}
		nets = append(nets, strings.Join(got, ","))
	}
	return nets
}

// TestEachProbeRunsOnItsTargetsNetwork guards ADR-0036: the services with no
// runtime egress are reachable only on villa-closed, so their probes run there;
// inference, the fetchers, the inference proxy and the egress negative control
// stay on the routed villa network.
func TestEachProbeRunsOnItsTargetsNetwork(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	closed := []struct {
		name  string
		probe func(ctx context.Context)
	}{
		{"qdrant and embed health", func(context.Context) { liveQdrantHealth(config.QdrantAddr, config.QdrantPort) }},
		{"rerank health", func(context.Context) { liveRerankHealth(config.RerankAddr, config.RerankPort) }},
		{"extract health", func(context.Context) { liveExtractHealth(config.ExtractAddr, config.ExtractPort) }},
		{"voice health", func(context.Context) { liveSttHealth() }},
		{"image health", func(context.Context) { liveImageHealth(config.VillaConfig{}) }},
		{"eval rerank", func(ctx context.Context) { _, _ = liveEvalRerank(ctx, "q", []string{"a", "b"}) }},
		{"eval extract", func(ctx context.Context) { _, _ = liveEvalExtract(ctx, "x.txt", "text/plain", []byte("x")) }},
		{"voice proof", func(ctx context.Context) { _, _ = liveVoiceDriver(ctx).Speak() }},
		{"image proof", func(ctx context.Context) { imageGenerate(catalog.ImageModel{})(ctx, "") }},
		{"memory proof", func(ctx context.Context) {
			liveMemoryProof(ctx, memoryProofInput{embedAddr: config.EmbedAddr, embedPort: config.EmbedPort, qdrantAddr: config.QdrantAddr, qdrantPort: config.QdrantPort})
		}},
	}
	routed := []struct {
		name  string
		probe func(ctx context.Context)
	}{
		{"searxng and websafe health", func(context.Context) { liveSearxngHealth(config.SearxngAddr, config.SearxngPort) }},
		{"inferproxy health", func(context.Context) { liveInferproxyHealth() }},
		{"inference request", func(ctx context.Context) {
			_, _ = runProbeCurlIn(ctx, "img", inference.CurlRequest{Args: []string{"http://villa-llama:8080/v1/models"}})
		}},
		{"egress negative control", func(ctx context.Context) { _, _ = runProbeCurl(ctx, "img", egressNegativeControlHost) }},
	}
	check := func(name string, probe func(context.Context), want string) {
		t.Run(name, func(t *testing.T) {
			dir := stubAnsweringPodman(t)
			resetMemoryHealthCache()
			rerankHealthCache.Reset()
			extractHealthCache.Reset()
			probe(t.Context())
			nets := runNetworks(t, dir)
			if len(nets) == 0 {
				t.Fatal("the probe ran no podman run")
			}
			for _, got := range nets {
				if got != want {
					t.Errorf("probe ran on --network %q, want %q", got, want)
				}
			}
		})
	}
	for _, c := range closed {
		check(c.name, c.probe, "villa-closed")
	}
	for _, c := range routed {
		check(c.name, c.probe, "villa")
	}
}
