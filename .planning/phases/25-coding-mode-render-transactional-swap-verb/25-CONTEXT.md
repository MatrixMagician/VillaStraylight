# Phase 25: Coding-Mode Render & Transactional Swap Verb - Context

**Gathered:** 2026-06-13
**Mode:** --auto (recommended defaults selected; decisions logged in 25-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 25 makes the running stack flip into a tool-calling-ready **coding mode** and back, via an explicit transactional verb — consuming the Phase-24 qualified coder catalog metadata (`agent_ctx`, agent sampling preset, `cache_reuse_safe`, residency mode `swap`/`shared`) and the D-13 toolbox-keep decision.

Two deliverables:

1. **CMODE-01 — Render delta.** With the addon enabled, coding mode renders a tool-calling-ready `llama-server` unit delta (`--jinja`, agent ctx, sampling preset, `--cache-reuse` only where the catalog entry declares `cache_reuse_safe=true`) BEHIND the `internal/inference` + `internal/orchestrate` seams. With the addon off, render output is **byte-identical to the v1.3 goldens** and the seam grep-gate stays green.

2. **CMODE-02 — Transactional enter/exit verb.** A new explicit verb composes the shipped `modelswap` forward ordering inside a cloned `backendswap` transactional frame (capture prior unit+config → cutover → under-load residency prove → verbatim rollback on any failure/non-pass). Entering swaps the chat model for the qualified coder model (in `swap` residency); exiting restores the chat model under the same discipline. A silent or partial CPU fallback during the prove step FAILS the swap and rolls back — idle-green is not green. The mode NEVER changes automatically (explicit verb only, ROCm `backend set` precedent).

**NOT in this phase:** Crush delivery / pin policy / `crush.json` / `villa code` launcher (Phase 26); install addon + preflight gates + `villa verify agent` egress/llama-down negative controls (Phase 27); any `status`/dashboard/doctor/backup surfacing of the coding feature (Phase 28); a real tool-call round-trip as a readiness gate (Phase 27 — this phase's prove is residency+generation, not a tool call); co-resident `villa-coder` unit (CODER-V2-01, deferred).

</domain>

<decisions>
## Implementation Decisions

### Render delta mechanism (CMODE-01)
- **D-01:** The tool-calling delta is carried by an OPTIONAL coding-mode descriptor threaded through `RunSpec` (and the `orchestrate` `RenderInput`), populated ONLY when coding mode is active: agent ctx (→ `-c`), the catalog-declared sampling-preset tokens, `--jinja` on, and `--cache-reuse` gated on `cache_reuse_safe`. The existing `backendVulkan`/`backendROCm` `ContainerArgs` append these flags behind the seam. NO new `Backend` implementation, NO new backend-resolver branch — coding mode is a render delta over the existing backend, not a new backend.
- **D-02:** Addon-off is byte-identical BY CONSTRUCTION: when the coding-mode descriptor is zero/absent, `ContainerArgs` and the rendered unit are emitted exactly as v1.3 — the existing `villa-llama.container` / `villa-llama-rocm*.container` goldens MUST NOT change in the off path. The flag literals (`--jinja`, `--cache-reuse`, sampling tokens) live in `internal/inference` (seam-locked); `TestSeamGrepGate` stays green. A render-delta golden change is admitted ONLY for the coding-mode-ON variant (a new golden, append-only — not a mutation of an existing one).
- **D-03 (carried forward, Phase 24):** `--cache-reuse` is appended ONLY where the catalog entry declares `cache_reuse_safe=true` (absent ⇒ false, fail-closed). This claim is **build-9496-scoped** — if the toolbox digest is ever re-pinned, the re-probe gate in `24-TOOLBOX-DECISION.md` Check 3 MUST be re-run before trusting `cache_reuse_safe`.

### Coding-mode state representation
- **D-04:** "Coding mode is active" is persisted in `config.toml` (the single source of truth) as an append-only OPTIONAL field set following the v1.3 memory-stack precedent (`memory_enabled` + companions): a `coding_mode` bool plus the resolved active coder model / quant / agent_ctx (resolved from the catalog AT ENTER, not re-picked later). All fields `omitempty`/omit-when-off so a non-coding install is **byte-identical on disk** (same guarantee as the memory fields). Units regenerate from config; the mode survives restart/reboot. Never a hand-edited unit, never an ephemeral in-memory flag.
- **D-05:** Render/reconcile derives the coding-mode descriptor (D-01) from these persisted config fields, so `ReconcileAndWrite(cfg)` is the single point that turns "coding_mode = true in config" into the rendered tool-calling unit — exactly as model/backend already flow config → render.

### Transactional verb (CMODE-02)
- **D-06:** Verb shape: `villa coding-mode enter` / `villa coding-mode exit` (two explicit subcommands). NOT `villa code` — that name is RESERVED for the Phase-26 agent launcher. The mode changes ONLY via this verb (explicit-only, ROCm `backend set` precedent); nothing auto-flips it. The core returns a typed `Result` (not an exit code); the cobra caller maps it to exit code + messages, mirroring `modelswap.Result` / `backendswap.Result`.
- **D-07:** Implement a new pure, `Deps`-injected core (e.g. `internal/codingmode`) that **clones the `backendswap` transactional frame** verbatim in shape: capture the prior `villa-llama.container` bytes + prior `VillaConfig` value snapshot STRICTLY before any mutation → mutate (persist config, reconcile+write, restart inference only) → **under-load Prove** → ANY mutate error or non-pass verdict rolls back to the verbatim captured unit+config with honest rollback-incomplete reporting (Pitfall 5). It composes `modelswap`'s forward ordering for the model change. The core is LITERAL-FREE of backend markers — residency/generation markers arrive only through the injected `Prove` seam (live wiring in `cmd/villa`). Do NOT fork `modelswap`; do NOT inline the state machine into `cmd/villa`.
- **D-08:** Exit restores the chat model (the captured prior model from before enter) under the SAME transactional discipline — capture → cutover → prove → rollback. Exit is symmetric to enter, not a bare config flip.

### Under-load prove & residency-mode handling
- **D-09:** The cutover Prove step reuses the SAME residency discipline as `backendswap`: a real generation probe AND a positive `RunningOffloadVerdict` (under load) — cutover succeeds ONLY on `ProveStatusPass`. A silent/partial CPU fallback or a ready+health-200-but-residency-FAIL verdict triggers verbatim rollback. Idle-green / is-active / health-200 alone is NEVER success. A real tool-call round-trip is explicitly DEFERRED to Phase 27 readiness — it is NOT part of this phase's prove.
- **D-10:** Residency mode (Phase-24 output) drives the enter path: in `swap` residency the verb performs the model swap (chat → coder) as above — this is the realized path on the gfx1151 box (all 3 Phase-24 coder entries qualified PASS at `swap`). In `shared` residency (no coder entry fits standalone) the verb applies the tool-calling render delta (`--jinja`, agent ctx if it fits, sampling) to the EXISTING chat-served endpoint WITHOUT a model change — still transactional, still proved. `swap` is the primary path for v1.4; the exact `shared`-mode operator UX is a planning detail (recommend: implement swap fully, shared applies render-delta-only; never silently degrade swap→shared without surfacing it).

### Claude's Discretion
- Exact Go package name (`internal/codingmode` vs `internal/modeswap`), field/JSON/TOML key names for the new config fields (follow the memory-stack `omitempty` naming precedent; keep append-only).
- Exact `RunSpec`/`RenderInput` descriptor field names and shape for the coding-mode delta.
- Golden test organization for the coding-mode-ON render variant (`-update` discipline as shipped; off-path goldens untouched).
- Human-readable CLI output of `villa coding-mode enter|exit` (status lines, rollback messaging) — mirror `backend set` rendering.
- The precise composition of the live `Prove` closure (PollHealth + GenerationProbe + RunningOffloadVerdict) in `cmd/villa`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope, requirements & prior context
- `.planning/ROADMAP.md` — Phase 25 goal + Success Criteria 1–3 (the contract this phase satisfies); Phase 26–28 boundaries (what is explicitly NOT here)
- `.planning/REQUIREMENTS.md` — CMODE-01, CMODE-02 (this phase's requirement set)
- `.planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-CONTEXT.md` — Phase-24 decisions D-01..D-13: catalog schema-3 coder fields (`role`, `agent_ctx`, `cache_reuse_safe`, sampling preset, `template_provenance`), residency-mode derivation (`swap`/`shared`), the `coder` recommend block
- `.planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-TOOLBOX-DECISION.md` — D-13 toolbox KEEP decision + **Check 3 re-probe gate** for `cache_reuse_safe` if the toolbox is ever re-pinned (build-9496-scoped)
- `.planning/STATE.md` §Blockers/Concerns — agent-scale KV / fit-at-rendered-ctx, `--cache-reuse` hybrid-model caution, Phase-27 egress threat (not this phase)

### Milestone research verdicts
- `.planning/research/SUMMARY.md` — v1.4 verdicts: Crush-not-OpenCode, swap-based residency
- `.planning/research/PITFALLS.md` — Pitfall 3 (jinja/tool-call template landmines, revision pinning), Pitfall 4 (agent-scale KV, fit-at-rendered-ctx), `--cache-reuse` hybrid-model incompatibility
- `.planning/research/ARCHITECTURE.md` — render-delta + swap-verb modified-component inventory; Phase-7/D-09 render-delta precedent

### Contracts & code this phase modifies/composes
- `internal/backendswap/backendswap.go` — the transactional frame to CLONE (capture→mutate→prove→rollback; `Deps`/`Result`/`ProveVerdict`; literal-free of backend markers; honest rollback-incomplete)
- `internal/modelswap/modelswap.go` — the forward ordering to COMPOSE (resolve→fit-guard→pull→persist-before-unit-work→reconcile→restart-inference-only)
- `internal/inference/inference.go` — `Backend` interface, `RunSpec` (`ContextLen`), `Status` PASS/WARN/FAIL; where the coding-mode descriptor threads through
- `internal/inference/backend.go` — `BackendFor` single polymorphism point (coding mode is a delta over the resolved backend, NOT a new branch)
- `internal/inference/backend_vulkan.go` / `backend_rocm.go` — `ContainerArgs` + `llamaServerFlags` (where `--jinja`/`--cache-reuse`/sampling append behind the seam); `internal/inference/seam_test.go` (`TestSeamGrepGate`)
- `internal/orchestrate/render.go` / `orchestrate.go` / `quadlet/*.tmpl` — unit render + `ReconcileAndWrite`; the byte-frozen `internal/orchestrate/testdata/villa-llama*.container.golden` files (off-path MUST stay byte-identical)
- `internal/config/villaconfig.go` — `VillaConfig`; the v1.3 memory-stack `omitempty` / omit-when-off precedent (`MemoryEnabled` + companions) for the new coding-mode fields

### Standing conventions
- `CLAUDE.md` / `docs/ARCHITECTURE.md` — pure-core + injectable-seam, config-is-truth, seam-locked backend literals, byte-frozen-golden (append-only + schema-bump), offload-asserting (silent CPU fallback = FAIL)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backendswap.Run` (`internal/backendswap/backendswap.go`): the proven capture→mutate→prove→rollback state machine with honest rollback-incomplete reporting — the coding-mode core clones this shape verbatim, swapping the "backend" axis for the "model + render-delta" axis.
- `modelswap.Run` (`internal/modelswap/modelswap.go`): the guarded forward ordering (resolve→fit-guard→pull→persist-before-unit-work→reconcile→restart-inference-only) — composed for the model change inside the transactional frame.
- `RunSpec`/`ContainerArgs` (`internal/inference`): the single assembly point for llama-server flags — the coding-mode delta appends here behind the seam; `llamaServerFlags` is the precedent for fixed-arg, injection-free flag lists.
- v1.3 memory-stack config fields (`internal/config/villaconfig.go`): `MemoryEnabled` + `omitempty`/omit-when-disabled marshal path is the exact precedent for append-only, byte-identical-when-off coding-mode config fields.
- `RunningOffloadVerdict` / generation-probe wiring (`cmd/villa` live `Prove` closures, reused by `backend set`): drop-in for the under-load prove seam.

### Established Patterns
- Transactional frame: capture verbatim prior unit bytes + prior config STRICTLY before mutate; roll back to the captured bytes on any failure; never claim a clean no-op when a rollback step errored (Pitfall 5).
- Seam-locked literals: `--jinja`/`--cache-reuse`/sampling tokens MUST live in `internal/inference`; `TestSeamGrepGate` walks `internal/` + `cmd/villa`.
- Byte-frozen goldens evolve append-only: off-path unit goldens unchanged; a NEW coding-mode-ON golden is added, not an existing one mutated.
- Config-is-truth: mode state in `config.toml`; units regenerate from config; explicit-verb-only mutation (ROCm `backend set` precedent).
- Offload-asserting prove: a silent/partial CPU fallback is a FAIL → rollback; idle-green is not green.

### Integration Points
- New `cmd/villa/coding-mode.go` (or similar) thin cobra caller → new `internal/codingmode` core via a `liveCodingModeDeps()` closure (mirrors `liveBackendSwapDeps`).
- `internal/config` — append-only coding-mode fields (schema/marshal path).
- `internal/inference` + `internal/orchestrate` — render-delta descriptor threaded through; new coding-mode-ON golden.
- Consumes Phase-24 catalog metadata: `agent_ctx`, agent sampling preset, `cache_reuse_safe`, and the recommend `coder` residency-mode output.
- Phase 26 consumes: the entered coding-mode endpoint (the `villa code` launcher points Crush at the loopback inference URL); Phase 27 adds the install-addon gating + egress/tool-call proofs ON TOP of this verb.

</code_context>

<specifics>
## Specific Ideas

- All 3 Phase-24 coder entries qualified PASS at `swap` residency on the pinned `vulkan-radv` digest (`sha256:9a74e555…`, llama.cpp build 9496) — so `swap` is the realized, primary path; `shared` is the honest small-envelope fallback.
- `--cache-reuse` on hybrid Qwen3-Coder-Next works via DeltaNet context checkpoints (not KV shift) under build 9496 — `cache_reuse_safe=true` is build-9496-scoped; re-probe if the toolbox is re-pinned.
- The verb name `villa coding-mode enter|exit` deliberately avoids `villa code` (reserved for the Phase-26 launcher).

</specifics>

<deferred>
## Deferred Ideas

- Real tool-call round-trip as a readiness gate, egress-zero + llama-down negative controls — Phase 27 (`villa verify agent`). This phase's prove is residency+generation only.
- Crush delivery / SHA-256 pin policy / `crush.json` render / `villa code` launcher / env lockdown — Phase 26.
- `status`/dashboard/doctor/backup surfacing of the coding feature (`status.Report` 3→4) — Phase 28.
- Co-resident `villa-coder` Quadlet unit (no swap) — CODER-V2-01 (v2 stretch, deferred).
- Exact `shared`-residency operator UX beyond render-delta-only — refine in planning if the swap path leaves it ambiguous; do not silently degrade swap→shared.

</deferred>

---

*Phase: 25-Coding-Mode Render & Transactional Swap Verb*
*Context gathered: 2026-06-13 (--auto mode, single pass)*
