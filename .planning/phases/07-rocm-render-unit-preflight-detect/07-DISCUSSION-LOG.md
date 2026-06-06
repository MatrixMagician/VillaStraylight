# Phase 7: ROCm Render Unit + Preflight/Detect - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-06
**Phase:** 7-rocm-render-unit-preflight-detect
**Mode:** default (interactive)
**Areas discussed:** ROCm preflight shape, Version policy form, detect --json fields, Multi-device rendering (all 4 selected)

---

## Area selection

| Option | Description | Selected |
|--------|-------------|----------|
| ROCm preflight shape | PRE-06 verdict structure | ✓ |
| Version policy form | go:embed JSON vs Go constants | ✓ |
| detect --json fields | DET-04 readiness fields | ✓ |
| Multi-device rendering | ROCM-03 two --device passthroughs | ✓ |

**User's choice:** all four areas.

---

## ROCm preflight shape (PRE-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Extend existing pipeline | ROCm checks as TierBlock CheckResults via RunROCm(profile); reuses CheckResult/exit-code/remediation/D-15 downgrade | ✓ |
| Separate ROCm verdict type | Distinct ROCmPreflightVerdict; more isolation, duplicates tier/status/remediation machinery | |

**User's choice:** Extend existing pipeline → **D-01**.

### Secondary — preflight CLI surface

| Option | Description | Selected |
|--------|-------------|----------|
| CLI + switch-gate consumable | `villa preflight --backend rocm` renders the table off-hardware AND is the reusable Phase-8 gate function | ✓ |
| Reusable function only | Pure verdict + table tests, no CLI flag until Phase 8 | |

**User's choice:** CLI + switch-gate consumable → **D-03**.

### Secondary — unevaluable signal handling

| Option | Description | Selected |
|--------|-------------|----------|
| Downgrade to WARN | Missing/unparseable BLOCK signal → StatusWarn (D-15); only confident known-bad refuses | ✓ |
| Refuse on missing signal | Missing/unreadable signal → FAIL; over-blocks off-hardware hosts | |

**User's choice:** Downgrade to WARN → **D-02**.

---

## Version policy form (PRE-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Embedded JSON policy file | internal/preflight/rocm-policy.json via go:embed (ranges + firmware + image denylists + HSA value); migrate Vulkan floors in too | ✓ |
| Typed Go struct + denylist | Keep floors.go constants + denylist struct; defer go:embed | |

**User's choice:** Embedded JSON policy file → **D-04 / D-05** (migrate existing v1.0 floors into the same file, behavior no-op for existing checks).

---

## detect --json fields (DET-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Nested rocm_readiness object | Single nested object after GPU block; schema 1→2; room to grow; dashboard reads one sub-tree | ✓ |
| Flat appended fields | Individual top-level rocm_* fields; flatter but clutters top level | |

**User's choice:** Nested rocm_readiness object → **D-06 / D-07**.

### Secondary — readiness typing

| Option | Description | Selected |
|--------|-------------|----------|
| Typed-Optional (null, not false) | value.go Bool/Str; "couldn't detect" → null/absent, distinct from real false (D-16) | ✓ |
| Plain bool (false when unknown) | Simpler JSON, conflates "not ready" with "couldn't check" | |

**User's choice:** Typed-Optional → **D-08**.

---

## Multi-device rendering (ROCM-03)

| Option | Description | Selected |
|--------|-------------|----------|
| AddDevice slice + template range | containerView.AddDevice []string; parseContainerArgs collects ALL --device; template emits one AddDevice= per device; Vulkan 1-element slice renders identical line | ✓ |
| Keep single, join devices | Keep string + join; risks Quadlet AddDevice= one-per-key semantics + perturbs Vulkan render | |

**User's choice:** AddDevice slice + template range → **D-09** (Vulkan golden stays byte-identical as proof).

---

## Claude's Discretion

- Exact entrypoint/struct names (`RunROCm` vs `RunROCmWithPolicy`, policy loader/struct, `rocm_readiness` Go type name).
- Exact JSON key spellings inside `rocm_readiness` and the `rocm-policy.json` schema (must stay typed-Optional + carry ranges + both denylists).
- ROCm device order in the rendered unit (`kfd,dri` vs `dri,kfd`) — must match `backend_rocm.go` ContainerArgs order; frozen by the new golden.

## Deferred Ideas

- Phase 8: `villa backend set` switch verb + transactional rollback + live cutover (consumes Phase 7's preflight verdict + ROCm render).
- Phase 8: live ROCm offload / HSA-override / load_tensors-hang behavior on real gfx1151.
- Phase 9: `villa bench` honest A/B (pp/tg separate).
- Phase 10: backend-aware recommend + dashboard/status active-backend + tok/s + ROCm-readiness indicator (consumes the rocm_readiness object).
- Pre-existing v1.0 `checkMesaFloor` TODO (floors.go MesaFloor) — migrate the constant into the policy file but leave unwired; unrelated to ROCm.
