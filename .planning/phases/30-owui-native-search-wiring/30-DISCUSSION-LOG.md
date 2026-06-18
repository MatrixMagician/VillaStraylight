# Phase 30: OWUI Native-Search Wiring - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-18
**Phase:** 30-owui-native-search-wiring
**Mode:** `--auto` (autonomous; recommended option selected for every gray area)
**Areas discussed:** Result-count exposure, ENABLE_PERSISTENT_CONFIG single-emit, No-results honesty scope, SEARXNG_QUERY_URL composition, Drift-test & golden scope

---

## Result-count exposure & default

| Option | Description | Selected |
|--------|-------------|----------|
| New `WebSearchResultCount` config field, default 3, omitzero | Operator tunes via config.toml; omit-when-off keeps disk byte-identical | ✓ |
| Hardcoded constant in orchestrate | Simpler, but no operator tuning (violates SRCH-03 "tune result count") | |
| Persistent-config UI tuning only | Conflicts with `ENABLE_PERSISTENT_CONFIG=False` (env is authoritative) | |

**Choice:** New config field, default 3, with the existing omit-when-off marshal discipline.
**Notes:** Default 3 keeps context budget conservative ahead of Phase 31's ctx-budget reservation.

---

## ENABLE_PERSISTENT_CONFIG=False single-emit

| Option | Description | Selected |
|--------|-------------|----------|
| Hoist to single trailing emit when memory OR web-search on | One authoritative `=False`, always last, never duplicated/dropped | ✓ |
| Emit inside each block independently | Risks duplication when both blocks on / drop when only web-search on | |

**Choice:** Single authoritative trailing emit gated on (memory OR web-search) enabled.
**Notes:** Load-bearing — without it OWUI ignores env after first boot (Phase-20 D-03 / T-20-01 rationale).

---

## No-results honesty scope

| Option | Description | Selected |
|--------|-------------|----------|
| Rely on OWUI native RAG-injection behavior; verify by UAT; harden in Phase 31 | No villa prompt/guard now; honesty proven by UAT; grounding+citations in Phase 31 | ✓ |
| Add a villa system-prompt / fabrication-guard env in Phase 30 | Pulls Phase-31/32 grounding work forward; scope creep | |

**Choice:** Native behavior + UAT assertion; defer grounding hardening to Phase 31, injection screening to Phase 32.

---

## SEARXNG_QUERY_URL composition

| Option | Description | Selected |
|--------|-------------|----------|
| Compose from cfg.SearxngAddr/SearxngPort + `&format=json`, never re-typed | Config single-source; keeps seam gate green; matches Phase-29 JSON proof | ✓ |
| Re-typed literal URL in openwebui.go | Violates no-re-typed-host-literal discipline | |

**Choice:** `http://{cfg.SearxngAddr}:{cfg.SearxngPort}/search?q=<query>&format=json` composed via fmt.

---

## Drift-test & golden scope (SC#4)

| Option | Description | Selected |
|--------|-------------|----------|
| New websearch container golden + key-binding drift test over orchestrate accessors | Mirrors memory golden; env-name churn fails build by construction | ✓ |
| Golden only, no drift test | Misses SC#4 "env-name churn caught by construction" | |

**Choice:** New `villa-openwebui.container.websearch.golden` + a drift test binding each web-search env key to its orchestrate accessor/constant. Search-off render proven byte-identical via existing negative-render test.

---

## Claude's Discretion

- Exact `buildOpenWebUIView` signature shape (extra params vs small web-search input struct).
- Trailing-append helper vs explicit ordered assembly for the single `ENABLE_PERSISTENT_CONFIG=False`.
- Placeholder-key / omission conventions follow existing `openwebui.go` patterns.

## Deferred Ideas

- Grounded fetch → embed grounding, ephemeral RAG collection, ctx-budget reservation, SSRF guard → **Phase 31**.
- Injection-guard sanitization (Unicode normalization, provenance-fencing, heuristic classifier) → **Phase 32**.
