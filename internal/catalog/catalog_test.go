package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goodSamplingJSON is a valid agent_sampling block shared by the coder-entry
// validation tests; individual cases override it to exercise a single guard.
const goodSamplingJSON = `{"temperature": 0.7, "top_p": 0.8, "top_k": 20, "repeat_penalty": 1.05}`

// TestLoadEmbeddedSeed asserts Load("") returns the embedded seed with the
// supported schema, at least three tier seeds plus exactly one bootstrap entry,
// and that every model carries the dimensions the fit math needs.
func TestLoadEmbeddedSeed(t *testing.T) {
	c, warnings, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(\"\"): unexpected warnings: %v", warnings)
	}
	if c.SchemaVersion != SupportedSchema {
		t.Errorf("embedded seed schema_version = %d, want %d", c.SchemaVersion, SupportedSchema)
	}

	var tiers, bootstraps int
	for _, m := range c.Models {
		if m.Bootstrap {
			bootstraps++
		} else {
			tiers++
		}
		if m.NLayers <= 0 || m.NKVHeads <= 0 || m.HeadDim <= 0 || m.KVBytesPerElem <= 0 {
			t.Errorf("model %q missing a required KV dimension (layers=%d kv_heads=%d head_dim=%d bytes_per_elem=%d)",
				m.ID, m.NLayers, m.NKVHeads, m.HeadDim, m.KVBytesPerElem)
		}
		if m.WeightBytes == 0 {
			t.Errorf("model %q has zero weight_bytes", m.ID)
		}
	}
	if tiers < 3 {
		t.Errorf("embedded seed has %d tier entries, want >= 3", tiers)
	}
	if bootstraps != 1 {
		t.Errorf("embedded seed has %d bootstrap entries, want exactly 1", bootstraps)
	}
}

// TestLoadSeedDownloadMetadata asserts the schema-v2 seed carries real download
// metadata: every tier/bootstrap entry exposes at least one shard, and each shard
// carries a resolve URL + a non-empty expected SHA256 + a non-zero expected size.
// This is the MODEL-02 contract the downloader consumes.
func TestLoadSeedDownloadMetadata(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if SupportedSchema != 3 {
		t.Fatalf("SupportedSchema = %d, want 3 (schema bumped for coder-role fields, Phase 24 D-01)", SupportedSchema)
	}
	if c.SchemaVersion != 3 {
		t.Errorf("embedded seed schema_version = %d, want 3", c.SchemaVersion)
	}
	for _, m := range c.Models {
		if len(m.Shards) == 0 {
			t.Errorf("model %q has no shards — download metadata missing", m.ID)
			continue
		}
		for i, s := range m.Shards {
			if s.URL == "" {
				t.Errorf("model %q shard %d has empty URL", m.ID, i)
			}
			if s.Filename == "" {
				t.Errorf("model %q shard %d has empty filename", m.ID, i)
			}
			if len(s.SHA256) != 64 {
				t.Errorf("model %q shard %d sha256 = %q, want 64 hex chars", m.ID, i, s.SHA256)
			}
			if s.SizeBytes == 0 {
				t.Errorf("model %q shard %d has zero size_bytes", m.ID, i)
			}
		}
	}
}

// TestLoadSeedVerifiedDims asserts the Pitfall-6 corrected dimensions made it into
// the seed for the validated entries (0.5B 24L/2KV/64; 1.5B 28L/2KV/128;
// 30B-A3B 48L/4KV/128 — n_kv_heads=4 NOT 8).
func TestLoadSeedVerifiedDims(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	want := map[string]struct{ layers, kv, head int }{
		"qwen2.5-0.5b":  {24, 2, 64},
		"qwen2.5-1.5b":  {28, 2, 128},
		"qwen3-30b-a3b": {48, 4, 128},
	}
	for id, w := range want {
		m, ok := c.FindByID(id)
		if !ok {
			t.Errorf("seed missing expected entry %q", id)
			continue
		}
		if m.NLayers != w.layers || m.NKVHeads != w.kv || m.HeadDim != w.head {
			t.Errorf("model %q dims = %dL/%dKV/%d, want %dL/%dKV/%d",
				id, m.NLayers, m.NKVHeads, m.HeadDim, w.layers, w.kv, w.head)
		}
	}
}

