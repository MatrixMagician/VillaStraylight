---
status: accepted
---

# First-party code may be any language the villa binary ships and runs

`CLAUDE.md` and `CONTRIBUTING.md` said first-party code is Go only. The rule protected
two things: an operator installs one file, and that file is a static, CGO-free binary
that the websafe, inferproxy and task-VM units can bind-mount into a distroless image.
The language was a means to those ends, and it had already been stretched once: the
dashboard's `dashboard.js` is first-party JavaScript, shipped verbatim through
`go:embed` with no toolchain.

The rule now blocks work it was never meant to block. The 2026-11-14 research on
replacing Crush found that the Pi coding agent (`earendil-works/pi`, MIT) has no
permission system of its own: a host can gate a tool call only from a TypeScript
extension that handles `tool_call` and returns `{ block: true }`
(`packages/coding-agent/docs/extensions.md`). Most agents' extension and plug-in APIs
are TypeScript or Python. Under the old rule, villa could integrate such an agent only
in the modes that need no first-party code inside it.

## Decision

- **The villa binary stays Go.** The CLI, the pure cores, orchestration and the
  dashboard server remain one Go module built into one static, CGO-free binary.
  `make build` and `make build-static` still need only the Go toolchain, and the
  CGO-free gate CI enforces is unchanged.
- **Other first-party code may be any language, if it integrates through the villa
  binary.** Integration means all of the following hold:
  - **villa ships it.** Its source is embedded in the binary with `go:embed`, as the
    dashboard assets are, or it is a pinned artifact villa downloads and checks by
    sha256, as Crush is (`internal/agent/crush-policy.json`). The operator installs
    nothing beside `villa`.
  - **villa runs it.** A villa verb writes it to disk, starts it, and removes it on
    `villa uninstall`. It has no installer, daemon or entry point of its own.
  - **villa configures it.** Its configuration is rendered from `config.toml`, like
    every Quadlet unit and `crush.json`, and is never the authority.
  - **villa proves it.** A `verify` or `doctor` path covers what it does, and its tests
    run in `make check` and in CI.
  - **Its runtime is pinned.** It runs inside a runtime villa already pins (an agent's
    release binary, a container image), not on an interpreter the operator must
    install on the host.
- **The invariants are language-neutral and do not change.** Zero telemetry, the
  launched-agent capture (ADR-0039), loopback-only binds, no shell interpolation,
  inference only through the authenticated client or `villa-inferproxy` (ADR-0011,
  ADR-0014), and the closed networks (ADR-0036, ADR-0043) bind code in every language.
- **The dependency budget extends to each component.** A non-Go component with
  third-party dependencies pins them with a lockfile, and each one needs the same
  argument a fifth direct Go dependency does. A component with none is the default.

## Consequences

- The Go source gates (`TestSeamGrepGate`, `TestInferenceReachedOnlyThroughClient`)
  walk Go files only. A non-Go component that could leak a backend literal, a route or
  the inference key states in its ADR which gate covers it, or adds one.
- `make check` grows a step, and CI a toolchain, when the first non-Go component with
  tests lands. That component's ADR names both.
- The first candidate is a TypeScript approval extension for Pi, embedded in the
  binary and passed to `pi -e`, if villa ever drives Pi headless. This ADR permits it
  and does not decide it.
- Reviewers read more than one language. That is the cost this ADR accepts.
- Unchanged: the AI services are still integrated OSS containers, not rebuilt
  (integration-first), and the four direct Go dependencies and the TUI-dependency grep
  in CI stand.
