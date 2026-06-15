# Phase 28: Agent Surfacing & Contracts - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Every decision below auto-selected the
> recommended default, which for this phase is uniformly **"mirror the proven
> v1.2/v1.3 precedent."** Phase 28 adds no new mechanism — it surfaces a finished
> feature set (Phases 24–27) through cores that already exist.

<domain>
## Phase Boundary

Make the coding agent (shipped in Phases 24–27) **visible, diagnosable, recoverable,
and measurable** to the operator — without introducing any new agent capability:

- **See** — `villa status` (human + `--json`) reports an append-only `coding` block; the
  dashboard renders a matching Agent panel, hidden until data exists.
- **Diagnose** — `villa doctor` folds agent checks (binary/version drift, config drift, a
  real tool-call round-trip probe, under-load residency).
- **Recover** — `villa backup`/`restore` cover the rendered agent config; the agent binary
  is identity-recorded and excluded from the archive (exactly like model weights).
- **Measure** — agent token usage attributed per-model via the v1.2 usage core; cache
  effectiveness (`timings.cache_n` vs `prompt_n`) surfaced as an honest agent-speed signal.

**This is the v1.4 finale.** `status.Report` 3→4 lands ONCE at the end of the phase
(the proven v1.2/v1.3 single-bump discipline) — no per-plan schema churn.

**Out of scope:** any new agent feature, a custom chat/agent UI, or changing how the agent
is installed/rendered/swapped (Phases 24–27 own those). Surfacing only.

</domain>

<decisions>
## Implementation Decisions

### Status `coding` block (SURF-01)
- **D-01:** Add a pointer block `Coding *CodingInfo \`json:"coding,omitempty"\`` to
  `status.Report`, **tail-appended above `SchemaVersion`** — mirroring the v1.3
  `Memory *MemoryInfo \`json:",omitempty"\`` precedent exactly (`internal/status/status.go:149`).
  Nothing above it moves (append-only contract).
- **D-02:** **Hidden-until-data:** the block is omitted entirely from JSON when the agent is
  disabled (`omitempty` + nil pointer), same as the Memory block. Coding-off output is
  byte-identical to today except for the schema version.
- **D-03:** Field set (from ROADMAP SC1): `enabled`, agent **version + pin match** (bool —
  does the on-disk binary identity match the policy pin), `model`, `mode`, `residency`
  (`swap`/`shared`). Append-only within the block.
- **D-04:** **Single schema re-freeze:** `reportSchemaVersion` 3→4 happens exactly ONCE,
  at the end of the phase, with coding-on AND coding-off golden variants refrozen together
  (`go test … -update`). No intermediate bumps across plans.

### Dashboard Agent panel (SURF-01)
- **D-05:** Add an Agent panel to the embedded SPA (`internal/dashboard/assets/dashboard.{html,css,js}`),
  mirroring the **Memory panel** treatment: rendered **only when the `coding` block is present**
  in the status JSON (hidden entirely otherwise — no empty shell). Renders version+pin-match,
  model, mode, residency, plus the usage + cache-effectiveness signals (D-09/D-10).

### Doctor agent checks (SURF-02)
- **D-06:** Fold agent checks into the existing `internal/doctor` core (not a new command):
  binary/version drift, config drift, a **real tool-call round-trip probe**, and **under-load
  residency**. Reuse the Phase-27 `villa verify agent` readiness round-trip and the
  `RunningOffloadVerdict`/`liveProve` seam rather than re-rolling probes.
- **D-07:** **Honesty dominance:** an offload/residency FAIL **dominates** a healthy-looking
  HTTP-200 — never a false-green. Same fail-closed discipline as `verify agent`
  (silent/partial CPU fallback = FAIL, not PASS).

### Backup / restore coverage (SURF-03)
- **D-08:** Mirror the **BAK-01 model-weights pattern** (`internal/backup/`): the rendered
  agent config (crush.json + agent units) goes **into** the archive; the agent **binary** is
  **identity-recorded in the manifest** (sha256 + version/pin, via `internal/backup/manifest.go`
  / `checksum.go`) and **excluded** from the archive bytes. Restore re-stages the binary the
  same way weights are re-pulled (refuse-with-remediation if identity drifts).

### Usage & cache-effectiveness (USAGE-03, USAGE-04)
- **D-09:** Attribute agent token usage **per-model** through the existing v1.2 usage core
  (`internal/status/usage.go`). The coder is a **distinct served model**, so attribution is
  keyed by model id — no new accounting path, just ensure the coder model surfaces.
- **D-10:** Surface cache effectiveness as the `timings.cache_n / prompt_n` ratio (an honest
  agent-speed signal) sourced from the llama.cpp `/metrics` scrape (`internal/metrics`).
  **Typed-Unknown degradation:** when timings are absent/unparseable, show Unknown — never a
  fabricated 0% or false signal.

