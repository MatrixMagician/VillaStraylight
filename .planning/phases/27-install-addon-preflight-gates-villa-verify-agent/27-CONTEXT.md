# Phase 27: Install Addon, Preflight Gates & `villa verify agent` - Context

**Gathered:** 2026-06-14
**Status:** Ready for planning
**Source:** discuss-phase (interactive; all four gray areas discussed, every decision grounded in an existing villa precedent + locked v1.4 roadmap SCs)

<domain>
## Phase Boundary

Make the Crush v0.76.0 coding agent (delivered & lockdown-launched in Phase 26) an **optional, honestly-gated `villa install` addon** that:
1. Comes up **proven-ready** — install pre-stages the coder GGUF + agent binary inside the single sanctioned outbound window, renders, then runs a **readiness proof that includes a REAL tool-call round-trip** (health-200 alone never passes).
2. Is **gated honestly by preflight** — disk BLOCK, post-coder envelope BLOCK, cloud-credential WARN; refuse-with-remediation on confident known-bad, typed-Unknown → WARN.
3. Is **proven strictly-local at runtime** by `villa verify agent` — negative-control-FIRST egress proof covering agent **startup**, plus a **llama-down** control proving no silent cloud-model fallback.
4. Is **cleanly removable** — `villa uninstall` removes the agent binary, rendered config, and addon artifacts.

**Requirements:** INSTALL-03, INSTALL-04, PRIV-06.

**NOT in this phase (Phase 28 — Agent Surfacing & Contracts):**
- `status.Report` 3→4 `coding` block (single golden re-freeze), dashboard Agent panel (SURF-01).
- `villa doctor` agent checks — binary/version/config drift, tool-call probe, under-load residency (SURF-02).
- `villa backup`/`restore` coverage of the rendered agent config (SURF-03).
- Per-model agent token usage + cache-effectiveness signals (USAGE-03/04).

**Consumed, not rebuilt (from prior phases):**
- `internal/agent` pure core + `liveAgentDeps` seam, `go:embed` pin policy, checksum-before-install, `crush.json` renderer, drift detector — Phase 26 (AGENT-01..04).
- Coder catalog (3 qualified `role:"coder"` entries) + `recommend` coder-fit math emitting residency mode (`swap`/`shared`) — Phase 24 (CODER-01/02/03).
- Coding-mode unit delta (`--jinja`, agent ctx) + transactional `villa coding-mode enter|exit` verb composing `modelswap` — Phase 25 (CMODE-01/02).

</domain>

<decisions>
## Implementation Decisions

