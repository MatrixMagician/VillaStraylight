# Development

This guide is for contributors working on `villa`, the Go control-plane CLI. It
covers the local build/run/test/lint loop, the package layout you will touch, and
the test and code conventions the codebase actually enforces. For the *why* behind
the layering (pure cores behind injectable host seams, the typed-Unknown spine, the
backend seam), see [ARCHITECTURE.md](ARCHITECTURE.md); this doc does not repeat it.

## Local setup

Clone the repo and confirm the toolchain. `villa` is a single Go module with no code
generation step and no system-library build dependencies: `go build` is the whole
toolchain.

```bash
git clone https://github.com/MatrixMagician/VillaStraylight.git
cd VillaStraylight
go version          # need Go 1.26+ (the module declares go 1.26.2 in go.mod)
make build          # builds ./villa
```

Notes:

- **No `.env` and no config file are required to build or test.** `villa` is read-only
  by default and synthesizes typed defaults when no config exists.
- The dependency set is small and pure-Go: four direct requires in `go.mod`, which is
  the list. Everything else comes from the standard library: the dashboard routes on
  `net/http`'s mux, hardware detection reads procfs and sysfs directly, and the guided
  install is a stdin prompt loop. Run `make tidy` after changing imports, and justify
  any new dependency against the single-static-binary goal.
- **You do not need a Strix Halo host, Podman, or a GPU to develop.** Every
  host-touching effect (sysfs reads, `podman`, `systemctl`, HTTP probes, downloads,
  file writes) is injected behind a function seam or an interface, so the full
  decision logic is exercised by `go test` on any machine. Running the live
  `install`/`up`/`status` paths against real hardware is only needed for UAT, not for
  unit work.

## Build commands

All day-to-day tasks are wired into the `Makefile`. Run `make help` to print the
self-documented target list.

| Command | What it runs | Notes |
|---------|--------------|-------|
| `make run` | `go run ./cmd/villa` | Runs the CLI in place; pass args after `--` (e.g. `go run ./cmd/villa detect`). |
| `make build` | `go build -ldflags "-X main.version=$(VERSION)" -o villa ./cmd/villa` | Produces the single static `./villa` binary, version-stamped from `git describe`. |
| `make test` | `go test ./...` | Runs the full test suite across all packages. |
| `make test-race` | `CGO_ENABLED=1 go test -race ./...` | The race gate (CR-01/WR-04). cgo is enabled for the TEST run only; the shipped binary stays CGO-free. |
| `make build-static` | `CGO_ENABLED=0 go build …` | The SC#4 CGO-free gate CI enforces. `make check` does NOT cover it; run this before pushing if you touched imports. |
| `make vet` | `go vet ./...` | Static checks; the first half of `make check`. |
| `make fmt` | `gofmt -w .` | Formats the tree in place. |
| `make lint` | pinned `golangci-lint run --new-from-merge-base=$(LINT_BASE)` | Runs the version in `.golangci-version` via `go run`; nothing needs to be on `PATH`. Diffs against `origin/main` to mirror CI's new-issues gate; `make LINT_ALL=1 lint` lints the whole tree. |
| `make check` | `make vet` + `make test` + `make test-race` | The pre-commit gate; run this before pushing. Note it does NOT check formatting or the static build. |
| `make tidy` | `go mod tidy` | Run after adding/removing imports. |
| `make clean` | `rm -rf bin villa` | Removes build artifacts. |

The binary name is `villa` and the only current entry point is `cmd/villa/main.go`
(`BINARY := villa` in the `Makefile`).

### Gotcha: the dashboard runs a long-lived copy of the binary

`status`, `recommend`, `detect`, and the other one-shot subcommands run fresh from
`./villa` every invocation, so a rebuild is picked up immediately. The **dashboard is
different**: `villa-dashboard.service` is a native systemd user unit
(`internal/orchestrate/dashboard_unit.go`, `DashboardServiceName =
"villa-dashboard.service"`) that holds a long-lived `villa dashboard` process. After
rebuilding, the running service still executes the *old* binary until you restart it:

```bash
go build -o ./villa ./cmd/villa            # or: make build
systemctl --user restart villa-dashboard.service   # required to load new dashboard code
```

If you are iterating on dashboard backend code and your changes do not appear, this
restart is almost always the reason.

## Package layout you will touch

The repo follows the standard Go `cmd/` + `internal/` split. The first-party,
under-active-development code is:

