# Architecture Patterns

**Domain:** Integrating a strictly-local coding agent (OpenCode, host-delivered) + a fit-guarded coder model + codebase memory into the shipped VillaStraylight Go control plane (v1.4 Coding Agent milestone)
**Researched:** 2026-06-12
**Confidence:** HIGH on codebase integration seams (every claim grounded in real files verified this session); HIGH on OpenCode delivery/config facts (official docs + releases page fetched); MEDIUM on coder-model KV math and OpenCode offline-flag completeness (version-sensitive — pin + re-verify in the implementing phase)

> This is a **subsequent-milestone** research doc. It does NOT re-derive v1.0–v1.3 architecture; it identifies exactly where the coding-agent addon bolts onto it, resolves the five open questions (delivery, residency, endpoints, codebase memory, modified-vs-new), and proposes a build order. Every codebase claim cites a real file verified on 2026-06-12.

---

## Recommended Architecture (one paragraph)

The coding agent is a **villa-managed host binary** (pinned OpenCode release, SHA256-verified, launched through a `villa code` wrapper that injects a villa-rendered config + offline-lockdown env), talking over **loopback** to a **second co-resident llama-server Quadlet unit `villa-coder`** (dedicated coder model, large ctx, `--jinja` tool calling) when the envelope fits — degrading honestly to **shared mode** (agent points at the existing `villa-llama` chat endpoint) on small envelopes. **Codebase memory is agent-native (LSP + ripgrep) by default** — the 2026 evidence is that top coding agents do not use vector RAG over the working repo — with an **optional, off-by-default semantic-index plugin that reuses `villa-embed`'s `/v1/embeddings`** (768-dim nomic-embed matches exactly); **villa-qdrant is NOT used for code memory** (the plugin stores its vector index per-project locally; pushing code vectors into Qdrant would require villa to build/maintain an MCP RAG server with the weakest effectiveness evidence of the three options).

```
HOST (Fedora, user session)                      CONTAINERS (rootless podman, villa.network)
┌──────────────────────────────┐
│ opencode (pinned binary)     │   /v1 chat ──► 127.0.0.1:8090 ─► villa-coder.container   (NEW)
│  launched via `villa code`   │                                   llama-server --jinja, coder GGUF
│  OPENCODE_CONFIG=<villa-     │   /v1 chat ──► 127.0.0.1:8080 ─► villa-llama.container   (existing)
│   rendered opencode.json>    │                                   (shared-mode fallback)
│  LSP + ripgrep (built-in)    │   /v1/embeddings (OPTIONAL)
│  [opt] codebase-index plugin │      └───────► 127.0.0.1:8091 ─► villa-embed.container   (MODIFIED:
│        .opencode/index/ local│                (new conditional      conditional loopback publish)
│        usearch+SQLite store  │                 loopback publish)
└──────────────────────────────┘                 villa-qdrant: UNCHANGED, stays unpublished,
   villa CLI: install/doctor/status/              NOT used for code memory
   backup/verify cover the addon
```

---

## (a) DELIVERY — host binary, villa-pinned. Not a container.

**Decision: villa downloads a pinned OpenCode release, verifies SHA256, installs under `$XDG_DATA_HOME/villa/bin/`, and renders its config from `config.toml`. Container delivery is rejected.**

### Evidence

