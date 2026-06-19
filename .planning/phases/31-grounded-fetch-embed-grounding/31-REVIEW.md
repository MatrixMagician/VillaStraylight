---
phase: 31-grounded-fetch-embed-grounding
reviewed: 2026-06-19T00:00:00Z
depth: deep
files_reviewed: 24
files_reviewed_list:
  - internal/websafe/ssrf.go
  - internal/websafe/loader.go
  - internal/websafe/websafe.go
  - internal/websafe/guard_stubs.go
  - internal/orchestrate/websafe.go
  - internal/orchestrate/websafe_secret_env_write.go
  - internal/orchestrate/openwebui.go
  - internal/orchestrate/render.go
  - internal/orchestrate/orchestrate.go
  - internal/recommend/recommend.go
  - internal/config/villaconfig.go
  - cmd/villa/install.go
  - cmd/villa/install_websafe.go
  - cmd/villa/websafe.go
  - cmd/villa/lifecycle.go
  - cmd/villa/coding-mode.go
  - cmd/villa/dashboard.go
  - cmd/villa/model.go
  - cmd/villa/recommend.go
  - cmd/villa/backend.go
  - cmd/villa/inference.go
  - cmd/villa/doctor.go
  - cmd/villa/restore.go
  - cmd/villa/root.go
findings:
  critical: 2
  warning: 1
  info: 2
  total: 5
status: issues_found
---

# Phase 31: Code Review Report

**Reviewed:** 2026-06-19
**Depth:** deep (cross-file import graph + call chains)
**Files Reviewed:** 24
**Status:** issues_found

## Summary

Deep adversarial review of the v1.5 Phase 31 grounded-fetch (`villa-websafe`) path,
with highest scrutiny on the security surface (SSRF guard, loader, secret handling,
recommend reservation, orchestrate seam, and the mechanical `WebSearchInputs{}`
threading across the cmd tier).

The **SSRF guard (`ssrf.go`) is well-constructed** and I found no bypass: the connect-time
`net.Dialer.Control` hook validates the *resolved* IP (defeating DNS-rebinding TOCTOU),
the reject-set is comprehensive (loopback/RFC1918/link-local incl. the 169.254.169.254
metadata IP / CGNAT / ULA / v4-mapped-v6 / this-network), `Is4In6().Unmap()` neutralizes
the IPv4-mapped-IPv6 bypass, `ParseAddr` fails closed on an unparseable connect target,
and `CheckRedirect` enforces a redirect cap + scheme allowlist + hostname reject on every
hop while the Control hook re-validates each new dial. Octal/decimal/hex literal-IP forms
are caught by the fail-closed post-resolution `ParseAddr`.

**Secret handling is sound**: `GenerateWebLoaderSecret` uses crypto/rand (256-bit hex); the
bearer reaches both containers only via a 0600 `EnvironmentFile` (`writeSearxngFile` →
MkdirAll 0700, `assertInsideDir` traversal guard, atomic temp-inode write at 0600 that is
renamed over the target, so the final mode is umask-safe 0600); the secret value never
lands in a 0644 unit, a log line, or stdout; the auth comparison is constant-time
(`crypto/subtle`). The recommend reservation math uses saturating add / clamp-to-zero
correctly, is gated on `WebSearchEnabled`, and bumps the schema 3→4 consistently. The
mechanical `WebSearchInputs{}` threading is correct everywhere — the two empty-input Pick
sites (`status.go:724`, `coding-mode.go:199`) only read envelope-independent `WeightBytes`
for an explicit `Overrides{Model:...}`, so dropping the reservation there is correct and
documented.

Two genuine defects remain in the loader's HTML handling — both reachable from
**attacker-controlled fetched page content** — plus a Phase-31-introduced gap in the
restore recovery path.

## Critical Issues

### CR-01: `extractTitle` slices `body` with indices from a length-shifted lowercased copy — remote panic (slice bounds out of range) crashes the loader process

**File:** `internal/websafe/websafe.go:186-203`
**Issue:** `extractTitle` computes `open`/`gt`/`start`/`end` against `s :=
strings.ToLower(string(body))`, then slices the **original** `body[start : start+end]`.
`strings.ToLower` is NOT length-preserving: runes such as U+023A (Ⱥ) and U+023E (Ⱦ) grow
2→3 bytes when lowercased. With such a rune in the `<title>` region, `len(s) > len(body)`,
so `start+end` (valid in `s`) exceeds `len(body)` → `runtime error: slice bounds out of
range`. Reproduced: `"<title>" + strings.Repeat("Ⱥ",20) + "X</title>"` (56 bytes; lowercased
76) panics with `[:68] with capacity 56`. The body is **attacker-controlled** fetched web
content. The panic fires inside the goroutine spawned by `Loader.Load` (`websafe.go:87-95`),
which has no `recover`, and `Server.Handler()` (`loader.go:78-82`) installs no Recoverer
middleware — an unrecovered goroutine panic crashes the entire `villa-websafe` process. Any
malicious or compromised search-result page DoSes the loader.
**Fix:** Index the SAME string you searched. Slice the un-lowercased `orig` (same byte length
as the `s` it was derived from), never `[]byte(body)` with `s`-derived offsets:
```go
func extractTitle(body []byte) string {
	orig := string(body)
	s := strings.ToLower(orig) // same byte length as orig
	open := strings.Index(s, "<title")
	if open < 0 { return "" }
	gt := strings.IndexByte(s[open:], '>')
	if gt < 0 { return "" }
	start := open + gt + 1
	end := strings.Index(s[start:], "</title>")
	if end < 0 { return "" }
	return strings.TrimSpace(orig[start : start+end])
}
```
Add a per-fetch `recover` in `fetchOne` (or a Recoverer on the websafe mux) as
defense-in-depth so any future extraction bug degrades to skip-and-continue, not a crash.