// TestLoadMultiShardParses asserts a multi-shard external catalog (using the
// -00001-of-0000N naming convention) parses and each shard carries its own
// per-shard sha256 + size.
func TestLoadMultiShardParses(t *testing.T) {
	path := filepath.Join("testdata", "multishard-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(multishard): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(multishard): unexpected warnings: %v", warnings)
	}
	m, ok := c.FindByID("sharded-235b")
	if !ok {
		t.Fatalf("Load(multishard): sharded-235b not found (got %d models)", len(c.Models))
	}
	if len(m.Shards) != 3 {
		t.Fatalf("sharded-235b has %d shards, want 3", len(m.Shards))
	}
	for i, s := range m.Shards {
		if len(s.SHA256) != 64 {
			t.Errorf("shard %d sha256 = %q, want 64 hex chars", i, s.SHA256)
		}
		if s.SizeBytes == 0 {
			t.Errorf("shard %d has zero size_bytes", i)
		}
		want := fmt.Sprintf("-0000%d-of-00003.gguf", i+1)
		if !strings.HasSuffix(s.Filename, want) {
			t.Errorf("shard %d filename = %q, want suffix %q", i, s.Filename, want)
		}
	}
}

// TestLoadPrefersExternal asserts a valid external catalog is preferred over the
// embedded seed.
func TestLoadPrefersExternal(t *testing.T) {
	path := filepath.Join("testdata", "good-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(good): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(good): unexpected warnings: %v", warnings)
	}
	if _, ok := c.FindByID("external-only-model"); !ok {
		t.Errorf("Load(good): expected external catalog to be used (external-only-model not found); got %d models", len(c.Models))
	}
}

// TestLoadVersionMismatchFallsBack asserts that too-new and too-old external
// catalogs warn and fall back to the embedded seed without an error.
func TestLoadVersionMismatchFallsBack(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"too-new", "too-new-catalog.json"},
		{"too-old", "too-old-catalog.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.file)
			c, warnings, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s): unexpected error: %v", tc.name, err)
			}
			if len(warnings) == 0 {
				t.Errorf("Load(%s): expected a schema-mismatch warning, got none", tc.name)
			}
			if !strings.Contains(strings.Join(warnings, " "), "schema_version") {
				t.Errorf("Load(%s): warning should mention schema_version, got %v", tc.name, warnings)
			}
			// Fell back to embedded seed (has the bootstrap entry).
			if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
				t.Errorf("Load(%s): expected fallback to embedded seed, but qwen2.5-1.5b not present", tc.name)
			}
		})
	}
}

// TestLoadMalformedFallsBack asserts an invalid-JSON external catalog warns and
// falls back to the embedded seed — never a panic, never a returned error.
func TestLoadMalformedFallsBack(t *testing.T) {
	path := filepath.Join("testdata", "malformed-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(malformed): unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("Load(malformed): expected a warning, got none")
	}
	if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
		t.Errorf("Load(malformed): expected fallback to embedded seed")
	}
}

