# Phase 6: ROCm Backend + Resolver Spine - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-05
**Phase:** 6-rocm-backend-resolver-spine
**Mode:** `--auto` (gray areas auto-selected; recommended option taken for each)
**Areas discussed:** Resolver shape, Residency interface extension, Off-hardware build boundary, ROCm image/args, Grep-gate strategy

---

## Resolver (`BackendFor`) — placement, signature, unknown-backend handling

| Option | Description | Selected |
|--------|-------------|----------|
| `internal/inference`, `BackendFor(name)(Backend,error)`, fail-closed on unknown | Resolver in the seam package; unrecognized backend returns an actionable error; empty → vulkan default | ✓ |
| Silent Vulkan fallback on unknown | Unknown backend silently resolves to Vulkan | |
| Resolver in `cmd/villa` | Map config→backend at the command layer | |

**Auto-choice:** internal/inference + fail-closed `(Backend, error)`.
**Notes:** Fail-closed matches the project's "no false-green" posture; a silent
fallback would mask a misconfigured ROCm install. Resolver in the seam keeps all
backend literals behind the D-03 boundary. All 7 call sites route through it.

---

## Residency proof — making the offload-assert backend-specific

| Option | Description | Selected |
|--------|-------------|----------|
| Extend `Backend` with `ResidencyProof()` descriptor; parameterize the scraper | Each backend owns its marker literals; `running_offload.go` reads the descriptor | ✓ |
| Separate per-backend offload files | Duplicate the scrape/verdict logic per backend | |
| Pass markers from the cmd layer | Caller supplies Vulkan0/ROCm0 strings | |

**Auto-choice:** `ResidencyProof()` interface extension + parameterized scraper.
**Notes:** Keeps `ROCm0`/`Vulkan0` literals behind the seam; reuses `combineOffload`
+ typed-Unknown discipline verbatim. Vulkan must stay byte-identical post-refactor.

---

## Off-hardware build boundary — how much of the ROCm assert to build in Phase 6

| Option | Description | Selected |
|--------|-------------|----------|
| Pure parse/verdict + fixtures + grep-gate now; live signals exercised Phase 8 | Build/test the logic off-hardware with synthetic ROCm0 fixtures | ✓ |
| Defer the whole assert to Phase 8 | Only stub the ROCm backend now | |
| Attempt live validation in Phase 6 | Requires real gfx1151 hardware | |

**Auto-choice:** Pure logic + fixtures + grep-gate now; live decode/fault signals
validated on hardware in Phase 8.
**Notes:** Mirrors `running_offload.go`'s pure/fixture-tested design; respects the
ROADMAP "off-hardware" flag on Phase 6.

---

## ROCm image + container args (`backend_rocm.go`)

| Option | Description | Selected |
|--------|-------------|----------|
| Digest-pinned `rocm-7.2.4` constant (podman pull); TODO-digest guard if unresolved | Mirror `vulkanImage`; never nightlies; kfd+dri+render+HSA env | ✓ |
| Tag-only pin | `:rocm-7.2.4` without a digest | |
| `rocm7-nightlies` | Rejected — 64 GB allocation-cap bug | |

**Auto-choice:** Digest-pinned constant resolved via `podman pull`; guard test fails
until a real `sha256:` digest is present (resolved before Phase 7 golden freeze).
**Notes:** Container args delta = `/dev/kfd` + `/dev/dri`, render group, ordered
`HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1`, keep `-ngl 999 -fa 1
--no-mmap` + loopback publish. Rendered-unit golden is Phase 7.

---

## Grep-gate strategy for ROCm markers

| Option | Description | Selected |
|--------|-------------|----------|
| Positive-presence gate + extend negative seam gate to rocm image token | Assert ROCm0/HIP markers exist in backend_rocm.go AND don't leak elsewhere | ✓ |
| Positive-presence gate only | Only assert markers exist | |
| Reuse existing negative gate unchanged | No ROCm-specific protection | |

**Auto-choice:** Add a positive-presence gate (SC#3) + extend `TestSeamGrepGate`'s
image-literal pattern to keep the rocm image token seam-bound.

---

## Claude's Discretion

- Exact `ResidencyProof()` descriptor struct shape and scraper factoring (provided
  Vulkan stays byte-identical and markers stay behind the seam).
- Error type/wording for the fail-closed unknown-backend path (must be actionable
  and routed through each call site's existing error handling).

## Deferred Ideas

- ROCm Quadlet render + byte-golden + ROCm preflight/detect → Phase 7.
- `villa backend set` switch verb + rollback → Phase 8 (on-hardware).
- `villa bench` honest A/B → Phase 9.
- Backend-aware `recommend` + dashboard/`status` surfacing → Phase 10.
- Live `gpu_busy_percent`-during-decode + fault-scan exercise → Phase 8 (on-hardware).
