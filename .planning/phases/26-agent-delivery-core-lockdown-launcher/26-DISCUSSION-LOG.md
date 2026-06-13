# Phase 26: Agent Delivery Core & Lockdown Launcher - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-13
**Phase:** 26-agent-delivery-core-lockdown-launcher
**Mode:** `--auto` (recommended-default selection; no interactive prompts — all options grounded in v1.4 research + Phase 25 locked decisions)
**Areas discussed:** Pure core boundaries, Pin policy & checksum, Binary install location, crush.json render contract, villa code launcher behavior, Drift detection

---

## Pure core module name & boundaries

| Option | Description | Selected |
|--------|-------------|----------|
| `internal/agent` | Milestone's noun; matches the `villa code` launcher; pure core + injected Deps | ✓ |
| `internal/coder` | Mirrors `role:"coder"` catalog naming | |
| `internal/agentconf` | Narrow "config renderer only" scope | |

**User's choice (auto):** `internal/agent` — broadest fit; owns pin policy + render + version + drift; literal-free of backend markers (D-01).
**Notes:** Host effects injected via `Deps`; live wiring is `liveAgentDeps()` in `cmd/villa`, same discipline as backendswap/modelswap/codingmode.

---

## Pin policy & checksum

| Option | Description | Selected |
|--------|-------------|----------|
| `go:embed` policy JSON (rocm-policy pattern), SHA-256 verified before install, autoupdate off | Clone the trusted preflight pattern verbatim | ✓ |
| Hardcoded constants in Go | Simpler but less auditable, no clean upgrade seam | |
| Verify-after-install | Rejected — installs unverified bytes first | |

**User's choice (auto):** `crush-policy.json` via `//go:embed`; checksum verified BEFORE placement; mismatch refuses with remediation; autoupdate forced off (static pin + config kill switch + env). Upgrade/re-pin verb deferred. (D-02/D-03/D-04)

---

## Binary install location

| Option | Description | Selected |
|--------|-------------|----------|
| `$XDG_DATA_HOME/villa/bin/crush` | villa-owned, isolated, uninstallable | ✓ |
| `~/.local/bin/crush` | On user PATH but risks collision with user-installed crush | |

**User's choice (auto):** `$XDG_DATA_HOME/villa/bin/crush`; `villa code` execs the explicit path, never a PATH lookup. (D-05)

---

## crush.json render contract

| Option | Description | Selected |
|--------|-------------|----------|
| Global config only (`~/.config/crush/crush.json`), derived from config.toml | One villa loopback provider, both kill switches, villa-unique model ids, detected-LSP block | ✓ |
| Also manage project-local crush.json | Rejected — project-local config executes `$(...)` at load (code-exec hazard) | |

**User's choice (auto):** Global-only; both kill switches (`disable_metrics`, `disable_provider_auto_update`); exactly one `openai-compat` loopback provider; `villa-`-prefixed model ids matching the served id (#2649 shadowing workaround); LSP for detected toolchains, missing `gopls` → WARN not BLOCK. (D-06..D-10)
**Notes:** Project-file trust model documented in consent text.

---

## villa code launcher behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Env lockdown + exec; no auto-flip of coding mode; WARN if off | Belt-and-braces env on top of config kill switches; honors Phase 25 no-auto-flip guard | ✓ |
| Auto-enter coding mode on launch | Rejected — violates Phase 25 `TestNoAutoFlipStructuralGuard` (explicit-only mode change) | |

**User's choice (auto):** `CRUSH_DISABLE_METRICS=1` + `DO_NOT_TRACK=1` (+ `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1`) before exec; launches against the current endpoint; WARNs (does not block) when coding mode is off; pre-exec presence + drift check. (D-11/D-12/D-13)

---

## Drift detection

| Option | Description | Selected |
|--------|-------------|----------|
| Detect binary-SHA drift + config drift; surface + remediate, never auto-correct | Pure-core comparison, injected reads; surfaced at `villa code` launch | ✓ |
| Auto re-render / auto-reinstall on drift | Rejected by AGENT-04 — never silently auto-corrected | |

**User's choice (auto):** Binary drift (SHA ≠ policy) and config drift (on-disk crush.json ≠ freshly-rendered) both detected + surfaced with remediation; doctor/status surfacing deferred to Phase 28. (D-14)

---

## Claude's Discretion

- Exact Go struct/JSON field names for `crush-policy.json` and rendered `crush.json` (subject to v0.76.0 schema frozen in phase research).
- Exact `villa-` model-id prefix string.
- LSP servers probed beyond `gopls`; WARN/remediation message wording.
- File decomposition of the `internal/agent` core.

## Deferred Ideas

- Install addon + pre-stage + readiness tool-call proof (Phase 27, INSTALL-03).
- Preflight gates + uninstall coverage (Phase 27, INSTALL-04).
- `villa verify agent` egress + llama-down negative controls (Phase 27, PRIV-06).
- status/dashboard/doctor/backup/usage surfacing (Phase 28, SURF-01/02/03).
- Upgrade/re-pin verb (future).
- Project-local crush.json management (out — trust hazard).
- Co-resident `villa-coder` unit (CODER-V2-01, v2).
- Qdrant/embed code-index MCP server (v1.5+, behind numeric eval gate).