// TestLoadSeedCoderEntries asserts the schema-v3 seed ships exactly three
// role:"coder" entries (CODER-01), each with an agent-profile context,
// a repo@revision template-provenance pin, and a single shard whose URL is
// revision-pinned (`resolve/{40-hex}` — never `resolve/main/`).
func TestLoadSeedCoderEntries(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if SupportedSchema != 3 {
		t.Fatalf("SupportedSchema = %d, want 3 (schema bumped for coder-role fields, Phase 24 D-01)", SupportedSchema)
	}
	wantIDs := map[string]bool{
		"qwen3-coder-30b-a3b": false,
		"qwen3-coder-next-q4": false,
		"qwen3-coder-next-q3": false,
	}
	revisionURL := regexp.MustCompile(`/resolve/[0-9a-f]{40}/`)
	coders := 0
	for _, m := range c.Models {
		if m.Role != "coder" {
			continue
		}
		coders++
		if _, expected := wantIDs[m.ID]; !expected {
			t.Errorf("unexpected coder entry %q in seed", m.ID)
			continue
		}
		wantIDs[m.ID] = true
		if m.AgentCtx <= 0 {
			t.Errorf("coder entry %q has agent_ctx %d, want > 0", m.ID, m.AgentCtx)
		}
		if m.TemplateProvenance == "" || !strings.Contains(m.TemplateProvenance, "@") {
			t.Errorf("coder entry %q template_provenance = %q, want non-empty repo@revision pin", m.ID, m.TemplateProvenance)
		}
		if len(m.Shards) != 1 {
			t.Errorf("coder entry %q has %d shards, want exactly 1", m.ID, len(m.Shards))
			continue
		}
		u := m.Shards[0].URL
		if strings.Contains(u, "/resolve/main/") {
			t.Errorf("coder entry %q shard URL %q uses /resolve/main/ — must be revision-pinned", m.ID, u)
		}
		if !revisionURL.MatchString(u) {
			t.Errorf("coder entry %q shard URL %q lacks a /resolve/{40-hex}/ revision pin", m.ID, u)
		}
	}
	if coders != 3 {
		t.Errorf("seed has %d role:\"coder\" entries, want exactly 3", coders)
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("seed missing expected coder entry %q", id)
		}
	}
}

// TestCatalogModelFailClosedDefaults asserts the fail-closed decode
// defaults: a Model decoded from JSON WITHOUT role / cache_reuse_safe
// keys yields Role == "" (treated as chat) and CacheReuseSafe == false
// absence never widens capability.
func TestCatalogModelFailClosedDefaults(t *testing.T) {
	raw := `{"id":"no-coder-keys","quant":"Q4_K_M","weight_bytes":1000}`
	var m Model
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal minimal entry: %v", err)
	}
	if m.Role != "" {
		t.Errorf("absent role decoded as %q, want \"\" (chat)", m.Role)
	}
	if m.CacheReuseSafe {
		t.Errorf("absent cache_reuse_safe decoded as true, want false (fail-closed)")
	}
	if m.AgentSampling != nil {
		t.Errorf("absent agent_sampling decoded non-nil, want nil")
	}
}

// TestLoadSeedChatEntriesUntouched asserts the pre-existing chat entries gained
// NO coder keys in the v3 bump (byte-untouched apart from the two
// top-level version bumps).
func TestLoadSeedChatEntriesUntouched(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	chatIDs := []string{"qwen2.5-0.5b", "qwen2.5-1.5b", "qwen3-30b-a3b", "qwen3.6-35b-a3b"}
	for _, id := range chatIDs {
		m, ok := c.FindByID(id)
		if !ok {
			t.Errorf("seed missing pre-existing chat entry %q", id)
			continue
		}
		if m.Role != "" {
			t.Errorf("chat entry %q has role %q, want absent/empty", id, m.Role)
		}
		if m.AgentCtx != 0 {
			t.Errorf("chat entry %q has agent_ctx %d, want absent/0", id, m.AgentCtx)
		}
		if m.CacheReuseSafe {
			t.Errorf("chat entry %q has cache_reuse_safe true, want absent/false", id)
		}
		if m.AgentSampling != nil {
			t.Errorf("chat entry %q has an agent_sampling block, want absent", id)
		}
		if m.TemplateProvenance != "" {
			t.Errorf("chat entry %q has template_provenance %q, want absent", id, m.TemplateProvenance)
		}
	}
}

