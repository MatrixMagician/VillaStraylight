# Project Research Summary

**Project:** VillaStraylight — v1.2 milestone (Operability)
**Domain:** Operability features layered onto a shipped, single-static-binary Go CLI control plane (`villa`) orchestrating a strictly-local AI stack (rootless Podman/Quadlet, llama.cpp Vulkan/ROCm, Open WebUI) on AMD Strix Halo / Fedora
**Researched:** 2026-06-07
**Confidence:** HIGH

## Executive Summary

v1.2 adds six operability features — `villa doctor` (DOCTOR-01), backup/restore (BAK-01), `bench --compare` + saved reports (BENCH-03), cumulative usage tracking (USAGE-01), a guided TUI install (INSTALL-01), and a `rocm-6.4.4` alternate backend (ROCM-ALT-01) — to a stack whose architecture (pure cores in `internal/*`, host effects behind injectable `Deps` func-field seams, `orchestrate` as the only intentionally-impure module) was deliberately built to absorb exactly this kind of extension. The headline finding across all four researchers is that **this is an integration milestone, not an ecosystem milestone**: five of the six features are buildable with the standard library plus the existing fixed-arg `podman`/systemd seams, and the entire milestone adds **exactly one** new first-party dependency — `charmbracelet/huh` v1.0.0, a pure-Go (CGO-free) TUI form library, gated to INSTALL-01 and confined to the command tier.

The recommended approach is to add one new pure `internal/*` core per feature that has decision logic (`doctor`, `backup`, `benchstore`, `usage`), keep every host effect (podman volume export/import, file I/O) behind an `orchestrate`-resident or command-tier seam, and persist new state as flat append-only **JSONL/JSON under `$XDG_DATA_HOME/villa/`** — never in `config.toml` (which stays the single source of *configuration* truth) and never in an embedded SQLite store (CGO SQLite breaks the static binary; pure-Go `modernc.org/sqlite` is a disproportionate migration burden for append-mostly data). ROCM-ALT-01 is the cheapest and most architecturally-proven feature — the `BackendFor` seam was first exercised by the v1.1 ROCm backend specifically to absorb a new digest-pinned image with zero caller changes; its `rocm-6.4.4` image is verified live (`sha256:c81f30a7…150ec62`), with a matrix-tuned `-rocwmma` variant (`sha256:9a97129a…3c0141`) to be treated as a **bench-decided** choice for recovering the v1.1 Δtg −11.15 regression.

The dominant risk is BAK-01 (highest-risk feature): rootless-Podman UID-mapping/SELinux mangling, torn live-SQLite snapshots, accidental sweeping of multi-GB model weights, and version-skew restore. Mitigation is to mirror the proven `backendswap` transactional discipline (quiesce → capture/swap → restart → prove → rollback), use `podman volume export/import` (never host-path tar), **exclude model weights** (record their identity in the manifest, re-pull on restore), and stamp every archive with a version/digest/host-fingerprint manifest that WARNs on skew. Secondary honesty risks: USAGE-01 must not become telemetry or log content (counts-only, local, single-writer), and DOCTOR-01 must inherit the offload-asserting discipline (a green doctor over a silent CPU fallback is worse than no doctor). All `--json`/dashboard contract changes evolve append-only + schema-bump, re-frozen exactly once.

## Key Findings

### Recommended Stack

The v1.0/v1.1 stack (Go 1.26.2, cobra, chi, ghw, BurntSushi/toml, rootless Podman v5 + Quadlet, digest-pinned kyuz0 inference images, Open WebUI, stdlib-`testing`-only) is fixed and not re-litigated. v1.2 adds **one** new first-party dependency and one new container image; everything else is stdlib + existing seams. Full detail: `.planning/research/STACK.md`.

**Core technologies (new for v1.2):**
- **`github.com/charmbracelet/huh` v1.0.0** (INSTALL-01 only): declarative TUI forms with built-in validation + an accessible/no-TTY mode — pure-Go/CGO-free, statically links, transitively pins the *stable* `bubbletea v1.3.6` / `lipgloss v1.1.0` (NOT the churny `charm.land/bubbletea/v2` path). The milestone's only new dependency; isolated to the command tier (no pure core may import it).
- **`podman volume export` / `import`** (BAK-01): native rootless tarball backup/restore of the Open WebUI volume via the existing fixed-arg `exec.Command` seam — zero new Go/runtime deps; `local`-driver only; not over podman-remote (fine — villa is local-only).
- **Flat JSONL under `$XDG_DATA_HOME/villa/`** (BENCH-03 + USAGE-01): append-mostly history is byte-stable, golden-testable, CGO-free, and matches the existing JSON/TOML persistence idiom. Decision RESOLVED: **JSONL over SQLite** (CGO SQLite breaks the static binary; pure-Go SQLite is an over-heavy generated tree + migration burden for data with no query workload).
- **`docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4`** (ROCM-ALT-01): existence VERIFIED, digest `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62`. The `-rocwmma` variant (`sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141`, matrix-multiply-tuned) is the more likely TG win and should be **A/B-benchmarked** before deciding which digest ships — re-verify at implementation time (kyuz0 re-pushes the rolling tag; pin the digest, never the bare tag).

