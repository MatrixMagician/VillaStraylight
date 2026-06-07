# Phase 12: `rocm-6.4.4` Alternate Backend - Research

**Researched:** 2026-06-07
**Domain:** Go inference-backend seam extension (digest-pinned container backend, fail-closed resolver, transactional switch, honest A/B bench, grep-gated seam)
**Confidence:** HIGH

## Summary

Phase 12 is a **pure delta over shipped v1.1 machinery**. The `BackendFor` resolver, `Backend` interface, `backendswap` transactional switch, `bench --ab` core, `RunROCm` preflight gate, and the `TestSeamGrepGate`/`TestROCmMarkerPresence` grep-gates all already exist and were built explicitly to admit a new backend as a sibling. Adding `rocm-6.4.4` (+ `-rocwmma`) is overwhelmingly an **additive** change: two new digest-pinned image literals behind the seam, two new `BackendFor` cases, one generalized "is-ROCm-family" predicate to replace the three `== "rocm"` literal comparisons in callers, the `seam_test.go` image regex extended in the same commit, and a small set of `switch name` maps (`backendLabel`, `rocmImagePolicyOK`) widened so the new images are recognized rather than falling to a default/Unknown branch.

The **one genuine design risk** is `internal/bench/bench.go`'s `other()` function: it is a hardcoded 2-value swap (vulkan ↔ rocm). With three+ ROCm-family backends, `villa bench --ab` cannot today select an *arbitrary* pair (e.g. `rocm-6.4.4` vs `rocm-7.2.4`) — it only flips between the current config backend and its single "other". SC#3 ("measures the new image against rocm-7.2.4 / Vulkan") requires the user to drive the comparison by manually setting one side as the active backend and `--ab`-flipping to "the other", which the 2-value `other()` cannot express for a 3-ROCm world. This is the only call site that needs more than a mechanical widen — the planner must decide whether to (a) add a `--ab-target <backend>` flag, or (b) document the v1.1 limitation and ship the additive backends, deferring multi-backend A/B selection. Both digests were **re-verified live via skopeo on the dev host 2026-06-07** and match CONTEXT.md byte-for-byte.

