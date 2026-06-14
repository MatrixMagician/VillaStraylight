---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 06
subsystem: cmd/villa verify-agent + orchestrate in-network endpoint seam
gap_closure: true
tags: [PRIV-06, WR-01, WR-06, honesty, negative-control, seam-accessor, dns-lockstep]
requires:
  - "internal/orchestrate.containerName (villa-llama DNS name)"
  - "internal/inference serverPort (8080)"
  - "cmd/villa runProbeCurl / liveAgentVerify / evalAgentVerify (Plan 27-03)"
provides:
  - "orchestrate.LlamaInNetworkEndpoint() — single-source in-network villa-llama URL"
  - "inference.ServerPort() — one-line port accessor"
  - "cmd/villa.classifyEgressProbe — pure egress blocked/infra-fail classifier"
  - "cmd/villa.runProbeCurlCode — exit-code-aware probe curl"
  - "cmd/villa.runLlamaDownControl + restoreLlamaWarning — surfaced restore failure"
affects:
  - "villa verify agent (PRIV-06 runtime strictly-local proof)"
tech-stack:
  added: []
  patterns:
    - "Single exported seam accessor composing two owning-package constants (DNS/port lockstep, Pitfall 3 / T-4-01)"
    - "Pure classifier extracted from an un-driveable live exec closure (off-hardware testable)"
    - "Named-return deferred capture to surface a restore error without dropping it"
key-files:
  created:
    - internal/orchestrate/endpoint.go
    - internal/orchestrate/endpoint_test.go
  modified:
    - internal/inference/backend_vulkan.go
    - cmd/villa/install_memory.go
    - cmd/villa/verify_agent.go
    - cmd/villa/verify_agent_test.go
decisions:
  - "WR-06 wired via option (b): a failed restore DOWNGRADES a would-be PASS to FAIL with the remediation in the verdict detail (printed to stderr by runVerifyAgent) — more honest than a warning-only path and avoids a churny io.Writer signature change across the verifyFn seam"
  - "Port sourced via a new inference.ServerPort() accessor (not the minimal fallback) so the accessor body hard-codes neither host nor :8080 — full single-source lockstep with both the rendered ContainerName= and the inference port"
metrics:
  duration: ~12 min
  tasks: 3
  files: 6
  completed: 2026-06-14
---

# Phase 27 Plan 06: Close WR-01 + WR-06 (verify-agent honesty defects) Summary

Closed the two `cmd/villa/verify_agent.go` honesty defects flagged in 27-VERIFICATION.md: the egress negative control no longer false-greens PRIV-06 on a broken probe environment (and no longer false-FAILs healthy hosts), and a failed villa-llama restore after the llama-down control is now surfaced with manual remediation instead of being silently swallowed.

## What was built

**Task 1 — `orchestrate.LlamaInNetworkEndpoint()` seam accessor (WR-01 prerequisite), commit `3256747`:**
- New `internal/orchestrate/endpoint.go`: `LlamaInNetworkEndpoint() string` returns `http://villa-llama:8080/v1`, composed from the orchestrate `containerName` constant + `inference.ServerPort()` — no re-typed `villa-llama:8080` host literal, no hard-coded `:8080` in the accessor body. This is the in-network analogue of inference's host-loopback `endpointURL`, mirroring the openwebui.go in-network composition and the `EmbedImage()`/`QdrantImage()` one-line-accessor shape.
- New `internal/inference` `ServerPort() int` one-line accessor (the port was unexported `serverPort = 8080`).
- `TestLlamaInNetworkEndpoint` proves the URL equals the rendered shape AND is built in DNS/port lockstep (prefix from `containerName`, contains `inference.ServerPort()`), so a future container rename or port change is caught by the test, not a drifted literal.

