---
phase: quick-260615-ipn
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/villa/code_test.go
  - internal/agent/render.go
  - internal/inference/backend_vulkan.go
autonomous: true
requirements: [v1.4-AUDIT-WARN-crush-port-drift]
must_haves:
  truths:
    - "A Go test in cmd/villa fails if internal/inference serverPort and render.go providerBaseURL desync"
    - "The new test PASSES today (both sides are port 8080)"
    - "internal/agent still imports NEITHER internal/inference NOR internal/detect after the change"
    - "make check (go vet + go test ./...) and TestSeamGrepGate stay green"
    - "Rendered crush.json bytes are unchanged — no golden re-freeze"
  artifacts:
    - path: "cmd/villa/code_test.go"
      provides: "Drift-guard test tying agent.Render crush.json provider baseURL to inference.ServerPort()"
      contains: "inference.ServerPort()"
  key_links:
    - from: "cmd/villa/code_test.go"
      to: "internal/inference.ServerPort()"
      via: "fmt.Sprintf with the host:port substring asserted against the rendered crush.json bytes"
      pattern: "inference\\.ServerPort\\(\\)"
    - from: "cmd/villa/code_test.go"
      to: "internal/agent.Render"
      via: "agent.Render(cfg, nil) rendered bytes searched for the inference port substring"
      pattern: "agent\\.Render"
---

<objective>
Close the v1.4 milestone-audit WARNING (`.planning/v1.4-MILESTONE-AUDIT.md`): `internal/agent/render.go:32`
hard-codes the inference port as the literal `providerBaseURL = "http://127.0.0.1:8080/v1"` instead of
sourcing `inference.ServerPort()`. A future `serverPort` change could silently desync the rendered
`crush.json` provider endpoint from the actually-served inference endpoint, with no test catching it.

The naive fix (have `render.go` call `inference.ServerPort()`) is REJECTED — `internal/agent` deliberately
imports NEITHER `internal/inference` NOR `internal/detect` (LOCKED seam invariant, `agent.go:17-24`; the
`8080` loopback literal there is SANCTIONED). Instead, add a **drift-guard test** in `cmd/villa` — the one
package that legitimately imports BOTH `internal/agent` and `internal/inference` — that fails if the two
desync.

Purpose: a single, off-hardware regression test makes the cross-seam port coupling EXPLICIT and self-checking,
without violating the agent core's import isolation and without changing any rendered bytes.
Output: one new test in `cmd/villa/code_test.go` (+ optional one-line cross-reference comments at the two
literal sites). No production logic change. No golden re-freeze.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md

# The renderer under guard — read the package doc comment + the providerBaseURL const (line 29-32)
@internal/agent/render.go
# The LOCKED seam invariant: internal/agent imports no internal/inference / internal/detect (lines 14-40)
@internal/agent/agent.go
# ServerPort() accessor returning serverPort=8080 (lines 155-166)
@internal/inference/backend_vulkan.go
# The precedent: orchestrate composes inference.ServerPort() because orchestrate MAY import inference
@internal/orchestrate/endpoint.go
# Natural co-location: existing agent.Render(cfg, nil) usage + renderRef helper (lines 53-62)
@cmd/villa/code_test.go
# Existing port-substring assertion style (line ~1125: strings.Contains(out, "http://127.0.0.1:8080"))
@cmd/villa/install_test.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add the cross-seam crush.json port drift-guard test in cmd/villa</name>
  <files>cmd/villa/code_test.go, internal/agent/render.go, internal/inference/backend_vulkan.go</files>
  <action>
Add a new test function `TestCrushProviderPortMatchesInferenceServerPort` to `cmd/villa/code_test.go`
(co-located with the existing `agent.Render` usage / `renderRef` helper — this file already imports both
`internal/agent` and `internal/config`).

Add the `internal/inference` import to the existing import block (path
`github.com/MatrixMagician/VillaStraylight/internal/inference`, kept in goimports group order — it is the
package whose import from `internal/agent` is FORBIDDEN, which is exactly why the binding lives here).

The test must:
  1. Build a representative `config.VillaConfig` the same way the sibling tests do (e.g.
     `config.VillaConfig{Model: "qwen3", CodingMode: true}` — matching `TestCodeLockdownEnv`).
  2. Render the crush.json bytes via `agent.Render(cfg, nil)` (fail the test on a non-nil error, mirroring
     `renderRef`).
  3. Compute the expected host:port substring from the inference seam:
     `want := fmt.Sprintf("127.0.0.1:%d/v1", inference.ServerPort())`.
  4. Assert the rendered bytes contain `want` via `bytes.Contains(rendered, []byte(want))` (`bytes` is
     already imported). On mismatch, `t.Errorf` with a message naming BOTH sources and stating the remedy —
     e.g. "rendered crush.json provider baseURL does not embed inference.ServerPort()=%d; render.go
     providerBaseURL desynced from the served inference port — update one to match the other".

