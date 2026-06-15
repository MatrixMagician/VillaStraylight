---
phase: 28-agent-surfacing-contracts
plan: 01
subsystem: doctor
tags: [doctor, coding-agent, residency, drift, honesty-by-construction, golden-contract]
requires:
  - "internal/doctor.Aggregate worst-wins fold + offload-FAIL-dominates switch (residencyUnderLoadFinding)"
  - "internal/agent.DetectDrift / DriftReport + agent.Render + agent.LoadCrushPolicy"
  - "cmd/villa liveAgentToolCallProbe (install_agent.go) — the reused crush-run round-trip"
  - "inference.RunningOffloadVerdict + Backend.ResidencyProof() (consumed opaquely)"
provides:
  - "internal/doctor: agentToolCallFinding / agentResidencyFinding / agentDriftFindings mappers"
  - "internal/doctor.Deps: AgentToolCall / AgentResidencyUnderLoad / AgentDrift nil-safe seams"
  - "cmd/villa.liveDoctorDeps agent-seam wiring gated on cfg.AgentEnabled"
  - "doctor reportSchemaVersion 2 (append-only) + doctor-agent.json.golden fixture"
affects:
  - "villa doctor --json contract (doctor's OWN schema, bumped 1->2; status.Report UNTOUCHED)"
tech-stack:
  added: []
  patterns:
    - "Pure-core + injectable nil-safe Deps seam (a nil seam emits NO finding — no PASS-by-default)"
    - "offload-FAIL-dominates switch clone (D-07 honesty dominance: confident agent FAIL > health-200)"
    - "Append-only byte-frozen golden, single doctor-own schema bump (1->2)"
    - "Backend-literal seam lock (inference.Verdict consumed opaquely; TestSeamGrepGate green)"
key-files:
  created:
    - "cmd/villa/testdata/doctor-agent.json.golden"
  modified:
    - "internal/doctor/doctor.go"
    - "internal/doctor/doctor_test.go"
    - "cmd/villa/doctor.go"
    - "cmd/villa/doctor_test.go"
    - "cmd/villa/testdata/doctor.json.golden"
    - "cmd/villa/testdata/doctor-memory.json.golden"
decisions:
  - "Agent drift is WARN-only (never BLOCK FAIL): a drifted/absent binary or hand-edited config is an operator decision surfaced with remediation, never auto-corrected (D-14)."
  - "AgentResidency keys on the SERVED coder model file (sd.ModelFile resolves cfg.Model, which IS the coder under coding mode — D-09 distinct served model)."
  - "AgentToolCall probe error/non-completion -> StatusFail (a confident failure to drive an ENABLED agent is a real fault, not an unevaluable signal); only precondition gaps in residency -> typed-Unknown WARN."
metrics:
  duration_min: 41
  completed: 2026-06-15
  tasks: 2
  files_changed: 6
  commits: 3
---

# Phase 28 Plan 01: Doctor Coding-Agent Fold Summary

Folded the coding-agent health checks — a real `crush run` tool-call round-trip, under-load coder-model residency, and binary/version + config drift — into the existing `villa doctor` core, composed worst-wins so a confident agent offload/residency FAIL DOMINATES a healthy-looking HTTP-200, with agent-off output byte-identical except the doctor schema bump.

## What Was Built

**Task 1 (TDD) — pure core (`internal/doctor`):**
- `agentToolCallFinding(inference.Verdict)` (ID `agent-tool-call`) and `agentResidencyFinding(inference.Verdict)` (ID `agent-residency`): exact clones of the `residencyUnderLoadFinding` offload-FAIL-dominates switch — StatusPass → BLOCK/PASS, StatusFail → BLOCK/FAIL+remediation, StatusWarn → typed-Unknown WARN+remediation (D-07). `inference.Verdict` consumed OPAQUELY (Status/Detail/Remediation only).
- `agentDriftFindings(agent.DriftReport) []Finding`: BinaryAbsent → WARN+Phase-27 install remediation; BinaryDriftUnknown → typed-Unknown WARN; BinaryDrift → WARN+re-install remediation; ConfigDrift → WARN+review/re-render remediation; **ConfigAbsent ALONE → NO finding** (first-run render trigger, not drift); all-clean → a single PASS `agent-drift` finding. Surfaced, never auto-corrected (D-14).
- Three nil-safe `Deps` seams (`AgentToolCall`, `AgentResidencyUnderLoad func() inference.Verdict`, `AgentDrift func() agent.DriftReport`) folded in `Aggregate` mirroring the residency fold — a nil seam emits NO finding (no PASS-by-default).
- `reportSchemaVersion` 1→2 (append-only) + version-history comment.

