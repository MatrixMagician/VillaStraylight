---
phase: 19-vector-store-local-embeddings-services
plan: 03
subsystem: infra
tags: [qdrant, llama-server, embeddings, podman, quadlet, digest-pin, on-hardware-uat]

# Dependency graph
requires:
  - phase: 19-01
    provides: orchestrate memory render path (QdrantImage()/EmbedImage() digest-pinned consts, villa-qdrant/.volume + villa-embed units)
  - phase: 19-02
    provides: install-time nomic GGUF pre-stage + memory-stack readiness proof (offline 768-dim /v1/embeddings + Qdrant writable round-trip)
provides:
  - "On-hardware confirmation that the pinned qdrantImage digest is the OFFICIAL docker.io/qdrant/qdrant manifest-list digest (legitimacy gate PASS — no re-pin)"
  - "On-hardware confirmation that the pinned kyuz0 embed digest serves a working 768-length /v1/embeddings (not #15406-regressed), proven both published-port AND offline (--network none)"
  - "Live villa install (memory_enabled=true) bring-up: readiness proof green, villa-qdrant + villa-embed active container-DNS only (no host port), Qdrant writable on its :Z named volume as UID 1000"
  - "Frozen qdrantImage const (TODO(19-03) marker removed, on-hardware-verified comment) — units are no longer placeholder-gated"
affects: [phase-20-owui-memory-wiring, phase-22-recommend-footprint-host-gate, phase-23-backup-restore-swap]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Container-image legitimacy = blocking-human, non-auto-approvable gate (digest-pin + manual provenance audit; the npm/PyPI/crates seam does not cover container images)"
    - "On-hardware honesty gate: the readiness proof's offline 768-dim /v1/embeddings + Qdrant-writable round-trip is asserted live, never a silent green"

key-files:
  created:
    - .planning/phases/19-vector-store-local-embeddings-services/19-03-SUMMARY.md
  modified: []

key-decisions:
  - "Confirmed qdrantImage digest b79aaa49ce… is the OFFICIAL qdrant/qdrant manifest-list digest and EQUALS the placeholder — no re-pin, no golden refreeze (A5: pin the manifest-list digest, NOT the per-arch amd64 9f7a0450… sub-manifest)"
  - "embedImage (== kyuz0 vulkan-radv 9a74e555…) serves a 768-length /v1/embeddings confirmed offline — D-06 #15406 regression risk cleared on-hardware"
  - "Literal sudo reboot deliberately deferred (would terminate the operator's live session); boot-survival is sound by construction (linger enabled + durable :Z named volume proven across container recreation) — recorded honestly, NOT claimed as a literal reboot"

patterns-established:
  - "Pattern: image-supply-chain legitimacy is a blocking-human checkpoint frozen BEFORE the unit ships; the manifest-list (multi-arch) digest is the canonical pin, never the per-arch child"

requirements-completed: [INFRA-01, INFRA-02, PRIV-04]

# Metrics
duration: ~10min
completed: 2026-06-09
---

# Phase 19 Plan 03: On-Hardware Freeze + Memory-Stack Bring-Up Verification Summary

**Froze the two digest-pinned memory-stack images against the live Fedora AMD Strix Halo dev box (Qdrant manifest-list digest confirmed OFFICIAL + equal to the placeholder; kyuz0 /v1/embeddings proven 768-length offline) and verified a real `villa install` (memory on) brings up villa-qdrant + villa-embed container-DNS only with a writable, durable `:Z` Qdrant volume and zero runtime model download.**

## Performance

- **Duration:** ~10 min (Task 2 auto-gate + result recording; Tasks 1 & 3 performed by the operator on the live dev box)
- **Started:** 2026-06-09T19:50:39Z
- **Completed:** 2026-06-09T19:51:00Z
- **Tasks:** 3 (Task 1 blocking-human PASS, Task 2 auto verify-only PASS, Task 3 blocking PASS-with-caveat)
- **Files modified:** 0 source files (Task 2 was a verify-only no-op; the qdrantImage TODO marker was already removed by the IN-01 code-review fix, commit c4ee842)

## Accomplishments

