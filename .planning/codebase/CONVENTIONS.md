# Coding Conventions

**Analysis Date:** 2026-06-07

VillaStraylight is a single-language Go project: the `villa` control plane
(`cmd/villa`) plus pure logic cores under `internal/*`. These conventions are
enforced by `gofmt`/`goimports`, `.golangci.yml`, and a set of grep-gate tests
(notably `TestSeamGrepGate`). Follow them when adding code — they are not
suggestions, several are guarded by failing tests.

## Naming Patterns

**Files:**
- Lowercase, no underscores for source: `value.go`, `backend.go`, `running_offload.go`
  (underscore only as a topic separator, e.g. `checks_rocm.go`, `dashboard_unit.go`).
- Tests mirror their source file with `_test.go`: `backend.go` → `backend_test.go`,
  `running_offload.go` → `running_offload_test.go`.
- Topic-grouped check files in `internal/preflight`: `checks_gpu.go`,
  `checks_podman.go`, `checks_selinux.go`, `checks_linger.go`, `checks_rocm.go`.

**Functions:**
- Standard Go `CamelCase` (exported) / `camelCase` (unexported).
- **`live*Deps` constructors** wire a pure core's `Deps` struct to the real host.
  One per command in `cmd/villa`: `liveStatusDeps`, `liveDashboardDeps`,
  `liveInstallDeps`, `liveConfigDeps`, `liveBenchDeps`, `liveBackendSwapDeps`,
  `liveLifecycleDeps`, `liveLingerDeps`, `liveListDeps`, `liveSwapDeps`,
  `liveUninstallDeps` (see `cmd/villa/status.go:157`). This is the single place
  host effects (exec, sockets, `/sys` reads, config load) are bound.
- **`*ForTest` helpers** expose an internal seam to tests in another package
  without widening the public API: `detect.GTTUsedBytesForTest`,
  `detect.GPUBusyPercentForTest`, `inference.rocmMarkersForTest`. Use this
  naming for any test-only accessor.
- **`fake*Deps` types** are test doubles for a command's `Deps`:
  `fakeConfigDeps` (`cmd/villa/config_test.go:12`), `fakeInstallDeps`,
  `fakeUninstallDeps`, `fakeLifecycleDeps`.

**Variables:**
- Short receiver names (`b backendVulkan`, `r CheckResult`); descriptive locals.
- The golden `-update` flag is a package-level `var update = flag.Bool(...)`
  declared once per test package (`cmd/villa/detect_test.go:13`) and shared.

**Types:**
- Typed `Optional` wrappers (`Bytes`/`Str`/`Int`/`Bool`) instead of bare
  primitives for any detected value — see Typed-Unknown below.
- Interface seams named for the role: `Backend`, `Deps`, `RenderInput`,
  `CheckResult`, `ResidencyMarkers`, `ResidencyProof`.

## Code Style

**Formatting:**
- `gofmt` (`make fmt` runs `gofmt -w .`). Tabs, standard Go layout.
- `goimports` enforced via `.golangci.yml` — imports are grouped and ordered.

