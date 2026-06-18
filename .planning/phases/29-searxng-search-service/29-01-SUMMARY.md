---
phase: 29-searxng-search-service
plan: 01
subsystem: orchestrate
tags: [searxng, web-search, managed-service, quadlet, config, secret, seam]
requires:
  - "internal/config.VillaConfig (existing v1.4 schema)"
  - "internal/orchestrate.Render + memory.go managed-service render path (analog)"
  - "internal/inference TestSeamGrepGate (seam allowlist)"
provides:
  - "config.WebSearchEnabled gate + SearxngAddr/SearxngPort/SearxngSecret fields"
  - "config.GenerateSearxngSecret (crypto/rand secret generator)"
  - "orchestrate.SearXNGImage / SearXNGContainerUnitName / SearXNGSecretEnvFilePath / SearxngEngines accessors"
  - "orchestrate.RenderSearxngSettings + RenderSearxngSecretEnv pure helpers (consumed by Plan 02)"
  - "WebSearchEnabled-gated villa-searxng.container render branch"
  - "byte-frozen goldens: villa-searxng.container.golden, searxng-settings.yml.golden"
affects:
  - "Plan 02 (settings.yml + searxng.env 0600 writers consume the render helpers + the EnvironmentFile path contract)"
  - "Plan 03 (install readiness proof asserts SearXNGContainerUnitName present in the plan)"
tech-stack:
  added:
    - "crypto/rand + encoding/hex (first runtime secret generator)"
    - "ghcr.io/searxng/searxng (digest-pinned managed-service image)"
  patterns:
    - "managed-service render path cloned from memory.go (qdrant/embed)"
    - "NEW sub-pattern: rendering a mounted settings.yml config FILE from config (RenderSearxngSettings, not a Unit)"
    - "secret via EnvironmentFile=<0600 path>, never inline in a 0644 unit (T-29-02)"
    - "seam allowlist extended in the SAME commit as the image literal (Pitfall 5)"
key-files:
  created:
    - "internal/orchestrate/searxng.go"
    - "internal/orchestrate/quadlet/searxng.container.tmpl"
    - "internal/orchestrate/quadlet/searxng-settings.yml.tmpl"
    - "internal/orchestrate/searxng_test.go"
    - "internal/orchestrate/testdata/villa-searxng.container.golden"
    - "internal/orchestrate/testdata/searxng-settings.yml.golden"
  modified:
    - "internal/config/villaconfig.go"
    - "internal/config/villaconfig_test.go"
    - "internal/orchestrate/render.go"
    - "internal/inference/seam_test.go"
decisions:
  - "Pin the manifest-list RepoDigests[0] digest (sha256:ed29454e…), mirroring qdrantImage — podman resolves the amd64 variant automatically. Digest on-hardware resolved on the live gfx1151 box."
  - "No villa-searxng.volume rendered (SC#1 .volume clause DECIDED N/A): a private JSON-only instance with limiter:false + image_proxy:false is stateless."
  - "Secret reaches the container ONLY via EnvironmentFile=<0600 path> (searxng.env); the render path never reads the secret; settings.yml renders secret_key empty (T-29-02 / A4)."
  - "Vetted engine subset (SRCH-04): duckduckgo, brave, wikipedia, wikidata — single-source slice, the auditable outbound surface (A1)."
metrics:
  tasks_completed: 3
  files_created: 6
  files_modified: 4
  completed: 2026-06-18
status: complete
---

# Phase 29 Plan 01: SearXNG Render Spine Summary

Config gate + crypto/rand secret generator, a memory.go-cloned `orchestrate/searxng.go` managed-service core, the net-new `settings.yml` config-file render with a bounded `keep_only` engine allowlist, and an append-only `WebSearchEnabled`-gated render branch — all byte-frozen by goldens, with the web-search-off render byte-identical to v1.4.

## What Was Built

