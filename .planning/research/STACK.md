# Stack Research

**Domain:** Strictly-local coding agent addon for a Go control-plane CLI orchestrating llama.cpp (Vulkan) + Open WebUI + Qdrant via rootless Podman/Quadlet on AMD Strix Halo (gfx1151) / Fedora — v1.4 "Coding Agent"
**Researched:** 2026-06-12
**Confidence:** HIGH on the agent landscape (versions/licenses/outbound behavior verified against GitHub releases, issues, and official docs today); HIGH on GGUF file sizes (HF repo listings); MEDIUM on throughput numbers (third-party Strix Halo benchmarks) and KV-cache estimates (computed from architecture, labeled as estimates); MEDIUM on exact `crush.json` schema details (freeze in phase research).

## Scope note

SUBSEQUENT-milestone study. The shipped v1.0–v1.3 stack — Go 1.26.2, cobra, chi, ghw, rootless Podman v5 + Quadlet, llama.cpp Vulkan/ROCm behind the `Backend` seam, Open WebUI, digest-pinned Qdrant + `villa-embed` (nomic-embed-text-v1.5, 768-dim) — is **fixed and NOT re-researched**. This file covers only: (a) which coding agent, (b) which coding model(s) for the gfx1151 memory tiers, (c) what codebase-memory machinery the agent actually needs.

**Three headline findings (each reverses a default assumption in PROJECT.md):**

1. **Agent: recommend Crush (charmbracelet), not OpenCode.** OpenCode was the leading candidate, but it cannot be locked down to villa's zero-outbound posture by config: it unconditionally fetches `models.dev/api.json`, downloads provider/plugin npm packages at runtime via embedded bun, auto-downloads LSP servers, and upstream **closed air-gapped support as "not planned"** (anomalyco/opencode #2224; #16117 closed as dup). Crush is a single static Go binary whose ONLY two outbound channels (pseudonymous metrics, Catwalk provider-DB refresh) both have **documented config kill switches** villa can generate (`disable_metrics`, `disable_provider_auto_update`) with an embedded provider DB as offline fallback.
2. **Codebase memory: do NOT extend Qdrant/villa-embed with a code collection.** The 2025–2026 industry consensus among serious coding agents (Cline, Claude Code, OpenCode, Crush) is that embedding-RAG over code is inferior to agentic search (grep/glob/AST/LSP): chunking breaks code semantics, indexes go stale on every commit, and a vector copy of the repo doubles the security surface. Crush ships LSP integration + agentic file tools natively. Codebase "memory" = `CRUSH.md`/`AGENTS.md` context files + LSP — **zero new services**.
3. **Model: Qwen3-Coder family, tiered by GTT envelope; swap-based "coding mode" is the universal mechanism.** Qwen3-Coder-30B-A3B (17.7 GB at UD-Q4_K_XL) fits every tier; Qwen3-Coder-Next (80B-A3B hybrid-attention MoE, 49.6 GB at UD-Q4_K_XL) is the quality pick for the 128 GB tier. Co-residency with the chat model is only honest on 96/128 GB tiers — the swap mechanism (reusing `internal/modelswap`) works on all three.

## Recommended Stack

