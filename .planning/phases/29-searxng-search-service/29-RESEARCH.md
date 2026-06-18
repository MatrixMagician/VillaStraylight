# Phase 29: SearXNG Search Service - Research

**Researched:** 2026-06-18
**Domain:** Self-hosted metasearch (SearXNG) as a rootless Podman Quadlet managed service in a Go control plane
**Confidence:** HIGH (codebase analog), HIGH (SearXNG settings.yml schema), MEDIUM (current image digest — rolling daily tag, must re-resolve on-hardware)

## Summary

Phase 29 adds **exactly one new managed-service container** — `villa-searxng` — to the established v1.3 managed-service render path. The codebase already ships the *exact* shape this phase needs: `internal/orchestrate/memory.go` renders `villa-qdrant`/`villa-embed` as digest-pinned, container-DNS-only, no-host-port Quadlet units behind a seam-locked image const, gated additively on a config bool (`MemoryEnabled`) so the off-render is byte-identical. SearXNG follows this pattern almost verbatim — a new `WebSearchEnabled` gate, a new `searxngImage` const + `SearXNGImage()` accessor in a new `internal/orchestrate/searxng.go`, new `searxng.container.tmpl`/`searxng.volume.tmpl`, and an append-only branch in `Render()`.

**The one genuinely new pattern this phase must invent:** SearXNG requires a `settings.yml` **config file** mounted into the container. Every prior managed service (Qdrant, embed, OWUI) is configured by either the image entrypoint defaults or a Quadlet `Exec=`/`Environment=` block — **no precedent exists for rendering a config FILE from config and mounting it.** The planner must design where this file is written (alongside the units, or in a dedicated dir mounted at `/etc/searxng/`), how it stays config-derived (single source of truth), and how it is byte-frozen by a golden. This is the central design decision of the phase.

**The security-sensitive decision (SRCH-04):** the bounded engine subset. SearXNG's `use_default_settings: {engines: {keep_only: [...]}}` cleanly restricts to an auditable list while inheriting all other defaults — this is the correct, low-surface mechanism. The planner must pick the concrete subset (see Architecture Patterns → Engine Allowlist).

