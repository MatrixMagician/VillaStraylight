# Phase 26: Agent Delivery Core & Lockdown Launcher - Context

**Gathered:** 2026-06-13
**Status:** Ready for planning
**Source:** discuss-phase `--auto` (recommended-default selection; all decisions grounded in v1.4 research + Phase 25 locked decisions)

<domain>
## Phase Boundary

Deliver the **agent delivery core + lockdown launcher** for the strictly-local terminal coding agent (Crush v0.76.0): a new pure `internal/agent` core that owns (1) a villa-owned `go:embed` **pin policy** (version + per-platform asset + SHA-256, rocm-policy pattern) with checksum verified BEFORE install and autoupdate forced off; (2) a **`crush.json` renderer** that is a derived artifact of `config.toml` — both kill switches set, exactly one loopback villa provider, villa-unique model ids, LSP entries for detected toolchains (missing server → WARN, never BLOCK); (3) a **version comparator + drift detector**; plus the `villa code` **launcher verb** that applies belt-and-braces env lockdown before exec, and binary download via the existing `internal/download` seam.

**NOT in this phase:**
- The optional `villa install` **agent addon** (gate → pre-stage GGUF + agent tarball in the sanctioned outbound window → render → readiness proof with a real tool-call round-trip) — **Phase 27** (INSTALL-03).
- Honest **preflight gates** (disk BLOCK, post-coder envelope BLOCK, gopls/LSP WARN, cloud-credential WARN) and **uninstall coverage** — **Phase 27** (INSTALL-04).
- `villa verify agent` **negative-control-first egress proof covering agent startup** + llama-down no-silent-cloud-fallback control — **Phase 27** (PRIV-06).
- Any `status.Report` 3→4 `coding` block, dashboard Agent panel, **doctor** agent checks, **backup** coverage, per-model usage/cache surfacing — **Phase 28** (SURF-01/02/03).
- The coding-mode unit delta + transactional `villa coding-mode enter|exit` verb — **shipped in Phase 25** (consumed here, not rebuilt).

</domain>

<decisions>
## Implementation Decisions

