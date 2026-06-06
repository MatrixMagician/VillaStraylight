# Codebase Concerns

**Analysis Date:** 2026-06-07

Scope: full repo, post-v1.1 (ROCm Opt-In Backend) merge. Every item below was
verified against the actual source at the cited file:line before listing. Items
the milestone audit / PR review flagged but which are in fact **already fixed**
are recorded in the "Verified FIXED — do not reopen" section so they are not
re-filed as open debt.

Severity legend: **High** (can mislead a user / corrupt a result / hang a
command), **Medium** (latent trap, fragility, or divergence risk), **Low**
(advisory, by-design, or documentation).

---

## Tech Debt

### ROCm policy literals duplicated across two packages (no shared leaf pkg) — **Medium**

- Issue: The ROCm firmware floor/denylist are hard-coded **twice**: as Go
  literals `rocmFirmwareFloor = "20260110"` / `rocmFirmwareDeny = ["20251125"]`
  in `internal/detect/gpu_amd.go:366,368`, and as `firmwareFloor` /
  `firmwareDeny` in `internal/preflight/rocm-policy.json`. The same pattern also
  duplicates the kernel floor (`rocmKernelFloorTarget`) and stable image tag
  (`rocmStableImageTag`). The code comment at `internal/detect/gpu_amd.go:361`
  documents the duplication as intentional.
