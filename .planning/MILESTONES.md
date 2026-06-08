# Milestones

## v1.2 Operability (Shipped: 2026-06-08)

**Phases completed:** 6 phases (12–17), 19 plans, 24 tasks
**Git range:** `v1.1` → `b8b94d3` (180 commits, 271 files changed, +31,077 / −15,316)
**Timeline:** 2026-06-07 → 2026-06-08
**Codebase:** ~36.9k Go LOC (internal + cmd); full suite green; CGO-free static build gated in CI
**Audit:** PASSED — 13/13 requirements satisfied, 5/5 cross-phase integration flows wired, 6/6 phases Nyquist-compliant (see `milestones/v1.2-MILESTONE-AUDIT.md`)

**Theme:** harden VillaStraylight from "just works after install" into an operable, recoverable, measurable daily-driver — without weakening the zero-telemetry / strictly-local posture or the single-static-binary constraint.

**Key accomplishments:**

- **Alternate TG-tuned ROCm backend (ROCM-ALT-01)** — digest-pinned `rocm-6.4.4` (+ `-rocwmma` variant) selectable through the proven `BackendFor` seam, policy-gated by `rocm-policy.json`, seam-locked so the new image literals can't leak. The honest on-hardware A/B **disproved** the perf premise (`rocm-6.4.4` does not recover the v1.1 Δtg −11.15; Vulkan still leads tg ~+11.68) — the capability shipped correctly and Vulkan rightly stays the tg default. Prove, don't assume.
- **`villa doctor` health diagnosis (DOCTOR-01/02/03)** — one-shot, read-only PASS/WARN/FAIL diagnosis composing the shipped preflight + status + residency-proof cores plus config-vs-disk drift, with a preflight-mirroring 0/1/2 exit contract, remediation on every non-PASS, offload-FAIL dominating a health-200 (no false-green), and ROCm residency-supersession so a healthy opt-in ROCm install reaches exit 0.
- **Saved bench reports + `--compare` (BENCH-03/04)** — pure `internal/benchstore` persists each run as a versioned JSONL saved report under XDG (pp/tg kept separate, residency-void recorded), and read-only `villa bench --compare`/`--list` print per-metric deltas behind a comparability guard that refuses cross-fingerprint deltas (UNKNOWN host ⇒ not-comparable, never false-equal).
- **Cumulative usage tracking (USAGE-01/02)** — reset-aware per-model token totals folded from monotonic `_total` counters, counts-only with zero new outbound, surfaced via ONE append-only `status.Report.usage` field (schema 1→2, golden re-frozen once) and the dashboard as the sole mutex-guarded writer of `usage.json`.
- **Backup / restore (BAK-01/02/03)** — self-describing local archive (config + Open WebUI volume + usage + bench; model weights excluded, identities recorded) with SHA-256 manifest, and a transactional restore mirroring `backendswap` (capture → quiesce → clean-recreate-before-import → offload-asserting prove → verbatim rollback) plus version-skew WARN-and-confirm and fail-closed BLOCK on checksum/incompatible-schema.
- **Guided TUI install capstone (INSTALL-01/02)** — a `charmbracelet/huh` 5-screen wizard wired into `villa install` as a pure collector over the finished detect→recommend→preflight→install pipeline, byte-identical to the flag path, bypassed by `--no-tui`/`--json`/non-TTY, NO_COLOR/dumb-terminal degradable — and the milestone's ONLY new dependency, kept command-tier-only with the binary still a single static CGO-free build (gated in CI; bubbletea pinned v1.3.6, no v2 leak).

**Honest outcomes & known limitations (not gaps):**

- ROCM-ALT-01 is a *validated, outcome-negative* close — capability delivered as specced; the perf hypothesis it tested is false on this host/model. `rocm-6.4.4-rocwmma` ships selectable but is non-functional on gfx1151 (a correct offload-asserting residency FAIL + rollback).
- Backup cross-host / post-`podman system reset` restore is a documented best-effort limitation (not run on hardware — too destructive); its UID-remap + SELinux `:Z` repair mechanism is validated indirectly.
- One on-hardware regression was found+fixed during Phase 16 UAT (WR-05 store-guard broke `/tmp` volume staging; fix `8eb2526` + regression test).

