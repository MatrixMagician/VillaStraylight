# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware. v1.2 (Operability) extends that bar to "and stays operable, recoverable, and measurable over time."

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)
- 🚧 **v1.2 Operability** — Phases 12–17 (active, started 2026-06-07)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:

- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1–5) — SHIPPED 2026-06-05</summary>

- [x] Phase 1: Hardware Foundation & Preflight Gate (3/3 plans) — completed 2026-06-03
- [x] Phase 2: GPU-Validated Inference Slice (3/3 plans) — completed 2026-06-04
- [x] Phase 3: Orchestrated Install & Lifecycle (6/6 plans) — completed 2026-06-04
- [x] Phase 4: Chat Integration (3/3 plans) — completed 2026-06-05
- [x] Phase 5: Control Dashboard (8/8 plans) — completed 2026-06-05

</details>

<details>
<summary>✅ v1.1 ROCm Opt-In Backend (Phases 6–11) — SHIPPED 2026-06-06</summary>

**Milestone goal:** Add an opt-in ROCm/HIP inference backend for higher throughput on AMD Strix Halo (gfx1151), gated hard enough to preserve the v1.0 "just works" bar, switchable on a running install with transactional rollback, and benchmarked honestly to prove the per-model win — while Vulkan RADV remains the safe default.

- [x] Phase 6: ROCm Backend + Resolver Spine (3/3 plans) — completed 2026-06-05
- [x] Phase 7: ROCm Render Unit + Preflight/Detect (3/3 plans) — completed 2026-06-06
- [x] Phase 8: `villa backend set` Switch Verb + Rollback (2/2 plans) — completed 2026-06-06 (4/4 on-hardware UAT)
- [x] Phase 9: `villa bench` (Honest A/B) (3/3 plans) — completed 2026-06-06 (3/3 on-hardware UAT; Δpp +4.84 / Δtg −11.15)
- [x] Phase 10: Backend + tok/s Surfacing (3/3 plans) — completed 2026-06-06
- [x] Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (2/2 plans) — completed 2026-06-06

