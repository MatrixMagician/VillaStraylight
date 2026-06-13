---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
plan: 03
subsystem: testing
tags: [qualification, on-hardware, gfx1151, llama-cpp, crush, vulkan, tool-call, cache-reuse, kv-measurement, evidence]

# Dependency graph
requires:
  - phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
    plan: 01
    provides: catalog schema 3 with three role:"coder" seed entries (ids + revision+sha256 pins) — the qualification pull targets
  - phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
    plan: 02
    provides: agent_ctx-locked coder fit math (computed KV/total estimates the on-hardware measurements validate)
provides:
  - On-hardware agent-in-the-loop qualification evidence for all three coder entries (CODER-03, D-08/D-09) — pinned-digest server-version proof, measured KV/GTT vs computed, tool-call smoke, real Crush v0.76.0 read→edit→verify transcripts, cache-reuse probe, explicit VERDICT per entry
  - Three VERDICT: PASS verdicts on build 9496 (qwen3-coder-30b-a3b @65536, qwen3-coder-next-q4 @131072, qwen3-coder-next-q3 @131072), each offload 49/49
  - FINDING A2 — measured DeltaNet recurrent-state constant (RS buffer = 301.50 MiB, ctx/quant-independent) for plan 24-04 min_envelope_bytes consideration
  - FINDING A3 — cache-reuse on the Next hybrids is true via context checkpoints (not KV shifting); cache_reuse_safe claim deferred to 24-04 ratification
  - Operator-approved evidence set released to plan 24-04 (REVIEW.md)
  - Reproducible qualification harness (qualify.sh + qual crush.json) — phase-dir artifacts, NOT product code
affects: [24-04 reconciliation + D-11 toolbox decision, phase-25 swap verb, phase-26 agent delivery, phase-27 villa verify agent]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "On-hardware qualification = serve-by-digest + measure KV/GTT + tool-call smoke + real agent loop + cache-reuse probe + explicit VERDICT; benchmark scores never qualify alone (D-08)"
    - "Offload-asserting on hardware: a verdict requires offloaded N/N residency lines, never a health-200 / is-active proxy"
    - "Crush qualification auto-accept via config permissions.allowed_tools (strictly tighter than --yolo) — model fault vs harness fault isolated by the smoke test passing first"

key-files:
  created:
    - .planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/qualification/REVIEW.md
  modified: []

key-decisions:
  - "All three coder entries PASS on the PINNED vulkan-radv digest (build 9496/94a220cd6), never the drifted tag (build 9579) — recorded per run via llama-server --version"
  - "Measured KV is an exact match to the computed fit math for all three entries (6.00 / 3.00 / 3.00 GiB); the Next n_layers:12 hybrid encoding is validated (Pitfall 2 ruled out — NOT ~12 GiB)"
  - "FINDING A2: DeltaNet recurrent-state buffer = 301.50 MiB (4 slots × ~75.4 MiB/seq), ctx- and quant-independent — candidate min_envelope_bytes constant for 24-04"
  - "FINDING A3: cache-reuse on the Next hybrids came back true (not the expected false) — build 9496 reuses via recurrent-state context checkpoints (75.376 MiB), not n_cache_reuse chunk reuse; cache_reuse_safe claim ratified in 24-04"

patterns-established:
  - "Qualification evidence directory shape: server-version.txt, kv-gtt.txt, smoke.json, crush-transcript.txt, cache-reuse.txt, server.log, verdict.md per entry — verdict.md ends in a single VERDICT: line + a cache-reuse: line"

requirements-completed: [CODER-03]

# Metrics
duration: ~3h (on-hardware; 3 GGUF loads ~104 GB + 3 full protocol runs across 2 sessions)
completed: 2026-06-13
---

# Phase 24 Plan 03: On-Hardware Coder Model Qualification Summary

**All three coder catalog entries (qwen3-coder-30b-a3b @65536, qwen3-coder-next-q4 + -q3 @131072) PASS real agent-in-the-loop qualification on the gfx1151 box via the pinned build-9496 toolbox digest — full 49/49 offload, exact-match KV, real Crush read→edit→verify loops — operator-approved and released to 24-04.**

## Performance

