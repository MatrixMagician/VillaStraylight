# Phase 18: Memory Spine — config core + embeddings/wiring research spike - Pattern Map

**Mapped:** 2026-06-09
**Files analyzed:** 3 (1 MODIFY, 1 NEW package, 1 NEW test)
**Analogs found:** 3 / 3 (all exact in-repo precedents)

> Phase 18 is a **spine + spike** phase: it MODIFIES `internal/config/villaconfig.go`,
> ADDS a new pure `internal/memory` core, and ADDS its tests. It renders NO Quadlet
> units and writes NO env block (Phases 19–20). `internal/orchestrate/openwebui.go`
> is mapped below ONLY as the downstream landing pattern the RenderView struct must
> fit — it is NOT a file this phase touches.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/config/villaconfig.go` (MODIFY) | config | transform (load/normalize/save) | self (existing `dashboard_*`/`chat_*` fields + `normalizeVilla`) | exact (same file, same pattern) |
| `internal/memory/memory.go` (NEW pkg) | service (pure decision core) | transform (typed-in → typed-out) | `internal/recommend/recommend.go` (`Pick`) + `internal/detect/value.go` (`Bytes`) | exact idiom-match |
| `internal/memory/memory_test.go` (NEW) | test | request-response (table-driven) | `internal/config/villaconfig_test.go` + (idiom) `internal/recommend/recommend.go` table tests | exact |

**Constraint carried on ALL three files:** `internal/memory` must import NEITHER
`os/exec` NOR any container-image literal (`kyuz0|docker.io/|server-vulkan|:rocm-…`).
`TestSeamGrepGate` (`internal/inference/seam_test.go:34`) walks all of `internal/`
(minus the seam) — a leaked literal fails the build. See **Shared Patterns →
Seam-cleanliness**.

## Pattern Assignments

### `internal/config/villaconfig.go` (config, transform) — MODIFY

**Analog:** the existing `dashboard_*`/`chat_*` fields in the SAME file (D-04/D-05
mandate matching this precedent exactly; defaults live in ONE place).

**Field-declaration pattern** (`internal/config/villaconfig.go:31-51`): add `memory_*`
fields to `VillaConfig` with `toml:"…"` tags, each carrying a doc comment naming the
default and the PRIV-01 "never widen a bind / container-DNS only" rule for endpoints
(D-06). New fields go in the struct alongside `ChatPort` (no append-only/schema-bump
constraint here — `VillaConfig` is not a golden-frozen `--json` contract; only
`recommend`/`status` are).

**Single-source default seeding** (`internal/config/villaconfig.go:55-62`) — extend
`defaultConfig()`; this is the ONLY place the literals (`nomic-embed-text-v1.5`, `768`,
`villa-qdrant`/`6333`, `villa-embed`/`8080`) appear. `MemoryEnabled` defaults `false`:
```go
func defaultConfig() VillaConfig {
	return VillaConfig{
		Backend:       "vulkan",
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		ChatPort:      3000,
		// NEW (default-OFF; coherent inert defaults; exact key spellings = planner's call):
		// MemoryEnabled: false, EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768,
		// QdrantAddr/QdrantPort: "villa-qdrant"/6333, EmbedAddr/EmbedPort: "villa-embed"/8080
	}
}
```

**Self-heal pattern** (`internal/config/villaconfig.go:80-92`) — extend `normalizeVilla`,
deriving every fill from `d := defaultConfig()` (NEVER re-hard-code `768`/`6333`). Treat
type-zero memory fields as "unset → default" exactly as the dashboard ports do. CRITICAL
endpoint rule (PRIV-01 continuity, lines 78-79): only ever fill the container-DNS
default for an empty addr — NEVER widen a bind. Existing precedent to copy verbatim:
```go
func normalizeVilla(cfg VillaConfig) VillaConfig {
	d := defaultConfig()
	if cfg.DashboardPort == 0 {
		cfg.DashboardPort = d.DashboardPort
	}
	// ... add the symmetric memory_* zero-fills here, all derived from d ...
	return cfg
}
```

**Load/Parse already call normalizeVilla** (`LoadVilla` line 144, `LoadVillaFrom` line
218, `Parse` line 233) — they self-heal for free once `normalizeVilla` is extended. No
change needed to `LoadVilla`/`SaveVilla`/`SaveVillaTo`/`assertInsideDir`: D-05 reuses
`SaveVilla`'s XDG-confined, 0600/0700, traversal-guarded discipline UNCHANGED.

**Byte-identical guarantee (SC#1) — the load-bearing nuance:** Phase 18 adds NO save
path for memory, so a non-opted-in v1.2 install is never rewritten. The pitfall to test
(RESEARCH Pitfall 1): `toml.Marshal` emits all exported fields, so a future save could
introduce `memory_enabled = false` etc. — assert the round-trip test below proves a
non-opt-in load→struct does not require re-saving.

---

### `internal/memory/memory.go` (service / pure decision core) — NEW PACKAGE

**Analog:** `internal/recommend/recommend.go` (the pure `Pick` idiom — typed inputs →
typed decision values, zero I/O, exhaustively table-testable) + `internal/detect/value.go`
(the `Bytes` typed-Unknown return type).

**Package doc + import pattern** (mirror `internal/recommend/recommend.go:1-20`): open
with a file/package doc comment stating the role and the "PURE function (no I/O)"
invariant. The ONLY in-repo import the core needs is `internal/detect` (for `Bytes`) and
`internal/config` (for `VillaConfig` input). It must import NEITHER `os/exec` NOR any
image string:
```go
import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)
```

**(a) Footprint — typed-Unknown return** (analog: `internal/detect/value.go:23-28`
`KnownBytes`/`UnknownBytes`; consumed by `recommend.Pick` in Phase 22). Lookup table,
never bare 0 on a miss (honesty-by-construction, D-02a):
```go
var embedFootprints = map[string]uint64{
	"nomic-embed-text-v1.5": 512 << 20, // ~512 MiB conservative resident reservation
}
func Footprint(modelID string) detect.Bytes {
	if b, ok := embedFootprints[modelID]; ok {
		return detect.KnownBytes(b, "memory: pinned embedding footprint reservation")
	}
	return detect.UnknownBytes("memory: no footprint known for "+modelID, modelID)
}
```

**(b) Decide — fail-closed gate, refuse-with-reason** (analog: the typed-result +
accumulated-`Notes` discipline of `recommend.Pick`, e.g. `recommend.go:128-131` refusal
with reasons; and `preflight`'s refuse-with-remediation tier). Off is a valid state;
on + missing/invalid field → `Valid:false` with reasons. Pure, never panics, no I/O:
```go
type Decision struct {
	Enabled bool
	Valid   bool
	Reasons []string // populated when !Valid
}
func Decide(cfg config.VillaConfig) Decision {
	if !cfg.MemoryEnabled {
		return Decision{Enabled: false, Valid: true}
	}
	var reasons []string
	// validate: embedding model non-empty, dim > 0, qdrant/embed addr+port present
	return Decision{Enabled: true, Valid: len(reasons) == 0, Reasons: reasons}
}
```

**(c) RenderView — resolved-values-only handoff struct** (analog: the recommend→orchestrate
handoff shape; the TARGET it must fit is `internal/orchestrate/openwebui.go`'s
`openWebUIView` / `buildOpenWebUIView`, see below). Carries ONLY resolved values
(model id, dim, container-DNS addrs/ports) — NO image literal (D-02c/D-10):
```go
type MemoryRenderInput struct {
	EmbeddingModel string
	EmbeddingDim   int
	QdrantAddr     string
	QdrantPort     int
	EmbedAddr      string
	EmbedPort      int
}
func RenderView(cfg config.VillaConfig) MemoryRenderInput { /* map cfg → resolved struct */ }
```

**Downstream landing target (DO NOT build here — shapes the struct):**
`internal/orchestrate/openwebui.go` is the managed-service render path that the Phase-19
Qdrant/`villa-embed` units and Phase-20 OWUI env block will copy. Note from it:
- `buildOpenWebUIView` (`openwebui.go:99-130`) builds endpoint URLs FROM resolved
  values, e.g. `"http://" + containerName + ":8080/v1"` (line 109) — the addr/port are
  composed in `orchestrate`, never re-typed. `MemoryRenderInput` must hand `orchestrate`
  the same kind of resolved `addr`/`port` pieces so the Phase-20 `RAG_OPENAI_API_BASE_URL`
  = `http://villa-embed:8080/v1` and `QDRANT_URI` = `http://villa-qdrant:6333` are built
  there, not in the core.