**Task 2 — egress negative control fails on broken probe infra (WR-01), commit `69ed87c`:**
- New pure `cmd/villa.classifyEgressProbe(sanityErr, externalExitCode, externalErr) (blocked, err)`: ANY sanity error → infra FAIL (never `blocked=true`); curl 6/7/28 → `blocked=true`; exit 0 → `blocked=false`; unclassified non-zero / never-started (-1) → infra FAIL.
- Rewrote the `egressBlocked` closure: runs a POSITIVE in-network sanity probe to `orchestrate.LlamaInNetworkEndpoint()+"/models"` FIRST (proving podman/network/helper-image/curl/villa-llama all work), then the exit-classified external probe. Removed the defective `return err != nil, nil`.
- New additive `cmd/villa.runProbeCurlCode` exposing the curl/container exit code via `errors.As` on `*exec.ExitError` (never-started → -1). `runProbeCurl` unchanged for existing callers.
- Tests: `TestClassifyEgressProbe` (7-row truth table) + `TestEvalAgentVerifyInfraErrorFails` (composed infra-error → `StatusFail` "could not run the egress negative-control probe").

**Task 3 — surface the llama-down restore failure (WR-06), commit `ce8a0f5`:**
- New pure `cmd/villa.runLlamaDownControl(stop, start, task)`: STOP → run task → ALWAYS attempt restore (deferred Start), CAPTURING the Start error via a named return (a stop failure short-circuits — nothing stopped, nothing to restore). Removed the discarded deferred Start.
- New `cmd/villa.restoreLlamaWarning(service, rerr)`: empty on clean restore; otherwise a message naming the service left stopped with the literal `systemctl --user start villa-llama.service` remediation.
- `liveAgentVerify` captures `restoreErr` and folds it into the verdict (wiring b): a would-be PASS with a failed restore downgrades to `StatusFail` carrying the remediation; an already-FAILed verdict gets the warning appended. The detail prints to stderr via the existing `runVerifyAgent` path.
- Tests: `TestRestoreLlamaWarning` (message contract) + `TestLlamaDownRestore` (restore-failure surfaced, restore still attempted, stop-failure short-circuit).

## Verification

- `go test ./internal/orchestrate/ -run TestLlamaInNetworkEndpoint -count=1` — PASS.
- `go test ./internal/orchestrate/ -count=1` — PASS (no golden update; additive accessor).
- `go test ./cmd/villa/ -run 'TestClassifyEgressProbe|TestEvalAgentVerify|TestLlamaDownRestore|TestRestoreLlama|TestVerifyAgent' -count=1` — all PASS (verbose run confirmed every subtest).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — PASS (helper image only from `orchestrate.EmbedImage()`, in-network URL only from `orchestrate.LlamaInNetworkEndpoint()`; no leaked literal).
- `make check` (vet + full suite) — all packages ok. `make lint` (falls back to go vet) — clean.
- Manual grep: `verify_agent.go` no longer contains `return err != nil, nil` nor `_ = deps.systemd.Start(...)`; the only `127.0.0.1` occurrence is an explanatory comment; the sanity probe consumes `orchestrate.LlamaInNetworkEndpoint()` (no host literal).

## Deviations from Plan

None — plan executed exactly as written. The plan offered two WR-06 wirings; option (b) (verdict downgrade) was chosen as the plan's stated more-honest alternative and to avoid threading an `io.Writer` through the `verifyFn` seam. Both `runLlamaDownControl` and `restoreLlamaWarning` helpers (the plan's suggested extraction points) were created and unit-tested directly.

## Known Stubs

None. The pure `evalAgentVerify` core was intentionally left unchanged (it already mapped an `egressBlocked` error to FAIL); all three fixes are in the live seam / new pure helpers.

## Threat Flags

None — no new network endpoint, auth path, file access, or schema change was introduced. The changes harden two existing trust boundaries (T-27-15 egress negative control, T-27-16 llama-down restore) per the plan's threat register.

## Self-Check: PASSED

- Created files exist: `internal/orchestrate/endpoint.go`, `internal/orchestrate/endpoint_test.go`, `27-06-SUMMARY.md`.
- Commits exist: `3256747` (Task 1), `69ed87c` (Task 2), `ce8a0f5` (Task 3).