### Core Technologies (NEW)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **Crush** (charmbracelet/crush) | **v0.76.0** (released 2026-06-05; pin exact release artifact + checksum) | The terminal coding agent — agentic read/write/exec + LSP over the local OpenAI-compatible endpoint | Single static Go binary (matches villa's distribution ethos; no Python/Node/bun runtime); plain-JSON `crush.json` config that villa can generate deterministically; `type: "openai-compat"` provider points straight at `villa-llama`; **both outbound channels are config-killable** (`disable_metrics` / `CRUSH_DISABLE_METRICS` / `DO_NOT_TRACK`, and `disable_provider_auto_update` / `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE` with an embedded Catwalk provider DB for offline); built-in LSP integration (`lsp` config block — gopls, pyright, rust-analyzer, etc.) that uses **locally installed** servers, never auto-downloads; MCP support for future extension; Fedora/RHEL repo + GitHub release tarballs with checksums. Active: ~weekly releases through June 2026. |
| **Coding model — default (all tiers):** Qwen3-Coder-30B-A3B-Instruct, GGUF | `unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF` **UD-Q4_K_XL (17.7 GB)** | The fit-everywhere agentic coding model | MoE with 3B active params → token generation is fast on Strix Halo's bandwidth-bound iGPU (third-party Strix Halo Vulkan/RADV reports ~70–98 tok/s tg, vs ~10–15 tok/s for a 24B dense model); 256K native context; Apache-2.0; purpose-trained for agentic tool use; unsloth quants carry the Aug-2025 tool-calling chat-template fixes llama-server needs. 17.7 GB fits even the 64 GB tier's ~31 GiB GTT envelope with KV to spare. |
| **Coding model — quality (128 GB tier, 96 GB at Q3):** Qwen3-Coder-Next, GGUF | `unsloth/Qwen3-Coder-Next-GGUF` **UD-Q4_K_XL (49.6 GB)**; UD-Q3_K_XL (36.3 GB) for the 96 GB tier | Best open agentic coder that fits 128 GB unified memory | 80B-total / 3B-active hybrid-attention MoE (Gated-DeltaNet-style + sparse full attention) → near-flagship agentic coding (unsloth: "performance comparable to models 10–20× larger", RL-trained for long-horizon tool use with failure recovery) at A3B generation speed, with a **small KV cache** (only a fraction of layers carry full attention) so 128K+ agent contexts are cheap; 256K native ctx. Third-party Strix Halo numbers: ~421 tok/s pp (Vulkan RADV), ~524 tok/s pp (ROCm 7.2). |
| **llama-server tool-calling flags** | `--jinja` added to the rendered `villa-llama` unit in coding mode | OpenAI `tools` API support — the agent's function-calling contract | llama-server hard-errors `"tools param requires --jinja flag"` when an agent sends `tools` without it. `--jinja` uses the GGUF-embedded chat template, which (for unsloth Qwen3-Coder quants) includes the XML tool-call parser fixes. This is a render-delta in `internal/orchestrate`, gated on coding mode — exactly the Phase-7-style byte-frozen unit-delta pattern. |

### Supporting Libraries (first-party Go — none new)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **(none required)** | — | The milestone adds NO new Go module dependency | Agent binary download reuses the `internal/download` pattern (SHA-256-verified artifact pull during the sanctioned install window); coding-model fit reuses `internal/recommend` (envelope math + embed reservation already shipped in v1.3 P22); model switch reuses `internal/modelswap`; config generation is a new pure core (`internal/agentconf` or similar) emitting `crush.json` via `encoding/json` (stdlib); health/status probes reuse the bounded-loopback patterns. |
| `encoding/json` (stdlib) | — | Render `crush.json` from `config.toml` | Config stays the single source of truth; `crush.json` becomes a villa-rendered artifact like Quadlet units — regenerated, never hand-edited as authority. |

> **Hard rule honored:** no cgo, no new runtime (no Python/Node/bun on the host), `villa` stays a single static `CGO_ENABLED=0` binary. The agent itself is also a single static Go binary.

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| Crush release artifacts | `crush_<ver>_Linux_x86_64.tar.gz` + `checksums.txt` from GitHub releases | villa pins an exact version + SHA-256 (mirror of the digest-pin discipline for images). Charm also publishes a Fedora/RHEL yum repo — viable alternative, but a villa-managed download keeps the version pinned and checksummed under villa's control. |
| LSP servers (optional, user-supplied) | gopls / pyright / rust-analyzer / typescript-language-server | Crush uses whatever is on `$PATH` per its `lsp` config block. villa should render sensible `lsp` entries but treat missing servers as a WARN (typed-Unknown style), never a BLOCK — the agent degrades gracefully to grep/glob. |

## Delivery mode: host binary, not container

**Recommendation: install Crush as a host binary** (villa-downloaded, version-pinned, checksum-verified, placed under `$XDG_DATA_HOME/villa/bin/` or `~/.local/bin/`), **not** a Quadlet container.

