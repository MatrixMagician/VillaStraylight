---
phase: 25-coding-mode-render-transactional-swap-verb
plan: 01
subsystem: infra
tags: [llama-cpp, jinja, quadlet, render-delta, seam-gate, golden-test, config-toml, coding-mode]

# Dependency graph
requires:
  - phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
    provides: "schema-3 coder catalog fields (Role/AgentCtx/CacheReuseSafe/AgentSampling) + build-9496 cache_reuse_safe qualification"
provides:
  - "inference.CodingModeSpec{Sampling, CacheReuseSafe} + inference.Sampling types"
  - "inference.RunSpec.CodingMode *CodingModeSpec (nil = byte-identical off path)"
  - "inference.appendCodingModeArgs seam helper (shared by Vulkan + ROCm ContainerArgs)"
  - "config.VillaConfig coding fields: CodingMode bool + CoderModel/CoderQuant (omitempty) + CoderAgentCtx (omitzero)"
  - "orchestrate.RenderInput.CodingMode *inference.CodingModeSpec + CoderAgentCtx int (D-05 plumbing)"
  - "internal/orchestrate/testdata/villa-llama-coding.container.golden (new append-only on-path render contract)"
  - "TestSeamGrepGate extended: coding-flag literals locked behind the inference seam in BOTH internal/ and cmd/villa walks"
affects: [25-02 transactional swap verb, 26 crush launcher, 27 install addon + verify agent]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional pointer descriptor on RunSpec ⇒ byte-identical off path BY CONSTRUCTION (nil = v1.3)"
    - "Caller pre-translates catalog.AgentSampling → inference.Sampling so the pure renderer never imports internal/catalog"
    - "Shared seam helper (appendCodingModeArgs) keeps flag literals in one place across both backends"

key-files:
  created:
    - internal/inference/containerargs_coding_test.go
    - internal/orchestrate/testdata/villa-llama-coding.container.golden
  modified:
    - internal/config/villaconfig.go
    - internal/config/villaconfig_test.go
    - internal/inference/inference.go
    - internal/inference/backend_vulkan.go
    - internal/inference/backend_rocm.go
    - internal/inference/seam_test.go
    - internal/orchestrate/orchestrate.go
    - internal/orchestrate/render.go
    - internal/orchestrate/render_test.go

key-decisions:
  - "CoderAgentCtx tagged omitzero (NOT omitempty) — BurntSushi/toml omitempty does not drop a zero int; this matches the v1.3 memory-stack int precedent (embedding_dim/qdrant_port/embed_port) and is required for the byte-identical-off guarantee"
  - "Seam-gate coding-flag regex anchored to QUOTED Go string literals (\"--jinja\" etc.) so it catches a real emission leak without false-matching catalog.go's bare-prose --cache-reuse provenance comment (the gate's documented DATA-vs-imperative scoping)"
  - "appendCodingModeArgs is a shared seam helper called by both backend ContainerArgs paths — flag literals live in exactly one place (backend_vulkan.go), ROCm calls the same helper"
  - "RenderInput carries the PRE-TRANSLATED inference.CodingModeSpec + CoderAgentCtx; the catalog→inference translation is the caller's job (Plan 02), keeping internal/catalog out of the pure renderer"

patterns-established:
  - "Pattern: optional *CodingModeSpec on RunSpec; nil = byte-identical v1.3 render (D-02)"
  - "Pattern: omitzero for zero-int omit-when-off config fields (BurntSushi/toml int gotcha)"
  - "Pattern: quoted-literal anchoring for seam-gate flag regexes to avoid prose false-positives"

requirements-completed: [CMODE-01]

# Metrics
duration: ~35min
completed: 2026-06-13
---

# Phase 25 Plan 01: Coding-Mode Render Delta Summary

**Tool-calling llama-server render delta (`--jinja` + single `-c <agent_ctx>` + A1-confirmed sampling preset + fail-closed `--cache-reuse 256`) appended behind the inference/orchestrate seams, driven by append-only `config.toml` fields, with the off path byte-identical to v1.3 and `TestSeamGrepGate` extended to lock the new flag literals.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-13 (Task 1)
- **Completed:** 2026-06-13
- **Tasks:** 3 (all tdd=true)
- **Files modified:** 9 modified + 2 created

