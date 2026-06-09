---
phase: 19-vector-store-local-embeddings-services
verified: 2026-06-09T00:00:00Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Literal reboot durability re-confirmation of SC#2"
    expected: "After `sudo reboot` (or a full host reboot) on the Fedora AMD Strix Halo dev box with memory_enabled=true and linger enabled, `systemctl --user is-active villa-qdrant.service` is active and a previously-written Qdrant collection/point is still present on the named :Z volume."
    why_human: "Requires an actual host reboot; deliberately deferred during 19-03 because it would terminate the operator's live session. Durability MECHANISM (durable named :Z volume + loginctl Linger=yes) is complete and proxy-proven (data survived `podman rm` + re-run), but the literal reboot re-confirmation is the one outstanding manual step."
---

# Phase 19: Vector Store + Local Embeddings Services Verification Report

**Phase Goal:** `villa install` brings up a local Qdrant vector DB and a local OpenAI-compatible embeddings endpoint as rootless Podman Quadlet managed services on `villa.network`, container-DNS only, with durable storage and the embedding model pre-staged so nothing is downloaded at runtime.
**Verified:** 2026-06-09
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (Success Criteria)

| #    | Truth (SC) | Status | Evidence |
| ---- | ---------- | ------ | -------- |
| SC#1 | memory on → install renders+starts villa-qdrant (digest-pinned, named `:Z` volume, no host port) + dedicated embeddings llama-server `/v1/embeddings` (pinned toolbox image, container-DNS), both regenerated from config | ✓ VERIFIED | Goldens `villa-qdrant.container` (digest `b79aaa49ce…`, `Volume=villa-qdrant.volume:/qdrant/storage:Z`, no `PublishPort=`), `villa-embed.container` (`Exec=llama-server … --embeddings --pooling mean … --host 0.0.0.0 --port 8080`, no host port). `render.go:160-185` appends the 3 memory units only when `MemoryEnabled`. **WR-01 fix is genuine:** `buildQdrantView(mv.QdrantAddr)` / `buildEmbedView(…, mv.EmbedAddr, mv.EmbedPort)` thread config (`memory.RenderView(in.Cfg)`) into the units; `TestRenderMemoryUnitsAreConfigDriven` (memory_test.go:171) proves custom DNS names + `--port 9090` flow through and the hardcoded `8080` does NOT survive. On-hardware (19-03): both services `is-active`, container-DNS only. |
| SC#2 | Qdrant store survives reboot (durable named volume + lingering) + Qdrant writable (no rootless UID / SELinux `:Z` failure) | ✓ VERIFIED (with documented caveat) | Durable named volume rendered (`villa-qdrant.volume`, `Driver=local`) + `:Z` PRIVATE label mount; image is the `-unprivileged` variant running USER_ID=1000 (writable rationale, memory.go:21-29). On-hardware (19-03): Qdrant wrote `/qdrant/storage/{collections,aliases,raft_state.json}` as UID 1000 on the named `:Z` volume; `loginctl Linger=yes` enabled; data survived `podman rm` + re-run (durability proxy). **Caveat:** the literal `sudo reboot` re-confirmation was deliberately deferred (would end the operator's session) — mechanism complete, literal reboot pending (see Human Verification). |
| SC#3 | embedding model present + served at install; first embedding request succeeds offline (zero runtime HF/model pull) | ✓ VERIFIED | `nomicEmbedShard` (install_memory.go:54) pinned size 146146432 + SHA256 `3e243421…544c3b7`; step-6b pre-stage (install.go:412-419) pulls only when absent via verified `download.PullModel` (size+SHA256+atomic rename, T-19-06). The served `-m` path binds `EmbedGGUFFilename()` single source (Pitfall 3; `TestEmbedGGUFFilenameSingleSource`). Readiness proof (install.go:519-542) asserts a 768-dim `/v1/embeddings` vector + Qdrant writable round-trip, refuses-with-remediation on FAIL (`evalMemoryProof`, no WARN tier). On-hardware (19-03): 768-length vector confirmed offline with `--network none`; GGUF pre-staged at install, zero runtime download. |
| SC#4 | neither service bound beyond loopback / `villa.network`; loopback-only privacy audit stays green | ✓ VERIFIED | `grep PublishPort` over both templates + both container goldens + memory.go → zero matches. `--host 0.0.0.0` is container-internal only (no host bind). `TestMemoryUnitsNoPublishPort` (memory_test.go:104) locks it. Proof reachability is container-DNS over `villa.network` (`podman run --network villa`, no host port — T-19-11). The existing loopback/privacy audit (`TestStatusPrivacyAssertion`, dashboard loopback tests) is reused unchanged and green. On-hardware (19-03): `podman ps`/`podman port` show no host bind on either service. |