- A coding agent's whole value is host-side: the user's repos, toolchains, LSP servers, git, and shell. A containerized agent needs the workspace bind-mounted plus every toolchain baked into the image — it cripples LSP and `bash` tool use, and there is no official Crush container image to pin anyway.
- The strictly-local posture is enforced by **config** (the two kill switches villa writes) and verified at **runtime** (a `villa verify agent`-style negative-control-first egress proof, mirroring `villa verify memory`) — not by network namespace. A container would only add false comfort while breaking the feature.
- Consequence for the install story: the agent binary pull joins images/models in the sanctioned outbound window ("image/model/agent pulls" — PROJECT.md already words v1.4 this way).

## villa-generated `crush.json` (sketch — freeze exact schema in phase research)

Villa renders this from `config.toml` (global config at `~/.config/crush/crush.json`); it is a derived artifact like Quadlet units:

```json
{
  "$schema": "https://charm.land/crush.json",
  "options": {
    "disable_metrics": true,
    "disable_provider_auto_update": true
  },
  "providers": {
    "villa": {
      "name": "VillaStraylight (local)",
      "type": "openai-compat",
      "base_url": "http://127.0.0.1:8080/v1",
      "models": [
        {
          "id": "qwen3-coder-30b-a3b",
          "name": "Qwen3-Coder-30B-A3B (local)",
          "context_window": 65536,
          "default_max_tokens": 16384
        }
      ]
    }
  },
  "lsp": {
    "go": { "command": "gopls" }
  }
}
```

Belt-and-braces: also set `CRUSH_DISABLE_METRICS=1` (and optionally `DO_NOT_TRACK=1`) in the shell wrapper/launcher villa provides. The `models[].id` must match what the rendered llama-server advertises. Note Crush warns that a **project-local** `crush.json` can execute `$(...)` at load time — villa should only manage the global config and document the project-file trust model.

## Memory-envelope math (grounded in real GGUF sizes)

The binding constraint is the **GTT envelope** villa already measures (`mem_info_gtt_total` — ~50% of RAM at Fedora defaults; 62.5 GiB measured on the 128 GB dev host), NOT total RAM. The v1.3 embed reservation (512 MiB) comes off the top first (shipped P22 behavior).

KV-cache estimates (computed, q8_0 KV halves them): Qwen3-Coder-30B-A3B ≈ 96 KiB/token fp16 (48 layers × 4 KV heads × 128 dim) → ~3 GiB @ 64K with q8_0 KV. Qwen3-Coder-Next: only a minority of layers carry full attention → roughly ¼ of that per token (~1.5 GiB @ 64K fp16-equivalent). Label: ESTIMATE — verify on-hardware in the phase.

| RAM tier | GTT envelope (default ≈ ½ RAM) | Coding mode (swap) pick | Footprint (model + KV@64K q8 + embed 0.5 GiB) | Co-resident option (chat + coder + embed) |
|----------|-------------------------------|--------------------------|----------------------------------------------|-------------------------------------------|
| 64 GB | ~31 GiB | Qwen3-Coder-30B-A3B UD-Q4_K_XL (17.7 GB) | ~21–22 GiB → **fits** | **None** — refuse honestly; swap-only |
| 96 GB | ~47 GiB | Qwen3-Coder-Next UD-Q3_K_XL (36.3 GB) | ~39–40 GiB → **fits** | 30B-A3B coder (17.7) + ~20 GB chat MoE + embed ≈ 41–43 GiB — borderline; only with reduced ctx, fit-gate decides |
| 128 GB | ~62.5 GiB (measured) | Qwen3-Coder-Next UD-Q4_K_XL (49.6 GB) | ~53–55 GiB (even @128K ctx) → **fits** | 30B-A3B coder + chat + embed ≈ 44–46 GiB → **fits comfortably** |

**Residency recommendation: swap-based "coding mode" is the core mechanism; co-residency is a fit-permitting enhancement.**

