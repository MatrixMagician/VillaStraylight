---
phase: 19-vector-store-local-embeddings-services
reviewed: 2026-06-09T00:00:00Z
depth: deep
files_reviewed: 13
files_reviewed_list:
  - cmd/villa/install.go
  - cmd/villa/install_memory.go
  - cmd/villa/install_test.go
  - internal/inference/seam_test.go
  - internal/orchestrate/memory.go
  - internal/orchestrate/memory_test.go
  - internal/orchestrate/render.go
  - internal/orchestrate/quadlet/qdrant.container.tmpl
  - internal/orchestrate/quadlet/qdrant.volume.tmpl
  - internal/orchestrate/quadlet/embed.container.tmpl
  - internal/orchestrate/testdata/villa-qdrant.container.golden
  - internal/orchestrate/testdata/villa-qdrant.volume.golden
  - internal/orchestrate/testdata/villa-embed.container.golden
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 19: Code Review Report

**Reviewed:** 2026-06-09
**Depth:** deep
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Phase 19 wires two new managed Quadlet services (Qdrant + villa-embed) into `villa install`, gated on the persisted `memory_enabled`. The core security and invariant posture is sound: the seam allowlist extension is correctly scoped, no `PublishPort=` leaks into the memory units, the GGUF pre-stage rides the existing SHA256+size-verified, path-traversal-guarded `download.PullModel`, the readiness proof is fixed-arg `exec.Command` (no shell interpolation), and `render.go`/`memory.go` stay pure. The memory gate correctly binds the persisted config rather than the always-false seed (T-19-16).

The defects are correctness/maintainability gaps, not security holes. The most important one is that the new memory units do NOT actually render from config — they render entirely from orchestrate-local constants, and the `memory.RenderView(in.Cfg)` "handoff" that is supposed to make the render config-driven discards its result. This breaks "config is the single source of truth" for the memory stack and creates a latent divergence between what the proof probes (config-resolved values) and what the units actually expose (hardcoded consts). Two further warnings cover a flaky-FAIL idempotency gap in the Qdrant probe and an unhandled `daemon-reload`/start gap for the new services.

No structural findings block was provided.

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: Memory units render from hardcoded constants, not config — the `RenderView` "handoff" is discarded dead code

**File:** `internal/orchestrate/render.go:155`, `internal/orchestrate/memory.go:130-174`
**Issue:**
`render.go:155` calls `_ = memory.RenderView(in.Cfg)` and throws the result away. `memory.RenderView` is a pure value-mapper (confirmed at `internal/memory/memory.go:95`) with no side effects, so this line is a no-op. The actual units are built by `buildQdrantView()` / `buildEmbedView(embedGGUFFilename)`, which read orchestrate-local constants exclusively (`qdrantContainerName = "villa-qdrant"`, `embedContainerPort = 8080`, `embedContextLen = 8192`, `qdrantVolumeMount`, the hardcoded `embedGGUFFilename`). None of `in.Cfg`'s memory fields (`QdrantAddr`, `QdrantPort`, `EmbedAddr`, `EmbedPort`, `EmbeddingDim`, `EmbeddingModel`) reach the rendered unit text.

Consequences:
1. The render comment ("the gate is keyed off `in.Cfg.MemoryEnabled` so the handoff is real") is misleading — only the on/off gate is config-driven; every other memory value is a constant. "Config is the single source of truth" (a hard project invariant) does not hold for the memory stack.
2. The container-DNS name / port the **proof** probes comes from `cfg` (`install.go:498-505`), while the unit the proof is validating exposes the orchestrate consts. They agree today only because both currently resolve to the same seed defaults (6333/8080/villa-qdrant/villa-embed). Any future divergence (config edit, or a defaults change in one place but not the other) silently breaks the proof or the wiring with no test catching it.

**Fix:** Either thread the `RenderView` result into `buildQdrantView`/`buildEmbedView` so the units derive their DNS name/port/dim from config (making the handoff genuine), or — if the values are intentionally pinned constants for v1.3 — delete the `_ = memory.RenderView(in.Cfg)` line and drop the "handoff is real" comment, and add a test asserting the rendered unit's port matches `cfg.QdrantPort`/`cfg.EmbedPort` so the constant-vs-config agreement is locked. Do not leave a discarded pure call masquerading as a data-flow.