| Fact | Source | Confidence |
|------|--------|------------|
| Active project is `anomalyco/opencode` (TypeScript/Bun, formerly sst/opencode); the old Go `opencode-ai/opencode` is the lineage that became Crush | [github.com/anomalyco/opencode](https://github.com/anomalyco/opencode) | HIGH |
| Releases ship per-platform zips **with SHA256 checksums per asset**; latest v1.17.4 (2026-06-12); cadence is multiple releases/week, sometimes several/day | [Releases page](https://github.com/anomalyco/opencode/releases) (fetched 2026-06-12) | HIGH |
| Built-in `opencode upgrade` self-updater + `autoupdate` config key (`false`/`"notify"`) | [opencode.ai/docs/config](https://opencode.ai/docs/config/) | HIGH |
| **No first-party container image.** Only community images exist (pilinux/opencode, openeuler/opencode, brockar/opencoded) | Docker Hub / GHCR search 2026-06-12 | MEDIUM (absence-of-evidence, cross-checked across 3 community projects that exist precisely because no official one does) |
| Config merge order includes `OPENCODE_CONFIG` env-var path overriding global `~/.config/opencode/opencode.json` | [opencode.ai/docs/config](https://opencode.ai/docs/config/) | HIGH |
| OpenCode performs **unconditional startup fetches** (`https://models.dev/api.json`, update check, plugin/npm manifests); `OPENCODE_DISABLE_MODELS_FETCH=1`, `"share": "disabled"`, `"autoupdate": false` mitigate but air-gap issues remain open upstream (#16117, #18492, #4959, #5554) | GitHub issues, anomalyco/opencode | MEDIUM-HIGH |

### Why host binary wins

- **TUI UX:** OpenCode is a terminal TUI working on the user's repo. Containerized TUI attach (`podman run -it` + workspace bind-mount) requires `:Z`/`:z` **SELinux relabeling of the user's arbitrary source tree** — mutating labels on directories villa does not own. That is invasive and un-villa-like; host binary needs zero SELinux work.
- **No upstream image to pin.** villa's discipline is digest-pinned *upstream* images (kyuz0 toolboxes, OWUI, Qdrant). For OpenCode villa would have to trust a third-party repackager or build/maintain its own image — both worse supply chains than the upstream release zip + per-asset SHA256 that already exists.
- **Pinning discipline maps cleanly.** Mirror `rocm-policy.json`: a `go:embed opencode-policy.json` in the new agent package pins `{version, platform→asset name, sha256}`. `villa coder upgrade` (explicit verb) moves the pin; OpenCode's own `autoupdate` is forced off. The multi-daily upstream cadence is exactly why villa must own the pin — an auto-updating agent breaks reproducibility and the no-surprise-outbound posture.
- **Strictly-local posture is enforceable on the host too.** `villa code` launches the binary with `OPENCODE_CONFIG=$XDG_CONFIG_HOME/villa/opencode.json` (villa-rendered, regenerated from `config.toml` — never hand-edited as authority, same rule as Quadlet units) plus `OPENCODE_DISABLE_MODELS_FETCH=1`, and the rendered config carries `"share": "disabled"`, `"autoupdate": false`, `"experimental": {"openTelemetry": false}`. Because mitigations are known-partial upstream, **`villa verify coder` must extend the v1.3 negative-control-first nft zero-outbound proof** (egress-open run must show the gate is real; blocked run must still complete a local completion). This is the single biggest honesty risk of the milestone — flag-trusting OpenCode's offline behavior would repeat the exact mistake the v1.3 PRIV-05 runtime proof was built to prevent.
- **Reuse:** the existing verified resumable downloader (`internal/download`) already does HEAD-verify → `.part` → SHA256 → atomic rename for GGUFs; the agent zip is a smaller, simpler case of the same seam.

**Trade-off accepted:** a host binary is outside the container sandbox — OpenCode executes shell commands on the host as the user. That is inherent to a coding agent's job (it edits the user's repo, runs the user's tests); villa's mitigation is OpenCode's own `permissions` config (rendered to `"ask"` for bash by default) and documentation, not sandbox theater. The agent binary is treated like model weights in backup: **identity recorded in the manifest, binary excluded, re-pull on restore.**

---

## (b) MODEL RESIDENCY — co-resident `villa-coder` unit when it fits; honest degradation to shared `villa-llama`; no swap-based mode.

**Decision: a second llama-server Quadlet unit (`villa-coder.container`) rendered through the existing `Backend` seam, co-resident with chat. `recommend.Pick` extends with a coder fit stage. If the coder model does not fit, the addon resolves to `shared` mode (agent uses the chat endpoint) — never a swap-based "coding mode".**

### Why not swap-based

Swap-based coding mode (re-rendering `villa-llama` with the coder model on demand) breaks chat while coding, adds a third transactional state machine, and reintroduces the D-09/D-10 hazard class v1.3 just closed. The cheap fallback — pointing OpenCode at the existing chat model — costs **zero** extra residency and `qwen3.6-35b-a3b` is itself a competent coder. Co-resident-or-shared covers all envelope tiers with no new transaction. (Community Strix Halo stacks reach the same shape with llama-swap; villa already owns orchestration, so importing llama-swap would be an anti-pattern — see Anti-Patterns.)

### Fit math extension (grounded in the real schema)

`internal/recommend/recommend.go` already does ordered reservation: schema-2 appended `embedding_reservation_bytes` is subtracted from the envelope **before** the chat fit, with a conservative 512 MiB default on typed-Unknown (D-01/D-02, verified lines 103–206). The coder stage extends the same pattern as a **third, last claimant**:

```
envelope' = envelope − embedding_reservation            (existing, v1.3)
chat_fit:  chat_weights + chat_KV@ctx + headroom ≤ envelope'   (existing)
coder_fit: coder_weights + coder_KV@coder_ctx ≤ envelope' − chat_total   (NEW)
  → fits  → coder_mode = "dedicated", largest role:"coder" catalog model wins
  → !fits → coder_mode = "shared" (advice line, never a refusal — shared always works)
```

Chat stays the primary claimant (the chat stack is the shipped product; the coder is the addon). `Recommendation` evolves **append-only, schema 2→3**: `coder_model`, `coder_quant`, `coder_ctx`, `coder_reservation_bytes`, `coder_mode`, `coder_considered` — one golden re-freeze, same discipline as v1.3's single 1→2 bump.

### Envelope tiers vs realistic coder GGUFs (approximate; the catalog entry pins exact numbers)

Coder candidate: **Qwen3-Coder-30B-A3B-Instruct** Q4_K_M ≈ 18.6 GiB weights; KV ≈ 96 KiB/token f16 (48 layers × 4 KV heads × 128 head_dim × 2(K+V) × 2 B), halved at q8_0 KV. Measured ~97 tok/s tg on Strix Halo ([strix-halo-guide](https://github.com/hogeheer499-commits/strix-halo-guide), MEDIUM). Chat claimant: qwen3.6-35b-a3b Q4 ≈ 20 GiB + KV + 0.5 GiB embed + headroom ≈ 27 GiB.

| RAM tier | Usable GTT envelope (default ÷2 / raised `ttm pages_limit`) | Coder co-residency verdict |
|----------|--------------------------------------------------------------|----------------------------|
| 128 GB | ~62.5 GiB observed on the dev host (detect-validated) / ~110 GiB raised | **YES, comfortable** — chat 27 + coder 18.6 + 64k-ctx q8_0 KV ~3 ≈ 49 GiB ≤ 62.5. Raised GTT also admits Qwen3-Coder-Next 80B-A3B (~46 GiB @4-bit, 256k ctx — [unsloth](https://unsloth.ai/docs/models/qwen3-coder-next), MEDIUM) as a premium catalog entry |
| 96 GB | ~48 GiB / ~84 GiB | **MARGINAL at default GTT** (49 > 48 → fits at 32k coder ctx or smaller chat model); YES raised |
| 64 GB | ~30 GiB / ~56 GiB | **NO at default** → `shared` mode; raised GTT fits only with a reduced-ctx coder or a smaller chat model — let the fit math decide, never special-case the tier |

The table is advisory; the shipped behavior is the inequality, exactly as today. `min_envelope_bytes`/`tier_gb` already exist per catalog entry (verified `internal/catalog/seed.json`, schema 2).

### llama-server flags the coder unit needs (vs the chat unit)

| Flag | Why | Confidence |
|------|-----|------------|
| `--jinja` | Required for tool-call template rendering — OpenCode is tool-call-driven; without it the agent's edit/bash tools silently degrade | HIGH ([llama-server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)) |
| `--ctx-size 65536` (tier-scaled; 131072 on big envelopes) | Agent sessions accumulate repo context far beyond chat norms; coder GGUFs support 256k native | HIGH |
| `--cache-reuse 256` | Biggest single agent-workload lever — prefix-cache reuse across the agent's many similar prompts | MEDIUM (community-converged, [rigel-computer](https://medium.com/rigel-computer-com/optimize-your-gpu-kv-cache-for-llama-cpp-opencode-co-13b6bc74f5ec)) |
| `-fa on` + `--cache-type-v q8_0`; **keep K-cache f16 (or at most q8_0) — never q4 K** | Halves V-cache memory; documented reports of **tool-call corruption from aggressive K-cache quantization** in coding agents — a residency/fit win that breaks the agent is a false economy | MEDIUM (same source + llama.cpp discussions) |
| `--parallel 1` (default) | `--parallel N` splits `--ctx-size` evenly across slots; OpenCode's `small_model` tasks (title gen) can share slot 1 sequentially. Only raise with ctx scaled ×N | HIGH (server README) |
| `--alias <catalog-id>` | OpenCode's provider config keys models by the exact `/v1/models` id — pin it to the catalog id so the rendered `opencode.json` and the unit can never drift | HIGH (provider docs + multiple setup guides) |

All of these are imperative literals → they live in `Backend.ContainerArgs(spec)` territory behind the inference seam (the seam grep-gate already enforces this); the coder unit **reuses the existing Vulkan/ROCm `Backend` implementations** with a service-role-parameterized spec — a coder unit gets ROCm support and residency markers for free, and the offload-asserting `RunningOffloadVerdict` applies unchanged (a CPU-fallback coder is FAIL, never false-green).

---

## (c) ENDPOINT EXPOSURE — loopback PublishPort, same pattern as villa-llama; one deliberate posture change for villa-embed.

The agent runs on the host, so container-DNS names are unreachable for it. Current published surface (verified in code):

- `villa-llama`: **already loopback-published** — `hostPublishAddr = "127.0.0.1"` lives behind the inference seam (`internal/inference/backend_vulkan.go:29`), asserted by `TestLoopbackPublish` (no `0.0.0.0:` publish). Shared mode needs **nothing new**.
- `villa-openwebui`: `127.0.0.1:3000:8080` (`internal/orchestrate/openwebui.go:71`). Unchanged.
- `villa-qdrant`, `villa-embed`: **no PublishPort at all**, enforced by `TestMemoryUnitsNoPublishPort` (T-19-01, `internal/orchestrate/memory_test.go:104`). 

What v1.4 needs:

1. **`villa-coder`: new loopback publish `127.0.0.1:<coder_port>:8080`** (default 8090, persisted in config). The `127.0.0.1` literal stays behind the inference seam; the *port number* becomes a spec field (the render parser already maps `-p/--publish` → `PublishPort`, `internal/orchestrate/render.go:264`). Extend the existing loopback test to cover the second unit.
2. **`villa-embed`: conditional loopback publish `127.0.0.1:<embed_host_port>:8080`, rendered ONLY when the optional codebase-index feature is enabled.** This deliberately relaxes T-19-01 from "no publish ever" to "no publish unless `coder_index_enabled`, and then loopback-only" — an explicit, test-renamed posture change, not a silent edit. Alternatives (systemd socket proxy, a villa Go reverse-proxy) add moving parts to dodge a publish that is exactly as loopback-safe as the three existing ones; rejected.
3. **`villa-qdrant`: stays unpublished.** Nothing on the host needs it (see (d)).

No socket-activation pattern is needed; rootless podman `PublishPort=127.0.0.1:p:p` is the established, STRIDE-reviewed mechanism in this codebase. `villa status` already asserts loopback posture — the new rows inherit that check.

---

## (d) CODEBASE MEMORY — agent-native (LSP + ripgrep) is the default; optional plugin reuses villa-embed; Qdrant is NOT the code-memory store.

This directly answers the milestone's "research validates effectiveness" clause, and the answer is opinionated:

### What OpenCode natively does (HIGH confidence, official docs)

- Built-in `grep`/`glob` tools are **ripgrep-backed** full-regex repo search respecting `.gitignore`; built-in LSP integration feeds type signatures, definitions, references, and diagnostics to the agent ([docs/lsp](https://opencode.ai/docs/lsp/), [docs/tools](https://opencode.ai/docs/tools/)). For this repo, that means `gopls` — preflight should WARN (not BLOCK) if absent.
- MCP servers are first-class config (`mcp` key) but each one adds permanent prompt-context cost; the docs themselves warn to be sparing.

### The 2026 effectiveness evidence (MEDIUM, consistent across sources)

Top SWE-bench-Verified agentic systems in 2026 do **not** use vector retrieval over the target repo; Claude Code, Codex CLI, and Aider all ship without embedding indexes — retrieval is exposed as tools (grep/LSP/file-read) and the LLM decides what to call ([MindStudio analysis](https://www.mindstudio.ai/blog/is-rag-dead-what-ai-coding-agents-use-instead), [agentic-search overview](https://buzzgrewal.medium.com/ai-agents-dont-need-vector-search-anymore-inside-the-agentic-search-stack-replacing-rag-in-2026-58efcabe4f6f); upstream Codex declined semantic indexing, openai/codex#5181). Code has explicit structure (imports, call graphs, types) that flat embeddings discard. **Conclusion: Qdrant-RAG-as-default for agent code memory fails the effectiveness test the milestone asked research to run.** The Graphmind-style fallback is likewise unnecessary as a *villa-built* component — LSP **is** the structural code graph, already integrated.

### The optional semantic layer that DOES reuse villa infrastructure (HIGH, repo fetched)

[`opencode-codebase-index`](https://github.com/Helweg/opencode-codebase-index) (OpenCode plugin, also exposable as MCP): tree-sitter chunking + hybrid BM25/vector search, and — decisive for villa — a **`custom` embedding provider speaking the OpenAI `/v1/embeddings` format with configurable `baseUrl`, `model`, and `dimensions: 768`**, i.e. a drop-in match for `villa-embed` (nomic-embed-text-v1.5, 768-dim). Its vector store is **per-project local** (`.opencode/index/`: SQLite + usearch + BM25 JSON) — private, zero services, survives without Qdrant.

### Decision matrix

| Option | What villa orchestrates | Effectiveness evidence | Verdict |
|--------|------------------------|------------------------|---------|
| **Agent-native LSP + ripgrep** | Nothing (config renders `lsp` enabled; preflight WARNs on missing gopls) | Strongest (2026 SOTA agents) | **DEFAULT — always on** |
| **codebase-index plugin → villa-embed** | Conditional villa-embed loopback publish + plugin pin in rendered opencode.json + `coder_index_enabled` flag | Positive but secondary (semantic recall helps exploration on unfamiliar/large repos; hybrid BM25+vector) | **OPTIONAL, default OFF** — third-party npm plugin = new supply-chain surface; pin version; pre-stage during the install outbound window (OpenCode fetches plugins via npm at startup — another outbound to gate in `verify coder`) |
| **villa-built MCP RAG server over Qdrant** | A whole new first-party service + collection lifecycle + chunking pipeline | Weakest; duplicates what the plugin does with more code | **REJECTED** — violates integration-first; build only the control plane |

**What villa must do vs configure:** villa *configures* (renders `lsp`, `mcp`/plugin, provider blocks into its `opencode.json`) and *orchestrates only the embed publish*. It never owns chunking, indexing, or a code collection. The `.opencode/index/` stores are user-project data — **excluded from `villa backup`** (like weights: per-project, regenerable).

---

## (e) MODIFIED vs NEW, and build order

### Modified (every touch is append-only / behind an existing seam)

| Component | File(s) (verified) | Change |
|-----------|--------------------|--------|
| config | `internal/config/villaconfig.go` | Append coder block after the v1.3 memory block (lines 60–82 pattern): `coder_enabled`, `coder_model`, `coder_quant`, `coder_ctx`, `coder_port` (default 8090), `coder_mode` (`dedicated`/`shared`, machine-resolved), `coder_index_enabled`, `embed_host_port`. Same omitempty/self-heal discipline |
| catalog | `internal/catalog/seed.json`, `catalog.go` | Schema 2→3: append `role` field (`"chat"` default / `"coder"`; absent = chat — old catalogs stay valid); add Qwen3-Coder-30B-A3B entry (+ optional Qwen3-Coder-Next for big envelopes) with real shard SHA256s and KV dims |
| recommend | `internal/recommend/recommend.go` | Coder fit stage after embed reservation + chat fit (see (b)); `Recommendation` schema 2→3 append-only; one golden re-freeze |
| inference | `internal/inference/` | Spec gains host-publish port + service-role; `--jinja`/ctx/KV-cache flags for the coder role live in `ContainerArgs` behind the seam (grep-gate `TestSeamGrepGate` keeps enforcing) |
| orchestrate | `internal/orchestrate/` | Render `villa-coder.container` (new tmpl, through the Backend seam like villa-llama, on `villa.network`, models from `villa-models.volume`); conditional villa-embed PublishPort; reconcile/WriteUnits untouched (they're unit-generic) |
| preflight | `internal/preflight/` | Coder gates: disk for coder GGUF + agent zip (BLOCK), post-coder envelope headroom (BLOCK), gopls present (WARN), node-free check N/A (plugin runs inside OpenCode's bundled Bun) |
| install | `cmd/villa/install.go`, `install_wizard.go`, new `install_coder.go` | Mirror `install_memory.go` addon pattern exactly (gate → pre-stage GGUF + agent zip + plugin tarball in the sanctioned outbound window → render → readiness proof: `/health` + residency + one real `/v1/chat/completions` tool-call probe) |
| doctor | `internal/doctor/` (+ `cmd/villa/doctor.go` wiring) | Coder checks: agent binary present + version==pin, villa-coder health + **offload-asserting residency under a real generation** (reuse the MEM-DOC pattern), rendered-config-vs-disk drift for `opencode.json` |
| status | `internal/status/status.go` + dashboard | `Report` 3→4 append-only, **landed in the final phase, one golden re-freeze** (the proven v1.2/v1.3 discipline): coder service row, mode (dedicated/shared), agent version + pin-match, coder model id |
| backup | `internal/backup/` | Manifest v3: include villa-rendered `opencode.json` + agent identity (version+sha256, binary excluded, re-pull on restore — same rule as weights); `.opencode/index/` explicitly excluded |
| verify | `cmd/villa/` (verify path) | `villa verify coder`: negative-control-first nft egress proof around a full agent round-trip (models.dev fetch, telemetry, npm, update check must all be absent/blocked-and-survivable) |
| uninstall | `cmd/villa/uninstall.go` | Remove villa-coder unit, agent binary, rendered opencode.json; leave user repos untouched |

### New

| Component | Shape |
|-----------|-------|
| `internal/coder` (pure core) | `go:embed opencode-policy.json` (pinned version + per-platform asset + sha256, deny-list room — the rocm-policy pattern); opencode.json renderer (`VillaConfig` → provider/model/lsp/plugin/permissions/lockdown JSON — config-as-source-of-truth, regenerated never hand-edited); pin/installed-version comparator. Host effects (download via `internal/download`, unzip, chmod, exec) injected as Deps — core stays exec-free per the v1.2 rule "one new pure core per decision-logic feature" |
| `cmd/villa/coder.go` | `villa code` (launcher: env lockdown + `OPENCODE_CONFIG` + exec), `villa coder status|upgrade` (explicit pin move) |
| `villa-coder.container` tmpl | In `internal/orchestrate/quadlet/`, sibling of the existing five units |

### Build order (dependency-driven)

1. **Catalog role + coder entries, recommend coder-fit stage** — pure, no host effects, everything downstream consumes the fit verdict; schema bumps (catalog 2→3, recommend 2→3) land here, goldens re-frozen once each.
2. **Inference spec port/role + orchestrate villa-coder render + conditional embed publish** — pure render + goldens; seam grep-gate and loopback tests extended.
3. **`internal/coder` delivery core + `villa code` launcher** — policy pin, checksum verify, opencode.json render, offline-lockdown env. Testable off-hardware end-to-end.
4. **Install addon + preflight gates** — wire 1–3 into `install_coder.go` (mirror `install_memory.go`), wizard screen, readiness proof with a real tool-call probe on the gfx1151 box.
5. **Codebase memory optional layer** — plugin pin + custom-provider wiring to villa-embed + on-hardware effectiveness validation (the milestone's explicit research-validates-effectiveness gate: compare agent task success/latency with index on/off before defaulting anything on).
6. **Surfacing + proofs LAST** — doctor coder checks, `status.Report` 3→4 + dashboard panel, backup manifest v3, `villa verify coder`. Single byte-frozen contract evolution in the final phase, exactly as v1.2 (P15) and v1.3 (P23) proved out.

---

## Anti-Patterns (domain-specific)

- **Trusting OpenCode's offline flags.** `OPENCODE_DISABLE_MODELS_FETCH` + `share:disabled` are known-partial (open upstream issues). A flag-trusted "strictly local" claim is the false-green this codebase exists to forbid → runtime nft proof, negative control first.
- **Letting OpenCode self-update.** `opencode upgrade`/autoupdate would silently move a binary villa attested by checksum. Force `autoupdate:false`; updates only via the villa pin.
- **Importing llama-swap (or building swap-based coding mode) for residency.** villa already owns Quadlet orchestration and has a fit engine; a swap proxy adds a second orchestrator and breaks chat during coding. Co-resident-or-shared needs no new transaction.
- **Writing code vectors into villa-qdrant.** Invisible to the agent's tooling, requires a first-party MCP RAG server, weakest effectiveness evidence; the plugin's local index + villa-embed endpoint achieves the reuse goal without it.
- **Hand-editing `opencode.json` as authority.** It is a rendered artifact of `config.toml`, same as Quadlet units; doctor should flag drift.
- **Re-typing llama-server coder flags outside the seam.** `--jinja`/KV/ctx/publish literals belong in `internal/inference` `ContainerArgs` — the grep-gate will catch leaks; don't fight it.
- **Aggressive K-cache quantization on the coder unit.** Saves memory, corrupts tool-call JSON in agent workloads (documented community reports) — the agent fails weirdly while residency stays green.

## Scalability / envelope considerations

Single-user, single-host by constraint. The only "scale" axis is the memory envelope, handled entirely by the recommend fit stage (tier table in (b)). The one operational concern: two co-resident llama-servers contend for iGPU compute — chat tok/s will dip while the agent generates. Surface honestly (status shows both; bench stays per-endpoint), don't engineer QoS.

## Sources

**Codebase (HIGH, verified 2026-06-12):** `internal/config/villaconfig.go` (fields, memory block 60–82), `internal/inference/backend_vulkan.go` (hostPublishAddr 127.0.0.1, loopback test), `internal/orchestrate/openwebui.go:71` + `memory_test.go:104` (publish posture, T-19-01), `internal/orchestrate/render.go:264` (publish parsing), `internal/recommend/recommend.go:103–206` (embedding reservation, schema 2), `internal/catalog/seed.json` (schema 2, entry shape, 4 model ids), `cmd/villa/install_memory.go` (addon pattern), `docs/ARCHITECTURE.md`, `.planning/PROJECT.md`.

**Web (fetched/searched 2026-06-12):**
- [anomalyco/opencode releases](https://github.com/anomalyco/opencode/releases) — v1.17.4, per-asset SHA256, cadence (HIGH)
- [OpenCode config docs](https://opencode.ai/docs/config/) — merge order, OPENCODE_CONFIG, autoupdate/share keys (HIGH); [providers](https://opencode.ai/docs/providers/), [LSP](https://opencode.ai/docs/lsp/), [tools](https://opencode.ai/docs/tools/), [MCP](https://opencode.ai/docs/mcp-servers/) (HIGH)
- OpenCode offline/air-gap issues #16117, #18492, #4959, #5554 + community offline forks (MEDIUM-HIGH)
- [Helweg/opencode-codebase-index](https://github.com/Helweg/opencode-codebase-index) — custom OpenAI-compatible embeddings (baseUrl/model/dimensions:768), local usearch/SQLite store, plugin+MCP modes (HIGH)
- [llama-server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) — --jinja, --parallel ctx-split, cache types (HIGH); [KV-cache for OpenCode agents](https://medium.com/rigel-computer-com/optimize-your-gpu-kv-cache-for-llama-cpp-opencode-co-13b6bc74f5ec), llama.cpp discussions (MEDIUM)
- [strix-halo-guide benchmarks](https://github.com/hogeheer499-commits/strix-halo-guide) — Qwen3-Coder-30B-A3B ~97 t/s (MEDIUM); [Unsloth Qwen3-Coder-Next](https://unsloth.ai/docs/models/qwen3-coder-next) — 80B-A3B, 256k ctx, ~46 GB @4-bit (MEDIUM)
- Agentic-search-vs-RAG evidence: [MindStudio](https://www.mindstudio.ai/blog/is-rag-dead-what-ai-coding-agents-use-instead), [agentic search stack 2026](https://buzzgrewal.medium.com/ai-agents-dont-need-vector-search-anymore-inside-the-agentic-search-stack-replacing-rag-in-2026-58efcabe4f6f), openai/codex#5181 (MEDIUM, cross-consistent)
- Community OpenCode container images (pilinux, openeuler, brockar) — evidence of no first-party image (MEDIUM)

---
*Architecture research for: VillaStraylight v1.4 Coding Agent milestone*
*Researched: 2026-06-12*
