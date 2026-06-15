---
slug: install-reverts-rocm-to-vulkan
status: resolved
resolved: 2026-06-15
fix_commit: 4424f13
trigger: "villa install (including --coding-agent) recomputes recommend.Pick with no overrides and overwrites a persisted backend=rocm opt-in back to vulkan (cmd/villa/install.go:432 cfg.Backend = rec.Backend). Config-is-source-of-truth violation. Confirmed on-hardware twice (Phase 27-04 and Phase 28 UAT 2026-06-15)."
created: 2026-06-15
updated: 2026-06-15
---

# Debug: `villa install` reverts a persisted `backend=rocm` to Vulkan

## Symptoms

- **Expected:** Re-running `villa install` (including `villa install --coding-agent`) on a box
  with a persisted `backend = "rocm"` opt-in preserves that backend — a re-install is a true
  no-op on the ROCm choice (config is the single source of truth for `backend`).
- **Actual:** `runInstall` recomputes `rec := d.pick(profile, recommend.Overrides{})` (no backend
  override) and then `cfg.Backend = rec.Backend` (cmd/villa/install.go:432). `recommend.Pick`
  always defaults to Vulkan (ROCm is strictly opt-in, never auto-recommended), so the persisted
  `backend=rocm` is silently overwritten to `vulkan`, the `villa-llama` unit is re-rendered to the
  vulkan-radv image, and `config.toml` persists `backend = "vulkan"`.
- **Error messages:** None — silent. This is a correctness / config-is-source-of-truth violation,
  not a crash.
- **Timeline:** Present since `villa install` gained the recompute-and-persist path. Reproduced
  on-hardware in Phase 27 Plan 04 acceptance and again during Phase 28 UAT (2026-06-15, gfx1151):
  `villa install --coding-agent` flipped `backend rocm→vulkan`; restored via `villa backend set rocm`.
- **Reproduction:** On a host with `backend = "rocm"` persisted (villa-llama on ROCm), run
  `villa install` or `villa install --coding-agent`. Observe the rendered `villa-llama.container`
  switch to `...amd-strix-halo-toolboxes:vulkan-radv...` and `config.toml` rewrite to
  `backend = "vulkan"`. `villa install --dry-run --coding-agent` shows the flip without writing.

## Key references

- `cmd/villa/install.go:319` — `rec := d.pick(profile, recommend.Overrides{})` (no backend override)
- `cmd/villa/install.go:401` — `rec = d.pick(profile, recommend.Overrides{Model: res.modelOverride})` (TUI path, also no backend override)
- `cmd/villa/install.go:432` — `cfg.Backend = rec.Backend` (the clobber)
- `internal/recommend` — `Pick` defaults backend to Vulkan; ROCm is opt-in only (never auto-recommended). Check `recommend.Overrides` for a `Backend` field.
- `internal/config/villaconfig.go` — `VillaConfig.Backend` (default `vulkan`; `rocm` opt-in); `LoadVilla` returns the persisted value.
- Anti-pattern to honor: "Config is the single source of truth" + "Backend literals seam-locked" (CLAUDE.md). The expected fix preserves a persisted non-default backend across re-install (carry `cfg.Backend` into `recommend.Overrides`, or guard the `cfg.Backend = rec.Backend` assignment when a valid persisted backend exists). Add a regression test: install on a `backend=rocm` config renders the ROCm unit and a re-install is a no-op (`plan.Changed == 0`).

## Current Focus

hypothesis: CONFIRMED — `runInstall` unconditionally clobbers the persisted `cfg.Backend`
(seeded from `d.loadedConfig()` at install.go:428) with the fresh recommendation's
always-vulkan `rec.Backend` (install.go:432). `recommend.Overrides` has NO Backend field,
and `recommend.Pick` defaults Backend to `vulkan` (ROCm never auto-recommended, REC-04), so
a persisted `backend=rocm` opt-in is silently overwritten and the ROCm unit is re-rendered to
the vulkan-radv image.

reasoning_checkpoint:
  hypothesis: "install.go:432 `cfg.Backend = rec.Backend` overwrites the persisted ROCm opt-in because rec.Backend is always vulkan (recommend.Pick defaults to vulkan; ROCm is opt-in only, never in catalog BackendDefault)."
  confirming_evidence:
    - "recommend.Overrides{Model,Quant,Ctx} has NO Backend field (recommend.go:136-140) — the pick can never carry a persisted opt-in."
    - "recommend.Pick / buildRecommendation default Backend to defaultBackend=vulkan unless catalog m.BackendDefault is set; rocm is opt-in only (REC-04, recommend.go:25,407-410)."
    - "cfg is seeded from the PERSISTED config at install.go:428 (d.loadedConfig()), so cfg.Backend already holds 'rocm' immediately before line 432 overwrites it."
    - "the rendered unit's backend is resolved from cfg.Backend at install.go:468 (inference.BackendFor) → a clobbered cfg.Backend re-renders the vulkan-radv image."
  falsification_test: "If install on a backend=rocm persisted config still rendered the ROCm unit and re-install was a no-op WITHOUT a guard, the hypothesis would be wrong. The new regression test asserts the opposite holds only WITH the fix."
  fix_rationale: "Guard the assignment at the cmd tier (the single source of truth for the persisted value) so a valid persisted ROCm-family opt-in (inference.IsROCmFamily) is PRESERVED, while a vulkan/empty/unknown persisted value still takes the recommendation. This honors config-is-source-of-truth, never auto-recommends ROCm, and adds no backend image/marker literal to cmd/villa (IsROCmFamily holds only NAME strings — seam-clean per its own doc comment)."
  blind_spots: "Catalog entries with a non-vulkan BackendDefault would also be 'recommended' — but rocm is never a catalog BackendDefault (REC-04), so the guard scoping to IsROCmFamily preserved-value is correct. A persisted unknown backend string must NOT be silently preserved (fail-closed at BackendFor render time stays intact)."
