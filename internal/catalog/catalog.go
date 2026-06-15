// Package catalog provides the versioned model catalog that `villa recommend`
// reads to choose a fitting model/quant/context. The default catalog is embedded
// in the binary via go:embed (D-09); an external override path may be supplied
// (config key or `--catalog`) and is schema-version-checked with a graceful
// fallback to the embedded seed (D-11/D-13) — a version mismatch or a malformed
// external file warns but never crashes.
//
// The schema carries the per-model dimensions the KV-cache fit math needs
// (n_layers / n_kv_heads / head_dim / kv_bytes_per_elem), a unified_memory_safe
// flag (entries flagged false are never auto-selected on gfx1151, REC-02), and a
// bootstrap flag for the small first-chat model carried forward for Phase 4 (D-12,
// presence only — never auto-selected in Phase 1).
package catalog

// SupportedSchema is the catalog schema_version this binary understands. An
// external catalog whose schema_version differs is rejected with a warning and
// the embedded seed is used instead (D-11). Bump this only on an incompatible
// schema change.
//
// v2 (Phase 2, D-07): adds the per-shard download metadata each CatalogModel
// carries (Shards: URL + expected SHA256 + expected size) so `villa model pull`
// can download+verify a GGUF without delegating to llama.cpp -hf (MODEL-02).
//
// v3 (Phase 24, D-01): adds the optional coder-role fields each CatalogModel
// may carry (role, agent_ctx, cache_reuse_safe, agent_sampling,
// template_provenance) so the coder fit stage and the Phase-25 render can
// consume qualified coding-model entries (CODER-01). All five are optional
// with fail-closed absence semantics: an entry without them is a chat entry.
const SupportedSchema = 3

// Catalog is the top-level catalog document. schema_version gates parser
// compatibility; catalog_version is informational data-freshness metadata.
type Catalog struct {
	SchemaVersion  int            `json:"schema_version"`
	CatalogVersion string         `json:"catalog_version"`
	Models         []CatalogModel `json:"models"`
}

// CatalogModel is a single catalog entry. The byte/dimension fields are the
// inputs to the recommend fit math (model_bytes + KV-cache@ctx + headroom ≤
// usable_envelope).
//
// NOTE (Assumption A2): the seed weight_bytes / n_layers / n_kv_heads / head_dim
// values are MEDIUM-confidence placeholders illustrating the schema and exercising
// the fit-math MECHANISM — they must be replaced with real GGUF metadata for each
// pinned model at catalog-authoring time. The mechanism (schema + KV math) is HIGH
// confidence; the seed numbers are not.
type CatalogModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Quant       string `json:"quant"`

	// Fit-math inputs.
	WeightBytes    uint64 `json:"weight_bytes"`
	NLayers        int    `json:"n_layers"`
	NKVHeads       int    `json:"n_kv_heads"` // KV heads (GQA), NOT attention heads — Pitfall 4.
	HeadDim        int    `json:"head_dim"`
	KVBytesPerElem int    `json:"kv_bytes_per_elem"` // bytes per KV element at this quant (e.g. 2 = f16).

	DefaultCtx       int    `json:"default_ctx"`
	MinEnvelopeBytes uint64 `json:"min_envelope_bytes"`
	TierGB           int    `json:"tier_gb"`

	// UnifiedMemorySafe gates auto-selection: entries flagged false are never
	// auto-picked on gfx1151 (REC-02). A user --model override of a flagged model
	// is allowed but warns loudly (D-07).
	UnifiedMemorySafe bool `json:"unified_memory_safe"`

	// BackendDefault is the recommended backend for this entry; vulkan for
	// gfx1151 (REC-04).
	BackendDefault string `json:"backend_default"`

	// Bootstrap marks the small first-chat model carried forward for Phase 4
	// (D-12). It is present in the Phase-1 catalog but never auto-selected.
	Bootstrap bool `json:"bootstrap"`

	// Role marks what the entry is for (schema v3, D-01). Absent/empty means
	// chat (D-03 — pre-v3 entries are untouched and stay chat); "coder" marks
	// an entry the coder fit stage may select. Absence never widens capability.
	Role string `json:"role,omitempty"`

	// AgentCtx is the agent-profile context window (schema v3, D-01/D-04): the
	// ctx the coder fit math is computed at AND the value Phase 25 renders into
	// --ctx-size for coding mode. It is independent of default_ctx and is NEVER
	// overridden by the chat-only --ctx flag.
	AgentCtx int `json:"agent_ctx,omitempty"`

	// CacheReuseSafe marks whether llama.cpp --cache-reuse is proven safe for
	// this entry (schema v3, D-01). The Go zero value false is the fail-closed
	// default: absence means NOT safe. Only the on-hardware qualification probe
	// licenses true (D-09) — never an upstream claim.
	CacheReuseSafe bool `json:"cache_reuse_safe,omitempty"`

	// AgentSampling is the qualified sampling preset for agentic/tool-call use
	// (schema v3, D-01). A pointer so an absent block stays absent on re-encode
	// (chat entries emit no key, D-03).
	AgentSampling *AgentSampling `json:"agent_sampling,omitempty"`

	// TemplateProvenance pins where the chat template came from (schema v3,
	// D-01/D-02): the HuggingFace repo@revision plus the template origin. The
	// embedded GGUF chat template is part of the qualified artifact — a repo
	// re-upload under the same quant name is a different artifact (T-24-01).
	TemplateProvenance string `json:"template_provenance,omitempty"`

	// Shards is the per-shard download manifest (schema v2, D-05/D-06). A
	// single-file model is the degenerate one-element case; large quants split
	// into the HuggingFace `-00001-of-0000N.gguf` convention carry one Shard per
	// file. `villa model pull` downloads + checksum-verifies every shard and
	// rejects the model unless all shards are present and individually verified.
	Shards []Shard `json:"shards,omitempty"`
}

// Shard is one downloadable GGUF file for a model. The values come from a
// HuggingFace `resolve/main/<file>` HEAD: URL is the canonical resolve URL,
// SHA256 is the file's git-LFS oid (HF `X-Linked-Etag` — NOT the CDN/Xet chunk
// ETag, Pitfall 2), and SizeBytes is the exact content length (`X-Linked-Size`).
// Filename is the on-disk name the downloader writes inside the models dir; it is
// confined with a path-traversal guard before any write (T-02-02).
type Shard struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	SizeBytes uint64 `json:"size_bytes"`
}

// AgentSampling is the qualified sampling preset for a coder entry (schema v3,
// D-01). The seed values come from the model card's recommended agentic/coding
// preset and are checked during on-hardware qualification (A4) — they are
// catalog data, not runtime-tunable knobs.
type AgentSampling struct {
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
	TopK          int     `json:"top_k"`
	RepeatPenalty float64 `json:"repeat_penalty"`
}

// FindByID returns the model with the given id and whether it was found.
func (c Catalog) FindByID(id string) (CatalogModel, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return CatalogModel{}, false
}
