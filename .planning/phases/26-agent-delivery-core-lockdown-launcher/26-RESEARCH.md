# Phase 26: Agent Delivery Core & Lockdown Launcher - Research

**Researched:** 2026-06-13
**Domain:** Strictly-local terminal coding agent delivery (Crush v0.76.0) — pin policy, `crush.json` render, drift detection, lockdown launcher — as a new pure Go core (`internal/agent`) + `villa code` verb on the shipped Go/Podman/llama.cpp control plane (AMD Strix Halo / Fedora)
**Confidence:** HIGH on the Crush v0.76.0 schema, release artifacts, the #2649 shadowing constraint, and the kill-switch surface (verified against the v0.76.0 tag, the GitHub release API, checksums.txt, and official docs today); HIGH on the verbatim codebase analogs (read in this session).

## Summary

Phase 26 is overwhelmingly an **off-hardware, pure-core + render + clone-an-existing-pattern** phase. Every host effect (download, extract, filesystem read for drift, `exec` of the launcher) is an injected `Deps` seam; the live wiring is a `liveAgentDeps()` closure in `cmd/villa/code.go`, exactly mirroring the shipped `internal/codingmode` + `cmd/villa/coding-mode.go` discipline. The single hard research deliverable — **freezing the exact `crush.json` schema at Crush v0.76.0** — is resolved here with HIGH confidence against the v0.76.0 schema and docs: the top-level blocks are `options`, `providers` (object keyed by provider id), `lsp` (object keyed by arbitrary server name), `permissions`, plus `$schema`. Both kill switches exist verbatim as `options.disable_metrics` and `options.disable_provider_auto_update`; the env-var trio `CRUSH_DISABLE_METRICS` / `DO_NOT_TRACK` / `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE` is confirmed.

Three findings are load-bearing for correctness and must be honored by the planner. (1) **The #2649 shadowing bug is an id-COLLISION bug, not a custom-model bug** — a custom `models[].id` that collides with an embedded Catwalk built-in id makes Crush send the embedded dated id on the wire; *truly-novel ids work correctly*. The `villa-` prefix (D-09) is the correct fix, and the dual constraint (unique vs Catwalk AND equal to the served id) is satisfiable because villa controls what llama-server advertises. (2) **An openai-compat provider with `models: []` (or omitted) triggers a `GET {base_url}/models` fetch at config load** — so villa MUST declare `models[]` explicitly (it does, D-08/D-09); an empty array would add a startup dependency on the loopback endpoint being up. (3) **Crush executes `$(...)` command substitution in `command`, `args`, `env`, `headers`, and `url` values** and project-local config (`./.crush.json` / `./crush.json`) takes precedence over global — confirming D-06: villa manages ONLY the global config and villa's own rendered values must contain no `$(...)`-looking strings (a loopback URL + dummy api_key are safe).

