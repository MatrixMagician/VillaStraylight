---
phase: 12
slug: rocm-6-4-4-alternate-backend
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-07
---

# Phase 12 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register authored at plan time (all three PLAN.md files carried `<threat_model>`
> blocks). Verified from artifacts + code + the on-hardware UAT (2026-06-07).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| upstream registry → inference seam | A rolling tag (`rocm-6.4.4`) can be re-pushed to a different image; only the `@sha256:` digest is trusted. | Container image manifest / digest |
| hand-edited `config.toml` → `BackendFor` | The `backend` string is untrusted user input; an unknown value must not reach the privileged `/dev/kfd` + `/dev/dri` device path. | Backend name (config value) |
| `--ab-target` flag → bench core | The explicit A/B flip target is untrusted CLI input; an unknown value must be rejected fail-closed before any live switch. | Backend name (CLI arg) |
| inference seam → rest of codebase | Image/marker literals must not leak past `internal/inference` (single-polymorphism guarantee). | Image tags, device args, residency markers |
| resolved target image → preflight policy gate | The gate must evaluate the ACTUAL resolved digest against `imageDeny`, not an empty string. | Resolved image digest |
| live ROCm switch → residency proof | A health-200 / is-active is NOT success; only an offload-asserting residency proof over real generation counts. | Offload/residency signal |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-12-01 | Tampering | rolling tag `rocm-6.4.4` re-pushed | mitigate | `@sha256:` digest pins (`backend_rocm.go:35-37`); skopeo re-verify before pinning (12-01 Task 1) + on-hardware re-verify (12-03). | closed |
| T-12-02 | Tampering / Repudiation | new image literal leaking outside the seam | mitigate | `TestSeamGrepGate` image regex extended same-commit (`seam_test.go:54`); gate green over `internal/` + `cmd/villa`. | closed |
| T-12-03 | Elevation of Privilege | untrusted `config.toml` backend value selecting an unintended device path | mitigate | Fail-closed `BackendFor` default returns actionable error (`backend.go:34-37`); only the 3 explicit ROCm cases reach kfd/dri. | closed |
| T-12-04 | Spoofing (false-green) | preflight gate skipped for a new ROCm name | mitigate | `IsROCmFamily` routes the gate for ALL ROCm-family backends (`preflight.go:50`, `backend.go:403`). | closed |
| T-12-05 | Tampering | denied image passing the gate unevaluated | mitigate | `RunROCmForImage` threads the resolved digest into the policy gate (`checks_rocm.go:48-50`) — no empty-image WARN bypass. | closed |
| T-12-06 | Repudiation | misleading "Vulkan RADV" Description / "unknown image" readiness on a ROCm unit | mitigate | Widened `backendLabel` + `rocmImagePolicyOK`; honest 6.4.4 label proven via additive render golden (12-02). | closed |
| T-12-07 | Elevation of Privilege | unknown `--ab-target` reaching the live switch | mitigate | Fail-closed `inference.BackendFor(abTarget)` validation in the cmd tier BEFORE Deps construction (12-03 Task 2). On-hardware: `--ab-target rocm-7.2.4` fail-closed rejected, zero side effects. | closed |
| T-12-08 | Spoofing (false-green) | silent/partial CPU fallback masquerading as a healthy switch | mitigate | Offload-asserting `liveProve`/`RunningOffloadVerdict` over `ResidencyProof()` markers. On-hardware: `-rocwmma` residency FAILed honestly + rolled back verbatim — a real FAIL, never a false-green. | closed |
| T-12-09 | Tampering | rolling-tag drift between pin (12-01) and on-hardware run | mitigate | On-hardware checkpoint re-runs `skopeo inspect`. Observed live: `rocm-6.4.4` tag moved to `sha256:44f115e0…` same day; the pinned `c81f30a7…` digest still resolved & pulled (content-addressed). | closed |
| T-12-SC | Tampering | container image pulls (no npm/pip/cargo) | accept | No package-manager installs; images are the already-audited kyuz0 repo (same provenance as v1.1), digest-pinned. See Accepted Risks Log. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-12-01 | T-12-SC | Supply-chain trust in the `kyuz0/amd-strix-halo-toolboxes` images is inherited from v1.1 (same provenance, RESEARCH Package Legitimacy Audit flagged no packages). No first-party package-manager installs are added; all three new images are `@sha256:` digest-pinned, so a re-pushed rolling tag cannot substitute content. | O Hingst | 2026-06-07 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-07 | 10 | 10 | 0 | /gsd-secure-phase (State B, from-artifacts; register authored at plan time) |

Verification basis:
- Static mitigations confirmed in code (digest pins, fail-closed `BackendFor` default, `IsROCmFamily` routing, `RunROCmForImage` digest threading, seam regex) with `TestSeamGrepGate` + `TestROCmMarkerPresence` green.
- Runtime mitigations (T-12-08 offload-assertion, T-12-09 tag-drift, T-12-07 fail-closed flip) observed live in the on-hardware UAT on the gfx1151 host (2026-06-07), recorded in 12-03-SUMMARY.md and 12-UAT.md.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-07
