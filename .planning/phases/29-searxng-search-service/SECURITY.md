---
phase: 29
slug: searxng-search-service
status: secured
threats_open: 0
threats_closed: 14
asvs_level: 1
created: 2026-06-18
---

# Phase 29 — SearXNG Search Service: Security Audit

**Audited:** 2026-06-18
**Mode:** register_authored_at_plan_time (3 PLAN files carry parseable `<threat_model>` blocks)
**ASVS Level:** 1 | **block_on:** high
**Result:** SECURED — 14/14 threats CLOSED (10 of them blocking-high)

Implementation files are read-only in this audit. Every mitigation below was verified by
reading the cited file/line, not by accepting documentation or intent. The 702-test
affected suite (`internal/orchestrate`, `internal/config`, `cmd/villa`, `internal/inference`)
is green, and a live on-hardware UAT confirmed T-29-04 / T-29-11 behaviorally.

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-29-01 | Information Disclosure (engine set) | mitigate (high) | CLOSED | `internal/orchestrate/searxng.go:95-100` single-source `searxngEngines` slice = `[duckduckgo, brave, wikipedia, wikidata]` (4 hosts). Template `searxng-settings.yml.tmpl:5-10` renders `use_default_settings.engines.keep_only` ranging only over `.Engines`; no full-default block. Golden `searxng-settings.yml.golden:5-11` byte-freezes the exact 4-engine keep_only. |
| T-29-02 | Information Disclosure (secret_key) | mitigate (high) | CLOSED | Secret from `crypto/rand`: `villaconfig.go:14,353-359` (`rand.Read` of 32 bytes, hex-encoded; no `math/rand`). Reaches container via `EnvironmentFile=` only: `searxng.container.tmpl:10` = `EnvironmentFile={{.SecretEnvFile}}`, golden line 10 = `EnvironmentFile=%h/.config/villa/searxng/searxng.env` — NO inline `Environment=SEARXNG_SECRET=` literal. `buildSearxngView` (searxng.go:128-136) does not thread the secret value; render.go:217-219 never reads it. settings.yml renders `secret_key: ""` (tmpl:12, golden:13). `marshalVilla` zeroes `SearxngSecret` on web-search-off save (villaconfig.go:339-343). |
| T-29-03 | Tampering (image literal) | mitigate | CLOSED | Digest-pinned manifest `const searxngImage = "ghcr.io/searxng/searxng@sha256:ed29454…"` (searxng.go:40), only accessor `SearXNGImage()` (searxng.go:45). Literal appears in source ONLY in searxng.go (grep confirmed; golden testdata is a frozen fixture, not source). Allowlisted in `seam_test.go:134` (`rel == "orchestrate/searxng.go"`) in the same commit; `TestSeamGrepGate` green. |
| T-29-04 | Elevation / scope creep (render bind) | mitigate (high) | CLOSED | `searxng.container.tmpl` has NO `Publish`/`-p`/`PublishPort` line (grep empty); only `Network=villa.network` (golden:9). `normalizeVilla` (villaconfig.go:243-248) fills `SearxngAddr` only with the container-DNS default `villa-searxng`, never a routable bind. Live UAT: `podman inspect` showed `PortBindings={}`. |
| T-29-05 | Tampering / privacy (off-render drift) | mitigate | CLOSED | `render.go:222` gates the append on `in.Cfg.WebSearchEnabled`, strictly after the memory branch, never mutating the shared `units` slice (lines 157-163). `TestRenderByteIdenticalWhenWebSearchOff` proves the off-render is the unchanged 5-unit baseline; 13 existing goldens untouched. |
| T-29-06 | Tampering (settings write path) | mitigate (high) | CLOSED | `searxng_settings_write.go:107` calls `assertInsideDir(target, dir)` before any write, inside the shared `writeSearxngFile` both writers call (lines 102-111). `TestWriteSearxngTraversalRefused` green. |
| T-29-07 | Tampering (partial/observable write) | mitigate | CLOSED | `atomicWriteMode` (searxng_settings_write.go:116-148): temp → Write → Sync → Close → `os.Rename` → best-effort dir-fsync, with `os.Remove(tmp)` on every error path (no `.tmp` remnant). Atomicity + idempotency tests green. |
| T-29-08 | Information Disclosure (settings file mode) | mitigate | CLOSED | `const searxngSettingsFileMode = 0o600` (own const, distinct from `unitFileMode` 0644), `searxngSettingsDirMode = 0o700` (searxng_settings_write.go:34-38); applied via `MkdirAll(dir, 0700)` + `atomicWriteMode(..., 0600)`. `TestWriteSearxngFilesMode` asserts both files 0600 / dir 0700 and `searxngSettingsFileMode != unitFileMode`. |
| T-29-09 | scope / privacy (file lands in unit dir) | mitigate (high) | CLOSED | `searxngSettingsDir()` (searxng_settings_write.go:45-51) resolves `os.UserConfigDir()` + `villa/searxng` — never `~/.config/containers/systemd/`. Both live writers (lines 56-75) route through it. Live-resolver test rejects any `systemd/user` / `containers/systemd` path. |
| T-29-10 | Tampering / Injection (probe query) | mitigate (high) | CLOSED | `liveSearxngProof` (install_searxng.go:182-186) passes `q=` and `format=json` via fixed-arg `--data-urlencode` through `runProbeCurl`, which is `exec.CommandContext(ctx, "podman", args...)` with fixed args, no shell (install_memory.go:359). Query never concatenated into the URL. |
| T-29-11 | Spoofing / false-green (readiness) | mitigate (high) | CLOSED | `evalSearxngProof` (install_searxng.go:134-153) requires `hasAnswer()` (≥1 result OR `number_of_results>0`, lines 115-117); an all-empty 200 → `StatusFail` with remediation; unreachable → `StatusFail`. No `/healthz` / `/health` path (grep empty). Install fails closed on `StatusFail` (install.go:882-885). Live UAT printed "real format=json query returned 10 result(s)", exit 0. |
| T-29-12 | Tampering (probe helper image) | mitigate | CLOSED | `helperImage := orchestrate.EmbedImage()` (install_searxng.go:164) — seam accessor (memory.go:47), no re-typed literal; `TestSeamGrepGate` over cmd/villa green. Probe runs over `--network villa` with no host port (runProbeCurl, install_memory.go:353-356). |
| T-29-13 | Elevation / fail-open (start unseen unit) | mitigate | CLOSED | Start gated on `planHasUnit(plan, orchestrate.SearXNGContainerUnitName())` (install.go:788); absent → INTERNAL-ERROR remediation + `return exitBlocked` (lines 789-792), before `d.start(searxngServiceName)`. |
| T-29-14 | Information Disclosure (live secret env file) | mitigate (high) | CLOSED | `WriteSearxngSecretEnv` (searxng_settings_write.go:69-93) writes `searxng.env` at 0600 (dir 0700) via the shared 0600 writer; host path == `SearXNGSecretEnvFilePath()` (searxng.go:79-85), the exact `EnvironmentFile=` path the unit references. Error wraps only the filename, never the body (line 90). Install renders+writes it before start (install.go:821-825). |

