# Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification - Pattern Map

**Mapped:** 2026-06-12
**Files analyzed:** 9 new/modified files
**Analogs found:** 8 / 9 (the on-hardware qualification scripts have no Go analog — they are phase-doc artifacts specified by 24-RESEARCH.md Pattern 4)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/catalog/catalog.go` | model (schema struct + version constant) | data definition | itself — the v1→v2 `Shards` append (lines 15-23, 71-90) | exact |
| `internal/catalog/load.go` | config loader | file I/O (bounded read + decode) | itself — exact-match schema window (lines 35-58) | exact |
| `internal/catalog/seed.json` | config/data (embedded catalog) | static data | itself — `qwen3-30b-a3b` entry shape (lines 53-76) | exact |
| `internal/catalog/catalog_test.go` (extend) | test | table-driven unit | `TestLoadSeedDownloadMetadata` (lines 52-83), `TestLoadVersionMismatchFallsBack` (lines 162-189) | exact |
| `internal/recommend/recommend.go` | service (pure core) | transform (fit math pipeline) | itself — `pickBest` (lines 275-332) + `finalizeRecommendation` (lines 209-227) + D-03 memory-fields precedent (lines 103-115) | exact |
| `internal/recommend/coder.go` (optional new file) or in-place in recommend.go | service (pure core helper) | transform | `internal/recommend/kv.go` (small, doc-commented, single-purpose helper file) | exact |
| `internal/recommend/recommend_test.go` (extend, or sibling `coder_test.go`) | test | table-driven unit | `TestPickMemoryReservation` (lines 359-444), `TestPickRefusalStampsMemoryFields` (lines 469-500) | exact |
| `cmd/villa/recommend.go` | controller (cobra render tier) | request-response (CLI render) | itself — gated embed-reservation row (lines 158-163) + gated ROCm advice block (lines 178-187) | exact |
| `cmd/villa/recommend_test.go` + `cmd/villa/testdata/recommend.golden.json` | test (golden contract) | byte-frozen contract | `fixtureRecommendation` + `TestRecommendJSONGolden` (lines 19-71) | exact |
| `.planning/phases/24-*/` qualification scripts + evidence | docs/scripts (dev-time, NOT product code) | manual on-hardware protocol | none in Go — follow 24-RESEARCH.md Pattern 4 + MEM-DOC measurement pattern | no-analog (by design) |

## Pattern Assignments

### `internal/catalog/catalog.go` (model, schema struct)

**Analog:** itself — the schema v1→v2 evolution is the exact precedent for 2→3.

**Schema-version constant + versioned doc comment** (lines 15-23) — bump to 3 and append a v3 paragraph exactly like the v2 one:
```go
// SupportedSchema is the catalog schema_version this binary understands. An
// external catalog whose schema_version differs is rejected with a warning and
// the embedded seed is used instead (D-11). Bump this only on an incompatible
// schema change.
//
// v2 (Phase 2, D-07): adds the per-shard download metadata each CatalogModel
// carries (Shards: URL + expected SHA256 + expected size) so `villa model pull`
// can download+verify a GGUF without delegating to llama.cpp -hf (MODEL-02).
const SupportedSchema = 2
```

**Optional-field convention to copy for the new coder fields** (lines 71-77) — doc comment naming the decision ID, `omitempty` so existing entries stay byte-untouched:
```go
	// Shards is the per-shard download manifest (schema v2, D-05/D-06). A
	// single-file model is the degenerate one-element case; large quants split
	// into the HuggingFace `-00001-of-0000N.gguf` convention carry one Shard per
	// file. ...
	Shards []Shard `json:"shards,omitempty"`
```
New fields follow this shape: `Role string \`json:"role,omitempty"\``, `AgentCtx int \`json:"agent_ctx,omitempty"\``, `CacheReuseSafe bool \`json:"cache_reuse_safe,omitempty"\`` (Go zero `false` = fail-closed for free, D-01), an `AgentSampling` nested struct, `TemplateProvenance string \`json:"template_provenance,omitempty"\``. Each carries a doc comment citing D-01/D-02 the way `UnifiedMemorySafe` cites REC-02/D-07 (lines 59-61).

