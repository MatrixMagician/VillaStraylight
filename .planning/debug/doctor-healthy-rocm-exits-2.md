---
status: diagnosed
trigger: "Phase 13 UAT Test 1 — `villa doctor` on a healthy live ROCm install exits 2 (WARN), can never reach exit 0"
created: 2026-06-07
updated: 2026-06-07
goal: find_root_cause_only
---

## Current Focus

hypothesis: CONFIRMED — doctor.Aggregate re-runs the STANDALONE ROCm host-prep gate
  (preflight.RunROCm) whose firmware/hsa/image checks are typed-Unknown BY CONSTRUCTION
  (firmware+hsa hardcoded UnknownStr; image passed empty), so they always degrade to
  WARN and the worst-wins fold rolls Overall up to WARN → exit 2, even when residency is
  proven. No code change made (diagnose-only).
test: traced doctor.go → doctor.Aggregate → preflight.RunROCm → RunROCmWithPolicy
expecting: the three WARNs originate at fixed source positions independent of host truth
next_action: hand root cause + fix directions to the planner

## Symptoms

expected: On a live, healthy install, `villa doctor` exits 0 (DOCTOR-01: renderDoctor
  maps Overall PASS→0, WARN→2, FAIL→1).
actual: On gfx1151, backend=rocm, model qwen3.6-35b-a3b, ctx 131072 — `villa doctor`
  returned exit 2 (Overall WARN) on a fully-healthy install. offload:villa-llama PASS
  (real residency proof: log "ROCm0 model buffer 20583.34 MiB resident on the iGPU" +
  sysfs GTT-used 26.4 GB ≥ 22.1 GB weight footprint). All /health 200. drift PASS.
  Exit-2 came SOLELY from three ROCm preflight findings degrading to typed-Unknown WARN:
    - ROCM-PRE-firmware WARN "firmware version not probed; ensure ≥ 20260110 and avoid 20251125"
    - ROCM-PRE-hsa      WARN "could not verify HSA_OVERRIDE_GFX_VERSION (expected 11.5.1)"
    - ROCM-PRE-image    WARN "no ROCm image requested; the standalone gate cannot evaluate the image (avoid rocm7-nightlies)"
errors: None — honest typed-Unknown behavior, not a crash.
reproduction: Test 1 in 13-UAT.md — run `./villa doctor` on a healthy ROCm install.
started: Always (since Phase 13 doctor shipped); structural, not a regression.

## Eliminated

- hypothesis: The exit-code mapping in renderDoctor is inverted / wrong.
  evidence: cmd/villa/doctor.go:95-109 maps PASS→exitPass(0), WARN→exitWarn(2),
    FAIL→exitBlocked(1) correctly. The WARN is genuine (Overall=="WARN"); the mapping
    is faithful. The fault is upstream in what produces Overall=WARN.
  timestamp: 2026-06-07

- hypothesis: A real host probe failed (firmware/HSA actually below floor / wrong).
  evidence: The findings are typed-Unknown WARN ("not probed" / "could not verify"),
    NOT confident FAIL. firmware+hsa are hardcoded UnknownStr in RunROCmWithPolicy
    (checks_rocm.go:66-67), so the verdict is independent of the real host — no probe
    even runs. Image WARN is "no image requested" (empty arg), not a deny match.
  timestamp: 2026-06-07

## Evidence

- timestamp: 2026-06-07
  checked: cmd/villa/doctor.go runDoctor → renderDoctor exit mapping
  found: runDoctor (doctor.go:67-70) calls doctor.Aggregate then renderDoctor.
    renderDoctor (doctor.go:80-110) switches on r.Overall: FAIL→exitBlocked,
    WARN→exitWarn(=2), default→exitPass(=0). Mapping is correct.
  implication: Overall=="WARN" is the true value being mapped; root cause is in Aggregate.

- timestamp: 2026-06-07
  checked: internal/doctor/doctor.go Aggregate — host-conditions step + worst-wins fold
  found: Aggregate (doctor.go:127-222). Lines 132-141: it calls d.Probe() then, when
    inference.IsROCmFamily(d.Backend), checks = preflight.RunROCm(profile) and appends
    findingFromCheck(c) for EVERY result. Lines 202-215: worst-wins fold — any WARN
    finding sets worst=1 → Overall="WARN". statusRank (doctor.go:114-123): PASS<WARN<FAIL.
  implication: Any single preflight WARN finding forces Overall=WARN regardless of how
    strong the residency proof is. There is NO supersession: a proven offload PASS does
    not down-rank an unevaluable host-prep WARN.

- timestamp: 2026-06-07
  checked: cmd/villa/doctor.go liveDoctorDeps — what backend/profile is passed
  found: liveDoctorDeps (doctor.go:155-211) sets Backend: cfg.Backend and
    Probe: detect.Probe. The running container's actual image digest, firmware date,
    and HSA env are NEVER threaded into the doctor host-prep gate. Aggregate calls the
    STANDALONE preflight.RunROCm(profile) (no image), not RunROCmForImage.
  implication: doctor re-runs the pre-INSTALL standalone host-prep gate against a
    POST-install running stack, with none of the running-stack facts that would let
    the checks evaluate confidently.