**Score:** 4/4 truths verified (SC#2 PASS-with-documented-caveat: durability mechanism complete, literal reboot pending).

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/orchestrate/memory.go` | digest-pinned image consts, view builders, single-source GGUF accessor | ✓ VERIFIED | Substantive (206 lines); pinned qdrant/embed digests, `EmbedGGUFFilename()`, `buildQdrantView`/`buildEmbedView` thread config; no host port; fixed-token `buildEmbedExec` (no shell interp). |
| `internal/orchestrate/render.go` | conditional memory append from config | ✓ VERIFIED | `if in.Cfg.MemoryEnabled` appends 3 units; `mv := memory.RenderView(in.Cfg)` result is now genuinely consumed (WR-01 fix). |
| `internal/orchestrate/quadlet/{qdrant.container,qdrant.volume,embed.container}.tmpl` | 3 Quadlet templates, no host port | ✓ VERIFIED | Auto-discovered by `//go:embed quadlet/*.tmpl`; render the 3 byte-frozen goldens. |
| `internal/orchestrate/testdata/villa-{qdrant.container,qdrant.volume,embed.container}.golden` | byte-frozen unit contracts | ✓ VERIFIED | Digest-pinned, `:Z` volume, no `PublishPort=`, correct embed Exec. 8 goldens byte-identical (5 pre-existing untouched + 3 new). |
| `cmd/villa/install_memory.go` | pre-stage shard, proof core + live seams, persisted-gate seams | ✓ VERIFIED | Substantive (342 lines); `nomicEmbedShard`, `evalMemoryProof` pure core, `liveMemoryProof` fixed-arg podman, `liveLoadedMemoryEnabled` (persisted gate), IN-03 size-integrity guard, WR-03 idempotent Qdrant probe. |
| `cmd/villa/install.go` | install lifecycle wiring (gate, pre-stage, start, proof) | ✓ VERIFIED | Persisted-config seed (WR-02), step-6b pre-stage, step-9b qdrant-then-embed start gated on plan-unit presence (WR-04), step-10b proof refusing-with-remediation. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `render.go` | unit text DNS/port | `memory.RenderView(in.Cfg)` → `buildQdrantView/buildEmbedView` | ✓ WIRED | Config values reach rendered units (WR-01 fixed; `TestRenderMemoryUnitsAreConfigDriven`). |
| install proof (`install.go:526-532`) | same config source | `cfg.EmbedAddr/EmbedPort/QdrantAddr/QdrantPort` | ✓ WIRED | Proof probes the SAME `cfg` fields the units render from — no proof/unit divergence (the WR-01 latent-divergence risk is closed). |
| pre-stage filename | served `-m` path | `nomicEmbedShard.Filename == orchestrate.EmbedGGUFFilename()` | ✓ WIRED | `TestEmbedGGUFFilenameSingleSource` asserts equality unconditionally (Pitfall 3). |
| install start gate | rendered plan | `planHasUnit(plan, QdrantContainerUnitName/EmbedContainerUnitName)` | ✓ WIRED | Starts gated on unit presence in plan, not just config flag (WR-04). |
| memory gate | persisted config | `cfg.MemoryEnabled = d.loadedMemoryEnabled()` → `config.LoadVilla().MemoryEnabled` | ✓ WIRED | Authoritative gate, not the always-false seed (T-19-16; `TestInstallMemoryGateUsesPersistedConfig`). |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| memory units | DNS name / port | `cfg` via `memory.RenderView` | Yes (custom values render through; on-hardware install rendered + started both) | ✓ FLOWING |
| embed `/v1/embeddings` | 768-dim vector | pre-staged GGUF served by llama-server | Yes (on-hardware 768-length confirmed offline `--network none`) | ✓ FLOWING |
| Qdrant storage | collections/raft_state | named `:Z` volume, UID 1000 | Yes (on-hardware wrote storage files; survived recreation) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| memory + install + memory-core suites | `go test ./internal/orchestrate/ ./cmd/villa/ ./internal/memory/ -count=1` | 365 passed (3 packages) | ✓ PASS |
| seam grep gate (no backend-literal leak) | `go test ./internal/inference/ -run TestSeamGrepGate` | PASS | ✓ PASS |
| no host port in memory units/templates/goldens | `grep -rn PublishPort <tmpls+goldens+memory.go>` | 0 matches (exit 1) | ✓ PASS |
| live install bring-up (memory on) | on-hardware 19-03 (`villa install --no-tui`) | proof green "768-dim embeddings + Qdrant writable"; both services active, no host port | ✓ PASS (operator-recorded) |
| first embedding offline | on-hardware 19-03 `--network none` | 768-length vector | ✓ PASS (operator-recorded) |
| reboot durability re-confirmation | `sudo reboot` | deferred (would end operator session) | ? SKIP → human |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| INFRA-01 | 19-01, 19-03 | local Qdrant rootless Quadlet on villa.network, digest-pinned, named `:Z` volume, no host port | ✓ SATISFIED | qdrant golden + memory.go; on-hardware official manifest-list digest confirmed (SC#1/SC#4). |
| INFRA-02 | 19-01, 19-02, 19-03 | local embeddings llama-server `/v1/embeddings`, pinned toolbox image, container-DNS, pinned default model | ✓ SATISFIED | embed golden + buildEmbedExec + nomic pre-stage; on-hardware 768-dim served (SC#1/SC#3). |
| PRIV-04 | 19-02, 19-03 | no runtime model download; embedding model pre-staged at install | ✓ SATISFIED | step-6b verified pre-stage + offline proof; on-hardware zero runtime pull, `--network none` success (SC#3). |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No `TODO(19-03)`/`FIXME`/`XXX` in phase files (the digest-pin TODO was removed in c4ee842/IN-01) | ℹ️ Info | None — `grep -c 'TODO(19-03)'` = 0; render.go/memory.go pure; orchestrate remains the only impure module. |

### Human Verification Required

#### 1. Literal reboot durability re-confirmation of SC#2

**Test:** On the Fedora AMD Strix Halo dev box with `memory_enabled=true` and `loginctl Linger=yes`, write a Qdrant collection/point, then `sudo reboot`. After boot, run `systemctl --user is-active villa-qdrant.service` and re-query the collection.
**Expected:** `villa-qdrant.service` is active without a manual login, and the previously-written collection/point is still present on the named `:Z` volume.
**Why human:** Requires an actual host reboot; deferred during 19-03 to avoid ending the operator's live session. The durability mechanism (durable named `:Z` volume + lingering enabled) is complete and proxy-proven (data survived `podman rm` + re-run), but the literal reboot re-confirmation is the single outstanding manual step.

### Gaps Summary

No blocking gaps. All 4 success criteria are satisfied and all 3 requirements (INFRA-01, INFRA-02, PRIV-04) are covered with concrete codebase + on-hardware evidence. The four code-review warnings (WR-01 config-driven render, WR-02 persisted-config seed, WR-03 idempotent Qdrant probe, WR-04 plan-gated starts) and three info items are all resolved and verified in the code (not merely claimed): in particular the WR-01 fix is locked by `TestRenderMemoryUnitsAreConfigDriven`, closing the proof/unit divergence risk. The seam gate stays green (no backend-literal leak from the new managed-service consts), no host port is published, and orchestrate remains the only impure module. The sole outstanding item is the deliberately-deferred literal `sudo reboot` re-confirmation of SC#2 durability — the mechanism is complete and proxy-proven, so this is routed to human verification rather than counted as a gap.

---

_Verified: 2026-06-09_
_Verifier: Claude (gsd-verifier)_
