# Phase 33: Egress-Bounding + `villa verify search` - Pattern Map

**Mapped:** 2026-06-19
**Files analyzed:** 5 (2 new source + 1 new golden + 2 existing test/render touched)
**Analogs found:** 5 / 5

> Per RESEARCH: 3 of 4 pillars already shipped. `web_search_enabled` (PRIV-07) and the OWUI
> outbound-kill env (PRIV-09) are ALREADY in the tree — Phase 33 ASSERTS them, never re-adds
> them. The single novel deliverable is `cmd/villa/verify_search.go`. All analogs below are
> in-tree; this phase adds ZERO external packages.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/villa/verify_search.go` (NEW) | command + pure core + live seam | request-response / event-driven (probe) | `cmd/villa/verify_agent.go` | exact (clone) |
| `cmd/villa/verify_search_test.go` (NEW) | test | table + golden | `cmd/villa/recommend_test.go` (golden) + `verify_agent_test.go` (truth-table) | exact |
| `cmd/villa/testdata/verify-search.json.golden` (NEW) | golden fixture | byte-frozen contract | `cmd/villa/testdata/recommend.golden.json` | exact |
| `cmd/villa/verify.go` (MODIFY: add `cmd.AddCommand(newVerifySearch())`) | route/dispatch | registration | `cmd/villa/verify.go:75-76` (`newVerify`) | exact (in-place) |
| `internal/orchestrate/*_test.go` (MODIFY, optional: PRIV-09 regression assert) | test | golden assertion | `TestRenderOpenWebUITelemetryFrozen` | exact |

**DO NOT TOUCH** (assert-only, per RESEARCH): `internal/orchestrate/openwebui.go` env block;
`internal/config/villaconfig.go` schema; the two `villa-openwebui.container*.golden` files.

## Pattern Assignments

### `cmd/villa/verify_search.go` (NEW — command + pure 3-state core + live seam)

**Analog:** `cmd/villa/verify_agent.go` (clone the four-layer shape verbatim; `verify.go` for dispatch).

**Imports pattern** (`verify_agent.go:1-12`):
```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)
```
For the in-process families add: `"github.com/MatrixMagician/VillaStraylight/internal/websafe"`,
`"strings"`, `"encoding/json"` (for `--json`), `"net/netip"` (family-c SSRF inputs).

**Exit-code convention** — REUSE the AUTHORITATIVE preflight constants (`cmd/villa/preflight.go:42-44`),
do NOT invent a 4th code:
```go
exitPass    = 0 // PASS
exitBlocked = 1 // FAIL  (a security property was violated)
exitWarn    = 2 // REJECT (could-not-evaluate / honest infra-fail — same semantics doctor.go uses)
```
Map: `PASS→exitPass`, `FAIL→exitBlocked`, `REJECT→exitWarn`.

**3-state verdict type** — `memoryProof` (PASS/FAIL only) is INSUFFICIENT. Introduce a `searchProof`
with a 3-value status. Shape from `memoryProof` (`install_memory.go:188`):
```go
type searchStatus int
const (
	searchPass searchStatus = iota
	searchFail
	searchReject
)
type searchProof struct {
	status searchStatus
	detail string // refuse-with-remediation text on every non-PASS
}
```

**Pure inverse-framing core** (clone `evalAgentVerify`, `verify_agent.go:62-103` — negative-control FIRST).
The locked truth-table order (RESEARCH Pattern 3 / SC2 — DO NOT INVERT):
1. allowlist-reachable UNGUARDED → else REJECT (probe env broken)
2. canary-reachable UNGUARDED → else REJECT (nothing to prove a block of — the empty-netns trap)
3. apply bound → canary MUST be unreachable; STILL reachable ⇒ **FAIL** (ineffective block, never a fabricated PASS)
4. allowlist STILL reachable under bound → else REJECT (blanket block proves nothing)
5. families (b) injection, (c) SSRF, (d) secret-query → any violation ⇒ FAIL
RESEARCH §"Code Examples" gives the full `evalSearchVerify(...)` skeleton — use it verbatim.

**curl-exit classifier** — clone `classifyEgressProbe` (`verify_agent.go:131-148`) + its constants
(`verify_agent.go:109-113`): exits `6/7/28` = genuinely blocked; exit `0` = reachable; any other
nonzero / never-started = REJECT (NOT a false block):
```go
const (
	curlExitCouldNotResolve  = 6
	curlExitFailedToConnect  = 7
	curlExitOperationTimeout = 28
)
```

**Live probe exec** — REUSE `runProbeCurlCode` / `runProbeCurl` / `extractExitCode`
(`install_memory.go:384`, `:351`, `:421`) and the helper image from `orchestrate.EmbedImage()`.
NEVER re-type the image literal (`TestSeamGrepGate` fails on a leaked literal). The probe network
constant is `memoryProofNetwork = "villa"` (`install_memory.go:243`).

**WR-02 (anti-pattern, `verify_agent.go:200-210`)** — the reachability probe MUST OMIT `curl -f`.
Use `"-s", "--max-time", "5"`. With `-f` a 4xx/5xx (reachable) becomes exit 22 → misclassified as
REJECT, excusing open egress. Any HTTP response = exit 0 = reachable.

**Deps seam + live wiring** (clone `verifyAgentDeps` `:285-300` + `liveVerifyAgentDeps` `:304-311`):
```go
type searchVerifyDeps struct {
	loadedWebSearchEnabled func() bool                       // gate: liveLoaded... → config.LoadVilla().WebSearchEnabled
	loadedConfig           func() config.VillaConfig
	verifyFn               func(ctx context.Context, deps searchVerifyDeps) searchProof // live: liveSearchVerify
}
```
Gate source is `VillaConfig.WebSearchEnabled` (`internal/config/villaconfig.go:138`,
toml `web_search_enabled`) — ALREADY shipped, read-only here.

**cobra registration + gate** (clone `newVerifyAgent` `:317-337` and `runVerifyAgent` `:345-361`):
the `RunE` does `os.Exit(runVerifySearch(...))`; `runVerifySearch` RETURNS the code (testable).
Gate-OFF exits `exitPass` with "nothing to verify" (NOT the silent-skip hazard):
```go
if !deps.loadedWebSearchEnabled() {
	fmt.Fprintln(out, "verify search: web search is not enabled (web_search_enabled=false) — nothing to verify ...")
	return exitPass
}
```
Add to the verify group in `verify.go:75-77`:
```go
cmd.AddCommand(newVerifyMemory())
cmd.AddCommand(newVerifyAgent())
cmd.AddCommand(newVerifySearch())  // NEW
```

---

### Family (b) — in-process injection assertion (NO network)

**Analog/source:** `internal/websafe/websafe.go` `Loader`/`Page`/`Verdict` (REUSE AS-IS).

`Page` (`websafe.go:40-55`) carries `Content string`, `Source string`, `Title string`,
`Verdict Verdict`. `Deps{Client *http.Client}` (`websafe.go:31`); construct via `NewLoader(deps, bounds)`
and drive `Load(ctx, urls) []Page`. Inject a stub `*http.Client` returning a planted-injection page,
then assert (RESEARCH §Code Examples):
```go
loader := websafe.NewLoader(websafe.Deps{Client: stubClientReturning(inj)}, websafe.DefaultBounds())
p := loader.Load(ctx, []string{"https://example.test/planted"})[0]
flagged  := p.Verdict.Detected && len(p.Verdict.Rules) > 0
stripped := !strings.Contains(p.Content, "<script>")
fenced   := strings.Contains(p.Content, "UNTRUSTED_WEB_CONTENT nonce=")
```
This asserts on Phase-32's shipped guard output — no live bound, no network.

---

### Family (c) — in-process SSRF assertion (direct function calls)

**Analog/source:** `internal/websafe/ssrf.go` (REUSE AS-IS). These are pure functions — call directly.

- `ipRejected(netip.Addr) bool` (`ssrf.go:84-102`) — true for loopback/link-local/private/CGNAT prefixes; assert `ipRejected(netip.MustParseAddr("169.254.169.254"))==true`, `ipRejected(127.0.0.1)==true`.
- `hostRejected(host string) bool` (`ssrf.go:109-115`) — true for `localhost`, `villa-*`, `*.network`, `*.localhost`; assert `hostRejected("villa-searxng")==true`, `hostRejected("localhost")==true`.
- `control(network, address string, _ syscall.RawConn) error` (`ssrf.go:122-135`) — connect-time hook; returns SSRF error for an internal connect address.
- Optionally drive `SafeClient(DefaultBounds())` (`ssrf.go:146`) against an internal URL to prove the live wiring refuses.

Note: `ipRejected`/`hostRejected`/`control` are UNEXPORTED — the family-c test must live in
`package websafe` (add an internal-host case to `internal/websafe/ssrf_test.go` per RESEARCH Wave-0
gap), and `verify_search.go` asserts SSRF via the exported `SafeClient` path.

---

### The transient rootless-netns nft bound (live seam only)

**Analog:** `runProbeCurlCode` fixed-arg exec (`install_memory.go:384`) + on-host-verified `unshare -rn nft -f -`.

- Build the ruleset as a string, feed via `nft -f -` on STDIN — NO shell interpolation (CLAUDE.md invariant). Allowlist IPs are `netip`-validated before formatting into `ip daddr <ip> accept`.
- Verified ruleset (RESEARCH Pattern 4): `table inet villabound { chain output { type filter hook output priority 0; policy drop; oif "lo" accept; ct state established,related accept; ip daddr <ALLOW> accept } }`.
- **Pitfall 1 (load-bearing):** a fresh `unshare -rn` netns has only `lo` — apply the bound where egress actually flows (architecture A, podman rootless-netns), gated on the allowlist-reachable positive control so an unroutable env REJECTs instead of false-passing. Mechanism is an on-hardware task (Open Q2); the pure core is mechanism-agnostic.
- Teardown: ephemeral `unshare -rn` subprocess auto-tears-down; if the rootless-netns is touched, use a `defer`-always restore (precedent `runLlamaDownControl`, `verify_agent.go:259-269`) and downgrade a restore failure to REJECT.
- Honest REJECT if `nft`/`unshare` absent (typed-Unknown → never a false PASS).

---

### `cmd/villa/verify-search.json.golden` (NEW — byte-frozen `--json`, schema v1)

**Analog:** `cmd/villa/recommend_test.go:TestRecommendJSONGolden` (`:65-90`) + `testdata/recommend.golden.json`.

`--json` is NET-NEW to the verify family (no existing verify cmd emits JSON), so schema starts at
**version 1** (greenfield, no append-only constraint against a prior verify json). Golden test shape:
```go
golden := filepath.Join("testdata", "verify-search.json.golden")
if *update { os.WriteFile(golden, buf.Bytes(), 0o644); return }
want, _ := os.ReadFile(golden)
if !bytes.Equal(buf.Bytes(), want) { t.Errorf("JSON does not match golden ...") }
```
`var update = flag.Bool("update", …)` is package-level in `cmd/villa` already — reuse it.

## Shared Patterns

### Pure-core + injectable-seam
**Source:** `verify_agent.go` (`evalAgentVerify` pure / `liveAgentVerify` seam / `verifyAgentDeps`).
**Apply to:** verify_search verdict math is pure; netns/nft/curl live work behind `searchVerifyDeps` func fields.

### Exit-code convention
**Source:** `cmd/villa/preflight.go:42-44` (`exitPass=0, exitBlocked=1, exitWarn=2`).
**Apply to:** PASS→0, FAIL→1, REJECT→2. `RunE` calls `os.Exit(run…())`; `run…` returns the code.

### Negative-control-FIRST honesty
**Source:** `evalAgentVerify:62-80` + `classifyEgressProbe:131-148`.
**Apply to:** assert allowlist-reachable + canary-reachable-UNGUARDED before any "blocked" claim; ineffective block = FAIL (never PASS); broken env = REJECT.

### No shell interpolation / seam-locked image
**Source:** `runProbeCurlCode` (`install_memory.go:384`) + `orchestrate.EmbedImage()` / `LlamaInNetworkEndpoint()`.
**Apply to:** fixed-arg `exec.Command`; `nft -f -` ruleset on stdin; helper image only from `EmbedImage()` (`TestSeamGrepGate`).

### Byte-frozen golden contract
**Source:** `TestRecommendJSONGolden` (`recommend_test.go:65`).
**Apply to:** `verify search --json` schema v1; refreeze intentionally with `-update`.

### PRIV-09 regression (assert, never modify)
**Source:** `internal/orchestrate/openwebui.go` base env (the `HF_HUB_OFFLINE=1`, `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False` block) + `TestRenderOpenWebUITelemetryFrozen`.
**Apply to:** optionally EXTEND the telemetry-frozen test to assert these keys present in BOTH `villa-openwebui.container.golden` and `…websearch.golden`. Adding the keys again duplicates them and breaks the golden — DO NOT.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every piece has a strong in-tree analog. The only genuinely new mechanics (the `unshare -rn`+`nft` bound seam) compose `runProbeCurlCode`'s fixed-arg exec pattern + on-host-verified tooling; mechanism A-vs-B is an on-hardware plan task (RESEARCH Open Q2), not a missing pattern. |

## Metadata

**Analog search scope:** `cmd/villa/` (verify_*, install_memory, preflight, recommend_test), `internal/websafe/` (websafe.go, ssrf.go), `internal/orchestrate/` (openwebui.go, searxng.go), `internal/config/villaconfig.go`.
**Files scanned:** ~12 (graphmind-indexed project; targeted reads).
**Pattern extraction date:** 2026-06-19
