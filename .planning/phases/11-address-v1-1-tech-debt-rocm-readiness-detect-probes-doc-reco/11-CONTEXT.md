# Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation - Context

**Gathered:** 2026-06-06
**Status:** Ready for planning

> `--auto` discuss. All six gray areas were auto-selected and decided with the
> recommended option on every choice (logged in `11-DISCUSSION-LOG.md`). Every
> decision is a HOW-to-implement refinement of the two ROADMAP Phase-11 named
> items — no scope added. This is a **post-milestone tech-debt cleanup** phase:
> it closes the milestone-audit functional follow-up (rocm_readiness badge) plus
> the cross-cutting documentation debt, and nothing else.

<domain>
## Phase Boundary

Close the **two named v1.1 milestone-audit tech-debt items** (and only those) —
see `.planning/v1.1-MILESTONE-AUDIT.md` for the authoritative list:

1. **rocm_readiness detect probes (functional follow-up).** Make
   `internal/detect/readiness_rocm.go`'s `firmwareDateOK()` and
   `hsaOverrideViable()` **real probes** instead of unconditional
   `UnknownBool`. Today the `rocm_readiness` badge surfaced by Phase 10
   (status/dashboard) stays `unknown` even on a live ROCm backend because these
   two underlying facts are never probed — the "non-unknown readiness badge"
   sub-clause of **DASH-06 SC#1** is unmet, and the **DET-04** readiness fields
   `firmware_date_ok` / `hsa_override_viable` are permanently UNSET. This phase
   makes them probe-real (KnownBool on-hardware) while preserving the
   no-false-green discipline (D-08) so off-hardware they stay honestly UNSET.

2. **Documentation reconciliation (cross-cutting doc debt).** Fix exactly the
   audit-named documentation drift, each verified against its phase
   VERIFICATION.md before correcting:
   - Add the missing `requirements-completed` SUMMARY frontmatter tags:
     **DET-04** (07-03), **BSET-01/02/03** (08-01/08-02), **BENCH-02**
     (09-02/09-03), **REC-05** (10-02) — 6 SUMMARYs, all already verified
     SATISFIED in their phase VERIFICATION.md + WIRED by the integration checker
     (tagging lag only).
   - Correct the stale **06-REVIEW.md** frontmatter (`status: issues_found` /
     `critical: 1`) — the CR-01 BLOCKER was fixed in `499644e` and is verified
     present + regression-tested.
   - Refresh the stale **REQUIREMENTS.md** ROCM-02 tracking note (audit notes
     line ~88).

**Out of scope (explicitly):**
- The **~13 advisory hardening warnings** (P8 ×5: fit-check vs ROCm cap, /props
  drift overlay, host-wide GTT floor, WARN-vs-FAIL collapse, double config-load
  TOCTOU; P7 ×4 + MesaFloor-unconsumed; P6 ×4 by-design advisories) — not named
  in the ROADMAP Phase-11 line; their own cleanup effort. Deferred.
- The **Nyquist VALIDATION.md draft reconciliation** (P6/P8/P9/P10
  `nyquist_compliant:false` / draft) — process/validation-status debt; reconcile
  via `/gsd-validate-phase N` if formal sign-off is wanted. Deferred.
- Any **new detect fields, schema bump, or golden re-freeze** — the two probe
  fields already exist (Phase-7 schema 2); this phase fills values, it does not
  grow the contract.
- Any **new ROCm capability** (alternate images, auto-switch, bench changes) —
  v1.1.x backlog, untouched here.

</domain>

<decisions>
## Implementation Decisions

### Firmware-date probe (`firmwareDateOK`) — DET-04 / DASH-06 SC#1
- **D-01:** Probe the installed **linux-firmware build date** via
  `exec.Command` (e.g. `rpm -q --qf '%{VERSION}' linux-firmware`, fixed args, no
  shell interpolation) — consistent with detect's existing `exec.Command` usage
  for `rocminfo`/`vulkaninfo` (`internal/detect/gpu_amd.go:243`). Parse the
  date stamp; return **KnownBool only when the date parses**, otherwise
  `UnknownBool(reason, raw)` (rpm absent, package not installed, unparseable →
  honest UNSET, never a fabricated false — no-false-green D-08).
