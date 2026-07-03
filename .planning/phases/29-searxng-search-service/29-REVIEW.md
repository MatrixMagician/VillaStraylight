---
phase: 29-searxng-search-service
reviewed: 2026-06-18T00:00:00Z
depth: deep
files_reviewed: 14
files_reviewed_list:
  - cmd/villa/install.go
  - cmd/villa/install_searxng.go
  - cmd/villa/install_searxng_test.go
  - cmd/villa/install_test.go
  - internal/config/villaconfig.go
  - internal/config/villaconfig_test.go
  - internal/inference/seam_test.go
  - internal/orchestrate/quadlet/searxng-settings.yml.tmpl
  - internal/orchestrate/quadlet/searxng.container.tmpl
  - internal/orchestrate/render.go
  - internal/orchestrate/searxng.go
  - internal/orchestrate/searxng_settings_write.go
  - internal/orchestrate/searxng_settings_write_test.go
  - internal/orchestrate/searxng_test.go
findings:
  critical: 0
  warning: 5
  info: 4
  total: 9
status: issues_found
---

# Phase 29: Code Review Report

**Reviewed:** 2026-06-18
**Depth:** deep (cross-file call-chain analysis)
**Files Reviewed:** 14
**Status:** issues_found

## Summary

Phase 29 adds a SearXNG web-search managed-service render path that mirrors the v1.3
Qdrant/embed `memory.go` precedent. The implementation is unusually disciplined: every
hard invariant called out in the brief is satisfied and verified end-to-end.

**Invariants verified PASS (no findings):**

- **Secret handling.** The `secret_key`/`$SEARXNG_SECRET` never lands in a 0644 file.
  `render.go` does NOT thread the secret into the unit view (`buildSearxngView` omits it);
  the unit references it only via `EnvironmentFile=%h/.config/villa/searxng/searxng.env`
  (golden confirmed). `settings.yml` renders `secret_key: ""`. The secret reaches the
  container exclusively through the 0600 env file (`searxng_settings_write.go`
  `searxngSettingsFileMode = 0o600`, asserted distinct from `unitFileMode` 0644 by
  `TestWriteSearxngFilesMode`). `marshalVilla` zeroes the secret on a web-search-OFF save,
  and `TestWebSearchSaveOmitsKeysWhenDisabled` proves the literal `deadbeef…` never appears
  in the off config. Error wrappers in the writers wrap only the filename, never the body.
- **Crypto.** `GenerateSearxngSecret` uses `crypto/rand` (32 bytes → 64 hex chars); a
  source-level guard test forbids `math/rand`.
- **Seam-lock.** `internal/inference/seam_test.go` extends `isSeam` to allow
  `orchestrate/searxng.go` for the `ghcr.io/searxng` digest literal, in the same commit as
  the const (mirrors the `orchestrate/memory.go` precedent). The cmd-tier walk still gates
  `cmd/villa`; `liveSearxngProof` sources its helper image from `orchestrate.EmbedImage()`,
  so no image literal leaks into `cmd/villa`.
- **orchestrate purity.** `render.go` stays pure; the settings/secret writers live in their
  own `searxng_settings_write.go` and use `assertInsideDir` + `atomicWriteMode`
  (temp→fsync→rename→dir-fsync, temp removed on every error path). Traversal refused for both
  writers (`TestWriteSearxngTraversalRefused`).
- **No shell interpolation.** `liveSearxngProof` issues fixed-arg `--data-urlencode q=…` and
  `--data-urlencode format=json` via `runProbeCurl` → `exec.CommandContext("podman", …)`; the
  query string is never concatenated into the URL.
- **Readiness = real signal.** `evalSearxngProof` parses real `results[]`/`number_of_results`
  and FAILs an all-empty 200 (`hasAnswer()`); there is no health-200 acceptance path. Bounded
  cold-start retry (3) with context-cancel-aware delay; never infinite.
- **Byte-frozen goldens.** `TestRenderByteIdenticalWhenWebSearchOff` proves the off-render is
  exactly 5 units and the searxng unit is strictly appended last when on.

The findings below are robustness/maintainability gaps and one milestone-completeness
concern — none compromise the secret/seam/purity invariants.

## Warnings

### WR-01: No opt-in path enables web search — the entire SearXNG stack is unreachable in production

