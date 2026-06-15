# Phase 27: Install Addon, Preflight Gates & `villa verify agent` - Research

**Researched:** 2026-06-14
**Domain:** Optional install-addon wiring + honest preflight gating + negative-control-first runtime egress/cloud-fallback proof for the Crush v0.76.0 terminal coding agent, composed over the Phase 24/25/26 assembly
**Confidence:** HIGH on reuse seams (every claim cites a read file), HIGH on Crush's two-channel outbound surface (official docs + #1852), MEDIUM on the exact `crush run` tool-call payload that deterministically forces a read→edit→result loop (must be confirmed on-hardware, like Phase 26's PONG round-trip)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Addon **mirrors the memory addon** (`install_memory.go` / `memory_enabled`). A **persisted config field** (`agent_enabled`-style, exact name Claude's discretion, consistent with `VillaConfig` naming) plus a **`--coding-agent` install flag**. `villa install --coding-agent` sets+persists the field then gates; a bare `villa install` gates on the persisted value. **One** install verb (NOT `villa install agent`). **Addon-off renders byte-identical** to v1.3.
- **D-02:** Pre-stage **exactly the single coder GGUF that `recommend`'s fit math selects for this host** (the Phase-24 `pickCoder` output) — not all three, no `--coder-model` override this phase. Mirrors `nomicEmbedShard`'s one-shard pattern.
- **D-03:** Pre-stage is the **single sanctioned outbound window** via `internal/download` (`download.PullModel`), **idempotent presence-skip**. Runtime stays zero-download (PRIV-04). The **agent binary** stages via the **Phase-26 `internal/agent` install seam** (`agent.Install`, checksum-before-extract) — composed, never re-implemented.
- **D-04:** The served `-m` path and the pre-stage filename must be **one source of truth** (mirror `TestEmbedGGUFFilenameSingleSource`).
- **D-05:** Install readiness **includes a real tool-call round-trip** (non-interactive `crush run` forcing read→edit→result), **never** a bare health-200.
- **D-06:** `villa verify agent` mirrors **`verify_memory.go`'s four-layer seam EXACTLY**: Verdict (PASS/FAIL only, no WARN) / pure negative-control-FIRST core / live seam / fixed-arg podman+curl exec.
- **D-07:** Control 1 (egress/startup) — negative-control-FIRST: external egress probe under host egress block MUST FAIL, THEN the real agent task (`crush run` tool-call round-trip over loopback) MUST complete while egress is blocked. Reuse `verify_memory`'s egress-block + `runProbeCurl`; no packet capture / new cap-root tooling.
- **D-08:** Control 2 (no silent cloud fallback) — folded into the SAME verb: with `villa-llama` stopped, the same agent task MUST FAIL. Final verdict = `ctrl1.pass && ctrl2.failed-as-expected`.
- **D-09:** Preflight gates honestly: **disk BLOCK** (staged GGUF + binary), **post-coder envelope BLOCK** (from `pickCoder` fit math), **cloud-credential WARN**; typed-Unknown → WARN.
- **D-10:** `villa uninstall` always removes the villa-owned crush binary (`$XDG_DATA_HOME/villa/bin/crush`), rendered `crush.json`, and addon artifacts via ordered `uninstallDeps` seams; staged coder GGUF governed by the existing keep/remove-models flag; `config.toml` left in place.

### Claude's Discretion
- Exact `config.toml` field name + section for the addon-enabled gate (consistent with `VillaConfig`).
- Exact disk/envelope BLOCK thresholds and the cloud-credential env-var allowlist to scan.
- Whether `villa verify agent` lives in a new `verify_agent.go` paralleling `verify_memory.go`, and how egress-block is applied on-host (reuse the Phase-20 mechanism).
- The exact `crush run` task payload that deterministically forces a tool-call round-trip for both D-05 and D-07.
- Plan/wave decomposition across INSTALL-03 / INSTALL-04 / PRIV-06.

### Deferred Ideas (OUT OF SCOPE)
- **Multi-entry / `--coder-model` override pre-staging** — staging more than the recommended coder GGUF, or letting the user pick a catalog entry to stage offline. Rejected this phase.
- **Agent surfacing** (status `coding` block, dashboard panel, doctor checks, backup, usage/cache signals) — **Phase 28** (SURF-01/02/03, USAGE-03/04).
- **villa-driven agent upgrade / re-pin verb** — deferred (pin is static; no upgrade verb yet).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INSTALL-03 | Coding agent is an optional `villa install` addon (mirror memory addon): gate → pre-stage coder GGUF + agent binary in the sanctioned outbound window → render → readiness proof including a real tool-call round-trip | Reuse `install_memory.go` shard-pre-stage seam (`nomicEmbedShard`+`download.PullModel`) for the coder GGUF; `agent.Install` for the binary; `agent.Render`+`Deps.WriteConfig` already render `crush.json`; the readiness proof is a new pure `evalAgentProof` core + `crush run` tool-call driver (Code Examples §1, §4) |
| INSTALL-04 | Preflight gates honestly: disk BLOCK, post-coder envelope BLOCK, cloud-credential WARN; uninstall removes binary, rendered config, addon artifacts | Append agent checks to `RunWithResources` via the install `runMemoryChecks`-style optional-seam (Architecture §Pattern 2); thresholds from `rec.Coder` `CoderFit` (`TotalBytes`/`Fits`) + staged shard `SizeBytes`; cloud-cred WARN scans env (Don't Hand-Roll); uninstall via ordered `uninstallDeps` extension (Code Examples §5) |
| PRIV-06 | `villa verify agent` proves zero outbound at runtime, negative-control-first, covering agent STARTUP; llama-down control proves no silent cloud fallback | Clone `verify_memory.go` four-layer seam: `evalAgentVerify` (negative-control-FIRST, ctrl1 egress + ctrl2 llama-down) + live seam reusing `runProbeCurl` for the external egress probe + a host-side `crush run` driver (Code Examples §6) |
</phase_requirements>

## Summary

Phase 27 is almost entirely a **composition-and-wiring** phase: every hard part (verified download, checksum-before-extract binary install, deterministic `crush.json` render, drift detection, BLOCK/WARN preflight tiers, ordered uninstall, negative-control-first egress proof) already exists as a proven seam in the codebase. The research confirms there are **no new external dependencies**, **no new packages to install**, and **no novel algorithms** — the work is mirroring three reuse anchors (`install_memory.go`, `verify_memory.go`, `uninstall.go`) and composing the Phase-26 `internal/agent` core, the Phase-24 `recommend.CoderFit`, and the existing `internal/download`/`internal/preflight` cores.

Two external facts were verified and are decision-grade. First, **Crush v0.76.0's complete outbound surface is exactly two channels** — pseudonymous PostHog metrics (`data.charm.land`) and the Catwalk provider-DB auto-update — both firing at **startup**, both already killed by the Phase-26 render (`disable_metrics`, `disable_provider_auto_update`, `disable_default_providers`) and the `villa code` launcher env (`CRUSH_DISABLE_METRICS`/`DO_NOT_TRACK`/`CRUSH_DISABLE_PROVIDER_AUTO_UPDATE`). There is **no binary self-update and no LSP auto-download** [CITED: charmbracelet/crush docs]. The non-obvious residual surface is the **agent's own tool-driven outbound** (`fetch`, `agentic_fetch`, `download`, `sourcegraph` tools) — these are agent capabilities, not phone-home, but `villa verify agent`'s egress block must hold even if a misfiring local model invokes one, and the planner should consider rendering `options.disabled_tools` for these in the readiness/verify config (Pitfall 3). Second, **`crush run` is the non-interactive path** and Crush's permission gate prompts by default; the safe, prior-phase-noted approach is to pre-approve tools via **`permissions.allowed_tools`** in the rendered config (NOT `--yolo`, which v0.76.0 rejects with `run`) so the read→edit→result loop completes without an interactive prompt [CITED: charmbracelet-crush.mintlify.app/configuration/permissions].

**Primary recommendation:** Decompose into three waves matching the three requirements — (1) the install addon (`--coding-agent` flag + `agent_enabled` gate + coder-GGUF pre-stage + binary install + tool-call readiness proof), (2) preflight gates + uninstall coverage, (3) `villa verify agent` (egress + llama-down controls). Mirror `install_memory.go`/`verify_memory.go`/`uninstall.go` seam-for-seam; introduce ZERO new backend-marker literals (TestSeamGrepGate walks `cmd/villa`); make the readiness/verify tool-call payload deterministic via `permissions.allowed_tools` and a planted file the agent must read then edit, confirmed on-hardware like Phase 26's PONG.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `--coding-agent` flag + persisted `agent_enabled` gate | Command (`cmd/villa/install.go`) | config (`VillaConfig`) | Flag parsing + gate are cobra/config concerns; mirrors `memory_enabled` exactly |
| Coder-GGUF pre-stage (sanctioned outbound window) | `internal/download` (`PullModel`) | Command (`install_*.go` seam) | The verified-download core is impure-but-owned; the command wires the idempotent presence-skip (like `liveEnsureEmbedModel`) |
| Agent binary install (checksum-before-extract) | `internal/agent` (`Install`) | Command (`liveInstallDeps`) | Phase-26 seam owns the verify-then-extract; the addon composes it |
| `crush.json` render at install | `internal/agent` (`Render`) | Command (`WriteConfig` seam) | Already a pure derived artifact of `config.toml`; the addon reuses `agent.Run`/`Render`, no new renderer |
| Tool-call readiness proof | Command (pure `evalAgentProof` + live `crush run` driver) | `internal/agent` (binary path) | Honesty verdict is a pure core (clone `evalMemoryProof`); the live driver execs the villa-owned binary fixed-arg |
| Disk / post-coder-envelope BLOCK + cloud-cred WARN | `internal/preflight` (tiers) + Command (gate) | `internal/recommend` (`CoderFit` basis) | BLOCK/WARN tiers + refuse-with-remediation are preflight; the envelope basis is `rec.Coder`, never re-derived (D-09) |
| Runtime egress + llama-down proof | Command (pure `evalAgentVerify` + live seam) | `internal/orchestrate` (helper image, service names) | Clone of `verify_memory.go`; the negative-control verdict is pure, the podman/curl exec is fixed-arg behind the seam |
| Uninstall agent teardown | Command (`uninstall.go` `uninstallDeps`) | `internal/agent` (binary path) | Ordered teardown is the command-tier contract; the binary/config paths are villa-owned XDG locations |

## Standard Stack

**No new third-party libraries.** Phase 27 adds zero Go module dependencies — it composes existing first-party cores and the Go stdlib (`os/exec`, `encoding/json`, `path/filepath`, `crypto/sha256`). This is consistent with the project constraint "single static binary; integrate, don't rebuild."

### Core (existing seams reused — these are the "stack")
| Seam | Location | Purpose | Why reused |
|------|----------|---------|------------|
| `download.PullModel(ctx, m, modelsDir)` | `internal/download/download.go:64` | Verified coder-GGUF pre-stage: HEAD size/etag → stream+SHA256 → atomic rename | The single sanctioned-outbound-window downloader (D-03); same path `liveEnsureEmbedModel` uses [VERIFIED: read source] |
| `agent.Install(asset, r, binDir)` | `internal/agent/install.go:52` | Checksum-BEFORE-extract binary install → `binDir/crush` | Phase-26 seam; composed not re-implemented (D-03) [VERIFIED: read source] |
| `agent.Render(cfg, probes)` / `agent.Run(deps)` | `internal/agent/render.go:136`, `agent.go:99` | Deterministic `crush.json` from `config.toml` + first-run write | Already renders kill switches + one loopback provider; reuse for addon render [VERIFIED: read source] |
| `recommend.Pick(...).Coder` (`CoderFit`) | `internal/recommend/coder.go:63`, `recommend.go:195` | Post-reservation coder fit + residency mode | Source of the staged-entry selection (D-02) and the envelope-BLOCK basis (D-09) [VERIFIED: read source] |
| `preflight.RunWithResources(p, req)` + BLOCK/WARN tiers | `internal/preflight/preflight.go:158` | Reusable refuse-with-remediation gate | Append agent checks via the install optional-seam (D-09) [VERIFIED: read source] |
| `runProbeCurl(ctx, helperImage, args...)` | `cmd/villa/install_memory.go:350` | Fixed-arg `podman run --rm --network villa --entrypoint curl` | The egress negative-control mechanism (D-07) [VERIFIED: read source] |
| `uninstallDeps` ordered seam | `cmd/villa/uninstall.go:52` | Ordered teardown; keep/remove-models flag; config-left invariant | Extend with agent teardown (D-10) [VERIFIED: read source] |

### Supporting
| Seam | Location | Purpose | When to use |
|------|----------|---------|-------------|
| `agent.CrushAsset` / `loadCrushPolicy()` | `internal/agent/policy.go` | Pinned version/asset/SHA-256/URL template | Resolve the binary download URL + tarball verify in the addon install |
| `agentBinPath()` / `crushConfigPath()` | `cmd/villa/code.go:217`, `:194` | XDG paths for the villa-owned binary + global `crush.json` | Uninstall targets + readiness proof binary path (DRY — reuse, don't re-derive) |
| `config.SaveVilla` + `,omitempty` discipline | `internal/config/villaconfig.go` | Persist `agent_enabled` without widening off-render | The `--coding-agent` gate persistence (D-01) |
| catalog shard of the picked coder | `internal/catalog/seed.json` (coder entries carry `shards[]`) | URL/filename/sha256/size for the pre-stage | The coder GGUF already has a catalog shard — unlike `nomicEmbedShard` (a hard-coded literal), resolve it from the picked entry |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Reuse `download.PullModel` | A new hard-coded `coderShard` literal à la `nomicEmbedShard` | The coder GGUF already lives in the catalog with `shards[]`; a hard-coded literal would DUPLICATE it and risk drift. **Resolve the shard from the `pickCoder`-selected catalog entry** so D-02 (pick selects) and D-04 (single source) hold by construction. `nomicEmbedShard` was a literal only because the embed model is NOT a catalog entry. |
| `permissions.allowed_tools` for the tool-call proof | `--yolo` flag | Crush v0.76.0 rejects `--yolo` with `run` (prior-phase note); `allowed_tools` is the documented non-interactive pre-approval path [CITED: crush permissions docs] |
| Fold ctrl2 (llama-down) into the same verb (D-08) | A separate verb / manual drill | Folding ensures the headline cloud-fallback control runs routinely — locked by D-08 |

**Installation:**
```bash
# No package installation. The agent BINARY pre-stage is the Phase-26 pinned release:
#   crush_0.76.0_Linux_x86_64.tar.gz (tarball sha256 0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9)
#   extracted-binary sha256 4fd811f68c05da6c8d11fd1d5b6298a75ecc38a6c105a342b74e080cce8342b4
# already pinned in internal/agent/crush-policy.json; the addon composes agent.Install.
```

**Version verification:** Crush v0.76.0 is the pinned, on-hardware-verified release (26-03-SUMMARY: tarball + extracted-binary SHA-256 both pinned). The coder GGUFs are revision-pinned catalog shards (Phase 24, FROZEN). No registry verification is needed because nothing is installed from a package registry — the binary is a SHA-256-verified GitHub release artifact and the model is a SHA-256-verified HuggingFace GGUF, both already pinned.

## Package Legitimacy Audit

> Phase 27 installs **no packages from any language registry** (npm/PyPI/crates). It composes first-party Go cores already in the module, the Go stdlib, and stages two already-pinned, SHA-256-verified artifacts (the Crush release binary and the recommended coder GGUF). The Package Legitimacy Gate is **N/A** — there is no registry-resolved dependency to audit.

| Artifact | Source | Verification | Verdict | Disposition |
|----------|--------|-------------|---------|-------------|
| Crush v0.76.0 binary | GitHub release (charmbracelet/crush) | Tarball + extracted-binary SHA-256 pinned in `crush-policy.json`, verified on-hardware (26-03) | OK (pinned) | Composed via `agent.Install` |
| Recommended coder GGUF (e.g. `Qwen3-Coder-30B-A3B-Instruct-UD-Q4_K_XL.gguf`) | HuggingFace (revision-pinned URL) | Catalog `shards[].sha256` + `size_bytes`, verified by `download.PullModel` | OK (pinned) | Staged via `download.PullModel` |

**Packages removed due to [SLOP] verdict:** none (no registry packages).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
villa install [--coding-agent]                         villa verify agent                    villa uninstall
        │                                                      │                                    │
        ▼                                                      ▼                                    ▼
 ┌──────────────┐  gate: agent_enabled (persisted)     ┌──────────────────┐              ┌────────────────────┐
 │ probe + pick │──────────────────────────────────┐  │ load agent_enabled│              │ ordered teardown   │
 │ (recommend)  │   rec.Coder (CoderFit)            │  │  (off → exit 0)   │              │ (uninstallDeps)    │
 └──────┬───────┘                                   │  └────────┬─────────┘              └─────────┬──────────┘
        │ checks[] += agent preflight (D-09)        │           │                                  │ removes (always):
        ▼                                           │           ▼                                  │  - crush binary
 ┌──────────────┐  disk BLOCK / envelope BLOCK /     │  ┌─────────────────────────────────┐        │  - crush.json
 │ gateInstall  │  cloud-cred WARN (refuse-w-remed)  │  │ evalAgentVerify (PURE,            │        │  - addon artifacts
 └──────┬───────┘                                   │  │   negative-control FIRST):        │        │ coder GGUF: keep/remove
        │ if agent_enabled & !dry-run:              │  │  ctrl1: egressBlocked()? MUST FAIL│        │  -models flag (D-10)
        ▼                                           │  │         then crush-run task PASS  │        │ config.toml: LEFT
 ┌─────────────────────────────────────┐           │  │  ctrl2: villa-llama DOWN →         │        └────────────────────┘
 │ (a) pre-stage coder GGUF             │           │  │         crush-run task MUST FAIL  │
 │     download.PullModel (presence-    │           │  │  verdict = c1.pass && c2.failed   │
 │     skip; single outbound window)    │           │  └──────────┬──────────────────────┘
 │ (b) agent.Install (binary, checksum- │           │             │ live seam:
 │     before-extract)                  │           │             ├─ runProbeCurl → external host (MUST be unreachable)
 │ (c) agent.Render → write crush.json  │           │             └─ crush run (villa-owned bin, loopback /v1, allowed_tools)
 │     (+ permissions.allowed_tools,    │           │
 │      options.disabled_tools fetch/…) │           │      ┌─────────────────────────────────┐
 └──────┬──────────────────────────────┘           └─────►│ HOST EGRESS BLOCK precondition    │ (Phase-20 mechanism,
        │ (d) readiness proof:                              │ operator/wave supplied)          │  operator-applied)
        ▼     evalAgentProof(toolCall())  ── tool-call round-trip (read→edit→result) over 127.0.0.1:8080/v1
 ┌──────────────┐  health-200 NEVER passes (D-05)
 │ PASS / FAIL  │  FAIL → exitBlocked refuse-with-remediation
 └──────────────┘
```

### Recommended Project Structure
```
cmd/villa/
├── install.go            # MODIFY: add --coding-agent flag, agent_enabled gate, agent pre-stage/install/render/readiness steps (mirror the memory block)
├── install_agent.go      # NEW: agent-addon seams (coderShardFor(rec), liveEnsureCoderModel, agentReadinessProof) — mirror install_memory.go
├── verify.go             # MODIFY: register newVerifyAgent() under the verify parent
├── verify_agent.go       # NEW: evalAgentVerify (pure, negative-control-first) + liveAgentVerify + crush-run driver — mirror verify_memory.go
├── uninstall.go          # MODIFY: extend uninstallDeps with removeAgentBinary/removeCrushConfig, ordered teardown
└── preflight_agent.go    # NEW (or fold into install_agent.go): agent preflight checks (disk/envelope BLOCK, cloud-cred WARN)
internal/
├── agent/                # CONSUMED (Phase 26): Install, Render, Run, policy — NO changes expected
├── recommend/            # CONSUMED (Phase 24): rec.Coder CoderFit — NO changes expected
├── preflight/            # MAY add agent check helpers if tiers need new check IDs (keep cmd-tier thin)
└── config/               # MODIFY: add AgentEnabled bool `toml:"agent_enabled,omitempty"` (mirror MemoryEnabled)
```

### Pattern 1: Persisted-gate addon mirroring `memory_enabled`
**What:** A `bool` config field `AgentEnabled` (default false, `,omitempty`, NOT self-healed in `normalizeVilla`) + a `--coding-agent` flag that sets+persists it, gated identically to the memory stack.
**When to use:** The single install verb's agent steps run only when `cfg.AgentEnabled` is true and `!dry-run`; a bare install gates on the persisted value (D-01). Addon-off render is byte-identical because the field is `,omitempty` (the `MemoryEnabled`/`CodingMode` precedent).
**Example:**
```go
// Source: internal/config/villaconfig.go:62 (MemoryEnabled precedent — mirror exactly)
// AgentEnabled gates the v1.4 coding-agent addon. Default false (D-01): an existing
// install stays agent-off until the user opts in. A deliberate bool toggle (mirrors
// MemoryEnabled / CodingMode) — false is a meaningful explicit choice, NOT self-healed.
AgentEnabled bool `toml:"agent_enabled,omitempty"`
```
The `--coding-agent` flag wiring mirrors how `runInstall` reads `d.loadedMemoryEnabled()`; add a `loadedAgentEnabled func() bool` seam (fail-soft to false) and a flag that, when set, overrides+persists `cfg.AgentEnabled = true` before the gate.

### Pattern 2: Optional preflight checks appended via a nil-safe seam
**What:** Agent preflight checks are appended to `checks[]` ONLY when `agent_enabled`, exactly like the memory gate's `runMemoryChecks`.
**When to use:** D-09 disk/envelope BLOCK + cloud-cred WARN. They flow through the SAME `gateInstall` so the agent-off gate is byte-identical and refuse-with-remediation is inherited.
**Example:**
```go
// Source: cmd/villa/install.go:299 (memory precedent)
if d.loadedAgentEnabled() && d.runAgentChecks != nil {
    checks = append(checks, d.runAgentChecks(profile, rec)...) // rec carries rec.Coder + staged size
}
```

### Pattern 3: Negative-control-FIRST pure verdict core (clone `evalRagSmoke`)
**What:** `evalAgentVerify` asserts the egress block is REAL before trusting any "zero outbound" claim, then drives the agent task; folds in the llama-down control.
**When to use:** PRIV-06 / D-06..D-08. Reuse the `memoryProof` Verdict type (`{status preflight.Status; detail string}`), PASS/FAIL only.
**Example:** see Code Examples §6.

### Anti-Patterns to Avoid
- **Re-deriving the post-coder envelope** in preflight instead of reading `rec.Coder` — D-09 says drive it from the `pickCoder` fit math, never re-compute (the fit terms `WeightBytes`/`KVCacheBytes`/`HeadroomBytes`/`TotalBytes`/`Fits` are already on `CoderFit`).
- **A hard-coded `coderShard` literal** mirroring `nomicEmbedShard` — the coder GGUF is a catalog entry with `shards[]`; resolve from the picked entry (D-02/D-04 single-source).
- **Trusting `crush.json`'s kill switches as the egress proof** — Pitfall 1: villa proves at runtime, never flag-trusts. The egress negative control proves the block is real.
- **A bare health-200 / `crush --version` as readiness** — D-05: must be a real tool-call round-trip; a health-200 is a false-green.
- **`--yolo` to skip prompts** — rejected with `run` in v0.76.0; use `permissions.allowed_tools`.
- **Introducing a backend-marker literal in `cmd/villa`** (image tag, `Vulkan0`/`ROCm0`, device arg) — `TestSeamGrepGate` walks `cmd/villa`; the helper image for `runProbeCurl` MUST come from `orchestrate.EmbedImage()` (the existing accessor), never a re-typed literal.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Verified GGUF download | A custom HTTP+hash pre-stage | `download.PullModel` | HEAD verify, resume `.part`, SHA256+size, atomic rename, traversal guard already solved [VERIFIED: read source] |
| Checksum-before-extract binary install | A custom tar extractor | `agent.Install` | Bounded read, size-then-SHA256 verify-before-extract, traversal-confined, no shell `tar` [VERIFIED: read source] |
| `crush.json` render | A new JSON writer | `agent.Render` / `agent.Run` | Deterministic, kill-switches set, one loopback provider, villa- model id, drift-comparable [VERIFIED: read source] |
| Egress negative control | nftables/packet-capture/cap-root tooling | `runProbeCurl` external probe + operator host-egress block (Phase-20 mechanism) | D-07 forbids new cap-root tooling; the reachable-external-host probe is the proven mechanism |
| Cloud-credential scan | A credential parser/keychain reader | `os.LookupEnv` over a fixed allowlist | A WARN (not BLOCK, D-09) — presence of an env var is sufficient signal; no parsing needed |
| Ordered teardown | A blunt `rm -rf` | `uninstallDeps` ordered seam | Ordering IS the contract; traversal-guarded removal + config-left invariant [VERIFIED: read source] |
| Disk free check | A custom statfs | `preflight` `liveStatfs` + `ResourceReq.MinDiskBytes` | The install disk-BLOCK path already statfs's the data dir |

**Key insight:** This phase has essentially nothing legitimate to hand-roll. The only genuinely new *logic* is (a) the pure verdict cores (`evalAgentProof`, `evalAgentVerify`) which are clones of `evalMemoryProof`/`evalRagSmoke`, and (b) the cloud-credential env allowlist. Everything else is calling an existing seam.

## Cloud-credential WARN allowlist (D-09 discretion — researched)

Crush is `openai-compat`-only in villa's render (one loopback provider, `disable_default_providers:true`), but Crush's embedded Catwalk DB recognizes many providers, and a cloud credential in the environment is the silent-fallback risk (Pitfall 2). Scan these env vars (presence → WARN, never BLOCK — the rendered config + env lockdown already neutralize them; surface so the operator knows):

| Env var | Provider | Source |
|---------|----------|--------|
| `ANTHROPIC_API_KEY` | Anthropic | [ASSUMED] common Crush/Catwalk provider key |
| `OPENAI_API_KEY` | OpenAI | [ASSUMED] |
| `OPENROUTER_API_KEY` | OpenRouter | [ASSUMED] |
| `GEMINI_API_KEY` / `GOOGLE_GENERATIVE_AI_API_KEY` | Google | [ASSUMED] |
| `GROQ_API_KEY` | Groq | [ASSUMED] |
| `XAI_API_KEY` | xAI | [ASSUMED] |
| `MISTRAL_API_KEY` | Mistral | [ASSUMED] |
| `DEEPSEEK_API_KEY` | DeepSeek | [ASSUMED] |
| `AZURE_OPENAI_API_KEY` | Azure OpenAI | [ASSUMED] |
| `CRUSH_API_KEY` / Charm/Catwalk auth | Crush hosted | [ASSUMED] verify against Crush auth-store docs in planning |

The WARN message should name the found var(s) and state the neutralization (rendered config has exactly one loopback provider + `disable_default_providers`; the egress proof catches any leak structurally). **Note:** the env-var list is the practical signal on this host (Crush reads provider keys from env). If Crush v0.76.0 also keeps an on-disk auth store, the planner should add that path to the scan during planning — but the env scan is the load-bearing one for the Strix Halo target. The whole list is `[ASSUMED]` and must be confirmed against Crush's provider/auth docs before locking.

## Runtime State Inventory

> Phase 27 is an addon-install + verify phase, not a rename/refactor. It CREATES runtime state (staged GGUF, installed binary, rendered config) rather than renaming existing state. The inventory below records what the addon writes and what uninstall must reverse (D-10).

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Staged coder GGUF in the models dir (`modelsDir()/<coder filename>`); rendered `crush.json` at `~/.config/crush/crush.json` | Pre-stage writes the GGUF (presence-skip); `agent.Run`/first-run write or addon-render writes `crush.json`. Uninstall: GGUF governed by keep/remove-models; `crush.json` always removed (D-10) |
| Live service config | None new — the agent is NOT a service (no Quadlet, no up/down). It rides the existing `villa-llama` loopback endpoint | None. Confirm no new `.container`/`.volume`/`.network` unit is rendered (agent-off byte-identical, D-01) |
| OS-registered state | None — `villa code` is an interactive binary, not a systemd unit; no linger/task-scheduler entry | None |
| Secrets/env vars | The launcher sets `CRUSH_DISABLE_METRICS`/`DO_NOT_TRACK`/`CRUSH_DISABLE_PROVIDER_AUTO_UPDATE` at exec time (Phase 26, in-process only). Cloud-cred env vars are SCANNED (read-only) by the new preflight WARN | None to migrate; the WARN only reads env |
| Build artifacts / installed packages | Villa-owned binary at `$XDG_DATA_HOME/villa/bin/crush` (installed by `agent.Install`) | Uninstall always removes it (D-10) |

**The addon's full on-disk footprint (what uninstall reverses):** `$XDG_DATA_HOME/villa/bin/crush` (always removed), `~/.config/crush/crush.json` (always removed), staged coder GGUF (keep/remove-models flag), `agent_enabled` in `config.toml` (LEFT — config.toml is never touched by uninstall, D-10).

## Common Pitfalls

### Pitfall 1: Trusting Crush's config kill switches as the egress proof
**What goes wrong:** The render sets `disable_metrics`/`disable_provider_auto_update`, the launcher sets the env vars, and the team declares "zero outbound" — a vacuous green.
**Why it happens:** "Points at a loopback baseURL + kill switches set" is conflated with "provably local."
**How to avoid:** `villa verify agent` is negative-control-FIRST: an external host MUST be unreachable under the host egress block (proving the block is real) BEFORE the agent task is trusted; the agent task then completes WHILE egress is blocked (D-07). Reuse `runProbeCurl` against `egressNegativeControlHost`.
**Warning signs:** A verify that asserts only "the task completed" without first asserting the external probe failed.

### Pitfall 2: Cloud fallback / agent answers with `villa-llama` down
**What goes wrong:** Inference silently resolves to a cloud provider (a stray `ANTHROPIC_API_KEY`, a Catwalk default), so the agent "works" while exfiltrating.
**Why it happens:** Agents are provider-first; "it answered" looks like success.
**How to avoid:** D-08 llama-down control — stop `villa-llama`, the same `crush run` task MUST FAIL. An answer with inference down is the smoking gun. Plus the cloud-cred WARN at preflight and the egress control catch it structurally.
**Warning signs:** The agent answering when `villa-llama.service` is stopped; responses faster/smarter than the local coder plausibly is.

### Pitfall 3: Non-deterministic / prompt-blocked tool-call round-trip
**What goes wrong:** `crush run` hangs on an interactive permission prompt (no TTY in the readiness wave), or the model narrates a tool call instead of executing it, so the proof is flaky.
**Why it happens:** Crush prompts before tool calls by default; a weak local model may not reliably invoke a tool [CITED: crush permissions docs]; Pitfall 3 of v1.4 research (tool-call/jinja landmines).
**How to avoid:** Render `permissions.allowed_tools` (e.g. `view`, `edit`, `write`) in the readiness/verify config so the loop never prompts. Make the task DETERMINISTIC: plant a known file with a known token, instruct "read FILE and replace TOKEN_A with TOKEN_B", assert the file now contains TOKEN_B AND `crush run` exit 0 (a read→edit→result loop, like Phase 26's PONG but tool-bearing). Bound it with a timeout (a timeout is a FAIL, never a silent skip). Confirm the exact payload on-hardware (Open Question 1).
**Warning signs:** `crush run` blocking indefinitely; the planted file unchanged after a "successful" run; the agent printing a tool-call as plain text.

### Pitfall 4: Drift between staged GGUF filename and served `-m` path
**What goes wrong:** The pre-stage writes one filename, the coding-mode unit serves another, and the agent talks to an absent/wrong model.
**Why it happens:** Two literals for one path.
**How to avoid:** D-04 single-source. Resolve the shard from the `pickCoder`-selected catalog entry so the staged filename and the served `-m` path both derive from the same catalog `shards[].filename` (mirror `TestEmbedGGUFFilenameSingleSource` with an analogous assertion). Do NOT introduce a `coderShard` literal.
**Warning signs:** A new hard-coded coder filename constant in `cmd/villa`; the readiness proof passing while the coding-mode unit references a different file.

### Pitfall 5: Agent tool-driven outbound (`fetch`/`download`/`sourcegraph`) leaking under the block
**What goes wrong:** The two phone-home channels are killed, but the agent's own `fetch`/`agentic_fetch`/`download`/`sourcegraph` tools are outbound-capable; a prompt-injected or misfiring local model could invoke one.
**Why it happens:** These are agent capabilities, not telemetry — no kill switch covers them; they're gated only by permissions.
**How to avoid:** The host egress block makes them fail by construction (that's the point of the runtime proof). Defense-in-depth: render `options.disabled_tools` for the outbound tools in the addon config, and/or omit them from `allowed_tools`. Surface this in the STRIDE/security pass.
**Warning signs:** A `fetch`/`download` tool succeeding during the egress-blocked verify (would mean the block is incomplete — itself a FAIL of ctrl1).

### Pitfall 6: A second golden re-freeze or a leaked seam literal
**What goes wrong:** The addon render evolves a byte-frozen contract, or a backend-marker literal lands in `cmd/villa`.
**Why it happens:** Surfacing pressure leaking into Phase 27 (which is Phase 28's job).
**How to avoid:** Phase 27 changes NO `status.Report`/dashboard contract (that's Phase 28). Agent-off render stays byte-identical (D-01). The `runProbeCurl` helper image comes from `orchestrate.EmbedImage()`; no new image/device literal in `cmd/villa`. Run `TestSeamGrepGate` + `make check` per task.
**Warning signs:** A `testdata/*.golden` diff that isn't a pure addition; an image tag string in `cmd/villa`.

## Code Examples

Verified patterns from the codebase (cite the read source).

### §1 Coder-GGUF pre-stage resolved from the picked catalog entry (D-02/D-03/D-04)
```go
// Source: pattern from cmd/villa/install_memory.go:111 (liveEnsureEmbedModel) +
//         internal/recommend/coder.go (rec.Coder.Model is the picked coder id).
// UNLIKE the embed model (a hard-coded nomicEmbedShard literal), the coder GGUF is a
// catalog entry — resolve its shard from rec.Coder so D-02 (pick selects) and D-04
// (single source) hold by construction.

func coderShardFor(rec recommend.Recommendation, cat catalog.Catalog) (catalog.Shard, bool) {
    for _, m := range cat.Models {
        if m.ID == rec.Coder.Model && len(m.Shards) > 0 {
            return m.Shards[0], true // single-shard coder entries (catalog FROZEN, Phase 24)
        }
    }
    return catalog.Shard{}, false // no coder fit / not found → addon refuses-with-remediation
}

// liveEnsureCoderModel pre-stages the picked coder GGUF via the verified downloader
// (the single sanctioned outbound window, D-03), idempotent presence-skip upstream.
func liveEnsureCoderModel(modelsDir string, sh catalog.Shard) error {
    if err := os.MkdirAll(modelsDir, 0o700); err != nil { return err }
    m := catalog.CatalogModel{Shards: []catalog.Shard{sh}}
    return pullFn(context.Background(), m, modelsDir) // == download.PullModel
}
```

### §2 Idempotent presence-skip (mirror `liveEmbedModelPresent`)
```go
// Source: cmd/villa/install_memory.go:93 — stat + size match → present (never re-pull).
func liveCoderModelPresent(modelsDir string, sh catalog.Shard) bool {
    fi, err := os.Stat(filepath.Join(modelsDir, sh.Filename))
    if err != nil { return false }
    return fi.Size() >= 0 && uint64(fi.Size()) == sh.SizeBytes
}
```

### §3 Agent binary install (compose the Phase-26 seam — D-03)
```go
// Source: internal/agent/install.go:52 (Install) + internal/agent/policy.go (asset/URL).
// The addon composes agent.Install; it never re-implements the checksum-before-extract.
policy := agent.LoadCrushPolicy()              // (export a loader or reuse the agent path)
asset := policy.Assets["linux/amd64"]
url := strings.ReplaceAll(policy.URLTmpl, "{asset}", asset.Name)
// download the tarball bytes to an io.Reader (internal/download or a bounded GET), then:
binPath, err := agent.Install(asset, tarballReader, agentBinDir()) // verify→extract→binDir/crush
```

### §4 Tool-call readiness verdict core (clone `evalMemoryProof` — D-05)
```go
// Source: cmd/villa/install_memory.go:208 (evalMemoryProof) — PURE, PASS/FAIL only.
type agentProof struct { status preflight.Status; detail string }

// evalAgentProof maps a tool-call round-trip outcome to a verdict. A health-200 NEVER
// reaches here (D-05): the only input is the real tool-call result.
func evalAgentProof(toolCall func() (edited bool, err error)) agentProof {
    edited, err := toolCall()
    if err != nil {
        return agentProof{preflight.StatusFail,
            "the coding agent could not complete a tool-call round-trip (" + err.Error() +
            ") — check `systemctl --user status villa-llama.service` and re-run `villa install --coding-agent`"}
    }
    if !edited {
        return agentProof{preflight.StatusFail,
            "the agent ran but did not perform the tool-call edit (read→edit→result) — the model may not be tool-calling; verify the coding-mode --jinja unit, then re-run"}
    }
    return agentProof{preflight.StatusPass, "tool-call round-trip (read→edit→result) completed against the local endpoint"}
}
```

### §5 Uninstall agent teardown (extend the ordered seam — D-10)
```go
// Source: cmd/villa/uninstall.go:52 (uninstallDeps) — add seams; ordering is the contract.
type uninstallDeps struct {
    // ... existing fields ...
    removeAgentBinary func() error // ALWAYS removes $XDG_DATA_HOME/villa/bin/crush (idempotent: absent = ok)
    removeCrushConfig func() error // ALWAYS removes ~/.config/crush/crush.json (idempotent)
    // staged coder GGUF: NOT a new seam — it lives in the models dir, governed by the
    // EXISTING removeModels/keep-models choice (D-10). config.toml: NO seam (LEFT).
}
// Live wiring reuses agentBinPath()/crushConfigPath() from code.go (DRY) with a
// traversal-guarded os.Remove tolerating os.IsNotExist (idempotent re-uninstall).
```

### §6 `villa verify agent` negative-control-FIRST core (clone `evalRagSmoke` — D-06..D-08)
```go
// Source: cmd/villa/verify_memory.go:70 (evalRagSmoke) — negative control FIRST; PASS/FAIL only.
func evalAgentVerify(
    egressBlocked func() (bool, error),       // ctrl1 negative control: external host MUST be unreachable
    agentTask func() (completed bool, err error), // ctrl1: crush-run tool-call while egress blocked
    llamaDownTask func() (answered bool, err error), // ctrl2: same task with villa-llama STOPPED
) memoryProof { // reuse the memoryProof Verdict type (D-06)
    // (1) ctrl1 negative control FIRST — egress must be proven blocked.
    blocked, err := egressBlocked()
    if err != nil { return memoryProof{preflight.StatusFail, "could not run the egress negative-control probe (" + err.Error() + ") — refusing to declare zero-outbound"} }
    if !blocked { return memoryProof{preflight.StatusFail, "egress is NOT blocked: an external host was reachable — block host outbound, then re-run `villa verify agent`"} }

    // (2) ctrl1 real agent task — MUST complete while egress is blocked.
    completed, err := agentTask()
    if err != nil || !completed {
        return memoryProof{preflight.StatusFail, "the agent task did not complete under the egress block — check villa-llama + the rendered crush.json, then re-run"}
    }

    // (3) ctrl2 llama-down — the SAME task MUST FAIL (an answer = silent cloud fallback = smoking gun, D-08).
    answered, _ := llamaDownTask() // an error here is the EXPECTED inference-down outcome
    if answered {
        return memoryProof{preflight.StatusFail, "the agent ANSWERED with villa-llama stopped — silent cloud-model fallback detected; this FAILS verification"}
    }
    return memoryProof{preflight.StatusPass, "zero-outbound agent task completed; no cloud fallback (llama-down control failed as expected)"}
}
```

### §7 Egress negative control + helper image from the seam accessor (no leaked literal)
```go
// Source: cmd/villa/verify_memory.go:147 (egressBlocked) — reuse runProbeCurl verbatim.
egressBlocked := func() (bool, error) {
    helperImage := orchestrate.EmbedImage() // seam accessor — NEVER a re-typed image literal (TestSeamGrepGate)
    _, err := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5", egressNegativeControlHost)
    return err != nil, nil // reachable (err==nil) → NOT blocked; unreachable → blocked=true
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Code-RAG / vector index for codebase memory | Agent-native grep/LSP + context files | v1.4 research (2026-06) | villa-qdrant/villa-embed untouched; NOT this phase's concern |
| OpenCode as the agent | Crush v0.76.0 (config-killable outbound) | v1.4 research ratified 2026-06-12 | Two outbound channels both killed + proven; the entire premise of `villa verify agent` |
| `--yolo` for non-interactive tool calls | `permissions.allowed_tools` pre-approval | Crush v0.76.0 (`--yolo` rejected with `run`) | The readiness/verify driver must use `allowed_tools`, not `--yolo` |
| Trust agent "local-only" flags | Runtime negative-control-first egress proof | v1.3 verify-memory precedent | `villa verify agent` proves, never flag-trusts |

**Deprecated/outdated:**
- `--yolo` with `crush run`: rejected in v0.76.0 — use `permissions.allowed_tools` [CITED: crush permissions docs].
- Treating health-200 / `crush --version` as readiness: a false-green; D-05 mandates a real tool-call round-trip.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The cloud-credential env-var allowlist (ANTHROPIC/OPENAI/OPENROUTER/GEMINI/GROQ/XAI/MISTRAL/DEEPSEEK/AZURE/CRUSH keys) is the right WARN scope | Cloud-credential WARN allowlist | A missing key → a real cloud cred goes un-warned (still a WARN-only gate; the egress proof catches the leak structurally, so risk is LOW). Confirm against Crush provider/auth docs in planning |
| A2 | Crush v0.76.0 reads provider keys primarily from **env vars** (and any on-disk auth store path should be added if it exists) | Cloud-credential WARN allowlist | If Crush keeps a non-env auth store, the env-only scan misses it → add the path in planning. Egress proof still catches an actual leak |
| A3 | `permissions.allowed_tools` lets `crush run` complete a read→edit→result loop non-interactively without a prompt | Pitfall 3, Code Examples §4 | If prompts still fire, the readiness/verify proof hangs → must be resolved on-hardware (Open Q1); fallback is a more permissive tool config or a confirmed flag |
| A4 | The recommended coder entry is single-shard (so `shards[0]` is the GGUF) | Code Examples §1 | Catalog is FROZEN with single-shard coder entries (verified in seed.json) — risk is effectively nil for the current catalog |
| A5 | Crush v0.76.0 has exactly two outbound channels (metrics + Catwalk), both at startup, no binary self-update, no LSP download | Summary, State of the Art | If a third channel exists, the egress proof still catches it (negative-control-first is channel-agnostic) — risk LOW; the proof is the safety net |
| A6 | The host egress block for the negative control is operator/wave-applied (the Phase-20 verify-memory mechanism), reusable verbatim for the agent | Architecture, Open Questions | If the Phase-20 mechanism was bespoke, the agent verify needs the same operator step documented; it is a precondition, not new code (D-07 forbids new tooling) |

## Open Questions

1. **Exact deterministic `crush run` tool-call payload (read→edit→result).**
   - What we know: `crush run "<prompt>"` is the non-interactive path; `permissions.allowed_tools` pre-approves tools to avoid prompts; Phase 26 proved a plain PONG round-trip; the coder catalog entries are agent-in-the-loop qualified (Phase 24).
   - What's unclear: the precise prompt + planted-file setup that DETERMINISTICALLY forces a `view`+`edit` (or `write`) tool call and a verifiable file mutation, on the pinned coder model + `--jinja` unit, without a TTY.
   - Recommendation: plant a temp file with TOKEN_A, prompt "read <file>, replace TOKEN_A with TOKEN_B, save", assert the file contains TOKEN_B and exit 0; confirm on-hardware (a checkpoint task, like Phase 26's PONG). Bound with a timeout (timeout = FAIL).

2. **Host egress-block mechanism for the negative control on the Strix Halo target.**
   - What we know: `verify_memory.go`'s egress control is "operator/wave supplies a host-egress precondition; the negative-control probe proves it's real"; D-07 reuses this exact mechanism and forbids new cap-root tooling.
   - What's unclear: the exact operator action (the Phase-20 verification artifacts don't record a command — it's an operator precondition).
   - Recommendation: document the operator step in the verify-agent plan's on-hardware acceptance (the same step verify-memory used); the code only runs the `runProbeCurl` probe — the block itself is external (D-07).

3. **Whether to render `options.disabled_tools` for `fetch`/`agentic_fetch`/`download`/`sourcegraph` in the addon config.**
   - What we know: these are outbound-capable agent tools (not phone-home); the egress block catches them at runtime; defense-in-depth would disable them.
   - What's unclear: whether disabling them harms the readiness tool-call loop (it only needs `view`/`edit`/`write`).
   - Recommendation: lean toward disabling outbound tools in the rendered config (STRIDE/security pass) since they're unnecessary for the local coding loop; decide in planning with the security pass.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Pinned Crush binary | Binary pre-stage (D-03) | ✓ (pinned + on-hardware verified 26-03) | v0.76.0 | — (refuse-with-remediation on checksum mismatch) |
| Recommended coder GGUF | GGUF pre-stage (D-02) | ✓ (catalog FROZEN, revision-pinned) | per-tier (Qwen3-Coder-30B-A3B etc.) | — |
| rootless Podman + `villa.network` | egress negative control via `runProbeCurl` | ✓ (used by verify-memory) | v5 | — |
| `villa-llama` running on loopback | tool-call readiness + ctrl1 | ✓ (the served endpoint) | — | — |
| Host egress block (operator) | ctrl1 negative control | operator-supplied precondition | — | — (no fallback; D-07 forbids new tooling) |
| A TTY for the live `crush code` TUI | NOT required by the proofs | — | — | `crush run` non-interactive is the proof path (Phase-26 precedent) |

**Missing dependencies with no fallback:** none at code level. The host egress block is an operator precondition (not a missing dependency) — the same one verify-memory uses.
**Missing dependencies with fallback:** none.

## Validation Architecture

> `.planning/config.json` — `workflow.nyquist_validation` is not explicitly false (Phase 26 was "nyquist-compliant 6/6 automated green"), so this section is included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven, `httptest`, golden fixtures) — the only framework (CLAUDE.md) |
| Config file | none (Go toolchain) |
| Quick run command | `go test ./cmd/villa/ ./internal/agent/ -count=1` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INSTALL-03 | `--coding-agent` gate + agent-off byte-identical render | unit | `go test ./cmd/villa/ -run TestInstall -count=1` | ❌ Wave 0 (extend install_test.go) |
| INSTALL-03 | coder-shard resolved from picked entry == served `-m` (single source, D-04) | unit | `go test ./cmd/villa/ -run TestCoderShardSingleSource -count=1` | ❌ Wave 0 (mirror TestEmbedGGUFFilenameSingleSource) |
| INSTALL-03 | `evalAgentProof` PASS only on a real tool-call edit; FAIL on health-200/no-edit | unit | `go test ./cmd/villa/ -run TestEvalAgentProof -count=1` | ❌ Wave 0 |
| INSTALL-04 | disk BLOCK / envelope BLOCK / cloud-cred WARN; typed-Unknown→WARN | unit | `go test ./cmd/villa/ -run TestAgentPreflight -count=1` | ❌ Wave 0 |
| INSTALL-04 | uninstall removes binary + crush.json (ordered); config.toml left; GGUF via flag | unit | `go test ./cmd/villa/ -run TestUninstall -count=1` | ✅ extend uninstall_test.go |
| PRIV-06 | `evalAgentVerify` negative-control-FIRST: egress-open→FAIL, blocked task→PASS, llama-down answer→FAIL | unit | `go test ./cmd/villa/ -run TestEvalAgentVerify -count=1` | ❌ Wave 0 (mirror evalRagSmoke tests) |
| INSTALL-03/PRIV-06 | real `crush run` tool-call round-trip + egress block + llama-down | on-hardware | manual acceptance (gfx1151 box) | ❌ on-hardware checkpoint plan |
| all | seam-gate green (no leaked literal in cmd/villa) | unit | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ exists |

### Sampling Rate
- **Per task commit:** `go test ./cmd/villa/ ./internal/agent/ -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** Full suite green + on-hardware acceptance before `/gsd-verify-work` (PRIV-06 is on-hardware by nature, like verify-memory).

### Wave 0 Gaps
- [ ] `cmd/villa/install_agent_test.go` — covers INSTALL-03 (gate, pre-stage seam, single-source, readiness verdict)
- [ ] `cmd/villa/verify_agent_test.go` — covers PRIV-06 (`evalAgentVerify` negative-control-first table)
- [ ] `cmd/villa/preflight_agent_test.go` (or fold into install_agent_test.go) — covers INSTALL-04 preflight tiers
- [ ] Extend `cmd/villa/uninstall_test.go` — agent teardown ordering + config-left invariant
- [ ] Extend `cmd/villa/install_test.go` — agent-off byte-identical render assertion

*(No new framework install — the Go testing toolchain covers all phase requirements.)*

## Security Domain

> `security_enforcement` is enabled (absent = enabled; Phase 26 shipped a `26-SECURITY.md`). This phase carries the v1.4 STRIDE pass on the injection→tool-call path (Open Q3) plus the egress/cloud-fallback honesty controls.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Pure-core + injected-seam; one impure module (orchestrate); seam-locked literals |
| V5 Input Validation | yes | Fixed-arg `exec` (no shell interpolation); coder id catalog-resolved; traversal-guarded file ops (reuse existing guards) |
| V6 Cryptography | yes | SHA-256 verify of the binary tarball (`agent.VerifyTarball`) and GGUF (`download` SHA256) — never hand-rolled |
| V10 Malicious Code / Supply Chain | yes | Pinned binary + pinned GGUF; checksum-before-extract; no `latest`, no install script (Pitfall 8) |
| V12 Files & Resources | yes | Traversal-confined install/extract/remove; villa-owned XDG paths only |
| V13 API / Comms | yes | Loopback-only provider; runtime egress negative control; cloud-cred WARN |

### Known Threat Patterns for {Go control plane + local agent + loopback inference}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Silent telemetry/auto-update phone-home | Information Disclosure | Config kill switches + env lockdown (Phase 26) PROVEN by the egress negative control (D-07), never flag-trusted |
| Silent cloud-model fallback (code exfiltration) | Information Disclosure | One loopback provider + `disable_default_providers`; cloud-cred WARN; llama-down negative control (D-08); egress block catches it structurally |
| Prompt-injection → outbound tool (`fetch`/`download`) | Tampering / Info Disclosure | Egress block fails outbound tools by construction; defense-in-depth `disabled_tools`/restrictive `allowed_tools` (Pitfall 5, Open Q3) |
| Slopped/forged binary or GGUF on disk | Tampering | SHA-256 verify-before-extract (`agent.Install`) + `download` size+SHA256+atomic-rename; refuse-with-remediation |
| Path traversal on install/extract/uninstall | Tampering | `assertInsideBinDir`/`assertInsideDir`/`assertUnitInsideDir` — reuse the existing guards, never a raw `os.Remove(userInput)` |
| Backend-marker literal leak into cmd tier | Tampering (contract) | `TestSeamGrepGate` walks `cmd/villa`; helper image from `orchestrate.EmbedImage()` accessor |

## Project Constraints (from CLAUDE.md)

- **Pure-core + injectable-seam:** `evalAgentProof`/`evalAgentVerify` are pure (unit-testable off-hardware); host effects (download, install, `crush run`, podman/curl) are injected `func` fields; live wiring is `live*Deps()` in `cmd/villa`.
- **`internal/orchestrate` is the only intentionally impure module:** no new impure first-party module; the agent is not a service (no Quadlet render).
- **Backend marker strings stay behind `internal/inference`/`internal/orchestrate`:** `TestSeamGrepGate` walks `internal/` AND `cmd/villa` — the egress helper image MUST come from `orchestrate.EmbedImage()`; no image/device/`Vulkan0`/`ROCm0` literal in the new files.
- **`--json`/dashboard contracts byte-frozen:** Phase 27 changes NO golden contract (Phase 28 owns surfacing); agent-off render is byte-identical (D-01).
- **Offload-asserting / honesty-by-construction:** readiness is a real tool-call round-trip (health-200 = false-green = FAIL); verify is negative-control-FIRST (absence alone = false-green); a timeout/unevaluable = FAIL, never a silent skip.
- **Config is the single source of truth:** `crush.json` is a derived artifact; the staged GGUF filename and served `-m` path derive from one catalog shard (D-04); `agent_enabled` gates like `memory_enabled`.
- **No shell interpolation:** every host command is fixed-arg `exec.Command`; the coder id is catalog-resolved, the `crush run` payload uses constant prompts + planted files (no metachars).
- **Refuse-with-remediation:** every non-pass path carries an actionable next step (preflight BLOCK, readiness FAIL, verify FAIL, drift refusal).
- **GSD workflow + graphmind:** all edits via a GSD command; code exploration via `/gm` first.

## Sources

### Primary (HIGH confidence)
- Codebase (read 2026-06-14): `cmd/villa/install_memory.go`, `cmd/villa/verify_memory.go`, `cmd/villa/uninstall.go`, `cmd/villa/install.go`, `cmd/villa/code.go`, `cmd/villa/verify.go`, `internal/agent/{agent,install,render,drift,policy}.go`, `internal/recommend/coder.go`, `internal/preflight/preflight.go`, `internal/download/download.go`, `internal/config/villaconfig.go`, `internal/catalog/seed.json` — the reuse seams + the coder catalog shards.
- `.planning/phases/26-agent-delivery-core-lockdown-launcher/26-03-SUMMARY.md` — the on-hardware PONG round-trip, pinned binary hash, `disable_default_providers` confirmation, ROCm caveat.
- `.planning/phases/27-.../27-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md` — locked decisions + requirements.

### Secondary (MEDIUM confidence)
- charmbracelet/crush docs (permissions, metrics/privacy) — `allowed_tools`, `disabled_tools`, the two outbound channels + kill switches, FSL-1.1-MIT — https://charmbracelet-crush.mintlify.app/configuration/permissions ; https://github.com/charmbracelet/crush
- `.planning/research/PITFALLS.md`, `.planning/research/SUMMARY.md` — Crush outbound-surface inventory, cloud-fallback + tool-call-template pitfalls, the negative-control-first discipline.
- FSL-1.1-MIT: SPDX + fsl.software — https://spdx.org/licenses/FSL-1.1-MIT.html

### Tertiary (LOW confidence, validate in phase)
- charmbracelet/crush #2649 (model-id shadowing — already mitigated by villa- prefix), #1852 (`CRUSH_DISABLE_DEFAULT_PROVIDERS` quirk — villa uses `disable_default_providers` config, not the env var) — verify provider/auth env-var list in planning.

## FSL-1.1-MIT consent text (install-addon obligation — researched)

The install addon should surface a short consent/notice before staging the pinned Crush binary (the v1.4 research flagged this as a Phase-27 deliverable). The license is **FSL-1.1-MIT** (Functional Source License v1.1 with an MIT future license) [CITED: SPDX/fsl.software]:

- **What it permits the end user:** use, copy, modify, create derivative works, and redistribute for **any Permitted Purpose** — which is any purpose **other than a "Competing Use"** (offering a commercial product/service that substitutes for the licensor's). Internal use and running the agent locally are squarely permitted.
- **Future license:** each released version **automatically converts to MIT two years after its release** (note: the FSL template lists MIT *or* Apache-2.0 as the future license; Crush uses the **MIT** variant — FSL-1.1-**MIT**).
- **What the addon should display (suggested):** a one-line notice naming the component and license, e.g.:
  > "The coding agent (Crush, charmbracelet) is distributed under the Functional Source License v1.1 (FSL-1.1-MIT): you may use, modify, and redistribute it for any purpose except offering a competing commercial service; each version becomes MIT-licensed two years after release. villa installs a pinned, checksum-verified release and renders its config locally."

  This mirrors how villa already surfaces honest notices (no-telemetry line in `villa status`, the SELinux note in uninstall). It is **informational**, not a click-through EULA — the user already invoked `--coding-agent`. Confirm exact wording in planning; the license fact (FSL-1.1-MIT, non-compete, 2-year→MIT) is the load-bearing content.

## Metadata

**Confidence breakdown:**
- Reuse seams / architecture: HIGH — every seam was read; the three reuse anchors map directly to the three requirements.
- Crush outbound surface (two channels, kill switches, no self-update/LSP-download): HIGH — official docs + #1852, consistent with v1.4 research and the Phase-26 on-hardware observation.
- Cloud-cred env allowlist + `crush run` tool-call payload: MEDIUM/LOW — `[ASSUMED]`, to be confirmed on-hardware/against Crush auth docs (Open Q1, A1/A2/A3).
- FSL-1.1-MIT: MEDIUM-HIGH — SPDX/fsl.software primary, the MIT-vs-Apache future-license variant confirmed as MIT by the license id.

**Research date:** 2026-06-14
**Valid until:** 2026-07-14 (stable; Crush is pinned to v0.76.0 so upstream churn does not affect this phase — the pin is the freeze). Re-verify the cloud-cred env list and the `crush run` tool-call payload on-hardware during planning regardless of date.