// TestLoadSeedCoderVerifiedDims asserts the verified per-entry values from the
// 24-RESEARCH GGUF artifact table made it into the seed (the
// TestLoadSeedVerifiedDims pattern). Note qwen3-coder-next-* encodes
// n_layers=12: the FULL-ATTENTION layer count of the hybrid
// Qwen3NextForCausalLM (48 / full_attention_interval 4) — NOT 48.
func TestLoadSeedCoderVerifiedDims(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	want := map[string]struct {
		weight                           uint64
		layers, kv, head, agentCtx, tier int
		cacheReuse                       bool
	}{
		// cache_reuse_safe truth-up (24-04 / FINDING A3): the on-hardware
		// probe returned true for ALL THREE entries — for the Next hybrids the
		// reuse is via DeltaNet recurrent-state context checkpoints (75.376 MiB
		// snapshots), not n_cache_reuse chunk reuse, but --cache-reuse 256 is
		// demonstrably harmless on build 9496 (no degrade warning, turn-2
		// cache_n>0). Probe verdict is the literal catalog claim. Evidence:
		// qualification/qwen3-coder-*/{verdict.md,cache-reuse.txt}.
		"qwen3-coder-30b-a3b": {17665334432, 48, 4, 128, 65536, 64, true},
		"qwen3-coder-next-q4": {49608478720, 12, 2, 256, 131072, 128, true},
		"qwen3-coder-next-q3": {36282685440, 12, 2, 256, 131072, 96, true},
	}
	for id, w := range want {
		m, ok := c.FindByID(id)
		if !ok {
			t.Errorf("seed missing expected coder entry %q", id)
			continue
		}
		if m.WeightBytes != w.weight {
			t.Errorf("model %q weight_bytes = %d, want %d", id, m.WeightBytes, w.weight)
		}
		if m.NLayers != w.layers || m.NKVHeads != w.kv || m.HeadDim != w.head {
			t.Errorf("model %q dims = %dL/%dKV/%d, want %dL/%dKV/%d",
				id, m.NLayers, m.NKVHeads, m.HeadDim, w.layers, w.kv, w.head)
		}
		if m.AgentCtx != w.agentCtx {
			t.Errorf("model %q agent_ctx = %d, want %d", id, m.AgentCtx, w.agentCtx)
		}
		if m.TierGB != w.tier {
			t.Errorf("model %q tier_gb = %d, want %d", id, m.TierGB, w.tier)
		}
		if m.CacheReuseSafe != w.cacheReuse {
			t.Errorf("model %q cache_reuse_safe = %v, want %v (D-01/D-09: only the on-hardware probe licenses true)", id, m.CacheReuseSafe, w.cacheReuse)
		}
	}
}

// TestLoadRejectsTraversalDir asserts a directory path is rejected (and falls
// back to the seed) rather than read.
func TestLoadMissingExternalFallsBack(t *testing.T) {
	c, warnings, err := Load(filepath.Join("testdata", "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load(missing): unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("Load(missing): expected a warning about the unusable external file")
	}
	if len(c.Models) == 0 {
		t.Errorf("Load(missing): expected fallback to embedded seed (non-empty)")
	}
}

// TestLoadSchema2ExternalFallsBack asserts an external catalog at the previous
// schema (2) now warns and falls back to the embedded seed — the v1→v2
// precedent exercised for 2-vs-3 (exact-match schema window).
func TestLoadSchema2ExternalFallsBack(t *testing.T) {
	path := filepath.Join("testdata", "schema2-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(schema2): unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatalf("Load(schema2): expected a schema-mismatch warning, got none")
	}
	if !strings.Contains(strings.Join(warnings, " "), "schema_version") {
		t.Errorf("Load(schema2): warning should mention schema_version, got %v", warnings)
	}
	if _, ok := c.FindByID("schema2-chat-model"); ok {
		t.Errorf("Load(schema2): external schema-2 catalog must NOT be used")
	}
	if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
		t.Errorf("Load(schema2): expected fallback to embedded seed, but qwen2.5-1.5b not present")
	}
}

