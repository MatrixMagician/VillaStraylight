# Phase 13: `villa doctor` Health Diagnosis - Context

**Gathered:** 2026-06-07
**Status:** Ready for planning
**Mode:** `--auto` (decisions auto-resolved to the recommended option for each gray area; review before planning)

<domain>
## Phase Boundary

Add a single `villa doctor` command that produces a **one-shot, read-only health
diagnosis** of a running install — built as a new **pure `internal/doctor` core**
that *composes* the shipped cores (preflight checks + `status` read-model +
inference residency proof) plus a **config-vs-disk drift check** — and renders a
findings report with a preflight-mirroring **0 (healthy) / 2 (blocking fault) /
1 (warning)** exit contract. Every non-healthy finding carries actionable
remediation text. Offload-asserting: a silent or partial CPU fallback is a FAIL,
never a false-green.

**In scope:**
- New pure `internal/doctor` core (no host I/O in the core; host effects via an
  injected `Deps` struct of `func` fields) that aggregates findings from the
  existing cores and a new drift check.
- A new `cmd/villa/doctor.go` cobra verb with `liveDoctorDeps` wiring and exit-code
  mapping (0/2/1) mirroring `renderPreflight`.
- Config-vs-disk drift detection: render Quadlet units from config and diff against
  on-disk units via `orchestrate.Reconcile` (a non-empty `Changed` slice = drift).
- A doctor-OWNED `--json` contract with its own `schema_version`, frozen by its own
  NEW golden — the byte-frozen `status.Report` is NOT extended.
- Remediation text on every non-healthy finding (DOCTOR-02).

**Out of scope (own phases / future / explicitly excluded):**
- **Any mutation/repair of the install** — doctor diagnoses + remediates-by-advice
  ONLY; mutation stays in explicit verbs (`install` / `backend set` / future
  `restore`). (REQUIREMENTS.md Out-of-Scope row.)
- **`doctor` diagnostic-bundle / output upload** — output stays strictly local
  (REQUIREMENTS.md Out-of-Scope; zero-telemetry invariant).
- Extending or re-freezing the `status.Report` schema (Phase 15 owns the next
  `status` schema bump; doctor must not touch it).
- A live generation probe / load (that is a mutation; see D-07).
</domain>

<decisions>
## Implementation Decisions

### Composition & module boundary
- **D-01:** Build a **new pure `internal/doctor` core** with an injected `Deps`
  struct of `func` fields that wrap the existing cores — **composition only, never
  re-implement**. It calls into the shipped preflight gate, the `status` read-model
  (`status.Aggregate`), the inference `ResidencyProof`/offload-assert path, and
  `orchestrate.Reconcile` for drift. Mirrors the established pure-core + injectable-seam
  pattern (`backendswap`/`bench`/`status`). `cmd/villa/doctor.go` provides
  `liveDoctorDeps`.
- **D-02:** **Do NOT extend `status.Report`.** doctor gets its own report type and
  its OWN unconstrained golden (per ROADMAP implementation note). The byte-frozen
  `status.Report` contract stays untouched (Phase 15 owns the next `status` bump —
  keep only one byte-frozen contract evolving at a time).
- **D-03:** **doctor never mutates the install.** It is read-only and
  remediates-by-advice only — no repair, no reconcile-write, no generation probe,
  no backend swap. Mutation stays in explicit verbs.

### Severity aggregation & exit-tier contract
- **D-04:** **Worst-severity-wins, mirroring preflight's `renderPreflight`.** Each
  finding carries an explicit tier so the exit rollup is mechanical:
  - any **blocking** finding (preflight BLOCK, a confident residency FAIL / CPU
    fallback) → **exit 2**;
  - any **warning** (preflight WARN, config-vs-disk drift, a typed-Unknown /
    unevaluable signal) → **exit 1**;
  - all healthy → **exit 0**.
  This satisfies DOCTOR-01 (mirror the preflight exit contract) by construction.
- **D-05:** **Config-vs-disk drift is a WARN (exit 1)**, not a block — the stack may
  still be running on stale units; remediation is "re-run `villa install` to
  reconcile". A confident **residency FAIL is a BLOCK (exit 2)** — degraded backend
  is a real fault.

