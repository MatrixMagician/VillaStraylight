# Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-06
**Phase:** 11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco
**Mode:** `--auto` (all gray areas auto-selected; recommended option chosen on every question)
**Areas discussed:** Firmware-date probe, HSA-override probe, Schema/golden impact, Doc-reconciliation scope, Out-of-scope boundary, On-hardware verification

---

## Firmware-date probe source & comparison (D-01/D-02)

| Option | Description | Selected |
|--------|-------------|----------|
| exec `rpm -q linux-firmware` + detect-seam policy compare | Mirrors detect's existing `exec.Command` pattern; compare floor/deny behind a `gpu_amd.go` seam (no literal in readiness_rocm.go); KnownBool only when date parses | ✓ |
| Read a firmware version-file from `/usr/lib/firmware` directly | Avoids rpm, but no stable version stamp file; brittle | |
| New literal/compare inline in `readiness_rocm.go` | Breaks the literal-behind-seam grep-gate discipline | |

**Choice:** Option 1 (recommended default). **Notes:** Floor/deny values = preflight `rocm-policy.json` (20260110 / 20251125); research decides share-vs-duplicate to avoid a detect→preflight import cycle (kernel floor is already duplicated detect-side).

---

## HSA-override viability probe definition (D-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Derive from already-Known facts (gfx1151 + ROCm substrate present) | Pure host inspection, no side effects; reuses gfx-id already threaded into computeROCmReadiness | ✓ |
| Exec a probe with the override env set | Side-effecting; out of place in the pure detect path | |
| Check an env var is already exported | Conflates render-unit env (Phase 7) with host viability | |

**Choice:** Option 1 (recommended default). **Notes:** Unknown source fact → UnknownBool; Known non-gfx1151/absent substrate → KnownBool(false).

---

## Schema / golden impact (D-04)

| Option | Description | Selected |
|--------|-------------|----------|
| No new fields, no schema bump, golden byte-identical | Both fields already exist (schema 2); only values get populated; off-hardware fixture keeps them UNSET | ✓ |
| Bump schema + re-freeze detect.golden.json | Unnecessary contract churn for a value-only change | |

**Choice:** Option 1 (recommended default). **Notes:** Research must confirm the golden is fixture-driven, not live-host derived; if live-derived, probes must be injectable/seam-stubbed for determinism.

---

## Doc-reconciliation scope & verification (D-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Fix exactly the audit-named set, each verified vs VERIFICATION.md | 6 SUMMARY tags + 06-REVIEW.md frontmatter + REQUIREMENTS.md ROCM-02 note; re-confirm 3-source cross-check | ✓ |
| Broader doc sweep / prose rewrite | Scope creep beyond the named debt | |

**Choice:** Option 1 (recommended default). **Notes:** Evidence-first — no green-washing; no prose changes beyond frontmatter + the one stale note.

---

## Out-of-scope boundary (deferred)

| Option | Description | Selected |
|--------|-------------|----------|
| Hardening warnings + Nyquist drafts OUT of scope, deferred | ROADMAP Phase-11 names only probes + doc reconciliation | ✓ |
| Fold hardening warnings + Nyquist reconciliation in | Expands a narrow cleanup phase beyond its named items | |

**Choice:** Option 1 (recommended default). **Notes:** ~13 advisory hardening warnings + 4 Nyquist VALIDATION.md drafts captured as Deferred Ideas.

---

## On-hardware verification expectation (D-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Two-tier: off-hardware wiring + no-false-green, on-hardware UAT confirms badge | Off-hardware proves UNSET/Known logic + golden hold; on-hardware UAT confirms live badge reads `ready` | ✓ |
| Off-hardware tests only | Cannot prove the DASH-06 SC#1 residual (badge value is host-dependent) | |

**Choice:** Option 1 (recommended default). **Notes:** Flag for on-hardware UAT like Phases 8–10; quotable acceptance = `firmware_date_ok:true` + `hsa_override_viable:true` + badge `ready`.

---

## Claude's Discretion

- Exact firmware-date parse format / `rpm` query string / fallback source (D-01).
- Share-vs-duplicate of firmware floor/deny values detect-side (D-02).
- Helper names / file placement for the probe bodies + firmware seam.
- Exact `requirements-completed` frontmatter key spelling (match existing green SUMMARYs).

## Deferred Ideas

- ~13 advisory hardening warnings (P8 ×5, P7 ×4 + MesaFloor, P6 ×4) — separate hardening cleanup.
- Nyquist VALIDATION.md draft reconciliation (P6/P8/P9/P10) — `/gsd-validate-phase N`.
- Turning preflight `checkROCmFirmware`/`checkROCmHSA` into consumers of the new probed facts — future small follow-up.
</content>