- **Day-one baseline (zero memory cost):** point Crush at the *existing* resident chat model — `qwen3.6-35b-a3b`-class MoE models are competent tool-callers. This works on every tier with no orchestration change and should be the install default.
- **Coding mode (the real feature):** `villa code on|off`-style transactional swap of the served model to the coding pick, reusing `internal/modelswap` ordering + the D-09 guarantee (chat swap never touches memory units). Works on all tiers; the chat UI simply talks to the coding model while coding mode is on (acceptable — Qwen3-Coder chats fine).
- **Co-resident second llama-server (`villa-code` unit):** only where the fit-gate proves it (128 GB always for 30B-A3B; never on 64 GB). Recommend deferring this to a stretch/later phase — it adds a second inference unit, second residency proof, and a second `/metrics` surface for marginal benefit over swap.

**Do not** silently depend on raising GTT (`amdttm.pages_limit` kernel args). If detect sees an envelope ≪ RAM, surface it as preflight ADVICE (it can ~double the envelope) — never auto-edit kernel parameters.

## Codebase memory: what's actually needed

**Validation verdict on Qdrant-style code RAG: NOT recommended.** Evidence:

- Cline's engineering position ("Why Cline doesn't index your codebase"): chunk-embedding breaks code semantics (definition/call/context land in different chunks), every merge desyncs the index, and a vector copy of the code doubles the security surface; agentic exploration (follow imports, AST, grep) "outperformed RAG by a lot" per a cited Anthropic engineer.
- Claude Code, OpenCode, and Crush — the three most-used terminal agents of 2025–2026 — all ship **without** embedding indexes; their retrieval is grep/glob + LSP + file reads. Aider's celebrated repo-map is tree-sitter/PageRank, also embedding-free.
- Counter-evidence honestly noted (Milvus: "grep burns tokens"): token burn = prompt-processing latency, which matters more on local hardware than in the cloud. Mitigations that hold on this host: MoE coding models prompt-process at ~400–500 tok/s on gfx1151, LSP narrows searches dramatically, and 256K-context models reduce re-reads. Net: agentic search still wins locally; an embedding pipeline would *also* steal GPU from the coding model on a constrained envelope.

**What v1.4 should ship instead (all agent-native, zero new services):**

| Need | Mechanism | villa's job |
|------|-----------|-------------|
| Code navigation / symbols | Crush LSP integration (`lsp` block) | Render `lsp` entries for detected toolchains; WARN (not BLOCK) when servers are missing |
| Repo retrieval | Crush agentic tools (grep/glob/read) | Nothing — built in |
| Persistent project memory | `CRUSH.md` / `AGENTS.md` context files (Crush reads project context files; `AGENTS.md` is the cross-agent convention) | Optionally scaffold a template on first run; document the convention |
| Semantic code search (if ever demanded) | An MCP server (e.g. a Qdrant-backed MCP) plugged into Crush's MCP support | **Defer** — opt-in, future milestone; do not build now |

**Consequence:** the existing villa-qdrant + villa-embed stack is untouched by v1.4 (beyond the already-shipped embed reservation in fit math). The "dedicated code collection" default in PROJECT.md should be dropped in requirements — it would be write-only plumbing no recommended agent reads.

## Installation

```bash
# Agent binary (villa-managed during install; sanctioned outbound window)
# Pin: crush v0.76.0, verify against checksums.txt
curl -fsSLO https://github.com/charmbracelet/crush/releases/download/v0.76.0/crush_0.76.0_Linux_x86_64.tar.gz
sha256sum --check ...   # villa embeds the expected digest

# Coding model GGUFs (villa download core, existing shard-aware path)
# 64 GB+ tiers:
#   https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF  (UD-Q4_K_XL, 17.7 GB)
# 96/128 GB tiers:
#   https://huggingface.co/unsloth/Qwen3-Coder-Next-GGUF              (UD-Q3_K_XL 36.3 GB / UD-Q4_K_XL 49.6 GB)

# llama-server render delta (coding mode): add --jinja for OpenAI tools support
```