- The image literal (`openWebUIImage`, line 20) and the accessor (`OpenWebUIImage()`,
  line 28) live ENTIRELY in `orchestrate` — that is exactly where the Qdrant +
  `villa-embed` image literals land in Phase 19 (D-10). `internal/memory` introduces none.
- The env block is an ORDERED `[]envPair` slice (lines 62-65, 106-128), golden-frozen —
  the Phase-20 RAG/memory env block appends to it.

---

### `internal/memory/memory_test.go` (test) — NEW

**Analog:** `internal/config/villaconfig_test.go` (table/round-trip discipline; no
third-party assert/mock — seams are plain funcs) + the off-hardware table-test style of
`recommend`.

**Tests to write** (per RESEARCH Validation Architecture, all Wave 0):
- `TestFootprint` — `KnownBytes` for the pinned model; `UnknownBytes` (Known==false,
  never bare 0) on a miss. Assert `.Known` like `value.go` consumers do.
- `TestDecide` — table: off→`{Enabled:false,Valid:true}`; on+complete→`Valid:true`;
  on+each-missing-field→`Valid:false` with the matching reason (fail-closed).
- `TestRenderView` — asserts only resolved values flow through and addrs are the
  container-DNS defaults (no image literal present in the struct).

**Config-side tests to ADD** to `internal/config/villaconfig_test.go` (mirror existing):
- Memory default-off load — mirror `TestLoadMissingReturnsDefaults` (`villaconfig_test.go:48`):
  assert `defaultConfig()` seeds `MemoryEnabled=false` + inert defaults.
