# Phase 31 — On-Hardware UAT Evidence Record

**Run:** 2026-06-19, gfx1151 dev box (AMD Ryzen AI MAX+ 395 / Radeon 8060S, Fedora 44, kernel 7.0.12, rootless Podman, backend=rocm)
**Plan:** 31-04 (blocking human-verify gate)
**Verdict:** ✅ PASS — all success criteria met. A1 (the HIGH-risk unknown) CONFIRMED present. No CONTEXT escalation.
**Stack:** OWUI v0.9.6 @sha256:7f1b0a1a…ea9184e · villa-websafe distroless @sha256:b669b9df…89400 · static villa binary (CGO_ENABLED=0)

---

## Pre-UAT prep (Task 1 + UAT-surfaced gap)

- **Static binary (Pitfall 5):** `make build-static` (`CGO_ENABLED=0`) → `file ./villa` = "statically linked"; `villa detect` runs correctly CGO-free (CPU/128 GiB/gfx1151/Vulkan/ROCm/GTT all detected — no typed-Unknowns). No alpine fallback needed. (commit c0bf4c1)
- **Distroless digest:** `podman pull gcr.io/distroless/static-debian12:nonroot` → RepoDigest `@sha256:b669b9df05a88a085fefed6520c6d2268aabacf3008b149ddf877e752ae89400`, pinned in `internal/orchestrate/websafe.go` (placeholder removed); villa-websafe golden re-frozen. (commit c0bf4c1)
- **UAT-surfaced gap (FIXED):** `villa install` rendered the villa-websafe + OWUI units that REQUIRE `EnvironmentFile=…/websafe/websafe.env` but never generated `web_loader_secret`, never wrote the 0600 env file, and never started `villa-websafe.service` → service stayed inactive (required EnvironmentFile missing). Fixed by mirroring the searxng secret-env install path: `orchestrate.WriteWebsafeSecretEnv` host writer + install-time generate-secret/write-env/start-service block + Deps wiring + test. (commits ca9909f, cf6806f). After the fix + redeploy: `started villa-websafe.service`, `websafe.env` present at mode 0600, `web_loader_secret` persisted.
  - **Note (consistent with shipped searxng):** the install secret/start block lives in the reconcile-changes path, so a units-unchanged re-install does not re-provision a missing env file (same behavior as searxng, Phase 29). A normal single-pass opt-in writes units + secret together. Recovery for an inconsistent state: force unit re-render.

---

## Success Criteria

| SC | Requirement | Result | Evidence |
|----|-------------|--------|----------|
| **A1** | Retrieval-fix key exists at the pinned OWUI digest (blocking) | ✅ PASS | `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS` at `/app/backend/open_webui/env.py:688`, wired in `retrieval/utils.py:1103,1146`; OWUI v0.9.6. `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` at config.py:1541. |
| **Pitfall 5** | Static binary execs on distroless | ✅ PASS | `podman logs villa-websafe` → `villa websafe-serve listening on http://0.0.0.0:8090 (container-internal)` — no "no such file"/glibc error. |
| **GUARD-01** | villa-websafe is the sole producer of `page_content` | ✅ PASS | `POST /load {"urls":["https://example.com"]}` (bearer) → HTTP 200, 1 item, `metadata.source=https://example.com`, `title="Example Domain"`, `page_content` len 284 (active markup stripped). |
| **GUARD-05 / SC4** | SSRF guard rejects internal targets | ✅ PASS | `POST /load` with `http://169.254.169.254/latest/meta-data/` + `http://villa-qdrant:6333/collections` → HTTP 200, **0 items** (both rejected, skip-and-continue; no internal host fetched). Unit tests are the authority; this is the live sanity pass. |
| **SC1 / GROUND-01** | Grounded answer with inline citations to live URLs | ✅ PASS | Web-search chat (BYPASS=False) Q: latest Podman version. Answer: **"Podman 5.8.3, released June 12 2026"** + CVE-2026-44517 detail (both AFTER the model's Jan-2026 cutoff → necessarily grounded), inline cites `[2,4]`. `sources` includes live URLs: `https://github.com/podman-container-tools/podman/releases`, `http://podman.io/`, `https://versionlog.com/podman/`. |
| **SC2 / GROUND-02** | Dedicated ephemeral collection, never the durable store | ✅ PASS (isolation) | Qdrant after query: `open-webui_web-search`=34 pts (web), distinct from durable `open-webui_memories`=0, `open-webui_knowledge`=15, `open-webui_files`=12. Web content never entered the durable stores. **Clean-replace cadence:** OWUI-managed (villa adds no Qdrant calls, Pitfall 2); not independently re-verified this session (2nd-query harness hit transient HTTP 400s) — residual note, isolation requirement met. |
| **SC3 / GROUND-03** | Offload-asserted residency under search load | ✅ PASS | `villa status` during an in-flight web-search query: `villa-llama.service active ready OFFLOAD PASS` (backend rocm) — chat model GPU-resident, no silent/partial CPU fallback. |

---

## Final freeze (Task 3)

- OWUI search-ON golden `villa-openwebui.container.websearch.golden` **re-frozen** against on-hardware-confirmed render (BYPASS=False + WEB_LOADER_ENGINE=external + EXTERNAL_WEB_LOADER_URL + ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True + 0600 EnvironmentFile bearer); `TestRenderOpenWebUIWebSearchContainerGolden` un-skipped and passing.
- villa-websafe golden carries the resolved distroless digest.
- `make check` green; full suite 1423+ tests pass.

## Residuals (non-blocking, recorded honestly)

1. **Clean-replace cadence of `open-webui_web-search`** not independently re-verified (OWUI-managed; isolation from durable proven). Candidate for a follow-up probe.
2. **Install idempotency:** a units-unchanged re-install won't re-provision a deleted `websafe.env` (consistent with shipped searxng pattern; normal opt-in unaffected).
3. **Bearer auth** is now generated; defense-in-depth over the already-private villa.network (no host port).