**Nested-struct convention if AgentSampling is a struct** — copy the `Shard` shape (lines 85-90): exported struct, snake_case JSON tags, field-by-field doc comment with provenance notes.

**Stale-comment cleanup:** the `NOTE (Assumption A2)` placeholder-values caveat in the `CatalogModel` doc comment (lines 37-41) predates the verified seed values — do not replicate it for coder entries; their values come from the verified 24-RESEARCH artifact table.

### `internal/catalog/load.go` (config loader, schema window)

**Analog:** itself. NO code change is structurally required beyond the constant bump in catalog.go — the window is exact-match equality.

**The schema window the bump flows through** (lines 38-49):
```go
	if externalPath != "" {
		ext, err := loadExternal(externalPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("catalog: external catalog %q unusable (%v) — using embedded seed", externalPath, err))
			// fall through to embedded seed below
		} else if ext.SchemaVersion != SupportedSchema {
			warnings = append(warnings, schemaMismatchWarning(externalPath, ext.SchemaVersion))
			// fall through to embedded seed below
		} else {
			return ext, warnings, nil
		}
	}
```

**DisallowUnknownFields trap** (lines 97-101) — the reason struct fields + seed bump MUST land in ONE commit (RESEARCH Pitfall 5):
```go
	var c Catalog
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields() // reject unexpected keys defensively
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
```

### `internal/catalog/seed.json` (embedded catalog, schema 3)

**Analog:** the existing `qwen3-30b-a3b` entry (lines 53-76) — copy its exact key set and order, then append the new coder keys after `bootstrap` and before `shards`:
```json
    {
      "id": "qwen3-30b-a3b",
      "display_name": "Qwen3-30B-A3B (MoE, 64 GB tier)",
      "quant": "Q4_K_M",
      "weight_bytes": 18556686912,
      "n_layers": 48,
      "n_kv_heads": 4,
      "head_dim": 128,
      "kv_bytes_per_elem": 2,
      "default_ctx": 131072,
      "min_envelope_bytes": 30000000000,
      "tier_gb": 64,
      "unified_memory_safe": true,
      "backend_default": "vulkan",
      "bootstrap": false,
      "shards": [
        {
          "url": "https://huggingface.co/unsloth/Qwen3-30B-A3B-GGUF/resolve/main/Qwen3-30B-A3B-Q4_K_M.gguf",
          "filename": "Qwen3-30B-A3B-Q4_K_M.gguf",
          "sha256": "9f1a24700a339b09c06009b729b5c809e0b64c213b8af5b711b3dbdfd0c5ba48",
          "size_bytes": 18556686912
        }
      ]
    }
```
Deltas for coder entries (values from 24-RESEARCH §GGUF Artifact Table — already verified, no placeholders):
- Shard `url` uses `resolve/{revision}/…` (D-02), NOT `resolve/main/…` as the existing chat entries do.
- Top-level: `"schema_version": 3` (line 2) and bump `"catalog_version"` (line 3).
- Existing 4 chat entries gain NO keys (D-03 byte-untouched apart from the two top-level bumps).
- Qwen3-Coder-Next encodes `"n_layers": 12` (full-attention layers, NOT 48) — RESEARCH Pattern 3; record why in `display_name`/phase docs.

### `internal/catalog/catalog_test.go` (schema-3 tests)

**Analog:** `TestLoadSeedDownloadMetadata` — the schema-2 contract test that pins the schema constant AND walks every entry asserting the new fields (lines 52-63):
```go
func TestLoadSeedDownloadMetadata(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if SupportedSchema != 2 {
		t.Fatalf("SupportedSchema = %d, want 2 (schema bumped for download fields)", SupportedSchema)
	}
	...
	for _, m := range c.Models {
		if len(m.Shards) == 0 { ... }
```
Copy this shape for a `TestLoadSeedCoderEntries` (schema=3 pin, coder entries present with role/agent_ctx/provenance, chat entries with `Role == ""`). NOTE: `TestLoadSeedDownloadMetadata` line 57-58 hard-pins `SupportedSchema = 2` — the bump will break it; update that assertion to 3 in the same commit.