**File:** `cmd/villa/install.go:483-485`, `cmd/villa/install_searxng.go:42-48`
**Issue:** Nothing in the codebase ever assigns `WebSearchEnabled = true`. The only writer is
`cfg.WebSearchEnabled = d.loadedWebSearchEnabled()`, which *reads* the persisted value;
`liveLoadedWebSearchEnabled` reads `config.LoadVilla().WebSearchEnabled` (default false).
Unlike the coding-agent addon (which has a `--coding-agent` flag wired into `newInstall`),
there is no `--web-search` install flag, no `villa search enable` verb, and no wizard screen
that flips the gate. A grep across `cmd/` and `internal/` for any `WebSearchEnabled = true`
assignment returns only test files. Consequently every SearXNG seam (render append, settings
write, secret write, start, proof) is dead in a real install: a user has no supported way to
turn it on short of hand-editing `config.toml` (which the project treats as untrusted input).
If Phase 29's success criterion is "a user can opt into web search and it comes up healthy,"
that criterion is not met by this code alone.
**Fix:** If the opt-in verb is a deliberate later-phase deliverable, document that dependency
in the phase summary and confirm Phase 29 is render-plumbing only. Otherwise add the opt-in
surface, e.g. a flag mirroring `--coding-agent`:
```go
cmd.Flags().BoolVar(&webSearch, "web-search", false, "install the SearXNG local web-search service")
// in runInstall, before the gate, mirroring opts.codingAgent:
if opts.webSearch { cfg.WebSearchEnabled = true /* persisted via saveConfig */ }
```

### WR-02: `secret_key: ""` relies on an unverified container-entrypoint override of the env var

**File:** `internal/orchestrate/quadlet/searxng-settings.yml.tmpl:11-12`
**Issue:** The settings.yml hard-codes `server.secret_key: ""` and depends on the SearXNG
container substituting the live value from `$SEARXNG_SECRET`. That substitution is performed
by the SearXNG *image entrypoint* (a `sed` over settings.yml), not by SearXNG core reading the
env var directly — and it only fires for certain image variants/versions. Because the image is
digest-pinned to a rolling `latest` snapshot resolved on-hardware (`searxng.go:40`), there is
no test in this phase that asserts the env override actually takes effect; an empty
`secret_key` with the override silently not applied yields a SearXNG that generates an
ephemeral per-restart key (session churn) or refuses to start. The code comments assert
"the live value arrives via $SEARXNG_SECRET" but nothing here proves the chosen image honors it.
**Fix:** Add an integration assertion (even a doc note in the phase summary recording the
on-hardware confirmation) that the pinned digest's entrypoint substitutes `SEARXNG_SECRET`
into `secret_key`. If a future digest re-resolve changes the entrypoint behavior, this is the
silent-break vector. Consider rendering `secret_key: "ultrasecretkey_PLACEHOLDER"` per the
SearXNG-documented override marker if the empty form is not reliably substituted.

### WR-03: `-sf` curl flag conflates a warming-up non-2xx with a hard probe error

**File:** `cmd/villa/install_searxng.go:182-189`
**Issue:** The readiness probe passes `-sf`, which makes curl exit non-zero on ANY non-2xx
response. During cold start SearXNG can briefly return 403 (limiter), 429, or 5xx while engines
initialize. With `-sf` these surface as a `probe()` error rather than a parseable empty result;
`evalSearxngProof` only retries on the *outcome* of `probe()` (it does retry on error too, which
mitigates this), but on the FINAL attempt a transient 5xx is reported as
"did not answer the format=json probe (%v)" — misleading remediation that points at
`systemctl status` when the service is up but still warming. The retry count (3) and 2s delay
(6s total worst case) may also be too short for a first-boot engine warmup on a loaded host.
**Fix:** This is tolerable but brittle. Consider widening the retry budget for the first opt-in,
or dropping `-f` and inspecting the decoded body so a 429/5xx with a structured SearXNG error is
reported distinctly from "unreachable." At minimum, document the 6s worst-case budget as a
known cold-start limit.

### WR-04: Settings.yml has no SearXNG schema/version guard — a silent upstream format drift fails open

