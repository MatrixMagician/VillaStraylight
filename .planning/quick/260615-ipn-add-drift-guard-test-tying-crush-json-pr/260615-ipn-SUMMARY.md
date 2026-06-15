---
phase: quick-260615-ipn
plan: 01
subsystem: agent / inference seam
tags: [test, drift-guard, cross-seam, crush.json, v1.4-audit]
requires:
  - internal/agent.Render (crush.json renderer)
  - internal/inference.ServerPort() (port accessor)
provides:
  - cmd/villa.TestCrushProviderPortMatchesInferenceServerPort (cross-seam port drift guard)
affects:
  - cmd/villa/code_test.go
  - internal/agent/render.go (comment-only)
  - internal/inference/backend_vulkan.go (comment-only)
tech-stack:
  added: []
  patterns:
    - "Cross-seam coupling asserted from the test tier when a seam invariant forbids the production import (mirrors orchestrate.LlamaInNetworkEndpoint() precedent)"
key-files:
  created: []
  modified:
    - cmd/villa/code_test.go
    - internal/agent/render.go
    - internal/inference/backend_vulkan.go
decisions:
  - "Guard lives in cmd/villa (imports BOTH agent + inference), not internal/agent — the agent core's no-inference seam is LOCKED; the 8080 literal in render.go stays SANCTIONED and unchanged"
metrics:
  duration: ~6 min
  completed: 2026-06-15
---

# Quick Task 260615-ipn: crush.json provider-port drift guard Summary

A new cmd/villa test (`TestCrushProviderPortMatchesInferenceServerPort`) ties the rendered crush.json provider baseURL port to `inference.ServerPort()`, failing if `render.go`'s hard-coded `:8080` desyncs from the served inference port — closing the v1.4 milestone-audit WARN without violating the agent core's no-inference import seam.

## What Was Done

- Added `TestCrushProviderPortMatchesInferenceServerPort` to `cmd/villa/code_test.go`, co-located with the existing `agent.Render` / `renderRef` usage. It renders `agent.Render(cfg, nil)` and asserts the bytes contain `fmt.Sprintf("127.0.0.1:%d/v1", inference.ServerPort())` via `bytes.Contains`, with a `t.Errorf` naming both sources and the remedy.
- Added `fmt` and `github.com/MatrixMagician/VillaStraylight/internal/inference` to the test import block (the latter is the import FORBIDDEN inside `internal/agent`, which is exactly why the binding lives in `cmd/villa`).
- A doc comment above the test explains WHY it lives in `cmd/villa` (the agent core's LOCKED no-inference seam, `agent.go:17-24`) and references the `orchestrate.LlamaInNetworkEndpoint()` precedent.
- Two comment-only cross-reference one-liners: `render.go`'s `providerBaseURL` const and `inference.ServerPort()`'s doc comment now each note the drift guard. No code, no rendered bytes, no golden, no new import changed by these.

## Guard Proven to Bite

Temporarily flipped `serverPort` 8080→9090 in `internal/inference/backend_vulkan.go`: the new test FAILED (`...does not embed inference.ServerPort()=9090...`). Reverted to 8080; the test PASSES. The flip was NOT committed.

## Verification

- `go test ./cmd/villa -run TestCrushProviderPortMatchesInferenceServerPort -v` → PASS (1 passed).
- `go test ./internal/inference -run TestSeamGrepGate` → PASS (no backend marker leaked, no literal moved).
- `go list -deps ./internal/agent | grep internal/(inference|detect)` → empty — the agent core still imports NEITHER (the earlier `grep -rn` hits were comment-only mentions, not imports).
- `make check` (go vet + full `go test ./...`) → PASS, EXIT=0.
- No `testdata/*.golden*` changed; `git status` shows only the three intended files.

## Deviations from Plan

None — plan executed exactly as written (both optional comment-only one-liners were applied per the planner's "do BOTH" judgment).

## Commit

- `6eda867`: `test(260615-ipn): guard crush.json provider port against inference.ServerPort() drift` (3 files changed, 38 insertions)

## Self-Check: PASSED

- FOUND: `cmd/villa/code_test.go` (contains `TestCrushProviderPortMatchesInferenceServerPort` + `inference.ServerPort()`)
- FOUND: commit `6eda867` in `git log`
- Import isolation verified via `go list -deps` (authoritative over textual grep)