**Primary recommendation:** Parameterize one `backendROCm{image, markers}` (D-06 — lowest churn), add two `BackendFor` cases, introduce `inference.IsROCmFamily(name)` as the single predicate, widen `backendLabel`/`rocmImagePolicyOK` switch maps, extend the `seam_test.go` image regex + thread a `requestedImage` into the live `PreflightROCm` so the policy gate evaluates the *actual* target image. Decompose into 3 plans mirroring the v1.1 spine→gate→surface order.

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** New image selected via **distinct `BackendFor` backend strings** — `rocm-6.4.4` (and `rocm-6.4.4-rocwmma`) as new `case`s in `internal/inference/backend.go`. NOT a sub-flag/`--image` variant of `rocm`.
- **D-02:** **Coexist** with existing `rocm` backend — `rocm` keeps meaning ROCm **7.2.4** (unchanged digest). New strings are additive (required by SC#3: `bench --ab` must measure against rocm-7.2.4).
- **D-03:** Fail-closed resolver semantics preserved — unknown/typo'd backend string still returns the actionable `BackendFor` error, never a silent fallback. New cases are explicit additions only.
- **D-04:** **Ship BOTH** `rocm-6.4.4` and `rocm-6.4.4-rocwmma` as selectable, digest-pinned, seam-locked, policy-gated backends. `bench --ab` proves which recovers Δtg at runtime.
- **D-05:** Both digests pinned from the ROADMAP research flag and **re-verified at implementation time**:
  - plain: `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62`
  - `-rocwmma`: `sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141`
- **D-06:** **Parameterize the proven `backendROCm` delta by image** rather than forking a type per digest. The kfd+dri passthrough, keep-groups rule, seccomp, loopback host-publish, read-only model bind, mandatory llama-server flags, and `ResidencyProof` markers are shared ROCm-family behaviour — only the image digest differs. Confirm whether 6.4.4 needs a different `HSA_OVERRIDE_GFX_VERSION` / `ROCBLAS_USE_HIPBLASLT` (default assumption: identical `11.5.1` + hipBLASLt=1). **[RESEARCH CONFIRMS: keep `11.5.1` + hipBLASLt=1 — see §Code Examples and §Common Pitfalls.]**
- **D-07:** Keep `rocm-policy.json` as deny-list + floors. Pin new digests in the seam; add an explicit allow/deny entry only if research shows the rolling tag needs it. **[RESEARCH: no allow-list needed — `imageDeny` is a substring denylist; `rocm-6.4.4` is not denied. But see §Common Pitfalls Pitfall 3: `rocmImagePolicyOK` in detect must be widened or `rocm_readiness.image_policy_ok` reads Unknown for a 6.4.4 host.]** A floor failure is refused with named remediation, never silently downgraded (SC#2).
- **D-08:** **Generalize the ROCm-preflight predicate.** `PreflightROCm` in `cmd/villa/backend.go` gates on `cfg.Backend != "rocm"` (exact string); it must fire for ALL ROCm-family backends. Introduce a single "is this a ROCm backend" predicate (likely in `internal/inference`) and route the preflight + any other `== "rocm"` checks through it. Audit other literal `"rocm"` comparisons.
- **D-09:** **No new surfacing logic.** `status`/dashboard already display active backend + image tag via `BackendFor(cfg.Backend).Image()`. `recommend` advice stays generic. Confirm goldens only need an append-only refresh if a new enum value appears; otherwise leave frozen contracts untouched. **[RESEARCH: no golden change required — see §Golden Impact.]**
- **D-10:** **Extend `internal/inference/seam_test.go`'s image regex in the SAME commit** that introduces the new image literal(s). Add `rocm-6\.4\.4` to the "container image literal" pattern (and the `cmdPatterns` copy). Keep `TestROCmMarkerPresence` green for the parameterized markers.

### Claude's Discretion

- Exact resolver string naming for the rocwmma variant (`rocm-6.4.4-rocwmma` assumed; planner may shorten if it keeps `villa backend set` UX clean and the seam regex tight).
- Whether the ROCm-family predicate lives in `internal/inference` (preferred) vs a small helper in `config` — must not leak image literals out of the seam.
- Whether the shared ROCm delta is refactored into one image-parameterized `backendROCm{image, markers}` struct or kept as thin sibling structs — pick the lowest-churn option that keeps the seam grep-gate green.

### Deferred Ideas (OUT OF SCOPE)

- **ROCm perf-tuning knobs** (hipBLASLt / rocWMMA-FA / batch tunables) — `ROCM-TUNE-01`, deferred beyond v1.2. Phase 12 ships the `-rocwmma` *image* as a selectable backend, NOT tunable flags.
- **Per-image `recommend` advice** — `recommend` stays image-agnostic to preserve the honesty constraint.
- **Retiring rocm-7.2.4** — not this phase; 7.2.4 must stay as the A/B baseline.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ROCM-ALT-01 | User can opt into a digest-pinned `rocm-6.4.4` (or `-rocwmma`) ROCm image as an alternate backend via `villa backend set`, gated by `rocm-policy.json` floors and kept behind the `internal/inference` seam (incl. extending `seam_test.go`) — addressing the v1.1 Δtg −11.15 regression. Never auto-switches; Vulkan stays default. | §Architecture Patterns (resolver + parameterized delta), §Don't Hand-Roll (reuse backendswap/bench/RunROCm), §Common Pitfalls (image regex same-commit, `other()` 2-value swap, detect `rocmImagePolicyOK` widen), §Code Examples (ContainerArgs delta, BackendFor cases, IsROCmFamily). Digests re-verified live (§Environment Availability). |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| New image digest literal(s) | `internal/inference` (seam) | — | SC#4 + CLAUDE.md: ALL backend image literals live behind the seam; grep-gate enforces it. |
| Backend-string → impl resolution | `internal/inference` (`BackendFor`) | — | D-01/D-03: single polymorphism point, fail-closed. |
| ROCm-family predicate | `internal/inference` (preferred) | — | D-08 discretion: keeps backend knowledge behind the seam; callers ask a function, never compare a literal. |
| ROCm preflight gate (floors/denylist/HSA) | `internal/preflight` (`RunROCm`) | `cmd/villa/backend.go` (live wiring) | SC#2: reusable refuse-with-remediation gate; the cmd tier only decides *whether* to run it (now via the family predicate). |
| Image-policy recognition for readiness | `internal/detect/gpu_amd.go` (seam) | `internal/detect/readiness_rocm.go` | `rocm_readiness.image_policy_ok` literal lives behind the detect seam (the only non-inference seam file). |
| Transactional switch + residency proof | `internal/backendswap` | `cmd/villa/backend.go` (`liveProve`) | Reused unchanged — `backendswap.Run` already works for any `Backend`. |
| Honest A/B bench | `internal/bench` | `cmd/villa/bench.go` | Reused; `other()` 2-value swap is the ONE risk for 3-backend selection (§Common Pitfalls Pitfall 4). |
| `--json`/dashboard surfacing | `internal/status` + `internal/dashboard` | — | D-09: auto-reflects via `BackendFor(cfg.Backend).Image()` — no change. |

## Standard Stack

No new external dependencies. This phase is internal Go refactoring/extension over the existing module.

### Core (existing, reused)
| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|--------------|
| `internal/inference` | in-repo | Backend seam: `BackendFor`, `Backend` iface, `backend_rocm.go`, `seam_test.go` | The single polymorphism point; built to admit new backends as siblings (D-01). |
| `internal/backendswap` | in-repo | Transactional capture→prove→cutover→rollback | Reused as-is for SC#1. |
| `internal/bench` | in-repo | Honest A/B pp/tg core; `--ab` composes `backendswap.Run` | Reused for SC#3 (with the `other()` caveat). |
| `internal/preflight` | in-repo | `RunROCm` BLOCK/WARN gate over `rocm-policy.json` | Reused for SC#2; only the *trigger predicate* widens. |
| `github.com/spf13/cobra` | v1.10.2 | `villa backend set` cobra surface | No change — `Args: ExactArgs(1)` already accepts any string; `BackendFor` validates. |

**Installation:** None. `make build` / `make check` are the only commands.

### Tooling (host-side, for digest re-verification)
| Tool | Version (host) | Purpose |
|------|----------------|---------|
| `skopeo` | 1.22.2 | `skopeo inspect docker://…` — read-only digest resolution, NO pull. |
| `podman` | 5.8.2 | Fallback `podman manifest inspect` if skopeo absent. |

## Package Legitimacy Audit

> Not applicable — this phase installs NO external packages. The only "packages" are two **container images** from the already-audited `kyuz0/amd-strix-halo-toolboxes` repo (same provenance as the shipped v1.1 ROCm/Vulkan images). Both digests were re-verified live on the dev host (see §Environment Availability).

| Image | Registry | Digest re-verified (2026-06-07) | Matches CONTEXT.md | Disposition |
|-------|----------|----------------------------------|--------------------|-------------|
| `kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4` | docker.io | `sha256:c81f30a7…f150ec62` | ✅ exact | Approved — pin digest |
| `kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma` | docker.io | `sha256:9a97129a…43c0141` | ✅ exact | Approved — pin digest |

**No packages removed or flagged.** Image provenance is the same audited kyuz0 repo as the v1.1 Vulkan/ROCm images.

## Architecture Patterns

### System Architecture Diagram

```
                       config.toml  (single source of truth)
                            │  backend = "rocm-6.4.4" | "rocm-6.4.4-rocwmma" | "rocm" | "vulkan"
                            ▼
              ┌──────────────────────────────────┐
              │  inference.BackendFor(name)        │  ← D-01/D-03: add 2 cases, stay fail-closed
              │  (SINGLE polymorphism point)       │
              └──────────────────────────────────┘
                            │ returns Backend
        ┌───────────────────┼────────────────────────────────┬──────────────────┐
        ▼                   ▼                                  ▼                  ▼
  backendVulkan      backendROCm{image, markers}        IsROCmFamily(name)   .Image()/.Name()
  (unchanged)        ├─ "rocm"           → rocm7.2.4 digest   │ (new predicate)  │ (surfacing)
                     ├─ "rocm-6.4.4"     → 6.4.4 digest       │                  ▼
                     └─ "rocm-6.4.4-rocwmma" → rocwmma digest │            status / dashboard
                       (shared ContainerArgs + ResidencyProof)│            (D-09: auto, no change)
                            │                                 │
                            ▼                                 ▼
                  ┌──────────────────┐            cmd/villa/backend.go PreflightROCm
                  │ backendswap.Run  │  ←─────────  if IsROCmFamily(cfg.Backend):
                  │ capture→prove→   │              preflight.RunROCm(probe, target.Image())
                  │ cutover→rollback │                        │
                  └──────────────────┘                        ▼
                            │                       internal/preflight.RunROCm
                            ▼                       (floors + imageDeny + HSA — SC#2)
                  liveProve: gen-probe +
                  RunningOffloadVerdict
                  (markers from ResidencyProof)
                            │
                            ▼
                  villa bench --ab  ── other(orig) ──► ⚠ 2-value swap (Pitfall 4)
```

### Recommended Project Structure (files touched — all additive/widening)
```
internal/inference/
├── backend.go              # +2 BackendFor cases; +IsROCmFamily(name) predicate
├── backend_rocm.go         # parameterize: rocmImage const → per-backend image field (D-06)
└── seam_test.go            # extend image regex + cmdPatterns (SAME commit, D-10)
internal/detect/
└── gpu_amd.go              # widen rocmImagePolicyOK switch to recognize rocm-6.4.4 (Pitfall 3)
internal/orchestrate/
└── render.go               # widen backendLabel switch for the new Name()s (Pitfall 2)
cmd/villa/
└── backend.go              # PreflightROCm: cfg.Backend != "rocm"  →  !IsROCmFamily(cfg.Backend)
                            #   + thread requestedImage into RunROCm (Pitfall 3)
CLAUDE.md                   # add the two new image rows to "Container Images Standardized On"
```

### Pattern 1: Image-parameterized single backend struct (D-06, recommended)
**What:** Replace `const rocmImage = "…7.2.4…"` + the empty `backendROCm struct{}` with a struct carrying the image (and, if ever needed, distinct markers). `BackendFor` constructs the three ROCm variants from one type.
**When to use:** When the only per-digest difference is the image string (confirmed for 6.4.4 — same HSA override, same hipBLASLt, same markers, same device args).
**Example:**
```go
// internal/inference/backend_rocm.go  (Source: existing backend_rocm.go, parameterized)
// Three digest-pinned kyuz0 Strix-Halo ROCm images. Tags are rolling; pin the @sha256
// digest (Pitfall 12 / T-6-04). Re-verified on the dev box 2026-06-07 via
// `skopeo inspect docker://docker.io/kyuz0/amd-strix-halo-toolboxes:<tag>`.
const (
    rocmImage724    = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89"
    rocmImage644    = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62"
    rocmImage644wmma = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141"
)

type backendROCm struct {
    name  string // "rocm" | "rocm-6.4.4" | "rocm-6.4.4-rocwmma"
    image string
}

func (b backendROCm) Name() string  { return b.name }
func (b backendROCm) Image() string { return b.image }
// ContainerArgs + ResidencyProof unchanged from the shipped delta — they reference
// b.image instead of the package const; HSA_OVERRIDE_GFX_VERSION=11.5.1 and
// ROCBLAS_USE_HIPBLASLT=1 stay identical for all three (research-confirmed for 6.4.4).
```
**Note:** `TestROCmMarkerPresence` reads `backend_rocm.go` for the literals `ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, `/dev/kfd` — all stay in this file, so it remains green with zero change.

### Pattern 2: The ROCm-family predicate (D-08)
**What:** A single exported `inference.IsROCmFamily(name string) bool` that every caller uses instead of `== "rocm"`. It is the ONLY place the set of ROCm backend names is enumerated.
**Where it lives:** `internal/inference` (discretion-preferred — keeps the backend taxonomy behind the seam). It carries no image literal (just the backend *name* strings, which are config VALUES, not imperatives — the same class as `bench.go`'s `vulkan`/`rocm` consts that already pass the grep-gate).
**Example:**
```go
// internal/inference/backend.go
// IsROCmFamily reports whether a config backend string selects a ROCm-family backend
// (any HIP image: 7.2.4 or the 6.4.4 TG-tuned variants). Callers use this instead of
// `== "rocm"` so a new ROCm digest is gated identically without editing the caller.
func IsROCmFamily(name string) bool {
    switch name {
    case "rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma":
        return true
    default:
        return false
    }
}
```
Then in `cmd/villa/backend.go`:
```go
PreflightROCm: func(cfg config.VillaConfig) (bool, string) {
    if !inference.IsROCmFamily(cfg.Backend) {   // was: cfg.Backend != "rocm"
        return true, ""
    }
    // thread the resolved target image into the policy gate (Pitfall 3 / SC#2):
    b, err := inference.BackendFor(cfg.Backend)
    if err != nil { return false, err.Error() }
    for _, c := range preflight.RunROCmForImage(detect.Probe(), b.Image()) { // see Pitfall 3
        if c.Status == preflight.StatusFail { return false, c.Detail }
    }
    return true, ""
},
```

### Anti-Patterns to Avoid
- **Forking a new `backendROCmXXX struct{}` per digest** — D-06 explicitly rejects this; it duplicates the entire ContainerArgs/ResidencyProof body three times and triples the grep-gate surface.
- **Adding `--image` as a sub-flag of `rocm`** — D-01 rejects this; the ROADMAP SC#1 literal is `villa backend set rocm-6.4.4`.
- **Comparing `cfg.Backend == "rocm"` anywhere new** — always route through `IsROCmFamily`. The audit below lists every existing literal.
- **Leaving the seam regex unextended** — D-10/SC#4: the regex MUST be extended in the same commit, or a leaked `rocm-6.4.4` literal would NOT fail CI (the new tag is not matched by the existing `rocm-7\.2\.4` alternation).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Transactional backend switch | New cutover/rollback logic | `backendswap.Run` (unchanged) | Already capture→prove→cutover→rollback for any `Backend`; SC#1 is satisfied by selecting the new string. |
| Residency proof for the new image | New offload-assert | `liveProve` + `RunningOffloadVerdict` fed `BackendFor(target).ResidencyProof()` | The dual-scrape is parameterized by markers; the new image reuses the identical ROCm markers. |
| A/B throughput delta | New bench loop | `bench.Run` / `--ab` | Honest pp/tg-separated A/B already composes `backendswap.Run`. (Caveat: `other()` selection — Pitfall 4.) |
| Floor/denylist/HSA gating | New policy checks | `preflight.RunROCm` over `rocm-policy.json` | Refuse-with-remediation gate already built; only the trigger predicate + the requested-image arg widen. |
| Version-floor comparison | New semver parser | `preflight.compareVersions` / detect `compareVersionSegments` | Already suffix-tolerant, fail-low-safe. |
| Image-tag → human label | Inline strings | `orchestrate.backendLabel` switch (widen) | Single label map; widen the `switch`, don't scatter labels. |

**Key insight:** Virtually every behavior this phase needs already exists and is backend-neutral by construction. The work is *recognizing the new names* in a handful of `switch` statements and *one predicate*, not building new mechanism.

## Backend-Family Refactor — Complete Call-Site Audit (D-08)

Every literal `"rocm"` comparison/branch in non-test Go, and its required action:

| File:Line | Current | Action | Notes |
|-----------|---------|--------|-------|
| `internal/inference/backend.go:25` | `case "rocm":` in `BackendFor` | **Add** `case "rocm-6.4.4":` + `case "rocm-6.4.4-rocwmma":` | The 3 cases construct `backendROCm{name, image}`. Default stays fail-closed (D-03); update the error message to name the new options. |
| `internal/inference/backend.go` | (new) | **Add** `IsROCmFamily(name)` | The single predicate (Pattern 2). |
| `internal/inference/backend_rocm.go:37` | `func (backendROCm) Name() string { return "rocm" }` | **Change** to return `b.name` | Part of the parameterization. |
| `cmd/villa/backend.go:402` | `if cfg.Backend != "rocm"` (PreflightROCm) | **Replace** with `if !inference.IsROCmFamily(cfg.Backend)` + thread target image | The load-bearing D-08 change. |
| `cmd/villa/preflight.go:47` | `if backend == "rocm"` (`--backend` flag router) | **Replace** with `if inference.IsROCmFamily(backend)` | So `villa preflight --backend rocm-6.4.4` routes to `RunROCm`. (Cheap UX win; verify the flag help text.) |
| `cmd/villa/inference.go:128` | comment only (`backend = "rocm"`) | No code change | Comment; optionally update for clarity. |
| `internal/orchestrate/render.go:59` | `case "rocm":` in `backendLabel` | **Widen** `switch` so 6.4.4 names get a ROCm label (e.g. `"ROCm 6.4.4 (HIP)"`); default stays "Vulkan RADV" | Label is unit Description= text, not an imperative — but a 6.4.4 backend should not silently render the "Vulkan RADV" label. **This changes the rendered `villa-llama.container` ONLY when on a 6.4.4 backend** — the Vulkan + rocm-7.2.4 goldens are unchanged. See §Golden Impact. |
| `internal/detect/gpu_amd.go:266,286` | `rocmStableImageTag = "rocm-7.2.4"` + `case strings.Contains(image, rocmStableImageTag)` | **Widen** `rocmImagePolicyOK` to also recognize `rocm-6.4.4` as a pinned-stable image | Otherwise `rocm_readiness.image_policy_ok` reads Unknown on a 6.4.4 host (Pitfall 3). Add a `rocm644ImageTag` const behind this detect seam. |
| `internal/bench/bench.go:174,185-188` | `other()` + `vulkan`/`rocm` consts | **Decide** (Pitfall 4) — 2-value swap is insufficient for 3 ROCm backends | Planner decision: `--ab-target` flag vs document limitation. |
| `internal/backendswap/backendswap.go:165` | comment referencing `cfg.Backend=="rocm"` | No code change | Comment about the injected `PreflightROCm` seam (which now widens). |

**Reusable assets that need NO change** (verified): `backendswap.Run` (works on any `Backend`), `liveProve`/`RunningOffloadVerdict` (markers via `ResidencyProof()`), `inference.Validate`, `status`/`dashboard` surfacing (D-09), `config.VillaConfig` (the `backend` field is a free string already validated by `BackendFor`).

## Common Pitfalls

### Pitfall 1: Seam regex not extended in the same commit (SC#4 / D-10)
**What goes wrong:** You add `const rocmImage644 = "…rocm-6.4.4@sha256…"` to `backend_rocm.go`. The existing `TestSeamGrepGate` "container image literal" regex is `kyuz0|docker\.io/|server-vulkan|rocm-7\.2\.4|rocm7-nightlies`. The `kyuz0|docker.io/` alternation *already* binds the new literal inside the seam — so the gate passes — BUT a leaked `rocm-6.4.4` literal that did NOT include `kyuz0`/`docker.io/` (e.g. a bare tag in a caller) would slip through.
**Why it happens:** The regex is intent-explicit: it lists each tag so a tag leaking outside the seam fails CI even without the registry prefix.
**How to avoid:** In the SAME commit, add `rocm-6\.4\.4` to BOTH the `patterns["container image literal"]` regex and the `cmdPatterns` copy (the cmd-tier walk reuses `patterns["container image literal"]` by reference at line 119, so editing the shared `patterns` entry covers both walks — verify this and do not duplicate). `rocm-6\.4\.4` matches both `rocm-6.4.4` and `rocm-6.4.4-rocwmma` (the rocwmma suffix is a superset).
**Warning signs:** A reviewer sees the new literal land without a `seam_test.go` diff in the same commit.

### Pitfall 2: `backendLabel` silently renders "Vulkan RADV" for a ROCm 6.4.4 backend
**What goes wrong:** `orchestrate.backendLabel` has `case "rocm": return "ROCm 7.2.4 (HIP)"` with a `default: return "Vulkan RADV"`. A new `Name()` of `"rocm-6.4.4"` falls to `default` → the rendered `villa-llama.container` Description= says "Vulkan RADV" while running a ROCm image — a misleading unit.
**Why it happens:** `default` was Vulkan-safe when only two backends existed.
**How to avoid:** Widen the `switch` to map the ROCm-family names to a ROCm label. This DOES change the rendered unit golden — but only for the 6.4.4 backends, which have no existing golden (the shipped goldens are Vulkan + rocm-7.2.4). The planner may add a `villa-llama-rocm-6.4.4.container.golden` if a render test is desired, or rely on the existing render test pattern. The Vulkan + rocm-7.2.4 goldens are byte-unchanged.

### Pitfall 3: `rocm_readiness.image_policy_ok` reads Unknown on a 6.4.4 host (D-07)
**What goes wrong:** `detect.rocmImagePolicyOK(image)` only recognizes `rocm-7.2.4` (stable→true) and `rocm7-nightlies` (deny→false); anything else falls to `default → UnknownBool("resolved ROCm image not recognized")`. BUT note `resolvedROCmImage()` currently *hardcodes* `return rocmStableImageTag` (7.2.4) — it does NOT read config — so `rocm_readiness` always scores against 7.2.4 today regardless of the active backend.
**Why it matters:** Two distinct concerns:
  1. **The preflight policy gate (`checkROCmImage`) is the SC#2 surface, not detect.** It is called as `RunROCm(probe)` → `RunROCmWithPolicy(p, pol, "")` with an **empty `requestedImage`**, so today it WARNs ("no image requested") rather than evaluating the real image. To make SC#2 meaningful for the new image, **thread the resolved target image into the gate**: add a `RunROCmForImage(p, image)` wrapper (or pass `b.Image()` through the existing `requestedImage` param) so `checkROCmImage` evaluates the *actual* 6.4.4 digest against `imageDeny`. Since `imageDeny = ["rocm7-nightlies"]`, the 6.4.4 image PASSES (not denied) — correct, no allow-list needed (confirms D-07).
  2. **The detect `image_policy_ok` readiness field** is informational (surfaced in `villa status` / `detect --json`). For honesty, widen `rocmImagePolicyOK` to recognize `rocm-6.4.4` as pinned-stable so a 6.4.4 host doesn't show a misleading "unknown image" readiness. Add a `rocm644ImageTag` const behind the detect seam (gpu_amd.go is an allowed seam file).
**How to avoid:** (a) add `RunROCmForImage` (thin wrapper passing `requestedImage`) and call it from the widened `PreflightROCm` with `BackendFor(target).Image()`; (b) widen `rocmImagePolicyOK`. Both keep image literals behind their respective seams.
**Warning signs:** A 6.4.4 `villa backend set` dry-run shows `preflight: PASS` only because the image check WARNed (didn't evaluate) — verify it actually evaluates the digest.

### Pitfall 4: `bench --ab`'s `other()` is a hardcoded 2-value swap (SC#3 — the one real design gap)
**What goes wrong:** `internal/bench/bench.go` defines `other(orig)` returning `rocm` if `orig==vulkan`, else `vulkan`. With three ROCm backends, `villa bench --ab` from a `rocm-6.4.4` config would flip to `vulkan` (the `else` branch) — it can NOT A/B `rocm-6.4.4` vs `rocm-7.2.4` (two ROCm backends), which SC#3 explicitly wants ("measures the new image against rocm-7.2.4 / Vulkan").
**Why it happens:** v1.1 had exactly two backends; `other()` encoded that as a binary flip with no allowlist (deliberately, to keep the seam gate green — it transforms config VALUES, holds no image literal).
**How to avoid (planner decision — surface as Open Question):**
  - **Option A (recommended for SC#3):** Add a `villa bench --ab-target <backend>` flag. The `--ab` path then flips to the *named* target instead of `other(orig)`. The pure `bench.Run` already takes the target via the injected `Switch` closure — pass the explicit target from the cmd tier; `other()` becomes the default when `--ab-target` is unset (back-compat). The `bench.json.golden` `from`/`to` fields are computed from `res.AB.From`/`res.AB.To`, so they reflect whatever pair ran — but the golden is a FIXED fixture (`from: vulkan, to: rocm`); a new `--ab-target` test would use its own golden or the `-update` refreeze. **This is the only change that touches the bench byte-frozen golden — and only if a new `--ab-target` test fixture is added; the existing test is untouched.**
  - **Option B (minimal, defer):** Ship the additive backends; document that `bench --ab` compares "current vs the other family member" per the v1.1 binary model, and the user proves Δtg by setting `rocm-6.4.4` active and `--ab`-ing against vulkan, then setting `rocm-7.2.4` active and `--ab`-ing against vulkan, comparing the two Δtg figures. Defer arbitrary-pair A/B to a follow-up. **Risk:** SC#3 says "against rocm-7.2.4 / Vulkan" — Option B satisfies the *Vulkan* leg directly but the *rocm-7.2.4* leg only indirectly (two separate runs vs Vulkan). The planner should confirm with the user whether Option B meets SC#3 or whether Option A is required.
**Warning signs:** A bench `--ab` from `rocm-6.4.4` reports `to: vulkan` when the user expected `to: rocm-7.2.4`.

### Pitfall 5: Rolling tag drift (Pitfall 12 / T-6-04 precedent)
**What goes wrong:** kyuz0 re-pushes the rolling `rocm-6.4.4` tag; a tag-only reference would silently pull a different build.
**How to avoid:** Pin `@sha256:…`, never the tag. Re-verify at implementation time (done — both digests match, 2026-06-07). Add a `checkpoint:human-verify` task to re-run `skopeo inspect` immediately before the seam commit in case the rolling tag moved between research and execution (it had not as of 2026-06-07).

### Pitfall 6: HSA override / hipBLASLt assumed-but-unverified for 6.4.4 (D-06)
**What goes wrong:** Assuming 6.4.4 needs a different `HSA_OVERRIDE_GFX_VERSION` than 7.2.4.
**Resolution (research-confirmed):** `HSA_OVERRIDE_GFX_VERSION=11.5.1` maps the device to gfx1151 (RDNA 3.5) and is the correct override for ROCm 6.4.x on Strix Halo — ROCm 6.3+ has gfx1151 support but the override is still required on 6.4 to force the HSA runtime to the gfx1151 codepath. `ROCBLAS_USE_HIPBLASLT=1` is a runtime opt-in independent of the ROCm minor version. **Keep both identical to 7.2.4 (D-06 default assumption holds).** [CITED: rocm.docs.amd.com RDNA3.5 system optimization; multiple gfx1151 community guides — see §Sources.] **Confidence: HIGH for the override value, MEDIUM that no other env differs — verify on-hardware at first `villa backend set rocm-6.4.4` (the residency proof will FAIL loudly if the device is not targeted, so a wrong override cannot false-green).**

## Golden / Byte-Frozen Impact

| Golden | Changes? | Why |
|--------|----------|-----|
| `cmd/villa/testdata/status.json.golden` | **No** | `backend`/`image` reflect the ACTIVE config backend (`vulkan` in the fixture). No new enum is enumerated; the field is a free string. D-09 holds. |
| `cmd/villa/testdata/bench.json.golden` | **Only if** Option A adds a new `--ab-target` test fixture | The existing `from: vulkan, to: rocm` fixture is untouched. A new pair would need its own golden or `-update`. |
| `cmd/villa/testdata/detect.golden.json` | **No** | `rocm_readiness`/`image_policy_ok` score the hardcoded `resolvedROCmImage()` (7.2.4) — unless the planner changes `resolvedROCmImage` to read config (NOT required by this phase; leave it). The fixture's "pinned stable ROCm image" provenance is unchanged. |
| `internal/orchestrate/testdata/villa-llama.container.golden` (Vulkan) | **No** | Vulkan render path unchanged. |
| `internal/orchestrate/testdata/villa-llama-rocm.container.golden` (7.2.4) | **No** | rocm-7.2.4 render path unchanged (label still "ROCm 7.2.4 (HIP)"). |
| (new) `villa-llama-rocm-6.4.4.container.golden` | **Optional add** | If the planner adds a render test for the 6.4.4 unit, it gets a NEW golden (additive, not a refreeze). |

**Procedure if a golden must change (only the optional new fixtures):** add the new `.golden` file via `go test ./… -update` for the specific test, review the diff, commit. NO schema bump is needed because no existing frozen contract gains/loses a field — the only changes are *new* fixtures for *new* backends. The status `SchemaVersion` is untouched (D-09).

## Code Examples

### BackendFor with the new cases (fail-closed, D-01/D-03)
```go
// internal/inference/backend.go  (Source: existing backend.go, extended)
func BackendFor(name string) (Backend, error) {
    switch name {
    case "", "vulkan":
        return backendVulkan{}, nil
    case "rocm":
        return backendROCm{name: "rocm", image: rocmImage724}, nil
    case "rocm-6.4.4":
        return backendROCm{name: "rocm-6.4.4", image: rocmImage644}, nil
    case "rocm-6.4.4-rocwmma":
        return backendROCm{name: "rocm-6.4.4-rocwmma", image: rocmImage644wmma}, nil
    default:
        return nil, fmt.Errorf("unknown inference backend %q: set backend = "+
            "\"vulkan\" (default), \"rocm\" (7.2.4), \"rocm-6.4.4\", or "+
            "\"rocm-6.4.4-rocwmma\" in config.toml", name)
    }
}
```

### ResidencyProof — unchanged, shared by all ROCm variants (D-06)
```go
// The 6.4.4 images emit the SAME ggml HIP markers as 7.2.4 (same llama.cpp HIP
// backend lineage in the kyuz0 toolboxes). ResidencyProof returns the identical
// ResidencyMarkers for every backendROCm; do NOT parameterize markers unless an
// on-hardware run proves a different device token. (Source: existing backend_rocm.go.)
func (backendROCm) ResidencyProof() ResidencyMarkers {
    return ResidencyMarkers{
        DeviceToken:            "ROCm0",
        DeviceLabel:            "- ROCm",
        StartLogDevicePrefix:   "ggml_cuda_init:",
        FaultString:            "Memory access fault by GPU node",
        RejectSoftwareRenderer: false,
    }
}
```

### seam_test.go regex extension (D-10, SAME commit)
```go
// internal/inference/seam_test.go  (Source: existing, extended)
"container image literal": regexp.MustCompile(`kyuz0|docker\.io/|server-vulkan|rocm-7\.2\.4|rocm-6\.4\.4|rocm7-nightlies`),
// `rocm-6\.4\.4` matches both rocm-6.4.4 and rocm-6.4.4-rocwmma. The cmdPatterns map
// reuses patterns["container image literal"] BY REFERENCE (line ~119), so this single
// edit covers both the internal/ walk and the cmd/villa walk — verify, do not duplicate.
```

## Runtime State Inventory

> This is a code/config phase (new image literals + resolver cases). No data migration, no stored-state rename. Categories explicitly checked:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — `config.toml`'s `backend` field is a free string; setting it to `rocm-6.4.4` needs no migration (old values `vulkan`/`rocm` still resolve). | None. |
| Live service config | The rendered `villa-llama.container` Quadlet unit embeds the image digest — but it is REGENERATED from config on `villa backend set` (config is the source of truth). A user on `rocm-6.4.4` gets the new digest written by the transactional switch. | None beyond the normal switch flow. |
| OS-registered state | None — no new systemd unit names; `villa-llama.service` name unchanged. | None. |
| Secrets/env vars | `HSA_OVERRIDE_GFX_VERSION` / `ROCBLAS_USE_HIPBLASLT` are rendered into the unit, not host secrets; values unchanged (11.5.1 / 1). | None. |
| Build artifacts | None — no package rename; `go build` produces the same `villa` binary. | None. |

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `HSA_OVERRIDE_GFX_VERSION` required for gfx1151 | Still required on ROCm 6.4.x; TheRock *nightlies* ship native gfx1151 kernels making it optional | ROCm 6.3+ added gfx1151 support; native kernels in recent nightlies (not the pinned 6.4.4 stable) | Keep `11.5.1` for the pinned 6.4.4 image (D-06 holds). Do NOT chase nightlies (CLAUDE.md "never nightlies"). |

**Deprecated/outdated:** Nothing in this phase's scope is deprecated. The pinned digest discipline is current best practice for rolling-tag registries.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | ROCm 6.4.4 images use the identical `HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1` as 7.2.4 (D-06 default). | §Common Pitfalls 6, §Code Examples | LOW — research-confirmed for the override; the residency proof FAILS LOUDLY (never false-green) if the device isn't targeted, so a wrong env is caught on-hardware at first switch, not shipped silently. |
| A2 | The 6.4.4 images emit the SAME ggml HIP residency markers (`ROCm0`, `ggml_cuda_init:`, fault string) as 7.2.4 — same kyuz0 toolbox llama.cpp HIP lineage. | §Code Examples (ResidencyProof) | MEDIUM — if a marker differs, the residency proof WARNs/FAILs (degrades safe, never false-green). Verify on-hardware; parameterize markers per-backend only if proven different. |
| A3 | `imageDeny` substring gate suffices (no allow-list) — `rocm-6.4.4` is not denied, so `checkROCmImage` PASSES it. | §Common Pitfalls 3, D-07 | LOW — confirmed by reading the policy + check logic; `imageDeny = ["rocm7-nightlies"]` only. |
| A4 | Extending the shared `patterns["container image literal"]` entry covers BOTH the internal/ and cmd/villa walks (cmdPatterns reuses it by reference). | §Common Pitfalls 1, §Code Examples | LOW — verified at seam_test.go line ~119; planner should confirm during edit. |

## Open Questions

1. **`bench --ab` multi-ROCm selection (the load-bearing one for SC#3)**
   - What we know: `other()` is a binary vulkan↔rocm swap; the pure core takes the target via the injected `Switch` closure, so a `--ab-target` flag is mechanically feasible.
   - What's unclear: Whether SC#3's "against rocm-7.2.4 / Vulkan" REQUIRES arbitrary-pair A/B (Option A) or is satisfied by two separate vs-Vulkan runs (Option B).
   - Recommendation: Surface to the user in planning. Default to **Option A (`--ab-target`)** as it directly satisfies SC#3 and is a small, well-bounded change (one flag, one cmd-tier target plumb, one optional new golden). If the user prefers minimal scope, Option B with documentation.

2. **Optional 6.4.4 render golden**
   - What we know: Widening `backendLabel` changes the rendered unit ONLY for 6.4.4 backends (no existing golden affected).
   - Recommendation: Add a `villa-llama-rocm-6.4.4.container.golden` render test for parity with the rocm-7.2.4 golden — additive, low cost, locks the new unit shape.

3. **On-hardware first-switch verification**
   - Recommendation: A `checkpoint:human-verify` task: re-run `skopeo inspect` for digest currency, then `villa backend set rocm-6.4.4` on the gfx1151 host and confirm the residency proof PASSES (validates A1/A2 — wrong env/markers FAIL loudly, never silently). This is the v1.1 Phase-8 on-hardware UAT pattern.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `skopeo` | Digest re-verification (read-only inspect) | ✓ | 1.22.2 | `podman manifest inspect` |
| `podman` | Fallback digest inspect; runtime (on-hardware only) | ✓ | 5.8.2 | — |
| docker.io network | Digest re-verification | ✓ (DNS resolves; skopeo inspect succeeded) | — | Trust CONTEXT.md digests, flag for on-host re-verify |
| gfx1151 AMD host | On-hardware switch/bench proof (execution-time only) | n/a on dev box | — | Off-hardware: all detect signals Unknown→WARN; the off-hardware tests (grep-gate, resolver, render) run on any host. On-hardware UAT deferred to a checkpoint. |

**Digest re-verification commands (for the implementation-time checkpoint):**
```bash
skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4 | grep Digest
skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma | grep Digest
# Fallback if skopeo absent:
podman manifest inspect docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4
```
**Re-verified 2026-06-07:** both digests resolve and match CONTEXT.md exactly (plain `c81f30a7…`, rocwmma `9a97129a…`). **No missing dependencies block this phase.**

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + golden fixtures); no third-party assertion/mocking lib |
| Config file | none — `go test` defaults; `.golangci.yml` for lint |
| Quick run command | `go test ./internal/inference/ ./cmd/villa/ -run 'Seam\|ROCm\|Backend' -count=1` |
| Full suite command | `make check` (= `go vet ./...` + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ROCM-ALT-01 | `BackendFor("rocm-6.4.4")` resolves to a ROCm backend with the pinned digest; unknown still errors (fail-closed) | unit | `go test ./internal/inference/ -run TestBackendFor -count=1` | ⚠ extend existing resolver test |
| ROCM-ALT-01 | `IsROCmFamily` returns true for all 3 ROCm names, false otherwise | unit | `go test ./internal/inference/ -run TestIsROCmFamily -count=1` | ❌ Wave 0 (new test) |
| ROCM-ALT-01 | Seam grep-gate stays green; the new literal cannot leak (SC#4) | unit | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ extend regex (same commit) |
| ROCM-ALT-01 | `TestROCmMarkerPresence` still finds markers after parameterization | unit | `go test ./internal/inference/ -run TestROCmMarkerPresence -count=1` | ✅ unchanged |
| ROCM-ALT-01 | `PreflightROCm` fires for 6.4.4 family; image-deny gate evaluates the real digest (SC#2) | unit | `go test ./cmd/villa/ -run 'TestRunBackendSet\|Preflight' -count=1` | ⚠ extend existing |
| ROCM-ALT-01 | `backendLabel` renders a ROCm label for 6.4.4 (Pitfall 2) | unit/golden | `go test ./internal/orchestrate/ -run Render -count=1` | ⚠ optional new golden |
| ROCM-ALT-01 | Switch + residency proof for the new backend (SC#1) | manual/on-hardware | `villa backend set rocm-6.4.4` (gfx1151 host) | n/a off-hardware → checkpoint |
| ROCM-ALT-01 | `bench --ab` measures the new image (SC#3) | unit (core) + manual | `go test ./internal/bench/ -count=1` + on-hardware `villa bench --ab` | ⚠ depends on Option A/B |

### Sampling Rate
- **Per task commit:** `go test ./internal/inference/ ./internal/preflight/ ./internal/orchestrate/ ./cmd/villa/ -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + on-hardware checkpoint (switch + residency proof) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] New `TestIsROCmFamily` in `internal/inference/backend_test.go` (or alongside) — covers ROCM-ALT-01 predicate.
- [ ] Extend the existing `BackendFor` resolver test for the two new cases + the widened error message.
- [ ] Extend `TestSeamGrepGate` regex assertion (the test guards itself; add a negative-leak assertion if a fixture exists).
- [ ] (Option A) `bench` cmd test for `--ab-target` + optional new `bench` golden.
- [ ] (Optional) `villa-llama-rocm-6.4.4.container.golden` render fixture.
- No framework install needed — existing Go `testing` covers all off-hardware behavior.

## Security Domain

> `security_enforcement: true`, ASVS level 1, block_on: high.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface in this phase. |
| V3 Session Management | no | n/a |
| V4 Access Control | no | n/a |
| V5 Input Validation | yes | `BackendFor` is the validation boundary — fail-closed on any unknown/typo'd backend string (D-03). `config.toml` `backend` is untrusted hand-editable input; an invalid value is an actionable error, never a silent privileged-device fallback. |
| V6 Cryptography | yes (supply-chain) | Image **digest pinning** (`@sha256:…`) is the integrity control — a re-pushed rolling tag cannot substitute a different image. Re-verify digests at implementation time (checkpoint). |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Rolling-tag substitution (kyuz0 re-pushes `rocm-6.4.4`) | Tampering | Pin `@sha256:` digest; never the bare tag (Pitfall 5). Both digests re-verified live 2026-06-07. |
| Backend-literal leak outside the seam (loss of single-polymorphism guarantee) | Tampering / Repudiation | `TestSeamGrepGate` regex extended same-commit (D-10/SC#4) — a leaked `rocm-6.4.4` literal fails CI. |
| Untrusted `config.toml` backend value selecting an unintended device path | Elevation of Privilege | Fail-closed `BackendFor` (D-03) — unknown value errors; only the explicit 3 ROCm cases reach the kfd/dri passthrough. |
| Silent CPU fallback masquerading as a healthy ROCm switch | Spoofing (false-green) | Offload-asserting residency proof in `liveProve` — a partial/silent CPU fallback FAILs and rolls back (SC#1 discipline carried by reused `backendswap`). |
| New outbound traffic | Information Disclosure | Only the additional image pull (consistent with existing image/model pulls); zero telemetry unchanged. |

## Plan Decomposition + Waves

Mirror the v1.1 spine→gate→surface order. Recommended **3 plans**:

**Plan 12-01 — Seam + resolver + digests (off-hardware, the additive core)**
- Parameterize `backendROCm{name, image}` (D-06); add the 3 image consts (digests pinned + re-verified).
- Add the 2 `BackendFor` cases + the widened fail-closed error (D-01/D-03).
- Add `inference.IsROCmFamily(name)` (D-08 predicate).
- Extend `seam_test.go` image regex + verify `cmdPatterns` reuse (D-10/SC#4, SAME commit/plan).
- Tests: extend `BackendFor` resolver test, new `IsROCmFamily` test, `TestSeamGrepGate`/`TestROCmMarkerPresence` green.
- Update CLAUDE.md "Container Images Standardized On" table (2 new rows).
- **Wave:** standalone (no deps). Everything here is pure/off-hardware.

**Plan 12-02 — Family predicate routing + policy gate + labels (off-hardware)**
- Replace the 3 literal `"rocm"` comparisons with `IsROCmFamily` (cmd/villa/backend.go PreflightROCm, cmd/villa/preflight.go `--backend` router).
- Thread the resolved target image into the policy gate: add `preflight.RunROCmForImage(p, image)` (or pass `b.Image()` to `RunROCmWithPolicy`'s `requestedImage`), call from PreflightROCm with `BackendFor(target).Image()` (Pitfall 3 / SC#2).
- Widen `orchestrate.backendLabel` for the ROCm-family names (Pitfall 2) + optional new render golden.
- Widen detect `rocmImagePolicyOK` to recognize `rocm-6.4.4` (add `rocm644ImageTag` behind the detect seam) for honest `rocm_readiness`.
- Tests: backend-set dry-run preflight for a 6.4.4 target, render label test.
- **Wave:** depends on 12-01 (`IsROCmFamily`, the new `BackendFor` cases).

**Plan 12-03 — bench `--ab` multi-backend selection + on-hardware checkpoint (SC#3)**
- Implement the planner-chosen Option A (`--ab-target`) or Option B (document) for `bench --ab` (Pitfall 4 / Open Question 1).
- If Option A: thread an explicit target through the cmd-tier `Switch` closure; `other()` stays the default; optional new bench golden via `-update`.
- `checkpoint:human-verify`: re-run `skopeo inspect` (digest currency) + on-hardware `villa backend set rocm-6.4.4` (residency proof PASS) + `villa bench --ab` against 7.2.4 / Vulkan (Δtg recovery). Validates A1/A2.
- **Wave:** depends on 12-01 + 12-02 (a switchable, gated backend must exist before benching it).

**Sequencing rationale:** 12-01 is the zero-risk additive core (ROADMAP placed this phase first precisely because the seams exist). 12-02 generalizes the predicate + gate off-hardware. 12-03 carries the only contract-touching change (bench `--ab` target) and the on-hardware risk concentration, isolated last so the byte-frozen bench golden evolves at most once and only if Option A is chosen.

## Sources

### Primary (HIGH confidence)
- Codebase (read directly this session): `internal/inference/{backend.go, backend_rocm.go, backend_vulkan.go, inference.go, seam_test.go}`, `internal/preflight/{checks_rocm.go, floors.go, rocm-policy.json}`, `internal/detect/{gpu_amd.go, readiness_rocm.go, profile.go}`, `internal/bench/bench.go`, `internal/orchestrate/render.go`, `cmd/villa/{backend.go, preflight.go, inference.go, bench.go}`, `cmd/villa/testdata/*.golden*`.
- `skopeo inspect docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4[-rocwmma]` — live digest re-verification 2026-06-07 (both match CONTEXT.md).
- `.planning/phases/12-rocm-6-4-4-alternate-backend/12-CONTEXT.md`, `.planning/ROADMAP.md`, `.planning/REQUIREMENTS.md`, `.planning/milestones/v1.1-ROADMAP.md`.

### Secondary (MEDIUM confidence)
- [ROCm RDNA3.5 system optimization](https://rocm.docs.amd.com/en/latest/how-to/system-optimization/rdna3-5.html) — gfx1151/HSA override context.
- [ollama gfx1151 ROCm working guide (issue #14855)](https://github.com/ollama/ollama/issues/14855), [ROCm/TheRock discussion #2684](https://github.com/ROCm/TheRock/discussions/2684), [llm-tracker Strix Halo](https://llm-tracker.info/_TOORG/Strix-Halo) — `HSA_OVERRIDE_GFX_VERSION=11.5.1` still applies on ROCm 6.4.x for gfx1151; native kernels only in nightlies (not the pinned stable).

### Tertiary (LOW confidence)
- None — all claims cross-verified against the codebase or live skopeo/docs.

## Metadata

**Confidence breakdown:**
- Standard stack (no new deps, reuse map): HIGH — read every relevant file directly.
- Architecture (parameterized delta, predicate, call-site audit): HIGH — audit is exhaustive over `grep "rocm"`.
- Digests: HIGH — re-verified live, byte-exact match.
- HSA/hipBLASLt for 6.4.4 (D-06): HIGH for the override value, MEDIUM that nothing else differs — caught on-hardware by the offload-asserting proof if wrong.
- `bench --ab` 3-backend gap: HIGH that the gap exists; the Option A/B choice is a user/planner decision.
- Goldens: HIGH — traced each frozen contract; only optional new fixtures, no schema bump.

**Research date:** 2026-06-07
**Valid until:** 2026-07-07 (stable; re-verify the rolling digests immediately before the 12-01 seam commit per Pitfall 5).