Config additions (`config.toml`, mirroring `memory_enabled`): `coding_enabled`, `coding_model`, `coding_quant`, `coding_ctx`, agent version pin.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Crush v0.76.0 | **OpenCode (anomalyco/opencode) v1.17.4** (2026-06-12; MIT; bun-compiled single binary; `opencode.json` with `@ai-sdk/openai-compatible` provider; `share: "disabled"`, `autoupdate: false` configurable) | If upstream ships a real offline mode (re-opened #2224 / #16117) — it is the most popular agent with the biggest ecosystem. Today it fails villa's bar: unconditional `models.dev` fetch at startup, runtime npm/bun package downloads for providers & plugins (proven by the #2224 `ConnectionRefused downloading package manifest` failure), LSP auto-download, autoupdate default-ON, and air-gapped support **closed as not planned**. A `villa verify agent` egress proof would fail it out of the box. |
| Crush | **Goose (aaif-goose/goose, ex-Block) v1.37.0** (2026-06-03; Apache-2.0; Rust CLI + desktop; YAML config; opt-in usage data) | If the user wants an MCP-extension-centric general agent rather than a coding-focused one. Weaker fit: no LSP integration, historically fussy with local models' tool-calling, desktop-app center of gravity. License (Apache-2.0) is cleaner than Crush's FSL — relevant if FSL is unacceptable. |
| Crush | **Aider v0.86.x** (Apache-2.0; pip/uv Python; tree-sitter repo-map; analytics opt-in) | If the user wants conversational pair-programming with git-commit discipline rather than an autonomous agent. Not the pick: Python runtime on host, release cadence has slowed sharply (v0.86.0 Aug 2025; v0.86.2 ~Feb 2026), and its REPL model predates the agentic tool-loop generation. |
| Crush | **Codex CLI** (OpenAI; Apache-2.0; Rust single binary; `config.toml` with custom `model_providers`) | If a TOML-config, Apache-2.0 single binary matters more than local-model polish; designed around OpenAI models, local-endpoint support is secondary. Worth re-checking at the next milestone. |
| Crush | **Qwen Code** (Apache-2.0 gemini-cli fork tuned for Qwen3-Coder) | If maximum Qwen3-Coder tool-template compatibility ever becomes a blocker — but it needs Node.js on the host and has weaker offline hygiene. |
| Qwen3-Coder-30B-A3B / -Next | **Devstral Small 2 (24B dense, `Devstral-Small-2-24B-Instruct-2512`, Apache-2.0, 256K ctx, Q4_K_M ≈ 14.5 GB)** | If dense-model quality-per-GB or vision input matters more than speed. On a bandwidth-bound iGPU a 24B dense generates ~3–5× slower than an A3B MoE — painful for agent loops. Catalog-alternate, not default. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Qdrant + villa-embed "code collection" RAG for the agent | Industry-validated as the wrong retrieval model for code (stale on every commit, chunking breaks semantics, doubled security surface); no recommended agent would even query it — it would be write-only plumbing; embedding steals GPU from the coding model | Crush LSP + agentic search + `CRUSH.md`/`AGENTS.md`; MCP-based semantic search as a future opt-in |
| OpenCode (today) | Cannot be config-locked to zero outbound: models.dev fetch, runtime bun/npm downloads, LSP auto-download; air-gapped mode closed as not planned | Crush with both kill switches rendered by villa + runtime egress proof |
| DeepSeek-Coder-V2-Lite | 2024-era; decisively superseded by Qwen3-Coder family for agentic use | Qwen3-Coder-30B-A3B |
| Devstral 2 (123B dense) / GLM-5-class / Kimi K2.6 / MiniMax M3 | Don't fit the GTT envelope at honest quants (123B dense ≈ 70 GB at Q4; the others are far larger) — would force silent CPU spill, which villa treats as FAIL | Qwen3-Coder-Next at the 128 GB tier |
| gpt-oss-120b as the coding default | ~60–63 GB MXFP4 vs a 62.5 GiB envelope — no KV headroom; weaker agentic-coding tuning than Qwen3-Coder-Next at similar size | Qwen3-Coder-Next UD-Q4_K_XL (49.6 GB) leaves real KV room |
| Containerizing the agent | Breaks LSP/toolchain/git access; no official image; network-namespace "privacy" is theater vs a config + runtime-proof approach | Host binary, pinned + checksummed, config-locked, runtime-verified |
| Auto-editing kernel GTT parameters | Violates "just works"/least-surprise; reboot-coupled failure mode | Preflight ADVICE with the exact kernel-arg remediation text |

## Stack Patterns by Variant

**If 64 GB tier (envelope ~31 GiB):**
- Swap-based coding mode only; Qwen3-Coder-30B-A3B UD-Q4_K_XL @ 64K ctx (q8_0 KV).
- Refuse co-residency at the fit gate with remediation ("coding mode swaps the chat model; both cannot fit").

**If 96 GB tier (envelope ~47 GiB):**
- Default: swap to Qwen3-Coder-Next UD-Q3_K_XL @ 64K.
- Fit-gate may permit co-resident 30B-A3B + chat at reduced ctx — let `recommend` decide, never assume.

**If 128 GB tier (envelope ~62.5 GiB measured):**
- Default: swap to Qwen3-Coder-Next UD-Q4_K_XL @ 128K.
- Co-resident 30B-A3B coder + chat + embed fits — the only tier where a dedicated `villa-code` unit is honest, if that stretch ships.

**If the user declines a dedicated coding model:**
- Agent rides the existing chat endpoint/model (zero cost, all tiers) — this is the install default; coding-model pull is the upsell inside the addon.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| Qwen3-Coder-Next GGUF | llama.cpp ≥ Qwen3-Next merge (PR #16095, landed ~Dec 2025) + Feb-2026 tool-template fixes | **Check the pinned `vulkan-radv` toolbox digest ships a llama.cpp new enough for the hybrid (DeltaNet) architecture and its tool-call parser; re-pin if not.** A residency/generation probe on the new arch is part of the phase gate. |
| Crush `openai-compat` provider | llama-server **with `--jinja`** | Without it, any `tools` request 500s ("tools param requires --jinja flag"). Known ecosystem quirks pairing agents with Qwen3-Coder jinja templates (e.g. anomalyco/opencode #1890) — unsloth quants carry the fixed templates; verify tool-call round-trip in UAT. |
| Crush custom provider models | Embedded Catwalk catalog | Known issue: a custom `providers.<name>.models[].id` can be shadowed by an embedded catalog alias (charmbracelet/crush #2649) — pick villa-unique model ids; verify in phase. |
| Crush v0.76.0 | FSL-1.1-MIT license | Source-available (converts to MIT after 2 years per release). Fine for end-users and for villa *downloading* it at install (no redistribution); document the license in the addon's install consent text. |
| `recommend` schema | Coding-model fit fields | Any new `--json` fields must be append-only + schema-bump per the frozen-contract rule (recommend currently at 2). |

## Open questions for phase research

- Exact `crush.json` schema for `options`/`models.large`/`models.small` selection and the model-id shadowing workaround — freeze against Crush docs at the pinned version.
- Crush's *complete* outbound surface under negative-control (`villa verify agent`, nft-scoped, mirroring `verify memory`) — config kill switches are documented, but villa proves, never trusts flags.
- On-hardware KV/footprint measurements for Qwen3-Coder-Next (hybrid-attention KV estimate is computed, not measured) and tool-call round-trip quality through Crush → llama-server `--jinja`.
- Whether the pinned toolbox digest's llama.cpp supports Qwen3-Next arch (re-pin decision).

## Sources

- [anomalyco/opencode releases](https://github.com/anomalyco/opencode/releases) — v1.17.4, 2026-06-12 (GitHub API, verified today) — HIGH
- [opencode docs: providers](https://opencode.ai/docs/providers/), [config](https://opencode.ai/docs/config/), [models](https://opencode.ai/docs/models/) — `@ai-sdk/openai-compatible` config shape; `share` default `manual`; `autoupdate` default ON — HIGH
- [anomalyco/opencode #2224](https://github.com/anomalyco/opencode/issues/2224) — air-gapped support **closed as not planned**; runtime npm manifest downloads proven — HIGH
- [anomalyco/opencode #16117](https://github.com/anomalyco/opencode/issues/16117) — outbound inventory (models.dev, autoupdate, plugin/LSP downloads); closed as dup of #2224 — HIGH
- [charmbracelet/crush README](https://github.com/charmbracelet/crush) + [releases](https://github.com/charmbracelet/crush/releases) — v0.76.0 (2026-06-05, GitHub API); `openai-compat` provider JSON; `disable_metrics` + `CRUSH_DISABLE_METRICS` + `DO_NOT_TRACK`; FSL-1.1-MIT; LSP config; install channels incl. Fedora repo — HIGH
- [charmbracelet/catwalk](https://github.com/charmbracelet/catwalk) + Crush docs — `disable_provider_auto_update` / `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE`, embedded provider DB, `crush update-providers` — HIGH
- [charmbracelet/crush #2649](https://github.com/charmbracelet/crush/issues/2649) — custom model-id shadowing by embedded catalog — MEDIUM (single report)
- [aaif-goose/goose](https://github.com/aaif-goose/goose) — v1.37.0 (2026-06-03, GitHub API; repo moved from block/goose); [usage-data docs](https://goose-docs.ai/docs/guides/usage-data/) — telemetry prompt opt-in — HIGH/MEDIUM
- [Aider-AI/aider releases](https://github.com/Aider-AI/aider/releases) — latest GH release v0.86.0 (2025-08-09); tags to v0.86.2; [analytics docs](https://aider.chat/docs/more/analytics.html) (opt-in) — HIGH
- [unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF](https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF) — UD-Q4_K_XL 17.7 GB; 262,144 native ctx; Aug-2025 tool-template fixes — HIGH
- [unsloth/Qwen3-Coder-Next-GGUF](https://huggingface.co/unsloth/Qwen3-Coder-Next-GGUF/tree/main) + [unsloth run guide](https://unsloth.ai/docs/models/qwen3-coder-next) — 80B-A3B hybrid MoE; UD-Q4_K_XL 49.6 GB / UD-Q3_K_XL 36.3 GB / Q4_K_M 48.5 GB; ~46 GB RAM rec for 4-bit; Feb-2026 tool-calling updates — HIGH
- [Devstral Small 2 GGUFs](https://huggingface.co/unsloth/Devstral-Small-2-24B-Instruct-2512-GGUF) + Mistral docs — 24B, Apache-2.0, 256K ctx, Q8 ≈ 25 GB — HIGH
- [kyuz0 Strix Halo backend benchmarks](https://kyuz0.github.io/amd-strix-halo-toolboxes/) (same author as villa's pinned toolbox images) + [pablo-ross strix-halo benchmarks](https://github.com/pablo-ross/strix-halo-gmktec-evo-x2/blob/main/QWEN3-CODER-30B_BENCHMARK.md) — Qwen3-Coder-Next pp 421 t/s Vulkan / 524 t/s ROCm; Qwen3-Coder-30B tg ~71–98 t/s — MEDIUM (third-party, JS-rendered grid not fully extracted)
- [Cline: Why Cline doesn't index your codebase](https://cline.bot/blog/why-cline-doesnt-index-your-codebase-and-why-thats-a-good-thing); [Claude Code no-indexing analysis](https://vadim.blog/claude-code-no-indexing/); [MindStudio: grep, not vectors](https://www.mindstudio.ai/blog/is-rag-dead-what-ai-agents-use-instead); counterpoint: [Milvus on grep token burn](https://milvus.io/blog/why-im-against-claude-codes-grep-only-retrieval-it-just-burns-too-many-tokens.md) — code-RAG effectiveness verdict — HIGH (consensus + counterpoint weighed)
- [Qwen llama.cpp docs](https://qwen.readthedocs.io/en/latest/run_locally/llama.cpp.html) + [unsloth Qwen3-Coder local guide](https://unsloth.ai/docs/models/tutorials/qwen3-coder-how-to-run-locally) — `--jinja` requirement for tools ("tools param requires --jinja flag") — HIGH
- [LM Studio / llama.cpp Qwen3-Next support](https://x.com/lmstudio/status/1995646603919606140) (PR #16095) — hybrid-arch support timeline — MEDIUM

---
*Stack research for: VillaStraylight v1.4 Coding Agent (agent selection, gfx1151 coding models, codebase memory)*
*Researched: 2026-06-12*
