---
phase: 19-vector-store-local-embeddings-services
plan: 02
subsystem: cmd/villa (install lifecycle)
tags: [install, memory, qdrant, embeddings, pre-stage, proof, gate, seam, honesty-by-construction, priv-04, infra-02]
requires:
  - orchestrate.EmbedGGUFFilename() (Plan 19-01 exported single-source accessor)
  - orchestrate.EmbedImage() (Plan 19-01 helper-image accessor for proof reachability)
  - config.VillaConfig memory_* fields + LoadVilla() persisted value (Phase-18 spine)
  - internal/download.PullModel via the cmd/villa pullFn seam
provides:
  - nomicEmbedShard (verified nomic-embed-text-v1.5 Q8_0 GGUF pre-stage source — size 146146432 / SHA256 3e2434…c3b7)
  - installDeps memory seams: loadedMemoryEnabled / embedModelPresent / ensureEmbedModel / memoryProofFn
  - runInstall memory steps (pre-stage → start villa-qdrant+villa-embed → 768-dim+Qdrant-writable proof) gated on PERSISTED memory_enabled
  - qdrantServiceName / embedServiceName consts
  - evalMemoryProof pure core + memoryProof/memoryProofInput verdict types
affects:
  - Plan 19-03 (dev-box RepoDigest + /v1/embeddings checkpoint:human-verify exercises this pre-stage + proof on hardware)
  - Phase 20 (OWUI env wiring to the now-installed villa-embed/villa-qdrant)
  - Phase 22 (measured memory footprint of the pre-staged stack)
tech-stack:
  added: []
  patterns:
    - "Memory gate keyed off the PERSISTED config.LoadVilla().MemoryEnabled (loadedMemoryEnabled seam), NOT the always-false DefaultVillaConfig() seed — T-19-16 silent-failure fix"
    - "Install-time controlled GGUF pre-stage via the existing verified download path → zero runtime download (PRIV-04/D-07); idempotent (absent-only) + dry-run-skipped"
    - "Honesty-by-construction install proof: offline 768-dim /v1/embeddings vector + Qdrant writable PUT/DELETE round-trip; FAIL refuses-with-remediation (exitBlocked), never a silent skip (D-09)"
    - "Container-DNS-only proof reachability via fixed-arg podman run --entrypoint curl over villa.network; helper image sourced from orchestrate.EmbedImage() accessor (no re-typed image literal — TestSeamGrepGate stays green)"
    - "Single-source GGUF filename bound unconditionally: nomicEmbedShard.Filename == orchestrate.EmbedGGUFFilename() (Pitfall 3, no TODO branch)"
key-files:
  created:
    - cmd/villa/install_memory.go
  modified:
    - cmd/villa/install.go
    - cmd/villa/install_test.go
decisions:
  - "Tasks 1 + 2 committed together: both modify the SAME runInstall steps + installDeps seam struct and are mutually compile-dependent, so a split commit would not build standalone (mirrors 19-01's coupled-unit single-commit precedent)"
  - "Proof reachability uses `podman run --rm --network villa --entrypoint curl <EmbedImage()>`; the cmd/villa seam-gate walk does NOT forbid podman invocations (only image/device/marker literals), and the image comes from the accessor — so the gate stays green"
  - "memoryProof has no WARN tier (unlike installReadiness): a memory stack that cannot answer 768-dim embeddings or accept a write is a confident known-bad the user opted into → FAIL"
metrics:
  duration: ~25 min
  completed: 2026-06-09
  tasks: 2
  files: 3
---

# Phase 19 Plan 02: Memory-Stack Install Wiring Summary

Wired the two Plan-19-01 rendered memory services into the `villa install` lifecycle: (1) pre-stage the pinned `nomic-embed-text-v1.5` Q8_0 GGUF into `villa-models` at install via the existing verified `internal/download` path so runtime is zero-download (PRIV-04/D-07); (2) start `villa-qdrant.service` + `villa-embed.service` after inference + Open WebUI, gated on the PERSISTED `memory_enabled`; (3) prove the stack with an offline 768-dim `/v1/embeddings` smoke + a Qdrant writable round-trip that refuses-with-remediation on failure (D-09). All three steps are no-ops when `memory_enabled=false` and under `--dry-run`.

## What Was Built