### Pure core boundaries
- **D-01:** New pure, `Deps`-injected core **`internal/agent`** (rejected `internal/coder` / `internal/agentconf`: "agent" is the milestone's noun and matches the launcher verb). It owns the pin policy, `crush.json` render, version comparator, lockdown env spec, and drift detection. It is **literal-free of backend markers** (image tags / `Vulkan0` / `ROCm0` / device args stay behind `internal/inference` per `TestSeamGrepGate`). All host effects (artifact download, archive extract, filesystem read for drift, `exec` of the launcher) are injected `func` fields on a `Deps` struct; the live wiring is a `liveAgentDeps()` closure in `cmd/villa` — same seam discipline as `backendswap`/`modelswap`/`codingmode`. The cores return typed values (a `Result`/`DriftReport`/`RenderResult`), never `os.Exit`, never print.

### Pin policy & checksum (AGENT-01)
- **D-02:** Pin policy is a `go:embed`'d `internal/agent/crush-policy.json`, cloning the `internal/preflight/rocm-policy.json` pattern verbatim: pinned `version` (`v0.76.0`), a per-platform asset table (`linux/amd64` for the Fedora/Strix Halo target; structure allows future `darwin/arm64`), each asset carrying its release artifact name + **SHA-256**, and the release URL template. Read via `//go:embed crush-policy.json`.
- **D-03:** Checksum is verified **BEFORE** the binary is placed/installed — the downloaded artifact's SHA-256 must equal the policy SHA-256 or villa **refuses with remediation** (never installs an unverified/mismatched binary, never falls back). This is fail-closed on untrusted input, mirroring preflight's refuse-with-remediation posture.
- **D-04:** Autoupdate is **forced off** by construction: the pin is static (no upgrade verb in this phase), `disable_provider_auto_update` is set in the rendered config, and `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` is set by the launcher. A `villa`-driven upgrade/re-pin verb is explicitly **deferred** (future phase) — Phase 26 ships pin + drift detection only.

### Binary install location
- **D-05:** The agent binary is placed at **`$XDG_DATA_HOME/villa/bin/crush`** (villa-owned, uninstallable, isolated from the user's `PATH`/system crush). Rejected `~/.local/bin/crush` — it risks colliding with a user-installed crush and muddies uninstall ownership. `villa code` execs this villa-owned path explicitly, never a `PATH` lookup.

### crush.json render contract (AGENT-02)
- **D-06:** villa renders **only the GLOBAL config** at `$XDG_CONFIG_HOME/crush/crush.json` (`~/.config/crush/crush.json`). villa **never** manages a project-local `crush.json` — Crush executes `$(...)` in project-local config at load time (code-exec trust hazard); the project-file trust model is documented in install/consent text. The global `crush.json` is a **derived artifact of `config.toml`** — regenerated, never hand-edited as the authority (Quadlet/config-as-source-of-truth pattern). Emitted via stdlib `encoding/json`.
- **D-07:** Both kill switches set: `options.disable_metrics = true` and `options.disable_provider_auto_update = true`.
- **D-08:** Exactly **one** villa provider block: `type: "openai-compat"`, `base_url` pointing at the **loopback** inference endpoint (`http://127.0.0.1:<inference_port>/v1`, derived from config), with a dummy/non-secret api key. No other providers are rendered (the embedded Catwalk DB stays for offline, but no cloud provider is configured).
- **D-09:** **villa-unique model ids** (shadowing workaround for charmbracelet/crush #2649): the rendered `models[].id` is namespaced with a `villa-` prefix (exact string is Claude's discretion in planning) so it does NOT shadow a Catwalk built-in id, AND it MUST exactly match the model id the rendered llama-server advertises. This dual constraint (unique vs Catwalk + equal to the served id) is the locked requirement; the literal prefix is implementation detail.
- **D-10:** The `lsp` block is rendered for **detected toolchains only**, by probing the host `PATH` for known servers (primary: `gopls` for Go, since villa is a Go shop; opportunistically `pyright`, `rust-analyzer`, `typescript-language-server` if present). A missing server (e.g. `gopls`) produces a **WARN with remediation** (install hint), **never a BLOCK** — typed-Unknown degradation, consistent with detect/preflight. Crush uses locally-installed servers and never auto-downloads, so villa only references; it does not fetch LSP servers.

### villa code launcher behavior (AGENT-03)
- **D-11:** `villa code` applies **belt-and-braces env lockdown** before `exec`: `CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1` (and, for symmetry with D-04, `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1`). Env lockdown is redundant-with-config on purpose (config + env, defense in depth) — the kill switches hold even if the rendered config is tampered.
- **D-12:** `villa code` does **NOT auto-flip coding mode** — it honors the Phase 25 no-auto-flip guard (`TestNoAutoFlipStructuralGuard`: the `CodingMode` toggle is mutated only by `codingmode.Run`). It launches Crush against whatever endpoint is currently served. If coding mode is OFF (chat model serving) it emits a **WARN** pointing at `villa coding-mode enter`, but still launches (the user may intentionally ride the chat endpoint — the research's zero-cost default).
- **D-13:** Pre-`exec`, `villa code` runs a **presence + drift check**: if the binary is absent → remediation pointing at the Phase-27 install addon (graceful, not a crash); if drift is detected → surface + remediation (see D-14), never silent auto-correct.

### Drift detection (AGENT-04)
- **D-14:** Drift is two signals, both **detected and surfaced with remediation, NEVER auto-corrected**: (a) **binary drift** — installed binary SHA-256 ≠ pinned-policy SHA-256; (b) **config drift** — the on-disk `crush.json` ≠ what villa would render from the current `config.toml` (hand-edit / staleness). The comparison logic lives in the pure core (it is handed bytes/hashes and a freshly-rendered reference); the live filesystem reads are injected. Phase 26 surfaces drift at `villa code` launch and exposes the reusable detector core; **doctor/status surfacing of drift is Phase 28**.

### Claude's Discretion
- Exact Go struct layout and JSON field names of `crush-policy.json` and the rendered `crush.json` (subject to the v0.76.0 schema frozen in this phase's research).
- The exact `villa-` model-id prefix string (constraint locked in D-09).
- Which LSP servers to probe beyond `gopls`, and the exact WARN/remediation message wording.
- Whether the pure core is one file or a few (`policy.go`, `render.go`, `drift.go`, `version.go`) — follow the lowercase-no-underscore naming convention.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` § "Phase 26: Agent Delivery Core & Lockdown Launcher" — goal + 4 success criteria.
- `.planning/REQUIREMENTS.md` — **AGENT-01, AGENT-02, AGENT-03, AGENT-04** (every ID must map to a plan).

### v1.4 research (agent-shape-generic; OpenCode→Crush reconciled)
- `.planning/research/SUMMARY.md` §§ "Agent selection: Crush, not OpenCode", "Architecture impact", "Phase 3 / Phase 4 deliverables", "Open questions for phase research" — the synthesized verdicts and the Crush caveats (FSL license, #2649 shadowing, permission surface).
- `.planning/research/STACK.md` § "villa-generated `crush.json` (sketch)", § install-as-host-binary recommendation, § open questions — the crush.json sketch + `$XDG_DATA_HOME/villa/bin/` placement + kill-switch names.
- `.planning/research/PITFALLS.md` — telemetry/phone-home defaults, project-local `crush.json` `$(...)` code-exec hazard, version-churn discipline.
- `.planning/research/FEATURES.md` — table-stakes agent features (transfer to Crush; key names don't).
- `.planning/research/ARCHITECTURE.md` — seam-level integration claims (host-binary delivery, go:embed pin policy, derived-artifact config).

### Prior-phase locked decisions (consumed, not rebuilt)
- `.planning/phases/25-coding-mode-render-transactional-swap-verb/25-CONTEXT.md` — D-06 (`villa code` name reserved here; no auto-flip), the entered coding-mode loopback endpoint Crush points at.
- `.planning/phases/25-coding-mode-render-transactional-swap-verb/25-02-SUMMARY.md` — `TestNoAutoFlipStructuralGuard`, `liveCodingModeDeps`, swap-vs-shared residency surfacing.

### Reusable code patterns (verbatim analogs)
- `internal/preflight/rocm-policy.json` + `internal/preflight/floors.go` — the `go:embed` policy-JSON pattern to clone for `crush-policy.json`.
- `internal/download/download.go` — SHA-256-verified artifact pull (binary download seam).
- `internal/backendswap/backendswap.go`, `internal/modelswap/modelswap.go`, `internal/codingmode/codingmode.go` — pure-core + injected-`Deps` + `live*Deps()` wiring + typed `Result`.
- `cmd/villa/coding-mode.go` (+ `root.go`) — thin cobra caller registering a verb and wiring live host seams.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/preflight/floors.go` `//go:embed rocm-policy.json`: exact pattern for `internal/agent` to embed + parse `crush-policy.json` (version/asset/sha256/floors → version/asset/sha256/url).
- `internal/download`: the SHA-256-verified pull already used for model weights — reuse for the agent tarball/binary (no new dependency).
- `internal/modelswap` / `internal/backendswap` / `internal/codingmode`: the pure-core/`Deps`-seam/`live*Deps()` template the new `internal/agent` core and `cmd/villa/code.go` follow.
- `config.VillaConfig` (`internal/config/villaconfig.go`): single source of truth — the inference port + model id + backend feed the `crush.json` provider block and model id (D-08/D-09). XDG resolution helpers already exist for the config dir.

### Established Patterns
- **Config is the single source of truth**; rendered files (Quadlet units, now `crush.json`) are derived artifacts, never hand-edited as authority (D-06).
- **Seam grep-gate (`TestSeamGrepGate`)**: backend marker literals must stay behind `internal/inference`; the new agent core must not embed image tags or device markers (it points at a loopback URL + a model id only).
- **Typed-Unknown degradation → WARN, confident-absence → BLOCK/FAIL**: missing LSP server is WARN (D-10), checksum mismatch is a hard refuse (D-03).
- **Refuse-with-remediation**: every non-pass path carries an actionable next step (D-03, D-13, D-14).
- **`live*Deps()` thin-caller convention** in `cmd/villa`: `villa code` is a thin cobra wrapper over the pure core + `liveAgentDeps()`.

### Integration Points
- `cmd/villa/root.go` — register the new `villa code` verb (sibling to `villa coding-mode`).
- The loopback inference endpoint from `config.toml` → `crush.json` provider `base_url` (D-08).
- Phase 25's entered coding-mode endpoint → what `villa code` launches against (D-12); Phase 27 bolts the install addon + egress/tool-call proofs on top of this core.

</code_context>

<specifics>
## Specific Ideas

- Clone `rocm-policy.json` + `floors.go` shape for `crush-policy.json` + its loader — the team already trusts this pattern.
- crush.json provider is `type: "openai-compat"` at `http://127.0.0.1:<inference_port>/v1` (loopback only — never `0.0.0.0`, consistent with PRIV-01 dashboard discipline).
- Belt-and-braces means BOTH config kill switches AND launcher env vars — redundancy is intentional defense-in-depth.
- `villa code` is the reserved launcher name (Phase 25 deliberately used `villa coding-mode enter|exit` to keep `code` free for this phase).
- Research recommends a research-phase pass for Phase 26 to freeze the exact v0.76.0 `crush.json` schema (options/models/lsp keys, #2649 shadowing, permission-config surface) — the planner should run/consume that research.

</specifics>

<deferred>
## Deferred Ideas

- Optional `villa install` agent addon: gate → pre-stage coder GGUF + agent tarball in the sanctioned outbound window → render → readiness proof with a real tool-call round-trip — **Phase 27 (INSTALL-03)**.
- Honest preflight gates (disk BLOCK, post-coder envelope BLOCK, cloud-credential WARN) + uninstall coverage — **Phase 27 (INSTALL-04)**.
- `villa verify agent` negative-control-first nft egress proof covering agent **startup** + llama-down no-silent-cloud-fallback control — **Phase 27 (PRIV-06)**.
- `status.Report` 3→4 `coding` block, dashboard Agent panel, doctor agent drift/probe checks, backup manifest coverage, per-model usage + cache-effectiveness signals — **Phase 28 (SURF-01/02/03)** (single golden re-freeze discipline).
- A villa-driven Crush **upgrade / re-pin verb** (pin is static this phase; drift detected, never auto-corrected) — future phase.
- Managing **project-local** `crush.json` (out of scope — `$(...)` code-exec trust hazard; villa manages global only).
- Co-resident `villa-coder` Quadlet unit (no swap) — **CODER-V2-01**, v2 stretch.
- Future Qdrant/`villa-embed`-backed code-index **MCP server** plugged into Crush — v1.5+, behind PITFALLS' pre-declared numeric eval gate (code-RAG premise was researched and rejected for v1.4).

</deferred>

---

*Phase: 26-agent-delivery-core-lockdown-launcher*
*Context gathered: 2026-06-13 via discuss-phase --auto*