**Linting (`.golangci.yml`, `make lint` / `make check`):**
- Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`,
  `gofmt`, `goimports`, `misspell`, `revive`.
- `run.timeout: 3m`.
- `errcheck` is disabled for `_test.go` files (the only exclude rule).
- `make lint` falls back to `go vet` if `golangci-lint` is not installed; CI
  expects the full linter set. `make check` = `go vet` + `go test ./...`.

## Import Organization

Three goimports-managed groups separated by blank lines (see
`cmd/villa/dashboard.go:1`):

1. Standard library (`fmt`, `os`, `path/filepath`)
2. Third-party (`github.com/spf13/cobra`, `github.com/go-chi/chi/v5`)
3. First-party (`github.com/MatrixMagician/VillaStraylight/internal/...`)

**Path Aliases:** None — full module-qualified import paths
(`github.com/MatrixMagician/VillaStraylight/...`).

## Core Architectural Conventions

These are the load-bearing patterns. New code MUST follow them.

### Typed-Unknown degradation (never bare 0 / never panic)

Every detection probe returns a typed Optional (`detect.Bytes`/`Str`/`Int`/`Bool`,
`internal/detect/value.go`) that distinguishes "couldn't detect" (`Known=false`)
from a legitimate zero (`Known=true, Value=0`). Construct with `KnownX(v, src)` on
success and `UnknownX(reason, raw)` on a missing tool or unparseable output —
never return a bare `0`, and never `panic`. `Source` carries provenance (surfaced
under `-v`); `Raw` captures offending output (never serialized). This is the
"no false-green" rule (D-08): an undetected envelope must never read as a
confident value downstream (recommender, `--json`, dashboard).

### Config is the single source of truth

`config.VillaConfig` drives generation. Quadlet units (`.container`/`.network`/
`.volume`/`.service`) are **regenerated** from config via `orchestrate.Render`
(`internal/orchestrate/render.go`), never hand-edited. To change a unit, change
the config/template and re-render — the golden tests in
`internal/orchestrate/testdata/*.golden` lock the rendered output byte-for-byte.

### Pure-core + injectable-seam

- Pure logic lives in `internal/*` cores (no host I/O): `detect`, `recommend`,
  `inference` (parsing/verdicts), `preflight` (checks), `status` (read-model),
  `catalog`, `config`.
- Host effects (exec, Unix sockets, `/sys`, filesystem) are injected via a `Deps`
  struct, wired only by a `live*Deps` constructor in `cmd/villa`.
- `internal/orchestrate` is the **only intentionally impure** module (it shells to
  podman/systemd and writes unit files).
- Consequence: every command is testable off-hardware by passing a `fake*Deps`.

### Backend interface seam + fail-closed resolver

The `Backend` interface (`internal/inference/backend.go`) is the single
polymorphism point. `BackendFor(name)` is the ONLY place a config `backend`
string maps to a concrete implementation; it **fails closed** — an unknown/typo'd
value returns an actionable error, never a silent fallback to Vulkan. Every other
site depends on the `Backend` interface, never on `backendVulkan`/`backendROCm`.

```go
func BackendFor(name string) (Backend, error) {
    switch name {
    case "", "vulkan": return backendVulkan{}, nil
    case "rocm":       return backendROCm{}, nil
    default:           return nil, fmt.Errorf("unknown inference backend %q: ...", name)
    }
}
```

### Backend marker strings stay behind the seam

Imperative backend assumptions — `ROCm0`/`Vulkan0` device tokens,
`HSA_OVERRIDE_GFX_VERSION`, container image tags (`kyuz0`, `docker.io/`,
`server-vulkan`, `rocm-7.2.4`), `--device /dev/dri`, `--group-add`, `keep-groups`,
`runtime.GOOS` branches, and `exec.Command("podman", ...)` — are confined to
`internal/inference/` + `internal/detect/gpu_amd.go` + `internal/orchestrate`.
**`TestSeamGrepGate`** (`internal/inference/seam_test.go`) walks both `internal/`
and `cmd/villa` and FAILS the build if these literals leak outside the seam.
**`TestROCmMarkerPresence`** is the positive counterpart asserting the ROCm
markers DO exist where required. This keeps a future ROCm/Metal/macOS backend a
drop-in.

### Byte-frozen output contracts (golden, append-only)

`--json`, dashboard, and rendered-unit outputs are byte-frozen golden contracts
(`testdata/*.golden*`). They evolve **append-only** and are **schema-bumped**
(`SchemaVersion`, e.g. rocm_readiness bumped detect schema to 2). Never reshape an
existing field; add new fields and bump the schema so existing consumers stay
green.

### Offload-asserting (silent CPU fallback = FAIL)

Inference health does not trust "server responded". `ResidencyProof` /
`RunningOffloadVerdict` (`internal/inference/running_offload.go`) parse the
`load_tensors` journal for backend device-buffer residency and cross-check the
GTT envelope. A silent or partial CPU fallback is a **FAIL**, not a PASS. The
parse keeps the MAX device-buffer line (not last-write), and an empty device
token degrades to WARN, never a false PASS (see the regression tests in
TESTING.md).

## Error Handling

- Return errors up; wrap with context using `fmt.Errorf(... %w ...)` (~60 of ~96
  `fmt.Errorf` sites wrap with `%w` for unwrap chains). Reserve non-`%w` for
  leaf/user-facing messages.
- **Fail closed** on untrusted input (hand-edited config): error, never a silent
  default that could select a privileged device path (`BackendFor`).
- **Refuse-with-remediation** in preflight: every non-PASS `CheckResult` carries a
  non-empty `Remediation` string with the exact fix command (e.g.
  `setsebool -P container_use_devices=true`, `loginctl enable-linger`). This is
  enforced by `TestRunEveryNonPassHasRemediation`
  (`internal/preflight/preflight_test.go:79`). Blocking failures map to exit code
  1 unless explicitly overridden with `--force`.

## Logging

No logging framework. The CLI writes human tables / `--json` to an injected
`io.Writer` (e.g. `renderDetect(&buf, ...)`), keeping output testable and
contract-frozen. Provenance lives in the typed Optionals (`Source`), surfaced
under `-v`. No telemetry from first-party components (privacy constraint; the
dashboard publishes on `127.0.0.1` only, asserted in tests).

## Comments

- Every file opens with a package/file-level doc comment stating its role and the
  decision IDs it implements (e.g. `D-01`, `D-08`, `D-13/D-16`, `SC#4`,
  `INF-03`). Preserve and extend these references when editing.
- Decision/requirement IDs (`D-NN`, `REQ-*`, `SC#N`, `T-6-03`) are the canonical
  cross-reference to `.planning/` docs — cite them in new code.
- Test functions carry a doc comment explaining the invariant being guarded and
  WHY (often the failure mode it prevents) — see the residency regression tests.

## Function & Module Design

- **`Deps` struct injection**: a command's host dependencies are a struct of
  function fields / interfaces, built by `live*Deps()` and overridable by tests.
- **Thin cobra callers**: `cmd/villa/*.go` commands are thin wrappers that call
  `live*Deps()` then delegate to a pure core; logic lives in `internal/*`.
- **Single polymorphism point**: choose a concrete backend only via `BackendFor`.
- Exports: package APIs are deliberately narrow; test-only access goes through
  `*ForTest` helpers rather than exporting internals. No barrel files.

## GOTCHA — dashboard restart after rebuild

`villa dashboard` runs as a long-lived user service that holds the **old**
binary. After `make build`, dashboard code changes do NOT take effect until you
restart the service:

```bash
make build
systemctl --user restart villa-dashboard.service
```

Forgetting this makes dashboard edits appear to have no effect.

---

*Convention analysis: 2026-06-07*