// TestLoadSchema3ExternalRoundTrip asserts an external schema-3 catalog
// carrying a coder entry with ALL five new keys decodes under
// DisallowUnknownFields and is returned by Load — the Pitfall-5 regression
// guard (struct + seed landed together; an external v3 file must round-trip).
func TestLoadSchema3ExternalRoundTrip(t *testing.T) {
	path := filepath.Join("testdata", "schema3-external.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(schema3): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(schema3): unexpected warnings: %v", warnings)
	}
	m, ok := c.FindByID("external-coder-model")
	if !ok {
		t.Fatalf("Load(schema3): external-coder-model not found (got %d models)", len(c.Models))
	}
	if m.Role != "coder" {
		t.Errorf("role = %q, want \"coder\"", m.Role)
	}
	if m.AgentCtx != 32768 {
		t.Errorf("agent_ctx = %d, want 32768", m.AgentCtx)
	}
	if !m.CacheReuseSafe {
		t.Errorf("cache_reuse_safe = false, want true (explicit key in fixture)")
	}
	if m.AgentSampling == nil {
		t.Fatalf("agent_sampling absent, want populated block")
	}
	if m.AgentSampling.Temperature != 0.7 || m.AgentSampling.TopP != 0.8 ||
		m.AgentSampling.TopK != 20 || m.AgentSampling.RepeatPenalty != 1.05 {
		t.Errorf("agent_sampling = %+v, want {0.7 0.8 20 1.05}", *m.AgentSampling)
	}
	if !strings.Contains(m.TemplateProvenance, "@") {
		t.Errorf("template_provenance = %q, want repo@revision pin", m.TemplateProvenance)
	}
}

// coderDims is the set of KV-cache sizing integers a coder entry must carry as
// positive values; a test case overrides one to exercise the guard.
type coderDims struct {
	nLayers, nKVHeads, headDim, kvBytesPerElem int
}

// goodCoderDims returns the valid KV dimensions every well-formed coder fixture
// in these tests starts from.
func goodCoderDims() coderDims { return coderDims{32, 8, 128, 2} }

// buildCoderCatalog renders a minimal schema-3 external catalog with one
// role:"coder" entry whose agent_ctx, KV dimensions, and sampling block come
// from the case under test. Shared by the refuse-whole and accept tests below.
func buildCoderCatalog(entryID string, agentCtx int, d coderDims, sampling string) string {
	return fmt.Sprintf(`{
  "schema_version": 3,
  "catalog_version": "test.invalid-coder",
  "models": [
    {
      "id": %q,
      "display_name": "Bad Coder Model",
      "quant": "Q4_K_M",
      "weight_bytes": 5000000000,
      "n_layers": %d,
      "n_kv_heads": %d,
      "head_dim": %d,
      "kv_bytes_per_elem": %d,
      "default_ctx": 16384,
      "min_envelope_bytes": 7000000000,
      "tier_gb": 16,
      "unified_memory_safe": true,
      "backend_default": "rocm",
      "bootstrap": false,
      "role": "coder",
      "agent_ctx": %d,
      "agent_sampling": %s,
      "template_provenance": "example/Bad-GGUF@0123456789abcdef0123456789abcdef01234567 (embedded GGUF chat template)"
    }
  ]
}`, entryID, d.nLayers, d.nKVHeads, d.headDim, d.kvBytesPerElem, agentCtx, sampling)
}