### Claude's Discretion
- Exact JSON field names within the `coding` block, panel layout/CSS, and the human-readable
  status table wording — pick the spelling that matches the Memory block/panel idiom; lock
  them at plan time. (All are append-only/byte-frozen once golden, so freeze deliberately.)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` — Phase 28 section + Success Criteria 1–5 (the authority on the block
  field set, single golden re-freeze, doctor dominance, backup identity-record, usage/cache).
- `.planning/REQUIREMENTS.md` — SURF-01, SURF-02, SURF-03, USAGE-03, USAGE-04.
- `.planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-CONTEXT.md` — the agent
  addon decisions, the `villa verify agent` readiness tool-call round-trip, and residency proof
  reused by the doctor checks (D-06/D-07).

### Cores being surfaced (precedents to mirror)
- `internal/status/status.go` — `Report` struct, `reportSchemaVersion` (3→4), and the
  `Memory *MemoryInfo,omitempty` pointer-block precedent for D-01/D-02/D-04.
- `internal/status/usage.go` — the v1.2 per-model usage core for D-09.
- `internal/doctor/doctor.go` — the existing doctor check core that D-06/D-07 fold into.
- `internal/backup/backup.go`, `internal/backup/manifest.go`, `internal/backup/checksum.go` —
  the BAK-01 weights exclude + identity-record precedent for D-08.
- `internal/dashboard/assets/dashboard.{html,css,js}` — the Memory panel hidden-until-data
  precedent for D-05.
- `internal/metrics/llamacpp.go` — the `/metrics` scrape (`cache_n`/`prompt_n`) for D-10.
- `cmd/villa/verify_agent.go` — the tool-call round-trip + `RunningOffloadVerdict` reuse for D-06.

### Frozen-contract guardrails
- `cmd/villa/testdata/*.golden*` — the `--json`/dashboard byte-frozen contracts; the 3→4 bump
  is the ONE intentional re-freeze (coding-on/off variants).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`status.Report` + `reportSchemaVersion`** (`internal/status/status.go`): currently v3; the
  `Memory *MemoryInfo,omitempty` pointer at line 149 is the exact shape to clone for `Coding`.
- **v1.2 usage core** (`internal/status/usage.go`): already attributes tokens per served model —
  the coder is just another model id; no new accounting.
- **`internal/doctor`**: existing check framework; agent checks fold in as additional checks.
- **`internal/backup` (BAK-01)**: manifest already records excluded-model identities + per-entry
  sha256 — the agent binary slots into the same exclude+identity mechanism.
- **Dashboard Memory panel** (`internal/dashboard/assets/`): hidden-until-data panel pattern to
  copy for the Agent panel.
- **`villa verify agent`** (`cmd/villa/verify_agent.go`) + `liveProve`/`RunningOffloadVerdict`:
  the tool-call round-trip + under-load residency probe the doctor check reuses.
- **`internal/metrics`**: llama.cpp `/metrics` scrape already parses timings for pp/tg; extend to
  read `cache_n`/`prompt_n` for the cache-effectiveness ratio.

### Established Patterns
- **Single schema bump per milestone phase** (v1.2/v1.3): bump `reportSchemaVersion` ONCE, refreeze
  goldens once, append-only — never per-plan.
- **Hidden-until-data** for optional feature blocks/panels (Memory precedent).
- **Honesty-by-construction:** offload/residency FAIL dominates HTTP-200; typed-Unknown for
  unevaluable signals (never a fabricated value).
- **Pure-core + injectable seam:** every host-touching probe is a `func` field; live wiring is a
  `live*Deps()` closure in `cmd/villa`.

### Integration Points
- `cmd/villa/status.go` (human render) + the status `--json` path → new `coding` block.
- `cmd/villa/doctor.go` → fold agent checks.
- `cmd/villa/backup.go` / `restore.go` → agent config + binary identity.
- `internal/dashboard/api.go` + assets → Agent panel fed by the same `status` read-model.

</code_context>

<specifics>
## Specific Ideas

Surfacing-only finale: reuse, don't reinvent. Each of the five requirements maps 1:1 to an
existing proven core (status block ↔ Memory block, doctor ↔ verify-agent probe, backup ↔ BAK-01
weights, usage ↔ v1.2 usage core, cache ↔ metrics scrape). The single `status.Report` 3→4
re-freeze is the only contract change in the entire phase and must land once, at the end.

</specifics>

<deferred>
## Deferred Ideas

### Reviewed Todos (not folded)
- **`install-reverts-rocm-to-vulkan`** (todo matcher score 0.6) — **reviewed, NOT folded.**
  It is a `cmd/villa/install.go` config-is-source-of-truth bug (`runInstall` recomputes the
  recommendation and clobbers a persisted `backend=rocm` opt-in back to Vulkan). It is a
  different domain from agent *surfacing* and the todo itself is self-tagged "Out of Phase-27
  scope … fix in a maintenance pass." Keep it as a standalone maintenance fix (a `/gsd-quick`
  or `/gsd-debug` item), not Phase 28 scope. Matched only on keyword overlap (install, rocm,
  status, phase, hardware).

</deferred>

---

*Phase: 28-agent-surfacing-contracts*
*Context gathered: 2026-06-15*
