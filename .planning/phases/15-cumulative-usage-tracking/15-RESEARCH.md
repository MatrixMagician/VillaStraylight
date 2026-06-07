# Phase 15: Cumulative Usage Tracking - Research

**Researched:** 2026-06-07
**Domain:** Local Prometheus-counter folding + XDG-data persistence + byte-frozen `status.Report` evolution (Go control plane)
**Confidence:** HIGH

## Summary

Phase 15 is an **internal extension phase**, not an integration phase: every primitive
it needs already ships in the repo and the work is composing them under the established
pure-core + injectable-seam discipline. The two MEDIUM-confidence research flags from the
ROADMAP are now resolved to **HIGH confidence**: the llama.cpp `/metrics` counter names
are confirmed verbatim against the official server README — `llamacpp:prompt_tokens_total`
and `llamacpp:tokens_predicted_total`, both declared Prometheus `Counter:` type — and the
counter-reset-on-restart pattern is confirmed structural (the counters live in an in-memory
`server_metrics` struct that starts at 0 each process start and is never persisted), which
is exactly what the reset-aware fold (D-03/D-04) is designed for.

There is one **non-obvious nuance worth a verification step** (see Pitfall 1): a long-standing
llama.cpp design quirk is that the `/metrics` and `/health` endpoints share `TASK_TYPE_METRICS`
and a "reset bucket" applies to the **timing buckets** that feed the *rate gauges*
(`*_seconds`) — NOT to the `_total` monotonic counters. The `_total` counters carry the
standard Prometheus monotonic-counter semantics. The fold's non-monotonicity reset detection
(D-04) is correct regardless, because it treats any backward step as a reset; but the plan
should include a single live-server sanity check confirming the `_total` counters monotonically
grow across consecutive scrapes (they do not reset per scrape).