### Expected Features

Six scoped features; full landscape in `.planning/research/FEATURES.md`. The unifying invariants gating all six: single static CGO-free binary, strictly-local/zero-telemetry, config-is-single-source-of-truth, byte-frozen `--json` goldens, offload-asserting (no false-green).

**Must have (the milestone core — P1):**
- **ROCM-ALT-01** — digest-pinned `rocm-6.4.4` selectable behind `BackendFor`; answers a shipped, measured v1.1 regression; lowest risk, proven seam.
- **DOCTOR-01** — one-shot health + remediation that *composes* preflight + status + residency proof + config-vs-disk drift; highest operability value per unit effort; 0/2/1 exit tiers; `--json` frozen.
- **BAK-01** — config + Open WebUI data backup/restore; the only protection for irreplaceable user state today; highest risk (see Pitfalls).

**Should have (builds on the core — P2):**
- **BENCH-03** — persist each `bench.Result` as a versioned saved report; `--compare` reads history and diffs pp/tg *separately*; pairs with ROCM-ALT-01 to *prove* the Δtg recovery.
- **USAGE-01** — cumulative prompt/generated token totals (from llama.cpp `*_tokens_total` counters) with reset detection, surfaced in `status`/dashboard; honesty-trickiest persistence surface.

**Sequence last (P2/P3):**
- **INSTALL-01** — guided TUI as a pure presentation layer over the same detect→recommend→preflight→install pipeline; most UI surface, the only new dependency, TTY/accessibility-gated. None of the other five depend on it.

**Explicit anti-features (do NOT build):** doctor auto-repair / diagnostic-bundle upload; backing up model weights or to a cloud target; scheduled-backup daemon; a single blended bench "score"; bench leaderboard upload; usage content-logging or a metrics-daemon/TSDB; usage/bench telemetry; a TUI-only (no-flag) install or a TUI that re-derives fit/preflight logic; auto-switching to rocm-6.4.4; unpinned/floating ROCm tags.

### Architecture Approach