- Files: `internal/detect/gpu_amd.go:356-368`, `internal/preflight/rocm-policy.json`
- Impact: This duplication is the **root cause of the firmware-floor divergence
  class** — the exact bug fixed in `8eb450d` (preflight PASSed sub-floor firmware
  while detect's readiness gate reported not-ready). The two surfaces can drift
  again on the next ROCm version bump because nothing forces them to agree.
- Fix approach: Extract a shared leaf package (e.g. `internal/rocmpolicy`) that
  both `detect` and `preflight` import — neither imports the other today, so a
  third leaf with no dependencies on either resolves the cycle constraint cited
  in the comment. Make the JSON the single source and have detect read the same
  embedded policy (or generate the Go consts from the JSON at build time).

### Backend-name vocabulary bypasses the single `BackendFor` resolver — **Medium**

- Issue: `inference.BackendFor()` (`internal/inference/backend.go:21`) is the
  designed single fail-closed resolution point, and it is correct. But three
  call sites encode the backend vocabulary directly instead of routing through a
  resolver/allowlist:
  - `internal/bench/bench.go:174` `other()` is a hard **2-value swap**
    (`vulkan`↔`rocm`); any non-`vulkan` token maps to `rocm`, and a third
    backend would silently flip to `vulkan`.
  - `cmd/villa/preflight.go:47` `if backend == "rocm"` — a bare string compare
    routes the ROCm gate.
  - Backend identifier consts are re-declared locally in
    `internal/bench/bench.go:185-188` (`vulkan`/`rocm`).
- Files: `internal/bench/bench.go:174-188`, `cmd/villa/preflight.go:47`,
  `internal/inference/backend.go:21-30`
- Impact: When a 3rd backend (the planned macOS/Metal path) is added, these
  sites will compile clean but behave wrong (Metal → treated as vulkan in the
  A/B swap; Metal preflight → falls through to the standalone host gate). The
  single-resolver design intent is silently bypassed.
- Fix approach: Expose the known-backend set / "the other backend(s)" from the
  `inference` package and have bench and preflight consume it, so adding a
  backend is a one-file change behind `BackendFor`.

### `liveProve` and `liveMeasure` are near-verbatim clones — **Medium**

- Issue: `liveProve` (`cmd/villa/backend.go:65`) and `liveMeasure`
  (`cmd/villa/bench.go:73`) share a line-for-line identical opening sequence
  (BackendFor → `config.LoadVilla` → `liveModelFile` → `NewContainerRunner(...).Endpoint()`
  → `context.WithTimeout`), including duplicated comments, and both then
  duplicate the in-flight `detect.GPUBusyPercent()` max-sampling loop +
  `RunningOffloadVerdict` fold.
- Files: `cmd/villa/backend.go:65-174`, `cmd/villa/bench.go:73-194`
- Impact: A residency-folding fix (e.g. the gpu_busy false-FAIL below, or a new
  marker) must be made in both places or the prove and bench paths diverge —
  exactly the divergence class that already bit the firmware floor.
- Fix approach: Extract the shared "resolve + probe + fold residency" body into
  one helper (the residency-corroboration sampling is the natural seam) that
  both prove and measure call.

### `MesaFloor` loaded but never consumed — **Low**

- Issue: `mesaFloor` is parsed from `rocm-policy.json` and embedded but no check
  reads it; flagged with an explicit `TODO(phase-2)`.
- Files: `internal/preflight/floors.go:33`, `internal/preflight/rocm-policy.json`
- Impact: A below-floor Mesa/RADV version is not gated. Dead policy field.
- Fix approach: Wire a Mesa-version check into the ROCm/Vulkan preflight, or drop
  the field until it is needed.

### Nyquist VALIDATION.md drafts for phases 6/8/9/10 — **Low**

- Issue: VALIDATION.md for phases 6, 8, 9, 10 are in `draft` /
  `nyquist_compliant:false` despite full green verification suites; only phase 7
  is formally compliant.
- Files: `.planning/phases/0{6,8,9,10}-*/` VALIDATION.md, recorded in
  `.planning/milestones/v1.1-MILESTONE-AUDIT.md:108-118`
- Impact: Process/validation-status debt only; no coverage hole. Blocks a clean
  formal Nyquist sign-off before archive.
- Fix approach: Run `/gsd-validate-phase N` for 6/8/9/10 if formal sign-off is
  wanted.

---

## Known Bugs

### `llm.Complete` conflates `predicted_n==0` with a missing timings block — **Medium** (PR #2 deferred #3)

- Symptoms: `Complete` returns `ErrNoTimings` (→ bench VOIDs the run) whenever
  `parsed.Timings.PredictedN == 0 && parsed.Timings.PredictedPerSec == 0`. A
  server that returns a **genuine** timings block but generated zero tokens
  (e.g. an immediate stop, max_tokens=0 edge, or a refusal) is indistinguishable
  from a build that omitted the `timings` extension entirely.
- Files: `internal/llm/openai.go:195-197` (note: the PR body cites
  `internal/llm/complete.go`; the actual file is `openai.go`)
- Trigger: A completion that legitimately produces 0 predicted tokens.
- Workaround: None today; such a run is silently voided rather than surfaced as a
  distinct "completed with 0 tokens" condition.
- Fix approach: Distinguish "no timings object present in JSON" (use a
  `*Timings` pointer or a `json.RawMessage` presence check) from "timings present
  but predicted_n==0". Only the former is `ErrNoTimings`.

### `gpu_busy_percent` Known-0 can false-FAIL residency on a short decode — **Medium** (PR #2 deferred #6, second clause)

- Symptoms: The residency proof folds a Known gpu_busy reading: `busy.Value == 0`
  → `StatusFail` ("silent CPU fallback"). The cmd layer keeps the **max** busy
  reading across in-flight samples, but a genuinely short decode that completes
  between ticker samples can leave the only Known reading at 0%, flipping a real
  GPU-resident PASS to FAIL.
- Files: `internal/inference/running_offload.go:277-300` (the 0%→FAIL rule),
  `cmd/villa/backend.go:117-149` (max-sampling loop), `cmd/villa/bench.go` (clone)
- Trigger: A backend switch / bench whose generation probe finishes faster than
  `busySampleInterval` while the GPU is briefly idle at every sample instant.
- Workaround: A longer probe (more predicted tokens) widens the busy window.
- Fix approach: Treat a Known-0 busy as a **corroborator-WARN**, not a hard FAIL,
  when the journal+GTT residency signals already PASS (gpu_busy is documented as
  a corroborator, not the primary signal); or guarantee at least one mid-decode
  sample.

---

## Concurrency / Hang Risks

### `ResidencyJournal` journalctl exec is unbounded and outside `proveTimeout` — **High** (PR #2 deferred #4)

- Problem: `liveProve` bounds readiness + generation probe by `proveTimeout`
  (5 min) via `deadlineCtx`, but then calls
  `orchestrate.NewSystemd().ResidencyJournal(installServiceName)` (`cmd/villa/backend.go:158`)
  with **no context**. `ResidencyJournal` → `runCmd` → `runTool`
  (`internal/orchestrate/systemd.go:68-76`) uses `exec.Command` (NOT
  `exec.CommandContext`) and `cmd.Output()` — output is memory-bounded to 256 KiB
  but **execution time is unbounded**.
- Files: `cmd/villa/backend.go:158`, `internal/orchestrate/systemd.go:68-76,199-219`
- Impact: A wedged journald / hung `journalctl --user` call hangs
  `villa backend set` (and `bench --ab`) indefinitely, defeating the
  T-8-07 hang-guard the rest of the prove path carefully enforces.
- Fix approach: Add a context-aware `runCmd` (`exec.CommandContext`) and thread
  `deadlineCtx` into `ResidencyJournal`, so the journal read shares the prove
  deadline.

### `backendswap.Run` proves under `context.Background()` → `--ab` Switch/Restore uncancellable — **High** (PR #2 deferred #6, first clause)

- Problem: `backendswap.Run(d Deps, target string)` takes **no `context`
  parameter** (`internal/backendswap/backendswap.go:145`) and internally calls
  `d.Prove(context.Background(), target)` (`backendswap.go:248`). The bench layer
  then explicitly discards the caller's SIGINT context when delegating:
  `d.Switch = func(_ context.Context, target string)` and the matching `Restore`
  (`cmd/villa/bench.go:227,230`).
- Files: `internal/backendswap/backendswap.go:145,248`, `cmd/villa/bench.go:227-243`
- Impact: This **contradicts the documented Ctrl-C contract**: `cmd/villa/bench.go:199-200`
  and `internal/bench/bench.go:230-231` both state the SIGINT-cancellable context
  is "threaded through bench.Run into every Measure/Switch/Restore call so a
  Ctrl-C aborts an in-flight bench." It is honored for Measure but **dropped for
  Switch/Restore** — a Ctrl-C during the transactional flip/rollback cannot
  cancel the in-flight prove (which itself runs the up-to-5-min readiness poll).
- Fix approach: Add a `ctx context.Context` parameter to `backendswap.Run` and
  pass it to `d.Prove`; stop discarding the context in the bench Switch/Restore
  closures.

---

## Security Considerations

### `--security-opt` mapping is last-wins (scalar, not slice) — **Medium** (P7 WR-01)

- Risk: `parseContainerArgs` maps `--security-opt` to a **scalar**
  `cv.PodmanArgs = flSecOpt + " " + flags[i+1]` (`internal/orchestrate/render.go:204`),
  unlike `--device`/`--group-add` which append to slices. A hypothetical second
  `--security-opt` from a backend's run spec silently overwrites the first.
- Files: `internal/orchestrate/render.go:202-206`,
  `internal/inference/backend_rocm.go:73`, `internal/inference/backend_vulkan.go:91`
- Current mitigation: Today each backend emits exactly one
  `--security-opt seccomp=unconfined`, so the bug is latent, not live.
- Recommendations: Make `PodmanArgs`/security-opt a slice that accumulates,
  mirroring `AddDevice`/`GroupAdd`.

### ROCm container runs `seccomp=unconfined` with `/dev/kfd` + `/dev/dri` access — **Low** (P6 by-design)

- Risk: The ROCm backend mounts the kfd/dri devices and disables the seccomp
  profile (`--security-opt seccomp=unconfined`), a wider syscall surface than the
  default profile, inside an otherwise-rootless container.
- Files: `internal/inference/backend_rocm.go:73`
- Current mitigation: Rootless Podman; ROCm is opt-in and never the default;
  documented as a known by-design security note in the v1.1 audit (P6 WR-03).
- Recommendations: Track upstream ROCm/Podman work toward a narrower seccomp
  profile; document the trade-off in user-facing docs for the ROCm opt-in.

---

## Fragile Areas

### `os.Exit` inside a cobra `RunE` — **Low** (P7 WR-04)

- Files: `cmd/villa/preflight.go:56` (`os.Exit(code)` then unreachable
  `return nil`)
- Why fragile: `os.Exit` skips deferred cleanup and cobra's own error handling;
  benign today because preflight is leaf and read-only, but a fragility trap if
  the command later acquires deferred resources or is composed into another
  command.
- Safe modification: Return a typed error / use a custom exit-code carrier and
  call `os.Exit` once in `main`.
- Test coverage: `cmd/villa/preflight_test.go` exists; exit-code path tested via
  the renderer, not the `os.Exit` itself.

### `strings.Cut` `ok` discarded on `--env` parsing — **Low** (P7 WR-03)

- Files: `internal/orchestrate/render.go:198` (`k, v, _ := strings.Cut(...)`)
- Why fragile: A bare `--env FOO` with no `=` yields `k="FOO", v=""` (ok
  ignored), emitting an empty-value env pair into the Quadlet rather than
  rejecting it. Latent — current backends only emit `KEY=VALUE` env.
- Safe modification: Check the `ok` return and skip/error on a malformed `--env`.

---

## TOCTOU / Consistency

### Double config-load during a backend switch — **Medium** (P8 WR-05)

- Problem: A single `villa backend set` reads config.toml twice from two
  independent code paths: `backendswap.Run` calls `d.LoadConfig()`
  (`internal/backendswap/backendswap.go:148`) to capture `from := cfg.Backend`
  and mutate, while the `Prove` callback `liveProve` independently re-loads via
  `config.LoadVilla()` (`cmd/villa/backend.go:75`). A `Restart` happens between
  them.
- Files: `internal/backendswap/backendswap.go:148`, `cmd/villa/backend.go:75`
- Impact: A config.toml edited mid-switch produces an inconsistent view (capture
  reads one value, prove reads another). Narrow window; single-user CLI makes it
  unlikely but not impossible.
- Fix approach: Capture the config once in `Run` and pass the loaded `cfg` (or
  the resolved model file) into the `Prove` callback rather than re-reading.

---

## ROCm Backend Gating Gaps (by-design / advisory)

### Fit-check is backend-agnostic vs the ROCm ~64 GB allocation cap — **Low** (P8 WR-01)

- Problem: The pre-switch fit-check does not special-case ROCm's documented
  ~64 GB single-allocation behavior; only the **image denylist** encodes the cap
  (refusing `rocm7-nightlies`). A stable-image ROCm switch of a model that fits
  the unified-memory envelope but exceeds ROCm's allocation behavior is not
  pre-refused — it surfaces as a residency/probe FAIL → rollback instead.
- Files: `internal/preflight/checks_rocm.go:192-212`,
  `internal/backendswap/backendswap.go:61`
- Impact: Correct outcome (rolls back), but later and less informatively than a
  pre-flight refusal would be.
- Fix approach: Add a ROCm-specific fit advisory keyed on model size vs the
  documented cap.

### `liveProve` omits the `/props` drift overlay that `status` includes — **Low** (P8 WR-02)

- Problem: The switch prove path does not apply the `/props` drift overlay that
  `internal/status/status.go` folds into its residency view.
- Files: `cmd/villa/backend.go` (liveProve), `internal/status/status.go`
- Impact: Prove and status can report slightly different residency provenance
  for the same running server.
- Fix approach: Share the overlay (relates to the liveProve/liveMeasure
  de-duplication above).

### GTT residency floor is a host-wide counter, not per-process — **Low** (P8 WR-03)

- Problem: The GTT-used floor reads a host-wide sysfs counter
  (`detect.GTTUsedBytes()`), so unrelated GPU memory consumers contribute to the
  residency signal.
- Files: `internal/inference/running_offload.go` (GTT fold),
  `cmd/villa/backend.go:161`
- Impact: The floor is a corroborator, not the sole signal, so impact is bounded;
  a noisy host could mask a partial CPU fallback that the journal/gpu_busy
  signals must then catch.
- Fix approach: Per-process GTT accounting if/when the kernel exposes it.

### WARN-vs-FAIL collapsed to a flat "fail" in some prove paths — **Low** (P8 WR-04)

- Problem: `liveProve` maps everything that is not `StatusPass` to a flat
  `Status: "fail"` (`cmd/villa/backend.go:171-174`), collapsing the distinction
  between a true FAIL and a could-not-evaluate WARN.
- Files: `cmd/villa/backend.go:169-174`
- Impact: A user sees "fail → rolled back" without knowing whether the cutover
  was proven-bad or merely unevaluable. Conservative (rolls back either way) but
  less informative.
- Fix approach: Carry the WARN reason into the rollback detail.

---

## Test Coverage Gaps

### On-hardware ROCm UAT remains BLOCKED on the primary dev box — **High** (validation risk)

- What's not tested: Full ROCm switch/bench/surfacing flows are not exercisable
  end-to-end on the primary development host as part of routine CI/local runs.
- Files: ROCm paths across `internal/inference/backend_rocm.go`,
  `internal/preflight/checks_rocm.go`, `internal/detect/{gpu_amd.go,readiness_rocm.go}`,
  `internal/backendswap/*`
- Risk: ROCm-specific regressions can land green because the dev-box suite proves
  them only via the **off-hardware proxy** (every ROCm signal degrades to
  Unknown/WARN by design) plus the **single** ROCm-capable host run captured in
  the v1.1 audit (`bench --ab` → Δpp +4.84 / Δtg −11.15, 2026-06-06). A working
  `rocminfo`/`vulkaninfo` binary is present on the dev box, but that does not
  constitute a validated gfx1151 ROCm runtime — the milestone treats on-hardware
  ROCm UAT as a separate, scarce resource.
- Priority: **High** for any future ROCm change — re-run the on-hardware UAT
  before merging ROCm-path edits; do not rely on the off-hardware proxy alone.

### `TestSeamGrepGate` walks only `.go` sources — **Low** (P10)

- What's not tested: The dashboard `.js`/`.html`/`.css` assets are not covered by
  the literal-leak seam gate (direct grep confirms they are literal-free today,
  so the property holds in fact — this is an enforcement-coverage gap, not a live
  leak).
- Files: dashboard assets under `cmd/villa/` (`dashboard.{html,css,js}`), the
  seam gate test in `internal/inference/seam_test.go`
- Risk: A future literal added to a dashboard asset would not be caught by the
  gate.
- Priority: Low.

---

## Documentation Drift (residual)

- `06-REVIEW.md` frontmatter is stale (`status: issues_found / critical: 1`)
  though CR-01 was fixed in `499644e`; the prose Status line was reconciled in
  `a39f42b` but verify the frontmatter. Files:
  `.planning/phases/06-rocm-backend-resolver-spine/06-REVIEW.md`.
- 6 SUMMARY `requirements-completed` frontmatter tags were missing (DET-04,
  BSET-01/02/03, BENCH-02, REC-05); Phase 11 reconciled these — verify before
  re-filing. See `.planning/milestones/v1.1-MILESTONE-AUDIT.md:38-40,126`.

---

## Verified FIXED — do NOT reopen

These were flagged in the milestone audit / PR review but are confirmed resolved
in the current tree. Listed so they are not re-filed as open debt.

- **ROCm firmware floor now enforced** (was P7 WR-02 "sub-floor PASSes"). Fixed
  in `8eb450d`: `checkROCmFirmware` now WARNs on sub-floor-but-not-denied
  firmware via the `FirmwareFloor` it previously ignored.
  `internal/preflight/checks_rocm.go`.
- **Residency parse keeps `max(deviceMiB)`, not last-write** (was the false-FAIL
  on a `-lv 5` 0.00 MiB estimate line). Fixed in `3b7d6af`.
  `internal/inference/running_offload.go`.
- **Empty `DeviceToken` guard** (was the false-PASS where
  `strings.Contains(line, "")` matched every line). Fixed in `3b7d6af`: an empty
  token now degrades to WARN, never PASS. `internal/inference/running_offload.go`.
- **`rocm_readiness` detect probes implemented** (was P10 "badge stays unknown";
  `FirmwareDateOK()`/`HSAOverrideViable()` not probed). Fixed in Phase 11: both
  are wired at `internal/detect/readiness_rocm.go:23-24`, live-verified badge
  reads `ready` on the gfx1151 host.

---

*Concerns audit: 2026-06-07*