### CR-02: `extractText` is a `<`/`>` state machine — an unterminated `<` silently swallows all following content (grounding-integrity loss)

**File:** `internal/websafe/websafe.go:166-182`
**Issue:** `extractText` sets `inTag = true` on the first `<` and only clears it on the next
`>`. A bare `<` with no matching `>` — common in real HTML/JS (`if (a < b)`, unclosed
comments) and especially likely when the body is truncated at `Bounds.MaxBytes` (2 MiB)
mid-`<...` — causes **every subsequent character to be discarded** as "inside a tag". The
page is still emitted as a "successful" citation via skip-and-continue (`websafe.go:152-160`),
so the model is silently grounded on empty/partial `page_content` with no error surfaced.
This defeats the GROUND-01 grounding guarantee invisibly to the operator. Because the input
is attacker-influenced and the failure is silent + always-on for truncated bodies, this is a
correctness defect that should be fixed before shipping.
**Fix:** Per the file's own RESEARCH "Don't Hand-Roll HTML→text" note, defer real extraction
to the Phase-32 bluemonday pass and emit the bounded raw text as the fallback rather than a
byte-eating tag scanner. As an interim, bound the in-tag state: if a `<` is not closed within
a small lookahead (or at EOF), treat it as literal text and flush the buffered run instead of
discarding the remainder of the body.

## Warnings

### WR-01: `restore` re-renders an OWUI/websafe unit with `EnvironmentFile=` but never writes the websafe.env secret — web-search restore can fail to start on a fresh host

**File:** `cmd/villa/restore.go:359-396` (`ReconcileAndWrite`); cf. `internal/orchestrate/openwebui.go:290-293`, `internal/orchestrate/render.go:240-244`
**Issue:** Phase 31 makes the **OWUI** unit carry `EnvironmentFile={websafe.env}` whenever
`cfg.WebSearchEnabled` (openwebui.go:118-124, template `{{if .SecretEnvFile}}`), in addition
to the new `villa-websafe` unit. `restore`'s `ReconcileAndWrite` renders these units from the
restored config and writes them, then `Start`s the services — but restore never calls
`WriteWebsafeSecretEnv` (nor `WriteSearxngSecretEnv`). The 0600 env files live outside the
backup archive (only `config.toml` is restored). On a fresh-host restore of a web-search-enabled
backup, `systemctl start villa-openwebui.service` fails because its `EnvironmentFile=` target
is absent. Previously OWUI had no EnvironmentFile dependency, so this is a Phase-31-introduced
regression to the recovery path. (Same-host restore happens to work because the file persists
from the original install; the SearXNG env file shares the pre-existing gap, but the OWUI
EnvironmentFile dependency is new here.)
**Fix:** In restore's reconcile/start flow, when the restored `cfg.WebSearchEnabled` is true
and `cfg.WebLoaderSecret`/`cfg.SearxngSecret` are present, write the 0600 env files via
`orchestrate.RenderWebsafeSecretEnv`/`WriteWebsafeSecretEnv` (and the SearXNG pair) BEFORE
starting OWUI/websafe — mirroring install.go step 9a. If the secret is absent from the
restored config, fail closed with remediation rather than starting into a missing-file failure.

## Info

### IN-01: Empty-secret posture accepts any villa.network caller — verify it can never be the live default

**File:** `internal/websafe/loader.go:87-90`, `cmd/villa/websafe.go:117`
**Issue:** `authOK` returns `true` when `s.secret == ""`, and `liveWebsafeDeps` sources the
bearer from `os.Getenv("EXTERNAL_WEB_LOADER_API_KEY")`. If the 0600 EnvironmentFile is ever
missing/empty at container start (e.g. the WR-01 restore gap, or a future wiring change), the
loader silently runs **unauthenticated** — any pod on villa.network could drive arbitrary
fetches through the SSRF-guarded client. The install flow does generate-and-persist the bearer
(install.go:741-765), so the live path is covered today; this is defense-in-depth.
**Fix:** Consider failing closed (refuse to serve, or log a loud warning) when the container
env bearer is empty in the live wiring, reserving empty-secret-accepts-all strictly for tests.

### IN-02: `websafe-serve` binds `--host 0.0.0.0` from a user-overridable flag

**File:** `cmd/villa/websafe.go:69`, `internal/orchestrate/websafe.go:128-136`
**Issue:** The hidden `websafe-serve` command defaults `--host 0.0.0.0` (container-internal;
the unit publishes no host port, so PRIV-01 holds). The flag is overridable, but the subcommand
is hidden and invoked only by the generated Exec with fixed tokens, so there is no real
exposure. Noting only that the loopback-only invariant for this service rests entirely on the
unit NOT publishing a host port (verified: `websafeView` has no PublishPort field and the
template emits none), not on the bind host. No change required.

---

_Reviewed: 2026-06-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