**Task 1 — config layer (commit 140c8d0):** Added the `WebSearchEnabled` bool gate plus `SearxngAddr` (default `villa-searxng`, container-DNS only), `SearxngPort` (default 8080, `,omitzero`), and `SearxngSecret` (`,omitempty`) fields to `VillaConfig`, mirroring the v1.3 memory block. `defaultConfig()` seeds them; `normalizeVilla` self-heals addr/port from the single default source (never widening to a routable bind, PRIV-01) while deliberately NOT self-healing the bool gate or the generated secret. `marshalVilla` zeroes the four searxng keys when web search is off so a web-search-off config is byte-identical on disk to v1.4 (SC#4/PRIV-07). Added `GenerateSearxngSecret()` — the repo's FIRST runtime secret generator — reading 32 bytes from `crypto/rand` and hex-encoding them (never `math/rand`, never logged).

**Task 2 — orchestrate core + templates + seam (commit ef61a65):** New `internal/orchestrate/searxng.go` with the digest-pinned `searxngImage` const + `SearXNGImage()`; the stable `searxngContainerUnitName` + exported `SearXNGContainerUnitName()`; the cross-plan secret contract (`searxngSecretEnvFileName` + `SearXNGSecretEnvFilePath()` — the `%h/.config/villa/searxng/searxng.env` path Plan 02 writes at 0600 and the unit references); `searxngView` + `buildSearxngView` (no secret threaded into the view); `settingsYmlView` + `buildSettingsYml`; and the single-source vetted engine slice (`searxngEngines`) + `SearxngEngines()`. Two templates: `searxng.container.tmpl` (EnvironmentFile= path ref, `/etc/searxng:ro,Z` mount, NO host port) and `searxng-settings.yml.tmpl` (keep_only allowlist, formats:[html,json], limiter:false, image_proxy:false, empty secret_key). The `seam_test.go` `isSeam` allowlist was extended for `orchestrate/searxng.go` IN THE SAME COMMIT as the image literal (Pitfall 5, mirroring the memory.go precedent).

**Task 3 — render branch + helpers + goldens (commit 3562b1e):** Appended an `if in.Cfg.WebSearchEnabled` branch at the END of `Render()`, strictly after the memory branch, emitting only `villa-searxng.container` with no shared-view mutation. Added two pure helpers consumed by Plan 02: `RenderSearxngSettings(cfg)` (returns `settings.yml` name+text — explicitly NOT a `Unit`, Pitfall 1) and `RenderSearxngSecretEnv(secret)` (the single-source `SEARXNG_SECRET=<value>` env-file format). Wrote `searxng_test.go` covering the render golden, the no-secret-leak assertion (T-29-02), no-publish-port (T-29-04), the settings golden, the SRCH-04 engine-allowlist explicit-contains test, the secret-env-path contract, the WR-01 config-driven test, and the SC#4 negative byte-identical-off test. Froze the two new goldens with `-update`.

## On-Hardware Digest Resolution

This ran on the live gfx1151 dev box. The SearXNG image digest was resolved per the plan's pin-time procedure:
```
podman pull ghcr.io/searxng/searxng:latest
podman image inspect … --format '{{index .RepoDigests 0}}'
→ ghcr.io/searxng/searxng@sha256:ed29454ec1f7149986d42819b8b75265e545e79dd9187ba241c09f16a0fe56d0
```
Confirmed a multi-arch manifest list; the amd64 variant (sha256:f63ce776…) is resolved automatically by podman from the manifest-list digest. Pinning the manifest-list `RepoDigests[0]` digest matches the established `qdrantImage` convention in `memory.go`.

## Deviations from Plan

None — plan executed exactly as written. The `RenderSearxngSettings`/`RenderSearxngSecretEnv` helpers were placed in `render.go` (Task 3) as the plan specified, consuming the `searxngSecretEnvName`/`searxngSecretEnvBody`/`buildSettingsYml` primitives defined in `searxng.go` (Task 2). All searxng.go symbols are consumed by render.go or the tests, so no unused-symbol lint exposure.

## Verification

- `go test ./internal/config/... ./internal/orchestrate/...` — green.
- `go test ./internal/inference/ -run TestSeamGrepGate` — green (image literal seam-locked, allowlisted same-commit).
- `go test ./...` — full suite green.
- `go vet ./...` — clean (`make lint` falls back to vet; golangci-lint not installed).
- `git diff --stat internal/orchestrate/testdata/` — ONLY the two new searxng goldens added; zero modifications to the 13 existing goldens (SC#4 / PRIV-07).
- Unit golden carries NO secret value (only the `EnvironmentFile=` path); settings golden renders `secret_key: ""` (T-29-02).

## Success Criteria Met

- SRCH-01 (render): `villa-searxng.container` renders on villa.network, no host port, digest-pinned behind the seam — golden-frozen.
- SRCH-01 (settings): settings.yml renders formats:[html,json], limiter:false, secret via env (empty in file) — golden-frozen.
- SRCH-04: settings.yml restricts engines via `keep_only` to the vetted 4-engine subset — explicit-contains test + golden.
- SC#4 / PRIV-07: web-search-off render byte-identical to v1.4 (13 goldens unchanged) and config byte-identical on disk.

## Notes for Downstream Plans

- **Plan 02** writes `settings.yml` (from `RenderSearxngSettings`) and `searxng.env` (from `RenderSearxngSecretEnv`) at mode 0600 into `$XDG_CONFIG_HOME/villa/searxng/`, the dir mounted read-only at `/etc/searxng`. The secret env file path MUST equal `SearXNGSecretEnvFilePath()` (the `%h`-form the unit references) on the host side (`$XDG_CONFIG_HOME/villa/searxng/searxng.env`).
- **Plan 03** generates+persists the secret via `config.GenerateSearxngSecret` then writes the env file before service start, and gates the start on `SearXNGContainerUnitName()` being present in the rendered plan.

## Self-Check: PASSED

- FOUND: internal/orchestrate/searxng.go
- FOUND: internal/orchestrate/quadlet/searxng.container.tmpl
- FOUND: internal/orchestrate/quadlet/searxng-settings.yml.tmpl
- FOUND: internal/orchestrate/searxng_test.go
- FOUND: internal/orchestrate/testdata/villa-searxng.container.golden
- FOUND: internal/orchestrate/testdata/searxng-settings.yml.golden
- FOUND commit 140c8d0 (Task 1), ef61a65 (Task 2), 3562b1e (Task 3)