### Addon opt-in & gate surface (INSTALL-03)
- **D-01:** The coding-agent addon **mirrors the memory addon** (`install_memory.go` / `memory_enabled`). A **persisted config field** (e.g. `[agent] enabled` / `agent_enabled` in `config.toml` — exact name is Claude's discretion, consistent with `VillaConfig` naming) plus a **`--coding-agent` install flag**. `villa install --coding-agent` sets+persists the field then gates; a bare `villa install` gates on the persisted value. There is **one** install verb, not a separate `villa install agent` subcommand. **Addon-off renders byte-identical** to v1.3 (the CMODE-01 / golden discipline). Config remains the single source of truth.

### Pre-stage scope in the sanctioned outbound window (INSTALL-03)
- **D-02:** Install pre-stages **exactly the single coder GGUF that `recommend`'s fit math selects for this host** (tier-dependent, the residency-mode output from Phase 24 `pickCoder`) — **not all three** qualified entries, and **no `--coder-model` override flag this phase**. This mirrors `nomicEmbedShard`'s one-shard pattern: minimizes the outbound window and disk footprint; most users only ever run the recommended entry. (Multi-entry / override staging is a deferred idea — see Deferred.)
- **D-03:** The pre-stage is the **single sanctioned outbound window** — a one-time, install-time controlled pull via the existing `internal/download` (`download.PullModel`: HEAD-verify size/etag → stream → SHA256 + size verify → atomic rename), **idempotent presence-skip** (pull only when the GGUF is absent). Runtime stays **zero-download** (PRIV-04 posture). The **agent binary** is staged via the **Phase-26 `internal/agent` install seam** (checksum-before-extract); install composes it, never re-implements it.
- **D-04:** The served `-m` path and the pre-stage filename must be **one source of truth** (mirror `TestEmbedGGUFFilenameSingleSource` discipline) so the coder GGUF the coding-mode unit serves and the staged filename cannot drift.

### Readiness proof — real tool-call round-trip (INSTALL-03)
- **D-05:** Install readiness for the addon **includes a real tool-call round-trip**, reusing the Phase-26 acceptance shape (non-interactive `crush run` forcing a read→edit→result tool path), **not** a bare health-200. A health-200 / is-active alone is a false-green and **never** passes readiness (honesty-by-construction, the offload-assert precedent).

### `villa verify agent` — runtime strictly-local proof (PRIV-06)
- **D-06:** New `villa verify agent` verb mirrors **`verify_memory.go`'s four-layer seam EXACTLY**: (1) a **Verdict** type reusing the memory-proof `{status; detail}` — **PASS/FAIL only, no WARN** (an unevaluable result is a FAIL, never a silent skip); (2) a **pure core** mapping probe outcomes to a verdict, asserting the **negative control FIRST** (unit-testable off-hardware); (3) a **live seam** composing the controls; (4) **fixed-arg podman/curl exec**, no shell.
- **D-07:** **Control 1 (egress / startup) — negative-control-FIRST:** an external egress probe run **under host egress block MUST FAIL** (proves the block is real, not merely unused), THEN the **real agent task** — a non-interactive **`crush run` forcing a tool-call round-trip** over the loopback inference endpoint — **MUST complete** while egress is blocked. Asserting zero-outbound by absence alone is a false-green; the negative control proves egress is actually blocked. (Reuse `verify_memory`'s egress-block + `runProbeCurl` mechanism — do **not** introduce packet capture / new cap-root tooling.)
- **D-08:** **Control 2 (no silent cloud fallback) — folded into the SAME `villa verify agent` verb:** with **`villa-llama` stopped**, the same agent task **MUST FAIL**. An agent that still answers with inference down is the smoking gun (silent cloud-model fallback) and **FAILS** verification. One verb proves both PRIV-06 clauses; the final verdict is `ctrl1.pass && ctrl2.failed-as-expected`. (Not a separate verb / manual drill — folding it in ensures the headline control is run routinely.)

### Preflight gates (INSTALL-04)
- **D-09:** Preflight gates the addon **honestly**, refuse-with-remediation, reusing the `internal/preflight` BLOCK/WARN tiers: **disk BLOCK** (insufficient space for the staged GGUF + binary), **post-coder envelope BLOCK** (the host can't fit the coder at agent-profile ctx — driven by the Phase-24 `pickCoder` fit math / residency output, never re-derived), **cloud-credential WARN** (presence of cloud LLM credentials in the environment — e.g. `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`-style vars — is a WARN, not a BLOCK, since the rendered config + env lockdown already neutralize them; surfaced so the operator knows). Typed-Unknown (unprobeable signal) degrades to **WARN**, never a false BLOCK.

### Uninstall scope (INSTALL-04)
- **D-10:** `villa uninstall` (the existing ordered-teardown verb, `uninstall.go`) gains agent-addon coverage: it **always** removes the **villa-owned crush binary** (`$XDG_DATA_HOME/villa/bin/crush`), the **rendered `crush.json`**, and addon artifacts — via injectable ordered `uninstallDeps` seams (ordering is the contract). The **staged coder GGUF is treated like model weights** — governed by the **existing keep/remove-models flag** (default-keep), not deleted unconditionally and not orphaned-forever. **`config.toml` is LEFT in place** (user data, never deleted — the verb has no seam that touches it), consistent with the existing uninstall invariants.

### Claude's Discretion (planner/researcher)
- Exact `config.toml` field name + section for the addon-enabled gate (consistent with `VillaConfig`).
- Exact disk/envelope BLOCK thresholds and the cloud-credential env-var allowlist to scan (research the full Crush outbound surface under negative control — see Research note).
- Whether `villa verify agent` lives in a new `verify_agent.go` paralleling `verify_memory.go`, and how the egress-block is applied on-host (reuse the Phase-20 mechanism).
- The exact `crush run` task payload that deterministically forces a tool-call round-trip for both the install readiness proof (D-05) and verify control 1 (D-07).
- Plan/wave decomposition across INSTALL-03 / INSTALL-04 / PRIV-06.

</decisions>

<deferred>
## Deferred Ideas (not this phase)
- **Multi-entry / `--coder-model` override pre-staging** — staging more than the recommended coder GGUF, or letting the user pick a specific catalog entry to stage offline. Rejected for Phase 27 (larger outbound window + disk; most users run only the recommended entry). Revisit if/when co-resident `villa-coder` (128 GB stretch) lands.
- **Agent surfacing** (status `coding` block, dashboard panel, doctor checks, backup, usage/cache signals) — explicitly **Phase 28** (SURF-01/02/03, USAGE-03/04).
- **villa-driven agent upgrade / re-pin verb** — deferred since Phase 26 (pin is static; no upgrade verb yet).
</deferred>

<canonical_refs>
## Canonical References (full relative paths — MUST read before/at planning)

### Phase scope & requirements
- `.planning/ROADMAP.md` § "Phase 27: Install Addon, Preflight Gates & `villa verify agent`" — goal + 5 success criteria.
- `.planning/REQUIREMENTS.md` — **INSTALL-03, INSTALL-04, PRIV-06** (every ID must map to a plan); also the v1.4 Core Value + "Out of Scope" table (cloud-fallback / OpenCode rejections).

### Precedents to mirror (the three reuse anchors — read these first)
- `cmd/villa/install_memory.go` — sanctioned-outbound-window pre-stage (`nomicEmbedShard` + `download.PullModel`), readiness-proof seam, `TestEmbedGGUFFilenameSingleSource` single-source discipline (mirror for D-02..D-05).
- `cmd/villa/verify_memory.go` — negative-control-FIRST zero-outbound proof, four-layer seam (Verdict / pure eval-negative-first / live seam / fixed-arg exec), `runProbeCurl` egress mechanism (mirror for D-06..D-08).
- `cmd/villa/uninstall.go` — ordered-teardown `uninstallDeps` seam, keep/remove-models flag, config.toml-left invariant (mirror for D-10).

### Consumed cores (Phase 24/25/26)
- `internal/agent/install.go`, `internal/agent/render.go`, `internal/agent/drift.go` — Phase-26 install seam (checksum-before-extract), `crush.json` render, drift detector.
- `internal/recommend/coder.go` — `pickCoder` fit math + residency mode (`swap`/`shared`); the source of the post-coder envelope BLOCK basis (D-09) and the staged-entry selection (D-02).
- `.planning/phases/26-agent-delivery-core-lockdown-launcher/26-CONTEXT.md` — D-05 binary placement (`$XDG_DATA_HOME/villa/bin/crush`), D-06 global-only `crush.json`, kill-switch contract.
- `internal/preflight/preflight.go` + `internal/preflight/rocm-policy.json` — BLOCK/WARN tier pattern + refuse-with-remediation (D-09).

### v1.4 research (read for the agent outbound surface)
- `.planning/research/PITFALLS.md` — Crush telemetry/phone-home defaults, project-local `crush.json` `$(...)` code-exec hazard.
- `.planning/research/STACK.md` — `crush.json` sketch, kill-switch names, host-binary delivery.
- `.planning/research/SUMMARY.md` §§ Crush selection + caveats (FSL license / #2649 / permission surface).

### Phase research (recommended — see Research note)
- `/gsd-plan-phase 27 --research-phase` — research target: **Crush's complete outbound surface under negative control** (every channel `villa verify agent` must prove blocked) + **FSL-1.1-MIT consent text** for the install addon.

</canonical_refs>
