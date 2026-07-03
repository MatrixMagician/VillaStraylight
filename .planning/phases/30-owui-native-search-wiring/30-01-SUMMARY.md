---
phase: 30-owui-native-search-wiring
plan: 01
subsystem: config
tags: [config, web-search, owui, toml, byte-identical-off]
requires: []
provides:
  - "VillaConfig.WebSearchResultCount int field (toml:web_search_result_count,omitzero)"
  - "Default 3 (single home of the literal in defaultConfig())"
  - "Zero -> default self-heal in normalizeVilla()"
  - "Omit-when-off zeroing in marshalVilla() !WebSearchEnabled block"
affects:
  - "Plan 02 (orchestrate OWUI render) threads in.Cfg.WebSearchResultCount into WEB_SEARCH_RESULT_COUNT"
tech-stack:
  added: []
  patterns:
    - "SearxngPort 4-site omit-when-off discipline (struct/default/self-heal/marshal-omit)"
    - ",omitzero (not ,omitempty) for zero-int drop (BurntSushi/toml precedent)"
key-files:
  created: []
  modified:
    - internal/config/villaconfig.go
    - internal/config/villaconfig_test.go
decisions:
  - "D-05: WEB_SEARCH_RESULT_COUNT operator knob lives in config; default 3 (conservative ctx budget ahead of Phase 31)"
metrics:
  duration: ~7m
  completed: 2026-06-18
  tasks: 2
  files: 2
requirements: [SRCH-03]
status: complete
---

# Phase 30 Plan 01: WebSearchResultCount Config Field Summary

Added the operator-tunable `WebSearchResultCount int` field to `VillaConfig`
(D-05, default 3, maps to OWUI's `WEB_SEARCH_RESULT_COUNT`), wired across all four
lifecycle sites following the established `SearxngPort` omit-when-off discipline
exactly, with extended config tests proving default / self-heal / omit-when-off /
round-trip.

## What Was Built

- **Struct field** (`internal/config/villaconfig.go`): `WebSearchResultCount int`
  with tag `toml:"web_search_result_count,omitzero"`, placed in the v1.5 web-search
  cluster immediately after `SearxngSecret`. `,omitzero` (not `,omitempty`) so a
  zero int is dropped from disk — the byte-identical-off guarantee depends on it
  (mirrors `SearxngPort` / `qdrant_port` / `embed_port`).
- **`defaultConfig()`**: `WebSearchResultCount: 3` — the SINGLE home of the literal.
- **`normalizeVilla()`**: `if cfg.WebSearchResultCount == 0 { cfg.WebSearchResultCount = d.WebSearchResultCount }`,
  placed within the web-search self-heal region (mirrors the `SearxngPort == 0` heal).
- **`marshalVilla()`**: `c.WebSearchResultCount = 0` inside the existing
  `if !c.WebSearchEnabled { ... }` block so the key is dropped from disk when web
  search is off (T-30-01-CFG / PRIV-07 byte-identical-off).
- **Tests** (`internal/config/villaconfig_test.go`): extended the three existing
  web-search tests in place (no duplicate parallel file):
  - `TestDefaultConfigWebSearchFields` — asserts default `== 3`.
  - `TestWebSearchSaveOmitsKeysWhenDisabled` — added `web_search_result_count` to the
    `webKeys` list (absent-when-off + present-when-on), set count `= 7` on the ON
    config and assert `web_search_result_count = 7` persists (round-trip when on).
  - `TestWebSearchNormalizeSelfHeal` — asserts zero self-heals to 3.

The memory/coder field blocks were not touched, and no default literal was re-typed
anywhere (every site derives from `defaultConfig()`).

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Add WebSearchResultCount field across four lifecycle sites | fffee06 | internal/config/villaconfig.go |
| 2 | Extend config tests (default/self-heal/omit/round-trip) | 0b242ae | internal/config/villaconfig_test.go |

## Verification

- `go build ./internal/config/` — success.
- `go vet ./internal/config/` — clean.
- `go build ./...` — whole module builds (additive field breaks no consumer).
- `go test ./internal/config/` — 25 passed.
  - Field exists with exact tag `toml:"web_search_result_count,omitzero"`.
  - `defaultConfig().WebSearchResultCount == 3`.
  - `normalizeVilla(VillaConfig{WebSearchEnabled:true}).WebSearchResultCount == 3`.
  - `marshalVilla` zeroes the field when `!WebSearchEnabled` (key absent from off TOML).
  - Non-default value (7) round-trips when web search is ON.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestSaveLoadRoundTrip broken by the schema extension**
- **Found during:** Task 2 (full-package test run after extending tests).
- **Issue:** `TestSaveLoadRoundTrip` constructs a full `want` struct with no
  `WebSearchResultCount` (so 0). With `WebSearchEnabled:false`, `marshalVilla`
  drops the key; on load `normalizeVilla` heals 0 -> 3, so `got.WebSearchResultCount
  (3) != want.WebSearchResultCount (0)` — the full-literal equality assertion failed.
- **Fix:** Seeded `WebSearchResultCount: 3` (the inert default) into the `want`
  struct, exactly as the test already does for the memory and web-search fields
  (its existing convention — see the `MemoryEnabled`/`SearxngAddr` inert-default
  comments in the same struct). This is the documented "populate inert defaults so
  the full-literal equality survives the schema extension" pattern.
- **Files modified:** internal/config/villaconfig_test.go
- **Commit:** 0b242ae

> Note: the plan's `<read_first>` references `30-PATTERNS.md`, which does not exist
> in the phase directory. The plan inlined the relevant `SearxngPort` 4-site analog
> and field-comment guidance in its `<read_first>`/`<action>`, and the full source
> file was available, so this did not block execution.

## Known Stubs

None.

## Threat Flags

None — config-only change, no new network/auth/file surface. T-30-01-CFG
(info-disclosure of the new key when web search is off) is mitigated exactly as
planned via the `marshalVilla` `!WebSearchEnabled` zeroing.

## Self-Check: PASSED

- FOUND: internal/config/villaconfig.go (modified, WebSearchResultCount at 4 sites)
- FOUND: internal/config/villaconfig_test.go (modified, extended coverage)
- FOUND commit: fffee06 (feat — field)
- FOUND commit: 0b242ae (test — coverage)