Add a doc comment ABOVE the test explaining WHY it lives in `cmd/villa` and NOT in `internal/agent`: the
agent core deliberately imports no `internal/inference` (LOCKED seam, `agent.go:17-24`; its `8080` loopback
literal is sanctioned), so `cmd/villa` — which legitimately imports BOTH cores — is the single correct place
to bind `render.go`'s rendered provider port to `inference.ServerPort()`. State the guard's contract: it
FAILS if someone changes `inference.serverPort` (or `render.go`'s `providerBaseURL`) without updating the
other; both are `8080` today so it PASSES. This mirrors the `orchestrate.LlamaInNetworkEndpoint()` precedent
(`internal/orchestrate/endpoint.go`), which composes `inference.ServerPort()` because orchestrate MAY import
inference — agent may not, so the coupling is asserted from the test tier instead.

Optional (keep MINIMAL, planner's judgment — do BOTH one-liners, no logic change):
  - In `internal/agent/render.go`, append a single trailing line to the existing `providerBaseURL` const
    comment (around line 29-32) noting the port is guarded by
    `cmd/villa.TestCrushProviderPortMatchesInferenceServerPort` (cross-seam drift guard; do not change the
    literal here — the seam forbids importing inference).
  - In `internal/inference/backend_vulkan.go`, append a single trailing line to the existing `ServerPort`
    doc comment (around line 160-166) noting the agent renderer's port is drift-guarded against this accessor
    by the same `cmd/villa` test.
  These are COMMENT-ONLY edits — they change no code, no rendered bytes, no golden, and add no import.

Do NOT modify `render.go`'s `providerBaseURL` value or any rendering logic. Do NOT add any `internal/inference`
import to `internal/agent`. The new cross-seam coupling is confined to the `cmd/villa` test.
  </action>
  <verify>
    <automated>go test ./cmd/villa -run TestCrushProviderPortMatchesInferenceServerPort -v && go test ./internal/inference -run TestSeamGrepGate && make check</automated>
  </verify>
  <done>
- `TestCrushProviderPortMatchesInferenceServerPort` exists in `cmd/villa/code_test.go`, imports
  `internal/inference`, and PASSES today (both sides are port 8080).
- The test asserts `agent.Render(cfg, nil)` bytes contain `fmt.Sprintf("127.0.0.1:%d/v1", inference.ServerPort())`
  — so a one-sided port change FAILS it.
- The test carries a doc comment explaining WHY it lives in `cmd/villa` (the agent core's no-inference seam) and
  references the `orchestrate.LlamaInNetworkEndpoint()` precedent.
- `internal/agent` still imports NEITHER `internal/inference` NOR `internal/detect`
  (`grep -rn "internal/inference\|internal/detect" internal/agent/` returns nothing).
- `make check` (go vet + `go test ./...`) is green; `TestSeamGrepGate` is green.
- Rendered crush.json bytes are unchanged — no `-update`, no golden re-freeze (the only non-test edits are
  comment-only lines).
  </done>
</task>

</tasks>

<verification>
- `go test ./cmd/villa -run TestCrushProviderPortMatchesInferenceServerPort -v` → PASS (the guard is green today).
- Sanity-prove the guard BITES (manual, do NOT commit): temporarily flip `serverPort` in
  `internal/inference/backend_vulkan.go` to a non-8080 value → the new test FAILS → revert. (Optional manual
  confidence check; the committed state keeps both at 8080.)
- `grep -rn "internal/inference" internal/agent/` returns nothing — the seam invariant holds.
- `go test ./internal/inference -run TestSeamGrepGate` → PASS (no backend marker leaked; no literal moved).
- `make check` → PASS (go vet + full `go test ./...`).
- No `testdata/*.golden*` file changed (`git status` shows only `cmd/villa/code_test.go` +
  the two comment-only source edits).
</verification>

<success_criteria>
- A cmd/villa drift-guard test ties the rendered crush.json provider port to `inference.ServerPort()` and is
  green today; it FAILS if the two desync.
- `internal/agent` import isolation is preserved (no `internal/inference` / `internal/detect` import).
- TEST-ONLY change (plus two optional comment-only one-liners): no production logic, no rendered-byte change,
  no golden re-freeze.
- `make check` and `TestSeamGrepGate` stay green.
</success_criteria>

<output>
Create `.planning/quick/260615-ipn-add-drift-guard-test-tying-crush-json-pr/260615-ipn-SUMMARY.md` when done.
</output>