## Cross-Plan Secret Contract (T-29-02 + T-29-14)

Verified end-to-end as a single chain: `GenerateSearxngSecret` (crypto/rand) → persisted
0600 in config.toml → `RenderSearxngSecretEnv` (single-source `SEARXNG_SECRET=<value>` body) →
`WriteSearxngSecretEnv` at 0600 to `SearXNGSecretEnvFilePath()` → unit `EnvironmentFile=` path
reference only. The secret value never appears in the 0644 unit, the 0644 settings.yml, or any
log. install.go generates-and-persists the secret once (lines 795-806) before rendering the env
file, then writes both artifacts before starting the service.

## Unregistered Flags

No SUMMARY.md `## Threat Flags` section was emitted by the executor for this phase. No new
attack surface appeared during implementation outside the registered threat IDs.

Note (informational, not a Phase 29 security gap): the code review (`29-REVIEW.md`) flagged
WR-01 — no opt-in path sets `WebSearchEnabled = true` yet (the enable verb is a later-phase
deliverable), so the SearXNG seams are currently dormant in production. This is a
completeness/forward-dependency observation, not a mitigation gap: every declared mitigation
is present and correct for the moment the gate flips on. WR-02 (the pinned image's entrypoint
must substitute `$SEARXNG_SECRET` into the empty `secret_key`) is a runtime behavior of the
upstream image, not a first-party mitigation in this register; it was confirmed by the live
on-hardware UAT.

## Accepted Risks

None declared for this phase. All 14 threats carry a `mitigate` disposition and all are CLOSED.