**File:** `internal/orchestrate/quadlet/searxng-settings.yml.tmpl`, `internal/orchestrate/searxng.go:87-100`
**Issue:** The rendered settings.yml pins behavior on `use_default_settings.engines.keep_only`
and `server`/`search` keys whose schema is owned by the rolling-`latest` SearXNG image. Because
the image is digest-pinned to a moving target re-resolved by hand, a future digest bump that
renames or restructures these keys (SearXNG has changed engine-config shape across releases)
would be silently accepted by the container (unknown keys ignored) and quietly widen the engine
set back to the full default list — defeating the SRCH-04 bounded-outbound-surface guarantee
without any test failing. The allowlist test (`TestSearxngEngineAllowlist`) only checks the
*rendered* text, not that the running container honored `keep_only`.
**Fix:** Out of v1 correctness scope to fully solve, but record the coupling: the SRCH-04
outbound-surface guarantee is only as strong as the pinned image's `keep_only` semantics.
Phase 33's `villa verify search` (referenced in `searxng.go:92`) should assert the *effective*
engine set, not just the rendered file. Flag this dependency in the phase summary.

### WR-05: Best-effort dir-fsync swallows errors, weakening the durability claim for the secret file

**File:** `internal/orchestrate/searxng_settings_write.go:142-147`
**Issue:** `atomicWriteMode` documents a "durable" rename but the directory fsync is best-effort
and fully discards both the open error and the sync error (`if df, derr := os.Open(dir); derr == nil { _ = df.Sync(); _ = df.Close() }`). On a crash immediately after `os.Rename` but before
the (skipped/failed) dir fsync, the rename can be lost on some filesystems — leaving the
0600 secret env file absent and the container starting with no secret. This mirrors
`reconcile.go`'s existing pattern, so it is consistent, but for the *secret* file the silent
durability gap is more consequential than for a regenerable unit.
**Fix:** Acceptable as-is for parity, but consider surfacing a dir-fsync failure as a warning
for the secret-env writer specifically, since its loss is not self-healing the way a unit is
(units are re-reconciled every install; the secret env is only re-written on the next opt-in).

## Info

### IN-01: `RenderSearxngSettings` accepts `cfg` but ignores it

**File:** `internal/orchestrate/render.go:242-252`
**Issue:** `RenderSearxngSettings(cfg config.VillaConfig)` never reads `cfg` — it renders from
the package-level `searxngEngines` slice only. The parameter is documented as "accepted for
forward symmetry," but an unused parameter invites a caller to assume config drives the output
(it does not — e.g. a custom `SearxngAddr` does not affect settings.yml).
**Fix:** Either drop the parameter or add a `_ = cfg` with a sharper comment; if forward
symmetry is intended, leave a TODO naming the config field that will eventually be consumed.

### IN-02: `searxngResult.UnresponsiveEngines` is decoded but never used in the verdict

**File:** `cmd/villa/install_searxng.go:108`, `116-117`
**Issue:** `UnresponsiveEngines []any` is parsed and documented as "observable but never a FAIL
on its own," but `hasAnswer()` and `evalSearxngProof` never reference it — it is decoded dead
weight. The FAIL detail also never reports it, so the "observable" claim isn't realized.
**Fix:** Either include the unresponsive-engine count in the PASS/FAIL detail string (genuinely
useful operator signal — "2/4 engines answered") or drop the field to avoid implying it informs
the verdict.

### IN-03: Magic retry constants are reasonable but undocumented as a total budget

**File:** `cmd/villa/install_searxng.go:67`, `70`
**Issue:** `searxngProofRetries = 3` and `searxngProofRetryDelay = 2 * time.Second` together
imply a ~6s cold-start budget, but that total is never stated and is not derived from any
measured warmup figure. (See WR-03.)
**Fix:** Add a one-line comment stating the effective worst-case budget and its basis.

### IN-04: Probe uses a hardcoded English query phrase — niche locale/engine fragility

**File:** `cmd/villa/install_searxng.go:60`
**Issue:** `searxngProbeQuery = "villa readiness probe"` assumes the keep_only engines return a
populated result set for an English phrase. For a future allowlist swapped to non-English /
reference-only engines, a healthy instance could legitimately return zero results and FAIL the
proof. Low risk given the current duckduckgo/brave/wikipedia set.
**Fix:** Keep, but note the coupling between the probe phrase and the engine allowlist; if the
allowlist changes, re-validate the probe returns results.

---

_Reviewed: 2026-06-18_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
