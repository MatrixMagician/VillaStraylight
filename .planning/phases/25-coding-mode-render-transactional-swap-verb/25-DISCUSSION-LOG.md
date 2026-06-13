# Phase 25: Coding-Mode Render & Transactional Swap Verb - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-13
**Phase:** 25-Coding-Mode Render & Transactional Swap Verb
**Mode:** --auto (recommended defaults auto-selected; no interactive prompts)
**Areas discussed:** Render-delta mechanism, Coding-mode state representation, Verb shape & naming, Transactional core reuse, Under-load prove & residency-mode handling

---

## Render-delta mechanism (CMODE-01)

| Option | Description | Selected |
|--------|-------------|----------|
| RunSpec/RenderInput descriptor, backends append behind seam | Optional coding-mode descriptor threaded through RunSpec; existing Vulkan/ROCm ContainerArgs append `--jinja`/agent-ctx/sampling/`--cache-reuse` behind the seam; zero descriptor ⇒ byte-identical | ✓ |
| New coding-mode Backend implementation | A 3rd backend impl + BackendFor branch | |
| Render entirely from a separate Quadlet template | A parallel `villa-coder.container.tmpl` rendered when coding-on | |

**Choice:** Descriptor over the existing backend (recommended). Keeps a single backend + single polymorphism point; addon-off is byte-identical by construction; flag literals stay seam-locked (`TestSeamGrepGate` green).

---

## Coding-mode state representation

| Option | Description | Selected |
|--------|-------------|----------|
| Append-only `omitempty` config fields (memory-stack precedent) | `coding_mode` bool + resolved coder model/quant/agent_ctx in config.toml; omit-when-off ⇒ byte-identical on disk; units regenerate from config; survives restart | ✓ |
| Ephemeral in-memory / runtime flag | Mode not persisted; lost on restart | |
| Separate sidecar state file | A non-config file tracking active mode | |

**Choice:** Config fields following the v1.3 `MemoryEnabled` precedent (recommended). Config is the single source of truth; render derives the descriptor from config; explicit-verb-only mutation.

---

## Verb shape & naming (CMODE-02)

| Option | Description | Selected |
|--------|-------------|----------|
| `villa coding-mode enter` / `villa coding-mode exit` | Two explicit subcommands; typed Result; explicit-only (ROCm `backend set` precedent); avoids `villa code` | ✓ |
| `villa code on` / `villa code off` | Reuses `villa code` namespace | |
| Auto-flip on agent launch | Mode changes implicitly | |

**Choice:** `villa coding-mode enter|exit` (recommended). `villa code` is RESERVED for the Phase-26 agent launcher; mode never auto-flips.

---

## Transactional core reuse

| Option | Description | Selected |
|--------|-------------|----------|
| New `internal/codingmode` core cloning backendswap, composing modelswap | Capture→mutate→prove→rollback frame (backendswap) wrapping modelswap forward ordering; literal-free of backend markers | ✓ |
| Fork/extend modelswap directly | Add transactional behavior inside modelswap | |
| Inline the state machine in cmd/villa | No pure core | |

**Choice:** New pure core cloning the proven backendswap frame (recommended). Keeps host-touching actions behind injected Deps; testable off-hardware; doesn't fork a proven core.

---

## Under-load prove & residency-mode handling

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse backendswap residency+generation Prove; defer tool-call to P27; swap=model swap, shared=render-delta-only | Real generation probe + RunningOffloadVerdict under load; silent/partial CPU fallback FAILs→rollback; tool-call round-trip is Phase 27 | ✓ |
| Add a real tool-call round-trip to the prove now | Move P27 readiness into P25 | |
| Health-200 / is-active as success | False-green | |

**Choice:** Residency+generation prove discipline matching backendswap (recommended). Tool-call round-trip explicitly deferred to Phase 27; `swap` is the realized primary path (all 3 Phase-24 entries PASS at swap), `shared` applies render-delta-only without a model change.

---

## Claude's Discretion

- Go package name (`internal/codingmode` vs `internal/modeswap`); config field/JSON/TOML key names (memory-stack `omitempty` precedent).
- `RunSpec`/`RenderInput` coding-mode descriptor field names/shape.
- Golden organization for the coding-mode-ON render variant (off-path goldens untouched).
- Human-readable `villa coding-mode enter|exit` CLI output (mirror `backend set`).
- Live `Prove` closure composition (PollHealth + GenerationProbe + RunningOffloadVerdict).

## Deferred Ideas

- Real tool-call round-trip readiness gate + egress/llama-down negative controls — Phase 27.
- Crush delivery / pin policy / `crush.json` / `villa code` launcher — Phase 26.
- Coding-feature surfacing (`status.Report` 3→4, dashboard, doctor, backup) — Phase 28.
- Co-resident `villa-coder` unit — CODER-V2-01 (deferred).
- Exact `shared`-residency operator UX beyond render-delta-only — refine in planning.
