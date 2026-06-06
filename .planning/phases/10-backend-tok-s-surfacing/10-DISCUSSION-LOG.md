# Phase 10: Backend + tok/s Surfacing - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-06
**Phase:** 10-backend-tok-s-surfacing
**Mode:** `--auto` (autonomous; recommended option selected on every choice)
**Areas discussed:** Active-backend single source of truth, Status offload correctness, Live tok/s in status, ROCm-readiness indicator, recommend ROCm advice, Schema-bump + golden re-freeze discipline

---

## Active-backend single source of truth (D-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Append Backend+Image to `status.Report` (mirror `backendShowEntry`); dashboard labels tok/s from the status poll | One SSOT for active-backend identity; status + `/api/status` both read it; no duplication into `metricsView` | ✓ |
| Add backend identity into `/api/metrics` `metricsView` | Couples backend identity to the tok/s panel; two sources of truth | |
| Recompute backend in the dashboard JS from config | Client-side guess; drifts from the resolved backend | |

**Auto-selected:** Append to `status.Report`, mirror `backendShowEntry{Backend,Image}`, source from `BackendFor(cfg.Backend).Name()/Image()`.
**Notes:** Keeps tok/s in metrics, identity in status; UI composes them. Matches `villa backend show` exactly.

---

## Status offload/residency correctness (D-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Key the offload verdict on the resolved backend's `ResidencyProof()` markers | ROCm install asserts `ROCm0`; Vulkan path byte-identical; research pins the exact `status.Deps` site | ✓ |
| Leave residency markers hardcoded Vulkan | Wrong verdict on a ROCm install — fails SC#1 | |

**Auto-selected:** Route residency markers through the resolved backend.
**Notes:** Endpoint already resolved in Phase 6; research confirms the residency-marker feed into `status.Deps`.

---

## Live tok/s in `villa status` (D-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse `internal/metrics.ScrapeMetrics`; add typed-optional tok/s to `Report`; honest idle/unavailable | Same collector the dashboard uses; never a fabricated 0 | ✓ |
| Write a new CLI-only metrics scraper | Duplicates dashboard-proven code | |
| Skip tok/s in the CLI (dashboard only) | Violates SC#1 ("`villa status` show … tok/s") | |

**Auto-selected:** Reuse `internal/metrics`, typed-optional, honest unknown.
**Notes:** New `Deps` seam member so `status_test.go` stubs it; mirror `metricsView` Available/Idle honesty.

---

## ROCm-readiness indicator (D-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Tri-state (ready/not-ready/unknown) folded from the existing detect `rocm_readiness` sub-tree | Consume, never recompute; typed-Unknown → unknown (no false-green) | ✓ |
| Recompute readiness in status | Duplicates Phase-7 logic; drift risk | |
| Boolean ready/not-ready | Loses the honest "unknown" off-hardware state | |

**Auto-selected:** Tri-state, fold the Phase-7 sub-tree.
**Notes:** Off-hardware most fields are unset → indicator is honestly `unknown`.

---

## recommend ROCm advice (D-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Typed `ROCmAdvice` enum + Note, derived purely from `HostProfile.rocm_readiness`; Vulkan stays the pick; never auto-switch; never promise a speed-up | Honest, bench-pointing; pure `Pick`; honors pp-weighted/tg-flat reality | ✓ |
| Auto-switch `Backend` to rocm when ready | Forbidden by REC-05 — never auto-switch | |
| Free-text advice only (no typed field) | Harder for the dashboard/JSON consumers to branch on | |

**Auto-selected:** Typed enum + Note, Vulkan default unchanged.
**Notes:** Advice MUST say "verify with `villa bench`"; MUST NOT promise a guaranteed speed-up.

---

## Schema-bump + golden re-freeze discipline (D-06, D-07)

| Option | Description | Selected |
|--------|-------------|----------|
| Append-only tail fields + re-freeze each golden once + explicit additive `schema_version` on status/recommend; detect unchanged (stays v2) | Satisfies SC#3 "bumped schema version" literally; reviewed pure-addition diffs | ✓ |
| Golden re-freeze only, no version field | Lighter, but SC#3 names a version bump | |
| Reorder/retag for cleanliness | Forbidden — breaks the frozen contract | |

**Auto-selected:** Append-only + re-freeze once + additive `schema_version`; detect stays at Phase-7 v2 (no new detect fields).
**Notes:** Flagged the golden-only fallback for research to weigh if a version field on a previously-unversioned contract is judged heavier than the golden guard.

---

## Claude's Discretion

- Exact Go field names / json key spellings for the new `status.Report` and
  `Recommendation` additions and the `ROCmAdvice` enum spelling.
- Dashboard CSS/markup for the backend label, image tag, and readiness badge
  (match the existing panel idiom; no framework).
- Whether the CLI human table shows the image tag inline or only under `-v`.
- The worst-wins fold order for the tri-state readiness indicator (provided
  unknown never masquerades as not-ready).

## Deferred Ideas

- `villa bench --compare` + saved report artifact — BENCH-03 (v1.1.x backlog).
- `rocm-6.4.4` alternate image — ROCM-ALT-01 (v1.1.x backlog).
- Cumulative token/throughput usage tracking — USAGE-01 (v1.1.x).
- Auto-switching to ROCm based on advice — explicitly forbidden (REC-05).
- Real on-hardware detect probes for `firmware_date_ok`/`hsa_override_viable` —
  separate detect-hardening effort, not Phase 10.