next_action: AWAITING human verification on the live gfx1151 box — confirm `villa install --coding-agent` on a persisted backend=rocm box no longer flips to vulkan (config.toml stays backend=rocm, villa-llama.container keeps the rocm image, dry-run shows no flip). On "confirmed fixed": commit fix atomically and archive the session.

## Evidence

- timestamp: 2026-06-15
  checked: cmd/villa/install.go:310-468 (runInstall recompute + seed + clobber + render)
  found: cfg is seeded from the persisted config via d.loadedConfig() at :428 (so cfg.Backend already = persisted 'rocm'); then :432 `cfg.Backend = rec.Backend` overwrites it; the rendered unit's backend is resolved from cfg.Backend at :468 via inference.BackendFor.
  implication: the clobber is the single mutation point; guarding it at :432 fixes both the flag path (:319) and the TUI path (:401) because both converge on this one assignment.

- timestamp: 2026-06-15
  checked: internal/recommend/recommend.go (Overrides struct, Pick, buildRecommendation, defaultBackend)
  found: Overrides has only {Model, Quant, Ctx} — no Backend field (:136-140). Pick/buildRecommendation default Backend to defaultBackend="vulkan" unless catalog m.BackendDefault is set (:25, :407-410); ROCm is opt-in only and never auto-selected (REC-04, package doc).
  implication: rec.Backend is structurally incapable of carrying a persisted ROCm opt-in. Threading a Backend override through Pick would change a byte-frozen pure-core contract; guarding at the cmd tier is the minimal, correct fix.

- timestamp: 2026-06-15
  checked: internal/inference/backend.go (BackendFor, IsROCmFamily)
  found: IsROCmFamily(name) is the SINGLE enumerator of ROCm-family backend NAME strings ("rocm","rocm-6.4.4","rocm-6.4.4-rocwmma") and its doc explicitly states it holds only NAME strings, never an image literal, so it stays seam-clean (no TestSeamGrepGate concern).
  implication: IsROCmFamily is the exact, seam-clean predicate for "the persisted backend is a valid non-default opt-in worth preserving" — usable in cmd/villa without tripping the seam grep gate.

## Eliminated

- hypothesis: "Add a Backend field to recommend.Overrides and thread it through Pick so the pick echoes the persisted backend."
  evidence: recommend.Pick is a byte-frozen pure core (golden-tested Recommendation contract) whose documented invariant is 'default to vulkan, ROCm opt-in only, advice never changes Backend' (REC-04, T-10-06). Threading a backend override muddies that invariant and risks the recommend goldens. The cmd tier already holds the authoritative persisted value, so the guard belongs there.
  timestamp: 2026-06-15

## Resolution

root_cause: cmd/villa/install.go:432 unconditionally assigns `cfg.Backend = rec.Backend`. cfg.Backend was just seeded from the persisted config (:428) but rec.Backend is always vulkan (recommend.Pick defaults to vulkan; ROCm is opt-in only and never auto-recommended — recommend.Overrides has no Backend field). The render path resolves the unit backend from cfg.Backend (:468), so a persisted backend=rocm is silently reverted to vulkan and the villa-llama unit is re-rendered to the vulkan-radv image on every re-install (including villa install --coding-agent).
fix: Guarded the clobber at cmd/villa/install.go:432. Replaced the unconditional `cfg.Backend = rec.Backend` with `if !inference.IsROCmFamily(cfg.Backend) { cfg.Backend = rec.Backend }`. cfg.Backend is seeded from the persisted config (d.loadedConfig(), :428), so a valid persisted ROCm-family opt-in ("rocm"/"rocm-6.4.4"/"rocm-6.4.4-rocwmma") is PRESERVED while an empty/vulkan/unknown persisted value still takes the recommendation (vulkan default; fail-closed inference.BackendFor still guards an unknown string at render time, :468). Fix is at the single mutation point, so it covers BOTH the flag pick path (:319) and the TUI pick path (:401). No backend image/marker literal added to cmd/villa — IsROCmFamily is a NAME-string predicate (seam-clean per its own doc), so TestSeamGrepGate stays green. Vulkan remains the default; ROCm is never auto-recommended — the guard only PRESERVES an already-persisted opt-in.
verification: make check (go vet ./... + go test ./...) ALL PASS; go test ./internal/inference/ -run TestSeamGrepGate PASS; make lint (go vet fallback) clean. New regression test TestInstallPreservesPersistedROCmBackend (cmd/villa/install_test.go) covers 4 cases — all pass: (1) flag-path install on a backend=rocm config renders the ROCm unit (renderedInput.Backend.Name()=="rocm") and persists backend=rocm; (2) `villa install --coding-agent` (the reported on-hardware repro) likewise preserves rocm; (3) a re-install on a backend=rocm config + Unchanged plan is a true no-op (writeCalls==0, no villa-llama.service restart); (4) a persisted vulkan default still takes the recommendation (guard preserves only opt-ins, never auto-selects ROCm). ON-HARDWARE CONFIRMED (2026-06-15, gfx1151): `villa install --coding-agent --dry-run` on the persisted backend=rocm box renders `Description=...ROCm 7.2.4 (HIP)` + `Image=...rocm-7.2.4...` — NO vulkan-radv flip (pre-fix the same dry-run rendered Vulkan RADV). Committed 4424f13.
files_changed:
  - cmd/villa/install.go (guarded cfg.Backend assignment at the seed-from-persisted-config block)
  - cmd/villa/install_test.go (added TestInstallPreservesPersistedROCmBackend regression test)