**Task 2 — live wiring (`cmd/villa/doctor.go`):**
- `liveDoctorDeps` binds the three agent seams ONLY when `cfg.AgentEnabled` (mirroring the `cfg.MemoryEnabled` conditional); all three stay nil when off.
- `AgentToolCall` reuses `liveAgentToolCallProbe` (the SAME read→edit `crush run` driver `verify_agent.go` wires as `agentTaskFn`) and maps completed→Pass / not-completed-or-err→Fail.
- `AgentResidencyUnderLoad` (`runAgentResidencyUnderLoad`) launches the reused tool-call probe in flight, samples `inference.RunningOffloadVerdict` over the exact `liveStatusDeps` input set keyed on the served coder model file (`sd.ModelFile`), then JOINs the probe (drive→sample→join; no process outlives the call).
- `AgentDrift` (`liveAgentDrift`) assembles `agent.DetectDrift` inputs from the `code.go` accessors (`agentBinPath`/`hashFileSHA256`/`crushConfigPath`) + `agent.Render` reference + `agent.LoadCrushPolicy().Assets["linux/amd64"].BinarySHA256`; any read error → typed-Unknown WARN report (never a fabricated drift).
- Goldens refrozen 1→2 (append-only); new `doctor-agent.json.golden` fixture; `TestLiveDoctorDepsWiresAgentSeams` asserts the gating.

## Deviations from Plan

None — plan executed exactly as written. (`go fmt` reformatted one `var` block's alignment in `cmd/villa/doctor.go`; cosmetic, no behavior change.)

## Verification

- `go test ./internal/doctor/... -count=1` — 36 passed (new mappers + nil-safe seams + worst-wins fold + schema=2).
- `go test ./cmd/villa/... -run 'TestDoctor|TestSeamGrepGate' -count=1` — 12 passed (agent golden + gating + exit mapping).
- `go test ./internal/inference/... -run TestSeamGrepGate -count=1` — passed (no backend marker literal leaked into doctor/cmd).
- `make check` (vet + full `go test ./...`) — all packages green.
- Append-only confirmed: the only diff to existing JSON goldens (`doctor.json.golden`, `doctor-memory.json.golden`) is `schema_version` 1→2; nothing above it moved. The human-table goldens (`doctor-pass`/`-warn`/`-rocm-superseded`/`-memory-pass`/`-memory-residency-fail`) are byte-unchanged (they do not render schema_version).
- No `status.Report` schema bump in this plan (isolated to Plan 03).

## Success Criteria

- [x] SURF-02: `villa doctor` folds agent binary/version drift, config drift, a real tool-call round-trip probe, and under-load coder residency.
- [x] D-07 honesty dominance: a confident agent offload/residency FAIL dominates a healthy-looking HTTP-200 (proved by `TestAgentToolCallFoldedFailRaisesOverall` / `TestAgentResidencyFoldedFailRaisesOverall`).
- [x] Agent-off output byte-identical except the doctor schema_version bump (append-only).
- [x] TestSeamGrepGate green (no backend marker literal in doctor/cmd).

## Known Stubs

None. No hardcoded empty data flows to output; every agent finding is data-driven from a live seam or a report-only drift outcome. The agent-on goldens are hand-built fixtures rendered through the real `renderDoctor` path (the established doctor test convention), not stubbed product code.

## Self-Check: PASSED
