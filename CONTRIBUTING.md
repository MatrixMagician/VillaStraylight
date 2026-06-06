<!-- generated-by: gsd-doc-writer -->
# Contributing to VillaStraylight

VillaStraylight is a single Go CLI (`villa`) that auto-detects an AMD Strix Halo
host, recommends a memory-fitting model/quant/context, generates rootless Podman
Quadlet units, and orchestrates a strictly-local AI stack. First-party code is
Go only; AI services are integrated OSS containers, not rebuilt.

Contributions are welcome. This guide covers the setup, the gates a pull request
must pass, and the invariants a contribution must not break.

## Development setup

For prerequisites (Go toolchain, Podman) and first-run instructions, see
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). For configuration semantics, see
[docs/CONFIGURATION.md](docs/CONFIGURATION.md); for the system layout, see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

The short version, run from the repo root:

```bash
make build   # build the villa CLI to ./villa
make run     # run the control-plane CLI directly (go run ./cmd/villa)
make test    # run the Go test suite
make check   # go vet + tests (the minimum a PR must pass)
```

Run `make help` to list all available targets.

## Coding standards

- **Language**: Go only for all first-party code (CLI, detection, orchestration,
  dashboard backend, gateway). The project ships as a single static binary — do
  not add dependencies that require build-time system libraries or break that
  goal (for example, the full `containers/podman/v5` bindings module).
- **Formatting**: `gofmt` and `goimports`. Run `make fmt` before committing.
  Formatting is enforced by the linter (`gofmt`, `goimports`).
- **Vetting**: `go vet` must be clean (`make vet`).
- **Linting**: the project uses `golangci-lint` (config in `.golangci.yml`) with
  `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `gofmt`,
  `goimports`, `misspell`, and `revive` enabled. Run `make lint`. If
  `golangci-lint` is not installed, `make lint` falls back to `go vet`; install
  `golangci-lint` to run the full set locally. `errcheck` is relaxed in
  `_test.go` files only.

## Tests a PR must pass

All tests must pass (`make test`) and the lint set must be clean before a pull
request is merged. See [docs/TESTING.md](docs/TESTING.md) for the full testing
guide. Two test disciplines are load-bearing in this repo and contributions must
respect them:

- **Byte-golden tests** — deterministic command output (detect, recommend,
  preflight, inference, status) is asserted byte-for-byte against fixtures in
  `cmd/villa/testdata/` (for example `detect.golden.json`, `status.json.golden`,
  `preflight-pass.golden`). The tests share an `-update` flag; golden files are
  regenerated only by an explicit `go test ./... -run <Test> -update`, never by
  accident. If your change alters command output, regenerate and review the diff
  in the same commit. Treat the `--json` output as an **append-only** contract:
  add fields when you must, but do not rename or remove existing ones — downstream
  consumers and the golden fixtures depend on the stable shape.
- **Grep-gate tests** — invariant strings in generated artifacts and CLI output
  (loopback addresses, telemetry-disabled flags, published ports) are asserted by
  substring checks. Do not weaken or delete these assertions to make a change
  pass; the assertion is the contract.
- **Backend-seam grep-gate** — `internal/inference/seam_test.go` enforces
  backend-neutrality so a future ROCm/Metal backend (and macOS) drops in without
  touching callers. `TestSeamGrepGate` (negative gate) fails if imperative backend
  literals — `runtime.GOOS`/platform branches, container image tokens (kyuz0,
  `docker.io/`, `server-vulkan`, `rocm-7.2.4`), container device args
  (`--device /dev/dri`, `--group-add`, keep-groups), raw backend markers
  (`ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, memory-access-fault), or `podman` process
  invocations — leak outside the seam (`internal/inference/` and
  `internal/detect/gpu_amd.go`). `TestROCmMarkerPresence` (positive gate) fails if
  the ROCm-only literals are dropped from `backend_rocm.go`. When adding a backend
  or touching the inference path, keep backend-specific literals behind the seam;
  do not relax the gate to make a caller compile.

## Core invariants — do not break these

These are product guarantees, not preferences. A change that violates one will be
rejected even if tests are green.

- **Strictly local / zero telemetry** — no telemetry from first-party
  components. Outbound network access is limited to image and model pulls.
  Generated stack units keep upstream telemetry off (for example, Open WebUI is
  configured telemetry-disabled). Do not introduce phone-home behavior, analytics,
  or non-pull outbound traffic.
- **Loopback-only services** — the dashboard, chat (Open WebUI), and inference
  endpoints bind to and are published on `127.0.0.1` only
  (dashboard `127.0.0.1:8888`, chat `127.0.0.1:3000`, inference
  `127.0.0.1:8080`). Never bind a first-party or published service to `0.0.0.0`
  or a routable interface.
- **Offload-assert / no silent CPU fallback** — inference must prove iGPU
  offload. The `villa inference` commands encode a hard exit-code contract:
  `0` = offload proven (and chat OK), `2` = offload unverifiable (warn), `1` =
  CPU fallback (fail). A model that would silently run on CPU must FAIL loudly,
  not degrade quietly. Preserve this assertion when touching the inference path.
  As of v1.1 the ROCm backend is an opt-in alternative to the Vulkan default; the
  same offload-assert contract applies to both, and backend-specific literals stay
  behind the inference seam (see the backend-seam grep-gate above).
- **Config is the source of truth** — runtime behavior derives from the resolved
  config, not from ad-hoc constants scattered through commands. Generated Quadlet
  units, dashboard wiring, and printed URLs must read from config. See
  [docs/CONFIGURATION.md](docs/CONFIGURATION.md).
- **Integration-first** — reuse mature OSS (llama.cpp `llama-server`, Open WebUI,
  Podman Quadlet). Build only the control plane; do not re-implement an AI service
  or a custom chat UI (explicitly out of scope).

When in doubt, add or extend a grep-gate or golden test that locks the invariant
rather than relying on review to catch a regression.

## Commit and PR guidelines

- **Commit style**: this project uses Conventional Commit prefixes —
  `feat:`, `fix:`, `test:`, `docs:`, `chore:`. Keep commits atomic: one logical
  change per commit, with code and its tests (and any regenerated golden files)
  together.
- **Before opening a PR**: ensure `make check` passes and `make lint` is clean.
  Regenerate golden files in the same commit as the output change that requires
  them.
- **PR description**: state what changed and why, call out any new dependency
  (and justify it against the single-static-binary goal), and confirm none of the
  core invariants above are affected. If output fixtures changed, say so.
- **Scope discipline**: keep changes focused on the stated goal; avoid unrelated
  refactors in the same PR.
- **Merge flow**: work lands on `main` via a GitHub pull request (v1.0 shipped as
  PR #1, v1.1 as PR #2). Releases are tagged on `main` (`v1.0`, `v1.1`). There is
  no CI configured in this repository yet, so the build, test, and lint gates above
  are the contributor's responsibility to run locally before requesting a merge.

## Issue reporting

There is no issue template configured yet. When filing a bug, include:

- the exact `villa` command and flags you ran, and the full output;
- expected vs. actual behavior, and the exit code;
- your host details — Fedora version, kernel version, GPU (Strix Halo / gfx1151),
  total/usable memory, Podman version, and the selected backend (Vulkan / ROCm);
- relevant config (with secrets redacted) and any generated Quadlet unit content.

For feature requests, describe the use case and how it fits the strictly-local,
integration-first scope.

## License

By contributing, you agree that your contributions are licensed under the
project's [MIT License](LICENSE).
