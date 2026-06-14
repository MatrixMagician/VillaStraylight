# Requirements: VillaStraylight — Milestone v1.4 Coding Agent

**Defined:** 2026-06-12
**Core Value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup, zero data leaving the box. v1.4 extends it to "and gives the operator a strictly-local terminal coding agent, wired to a fit-guarded coding model."

**Research verdicts ratified 2026-06-12** (see `.planning/research/SUMMARY.md`):

1. **Agent of record: Crush v0.76.0** (charmbracelet) — not OpenCode. OpenCode is structurally unlockable to the zero-outbound posture (unconditional `models.dev` startup fetch, runtime bun/npm downloads, autoupdate default-ON, air-gap closed upstream as not-planned). Crush's two outbound channels both have config kill switches villa renders — and proves at runtime, never flag-trusts.
2. **Residency: transactional swap-based coding mode** (composing `internal/modelswap`); install default is **shared mode** (agent rides the existing chat endpoint). Co-resident `villa-coder` deferred as a 128 GB fit-gated stretch. Residency mode is always an OUTPUT of recommend fit math at agent-profile ctx.
3. **Codebase memory: agent-native retrieval** (Crush LSP + ripgrep/glob + `AGENTS.md`/`CRUSH.md` context files). The original "Qdrant tracks the codebase" premise was researched and **rejected on evidence** (text embedder, chunking breaks code semantics, index staleness; leading agents ship embedding-free by design). villa-qdrant/villa-embed are untouched by v1.4.

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Coding Model & Fit

- [x] **CODER-01**: Catalog ships `role:"coder"` entries (Qwen3-Coder-30B-A3B for all tiers; Qwen3-Coder-Next for 96/128 GB tiers) with revision-pinned GGUF artifacts and template provenance (catalog schema 2→3, append-only)
- [x] **CODER-02**: `villa recommend` computes a coder fit at agent-profile context (after embed reservation + chat fit) and outputs an honest residency mode (`swap`/`shared`) as a fit-math output, never a preference (recommend schema 2→3, append-only)
- [x] **CODER-03**: Coder catalog entries are qualified agent-in-the-loop on hardware (real multi-step tool-call loop through llama-server `--jinja` on the pinned image, measured KV at agent ctx) before freezing; toolbox re-pin decision recorded

### Coding Mode

- [x] **CMODE-01**: Coding mode renders a tool-calling-ready llama-server unit delta (`--jinja`, agent ctx, sampling preset, `--cache-reuse` where model-compatible) behind the inference/orchestrate seams; addon-off renders byte-identical to v1.3
- [x] **CMODE-02**: User can enter/exit coding mode via a transactional verb composing `modelswap` (capture → under-load residency prove → cutover → verbatim rollback), with the chat model restored on exit

### Agent Delivery & Lockdown