**Fail-closed/role-filter defaults test analog:** `TestLoadSeedVerifiedDims` (lines 88-109) — map-of-expected-values walk per entry ID; reuse for per-entry `agent_ctx`/`cache_reuse_safe` expectations.

**Schema-fallback test analog:** `TestLoadVersionMismatchFallsBack` (lines 162-189) — the existing `testdata/too-old-catalog.json` fixture pattern covers "external schema-2 file now warns + falls back"; add/adjust fixtures so a schema-2 external file exercises the older-than branch, plus an external schema-3 round-trip fixture (Pitfall 5, `DisallowUnknownFields`).

### `internal/recommend/recommend.go` (pure core: coder fit stage + schema 3)

**Analog:** itself — the Phase-22 memory-reservation additions are the byte-for-byte precedent for every move this phase makes.

**Schema constant bump** (lines 27-32) — bump to 3, append the history line exactly like the 1→2 note:
```go
// recommendSchemaVersion is the Recommendation contract self-version. It is the
// LAST tagged field of Recommendation and surfaces unconditionally in --json so
// dashboards can gate on additive growth (D-06/D-07). Bumped 1→2 when the
// append-only embedding_reservation_bytes + memory_considered fields landed
// (Phase 22, D-03).
const recommendSchemaVersion = 2
```

**Contract-field placement precedent** (lines 103-115) — the new `Coder` block goes here, ABOVE `SchemaVersion`, NOT `omitempty` (D-07 always-stamped; RESEARCH Pitfall 6):
```go
	// EmbeddingReservationBytes is the embedding-model footprint subtracted from
	// the envelope BEFORE the chat-model fit when memory is enabled (D-01/D-03).
	// Zero when memory is off — the off-path JSON shape changes only by this key.
	EmbeddingReservationBytes uint64 `json:"embedding_reservation_bytes"`

	// MemoryConsidered marks whether the memory reservation was applied to this
	// pick (D-03): true whenever memory inputs were enabled — including refusals,
	// which honestly report the reservation they would have applied.
	MemoryConsidered bool `json:"memory_considered"`

	// SchemaVersion is the Recommendation contract self-version and MUST stay the
	// LAST tagged field (append-only discipline; new fields go above it, D-06/D-07).
	SchemaVersion int `json:"schema_version"`
```

**Unconditional-stamp point** (lines 209-227) — `finalizeRecommendation` is where the coder fit stage runs/stamps; every `Pick` return path already flows through it (including the line-160 no-envelope refusal):
```go
func finalizeRecommendation(rec Recommendation, p detect.HostProfile, mem MemoryInputs, reservation uint64) Recommendation {
	rec.SchemaVersion = recommendSchemaVersion
	rec.EmbeddingReservationBytes = reservation
	rec.MemoryConsidered = mem.Enabled
	advice, note := deriveROCmAdvice(p.ROCmReadiness)
	rec.ROCmAdvice = advice
	if note != "" {
		rec.ROCmNote = note
	}
	return rec
}
```
The coder stage needs the catalog + post-reservation envelope, which `finalizeRecommendation` does not currently receive — either extend its signature (it has exactly 3 call sites: lines 160, 184, 187) or compute the coder block in `Pick` after the envelope shrink (lines 169-174) and pass it through. On the no-envelope refusal path stamp `fits:false, residency:"shared"` (CONTEXT D-06 conservative floor).