- **Duration:** ~3h on-hardware (3 GGUF loads totalling ~104 GB + three full Pattern-4 protocol runs, spanning two sessions 2026-06-12/13)
- **Started:** 2026-06-12 (Task 1 pre-stage)
- **Completed:** 2026-06-13T07:06Z (operator approval recorded)
- **Tasks:** 4 (3 auto + 1 blocking human-verify checkpoint, APPROVED)
- **Files modified:** 24 evidence files across three entry directories + qualify.sh, crush.json, REVIEW.md

## Accomplishments

- **Three VERDICT: PASS** on-hardware qualifications, every run served BY DIGEST `sha256:9a74e555…` (build 9496/94a220cd6) — the drifted `vulkan-radv` tag (build 9579) never leaked in (T-24-07 gate green via per-run `llama-server --version`).
- **Full offload (49/49) on all three** — offload-asserting, never liveness; a partial offload would have FAILed the entry/ctx.
- **Measured KV is an exact match to the computed fit math** for every entry: 30B `6144.00 MiB = 6.00 GiB`; both Next `3072.00 MiB = 3.00 GiB`. The Next `n_layers:12` hybrid KV encoding is validated on real hardware (Pitfall 2 ruled out).
- **Real Crush v0.76.0 agent loops** drove a genuine read→edit→verify task (fix a seeded `i < n` → `i <= n` off-by-one until `go test ./...` goes green) with ≥3 distinct executed tool types, zero 5xx while tools present, no narrated XML tool calls, no Vulkan allocation failure, clean self-termination.
- **Tool-call smoke** passed both variants per entry (standard: HTTP 200 + STRING `arguments`; no-`properties`: must-not-500 → HTTP 200) — cheap disqualifier cleared before each agent loop.
- **Two findings captured for 24-04**: A2 (DeltaNet RS constant = 301.50 MiB, ctx/quant-independent) and A3 (cache-reuse true via context checkpoints, not KV shifting).
- **Chat stack restored green** after qualification (`villa-llama.service` restarted; `./villa status` green with OFFLOAD asserted).
- **Operator reviewed and APPROVED** the full evidence set; released to plan 24-04 for reconciliation + the D-11 toolbox decision.

## Task Commits

1. **Task 1: Pre-stage the qualification harness** (Crush v0.76.0 sha256-verified to /tmp, scratch repo with seeded failing test, qual config + script, 3 GGUFs ~104 GB pulled + sha256-verified) — `e012887` (chore)
2. **Task 2: Qualify qwen3-coder-30b-a3b @65536** (full protocol + cache-reuse probe; VERDICT: PASS, offload 49/49, KV 6.00 GiB exact, cache-reuse true) — `750ee17` (docs)
3. **Task 3: Qualify qwen3-coder-next-q4 + -q3 @131072; restore chat stack** (both VERDICT: PASS, offload 49/49, KV 3.00 GiB exact each, stack restored green) — `7d63df3` (docs)
4. **Task 4: Operator review of qualification evidence + restored stack** (blocking checkpoint, APPROVED — evidence released to 24-04) — `abd4c80` (docs)

**Plan metadata:** see final `docs(24-03): complete` commit (SUMMARY.md, STATE.md, ROADMAP.md, REQUIREMENTS.md).

_Note: Tasks 2–4 are evidence/documentation commits — the verdicts and probe results live on disk under `qualification/`, no product code touched._

## Files Created/Modified

- `qualification/qualify.sh` - parameterized per-entry Pattern-4 protocol script (serve-by-digest, measure KV/GTT, dual smoke variants, agent loop, cache-reuse probe). Phase-dir artifact, NOT product code. (Task 1)
- `qualification/crush.json` - qualification provider config: kill switches (`disable_metrics`, `disable_provider_auto_update`), loopback `127.0.0.1:8081`, villa-unique model id. (Task 1)
- `qualification/qwen3-coder-30b-a3b/` - 7 evidence files (server-version, kv-gtt, smoke.json, crush-transcript, cache-reuse, server.log, verdict.md) — VERDICT: PASS. (Task 2)
- `qualification/qwen3-coder-next-q4/` - same 7-file shape — VERDICT: PASS @131072 (D-12 ship gate). (Task 3)
- `qualification/qwen3-coder-next-q3/` - same 7-file shape — VERDICT: PASS @131072 (D-12 ship gate). (Task 3)
- `qualification/REVIEW.md` - operator approval record + findings carried to 24-04. (Task 4)