- timestamp: 2026-06-07
  checked: internal/preflight/checks_rocm.go RunROCm / RunROCmWithPolicy
  found: RunROCm (checks_rocm.go:38-40) → RunROCmWithPolicy(p, policy, ""). Inside
    RunROCmWithPolicy (checks_rocm.go:64-75): firmware := detect.UnknownStr(...) and
    hsa := detect.UnknownStr(...) are HARDCODED typed-Unknown (lines 66-67, "not probed
    in Phase 1"), and requestedImage is "" from RunROCm. These feed checkROCmFirmware,
    checkROCmHSA, checkROCmImage.
  implication: firmware and hsa can NEVER be Known via RunROCm — they are unconditionally
    Unknown. image is unconditionally empty via RunROCm. The three WARNs are structural,
    not host-dependent.

- timestamp: 2026-06-07
  checked: the three check functions' Unknown branches
  found:
    - checkROCmFirmware (checks_rocm.go:126-160): `if !fw.Known || fw.Value == ""` →
      warn(...) "firmware version not probed; ensure ≥ %s and avoid %s" (lines 135-139).
      FAIL only on a Known deny-list match (lines 140-146).
    - checkROCmHSA (checks_rocm.go:180-198): `if !hsa.Known` → warn(...) "could not
      verify HSA_OVERRIDE_GFX_VERSION (expected %s)" (lines 184-187). FAIL only when
      Known-and-wrong (lines 189-194).
    - checkROCmImage (checks_rocm.go:209-228): `if strings.TrimSpace(requestedImage)==""`
      → warn(...) "no ROCm image requested; the standalone gate cannot evaluate the
      image (avoid %s)" (lines 214-217). FAIL only on a Known deny-substring (219-225).
  implication: Each WARN string in the UAT exactly matches its Unknown/empty branch.
    Confident BLOCK (FAIL) still works and must be preserved — only the typed-Unknown
    branches over-warn here.

- timestamp: 2026-06-07
  checked: detect profile — are firmware/HSA even probeable to thread in?
  found: detect/profile.go:71-76 has HSAOverrideViable Bool + FirmwareDateOK Bool, but
    readiness_rocm.go:66-69 states HSA is BY DESIGN not read from the host env ("the env
    var is set inside the container runtime, not the host doctor reads" — Pitfall 4).
    RunROCmWithPolicy ignores these profile fields entirely and re-stubs Unknown.
  implication: firmware/HSA are not cheaply made Known on the host for a POST-install
    diagnosis — HSA in particular is a CONTAINER-runtime env, not a host fact. The image
    digest IS knowable (BackendFor(cfg.Backend).Image(), backend_rocm.go:60), so the
    image WARN alone is threadable via the existing RunROCmForImage (checks_rocm.go:48).

- timestamp: 2026-06-07
  checked: the contradiction with the proven residency
  found: When offload:villa-llama is inference.StatusPass, offloadFinding (doctor.go:274-297)
    emits a BLOCK-tier PASS — residency is PROVEN (log marker + GTT delta). Yet the
    host-prep firmware/HSA/image WARNs (which exist to PREDICT whether ROCm will work)
    still fold to Overall=WARN. A proven-working backend logically supersedes a
    can't-predict-if-it-will-work host-prep advisory.
  implication: The fix is a SUPERSESSION rule in the doctor core, not a probe change:
    when residency is proven for the ROCm service, the typed-Unknown (un-evaluable)
    ROCm host-prep WARNs should not force Overall=WARN. Confident FAILs must still BLOCK.

## Resolution

root_cause: |
  doctor.Aggregate (internal/doctor/doctor.go:132-141) re-runs the STANDALONE
  pre-install ROCm host-prep gate `preflight.RunROCm(profile)` against a POST-install
  running stack. RunROCm → RunROCmWithPolicy (internal/preflight/checks_rocm.go:64-75)
  hardcodes the firmware and HSA signals as typed-Unknown (lines 66-67) and passes an
  EMPTY requested image (line 39), so checkROCmFirmware/checkROCmHSA/checkROCmImage take
  their "could-not-evaluate" WARN branches (checks_rocm.go:135-139, 184-187, 214-217)
  unconditionally — independent of the real, healthy host. doctor.Aggregate appends each
  as a Finding (doctor.go:139-141) and the worst-wins fold (doctor.go:202-215, statusRank
  doctor.go:114-123) rolls any WARN up to Overall="WARN", which renderDoctor faithfully
  maps to exit 2 (cmd/villa/doctor.go:105-106). There is NO supersession: a proven ROCm
  residency PASS (offloadFinding, doctor.go:282-284, StatusPass) does not down-rank these
  un-evaluable host-prep WARNs. Net: an opt-in-ROCm install with PROVEN residency can
  never reach exit 0. This is correct, honest typed-Unknown behavior in the wrong context
  — the install-time host-prep gate is being applied verbatim to a running-install
  diagnosis where residency already answers the question those checks only predict.

fix: "" # diagnose-only — not applied

verification: ""

files_changed: []

## Candidate Fix Directions (for the planner — preserve no-false-green)

Constraint for ALL options: a CONFIDENT bad signal must still BLOCK/FAIL — a deny-listed
firmware build (checks_rocm.go:140-146), a Known-wrong HSA override (189-194), or a
deny-listed image (219-225) must continue to fold to FAIL → exit 1. Only typed-Unknown
(un-evaluable) host-prep that is SUPERSEDED by proven residency may be prevented from
forcing Overall=WARN.

Option A — Residency-supersession in the doctor core (RECOMMENDED).
  Where: internal/doctor/doctor.go Aggregate (around the host-conditions loop, 139-141,
  using the offload outcome computed at 158-163).
  What: when the ROCm-family service's offload Verdict is inference.StatusPass (residency
  PROVEN), DOWNGRADE the typed-Unknown ROCm host-prep findings (firmware/hsa/image whose
  Status==WARN AND whose WARN is the "could not evaluate"/Unknown branch) so they do not
  raise the worst-wins rank — e.g. keep them in Findings for visibility but mark them
  PASS-or-informational (a new non-rank-raising status, or exclude from the fold) ONLY
  when residency is proven. A Known-FAIL on any of these is untouched and still BLOCKS.
  Trade-off: keeps preflight pure/unchanged; concentrates the policy in the one place
  that already owns the worst-wins fold. Must be careful to downgrade ONLY the specific
  un-evaluable host-prep checks, identified by ID (idROCmFirmware/idROCmHSA/idROCmImage)
  + Status==WARN, and ONLY under a proven ROCm StatusPass. Preserves no-false-green:
  confident FAIL paths are never reached by the downgrade.

Option B — Thread the running image into the gate (PARTIAL — fixes only image).
  Where: cmd/villa/doctor.go liveDoctorDeps + internal/doctor/doctor.go Aggregate:137.
  What: pass BackendFor(cfg.Backend).Image() (backend_rocm.go:60) and call the existing
  preflight.RunROCmForImage(profile, image) (checks_rocm.go:48) instead of RunROCm, so
  checkROCmImage evaluates the real digest (PASS if not denied, FAIL if denied).
  Trade-off: legitimately upgrades the image WARN to PASS and is the "correct" evaluation
  for a running stack — but does NOT fix firmware/hsa (still hardcoded Unknown at
  checks_rocm.go:66-67), so exit 2 persists. Best combined with A (or with a firmware
  probe), not alone. Strongly preserves no-false-green (a denied image now FAILs).

Option C — Probe firmware/HSA for real and thread them in (LARGEST; partial for HSA).
  Where: internal/detect (add a firmware-date + HSA probe) + RunROCmWithPolicy:66-67.
  What: source firmware from rpm/linux-firmware and HSA from the running container's env;
  pass them typed-Known so the checks evaluate confidently.
  Trade-off: most "honest", but HSA_OVERRIDE_GFX_VERSION is by design a CONTAINER-runtime
  env, not a host fact (readiness_rocm.go:66-69 / Pitfall 4) — reading it on the host is
  wrong, and reading it from the container is a new impure probe outside orchestrate.
  High cost, scope creep, and still leaves HSA awkward. Not recommended as the primary
  fix; residency already proves what HSA would only predict (favoring Option A).

Recommendation: Option A (residency-supersession in the doctor core), optionally plus
Option B for the image check so a denied running image is actively caught. A alone is the
minimal change that lets a residency-proven ROCm install reach exit 0 while keeping every
confident BLOCK path intact.

## Artifacts list (files + functions + line ranges to change for the fix)

- internal/doctor/doctor.go
  - Aggregate, host-conditions loop: lines 132-141 (where RunROCm results become Findings)
  - worst-wins fold + supersession point: lines 158-163 (offload outcome) and 202-215 (fold)
  - statusRank (referenced): lines 114-123
  - offloadFinding StatusPass branch (the "residency proven" signal to gate on): 282-284
- cmd/villa/doctor.go (only if Option B): liveDoctorDeps 155-211; pass image + switch
  Aggregate's call from RunROCm to RunROCmForImage (would also need a Deps field/param).
- internal/preflight/checks_rocm.go (reference — DO NOT need to change for Option A):
  RunROCm 38-40, RunROCmWithPolicy 64-75 (hardcoded Unknown 66-67), RunROCmForImage 48-50,
  checkROCmFirmware Unknown branch 135-139, checkROCmHSA Unknown branch 184-187,
  checkROCmImage empty branch 214-217.
- cmd/villa/doctor.go renderDoctor 80-110 (reference only — mapping is correct, unchanged).
- doctor golden + tests (will need refreezing once behavior changes): internal/doctor
  golden fixtures and doctor_test.go (the worst-wins/supersession case must be added).