**Primary recommendation:** Build a new pure `internal/usage` core (`Fold(prior, sample) -> Totals`),
extend `internal/metrics` with the two `_total` counters as a sibling typed-Unknown read, persist
to `$XDG_DATA_HOME/villa/usage.json` with a full-file **atomic temp+rename** write (NOT append —
this is a single mutable JSON document, unlike benchstore's JSONL), make the dashboard
`/api/metrics` handler the SOLE mutex-guarded writer keyed by `cfg.Model`, and surface ONE
`*UsageTotals omitempty` field on `status.Report` above `SchemaVersion` with `reportSchemaVersion`
bumped 1→2 and the single golden `cmd/villa/testdata/status.json.golden` re-frozen once.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Counter scrape (read `_total` from loopback `/metrics`) | `internal/metrics` | — | Already owns the bounded `/metrics` scrape + `parsePromText`; extend, never fork (D-06) |
| Reset-aware fold arithmetic | `internal/usage` (pure core) | — | Pure decision logic, no I/O — mirrors `recommend`/`bench` pure-core convention (D-01) |
| Persist/read `usage.json` | `internal/usage` store seam | cmd-tier live wiring | Pure core owns the contract; host I/O is an injected `Deps` seam (D-01/D-02) |
| Write trigger (fold + atomic write) | `internal/dashboard` `/api/metrics` handler | cmd-tier live wiring | The long-lived service is the single accumulation authority (D-07/D-08) |
| Read totals for display | `internal/status` `Run` | — | One append-only `Report` field, read by BOTH CLI and dashboard (D-09/D-10) |
| Model identity per scrape | cmd-tier live wiring (`cfg.Model`) | — | Config is the single source of truth; identity is `cfg.Model` at scrape time (D-07) |
| No-new-outbound posture | `internal/status` `no_telemetry` + structural test | `internal/usage` field-set test | Derives only from the existing loopback scrape; assert structurally (D-11/D-12) |

## Standard Stack

This phase adds **NO new third-party dependencies.** Everything is Go stdlib + existing
first-party packages. This is a deliberate constraint (single static CGO-free binary).

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `encoding/json` | go1.26.2 | Marshal/unmarshal `usage.json` | Already the project's JSON store format (benchstore, goldens) `[VERIFIED: go.mod]` |
| Go stdlib `os` / `path/filepath` | go1.26.2 | XDG path resolve, MkdirAll, atomic temp+rename | Mirrors `internal/config` + `internal/benchstore` write discipline `[VERIFIED: codebase]` |
| Go stdlib `sync` | go1.26.2 | Mutex guarding the sole writer | `swapMu` precedent on `Server` (`server.go:120`) `[VERIFIED: codebase]` |

### Supporting (existing first-party — extend, do not fork)
| Package | Purpose | When to Use |
|---------|---------|-------------|
| `internal/metrics` | Bounded `/metrics` scrape; extend `parsePromText`/`ScrapeMetrics` to surface the two `_total` counters | The ONLY place the counter literals enter the codebase (D-06) |
| `internal/config` | XDG write-discipline TEMPLATE (0600/0700, traversal guard) to MIRROR (not import-for-config) | Copy the `assertInsideDir` shape into the usage store as benchstore did |
| `internal/status` | `Report` struct + `reportSchemaVersion` — the single byte-frozen contract surface | Add ONE field + bump version + re-freeze golden once |
| `internal/dashboard` | `/api/metrics` handler — the sole writer hook | Fold+write under a new mutex |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Full-file atomic temp+rename | benchstore's `O_APPEND` JSONL | REJECTED: usage is a single *mutable* document (read-modify-write per model), not an append-only log. Append would duplicate per-model records and require last-wins scan-folding on read. Atomic temp+rename is correct for a mutable doc `[CITED: D-02 store semantics]` |
| `os.UserCacheDir` | `XDG_DATA_HOME` resolver (benchstore's `benchReportsPath` shape) | RECOMMENDED: use the **benchstore `benchReportsPath` resolver shape** — see "XDG Data-Dir Resolver" below. Usage is durable accumulated *data* (D-02), not disposable cache; `UserCacheDir` semantics imply it may be evicted `[VERIFIED: codebase benchstore.go:300]` |
| New `/api/usage` endpoint | Reuse `status.Report` field | REJECTED by D-10: no new API surface; dashboard reads the same `Report` field |

**Installation:** None — no packages added.

**Version verification:** N/A — no external packages. The only external *contract* (the
llama.cpp `/metrics` counter names) is verified below under Open Questions / State of the Art.

## Package Legitimacy Audit

> Not applicable — this phase installs **zero** external packages. All code is Go stdlib +
> existing first-party `internal/*` packages already vendored in `go.mod`. No registry
> lookup, no slopcheck run required.

## XDG Data-Dir Resolver (D-02 research flag — RESOLVED)

**Recommendation: clone the benchstore `benchReportsPath` resolver shape, not `os.UserCacheDir`.**

The project already shipped exactly this decision in Phase 14. `internal/benchstore/benchstore.go:300`:

```go
// Source: internal/benchstore/benchstore.go:300 [VERIFIED: codebase]
func benchReportsPath() string {
	const file = "bench-reports.jsonl"
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa", file)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa", file)
	}
	return filepath.Join("/var/tmp", "villa", file)
}
```

For Phase 15 the file is `usage.json`. This honors `$XDG_DATA_HOME`, falls back to
`~/.local/share/villa/`, then `/var/tmp/villa/` — and crucially it is **testable** via
`t.Setenv("XDG_DATA_HOME", tmpDir)` (benchstore_test.go:386 proves the pattern).

**Why not `os.UserConfigDir` (config's choice):** D-02 is explicit — usage is accumulated
*state/data*, not user configuration; it must NOT live next to `config.toml`. `os.UserConfigDir`
resolves to `$XDG_CONFIG_HOME` (`~/.config`), the wrong dir. **Why not `os.UserCacheDir`:**
it resolves to `$XDG_CACHE_HOME` (`~/.cache`), which carries "safe to evict" semantics —
wrong for durable accumulated totals. `[VERIFIED: codebase + XDG Base Directory spec]`

**Consistency note:** the project's `config` package uses `os.UserConfigDir` (XDG-honoring)
and `benchstore` uses the explicit `$XDG_DATA_HOME` resolver above. Phase 15 is data, so it
matches benchstore, giving the milestone exactly one data-dir convention for the two
data-stores (`bench-reports.jsonl` and `usage.json` side-by-side under `villa/`).

## Counts-Only Write Discipline (mirror, do not reuse)

The new usage store must MIRROR `internal/config`'s proven write discipline (D-02) but as a
**full-file atomic rewrite** (not config's plain `os.WriteFile`, and not benchstore's append):

1. Resolve dir via the `usagePath()` resolver above.
2. `assertInsideDir(path, dir)` — copy the guard shape (it is unexported in config; benchstore
   made a LOCAL copy at `benchstore.go:314` to avoid widening deps — do the same). `[VERIFIED: codebase]`
3. `os.MkdirAll(dir, 0o700)`.
4. Write to a temp file in the SAME dir (`os.CreateTemp(dir, "usage-*.json.tmp")`), `0o600`.
5. `os.Rename(tmp, path)` — atomic on the same filesystem; a crash mid-write never leaves a
   torn `usage.json`.
6. `os.Chmod(path, 0o600)` to tighten if the file pre-existed looser.

Atomic temp+rename is the correct upgrade over config's plain `WriteFile` here precisely
because the dashboard rewrites this file repeatedly under load (per scrape), where a
torn-write window is a real (not theoretical) risk.

## Architecture Patterns

### System Architecture Diagram

```
                         (long-lived villa-dashboard.service)
  embedded SPA  ──poll──▶  GET /api/metrics  (internal/dashboard/api.go handleMetrics)
   (interval)                     │
                                  ▼
                     s.scrapeMetrics()  ──▶ internal/metrics.ScrapeMetrics(endpoint)
                                  │                 (loopback /metrics, bounded LimitReader)
                                  │                 returns PerfSnapshot{...} + the two
                                  │                 NEW *_total counter reads (typed-Unknown)
                                  ▼
                    ┌──── usageMu.Lock() ───────────────────────────────┐
                    │  prior := store.Read()        (usage.json)         │  SOLE WRITER
                    │  next  := usage.Fold(prior, sample, cfg.Model)     │  (D-07)
                    │  store.Write(next)            (atomic temp+rename) │
                    └──── usageMu.Unlock() ─────────────────────────────┘
                                  │
                                  ▼
                       metricsView JSON  (live tok/s — UNCHANGED)


  ── READ PATH (both CLI `villa status` and dashboard `/api/status`) ──

  villa status  ──┐
                  ├─▶ internal/status.Run(Deps)
  GET /api/status ┘        │
                           ├─ d.ReadUsage()  ──▶ store.Read(usage.json)  [READ-ONLY]
                           │       (nil/empty ⇒ field omitted = typed-Unknown)
                           ▼
                  Report{ ..., Usage *UsageTotals omitempty, SchemaVersion: 2 }
                           │
                  ┌────────┴─────────┐
            CLI table/--json   dashboard SPA renders alongside live tok/s
```

Key invariants the diagram encodes:
- **One writer** (dashboard scrape path), **two readers** (CLI status + dashboard status) — D-07.
- The fold is keyed by `cfg.Model` captured at scrape time (the live wiring reads config) — D-07.
- An absent/empty store on the read path → omitted field → typed-Unknown, never a fabricated 0 — D-05/D-09.

### Recommended Project Structure
```
internal/usage/
├── usage.go            # pure core: Totals / ModelUsage / CounterPair types + Fold + store Deps seam
├── usage_test.go       # fold reset-aware tests + counts-only structural field-set test (D-11)
└── (no testdata — usage.json is not byte-frozen by a golden; status.Report golden is)

internal/metrics/
├── llamacpp.go         # EXTEND: add the two _total counters (sibling struct or PerfSnapshot fields)
├── llamacpp_test.go    # EXTEND: fixture already has n_decode_total; add the two token _total lines
└── testdata/metrics.txt# EXTEND: add llamacpp:prompt_tokens_total + llamacpp:tokens_predicted_total

internal/status/status.go     # +1 field (*UsageTotals omitempty above SchemaVersion); bump 1→2
internal/dashboard/server.go  # +usageMu + ReadUsage/WriteUsage seams on Config/Server
internal/dashboard/api.go     # handleMetrics: fold+atomic-write under usageMu
cmd/villa/status.go           # liveStatusDeps: wire ReadUsage seam (read-only)
cmd/villa/dashboard.go        # liveDashboardDeps: wire the usage store path + Now seam
cmd/villa/testdata/status.json.golden  # RE-FREEZE ONCE via `go test -update`
```

### Pattern 1: Reset-aware fold over a monotonic counter (D-03/D-04)
**What:** Accumulate a durable cumulative total from a counter that resets to 0 on server
restart / backend swap, by detecting non-monotonicity.
**When to use:** Every fold of a sampled `_total` counter into the persisted total.
**Example:**
```go
// Source: derived from CONTEXT.md D-04 + benchstore pure-core convention [CITED: 15-CONTEXT.md]
// foldCounter adds the new generation since last_seen, treating a backward step
// (server restarted → counter reset low) as "the whole sample is new".
func foldCounter(prior CounterState, sampleRaw uint64) CounterState {
	var delta uint64
	if sampleRaw >= prior.LastSeenRaw {
		delta = sampleRaw - prior.LastSeenRaw // normal monotonic growth
	} else {
		delta = sampleRaw // reset detected: counter went backwards → whole sample is new
	}
	return CounterState{
		Cumulative:  prior.Cumulative + delta,
		LastSeenRaw: sampleRaw,
	}
}
```
Per-model `Fold` applies `foldCounter` to BOTH the prompt and predicted counters, keyed by
model id. A scrape where a counter is typed-Unknown contributes NO fold for that counter (D-05).

### Pattern 2: Typed-Unknown counter read (mirror `ok=false`)
**What:** A `_total` counter absent/unparseable in the scrape → no reading, never a fabricated 0.
**Example:**
```go
// Source: internal/metrics/llamacpp.go parsePromText returns a map; presence is the signal [VERIFIED: codebase]
// Extend ScrapeMetrics to expose presence, e.g. a sibling struct:
type CounterSample struct {
	PromptTokensTotal    uint64
	PromptTokensKnown    bool
	PredictedTokensTotal uint64
	PredictedTokensKnown bool
}
// In ScrapeMetrics, after m := parsePromText(body):
v, ok := m["llamacpp:prompt_tokens_total"]   // ok=false ⇒ counter absent ⇒ Known=false
```
Note `parsePromText` returns `map[string]float64`; the counter is an integer token count.
Read it as float64 and convert to `uint64` (token counts are well within float64 exact-integer
range for any realistic lifetime), OR — preferred for a counts contract — have the metrics
extension carry the comma-clean `uint64` via a typed read. The planner decides (Claude's discretion).

### Pattern 3: Append-only `status.Report` field + single schema bump (D-09)
**What:** Add the cumulative-usage surface as exactly one tail-appended field above
`SchemaVersion`, as a pointer with `omitempty` so an absent store is omitted (typed-Unknown).
**Example:**
```go
// Source: internal/status/status.go:108-123 GenTokensPerSec/ROCmReadiness precedent [VERIFIED: codebase]
type Report struct {
	// ... all existing fields UNCHANGED, in order ...
	ROCmReadiness ROCmReadinessIndicator `json:"rocm_readiness"`

	// NEW (Phase 15): cumulative per-model usage totals. Pointer+omitempty so an
	// absent/empty store renders as typed-Unknown rather than a fabricated zero (D-09).
	Usage *UsageTotals `json:"usage,omitempty"`   // exact name = Claude's discretion

	SchemaVersion int `json:"schema_version"`   // bump const 1 → 2
	err error
}
const reportSchemaVersion = 2   // was 1
```

### Anti-Patterns to Avoid
- **Appending to usage.json like benchstore's JSONL:** WRONG — usage is a single mutable doc;
  append produces duplicate per-model records. Use full-file atomic temp+rename.
- **Letting the CLI write usage.json:** Violates D-07 (sole-writer = dashboard). The CLI is
  one-shot and read-only; a CLI writer races the dashboard.
- **Putting the counter literals in `internal/usage` or the dashboard:** The counter name
  strings (`llamacpp:prompt_tokens_total`, `llamacpp:tokens_predicted_total`) belong ONLY in
  `internal/metrics` (D-06) — keep the scrape vocabulary in one place, exactly like the existing
  gauges. (NB: these are llama.cpp metric names, NOT backend marker literals, so they are not
  policed by `TestSeamGrepGate` — but the same single-home discipline applies.)
- **Fabricating a 0 total when the store is absent:** Omit the field (typed-Unknown). A real
  0 (model seen, zero tokens) and "never accumulated" must be distinguishable — pointer+omitempty.
- **Two schema bumps / two golden re-freezes:** Exactly ONE `status.Report` bump and ONE
  `status.json.golden` re-freeze (D-09). The usage-store's own `schema_version` is independent
  and is NOT golden-frozen.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Prometheus text parsing | A new parser | Existing `metrics.parsePromText` (strips labels, skips `#`) | Already battle-tested, WR-02 label-strip guard in place `[VERIFIED: codebase]` |
| Bounded HTTP scrape | New http.Client | Existing `metrics.ScrapeMetrics` path (2s timeout, 64 KiB LimitReader) | Reuse the bound; adding a counter is a map lookup, not a new request `[VERIFIED: codebase]` |
| XDG path + traversal guard | New resolver/guard from scratch | Clone `benchstore.benchReportsPath` + `assertInsideDir` | Phase 14 precedent; identical data-dir requirement `[VERIFIED: codebase]` |
| Atomic file write | Lockfiles / fsync dance | `os.CreateTemp` in same dir + `os.Rename` | POSIX rename is atomic on same fs; stdlib-only `[CITED: Go os docs]` |
| Concurrency guard | Channels/actor | `sync.Mutex` sibling to `swapMu` | `swapMu` precedent; TryLock-or-proceed pattern available `[VERIFIED: codebase server.go:120]` |
| Schema/contract test | Hand-diff JSON | Existing golden harness (`-update` flag, `TestStatusJSONGolden`) | Byte-frozen contract already has tooling `[VERIFIED: codebase status_test.go:285]` |

**Key insight:** Phase 15 introduces no genuinely new technical problem — it recombines the
metrics scrape (Phase 5), the XDG data-store discipline (Phase 14 benchstore), the
append-only `Report` evolution (Phase 10 `GenTokensPerSec`), the narrow-field security test
(Phase 5 `metrics.Slot`), and the mutex-guarded dashboard writer (Phase 5 `swapMu`). Every
piece has shipped precedent; "hand-rolling" here means re-deriving a solved problem.

## Runtime State Inventory

> Not a rename/refactor/migration phase — this is additive greenfield within an existing
> codebase. No stored data is being renamed, no live-service config rewritten, no OS state
> re-registered. **The ONE new piece of runtime state created** is `usage.json` under XDG
> data — net-new, so there is nothing pre-existing to migrate. Backup (Phase 16) is noted in
> the ROADMAP to capture this new store; that is Phase 16's concern, not this phase's.
>
> **Stored data:** None pre-existing. New: `$XDG_DATA_HOME/villa/usage.json` (created fresh).
> **Live service config:** None. The dashboard service gains a writer hook but no config change.
> **OS-registered state:** None.
> **Secrets/env vars:** None. (Reads existing `$XDG_DATA_HOME` only.)
> **Build artifacts:** None.

## Common Pitfalls

### Pitfall 1: Assuming `_total` resets per-scrape (the `reset_bucket` quirk)
**What goes wrong:** A web-search-level reading of llama.cpp metrics suggests "metrics are
reset on each `/metrics` call" because `/metrics` and `/health` share `TASK_TYPE_METRICS` and
there is a known `reset_bucket()` behavior.
**Why it happens:** The reset bucket applies to the **timing accumulators** that back the
*rate gauges* (`llamacpp:prompt_tokens_seconds` / `predicted_tokens_seconds` — the existing
gauges the dashboard already uses), NOT to the `_total` monotonic counters. The `_total`
suffix carries standard Prometheus monotonic-counter semantics (cumulative since process start).
**How to avoid:** (1) Trust the `_total` semantics for the design. (2) The fold is *correct
regardless* — non-monotonicity detection (D-04) treats ANY backward step as a reset, so even
if a future llama.cpp version changed semantics, the worst case is under-counting within one
process lifetime, never a negative or garbage total. (3) Add a one-line **live sanity check**
in UAT: scrape `/metrics` twice during generation and confirm the two `_total` counters
**grow**, not reset. (See Validation Architecture → live samples.)
**Warning signs:** Cumulative total stops growing or oscillates while a single server runs.

### Pitfall 2: Model-identity drift mid-accumulation
**What goes wrong:** Totals get attributed to the wrong model if the swap path changes
`cfg.Model` between a scrape's read and the fold.
**Why it happens:** The dashboard hosts BOTH the `swapMu`-guarded model switch and the new
usage writer. A model swap rewrites config; a concurrent scrape could read the post-swap
`cfg.Model` while folding a counter sample produced under the pre-swap model.
**How to avoid:** Capture `cfg.Model` at the *same point* the scrape sample is taken, inside
the usage-write critical section, reading config once. Because a backend/model swap restarts
`llama-server`, the counter resets to 0 anyway — D-04's reset detection then correctly starts
a fresh accumulation for the new model key. The keying is per-model, so a swap simply begins
populating a different map entry. Confirm: model swap → counter reset → new model key.
**Warning signs:** A model's cumulative total jumps by a large delta right after a swap.

### Pitfall 3: Float64 precision on very large token counts
**What goes wrong:** `parsePromText` returns `float64`; token counts read through it lose
integer exactness above 2^53.
**Why it happens:** The existing parser is gauge-oriented (float rates).
**How to avoid:** 2^53 ≈ 9.0e15 tokens — unreachable in any realistic local single-user
lifetime, so reading via the existing float map is acceptable. If the planner wants a
belt-and-suspenders counts contract, add a typed `uint64` read path in the metrics extension
(Claude's discretion per CONTEXT.md). Store the cumulative total as `uint64` in `usage.json`
regardless.
**Warning signs:** Totals that are off-by-a-few at extreme magnitudes (not a practical concern).

### Pitfall 4: Re-freezing the wrong / multiple goldens
**What goes wrong:** Running `go test ./... -update` re-freezes every golden, masking
unintended contract drift elsewhere.
**Why it happens:** `-update` is a package-level flag honored by multiple golden tests.
**How to avoid:** Re-freeze the ONE affected golden surgically:
`go test ./cmd/villa -run TestStatusJSONGolden -update`. Then `git diff` it and confirm the
ONLY change is the new `"usage"` key inserted before `"schema_version"` and `schema_version: 1`→`2`.
The dashboard reads the same `Report` (handleStatus serializes `status.Run`), so it has no
separate golden to refreeze (D-10). `[VERIFIED: codebase — only cmd/villa/testdata/status.json.golden locks status.Report]`
**Warning signs:** `git diff` after `-update` touches `detect.golden.json`, `doctor.json.golden`,
`bench*.golden`, or `recommend.golden.json` — those are unrelated and must NOT change.

## Code Examples

### Confirmed counter names in the metrics extension (the ONLY new literals)
```go
// Source: official llama.cpp tools/server/README.md [CITED: github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md]
// Both confirmed declared as "Counter:" type in the README metrics table.
const (
	mPromptTokensTotal    = "llamacpp:prompt_tokens_total"
	mPredictedTokensTotal = "llamacpp:tokens_predicted_total"
)
```

### The single golden re-freeze command (D-09)
```bash
# Source: cmd/villa/status_test.go:298 (`if *update`) [VERIFIED: codebase]
go test ./cmd/villa -run TestStatusJSONGolden -update
git diff cmd/villa/testdata/status.json.golden   # confirm ONLY usage + schema_version 1→2 changed
```

### Counts-only structural field-set test (D-11, mirror metrics.Slot)
```go
// Source: internal/metrics/llamacpp_test.go:154 (Slot field-set assertion) [VERIFIED: codebase]
func TestUsageTotalsHasNoContentFields(t *testing.T) {
	allowed := map[string]bool{ /* only count/identity fields: Model, PromptTokens, PredictedTokens, LastSeen... */ }
	st := reflect.TypeOf(UsageTotals{}) // and ModelUsage{}
	for i := 0; i < st.NumField(); i++ {
		if !allowed[st.Field(i).Name] {
			t.Errorf("UsageTotals has unexpected field %q — counts-only: no prompt/response/content", st.Field(i).Name)
		}
	}
	// Also assert the marshaled JSON contains none of: "prompt", "response", "content", "text", "messages".
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| MEDIUM-confidence counter names (ROADMAP flag) | CONFIRMED `llamacpp:prompt_tokens_total` + `llamacpp:tokens_predicted_total`, both `Counter:` type | Verified 2026-06-07 vs official README | Research flag CLOSED — names are HIGH confidence |
| Assumed simple reset-on-restart | Confirmed in-memory `server_metrics`, no persistence ⇒ resets on restart; `reset_bucket` affects only the *rate gauge* timing buckets, NOT `_total` | Verified 2026-06-07 | Fold design (D-04) validated; add a live monotonic-growth sanity check |
| (no usage store) | `usage.json` under `$XDG_DATA_HOME/villa/`, full-file atomic rewrite | This phase | Net-new durable state; Phase 16 backup will capture it |

**Deprecated/outdated:**
- The KV-cache-usage ratio gauge (`llamacpp:kv_cache_usage_ratio`) is GONE from current
  llama.cpp `/metrics` (already noted in `internal/metrics/llamacpp.go` header). Do not
  reference it. `[VERIFIED: codebase comment + official README]`

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `_total` counters are monotonic across scrapes (not reset per `/metrics` call); only rate-gauge timing buckets reset | Pitfall 1 / State of the Art | LOW — fold's non-monotonicity detection (D-04) is correct either way; worst case under-count within one process lifetime. Add a live UAT sanity check to confirm growth. |
| A2 | Reading token counts through `parsePromText`'s float64 map is precision-safe for realistic lifetimes (< 2^53 tokens) | Pitfall 3 | LOW — 2^53 ≈ 9e15 tokens is unreachable single-user; planner may add a uint64 read path if desired |
| A3 | Model identity at scrape time = `cfg.Model` read in the live dashboard wiring | Pitfall 2 / D-07 | LOW — config is the single source of truth and `liveDashboardDeps` already loads it (`cfg.Model`); confirmed in codebase |

**Note:** The two ROADMAP research flags (counter names + reset semantics) are NOT in this
table because they were VERIFIED against the official llama.cpp README this session — they
are HIGH-confidence findings, not assumptions. A1 is the residual nuance worth a live check.

## Open Questions (RESOLVED)

1. **Live monotonic-growth confirmation of the two `_total` counters** — RESOLVED: deferred to Plan 04 Task 3 on-hardware UAT; non-fatal by design (absent counter degrades to typed-Unknown per D-05).
   - What we know: README declares both as `Counter:` type; in-memory struct resets on restart.
   - What's unclear: Whether the running gfx1151 image (`kyuz0/amd-strix-halo-toolboxes:vulkan-radv`)
     build emits these specific counters and grows them across scrapes (vs. resetting per scrape).
   - Recommendation: One-line UAT — scrape `/metrics` twice during a generation and assert both
     `_total` values increase. The fold degrades to typed-Unknown if absent (D-05), so even a
     missing counter is non-fatal; this check only confirms the happy path accumulates.

2. **Whether the two `_total` counters ride on `PerfSnapshot` or a sibling struct** — RESOLVED: sibling `CounterSample` struct (decided in Plan 02, reusing the same bounded scrape).
   - This is explicitly Claude's discretion (CONTEXT.md). A sibling `CounterSample` struct is
     cleaner (the existing `PerfSnapshot` is documented as "last-window gauge values"; counters
     are a different category). Recommendation: sibling struct + a sibling scrape accessor that
     reuses the SAME bounded request, OR fold both into one `ScrapeMetrics` return. Planner decides.

## Environment Availability

> This phase is code/config-only at build time. The single runtime dependency is the existing
> loopback `/metrics` endpoint already consumed by the dashboard — NO new external dependency.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| llama-server `/metrics` (loopback) | Counter scrape | ✓ (already scraped by dashboard) | n/a | Typed-Unknown fold — no accumulation, no crash (D-05) |
| `$XDG_DATA_HOME` (or `~/.local/share` / `/var/tmp` fallback) | usage.json store | ✓ (benchstore uses same) | n/a | `/var/tmp/villa/` last-resort (benchstore precedent) |
| Go 1.26.2 toolchain | Build | ✓ | 1.26.2 | — |

**Missing dependencies with no fallback:** None.
**Missing dependencies with fallback:** `/metrics` counters — absent counters degrade to
typed-Unknown (no fold, no write for that counter) by design (D-05).

## Validation Architecture

> nyquist_validation is enabled in `.planning/config.json`.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + `httptest` + golden fixtures) — no third-party assert lib |
| Config file | none — `go test` convention |
| Quick run command | `go test ./internal/usage ./internal/metrics ./internal/status` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| USAGE-01 | Reset-aware fold: monotonic delta + backward-step ⇒ whole-sample-as-new | unit | `go test ./internal/usage -run TestFoldResetAware -x` | ❌ Wave 0 |
| USAGE-01 | Per-model keying: two models accumulate independently | unit | `go test ./internal/usage -run TestFoldPerModel -x` | ❌ Wave 0 |
| USAGE-01 | Counter absent ⇒ no fold, no write for that counter (typed-Unknown, D-05) | unit | `go test ./internal/usage -run TestFoldTypedUnknown -x` | ❌ Wave 0 |
| USAGE-01 | Metrics extension surfaces both `_total` counters from a fixture | unit | `go test ./internal/metrics -run TestScrapeCountersTotal -x` | ⚠️ extend existing |
| USAGE-01 | Persist round-trip: atomic write then read returns identical totals | unit | `go test ./internal/usage -run TestStoreRoundTrip -x` | ❌ Wave 0 |
| USAGE-01 | XDG path resolver honors `$XDG_DATA_HOME` + traversal guard | unit | `go test ./internal/usage -run TestUsagePathXDG -x` | ❌ Wave 0 |
| USAGE-02 | `status.Report` carries `usage` field; absent store ⇒ omitted (omitempty) | unit | `go test ./internal/status -run TestUsageOmittedWhenAbsent -x` | ❌ Wave 0 |
| USAGE-02 | Byte-frozen `--json` golden: only `usage` + `schema_version 1→2` changed | golden | `go test ./cmd/villa -run TestStatusJSONGolden` | ✅ (re-freeze once) |
| USAGE-02 | Dashboard reads SAME `Report` field (no new endpoint) | unit | `go test ./internal/dashboard -run TestStatusUsageSurfaced -x` | ❌ Wave 0 |
| USAGE-02 (D-07) | Dashboard `/api/metrics` folds+writes under mutex (sole writer) | unit | `go test ./internal/dashboard -run TestMetricsWritesUsage -x` | ❌ Wave 0 |
| USAGE-02 (D-11) | Counts-only: `UsageTotals`/`ModelUsage` have NO content fields | security | `go test ./internal/usage -run TestUsageTotalsHasNoContentFields -x` | ❌ Wave 0 |
| USAGE-02 (D-12) | No-new-outbound: usage derives only from existing scrape; `no_telemetry` intact | structural | `go test ./internal/status -run TestNoTelemetry` + grep gate | ⚠️ extend |

### Sampling Rate
- **Per task commit:** `go test ./internal/usage ./internal/metrics ./internal/status`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + the single golden diff reviewed, BEFORE `/gsd-verify-work`.

### On-hardware UAT (live, beyond automated)
- **A1 monotonic-growth check:** during a generation, scrape `/metrics` twice; assert both
  `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total` increase.
- **Reset-aware end-to-end:** note cumulative total in `villa status`; restart `villa-llama`
  (counter resets to 0); generate again; assert `villa status` cumulative total **continues
  from the prior value** (does not drop to the new low raw count).
- **No-new-outbound:** confirm no new host/port appears (e.g. `ss -tnp` / dashboard logs) — the
  scrape target is the SAME loopback endpoint already used for live tok/s.

### Wave 0 Gaps
- [ ] `internal/usage/usage.go` + `internal/usage/usage_test.go` — net-new pure core + tests (USAGE-01, D-01/D-03/D-04/D-05/D-11)
- [ ] `internal/metrics/llamacpp_test.go` + `testdata/metrics.txt` — add the two `_total` token counters (fixture already has `n_decode_total`)
- [ ] `internal/dashboard/*_test.go` — writer-hook + sole-writer mutex test (D-07)
- [ ] `internal/status/status_test.go` — usage-omitted-when-absent + surfaced-when-present
- [ ] Re-freeze `cmd/villa/testdata/status.json.golden` ONCE (`-update`), review diff
- [ ] No framework install needed (stdlib `testing` already in use)

## Security Domain

> security_enforcement is enabled in `.planning/config.json`.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Loopback-only, single-user local; no auth surface added |
| V3 Session Management | no | No sessions |
| V4 Access Control | no | No new endpoint (D-10); existing `requireSameOrigin` unchanged |
| V5 Input Validation | yes | `_total` values parsed via existing bounded `parsePromText` (skips malformed); usage.json read fail-closed per-field (typed-Unknown, never trusted blindly) |
| V6 Cryptography | no | No crypto; counts-only data, no secrets |
| V8 Data Protection (privacy) | yes | **Counts-only** structural test (D-11); 0600 file / 0700 dir; NO prompt/response content ever stored |
| V12 File Handling | yes | XDG-confined path + `assertInsideDir` traversal guard + atomic temp+rename (mirror config/benchstore) |
| V13 API / Web Service | yes | No new API surface (D-10); `/metrics` scrape stays bounded (2s timeout, 64 KiB LimitReader) |

### Known Threat Patterns for this stack (Go control plane + local file store)

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal writing usage.json outside XDG dir | Tampering | `assertInsideDir` guard (clone config/benchstore shape, D-02) |
| Torn write under concurrent dashboard scrapes | Tampering / DoS | `usageMu` sole-writer + atomic temp+rename (D-07) |
| Prompt/response content leaking into the store | Information Disclosure | Counts-only structural field-set test (D-11) + JSON key denylist (`prompt`/`content`/`text`/`messages`) |
| New outbound exfil channel introduced | Information Disclosure | Derives ONLY from existing loopback scrape; `no_telemetry` assertion intact + no-new-host UAT (D-12) |
| Unbounded `/metrics` body exhausting memory | DoS | Reuse existing `io.LimitReader(64 KiB)` + 2s timeout (no new request) |
| World-readable usage.json exposing usage patterns | Information Disclosure | 0600 file / 0700 dir + chmod-tighten on pre-existing (mirror config) |
| Hand-edited / corrupt usage.json mis-read | Tampering | Fail-closed per-field on load; absent/garbage ⇒ typed-Unknown, never a fabricated total |

## Sources

### Primary (HIGH confidence)
- llama.cpp official server README — `github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md` — confirmed both counter names + `Counter:` type for `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total`.
- Codebase `internal/metrics/llamacpp.go` + `llamacpp_test.go` + `testdata/metrics.txt` — bounded scrape, `parsePromText`, typed-Unknown discipline, fixture already carries `n_decode_total` counter.
- Codebase `internal/status/status.go:108-135` + `cmd/villa/status_test.go:285-313` + `cmd/villa/testdata/status.json.golden` — single golden contract surface + `-update` mechanics + append-only field convention.
- Codebase `internal/dashboard/server.go:120` (`swapMu`) + `api.go:73-107` (`handleMetrics`) + `cmd/villa/dashboard.go` (`liveDashboardDeps`, `cfg.Model`, endpoint reuse).
- Codebase `internal/config/villaconfig.go:147-240` (write discipline + `assertInsideDir`) + `internal/benchstore/benchstore.go:300-331` (XDG data-dir resolver + local guard copy + `Deps` seam).
- `.planning/phases/15-cumulative-usage-tracking/15-CONTEXT.md` (D-01..D-12) + `.planning/REQUIREMENTS.md` (USAGE-01/02, Out-of-Scope) + `.planning/ROADMAP.md` Phase 15.

### Secondary (MEDIUM confidence)
- llama.cpp Discussion #10325 (rate-gauge calculation context) and Issue #5850 / Discussion #19197 (metrics endpoint + reset-bucket behavior on shared `TASK_TYPE_METRICS`) — used to characterize the `reset_bucket` quirk as applying to rate-gauge timing buckets, not the `_total` counters.

### Tertiary (LOW confidence)
- WebSearch summary asserting "metrics reset on each /metrics call" — TREATED AS the rate-gauge/bucket behavior, NOT the `_total` counters; flagged as A1 for a live confirmation. Not relied upon for the design (fold is reset-tolerant regardless).

## Metadata

**Confidence breakdown:**
- Counter names + types: HIGH — verified verbatim against official llama.cpp README.
- Reset semantics: HIGH for reset-on-restart (in-memory struct, no persistence); the per-scrape-reset nuance (A1) is the one item flagged for a trivial live check, and the fold is correct regardless.
- Architecture (pure core + seam, sole-writer, single golden bump): HIGH — every pattern has shipped precedent in this codebase (Phases 5, 10, 14).
- XDG data-dir resolver: HIGH — direct Phase 14 benchstore precedent.
- Pitfalls: HIGH — derived from codebase invariants and the verified metrics behavior.

**Research date:** 2026-06-07
**Valid until:** 2026-07-07 (stable — Go stdlib + an established codebase; llama.cpp metric names are a stable documented contract. Re-confirm A1 against the running image at phase start.)

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| USAGE-01 | Accumulate cumulative prompt + generated token counts per model locally, reset-aware (survives `llama-server` counter resets), counts-only, no new outbound | Confirmed counter names (`llamacpp:prompt_tokens_total`/`tokens_predicted_total`, Counter type) + reset-on-restart semantics → reset-aware `Fold` (Pattern 1, D-04); typed-Unknown read (Pattern 2, D-05); XDG atomic store (XDG resolver + write discipline, D-02); counts-only structural test (Code Examples, D-11) |
| USAGE-02 | `villa status` + dashboard surface cumulative usage as an append-only, schema-bumped contract change (live tok/s remains) | Single append-only `*UsageTotals omitempty` field above `SchemaVersion` + `reportSchemaVersion 1→2` + ONE golden re-freeze (Pattern 3, D-09); dashboard reads SAME `Report` field — no new endpoint (D-10); sole mutex-guarded writer in `handleMetrics` (Architecture diagram, D-07) |
</phase_requirements>
