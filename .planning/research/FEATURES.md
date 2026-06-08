# Feature Research

**Domain:** Operability features for a self-hosted local-AI control-plane CLI (`villa`) — single static Go binary orchestrating rootless Podman Quadlets (llama.cpp Vulkan/ROCm + Open WebUI) on AMD Strix Halo / Fedora. Strictly local, zero telemetry.
**Researched:** 2026-06-07
**Confidence:** MEDIUM-HIGH (CLI-doctor patterns, Podman volume backup, and Charm TUI ecosystem are HIGH from official sources; llama.cpp counter persistence is MEDIUM — inferred from standard Prometheus counter semantics rather than an explicit llama.cpp doc statement)

This file scopes the SIX v1.2 features only. Each feature is analyzed as table-stakes / differentiator / anti-feature, with complexity and the existing `internal/*` cores it must depend on. Overarching invariants that gate EVERY feature: **single static binary** (no CGO, no new runtime), **zero telemetry / strictly local** (nothing leaves the box, no outbound except image/model pulls), and **honesty-by-construction** (typed-Unknown over false-green; never blend pp/tg; never fabricate a 0).

---

## Per-Feature Landscape

### DOCTOR-01 — `villa doctor` (one-shot health + remediation)

The mature pattern (wp-cli `doctor`, `brew doctor`, `flutter doctor`, `gh ext doctor`): a doctor command runs a **series of named checks against a *running/installed* system**, each emitting `{name, status ∈ success|warning|error, message, remediation}`, suppresses passing noise by default, and exits non-zero if any check fails. The defining distinction from preflight: **preflight asks "is this host safe to install ON?" (pre-state); doctor asks "is what I installed actually healthy NOW?" (post-state)** — drift, faults, dead services, broken offload, telemetry leak.

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Re-run preflight checks against the live install | A doctor must re-assert the host invariants didn't drift (kernel/firmware updated, linger disabled, SELinux flipped) | LOW — reuse `internal/preflight.Run`/`RunWithResources` verbatim | `preflight` |
| Table stakes | Service-health roll-up (each Quadlet unit active? healthy? offload-resident?) | "Is my stack actually up and on the GPU?" is the core question | LOW-MED — fold the existing `status.Report` (Services, Overall, offload residency) | `status`, `inference` (residency proof), `orchestrate` |
| Table stakes | Per-check remediation string + overall exit-code contract | Doctor's value is *actionable* output, not just red/green | LOW — preflight `CheckResult` already carries `Remediation`+`Provenance` | `preflight` |
| Table stakes | Config-vs-disk drift detection (does rendered Quadlet match current config?) | A hand-edited unit or stale render is the #1 "it worked yesterday" cause | MED — needs a pure render + compare against on-disk units | `orchestrate` (Render/Reconcile), `config` |
| Differentiator | Offload-residency assertion as a *health* check (not just liveness) | Aligns with the project's signature honesty constraint — a silent CPU fallback is a FAIL, surfaced by doctor, not a false-green | MED — reuse `inference.ResidencyProof` seam | `inference`, `metrics` |
| Differentiator | Privacy-posture assertion (loopback-only binds, no-telemetry, no unexpected outbound) | Re-proves the strictly-local promise on the running stack — a security-conscious "daily-driver" check competitors lack | LOW — `status` already computes `LoopbackOnly`/`NoTelemetry` | `status` |
| Differentiator | `--json` machine-readable check array (frozen, schema-versioned) | Lets the dashboard/scripts consume doctor; matches the byte-frozen-contract convention | MED — new golden + schema version | (output contract) |
| Anti-feature | Auto-fix / auto-remediate ("doctor --repair" that mutates the stack) | Surface appeal: "just fix it." Problem: silent host mutation violates the deliberate-action posture and can mask root cause; a bad auto-fix on a daily-driver is worse than a clear diagnosis | Report + remediation string; let the user run the suggested `villa` verb (or at most an explicit opt-in `--fix` that only re-runs an *existing* idempotent verb like `villa up`, never novel mutation) |
| Anti-feature | Doctor that phones home / uploads a diagnostic bundle | Common in commercial tools | Breaks zero-telemetry hard | Local-only report; if a bundle is wanted, write it to a local file the user chooses to share |
| Anti-feature | Re-implementing checks already in preflight/status | Drift between two check sets | N/A | Compose the existing cores; doctor is a thin orchestrator, like `bench --ab` composes `backendswap` |