## Accomplishments
- **CMODE-01 render half delivered.** Coding mode renders the tool-calling unit delta behind the `internal/inference` + `internal/orchestrate` seams; addon-off render is byte-identical to the v1.3 goldens (no existing golden mutated).
- **Append-only config contract.** `config.toml` gains `coding_mode` + resolved `coder_model`/`coder_quant`/`coder_agent_ctx`, all dropped on disk when coding mode is off (memory-stack precedent), so a non-coding install is byte-identical on disk.
- **Seam gate is a REAL cmd-tier guarantee.** The coding-flag regex was added to BOTH the `patterns` (internal/) and `cmdPatterns` (cmd/villa) maps, with a synthetic cmd/villa fixture asserting a `--jinja` leak is caught — closing the SC1 leak path before the Plan-02 `cmd/villa/coding-mode.go` noun exists.
- **LANDMINE A1 closed on hardware.** All sampling/flag spellings confirmed against the pinned build-9496 `llama-server --help` before the on-path golden was frozen.

## Task Commits

Each task was committed atomically (RED→GREEN folded into the task's feat commit; the failing test and its implementation landed together per the inference-seam same-commit precedent):

1. **Task 1: Append-only coding-mode config fields + RunSpec descriptor** — `1ce0ce9` (feat)
2. **Task 2: ContainerArgs coding-mode delta behind the seam + seam-gate lock** — `3cf9e40` (feat)
3. **Task 3: Render derives the descriptor from config + new on-path golden** — `c678519` (feat)

**Plan metadata:** committed separately (docs: complete plan) with SUMMARY/STATE/ROADMAP.

## A1 Sampling Flag Spellings — CONFIRMED (build-9496)

Probed live against the pinned vulkan-radv toolbox
(`docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad`) via
`podman run --rm <digest> llama-server --help`. **No correction needed** — every assumed
spelling is exact:

| Flag | Confirmed `--help` form |
|------|--------------------------|
| `--temp` | `--temp, --temperature N` (temperature, default 0.80) |
| `--top-k` | `--top-k N` (default 40) |
| `--top-p` | `--top-p N` (default 0.95) |
| `--repeat-penalty` | `--repeat-penalty N` (default 1.00) |
| `--cache-reuse` | `--cache-reuse N` (min chunk size for KV-shift reuse) |
| `--jinja` | `--jinja, --no-jinja` (jinja template engine for chat) |

The on-path golden (`villa-llama-coding.container.golden`) was frozen only after this confirmation.

## Descriptor Field Names Chosen (Plan 02 consumes these)

- `inference.RunSpec.CodingMode *inference.CodingModeSpec`
- `inference.CodingModeSpec{ Sampling *inference.Sampling; CacheReuseSafe bool }`
- `inference.Sampling{ Temperature float64; TopP float64; TopK int; RepeatPenalty float64 }`
- `orchestrate.RenderInput.CodingMode *inference.CodingModeSpec`
- `orchestrate.RenderInput.CoderAgentCtx int`
- `config.VillaConfig.CodingMode bool` (`coding_mode,omitempty`), `CoderModel` (`coder_model,omitempty`), `CoderQuant` (`coder_quant,omitempty`), `CoderAgentCtx int` (`coder_agent_ctx,omitzero`)
- **New golden path:** `internal/orchestrate/testdata/villa-llama-coding.container.golden`

## Files Created/Modified
- `internal/config/villaconfig.go` — four append-only coding fields + marshalVilla omit-when-off block
- `internal/inference/inference.go` — `RunSpec.CodingMode` field + `CodingModeSpec`/`Sampling` types
- `internal/inference/backend_vulkan.go` — `appendCodingModeArgs` seam helper + call after `llamaServerFlags`
- `internal/inference/backend_rocm.go` — symmetric call to the shared helper
- `internal/inference/seam_test.go` — `codingModeFlagPattern()` added to `patterns` + `cmdPatterns`; cmd-tier leak fixture assertion
- `internal/inference/containerargs_coding_test.go` — off-path-identical / on-path-delta / fail-closed cache-reuse tests (both backends)
- `internal/orchestrate/orchestrate.go` — `RenderInput.CodingMode` + `CoderAgentCtx`
- `internal/orchestrate/render.go` — D-05 single config→descriptor point (sets `spec.CodingMode`, overrides `spec.ContextLen`)
- `internal/orchestrate/render_test.go` — coding-mode golden + fail-closed + off-path-unchanged tests
- `internal/orchestrate/testdata/villa-llama-coding.container.golden` — new append-only on-path render contract

## Decisions Made
- **`omitzero` over `omitempty` for `CoderAgentCtx`** — see Deviations (Rule 1). The plan text said `omitempty`; the working v1.3 int precedent and BurntSushi/toml semantics require `omitzero`.
- **Quoted-literal anchoring for the seam-gate regex** — see Deviations (Rule 1). Avoids a false-positive on a pre-existing catalog doc comment while still catching a real emission leak.
- Followed all other plan specifics exactly (descriptor shape, no catalog import in the renderer, single `-c`, fail-closed cache-reuse, append-only golden).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `coder_agent_ctx` not dropped off-path with the planned `omitempty` tag**
- **Found during:** Task 1 (config marshal omit test)
- **Issue:** The plan's action text specified `toml:"coder_agent_ctx,omitempty"`. BurntSushi/toml's `omitempty` does NOT drop a zero-valued integer (it only drops empty strings/slices/maps), so the off-path marshal emitted `coder_agent_ctx = 0` — breaking the D-02/D-04 byte-identical-off guarantee. The RED test caught it.
- **Fix:** Tagged the int `toml:"coder_agent_ctx,omitzero"`, matching the EXISTING v1.3 memory-stack int fields in the same file (`embedding_dim,omitzero`, `qdrant_port,omitzero`, `embed_port,omitzero`). This is the established working precedent the plan's pattern map pointed at.
- **Files modified:** internal/config/villaconfig.go
- **Verification:** `TestCodingModeSaveOmitsKeysWhenDisabled` passes (no coding keys on disk when off; full round-trip when on).
- **Committed in:** `1ce0ce9` (Task 1)

**2. [Rule 1 - Bug] Seam-gate coding-flag regex false-matched a catalog provenance comment**
- **Found during:** Task 2 (TestSeamGrepGate after adding the new pattern)
- **Issue:** The plan-suggested regex `--jinja|--cache-reuse|--repeat-penalty` matched a pre-existing, legitimate bare-prose doc comment in `internal/catalog/catalog.go:88` ("whether llama.cpp --cache-reuse is proven safe…"), which describes the `CacheReuseSafe` field. That is DATA/provenance, not an imperative flag emission — exactly the false-positive class the gate's own top-of-file scoping comment deliberately excludes. Flagging it would weaken the gate into noise.
- **Fix:** Anchored each alternative to a leading double-quote (`"--jinja"`, `"--cache-reuse"`, `"--repeat-penalty"`), mirroring the gate's existing image-CONTEXT anchoring approach. The seam EMITS these as quoted Go string-literal args, so a real leak is caught; the bare-prose comment is not.
- **Files modified:** internal/inference/seam_test.go
- **Verification:** `TestSeamGrepGate` passes (catalog.go no longer flagged); the cmd-tier fixture assertion still catches a quoted `--jinja` leak; `TestSeamGateForbidsCodingFlagsInCmdFixture` passes.
- **Committed in:** `3cf9e40` (Task 2)

---

**Total deviations:** 2 auto-fixed (2 Rule 1 bugs)
**Impact on plan:** Both fixes were necessary for the plan's own load-bearing invariants (off-path byte-identity; a real, non-vacuous seam gate). Neither changed the plan's intent — they corrected a TOML-tag gotcha and a regex over-match against the working in-repo precedents the plan's pattern map already cited. No scope creep.

## Issues Encountered
None beyond the two auto-fixed deviations above (both caught by the RED tests / the seam gate, fixed inline).

## Known Stubs
None. The render half is fully wired against frozen contracts. The Plan-02 live wiring will populate `RenderInput.CodingMode`/`CoderAgentCtx` by resolving the coder catalog entry and translating `catalog.AgentSampling → inference.Sampling`; the plumbing and the descriptor types are in place and tested.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- **Plan 02 (CMODE-02, transactional verb) is unblocked.** It consumes: `inference.CodingModeSpec`/`Sampling`, `RunSpec.CodingMode`, the four `config.VillaConfig` coding fields, `RenderInput.CodingMode`/`CoderAgentCtx`, and the frozen `villa-llama-coding.container.golden`. The live `ReconcileAndWrite` closure needs no change beyond passing a cfg carrying `coding_mode` plus populating the new RenderInput fields from the resolved coder entry.
- **Off-path byte-identity verified:** `git diff --stat internal/orchestrate/testdata/` shows ONLY `villa-llama-coding.container.golden` added; no existing golden modified (T-25-05 mitigated).
- **`make check` GREEN** (vet + full suite incl. TestSeamGrepGate + all goldens + cmd/villa).
- **A1 is the only MEDIUM-confidence item and it is now CLOSED** on the gfx1151 dev box against build-9496. If the toolbox is ever re-pinned, the 24-TOOLBOX-DECISION.md Check 3 re-probe gate governs `cache_reuse_safe` (D-03).

## Self-Check: PASSED
- Created files verified on disk: `internal/inference/containerargs_coding_test.go`, `internal/orchestrate/testdata/villa-llama-coding.container.golden`, `25-01-SUMMARY.md`.
- Task commits verified in git history: `1ce0ce9`, `3cf9e40`, `c678519`.

---
*Phase: 25-coding-mode-render-transactional-swap-verb*
*Completed: 2026-06-13*