### Offload assertion on a running install (read-only)
- **D-06:** **Assert offload read-only from the EXISTING running `llama-server`** —
  reuse the shipped dual-assert (log-scrape residency markers + sysfs GTT delta).
  Confident CPU fallback → **FAIL (exit 2)**; an unevaluable signal (stack down, no
  logs, sysfs absent) → **typed-Unknown → WARN (exit 1)**, NEVER a false-green over
  a health-200 (DOCTOR-02, SC#3).
- **D-07:** **No generation probe / model load.** Unlike `backendswap`/`bench`,
  doctor must not trigger a live generation to prove offload — that is a mutation/load
  doctor is forbidden from doing (D-03). It diagnoses over whatever the running
  stack already exposes.
- **D-08:** **A down / not-installed stack is a reported finding, not a crash.**
  doctor degrades gracefully (typed-Unknown / WARN with remediation), consistent
  with the project's honesty-by-construction degradation.

### Output contract & drift mechanism
- **D-09:** **doctor owns its own `--json` contract** with a `schema_version` from
  day one, frozen by a NEW golden under `cmd/villa/testdata/`. Human/text output
  mirrors preflight's findings table (`renderPreflight`-style: check name, tier,
  detail, remediation). Append-only + schema-bump discipline applies to the new
  golden going forward.
- **D-10:** **Drift is detected via `orchestrate.Reconcile(units, unitDir)`** —
  render the expected Quadlet units from config (the source of truth), Reconcile
  against the on-disk unit dir, and treat a **non-empty `Changed` slice as drift**
  (DOCTOR-03). Reconcile is the proven pure diff primitive (already used by
  `model swap` / `backend set`); reuse it, do not write units.
- **D-11:** **Every non-healthy finding carries remediation text** (DOCTOR-02).
  Preflight `CheckResult`s already carry remediation (refuse-with-remediation);
  doctor's own findings (drift, residency, stack-down) must supply equivalent
  actionable remediation strings.

### Claude's Discretion
- Exact package/file layout (`internal/doctor/doctor.go` + `cmd/villa/doctor.go`),
  and whether doctor reuses `preflight.CheckResult` directly as its finding type or
  defines a small doctor-specific `Finding` wrapper — planner picks the lowest-churn
  option that keeps doctor's OWN golden and does not leak backend literals out of
  the inference seam.
- Whether the ROCm-family preflight checks run by doctor key off the **configured
  backend** (so doctor diagnoses over the active backend, incl. the Phase 12 alt
  image) — preferred — vs always running the full matrix; planner decides.
- The exact set of `status` signals folded in (service active-state, health-200,
  loopback bind assertion, active backend + image tag, live tok/s) — pick the subset
  that yields actionable health findings without duplicating `villa status` verbatim.
- Naming of doctor's findings/checks and the precise drift remediation wording.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → "Phase 13: `villa doctor` Health Diagnosis" — Goal, 4
  Success Criteria, and the **Implementation note** (new pure `internal/doctor` core
  with its OWN unconstrained golden; do NOT extend `status.Report`; diagnose +
  remediate-by-advice only, never mutate).