### WR-02: Install overwrites any persisted memory-config customization with seed defaults on every run

**File:** `cmd/villa/install.go:326-337`, `cmd/villa/install.go:413`
**Issue:**
`runInstall` seeds `cfg := config.DefaultVillaConfig()` and overrides only `Model/Quant/Ctx/Backend/MemoryEnabled`. The memory address/port/dim/model fields are left at the seed defaults and are never read back from the persisted config (only `MemoryEnabled` is, via `loadedMemoryEnabled()`). `saveConfig(cfg)` at line 413 then persists this seed-default cfg, so a user who edited `qdrant_port`, `embed_port`, `embedding_model`, or the DNS addresses in `config.toml` has those values silently reset to defaults by `villa install`. This is the same class of "the persisted memory spine is not honored" problem as WR-01, on the write side.

**Fix:** Load the persisted config once (`config.LoadVilla()`), and override only the recommendation-derived fields (`Model/Quant/Ctx/Backend`) and `MemoryEnabled`, preserving the user's persisted memory fields — instead of starting from `DefaultVillaConfig()`. If memory fields are deliberately non-customizable in v1.3, document that and have the proof/render read the same single source so they cannot diverge from WR-01.

### WR-03: Qdrant writable-proof can FAIL spuriously if a prior `villa-probe` collection survives

**File:** `cmd/villa/install_memory.go:235-261`
**Issue:**
`qdrantProbe` does PUT-create then best-effort DELETE of `villa-probe`. The DELETE is explicitly best-effort (`_, _ = runProbeCurl(... "DELETE" ...)`) and the create is not guarded against a pre-existing collection. If a previous proof run created the collection and then crashed / was interrupted / the DELETE failed (network blip, container restart), the next `villa install` issues `PUT /collections/villa-probe` against an already-existing collection. Qdrant returns a non-2xx for a conflicting create, `curl -sf` exits non-zero, `qdrantProbe` returns an error, `evalMemoryProof` yields `StatusFail`, and `runInstall` returns `exitBlocked` (install refuses) — even though Qdrant is perfectly writable. This turns a transient leftover into a hard, confusing install block.

**Fix:** Make the probe idempotent: DELETE the probe collection before the PUT (ignore the delete result), or treat a "collection already exists" response as a successful writability proof. Best:
```go
// delete any stale probe collection first (idempotent), then create.
_, _ = runProbeCurl(ctx, helperImage, "-sf", "-X", "DELETE", coll)
if _, err := runProbeCurl(ctx, helperImage, "-sf", "-X", "PUT", coll, ...); err != nil {
    return false, fmt.Errorf("create probe collection: %w", err)
}
```

### WR-04: New memory services start without a daemon-reload after their units are written

**File:** `cmd/villa/install.go:446-485`
**Issue:**
On the write path install does `writeUnits(plan, ...)` → `daemonReload()` → start llama → start owui → (memory on) start qdrant → start embed. The single `daemonReload()` at line 451 happens before the memory starts, so that is fine **as long as the qdrant/embed `.container` units are part of `plan.Changed`**. They are, because `Render` appends them when `MemoryEnabled` and `reconcile` diffs them in. However, there is no guard that `start(qdrantServiceName)` is only attempted when the unit was actually rendered/written — the start is gated purely on `cfg.MemoryEnabled`. If a future change ever lets `MemoryEnabled` be true while the memory units are filtered out of the plan (e.g. a partial render error swallowed, or a reconcile that drops them), install would `systemctl start villa-qdrant.service` for a unit systemd has never seen, surfacing a raw "Unit not found" as an `exitBlocked` with no actionable remediation. The gate for *starting* a service should be "its unit exists in the written plan", not just the config flag.

**Fix:** Gate the memory-service starts on the presence of `villa-qdrant.container` / `villa-embed.container` in `plan.Changed`∪`plan.Unchanged` (i.e. the units that were actually rendered), or assert post-render that memory-on implies the three memory units are present and block with a clear internal-error message otherwise. At minimum add a test that memory-on + a render that omits the memory units fails closed with a clear message rather than a bare systemd error.

## Info

### IN-01: Stale TODO in a digest-pinned production constant

