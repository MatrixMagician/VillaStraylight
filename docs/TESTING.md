# Testing

The `villa` control plane is tested entirely with the Go standard `testing`
package — no third-party test framework, no mocking library, no test runner
beyond `go test`. Every test runs fully offline: there is **no live GPU, podman,
systemd, journald, SELinux, or network dependency** in the suite. Host-touching
behaviour is reached through injectable dependency seams (fakes), and
hardware/log inputs are frozen as `testdata` fixtures. This keeps the suite
deterministic and CI-safe while still asserting the real orchestration ordering,
the rendered Quadlet bytes, and the iGPU-offload verdicts.

## Test framework and setup

- **Framework:** Go stdlib `testing` (`go1.26.2`, per `go.mod`). No Jest/Vitest/
  pytest equivalent — assertions are hand-written `t.Errorf` / `t.Fatalf`.
- **Helpers:** the only external testing import is `github.com/spf13/cobra`,
  used to construct in-memory command objects (`cmd.SetOut`/`cmd.SetErr` into a
  `bytes.Buffer`) so command handlers can be driven and their output captured
  without a real terminal.
- **Setup:** none beyond a working Go toolchain. There is no global test setup
  file, no fixture database, and no environment configuration step. Run
  `go mod download` once if dependencies are not yet cached, then `go test`.