- `.planning/REQUIREMENTS.md` → **DOCTOR-01 / DOCTOR-02 / DOCTOR-03**, plus the
  Out-of-Scope rows ("`doctor` diagnostic-bundle upload"; "`doctor` mutating/repairing
  the install automatically").
- `.planning/PROJECT.md` → Key Decisions (offload-asserting / no false-green;
  config is the single source of truth; strictly-local / zero-telemetry).

### Cores doctor composes (compose, do NOT re-implement)
- `internal/preflight/preflight.go` — `CheckResult` (BLOCK/WARN tiers, fail-soft),
  `RunROCm` and the reusable gate; the BLOCK/WARN→exit semantics doctor mirrors.
- `cmd/villa/preflight.go` → `renderPreflight` (`cmd/villa/preflight.go:73`) — the
  0/2/1 exit-code mapping + findings-table rendering to mirror; and
  `TestPreflightExitCodes` for the exit contract pattern.
- `internal/status/status.go` — `Aggregate` / `Report` read-model (the health
  signals to fold in; do NOT extend `Report`).
- `internal/inference/backend.go`, `backend_rocm.go`, `backend_vulkan.go`,
  `offload.go`, `running_offload.go` — `ResidencyProof` + the dual-assert
  (log-scrape + sysfs GTT) offload path; backend literals stay behind this seam.
- `internal/orchestrate/reconcile.go` → `Reconcile(units, unitDir) (Plan, error)`
  (`internal/orchestrate/reconcile.go:26`) — the pure config-vs-disk diff; non-empty
  `Plan.Changed` = drift. `internal/orchestrate/render.go` for rendering expected
  units from config.

### Contracts & conventions
- `CLAUDE.md` → "Offload-asserting (silent CPU fallback = FAIL)", "Inference seam
  grep-gate (`TestSeamGrepGate`)", "`--json`/dashboard contracts are byte-frozen by
  golden tests", "Config is the single source of truth", "Dashboard binary trap".
- `.planning/codebase/ARCHITECTURE.md`, `CONVENTIONS.md`, `TESTING.md` — pure-core +
  injectable-seam pattern, `live*Deps` wiring, golden-test discipline, exit-code
  mapping conventions.
- `cmd/villa/testdata/*.golden*` — golden-test format reference for doctor's NEW
  `--json` golden.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`renderPreflight`** (`cmd/villa/preflight.go:73`): the exact 0/2/1 exit-code
  mapping + `--force`/`--json`/`--provenance` rendering doctor mirrors; doctor reuses
  the table shape and the BLOCK/WARN→exit rollup.
- **`preflight.CheckResult`** + `RunROCm`: BLOCK/WARN findings with remediation
  already carried; doctor can reuse the type (or wrap it) and re-run preflight against
  a running install.
- **`status.Aggregate` / `status.Report`** (`internal/status/status.go`): the health
  read-model (service state, health, loopback, backend+image, tok/s) — fold its
  signals in; do NOT extend the frozen `Report`.
- **`ResidencyProof` + offload dual-assert** (`internal/inference/*`,
  `offload.go`/`running_offload.go`): the no-false-green offload assertion doctor
  needs for SC#3 — read-only over the running server (no generation probe).
- **`orchestrate.Reconcile`** (`internal/orchestrate/reconcile.go:26`): pure
  config-vs-disk diff; empty `Changed` = no-op, non-empty = drift (SC#4/DOCTOR-03).
  Already proven by `model swap` / `backend set`; reuse as the drift primitive.

### Established Patterns
- **Pure-core + injectable-seam** (`backendswap`/`bench`/`status`/`modelswap`): every
  host action is a `func` field on a `Deps` struct; `live*Deps` closure in `cmd/villa`
  wires the real host — keeps doctor testable off-hardware.
- **Worst-severity-wins exit mapping** (preflight): any BLOCK→2, any WARN→1, else 0.
- **Offload-asserting, never liveness** (D-11 project-wide): confident CPU fallback =
  FAIL; unevaluable signal = typed-Unknown → WARN, never false-green.
- **Byte-frozen `--json` goldens, append-only + schema-bump**: doctor gets its OWN
  new golden with `schema_version` from day one — it does NOT touch `status.Report`.
- **Config is the single source of truth**: drift = rendered-from-config units vs
  on-disk units; remediation is to reconcile, never hand-edit units.

### Integration Points
- `cmd/villa/root.go` (`newRoot`) — register the new `doctor` cobra verb.
- `cmd/villa/doctor.go` (new) — `liveDoctorDeps` wiring + exit-code mapping.
- `internal/doctor/` (new pure core) — composes preflight + status + residency proof
  + `orchestrate.Reconcile`.
- New golden under `cmd/villa/testdata/` for doctor's `--json` contract.

</code_context>

<specifics>
## Specific Ideas

- doctor is the **read-only, compositional twin of install's preflight gate** — it
  answers "is this *running* install still healthy?" the way preflight answers "is
  this host *ready* to install?". Same 0/2/1 exit grammar, same
  refuse/warn-with-remediation honesty, but over a live install instead of a bare host.
- Built early in v1.2 deliberately: it surfaces faults that later phases (backup/restore,
  usage, TUI) may introduce, and it carries zero/trivial contract risk (pure core +
  its own golden, no shared frozen-contract evolution).
- Strictly-local / zero-telemetry unchanged — doctor adds NO new outbound; its output
  stays on-box (diagnostic-bundle upload is explicitly out of scope).

</specifics>

<deferred>
## Deferred Ideas

- **Auto-repair / `villa doctor --fix`** — mutation is explicitly out of scope for
  this phase (doctor remediates-by-advice only). Could be a future verb that *composes*
  doctor findings with existing mutating verbs, but not now.
- **Diagnostic-bundle export/upload** — explicitly out of scope (off-box data flow;
  zero-telemetry). Output stays local.
- **A `status`-surfaced "last doctor verdict" field** — would require a `status.Report`
  schema bump, which Phase 15 owns; do not couple doctor into the `status` contract now.
- **None of the above are scope creep into this phase** — discussion stayed within the
  DOCTOR-01/02/03 boundary.

</deferred>

---

*Phase: 13-villa-doctor-health-diagnosis*
*Context gathered: 2026-06-07*