// TestLoadCoderValidationRefusesNeverClamps asserts the ASVS-V5 control on the
// external-catalog trust boundary: a role:"coder" entry with
// agent_ctx <= 0, a non-positive KV dimension, or out-of-range
// sampling values causes the WHOLE external catalog to be refused with a warning
// naming the offending entry id and a fallback to the embedded seed — values are
// NEVER silently clamped.
func TestLoadCoderValidationRefusesNeverClamps(t *testing.T) {
	const entryID = "bad-coder-model"
	build := func(agentCtx int, sampling string) string {
		return buildCoderCatalog(entryID, agentCtx, goodCoderDims(), sampling)
	}
	buildDims := func(d coderDims) string {
		return buildCoderCatalog(entryID, 32768, d, goodSamplingJSON)
	}
	cases := []struct {
		name string
		doc  string
	}{
		{"agent_ctx zero", build(0, goodSamplingJSON)},
		// a zeroed/omitted KV dimension collapses the KV term and must be refused.
		{"n_layers zero", buildDims(coderDims{0, 8, 128, 2})},
		{"n_kv_heads zero", buildDims(coderDims{32, 0, 128, 2})},
		{"head_dim zero", buildDims(coderDims{32, 8, 0, 2})},
		{"kv_bytes_per_elem zero", buildDims(coderDims{32, 8, 128, 0})},
		// a negative signed dimension (decodes to a huge uint64) must be refused, not silently saturated.
		{"n_layers negative", buildDims(coderDims{-1, 8, 128, 2})},
		{"head_dim negative", buildDims(coderDims{32, 8, -1, 2})},
		{"temperature above 2", build(32768, `{"temperature": 2.5, "top_p": 0.8, "top_k": 20, "repeat_penalty": 1.05}`)},
		{"top_p above 1", build(32768, `{"temperature": 0.7, "top_p": 1.5, "top_k": 20, "repeat_penalty": 1.05}`)},
		{"top_k negative", build(32768, `{"temperature": 0.7, "top_p": 0.8, "top_k": -1, "repeat_penalty": 1.05}`)},
		{"repeat_penalty above 3", build(32768, `{"temperature": 0.7, "top_p": 0.8, "top_k": 20, "repeat_penalty": 3.5}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid-coder-catalog.json")
			if err := os.WriteFile(path, []byte(tc.doc), 0o600); err != nil {
				t.Fatal(err)
			}
			c, warnings, err := Load(path)
			if err != nil {
				t.Fatalf("Load(invalid coder): unexpected error: %v", err)
			}
			if len(warnings) == 0 {
				t.Fatalf("Load(invalid coder): expected a refusal warning, got none")
			}
			joined := strings.Join(warnings, " ")
			if !strings.Contains(joined, entryID) {
				t.Errorf("refusal warning must name the offending entry %q, got %v", entryID, warnings)
			}
			// Never partially accepted: the invalid entry must not be returned.
			if _, ok := c.FindByID(entryID); ok {
				t.Errorf("invalid coder entry %q was returned — external catalog must be refused whole, never clamped", entryID)
			}
			// Fell back to the embedded seed.
			if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
				t.Errorf("expected fallback to embedded seed, but qwen2.5-1.5b not present")
			}
		})
	}
}

// TestLoadCoderAcceptsGreedyTemperature asserts: a coder entry with
// temperature == 0 (greedy/deterministic decoding — llama.cpp treats temp <= 0
// as greedy) is now ACCEPTED, not refused. The inclusive lower bound is [0, 2];
// a hand-authored deterministic coder preset must NOT silently fall the whole
// external catalog back to the embedded seed.
func TestLoadCoderAcceptsGreedyTemperature(t *testing.T) {
	const entryID = "greedy-coder-model"
	greedySampling := `{"temperature": 0, "top_p": 0.8, "top_k": 20, "repeat_penalty": 1.05}`
	doc := buildCoderCatalog(entryID, 32768, goodCoderDims(), greedySampling)

	path := filepath.Join(t.TempDir(), "greedy-coder-catalog.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(greedy coder): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(greedy coder): temperature 0 must be accepted, got warnings: %v", warnings)
	}
	m, ok := c.FindByID(entryID)
	if !ok {
		t.Fatalf("Load(greedy coder): expected the external catalog to be used (%q not found); got %d models", entryID, len(c.Models))
	}
	if m.AgentSampling == nil || m.AgentSampling.Temperature != 0 {
		t.Errorf("greedy coder temperature not preserved: %+v", m.AgentSampling)
	}
}

// TestLoadSchema4NgramExternal asserts a schema-4 external catalog round-trips
// both shapes of the qualification: an entry that declares ngram_safe with its
// provenance, and one that carries neither key at all.
func TestLoadSchema4NgramExternal(t *testing.T) {
	path := filepath.Join("testdata", "ngram-external.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(ngram-external): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(ngram-external): unexpected warnings: %v", warnings)
	}
	qualified, ok := c.FindByID("external-qualified-model")
	if !ok {
		t.Fatalf("external-qualified-model not found (got %d models)", len(c.Models))
	}
	if !qualified.NgramSafe {
		t.Errorf("ngram_safe = false, want true (explicit key in fixture)")
	}
	if !strings.Contains(qualified.NgramProvenance, "gfx1151") {
		t.Errorf("ngram_provenance = %q, want the measurement text", qualified.NgramProvenance)
	}
	plain, ok := c.FindByID("external-unqualified-model")
	if !ok {
		t.Fatalf("external-unqualified-model not found")
	}
	if plain.NgramSafe || plain.NgramProvenance != "" {
		t.Errorf("an entry with no ngram keys decoded as %v/%q, want false/\"\"", plain.NgramSafe, plain.NgramProvenance)
	}
}

// TestLoadNgramValidationRejectsUnprovenanced asserts the fail-closed
// qualification: ngram_safe is licensed only by a recorded measurement, so an
// entry claiming it with no provenance invalidates the WHOLE external catalog and
// falls back to the seed rather than being accepted or silently downgraded.
func TestLoadNgramValidationRejectsUnprovenanced(t *testing.T) {
	body := `{
  "schema_version": 4,
  "catalog_version": "test.invalid-ngram",
  "models": [
    {
      "id": "unproven-model",
      "display_name": "Unproven Model",
      "quant": "Q4_K_M",
      "weight_bytes": 5000000000,
      "n_layers": 32,
      "n_kv_heads": 8,
      "head_dim": 128,
      "kv_bytes_per_elem": 2,
      "default_ctx": 16384,
      "min_envelope_bytes": 7000000000,
      "tier_gb": 16,
      "unified_memory_safe": true,
      "backend_default": "rocm",
      "bootstrap": false,
      "ngram_safe": true
    }
  ]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	joined := strings.Join(warnings, " ")
	if !strings.Contains(joined, "unproven-model") {
		t.Errorf("refusal should name the offending entry, got %v", warnings)
	}
	if _, ok := c.FindByID("unproven-model"); ok {
		t.Errorf("an unprovenanced ngram_safe entry must not be used")
	}
	if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
		t.Errorf("expected fallback to the embedded seed")
	}
}

// TestSeedNgramQualificationsCarryProvenance asserts the seed itself honours the
// rule the external validator enforces: absence never widens what villa will do,
// so a compiled-in qualification must name the measurement that licensed it.
func TestSeedNgramQualificationsCarryProvenance(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	qualified := 0
	for _, m := range c.Models {
		if !m.NgramSafe {
			if m.NgramProvenance != "" {
				t.Errorf("seed entry %q carries ngram_provenance %q without ngram_safe", m.ID, m.NgramProvenance)
			}
			continue
		}
		qualified++
		if !strings.Contains(m.NgramProvenance, "gfx1151") {
			t.Errorf("seed entry %q ngram_provenance = %q, want a gfx1151 measurement", m.ID, m.NgramProvenance)
		}
	}
	if qualified == 0 {
		t.Errorf("no seed entry is qualified for ngram; the measured pair should be")
	}
}