- **D-02:** Do the floor/deny comparison **behind a detect-local seam in
  `gpu_amd.go`** (e.g. a `firmwareDatePolicyOK(date)` helper), mirroring the
  existing `kernelMeetsROCmFloor` / `rocmImagePolicyOK` seam pattern — so
  `readiness_rocm.go` carries **no firmware-version literal** and the inference
  `TestSeamGrepGate` stays green. The policy values are the same as preflight's
  `rocm-policy.json` (`firmwareFloor: 20260110`, `firmwareDeny: [20251125]`);
  **research to decide** whether to share that embedded JSON or duplicate the two
  values detect-side (kernel floor is already duplicated in the detect seam, so
  duplication is the established precedent and avoids a detect→preflight import,
  which would invert the existing layering). FAIL semantics: a Known date in the
  denylist OR below the floor → `KnownBool(false)`; a Known clear date →
  `KnownBool(true)`.

### HSA-override viability probe (`hsaOverrideViable`) — DET-04 / DASH-06 SC#1
- **D-03:** Derive viability from **already-Known host facts**, not a runtime
  experiment: the iGPU is **gfx1151** (the target the `HSA_OVERRIDE_GFX_VERSION=
  11.5.1` override maps to) AND the ROCm substrate is present (rocminfo
  enumerated / kernel floor met). All source facts Known-good → `KnownBool(true)`;
  a Known non-gfx1151 / absent substrate → `KnownBool(false)`; any source fact
  Unknown → `UnknownBool`. **No container run, no env mutation, no side effects** —
  detect stays pure host inspection. Reuse the gfx-id `Str` already threaded into
  `computeROCmReadiness` (and `RocminfoGfx1151` / `KernelFloorOK` already
  computed there) rather than re-reading.

### Schema / golden impact — append-only discipline preserved
- **D-04:** **No new fields, no schema bump, no golden re-freeze.**
  `firmware_date_ok` and `hsa_override_viable` already exist on `HostProfile`
  (Phase-7 schema 2); this phase only makes their **values** real.
  `hostProfileSchemaVersion` stays **2** and `cmd/villa/testdata/detect.golden.json`
  stays **byte-identical**, because the off-hardware test fixture leaves gfx-id /
  kernel / firmware unprobeable → both fields remain UNSET exactly as today.
  **Research must confirm** the detect golden is fixture-driven (not live-host
  derived); if any harness derives it from the live host, the new probes MUST be
  injectable/seam-stubbed so tests stay deterministic and the golden holds.

### Documentation reconciliation — evidence-checked, narrow
- **D-05:** Fix **exactly the audit-named set**, each corrected only after
  confirming the claim against its phase **VERIFICATION.md** (the source of
  truth), never green-washing:
  - Add `requirements-completed` SUMMARY frontmatter: DET-04 (07-03),
    BSET-01/02/03 (08-01/08-02), BENCH-02 (09-02/09-03), REC-05 (10-02).
  - Correct 06-REVIEW.md frontmatter (`status: issues_found`→resolved,
    `critical: 1`→0), citing the CR-01 fix commit `499644e`.
  - Refresh the stale REQUIREMENTS.md ROCM-02 tracking note.
  After the edits, **re-confirm the audit's 3-source cross-check passes**
  (REQUIREMENTS traceability ↔ VERIFICATION coverage ↔ SUMMARY frontmatter +
  integration wiring). **No prose rewriting** beyond frontmatter + the one stale
  note.