- **`cmd/villa/install_memory.go`** (new) — `nomicEmbedShard` (verified size 146146432 + SHA256 `3e2434…c3b7`, provenance commented), `qdrantServiceName`/`embedServiceName` consts, `embedModelPath`, the live seams (`liveLoadedMemoryEnabled` → `config.LoadVilla().MemoryEnabled` fail-soft to false; `liveEmbedModelPresent`; `liveEnsureEmbedModel` pulling a single-shard `CatalogModel` via `pullFn`), and the Task-2 proof: `memoryProof`/`memoryProofInput` types, the PURE `evalMemoryProof` core, and `liveMemoryProof` (fixed-arg `podman run --entrypoint curl` over `villa.network`, helper image from `orchestrate.EmbedImage()`).
- **`cmd/villa/install.go`** — four new `installDeps` seams (`loadedMemoryEnabled`/`embedModelPresent`/`ensureEmbedModel`/`memoryProofFn`); `runInstall` sets `cfg.MemoryEnabled = d.loadedMemoryEnabled()` right after the `DefaultVillaConfig()` seed (the authoritative gate, NOT the false seed — T-19-16); a step-6b pre-stage (absent-only, dry-run-skipped); a step-9b Qdrant-then-embed start; a step-10b proof that returns `exitBlocked` on FAIL; all wired live in `liveInstallDeps`.
- **`cmd/villa/install_test.go`** — extended `fakeInstallDeps` with the memory seam controls/counters; added `TestInstallMemoryGateUsesPersistedConfig` (end-to-end through the loaded gate, both polarities), `TestInstallMemoryServices` (pre-stage ordering, idempotency, off/dry-run), `TestEmbedGGUFFilenameSingleSource` (unconditional accessor equality), `TestNomicShardValues`, `TestInstallMemoryProofPass/Fail`, `TestInstallMemoryProofSkippedWhenOffOrDryRun`, `TestEvalMemoryProof` (pure-core table).

## Verification

- `go build ./...` — Success.
- `go test ./cmd/villa/ -count=1` — 283 passed (all new memory cases + existing install tests unchanged; memory-off path byte-for-byte intact).
- `go test ./cmd/villa/ -run 'TestInstallMemoryGateUsesPersistedConfig|TestInstallMemoryServices|TestEmbedGGUFFilenameSingleSource|TestNomicShardValues|TestInstallMemoryProof|TestEvalMemoryProof|TestInstallMemoryProofSkippedWhenOffOrDryRun'` — 21 passed.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — PASS (no image/device literal leaks; proof helper image sourced via accessor; nomic URL is huggingface.co, not docker.io/kyuz0).
- `make check` (vet + `go test ./...`) — full suite green, CGO-free static build intact.
- `make lint` — golangci-lint absent → `go vet` clean.
- Acceptance greps: size/SHA256/service-names present in install_memory.go; `loadedMemoryEnabled` + `cfg.MemoryEnabled = d.loadedMemoryEnabled()` in install.go; `orchestrate.EmbedGGUFFilename()` in install_test.go; `grep -c TODO install_memory.go` = 0; `/v1/embeddings` + `/readyz` + `villa-probe` present.

## TDD Gate Compliance

Both tasks are `tdd="true"`. Per the sequential-executor single-commit convention (19-01 precedent), the RED tests and the GREEN implementation landed in one `feat(19-02)` commit. The tests were authored against the seam contract and the full suite passes (RED→GREEN verified by the passing run); no standalone failing-only commit was recorded. A warning is noted here for transparency: the canonical RED-then-GREEN two-commit gate sequence is collapsed into one commit because the two tasks are mutually compile-dependent within the shared `runInstall`/`installDeps`.

## Deviations from Plan

**1. [Process] Tasks 1 and 2 committed together (single `feat(19-02)` commit `7b0bb58`)**
- **Found during:** staging for per-task commits.
- **Issue:** Task 1 (gate/pre-stage/start) and Task 2 (proof) both modify the SAME `runInstall` steps and the SAME `installDeps` seam struct, interleaved; the proof types are referenced by the runInstall invocation and the shared `fakeInstallDeps` harness. A split commit would leave an intermediate non-building tree (a violated invariant).
- **Resolution:** Committed both as one cohesive, buildable, fully-tested unit — mirroring 19-01's documented coupled-unit single-commit precedent. Every commit in this plan builds and passes `make check`.
- **Files:** cmd/villa/install_memory.go, cmd/villa/install.go, cmd/villa/install_test.go. **Commit:** `7b0bb58`.

**2. [Rule 3 - Blocking] Reverted an unrelated gofmt comment-reflow in `cmd/villa/bench_compare.go`**
- **Found during:** `go fmt ./cmd/villa/` (run to format my files) also reflowed a pre-existing comment in `bench_compare.go`, an out-of-scope file.
- **Resolution:** `git checkout -- cmd/villa/bench_compare.go` to keep the commit scoped to my task's files (per the dirty-working-tree instruction). Not committed.

## Threat Coverage

All Plan-19-02 `mitigate` dispositions are implemented:
- **T-19-06** (pre-stage tampering): `download.PullModel` size+SHA256+atomic-rename; `TestNomicShardValues` pins the integrity values.
- **T-19-07/T-19-08** (runtime download / #15406): install-time pre-stage + offline 768-dim proof; a wrong-length/non-200 → FAIL.
- **T-19-09** (Qdrant false-green): active PUT/DELETE 768-dim probe collection, not `/readyz`-only.
- **T-19-10** (proof reachability injection): fixed-arg `podman run`, constant JSON body, config-resolved model id, helper image via accessor.
- **T-19-11** (host binding): proof reaches services over `villa.network` (container-DNS), no host port.
- **T-19-16** (silent-failure gate source): gate bound to `LoadVilla().MemoryEnabled`, proven end-to-end by `TestInstallMemoryGateUsesPersistedConfig`.

## Self-Check: PASSED