- [ ] **AGENT-01**: villa installs a pinned Crush release via a villa-owned `go:embed` pin policy (version, per-platform asset, SHA-256 verified before install); autoupdate forced off
- [x] **AGENT-02**: villa renders `crush.json` as a derived artifact of `config.toml` — both kill switches set (`disable_metrics`, `disable_provider_auto_update`), exactly one villa provider block (loopback), villa-unique model ids, LSP entries for detected toolchains (missing `gopls` → WARN, never BLOCK)
- [x] **AGENT-03**: User launches the agent via a `villa code` launcher with belt-and-braces env lockdown (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`)
- [ ] **AGENT-04**: Agent binary/config drift from the pin policy is detected and surfaced with remediation, never auto-corrected

### Install & Verification

- [x] **INSTALL-03**: Coding agent is an optional `villa install` addon (mirroring the memory addon): gate → pre-stage coder GGUF + agent binary in the sanctioned outbound window → render → readiness proof including a real tool-call round-trip
- [x] **INSTALL-04**: Preflight gates the addon honestly: disk BLOCK, post-coder envelope BLOCK, cloud-credential WARN; uninstall removes the agent binary, rendered config, and addon artifacts
- [ ] **PRIV-06**: `villa verify agent` proves zero outbound at runtime, negative-control-first, covering agent **startup**; a llama-down negative control proves no silent cloud-model fallback

### Surfacing

- [ ] **SURF-01**: `status.Report` 3→4 append-only `coding` block (enabled, agent version + pin match, model, mode, residency) — single golden re-freeze; dashboard Agent panel, hidden-until-data
- [ ] **SURF-02**: `villa doctor` folds agent checks: binary/version drift, config drift, tool-call round-trip probe, under-load residency
- [ ] **SURF-03**: `villa backup`/`restore` cover the rendered agent config; the agent binary is identity-recorded and excluded (like model weights)
- [ ] **USAGE-03**: Agent token usage is attributed per-model in status/dashboard via the v1.2 usage core
- [ ] **USAGE-04**: Cache effectiveness (`timings.cache_n` vs `prompt_n`) is surfaced as an honest agent-speed signal

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Coding Agent (deferred)

- **CODER-V2-01**: Co-resident `villa-coder` Quadlet unit (fit-gated, realistically 128 GB tier only; design shelf-ready in `.planning/research/ARCHITECTURE.md`)
- **CODER-V2-02**: Qdrant/villa-embed-backed MCP semantic code search — only behind a pre-declared numeric eval it must WIN against the grep/LSP baseline to ship
- **CODER-V2-03**: Sandboxed/containerized agent profile for untrusted repos

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Qdrant code collection for codebase tracking | Researched and rejected (four-way convergence): nomic-embed-text is a text model, chunking breaks code semantics, vector index is stale the moment the agent edits a file; no recommended agent would read it |
| OpenCode as the agent | Cannot be config-locked to the zero-outbound posture; air-gapped support closed upstream as not-planned (#2224) |
| villa wrapping the agent's UI or running it as a service | The agent is an interactive terminal tool, not a service — no Quadlet, no up/down |
| Auto-switching models when the agent connects | Violates the "never auto-switch" precedent (ROCm); coding mode is an explicit verb |
| villa writing `AGENTS.md`/`CRUSH.md` into user repos | User working trees are sacrosanct; villa documents the convention only |
| Gateway/proxy layer between agent and llama-server | Needless moving part; loopback endpoint + rendered config suffice |
| Swap-based coding mode auto-reverting on agent exit | Mode change is explicit and surfaced in status, mirroring `villa backend set` |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CODER-01 | Phase 24 | Complete |
| CODER-02 | Phase 24 | Complete |
| CODER-03 | Phase 24 | Complete |
| CMODE-01 | Phase 25 | Complete |
| CMODE-02 | Phase 25 | Complete |
| AGENT-01 | Phase 26 | Complete (26-01: pin policy + pure VerifyTarball gate; 26-02: checksum-before-extract Install seam; 26-03: real binary hash pinned on-hardware from the verified tarball; autoupdate forced off) |
| AGENT-02 | Phase 26 | Complete (26-01: deterministic crush.json renderer + golden; 26-03 on-hardware: first-run render verified — kill switches, one loopback provider, villa- model id, LSP block) |
| AGENT-03 | Phase 26 | Complete (26-02: villa code launcher + lockdown env before exec + explicit villa-owned syscall.Exec + no-auto-flip; 26-03 on-hardware: round-trip + WARN-before-exec D-12 fix verified) |
| AGENT-04 | Phase 26 | Complete (26-01: pure DetectDrift core; 26-02: live drift surfacing at launch, never auto-corrected; 26-03: confident binary-drift signal + drift-refusal negative control verified on-hardware) |
| INSTALL-03 | Phase 27 | Complete |
| INSTALL-04 | Phase 27 | Complete |
| PRIV-06 | Phase 27 | Pending |
| SURF-01 | Phase 28 | Pending |
| SURF-02 | Phase 28 | Pending |
| SURF-03 | Phase 28 | Pending |
| USAGE-03 | Phase 28 | Pending |
| USAGE-04 | Phase 28 | Pending |

**Coverage:**

- v1 requirements: 17 total
- Mapped to phases: 17 ✓ (Phases 24–28)
- Unmapped: 0

---
*Requirements defined: 2026-06-12*
*Last updated: 2026-06-13 — CODER-03 marked complete (Phase 24-04: catalog frozen, D-13 toolbox keep recorded); Phase 24 fully complete (CODER-01/02/03)*
