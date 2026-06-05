<!-- generated-by: gsd-doc-writer -->
# Development

This guide is for contributors working on `villa`, the Go control-plane CLI. It
covers the local build/run/test/lint loop, the package layout you will touch, and
the test and code conventions the codebase actually enforces. For the *why* behind
the layering (pure cores behind injectable host seams, the typed-Unknown spine, the
backend seam), see [ARCHITECTURE.md](ARCHITECTURE.md) — this doc does not repeat it.

## Local setup

Clone the repo and confirm the toolchain. `villa` is a single Go module with no code
generation step and no system-library build dependencies — `go build` is the whole
toolchain.

```bash
git clone https://github.com/MatrixMagician/VillaStraylight.git
cd VillaStraylight
go version          # need Go 1.26+ (the module declares go 1.26.2 in go.mod)
make build          # builds ./villa
```

Notes:

- **No `.env` and no config file are required to build or test.** `villa` is read-only
  by default and synthesizes typed defaults when no config exists. The `.env.example`
  at the repo root belongs to the legacy reference-only scaffold (see below), not to
  the `villa` control plane.
- The dependency set is small and pure-Go: `cobra` (CLI), `chi`/`cors` (dashboard
  HTTP), `ghw` (hardware detection), and `BurntSushi/toml` (config). All are vendored
  through the module graph; run `make tidy` after changing imports.
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
| `make build` | `go build -o villa ./cmd/villa` | Produces the single static `./villa` binary. |
| `make test` | `go test ./...` | Runs the full test suite across all packages. |
| `make vet` | `go vet ./...` | Static checks; also the `make lint` fallback. |
| `make fmt` | `gofmt -w .` | Formats the tree in place. |
| `make lint` | `golangci-lint run`, else `go vet ./...` | Uses golangci-lint if it is on `PATH`; otherwise falls back to `go vet` with a notice. |
| `make check` | `make vet` + `make test` | The pre-commit gate — run this before pushing. |
| `make tidy` | `go mod tidy` | Run after adding/removing imports. |
| `make clean` | `rm -rf bin villa` | Removes build artifacts. |

The binary name is `villa` and the only current entry point is `cmd/villa/main.go`
(`BINARY := villa` in the `Makefile`).

## Package layout you will touch

The repo follows the standard Go `cmd/` + `internal/` split. The first-party,
under-active-development code is:

- `cmd/villa/` — the cobra CLI: one file per subcommand (`detect.go`, `recommend.go`,
  `preflight.go`, `model.go`, `install.go`, `up.go`/`down.go`/`restart.go`/`logs.go`,
  `status.go`, `dashboard.go`, `uninstall.go`) plus `root.go` and the live-wiring of
  each package's seams. This is the only layer that prints, maps verdicts to exit
  codes, and calls `os.Exit`.
- `internal/` — the pure / seam-injected libraries: `detect`, `catalog`, `recommend`,
  `preflight`, `download`, `config`, `inference`, `orchestrate`, `modelswap`,
  `status`, `metrics`, `dashboard`. Each returns typed values and contains no CLI
  behavior.

### Legacy reference-only scaffold — do not extend

The following trees are an earlier exploratory scaffold (an embedded-React-UI,
OpenAI-compatible proxy). It is **superseded** by the `villa` control plane and
integrated Open WebUI, and is kept only as a parts bin:

- `cmd/villastraylight/`
- `internal/llm/`, `internal/server/`
- `web/`
- the root `.env.example`

Do not add features to these packages or let their layout constrain new work. New
code belongs under `cmd/villa` and `internal/<new-or-existing-package>`.

## Code style

| Tool | Config | How to run |
|------|--------|-----------|
| gofmt | (none) | `make fmt` |
| go vet | (none) | `make vet` |
| golangci-lint | `.golangci.yml` (repo root) | `make lint` (falls back to `go vet` if golangci-lint is not installed) |

`golangci-lint` is **optional** — the `Makefile` and `.golangci.yml` are written so a
contributor without it installed still gets `go vet` coverage via `make lint`. To run
the full linter set locally, install golangci-lint and run `make lint`.

The enabled linters (`.golangci.yml`) are: `errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`, `gofmt`, `goimports`, `misspell`, and `revive`. Test files
are excluded from `errcheck` (an `issues.exclude-rules` entry on `_test\.go`), so
table tests may ignore returned errors where it aids readability; non-test code may
not. The lint run has a 3-minute timeout.

## Testing conventions

Tests are the load-bearing part of this codebase — the architecture exists to make
decision logic exhaustively table-testable on any host. Three patterns recur and you
are expected to follow them.

### 1. Function-seam / interface injection (host-free testing)

Every host-touching effect is injected, so tests never touch real hardware, Podman, or
the network. Two idioms appear:

- **Interface seams** in library packages — e.g. `inference.Backend` and
  `inference.Runner` (`internal/inference/inference.go`). Tests pass a fake
  implementation.
