# Phase 19: Vector Store + Local Embeddings Services - Pattern Map

**Mapped:** 2026-06-09
**Files analyzed:** 11 (3 new templates, 1 new Go file, 5 modified Go files, 3 new + 5 frozen goldens)
**Analogs found:** 11 / 11 (all exact — this is an integrate-not-rebuild phase that mirrors `openwebui.go`)

> Every new file copies a shipped, golden-frozen precedent. The dominant analog is the Open WebUI managed-service path (`internal/orchestrate/openwebui.go` + its two templates), which is byte-for-byte the shape Qdrant + villa-embed must take. No first-party Go libraries are added; `go.mod` is unchanged.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/orchestrate/memory.go` (NEW) | config/managed-service consts + view builders | transform (cfg→view) | `internal/orchestrate/openwebui.go` | exact |
| `internal/orchestrate/quadlet/qdrant.container.tmpl` (NEW) | config template | transform | `quadlet/openwebui.container.tmpl` | exact (minus Env, minus PublishPort) |
| `internal/orchestrate/quadlet/qdrant.volume.tmpl` (NEW) | config template | transform | `quadlet/openwebui.volume.tmpl` | exact |
| `internal/orchestrate/quadlet/embed.container.tmpl` (NEW) | config template | transform | `quadlet/openwebui.container.tmpl` | exact (Exec instead of Env, no PublishPort) |
| `internal/orchestrate/render.go` (MOD) | renderer (pure) | transform | existing OWUI append block (lines 119-141) | exact |
| `internal/inference/seam_test.go` (MOD) | test (seam gate) | request-response | ROCm-tag allowlist extension (12-02) | exact |
| `cmd/villa/install.go` (MOD) | command (impure edge) | event-driven (lifecycle) | `ensureModel`/`pollReady` seams | exact |
| `internal/orchestrate/testdata/villa-qdrant.container.golden` (NEW) | test fixture | — | `villa-openwebui.container.golden` | exact |
| `internal/orchestrate/testdata/villa-qdrant.volume.golden` (NEW) | test fixture | — | `villa-openwebui.volume.golden` | exact |
| `internal/orchestrate/testdata/villa-embed.container.golden` (NEW) | test fixture | — | `villa-openwebui.container.golden` | exact |
| catalog Shard / `internal/download` use (MOD/NEW const) | model pre-stage | file-I/O (download) | `nomicEmbedShard` + `download.PullModel` (`ensureModel`) | exact |

## Shared Patterns

### Managed-service image literal behind an accessor (D-02/D-04/D-10)
**Source:** `internal/orchestrate/openwebui.go:12-28`
**Apply to:** `internal/orchestrate/memory.go` — both `qdrantImage`/`QdrantImage()` and `embedImage`/`EmbedImage()`.

```go
// openWebUIImage is digest-pinned ... Resolved on the dev box <date> via
// `podman pull ... && podman image inspect ... --format '{{index .RepoDigests 0}}'`.
const openWebUIImage = "ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a...ea9184e"

