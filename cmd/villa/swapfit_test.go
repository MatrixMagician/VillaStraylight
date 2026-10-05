package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// TestSwapFitSizesTheServedCtx (#301): the swap's fit sizes the target the way the
// render serves the config: at its ctx (unset means the entry's default), floored
// at the entry's agent ctx in tools mode, with the persisted speculation mode. A
// fixed 64 GiB fixture envelope keeps the verdicts the same on any host.
func TestSwapFitSizesTheServedCtx(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	fit := swapFit(fixtureProfile(), cat, recommend.MemoryInputs{}, recommend.WebSearchInputs{})
	model := func(id string) catalog.Model {
		m, ok := cat.FindByID(id)
		if !ok {
			t.Fatalf("seed catalog has no %s", id)
		}
		return m
	}
	cases := []struct {
		name, model  string
		cfg          config.VillaConfig
		ok, over     bool
		detailSuffix string
		detailHas    string
	}{
		{"the configured ctx", "qwen3.6-35b-a3b",
			config.VillaConfig{Ctx: 4096}, true, false, "at 4096 context.", ""},
		{"an unset ctx is the entry's default", "qwen3.5-2b",
			config.VillaConfig{}, true, false, "at 8192 context.", ""},
		{"tools mode floors at the agent ctx", "qwen3.6-35b-a3b",
			config.VillaConfig{Ctx: 4096, ToolsMode: true}, true, false, "at 16384 context.", ""},
		{"an unqualified speculation mode refuses without a shortfall", "qwen3.5-0.8b",
			config.VillaConfig{Ctx: 4096, Speculation: config.SpeculationNgram}, false, false, "", "not qualified"},
		{"over the envelope is a shortfall", "qwen3-coder-next-q4",
			config.VillaConfig{Ctx: 131072}, false, true, "usable", "needs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fit(model(tc.model), tc.cfg)
			if got.OK != tc.ok || got.OverEnvelope != tc.over {
				t.Fatalf("fit OK=%v OverEnvelope=%v (%q), want %v %v", got.OK, got.OverEnvelope, got.Detail, tc.ok, tc.over)
			}
			if !strings.HasSuffix(got.Detail, tc.detailSuffix) || !strings.Contains(got.Detail, tc.detailHas) {
				t.Errorf("detail %q, want suffix %q containing %q", got.Detail, tc.detailSuffix, tc.detailHas)
			}
		})
	}
}