**Primary recommendation:** Clone `internal/codingmode` (pure-core + `Deps` + typed `Result`) and `internal/preflight/floors.go` (`go:embed` policy JSON) verbatim into a new `internal/agent` with `policy.go` / `render.go` / `drift.go` / `version.go`; render `crush.json` via stdlib `encoding/json` from `config.VillaConfig`; install `crush_0.76.0_Linux_x86_64.tar.gz` (SHA-256 `0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9`) to `$XDG_DATA_HOME/villa/bin/crush` with the tarball checksum verified BEFORE extraction; wire `cmd/villa/code.go` as a thin cobra caller cloning `cmd/villa/coding-mode.go`.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** New pure, `Deps`-injected core **`internal/agent`** owns the pin policy, `crush.json` render, version comparator, lockdown env spec, and drift detection. Literal-free of backend markers (TestSeamGrepGate). All host effects injected as `func` fields on `Deps`; live wiring is `liveAgentDeps()` in `cmd/villa`. Cores return typed values (`Result`/`DriftReport`/`RenderResult`), never `os.Exit`, never print.
- **D-02:** Pin policy is a `go:embed`'d `internal/agent/crush-policy.json`, cloning the `rocm-policy.json` pattern: pinned `version` (`v0.76.0`), per-platform asset table (`linux/amd64`; structure allows future `darwin/arm64`), each asset carrying release artifact name + **SHA-256**, and the release URL template. Read via `//go:embed crush-policy.json`.
- **D-03:** Checksum verified **BEFORE** the binary is placed/installed — downloaded artifact SHA-256 must equal policy SHA-256 or villa **refuses with remediation** (never installs unverified/mismatched, never falls back). Fail-closed on untrusted input.
- **D-04:** Autoupdate **forced off** by construction: static pin (no upgrade verb this phase), `disable_provider_auto_update` set in rendered config, `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` set by launcher. villa-driven upgrade/re-pin verb **deferred**.
- **D-05:** Agent binary placed at **`$XDG_DATA_HOME/villa/bin/crush`** (villa-owned, uninstallable, isolated from PATH/system crush). `villa code` execs this path explicitly, never a PATH lookup.
- **D-06:** villa renders **only the GLOBAL config** at `$XDG_CONFIG_HOME/crush/crush.json` (`~/.config/crush/crush.json`). NEVER manages a project-local `crush.json` (Crush executes `$(...)` in project-local config at load — code-exec hazard; documented in install/consent text). Global config is a **derived artifact of `config.toml`** — regenerated, never hand-edited as authority. Emitted via stdlib `encoding/json`.
- **D-07:** Both kill switches set: `options.disable_metrics = true` and `options.disable_provider_auto_update = true`.
- **D-08:** Exactly **one** villa provider block: `type: "openai-compat"`, `base_url` at the **loopback** inference endpoint (`http://127.0.0.1:<inference_port>/v1`, derived from config), with a dummy/non-secret api key. No other providers rendered.
- **D-09:** **villa-unique model ids** (shadowing workaround for #2649): rendered `models[].id` namespaced with a `villa-` prefix (exact string Claude's discretion) so it does NOT shadow a Catwalk built-in id, AND it MUST exactly match the model id the rendered llama-server advertises. Dual constraint locked; literal prefix is implementation detail.
- **D-10:** `lsp` block rendered for **detected toolchains only**, probing host `PATH` for known servers (primary: `gopls`; opportunistically `pyright`, `rust-analyzer`, `typescript-language-server` if present). A missing server → **WARN with remediation**, **never a BLOCK** (typed-Unknown degradation). villa only references; never fetches LSP servers.
- **D-11:** `villa code` applies **belt-and-braces env lockdown** before `exec`: `CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`, `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1`. Redundant-with-config on purpose (defense in depth).
- **D-12:** `villa code` does **NOT auto-flip coding mode** — honors the Phase 25 no-auto-flip guard (`TestNoAutoFlipStructuralGuard`). Launches Crush against whatever endpoint is currently served. If coding mode OFF, emits a **WARN** pointing at `villa coding-mode enter`, but still launches.
- **D-13:** Pre-`exec`, `villa code` runs a **presence + drift check**: binary absent → remediation pointing at the Phase-27 install addon (graceful, not a crash); drift detected → surface + remediation (D-14), never silent auto-correct.
- **D-14:** Drift is two signals, both **detected and surfaced with remediation, NEVER auto-corrected**: (a) **binary drift** — installed binary SHA-256 ≠ pinned-policy SHA-256; (b) **config drift** — on-disk `crush.json` ≠ what villa would render from current `config.toml`. Comparison logic in the pure core (handed bytes/hashes + a freshly-rendered reference); live filesystem reads injected. Phase 26 surfaces drift at `villa code` launch + exposes the reusable detector core; **doctor/status surfacing is Phase 28**.

### Claude's Discretion
- Exact Go struct layout and JSON field names of `crush-policy.json` and the rendered `crush.json` (subject to the v0.76.0 schema frozen in this research).
- The exact `villa-` model-id prefix string (constraint locked in D-09).
- Which LSP servers to probe beyond `gopls`, and the exact WARN/remediation message wording.
- Whether the pure core is one file or a few (`policy.go`, `render.go`, `drift.go`, `version.go`) — follow the lowercase-no-underscore naming convention.

### Deferred Ideas (OUT OF SCOPE)
- `villa install` agent addon (gate → pre-stage GGUF + agent tarball in the sanctioned outbound window → render → readiness proof with a real tool-call round-trip) — **Phase 27 (INSTALL-03)**.
- Honest preflight gates (disk BLOCK, post-coder envelope BLOCK, gopls/LSP WARN, cloud-credential WARN) + uninstall coverage — **Phase 27 (INSTALL-04)**.
- `villa verify agent` negative-control-first egress proof covering agent startup + llama-down no-silent-cloud-fallback control — **Phase 27 (PRIV-06)**.
- `status.Report` 3→4 `coding` block, dashboard Agent panel, doctor agent checks, backup coverage, per-model usage/cache surfacing — **Phase 28 (SURF-01/02/03)**.
- The coding-mode unit delta + transactional `villa coding-mode enter|exit` verb — **shipped in Phase 25** (consumed here, not rebuilt).
- A villa-driven Crush upgrade / re-pin verb — future phase.
- Managing project-local `crush.json` — out of scope (`$(...)` code-exec hazard).
- Co-resident `villa-coder` Quadlet unit (CODER-V2-01); Qdrant/`villa-embed` code-index MCP server (v1.5+, behind a numeric eval gate).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| AGENT-01 | villa installs a pinned Crush release via a villa-owned `go:embed` pin policy (version, per-platform asset, SHA-256 verified before install); autoupdate forced off | Verified release artifact `crush_0.76.0_Linux_x86_64.tar.gz`, SHA-256 `0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9`, size 25,155,696 B, `checksums.txt` published; `rocm-policy.json`/`floors.go` `go:embed` pattern to clone; `internal/download` SHA-256 verify-before-rename pattern. Autoupdate off via `disable_provider_auto_update` + env var (verified). |
| AGENT-02 | villa renders `crush.json` as a derived artifact of `config.toml` — both kill switches, exactly one loopback provider, villa-unique model ids, LSP entries for detected toolchains (missing `gopls` → WARN) | EXACT v0.76.0 schema frozen below (options/providers/models/lsp/permissions); #2649 shadowing constraint resolved (villa- prefix); loopback endpoint is fixed `http://127.0.0.1:8080` (`serverPort` const, NOT a config field — see Open Questions Q1); `auto_lsp` default `true` (consider setting false — see Pitfall 5). |
| AGENT-03 | User launches the agent via `villa code` with belt-and-braces env lockdown (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`) | `cmd/villa/coding-mode.go` thin-caller analog; env-var names verified; `villa code` reserved by Phase-25 D-06; no-auto-flip guard (`TestNoAutoFlipStructuralGuard`) must be honored (D-12). |
| AGENT-04 | Agent binary/config drift from the pin policy is detected and surfaced with remediation, never auto-corrected | Binary drift = SHA-256(installed binary) ≠ policy binary SHA-256 (NOTE: policy stores the *tarball* checksum from checksums.txt; the *binary* hash differs — see Pitfall 6); config drift = byte-compare on-disk `crush.json` vs freshly-rendered (determinism concerns in Pitfall 4). Pure comparator core; injected reads. |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

These have locked-decision authority. Research recommendations below comply with all of them.

- **Pure-core + injectable-seam:** decision/render logic in `internal/agent` (no host I/O); host effects (download/extract/read/exec) are injected `Deps func` fields wired by `liveAgentDeps()` in `cmd/villa`. `internal/orchestrate` is the ONLY intentionally-impure first-party module — `internal/agent` must NOT shell out or touch the filesystem in its pure paths.
- **Config is the single source of truth:** `crush.json` is a derived artifact regenerated from `config.toml` — never hand-edited as authority (drift is flagged, never auto-corrected: D-14).
- **Seam grep-gate (`TestSeamGrepGate`):** backend marker strings (`Vulkan0`/`ROCm0`/`HSA_OVERRIDE…`/image tags/device args) stay behind `internal/inference` + `internal/orchestrate`. `internal/agent` references a loopback URL + a model id ONLY — no image tags, no device markers. The gate walks `internal/` and `cmd/villa`.
- **Typed-Unknown degradation → WARN; confident absence → BLOCK/FAIL:** missing LSP server is WARN (D-10); checksum mismatch is a hard refuse (D-03).
- **Refuse-with-remediation:** every non-pass path carries an actionable next step (D-03, D-13, D-14).
- **No shell interpolation:** all host commands are fixed-arg `exec.Command`; model names catalog-resolved, never shell-interpolated. (Reinforced by Crush's own `$(...)` expansion — Pitfall 1.)
- **Loopback-only binds:** the rendered provider `base_url` is `http://127.0.0.1:8080/v1` — never `0.0.0.0`, consistent with PRIV-01.
- **`--json`/dashboard/golden contracts are byte-frozen:** Phase 26 introduces NO `status.Report`/dashboard contract changes (those land in Phase 28). Any new golden (e.g. a rendered-`crush.json` golden) is a NEW append-only fixture, not a mutation of an existing one.
- **Single static binary:** `villa` stays `CGO_ENABLED=0`; no new Go module dependency (stdlib `encoding/json` + `archive/tar` + `compress/gzip` + `crypto/sha256` cover everything).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Pin policy (version + asset + SHA-256 + URL) | `internal/agent` (pure, `go:embed`) | — | Mirrors `internal/preflight/floors.go` — embedded policy data, build-time-validated, never runtime input. |
| `crush.json` render | `internal/agent` (pure) | `internal/config` (source) | Derived artifact of `config.VillaConfig`; pure render → bytes; the live writer persists them. |
| Version comparator | `internal/agent` (pure) | — | Reuse the dotted-numeric `compareVersions` shape from `floors.go` (or compare the pinned string to `crush --version` output). |
| Drift detection (compare) | `internal/agent` (pure) | — | Handed bytes/hashes + a freshly-rendered reference; pure compare, injected reads (D-14). |
| Binary download + checksum-before-place | `cmd/villa` live seam | `internal/download` (pattern) | Host I/O — injected `Deps.Download`. Reuse `internal/download`'s stream-hash-verify-then-rename discipline. |
| Tarball extract + place binary | `cmd/villa` live seam | stdlib `archive/tar`+`compress/gzip` | Host I/O — injected `Deps.Install`/`Extract`. Confine output to `$XDG_DATA_HOME/villa/bin` (traversal guard). |
| Env lockdown + `exec` of crush | `cmd/villa` live seam | stdlib `os/exec` / `syscall.Exec` | Host I/O — injected `Deps.Launch`. Belt-and-braces env (D-11). |
| LSP-server presence probe | `cmd/villa` live seam | — | Host I/O (`exec.LookPath`) — injected `Deps.LookPath`; the pure render consumes the resolved bool/path. |
| Cobra `villa code` surface | `cmd/villa/code.go` | — | Thin caller cloning `coding-mode.go`; Result→exit mapping. |

## Standard Stack

No new external dependency. Everything is Go stdlib + verbatim reuse of shipped first-party cores.

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `encoding/json` (stdlib) | go1.26 | Render `crush.json` (and decode `crush-policy.json`) | Config-as-source-of-truth: `crush.json` is a rendered artifact like Quadlet units. `MarshalIndent` for human-diffable output. [CITED: CLAUDE.md; STACK.md] |
| `crypto/sha256` + `encoding/hex` (stdlib) | go1.26 | Checksum-before-install (D-03) + binary-drift hash (D-14) | Exact pattern already in `internal/download/download.go` (rolling hash → hex → `EqualFold` compare). [VERIFIED: internal/download/download.go] |
| `archive/tar` + `compress/gzip` (stdlib) | go1.26 | Extract `crush_0.76.0_Linux_x86_64.tar.gz` and place the `crush` binary | The release artifact is a `.tar.gz`; stdlib extracts it with no cgo. Confine extraction to `$XDG_DATA_HOME/villa/bin`. [VERIFIED: GitHub release API — asset is `.tar.gz`] |
| `os/exec` / `syscall.Exec` (stdlib) | go1.26 | Launch crush with the lockdown env (D-11) | `syscall.Exec` replaces the villa process so the user gets crush's TUI directly (no wrapper process); `exec.Command` is the testable alternative behind the `Deps.Launch` seam. Both are fixed-arg, no shell. |
| `internal/download` (first-party) | shipped | SHA-256-verified artifact pull seam | Reuse the HEAD-verify → stream-hash → size+sha256 verify → atomic-rename discipline for the agent tarball (D-03). [VERIFIED: internal/download/download.go] |
| `internal/config` (first-party) | shipped | Source of truth feeding `crush.json` | `VillaConfig` + `LoadVilla`/`SaveVilla`; XDG config-dir resolution helpers. [VERIFIED: internal/config/villaconfig.go] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `internal/codingmode` (first-party) | shipped | Pure-core + `Deps` + typed `Result` template | Clone its shape (NOT its state machine — Phase 26 has no transactional cutover) for `internal/agent`'s `Deps`/`Result` ergonomics. [VERIFIED: internal/codingmode/codingmode.go] |
| `internal/preflight` `floors.go` (first-party) | shipped | `go:embed` policy-JSON loader + `compareVersions` | Clone verbatim for `crush-policy.json` loading + the version comparator. [VERIFIED: internal/preflight/floors.go] |
| `os.UserConfigDir` (stdlib) | go1.26 | `~/.config` resolution for the crush global config | Same helper `internal/config` uses (`villaConfigDir`); `filepath.Join(base, "crush", "crush.json")`. [VERIFIED: internal/config/villaconfig.go:203] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `$XDG_DATA_HOME/villa/bin/crush` | `~/.local/bin/crush` | Rejected by D-05 — collides with user/system crush, muddies uninstall ownership. |
| stdlib tar extract | shell `tar -xzf` | Violates no-shell-interpolation + single-static-binary ethos; stdlib is cgo-free and testable. |
| `syscall.Exec` (replace process) | `exec.Command(...).Run()` (child) | `syscall.Exec` gives the cleanest TUI handoff; a child process is easier to unit-test. Recommend the `Deps.Launch` seam so the pure flow is testable and the live impl uses `syscall.Exec`. |
| Charm Fedora/RHEL yum repo | villa-managed download + pin | Rejected — a repo install is unpinned/auto-updatable; villa owns the version + SHA-256 (digest-pin discipline). [CITED: STACK.md] |

**Installation (what villa performs at install time — Phase 27 wires the addon; Phase 26 ships the core + launcher):**
```bash
# Pin (recorded in internal/agent/crush-policy.json):
#   version  v0.76.0
#   asset    crush_0.76.0_Linux_x86_64.tar.gz
#   sha256   0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9   (tarball)
#   size     25155696
#   url      https://github.com/charmbracelet/crush/releases/download/v0.76.0/crush_0.76.0_Linux_x86_64.tar.gz
# 1. download tarball; verify SHA-256 == policy BEFORE extraction (D-03)
# 2. extract `crush` to $XDG_DATA_HOME/villa/bin/crush (0700 dir, 0755 binary)
# 3. render ~/.config/crush/crush.json from config.toml
```

**Version verification (performed this session):**
```
GitHub release API charmbracelet/crush tags/v0.76.0 → published_at 2026-06-05T21:00:12Z
asset: crush_0.76.0_Linux_x86_64.tar.gz   (linux/amd64)
checksums.txt: 0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9  crush_0.76.0_Linux_x86_64.tar.gz
HEAD content-length: 25155696
```

## Package Legitimacy Audit

> The only external artifact this phase installs is the Crush release binary (not a Go-module dependency). No new `go.mod` entry is added.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| charmbracelet/crush v0.76.0 (release tarball) | GitHub Releases | repo active, ~weekly releases through 2026-06 | n/a (CLI release) | github.com/charmbracelet/crush | OK | Approved — pinned by version + SHA-256 over `checksums.txt`; org `charmbracelet` is the well-known Charm publisher (bubbletea/glamour/lipgloss). License FSL-1.1-MIT (download/use fine; document in install consent — Phase 27). |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

The tarball pin is the supply-chain control: SHA-256 verified before install (D-03), `checksums.txt.sigstore.json` is also published (sigstore attestation — Phase 27 may optionally verify the sigstore bundle; out of scope for Phase 26's core).

## crush.json Schema — FROZEN at Crush v0.76.0

> This is the single most important research output. Verified against the v0.76.0 schema (`charmbracelet/crush@v0.76.0/schema.json`) and official docs today. [VERIFIED: charmbracelet/crush v0.76.0 schema.json + docs]

### Top-level keys
`$schema`, `options`, `providers`, `models`, `mcp`, `lsp`, `permissions`, `tools`, `hooks`. villa renders ONLY: `$schema`, `options`, `providers`, `lsp`, `permissions`. (`models` at top-level is for *model role selection*, distinct from `providers.<id>.models[]` — villa declares models inside the provider block, see #2649 below.)

- `$schema`: `"https://charm.land/crush.json"` (verbatim). [VERIFIED]

### `options` (kill switches + relevant flags)
| Key | Type | Default | villa value | Note |
|-----|------|---------|-------------|------|
| `disable_metrics` | bool | `false` | **`true`** (D-07) | Opts out of PostHog `data.charm.land` metrics. [VERIFIED] |
| `disable_provider_auto_update` | bool | `false` | **`true`** (D-04/D-07) | Stops the Catwalk provider-DB refresh fetch; embedded provider DB is the offline fallback. [VERIFIED] |
| `disable_default_providers` | bool | `false` | **consider `true`** | Disables built-in providers so only villa's custom provider is usable — strengthens cloud-fallback prevention (Pitfall 2). [VERIFIED: schema lists `disable_default_providers`] [ASSUMED: exact runtime effect — confirm in Phase 27 egress proof] |
| `auto_lsp` | bool | `true` | **consider `false`** | When true, Crush auto-discovers/configures LSP servers by root markers — could configure servers villa didn't render. Setting false keeps the lsp block authoritative (Pitfall 5). [VERIFIED: default true] |
| `disabled_tools` | []string | — | discretion | Can hide tools (e.g. `sourcegraph`, which is a network tool) from the agent. [VERIFIED] |
| `disable_auto_summarize`, `debug`, `debug_lsp`, `data_directory`, `notification_style`, `context_paths` | — | — | not rendered by villa | Present in schema; out of villa's minimal surface. |

### `providers` (object keyed by provider id) — exactly ONE villa block (D-08)
Provider object fields (verified): `id`, `name`, `base_url` (URI), `api_key`, `type` (enum), `disable` (bool, default false), `system_prompt_prefix`, `extra_headers` (obj), `extra_body` (obj), `provider_options`, `models` ([]Model).

- **`type` enum:** `"openai"`, `"openai-compat"`, `"anthropic"`, `"gemini"`, `"azure"`, `"vertexai"`. villa uses **`"openai-compat"`** (D-08). [VERIFIED]
- **`base_url`:** `"http://127.0.0.1:8080/v1"` (loopback; see Open Questions Q1 on the port literal). [VERIFIED schema; endpoint literal from codebase]
- **`api_key`:** a dummy non-secret string (e.g. `"local"` — the existing `internal/llm` client already sends `APIKey: "local"`). MUST NOT contain `$(...)` (Pitfall 1). [VERIFIED: internal/inference/probe.go uses APIKey "local"]
- **`models`: MUST be declared explicitly and non-empty** — an empty/omitted `models[]` on an openai-compat provider triggers a `GET {base_url}/models` fetch at config load (a startup dependency on the loopback server being up). [VERIFIED: issue #2983 behavior — "models: [] (or omitted) → Crush calls GET {base_url}/models during config load"]

### `providers.<id>.models[]` (Model object)
Required fields (per schema): `id`, `name`, `cost_per_1m_in`, `cost_per_1m_out`, `cost_per_1m_in_cached`, `cost_per_1m_out_cached`, `context_window`, `default_max_tokens`, `can_reason`, `supports_attachments`. Optional: `reasoning_levels`, `default_reasoning_effort`, `options`. [VERIFIED]

- villa sets the cost fields to `0` (local, no cost), `can_reason`/`supports_attachments` to honest values for the served model, `context_window` to the agent ctx (the rendered `-c` = `CoderAgentCtx`), `default_max_tokens` to a sane fraction of context.
- **`id`: the #2649-safe villa-unique id (D-09)** — see below.

### `lsp` (object keyed by arbitrary server name) — detected toolchains only (D-10)
Per-entry fields (verified): `command` (string), `args` ([]string), `env` (obj), `disabled` (bool, default false), `filetypes` ([]string), `root_markers` ([]string), `init_options` (obj), `options` (obj), `timeout` (int seconds, default 30). [VERIFIED]

- Keyed by an arbitrary name; convention is the language or server name (e.g. `"go"` or `"gopls"`). Crush uses **locally-installed servers only — never auto-downloads** (confirms D-10: villa references, never fetches). [VERIFIED: LSP docs]
- villa renders an entry ONLY when the server is found on PATH; a missing server → WARN-with-remediation, the entry is omitted (D-10).

### `permissions`
Field (verified): `allowed_tools` ([]string) — tools that bypass the approval prompt. [VERIFIED]
- Phase-24's on-hardware harness already used `permissions.allowed_tools` for auto-accept (strictly tighter than `--yolo`, which v0.76.0 `run` rejects). [VERIFIED: STATE.md Phase 24-03 finding]
- Phase 26 should render a **restrictive** baseline (do NOT allow `bash`/`edit` broadly). The full STRIDE pass on the injection→tool-call path is Phase 27 — but Phase 26 must not render an allow-all surface. Recommend: render `permissions` minimal or omitted (Crush defaults to prompting). [CITED: PITFALLS.md Pitfall 7] [ASSUMED: exact restrictive baseline — finalize with the Phase-27 STRIDE pass]

### The #2649 model-id shadowing constraint (D-09) — RESOLVED
**The bug is an id-COLLISION bug, not a custom-model bug.** When `crush.json` declares a custom `providers.<id>.models[].id` whose value is the short form of an embedded Catwalk catalog entry's dated id (e.g. declaring `claude-haiku-4-5` which collides with the embedded `claude-haiku-4-5-20251001`), Crush sends the **embedded dated id on the wire** instead of the declared id. **"Truly novel ids (no collision) work correctly — so the bug is specifically about id collisions."** [VERIFIED: charmbracelet/crush #2649]

→ The `villa-` prefix (D-09) is exactly the correct fix: a `villa-`-prefixed id cannot collide with any Catwalk built-in id. The dual constraint — (a) unique vs Catwalk AND (b) equal to the id llama-server advertises — is satisfiable because **villa controls the served id** (see Open Questions Q1: llama-server with no `--alias` advertises the GGUF basename; with `--alias` it advertises whatever villa sets). Recommend: render BOTH sides from the same source so they cannot drift — i.e. the planner should decide whether to (i) add `--alias villa-<model>` to the llama-server render delta so the served id matches the crush.json id, or (ii) set crush.json `models[].id` = the GGUF basename llama-server already advertises, prefixed appropriately. Option (i) is cleaner but touches `internal/inference` (seam) and a golden — option (ii) keeps Phase 26 render-only. **This is the single decision the planner must lock — see Open Questions Q1.**

## Architecture Patterns

### System Architecture Diagram

```
config.toml (single source of truth)
   │  Model / CoderModel / CoderAgentCtx / Backend / (inference port = fixed 8080)
   ▼
internal/agent  (NEW pure core — no host I/O)
   ├── policy.go   : //go:embed crush-policy.json → CrushPolicy{Version, Assets[platform]{Name,SHA256,Size}, URLTemplate}
   ├── render.go   : Render(cfg) → ([]byte crush.json, []Warning)        ── derived artifact, deterministic bytes
   ├── version.go  : Compare(installedVer, policyVer)                      ── clone floors.go compareVersions
   └── drift.go    : DetectDrift(DriftInput{InstalledBinSHA, PolicyBinSHA, OnDiskConfig, RenderedConfig}) → DriftReport
        ▲ (handed bytes/hashes + a freshly-rendered reference; pure compare)
        │
cmd/villa/code.go  (thin cobra caller + liveAgentDeps())   ── all host effects here
   ├── Deps.LookPath(server)      → exec.LookPath           (LSP probe, D-10)
   ├── Deps.ReadConfig()          → os.ReadFile(~/.config/crush/crush.json)   (drift input)
   ├── Deps.HashBinary()          → sha256 of $XDG_DATA_HOME/villa/bin/crush  (binary drift, D-14)
   ├── Deps.WriteConfig(bytes)    → write ~/.config/crush/crush.json 0600     (render output)
   ├── Deps.Download/Install      → internal/download + tar extract (Phase 27 wires the addon install)
   └── Deps.Launch(env)           → syscall.Exec($XDG_DATA_HOME/villa/bin/crush) with lockdown env (D-11)

   villa code flow (D-12/D-13):
     LoadVilla → DetectDrift(presence+binary+config) ─┬─ binary absent → remediation (Phase-27 install)  EXIT
                                                       ├─ drift found  → surface + remediation           EXIT (never auto-correct)
                                                       └─ clean        → coding-mode WARN if OFF → Launch crush (lockdown env)
```

### Recommended Project Structure
```
internal/agent/
├── policy.go          # //go:embed crush-policy.json; CrushPolicy type + loader (clone floors.go)
├── crush-policy.json  # embedded pin: version, per-platform asset+sha256+size, url template
├── render.go          # Render(cfg) → crush.json bytes + LSP warnings (pure)
├── version.go         # Compare(installed, pinned) (clone compareVersions)
├── drift.go           # DetectDrift(DriftInput) → DriftReport (pure compare)
├── agent.go           # Deps struct + typed Result/Report (clone codingmode.Deps shape)
├── *_test.go          # table-driven + a golden for the rendered crush.json (NEW fixture)
cmd/villa/
├── code.go            # `villa code` thin caller + liveAgentDeps() (clone coding-mode.go)
```

### Pattern 1: `go:embed` policy JSON (clone of `internal/preflight/floors.go`)
**What:** Embed `crush-policy.json` at build time; decode with a panic-on-malformed loader (build-time bytes, never runtime input).
**When to use:** the AGENT-01 pin policy.
```go
// Source: internal/preflight/floors.go (verbatim shape)
//go:embed crush-policy.json
var crushPolicyBytes []byte

type CrushPolicy struct {
    Version string                 `json:"version"`             // "v0.76.0"
    Assets  map[string]CrushAsset  `json:"assets"`              // key "linux/amd64"
    URLTmpl string                 `json:"urlTemplate"`         // ".../v0.76.0/{asset}"
}
type CrushAsset struct {
    Name   string `json:"name"`    // crush_0.76.0_Linux_x86_64.tar.gz
    SHA256 string `json:"sha256"`  // TARBALL checksum: 0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9
    Size   uint64 `json:"size"`    // 25155696
}
func loadCrushPolicy() CrushPolicy {
    var p CrushPolicy
    if err := json.Unmarshal(crushPolicyBytes, &p); err != nil {
        panic(fmt.Sprintf("agent: malformed embedded crush-policy.json: %v", err))
    }
    return p
}
```

### Pattern 2: Checksum-before-place (clone of `internal/download` verify discipline)
**What:** stream the tarball into a `.part`, roll a sha256, assert size + `EqualFold(hex(sum), policySHA)` BEFORE extraction; on mismatch delete and refuse-with-remediation. Never extract an unverified artifact (D-03).
**When to use:** AGENT-01 install (wired in Phase 27; the verify helper lives in the Phase-26 core/seam).
```go
// Source: internal/download/download.go (pattern)
if uint64(written) != asset.Size { return refuse("size mismatch") }
if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), asset.SHA256) {
    return refuse("checksum mismatch — refusing to install an unverified Crush binary")
}
// only now: extract tar.gz → $XDG_DATA_HOME/villa/bin/crush (traversal-guarded)
```

### Pattern 3: Deterministic render → byte-stable drift compare (D-14)
**What:** `Render(cfg)` must produce byte-identical output for the same config so config-drift compare is a clean `bytes.Equal`. Use `json.MarshalIndent` with stable key ordering (Go's `encoding/json` emits struct fields in declaration order and map keys sorted — prefer structs over maps for deterministic ordering, EXCEPT `providers`/`lsp` which are maps; for those, either accept Go's sorted-key marshal or build them as ordered structs).
**When to use:** AGENT-02 render + AGENT-04 config drift.
See Pitfall 4 for the normalization hazard.

### Pattern 4: Thin cobra caller (clone of `cmd/villa/coding-mode.go`)
**What:** `villa code` is a thin wrapper: build `liveAgentDeps()`, call the pure flow, map a typed `Result` to exit code + messages. Honor the no-auto-flip guard (D-12).
```go
// Source: cmd/villa/coding-mode.go (shape)
func newCode() *cobra.Command { /* Use:"code", RunE → os.Exit(runCode(cmd, liveAgentDeps())) */ }
```

### Anti-Patterns to Avoid
- **Empty `providers.<id>.models[]`** — triggers a `GET {base_url}/models` startup fetch (loopback dependency at launch). Always declare models explicitly. [VERIFIED: #2983]
- **A `models[].id` that collides with a Catwalk built-in** — #2649 sends the wrong id on the wire. Always `villa-`-prefix. [VERIFIED: #2649]
- **Auto-correcting drift** — D-14 forbids it; surface + remediate only.
- **Managing a project-local `crush.json`** — `$(...)` code-exec hazard (D-06).
- **Rendering an allow-all `permissions`** — leaves the injection→tool-call path open (Phase-27 STRIDE); render restrictive/minimal.
- **Backend literals in `internal/agent`** — would fail `TestSeamGrepGate`. The core knows only a loopback URL + a model id.
- **A second `-c` or any `--jinja`/sampling literal in `internal/agent` or `cmd/villa/code.go`** — those live behind the inference seam (Phase 25). `villa code` does NOT render units; it launches the agent against the already-served endpoint.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Artifact download + checksum + resume | A new HTTP+hash loop | `internal/download` discipline | Resume, HEAD-verify, bounded-error-body, atomic-rename, signed-URL-leak guard all solved. [VERIFIED] |
| Embedded policy + version compare | A bespoke parser/comparator | `internal/preflight/floors.go` (`loadROCmPolicy`, `compareVersions`, `splitVersion`) | Panic-on-malformed-embed + distro-suffix-tolerant compare already shipped. [VERIFIED] |
| XDG config/data dir resolution | Hard-coded `~/.config` / `~/.local/share` | `os.UserConfigDir` (config) + the `storeRootDir()` fallback chain (data) | Matches the rest of the codebase; XDG-safe. [VERIFIED: internal/config, internal/recall/store.go] |
| Pure-core + injected-seam plumbing | A new pattern | clone `internal/codingmode` `Deps`/`Result` ergonomics | Tested, conventional, grep-gate-clean. [VERIFIED] |
| Tar extraction | shell `tar` | stdlib `archive/tar`+`compress/gzip` | cgo-free, testable, traversal-guardable. |
| crush.json schema knowledge | Guessing key names | The FROZEN schema above | Verified against v0.76.0; #2649 + empty-models traps documented. [VERIFIED] |

**Key insight:** Phase 26 adds zero new dependencies and invents zero new patterns — it is a render + clone-two-shipped-cores phase. The ONLY genuinely new knowledge is the externally-owned Crush schema, which this research freezes.

## Runtime State Inventory

> Phase 26 is greenfield-additive (a new core + a new verb), NOT a rename/refactor. This section is included because the phase WRITES new runtime state outside the repo; each category is answered explicitly.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | NEW: the agent binary at `$XDG_DATA_HOME/villa/bin/crush`; the rendered `~/.config/crush/crush.json`. Crush also writes ephemeral data under `data_directory` (default `.crush` in CWD) + `$XDG_DATA_HOME` (`CRUSH_GLOBAL_DATA`) — villa does not manage these. | Phase 27 uninstall must remove the villa-owned binary + rendered config (INSTALL-04, out of scope here). Phase 26 only creates the binary (install seam) + config (render). |
| Live service config | None — Crush is an interactive terminal tool, not a Quadlet service (no up/down, no systemd unit). [CITED: REQUIREMENTS Out-of-Scope] | None. |
| OS-registered state | None — no systemd unit, no Task Scheduler, no launchd. The binary is exec'd on demand. | None. |
| Secrets/env vars | The launcher SETS `CRUSH_DISABLE_METRICS` / `DO_NOT_TRACK` / `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE` (D-11) at exec time — not persisted. The rendered `api_key` is a dummy `"local"`, not a secret. Crush MAY read cloud creds from its own auth store (Phase-27 preflight WARN, out of scope). | None for Phase 26 (env is exec-scoped). |
| Build artifacts / installed packages | None new in the repo build. The downloaded crush binary is an installed artifact carrying its own SHA-256 (drift target, D-14) — NOTE: the *binary* hash ≠ the *tarball* checksum in the policy (Pitfall 6). | Drift detector must hash the extracted binary, and the policy must EITHER also store the expected binary SHA-256 OR drift must re-derive it (see Pitfall 6 / Open Questions Q2). |

**The canonical question:** after villa renders/installs, what runtime state exists that the repo doesn't track? → the binary at `$XDG_DATA_HOME/villa/bin/crush` and `~/.config/crush/crush.json`. Both are villa-owned, drift-checked (D-14), and Phase-27-uninstallable.

## Common Pitfalls

### Pitfall 1: Crush executes `$(...)` in config values
**What goes wrong:** Crush performs shell-style expansion (`$VAR`, `${VAR:-default}`, `$(command)`, quoting, nesting) in `command`, `args`, `env`, `headers`, and `url` config values. A rendered value containing `$(...)` would execute at config load.
**Why it happens:** Crush treats those fields as shell-expandable for provider/LSP flexibility. Project-local `./.crush.json`/`./crush.json` ALSO take precedence over global — an attacker-planted project config is a code-exec vector.
**How to avoid:** (a) villa renders ONLY the global config (D-06). (b) Every villa-rendered value (base_url, api_key, lsp command/args) is a fixed, metachar-free literal — a loopback URL and `gopls` contain no `$()`. (c) Document the project-local trust model in install/consent text (Phase 27). (d) Do NOT pass user-controlled strings into rendered config values.
**Warning signs:** any `$`, `` ` ``, or `(` appearing in a rendered crush.json value. [VERIFIED: deepwiki + STACK.md + PITFALLS.md]

### Pitfall 2: Cloud-model fallback / extra providers
**What goes wrong:** if villa's provider is misrendered/typoed, or built-in providers stay enabled, Crush could resolve a cloud model — code leaves the box while everything "works."
**How to avoid:** render exactly ONE provider (D-08); consider `options.disable_default_providers = true` to make built-ins unusable; declare `models[]` explicitly (no auto-fetch). The runtime egress + llama-down negative controls are Phase 27 — but Phase 26's render must not leave a cloud path configured.
**Warning signs:** more than one provider block; an empty models array; agent works with `villa-llama` down. [VERIFIED: PITFALLS.md Pitfall 2; #2983]

### Pitfall 3: Empty `models[]` triggers a startup `/models` fetch
**What goes wrong:** an omitted/empty `providers.<id>.models[]` on an openai-compat provider makes Crush `GET {base_url}/models` at config load — a launch-time dependency on the loopback server being up (and a failure mode if it isn't).
**How to avoid:** always render a non-empty explicit `models[]` (D-08/D-09). [VERIFIED: #2983]

### Pitfall 4: Non-deterministic render breaks config-drift compare
**What goes wrong:** D-14 config drift = byte-compare on-disk vs freshly-rendered. If render output isn't byte-stable (map key ordering, whitespace, trailing newline, float formatting), every launch falsely reports drift.
**Why it happens:** `providers` and `lsp` are JSON objects (maps). Go's `encoding/json` sorts map keys deterministically, BUT mixing struct fields (declaration order) with `map[string]X` can surprise; `MarshalIndent` indentation/newline must be fixed; cost-field floats must format identically.
**How to avoid:** (a) prefer ordered structs over maps where a stable order matters, OR rely on Go's sorted-map-key guarantee and pin it with a golden test. (b) Fix the indent string + trailing newline. (c) Compare *canonicalized* forms if needed: unmarshal both sides into the same struct and compare values, rather than raw bytes — this tolerates a user re-saving with different whitespace while still catching semantic edits. **Recommend:** the pure drift comparator should compare *parsed semantic content* (unmarshal both → DeepEqual), not raw bytes, to avoid whitespace-only false positives — but also flag raw-byte differences as a softer "stale formatting" signal. The planner should pick one and golden-freeze it.
**Warning signs:** `villa code` reports config drift immediately after a clean render. [VERIFIED reasoning; schema is map-keyed]

### Pitfall 5: `auto_lsp: true` re-introduces servers villa didn't render
**What goes wrong:** with `auto_lsp` default true, Crush auto-discovers/configures LSP servers by root markers — so the rendered `lsp` block is not authoritative; a server villa intentionally omitted (e.g. because it wasn't on PATH) might still be auto-configured if installed later.
**How to avoid:** consider `options.auto_lsp = false` so villa's `lsp` block is the complete, authoritative set (D-10's "detected toolchains only"). Trade-off: false means villa must render every server it wants; true is more convenient but less deterministic. Recommend false for the determinism the drift check (Pitfall 4) needs. [VERIFIED: auto_lsp default true] [ASSUMED: exact interaction with an explicit lsp block — confirm wording in planning]

### Pitfall 6: Binary-drift hash ≠ tarball checksum
**What goes wrong:** the policy SHA-256 from `checksums.txt` is over the **`.tar.gz`**, not the extracted `crush` binary. Drift detection (D-14a) compares the *installed binary* hash — which is a DIFFERENT value. Naively comparing the binary hash to the tarball checksum always reports drift.
**How to avoid:** EITHER (i) the policy stores BOTH the tarball SHA-256 (for install-verify, D-03) AND the expected extracted-binary SHA-256 (for drift, D-14) — the binary hash must be computed once at pin time and recorded; OR (ii) drift re-verifies by hashing the binary and comparing to a binary-hash field. Phase 26's pin step should record the binary SHA-256 (extract the tarball once, hash `crush`, add `binarySHA256` to the policy asset). **This is Open Questions Q2 — the planner must decide and the pin must capture the binary hash on-hardware.**
**Warning signs:** drift reported on a freshly, correctly installed binary. [VERIFIED reasoning; tarball checksum confirmed]

### Pitfall 7: Version churn (Crush releases ~weekly)
**What goes wrong:** an unpinned/auto-updating crush drifts under the user; config schema breaks.
**How to avoid:** static pin (D-04), `disable_provider_auto_update` + `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1`, drift detection (D-14). No upgrade verb this phase. [VERIFIED: PITFALLS.md Pitfall 8]

## Code Examples

### Detecting an LSP server on PATH (live seam) and rendering the lsp entry (pure)
```go
// cmd/villa/code.go (live) — host I/O behind the seam
LookPath: func(bin string) (string, bool) {
    p, err := exec.LookPath(bin)   // never auto-installs; references only (D-10)
    return p, err == nil
},

// internal/agent/render.go (pure) — consumes resolved presence, emits WARN on absence
type lspProbe struct{ Key, Command string; Found bool }
func renderLSP(probes []lspProbe) (map[string]lspEntry, []Warning) {
    out := map[string]lspEntry{}
    var warns []Warning
    for _, pr := range probes {
        if pr.Found {
            out[pr.Key] = lspEntry{Command: pr.Command}  // no $() — fixed literal
        } else {
            warns = append(warns, Warning{
                Code: "lsp_missing",
                Msg:  fmt.Sprintf("%s not found on PATH — code navigation degraded; install it to enable LSP (e.g. `go install golang.org/x/tools/gopls@latest`)", pr.Command),
            })  // WARN, never BLOCK (D-10)
        }
    }
    return out, warns
}
```

### Minimal frozen crush.json villa renders (illustrative — field names verified)
```json
{
  "$schema": "https://charm.land/crush.json",
  "options": {
    "disable_metrics": true,
    "disable_provider_auto_update": true,
    "disable_default_providers": true,
    "auto_lsp": false
  },
  "providers": {
    "villa": {
      "name": "VillaStraylight (local)",
      "type": "openai-compat",
      "base_url": "http://127.0.0.1:8080/v1",
      "api_key": "local",
      "models": [
        {
          "id": "villa-qwen3-coder-30b-a3b",
          "name": "Qwen3-Coder-30B-A3B (local)",
          "context_window": 65536,
          "default_max_tokens": 16384,
          "cost_per_1m_in": 0, "cost_per_1m_out": 0,
          "cost_per_1m_in_cached": 0, "cost_per_1m_out_cached": 0,
          "can_reason": false, "supports_attachments": false
        }
      ]
    }
  },
  "lsp": {
    "go": { "command": "gopls" }
  }
}
```
> `models[].id` is `villa-`-prefixed (#2649-safe, D-09). The exact prefix and whether the id is `villa-<servedbasename>` is the planner's call (Open Questions Q1). `context_window` = the agent ctx (`CoderAgentCtx`). `disable_default_providers`/`auto_lsp` are recommended but confirm exact effect before locking (see schema notes).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| OpenCode as the agent | Crush v0.76.0 | v1.4 roadmap (2026-06-12) | OpenCode is structurally unlockable to zero-outbound; Crush's two channels are config-killable. [CITED: SUMMARY.md] |
| `--yolo` for unattended runs | `permissions.allowed_tools` allowlist | Crush v0.76.0 `run` rejects `--yolo` | Phase-24 harness already uses allowed_tools (tighter). [VERIFIED: STATE.md 24-03] |
| Code-RAG / Qdrant code collection | Agent-native LSP + grep/glob + AGENTS.md | v1.4 roadmap | villa renders `lsp` only; no vector index. [CITED: SUMMARY.md] |

**Deprecated/outdated:**
- The STACK.md crush.json *sketch* (MEDIUM confidence) — superseded by the FROZEN schema here. Notably the sketch's `lsp: { "go": { "command": "gopls" } }` shape is CORRECT, but the model fields needed the verified required-field set (cost_*, can_reason, supports_attachments).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `options.disable_default_providers = true` makes built-in providers unusable (strengthens cloud-fallback prevention) | crush.json schema / Pitfall 2 | LOW — it is in the schema; exact runtime scope confirmed only by the Phase-27 egress proof. If wrong, the single-provider + egress proof still close the threat. |
| A2 | Setting `options.auto_lsp = false` makes the rendered `lsp` block the authoritative complete set | Pitfall 5 | LOW — worst case Crush still auto-configures a server; harmless for locality (LSP servers are local), only affects render determinism. |
| A3 | A restrictive `permissions` baseline is renderable in Phase 26 without the full STRIDE pass | crush.json schema / permissions | MEDIUM — the exact restrictive set is finalized in Phase 27 (STRIDE). Phase 26 must avoid allow-all; the precise allowlist is deferred. |
| A4 | Comparing parsed-semantic config (unmarshal→DeepEqual) is the right config-drift primitive | Pitfall 4 | MEDIUM — the planner may prefer raw-byte compare; both are viable, must pick one and golden-freeze. |

**If this table is empty:** it is not — these four assumptions need a planning/Phase-27 confirmation but none blocks Phase 26 implementation.

## Open Questions

1. **What model id does the rendered llama-server advertise, and how does villa make crush.json's `models[].id` equal it while staying `villa-`-prefixed (D-09 dual constraint)?**
   - What we know: llama-server is launched with `-m <path>` and **no `--alias`** today (`internal/inference/backend_vulkan.go`), so it advertises the model id as the **GGUF file basename**. The existing `GenerationProbe` passes `cfg.Model` (catalog id) as the request model param, which works because llama.cpp ignores the model field when one model is loaded. The loopback endpoint is the **fixed constant `http://127.0.0.1:8080`** (`serverPort = 8080`), NOT a `config.toml` field — so D-08's "derived from config `<inference_port>`" actually resolves to this constant (or `inference`'s `Endpoint()`), not a config key.
   - What's unclear: whether Crush sends `models[].id` as the request `model` and whether llama-server's single-model leniency makes the exact id immaterial — OR whether to add `--alias villa-<model>` to the render so the served id is authoritative.
   - Recommendation: **lock one of two options in planning.** (i) Add `--alias villa-<servedmodel>` to the coding-mode render delta (touches `internal/inference` seam + a NEW coding-mode golden — append-only, Phase-25-style) so the served id deterministically equals crush.json's id; OR (ii) keep Phase 26 render-only and rely on llama.cpp's single-model leniency (the request model id is effectively ignored), setting `models[].id` to any `villa-`-prefixed value. Option (ii) is lower-risk for Phase 26 scope; option (i) is more honest/explicit. The base_url derivation should read `inference.NewContainerRunner(...).Endpoint()` (or the `127.0.0.1:8080` constants) rather than a non-existent config port field.

2. **Does the pin policy store the extracted-binary SHA-256 (for D-14 binary drift) in addition to the tarball SHA-256 (for D-03 install verify)?**
   - What we know: `checksums.txt` gives the **tarball** SHA-256 (`0f66...1ec9`). The binary inside has a different hash.
   - What's unclear: the binary's SHA-256 (must be computed by extracting the verified tarball once — ideally on-hardware during pinning).
   - Recommendation: record a `binarySHA256` field in `crush-policy.json` per asset; compute it once at pin time (extract verified tarball → `sha256sum crush`). Drift (D-14a) compares the installed binary hash to `binarySHA256`. If the planner prefers, drift can instead compare against a hash re-derived from a re-download — but a stored binary hash is the clean, offline-checkable approach. (This is a Phase-26 pinning action; the binary hash is not yet in this research because it requires extracting the tarball.)

3. **What restrictive `permissions` baseline does Phase 26 render (pending the Phase-27 STRIDE pass)?**
   - What we know: `permissions.allowed_tools` bypasses prompts; Crush prompts by default; an allow-all surface is forbidden (Pitfall 7).
   - Recommendation: render `permissions` minimal or omitted (default-prompt) in Phase 26; finalize the exact allowlist with the Phase-27 STRIDE pass. Do NOT allow `bash`/`edit` broadly.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `gopls` (Go LSP) | D-10 lsp render (primary) | probe at runtime (`exec.LookPath`) | — | WARN-with-remediation, omit entry, never BLOCK (D-10) |
| `pyright` / `rust-analyzer` / `typescript-language-server` | D-10 lsp render (opportunistic) | probe at runtime | — | omit entry silently or WARN (discretion) |
| Crush v0.76.0 binary | `villa code` launch | NOT installed by Phase 26 alone (install addon is Phase 27) | v0.76.0 (pinned) | absent → remediation pointing at the Phase-27 install addon (D-13), graceful — never a crash |
| `villa-llama` loopback endpoint (`127.0.0.1:8080`) | the agent's inference target | runtime (may be up/down) | — | `villa code` launches regardless (D-12); a down endpoint is the agent's problem to surface, not villa's to block |

**Missing dependencies with no fallback:** none block Phase 26 *implementation* (all probes/installs are runtime concerns behind seams; tests run off-hardware).
**Missing dependencies with fallback:** LSP servers (WARN), crush binary (remediation to Phase-27 install).

## Validation Architecture

> `workflow.nyquist_validation` not explicitly false in config → section included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven + golden fixtures); no third-party assert/mocking — seams are injected `func` fields |
| Config file | none (`go test`) |
| Quick run command | `go test ./internal/agent/... ./cmd/villa/... -run TestAgent -count=1` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| AGENT-01 | checksum mismatch → refuse-with-remediation, no install (fail-closed) | unit | `go test ./internal/agent -run TestChecksumGate -x` | ❌ Wave 0 |
| AGENT-01 | `crush-policy.json` decodes; version/asset/sha256/size present | unit | `go test ./internal/agent -run TestPolicyLoad -x` | ❌ Wave 0 |
| AGENT-02 | `Render(cfg)` is byte-deterministic (same cfg → identical bytes) | golden | `go test ./internal/agent -run TestRenderGolden -x` | ❌ Wave 0 (NEW golden) |
| AGENT-02 | both kill switches set; exactly one openai-compat loopback provider; non-empty models[]; villa- id | unit | `go test ./internal/agent -run TestRenderContract -x` | ❌ Wave 0 |
| AGENT-02 | missing LSP server → WARN (not BLOCK), entry omitted | unit | `go test ./internal/agent -run TestLSPMissingWarn -x` | ❌ Wave 0 |
| AGENT-03 | `villa code` sets the three lockdown env vars; honors no-auto-flip (coding-mode WARN, still launches) | unit | `go test ./cmd/villa -run TestCodeLockdownEnv -x` | ❌ Wave 0 |
| AGENT-03 | the no-auto-flip structural guard stays green with the new verb | structural | `go test ./cmd/villa -run TestNoAutoFlipStructuralGuard -x` | ✅ (Phase 25) — must stay green |
| AGENT-03 | `TestSeamGrepGate` stays green (no backend/coding literals in internal/agent or code.go) | structural | `go test ./internal/inference -run TestSeamGrepGate -x` | ✅ — must stay green |
| AGENT-04 | binary drift (installed SHA ≠ policy binary SHA) detected, never auto-corrected | unit | `go test ./internal/agent -run TestBinaryDrift -x` | ❌ Wave 0 |
| AGENT-04 | config drift (on-disk ≠ freshly-rendered) detected + remediation, never auto-corrected | unit | `go test ./internal/agent -run TestConfigDrift -x` | ❌ Wave 0 |
| AGENT-04 | binary absent → graceful remediation (Phase-27 install), not a crash | unit | `go test ./cmd/villa -run TestCodeBinaryAbsent -x` | ❌ Wave 0 |

### Testable invariants (the Nyquist samples)
- **checksum-before-install:** a mismatched tarball never reaches extraction (D-03).
- **byte-identical re-render determinism:** `Render(cfg)` twice → identical bytes (D-14 config drift depends on this).
- **WARN-not-BLOCK on missing LSP:** a missing `gopls` produces a warning + an omitted entry, never a non-zero/blocking result (D-10).
- **no-auto-flip preserved:** `villa code` never mutates `CodingMode` (the Phase-25 guard stays green; D-12).
- **drift-never-auto-corrected:** both drift signals return a report only; no write/repair path exists in the core (D-14).
- **seam-clean:** `internal/agent` + `cmd/villa/code.go` contain no backend/coding-flag literals (`TestSeamGrepGate`).

### Sampling Rate
- **Per task commit:** `go test ./internal/agent/... ./cmd/villa/... -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** full suite green + `TestSeamGrepGate` + `TestNoAutoFlipStructuralGuard` green before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/agent/agent_test.go` — policy load, checksum gate, render contract, drift — covers AGENT-01/02/04
- [ ] `internal/agent/testdata/crush.json.golden` — rendered-config golden (NEW fixture) — covers AGENT-02 determinism
- [ ] `cmd/villa/code_test.go` — lockdown env, binary-absent remediation, coding-mode WARN — covers AGENT-03
- [ ] Framework install: none — stdlib `testing` already in use.

*(`TestSeamGrepGate` and `TestNoAutoFlipStructuralGuard` already exist and must remain green — they are regression anchors, not new tests.)*

## Security Domain

> `security_enforcement` not explicitly false → included. Phase 26 sets the rendered-config security surface; the runtime STRIDE/egress proofs are Phase 27.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | local-only; api_key is a non-secret dummy |
| V3 Session Management | no | n/a |
| V4 Access Control | yes | `permissions.allowed_tools` restrictive baseline (no allow-all); `disable_default_providers` to deny cloud providers |
| V5 Input Validation | yes | rendered config values are fixed literals with NO `$(...)`/shell metachars (Pitfall 1); decode of embedded policy is build-time (panic-on-malformed), never runtime attacker input |
| V6 Cryptography | yes | SHA-256 verify-before-install (D-03) via stdlib `crypto/sha256` — never hand-rolled; sigstore bundle available for optional Phase-27 attestation |
| V12 File Resources | yes | extraction confined to `$XDG_DATA_HOME/villa/bin` (traversal guard, `assertInsideDir` pattern); config written 0600 under `~/.config/crush`; binary 0755, dir 0700 |

### Known Threat Patterns for {Crush integration}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| `$(...)` command execution via config values | Elevation of Privilege / Tampering | global-only render (D-06); metachar-free fixed literals; document project-local trust model (Pitfall 1) |
| Slopsquatted / tampered release binary | Tampering | SHA-256 pin verified before install (D-03); org is well-known Charm; sigstore bundle available |
| Cloud-model fallback (code exfiltration) | Information Disclosure | exactly one provider (D-08); `disable_default_providers`; explicit non-empty models[]; runtime egress + llama-down controls (Phase 27) |
| Telemetry phone-home | Information Disclosure | `disable_metrics` + `disable_provider_auto_update` (config) AND the env-var trio (launcher) — belt-and-braces (D-07/D-11); proven at runtime in Phase 27 |
| Tar extraction path traversal | Tampering | confine extraction to `$XDG_DATA_HOME/villa/bin` (reject `..`/absolute entries) |
| Workspace-escape / allow-all permissions | Tampering / EoP | restrictive `permissions` baseline (no broad `bash`/`edit`); full STRIDE pass in Phase 27 |

## Sources

### Primary (HIGH confidence)
- charmbracelet/crush **v0.76.0** `schema.json` (raw GitHub at the v0.76.0 tag) — top-level keys; `options`/`providers`/`models`/`lsp`/`permissions` field names + types + defaults; provider `type` enum; model required fields. [VERIFIED this session]
- charmbracelet/crush **GitHub Releases API** `tags/v0.76.0` — asset `crush_0.76.0_Linux_x86_64.tar.gz`, `checksums.txt`, `checksums.txt.sigstore.json`, published 2026-06-05T21:00:12Z. [VERIFIED]
- charmbracelet/crush **checksums.txt** (release download) — `0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9  crush_0.76.0_Linux_x86_64.tar.gz`; HEAD content-length 25155696. [VERIFIED]
- charmbracelet/crush **#2649** — model-id shadowing is an id-COLLISION bug; truly-novel ids work; villa- prefix is the fix. [VERIFIED]
- charmbracelet/crush **#2983** — empty/omitted `models[]` on openai-compat → `GET {base_url}/models` at config load. [VERIFIED]
- Crush docs (mintlify) — LSP config shape (keyed by name; command/args/env/disabled/timeout; locally-installed only, never auto-downloads); `auto_lsp` default true. [VERIFIED]
- deepwiki Crush config — global config path `$HOME/.config/crush/crush.json`; project-local precedence; `$VAR`/`$(...)` expansion in command/args/env/headers/url; `CRUSH_GLOBAL_CONFIG`; embedded Catwalk DB fallback; `disable_default_providers` exists. [VERIFIED/MEDIUM where docs were terse]
- Codebase (read this session): `internal/preflight/floors.go` + `rocm-policy.json` (go:embed policy pattern), `internal/download/download.go` (checksum-verify discipline), `internal/codingmode/codingmode.go` (pure-core+Deps+Result), `cmd/villa/coding-mode.go` (thin caller + live wiring), `internal/config/villaconfig.go` (config source + XDG helpers + omit-when-off), `internal/inference/backend_vulkan.go` (loopback `127.0.0.1:8080` constant; `-m` with no `--alias`), `internal/recall/store.go` (`$XDG_DATA_HOME/villa` resolution). [VERIFIED]

### Secondary (MEDIUM confidence)
- `.planning/research/STACK.md`, `SUMMARY.md`, `PITFALLS.md` — agent selection, residency, kill-switch names, telemetry/permission pitfalls (OpenCode-flavored where noted; Crush-specific facts re-verified above).
- `.planning/phases/25-*/25-CONTEXT.md` + STATE.md — `villa code` reservation, no-auto-flip guard, build-9496-scoped qualification, Phase-24 `permissions.allowed_tools` finding.

### Tertiary (LOW confidence, validate in phase)
- `options.disable_default_providers` exact runtime scope and `auto_lsp:false` interaction with an explicit lsp block — schema-present, confirm behavior in planning/Phase-27.

## Metadata

**Confidence breakdown:**
- crush.json schema: HIGH — verified against the v0.76.0 schema.json + docs; the two traps (#2649, empty-models) confirmed against the source issues.
- Pin policy / release artifacts: HIGH — asset name, SHA-256, size, checksums.txt all fetched this session.
- Codebase analogs: HIGH — every cloned pattern read in full this session.
- Drift normalization + alias/served-id linkage: MEDIUM — two viable approaches each; the planner must lock one (Open Questions Q1/Q2).
- Permissions restrictive baseline: MEDIUM — finalized with the Phase-27 STRIDE pass (A3).

**Research date:** 2026-06-13
**Valid until:** crush.json schema + pin artifacts are version-locked at v0.76.0 (stable as long as the pin holds). Re-verify only on a deliberate re-pin. Codebase analogs: stable. ~30 days for the MEDIUM items.