- `cmd/villa/`: the cobra CLI, one file per subcommand plus `root.go` and the
  live-wiring of each package's seams. `newRoot` in `root.go` is the authoritative
  command list; `ls cmd/villa/*.go` is the file list. A few shapes are worth knowing
  before you go looking: `up`/`down`/`restart`/`logs` share their wiring in
  `lifecycle.go`, and the larger verbs split across files by concern rather than by
  noun (`install.go` + `install_hostprep.go` + `install_memory.go` + ...,
  `verify.go` + `verify_agent.go` + `verify_memory.go` + ...). This is the only layer
  that prints, maps verdicts to exit codes, and calls `os.Exit`.
- `internal/`: the pure / seam-injected libraries. Each returns typed values and
  contains no CLI behavior. `ls internal/` is the list and the code map in `CLAUDE.md`
  says what each one owns in a line; `docs/ARCHITECTURE.md` carries the layering and
  the key abstractions. Four groupings orient you: the v1.0–v1.2 spine (`detect`,
  `catalog`, `recommend`, `preflight`, `download`, `config`, `inference`,
  `orchestrate`, `modelswap`, `backendswap`, `bench`, `llm`, `status`, `metrics`,
  `dashboard`), the v1.3–v1.5 feature cores (`memory`, `recall`, `agent`,
  `codingmode`, `websafe`, `doctor`, `backup`, `usage`), the v1.8 pin-and-update
  spine (`pins`, `pinstate`, `pinresolve`, `manifest`, `manifestverify`,
  `updatecheck`, `updatefetch`, `updateflow`, `prune`, `snapshotprune`), and the
  shared plumbing: `prove`, `residency`, `openwebui`, `subsystem`, `verify` and
  `install` (the six the v1.6 consolidation extracted from forked copies), plus
  `residentset` and the persistence layer `pathsafe` / `jsonstore` / `benchstore` /
  `verifystate`.

## Code style

| Tool | Config | How to run |
|------|--------|-----------|
| gofmt | (none) | `make fmt` |
| go vet | (none) | `make vet` |
| golangci-lint | `.golangci.yml` + the pin in `.golangci-version` | `make lint` (fetched via `go run`; no local install needed) |

`make lint` needs nothing installed: it runs the version pinned in `.golangci-version`
through `go run`, and that file is the single place the pin lives. The CI workflow reads
the same file, so local and CI cannot drift onto different linters and disagree about
what is clean.

There is deliberately **no fall back to `go vet`**. The target used to be
`command -v … && golangci-lint run || (echo "not found" && go vet)`, and in
`A && B || C` a *failure* of B runs C, so any real finding printed "golangci-lint not
found" and exited 0 behind a passing vet. The gate could never fail and it misreported
why. Do not reintroduce that shape.

In CI the lint step runs on **pull requests only**, gated with `only-new-issues`. That
restriction is load-bearing: on a push event there is no base to diff against, so the
action silently degrades to linting the whole tree. The whole tree is currently clean
(`make LINT_ALL=1 lint` reports 0 issues), so the new-issues gate is a floor to hold
rather than a workaround around a backlog.

The config is **v2 format**. Under v2 the `errcheck`, `govet`, `ineffassign`,
`staticcheck` and `unused` set is enabled by default and is no longer listed;
`misspell` and `revive` are the explicit additions, and `gofmt`/`goimports` moved to
the `formatters` block. Two exclusions are scoped to `_test.go`: `errcheck`, so table
tests may ignore returned errors where it aids readability, and `revive`'s
`unused-parameter`, because test doubles must keep a seam's parameter names to satisfy
a fixed `Deps` signature. Non-test code gets neither.

A v1-format config is unusable by a v2 binary, but not silently: it fails to load rather
than linting nothing quietly. Run `golangci-lint migrate` if you ever hit
"unsupported version of the configuration".

## Testing conventions

Tests are the load-bearing part of this codebase: the architecture exists to make
decision logic exhaustively table-testable on any host. Three patterns recur and you
are expected to follow them.

### 1. Function-seam / interface injection (host-free testing)

Every host-touching effect is injected, so tests never touch real hardware, Podman, or
the network. Two idioms appear:

- **Interface seams** in library packages, e.g. `inference.Backend` and
  `inference.Runner` (`internal/inference/inference.go`). Tests pass a fake
  implementation.