- **Struct-of-function-fields seams** in the command layer — the larger commands hold
  their effects as overridable function fields. For example `cmd/villa/install_test.go`
  builds the install dependencies struct and replaces each effect with a fake:

  ```go
  d.ensureModel = func(recommend.Recommendation) error { /* record + stub */ }
  d.saveConfig  = func(c config.VillaConfig) error { f.saveCalls++; f.savedCfg = c; return nil }
  d.writeUnits  = func(orchestrate.Plan, string) error { f.writeCalls++; return nil }
  d.daemonReload = func() error { f.reloadCalls++; return nil }
  ```

  Tests then assert on the recorded call counts and captured values (e.g. that config
  is saved **before** any unit work, the ordering-is-the-security contract). When you
  add a new effect to a command, add it as a seam field and a default live
  implementation in the wiring — never call `exec.Command`, `os`, or `net/http`
  directly from decision logic.

### 2. Byte-golden tests for generated artifacts

Anything `villa` renders or emits as a stable contract — Quadlet units and `--json`
output — is frozen against a byte-for-byte golden fixture under a sibling `testdata/`
directory. Golden tests share a `-update` harness:

```bash
# Regenerate all goldens after an intentional output change:
go test ./... -update

# Regenerate a single package's goldens:
go test ./internal/orchestrate/... -update
go test ./cmd/villa/... -update
```

Examples in the tree:

- `internal/orchestrate/render_test.go` freezes each rendered Quadlet unit
  (`villa-llama.container`, `villa.network`, `villa-models.volume`,
  `villa-openwebui.container`/`.volume`) against `internal/orchestrate/testdata/*.golden`.
  The fixture `RenderInput` uses a **fixed absolute path** (not live `$HOME`) so the
  golden is stable in CI, and the image digest is sourced **through** the backend seam
  (`inference.VulkanBackend()`), never hand-typed in the test.
- `cmd/villa/recommend_test.go` freezes `villa recommend --json` against
  `cmd/villa/testdata/recommend.golden.json` from a deterministic fixture
  `Recommendation`. The same pattern backs the `detect`, `preflight`, `inference`, and
  `status` JSON/text goldens (see `cmd/villa/testdata/*.golden*`).

When you change rendered output or JSON shape on purpose, run `-update`, then **review
the golden diff** as part of your change — the diff is the proof of intent.

Some golden tests are paired with **intent assertions** that survive whitespace edits
and document load-bearing invariants — e.g. `TestRenderOpenWebUITelemetryFrozen`
asserts the full telemetry-kill env set is present *and* that the rendered unit carries
exactly that many `Environment=` lines, so a contributor cannot silently add or drop a
privacy-relevant variable without tripping the guard. Prefer adding such an intent test
alongside a new golden when the bytes encode a security or privacy contract.

### 3. The seam grep-gate

`internal/inference/seam_test.go` (`TestSeamGrepGate`) is a structural test that walks
every non-test `.go` file under `internal/` and fails if an **imperative backend leak**
appears outside the sanctioned seam (`internal/inference/` and
`internal/detect/gpu_amd.go`). The four gated patterns are:

- `runtime.GOOS` / `GOOS ==` platform branching,
- the container **image** literal (`kyuz0`, `docker.io/`, `server-vulkan`),
- container **device** args (`--device /dev/dri`, `--group-add`, `keep-groups`),
- `podman` process invocations (`exec.Command("podman", …)`, `"podman" run|stop|logs`).

The gate is deliberately scoped to imperative behavior, not to provenance/remediation
*strings* that merely name these tools as findings (those are data and predate the
seam). If you need an image digest, a device passthrough, a `GOOS` branch, or a
`podman` exec, it must live in the seam — that is how a future ROCm or Metal backend
slots in as a sibling `Backend` implementation without touching callers. If this test
goes red, move the literal into the seam rather than widening the allow-list.

### Running tests

```bash
make test                              # everything
go test ./internal/recommend/...       # one package
go test ./cmd/villa/ -run TestInstall  # a single test by name
go test ./... -update                  # refreeze all goldens (review the diff!)
```

There is no configured coverage threshold; the bar is behavioral — every decision path
and verdict (PASS/WARN/FAIL, typed-Unknown degradation) should be table-covered.

## Branch and commit conventions

There is no `CONTRIBUTING.md` or `.github/` template in the repo; the conventions below
are the ones the existing history actually follows.

- **Default branch:** `main`.
- **Branch naming:** type-prefixed, kebab-case, scoped to the work — e.g.
  `fix/phase-03-install-model-pull-config`. Use a `feat/`, `fix/`, `docs/`, `test/`, or
  `chore/` prefix matching the change.
- **Commit messages:** Conventional-Commits style with an optional scope, e.g.
  `fix(260605-tuv): drop unsupported --ignore from podman volume rm in uninstall`,
  `test(05): add failing regression tests for villa uninstall volume rm`,
  `docs: create milestone v1.1 roadmap`. Commits are kept atomic — tests, the fix, and
  the docs/plan update land as separate commits.

## Submitting changes

Before opening a PR:

1. `make fmt` — format the tree.
2. `make check` — runs `go vet` + the full test suite (the minimum gate).
3. `make lint` — run golangci-lint if you have it installed.
4. If you changed rendered units or `--json` output, run the relevant `-update` and
   commit the regenerated goldens **with** the code change so reviewers see the diff.
5. Keep commits atomic and Conventional-Commits-formatted; push your type-prefixed
   branch and open a PR against `main`.

This project is developed through the GSD workflow, so planning context for a change
typically lives under `.planning/` — read the relevant phase/plan there to understand
the intent before modifying a package, and keep your branch and commits scoped to that
unit of work.