## Decisions Made

- **Pinned digest only (T-24-07):** every serve ran `docker.io/kyuz0/amd-strix-halo-toolboxes@sha256:9a74e555…` (build 9496). The local `vulkan-radv` tag has drifted to build 9579; the per-run `--version` proof confirms the pin held on all three runs.
- **cache_reuse_safe for the Next entries deferred to 24-04 (A3):** the probe verdict is `true` by the stated criteria, but the reuse mechanism is recurrent-state context checkpointing, not `n_cache_reuse` chunk reuse — the nuance is recorded so 24-04 ratifies the catalog claim deliberately, not by assumption.
- **DeltaNet RS constant (A2) measured, not assumed:** 301.50 MiB at the default 4 slots, identical across Q4 and Q3 — a material, ctx-independent constant 24-04 may fold into `min_envelope_bytes`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Crush v0.76.0 rejects `--yolo` with the `run` subcommand**
- **Found during:** Task 2 (30B agent loop) — confirmed and reused for the Next runs
- **Issue:** The plan/RESEARCH assumed `crush run --yolo` (assumption A5); Crush v0.76.0 treats `--yolo` as a root-local cobra flag that the `run` subcommand rejects, blocking the non-interactive agent loop.
- **Fix:** Implemented non-interactive auto-accept via the qual `crush.json` `permissions.allowed_tools` list (view/ls/grep/glob/edit/write/bash) — strictly TIGHTER than `--yolo` (no agent/fetch/download auto-accept), so T-24-09 (workspace-escape mitigation) is unchanged. Model fault vs harness fault isolation held: the tool-call smoke test passed before any agent attempt.
- **Files modified:** `qualification/crush.json`, `qualification/qualify.sh` (phase-dir harness only — no product code)
- **Verification:** All three agent loops completed with ≥3 distinct executed tool types and green final `go test`; transcripts on disk.
- **Committed in:** `750ee17` (Task 2) / `7d63df3` (Task 3)

---

**Total deviations:** 1 auto-fixed (1 blocking). No product code (`internal/`, `cmd/`) touched — confirmed `git status --porcelain -- internal cmd` empty (TestSeamGrepGate untouchable by construction).
**Impact on plan:** The auto-fix was necessary to run the agent loop at all and tightened the security posture rather than loosening it. No scope creep.

## Issues Encountered

- **Cache-reuse came back true on the Next hybrids (expected false, A3).** Resolved by investigation, not assumption: scraped the journal and the API `timings.cache_n`, then traced the mechanism to recurrent-state context checkpoints (`context checkpoints enabled, max = 32, min spacing = 256`; `restored context checkpoint … size = 75.376 MiB`). Recorded the full nuance in both Next verdict.md files and REVIEW.md; the catalog ratification decision is explicitly 24-04's.

## User Setup Required

None - no external service configuration required. Qualification artifacts (Crush) live in `/tmp` only and are never installed into PATH or shipped (D-08).

## Threat Flags

None - no security-relevant surface introduced beyond the plan's `<threat_model>`. The qualification ran loopback-only (`127.0.0.1:8081`), models volume read-only, sanctioned outbound limited to the Crush tarball + GGUF pulls; the Crush tool stayed `/tmp`-scoped with `--cwd` pinned to the disposable scratch repo.

## Next Phase Readiness

- **Plan 24-04 (reconciliation + D-11) is unblocked:** three operator-approved PASS verdicts on real hardware, with A2/A3 findings and the measured-vs-computed deltas ready to fold into the catalog and the D-11 toolbox decision record. Build 9579 (locally present, digest-pinnable) was the pre-identified re-pin fallback but is NOT needed — build 9496 qualified all three entries, including full Qwen3-Next/DeltaNet arch support.
- **No blockers.** Chat stack is restored and green.

## Self-Check: PASSED

- All claimed files exist on disk (SUMMARY, REVIEW.md, three verdict.md).
- All task commits present in git history (`e012887`, `750ee17`, `7d63df3`, `abd4c80`, `a8dbf45`).
- `git status --porcelain -- internal cmd` empty — zero product-code changes (TestSeamGrepGate untouched).

---
*Phase: 24-coder-fit-math-catalog-on-hardware-model-qualification*
*Completed: 2026-06-13*
