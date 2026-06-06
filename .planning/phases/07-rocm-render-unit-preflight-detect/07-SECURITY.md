---
phase: 07
slug: rocm-render-unit-preflight-detect
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-06
---

# Phase 07 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> **Audit type:** Threat-mitigation verification (GSD secure-phase, State B — authored from PLAN.md `<threat_model>` blocks, verified against implemented code).
> **Result:** SECURED — 7/7 threats resolved (5 mitigate CLOSED, 2 accept CLOSED). Implementation files read-only (never modified).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| rendered ROCm unit → Podman/systemd (runtime, Phase 8) | The ROCm unit passes `/dev/kfd`+`/dev/dri` into a rootless container and adds the `render` group — a privilege-surface delta over Vulkan. Phase 7 freezes the TEXT only; nothing is run. | device-passthrough + group spec (rendered text) |
| seam (`backend_rocm.go`) → renderer | The renderer reads device/group/env VALUES from the Phase-6 seam; it must not re-type them (grep-gate enforced). | container args / image (typed seam output) |
| build-time `go:embed` rocm-policy.json → binary | The policy is COMPILED INTO the binary, not loaded from a writable runtime path — not externally tamperable at runtime. A malformed embed is a build-time error. | floor/denylist policy data |
| `rocminfo`/host facts → RunROCm verdict / HostProfile | Tool output is already bounded (`maxToolOutput` 8 KiB) and parsed with fixed-arg exec in `gpu_amd.go` (reused, not re-rolled). | host capability facts |
| HostProfile `--json` → dashboard / Phase-10 recommend | Schema bump (1→2) lets consumers detect the v1.1 contract; append-only freeze prevents a silent breaking reshape. | readiness fields (JSON) |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-07-01-RENDER | Information Disclosure / Elevation | `/dev/kfd`+`/dev/dri` passthrough + `render` group in the rendered ROCm unit | accept (informational) | Device/group/env owned solely by seam `internal/inference/backend_rocm.go:65-86`; `render.go:88` reads via `ContainerArgs`/`Image()`, never re-typed; `parseContainerArgs` (render.go:184-201) appends all tokens; ROCm golden renders exactly the seam output; `grep -rn label=disable` repo-wide returns NONE (deferred to Phase 8, not speculative). | closed |
| T-07-02-IMG | Tampering | floating/`rocm7-nightlies` image reintroducing the 64 GB cap | mitigate | `checkROCmImage` FAILs on `pol.ImageDeny` match (checks_rocm.go:173-191); denylist `["rocm7-nightlies"]` present in rocm-policy.json:7; test asserts FAIL vs PASS (checks_rocm_test.go:138-155); in-tree image digest-pinned (backend_rocm.go:26). | closed |
| T-07-02-DOS | Denial of Service (self-inflicted) | over-blocking a genuinely-working host | mitigate | Every `checkROCm*` returns `warn(...)` on `!Known`, `fail(...)` only on confident known-bad (checks_rocm.go:70-191); off-hardware zero-StatusFail guard `TestRunROCmOffHardwareNoFalseFail` (checks_rocm_test.go:160-175) + `TestRunROCmUsesEmbeddedPolicy` (201-211); CLI maps WARN→exit 2, never exit 1 (preflight.go:47-50). | closed |
| T-07-02-EMBED | Input Validation (V5) | malformed `rocm-policy.json` embed | mitigate | `//go:embed rocm-policy.json` compiled-in (floors.go:80-81); `loadROCmPolicy()` panics fail-closed on malformed JSON (floors.go:111-117); no runtime/external policy-read path exists (checks_rocm.go:39, floors.go:127). | closed |
| T-07-03-FALSEGREEN | Information Disclosure (false-green) | a readiness field reporting a real `false` when the signal was undetectable | mitigate | Every `rocm_readiness` field typed-Optional `Bool`; `UnknownBool(...)` when source `!Known`, `KnownBool(...)` only when Known (readiness_rocm.go:20-65); mixed-Known round-trip asserts Unknown≠Known (profile_test.go:26-74); golden shows `"known": false` distinct from real values (detect.golden.json:86-112). | closed |
| T-07-03-RESHAPE | Tampering | silent breaking reshape of the frozen v1.0 `--json` contract | mitigate | `rocm_readiness` appended after GPU block, `schema_version` stays last (profile.go:56,60); schema bumped 1→2; golden git diff `f47782e` purely additive — no key removed/renamed. | closed |
| T-07-SC | Tampering | npm/pip/cargo installs | accept | No package installs this phase; Go stdlib only (`embed`, `encoding/json`, `text/template`, `strings`); container digests pinned Phase 6. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-07-01 | T-07-01-RENDER | Rendering `/dev/kfd`+`/dev/dri` passthrough and the `render` group into the ROCm Quadlet unit is the documented, required ROCm-on-gfx1151 device surface, encoded in the Phase-6 seam and only RENDERED (as TEXT) in Phase 7 — nothing is run. The SELinux `--security-opt label=disable` decision is explicitly DEFERRED to Phase 8 (on-hardware AVC) and was verified NOT present. Informational for this phase. | gsd-security-auditor | 2026-06-06 |
| AR-07-SC | T-07-SC | No package installs this phase (stdlib only; container digests pinned in Phase 6). No legitimacy gate required. | gsd-security-auditor | 2026-06-06 |

*Accepted risks do not resurface in future audit runs.*

---

## Unregistered Flags

None. The three plan SUMMARYs contain no `## Threat Flags` section; their "Threat Model Coverage" / decision notes (including the two Rule-3 seam-grep-gate auto-fixes) all map to declared threat IDs. No new unmapped attack surface appeared during implementation.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-06 | 7 | 7 | 0 | gsd-security-auditor (secure-phase, State B) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-06
