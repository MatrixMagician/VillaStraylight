package catalog

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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
		"qwen2.5-0.5b": {24, 2, 64},
		"qwen2.5-1.5b": {28, 2, 128},
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
// role:"coder" entries (CODER-01, D-02), each with an agent-profile context,
// a repo@revision template-provenance pin, and a single shard whose URL is
// revision-pinned (`resolve/{40-hex}` — never `resolve/main/`, T-24-01).
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
			t.Errorf("coder entry %q has agent_ctx %d, want > 0 (D-01/D-04)", m.ID, m.AgentCtx)
		}
		if m.TemplateProvenance == "" || !strings.Contains(m.TemplateProvenance, "@") {
			t.Errorf("coder entry %q template_provenance = %q, want non-empty repo@revision pin (D-02)", m.ID, m.TemplateProvenance)
		}
		if len(m.Shards) != 1 {
			t.Errorf("coder entry %q has %d shards, want exactly 1", m.ID, len(m.Shards))
			continue
		}
		u := m.Shards[0].URL
		if strings.Contains(u, "/resolve/main/") {
			t.Errorf("coder entry %q shard URL %q uses /resolve/main/ — must be revision-pinned (D-02, T-24-01)", m.ID, u)
		}
		if !revisionURL.MatchString(u) {
			t.Errorf("coder entry %q shard URL %q lacks a /resolve/{40-hex}/ revision pin (D-02, T-24-01)", m.ID, u)
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

// TestCatalogModelFailClosedDefaults asserts the D-01 fail-closed decode
// defaults: a CatalogModel decoded from JSON WITHOUT role / cache_reuse_safe
// keys yields Role == "" (treated as chat) and CacheReuseSafe == false —
// absence never widens capability.
func TestCatalogModelFailClosedDefaults(t *testing.T) {
	raw := `{"id":"no-coder-keys","quant":"Q4_K_M","weight_bytes":1000}`
	var m CatalogModel
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal minimal entry: %v", err)
	}
	if m.Role != "" {
		t.Errorf("absent role decoded as %q, want \"\" (chat, D-01/D-03)", m.Role)
	}
	if m.CacheReuseSafe {
		t.Errorf("absent cache_reuse_safe decoded as true, want false (fail-closed, D-01)")
	}
	if m.AgentSampling != nil {
		t.Errorf("absent agent_sampling decoded non-nil, want nil")
	}
}

// TestLoadSeedChatEntriesUntouched asserts the pre-existing chat entries gained
// NO coder keys in the v3 bump (D-03 byte-untouched apart from the two
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
			t.Errorf("seed missing pre-existing chat entry %q (D-03)", id)
			continue
		}
		if m.Role != "" {
			t.Errorf("chat entry %q has role %q, want absent/empty (D-03)", id, m.Role)
		}
		if m.AgentCtx != 0 {
			t.Errorf("chat entry %q has agent_ctx %d, want absent/0 (D-03)", id, m.AgentCtx)
		}
		if m.CacheReuseSafe {
			t.Errorf("chat entry %q has cache_reuse_safe true, want absent/false (D-03)", id)
		}
		if m.AgentSampling != nil {
			t.Errorf("chat entry %q has an agent_sampling block, want absent (D-03)", id)
		}
		if m.TemplateProvenance != "" {
			t.Errorf("chat entry %q has template_provenance %q, want absent (D-03)", id, m.TemplateProvenance)
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
		weight                          uint64
		layers, kv, head, agentCtx, tier int
		cacheReuse                      bool
	}{
		"qwen3-coder-30b-a3b": {17665334432, 48, 4, 128, 65536, 64, true},
		"qwen3-coder-next-q4": {49608478720, 12, 2, 256, 131072, 128, false},
		"qwen3-coder-next-q3": {36282685440, 12, 2, 256, 131072, 96, false},
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