### Verification expectation — two-tier (off-hardware + on-hardware UAT)
- **D-06:** Item 1's success is proven in two tiers:
  - **Off-hardware (CI/unit):** prove the probe **wiring + no-false-green** —
    Unknown source facts → fields stay UNSET (golden byte-identical); a
    Known-good fixture → `KnownBool(true)`; a Known-bad fixture (denied
    firmware / non-gfx1151) → `KnownBool(false)`.
  - **On-hardware UAT:** confirm the live gfx1151 ROCm host now reports
    `firmware_date_ok` / `hsa_override_viable` as **real Known values** and the
    Phase-10 status/dashboard **readiness badge reads `ready` (non-`unknown`)** —
    closing the DASH-06 SC#1 residual. Flag for on-hardware UAT like Phases 8–10.
  Item 2 is verified purely off-hardware (frontmatter ↔ VERIFICATION.md cross-check).

### Claude's Discretion
- Exact firmware-date parse format and the precise `rpm` query string / fallback
  probe source (provided it's fixed-args, side-effect-free, and degrades to
  `UnknownBool` honestly per D-01).
- Whether the firmware floor/deny values are shared from preflight's embedded
  `rocm-policy.json` or duplicated detect-side (D-02) — planner/research call,
  provided no detect→preflight import cycle and no literal in `readiness_rocm.go`.
- The exact helper names / file placement for the two new probe bodies and the
  `gpu_amd.go` firmware seam, provided the seam/grep-gate discipline holds.
- The precise frontmatter key spellings used in the SUMMARY tags (match the
  existing `requirements-completed` convention already present in the green
  SUMMARYs, e.g. 06-02 / 07-01 / 09-01).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & the authoritative tech-debt list
- `.planning/v1.1-MILESTONE-AUDIT.md` — **the authoritative source** for both
  Phase-11 items: the `tech_debt` block names the rocm_readiness functional
  follow-up (P10) with the exact file+lines
  (`internal/detect/readiness_rocm.go:56,63`), the 6 missing SUMMARY frontmatter
  tags, the stale 06-REVIEW.md frontmatter (CR-01 fixed in `499644e`), and the
  stale REQUIREMENTS.md ROCM-02 note. Also defines what is OUT of scope (the ~13
  advisory hardening warnings + the Nyquist drafts).
- `.planning/ROADMAP.md` §"Phase 11: Address v1.1 tech debt" — the phase line
  scoping it to "rocm_readiness detect probes + doc reconciliation
  (post-milestone tech-debt cleanup)".
- `.planning/REQUIREMENTS.md` — DET-04 + DASH-06 (the requirements whose
  residual sub-clauses this phase closes); the stale ROCM-02 tracking note to
  refresh (D-05).

