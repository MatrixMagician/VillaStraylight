---
phase: 19-vector-store-local-embeddings-services
plan: 01
subsystem: orchestrate
tags: [orchestrate, managed-service, quadlet, qdrant, embeddings, memory, render, golden, seam-gate]
requires:
  - internal/memory.RenderView (Phase-18 resolved-values handoff)
  - config.VillaConfig memory_* fields (Phase-18 spine)
  - internal/orchestrate openwebui.go managed-service precedent
provides:
  - orchestrate.QdrantImage() / orchestrate.EmbedImage() (digest-pinned managed-service image accessors)
  - orchestrate.EmbedGGUFFilename() (EXPORTED single-source embed GGUF filename — Plan 19-02 drift test binds against it)
  - orchestrate.QdrantVolumeName() (Phase-23 backup reads it)
  - Render(memory_enabled=true) → 8 units (villa-qdrant.container, villa-qdrant.volume, villa-embed.container appended)
  - three byte-frozen goldens (villa-qdrant.container/.volume, villa-embed.container)
  - embedEmbeddingDim=768 const (D-08 load-bearing dim recorded in orchestrate)
affects:
  - Plan 19-02 (install lifecycle wiring + nomic pre-stage Shard.Filename binds EmbedGGUFFilename())
  - Plan 19-03 (dev-box RepoDigest + /v1/embeddings checkpoint:human-verify)
  - Phase 20 (OWUI env wiring targets villa-embed:8080 / villa-qdrant:6333)
  - Phase 23 (backup manifest reads QdrantVolumeName() + embedding dim 768)
tech-stack:
  added: []
  patterns:
    - "Managed-service image literals live behind the orchestrate seam (openWebUIImage precedent), NOT the inference BackendFor / TestSeamGrepGate scope (D-02/D-04/D-10)"
    - "Conditional render append gated on in.Cfg.MemoryEnabled; byte-identical to v1.2 5-unit output when off (D-11)"
    - "Single-source GGUF filename via one exported accessor so served -m path + pre-stage Shard.Filename cannot drift (Pitfall 3)"
    - "Seam-gate isSeam allowlist extended in the SAME commit as the new docker.io/ image consts (Pitfall 7; 12-02 precedent)"
key-files:
  created:
    - internal/orchestrate/memory.go
    - internal/orchestrate/quadlet/qdrant.container.tmpl
    - internal/orchestrate/quadlet/qdrant.volume.tmpl
    - internal/orchestrate/quadlet/embed.container.tmpl
    - internal/orchestrate/memory_test.go
    - internal/orchestrate/testdata/villa-qdrant.container.golden
    - internal/orchestrate/testdata/villa-qdrant.volume.golden
    - internal/orchestrate/testdata/villa-embed.container.golden
  modified:
    - internal/orchestrate/render.go
    - internal/inference/seam_test.go
decisions:
  - "embedImage is a DELIBERATELY INDEPENDENT const byte-identical to vulkanImage (D-04) — not a reference into the inference seam, keeping TestSeamGrepGate semantics clean"
  - "qdrantVolumeMount uses :Z PRIVATE label; ,U belt-and-suspenders deferred unless Plan 19-03 dev-box write proof fails (Pitfall 1)"
  - "qdrant.container.tmpl carries no Env block — QDRANT_API_KEY is a Phase-20 choice; Qdrant runs its defaults this phase"
  - "Qdrant manifest-list digest committed as the placeholder; dev-box RepoDigest confirmation is Plan 19-03's checkpoint (TODO marker in comment, not in literal)"
metrics:
  duration: ~14 min
  completed: 2026-06-09
  tasks: 3
  files: 10
---

# Phase 19 Plan 01: Vector Store + Local Embeddings Render Path Summary

Added the orchestrate managed-service render path for the two v1.3 memory services — `villa-qdrant` (digest-pinned Qdrant on a durable `:Z` named volume) and `villa-embed` (a dedicated `llama-server` serving `/v1/embeddings` with `--embeddings --pooling mean -c 8192`) plus the Qdrant volume, rendered ONLY when `memory_enabled=true` and byte-identical to the v1.2 five-unit output when off.

