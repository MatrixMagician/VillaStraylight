# Phase 13: `villa doctor` Health Diagnosis - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-07
**Phase:** 13-villa-doctor-health-diagnosis
**Mode:** `--auto` (all gray areas auto-selected; each resolved to the recommended option)
**Areas discussed:** Composition & module boundary, Severity aggregation & exit tiers, Offload assertion on a running install, Output contract & drift detection

---

## Composition & module boundary

| Option | Description | Selected |
|--------|-------------|----------|
| Pure `internal/doctor` core + injected `Deps`, compose existing cores | New pure core wraps preflight + status + residency proof + `orchestrate.Reconcile` via func-field seam; never reimplement; never extend `status.Report` | ✓ |
| Re-implement checks inside doctor | Duplicate preflight/status/residency logic in a doctor-specific path | |
| Extend `status.Report` to carry doctor verdict | Fold doctor output into the existing byte-frozen status contract | |

**Auto-selected:** Pure core + injected Deps (composition). **Notes:** Matches the shipped pure-core + injectable-seam pattern (`backendswap`/`bench`/`status`) and the ROADMAP implementation note (own unconstrained golden; do NOT extend `status.Report`). doctor never mutates — diagnoses + remediates-by-advice only.

---

## Severity aggregation & exit-tier contract

| Option | Description | Selected |
|--------|-------------|----------|
| Worst-severity-wins, mirror preflight | BLOCK / residency-FAIL → 2; WARN / drift / typed-Unknown → 1; all healthy → 0 | ✓ |
| Custom doctor-specific severity scale | New tier model distinct from preflight's 0/2/1 | |

**Auto-selected:** Worst-severity-wins mirroring `renderPreflight`. **Notes:** Satisfies DOCTOR-01 (mirror the preflight exit contract) by construction; config-vs-disk drift = WARN(1), confident residency FAIL = BLOCK(2).

---

## Offload assertion on a running install (read-only)

| Option | Description | Selected |
|--------|-------------|----------|
| Read-only scrape of existing running `llama-server` markers | Reuse dual-assert (log scrape + sysfs GTT delta) over what's already running; confident CPU fallback → FAIL(2); unevaluable → typed-Unknown WARN(1); stack-down reported as a finding; NO generation probe | ✓ |
| Run a live generation probe (backendswap-style) | Trigger a generation to prove offload | |

**Auto-selected:** Read-only scrape, no generation probe. **Notes:** A generation probe is a mutation/load doctor is forbidden from doing. Never a false-green over a health-200 (DOCTOR-02 / SC#3).

---

## Output contract & drift detection

| Option | Description | Selected |
|--------|-------------|----------|
| Own `--json` golden + `orchestrate.Reconcile` drift | doctor-OWNED `--json` with own `schema_version` (status.Report untouched); preflight-style findings table; drift via render→`Reconcile` non-empty `Changed` = WARN w/ remediation | ✓ |
| Reuse the `status` golden / contract | Emit doctor output through the existing frozen status JSON | |

**Auto-selected:** Own golden + Reconcile-based drift. **Notes:** Reconcile is the proven pure config-vs-disk diff (already used by `model swap` / `backend set`); reuse it, never write units. Every non-healthy finding carries remediation (DOCTOR-02 / DOCTOR-03).

---

## Claude's Discretion

- Exact package/file layout (`internal/doctor/doctor.go` + `cmd/villa/doctor.go`) and whether to reuse `preflight.CheckResult` directly vs a doctor-specific `Finding` wrapper.
- Whether the ROCm-family preflight checks key off the configured backend (diagnose over the active backend, incl. the Phase 12 alt image) vs always running the full matrix.
- The exact subset of `status` signals folded in (service state, health-200, loopback bind, backend+image, tok/s) without duplicating `villa status` verbatim.
- Naming of doctor findings/checks and the precise drift remediation wording.

## Deferred Ideas

- **Auto-repair / `villa doctor --fix`** — mutation out of scope; doctor remediates-by-advice only.
- **Diagnostic-bundle export/upload** — explicitly out of scope (off-box; zero-telemetry).
- **A `status`-surfaced "last doctor verdict" field** — would require a `status.Report` schema bump that Phase 15 owns.