- **Struct-of-function-fields seams**: a flow holds its effects as overridable
  function fields on a `Deps` struct. For example `internal/install/fake_test.go`
  builds `install.Deps` and replaces each effect with a fake:

  ```go
  d.EnsureModel = func(recommend.Recommendation) error { /* record + stub */ }
  d.SaveConfig  = func(c config.VillaConfig) error { f.saveCalls++; f.savedCfg = c; return nil }
  d.WriteUnits  = func(orchestrate.Plan, string) error { f.writeCalls++; return nil }
  d.DaemonReload = func() error { f.reloadCalls++; return nil }
  ```

  Tests then assert on the recorded call counts and captured values (e.g. that config
  is saved **before** any unit work, the ordering-is-the-security contract). When you
  add a new effect to a command, add it as a seam field and a default live
  implementation in the wiring; never call `exec.Command`, `os`, or `net/http`
  directly from decision logic.

  **The `live*Deps` convention.** Every command's real host wiring is built by a single
  constructor named `live<Noun>Deps` that fills the deps struct with the genuinely
  host-touching functions; the rest of the file is pure decision logic that takes the
  struct as a parameter. Individual injected effects follow the same `live*` prefix
  (e.g. `liveProve`, `liveMeasure`, `liveModelFile`, `liveWeightBytes`,
  `liveReadinessPoll`). Do not look for a list here; it goes stale every time a verb
  is added. `grep -rn "func live" cmd/villa` is the enumeration, and it IS the map of
  every host boundary in the cmd tier. When you add a subcommand, mirror this:
  one `live<Noun>Deps` constructor, a deps struct of function fields, and a test that
  swaps each field for a fake.

### 2. Byte-golden tests for generated artifacts

Anything `villa` renders or emits as a stable contract (Quadlet units and `--json`
output) is frozen against a byte-for-byte golden fixture under a sibling `testdata/`
directory. Golden tests share a `-update` harness:

```bash
# Regenerate all goldens after an intentional output change:
go test ./... -update

# Regenerate a single package's goldens:
go test ./internal/orchestrate/... -update
go test ./cmd/villa/... -update
```

Examples in the tree:

- `internal/orchestrate/render_test.go` freezes every rendered Quadlet unit (one
  golden per unit the stack can write) plus per-variant fixtures where a flag changes
  the output (`villa-openwebui.container.memory`/`.websearch`/`.resident`), against
  `internal/orchestrate/testdata/*.golden`. `ls internal/orchestrate/testdata/` is the
  current set. The
  fixture `RenderInput` uses a **fixed absolute path** (not live `$HOME`) so the golden
  is stable in CI, and the image digest is sourced **through** the backend seam
  (`inference.VulkanBackend()` / the ROCm backend), never hand-typed in the test. The
  separate `villa-llama-rocm.container.golden` is the proof that the ROCm backend
  renders its own device/env block without a caller ever typing a backend literal.
  Note the Vulkan-rendered fixtures pin `backend = "vulkan"` explicitly rather than
  inheriting `DefaultVillaConfig()`, whose default is now `rocm`.
- `cmd/villa/recommend_test.go` freezes `villa recommend --json` against
  `cmd/villa/testdata/recommend.golden.json` from a deterministic fixture
  `Recommendation`. The same pattern backs the `detect`, `preflight`, `inference`, and
  `status` JSON/text goldens (see `cmd/villa/testdata/*.golden*`).

When you change rendered output or JSON shape on purpose, run `-update`, then **review
the golden diff** as part of your change: the diff is the proof of intent.

Some golden tests are paired with **intent assertions** that survive whitespace edits
and document load-bearing invariants, e.g. `TestRenderOpenWebUITelemetryFrozen`
asserts the full telemetry-kill env set is present *and* that the rendered unit carries
exactly that many `Environment=` lines, so a contributor cannot silently add or drop a
privacy-relevant variable without tripping the guard. Prefer adding such an intent test
alongside a new golden when the bytes encode a security or privacy contract.

### 3. The seam grep-gate

`internal/inference/seam_test.go` (`TestSeamGrepGate`) is a structural test that fails
if an **imperative backend leak** appears outside the sanctioned seam
(`internal/inference/` and `internal/detect/gpu_amd.go`). As of v1.1 it walks **two**
trees with two pattern sets:

**Walk 1: every non-test `.go` under `internal/`.** Five gated patterns:

- `runtime.GOOS` / `GOOS ==` platform branching,
- the container **image** literal: `kyuz0`, `docker.io/`, `server-vulkan`, and the
  ROCm tags (`rocm-7.2.4`, `rocm-6.4.4`, which covers the `-rocwmma` suffix too, and
  `rocm7-nightlies`). Note these are anchored to the IMAGE context (`:tag` / `tag@`),
  so a bare backend NAME as a config value (`case "rocm-6.4.4":` in `render.go`, a
  `--backend` help line) is deliberately not a hit,
