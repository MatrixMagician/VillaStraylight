---
phase: 12-rocm-6-4-4-alternate-backend
plan: 01
subsystem: inference
tags: [rocm, backend-resolver, inference-seam, digest-pin, seam-grep-gate]
requires:
  - "internal/inference/BackendFor (existing single polymorphism point)"
  - "internal/inference/backendROCm delta (v1.1 ROCm machinery)"
provides:
  - "BackendFor(\"rocm-6.4.4\") → digest-pinned ROCm 6.4.4 backend"
  - "BackendFor(\"rocm-6.4.4-rocwmma\") → digest-pinned ROCm 6.4.4 rocWMMA backend"
  - "inference.IsROCmFamily(name) — single ROCm-name enumeration (consumed by 12-02)"
  - "image-parameterized backendROCm{name,image} struct"
affects:
  - "12-02 (family predicate routing: PreflightROCm, preflight flag router, render label)"
  - "12-03 (bench --ab against the new backends)"
tech-stack:
  added: []
  patterns:
    - "Fail-closed resolver: new cases are explicit additions; default still errors"
    - "Digest-pin never tag: 3 @sha256 consts; rolling tag re-verified before pinning"
    - "Seam-lock same-commit rule: image regex extended in the SAME commit as the literal"
    - "Image-parameterized backend (one struct, three digests) over per-digest forks"
key-files:
  created:
    - "internal/inference/backend_test.go (TestIsROCmFamily)"
  modified:
    - "internal/inference/backend_rocm.go (3 digest consts + name/image fields + named receivers)"
    - "internal/inference/backend.go (2 new BackendFor cases + widened error + IsROCmFamily)"
    - "internal/inference/seam_test.go (container-image-literal regex + rocm-6\\.4\\.4)"
    - "internal/inference/backend_rocm_test.go (extended TestBackendFor)"
    - "CLAUDE.md (2 new image rows)"
decisions:
  - "Extended existing TestBackendFor in backend_rocm_test.go rather than duplicating a resolver test; put TestIsROCmFamily in new backend_test.go per plan artifact spec"
  - "Gave ResidencyProof/ContainerArgs named receiver b for consistency; markers byte-unchanged (D-06, not parameterized)"
  - "Reverted out-of-scope gofmt drift in validate.go/validate_test.go (logged to deferred-items.md)"
metrics:
  duration: "~15 min"
  completed: "2026-06-07"
  tasks: 3
  files_changed: 6
---

# Phase 12 Plan 01: rocm-6.4.4 Alternate Backend (seam + resolver + digests) Summary

Added two additive, digest-pinned, fail-closed, seam-locked ROCm backends
(`rocm-6.4.4` TG-tuned + `rocm-6.4.4-rocwmma`) by parameterizing the proven v1.1
`backendROCm` delta by image, plus the single `inference.IsROCmFamily` predicate —
all green off-hardware via `make check`, with the seam image regex extended in the
same commit so the new literal cannot leak.

## What Was Built

- **Image-parameterized `backendROCm`** (`backend_rocm.go`): replaced the single
  `const rocmImage` with a 3-const block (`rocmImage724` unchanged, `rocmImage644`,
  `rocmImage644wmma` — all `@sha256` digest-pinned). The empty `struct{}` gained
  `name`/`image` fields; `Name()`/`Image()`/`ContainerArgs()`/`ResidencyProof()` now
  use named receiver `b`; `ContainerArgs` uses `b.image`. Every other arg (kfd+dri
  passthrough, keep-groups-only, seccomp, loopback publish, RO model bind, mandatory
  llama-server flags, ordered `HSA_OVERRIDE_GFX_VERSION=11.5.1` → `ROCBLAS_USE_HIPBLASLT=1`)
  and all ResidencyProof markers are byte-identical for the three variants (D-06/A2).
- **Resolver** (`backend.go`): two new fail-closed `BackendFor` cases; `rocm` still
  resolves to the unchanged 7.2.4 digest (`2da150c1…531a89`, D-02). The `default`
  branch still returns `(nil, error)` and the widened message names all four options
  (D-03). Added exported `IsROCmFamily(name)` — the single ROCm-name enumeration (D-08),
  holding only config-value names (seam-clean).