func OpenWebUIImage() string { return openWebUIImage }
```
Copy verbatim: a doc-comment naming the dev-box resolution date + provenance, a digest-pinned `const`, an exported zero-arg accessor. `embedImage` MUST equal the `vulkanImage` literal (`internal/inference/backend_vulkan.go:19`) but is a deliberate INDEPENDENT const (D-04) — do not reference `inference.VulkanBackend().Image()`.

### Stable Quadlet identity const block
**Source:** `internal/orchestrate/openwebui.go:40-56` (and `render.go:24-33`)
```go
const (
    openWebUIContainerUnitName = "villa-openwebui.container"
    openWebUIVolumeUnitName    = "villa-openwebui.volume"
    openWebUIContainerName     = "villa-openwebui"
    openWebUIVolumeName        = "villa-openwebui"
    openWebUIVolumeMount = openWebUIVolumeName + ".volume:/app/backend/data:Z"
)
```
**Apply to:** Qdrant unit/volume/container names + `qdrantVolumeMount = "villa-qdrant.volume:/qdrant/storage:Z"` (add `,U` only if the dev-box proof shows a write failure — Pitfall 1). Embed: `embedModelMount = "villa-models:/models:ro,z"` (note lowercase `:z` for the shared models dir, matching `backend_vulkan.go:83` `modelBind`, NOT `:Z`). NO `*PublishPort` const for either (D-03/D-05/D-10, SC#4).

### `:Z` vs `:z` mount-label discipline
- Dedicated durable named volume (Qdrant storage) → **`:Z`** (private label), like OWUI's `/app/backend/data:Z`.
- Shared models dir mounted into multiple containers (villa-embed reads the GGUF) → **`:z`** (shared label) + `ro`, matching `backend_vulkan.go:83` (`%s:%s:ro,z`).

### `villa.network` join
**Source:** `internal/orchestrate/openwebui.go:104` — `Network: networkAttach` (the package const `networkAttach = "villa.network"`, `render.go:31`). Reuse `networkAttach` for both new views; never re-type `"villa.network"`.

## Pattern Assignments

### `internal/orchestrate/memory.go` (NEW — consts + view builders)

**Analog:** `internal/orchestrate/openwebui.go` (whole file).

**View struct + builder** (analog `openWebUIView` / `buildOpenWebUIView`, lines 73-135):
- `qdrantView{ContainerName, Image, Network, Volume}` — NO Env, NO PublishPort, NO Exec.
- `qdrantVolumeView{VolumeName}` — copy `openWebUIVolumeView` (lines 86-88) exactly.
- `embedView{ContainerName, Image, Network, Volume, Exec}` — like `qdrantView` plus an `Exec` string.

**Embed Exec builder** (analog `backend_vulkan.go:81-103` ContainerArgs Exec tail):
```go
// Fixed tokens — NO shell interpolation (T-03-01). Mirror backend_vulkan.go lines 95-100.
func buildEmbedExec(ggufFilename string) string {
    return strings.Join([]string{
        "llama-server",
        "-m", "/models/" + ggufFilename,
        "--embeddings", "--pooling", "mean",
        "-c", strconv.Itoa(embedContextLen), // 8192 (D-05/D-08)
        "--host", "0.0.0.0",                 // container-internal only (backend_vulkan.go:98)
        "--port", strconv.Itoa(embedContainerPort), // 8080
    }, " ")
}
```
`--embeddings` (trailing s) + `--pooling mean` are load-bearing (Pitfall 2). The GGUF filename is a SINGLE source of truth shared with the pre-stage Shard (Pitfall 3). Record `EmbeddingDim = 768` on the rendered embed view for the Phase-23 swap guard (D-08).

---

### `internal/orchestrate/quadlet/qdrant.container.tmpl` (NEW)

**Analog:** `quadlet/openwebui.container.tmpl` (read in full — lines 1-19).

Copy the OWUI container template, then: DROP the `PublishPort=` line (13→omit), DROP the `{{range .Env}}...{{end}}` block (lines 13-14). Keep `# GENERATED — do not edit` header, `[Unit] Description=`, `After=villa-network.service`, `[Container] ContainerName/Image/Network/Volume`, `[Service] Restart=on-failure`, `[Install] WantedBy=default.target`. `{{.Volume}}` = `villa-qdrant.volume:/qdrant/storage:Z`.

---

### `internal/orchestrate/quadlet/embed.container.tmpl` (NEW)

**Analog:** `quadlet/openwebui.container.tmpl`. Same as qdrant.container.tmpl PLUS an `Exec={{.Exec}}` line under `[Container]` (the OWUI template has no Exec; the `containerView` path renders Exec via `container.tmpl` — but embed bypasses `parseContainerArgs` like OWUI does). NO `PublishPort=`. `{{.Volume}}` = `villa-models:/models:ro,z`.

---

### `internal/orchestrate/quadlet/qdrant.volume.tmpl` (NEW)

**Analog:** `quadlet/openwebui.volume.tmpl` (lines 1-7) — copy VERBATIM, only the comment path differs. A plain named volume:
```
[Volume]
VolumeName={{.VolumeName}}
Driver=local
[Install]
WantedBy=default.target
```
Do NOT use `volume.tmpl`'s `Type=none`/`Device=`/`Options=bind` form (that is only for the host-path-bound `villa-models` volume, lines 5-7 of `volume.tmpl`).

---

### `internal/orchestrate/render.go` (MOD — conditional append)

**Analog:** the OWUI render block (lines 119-141) + the OWUI parse-bypass comment.

Append AFTER the existing 5 units, gated on `in.Cfg.MemoryEnabled`. `in.Cfg` is already threaded into `Render()` (the existing `RenderInput.Cfg` field; `install.go:320` sets `Cfg: cfg`). Compute the resolved view inside `Render()` from `in.Cfg`:
```go
units := []Unit{ /* existing 5: container, network, volume, owui-container, owui-volume */ }
if in.Cfg.MemoryEnabled {
    mv := memory.RenderView(in.Cfg) // resolved values only — model id, dim, addr/port (memory.go:95)
    // render qdrant.container.tmpl, qdrant.volume.tmpl, embed.container.tmpl via execTemplate
    units = append(units,
        Unit{Name: qdrantContainerUnitName, Text: qdrantText},
        Unit{Name: qdrantVolumeUnitName, Text: qdrantVolumeText},
        Unit{Name: embedContainerUnitName, Text: embedText},
    )
}
return units, nil
```
Render each via `execTemplate(tmpl, "qdrant.container.tmpl", buildQdrantView(...))` (analog `render.go:124-131`). When `MemoryEnabled=false` the returned slice is BYTE-IDENTICAL to the current 5-unit output (D-11) — the 5 existing goldens stay UNCHANGED and prove this. `memory.RenderView`/`MemoryRenderInput` (`internal/memory/memory.go:83-104`) carries `EmbeddingModel, EmbeddingDim, QdrantAddr/Port, EmbedAddr/Port` — NO image literal; orchestrate owns the image consts.

---