### Prior-phase decisions this phase honors (do NOT re-litigate)
- `.planning/phases/07-rocm-render-unit-preflight-detect/07-CONTEXT.md` —
  D-06/D-07/**D-08**: the `rocm_readiness` nested typed-Optional object,
  `hostProfileSchemaVersion` 1→2, and the **no-false-green** discipline this
  phase must preserve when the two probes become real.
- `.planning/phases/10-backend-tok-s-surfacing/10-CONTEXT.md` — D-04 (the
  tri-state readiness indicator that *consumes* these fields worst-wins; the
  badge that currently reads `unknown`); its `<deferred>` explicitly named
  "New detect ROCm probes (real firmware-date / HSA-override on-hardware checks)"
  as a separate effort — i.e. exactly this phase.

### Stack / policy constraints (firmware floor + deny, HSA override)
- `internal/preflight/rocm-policy.json` — the embedded policy: `firmwareFloor`
  `20260110`, `firmwareDeny` `["20251125"]`, `requiredHSAOverride` `11.5.1`,
  `kernelFloor` `6.18.4`. The values the firmware probe (D-02) compares against.
- `CLAUDE.md` §"Version Compatibility" / §"What NOT to Use" — linux-firmware
  ≥ 20260110 (avoid 20251125, breaks ROCm); ROCm needs
  `HSA_OVERRIDE_GFX_VERSION=11.5.1` on gfx1151; kernel floor 6.18.4. The
  real-world rationale behind the two probes.

### Code to extend / mirror
- `internal/detect/readiness_rocm.go` — `computeROCmReadiness` +
  `firmwareDateOK()` (line ~56) + `hsaOverrideViable()` (line ~63): the two stub
  bodies to make real (D-01/D-03), and the existing `rocminfoGfx1151` /
  `kernelFloorOK` patterns to mirror for KnownBool/UnknownBool discipline.
- `internal/detect/gpu_amd.go` — the seam home: `resolvedROCmImage` (276),
  `rocmImagePolicyOK` (282), `kernelMeetsROCmFloor` (305), and the
  `exec.Command` pattern (243) + `os.ReadFile` (480). Add the firmware-date probe
  source + `firmwareDatePolicyOK` seam here (D-02) so no literal lands in
  `readiness_rocm.go`.
- `internal/detect/profile.go` — `FirmwareDateOK Bool` (76) +
  `HSAOverrideViable` fields on `HostProfile` / `ROCmReadiness` (already exist;
  values get populated, no struct change — D-04).
- `cmd/villa/testdata/detect.golden.json` — must stay **byte-identical**
  off-hardware (D-04); the regression guard that the probes don't perturb the
  off-hardware contract.
- `internal/preflight/checks_rocm.go` — `checkROCmFirmware` (116) /
  `checkROCmHSA` (144): the preflight advisory shape these probes mirror; useful
  to keep verdict semantics consistent (denylist match → fail; floor → warn).
- SUMMARY frontmatter targets (D-05): `.planning/phases/07-*/07-03-SUMMARY.md`,
  `.planning/phases/08-*/08-01-SUMMARY.md`, `08-02-SUMMARY.md`,
  `.planning/phases/09-*/09-02-SUMMARY.md`, `09-03-SUMMARY.md`,
  `.planning/phases/10-*/10-02-SUMMARY.md`; review-frontmatter target:
  `.planning/phases/06-*/06-REVIEW.md`. Verify each against the sibling
  `*-VERIFICATION.md` before tagging.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`detect` seam pattern (`gpu_amd.go`)** — `kernelMeetsROCmFloor`,
  `rocmImagePolicyOK`, `resolvedROCmImage` already isolate every ROCm literal
  behind detect-local helpers so `readiness_rocm.go` stays literal-free and
  grep-gate-safe. The firmware-date probe + its floor/deny compare slot straight
  into this pattern (D-02).
- **`exec.Command` host probing** — detect already shells out (fixed args) to
  `rocminfo`/`vulkaninfo` (`gpu_amd.go:243`) and reads `/sys` + `/proc` files;
  the firmware-date probe (rpm query or firmware version-file read) is the same
  shape, not a new I/O mechanism (D-01).
- **`KnownBool` / `UnknownBool` (the typed-Optional `Bool`)** — the no-false-green
  primitive Phase 7 standardized; both new probes return it so off-hardware =
  honest UNSET, on-hardware = real Known. `rocminfoGfx1151` / `kernelFloorOK` are
  the exact mirror templates.
- **Already-computed readiness facts** — `computeROCmReadiness` already threads
  the gfx-id `Str` + kernel `Str`; `hsaOverrideViable` (D-03) derives from those
  without new reads.

### Established Patterns
- **No-false-green / typed-Unknown (D-08)** — unevaluable = UNSET, distinct from
  a real false; the hard invariant the probes must not break (off-hardware stays
  UNSET → golden byte-identical).
- **Append-only contract + frozen goldens** — this phase is the rare case that
  touches NEITHER: no field added, golden unchanged (D-04). The discipline shows
  up as a *regression guard*, not a re-freeze.
- **Literal-behind-seam + grep-gate** — `TestSeamGrepGate` fails if ROCm marker
  literals leak; keep the firmware floor/deny + HSA values in `gpu_amd.go`, never
  in `readiness_rocm.go` (D-02).
- **Evidence-first doc edits** — every reconciliation edit (D-05) is confirmed
  against the phase VERIFICATION.md before writing, mirroring how the audit
  itself cross-referenced 3 sources.

### Integration Points
- `firmwareDateOK()` / `hsaOverrideViable()` → `computeROCmReadiness` →
  `ROCmReadiness` sub-tree → `detect --json` `rocm_readiness` → Phase-10
  `foldROCmReadiness` (status/dashboard) → the readiness **badge** that today
  reads `unknown`. Making the two leaves Known on-hardware flips the badge to
  `ready` without any change to the Phase-10 fold (it already consumes worst-wins).
- New firmware probe ← `rocm-policy.json` floor/deny values (shared or
  duplicated, D-02) → `firmwareDatePolicyOK` seam in `gpu_amd.go`.
- Doc edits (D-05) → SUMMARY/REVIEW/REQUIREMENTS frontmatter ↔ VERIFICATION.md
  cross-check → the audit's automated 3-source frontmatter check passes.

</code_context>

<specifics>
## Specific Ideas

- The firmware probe should degrade like preflight's `checkROCmFirmware` already
  reasons: a **Known denied build (20251125) → fail**, a **Known sub-floor date →
  fail/warn**, a **Known clear date → ok**, **unprobeable → UNSET** (never assert
  a value it can't read). Keep the detect verdict and the preflight verdict
  telling the same story.
- HSA-override viability is a *substrate-readiness* statement, not "the env var is
  currently exported": "this host is a gfx1151 where the 11.5.1 override applies
  and ROCm can enumerate it" → viable. Don't conflate it with whether the running
  container happens to have the env set (the render unit owns that, Phase 7).
- The on-hardware UAT acceptance is concrete and quotable: on the live gfx1151
  ROCm host, `villa detect --json` shows `firmware_date_ok: true` +
  `hsa_override_viable: true`, and the dashboard/`villa status` ROCm-readiness
  badge reads **`ready`** (not `unknown`) — the exact DASH-06 SC#1 residual.
- Doc reconciliation is a "make the green truth visible" task, not a re-judgement:
  the requirements are already SATISFIED + WIRED; the edits only stop the
  automated 3-source check from flagging tagging lag.

</specifics>

<deferred>
## Deferred Ideas

- **The ~13 advisory hardening warnings** (P8 ×5: fit-check vs ROCm ~64 GB cap,
  /props drift overlay in liveProve, host-wide GTT floor, WARN-vs-FAIL collapse,
  double config-load TOCTOU; P7 ×4: --security-opt last-wins, firmware sub-floor
  PASS, `strings.Cut` ok ignored, os.Exit in RunE; + MesaFloor embedded but
  unconsumed; P6 ×4 by-design advisories) — not named in the ROADMAP Phase-11
  line; a separate hardening cleanup effort (own phase / `/gsd-audit-fix`).
- **Nyquist VALIDATION.md draft reconciliation** (P6/P8/P9/P10
  `nyquist_compliant:false` / draft despite full green suites) —
  process/validation-status debt; run `/gsd-validate-phase N` per phase if formal
  Nyquist sign-off is wanted before archive.
- **Turning the preflight firmware/HSA checks into live probes too** — Phase 11
  scopes the *detect* path (`readiness_rocm.go`); whether preflight's
  `checkROCmFirmware`/`checkROCmHSA` should consume the same new probed facts
  (instead of degrading to WARN) is a natural follow-up but not named here. If
  the probe lands as a shared `detect` fact, a thin preflight follow-up could
  consume it — note for a future small phase.

None of these were raised as new scope — the `--auto` pass stayed within the
two named Phase-11 items.

</deferred>

---

*Phase: 11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco*
*Context gathered: 2026-06-06*
</content>
</invoke>