**Fit-loop pattern to copy for `pickCoder`** — `pickBest`'s eligibility-filter + most-capable-wins loop (lines 282-307):
```go
	for i := range c.Models {
		m := c.Models[i]
		if m.Bootstrap {
			continue // never auto-select the bootstrap entry (D-12)
		}
		if !m.UnifiedMemorySafe {
			continue // never auto-select a unified-memory-unsafe entry (REC-02)
		}
		if m.MinEnvelopeBytes > 0 && envelope < m.MinEnvelopeBytes {
			continue // secondary floor guard ...
		}
		ctx := effectiveCtx(m, ov)
		total := m.WeightBytes + kvCacheBytes(m, ctx) + headroom
		if total > envelope {
			continue // OOM guard: never select a pick that exceeds the envelope
		}
		...
		// "Best" = the largest footprint that still fits (most capable).
		if best == nil || total > bestTotal {
```
Coder-stage deltas: filter is `m.Role != "coder"` → continue (and `pickBest` gains the inverse filter `m.Role == "coder"` → continue, D-03); ctx is `m.AgentCtx` — NEVER `effectiveCtx(m, ov)` (D-04, `--ctx` is chat-only); respect `UnifiedMemorySafe` and `MinEnvelopeBytes` identically; total must use the saturating form from `buildRecommendation` line 377, not the bare `+` of line 296:
```go
	total := addSaturating(addSaturating(m.WeightBytes, kv), headroom)
```

**Honest no-fit precedent** (lines 309-317) — the coder stage mirrors this with `residency:"shared"` instead of a refusal:
```go
	if best == nil {
		notes = append(notes, fmt.Sprintf("no catalog model fits the usable envelope of %s (with %.0f%% headroom) — consider a smaller model or larger memory", humanGiB(envelope), headroomFraction*100))
```

**Override-of-coder-entry note precedent** (open question 1) — the warn-and-allow unsafe-override note in `pickOverride` (lines 349-351) is the template if planner chooses warn-and-allow:
```go
	if !m.UnifiedMemorySafe {
		notes = append(notes, fmt.Sprintf("WARNING: model %q is flagged unified_memory_safe:false — it is known to misbehave on unified memory; using it only because you overrode (D-07)", m.ID))
	}
```

### `internal/recommend/coder.go` (if the fit stage gets its own file)

**Analog:** `internal/recommend/kv.go` — the house style for a small single-purpose helper file: package-internal, no new imports beyond catalog, doc comment citing source + decision IDs, saturating math (lines 26-45 `kvCacheBytes`, lines 51-57 `addSaturating`). Reuse `kvCacheBytes`/`addSaturating`/`headroomBytes` (envelope.go lines 20-22) VERBATIM — do not fork the formula; Next's hybrid KV is handled by `n_layers:12` data encoding, not a formula branch (RESEARCH Pattern 3).

### `internal/recommend/recommend_test.go` (coder fit tests)

**Analog 1 — fixture catalog builder** (lines 14-46): extend `testCatalog()` (or a sibling builder) with coder entries the same way:
```go
func testCatalog() catalog.Catalog {
	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models: []catalog.CatalogModel{
			{
				ID: "tiny", Quant: "Q4_K_M", WeightBytes: 4 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "vulkan",
			},
			...
```

**Analog 2 — subtest matrix for a new contract stage** (`TestPickMemoryReservation`, lines 359-382): named `t.Run` subtests per behavior, asserting BOTH the math and the stamped contract fields:
```go
	t.Run("memory off: zero-value inputs leave envelope untouched, fields zero/false", func(t *testing.T) {
		rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{})
		if rec.UsableEnvelopeBytes != env {
			t.Errorf("UsableEnvelopeBytes = %d, want untouched %d (memory off must be byte-identical math)", rec.UsableEnvelopeBytes, env)
		}
		...
		if rec.SchemaVersion != 2 {
			t.Errorf("SchemaVersion = %d, want 2 (D-03 bump)", rec.SchemaVersion)
		}
```
Coder mirror: swap-when-fits, shared-when-none-fit, fit-at-`agent_ctx`-not-`--ctx`, chat pick unchanged with coder entries present (D-03 bit-identical), `SchemaVersion == 3` on every path. NOTE: existing tests assert `SchemaVersion != 2` at lines 376-378, 483-485, 497-499 — all flip to 3 in the same commit as the bump.