- container **device** args (`--device /dev/dri`, `--group-add`, `keep-groups`),
- `podman` process invocations (`exec.Command("podman", …)`, `"podman" run|stop|logs`),
- **coding-mode llama-server flags** (`codingModeFlagPattern`): the quoted literals
  `"--jinja"`, `"--cache-reuse"`, `"--repeat-penalty"`. These belong only to
  `appendCodingModeArgs` in the two backend files. The anchor on a leading double-quote
  is what lets a doc comment discuss `--cache-reuse` as prose without tripping the gate.

**Walk 2: every non-test `.go` under `cmd/villa`.** The cmd tier *legitimately* invokes
`podman` (lifecycle up/down/logs, uninstall volume rm, fixed-arg, never a shell), so the
`podman` pattern does **not** apply here. Instead it gates the other four patterns
above, including the coding-mode flags (which the single `codingModeFlagPattern`
helper feeds to both walks so adding it to one map cannot leave the other unguarded),
**plus** a `backend marker literal` pattern: `ROCm0`,
`HSA_OVERRIDE_GFX_VERSION`, and `Memory access fault`. These raw backend markers must
arrive in a cmd-tier caller only through `inference.BackendFor(target).ResidencyProof()`;
`cmd/villa/backend.go` and `cmd/villa/bench.go` carry explicit "literal-free" header
comments enforcing exactly this. This is why a new noun (e.g. `villa backend`,
`villa bench`) composes the `backendswap` core and the exported `inference` prove/measure
primitives without ever retyping a backend literal.

The gate is deliberately scoped to imperative behavior, not to provenance/remediation
*strings* that merely name these tools as findings (those are data and predate the
seam). If you need an image digest, a device passthrough, a `GOOS` branch, a `podman`
exec, or a ROCm/HSA marker, it must live in the seam: that is how the v1.1 ROCm backend
slotted in (and how a future Metal backend slots in) as a sibling `Backend`
implementation without touching callers. If this test goes red, move the literal into the
seam rather than widening the allow-list.

**The positive dual: `TestROCmMarkerPresence`.** The same file holds the inverse gate. It
reads `internal/inference/backend_rocm.go` and fails if it is **missing** the ROCm-only
markers `ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, and `/dev/kfd`. Where `TestSeamGrepGate`
keeps these literals *out* of callers, `TestROCmMarkerPresence` keeps them *in* the seam,
so a refactor that drops or relocates the ROCm descriptor also trips CI.

### Running tests

```bash
make test                              # everything
go test ./internal/recommend/...       # one package
go test ./cmd/villa/ -run TestInstall  # a single test by name
go test ./... -update                  # refreeze all goldens (review the diff!)
```

There is no configured coverage threshold; the bar is behavioral: every decision path
and verdict (PASS/WARN/FAIL, typed-Unknown degradation) should be table-covered.

## Branch and commit conventions

[`CONTRIBUTING.md`](../CONTRIBUTING.md) is the authority on standards, required gates
and PR expectations; the conventions below are the branch/commit shapes the existing
history actually follows, which it does not cover. There are no `.github/` issue or PR
templates.

- **Default branch:** `main`.
- **Branch naming:** type-prefixed, kebab-case, scoped to the work, e.g.
  `fix/phase-03-install-model-pull-config`. Use a `feat/`, `fix/`, `docs/`, `test/`, or
  `chore/` prefix matching the change.
- **Commit messages:** Conventional-Commits style with an optional scope, e.g.
  `fix(260605-tuv): drop unsupported --ignore from podman volume rm in uninstall`,
  `test(05): add failing regression tests for villa uninstall volume rm`,
  `docs: create milestone v1.1 roadmap`. Commits are kept atomic: tests, the fix, and
  the docs/plan update land as separate commits.

## Submitting changes

Before opening a PR:

1. `make fmt` to format the tree.
2. `make check`: `go vet` + the full suite + the `-race` gate (the minimum gate).
3. `make lint`: the new-issues gate; needs nothing installed.
4. `make build-static` if you touched imports. `make check` does not cover the
   CGO-free build, and the `villa-websafe` unit bind-mounts the binary into a
   distroless container with no libc, so a dynamically-linked build fails to exec.
5. If you changed rendered units or `--json` output, run the relevant `-update` and
   commit the regenerated goldens **with** the code change so reviewers see the diff.
6. Keep commits atomic and Conventional-Commits-formatted; push your type-prefixed
   branch and open a PR against `main`.

Read `CLAUDE.md` and the package's own file-level doc comments to understand the
intent before modifying a package, and keep your branch and commits scoped to a
single unit of work.
