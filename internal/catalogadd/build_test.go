package catalogadd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

const (
	testSHA  = "e4c1edb8d458d8f840d7d03a1ee997e50b94e6513c7b469d3948a7f7b9a50ea1"
	testSize = 28514152896
)

// testSource is the commit-pinned source every builder case starts from.
func testSource(t *testing.T) Source {
	t.Helper()
	s, err := ParseSource("https://huggingface.co/Youssofal/Heretic-GGUF/resolve/" + commit + "/Q6_K/Heretic-35B-A3B-Q6_K.gguf")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// hybridHeader is a qwen35moe-shaped fixture: 40 blocks of which every 4th carries a
// growing KV cache, so the entry must say 10 layers and not 40 (ADR-0007).
func hybridHeader(t *testing.T, extra map[string]uint64) gguf.Header {
	t.Helper()
	keys := map[string]uint64{
		"qwen35moe.block_count":             40,
		"qwen35moe.full_attention_interval": 4,
		"qwen35moe.attention.head_count_kv": 2,
		"qwen35moe.attention.key_length":    256,
		"qwen35moe.context_length":          262144,
	}
	for k, v := range extra {
		keys[k] = v
	}
	h, err := gguf.ReadHeader(bytes.NewReader(gguf.FixtureForTest("qwen35moe", keys)))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func testFacts() FileFacts { return FileFacts{SHA256: testSHA, SizeBytes: testSize} }

func mustHeaderFacts(t *testing.T, h gguf.Header) HeaderFacts {
	t.Helper()
	hf, err := HeaderFactsOf(h)
	if err != nil {
		t.Fatal(err)
	}
	return hf
}

// TestBuildFromAFixtureHeader is the table for the whole entry (ADR-0041): every
// field's value, and where it came from. The expected model is spelled out in full
// so a field that gains a default shows up as a diff here, not as a surprise on a host.
func TestBuildFromAFixtureHeader(t *testing.T) {
	src := testSource(t)
	names, err := ResolveNames(src, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	got, notes, err := Build(src, names, testFacts(), mustHeaderFacts(t, hybridHeader(t, nil)), Overrides{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := catalog.Model{
		ID:          "heretic-35b-a3b-q6_k",
		DisplayName: "Heretic-35B-A3B-Q6_K",
		Quant:       "Q6_K",

		WeightBytes:    testSize,
		NLayers:        10,
		NKVHeads:       2,
		HeadDim:        256,
		KVBytesPerElem: 2,

		DefaultCtx: 32768,

		// Left at their absence values, never guessed.
		MinEnvelopeBytes: 0,
		TierGB:           0,
		AgentCtx:         0,
		BackendDefault:   "",

		// Unqualified until a measurement or the operator says otherwise.
		UnifiedMemorySafe: false,
		Bootstrap:         false,

		Shards: []catalog.Shard{{
			URL:       src.URL,
			Filename:  "Heretic-35B-A3B-Q6_K.gguf",
			SHA256:    testSHA,
			SizeBytes: testSize,
		}},
	}
	if !modelsEqual(got, want) {
		t.Fatalf("Build =\n%+v\nwant\n%+v", got, want)
	}
	if got.NgramSafe || got.NgramProvenance != "" || got.Projector != nil || got.Draft != nil || got.SWA != nil || got.Role != "" {
		t.Fatalf("an added entry must start with no speculation qualification, no projector, no draft, no swa, no role: %+v", got)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "capped") {
		t.Fatalf("the ctx cap must be reported to the operator, notes: %v", notes)
	}
}

// modelsEqual compares the way the catalog does, by value.
func modelsEqual(a, b catalog.Model) bool {
	return catalog.OriginOf(a) == catalog.OriginOf(b) && a.ID == b.ID && a.DisplayName == b.DisplayName && a.Quant == b.Quant &&
		a.WeightBytes == b.WeightBytes && a.NLayers == b.NLayers && a.NKVHeads == b.NKVHeads && a.HeadDim == b.HeadDim &&
		a.KVBytesPerElem == b.KVBytesPerElem && a.DefaultCtx == b.DefaultCtx && a.MinEnvelopeBytes == b.MinEnvelopeBytes &&
		a.TierGB == b.TierGB && a.AgentCtx == b.AgentCtx && a.BackendDefault == b.BackendDefault &&
		a.UnifiedMemorySafe == b.UnifiedMemorySafe && a.Bootstrap == b.Bootstrap &&
		len(a.Shards) == len(b.Shards) && (len(a.Shards) == 0 || a.Shards[0] == b.Shards[0])
}

// TestBuiltEntryPassesTheCatalogChecks: the entry Build returns is one Load accepts
// and one CAT-01 finds consistent with the header it was built from.
func TestBuiltEntryPassesTheCatalogChecks(t *testing.T) {
	src := testSource(t)
	names, _ := ResolveNames(src, Overrides{})
	h := hybridHeader(t, nil)
	m, _, err := Build(src, names, testFacts(), mustHeaderFacts(t, h), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(catalog.Catalog{SchemaVersion: catalog.SupportedSchema, Models: []catalog.Model{m}}); err != nil {
		t.Fatalf("catalog.Validate: %v", err)
	}
	got, err := h.Geometry()
	if err != nil {
		t.Fatal(err)
	}
	if m.Geometry() != got {
		t.Fatalf("entry geometry %+v != header geometry %+v (CAT-01 would FAIL)", m.Geometry(), got)
	}
}

// TestBuildCopiesTheSlidingWindowBlock: a header with sliding layers yields an swa
// block carrying the same four values, or CAT-01 would compare unequal geometries on
// the first doctor run.
func TestBuildCopiesTheSlidingWindowBlock(t *testing.T) {
	keys := map[string]uint64{
		"gemma4.block_count":              10,
		"gemma4.attention.head_count_kv":  4,
		"gemma4.attention.key_length":     512,
		"gemma4.attention.key_length_swa": 256,
		"gemma4.attention.sliding_window": 1024,
		"gemma4.context_length":           131072,
	}
	pattern := map[string][]bool{"gemma4.attention.sliding_window_pattern": {true, true, true, true, false, true, true, true, true, false}}
	raw := gguf.FixtureWithArraysForTest("gemma4", keys, nil, pattern)
	h, err := gguf.ReadHeader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	src := testSource(t)
	names, _ := ResolveNames(src, Overrides{})
	m, _, err := Build(src, names, testFacts(), mustHeaderFacts(t, h), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if m.NLayers != 2 || m.SWA == nil || *m.SWA != (catalog.SlidingWindow{NLayers: 8, NKVHeads: 4, HeadDim: 256, Window: 1024}) {
		t.Fatalf("NLayers=%d SWA=%+v", m.NLayers, m.SWA)
	}
	want, _ := h.Geometry()
	if m.Geometry() != want {
		t.Fatalf("entry geometry %+v != header %+v", m.Geometry(), want)
	}
}

// TestBuildContextRules covers default_ctx, the one derived number with no formula in
// the seed: --ctx wins, otherwise min(trained context, cap), and a header with no
// trained context makes the flag required. A --ctx the model was not trained for refuses.
func TestBuildContextRules(t *testing.T) {
	src := testSource(t)
	names, _ := ResolveNames(src, Overrides{})

	tests := []struct {
		name    string
		extra   map[string]uint64
		drop    bool
		ov      Overrides
		want    int
		wantErr string
	}{
		{"trained above the cap is capped", nil, false, Overrides{}, DefaultCtxCap, ""},
		{"trained below the cap is used whole", map[string]uint64{"qwen35moe.context_length": 8192}, false, Overrides{}, 8192, ""},
		{"trained exactly at the cap", map[string]uint64{"qwen35moe.context_length": uint64(DefaultCtxCap)}, false, Overrides{}, DefaultCtxCap, ""},
		{"flag overrides the cap", nil, false, Overrides{Ctx: 131072}, 131072, ""},
		{"flag may go below", nil, false, Overrides{Ctx: 4096}, 4096, ""},
		{"flag above the trained length refuses", map[string]uint64{"qwen35moe.context_length": 8192}, false, Overrides{Ctx: 16384}, 0, "trained"},
		{"negative flag refuses", nil, false, Overrides{Ctx: -1}, 0, "positive"},
		{"no trained context and no flag refuses", nil, true, Overrides{}, 0, "--ctx"},
		{"no trained context with a flag is fine", nil, true, Overrides{Ctx: 16384}, 16384, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := hybridHeader(t, tt.extra)
			if tt.drop {
				keys := map[string]uint64{
					"qwen35moe.block_count":             40,
					"qwen35moe.full_attention_interval": 4,
					"qwen35moe.attention.head_count_kv": 2,
					"qwen35moe.attention.key_length":    256,
				}
				var err error
				h, err = gguf.ReadHeader(bytes.NewReader(gguf.FixtureForTest("qwen35moe", keys)))
				if err != nil {
					t.Fatal(err)
				}
			}
			m, _, err := Build(src, names, testFacts(), mustHeaderFacts(t, h), tt.ov)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Build error = %v, want one mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.DefaultCtx != tt.want {
				t.Fatalf("default_ctx = %d, want %d", m.DefaultCtx, tt.want)
			}
		})
	}
}

// TestHeaderFactsRefuseAnEntryVillaCannotSize: a header with no growing KV cache, or
// with a key villa cannot read, is refused rather than recorded as a zero that would
// make the KV term of the fit vanish.
func TestHeaderFactsRefuseAnEntryVillaCannotSize(t *testing.T) {
	tests := []struct {
		name string
		keys map[string]uint64
	}{
		{"no block_count", map[string]uint64{"x.attention.head_count_kv": 2, "x.attention.key_length": 128}},
		{"zero kv heads", map[string]uint64{"x.block_count": 4, "x.attention.head_count_kv": 0, "x.attention.key_length": 128}},
		{"zero key length", map[string]uint64{"x.block_count": 4, "x.attention.head_count_kv": 2, "x.attention.key_length": 0}},
		{"no kv-bearing layer", map[string]uint64{"x.block_count": 3, "x.full_attention_interval": 4, "x.attention.head_count_kv": 2, "x.attention.key_length": 128}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := gguf.ReadHeader(bytes.NewReader(gguf.FixtureForTest("x", tt.keys)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := HeaderFactsOf(h); err == nil {
				t.Fatal("HeaderFactsOf accepted a header villa cannot size")
			}
		})
	}
}

// TestBuildRefusesUnusableFileFacts: the digest is what every later pull verifies
// against, so a malformed one or an empty file never reaches an entry.
func TestBuildRefusesUnusableFileFacts(t *testing.T) {
	src := testSource(t)
	names, _ := ResolveNames(src, Overrides{})
	hf := mustHeaderFacts(t, hybridHeader(t, nil))
	for name, f := range map[string]FileFacts{
		"empty digest": {SizeBytes: 10},
		"short digest": {SHA256: "abc", SizeBytes: 10},
		"upper digest": {SHA256: strings.ToUpper(testSHA), SizeBytes: 10},
		"non-hex":      {SHA256: strings.Repeat("g", 64), SizeBytes: 10},
		"zero size":    {SHA256: testSHA},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Build(src, names, f, hf, Overrides{}); err == nil {
				t.Fatal("Build accepted unusable file facts")
			}
		})
	}
}

// TestResolveNames covers id, quant and display name: derived from the file name,
// overridable, and refused rather than guessed when neither works.
func TestResolveNames(t *testing.T) {
	mk := func(file string) Source {
		s, err := ParseSource("https://huggingface.co/o/r/resolve/" + commit + "/" + file)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	tests := []struct {
		name    string
		file    string
		ov      Overrides
		want    Names
		wantErr string
	}{
		{"unsloth dynamic quant", "Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf", Overrides{},
			Names{ID: "qwen3.6-35b-a3b-ud-q4_k_xl", Quant: "UD-Q4_K_XL", DisplayName: "Qwen3.6-35B-A3B-UD-Q4_K_XL"}, ""},
		{"plain k-quant", "Model-Q6_K.gguf", Overrides{}, Names{ID: "model-q6_k", Quant: "Q6_K", DisplayName: "Model-Q6_K"}, ""},
		{"q8_0", "Model-Q8_0.gguf", Overrides{}, Names{ID: "model-q8_0", Quant: "Q8_0", DisplayName: "Model-Q8_0"}, ""},
		{"iq", "Model-IQ4_XS.gguf", Overrides{}, Names{ID: "model-iq4_xs", Quant: "IQ4_XS", DisplayName: "Model-IQ4_XS"}, ""},
		{"lowercase token is upper-cased", "model.q4_k_m.gguf", Overrides{}, Names{ID: "model.q4_k_m", Quant: "Q4_K_M", DisplayName: "model.q4_k_m"}, ""},
		{"bf16", "Model-BF16.gguf", Overrides{}, Names{ID: "model-bf16", Quant: "BF16", DisplayName: "Model-BF16"}, ""},
		{"overrides win", "Model-Q6_K.gguf", Overrides{ID: "mine", Quant: "Q5_K_M", DisplayName: "My Model"}, Names{ID: "mine", Quant: "Q5_K_M", DisplayName: "My Model"}, ""},
		{"no quant token and no flag", "weights.gguf", Overrides{}, Names{}, "--quant"},
		{"no quant token with the flag", "weights.gguf", Overrides{Quant: "Q4_0"}, Names{ID: "weights", Quant: "Q4_0", DisplayName: "weights"}, ""},
		{"quant in the middle is not a token", "Q6_K-model.gguf", Overrides{}, Names{}, "--quant"},
		{"bad id override", "Model-Q6_K.gguf", Overrides{ID: "Has Space"}, Names{}, "--id"},
		{"uppercase id override", "Model-Q6_K.gguf", Overrides{ID: "Mine"}, Names{}, "--id"},
		{"id override with a separator", "Model-Q6_K.gguf", Overrides{ID: "a/b"}, Names{}, "--id"},
		{"bad quant override", "Model-Q6_K.gguf", Overrides{Quant: "q 6"}, Names{}, "--quant"},
		{"control character in the name", "Model-Q6_K.gguf", Overrides{DisplayName: "a\nb"}, Names{}, "--name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveNames(mk(tt.file), tt.ov)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResolveNames error = %v, want one mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("ResolveNames = %+v, want %+v", got, tt.want)
			}
		})
	}
}