- **Off-hardware by design:** the entire suite is pure cores plus frozen
  fixtures, so it passes on a runner with no GPU, podman, or journald. The
  optional ROCm backend (v1.1) is no exception — its preflight gates, residency
  parser, and A/B swap are all driven by fixtures, never a live gfx1151. The
  on-hardware UAT that exercised a real Strix Halo (Phases 8/9/10) is a separate
  **manual** step, not part of `go test` (see [On-hardware UAT](#on-hardware-uat)).

## Running tests

The full suite is wired through the `Makefile` and standard `go test`.

Run the entire suite:

```bash
make test
# equivalent to:
go test ./...
```

Run `vet` plus the suite together (the pre-commit gate):

```bash
make check
# equivalent to: go vet ./... && go test ./... && CGO_ENABLED=1 go test -race ./...
```

Run the race detector on its own:

```bash
make test-race
# CGO_ENABLED=1 go test -race ./...
```

This is a required gate, not an optional extra (CR-01/WR-04). `internal/websafe`
is the project's one intentionally-concurrent pure core — `Loader.Load` fans out
fetch goroutines — so its concurrency invariants MUST be guarded under `-race`, and
the whole tree is run that way so any future concurrent core is covered too. Note
this is the one target that enables cgo, and only for the TEST binary: `build` and
`build-static` keep `CGO_ENABLED=0`, so the shipped `villa` stays a CGO-free static
binary.

Run the linter:

```bash
make lint
# the version pinned in .golangci-version, fetched via `go run` — nothing
# needs to be on PATH. Configured by .golangci.yml (errcheck, govet,
# ineffassign, staticcheck, unused, gofmt, goimports, misspell, revive).
# Diffs against origin/main; `make LINT_ALL=1 lint` lints the whole tree.
```

Run a single package:

```bash
go test ./internal/orchestrate/...
go test ./cmd/villa/...
```

Run a single test (or a group) by name with `-run` and a regexp:

```bash
go test ./internal/inference/... -run TestRunningServerOffloadVerdict
go test ./internal/modelswap/... -run TestSwapSaveBeforeReconcileAndInferenceOnlyRestart -v
go test ./internal/backendswap/... -run TestRollbackVerbatim -v
```

Regenerate the byte-golden fixtures after an intentional change to rendered
output (see [Golden tests](#golden-tests-byte-for-byte-fixtures)):

```bash
go test ./internal/orchestrate/... -update
go test ./cmd/villa/... -update
```

There is no separate `test:unit` / `test:integration` / `test:e2e` split and no
watch-mode target — the suite is a single fast `go test ./...` run.

## Test categories

The suite is organised by the package under test (`<pkg>_test.go` files sit
beside their production code in the same package, so tests can reach unexported
functions). Test files span `cmd/villa` and essentially every package under
`internal/` — `find . -name '*_test.go' -printf '%h\n' | sort -u` is the current
list, and the code map in `CLAUDE.md` says what each package owns. Several distinct
testing patterns recur across the tree:

### Table-driven unit tests

Pure-function logic is covered with table-driven cases and `t.Run` subtests. The
memory-fit math (`internal/recommend`) and the host-readiness gate
(`internal/preflight`) are the clearest examples. Inputs are built from the
typed `detect.Known*` / `detect.Unknown*` constructors so a test can express
"GTT envelope is Unknown but RAM is Known" precisely:

```go
p := detect.HostProfile{
    TotalRAMBytes:       detect.KnownBytes(128<<30, "/proc/meminfo:MemTotal"),
    UsableEnvelopeBytes: detect.UnknownBytes("envelope unreadable", ""),
}
floor, ok := conservativeFloor(p)
```

These tests assert exact numeric outcomes (the ~50%-of-RAM degraded floor, the
headroom fraction, the four fit terms) and invariants such as "a derived floor
must never meet or exceed total RAM."

### Golden tests (byte-for-byte fixtures)

Rendered output whose exact text is a contract — Quadlet unit files and `--json`
payloads — is frozen in `testdata/*.golden` and compared **byte-for-byte**. A
shared `-update` flag regenerates the fixtures so an intended change is a
reviewable diff rather than an inline edit.

Golden coverage lives in two places, each with its own `-update` flag scoped to
that package:

- `internal/orchestrate/testdata/` — the rendered Quadlet units
  (`villa-llama.container.golden`, `villa-llama-rocm.container.golden`,
  `villa.network.golden`, `villa-models.volume.golden`, the Open WebUI and
  dashboard units). The fixture input is a fixed, deterministic `RenderInput`
  with an absolute host `ModelsDir` (not live `$HOME`) so the golden is stable
  in CI. Crucially the container image digest is sourced **through the backend
  seam** (e.g. `inference.VulkanBackend()`), never hand-typed, so the golden
  tracks `Backend.Image()` automatically. These Vulkan-rendered fixtures pin
  `backend = "vulkan"` explicitly rather than inheriting `DefaultVillaConfig()`,
  whose default is now `rocm`. The ROCm backend adds its own
  golden pair — `TestRenderROCmContainerGolden` (the rendered ROCm `.container`)
  and `TestRenderROCmEnvGroupFrozen` (the ROCm env/`--group-add` block frozen
  byte-for-byte). Refreeze with `go test ./internal/orchestrate/... -update`.
- `cmd/villa/testdata/` — command-output goldens
  (`inference-pass.json.golden`, `status.json.golden`, `preflight-pass.golden`,
  `bench.json.golden`, `detect.golden.json`, etc.) that lock the `--json` and
  human-readable command contracts against fixed, non-live `Recommendation` /
  status fixtures. A single `update` flag in the `cmd/villa` test package gates
  every command golden; refreeze them all with
  `go test ./cmd/villa/... -update`.

Beyond the byte compare, golden tests also assert specific structural facts
(e.g. the `.container` carries `ContainerName=villa-llama`,
`Network=villa.network`, and an `[Install]` section with
`WantedBy=default.target`).

### Grep-gate seam tests

`internal/inference/seam_test.go` enforces backend-neutrality (Phase-2 success
criterion, INF-03): `TestSeamGrepGate` **fails the build if an imperative
backend assumption leaks outside the seam**. The seam — the only paths allowed
to hold backend literals — is `internal/inference/` plus
`internal/detect/gpu_amd.go`. The gate runs two walks:

- **Walk 1 — `internal/`:** every non-test `.go` file outside the seam is matched
  against five imperative-leak patterns:
  - `runtime.GOOS` / `GOOS ==` platform branching,
  - container **image** literals (`kyuz0`, `docker.io/`, `server-vulkan`, and the
    ROCm tags `rocm-7.2.4` / `rocm-6.4.4` / `rocm7-nightlies`). These anchor on the
    IMAGE context (`:tag` / `tag@`), so a bare backend NAME as a config value —
    `case "rocm-6.4.4":` in `render.go` — is deliberately not a hit,
  - container **device** args (`--device /dev/dri`, `--group-add`,
    `keep-groups`),
  - `podman` **process** invocations (`exec.Command("podman", …)`),
  - **coding-mode llama-server flags** (`codingModeFlagPattern`): the quoted
    literals `"--jinja"`, `"--cache-reuse"`, `"--repeat-penalty"`, which belong
    only to `appendCodingModeArgs` in the two backend files. The anchor on a
    leading double-quote is what lets a doc comment discuss `--cache-reuse` as
    prose without tripping the gate.
- **Walk 2 — `cmd/villa`:** the OS-orchestration tier legitimately invokes
  podman, so the `podman` pattern is dropped here, but it adds a **backend
  marker** pattern (`ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, `Memory access fault`)
  alongside the platform/image/device/coding-mode patterns — a single
  `codingModeFlagPattern` helper feeds both walks, so adding it to one map cannot
  leave the other unguarded. This keeps the v1.1 cmd-tier
  composers (`cmd/villa/backend.go` and siblings) free of any retyped backend
  literal — they compose the `backendswap` core plus the exported inference
  prove primitives instead.

The gate is deliberately scoped to *imperative* leaks; it does not flag
Phase-1 provenance/remediation **strings** that merely name these tools as
findings (those are data, not backend assumptions). The purpose is structural:
a future ROCm/Metal backend (and macOS) must drop in without editing callers.

The same file carries the **positive** dual, `TestROCmMarkerPresence`: it asserts
the ROCm backend's privilege/residency literals (`ROCm0`,
`HSA_OVERRIDE_GFX_VERSION`, `/dev/kfd`) **do** still live in `backend_rocm.go`,
so a refactor that drops or relocates them also fails CI.

### Grep-gate docs tests

`cmd/villa/docs_gate_test.go` applies the same shape to **prose**. Three tests
walk every `.md` in the repo and fail the build on a statement the tree has
stopped supporting:

- **`TestDocsLinkedPathsExist`** — every repo-relative markdown link resolves on
  disk. External URLs and in-page anchors are out of scope (this test does no
  network I/O).
- **`TestDocsMakeTargetsExist`** — every `` `make <target>` `` the docs tell a
  contributor to run is a target the `Makefile` defines. A `VAR=value` prefix is
  skipped, so `` `make LINT_ALL=1 lint` `` resolves to `lint`.
- **`TestDocsNoRetiredClaims`** — a line-scoped denylist of claims that were
  **true once**. Each entry carries the reason it was retired, which is what the
  failure prints, and `retiredClaims` in the test is the list — it is deliberately
  not restated here. It was seeded from the claims a full audit of this repo
  found still asserted, one of which had survived in **four** files long after
  the behaviour it described was deleted.

Each claim requires several tokens to co-occur **on one line** and carries an
`unless` list, so a doc can still discuss a retired behaviour historically —
which these docs deliberately do, to stop it being reintroduced — without
tripping the gate.

> **When you retire a behaviour, add its claim to `retiredClaims` in the same
> commit that removes it.** That is the only way the gate stays ahead of the
> docs rather than recording what someone already had to find by hand.

**What this gate does NOT catch — read before trusting it.** It sees *dead
references* and *retired claims*, both decidable from the tree. It cannot see an
enumeration that has gone incomplete, a description that is simply wrong, or
prose that is stale but self-consistent: a sentence naming a test-function count
that the suite has since grown past does not trip it, and neither does a package
list naming 24 of the packages that exist. (This paragraph used to demonstrate
the point with two live numbers, and both had rotted by the next release — which
demonstrates it better than the numbers did.) Those still need someone reading
the doc against the code. The structural defence against that class is not a test
— it is to stop writing enumerations into prose, which is why the docs now point
at `ls internal/`, `newRoot`, and `grep -rn "func live" cmd/villa` instead of
restating them.

It needs no CI wiring: it is an ordinary Go test, so `make test`, `make check`,
and CI's existing `go test ./...` step all run it.

### Dependency-seam (fake) tests

Command handlers that would otherwise touch a live host are tested through an
injectable `*Deps` struct whose fields are functions. Each test wires a fake
`installDeps` / `lifecycleDeps` / `modelswap.Deps` / `listDeps` to stubs and uses
counters and a recorded call order to assert **exactly which seams fired** —
idempotency, consent, model-pull, config-persist, restart targeting.

Two high-value invariants are asserted this way:

- **Save-before-restart ordering** (`internal/modelswap`,
  `TestSwapSaveBeforeReconcileAndInferenceOnlyRestart`): a fitting model swap
  must run `pull → save → write → restart` in that order, and the restart must
  target **only** the inference service (the network/volume units are left
  untouched). The test records every seam call into a `callOrder` slice and
  asserts `pullIdx < saveIdx < writeIdx < restartIdx`. A no-op swap skips the
  restart entirely (WR-06).
- **Capture-before-mutate + verbatim rollback** (`internal/backendswap`, the
  v1.1 ROCm A/B swap): a backend switch must snapshot the current state
  before touching anything (`TestCaptureBeforeMutate`), restart **only** the
  inference unit (`TestSwapInferenceOnly`), refuse the swap if the snapshot can't
  be captured (`TestCaptureFailureRefuses`), and on a mid-swap error restore the
  original units **byte-for-byte** (`TestRollbackVerbatim`,
  `TestMutateErrorRollsBack`). A same-backend request is a no-op
  (`TestNoOpSameBackend`); ROCm is gated behind a fit guard and a prove-flight
  (`TestRefuseFitGuard`, `TestRefuseProveFlightROCm`, `TestProveGate`).
- **Block-before-side-effects** (`cmd/villa/lifecycle_test.go`,
  `install_test.go`): when an upstream step fails (e.g. the model file cannot be
  resolved from the catalog), the handler must return the blocked exit code
  having fired **zero** write/reload/start seams — it must never render a
  container whose `-m` points at a fabricated GGUF (WR-08). Counters assert
  `writeCalls == 0 && reloadCalls == 0 && len(startCalls) == 0`.

These tests also pin the exit-code contract used by the cobra layer
(`internal/preflight` / `cmd/villa/preflight.go`):

| Exit code | Meaning |
|-----------|---------|
| `0` (`exitPass`) | all checks pass |
| `1` (`exitBlocked`) | an un-overridden BLOCK check failed |
| `2` (`exitWarn`) | passed with warnings (or an overridden block) |
| `130` (`exitInterrupted`) | SIGINT/SIGTERM during the run (128+SIGINT) |

`exitInterrupted` is tree-wide rather than preflight-specific: `main` runs the
command tree under a signal-cancelled context, so a Ctrl-C unwinds through each
command's cleanup (notably `bench --ab`'s deferred backend restore) instead of
killing the process mid-flip. It is deliberately distinct from the verdict codes
above so a scripted caller can tell an interrupt from a BLOCK. See
`cmd/villa/main_test.go`.

### Offload-assertion tests

`internal/inference` carries the proof-of-residency logic — the verdict that the
model actually loaded onto the iGPU rather than silently falling back to CPU.
These tests drive frozen fixtures rather than a live server:

- `offload_test.go` — `scrapeOffloadLog` against captured `*.stderr` fixtures:
  a RADV `offloaded N/N` line → **PASS**; an `llvmpipe` (software renderer) line
  → **FAIL**; `offloaded 0` → **FAIL**; an empty/truncated log → **Unknown**
  (degrades to WARN, never a false FAIL). A sysfs-delta variant reads
  `mem_info_gtt_used` fixtures and bands the GTT delta against the model weight
  (`≥ 0.5×weight → PASS`, `< 0.1×weight → FAIL`, in-between → WARN). It reads
  through the `detect` sysfs seam and never hard-codes `card0`.
- `running_offload_test.go` — the already-running-server verdict: residency
  proven by the journald `load_tensors: Vulkan0 model buffer size = N MiB` line
  (`ROCm0` for the default ROCm backend; `Vulkan0` for the Vulkan fallback,
  `TestRunningServerROCmResidency`), corroborated by a point-in-time GTT floor
  (`TestGTTFloorCorroboration`), with `/props` used only as a config-identity
  drift overlay (`TestRunningServerOffloadPropsDrift`). A ROCm "Memory access
  fault" line forces FAIL (`TestScrapeLoadTensorsResidencyFault`). The v1.1
  code-review hardened the device-buffer parse with two regression tests:
  `TestScrapeLoadTensorsResidencyMaxNotLast` (the largest buffer wins even when
  it is not the last line) and `TestScrapeLoadTensorsResidencyEmptyDeviceToken`
  (an empty device token never mis-keys the residency proof). Every signal
  degrades to a typed Unknown → WARN, never a false PASS.

A parallel set of ROCm preflight gates lives in
`internal/preflight/checks_rocm_test.go` — gfx/kernel/firmware/HSA-override/image
checks, each covering its FAIL and pass branches (e.g.
`TestRunROCmFirmware` exercises both a denied firmware version and an unknown one),
plus `TestRunROCmOffHardwareNoFalseFail` proving the gates never over-block when
run off a real gfx1151.

### Honest A/B benchmark tests

`internal/bench` (the v1.1 `villa bench` honest A/B harness) verifies the
measurement discipline itself rather than any host effect: warmup runs are
discarded from the stats (`TestWarmupDiscarded`), prompt-processing and
token-generation rates are reported separately (`TestSeparatePPTG`), a
non-resident or memory-exhausted run is voided rather than scored
(`TestVoidNonResident`, `TestVoidExhaustionWarn`), both arms of an A/B run use an
identical spec (`TestIdenticalSpecBothSides`), and the harness restores the
original backend/config after the run (`TestBenchABRestoresOriginal`).

## Writing new tests

- **File naming:** Go convention — `<file>_test.go` beside the code it tests,
  in the **same package** (`package orchestrate`, `package inference`, …) so
  unexported helpers are reachable. Test functions are `func TestXxx(t *testing.T)`.
- **Subtests:** group related cases with `t.Run(name, func(t *testing.T){…})`;
  the suite uses this for both table cases and named scenarios.
- **Fakes over mocks:** to exercise a command that touches the host, build the
  package's `*Deps` struct with stub functions rather than mocking — see
  `newFakeInstallDeps`, `newFakeLifecycleDeps`, `newSwapStub`. Record calls in a
  counter or a `callOrder` slice and assert the exact seam interaction.
- **Fixtures, not live IO:** put captured stderr/sysfs/JSON inputs under the
  package's `testdata/` directory and read them via a `readFixture(t, rel)`
  helper. `testdata/` is ignored by the Go toolchain so fixtures never compile.
- **Golden output:** if your test asserts rendered text byte-for-byte, follow
  the `-update` discipline — gate the write behind the package's `update` flag
  so `go test ./<pkg>/... -update` refreezes the fixture, and keep the fixture
  input deterministic (fixed paths, seam-sourced digests, no live `$HOME`).
- **Mark helpers:** call `t.Helper()` at the top of any assertion/fixture helper
  so failures report the caller's line.
- **Temp dirs:** use `t.TempDir()` for any path a fake needs to write into — it
  is auto-cleaned and unique per test.
- **Concurrency:** if your code starts a goroutine, its test must pass under
  `make test-race`, not just `make test`. `make check` runs both, so a data race
  fails the pre-commit gate and CI's race step alike.

## Coverage requirements

No coverage threshold is configured in the repository — there is no
`coverprofile` gate in the `Makefile` and no coverage tooling wired into CI. To
inspect coverage locally, run:

```bash
go test ./... -cover
go test ./... -coverprofile=cover.out && go tool cover -html=cover.out
```

The suite is large and grows with every verb, so this doc does not carry a count
that would go stale. To take one:

```bash
find . -name '*_test.go' | wc -l                      # test files
grep -rh '^func Test' --include='*_test.go' . | wc -l # test functions
```

Whatever those print is an observed total, not an enforced threshold.

## On-hardware UAT

The automated suite is entirely off-hardware. Validating that a recommended
config actually loads and runs on a real AMD Strix Halo (gfx1151) — including the
v1.1 ROCm backend and the honest A/B benchmark — is a **separate, manual
UAT step** performed against a live host (Phases 8/9/10 ran this on real
gfx1151). It is not invoked by `go test` and is not required for CI: the unit
suite proves the parsing, ordering, rendering, and verdict logic against frozen
fixtures, while the UAT proves the integrated stack comes up healthy on the
detected hardware.

## CI integration

`.github/workflows/ci.yml` runs on **every push and pull request**. It needs no
services, secrets, or GPU — the suite is off-hardware by design, so it runs on a
standard Linux runner. Six gates, in order:

| Step | What it proves |
|------|----------------|
| CGO-free static build (SC#4) | `CGO_ENABLED=0 go build ./...`. Load-bearing beyond tidiness: the `villa-websafe` unit bind-mounts this binary into a distroless container with no libc, so a dynamically-linked build fails to exec there. |
| vet + test | `go vet ./... && go test ./...`, mirroring the first half of `make check`. |
| race gate (CR-01) | The whole tree under `-race`, mirroring `make test-race`. |
| `go mod verify` | Supply-chain integrity — `go.sum` hashes match the checksum DB. Stronger than registry-existence checking. |
| lint (new issues only) | golangci-lint at the version in `.golangci-version`, gated `if: github.event_name == 'pull_request'`. That restriction is load-bearing: on a push there is no base for `only-new-issues` to diff against, so the action would silently degrade to linting the whole tree. |
| TUI-stack grep | Asserts the guided install stays on the standard library — no `huh`/`bubbletea`/`lipgloss` reappearing in `go.mod`. |

The local gate that mirrors it:

```bash
make check         # vet + test + test-race
make lint          # the new-issues gate
make build-static  # the SC#4 CGO-free build — `make check` does NOT cover it
```

There is no coverage step in CI, matching the absence of a configured threshold.