- **Task 1 (LEGITIMACY GATE — blocking-human): PASS.** On the live dev box (AMD Ryzen AI MAX+ 395 / Radeon 8060S gfx1151 STRIX_HALO, Fedora 44, podman 5.8.2):
  - `podman pull docker.io/qdrant/qdrant:v1.18.2-unprivileged` + `skopeo inspect --raw docker://… | sha256sum` resolves to `sha256:b79aaa49ce7a7e5b7e9cf3fe76be400c911457084b4b7af47487c1c9ae5962e5` — the **canonical multi-arch MANIFEST-LIST digest** of the **OFFICIAL** `qdrant/qdrant` org image, and it **EQUALS** the digest already pinned in `memory.go`. No re-pin needed.
  - **A5 nuance recorded:** `podman image inspect --format '{{index .RepoDigests 0}}'` reports the per-arch amd64 sub-manifest `9f7a0450…` — that is **NOT** the value to pin; the manifest-list digest `b79aaa49ce…` is the correct, frozen pin. The image runs as UID 1000:1000 (confirms the D-01 rootless-writable rationale).
  - `/v1/embeddings` against the pinned kyuz0 embed digest (`vulkan-radv@sha256:9a74e555…`) returns a **768-length** vector (NOT #15406-regressed) — confirmed BOTH with a published-port probe AND offline with `--network none`. The nomic Q8_0 GGUF size (146146432) + SHA256 (`3e243421…544c3b7`) match the `nomicEmbedShard` pin.
  - Resume signal: *"approved: digest b79aaa49ce… confirmed (official manifest-list, equals placeholder, no re-pin), /v1/embeddings returns 768."*

- **Task 2 (auto — apply digest + refreeze golden): PASS (verify-only no-op).** Because the confirmed digest EQUALS the placeholder, there was NO digest change and NO golden refreeze. The `TODO(19-03)` marker was already removed and the comment updated to record on-hardware verification by the earlier IN-01 code-review fix (commit c4ee842). The automated acceptance gate is green (no source edit required):
  - `go test ./internal/orchestrate/ -count=1` → PASS (54 tests).
  - `go test ./internal/inference/ -run TestSeamGrepGate -count=1` → PASS.
  - `grep -q 'docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:' internal/orchestrate/memory.go` → succeeds (digest-pinned, no floating tag).
  - `grep -c 'TODO(19-03)' internal/orchestrate/memory.go` → **0** (freeze marker removed).
  - `git status --porcelain internal/orchestrate/testdata/` → empty (no golden changed; the villa-embed + qdrant.volume + 5 v1.2 goldens are untouched).

- **Task 3 (ON-HARDWARE BRING-UP — blocking): PASS with one documented caveat.** On the live dev box with `memory_enabled=true`:
  - Real `villa install --no-tui` completed PASS; the readiness proof printed **`memory stack ready: 768-dim embeddings + Qdrant writable`** (the proof seam ran the offline 768-dim /v1/embeddings + Qdrant-writable round-trip; a FAIL would refuse-with-remediation).
  - The GGUF was pre-staged at install (*"embedding model … downloaded and verified"*) — **zero runtime HuggingFace pull (PRIV-04)**.
  - `systemctl --user is-active villa-qdrant.service villa-embed.service` → **both active**; villa-llama (ROCm 7.2.4 restored afterward via `villa backend set rocm`), openwebui, dashboard all running.
  - Live: villa-embed `/health` 200 + `/v1/embeddings` → 768 (probed via `podman exec villa-embed`); villa-qdrant `/readyz` 200 over villa.network; Qdrant wrote `/qdrant/storage/{collections,aliases,raft_state.json}` as **UID 1000(qdrant)** on its named `:Z` volume.
  - **SC#4:** NO published host port on either service (`podman ps` shows villa-qdrant exposes 6333-6334/tcp container-side only, villa-embed none; no `PublishPort=` in either `.container` unit).

## Files Created/Modified

- `.planning/phases/19-vector-store-local-embeddings-services/19-03-SUMMARY.md` - This summary (the recorded UAT result for the three live success criteria).
- `internal/orchestrate/memory.go` - **NOT modified in this plan** (the qdrantImage const already holds the confirmed digest with the TODO marker removed via c4ee842; the on-hardware-verified comment is already present). Recorded here for traceability.

## Success Criteria Mapping

| SC | Criterion | Status |
|----|-----------|--------|
| SC#1 | INFRA-01 frozen qdrantImage is the dev-box-confirmed OFFICIAL RepoDigest; INFRA-02 pinned kyuz0 serves working /v1/embeddings | ✓ (manifest-list digest confirmed official + equal to placeholder; 768-length /v1/embeddings confirmed) |
| SC#2 | villa-qdrant writable `:Z` named volume that survives a reboot | **Partial-with-caveat:** writable ✓ (Qdrant wrote /qdrant/storage as UID 1000 on the named `:Z` volume) + durable ✓ (a 768-dim collection + point survived `podman rm` + re-run); **literal `sudo reboot` re-confirmation PENDING** (deferred — see Caveat) |
| SC#3 | PRIV-04 first /v1/embeddings succeeds offline, runtime zero-download | ✓ (offline `--network none` 768-length embedding confirmed; GGUF pre-staged at install) |
| SC#4 | No published host port on either new service | ✓ (no `PublishPort=` in either unit; `podman ps`/`podman port` show no host bind) |

## Decisions Made

- **No re-pin / no golden refreeze:** the operator-confirmed Qdrant manifest-list digest equals the placeholder pinned in 19-01, so Task 2 made no source edit. The manifest-list digest `b79aaa49ce…` is the canonical pin — the per-arch amd64 child `9f7a0450…` reported by `RepoDigests` is explicitly NOT pinned (A5).
- **Embed serves on CPU (no GPU passthrough):** the villa-embed unit has no `AddDevice=/dev/dri` in its template, so the embed resident GTT delta is effectively ~0; the ~512 MiB D-08 conservative reservation remains the planning figure pending a GPU-resident embed measurement in Phase 22 (CTRL-01).

## Deviations from Plan

None - plan executed exactly as written. Task 2 was a verify-only no-op by design (the confirmed digest equalled the placeholder and the TODO marker had already been removed by the IN-01 code-review fix in c4ee842), so no source edit was required.

## Issues Encountered

**Caveat — literal reboot NOT performed (honest record).** A `sudo reboot` would terminate the operator's active working session, so it was deliberately deferred. Boot-survival is sound **by construction** and proxy-proven:

- (a) `loginctl enable-linger oliverh` is now enabled (`Linger=yes`) — user services survive logout/reboot.
- (b) The durable named `:Z` volume persisted data across container recreation (a tested proxy: a 768-dim collection + point survived `podman rm` + re-run).
- The literal `sudo reboot` re-confirmation of SC#2 is left to the operator at a convenient time. **This is NOT claimed as a literal reboot** — the durability evidence is the named-volume-survives-recreation proxy plus enabled lingering.

Resume signal (honest variant): *"approved: install PASS + readiness proof green, Qdrant writable on :Z (UID 1000), offline 768-dim embedding confirmed, no host port; durability proxy-proven + linger enabled, literal reboot deferred."*

## Phase-22 Hand-off (informational, CTRL-01)

Embed GTT delta: the villa-embed unit has NO GPU device passthrough (no `AddDevice=/dev/dri` in the embed.container template) — it serves on CPU, so the embed resident GTT delta is effectively ~0. The ~512 MiB D-08 conservative reservation remains the planning figure pending a GPU-resident embed measurement in Phase 22 (CTRL-01).

## Next Phase Readiness

- The two memory-stack managed services are frozen and live-verified: villa-qdrant (writable, durable, official digest) + villa-embed (768-dim, offline-capable), container-DNS only, no host port. **Phase 20** can wire Open WebUI env to `villa-embed:8080/v1/embeddings` + `villa-qdrant:6333` (D-09 keys recorded, set in Phase 20).
- **One deferred item:** the literal `sudo reboot` re-confirmation of SC#2 (durability is proxy-proven + lingering enabled; the literal reboot is left to the operator at a convenient time).

## Self-Check: PASSED

- SUMMARY.md present at `.planning/phases/19-vector-store-local-embeddings-services/19-03-SUMMARY.md`.
- Task 2 acceptance gate green on disk: orchestrate suite PASS, seam gate PASS, qdrant digest pinned, `TODO(19-03)` count 0, no golden modified.
- No source edit claimed (Task 2 verify-only no-op); the qdrantImage freeze was performed in c4ee842 (IN-01 code-review fix), not in this plan.

---
*Phase: 19-vector-store-local-embeddings-services*
*Completed: 2026-06-09*