- **Seam grep-gate** (`seam_test.go`): extended the `container image literal` regex with
  `rocm-6\.4\.4` (covers both the plain tag and the `-rocwmma` suffix superset) in the
  SAME commit as the literal (D-10/SC#4/T-12-02). `cmdPatterns` reuses it by reference —
  one edit covers both the `internal/` and `cmd/villa` walks.
- **Tests**: extended `TestBackendFor` (two new digests, the unchanged 7.2.4 digest,
  widened fail-closed error naming all four options, `@sha256:` 64-hex pin assert) and
  added `TestIsROCmFamily` (true for the three ROCm names; false for ``/`vulkan`/`bogus`/`ROCM`).
- **Docs** (`CLAUDE.md`): two new rows in "Container Images Standardized On" with
  abbreviated digests (canonical literal stays in `backend_rocm.go` — no seam leak).

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Re-verify both rolling-tag digests before pinning | (no source — read-only check) | — |
| 2 | Parameterize backendROCm + 2 fail-closed BackendFor cases + IsROCmFamily + seam regex | 65fe6a5 | backend_rocm.go, backend.go, backend_rocm_test.go, backend_test.go, seam_test.go |
| 3 | Add 2 rows to CLAUDE.md image table | 6d179ad | CLAUDE.md |

## Digest Re-Verification (Task 1)

Both rolling-tag digests were re-confirmed **live** via `skopeo inspect --no-tags`
on 2026-06-07 against the pinned values — result **DIGESTS_MATCH**:

- `rocm-6.4.4` top-level manifest → `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62` ✓ (matches pin)
- `rocm-6.4.4-rocwmma` top-level manifest → `sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141` ✓ (matches pin)

skopeo and network were available in this environment, so the documented off-grid
fallback was not needed. The authoritative pre-live-switch re-verify remains the
on-hardware checkpoint in 12-03 Step 1 (the rolling tag can move again before then).

## Verification Results

- `go test ./internal/inference/ -run 'TestBackendFor|TestIsROCmFamily|TestSeamGrepGate|TestROCmMarkerPresence' -count=1` → 13 pass, exit 0.
- `go test ./internal/inference/ -count=1` → 76 pass.
- `make check` (`go vet ./...` + `go test ./...`) → all packages OK, exit 0 (no caller broke from the struct-shape change).
- Image-literal seam-lock: `grep -rln "rocm-6.4.4@sha256\|rocm-6.4.4-rocwmma@sha256" --include="*.go" .` → only `internal/inference/backend_rocm.go`.
- `BackendFor("rocm-6.4.4").Image()` contains `sha256:c81f30a7`; `…-rocwmma` contains `sha256:9a97129a`; `rocm` contains `2da150c1`; `BackendFor("bogus")` → non-nil error + nil Backend.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Out-of-scope gofmt drift reverted**
- **Found during:** Task 2 (running `go fmt ./internal/inference/`).
- **Issue:** `go fmt` over the package reformatted two PRE-EXISTING files not in this
  plan's scope — `internal/inference/validate.go` (doc-comment bullet) and
  `validate_test.go` (one-line method bodies). This is pre-existing drift, not caused
  by this task.
- **Fix:** `git checkout -- internal/inference/validate.go internal/inference/validate_test.go`
  to keep the 12-01 commits scoped strictly to plan files; logged to
  `deferred-items.md` for a standalone `make fmt` cleanup commit.
- **Files modified:** none committed (reverted).
- **Commit:** n/a.

### Plan-vs-reality note (not a behavior deviation)

- The plan's `<artifacts_this_phase_produces>` described `backend_test.go` as possibly
  holding the extended resolver assertions; the existing resolver test
  (`TestBackendFor`) actually lives in `backend_rocm_test.go`. Extended it there and
  reserved the NEW `backend_test.go` for `TestIsROCmFamily` (the resolver-adjacent
  predicate that does not belong to a single backend impl). Both plan artifacts
  (`internal/inference/backend_test.go` with `TestIsROCmFamily`; extended `TestBackendFor`)
  are satisfied.

## TDD Gate Compliance

Task 2 was `tdd="true"`. RED→GREEN was followed within a single atomic commit (the
seam image-literal and its regex guard MUST land together per D-10/SC#4, so RED and
GREEN could not be split into separate commits without leaving an intermediate state
that violates the same-commit seam rule):
- RED: added `TestIsROCmFamily` + extended `TestBackendFor` → `go test` failed with
  `undefined: IsROCmFamily` (build fail) before implementation.
- GREEN: implemented the consts/struct/cases/predicate/regex → all 13 targeted tests pass.
- No separate REFACTOR step was needed.

## Authentication Gates

None.

## Known Stubs

None. Both new backends fully resolve to their pinned digests; no placeholder/empty values.

## Threat Flags

None. No new network endpoints, auth paths, or trust-boundary surface beyond the
already-modeled rolling-tag (T-12-01, mitigated by digest-pin + Task 1 re-verify) and
seam-leak (T-12-02, mitigated by the same-commit regex). The new image pull is the same
provenance/class as the existing v1.1 kyuz0 images (T-12-SC accepted).

## Self-Check: PASSED

- FOUND: internal/inference/backend_test.go
- FOUND: internal/inference/backend.go (IsROCmFamily, 2 new cases)
- FOUND: internal/inference/backend_rocm.go (rocmImage644 ×4 refs)
- FOUND: CLAUDE.md (2 rocm-6.4.4 rows)
- FOUND commit: 65fe6a5 (feat 12-01 seam core)
- FOUND commit: 6d179ad (docs 12-01 CLAUDE.md rows)