**Output contract / exit codes (recommended, mirrors preflight's existing 0/2/1 and the wp-cli/brew convention):**
- `0` = all checks PASS (or PASS+informational only)
- `2` = at least one WARN, no FAIL (degraded but usable — drift, ROCm-absent-informational, unevaluable signal)
- `1` = at least one FAIL (a service down, offload fell to CPU, loopback breached, render drift on a critical unit)
- Default human output suppresses PASS noise (show WARN/FAIL with remediation); `--all`/`-v` shows passes; `--json` emits the full frozen array.

---

### BAK-01 — Backup / restore (config + Open WebUI data volume)

The mature rootless-Podman pattern: **`podman volume export <vol> | tar` for data, plus a copy of declarative config**, restored by `podman volume import` (or a tar-into-volume from the user namespace). Best practice from the Podman docs/discussions: **stop the writing container before export for consistency**, verify with a checksum, and treat the archive as portable. Open WebUI's `open-webui` volume holds the SQLite DB (chats, users, settings, prompts) — this is the irreplaceable state.

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Back up `config.toml` | The single source of truth; trivially small, must be in every backup | LOW | `config` |
| Table stakes | Back up the Open WebUI data volume (chats, settings, users) | The only irreplaceable user state in the stack | MED — `podman volume export` behind an orchestrate seam; quiesce the container first | `orchestrate` (new volume-export seam), `status` (to know what's running) |
| Table stakes | Restore with an explicit overwrite guard (refuse to clobber a live install silently) | Restore is destructive; a daily-driver must not lose current chats to a fat-finger | MED — fail-closed unless `--force`/confirmation; ideally back up current state first | `orchestrate`, `config` |
| Table stakes | Single self-describing archive with a manifest (versions, date, contents, checksums) | Recovery/migration needs provenance; checksum verify on restore | MED — manifest = villa version, schema versions, image digests, volume name, sha256 | `config`, `inference` (image digests), build version |
| Differentiator | Quiesce-then-snapshot (stop Open WebUI → export → restart) for a consistent SQLite snapshot | Avoids torn-DB backups — the honest, reliable path; transactional, mirrors `backendswap`'s capture→mutate discipline | MED — reuse the orchestrate stop/start seam; restore current-state on failure | `orchestrate`, `backendswap` (pattern, not code) |
| Differentiator | Version/compat check on restore (warn if archive's villa/Open-WebUI image digest ≠ current) | Restoring an old Open WebUI DB into a newer image can need a migration; warn, don't silently break | LOW-MED — compare manifest vs current; WARN not BLOCK | manifest + `inference`/`config` |
| Anti-feature | Backing up model weights in the archive | Surface appeal: "back up everything." Problem: tens-to-hundreds of GB; they're re-pullable, content-addressed, and bloat every backup. Migration intent is *state*, not *weights* | Back up config + Open WebUI volume only; record the model id/quant in the manifest so restore can re-pull weights via the existing `villa model pull` |
| Anti-feature | Cloud/remote backup target (S3, rsync to a server) | Common in backup tooling | Breaks strictly-local posture; adds network/credentials surface | Write a local archive file; the user moves it with their own tools (scp/rsync) — villa never opens an outbound connection for backup |
| Anti-feature | Continuous/scheduled automatic backups (cron daemon) | "Set and forget" | Adds a background daemon, scope creep beyond a control-plane verb, silent disk growth | Manual `villa backup` verb the user (or their own systemd timer) invokes; document the timer recipe |
| Anti-feature | Backing up the running container itself (`podman checkpoint`/commit) | Seems thorough | Containers are disposable + regenerated from config; checkpoint is fragile across image digests | Back up the *volume* (state) only; the container is re-rendered from config |

---

### BENCH-03 — `villa bench --compare` + persisted/saved reports

Building on the Phase-9 honest A/B core. Mature pattern (hyperfine `--export-json`, `go test -bench` + benchstat, criterion baselines): **persist each run as a self-describing record keyed by its parameters, then compare records by identity (model + quant + ctx + backend + spec) over time.** The hard constraint here is the existing one: **pp and tg are NEVER blended** — a saved report and any comparison must keep them separate end-to-end.

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Persist a bench run to a local report file | Without persistence there is nothing to compare; today `Result` is ephemeral | LOW-MED — serialize the existing `bench.Result` (+ spec + identity + timestamp + host/backend image) to a versioned JSON under XDG state | `bench`, `config` (XDG path), `inference` (image digest) |
| Table stakes | Stable run identity (model + quant + ctx + backend + image digest + spec hash) | Comparisons are only honest between like runs; identity prevents apples-to-oranges | LOW — derive a key from the spec + resolved backend | `bench`, `inference`, `config` |
| Table stakes | `--compare` of two/more saved reports (per-metric delta, pp and tg separately) | The feature's whole point — track regressions/improvements over time | MED — reuse the existing `ABResult` delta math (B−A per metric); never a blended figure | `bench` (`statsOf`, delta logic) |
| Table stakes | List/show saved reports | Navigability — pick what to compare | LOW | `bench`, `config` |
| Differentiator | Void-aware comparison (carry Kept/Void + VoidExhausted into the saved record; refuse a confident delta below MinResident) | Preserves the Phase-9 honesty gate in the historical view — a low-confidence saved run is flagged, not silently authoritative | LOW — fields already exist on `Result`/`Stats`; persist them | `bench` |
| Differentiator | Identity-mismatch guard on `--compare` (warn/refuse comparing different model/quant/ctx) | Stops misleading "regression" reports from comparing unlike runs | LOW — compare identity keys; WARN/BLOCK on mismatch | `bench` |
| Differentiator | Median + sample-stddev carried per metric in the saved report | Lets a comparison say "within noise" vs "real delta" honestly | LOW — `Stats` already has MedianPP/StddevPP/MedianTG/StddevTG | `bench` |
| Anti-feature | A single blended "score" / tok-s number per run for easy ranking | Surface appeal: "which is fastest?" one number. Problem: blending pp and tg is exactly the dishonesty the Phase-9 core was built to avoid (ROCm wins pp, loses tg) | Keep pp and tg separate everywhere; a comparison shows both deltas and lets the user judge by workload |
| Anti-feature | Uploading benchmark results to a shared leaderboard / public DB | Common in benchmark tooling | Breaks zero-telemetry; cross-host comparison is meaningless given hardware variance | Local report files only; the user shares manually if desired |
| Anti-feature | Auto-tuning / auto-selecting a backend from saved benches | "Pick the winner for me" | Violates the v1.1 decision that recommend *advises, never auto-switches*; pp/tg tradeoff is workload-dependent | Surface the comparison; the human runs `villa backend set` deliberately |

**Saved report contents (recommended):** schema version; timestamp; villa version; host fingerprint (model id, GPU gfx, memory envelope); identity key (model+quant+ctx+backend+image-digest+spec-hash); the full `BenchSpec`; per-side `Stats` (MedianPP/StddevPP/MedianTG/StddevTG, Kept, Void); `VoidExhausted`+Reason; for `--ab`, the `ABResult` (From/To, DeltaPP, DeltaTG). Identification for `--compare`: by identity key first (like-for-like), then ordered by timestamp.

---

### USAGE-01 — Cumulative token / throughput usage tracking over time

The critical finding: **llama.cpp already exposes cumulative counters `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total`** (Prometheus counters via `--metrics`). But the existing `internal/metrics` core deliberately treats `/metrics` gauges as **last-window snapshots, NOT a live counter** — and Prometheus counters are **in-memory and reset to zero on every server restart** (standard counter semantics; the scraper normally handles this with `rate()`). So an honest cumulative-over-time metric requires villa to **scrape the counter periodically, detect resets (value dropped → add the pre-reset total), and persist the running total locally** — there is no telemetry, just local accumulation of a metric the inference server already produces.

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Cumulative prompt-tokens-in + generated-tokens-out total | The minimal honest "how much have I used this box?" — both counters already exist server-side | MED — scrape `*_total` counters; needs a small persisted accumulator with reset detection | `metrics` (extend to read the `_total` counters), `config`/state (XDG) |
| Table stakes | Surface totals in `villa status` and the dashboard (not just live tok/s) | The requirement is explicitly "over time, not just live" | LOW-MED — append fields to the frozen `status.Report` (append-only + schema bump) | `status`, `dashboard`, `metrics` |
| Table stakes | Counter-reset handling (server restart resets the in-memory counter to 0) | Without it, cumulative totals silently undercount after every restart/backend-swap → dishonest | MED — store last-seen counter; on a decrease, fold the prior peak into the persisted total | `metrics`, state |
| Differentiator | Per-model attribution of cumulative tokens | "Which model have I used most?" — useful for a daily-driver picking what to keep loaded | MED — key the accumulator by the loaded model id at scrape time | `metrics`, `status` (active model), state |
| Differentiator | Honest throughput history (median pp/tg over a window) distinct from the live snapshot | Trend without fabricating precision; reuses the pp/tg-separate discipline | MED — sample + store windowed medians; never blend | `metrics`, state |
| Differentiator | Typed-Unknown when `/metrics` is absent/unscrapable (omit, never fabricate a 0) | Matches the existing `*float64 + omitempty` no-false-green pattern in `status.Report.GenTokensPerSec` | LOW — same omitempty discipline | `status`, `metrics` |
| Anti-feature | Logging prompt/response *content* or per-request records to build "usage" | Surface appeal: "usage analytics." Problem: storing chat content is a privacy regression and risks prompt leakage — the metrics core already deliberately excludes prompt/params from `/slots` | Aggregate token *counts* only; never persist prompt or completion text |
| Anti-feature | Sending usage data anywhere / analytics endpoint | This is literally telemetry | Breaks the core zero-telemetry promise | Local persisted aggregate only; surfaced in status/dashboard |
| Anti-feature | A high-frequency background metrics daemon / time-series DB | "Real observability" | Scope creep, disk growth, a new long-lived process beyond the dashboard scrape; an embedded TSDB is a heavy dependency | Coarse periodic scrape (the dashboard already polls); persist a small rolling aggregate, not a full time-series. Document the existing Prometheus `--metrics` endpoint for users who want real Grafana |
| Anti-feature | Reporting a single blended "tokens/sec" lifetime average | Easy headline number | Blends pp and tg; conflates idle and active time → misleading | Keep counts (in/out) and rates (pp/tg) separate; report totals for counts, windowed medians for rates |

---

### INSTALL-01 — Guided TUI install flow

Mature pattern (the Charm ecosystem — `bubbletea`/`huh`/`gum`, also `gh`'s interactive prompts): **an interactive front-end over the SAME business logic that the flags drive** — "business logic in separate packages, callable non-interactively via flags." `huh` is **pure Go (no CGO)** → preserves the single-static-binary constraint, and has a screen-reader-accessible mode. The TUI is a *thin presentation layer*; it must call detect → recommend → preflight → install exactly as the flag path does, and produce the same `config.toml`.

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Guided sequence: detect → show recommendation → confirm/adjust → preflight gate → install | First-run users need a hand-held path; this is the "just works after install" bar made interactive | MED-HIGH — orchestrate the existing cores behind prompts; no new decision logic | `detect`, `recommend`, `preflight`, `orchestrate`, `config` |
| Table stakes | Show the preflight result and require acknowledgement of WARN/BLOCK before proceeding | Preflight is non-optional; the TUI must not let a user blow past a BLOCK | LOW-MED — render `[]CheckResult`; map tiers to gate behavior (same 0/2/1 semantics) | `preflight` |
| Table stakes | Model/quant/ctx selection seeded by the recommendation (accept default or pick within the memory envelope) | The hardware-aware pick is the product's core value; let the user confirm/override safely | MED — present `recommend.Pick` output; constrain choices to fit math | `recommend`, `catalog`, `detect` |
| Table stakes | Write the same `config.toml` the flag path writes, then run the same install | TUI must not be a second, divergent code path | LOW — call the existing `SaveVilla` + install flow | `config`, `orchestrate` |
| Differentiator | Backend choice surfaced with honest ROCm advice (Vulkan default; ROCm opt-in, "worth trying / verify with bench") | Carries the v1.1 honesty posture into the guided flow; ROCm stays opt-in, never auto-selected | LOW — reuse the `recommend` ROCm advice + `rocm_readiness` | `recommend`, `detect`, `inference` |
| Differentiator | Live progress for long steps (image pull, model download) with cancel | Long pulls without feedback feel hung; a daily-driver install should feel responsive | MED — stream progress from `download`/`orchestrate` into the TUI | `download`, `orchestrate` |
| Differentiator | Accessible / non-TTY fallback (if not a TTY, drop to the flag/prompt path) | Don't strand piped/CI/headless invocations; `huh` has an accessible mode | LOW-MED — TTY detection → fall back to existing flag-driven `install` | (presentation) |
| Anti-feature | TUI becomes the ONLY way to install (no flag path) | "Polished UX" temptation | Breaks scriptability, CI, headless servers; violates the flag-driven contract | TUI is additive; every flag path keeps working unchanged. TUI calls the same cores |
| Anti-feature | TUI embeds its own decision logic (re-deriving recommendations/fit math) | Convenience during build | Divergence from the canonical `recommend`/`preflight` cores → two sources of truth | Pure-presentation only; all decisions come from the existing pure cores |
| Anti-feature | A heavy CGO/native TUI toolkit (ncurses bindings, etc.) | "Richer UI" | Breaks the single static binary (CGO) | Use the pure-Go Charm stack (`huh`/`bubbletea`) — static, no CGO |
| Anti-feature | Persisting TUI-only state/preferences outside config.toml | Seems handy | A second state store undermines "config is the single source of truth" | Everything the TUI collects lands in `config.toml` |

---

### ROCM-ALT-01 — `rocm-6.4.4` alternate image for TG-heavy models

Direct response to the v1.1 live finding **Δtg −11.15** (the pinned `rocm-7.2.4` regressed token-generation vs Vulkan). This is the **lowest-risk feature**: the entire backend architecture already supports it — a second ROCm image is a new digest-pinned `Backend` behind the single `BackendFor` resolver, selectable like any backend, with no caller changes (the seam grep-gate already enforces literals stay in `internal/inference`).

| Category | Feature | Why | Complexity | Core dependency |
|----------|---------|-----|------------|-----------------|
| Table stakes | Digest-pinned `rocm-6.4.4` image as a selectable backend variant | The feature itself; must be pinned by sha256 like the existing ROCm/Vulkan images | LOW — new `Backend` impl (or parameterized image) behind `BackendFor`; literals stay seam-locked | `inference` (`BackendFor`, Backend iface) |
| Table stakes | Same preflight/policy gating as `rocm-7.2.4` (image-tag allow/deny, kernel/firmware floors, HSA override) | A second ROCm image must obey the same `rocm-policy.json` gate; an un-allow-listed tag must refuse | LOW-MED — add tag to `rocm-policy.json`; floors/HSA already apply | `preflight` (floors + policy), `detect` (rocm_readiness) |
| Table stakes | Same transactional switch + residency proof as the existing ROCm backend | Bring-up must be a no-op on failure (the "just works" bar); offload-asserting, no false-green | LOW — already provided by `backendswap` + `inference.ResidencyProof`; reuse verbatim | `backendswap`, `inference` |
| Differentiator | Honest A/B that proves the TG improvement (rocm-6.4.4 vs rocm-7.2.4 vs vulkan) | Closes the loop on the Δtg −11.15 motivation — *prove* the alt image helps tg, don't assume | LOW-MED — the bench `--ab`/`--compare` core already does this; just another backend identity | `bench`, `backendswap` |
| Differentiator | Backend-aware recommend advice mentions the TG-tuned option for tg-heavy fit | Helps the user discover the alt image without auto-switching | LOW — extend the existing ROCm advice; advise, never reassign | `recommend` |
| Anti-feature | Auto-switching to rocm-6.4.4 when a "tg-heavy" model is detected | "Smart default" | Violates the ROCm-is-opt-in / advise-never-switch decision; tg-heaviness is workload-dependent, not detectable at install | Recommend *advises*; the user runs `villa backend set` and confirms with `villa bench` |
| Anti-feature | Tracking unpinned `rocm-6.4.4` (floating tag) or a nightly | Convenience | The v1.1 decision pins stable and refuses nightlies (the 64 GB allocation-cap bug); floating tags break reproducibility | Digest-pin; add to the policy allow-list explicitly |
| Anti-feature | Proliferating many ROCm image variants "just in case" | "More options" | Each image is a maintenance/test surface; dilutes the curated, proven set | Ship exactly the two justified ROCm images (7.2.4 pp-strong, 6.4.4 tg-tuned) + Vulkan default; add more only with bench evidence |

---

## Feature Dependencies

```
DOCTOR-01
    ├──reuses──> preflight.Run            (host-invariant re-check)
    ├──reuses──> status.Report            (service health, loopback, no-telemetry)
    ├──reuses──> inference.ResidencyProof (offload-asserting health, not liveness)
    └──reuses──> orchestrate Render/Reconcile (config-vs-disk drift)

BAK-01
    ├──reads───> config (config.toml is part of the archive + manifest)
    ├──needs───> orchestrate (NEW volume export/import + quiesce seam)
    └──reads───> inference image digests + version (manifest / restore compat warn)

BENCH-03
    ├──builds-on──> bench.Result / ABResult / Stats   (persist + delta math)
    ├──reads──────> inference (image digest → run identity)
    └──reads──────> config (XDG state dir for saved reports)

USAGE-01
    ├──extends────> metrics (read llamacpp:*_tokens_total counters)
    ├──needs──────> persisted accumulator (reset detection)  [NEW small state]
    └──surfaces-in> status.Report (append-only) ──> dashboard

INSTALL-01  (pure presentation over existing cores — adds NO decision logic)
    └──orchestrates──> detect ─> recommend ─> preflight ─> orchestrate/install ─> config
                       (same path the flags drive; huh = pure-Go TUI layer)

ROCM-ALT-01
    ├──slots-into──> inference.BackendFor   (new digest-pinned Backend, seam-locked)
    ├──gated-by────> preflight rocm-policy.json + floors (add allow-listed tag)
    ├──switched-by─> backendswap (transactional, verbatim rollback) — reused as-is
    └──proven-by───> bench --ab / BENCH-03  (prove the Δtg recovery)
```

### Dependency Notes

- **DOCTOR-01 is a thin composer, not new logic** — like `bench --ab` composes `backendswap`, doctor composes `preflight` + `status` + `inference` residency + `orchestrate` drift. Building it before/with the others lowers risk; it surfaces faults the other features may introduce.
- **ROCM-ALT-01 is the cheapest and most architecturally-proven** — the `BackendFor` seam + `backendswap` + `bench` were *built* to absorb a new backend with zero caller changes (the v1.1 ROCm backend was the first exercise of that seam). Its only new surface is one policy-allow-list entry and one `Backend` impl.
- **BENCH-03 ⇄ ROCM-ALT-01 reinforce each other** — BENCH-03's saved/`--compare` reports are exactly how ROCM-ALT-01 *proves* it recovered the Δtg −11.15. Sequence ROCM-ALT-01 to land with, or just after, BENCH-03.
- **USAGE-01 must extend `metrics` honestly** — `internal/metrics` currently reads *gauges as snapshots*; USAGE-01 adds reading the `_total` *counters* + a persisted accumulator with reset detection. Both touch the frozen `status.Report` → strictly append-only + schema bump (the established golden-refreeze discipline).
- **INSTALL-01 conflicts with nothing but must add no decision logic** — it is presentation only; the conflict to guard against is *divergence* from `recommend`/`preflight`. It also adds the only new third-party dependency of the milestone (`huh`/`bubbletea`, pure-Go) — verify CGO-free before committing.
- **BAK-01 introduces the milestone's only new impure surface** — `podman volume export/import` must live behind an `orchestrate` seam (orchestrate is the *only* intentionally-impure module). Restore must follow the `backendswap` capture→prove discipline: back up current state before clobbering.

---

## MVP Definition (for the v1.2 milestone)

### Launch With (the milestone's core)

- [ ] **ROCM-ALT-01** — lowest risk, directly answers a shipped-and-measured v1.1 regression; the seam is proven.
- [ ] **DOCTOR-01** — highest operability value per unit of effort; pure composition of existing cores; the "operable daily-driver" thesis hinges on self-diagnosis.
- [ ] **BAK-01** — recoverability is the other half of "operable daily-driver"; the irreplaceable Open WebUI state has no protection today.

### Add After Core (same milestone, builds on the above)

- [ ] **BENCH-03** — needs the bench core (have it) + a persistence format; pairs with ROCM-ALT-01 to prove the tg recovery.
- [ ] **USAGE-01** — valuable but the trickiest honesty surface (counter resets, no-fabrication); land after the cheaper wins, with care on the frozen `status.Report`.

### Highest-Effort / Sequence Last

- [ ] **INSTALL-01** — most UI surface, the only new dependency, and gated on TTY/accessibility fallbacks. Highest polish-to-risk ratio; sequence last so it wraps a stable set of cores. (None of the other five depend on it.)

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| ROCM-ALT-01 | HIGH (answers a measured regression) | LOW | P1 |
| DOCTOR-01 | HIGH (core operability thesis) | LOW-MED (composition) | P1 |
| BAK-01 | HIGH (irreplaceable state, none today) | MED | P1 |
| BENCH-03 | MED-HIGH (regression tracking) | MED | P2 |
| USAGE-01 | MED (nice history, honesty-tricky) | MED | P2 |
| INSTALL-01 | MED (first-run polish; experts use flags) | MED-HIGH (UI + new dep) | P2/P3 |

## Competitor / Prior-Art Feature Analysis

| Feature | Prior art | Their approach | Villa's approach (distinct angle) |
|---------|-----------|----------------|-----------------------------------|
| doctor | wp-cli `doctor`, `brew/flutter doctor` | Named checks, success/warn/error, exit-1 on fail, suppress passes | Same contract + **offload-residency & privacy-posture as health checks**, 0/2/1 tiers, `--json` frozen, **no auto-fix/no-bundle-upload** |
| backup/restore | Podman `volume export/import`, Docker-volume tar pattern | tar a volume, quiesce, checksum, manual transfer | Same + **manifest with version/digest compat + overwrite guard**; **weights excluded** (re-pullable); **local-only, never outbound** |
| bench history | hyperfine `--export-json`, benchstat, criterion baselines | persist runs, compare by params, report median+stddev | Same + **pp/tg never blended**, **void-aware/MinResident-gated** saved runs, identity-mismatch guard |
| usage tracking | Grafana over llama.cpp `--metrics`, OpenWebUI usage | Prometheus counters → TSDB → dashboards | **Local accumulator over the same `_total` counters, reset-aware, no TSDB, no content logging, no telemetry**; surfaced in status/dashboard |
| guided install | `gh` prompts, Charm `gum`/`huh` wizards, many installers | TUI over the same logic, flag path retained | Pure-Go `huh` TUI **strictly composing detect→recommend→preflight→install**; flags stay first-class; TTY fallback |
| ROCm image variant | kyuz0 amd-strix-halo-toolboxes (multiple ROCm tags) | publish many tags | **Curated, digest-pinned, policy-allow-listed** pair (7.2.4 pp / 6.4.4 tg) + Vulkan default; **bench-proven, never auto-switched** |

## Sources

- [wp-cli/doctor-command](https://github.com/wp-cli/doctor-command) — doctor check model (name/status/message), exit-1-on-fail, suppress passes (HIGH)
- [Automating WordPress Health Checks with WP-CLI doctor](https://guides.wp-bullet.com/automating-wordpress-health-checks-with-wp-cli-doctor-command/) — remediation + restart-when-unhealthy patterns (MED)
- [llama.cpp Issue #19811 — Prometheus metrics need improvement](https://github.com/ggml-org/llama.cpp/issues/19811) — `prompt_tokens_total` / `tokens_predicted_total` counters, naming, per-model labeling gaps (HIGH for counter existence)
- [Monitor LLM Inference (2026): Prometheus & Grafana for llama.cpp](https://www.glukhov.org/observability/monitoring-llm-inference-prometheus-grafana/) — "Cumulative Tokens" panel uses the two `_total` counters; `--metrics` flag (MED)
- [How to backup Volumes using command line — containers/podman #23054](https://github.com/containers/podman/discussions/23054) — `podman volume export`/tar backup pattern (HIGH)
- [How to Migrate Podman Volumes Between Hosts](https://oneuptime.com/blog/post/2026-03-17-migrate-podman-volumes-between-hosts/view) — quiesce, checksum-verify, rootless tar export/import (HIGH)
- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) and [charmbracelet/huh](https://github.com/charmbracelet/huh) — pure-Go TUI/form stack, accessible mode, "business logic in separate packages, callable via flags" (HIGH)
- [Rapidly building interactive CLIs in Go with Bubbletea (Inngest)](https://www.inngest.com/blog/interactive-clis-with-bubbletea) — interactive-over-same-logic + flag-path pattern (MED)
- Existing codebase cores read directly: `internal/preflight/preflight.go`, `internal/status/status.go`, `internal/bench/bench.go`, `internal/metrics/llamacpp.go`, `internal/config`, `internal/inference` (HIGH — primary source)

---
*Feature research for: VillaStraylight v1.2 Operability (DOCTOR-01, BAK-01, BENCH-03, USAGE-01, INSTALL-01, ROCM-ALT-01)*
*Researched: 2026-06-07*