**Analog 3 — refusal-path stamping** (`TestPickRefusalStampsMemoryFields`, lines 469-500) — the exact template for "coder block present with fits:false/residency:shared on the no-envelope refusal" (Pitfall 6):
```go
func TestPickRefusalStampsMemoryFields(t *testing.T) {
	p := detect.HostProfile{
		TotalRAMBytes:       detect.UnknownBytes("ram unknown", ""),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unknown", ""),
	}
	...
	off := Pick(p, cat, Overrides{}, MemoryInputs{})
	if off.Model != "" {
		t.Fatalf("precondition: expected refusal, got %q", off.Model)
	}
```

**Analog 4 — note assertion helper** (lines 502-509): reuse `hasNote(notes, substr)` as-is.

### `cmd/villa/recommend.go` (human-table coder section)

**Analog:** itself — two gated-rendering precedents to copy for the coder section.

**Gated table row** (lines 158-163) — keeps prior output byte-identical when the feature is inert:
```go
	// Embed-reservation row gated on a non-zero value (D-03 / Pitfall 4, the
	// ROCmAdvice gated-line pattern) so memory-off table output stays byte-identical.
	if rec.EmbeddingReservationBytes > 0 {
		fmt.Fprintf(tw, "− embed reservation\t%s\n", gib(rec.EmbeddingReservationBytes))
	}
```

