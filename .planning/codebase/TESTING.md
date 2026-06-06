# Testing Patterns

**Analysis Date:** 2026-06-07

VillaStraylight is tested **entirely off-hardware in CI**: pure cores plus
fixture/golden/journal-driven tests, with host effects driven through injected
`Deps`. There is no live GPU, podman, or systemd in CI. Live `gfx1151` behaviour
is validated separately by manual on-hardware UAT during phase execution.

Scale: ~383 top-level `Test*` functions across 16 packages, plus ~98+ `t.Run`
table sub-cases (≈561 named cases total). No `//go:build` integration tags — the
whole suite runs under a plain `go test ./...`.

## Test Framework

**Runner:**
- Go standard `testing` package (Go 1.26.x). No third-party test framework.
- No external assertion library — direct `if got != want { t.Errorf(...) }`,
  `bytes.Equal`, `reflect.DeepEqual`.

**Run Commands:**
```bash
make test          # go test ./...
make check         # go vet ./... + go test ./...   (default pre-commit gate)
make vet           # go vet ./...
make lint          # golangci-lint run (falls back to go vet if not installed)
make fmt           # gofmt -w .

go test ./...                          # run everything
go test ./internal/inference/...       # one package
go test ./cmd/villa -run TestJSONGolden
go test ./... -update                  # refreeze ALL golden files (see below)
```

Linters configured in `.golangci.yml`: `errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`, `gofmt`, `goimports`, `misspell`, `revive`
(`errcheck` excluded for `_test.go`).

## Test File Organization

**Location:** Co-located with source in the same package (white-box). Test file
mirrors its source: `running_offload.go` → `running_offload_test.go`.

**Fixtures:** Per-package `testdata/` directory (Go ignores `testdata` in builds):
- `cmd/villa/testdata/` — `--json`/preflight golden contracts.
- `internal/orchestrate/testdata/` — rendered Quadlet unit goldens.
- `internal/inference/testdata/` — `load_tensors_*.txt` journals,
  `*.stderr` device-probe fixtures, `sysfs/` GTT readings.
- `internal/detect/testdata/` — `vulkaninfo`/`rocminfo` captures, `/sys` files.
- `internal/catalog/testdata/`, `internal/metrics/testdata/`,
  `internal/preflight/testdata/`, `internal/download/testdata/`.

**Per-package test counts (named `Test*`/`Benchmark` funcs):**

| Package | ~Tests |
|---------|-------:|
| `cmd/villa` | 121 |
| `internal/detect` | 43 |
| `internal/inference` | 36 |
| `internal/orchestrate` | 28 |
| `internal/preflight` | 31 |
| `internal/dashboard` | 29 |
| `internal/recommend` | 17 |
| `internal/backendswap` | 13 |
| `internal/status` | 11 |
| `internal/download` | 10 |
| `internal/catalog` / `llm` / `metrics` / `bench` | 6–8 each |
| `internal/config` / `modelswap` | 6–7 each |

## Test Structure

**Table-driven** is the default for pure-core logic (~98 `t.Run` sub-cases):

```go
func TestX(t *testing.T) {
    tests := []struct{
        name    string
        in      ...
        want    Status
    }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Fn(tt.in)
            if got != tt.want { t.Errorf("...") }
        })
    }
}
```

**Fixture/journal-driven** for inference residency and detection: read a captured
journal/probe file from `testdata/` and assert the verdict (`readFixture(t, ...)`
helpers; `internal/inference/running_offload_test.go`).

**Fake-Deps / injected-seam** for the command layer: a `fake*Deps` (or a
fully-stubbed core `Deps`) drives the thin cobra caller with NO live host. Example
`newStatusDeps` in `cmd/villa/status_test.go` builds a stubbed `status.Deps` — a
healthy residency journal, a matching `/props`, a `t.TempDir()` GTT reading, and a
real `orchestrate.Render` of the loopback stack — then asserts the `--json`
golden, the `127.0.0.1`/no-telemetry privacy property, and exit-code mapping
(all-PASS→0, any-WARN→2, any-FAIL→1; 503→loading→WARN not FAIL).

## Golden / Contract Tests

Byte-frozen output contracts (`testdata/*.golden*`):

- **`cmd/villa`**: `detect.golden.json`, `recommend.golden.json`,
  `status.json.golden`, `bench.json.golden`, `inference-pass/fail.json.golden`,
  `preflight-pass/warn/force.golden`.
- **`internal/orchestrate`**: rendered units —
  `villa-llama.container.golden`, `villa-llama-rocm.container.golden`,
  `villa-openwebui.container.golden`, `villa-dashboard.service.golden`,
  `villa.network.golden`, `villa-models.volume.golden`,
  `villa-openwebui.volume.golden`.