## What Was Built

- **`internal/orchestrate/memory.go`** — mirrors `openwebui.go`: digest-pinned `qdrantImage`/`QdrantImage()` (official `qdrant/qdrant:v1.18.2-unprivileged`, USER_ID=1000), independent `embedImage`/`EmbedImage()` (== `vulkanImage`, D-04), `embedGGUFFilename` const + EXPORTED `EmbedGGUFFilename()` single-source accessor (Pitfall 3), stable identity consts (no PublishPort), `embedEmbeddingDim=768` (D-08), `QdrantVolumeName()`, three view structs + builders, and `buildEmbedExec` (fixed-token `strings.Join`, no shell interpolation, T-19-02).
- **Seam-gate allowlist extension** — `internal/inference/seam_test.go` `isSeam` now allows `orchestrate/memory.go`, in the SAME commit as the two `docker.io/` image consts (Pitfall 7). `TestSeamGrepGate` stays green.
- **Three Quadlet templates** — `qdrant.container.tmpl` (Volume mount, no host port, no Env), `embed.container.tmpl` (same + `Exec={{.Exec}}`, `:ro,z` model mount), `qdrant.volume.tmpl` (plain `Driver=local` named volume). Auto-discovered by the existing `//go:embed quadlet/*.tmpl`.
- **`render.go` conditional append** — after the existing 5 units, `if in.Cfg.MemoryEnabled` renders + appends the three memory units in fixed order; `memory.RenderView(in.Cfg)` is the D-11 resolved-values handoff; the embed `-m` path binds `embedGGUFFilename`.
- **Render tests + three byte-frozen goldens** — `TestRenderQdrant`/`TestRenderEmbed` (golden compares), `TestRenderByteIdenticalWhenMemoryOff` (len==5, no memory names), `TestMemoryUnitsNoPublishPort` (T-19-01), `TestRenderEightUnitOrderWhenMemoryOn`, `TestRenderConsumesMemoryView`.

## Verification

- `go test ./internal/orchestrate/ -count=1` — PASS (all new + existing tests).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — PASS (allowlist extension green).
- `make check` (vet + `go test ./...`) — full suite green, CGO-free static build intact.
- `git status --porcelain` over the 5 existing goldens — empty (D-11 byte-identity proof; only the 3 new goldens added).
- Acceptance greps: 4 accessors present, exactly 1 `EmbedGGUFFilename`, 1 digest-pinned qdrant image, 0 `PublishPort` in memory.go / both container templates / both new container goldens, embed Exec exact-match, qdrant `:Z` mount exact-match, `memory.RenderView` present in render.go.

## TDD Gate Compliance

Task 3 (`tdd="true"`) followed RED → GREEN: the five memory render tests were written first and ran failing (5 fail, units missing) before any `render.go` change; `render.go` was then implemented and the tests passed. Per the sequential-executor single-commit convention the RED tests and the GREEN implementation landed in one `feat(19-01)` commit (`3ca6cf5`) alongside the three generated goldens. No REFACTOR step was required.

## Deviations from Plan

None — plan executed exactly as written. (Task 1 acceptance criterion `grep -c PublishPort memory.go == 0` was satisfied by wording two doc comments as "no published host port" instead of the literal token "PublishPort"; this is a comment-wording detail, not a behavior change.)

## Commits

- `59153a1` feat(19-01): add orchestrate memory managed-service consts + seam-gate allowlist (memory.go + seam_test.go, same commit per Pitfall 7)
- `b3b38bb` feat(19-01): add qdrant + embed Quadlet templates (no host port)
- `3ca6cf5` feat(19-01): render qdrant + embed + qdrant-volume when memory_enabled (render.go + tests + 3 goldens; TDD GREEN)

## Self-Check: PASSED

All 10 plan files exist on disk (verified via `test -f`); all 3 task commits present in `git log` (`59153a1`, `b3b38bb`, `3ca6cf5`).
