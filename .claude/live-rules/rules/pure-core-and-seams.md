---
description: Pure cores, injected seams, confined impurity
globs: ["internal/**/*.go"]
priority: 55
prompt: ["seam", "deps struct", "pure core", "new package", "refactor", "livedeps", "fakedeps"]
---
Cores in `internal/*` take typed input and return typed values. They never print and never call
`os.Exit`; only the command tier turns a returned value into output and an exit code. Decision logic
inside a cobra `RunE` is a smell — the caller parses flags, calls one core, and renders.

Host effects are injected as a `Deps` struct of `func` fields, not an interface hierarchy: one live
implementation wired by a `live*Deps` closure in `cmd/villa`, one `fake*Deps` in tests. That is what
makes every command testable off-hardware.

Impurity is confined to named seams. `os/exec` touch belongs in `internal/orchestrate/systemd.go` and
unit writing in `WriteUnits`; every other filesystem access goes through `internal/pathsafe`
(containment, XDG resolution, atomic writes) and `internal/jsonstore` on top of it. A core must not
reach for `os` directly.

Wrap errors with context (`fmt.Errorf("…: %w", err)`) and fail closed on untrusted input — an
actionable error, never a silent default. A non-PASS `CheckResult` carries both a `Remediation` hint
and a `Provenance` string, so a refusal says what to do next and where the finding came from.

Open each new file with a doc comment naming its role and the invariant it upholds, and give each test
function a doc comment naming the invariant it guards. Carry the `D-NN` / `REQ-*` / `SC#N` / `GUARD-NN`
/ `PRIV-NN` identifiers through code, tests, and docs.