**Gated post-notes block** (lines 178-187) — the shape for a "Coder:" section after the notes (model/quant/agent ctx/residency/fits):
```go
	if rec.ROCmAdvice != "" {
		fmt.Fprintf(w, "\nROCm advice: %s\n", rec.ROCmAdvice)
		if rec.ROCmNote != "" {
			fmt.Fprintf(w, "  - %s\n", rec.ROCmNote)
		}
	}
```
Caveat: the JSON coder block is ALWAYS stamped (D-07), but the human table may gate rendering on content (Claude's discretion per CONTEXT). Use `gib()` (lines 216-218) and `fitsGlyph()` (lines 221-226) for fit-term rows.

**JSON path needs no change** (lines 123-130) — `renderRecommend` encodes the struct directly; the new block flows through automatically:
```go
func renderRecommend(w io.Writer, rec recommend.Recommendation, warnings []string, asJSON, withAlternatives bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rec)
	}
	return renderRecommendTable(w, rec, warnings, withAlternatives)
}
```
`saveRecommendation` (lines 98-118) persists chat fields only — the coder block is NOT persisted this phase (config/render is Phase 25).

### `cmd/villa/recommend_test.go` + `testdata/recommend.golden.json` (the ONE re-freeze)

**Analog:** `fixtureRecommendation` + `TestRecommendJSONGolden` (lines 19-71). The fixture builds the struct directly (does not call `Pick`) and pins `SchemaVersion` explicitly with a schema-history comment (lines 33-40):
```go
		// SchemaVersion surfaces unconditionally in --json (D-06/D-07). The fixture
		// builds the struct directly (it does not call Pick), so it pins the contract
		// version explicitly; advice fields stay empty (no readiness fixture) and so
		// remain absent under omitempty. Schema 2 (Phase 22, D-03): the append-only
		// embedding_reservation_bytes + memory_considered keys surface as zero/false
		// here — the memory-off contract shape.
		SchemaVersion: 2,
```
Phase-24 deltas: fixture gains a POPULATED coder block (so the frozen bytes exercise the full schema-3 shape — RESEARCH golden-re-freeze note) and `SchemaVersion: 3` with an appended history sentence.

**Golden update mechanics** (lines 52-62) — uses the package-level `update` flag declared once in `cmd/villa/detect_test.go:13` (`var update = flag.Bool("update", false, "regenerate golden files")`); do NOT redeclare it:
```go
	golden := filepath.Join("testdata", "recommend.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
```
Re-freeze command (exactly once this phase): `go test ./cmd/villa -run Recommend -update`, then review that the diff is a pure addition above `"schema_version": 3`. Current golden shape (target: insert `"coder": {…}` between `memory_considered` and `schema_version`):
```json
  "embedding_reservation_bytes": 0,
  "memory_considered": false,
  "schema_version": 2
}
```

**Table-surface test analog** (`TestRecommendTableShowsFitMath`, lines 75-86) — substring-contains assertions over the rendered table; copy for the coder section's table strings.

## Shared Patterns

### Append-only byte-frozen contract evolution
**Source:** `internal/recommend/recommend.go` lines 27-32 + 113-115; `internal/catalog/catalog.go` lines 15-23
**Apply to:** both schema bumps (catalog 2→3, recommend 2→3)
Rules carried by the precedent: new fields go ABOVE `SchemaVersion`/before `shards`; constant gets a versioned history paragraph; struct + seed + constant + test-pin updates land in ONE commit; exactly one golden re-freeze.

### Unconditional contract stamping (`finalizeRecommendation`)
**Source:** `internal/recommend/recommend.go` lines 209-227 (3 call sites: 160, 184, 187)
**Apply to:** the coder block (D-07)
Every `Pick` return path flows through one stamping function — refusals included. NEVER `omitempty` on an always-stamped contract field (Pitfall 6); contrast with the deliberately-`omitempty` ROCm advice fields (lines 100-101), which are the WRONG precedent for the coder block.

### Fail-closed optional fields (typed-Unknown discipline applied to JSON)
**Source:** `internal/catalog/catalog.go` lines 59-69 (`UnifiedMemorySafe`/`Bootstrap` gating comments)
**Apply to:** `role` (absent ⇒ chat), `cache_reuse_safe` (absent ⇒ Go zero `false`)
Absence never widens capability; gating comments cite the decision ID.

### Saturating fit math (WR-07)
**Source:** `internal/recommend/kv.go` lines 26-57 (`kvCacheBytes`, `addSaturating`); `internal/recommend/recommend.go` line 377 (saturating total); `internal/recommend/envelope.go` lines 20-22 (`headroomBytes`)
**Apply to:** the coder fit total at `agent_ctx`
Reuse verbatim — no second formula, no formula branch for hybrid models (data-encode `n_layers:12` instead).

### Honest notes with decision-ID provenance
**Source:** `internal/recommend/recommend.go` lines 310, 350, 362-364 (note format strings naming D-07/REC-02 and using `humanGiB`)
**Apply to:** any new coder-stage notes (e.g., shared-mode explanation, coder-override warning)

### Golden `-update` discipline
**Source:** `cmd/villa/detect_test.go:13` (single package-level flag declaration); `cmd/villa/recommend_test.go` lines 52-62
**Apply to:** the one recommend re-freeze
Never redeclare the flag; never refreeze twice in the phase.

### Seam grep-gate constraint (negative pattern)
**Source:** CLAUDE.md / `internal/inference/seam_test.go` (`TestSeamGrepGate` walks `internal/` + `cmd/villa`)
**Apply to:** ALL Go files this phase
No image digests, `podman` literals, or backend markers in any Phase-24 Go change. The qualification protocol's `podman run … @sha256:9a74e555…` commands live in phase-dir scripts/docs ONLY.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `.planning/phases/24-*/` qualification scripts + evidence (Crush agent loop, KV/GTT measurement, cache-reuse probe, D-11 decision record) | dev-time scripts/docs | manual on-hardware protocol | No prior phase ships agent-in-the-loop qualification. The spec is 24-RESEARCH.md Pattern 4 (11-step protocol with concrete commands) + §Code Examples (tool-call smoke test, qual `crush.json`, GTT capture). Measurement reuses the shipped MEM-DOC dual-assert idea (`mem_info_gtt_used` delta + `Vulkan0`/`offloaded N/N` log lines) as a procedure, not as code. |

## Metadata

**Analog search scope:** `internal/catalog/`, `internal/recommend/`, `cmd/villa/` (the complete modification surface per CONTEXT §Integration Points; all analogs are the files being modified — this phase is precedent-reuse by design)
**Files scanned:** 11 read in full (catalog.go, load.go, seed.json, catalog_test.go, recommend.go, kv.go, envelope.go, recommend_test.go, cmd/villa/recommend.go, cmd/villa/recommend_test.go, recommend.golden.json) + grep for the `-update` flag declaration
**Pattern extraction date:** 2026-06-12