- Zero/absent self-heal — mirror `TestLoadNormalizesZeroPorts` (`villaconfig_test.go:76`):
  a config.toml with no memory keys (or zeroed ones) loads to the defaulted-off struct.
- Byte-identical (SC#1) — extend the round-trip idiom of `TestSaveLoadRoundTrip`
  (`villaconfig_test.go:12`): a v1.2 config with no memory keys loads correct and a
  non-opt-in path introduces no memory keys. Watch RESEARCH Pitfall 1.

*(No new golden/fixture files: Phase 18 changes no byte-frozen `--json` contract.)*

## Shared Patterns

### Seam-cleanliness (applies to: `internal/memory/*`)
**Enforced by:** `internal/inference/seam_test.go:34` (`TestSeamGrepGate`).
**Apply to:** every non-test `.go` file in `internal/memory`.
The gate matches these patterns across all of `internal/` (memory is NOT in the seam
allowlist `inference/` + `detect/gpu_amd.go`, lines 92-95):
```go
"container image literal": regexp.MustCompile(`kyuz0|docker\.io/|server-vulkan|:rocm-7\.2\.4|…`), // seam_test.go:54
"podman invocation":       regexp.MustCompile(`exec\.Command\(\s*"podman"|…`),                     // seam_test.go:56
```
Consequence: `internal/memory` imports no `os/exec`, types no image string. The
RenderView carries resolved VALUES; the image literal is added later in `orchestrate`
(the `openWebUIImage` precedent, `openwebui.go:20`).

### Single-source defaults + self-heal (applies to: `internal/config/villaconfig.go`)
**Source:** `defaultConfig()` (`villaconfig.go:55`) is the SOLE home of default literals;
`normalizeVilla` (`villaconfig.go:80`) derives every fill from it. Never duplicate a
literal across the two — duplication is a drift bug (RESEARCH anti-pattern).

### Typed-Unknown (honesty-by-construction) (applies to: `memory.Footprint`)
**Source:** `internal/detect/value.go:16-28` (`Bytes`, `KnownBytes`, `UnknownBytes`).
A catalog miss returns `UnknownBytes(...)` (Known=false), never a sentinel `uint64` —
`--json`/`recommend` already understand this type.

### Fail-closed validation / refuse-with-reason (applies to: `memory.Decide`)
**Source:** `recommend.Pick` refusal-with-`Notes` (`recommend.go:128-131`, 244-252) +
preflight refuse-with-remediation. Bad/incomplete config → typed invalid decision with
reasons, never a silent accept or panic.

### Config-write safety reused unchanged (applies to: `internal/config/villaconfig.go`)
**Source:** `SaveVilla`/`SaveVillaTo` + `assertInsideDir` (`villaconfig.go:150,184,238`):
XDG-confined, 0600 file / 0700 dir, path-traversal guard, no shell interpolation
(BurntSushi/toml codec). D-05 mandates NO change to this discipline.

### Container-DNS / loopback-only endpoints, never widen a bind (applies to: config memory endpoints + RenderView)
**Source:** the `normalizeVilla` PRIV-01 comment (`villaconfig.go:78-79`) and the
loopback `openWebUIPublishPort` (`openwebui.go:50`). Qdrant has NO published host port
(D-06); endpoint defaults are container-DNS names on `villa.network`.

## No Analog Found

None. Every file maps to an exact in-repo precedent: the config triad (`defaultConfig`
/ `normalizeVilla` / `SaveVilla`), the pure-core idiom (`recommend.Pick`), the
typed-Unknown wrapper (`detect.Bytes`), and the test discipline (`villaconfig_test.go`).
The embeddings runtime / OWUI env / model-footprint are SPIKE DECISIONS recorded in
`18-RESEARCH.md` (§ Spike Decisions D-07/D-08/D-09), not files this phase creates.

## Metadata

**Analog search scope:** `internal/config`, `internal/recommend`, `internal/detect`,
`internal/orchestrate`, `internal/inference`.
**Files scanned:** 6 (villaconfig.go, recommend.go, value.go, openwebui.go,
seam_test.go, villaconfig_test.go).
**Pattern extraction date:** 2026-06-09
