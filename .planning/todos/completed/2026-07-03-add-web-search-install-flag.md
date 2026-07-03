---
created: 2026-07-03T18:16:30.680Z
title: Add a --web-search install flag to enable web search
area: general
files:
  - cmd/villa/install.go:335
  - cmd/villa/install.go:492
  - cmd/villa/config.go:187
  - internal/config/villaconfig.go:134
---

## Problem

v1.5 shipped web-search grounding but with **no supported way to turn it on** short of
hand-editing `config.toml`. The install path only reads the *already-persisted* gate:

- `cmd/villa/install.go:492` — `cfg.WebSearchEnabled = d.loadedWebSearchEnabled()` reads the
  persisted `web_search_enabled` bool; there is no flag that sets it.
- `cmd/villa/install.go:335` registers `--coding-agent` (the v1.4 addon) but there is **no
  `--web-search` flag** alongside it.
- `cmd/villa/config.go:187` — `config set` only accepts `model` / `quant` / `backend` /
  `catalog_path` / `ctx`; `web_search_enabled` is not a settable key.
- There is no TUI wizard screen for web search either.
- `internal/config/villaconfig.go:134` — `WebSearchEnabled` is a deliberate bool that
  `normalizeVilla` never self-heals ON, so nothing flips it to true automatically.

Net: the only way an operator can enable the shipped v1.5 feature is to manually write
`web_search_enabled = true` into `~/.config/villa/config.toml`, which contradicts the
"config is written by villa, not hand-edited" ergonomics of every other addon.

Surfaced 2026-07-03 while writing the v1.5 README (PR #7), which had to document the
hand-edit path rather than a clean command.

## Solution

Mirror the v1.4 `--coding-agent` flag pattern:

1. Add a `--web-search` BoolVar to `villa install` (`cmd/villa/install.go`, next to
   `--coding-agent` at line 335) that persists `web_search_enabled=true` before the gate is
   read at line 492 — same shape as how `--coding-agent` persists `agent_enabled`.
2. (Optional) Add `web_search_enabled` (and maybe `web_search_result_count`) as a
   `config set` key in `cmd/villa/config.go` so it can be toggled without an install.
3. (Optional) Add a web-search screen to the guided TUI wizard, consistent with the
   memory/coding-agent screens.

Keep **default-OFF / byte-identical-off** intact: absent the flag, nothing changes and the
render stays identical to v1.4. Add/extend the install test that asserts the flag persists
the gate (mirror the coding-agent install-flag test). Update the README web-search section
(currently on the `docs/v1.5-readme` branch) once the flag exists.

Good fit for `/gsd-quick`.
