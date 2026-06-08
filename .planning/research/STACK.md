# Stack Research

**Domain:** Operability features for a single-static-binary Go CLI orchestrating a local AI stack (rootless Podman/Quadlet, llama.cpp, Open WebUI) on AMD Strix Halo / Fedora
**Researched:** 2026-06-07
**Confidence:** HIGH (TUI lib versions, podman volume cmds, and the rocm-6.4.4 image+digest all verified against live sources; usage-store recommendation is a design call with HIGH confidence on the constraint analysis)

## Scope note

This is a SUBSEQUENT-milestone (v1.2 Operability) stack study. The v1.0/v1.1 stack
(Go 1.26.2, cobra v1.10.2, chi v5.3.0, ghw v0.24.0, BurntSushi/toml v1.6.0, rootless
Podman v5 + Quadlet, llama.cpp Vulkan/ROCm via digest-pinned kyuz0 images, Open WebUI,
stdlib-`testing`-only) is FIXED and is not re-litigated here. This document covers ONLY
what the six v1.2 features add — and, critically, what they must NOT add.

**Headline:** v1.2 needs exactly **one** new first-party dependency — a TUI form library
(`charmbracelet/huh`) for the guided install (INSTALL-01). Every other feature is
buildable with the standard library plus fixed-arg `podman` exec, preserving the
single-static-binary / CGO-free / zero-telemetry / no-third-party-test-lib posture.

## Recommended Stack

### Core Technologies (NEW for v1.2)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/charmbracelet/huh` | **v1.0.0** (2026-02-23) | INSTALL-01 guided TUI: grouped forms (Select/Input/Confirm/Note), built-in validation + accessible mode | Highest-level fit for a *wizard* (not a bespoke app): you declare fields, it runs the loop. Pure-Go, CGO-free, statically links — preserves the single-binary constraint. Stable v1.0.0 transitively pins the **stable** `bubbletea v1.3.6` + `lipgloss v1.1.0` (NOT the churny new `charm.land/bubbletea/v2` path), so the dep graph is coherent and proven. Has a no-TTY/`WithAccessible` path so the flag-driven CLI install stays the non-interactive default and CI/`--json` is unaffected. |
| `podman volume export` / `podman volume import` (CLI, already on host) | Podman v5 | BAK-01 backup/restore of the Open WebUI named volume | **Zero new runtime/Go deps** — invoked via the existing fixed-arg `exec.Command` seam pattern (no shell interpolation). Native tarball over STDOUT/STDIN, works rootless, and works on the `local` volume driver — which is exactly the driver villa's Quadlet `.volume` units use. Strictly local (writes a tar to a user-chosen path); no telemetry surface. |
| Standard library (`encoding/json`, `os`, `path/filepath`, `time`) | Go 1.26.2 | BENCH-03 saved reports + USAGE-01 usage history persistence | A flat append-only **JSONL** log under XDG (see decision below) covers both. No store, no CGO, no schema migrations, trivially golden-testable — matches the existing "config/TOML + go:embed JSON" persistence idiom and the byte-frozen-contract discipline. |
| Container image `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4` | digest `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62` | ROCM-ALT-01 TG-tuned alternate backend | **Image existence CONFIRMED** on Docker Hub (live registry manifest, pushed ~2026-06-06). Slots behind the existing `inference.BackendFor` resolver / `Backend` seam exactly like the v1.1 `rocm-7.2.4` pin — no new dependency, just a new digest-pinned literal inside `internal/inference`. |

### Supporting Libraries (transitively pulled by `huh` — informational, not separately added)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/charmbracelet/bubbletea` | v1.3.6 (pinned by huh) | Elm-architecture TUI runtime under `huh` | Only if a screen outgrows declarative forms (unlikely for an install wizard). Prefer staying in `huh`. |
| `github.com/charmbracelet/bubbles` | v0.21.x (pinned by huh) | Reusable TUI components (spinner/progress) | A `spinner`/`progress` during the long `podman pull` + bring-up step of the guided install. |
| `github.com/charmbracelet/lipgloss` | v1.1.0 (pinned by huh) | Terminal styling | Already transitive; use for wizard styling only — do not adopt for non-TUI CLI output (keep plain tables). |