---

## v1.1 ROCm Opt-In Backend (Shipped: 2026-06-06)

**Phases completed:** 6 phases (6–11), 16 plans, 29 tasks
**Git range:** `v1.0` → `c62eb52` (141 commits, 160 files changed, +23,360 / −328)
**Timeline:** 2026-06-05 → 2026-06-06
**Codebase:** 129 Go files, ~26.3k LOC; full suite green (16 packages)

**Proof-of-value (measured on-hardware 2026-06-06):** `villa bench --ab` on the live gfx1151 host → **Δpp +4.84 / Δtg −11.15** — ROCm wins prompt-processing, regresses token-generation. The milestone honours its honesty constraint: `recommend` never promises a speed-up; the user proves it per-model with `bench`.

**Key accomplishments:**

- **Phase 6 — ROCm backend + resolver spine:** a backend-neutral residency-proof engine (`ResidencyMarkers` + `ResidencyProof()` on the `Backend` interface, dual offload-assert scrapes, journal fault scan, 0<N<M partial-offload FAIL, D-06 `gpu_busy_percent` fold) and `backend_rocm.go` (digest-pinned `rocm-7.2.4`, kfd+dri, render group, ordered HSA→hipBLASLt env, ROCm0 markers) selected through a single `BackendFor()` resolver that fails closed — Vulkan stays the default, byte-identical.
- **Phase 7 — render + preflight + detect:** renders the ROCm `villa-llama.container` as a byte-frozen additive delta over Vulkan; a reusable `RunROCm` host-fitness gate driven by a `go:embed` `rocm-policy.json` that refuses-with-remediation only on confident known-bad (firmware 20251125, nightly image, kernel < 6.18.4, missing HSA override, non-gfx1151) and degrades unevaluable signals to WARN; `villa detect --json` gains an append-only `rocm_readiness` block (schema 1→2).
- **Phase 8 — `villa backend set` switch + rollback:** the transactional `internal/backendswap` capture→mutate→prove→rollback state machine flips the backend on a running install, gating cutover on a real generation-probe + residency proof and auto-rolling back verbatim on any failure — **4/4 on-hardware UAT** (happy-path cutover, dry-run/show, forced CPU-fallback rollback, bounded 5m timeout).
- **Phase 9 — honest A/B `villa bench`:** non-streaming `llm.Complete` + a pure `internal/bench` core report prompt-processing and token-generation tok/s separately (never blended), warmup-discarded, N-rep median+stddev, residency-void-gated, with `--ab` delegating the flip to `backendswap.Run` (never re-implementing switching) — **3/3 on-hardware UAT**.
- **Phase 10 — backend + tok/s surfacing:** `villa status`, `recommend`, and the control dashboard became backend-aware as a single append-only contract change — active backend + image tag, live tok/s labeled by backend, tri-state ROCm-readiness badge, honest ROCm advice — with the byte-frozen `--json`/goldens re-frozen exactly once.
- **Phase 11 — tech-debt cleanup:** made the `rocm_readiness` firmware/HSA detect probes real (live-verified badge reads `ready`, backend `rocm` on the gfx1151 host) and reconciled the milestone-audit documentation drift (6 SUMMARY `requirements-completed` tags, the stale 06-REVIEW status line, the REQUIREMENTS.md ROCM-02 note).

**Milestone audit:** `tech_debt` verdict — 13/13 requirements satisfied, 5/5 phases verified, 5/5 integration seams wired, 3/3 E2E flows complete; 0 critical blockers. Highest-value debt (rocm_readiness probes + doc reconciliation) closed inline by Phase 11. Remaining items are advisory hardening notes and Nyquist validation-status drafts.

**Known deferred items at close:** 1 — quick task `260606-p3a` is complete (commit `8aa9c90`, in Quick Tasks Completed) but its task-status frontmatter reads `unknown` (tag lag only). See STATE.md → Deferred Items.

---