**Primary recommendation:** Clone `memory.go`'s managed-service render path for the container/volume units; add a `settings.yml` render-and-mount sub-pattern; pass `secret_key` via the `$SEARXNG_SECRET` env var (NOT into the world-readable settings file); set `limiter: false` (the limiter needs a valkey/redis DB villa does not run); prove readiness with a real `format=json` query through the existing `podman run --rm --network villa --entrypoint curl` probe seam.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SearXNG container/volume Quadlet render | `internal/orchestrate` (managed-service path) | — | Same category as Qdrant/embed/OWUI; the only intentionally-impure module owns unit writing |
| `settings.yml` rendering | `internal/orchestrate` (pure render) | config | New file-render sub-pattern; data sourced from config, single source of truth |
| `settings.yml` + secret_key + gate config | `internal/config` (`VillaConfig`) | — | Config is the single source of truth; Quadlet + settings.yml regenerated from it |
| Image digest pinning | `internal/orchestrate` (seam-locked const) | — | Managed-service image literal, same seam category as `qdrantImage`/`openWebUIImage` (NOT the inference `TestSeamGrepGate` backend seam) |
| `format=json` readiness proof | `cmd/villa` (install/up layer) | `internal/preflight` (verdict shape) | Proof issues a real query via the existing `runProbeCurl` podman seam; verdict mirrors `memoryProof` |
| Service lifecycle (start ordering) | `cmd/villa/install*` | `internal/orchestrate/systemd.go` | Fixed-arg `systemctl --user` seam; started additively when gated on |

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SRCH-01 | SearXNG as a rootless Podman Quadlet unit on `villa.network` (container-DNS only, no host port, digest-pinned), `settings.yml` from config (`search.formats:[html,json]`, generated `secret_key`, `limiter:false`); readiness proven by a real `format=json` query, never a health-200 | Render path = exact clone of `memory.go` Qdrant pattern (Architecture Patterns §1–2); settings.yml schema verified (Code Examples §1); `$SEARXNG_SECRET` env route for the generated key (Don't Hand-Roll + Pitfall 3); readiness probe = `runProbeCurl` + `format=json` parse (Architecture Patterns §4, Validation Architecture) |
| SRCH-04 | SearXNG rendered with a vetted, bounded, auditable subset of upstream engines rather than the full default set | `use_default_settings:{engines:{keep_only:[...]}}` mechanism verified (Code Examples §2); concrete proposed subset + rationale (Architecture Patterns §3) |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| SearXNG container image | `ghcr.io/searxng/searxng` (rolling daily tag, e.g. `2026.6.18-b5ef7ec8f`) | The metasearch service | The official, self-hosted, privacy-respecting metasearch engine; the only OSS option consistent with strictly-local posture (cloud search APIs are explicitly Out of Scope) `[VERIFIED: ghcr.io official package page]` |
| Go `crypto/rand` (stdlib) | Go 1.26 stdlib | Generate the `secret_key` | No new dependency; the secret must be cryptographically random. **Note: no existing crypto/rand usage in the repo — this is net-new** `[VERIFIED: codebase grep — no current secret generation]` |
| `text/template` (stdlib) | Go 1.26 stdlib | Render `settings.yml` (and units) | Already the render engine for all Quadlet templates `[VERIFIED: render.go]` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Existing `runProbeCurl` seam | in `cmd/villa/install_memory.go` | Issue the `format=json` readiness query over `villa.network` with no host port | Reuse verbatim (`podman run --rm --network villa --entrypoint curl <helperImage>`); the helper image is sourced from `orchestrate.EmbedImage()` (ships curl) `[VERIFIED: install_memory.go:351]` |
| `BurntSushi/toml` v1.6.0 | (already a dep) | Persist the new config gate + searxng fields | Same pattern as the memory/coder field blocks `[VERIFIED: go.mod / villaconfig.go]` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `ghcr.io/searxng/searxng` | `docker.io/searxng/searxng` (DockerHub mirror) | DockerHub now rate-limits unauthenticated pulls; ghcr is the recommended source. Pin a digest either way. `[CITED: docs.searxng.org/admin/installation-docker]` |
| `limiter: false` | `limiter: true` + a valkey/redis sidecar | The limiter needs a valkey DB to function — enabling it adds a SECOND container, breaking "exactly ONE new container." For a private, container-DNS-only instance the limiter is unnecessary (it blocks bots on public instances). `limiter: false` is correct. `[VERIFIED: docs.searxng.org/admin/settings/settings_server — limiter requires valkey]` |
| `secret_key` via `$SEARXNG_SECRET` env | `secret_key` rendered into settings.yml | Unit/settings files are written world-readable (0644, per `reconcile.go`). The repo's secret discipline keeps secrets at 0600 (config.toml). Passing the secret as an `Environment=$SEARXNG_SECRET` keeps it out of the 0644 file. **Planner decision — see Pitfall 3.** `[VERIFIED: settings_server docs confirm $SEARXNG_SECRET override; reconcile.go:20 confirms 0644]` |

**Installation:** No host package install. The image is pulled by Podman at service start (a digest-pinned managed-service image, like Qdrant/OWUI). The single sanctioned outbound window is the install-time image pull (mirrors the v1.3 nomic GGUF pre-stage precedent).

**Version verification:** The SearXNG image uses **rolling daily tags**, not semver releases — there is no stable "latest version" to pin by tag. The planner MUST resolve the **amd64 manifest digest** on-hardware at pin time, exactly as Plan 19-03 did for Qdrant:
```bash
podman pull ghcr.io/searxng/searxng:<current-tag>
podman image inspect ghcr.io/searxng/searxng:<current-tag> --format '{{index .RepoDigests 0}}'
```
The tag observed during research (`2026.6.18-b5ef7ec8f`, digest `sha256:ed29454ec1f7149986d42819b8b75265e545e79dd9187ba241c09f16a0fe56d0`) is a **manifest-list** digest published ~hours before this research and WILL be superseded daily; treat it as a starting reference, not the final pin. `[VERIFIED: ghcr package page 2026-06-18]` `[ASSUMED: exact digest stable]`

## Package Legitimacy Audit

> The `gsd-tools query package-legitimacy check` seam does not support the `docker` ecosystem (returned N/A). Legitimacy verified manually against the official source.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| `ghcr.io/searxng/searxng` | GitHub Container Registry | project since 2021 | 5k+/day on recent tags | github.com/searxng/searxng (the official org) | OK | Approved — official org, high-traffic, active daily releases |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

The image is the official SearXNG project's own registry package (`github.com/searxng/searxng` → `pkgs/container/searxng`). DockerHub `searxng/searxng` is the same project's mirror. No third-party/fork image is recommended.

## Architecture Patterns

### System Architecture Diagram

```
                         villa CLI (control plane, Go)
                                   │
          ┌────────────────────────┼─────────────────────────────┐
          │ config.toml            │ orchestrate.Render()         │ cmd/villa install/up
          │ (WebSearchEnabled,     │ (PURE)                       │ (impure: write + systemctl)
          │  searxng_addr/port,    │                              │
          │  engine subset?)       ▼                              ▼
          │                  ┌───────────────┐         ┌──────────────────────────┐
          └─────────────────►│ render units  │────────►│ WriteUnits (atomic 0644) │
                             │ + settings.yml │         │ + write settings.yml     │
                             └───────────────┘         │ daemon-reload + start    │
                                                        └────────────┬─────────────┘
                                                                     │
        readiness proof (real query, NOT health-200):               ▼
        podman run --rm --network villa --entrypoint curl   ┌──────────────────────┐
        http://villa-searxng:8080/search?q=...&format=json  │ villa-searxng        │
                │  parse results[] → PASS/FAIL               │  (Quadlet container) │
                └───────────────────────────────────────────│  on villa.network    │
                                                             │  NO host port        │
                                                             │  settings.yml mount  │
                                                             │  $SEARXNG_SECRET env │
                                                             └──────────┬───────────┘
                                                                        │ outbound (opt-in only)
                                                                        ▼
                                                         vetted upstream engines
                                                         (bounded, auditable host set)
```

### Component Responsibilities

| Conceptual component | Implementation file (proposed) | Mirrors |
|----------------------|-------------------------------|---------|
| `searxngImage` const + `SearXNGImage()` accessor | `internal/orchestrate/searxng.go` (NEW) | `memory.go` `qdrantImage`/`QdrantImage()` |
| `searxngView` + `buildSearxngView()` + `searxngVolumeView` | `internal/orchestrate/searxng.go` (NEW) | `memory.go` `qdrantView`/`buildQdrantView()` |
| `settingsYmlView` + `buildSettingsYml()` | `internal/orchestrate/searxng.go` (NEW) | **no precedent — new file-render** |
| `searxng.container.tmpl` / `searxng.volume.tmpl` / `searxng-settings.yml.tmpl` | `internal/orchestrate/quadlet/` (NEW) | `qdrant.container.tmpl` (+ a new non-`.container` template) |
| Render branch (append-only, gated) | `render.go` `if in.Cfg.WebSearchEnabled { … }` | `render.go` `if in.Cfg.MemoryEnabled { … }` |
| Config gate + fields | `internal/config/villaconfig.go` | `MemoryEnabled` + `qdrant_addr`/`qdrant_port` block |
| Readiness proof verdict | `cmd/villa/install_*.go` | `memoryProof` / `evalMemoryProof` |

### Pattern 1: Managed-service render (clone of memory.go)

**What:** A new `internal/orchestrate/searxng.go` holds the digest-pinned image const behind a `SearXNGImage()` accessor, the container/volume view builders, and the container-DNS identity resolved from config. The render branch in `Render()` appends the new units **only when `WebSearchEnabled`**, so the off-render is byte-identical (SC#4).

**When to use:** This is the spine of SC#1.

**Example (the existing analog the planner copies):**
```go
// Source: internal/orchestrate/memory.go (verified in-repo)
const qdrantImage = "docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:b79aaa49…"
func QdrantImage() string { return qdrantImage }

func buildQdrantView(qdrantAddr string) qdrantView {
    return qdrantView{
        ContainerName: qdrantAddr,   // config-resolved (single source of truth)
        Image:         qdrantImage,  // seam-locked managed-service literal
        Network:       networkAttach,
        Volume:        qdrantVolumeMount,
    }
}
```
```go
// Source: internal/orchestrate/render.go (verified in-repo) — the gated append
if in.Cfg.MemoryEnabled {
    qdrantContainerText, _ := execTemplate(tmpl, "qdrant.container.tmpl", buildQdrantView(mv.QdrantAddr))
    // … append Unit{Name: qdrantContainerUnitName, Text: …}
}
```

**Critical seam note:** the SearXNG image literal trips the "container image literal" regex that `internal/inference/seam_test.go` (`TestSeamGrepGate`) walks over `internal/` + `cmd/villa`. The `isSeam` allowlist in `seam_test.go` was extended for `orchestrate/memory.go` in the SAME commit that added the Qdrant/embed literals (documented in `memory.go`'s header). The planner MUST extend that allowlist for `orchestrate/searxng.go` in the same commit (this is a documented, repeated pattern — see Pitfall 5).

### Pattern 2: Rendering and mounting settings.yml (THE NEW PATTERN)

**What:** SearXNG reads its config from `$SEARXNG_SETTINGS_PATH` → else `/etc/searxng/settings.yml` → else the bundled default. To control `search.formats`, `limiter`, and the engine subset, villa must render a `settings.yml` from config and mount it into the container.

**When to use:** SC#2 and SC#3 cannot be met without it.

**The design problem (planner must decide):** `WriteUnits` only writes Quadlet units into the unit dir (`~/.config/containers/systemd/`). A `settings.yml` is NOT a unit and must NOT land in the unit dir (systemd would try to parse it). Two viable approaches:

1. **Render settings.yml as a string + mount via a dedicated directory.** Write `settings.yml` into a villa-owned config dir (e.g. `$XDG_CONFIG_HOME/villa/searxng/settings.yml`), mount it read-only: `Volume=%h/.config/villa/searxng:/etc/searxng:ro,Z`. The render stays pure (a `(name, text)` pair like a Unit); a new impure writer (sibling of `WriteUnits`, same atomic-write + `assertInsideDir` guard) writes it. **Recommended** — keeps render pure, reuses the atomic-write discipline, keeps the file out of the unit dir.
2. **Pass everything via `$SEARXNG_*` env vars.** Not viable: `search.formats` and the engine `keep_only` subset have no env override; only a handful of `server.*` keys do. A settings.yml is mandatory for SRCH-01/04.

**Recommended file path:** mount the whole directory (`/etc/searxng`) read-only, not a single file (single-file bind-mounts interact badly with SELinux relabel and atomic-rename writes). Use a named-volume or a host-dir bind with `:ro,Z` — mirror the SELinux-label discipline already in `memory.go` (`:Z` private for villa-owned config).

**Single-source-of-truth requirement:** the `secret_key`, the engine subset, and `search.formats` must be config-derived and the rendered `settings.yml` byte-frozen by a golden (see Validation Architecture). A drift between the readiness probe's target and the rendered service is the classic failure — bind the searxng container-DNS name/port through config exactly as `mv.QdrantAddr`/`mv.EmbedPort` are (WR-01 discipline).

### Pattern 3: Bounded engine allowlist (SRCH-04 — the security-sensitive decision)

**What:** Restrict SearXNG to a small, auditable set of upstream engines instead of the full default set (~80+ engines reaching dozens of upstream hosts).

**Mechanism (verified):** `use_default_settings: {engines: {keep_only: [...]}}` inherits all non-engine defaults but keeps ONLY the listed engines:
```yaml
# Source: docs.searxng.org/admin/settings/settings.html (verified)
use_default_settings:
  engines:
    keep_only:
      - duckduckgo
      - brave
      - wikipedia
      # … (the bounded set)
server:
  secret_key: ""   # supplied via $SEARXNG_SECRET (see Pitfall 3)
  limiter: false
search:
  formats:
    - html
    - json
```

**Proposed defensible subset (planner confirms — this is a `[ASSUMED]` recommendation, NOT a locked decision; it should be reviewed against the operator's intent):**

| Engine | Upstream host(s) | Why include / exclude |
|--------|------------------|------------------------|
| `duckduckgo` | duckduckgo.com | General web; privacy-aligned; reliable in SearXNG |
| `brave` | search.brave.com | Independent index (not Google/Bing reseller); good coverage |
| `wikipedia` | *.wikipedia.org | High-trust reference; low injection risk |
| `wikidata` | wikidata.org | Structured facts for infoboxes |
| *(consider)* `startpage` or `mojeek` | startpage.com / mojeek.com | Add ONE more general engine for redundancy if a single engine is flaky |
| **Exclude** Google/Bing direct | google.com / bing.com | Frequently rate-limit/CAPTCHA self-hosted instances; brittle |
| **Exclude** social/forum engines (reddit, twitter, etc.) | many hosts | Higher untrusted-content + injection surface; not needed for v1.5 (focus modes are SRCH-V2) |
| **Exclude** image/video/file engines | many hosts | v1.5 grounds on text page fetches; image_proxy is off; out of scope |

**Rationale:** keep the outbound host set small (≤ ~5 upstream hosts), auditable (each host justifiable), and biased toward high-trust general/reference engines. This list is the auditable outbound surface that Phase 33's `villa verify search` allowlist will be cross-checked against — keep it minimal. The planner should treat the exact list as a discuss-able decision (see Assumptions Log A1).

### Pattern 4: Readiness by real `format=json` query (NOT health-200)

**What:** After starting `villa-searxng.service`, prove it works by issuing a real search query and parsing `results[]`, never by a `/healthz` 200.

**When to use:** SC#2 explicitly demands this; it matches the project's "offload-asserting, never liveness" principle and the existing `memoryProof` (which does an actual `/v1/embeddings` round-trip + a Qdrant write, not a `/readyz` alone).

**Example (the existing probe seam to reuse):**
```go
// Source: cmd/villa/install_memory.go (verified in-repo) — reuse this seam verbatim
func runProbeCurl(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, error) {
    args := []string{"run", "--rm", "--network", "villa", "--entrypoint", "curl", helperImage}
    args = append(args, curlArgs...)
    cmd := exec.CommandContext(ctx, "podman", args...) // fixed args; no shell
    // … return stdout
}
// SearXNG readiness: GET http://<searxngAddr>:<port>/search?q=villa+readiness+probe&format=json
// then json.Unmarshal → assert len(results) considered; an empty results[] with a 200
// is NOT automatically a pass — decide the verdict on parseable structure + at least
// the top-level number_of_results / results keys present (a real engine answer).
```
The JSON shape to parse: top-level `query`, `number_of_results`, `results[]`, `answers`, `suggestions`, `corrections`, `infoboxes`, `unresponsive_engines`; each `results[]` item has `url`, `title`, `content`, `engine`, `engines[]`, `score`, `category`, `publishedDate`. `[VERIFIED: docs.searxng.org/dev/search_api + JSON engine docs]`

**Verdict nuance:** a query that returns HTTP 200 with `{"results": []}` because every upstream engine timed out is NOT a healthy instance. The proof should require parseable JSON AND (recommended) at least one result OR a non-empty `number_of_results` for a well-known query, while tolerating transient single-engine failures via `unresponsive_engines`. The planner must define the exact pass condition (see Open Questions Q2).

### Anti-Patterns to Avoid
- **Rendering settings.yml into the unit dir.** systemd would try to parse a non-unit file. Use a separate config dir/mount.
- **Writing the secret_key into the 0644 settings.yml.** Units are world-readable; use `$SEARXNG_SECRET`.
- **Pinning the rolling daily tag without a digest.** Tags are rebuilt daily; pin the resolved amd64 manifest digest.
- **`limiter: true` without a valkey DB.** It silently fails / blocks; and it would require a second container.
- **Health-200 readiness.** Violates SC#2 and the project's real-signal principle.
- **Re-typing the image literal anywhere outside `searxng.go`.** Trips `TestSeamGrepGate`.
- **Letting `WebSearchEnabled` widen the off-render.** Any change to the always-on units breaks the v1.4 byte-identical goldens.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Metasearch / engine federation | A custom search aggregator | SearXNG image | The whole premise; reuse mature OSS (CLAUDE.md integration-first) |
| Container readiness probe over villa.network | A new HTTP client / host-port bind | Existing `runProbeCurl` seam (`podman run --rm --network villa --entrypoint curl`) | Already solves no-host-port reachability + fixed-arg-no-shell safety `[VERIFIED: install_memory.go]` |
| Atomic config-file write | A bespoke writer | Clone `reconcile.go` `atomicWrite` + `assertInsideDir` | Already does temp-write→fsync→rename + traversal guard `[VERIFIED: reconcile.go]` |
| settings.yml secret in a file | Hand-rolled file perms juggling | `$SEARXNG_SECRET` env var | SearXNG natively reads the secret from env; keeps it out of the 0644 file `[VERIFIED: settings_server docs]` |
| Engine pruning by editing the full default list | Copying + trimming the ~80-engine default block | `use_default_settings: {engines: {keep_only: […]}}` | Inherits all non-engine defaults; the list IS the audit artifact `[VERIFIED: settings.html docs]` |

**Key insight:** This phase is ~90% "clone the proven memory.go managed-service path" and ~10% "invent the settings.yml render+mount sub-pattern." Resist building anything the Qdrant/embed path already solved.

## Runtime State Inventory

> Greenfield-additive phase (new service, new config gate). Not a rename/refactor. Listed for completeness because it touches install/render state.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | New `villa-searxng` named volume (if SearXNG needs writable state for caches). SearXNG itself is largely stateless for a private JSON instance. | Likely a minimal/optional volume — planner confirms whether a volume is needed at all (Qdrant needed one; SearXNG may not). |
| Live service config | The rendered `settings.yml` is NEW villa-owned config NOT previously tracked by backup. | Phase 34 (SURF-07) covers backing it up; flag the path now so Phase 34 includes it. |
| OS-registered state | New `villa-searxng.service` (Quadlet-generated from `.container`), enabled/started additively when gated on. | Start ordering: after `villa.network`; independent of llama/qdrant (SearXNG has no dependency on them). |
| Secrets/env vars | NEW `$SEARXNG_SECRET` (the generated secret_key). First secret villa generates at runtime. | Store provenance/regeneration policy: regenerate on each render? persist in config.toml (0600)? Planner decides (see Pitfall 3 / Open Questions Q1). |
| Build artifacts | None — no host binary, no compiled artifact (image is pulled). | None. |

## Common Pitfalls

### Pitfall 1: settings.yml landing in the systemd unit directory
**What goes wrong:** `WriteUnits` writes into `~/.config/containers/systemd/`; if settings.yml is treated as a "unit" it lands there and systemd's generator chokes on a non-unit file.
**Why it happens:** Reusing the Unit slice naively for the config file.
**How to avoid:** Render settings.yml as a separate `(path, text)` artifact written by a sibling-of-`WriteUnits` impure writer into a dedicated villa config dir; mount that dir at `/etc/searxng:ro,Z`.
**Warning signs:** systemd `daemon-reload` warnings about an unparseable unit; `villa-searxng` failing to find its settings.

### Pitfall 2: secret_key exposed in a world-readable file
**What goes wrong:** Unit/config files written by `reconcile.go` are mode 0644 (world-readable, by design — systemd --user must read them). A secret_key rendered into such a file is exposed to every local user.
**Why it happens:** Copying the memory.go pattern (which has no secrets) verbatim.
**How to avoid:** Pass the secret via `Environment=SEARXNG_SECRET=…` (the env is in the 0644 .container unit too — so even that leaks it). **Best:** keep the secret in config.toml (0600) and inject it via an env file or a 0600 settings file mounted separately — the planner must choose a route that does not put the secret in a 0644 file. At minimum, document the exposure if a 0644 route is unavoidable for v1.5.
**Warning signs:** secret visible in `cat ~/.config/containers/systemd/villa-searxng.container`.

### Pitfall 3: secret_key regeneration churning the golden / breaking sessions
**What goes wrong:** If the secret_key is regenerated on every `Render()`, (a) the rendered settings.yml/unit can never be byte-frozen by a golden, and (b) the reconcile content-hash always differs → spurious restart every install.
**Why it happens:** Generating the secret inside the pure render.
**How to avoid:** Generate the secret ONCE (at first opt-in / install), persist it in config.toml (0600), and have render READ it from config (single source of truth). The golden then uses a fixed test secret. This mirrors how `coder_*` fields are "resolved AT ENTER, never re-picked later."
**Warning signs:** every `villa install` restarts villa-searxng; golden test flaps.

### Pitfall 4: bounded engine subset silently wrong because keep_only names mismatch
**What goes wrong:** `keep_only` entries must match SearXNG's engine **names** exactly; a typo silently drops the engine, shrinking results to empty and making the readiness proof fail intermittently.
**Why it happens:** Engine names vs shortcuts vs display names are easy to confuse.
**How to avoid:** Verify each `keep_only` name against the running image's default settings (`use_default_settings` source) on-hardware; the readiness proof (a real query) is the backstop that catches an all-empty result set.
**Warning signs:** `format=json` query returns `results: []` with all chosen engines in `unresponsive_engines` or absent.

### Pitfall 5: TestSeamGrepGate fails on the new image literal
**What goes wrong:** Adding `searxngImage = "ghcr.io/searxng/searxng@sha256:…"` to `orchestrate/searxng.go` trips the container-image-literal regex in `internal/inference/seam_test.go`.
**Why it happens:** The gate walks `internal/` + `cmd/villa` and flags image literals outside the allowlist.
**How to avoid:** Extend the `isSeam` allowlist in `seam_test.go` for `orchestrate/searxng.go` in the SAME commit (documented precedent for `memory.go` and the 12-02 ROCm tag).
**Warning signs:** `TestSeamGrepGate` red on first build.

### Pitfall 6: off-render drift breaks the v1.4 byte-identical goldens (SC#4)
**What goes wrong:** Any incidental change to the always-on render path (reordering, a new field on a shared view) changes the existing `villa-llama.container.golden` / `villa-openwebui.container*.golden` bytes.
**Why it happens:** Touching shared render code instead of an isolated gated branch.
**How to avoid:** Add SearXNG strictly as an APPENDED, `WebSearchEnabled`-gated branch (exactly like the `MemoryEnabled` branch); never mutate shared views. The existing 13 orchestrate goldens MUST stay unchanged.
**Warning signs:** golden diff on a unit unrelated to SearXNG.

## Code Examples

### Example 1: Minimal private SearXNG settings.yml (the render target)
```yaml
# Source: docs.searxng.org/admin/settings/* (verified) — rendered by villa from config
use_default_settings:
  engines:
    keep_only:
      - duckduckgo
      - brave
      - wikipedia
      - wikidata
server:
  secret_key: "{{.SecretKey}}"   # supplied via $SEARXNG_SECRET — see Pitfall 2/3
  limiter: false                 # no valkey DB; private instance
  image_proxy: false
search:
  formats:
    - html
    - json
  safe_search: 0
  autocomplete: ""
```

### Example 2: settings.yml mount in the .container unit
```ini
# Source: extrapolated from qdrant.container.tmpl + searxng docs mount path
[Container]
ContainerName={{.ContainerName}}
Image={{.Image}}
Network={{.Network}}
Environment=SEARXNG_SECRET={{.SecretRef}}     # planner: prefer a 0600 route
Volume={{.SettingsMount}}                       # %h/.config/villa/searxng:/etc/searxng:ro,Z
[Service]
Restart=on-failure
```

### Example 3: readiness proof query (reusing runProbeCurl)
```go
// Source: composed from install_memory.go runProbeCurl + searxng JSON API
url := fmt.Sprintf("http://%s:%d/search", in.searxngAddr, in.searxngPort)
out, err := runProbeCurl(ctx, helperImage,
    "-sf", "-G", url,
    "--data-urlencode", "q=villa readiness probe",
    "--data-urlencode", "format=json",
)
var resp struct {
    NumberOfResults int `json:"number_of_results"`
    Results []struct {
        URL, Title, Content, Engine string
    } `json:"results"`
}
// PASS condition: parseable JSON with structure present (planner defines exact threshold).
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `ENABLE_RAG_WEB_SEARCH` / `RAG_WEB_SEARCH_ENGINE` (older OWUI env family) | `ENABLE_WEB_SEARCH` / `WEB_SEARCH_ENGINE` (current) | OWUI evolution | Phase 30 concern, not 29 — but flag: re-verify env names at the pinned OWUI digest |
| DockerHub pulls | ghcr.io (DockerHub now rate-limits unauthenticated pulls) | 2025–2026 | Pull from ghcr; pin digest |
| Public-instance limiter on by default | Private instances disable limiter (avoids valkey dep) | standing | `limiter: false` for villa |

**Deprecated/outdated:**
- Pinning SearXNG by `:latest` or a bare daily tag — always digest-pin.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The proposed engine subset (duckduckgo, brave, wikipedia, wikidata) is the right vetted set | Architecture Patterns §3 | Wrong set = either too-broad outbound surface (security) or too-flaky results (UX). This is the security-sensitive SRCH-04 decision — should be confirmed with the operator before locking. |
| A2 | The `2026.6.18-b5ef7ec8f` digest is a usable starting reference | Standard Stack | Rolling tag; the exact digest will be superseded — planner re-resolves on-hardware. Low risk (procedure, not value). |
| A3 | SearXNG needs little/no writable volume for a private JSON instance | Runtime State Inventory | If a cache/state dir is needed, planner adds a `.volume` unit (cheap clone). |
| A4 | A 0600 route for the secret_key is achievable within v1.5 scope | Pitfall 2/3 | If no clean 0600 route exists, the secret may land in a 0644 unit/env — must be documented as a known limitation rather than silently exposed. |

## Open Questions

1. **Where does the generated secret_key live and when is it regenerated?**
   - What we know: must be config-derived for a stable golden; the repo keeps secrets at 0600 (config.toml), units at 0644.
   - What's unclear: persist in config.toml and inject via env, vs a separate 0600 settings file; regenerate-on-first-opt-in vs rotate.
   - Recommendation: generate once at opt-in via `crypto/rand`, persist in config.toml (0600), inject via `$SEARXNG_SECRET`; never regenerate on plain re-install.

2. **Exact `format=json` readiness PASS condition.**
   - What we know: must be a real query, not health-200; transient single-engine failures are normal (`unresponsive_engines`).
   - What's unclear: require ≥1 result, vs require parseable structure + non-empty `number_of_results`, vs tolerate empty results if JSON is well-formed.
   - Recommendation: require parseable JSON with `results`/`number_of_results` keys present AND ≥1 result for a well-known probe query, with a short retry to absorb cold-start engine timeouts.

3. **Does the settings.yml mount need a named volume or a host-dir bind?**
   - What we know: `:Z` private SELinux label discipline is established; atomic-rename writes interact badly with single-file binds.
   - Recommendation: mount a villa-owned config DIRECTORY (`%h/.config/villa/searxng:/etc/searxng:ro,Z`), write settings.yml into it via the atomicWrite clone.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| rootless Podman v5 | Render → run the container | ✓ (project target; dev box is live gfx1151) | v5 | — (hard requirement, already met by v1.0–v1.4) |
| `systemctl --user` | Quadlet lifecycle | ✓ | — | — |
| ghcr.io reachability | One-time image pull at install | ✓ (sanctioned install-time outbound) | — | DockerHub mirror (rate-limited) |
| curl (in helper image) | Readiness proof | ✓ via `orchestrate.EmbedImage()` (ships curl) | — | — |

**Missing dependencies with no fallback:** none (all are the established v1.x runtime).
**Missing dependencies with fallback:** ghcr.io → DockerHub mirror.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + byte-for-byte goldens; no third-party assert/mock) |
| Config file | none (stdlib `go test`) |
| Quick run command | `go test ./internal/orchestrate/... ./internal/config/... ./cmd/villa/...` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Automated Command | File Exists? |
|-----|----------|-----------|-------------------|-------------|
| SRCH-01 (render) | `villa-searxng.container`/`.volume` render on `villa.network`, no host port, digest-pinned | golden | `go test ./internal/orchestrate -run TestRenderSearxng` | ❌ Wave 0 (new golden `villa-searxng.container.golden`) |
| SRCH-01 (settings) | settings.yml rendered with `search.formats:[html,json]`, `limiter:false`, secret ref | golden | `go test ./internal/orchestrate -run TestRenderSearxngSettings` | ❌ Wave 0 (new golden `searxng-settings.yml.golden`) |
| SRCH-01 (seam) | image literal stays behind the searxng seam | grep-gate | `go test ./internal/inference -run TestSeamGrepGate` | ✅ exists (extend `isSeam` allowlist) |
| SRCH-01 (readiness) | a real `format=json` query parses results[] → PASS; an unreachable/empty → FAIL | unit (pure verdict + injected probe) | `go test ./cmd/villa -run TestSearxngProof` | ❌ Wave 0 (mirror `evalMemoryProof`) |
| SRCH-04 | settings.yml restricts engines via `keep_only` to the vetted subset | golden + explicit-contains test | `go test ./internal/orchestrate -run TestRenderSearxngEngineAllowlist` | ❌ Wave 0 |
| SC#4 / PRIV-07 | with `WebSearchEnabled=false`, the full render is byte-identical to v1.4 (13 existing goldens unchanged) | golden (negative) | `go test ./internal/orchestrate` (existing goldens must NOT change) | ✅ exists (must stay green) |
| SRCH-01 (config) | new gate + searxng fields omit-when-off (byte-identical config) | unit | `go test ./internal/config` | ✅ exists (extend, mirror memory-block marshal test) |

### Sampling Rate
- **Per task commit:** `go test ./internal/orchestrate/... ./internal/config/...`
- **Per wave merge:** `make check`
- **Phase gate:** full suite green + on-hardware readiness UAT (real `format=json` query against the running container) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/orchestrate/testdata/villa-searxng.container.golden` — covers SRCH-01
- [ ] `internal/orchestrate/testdata/villa-searxng.volume.golden` — covers SRCH-01 (if a volume is used)
- [ ] `internal/orchestrate/testdata/searxng-settings.yml.golden` — covers SRCH-01 + SRCH-04
- [ ] `internal/orchestrate/searxng_test.go` — render + engine-allowlist explicit-contains tests
- [ ] `cmd/villa/*searxng*_test.go` — pure `evalSearxngProof` + injected-probe readiness test
- [ ] Extend `internal/inference/seam_test.go` `isSeam` allowlist for `orchestrate/searxng.go`
- [ ] Extend `internal/config` marshal/omit test for the new gate + fields
- [ ] On-hardware UAT step: real `format=json` query parses results[]; `ss`/`podman port` confirms NO host port

## Security Domain

> `security_enforcement: true`, ASVS L1.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No user auth in this phase (container-DNS-only service) |
| V3 Session Management | partial | `secret_key` is SearXNG's session/CSRF crypto seed — generate with `crypto/rand`, keep secret (Pitfall 2/3) |
| V4 Access Control | yes | No host port; container-DNS reachability only on `villa.network` (network-level access control) |
| V5 Input Validation | yes | All podman/curl args FIXED, no shell interpolation; the probe query string passed via `--data-urlencode`, never concatenated |
| V6 Cryptography | yes | `secret_key` via `crypto/rand` (never `math/rand`); never hand-roll; never log the secret |
| V12 File handling | yes | settings.yml written via atomic-write + `assertInsideDir` traversal guard (clone reconcile.go); secret never in a 0644 file |
| V14 Configuration | yes | Image digest-pinned; `limiter:false` documented; engine `keep_only` is the auditable outbound surface |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| secret_key leakage via world-readable unit/config | Information Disclosure | `$SEARXNG_SECRET` from a 0600 source; never render into a 0644 file (Pitfall 2) |
| Unbounded outbound via full default engine set | Information Disclosure / scope creep | `keep_only` bounded subset (SRCH-04); the list is the audit artifact Phase 33 verifies against |
| Floating image tag substituted with malicious image | Tampering | Digest-pin the resolved amd64 manifest; pull from official ghcr org |
| Shell injection via query string in the probe | Tampering / Injection | Fixed-arg exec, `--data-urlencode`, no shell (existing `runProbeCurl` discipline) |
| Path traversal writing settings.yml | Tampering | `assertInsideDir` guard (clone reconcile.go) |
| Off-render leaking outbound when web search is "off" | scope/privacy | `WebSearchEnabled`-gated append-only branch; byte-identical off-render proven by goldens (SC#4 / PRIV-07) |

> Out of scope for Phase 29 (later phases): the actual outbound-bound PROOF (`villa verify search`, Phase 33), injection guard on fetched content (Phase 32), SSRF on result-page fetch (Phase 31). Phase 29 only stands up the service and bounds the engine set.

## Sources

### Primary (HIGH confidence)
- `internal/orchestrate/memory.go`, `render.go`, `reconcile.go`, `systemd.go`, `openwebui.go` — the managed-service render path, atomic write, seam-lock, gated-append pattern (verified in-repo)
- `cmd/villa/install_memory.go` — the `runProbeCurl` / `evalMemoryProof` readiness-by-real-query seam (verified in-repo)
- `internal/config/villaconfig.go` — config gate + omit-when-off marshal pattern (verified in-repo)
- docs.searxng.org/admin/settings/settings_server.html — secret_key/`$SEARXNG_SECRET`, limiter-needs-valkey, image_proxy
- docs.searxng.org/admin/settings/settings_search.html — `search.formats` (html/json/csv/rss)
- docs.searxng.org/admin/settings/settings.html — `use_default_settings:{engines:{keep_only:…}}`, `SEARXNG_SETTINGS_PATH`/`/etc/searxng/settings.yml`
- docs.searxng.org/dev/search_api.html — `format=json` query + JSON response shape
- ghcr.io/searxng/searxng package page — official image, current tags + digest

### Secondary (MEDIUM confidence)
- docs.searxng.org/admin/installation-docker.html — ghcr vs DockerHub rate-limit guidance
- WebSearch synthesis of JSON response field names (cross-checked with the JSON-engine docs)

### Tertiary (LOW confidence)
- Exact current image digest (`sha256:ed29454…`) — rolling daily tag, re-resolve on-hardware

## Metadata

**Confidence breakdown:**
- Render path / managed-service pattern: HIGH — verbatim in-repo analog (memory.go), fully read
- settings.yml schema (formats/limiter/secret/keep_only): HIGH — official docs, cross-checked
- Image identity: HIGH (repo) / MEDIUM (exact digest — rolling tag)
- Engine subset: MEDIUM — mechanism HIGH, the specific list is an `[ASSUMED]` recommendation pending operator confirmation
- Readiness proof: HIGH — existing seam + verified JSON API
- secret_key handling: MEDIUM — route options clear, exact 0600 mechanism is a planner decision

**Research date:** 2026-06-18
**Valid until:** 2026-07-18 for the codebase/settings.yml facts (stable); ~7 days for the image digest (rolling daily tag — re-resolve at pin time).
