# Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification - Research

**Researched:** 2026-06-12
**Domain:** Catalog/recommend schema evolution (Go pure cores) + on-hardware agent-in-the-loop model qualification on gfx1151 (llama.cpp `--jinja` tool calling, KV measurement, toolbox vintage)
**Confidence:** HIGH — the load-bearing unknowns (toolbox vintage, GGUF artifacts, arch params, Crush harness mechanics) were verified live on this box and against the HF/GitHub APIs during this research session

## Summary

This phase has two halves: a conventional pure-core schema evolution (catalog 2→3 with `role:"coder"` entries; recommend 2→3 with a coder fit stage and residency-mode output) and an unconventional on-hardware qualification gate (real agent-in-the-loop tool-call loops on the gfx1151 box, measured KV, a toolbox keep/re-pin decision). The pure-core half follows shipped v1.3 patterns verbatim — reservation-before-fit, `finalizeRecommendation` unconditional stamping, append-only `SchemaVersion`-last, one golden re-freeze. The qualification half is fully executable in this phase because the dev box IS the live gfx1151 host: villa services are active, the pinned toolbox image is present locally, 468 GB disk is free, and the GTT envelope measures 62.54 GiB.

The headline finding: **the toolbox re-pin question is essentially answered — the pinned `vulkan-radv` digest very likely needs NO re-pin.** Verified live inside `sha256:9a74e555…` via `podman run`: llama.cpp **build 9496 (commit 94a220cd6)**, whose `libllama.so` contains the `qwen3next` arch string (Qwen3-Next/DeltaNet support, PR #16095), whose `libllama-common.so` contains the dedicated `Qwen3-Coder-` chat-format parser with `<tool_call>`/`<function=` XML markers, and whose server lib carries both the `tools param requires --jinja` guard and the `--cache-reuse` flag with graceful-degrade strings (`cache reuse is not supported - ignoring n_cache_reuse`). Binary-string presence is necessary-not-sufficient — D-11's decision still requires the functional agent-loop proof — but every static precondition passes. Also observed: the local `vulkan-radv` *tag* has drifted ahead of the pin (build 9579, image built ~3 days ago), so qualification MUST run by digest, never by tag.

The second headline: **exact revision-pinned GGUF artifacts are captured with sizes and SHA-256s** (HF API, this session), all single-file (degenerate one-shard case for the existing download core), and the architecture params needed for fit math are confirmed from the official Qwen `config.json` files — including the critical hybrid-model modeling insight that Qwen3-Coder-Next has only **12 full-attention layers** (48 layers ÷ `full_attention_interval` 4) with 2 KV heads × 256 head_dim, so its catalog entry must encode `n_layers:12` for the existing uniform `kvCacheBytes` formula to compute the correct ~24 KiB/token (vs ~96 KiB/token for 30B-A3B).

**Primary recommendation:** Plan 3 waves — (1) pure catalog+recommend schema work with placeholder-free entries built from the verified artifact table below, (2) on-hardware qualification checkpoints per entry (tool-call smoke → Crush agent loop → KV/GTT measurement → cache-reuse probe), (3) reconcile measured values into the catalog, record the toolbox keep/re-pin decision with the evidence in this file, and land the single recommend golden re-freeze.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Catalog schema 3 shape (CODER-01)
- **D-01:** Coder entries extend `CatalogModel` with append-only OPTIONAL fields: `role` (absent/empty ⇒ chat — existing entries stay byte-untouched), `agent_ctx` (per-entry agent-profile context the fit is computed at), `cache_reuse_safe` (catalog-declared per-model `--cache-reuse` compatibility; absent ⇒ false, fail-closed), an agent sampling-preset field, and `template_provenance` (HF repo + revision + where the chat/tool-call template comes from). `schema_version` 2→3; loader's schema window widened accordingly.
- **D-02:** Coder GGUF shard URLs are revision-pinned at repo+revision level (`resolve/{revision}/...`, never `main`) because the embedded chat template is part of the artifact (PITFALLS #3). Per-shard `sha256` + `size_bytes` exactly as today.
- **D-03:** Chat-pick behavior is bit-for-bit unchanged: `role:"coder"` entries are excluded from the existing chat `pickBest` path (a role-aware filter, with absent-role ⇒ chat), so the v1.3 chat recommendation and its goldens are unaffected except for the deliberate schema-3 additions.

#### Coder fit stage & residency derivation (CODER-02)
- **D-04:** The coder fit is computed at the entry's catalog-declared `agent_ctx` — the SAME value Phase 25 will render into `--ctx-size`. Never the chat `default_ctx`, never a global constant (fit-at-rendered-ctx, STATE.md Phase 24 risk note).
- **D-05:** Stage ordering inside `Pick`: envelope → embed reservation (D-01 v1.3, unchanged) → chat fit (unchanged) → coder fit stage against the same post-reservation envelope. The coder fit is standalone (weights + KV@agent_ctx + headroom ≤ post-reservation envelope), NOT additive to the chat claimant, because `swap` unloads the chat model.
- **D-06:** Residency mode is a pure inequality output: `swap` when the best qualified coder entry fits standalone at its `agent_ctx`; `shared` when no coder entry fits (the agent rides the existing chat endpoint). Never a preference, never a tier special-case, no co-resident output in v1.4 (deferred CODER-V2-01).
- **D-07:** The recommendation surfaces an always-stamped append-only `coder` block (model, quant, agent ctx, fit terms, residency mode, fits verdict) placed ABOVE `SchemaVersion`, stamped unconditionally through `finalizeRecommendation` exactly like the D-03 memory fields precedent — honest on refusals too. Recommend `schema_version` 2→3, exactly ONE golden re-freeze for this contract in this phase.

#### On-hardware qualification protocol (CODER-03)
- **D-08:** Qualification is a dev-time on-hardware activity on the gfx1151 box (this IS the dev host), driven by a real agent loop: a locally-fetched Crush v0.76.0 used as a QUALIFICATION TOOL ONLY (not a shipped artifact — pinned delivery is Phase 26), pointed at llama-server `--jinja` on the pinned toolbox image, exercising a real multi-step tool-call task (read→edit→verify shape). Benchmark scores alone never qualify an entry.
- **D-09:** KV footprint is MEASURED at `agent_ctx` on the box (llama-server `/metrics` + GTT delta, MEM-DOC pattern) and recorded against the computed estimate; catalog `agent_ctx`/fit numbers must reflect measured reality before the catalog freezes. KV-quantization may only enter as a catalog-declared, benched choice — never an implicit default (aggressive K-cache quant corrupts tool-call JSON).
- **D-10:** Entries that fail qualification are deleted or re-pinned (different quant/revision), never shipped on hope. Qualification evidence (task transcript shape, pass/fail, measured KV) is recorded in the phase verification artifacts.

#### Toolbox re-pin decision (CODER-03 / SC#4)
- **D-11:** Before the catalog freezes, verify the pinned `vulkan-radv` toolbox digest's llama.cpp vintage against: Qwen3-Next/DeltaNet arch support (llama.cpp PR #16095), the Feb-2026 tool-call parser fixes, and per-model `--cache-reuse` semantics (known incompatible with recurrent/hybrid models incl. the current chat model; verify for Qwen3-Coder-Next's hybrid attention). Record the keep/re-pin decision as a numbered decision with evidence. Any re-pin is digest-pinned (never floating/nightly — v1.1 standing decision).
- **D-12:** Qwen3-Coder-Next entries ship ONLY if the (re-)pinned image proves the arch + tool-call path on hardware; otherwise the 96/128 GB tiers ship 30B-A3B only and Next is deferred honestly — deletion over hope.

### Claude's Discretion
- Exact Go field/JSON key names for the new catalog and recommend fields (follow existing naming conventions; `SchemaVersion` stays the last tagged field).
- Golden test organization for the schema-3 re-freeze (coder-present variants; `-update` flag discipline as shipped).
- The exact qualification task script/steps and how its evidence is captured in phase docs.
- Human-readable CLI rendering of the coder block in `villa recommend` output.

### Deferred Ideas (OUT OF SCOPE)
- Co-resident `villa-coder` Quadlet unit — CODER-V2-01 (128 GB fit-gated v2 stretch; design shelf-ready in `.planning/research/ARCHITECTURE.md`).
- Qdrant/villa-embed-backed MCP semantic code search — CODER-V2-02 (v2-only, behind a pre-declared numeric eval it must WIN vs grep/LSP).
- KV-cache quantization as a default — only ever as a catalog-declared, benched, per-entry choice; not pursued in v1.4 unless qualification demands it (D-09).
- Coding-mode render delta, swap verb, Crush pin policy, install addon, surfacing — Phases 25–28 (boundary, not loss).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CODER-01 | Catalog ships `role:"coder"` entries (Qwen3-Coder-30B-A3B all tiers; Qwen3-Coder-Next 96/128 GB tiers) with revision-pinned GGUF artifacts and template provenance (catalog schema 2→3, append-only) | §GGUF Artifact Table (verified revisions/sizes/SHA-256s), §Schema Evolution Mechanics (exact loader/struct changes), §Hybrid KV Modeling (Next's `n_layers:12` encoding), §Sampling Preset (cited values) |
| CODER-02 | `villa recommend` computes a coder fit at agent-profile context (after embed reservation + chat fit) and outputs an honest residency mode (`swap`/`shared`) as a fit-math output (recommend schema 2→3, append-only) | §Coder Fit Stage pattern (Pick pipeline insertion point, `finalizeRecommendation` precedent), §Fit Pre-Computations (expected numbers per tier), §Golden Re-Freeze Mechanics |
| CODER-03 | Coder catalog entries are qualified agent-in-the-loop on hardware (real multi-step tool-call loop through llama-server `--jinja` on the pinned image, measured KV at agent ctx) before freezing; toolbox re-pin decision recorded | §On-Hardware Qualification Protocol (task shape, PASS/FAIL, evidence capture), §KV Measurement Method, §Toolbox Vintage Findings (on-box verified evidence for the keep/re-pin decision), §Cache-Reuse Compatibility |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

Directives the planner MUST honor (same authority as locked decisions):

- **GSD workflow enforcement** — all file changes through GSD commands; on-hardware steps run for real (graphmind convention: never deferred or auto-approved).
- **Config is the single source of truth** — Quadlet units regenerated from config, never hand-edited (qualification serving is dev-time `podman run`, not unit edits).
- **Inference seam grep-gate (`TestSeamGrepGate`)** — backend marker strings, image tags, podman literals stay behind `internal/inference` + `internal/orchestrate`; the gate walks `internal/` AND `cmd/villa`. Phase 24 must NOT introduce Go-side image/backend literals anywhere (the re-pin decision is a recorded decision; any actual pin change lands at the inference seam in Phase 25).
- **`--json` contracts byte-frozen by golden tests** — evolve append-only + schema bump; refreeze intentionally with `go test … -update`, exactly once this phase (recommend), plus the catalog 2→3 (embedded seed, guarded by catalog tests).
- **Offload is offload-asserting, never liveness** — qualification runs must assert GPU residency (log scrape + GTT delta), never treat a 200 as success.
- **Vulkan RADV is the default**; ROCm strictly opt-in — qualification runs on the pinned Vulkan image.
- **Pure-core + injectable-seam** — new decision logic (coder fit) is pure in `internal/recommend`; no I/O in `Pick`.
- **Build/test gates**: `make build`, `make test`, `make check` (vet+test); Go 1.26.2; gofmt/goimports; errcheck/staticcheck via `.golangci.yml`.
- **Dashboard binary trap** — not triggered this phase (`villa recommend` runs fresh from `./villa`), but if dashboard code is touched (it should NOT be — surfacing is Phase 28), the long-lived service needs a restart.
- **No telemetry; strictly local** — the only sanctioned outbound in this phase is dev-time artifact pulls (GGUFs from HF, Crush tarball from GitHub) — both checksum-verified.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Catalog schema 3 (`role`, `agent_ctx`, `cache_reuse_safe`, sampling, provenance) | `internal/catalog` (pure core) | — | Catalog owns entry shape + schema window; loader already validates externally-supplied JSON |
| Coder fit stage + residency mode | `internal/recommend` (pure core) | — | All fit math lives in `Pick`; reuses `kvCacheBytes`/`headroomBytes`/`addSaturating` verbatim |
| Recommend contract (`coder` block, schema 3) | `internal/recommend` (struct) + `cmd/villa/recommend.go` (render) | `cmd/villa/testdata/recommend.golden.json` | Contract struct in the core; JSON/golden + human table in the command tier |
| GGUF pull + sha256 verify (dev-time pre-stage for qualification) | `internal/download` via `villa model pull` | — | Existing shard-verify machinery; revision-pinned URLs are just different `url` values |
| Qualification serving (llama-server `--jinja` at agent ctx) | Dev-time `podman run` by pinned digest (scripted, NOT Go code) | — | Phase 25 owns the rendered `--jinja` unit delta; Phase 24 qualification replicates `ContainerArgs` + `--jinja -c <agent_ctx>` manually — no orchestrate/inference code changes |
| KV / GTT measurement | Dev-time script reading `/sys/class/drm/card1/device/mem_info_gtt_used` + llama-server load logs | `internal/detect` patterns (reference, not modified) | Measurement is evidence capture, not product code |
| Agent loop (Crush v0.76.0) | Dev-time local fetch, scratch workspace | — | Qualification tool only (D-08); shipped delivery is Phase 26 |
| Toolbox keep/re-pin decision | Phase decision record (docs) | `internal/inference` (Phase 25 if re-pin) | Decision + evidence recorded this phase; any pin change is a Phase 25 seam edit |

## Standard Stack

### Core

No new Go module dependencies. This phase modifies two existing pure cores and runs dev-time on-hardware protocols.

| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|--------------|
| `internal/catalog` | schema 2→3 | Coder entries + new optional fields | Existing entry shape, `go:embed seed.json`, exact-match schema window `[VERIFIED: internal/catalog/load.go]` |
| `internal/recommend` | schema 2→3 | Coder fit stage + residency output | Existing `Pick` pipeline with reservation-before-fit; `kvCacheBytes` formula CITED to llama.cpp KV layout `[VERIFIED: internal/recommend/kv.go]` |
| llama.cpp (pinned toolbox) | build 9496 (94a220cd6), GCC 15.2.1 | Qualification server (`--jinja`, tool-call parser, `--cache-reuse`) | Verified live in the pinned image on this box `[VERIFIED: podman run --rm <digest> llama-server --version]` |
| Crush | v0.76.0 (2026-06-05) | Qualification agent (dev-time only) | Milestone-ratified agent of record; non-interactive `crush run` + `--yolo` exists `[CITED: github.com/charmbracelet/crush, deepwiki CLI usage]` |

### Qualification Artifacts (dev-time fetches, all checksum-pinned)

| Artifact | Source | Size | SHA-256 |
|----------|--------|------|---------|
| `crush_0.76.0_Linux_x86_64.tar.gz` | `github.com/charmbracelet/crush/releases/download/v0.76.0/` | 25,155,696 B | `0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9` `[VERIFIED: checksums.txt fetched 2026-06-12]` |
| (checksums.txt also ships `checksums.txt.sigstore.json` for provenance) | same release | 6,337 B / 10,088 B | — |

### GGUF Artifact Table (CODER-01 catalog inputs — VERIFIED via HF API 2026-06-12)

All values fetched from the HuggingFace tree API at the pinned revision; `sha256` is the git-LFS oid (the same field the existing Shard schema records).

| Model entry | HF repo | Pinned revision | Filename | size_bytes | sha256 |
|-------------|---------|-----------------|----------|-----------|--------|
| Qwen3-Coder-30B-A3B (all tiers) | `unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF` | `b17cb02dd882d5b6ab62fc777ad2995f19668350` (lastModified 2026-01-30) | `Qwen3-Coder-30B-A3B-Instruct-UD-Q4_K_XL.gguf` | 17,665,334,432 | `2841aa314d916434860cfb8990347528dcdfe5c350dbcb9d1461dbee88ff2533` |
| Qwen3-Coder-Next Q4 (128 GB tier) | `unsloth/Qwen3-Coder-Next-GGUF` | `ce09c67b53bc8739eef83fe67b2f5d293c270632` (lastModified 2026-03-06) | `Qwen3-Coder-Next-UD-Q4_K_XL.gguf` | 49,608,478,720 | `4bb93f0a0221ef4ff963ca9094df629c8dfdfabc3b4fdd85c1a2e4c0624fce36` |
| Qwen3-Coder-Next Q3 (96 GB tier) | `unsloth/Qwen3-Coder-Next-GGUF` | `ce09c67b53bc8739eef83fe67b2f5d293c270632` | `Qwen3-Coder-Next-UD-Q3_K_XL.gguf` | 36,282,685,440 | `91b928f28a2f4b76a0d9147f148311bfe8716ac8e495acc33eb7aace0ad76135` |

- **All three are single files at repo root** — the degenerate one-shard case; the existing `Shards[]` + `internal/download` verify path needs zero changes `[VERIFIED: HF tree API]`.
- **Revision-pinned URL form (D-02):** `https://huggingface.co/{repo}/resolve/{revision}/{filename}` — e.g. `https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF/resolve/b17cb02dd882d5b6ab62fc777ad2995f19668350/Qwen3-Coder-30B-A3B-Instruct-UD-Q4_K_XL.gguf`.
- **Template provenance (D-01):** the chat/tool-call template is embedded in the GGUF at that revision. Provenance value = repo + revision + "embedded GGUF chat template". The Next repo's revision (2026-03-06) post-dates the Feb-2026 tool-calling updates; the 30B revision (2026-01-30) post-dates the Aug-2025 template fixes `[VERIFIED: HF API lastModified]`.

### Architecture Params (fit-math inputs — VERIFIED from official Qwen config.json 2026-06-12)

| Model | architectures | n_layers | n_kv_heads | head_dim | max_position | KV/token f16 |
|-------|--------------|----------|-----------|----------|--------------|--------------|
| Qwen3-Coder-30B-A3B-Instruct | `Qwen3MoeForCausalLM` | 48 | 4 | 128 | 262,144 | 2×48×4×128×2 = 98,304 B (96 KiB) |
| Qwen3-Coder-Next | `Qwen3NextForCausalLM` (hybrid) | 48 total, `full_attention_interval: 4` → **12 full-attention** | 2 | 256 | 262,144 | 2×12×2×256×2 = 24,576 B (24 KiB) |

Qwen3-Coder-Next also carries DeltaNet linear-attention state on the other 36 layers (`linear_num_value_heads: 32`, `linear_key_head_dim: 128`, `linear_value_head_dim: 128`) — a constant-per-slot recurrent state on the order of ~75–150 MiB total, NOT ctx-proportional `[ASSUMED — computed; D-09 measurement captures the truth]`.

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Encoding Next's hybrid KV as `n_layers:12` in the catalog | New schema field (e.g. `full_attn_layers`) + formula branch | Rejected: append-only schema churn and a formula fork for zero benefit — the uniform formula with effective full-attention layer count computes the identical number; D-09's measured-vs-computed check validates it |
| Keeping the current toolbox pin (build 9496) | Re-pin to the drifted tag's build 9579 (`sha256:9df33843…`, locally present) | Only if qualification FAILS on a parser/arch bug fixed between 9496→9579; otherwise keep — fewer moving parts, and any re-pin lands at the inference seam in Phase 25 |
| Crush `--yolo` scripted run for qualification | Hand-driving the Crush TUI | TUI is non-reproducible and non-capturable; `crush run` + `--yolo` in a throwaway workspace is scriptable and evidence-friendly `[CITED: charmbracelet/crush docs/issues #1030]` |

**Installation (dev-time, this phase):**
```bash
# Qualification agent (NOT shipped; Phase 26 owns delivery)
curl -fsSLO https://github.com/charmbracelet/crush/releases/download/v0.76.0/crush_0.76.0_Linux_x86_64.tar.gz
echo "0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9  crush_0.76.0_Linux_x86_64.tar.gz" | sha256sum --check
tar -xzf crush_0.76.0_Linux_x86_64.tar.gz -C /tmp/crush-qual/

# Coder GGUFs — once catalog entries land, pull through villa's own verify path:
./villa model pull qwen3-coder-30b-a3b   # 17.7 GB
./villa model pull qwen3-coder-next      # 49.6 GB (Q4) / 36.3 GB (Q3)
```

## Package Legitimacy Audit

**No new packages are installed by this phase** — zero new Go module dependencies (the diff touches only first-party `internal/*` + `cmd/villa`). The external artifacts are binary/model files, each pinned by exact SHA-256 from an authoritative source:

| Artifact | Registry/Source | Age | Provenance | Verdict | Disposition |
|----------|-----------------|-----|------------|---------|-------------|
| Crush v0.76.0 tarball | GitHub releases (charmbracelet) | released 2026-06-05 | checksums.txt + sigstore bundle | OK | Approved (dev-time tool only) |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | HF `unsloth/…-GGUF` | revision 2026-01-30 | LFS oid sha256, revision-pinned | OK | Approved |
| Qwen3-Coder-Next UD-Q4_K_XL / UD-Q3_K_XL | HF `unsloth/Qwen3-Coder-Next-GGUF` | revision 2026-03-06 | LFS oid sha256, revision-pinned | OK | Approved (ship gated on D-12 qualification) |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─────────────────────────────────────────────┐
                         │ Pure-core half (off-hardware, table-tested) │
                         └─────────────────────────────────────────────┘
 config.toml ──► cmd/villa/recommend.go ──► recommend.Pick(profile, catalog, ov, mem)
                                              │
   internal/catalog (schema 3) ───────────────┤   envelope = resolveEnvelope(profile)
   • chat entries (role absent)               │   envelope -= embedReservation      (v1.3, unchanged)
   • coder entries (role:"coder",             ├─► chat fit: pickBest/pickOverride   (coder entries FILTERED OUT, D-03)
     agent_ctx, cache_reuse_safe,             ├─► coder fit: best role:"coder" entry where
     sampling, template_provenance,           │     weights + kvCacheBytes(entry, agent_ctx) + headroom ≤ envelope
     revision-pinned Shards)                  │     → fits ⇒ residency "swap"; none fit ⇒ "shared" (D-06)
                                              └─► finalizeRecommendation: stamp coder block (always), SchemaVersion=3 (D-07)
                                                    │
                                                    ├─► --json  ──► recommend.golden.json (ONE re-freeze)
                                                    └─► human table (+ coder section)

                         ┌─────────────────────────────────────────────┐
                         │ On-hardware half (gfx1151 dev box, D-08/09) │
                         └─────────────────────────────────────────────┘
 villa model pull (sha256 verify) ──► ~/.local/share/villa/models/<coder.gguf>
                                              │
 systemctl --user stop villa-llama  (clean GTT baseline)
                                              │
 podman run BY DIGEST sha256:9a74e555… llama-server --jinja -c <agent_ctx> -fa 1 -ngl 999 --no-mmap -lv 4 --metrics
        │                                     │
        ├── load logs: "KV buffer size = X MiB"   ──┐
        ├── sysfs: mem_info_gtt_used delta          ├─► measured KV/footprint vs computed (D-09)
        ├── curl tool-call smoke (tools + --jinja)  │
        └── crush run --yolo  (scratch repo, read→edit→verify task) ──► PASS/FAIL + transcript (D-08/D-10)
                                              │
 evidence ──► toolbox keep/re-pin decision (D-11) + catalog freeze/delete/re-pin (D-10/D-12)
                                              │
 systemctl --user start villa-llama  (restore chat stack)
```

### Recommended Project Structure (delta only)

```
internal/catalog/
├── catalog.go          # SupportedSchema 2→3; CatalogModel gains role/agent_ctx/cache_reuse_safe/sampling/provenance
├── seed.json           # schema_version 3; +3 coder entries; existing 4 entries byte-untouched
└── (tests)             # role-filter, fail-closed defaults, seed-integrity tests
internal/recommend/
├── recommend.go        # Recommendation gains Coder block (above SchemaVersion); pickBest gains role filter;
│                       # new pickCoder stage; finalizeRecommendation stamps coder block unconditionally
└── (tests)             # coder fit table tests incl. refusal/no-fit/override interactions
cmd/villa/
├── recommend.go        # human-table coder section (gated rendering like ROCm advice / embed row)
└── testdata/recommend.golden.json   # ONE deliberate re-freeze (-update)
.planning/phases/24-*/  # qualification scripts/evidence + the D-11 toolbox decision record
```

### Pattern 1: Coder fit stage in `Pick` (CODER-02)

**What:** A standalone fit pass over `role:"coder"` entries against the post-reservation envelope, after the chat fit.
**When to use:** Implemented once in `internal/recommend`; reuses `kvCacheBytes`, `headroomBytes`, `addSaturating` verbatim.
**Key mechanics (from the live code, `[VERIFIED: internal/recommend/recommend.go]`):**
- Every `Pick` return path already flows through `finalizeRecommendation` — the coder block stamps there, exactly like `EmbeddingReservationBytes`/`MemoryConsidered` (the D-03 v1.3 precedent). Make the coder block a struct field placed ABOVE `SchemaVersion`, always serialized (NOT `omitempty`) so refusals are honest and the off-path JSON changes exactly once, deliberately.
- The coder stage evaluates each coder entry at **its own `agent_ctx`** (D-04), never `effectiveCtx`'s override path — `--ctx` overrides apply to the chat pick only (an override semantics decision the planner should make explicit in tests).
- "Best" coder = largest fitting footprint among `role:"coder"` entries (mirrors `pickBest`'s most-capable rule); respect `UnifiedMemorySafe` and `MinEnvelopeBytes` guards identically.
- Residency: `fits ⇒ "swap"`, `no coder fits ⇒ "shared"` (D-06). On the no-envelope refusal path, the coder fit is also unevaluable — stamp the block with `fits:false`, residency `"shared"` (conservative floor: swap requires a proven fit), and the envelope refusal note already explains why.
- The chat path (`pickBest` AND `pickOverride` via `FindByID`) must skip/handle `role:"coder"` entries: `pickBest` filters them; a user `--model qwen3-coder-…` override is a planner decision (recommend: allow with a loud note, mirroring the unsafe-override precedent — but verify it doesn't disturb the golden).

### Pattern 2: Catalog schema 3, append-only with fail-closed defaults (CODER-01)

**What:** New OPTIONAL fields on `CatalogModel`; `SupportedSchema` 2→3.
**Mechanics (from the live code, `[VERIFIED: internal/catalog/load.go, catalog.go]`):**
- The "schema window" is an **exact-match equality** (`ext.SchemaVersion != SupportedSchema` → warn + fall back to embedded seed). The 2→3 change is: bump the constant, bump `seed.json`'s `schema_version` (and `catalog_version`), update the v2 doc comment with a v3 paragraph. An external schema-2 catalog will then warn + fall back — correct and consistent with the v1→v2 precedent.
- `loadExternal` uses `DisallowUnknownFields` — the new struct fields MUST land in the same commit as any seed/external JSON that uses them, or external schema-3 catalogs fail decode with the wrong (parse-error) warning.
- Field shape suggestion (names are Claude's discretion; conventions from the existing struct): `Role string \`json:"role,omitempty"\``, `AgentCtx int \`json:"agent_ctx,omitempty"\``, `CacheReuseSafe bool \`json:"cache_reuse_safe,omitempty"\`` (absent ⇒ Go zero `false` — fail-closed for free), a sampling struct (e.g. `AgentSampling` with temperature/top_p/top_k/repeat_penalty), `TemplateProvenance string \`json:"template_provenance,omitempty"\``.
- Existing 4 chat entries in `seed.json` gain NO keys (omitempty + hand-authored JSON keeps them byte-untouched apart from the top-level version bumps).

### Pattern 3: Hybrid KV modeling for Qwen3-Coder-Next

**What:** Encode the hybrid model so the EXISTING uniform formula computes the right KV.
**How:** `n_layers: 12` (the full-attention layer count = 48 ÷ `full_attention_interval` 4), `n_kv_heads: 2`, `head_dim: 256`, `kv_bytes_per_elem: 2` → 24 KiB/token. Record the encoding rationale in the entry's `display_name`/comment-equivalent and in the phase docs so a future reader doesn't "fix" 12 back to 48 (which would 4× overcount and falsely refuse Next on the 96 GB tier).
**Validation:** D-09's measured KV at `agent_ctx` is the proof this encoding is honest. The DeltaNet recurrent state (~constant, est. ≤150 MiB) lands inside the measured-vs-computed delta; if measurement shows it material, fold it into `weight_bytes` padding or `min_envelope_bytes` — never into the ctx-proportional term.

### Pattern 4: On-hardware qualification protocol (CODER-03 / D-08) — recommended concrete shape

The exact script is Claude's discretion per CONTEXT; this is the research-recommended protocol the planner should encode as tasks + checkpoints.

**Per coder entry (30B-A3B @ its agent_ctx; Next-Q4 @ 131072; Next-Q3 @ its agent_ctx):**

1. **Pre-stage:** `./villa model pull <entry>` (sha256-verified via the existing download core). Disk gate: ~104 GB total for all three; 468 GB free on this box `[VERIFIED]`.
2. **Quiesce + baseline:** `systemctl --user stop villa-llama.service`; record `cat /sys/class/drm/card1/device/mem_info_gtt_used` (clean baseline — chat+embed currently hold ~25.4 GiB).
3. **Serve by DIGEST** (never the drifted tag), replicating the seam's flags plus the qualification deltas:
   ```bash
   podman run --rm --name villa-qual \
     --device /dev/dri --group-add keep-groups --security-opt seccomp=unconfined \
     -p 127.0.0.1:8081:8080 \
     -v ~/.local/share/villa/models:/models:ro,z \
     docker.io/kyuz0/amd-strix-halo-toolboxes@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad \
     llama-server -m /models/<file>.gguf -c <agent_ctx> --host 0.0.0.0 --port 8080 \
     -ngl 999 -fa 1 --no-mmap -lv 4 --metrics --jinja
   ```
   (Port 8081 avoids colliding with the normal 8080; flags mirror `internal/inference/backend_vulkan.go` + `--jinja`.)
4. **Measure (D-09):** (a) load-log `KV buffer size = X MiB` lines (string verified present in build 9496); (b) GTT delta = `mem_info_gtt_used` after-load minus baseline; (c) residency assert — `load_tensors: Vulkan0 model buffer size` + `offloaded N/N` lines at `-lv 4` (a partial offload = FAIL for that entry/ctx, per D-11 honesty). Record measured KV and total footprint vs computed.
5. **Tool-call smoke (cheap disqualifier before the agent loop):** POST a canned `tools` request; assert HTTP 200 and a well-formed `tool_calls` array with **string** `arguments` (the llama.cpp #20198 failure class). See Code Examples.
6. **Agent-in-the-loop task (read→edit→verify):** scratch git repo containing a tiny Go module with one failing test; run
   ```bash
   CRUSH_DISABLE_METRICS=1 DO_NOT_TRACK=1 /tmp/crush-qual/crush run --yolo --cwd /tmp/qual-repo \
     "Run 'go test ./...', read the failing test and the source file it covers, fix the bug so the test passes, then run 'go test ./...' again and confirm it passes."
   ```
   with a workspace-local `crush.json` (single openai-compat provider at `http://127.0.0.1:8081/v1`, kill switches on — see Code Examples). `--yolo` auto-accepts tool permissions (required for a scripted run) `[CITED: charmbracelet/crush issue #1030 / permissions docs]` — acceptable ONLY in the throwaway workspace.
7. **PASS criteria (all required):** ≥3 distinct tool types actually executed (read/view, edit/write, bash); final `go test` green; zero 5xx on `/v1/chat/completions` while `tools` present (server journal); no narrated tool calls (raw `<tool_call>`/`<function=` XML appearing as assistant prose); no `ggml_vulkan: Device memory allocation … failed`; session terminates on its own.
8. **FAIL handling (D-10/D-12):** any criterion fails → the entry is deleted or re-pinned (different quant/revision), never shipped. A Next failure on arch/parser grounds triggers the re-pin evaluation against build 9579 (locally present) — re-pin by digest only.
9. **Cache-reuse probe (per entry, feeds `cache_reuse_safe`):** restart the server with `--cache-reuse 256`, run a 2-turn prompt; journal must NOT contain `cache reuse is not supported - ignoring n_cache_reuse` / `cache_reuse is not supported by this context` AND second-turn `timings.cache_n > 0` (or `prompt_n` collapse) for `true`. Expected: 30B-A3B `true` (standard GQA cache), Next `false` (hybrid) — but evidence decides, never assumption.
10. **Evidence capture:** transcript (crush stdout), server journal excerpt, measured KV/GTT table, smoke-test response JSON — into the phase verification artifacts; the D-11 decision record cites them.
11. **Restore:** stop the qual container; `systemctl --user start villa-llama.service`; confirm `villa status` green.

### Anti-Patterns to Avoid

- **Qualifying against the `vulkan-radv` tag:** the local tag has ALREADY drifted to build 9579 (`sha256:9df33843…`) while the pin is build 9496 — a tag-based run qualifies the wrong binary. Always `@sha256:9a74e555…` `[VERIFIED: podman images on this box]`.
- **Computing Next's KV with `n_layers:48`:** 4× overcount (12 GiB instead of 3 GiB at 128k) → false refusal on the 96 GB tier and a wrong residency mode.
- **`omitempty` on the recommend coder block:** D-07 demands always-stamped (refusals included); omitempty would make the off-path JSON shape conditional — the exact dishonesty the D-03 memory-fields precedent forbids.
- **A second golden re-freeze:** the catalog 2→3 (seed) and recommend 2→3 (golden) each land exactly once; any later "small fix" that touches the golden again violates the phase contract.
- **Adding podman/image literals to Go code for qualification:** `TestSeamGrepGate` walks `internal/` + `cmd/villa`; qualification is scripts/docs, not product code.
- **Letting `--ctx` override leak into the coder fit:** D-04 — the coder fit is at catalog `agent_ctx`, full stop.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Coder GGUF download + integrity | New fetch/verify code | `internal/download` via existing `Shards[]` (single-file degenerate case) | Path-traversal guard, sha256 + size verify, shard handling already shipped and tested |
| KV math at agent ctx | A second formula | `kvCacheBytes` + `addSaturating` (WR-07 saturation already guards absurd ctx) | Formula is CITED to llama.cpp KV layout; hybrid is handled by the `n_layers:12` encoding |
| Residency assertion during qualification | New log parsing | The `-lv 4` `load_tensors: Vulkan0 … offloaded N/N` lines + GTT delta (the shipped dual-assert pattern) | Marker semantics already battle-tested on this exact image family |
| Contract freeze | Ad-hoc JSON comparison | Golden tests + package-level `-update` flag (`cmd/villa/detect_test.go` declares it; recommend test consumes it) | One-command intentional re-freeze: `go test ./cmd/villa -run Recommend -update` |
| Agent harness | A custom OpenAI tool-loop driver | Crush v0.76.0 `crush run --yolo` | D-08 demands a REAL agent loop — and Crush is the v1.4 agent of record, so qualification doubles as early integration evidence |

**Key insight:** every host-touching mechanism this phase needs (download/verify, residency proof, GTT read, golden freeze) already exists; the only genuinely new code is ~2 pure-core deltas plus seed JSON.

## Toolbox Vintage Findings (D-11 evidence — gathered on this box, 2026-06-12)

These are the re-pin decision inputs, ready for the planner to wire into the decision record:

| Check | Result | Evidence |
|-------|--------|----------|
| Pinned digest's llama.cpp version | **build 9496 (commit 94a220cd6)**, GCC 15.2.1 | `podman run --rm <pinned digest> llama-server --version` `[VERIFIED]` |
| Qwen3-Next/DeltaNet arch support (PR #16095) | **Present** | `libllama.so` contains arch strings `qwen3next`, `qwen3moe`, `qwen35moe`, `qwen3vl` `[VERIFIED: binary grep in image]` |
| Qwen3-Coder tool-call parser | **Present** | `libllama-common.so` contains `Qwen3-Coder-` chat-format string + `<tool_call>` + `<function=` XML markers `[VERIFIED]` |
| `--jinja` tools guard | Present (`tools param requires --jinja` in `libllama-server-impl.so`) | `[VERIFIED]` |
| `--cache-reuse` flag | Present in `--help`; degrade strings `cache reuse is not supported - ignoring n_cache_reuse` and `cache_reuse is not supported by this context` present | `[VERIFIED]` — i.e. an incompatible model **ignores** cache-reuse with a journal warning, it does not crash |
| KV measurement hook | `KV buffer size = %8.2f MiB` log format string present | `[VERIFIED]` |
| Tag drift | Local `vulkan-radv` tag → `sha256:9df33843…` = **build 9579** (image built ~3 days ago); pinned `9a74e555…` (build 9496) still present locally untagged | `podman images` + version probe `[VERIFIED]` |
| Feb-2026 tool-call parser fixes included | **Very likely** — builds 9496/9579 are June-2026 master builds (~4 months after Feb 2026; the dedicated Qwen3-Coder parser string is present) | `[ASSUMED — ordering argument; the functional agent-loop proof is the closure]` |

**Recommended verdict for the planner:** KEEP the current pin, contingent on the functional qualification passing (D-11's evidence = the agent-loop run, not the binary strings alone). The drifted-tag build 9579 is the pre-identified fallback re-pin candidate if a 9496-specific parser bug surfaces.

## Cache-Reuse Compatibility (per-entry `cache_reuse_safe` inputs)

- Mechanism: `--cache-reuse` performs KV shifting, which requires a shiftable unified KV cache; recurrent/hybrid caches (Mamba/DeltaNet hybrids — including the current chat model Qwen3.6-35B-A3B) don't support it `[CITED: llama.cpp discussions #13606/#22354/#20574 via .planning/research/SUMMARY.md]`.
- In build 9496 the server **detects and ignores** unsupported cache-reuse with a journal warning (strings verified above) — so a wrong `true` would not crash, but it WOULD be a dishonest catalog claim (Phase 25 renders the flag from it). Fail-closed default (absent ⇒ false) is structurally free in Go.
- Expected outcomes (verify on hardware, step 9 of the protocol): **30B-A3B → `true`** (standard full-attention GQA, `Qwen3MoeForCausalLM`); **Qwen3-Coder-Next → `false`** (`Qwen3NextForCausalLM` hybrid/DeltaNet) `[ASSUMED until the probe runs]`.

## Fit Pre-Computations (expected values for D-09 measured-vs-computed and entry authoring)

Headroom = 12% of post-reservation envelope (`headroomFraction = 0.12`); embed reservation applies when memory is enabled. GTT envelope on this box: 67,149,369,344 B = 62.54 GiB `[VERIFIED: sysfs]`. Tier envelopes ≈ ½ RAM at Fedora defaults.

| Entry | weight_bytes (GiB) | KV f16 @65536 | KV f16 @131072 | 64 GB tier (~31 GiB) | 96 GB tier (~47 GiB) | 128 GB tier (62.5 GiB) |
|-------|-------------------|---------------|----------------|---------------------|---------------------|------------------------|
| 30B-A3B UD-Q4_K_XL | 16.45 | 6.00 GiB | 12.00 GiB | fits @65536 (~26.2 total incl. headroom); does NOT fit @131072 | fits @131072 (~34.1) | fits @131072 (~36.0) |
| Next UD-Q3_K_XL | 33.79 | 1.50 GiB | 3.00 GiB | no | fits @131072 (~42.4) | fits @131072 (~44.3) |
| Next UD-Q4_K_XL | 46.20 | 1.50 GiB | 3.00 GiB | no | no (~54.9 > 47) | fits @131072 (~56.7 ≤ 62.5) |

Implication for `agent_ctx` authoring: a per-entry single `agent_ctx` (D-01/D-04) means the 30B entry's value sets its fit on EVERY tier — `agent_ctx: 65536` keeps it fitting the 64 GB tier (the fit-everywhere default); the Next entries can declare `131072`. If the planner wants 30B at 128k on big tiers, that needs a second catalog entry or a tier-aware ctx — recommend keeping ONE 30B entry at 65536 in v1.4 (simplest honest shape; matches STACK's 64k-class agent profile).

## Common Pitfalls

### Pitfall 1: Qualification runs against the drifted tag
**What goes wrong:** `podman run …:vulkan-radv` silently uses build 9579, not the pinned 9496 — the catalog freezes against an unpinned binary.
**Why it happens:** The tag drifted on this very box (verified); tags feel equivalent to digests.
**How to avoid:** Every qualification command uses `@sha256:9a74e555…`. The evidence record includes the `llama-server --version` output from the run.
**Warning signs:** `version: 9579` in the qualification journal.

### Pitfall 2: Hybrid KV overcount (or undercount) for Qwen3-Coder-Next
**What goes wrong:** `n_layers:48` → 12 GiB computed KV @131072 (4× real) → false refusal on 96 GB tier; or someone "corrects" 12→48 later.
**How to avoid:** Encode `n_layers:12`, document why in the phase docs and entry metadata; D-09 measured KV (~3 GiB @131072) is the regression anchor.
**Warning signs:** measured KV differing from computed by ~4×.

### Pitfall 3: GTT delta polluted by the resident chat stack
**What goes wrong:** Measuring with villa-llama + villa-embed up (~25.4 GiB used now) makes the delta noisy and risks OOM during Next-Q4 loads (46 GiB weights into a 62.5 GiB envelope).
**How to avoid:** `systemctl --user stop villa-llama.service` before each qualification run; record the quiesced baseline; restart and `villa status` after.
**Warning signs:** `ggml_vulkan: Device memory allocation … failed` mid-load; baseline > ~2 GiB.

### Pitfall 4: Tools request without `--jinja`
**What goes wrong:** Any `tools` POST 500s (`tools param requires --jinja` — guard verified in the pinned build); the agent loop fails for a harness reason, wrongly disqualifying a good model.
**How to avoid:** `--jinja` in the qualification serve command (it is NOT in the v1.3 `llamaServerFlags`); the smoke test (step 5) runs before the agent loop so harness faults are caught separately from model faults.

### Pitfall 5: `DisallowUnknownFields` decode trap on schema 3
**What goes wrong:** Bumping `seed.json` (or shipping an external schema-3 example) before the struct gains the new fields → external catalogs fail with a parse-error warning instead of a schema warning; worse, partial field landing splits across commits.
**How to avoid:** Struct fields + `SupportedSchema` bump + seed bump land in one commit; tests cover an external schema-3 file round-trip and a schema-2 fallback warning.

### Pitfall 6: Conditional coder block breaks the always-stamped contract
**What goes wrong:** `omitempty` on the coder block (or stamping only on success paths) makes the JSON shape depend on fit outcome — refusals stop being honest and the golden only covers one shape.
**How to avoid:** Stamp in `finalizeRecommendation` (every return path already flows through it); golden fixture exercises the populated block; a table test asserts the block is present on the refusal path too.

### Pitfall 7: Crush `--yolo` outside the throwaway workspace
**What goes wrong:** Auto-accepted tool calls from a mid-size local model in a real repo (this repo!) — the Pitfall-7 workspace-escape class from milestone research.
**How to avoid:** Qualification workspace is a disposable `/tmp` git repo; `--cwd` pins it; kill-switch env (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`) set; the villa repo is never the cwd. Note Crush warns a project-local `crush.json` can execute `$(…)` at load — we author that file ourselves in the scratch dir.

### Pitfall 8: Model-id shadowing in the Crush provider config
**What goes wrong:** A custom `providers.<name>.models[].id` can be shadowed by Crush's embedded catalog alias (charmbracelet/crush #2649) — the qualification request resolves oddly.
**How to avoid:** villa-unique ids in the qual `crush.json` (e.g. `villa-qwen3-coder-30b`); llama-server ignores the model field anyway, so uniqueness is free.

## Code Examples

### Tool-call smoke test (protocol step 5)

```bash
# Source: llama.cpp function-calling docs (--jinja requirement); arguments-as-string check per llama.cpp #20198
curl -s http://127.0.0.1:8081/v1/chat/completions -H 'Content-Type: application/json' -d '{
  "model": "villa-qual",
  "messages": [{"role":"user","content":"What is the weather in Berlin? Use the tool."}],
  "tools": [{
    "type":"function",
    "function":{
      "name":"get_weather",
      "description":"Get current weather for a city",
      "parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}
    }
  }]
}' | python3 -c "
import json,sys
r=json.load(sys.stdin)
tc=r['choices'][0]['message'].get('tool_calls') or []
assert tc, 'FAIL: no tool_calls in response'
args=tc[0]['function']['arguments']
assert isinstance(args,str), 'FAIL: arguments is %s, not string (llama.cpp #20198 class)' % type(args)
json.loads(args)
print('PASS: well-formed tool_calls, string arguments:', args)
"
```

Also run the no-`properties` variant (a historical 500 class from PITFALLS #3): same request with `"parameters": {"type":"object"}` — must not 500.

### Qualification `crush.json` (scratch workspace; sketch — schema freeze is Phase 26's job)

```json
{
  "$schema": "https://charm.land/crush.json",
  "options": { "disable_metrics": true, "disable_provider_auto_update": true },
  "providers": {
    "villa-qual": {
      "name": "villa qualification (local)",
      "type": "openai-compat",
      "base_url": "http://127.0.0.1:8081/v1",
      "models": [{ "id": "villa-qwen3-coder-30b", "name": "Qwen3-Coder 30B (qual)", "context_window": 65536, "default_max_tokens": 16384 }]
    }
  }
}
```
`[CITED: .planning/research/STACK.md sketch + charmbracelet/crush README — exact key validation happens when the file is loaded by the real v0.76.0 binary during qualification]`

### Catalog coder entry shape (seed.json, schema 3 — values from the verified artifact + arch tables)

```json
{
  "id": "qwen3-coder-30b-a3b",
  "display_name": "Qwen3-Coder-30B-A3B-Instruct (coder, all tiers)",
  "quant": "UD-Q4_K_XL",
  "weight_bytes": 17665334432,
  "n_layers": 48,
  "n_kv_heads": 4,
  "head_dim": 128,
  "kv_bytes_per_elem": 2,
  "default_ctx": 65536,
  "min_envelope_bytes": 28000000000,
  "tier_gb": 64,
  "unified_memory_safe": true,
  "backend_default": "vulkan",
  "bootstrap": false,
  "role": "coder",
  "agent_ctx": 65536,
  "cache_reuse_safe": true,
  "agent_sampling": { "temperature": 0.7, "top_p": 0.8, "top_k": 20, "repeat_penalty": 1.05 },
  "template_provenance": "unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF@b17cb02dd882d5b6ab62fc777ad2995f19668350 (embedded GGUF chat template)",
  "shards": [{
    "url": "https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF/resolve/b17cb02dd882d5b6ab62fc777ad2995f19668350/Qwen3-Coder-30B-A3B-Instruct-UD-Q4_K_XL.gguf",
    "filename": "Qwen3-Coder-30B-A3B-Instruct-UD-Q4_K_XL.gguf",
    "sha256": "2841aa314d916434860cfb8990347528dcdfe5c350dbcb9d1461dbee88ff2533",
    "size_bytes": 17665334432
  }]
}
```
(`cache_reuse_safe: true` here is the EXPECTED value — protocol step 9's probe is what licenses it. Next entries: `n_layers: 12`, `n_kv_heads: 2`, `head_dim: 256`, `agent_ctx: 131072`, `cache_reuse_safe` absent/false, `min_envelope_bytes` per the fit table. Field names are discretionary.)

Sampling preset values temperature 0.7 / top_p 0.8 / top_k 20 / repetition_penalty 1.05 are Qwen's official guidance for Qwen3-Coder `[CITED: unsloth.ai Qwen3-Coder run-locally guide, fetched 2026-06-12]`; applying the same preset to Qwen3-Coder-Next is `[ASSUMED]` pending the entry-specific check during qualification.

### Golden re-freeze (protocol for the ONE deliberate refreeze)

```bash
# Source: cmd/villa/detect_test.go (package-level update flag), recommend_test.go usage [VERIFIED]
go test ./cmd/villa -run Recommend -update   # regenerates cmd/villa/testdata/recommend.golden.json
git diff cmd/villa/testdata/recommend.golden.json   # review: pure addition above "schema_version": 3
```
The golden test injects a fixture `Recommendation` into `renderRecommend` — the fixture must populate the coder block so the frozen bytes exercise the full schema-3 shape.

### KV / GTT measurement capture (protocol step 4)

```bash
BASE=$(cat /sys/class/drm/card1/device/mem_info_gtt_used)
# ... start qual server (see Pattern 4 step 3), wait for "server is listening" ...
AFTER=$(cat /sys/class/drm/card1/device/mem_info_gtt_used)
echo "GTT delta: $(( (AFTER-BASE) / 1048576 )) MiB"
podman logs villa-qual 2>&1 | grep -E "KV buffer size|offloaded|Vulkan0 model buffer"
# Expected log lines (format string verified in build 9496): "... KV buffer size = XXXX.XX MiB"
```

## State of the Art

| Old Approach / Assumption | Current Reality | When Changed | Impact |
|---------------------------|-----------------|--------------|--------|
| "Pinned toolbox may predate Qwen3-Next arch (PR #16095) + Feb-2026 parser fixes" (STATE.md risk) | Pinned digest = llama.cpp build 9496 (June 2026): `qwen3next` arch + `Qwen3-Coder-` parser + `--cache-reuse` all present | Verified on-box 2026-06-12 | Re-pin likely unnecessary; D-11 record can be written from this evidence + the functional proof |
| `/metrics` exposes a KV-usage gauge | Removed upstream; villa's `internal/metrics` already documents this and derives ctx signals from `/slots` | pre-v1.2 | KV measurement = load-log `KV buffer size` lines + GTT delta, NOT `/metrics` |
| `--cache-reuse` on hybrid models crashes | Build 9496 detects and IGNORES it with a journal warning | upstream hardening | The `cache_reuse_safe` probe keys on the warning's absence + `cache_n` evidence, not on a crash |
| GGUF sizes from STACK ("17.7 GB", "49.6 GB", "36.3 GB") | Exact bytes + sha256 + revision captured (table above) | this session | Catalog entries can be authored without placeholder values |
| unsloth repos as moving targets | 30B repo last modified 2026-01-30; Next repo 2026-03-06 — both stable for months | — | Revision pins captured now are current heads; re-pin risk low during the phase |

**Deprecated/outdated:** none newly discovered; the v1.3 contract/seam disciplines apply unchanged.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Build 9496 contains ALL Feb-2026 tool-call parser fixes (ordering argument: June-2026 master build, parser string present) | Toolbox Vintage | Agent loop fails on a parser bug → fall back to re-pin candidate build 9579 (already local); D-10 absorbs this |
| A2 | Qwen3-Coder-Next DeltaNet recurrent state is small/constant (~75–150 MiB), not ctx-proportional | Architecture Params / Pattern 3 | Measured-vs-computed delta exceeds tolerance → fold the constant into the entry's weight padding or `min_envelope_bytes` |
| A3 | `cache_reuse_safe` outcomes: 30B-A3B true, Next false | Cache-Reuse Compatibility | None shipped wrong — protocol step 9 measures before the catalog freezes |
| A4 | Qwen3-Coder sampling preset applies to Qwen3-Coder-Next unchanged | Code Examples (sampling) | Wrong preset degrades agentic quality → check the unsloth Next run guide during qualification, adjust the entry value |
| A5 | Crush `--yolo` + `crush run` behave at v0.76.0 as documented (auto-accept, non-interactive single-turn-driving-multi-tool-loop) | Pattern 4 | Harness friction only — verify on first qual run; the protocol isolates harness faults (smoke test first) from model faults |
| A6 | Tier envelopes for 64/96 GB hosts (~31/~47 GiB, ½-RAM default) — only the 128 GB box is measured | Fit Pre-Computations | Entry `min_envelope_bytes`/`tier_gb` values misjudge smaller tiers — values are conservative; fit math (not the tier label) is the actual gate |

## Open Questions

1. **Should a user `--model` override be allowed to name a coder entry on the chat path?**
   - What we know: `pickOverride` uses `FindByID` (no role filter today); D-03 only mandates the auto-pick filter.
   - What's unclear: whether overriding chat to a coder model should warn-and-allow (unsafe-override precedent) or refuse.
   - Recommendation: warn-and-allow with a loud note (consistent with D-07 v1.0 override philosophy); add a table test either way. Must not perturb the existing golden.
2. **Exact JSON shape of the recommend `coder` block** (flat fields vs nested struct).
   - What we know: D-07 lists contents (model, quant, agent ctx, fit terms, residency mode, fits) and placement (above `SchemaVersion`, always stamped).
   - Recommendation: a nested struct (`"coder": {...}`) keeps the top level clean and makes "always stamped" unambiguous (`"coder": {"fits": false, "residency": "shared", ...}` on refusals); planner's call.
3. **30B-A3B at 128k on big tiers** — one entry at `agent_ctx: 65536` (recommended, fits everywhere) forgoes a 128k 30B profile on 96/128 GB tiers. Acceptable for v1.4? (Next covers the big tiers at 131072.)
4. **Whether qualification should also exercise Open WebUI-style plain chat on the coder models** — not required by CODER-03; recommend no (scope discipline), the agent loop + smoke test cover the contract Phase 25 consumes.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | go1.26.2 | — |
| rootless Podman | qualification serving | ✓ | 5.8.2 | — |
| Pinned toolbox image (`sha256:9a74e555…`) | qualification | ✓ (present locally) | llama.cpp build 9496 | re-pull by digest |
| Drifted-tag image (`sha256:9df33843…`, build 9579) | re-pin fallback candidate | ✓ (present locally) | llama.cpp build 9579 | — |
| villa services (`villa-llama`, `villa-dashboard`) | restore-after-qual baseline | ✓ active | — | — |
| GTT sysfs (`card1/device/mem_info_gtt_*`) | KV/GTT measurement | ✓ | total 67,149,369,344 B (62.54 GiB); used ~25.4 GiB (chat+embed resident) | — |
| Disk space | 3 GGUF pulls (~104 GB) + crush | ✓ | 468 GB free on /home | — |
| Crush v0.76.0 | agent loop | ✗ (not installed) | — | fetch tarball (URL + sha256 verified above) — dev-time, sanctioned |
| HF reachability | GGUF pulls | ✓ (API reachable during research) | — | — |
| `crush` collision check | harness | n/a | `command -v crush` empty — no conflicting binary | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** Crush v0.76.0 (one verified download).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven + golden files; no third-party assert/mock) |
| Config file | none (Makefile targets; `.golangci.yml` for lint) |
| Quick run command | `go test ./internal/catalog ./internal/recommend ./cmd/villa` |
| Full suite command | `make check` (go vet + go test ./...) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CODER-01 | Schema-3 decode, fail-closed defaults (`role` absent ⇒ chat, `cache_reuse_safe` absent ⇒ false), seed integrity, external schema-2 fallback warning | unit | `go test ./internal/catalog -run 'Schema\|Role\|Seed' ` | ❌ new tests in existing `internal/catalog/*_test.go` files |
| CODER-02 | Coder fit at `agent_ctx` post-reservation; residency `swap`/`shared` inequality; always-stamped block incl. refusal path; chat pick bit-identical (role filter) | unit | `go test ./internal/recommend -run 'Coder\|Pick'` | ❌ new tests alongside `recommend_test.go` |
| CODER-02 | `--json` contract: schema 3, coder block above `schema_version`, ONE golden re-freeze | golden | `go test ./cmd/villa -run Recommend` (refreeze: `-update` once) | ✅ `cmd/villa/recommend_test.go` + `testdata/recommend.golden.json` |
| CODER-03 | Agent-in-the-loop qualification, measured KV, residency assert, cache-reuse probe, toolbox decision | manual-only (on-hardware, multi-GB models, real GPU) — justified: requires gfx1151 + live podman; evidence recorded in phase verification artifacts + checkpoints | scripted commands per Pattern 4 | ❌ qualification scripts/evidence under the phase dir |

### Sampling Rate
- **Per task commit:** `go test ./internal/catalog ./internal/recommend ./cmd/villa`
- **Per wave merge:** `make check`
- **Phase gate:** full suite green + all CODER-03 checkpoint evidence captured before `/gsd-verify-work`

### Wave 0 Gaps
- None — the framework, golden harness, and `-update` discipline exist; new tests slot into existing packages. (The qualification scripts are phase artifacts, not test infrastructure.)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | no | — |
| V5 Input Validation | yes | Existing bounded-read + `DisallowUnknownFields` + path-traversal guard in `catalog.Load` extends to the new fields; validate `agent_ctx > 0` and sane sampling ranges on coder entries (refuse/skip, never silently clamp) |
| V6 Cryptography | yes (integrity) | SHA-256 verification via the existing `internal/download` shard path for GGUFs; sha256sum check for the Crush tarball (sigstore bundle available for extra provenance) — never hand-roll hashing |
| V10/V14 Supply chain & config | yes | Revision-pinned artifact URLs (`resolve/{revision}/…`, never `main`); image runs by digest only; no new ports beyond a loopback-only qual publish (`127.0.0.1:8081`) |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| HF repo tampering / re-uploaded quant with different template | Tampering | Repo+revision pin + per-shard sha256 (D-02); a mismatch fails the existing verify path |
| Slopsquatted/wrong toolbox build qualifying the catalog | Tampering/Repudiation | Run by digest; record `llama-server --version` in evidence |
| Prompt-injected tool calls during `--yolo` qualification escaping the workspace | Elevation of privilege | Disposable `/tmp` workspace, `--cwd` pinned, villa repo never the cwd; kill-switch env set; review the scratch repo only |
| Qualification agent phoning home | Information disclosure | `CRUSH_DISABLE_METRICS=1` + `DO_NOT_TRACK=1` + config kill switches (full negative-control proof is Phase 27's job; this is dev-time belt-and-braces) |
| Catalog JSON DoS via external override | DoS | Existing 1 MiB bounded reader (unchanged) |

## Sources

### Primary (HIGH confidence)
- On-box verification (2026-06-12): `podman run --rm <pinned digest> llama-server --version` → build 9496/94a220cd6; binary greps of `libllama.so` / `libllama-common.so` / `libllama-server-impl.so` (qwen3next arch, Qwen3-Coder parser, `--jinja` guard, cache-reuse degrade strings, `KV buffer size` format); `podman images` tag-drift observation; sysfs GTT readings; `go version`, disk, service status
- HuggingFace API (2026-06-12): repo revisions (`sha`/`lastModified`) and tree listings with LFS oids/sizes for `unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF` and `unsloth/Qwen3-Coder-Next-GGUF`; official `Qwen/Qwen3-Coder-30B-A3B-Instruct` and `Qwen/Qwen3-Coder-Next` `config.json` arch params
- GitHub releases API (2026-06-12): charmbracelet/crush v0.76.0 assets + `checksums.txt` (Linux x86_64 sha256)
- Codebase (read this session): `internal/catalog/{catalog.go,load.go,seed.json}`, `internal/recommend/{recommend.go,kv.go,envelope.go}`, `internal/inference/backend_vulkan.go`, `internal/metrics/llamacpp.go`, `cmd/villa/recommend.go`, `cmd/villa/testdata/recommend.golden.json`, `cmd/villa/detect_test.go` (-update flag)
- `.planning/phases/24-…/24-CONTEXT.md` (locked decisions), `.planning/REQUIREMENTS.md`, `.planning/STATE.md`

### Secondary (MEDIUM confidence)
- unsloth.ai Qwen3-Coder run-locally guide (fetched 2026-06-12) — sampling preset (temp 0.7 / top_p 0.8 / top_k 20 / rep 1.05), tool-calling fix notes
- charmbracelet/crush docs/issues via web search — `crush run` non-interactive mode, `--yolo` auto-accept ([issue #1030](https://github.com/charmbracelet/crush/issues/1030), [README](https://github.com/charmbracelet/crush/blob/main/README.md), [permissions docs](https://charmbracelet-crush.mintlify.app/configuration/permissions), [DeepWiki CLI usage](https://deepwiki.com/charmbracelet/crush/2.2-cli-usage))
- `.planning/research/{SUMMARY,PITFALLS,STACK}.md` — ratified milestone verdicts; llama.cpp discussions #13606/#22354/#20574 (cache-reuse hybrid semantics), #20198 (arguments-as-object), unsloth template-fix history

### Tertiary (LOW confidence, validate in phase)
- charmbracelet/crush #2649 (model-id shadowing — single report)
- DeltaNet recurrent-state size estimate (computed from config.json linear-attention dims)

## Metadata

**Confidence breakdown:**
- Standard stack / artifacts: HIGH — every artifact (image build, GGUF revisions/sizes/sha256s, Crush tarball checksum, arch params) verified live this session
- Architecture (schema/fit-stage patterns): HIGH — derived from the actual shipped code read this session; patterns are direct precedent reuse
- Qualification protocol: MEDIUM-HIGH — mechanics verified (flags, parser strings, `crush run --yolo` documented), but the protocol itself runs for the first time in this phase; the smoke-before-agent-loop ordering isolates harness faults
- Pitfalls: HIGH — tag drift, hybrid KV, `--jinja` guard, and DisallowUnknownFields traps all observed/verified directly

**Research date:** 2026-06-12
**Valid until:** ~2026-07-12 for code/schema mechanics (stable); re-verify HF revision heads and the tag-drift state if qualification slips more than ~2 weeks (the unsloth repos have been stable for months, but the toolbox tag rebuilds weekly)