The architecture is fixed; the question for each feature is *where it slots in without violating an invariant*. The answer is uniform: one new **pure** `internal/*` core per feature with decision logic, host I/O confined to `orchestrate` (or the command tier, as `uninstall.go`'s `podmanVolumeRm` already proves passes the seam gate), persisted state under XDG data dir, and any surfaced state evolved append-only. Full integration map: `.planning/research/ARCHITECTURE.md`.

**Major components (v1.2 additions):**
1. **`internal/doctor`** — pure: composes `preflight.RunWithResources` + `status.Run` + adds drift cross-checks; its OWN unconstrained golden (do NOT extend the byte-frozen `status.Report`).
2. **`internal/backup`** (pure manifest/verify) + **`orchestrate` volume_io seam** (`podman volume export/import`, fixed-arg) — preserves the single-impure-module invariant; restore is transactional (stop → import → restart → prove → rollback).
3. **`internal/benchstore`** — pure, frozen versioned `SavedReport` codec (golden-frozen on creation); `--compare` is read-only and distinct from live `--ab`; carries `VoidExhausted`/`Reason` honesty flags + full env fingerprint.
4. **`internal/usage`** — pure `Fold(prior, sample) -> Totals`; **the dashboard server's existing poll loop is the SOLE writer** of `usage.json` (mutex-guarded; the CLI is one-shot and only reads); folds from monotonic `_total` counters, not rate gauges.
5. **`internal/inference/backend_rocm_alt.go`** — the `rocm-6.4.4` image literal + args + markers, seam-locked; **`seam_test.go`'s image regex MUST be extended** (`rocm-7\.2\.4` → also `rocm-6\.4\.4`) in the same commit, or a future leak passes CI silently. Add the tag to `rocm-policy.json`.

### Critical Pitfalls

Top items from `.planning/research/PITFALLS.md` (12 pitfalls total, mapped to phases):

1. **BAK-01 — UID-mapping / SELinux mangle + torn SQLite (Pitfalls 1–2):** rootless Podman maps owners via subuid; raw host-path tar restores with meaningless ownership and breaks the `:Z` SELinux label, and overwriting a live DB corrupts it. *Avoid:* `podman volume export/import` (never host-path tar), recreate the volume via Quadlet, and order restore like `backendswap` — quiesce → swap → restart → prove → verbatim rollback on failure.
2. **BAK-01 — model-weight sweep + version skew (Pitfalls 3–4):** "back up everything" balloons to tens of GB (`villa-models.volume` is a bind volume of re-pullable weights) or silently drops them; a backup restored under a newer Open WebUI digest / stale ROCm floors breaks. *Avoid:* scope to config + Open WebUI data only, record model identity in the manifest (re-pull on restore), stamp version/digest/host-fingerprint, WARN-with-remediation on skew, restore config via `config.SaveVilla` + re-run preflight.
3. **DOCTOR-01 — false-green over CPU fallback (Pitfall 7):** an `is-active` + `/health 200` doctor that hides a silent CPU fallback betrays D-11. *Avoid:* compose the existing honest cores (preflight + status + `ResidencyProof`); confident CPU fallback = FAIL, unevaluable = WARN; three-state output with remediation; never re-type backend markers (seam gate).
4. **USAGE-01 — telemetry/leak + unbounded growth/write race (Pitfalls 5–6):** usage that phones home, logs content, binds off-loopback, grows forever, or is written by two processes. *Avoid:* counts-only, local, no new outbound (assert it); bounded/rolling aggregate with retention; **single writer = the dashboard poller** + atomic write; XDG 0600, loopback-only.
5. **BENCH-03 — broken golden contract + non-comparable runs (Pitfalls 8–9):** an unversioned saved-report shape, an in-place edit to a frozen `--json`, blended pp/tg, or a delta across mismatched model/quant/host. *Avoid:* `schema_version` from day one, golden-frozen, append-only evolution; keep pp/tg separate; persist the full `BenchSpec` + env fingerprint; comparability guard labels mismatches "not comparable."
6. **INSTALL-01 / ROCM-ALT-01 (Pitfalls 10–12):** TUI pulling CGO / becoming the only path / re-deriving fit logic; rocm-6.4.4 added unpinned, outside the seam, or bypassing floors. *Avoid:* CGO-free `huh`, TTY-gated fall-through to the flag path, TUI computes nothing (calls the cores); digest-pin inside `internal/inference`, extend the seam regex, gate via `rocm-policy.json`, opt-in + bench-proven only.

## Implications for Roadmap

All four researchers independently converged on the **same build order** — preserve it. It sequences seam-locked + composition features first (zero/trivial contract risk), then the two persistence features with their byte-frozen evolutions staggered, then the destructive backup, then the TUI capstone.

### Phase 1: ROCM-ALT-01 — `rocm-6.4.4` alternate backend
**Rationale:** Lowest risk, fully self-contained, no dependents; the `BackendFor`/`backendswap`/`bench --ab` machinery was built for exactly this. Lands the milestone's measured-regression motivation early.
**Delivers:** A digest-pinned `rocm-6.4.4` (and/or `-rocwmma`) backend selectable like any other, gated by the same `rocm-policy.json` floors, switched transactionally with residency proof.
**Addresses:** ROCM-ALT-01.
**Avoids:** Pitfall 12 — **extend `seam_test.go`'s image regex in the same commit** (the gate change IS the regression guard); digest-pin (re-verify at impl time); add the tag to `rocm-policy.json`; opt-in only, never auto-switched. Which digest ships (plain 6.4.4 vs `-rocwmma`) is a bench-decided choice — validate the Δtg recovery, don't assume it.

### Phase 2: DOCTOR-01 — `villa doctor`
**Rationale:** Composes already-shipped `preflight` + `status` + residency + `orchestrate` drift; no new persistence, no frozen-contract risk. Building it early surfaces faults the later features may introduce.
**Delivers:** A read-only health + remediation report with config-vs-disk drift detection, 0/2/1 exit tiers, and a `--json` array.
**Implements:** New pure `internal/doctor` core (its OWN golden) + thin `cmd/villa/doctor.go`.
**Avoids:** Pitfall 7 — offload-assert, never `is-active`-only; do not extend `status.Report` (Anti-Pattern 3).

### Phase 3: BENCH-03 — saved reports + `--compare`
**Rationale:** Builds on the shipped honest-A/B bench core; pairs with Phase 1 to *prove* the Δtg recovery. **Freeze the `benchstore` saved-report format FIRST**, then wire `--compare` + save — the on-disk format is a contract; locking it before any real reports are written prevents migrations.
**Delivers:** Versioned `SavedReport` persistence (JSONL under XDG), `bench --compare` reading history with per-metric pp/tg deltas, list/show.
**Uses:** Flat JSONL persistence (STACK decision); `internal/inference` image digest for run identity.
**Avoids:** Pitfalls 8–9 — `schema_version` + golden freeze; pp/tg never blended; full `BenchSpec` + env fingerprint + comparability guard; carry `VoidExhausted`/`Reason`.

### Phase 4: USAGE-01 — cumulative usage tracking
**Rationale:** The most invariant-sensitive surfacing (touches the byte-frozen `status.Report`). Sequence after BENCH-03 so only one byte-frozen evolution is in flight at a time.
**Delivers:** Cumulative prompt/generated token totals with counter-reset handling, persisted to `usage.json`, surfaced (append-only) in `status` + dashboard.
**Implements:** Pure `internal/usage` `Fold`; **the dashboard poll loop as the sole, mutex-guarded writer**; CLI reads only; fold from monotonic `_total` counters (confirm exact names against the live server).
**Avoids:** Pitfalls 5–6 — counts-only/no content, no new outbound, bounded retention, single-writer, loopback-only; `status` change is one append-only field above `SchemaVersion` + one schema bump + one golden re-freeze.

### Phase 5: BAK-01 — backup / restore
**Rationale:** Independent of the others but the highest host-I/O and destructive risk; sequence after the read-only/additive features so the safer surface is proven first.
**Delivers:** A self-describing local archive (config + Open WebUI volume + manifest) and a transactional, consent-gated restore.
**Implements:** Pure `internal/backup` (manifest/verify) + `orchestrate` `volume_io` seam (or cmd-tier fixed-arg podman, as `uninstall.go` proves passes the gate).
**Avoids:** Pitfalls 1–4 — `podman volume export/import`; quiesce → swap → restart → prove → rollback; **exclude model weights** (manifest identity, re-pull on restore); version/digest/host-fingerprint stamp + WARN on skew; 0600/0700 XDG, restore via `config.SaveVilla` + re-preflight.

### Phase 6: INSTALL-01 — guided TUI install (capstone)
**Rationale:** A front-end over the *whole* pipeline, ideally over the final command surface; introduces the milestone's only new dependency. Sequence last so it wraps a stable set of cores.
**Delivers:** A guided detect → recommend → confirm/adjust → preflight-gate → install flow that writes the same `config.toml` and runs the same install, with a TTY/accessible fall-through to the existing flag path.
**Uses:** `charmbracelet/huh` v1.0.0 (command-tier only; verify `CGO_ENABLED=0` build).
**Avoids:** Pitfalls 10–11 — pure presentation (no fit/preflight re-implementation; decisions only via the cores + `BackendFor`); flags stay first-class; CGO-free static build check in CI.

### Phase Ordering Rationale
- **Dependency-honoring:** ROCM-ALT-01 and DOCTOR-01 have zero/trivial contract risk and no dependents → first. BENCH-03's format must be frozen before USAGE-01's status surfacing so **only one byte-frozen evolution lands at a time**. BAK-01 (destructive, new impure surface) follows the safe additive features. INSTALL-01 is a capstone over the finished surface.
- **Reinforcement:** BENCH-03 ⇄ ROCM-ALT-01 — saved/`--compare` reports are exactly how the alt image *proves* it recovered Δtg −11.15.
- **Invariant protection:** the order keeps the two riskiest invariant interactions (byte-frozen golden evolution; the only-impure-module rule) isolated and sequential rather than concurrent.

### Research Flags

Phases likely needing deeper research during planning (`/gsd-plan-phase --research-phase`):
- **Phase 4 (USAGE-01):** confirm the exact llama.cpp `/metrics` cumulative counter names (`llamacpp:prompt_tokens_total` / `tokens_predicted_total`) against a live `llama-server`, and the counter-reset semantics on restart/backend-swap (MEDIUM-confidence names; HIGH-confidence pattern).
- **Phase 5 (BAK-01):** validate cross-host / post-`podman system reset` restore (UID-mapping + SELinux `:Z`) — the "looks done but isn't" case the round-trip test misses; external Podman volume mechanics are MEDIUM (WebSearch-verified, not Context7).

Phases with standard/well-documented patterns (skip research-phase):
- **Phase 1 (ROCM-ALT-01):** image verified, seam proven by v1.1; only the digest needs re-verification (a build step, not research).
- **Phase 2 (DOCTOR-01):** pure composition of shipped cores; the doctor pattern (wp-cli/brew) is HIGH-confidence.
- **Phase 3 (BENCH-03):** builds on the shipped bench core; JSONL persistence decision RESOLVED.
- **Phase 6 (INSTALL-01):** `huh` versions + dep graph verified against the module proxy; pattern is HIGH-confidence — main work is execution, not research.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | huh versions + transitive graph from the Go module proxy; rocm-6.4.4 + `-rocwmma` digests from the live Docker registry manifest; podman volume export/import from official docs; JSONL-vs-SQLite is a design call with HIGH confidence on the constraint analysis. |
| Features | MEDIUM-HIGH | doctor / Podman backup / Charm TUI patterns HIGH from official sources; llama.cpp counter *persistence* MEDIUM (inferred from standard Prometheus counter semantics, not an explicit llama.cpp doc statement). |
| Architecture | HIGH | Grounded directly in the v1.1 codebase (seam_test.go, status.go, bench.go, preflight.go, config.go, systemd.go, install.go, uninstall.go all read); integration points cite exact files. |
| Pitfalls | HIGH (this-system) / MEDIUM (external) | HIGH for this-system invariants (sourced from the codebase); MEDIUM for external rootless-Podman volume mechanics (WebSearch-verified, not Context7). |

**Overall confidence:** HIGH

### Gaps to Address
- **llama.cpp cumulative counter names (USAGE-01):** confirm `llamacpp:prompt_tokens_total` / `tokens_predicted_total` and reset behavior on the running server before wiring the accumulator. *Handle:* a 10-minute live `/metrics` check at the start of Phase 4; design the fold to degrade to typed-Unknown if a counter is absent.
- **Cross-host / post-reset restore (BAK-01):** the UID-mapping + SELinux `:Z` repair path (`podman unshare chown -R`) is MEDIUM-confidence external mechanics. *Handle:* explicit cross-host (or post-`podman system reset`) round-trip test in Phase 5 acceptance, not just same-host.
- **Which rocm-6.4.4 digest ships (ROCM-ALT-01):** plain `rocm-6.4.4` vs `-rocwmma` is a TG-perf question. *Handle:* `villa bench --ab` against rocm-7.2.4 + Vulkan during Phase 1; ship the digest the bench proves, consistent with the never-promise-an-unbenchmarked-speedup constraint.

## Sources

### Primary (HIGH confidence)
- VillaStraylight v1.1 source read directly — `internal/inference/{seam_test.go,backend.go,backend_rocm.go}`, `internal/{status,bench,preflight,metrics,config,orchestrate,dashboard}/…`, `cmd/villa/{install,uninstall,bench}.go` — binding invariants, integration points.
- `.planning/PROJECT.md` + `CLAUDE.md` — Key Decisions, v1.2 scope, architectural constraints / anti-patterns, the live Δtg −11.15 motivation.
- Go module proxy (`proxy.golang.org`) — authoritative versions/dep graph: huh v1.0.0 → bubbletea v1.3.6 / lipgloss v1.1.0 (NOT charm.land/v2).
- Docker registry manifest API (`registry-1.docker.io`) — full sha256 digests for rocm-6.4.4, rocm-6.4.4-rocwmma, rocm-7.2.4, vulkan-radv.
- Podman docs — `podman-volume-export(1)` / `podman-volume-import(1)` (tarball, STDOUT/STDIN, volume-must-pre-exist, local-driver-only, not over remote).
- Context7 `/charmbracelet/{huh,bubbletea,bubbles}` — capability summaries, accessible mode, business-logic-behind-flags pattern.
- wp-cli/doctor-command, llama.cpp Issue #19811 (`prompt_tokens_total` / `tokens_predicted_total` counters).

### Secondary (MEDIUM confidence)
- containers/podman discussions #23054 / issues #14411, #25442, #10669 + oneuptime/tutorialworks rootless-volume guides — rootless backup, UID-mapping/SELinux, `podman unshare chown` repair.
- glukhov.org "Monitor LLM Inference (2026)" — "Cumulative Tokens" Grafana panel over the two `_total` counters; `--metrics` flag.

### Tertiary (LOW confidence)
- None load-bearing; all key decisions corroborated by at least one HIGH-confidence source or the codebase itself.

---
*Research completed: 2026-06-07*
*Ready for roadmap: yes*