### `internal/inference/seam_test.go` (MOD — MANDATORY, Pitfall 7)

**Analog:** the 12-02 ROCm-tag extension already in this file (lines 41-54).

The `container image literal` regex (line 54) is `kyuz0|docker\.io/|...`. Adding `qdrantImage = "docker.io/qdrant/..."` and `embedImage = "docker.io/kyuz0/..."` to `internal/orchestrate/memory.go` WILL trip the `internal/` walk because the `isSeam` allowlist (lines 92-95) is ONLY `inference/` + `detect/gpu_amd.go`:
```go
isSeam := func(rel string) bool {
    rel = filepath.ToSlash(rel)
    return strings.HasPrefix(rel, "inference/") || rel == "detect/gpu_amd.go"
}
```
**Required fix (same commit as the consts):** add `|| rel == "orchestrate/memory.go"` with a comment mirroring the D-10 rationale ("managed-service image, not a GPU-backend token") — exactly how the ROCm tags were added in one commit. The cmd-tier walk (`cmdPatterns`, lines 129-137) is unaffected (no Qdrant/embed literal lands in `cmd/villa`). `TestROCmMarkerPresence` (lines 164-175) is untouched. Note: existing `openWebUIImage` does NOT trip the gate because it is `ghcr.io/...` (no `docker.io/`/`kyuz0`); the two new `docker.io/` literals DO.

---

### `cmd/villa/install.go` (MOD — pre-stage + start + memory proof)

**Analogs:** `ensureModel`/`modelDownloaded` seams (struct fields lines 97-104; live wiring lines 867-890; call site lines 348-360) and `pollReady`/`installReadiness` (lines 71-74, 151, 424).

**Pre-stage GGUF (D-07/PRIV-04):** mirror the `ensureModel` seam exactly — gated on `memory_enabled` AND file absence (idempotent), skipped under `--dry-run`, BEFORE starting `villa-embed` (Pitfall 4). Reuse the same `pullFn`→`download.PullModel(ctx, m, dir)` path the chat model uses (lines 878-890) with the nomic `catalog.CatalogModel{Shards: []Shard{nomicEmbedShard}}`:
```go
var nomicEmbedShard = catalog.Shard{
    URL:       "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf",
    Filename:  "nomic-embed-text-v1.5.Q8_0.gguf",
    SHA256:    "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7", // X-Linked-Etag git-LFS oid
    SizeBytes: 146146432,                                                          // X-Linked-Size
}
```
`catalog.Shard` (`internal/catalog/catalog.go:85-90`) is `{URL, Filename, SHA256, SizeBytes uint64}`. `download.PullModel(ctx, m catalog.CatalogModel, modelsDir string)` (`download.go:64`) does HEAD-verify + SHA256 + size + atomic rename. The Shard `Filename` must match `buildEmbedExec`'s `/models/<file>` (single source of truth, Pitfall 3).

**Start the two services:** mirror the OWUI start ordering (install.go ~line 414 "Start Open WebUI AFTER inference") via the existing `start`/`isActive` seams (`sys.Start`, lines 898-899). Gate on `memory_enabled`.

**Memory-stack readiness proof (D-09):** add a NEW injectable seam shaped like `pollReady` (`installReadiness` struct, lines 71-74). It asserts (a) `/v1/embeddings` returns a 768-length float vector offline, and (b) Qdrant `/readyz` + a write probe (create/delete a 768-dim probe collection). Failure → refuse-with-remediation (reuse the typed-error / non-PASS discipline, never a silent skip — honesty-by-construction). Reachability mechanism (one-shot `podman run --network villa.network curl`, etc.) is planner's call; the assertion (768-length, offline, writable) is fixed. Under `--dry-run`: no pull, no start, no proof (existing dry-run contract, lines 334-346).

---

### Golden fixtures (NEW × 3)

**Analog:** `internal/orchestrate/testdata/villa-openwebui.container.golden` (read in full) + `villa-openwebui.volume.golden`. Generate the 3 new goldens with `go test ./internal/orchestrate/ -update` for the NEW files ONLY. The existing 5 goldens (`villa-llama.container`, `villa.network`, `villa-models.volume`, `villa-openwebui.container`, `villa-openwebui.volume`) MUST remain unchanged — they prove byte-identical-when-off. `villa-qdrant.container.golden` will resemble `villa-openwebui.container.golden` MINUS the `PublishPort=` + `Environment=` lines.

## No Analog Found

None — every file has an exact in-repo precedent. (The only genuinely novel logic is the install-time 768-dim/Qdrant-writable proof, but it copies the `pollReady`/`installReadiness` injectable-seam shape.)

## Metadata

**Analog search scope:** `internal/orchestrate/` (openwebui.go, render.go, quadlet/*.tmpl, testdata/*.golden), `internal/inference/` (seam_test.go, backend_vulkan.go), `internal/memory/memory.go`, `internal/catalog/catalog.go`, `internal/download/download.go`, `cmd/villa/install.go`.
**Files scanned:** 14
**Pattern extraction date:** 2026-06-09