See `milestones/v1.1-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

### 🚧 v1.2 Operability (Phases 12–17) — ACTIVE

**Milestone goal:** Harden VillaStraylight into an operable, recoverable daily-driver — self-diagnosis, backup/restore, comparative benchmarking, usage history, a guided install, and a TG-tuned ROCm option — without weakening the v1.0 "just works" bar or the strictly-local / zero-telemetry posture.

**Build order is research-converged** (all four researchers + the synthesizer agreed): seam-locked + composition features first (zero/trivial contract risk), then the two persistence features with their byte-frozen evolutions staggered so **only one byte-frozen contract evolves at a time**, then the destructive backup, then the TUI capstone over the finished surface.

- [x] **Phase 12: `rocm-6.4.4` Alternate Backend** — Add a digest-pinned TG-tuned ROCm image selectable behind `BackendFor`, seam-locked + policy-gated, to recover the v1.1 Δtg −11.15 regression. (3 plans) (completed 2026-06-07)
- [x] **Phase 13: `villa doctor` Health Diagnosis** — One-shot, read-only health/diagnosis composing preflight + status + residency proof + config-vs-disk drift, with remediation and 0/2/1 exit tiers. (verified 15/15 must-haves; on-hardware UAT 3/3 live on gfx1151 — healthy→exit 0, induced CPU fallback→exit 1 no-false-green, drift→exit 2; gap-closure 13-03 added ROCm residency-supersession so a residency-proven ROCm install reaches exit 0) (completed 2026-06-07)
- [ ] **Phase 14: Saved Bench Reports + `--compare`** — Persist each bench run as a versioned saved report under XDG, and compare runs over time behind a comparability guard.
- [ ] **Phase 15: Cumulative Usage Tracking** — Accumulate reset-aware token totals locally and surface them (append-only) in `status` + dashboard, counts-only, no new outbound.
- [ ] **Phase 16: Backup / Restore** — Self-describing local archive (config + Open WebUI data, model weights excluded) with a transactional, skew-warning restore.
- [ ] **Phase 17: Guided TUI Install (Capstone)** — Interactive terminal install flow over the finished pipeline, pure presentation, TTY-gated with a `--no-tui` fallback, single static CGO-free binary.

## Phase Details

### Phase 12: `rocm-6.4.4` Alternate Backend

**Goal**: Users can opt into a digest-pinned, token-generation-tuned ROCm image (`rocm-6.4.4` or its bench-decided `-rocwmma` variant) as an alternate inference backend, selected like any other backend through the proven `BackendFor` seam, to recover the v1.1 Δtg −11.15 token-generation regression — Vulkan stays default, never auto-switched.
**Depends on**: Nothing new (builds on the shipped v1.1 `BackendFor` / `backendswap` / `bench --ab` machinery; first phase of v1.2)
**Requirements**: ROCM-ALT-01
**Success Criteria** (what must be TRUE):

  1. User can run `villa backend set rocm-6.4.4` (or the chosen variant) and the stack switches transactionally with a residency proof, exactly like the existing ROCm backend — Vulkan remains the default and is never auto-switched.
  2. The new image is digest-pinned and gated by `rocm-policy.json` floors; a request that fails a floor is refused with named remediation, never silently downgraded.
  3. `villa bench --ab` measures the new image against rocm-7.2.4 / Vulkan and reports the pp/tg deltas separately, so the user can prove (not assume) which digest recovers Δtg before it ships.
  4. The new image literal cannot leak outside the inference seam — `internal/inference/seam_test.go`'s image regex is extended in the same commit and `TestSeamGrepGate` stays green.**Plans**: 3 plans

**Wave 1**

- [x] 12-01-PLAN.md — Seam + resolver + digests: parameterize `backendROCm` by image (D-06), pin both re-verified digests, add the two fail-closed `BackendFor` cases + `IsROCmFamily` (D-01/D-03/D-08), extend the `seam_test.go` regex same-commit (D-10/SC#4) [Wave 1]

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 12-02-PLAN.md — Family predicate routing + policy gate + labels: route every `"rocm"` check through `IsROCmFamily`, thread the resolved image into `RunROCmForImage` (SC#2), widen `backendLabel` + detect `rocmImagePolicyOK` [Wave 2]

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 12-03-PLAN.md — bench `--ab-target` (Option A, SC#3); on-hardware UAT COMPLETE 2026-06-07 on gfx1151 — SC#1/SC#2/SC#3 PASS (rocm-6.4.4 residency-proven; rocwmma residency FAIL→rolled back; fail-closed validated). Honest bench result: rocm-6.4.4 does NOT recover Δtg (Vulkan still +11.68 tg) — Vulkan stays default. [Wave 3] [commits 9fcd5d3, c049e31]

**Research flag**: Re-verify the rolling `rocm-6.4.4` tag digest at implementation time (kyuz0 re-pushes the rolling tag — pin the digest `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62`; the `-rocwmma` variant `sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141` is a bench-decided choice — ship the one the A/B proves).

### Phase 13: `villa doctor` Health Diagnosis

**Goal**: Users can run a single `villa doctor` command to get an honest, read-only health diagnosis of a running install — composing the shipped preflight + status + residency-proof cores plus a config-vs-disk drift check — with actionable remediation for every non-healthy finding and a preflight-mirroring 0/2/1 exit contract.
**Depends on**: Phase 12 (so doctor diagnoses over the final backend surface, incl. the alt image; building it early also surfaces faults later phases may introduce)
**Requirements**: DOCTOR-01, DOCTOR-02, DOCTOR-03
**Success Criteria** (what must be TRUE):

  1. User can run `villa doctor` and get a one-shot health report that exits `0` (healthy), `2` (blocking fault), or `1` (warning), mirroring the preflight exit contract.
  2. Every non-healthy finding carries actionable remediation text the user can act on.
  3. A silent or partial CPU fallback is reported as a FAIL (offload-asserting) — `villa doctor` never returns a false-green over a health-200 that hides a degraded backend.
  4. `villa doctor` detects and reports config-vs-disk drift — rendered Quadlet units that no longer match the config source of truth.

**Plans**: 3 plans (1 gap-closure)

**Wave 1**

- [x] 13-01-PLAN.md — Pure `internal/doctor` core (Finding/Report/Deps/Aggregate, worst-wins fold, offload-FAIL-dominates-health, drift WARN) + Wave-0 off-hardware tests [Wave 1]

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 13-02-PLAN.md — `cmd/villa/doctor.go` verb + `liveDoctorDeps` + read-only unit-dir resolver + `renderDoctor` 0/2/1 exit rollup + OWN `--json` golden + root registration [Wave 2]

**Wave 3** *(gap closure — 13-UAT Test 1)*

- [x] 13-03-PLAN.md — Residency-supersession in pure `doctor.Aggregate`: a proven ROCm offload PASS down-ranks the typed-Unknown ROCm host-prep WARNs (firmware/hsa/image) so a healthy ROCm install reaches exit 0, while a CONFIDENT ROCm FAIL still BLOCKs (no-false-green); + Option B image thread-through via `RunROCmForImage`. Closes the DOCTOR-01 exit-0 gap. [Wave 3]

**Implementation note**: New pure `internal/doctor` core with its OWN unconstrained golden; do NOT extend the byte-frozen `status.Report`. doctor diagnoses + remediates-by-advice only — it never mutates/repairs the install (mutation stays in explicit verbs).

### Phase 14: Saved Bench Reports + `--compare`

**Goal**: Users can persist every `villa bench` run as a versioned saved report under `$XDG_DATA_HOME/villa/` and compare saved reports over time / across models — with prompt-processing and token-generation tok/s kept separate (never blended) and a comparability guard that refuses to print deltas across non-comparable runs. Pairs with Phase 12 to *prove* the Δtg recovery.
**Depends on**: Phase 12 (so the alt-image bench runs are the first saved reports that prove the regression recovery); independent of Phase 13.
**Requirements**: BENCH-03, BENCH-04
**Success Criteria** (what must be TRUE):

  1. After running `villa bench`, the run is persisted as a versioned saved report under `$XDG_DATA_HOME/villa/`, recording pp and tg tok/s separately and the residency-void state.
  2. User can list and view saved bench reports.
  3. User can run `villa bench --compare` to see pp/tg deltas between saved reports.
  4. `--compare` refuses to print deltas across runs with mismatched model/quant/host fingerprint, labeling them "not comparable" rather than emitting a misleading delta.

**Plans**: TBD
**Implementation note**: Freeze the new `internal/benchstore` saved-report format FIRST via its own golden (`schema_version` from day one) — the on-disk format is a contract; lock it before any real reports are written to prevent migrations. Flat JSONL persistence; persist the full `BenchSpec` + env fingerprint + `VoidExhausted`/`Reason`. `--compare` is read-only and distinct from live `--ab`.

### Phase 15: Cumulative Usage Tracking

**Goal**: villa accumulates cumulative prompt/generated token counts per model locally over time — reset-aware (surviving `llama-server` counter resets) and counts-only (no prompt/response content, no new outbound) — and surfaces those cumulative totals in `villa status` and the control dashboard alongside the existing live tok/s.
**Depends on**: Phase 14 (sequenced after BENCH-03 so only ONE byte-frozen contract evolution — the `status.Report` schema bump — is in flight at a time)
**Requirements**: USAGE-01, USAGE-02
**Success Criteria** (what must be TRUE):

  1. villa accumulates cumulative prompt + generated token counts per model and they persist across `llama-server` restarts / counter resets (reset-aware folding).
  2. Usage is counts-only — no prompt or response content is stored — and tracking adds no new outbound traffic (the strictly-local posture is preserved and asserted).
  3. `villa status` surfaces cumulative usage totals over time (live tok/s remains).
  4. The control dashboard surfaces cumulative usage totals.

**Plans**: TBD
**UI hint**: yes
**Research flag**: Confirm the exact llama.cpp `/metrics` cumulative counter names (`llamacpp:prompt_tokens_total` / `tokens_predicted_total`) and the counter-reset semantics on restart/backend-swap against a live `llama-server` at the start of the phase (MEDIUM-confidence names; HIGH-confidence pattern). Design the fold to degrade to typed-Unknown if a counter is absent.
**Implementation note**: New pure `internal/usage` `Fold(prior, sample) -> Totals` folding from monotonic `_total` counters (not rate gauges). The dashboard server's existing poll loop is the SOLE, mutex-guarded writer of `usage.json` (atomic write, XDG 0600); the CLI is one-shot and only reads. The `status` change is exactly ONE append-only field above `SchemaVersion` + ONE schema bump + ONE golden re-freeze.

### Phase 16: Backup / Restore

**Goal**: Users can back up their workspace (config `.toml` + the Open WebUI data volume) to a single self-describing local archive that EXCLUDES re-pullable model weights and carries a manifest of versions / image digests / checksums — and restore from it transactionally, so a failed or partial restore never corrupts a running stack.
**Depends on**: Phase 15 (highest host-I/O and destructive risk — sequenced after the read-only/additive features so the safer surface is proven first; backup must capture the usage store and saved reports added by 14–15)
**Requirements**: BAK-01, BAK-02, BAK-03
**Success Criteria** (what must be TRUE):

  1. User can run `villa backup` to produce a single archive containing config + the Open WebUI data volume + a version/digest/checksum manifest, with model weights excluded (their identity recorded for re-pull).
  2. User can run `villa restore` and have it apply transactionally (capture → quiesce → swap → restart → prove → rollback-on-failure) — a failed/partial restore leaves the running stack intact.
  3. `villa restore` warns on version/digest skew between the archive manifest and the current install before applying, with remediation.

**Plans**: TBD
**Research flag**: Validate cross-host / post-`podman system reset` restore (UID-mapping + SELinux `:Z` repair, e.g. `podman unshare chown -R`) — the "looks done but isn't" case the same-host round-trip test misses — and decide the Open WebUI live-SQLite quiesce approach (avoid overwriting a live DB). External Podman volume mechanics are MEDIUM-confidence.
**Implementation note**: Use `podman volume export/import` (NEVER host-path tar) behind an `orchestrate`-resident `volume_io` seam or a cmd-tier fixed-arg podman call (as `uninstall.go`'s `podmanVolumeRm` already proves passes the seam gate) — do NOT add a new impure module; `orchestrate` stays the only intentionally-impure module. Pure `internal/backup` does manifest/verify. Restore config via `config.SaveVilla` + re-run preflight; recreate the volume via Quadlet. Mirror `backendswap` transactional discipline. 0600/0700 XDG.

### Phase 17: Guided TUI Install (Capstone)

**Goal**: Users can run a guided interactive terminal install that composes the finished detect → recommend → confirm/adjust → preflight-gate → install pipeline with confirmation/consent steps — pure presentation, adding no decision logic to any core — and that degrades gracefully on a non-TTY environment and via an explicit `--no-tui` flag back to the existing flag-driven path, with the binary remaining a single static CGO-free build.
**Depends on**: Phase 16 (capstone over the *whole*, finished command surface; introduces the milestone's only new dependency, so it wraps a stable set of cores)
**Requirements**: INSTALL-01, INSTALL-02
**Success Criteria** (what must be TRUE):

  1. User can run a guided TUI that walks detect → recommend → confirm/adjust → preflight gate → install, writing the same `config.toml` and running the same install as the flag path.
  2. The TUI computes nothing itself — all fit/preflight/backend decisions come from the existing cores (`recommend.Pick`, `preflight`, `BackendFor`) — so its output matches the flag-driven path.
  3. On a non-TTY environment or with `--no-tui`, the command degrades gracefully to the existing flag-driven install path; flags stay first-class.
  4. The binary still builds as a single static CGO-free binary (`CGO_ENABLED=0` build check passes).

**Plans**: TBD
**UI hint**: yes
**Implementation note**: `charmbracelet/huh` v1.0.0 is the ONLY new first-party dependency — pure-Go/CGO-free; it transitively pins the *stable* `bubbletea v1.3.6` / `lipgloss v1.1.0` (NOT `charm.land/bubbletea/v2`). Confined to the command tier; no pure core may import it. Verify the `CGO_ENABLED=0` static build in CI.

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Hardware Foundation & Preflight Gate | v1.0 | 3/3 | Complete | 2026-06-03 |
| 2. GPU-Validated Inference Slice | v1.0 | 3/3 | Complete | 2026-06-04 |
| 3. Orchestrated Install & Lifecycle | v1.0 | 6/6 | Complete | 2026-06-04 |
| 4. Chat Integration | v1.0 | 3/3 | Complete | 2026-06-05 |
| 5. Control Dashboard | v1.0 | 8/8 | Complete | 2026-06-05 |
| 6. ROCm Backend + Resolver Spine | v1.1 | 3/3 | Complete | 2026-06-05 |
| 7. ROCm Render Unit + Preflight/Detect | v1.1 | 3/3 | Complete | 2026-06-06 |
| 8. `villa backend set` Switch Verb + Rollback | v1.1 | 2/2 | Complete | 2026-06-06 |
| 9. `villa bench` (Honest A/B) | v1.1 | 3/3 | Complete | 2026-06-06 |
| 10. Backend + tok/s Surfacing | v1.1 | 3/3 | Complete | 2026-06-06 |
| 11. Address v1.1 tech debt | v1.1 | 2/2 | Complete | 2026-06-06 |
| 12. `rocm-6.4.4` Alternate Backend | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 13. `villa doctor` Health Diagnosis | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 14. Saved Bench Reports + `--compare` | v1.2 | 0/? | Not started | - |
| 15. Cumulative Usage Tracking | v1.2 | 0/? | Not started | - |
| 16. Backup / Restore | v1.2 | 0/? | Not started | - |
| 17. Guided TUI Install | v1.2 | 0/? | Not started | - |