> All four are pure-Go and statically link cleanly. The full transitive set (see go.mod
> dump in Sources) is ~30 small terminal/ANSI helper modules — no cgo, no native libs.

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| stdlib `testing` + golden files | Test the new JSONL store, bench-compare math, doctor report, and rocm-6.4.4 render delta | **No new test lib** (hard constraint). USAGE/BENCH stores are pure → table-driven; render delta + `--json` get `*.golden` like v1.1 (append-only, schema-bump, refreeze once). |
| `huh` accessible/no-TTY mode | Keep INSTALL-01 testable + CI-safe | Drive the wizard's underlying pure `Deps`/answers struct in tests; never require a PTY in `go test`. Mirror the existing `live*Deps`/`fake*Deps` seam so the wizard is a thin shell over the already-tested install core. |

## Installation

```bash
# Single NEW first-party dependency for the whole milestone:
go get github.com/charmbracelet/huh@v1.0.0

# go.sum/go.mod updated; verify the binary still builds CGO-free + statically:
CGO_ENABLED=0 go build -o ./villa ./cmd/villa
make check    # vet + test gate

# No new RUNTIME dependency for backup/restore — podman is already required on the host:
#   podman volume export  villa-openwebui --output <backup>.tar
#   podman volume import   villa-openwebui  <backup>.tar      # volume must pre-exist
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `huh` v1.0.0 (declarative forms) | `bubbletea` v1.3.10 raw (Model-View-Update) | Only if the install UI needs a genuinely custom multi-pane interactive layout. For a linear setup wizard this is strictly more code with no benefit. |
| `huh` v1.0.0 | `charmbracelet/bubbletea` **v2** (`charm.land/bubbletea/v2`, v2.0.7) | Avoid for now: v2 moved to a new module path (`charm.land/...`) mid-2026 and `huh v1.0.0` still targets stable `bubbletea v1.3.6`. Adopting v2 directly would split the dep graph and fight `huh`. Revisit only if/when `huh` itself moves to the v2 base. |
| `huh` v1.0.0 | `manifoldco/promptui` v0.9.0 / `AlecAivazis/survey/v2` v2.3.7 | Lighter, fewer deps — choose ONLY if you want minimal prompt-by-prompt Q&A and reject any Charm styling. **Caveat: both are effectively unmaintained** (promptui last release 2021, survey 2022). For a flagship "guided install" UX, `huh`'s active maintenance + accessibility mode wins. Keep this row as the fallback if dep-weight review rejects the Charm tree. |
| `huh` v1.0.0 | Hand-rolled `bufio.Scanner` prompts (stdlib only) | The zero-dependency purist option. Viable and fully constraint-compliant, but yields a markedly worse "just works" first-run UX (no validation, no re-edit, no arrow-key select). Use only if the roadmap decides INSTALL-01 should ship as a plain prompted flow first and defer the rich TUI. |
| Flat **JSONL** files under XDG (BENCH-03/USAGE-01) | Embedded SQLite (`modernc.org/sqlite`, pure-Go, CGO-free) | Only if usage data grows to needing indexed/aggregate queries over very large histories. modernc.org/sqlite IS CGO-free (so it wouldn't break the static binary) but it is a **large** generated dependency (~hundreds of files) and a real schema/migration burden — disproportionate for append-mostly bench reports and a rolling usage counter. Defer unless a future milestone needs SQL. |
| Flat JSONL | `mattn/go-sqlite3` | **Never** — CGO-based; breaks `CGO_ENABLED=0` static-binary build. Hard no. |
| `podman volume export/import` | tar-in-a-throwaway-container (`podman run --rm -v vol:/data busybox tar ...`) | Fallback only if a host's Podman predates `volume export/import` (both have been stable since Podman 4.3). The native subcommands are simpler, need no extra image pull, and avoid the ownership/permission foot-guns reported with manual tar-into-mountpoint. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `mattn/go-sqlite3` (or any CGO store) | Requires `CGO_ENABLED=1`; destroys the single-static-binary distribution constraint | Flat JSONL under XDG; or pure-Go `modernc.org/sqlite` *only* if SQL is truly required later |
| `charm.land/bubbletea/v2` as a *direct* dep right now | New module path; not what `huh v1.0.0` targets → bifurcated dep graph and version friction | `huh v1.0.0` (pulls stable `bubbletea v1.3.6`) |
| Any third-party test/assertion/mock lib (testify, ginkgo, gomock) | Hard project constraint: stdlib `testing` + golden files only | Table-driven stdlib tests; injected `fake*Deps`; `*.golden` fixtures |
| `podman-remote` for backup/restore | `podman volume export` is **not supported over podman-remote** | Local rootless `podman` (villa is local-only by design — no remote socket) |
| Hand-editing the Open WebUI Quadlet `.volume` to a non-`local` driver to ease backup | `volume export/import` only works with the `local` driver; also violates "config is the single source of truth / units regenerated, never hand-edited" | Keep the `local` driver (current behavior); export/import works as-is |
| Sending any usage/bench telemetry off-box | Zero-telemetry is a core constraint | Persist usage/bench ONLY to local XDG files; surface via `status`/dashboard read-models |
| Adding a metrics/Prometheus client dep for USAGE-01 | The existing `internal/metrics` already scrapes llama.cpp `/metrics` (Prometheus text) with stdlib `net/http` + a bounded `io.LimitReader` | Extend `internal/metrics` + a new pure usage-accumulator core; persist deltas to JSONL |

## Integration Points (existing `internal/*` layout)

| Feature | New/changed code | Reuses (do NOT re-implement) |
|---------|------------------|------------------------------|
| INSTALL-01 (TUI) | New thin `cmd/villa/install_tui.go` (or a `huh` form behind an `--interactive`/no-flags path) that *collects answers* then calls the **existing** install core. New `internal/installwizard` pure answer/validation core if logic warrants. | The whole detect→recommend→preflight→orchestrate pipeline + `live*Deps` seam already in `cmd/villa/install.go`. The wizard MUST be a shell over it, not a fork. |
| BAK-01 (backup/restore) | New `internal/backup` core (pure plan: which paths/volume/config) + a host-touching seam for `podman volume export/import`; new `cmd/villa/backup.go` (`villa backup` / `villa restore`). Volume literals (`villa-openwebui`) and `podman` invocation belong with `orchestrate`/inference seam rules — keep `podman`/volume-name literals out of `cmd/villa` per `TestSeamGrepGate` discipline (extend the gate if needed). | `internal/config` for the config.toml side; `internal/orchestrate` is the existing impure host-exec home — restore must stop the stack, import, restart (compose lifecycle verbs, don't re-roll them). |
| BENCH-03 (compare + saved) | New `internal/benchstore` (pure: marshal/append/load/compare `bench.Result` JSONL). `villa bench --compare` reads history; saving is an append on each run. | `internal/bench` `Result`/`Stats`/`ABResult` structs are the persisted record (version them; freeze with goldens, append-only). |
| USAGE-01 (cumulative usage) | New pure `internal/usage` accumulator (counter deltas + JSONL rollup under XDG) fed by a periodic scrape; surfaced in the frozen `status.Report` + dashboard (append-only, schema-bump). | `internal/metrics` (`PerfSnapshot`, `/metrics` scrape) already exists — derive cumulative tokens from gauges/counters there; do not add a metrics dep. `internal/status` + dashboard read-model are the surfacing path. |
| DOCTOR-01 (`villa doctor`) | New `cmd/villa/doctor.go` + a `internal/doctor` orchestration core that re-runs the existing checks against a *running* install and adds drift checks. **No new dependency.** | `internal/preflight` (reusable BLOCK/WARN `[]CheckResult` + refuse-with-remediation), `internal/status` read-model, `internal/inference` residency proof. Doctor = compose these; remediation strings already exist on `CheckResult`. |
| ROCM-ALT-01 (rocm-6.4.4) | New digest-pinned backend literal in `internal/inference` (alongside `backend_rocm.go`) + `BackendFor` mapping for the alt image; preflight `rocm-policy.json` allow-list entry. | The entire v1.1 `Backend` seam, transactional `backendswap`, and `bench --ab` machinery — the alt image is just another seam-locked literal + policy entry. |

## Decision: JSONL flat files vs SQLite (BENCH-03 + USAGE-01) — RESOLVED

**Recommendation: flat append-only JSONL files under `$XDG_DATA_HOME/villa/` (e.g. `bench-history.jsonl`, `usage.jsonl`).** Rationale:

1. **Single-binary / CGO-free is non-negotiable.** CGO SQLite (`mattn/go-sqlite3`) is out. Pure-Go `modernc.org/sqlite` would technically link statically but is a heavyweight generated tree and a migration burden.
2. **Access pattern is append-mostly + small.** Bench reports are written once per run and read in full for `--compare`; usage is a rolling counter. There is no query workload that needs an index. Loading a few hundred JSONL lines is trivially fast and bounded by `io.LimitReader` like the existing scrapes.
3. **Matches the established idiom & test discipline.** The codebase already persists structured data as JSON (`seed.json`, `rocm-policy.json`, golden fixtures) and TOML config; JSONL records are byte-stable and golden-testable, fitting the "byte-frozen contract / append-only / schema-bump" rule.
4. **Honesty/typed-Unknown friendly.** A missing/partial line degrades to "no history" cleanly, mirroring the typed-Unknown posture.

**Storage location:** use `$XDG_DATA_HOME/villa/` (resolve via `os.UserConfigDir` sibling or explicit XDG lookup) — keep volatile history OUT of `config.toml` (config stays the source of truth for *settings*, not append logs). Mode `0600` files / `0700` dir, with the same path-traversal guard `internal/config` already uses on writes.

## ROCM-ALT-01: rocm-6.4.4 image — VERIFIED

- **Exists:** YES. `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4` is a live tag.
- **Pin digest (manifest, from live registry):**
  `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62`
- **rocWMMA variant also exists** (matrix-multiply-tuned, relevant to TG perf):
  `rocm-6.4.4-rocwmma` → `sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141`
- **Caveat:** kyuz0 re-pushes frequently (multiple timestamped `rocm-6.4.4_2026...` tags share the rolling `rocm-6.4.4` tag). PIN THE DIGEST, never the bare tag — exactly as v1.1 did for `rocm-7.2.4`. Re-verify the digest at implementation time (`skopeo inspect` / registry manifest) in case the rolling tag advanced; record the chosen digest in `rocm-policy.json` allow-list + `internal/inference`.
- **rocWMMA decision flag:** ROCM-ALT-01's stated goal is fixing the v1.1 Δtg −11.15 regression. The `-rocwmma` variant is the more likely TG win and should be benchmarked (`villa bench --ab`) against plain `rocm-6.4.4` and the incumbent `rocm-7.2.4` during the phase. Treat "which rocm-6.4.4 digest to ship" as a bench-decided choice, consistent with the v1.1 honesty constraint (never promise a speed-up un-benchmarked).

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `huh@v1.0.0` | `bubbletea@v1.3.6`, `lipgloss@v1.1.0`, `bubbles@v0.21.x` | These are the exact versions huh v1.0.0's go.mod requires; let `go mod tidy` resolve — do NOT manually pin bubbletea v2. |
| `huh@v1.0.0` | Go 1.26.2 | huh declares `go 1.23.0`; well within the project toolchain. CGO not required. |
| `huh@v1.0.0` | cobra v1.10.2 / chi v5.3.0 | No overlap/conflict; huh is invoked only from the install command path, isolated from the dashboard/server. |
| `podman volume export/import` | Podman v5 (host) | Stable since Podman 4.3; rootless OK; `local` driver only; not over podman-remote. Villa already requires rootless Podman v5. |
| rocm-6.4.4 image | gfx1151 / Fedora 44+ kernel floor | Subject to the same `rocm-policy.json` kernel/firmware floors as rocm-7.2.4; re-verify floors apply before allow-listing. |

## Sources

- ctx7 CLI (Context7) `/charmbracelet/huh`, `/charmbracelet/bubbletea`, `/charmbracelet/bubbles` — library resolution, capability summaries — HIGH
- `https://proxy.golang.org/.../@latest` + `@v/v1.0.0.mod` — **authoritative** Go-module versions & dep graph: huh v1.0.0 (2026-02-23) depends on bubbletea **v1.3.6** (NOT charm.land/v2); bubbletea github path latest stable v1.3.10; `charm.land/bubbletea/v2` latest v2.0.7; lipgloss v1.1.0; bubbles v1.0.0; promptui v0.9.0 (2021); survey/v2 v2.3.7 (2022) — HIGH
- Docker registry manifest API (`registry-1.docker.io/v2/kyuz0/amd-strix-halo-toolboxes/manifests/*`) — **full sha256 digests** for rocm-6.4.4, rocm-6.4.4-rocwmma, rocm-7.2.4, vulkan-radv — HIGH
- `https://hub.docker.com/r/kyuz0/amd-strix-halo-toolboxes/tags` — tag inventory incl. timestamped rocm-6.4.4 re-pushes — HIGH
- `https://docs.podman.io/en/latest/markdown/podman-volume-export.1.html` + `podman-volume-import.1.html` — export→tarball/STDOUT, import from file/STDIN, volume must pre-exist — HIGH
- `github.com/containers/podman` discussions #23054 / issues #14411, #25442 — rootless backup patterns + ownership/permission caveats with manual tar; `local`-driver / podman-remote limitations — MEDIUM
- Existing codebase: `internal/metrics/llamacpp.go`, `internal/bench/bench.go`, `internal/config/villaconfig.go`, CLAUDE.md (seam/golden/single-binary constraints) — HIGH

---
*Stack research for: v1.2 Operability features on the villa Go control plane*
*Researched: 2026-06-07*
