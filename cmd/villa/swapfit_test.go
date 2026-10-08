package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
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
	fit := swapFit(fixtureProfile(), cat)
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

// TestSwapFitDraftDroppedForMemoryIsAShortfall (#301 review): with speculation =
// draft, a draft that does not fit beside the model is a memory shortfall, which a
// smaller ctx can cure because the draft's KV shrinks with it, so Size must be free
// to retry at the default. It is not reported as an unqualified mode.
func TestSwapFitDraftDroppedForMemoryIsAShortfall(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range cat.Models {
		if m.ID == "qwen3.8-27b" {
			draft := *m.Draft
			draft.WeightBytes = 60 << 30
			cat.Models[i].Draft = &draft
		}
	}
	m, _ := cat.FindByID("qwen3.8-27b")
	got := swapFit(fixtureProfile(), cat)(m,
		config.VillaConfig{Ctx: 4096, Speculation: config.SpeculationDraft})
	if got.OK || !got.OverEnvelope {
		t.Fatalf("fit OK=%v OverEnvelope=%v (%q), want a shortfall", got.OK, got.OverEnvelope, got.Detail)
	}
	if !strings.Contains(got.Detail, "dropped") {
		t.Errorf("detail %q, want the draft's shortfall", got.Detail)
	}
}

// TestSwapFitSharedCodingModeServesAtTheAgentCtx (#301 review): in coding mode with
// no separate coder (shared residency) the chat model itself is served, at the
// coder agent ctx, so that is the ctx the swap must size.
func TestSwapFitSharedCodingModeServesAtTheAgentCtx(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cat.FindByID("qwen3.6-35b-a3b")
	got := swapFit(fixtureProfile(), cat)(m,
		config.VillaConfig{Ctx: 4096, CodingMode: true, CoderAgentCtx: 65536})
	if !got.OK || !strings.HasSuffix(got.Detail, "at 65536 context.") {
		t.Errorf("fit OK=%v detail %q, want a fit at 65536 context", got.OK, got.Detail)
	}
}

// TestSwapFitSharedCodingModeKeepsSpeculationOff (#301 review): liveSpeculation
// renders no speculation when none is persisted, coding mode included, so the fit
// must not reserve the draft the entry's qualification would otherwise pick: the
// coding-mode verdict equals the plain one at the same ctx.
func TestSwapFitSharedCodingModeKeepsSpeculationOff(t *testing.T) {
	cat, _, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cat.FindByID("qwen3.8-27b")
	fit := swapFit(fixtureProfile(), cat)
	plain := fit(m, config.VillaConfig{Ctx: 8192})
	coding := fit(m, config.VillaConfig{Ctx: 4096, CodingMode: true, CoderAgentCtx: 8192})
	if !plain.OK || coding.Detail != plain.Detail {
		t.Errorf("coding-mode fit %q, want the plain off fit %q", coding.Detail, plain.Detail)
	}
}