**Refreeze with `-update`:** each test package declares
`var update = flag.Bool("update", false, "regenerate golden files")`
(`cmd/villa/detect_test.go:13`, shared across that package's tests). Running with
`-update` rewrites the golden and `t.Logf("updated ...")`; without it, a mismatch
is a hard `t.Errorf` diff. Goldens evolve **append-only** with a schema bump — a
fixture profile (`fixtureProfile()`) deliberately mixes Known and Unknown fields
to lock both serialized shapes (e.g. detect schema bumped to 2 for
`rocm_readiness`).

### Grep-gate tests (architectural guards)

- **`TestSeamGrepGate`** (`internal/inference/seam_test.go`) — walks `internal/`
  AND `cmd/villa`; FAILS if imperative backend literals (`runtime.GOOS`, image
  tags `kyuz0`/`docker.io/`/`server-vulkan`/`rocm-7.2.4`, `--device /dev/dri`/
  `--group-add`/`keep-groups`, `exec.Command("podman", ...)`) leak outside the
  inference/detect seam. Deliberately scoped to imperative leaks, not finding
  strings.
- **`TestROCmMarkerPresence`** (`internal/inference/seam_test.go:152`) — positive
  gate asserting the ROCm backend markers DO exist where required.

## Offload-Assert Regression Tests (v1.1)

The residency parser is the project's correctness keystone (silent CPU fallback =
FAIL). v1.1 hardened it with targeted regression tests in
`internal/inference/running_offload_test.go`:

- **`TestScrapeLoadTensorsResidencyMaxNotLast`** — a real non-zero device-buffer
  line followed by a `0.00 MiB` first-pass estimate (same token) must keep the MAX
  and report PASS, not flip to a false "0 MiB → no weights resident" FAIL.
  All-zero device lines still FAIL.
- **`TestScrapeLoadTensorsResidencyEmptyDeviceToken`** — a zero-value
  `ResidencyMarkers{DeviceToken:""}` makes `strings.Contains(line,"")` true for
  every line; this must degrade to WARN (could-not-evaluate), never a false PASS
  on a CPU-only journal.
- **`TestRunROCmFirmware`** (`internal/preflight/checks_rocm_test.go:89`) —
  extended in v1.1 to cover both a Known-denied firmware (FAIL) and the
  unevaluable branch.

## Mocking / Test Doubles

No mocking library. Doubles are hand-written:
- **`fake*Deps`** structs implement a command's `Deps` (function-field/interface
  injection): `fakeConfigDeps`, `fakeInstallDeps`, `fakeUninstallDeps`,
  `fakeLifecycleDeps`.
- **`*ForTest` helpers** expose an internal seam over real fixture files:
  `detect.GTTUsedBytesForTest(dir)`, `detect.GPUBusyPercentForTest`,
  `inference.rocmMarkersForTest`.
- **`t.TempDir()`** for transient `/sys`-style files (GTT readings) — written then
  read through the real parser.
- **What to mock:** host effects only (exec, sockets, `/sys`, config load) via
  `Deps`. **What NOT to mock:** the pure cores — exercise them directly over real
  fixtures so the parse/verdict logic itself is under test.

## Fixtures and Factories

- **`fixtureProfile()`** (`cmd/villa/detect_test.go:17`) — a deterministic
  `detect.HostProfile` (NOT live hardware) mixing Known/Unknown fields to lock the
  full `--json` shape.
- **`readFixture(t, name)`** — loads a `testdata/` journal/stderr for residency
  tests.
- **`loopbackUnits(t)`** (`cmd/villa/status_test.go`) — renders the real stack via
  `orchestrate.Render` so goldens/privacy assertions reflect the actual
  `PublishPort=127.0.0.1` mechanism.

## Coverage

No enforced coverage threshold. `go test ./... -cover` available.

## Test Types

- **Unit / pure-core:** table-driven over `internal/*` cores (the bulk).
- **Contract/golden:** `--json`, dashboard, rendered-unit byte-equality.
- **Architectural guards:** `TestSeamGrepGate`, `TestROCmMarkerPresence`.
- **Command integration (off-host):** thin cobra callers driven through stubbed
  `Deps` — no live host, exit-code + output asserted.
- **On-hardware UAT (manual, NOT in CI):** live `gfx1151` user-acceptance testing
  performed during phase execution (Phases 8, 9, 10, 11). This is the
  off-hardware-vs-on-hardware split: CI proves logic over fixtures; real
  inference/residency/dashboard behaviour is signed off manually on the Strix Halo
  box. New offload/detection logic should add a fixture-driven regression test AND
  be flagged for on-hardware UAT.

## Common Patterns

**Status/verdict assertion (residency):**
```go
markers := ResidencyMarkers{DeviceToken: "Vulkan0"}
if r := scrapeLoadTensorsResidency(journal, markers); r.Status != StatusPass {
    t.Fatalf("... → %s, want PASS", r.Status)
}
```

**Golden assertion:**
```go
if *update { os.WriteFile(golden, buf.Bytes(), 0o644); return }
want, _ := os.ReadFile(golden)
if !bytes.Equal(buf.Bytes(), want) { t.Errorf("does not match golden\n--- got ---\n%s", buf.String()) }
```

**Remediation invariant:**
```go
for _, r := range results {
    if r.Status != StatusPass && r.Remediation == "" {
        t.Errorf("%s non-PASS without remediation", r.ID)
    }
}
```

---

*Testing analysis: 2026-06-07*