**File:** `internal/orchestrate/memory.go:29`
**Issue:** `// TODO(19-03): confirm dev-box RepoDigest matches this manifest-list digest.` The phase context states the digest was on-hardware-confirmed during 19-03, so this TODO is stale and now misleads a future reader into thinking the pin is unverified.
**Fix:** Remove the TODO (and tighten the comment to state the digest is verified), since the verification it asks for has been performed.

### IN-02: Proof success message duplicates the verdict detail string as a literal

**File:** `cmd/villa/install.go:510` vs `cmd/villa/install_memory.go:178`
**Issue:** `install.go:510` prints the literal `"memory stack ready (768-dim embeddings + Qdrant writable)"` while `evalMemoryProof` already returns `detail: "768-dim embeddings + Qdrant writable"` on PASS. The "768-dim" figure is hardcoded in two places and is not sourced from `cfg.EmbeddingDim`, so a dimension change would leave these strings stale.
**Fix:** Print `proof.detail` on PASS instead of re-typing the message, or interpolate `cfg.EmbeddingDim` into both.
**Severity:** Info (cosmetic / drift risk only; the load-bearing dim check is in `evalMemoryProof`).

### IN-03: `embedModelPresent` presence check trusts mere existence, not integrity

**File:** `cmd/villa/install_memory.go:83-86`
**Issue:** `liveEmbedModelPresent` returns true on a successful `os.Stat` of the GGUF path — a present-but-truncated/corrupt file (e.g. a leftover from a kill between rename steps elsewhere, or manual tampering) is treated as "present, never re-pulled", and `villa-embed` would then crash-loop on a bad weight. The chat-model `modelDownloaded` seam has the identical property, so this is consistent with existing behavior, not a regression — noted for completeness. The atomic-rename in `download.PullModel` means a *villa-written* file is always complete, so the realistic exposure is only external tampering.
**Fix:** (Optional) treat the file as present only if its size matches `nomicEmbedShard.SizeBytes`; full re-hash on every install would be wasteful and is not warranted.

## Invariants verified (no finding)

- **Seam allowlist scoping (seam_test.go:92-103):** the `isSeam` extension admits only the exact path `orchestrate/memory.go` for Walk 1 (internal/). It does not widen the cmd/villa walk (Walk 2 has no seam files) and does not relax any pattern. A backend-image leak in any other orchestrate file still trips the gate. Correctly scoped.
- **No published host port:** `qdrant.container.tmpl`, `embed.container.tmpl`, and all three goldens carry no `PublishPort=`; the embed Exec binds `--host 0.0.0.0` container-internally only. `TestMemoryUnitsNoPublishPort` locks it. PRIV-01/SC#4 upheld.
- **No shell interpolation:** `buildEmbedExec` joins fixed tokens; `runProbeCurl` is a fixed-arg `exec.CommandContext("podman", ...)`; the JSON body is `json.Marshal`'d and passed as a single `-d` arg; `embedAddr`/model id are passed as discrete args, never shell-expanded. T-19-10 upheld.
- **GGUF pre-stage integrity:** `liveEnsureEmbedModel` → `download.PullModel` performs HEAD verify → stream → size check → SHA256 check → atomic rename, with path-confinement (`assertInsideDir`) and a bare-filename guard (`download.go:75-83`). `nomicEmbedShard` is a flat filename, so traversal is impossible. Idempotent via `embedModelPresent`.
- **Memory gate binds persisted config:** `cfg.MemoryEnabled = d.loadedMemoryEnabled()` (→ `config.LoadVilla().MemoryEnabled`, fail-soft false), not the seed. T-19-16 upheld; covered by `TestInstallMemoryGateUsesPersistedConfig`.
- **Byte-frozen contracts:** `TestRenderByteIdenticalWhenMemoryOff` proves the 5 pre-existing units are unchanged when memory is off; the 3 new goldens match the templates.
- **Proof refuses-with-remediation:** a FAIL verdict returns `exitBlocked` with a remediation detail (no silent skip / false-green). `TestInstallMemoryProofFail` locks it.
- **QDRANT_API_KEY:** the qdrant unit carries no `Environment=` block (asserted by `TestRenderQdrant`); nothing logs a secret.

---

_Reviewed: 2026-06-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
