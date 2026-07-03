# Plan 31-04 Summary — On-Hardware UAT + Final Freeze

**Plan:** 31-04 (blocking human-verify gate)
**Status:** ✅ COMPLETE — UAT PASSED (all SC met; A1 confirmed; no escalation)
**Date:** 2026-06-19 (gfx1151 dev box)
**Requirements:** GROUND-01, GROUND-02, GROUND-03, GUARD-01 (+ GUARD-05 live sanity)

## What was done

1. **Task 1 — static binary + distroless digest (Pitfall 5 cleared).** `make build-static` (CGO_ENABLED=0) → statically-linked `villa`; `villa detect` works CGO-free. Resolved `gcr.io/distroless/static-debian12@sha256:b669b9df…89400` and pinned it in `websafe.go` (placeholder removed); villa-websafe golden re-frozen. (c0bf4c1)
2. **UAT-surfaced gap fixed.** Install rendered units requiring `websafe.env` but never generated `web_loader_secret`, wrote the 0600 env file, or started `villa-websafe`. Wired the searxng-mirrored secret-env install path: `orchestrate.WriteWebsafeSecretEnv` + install generate/write/start block + Deps + test. (ca9909f, cf6806f)
3. **Task 2 — on-hardware UAT.** All success criteria PASS (see `31-UAT.md`): A1 retrieval key present (OWUI v0.9.6 `env.py:688`); static binary execs on distroless; `/load` is the sole producer of `page_content` (GUARD-01); SSRF rejects internal hosts live (GUARD-05); a real web-search query returned a grounded answer (Podman 5.8.3 / June 2026, post-cutoff) with inline citations to **live URLs** (GROUND-01); web content isolated in `open-webui_web-search` (34 pts) distinct from durable stores (GROUND-02); `villa-llama` OFFLOAD PASS under search load (GROUND-03).
4. **Task 3 — final freeze.** Un-skipped + re-froze `villa-openwebui.container.websearch.golden` against the on-hardware-confirmed render (BYPASS=False + external loader + retrieval-fix key + 0600 bearer). `make check` green.

## Deviations
- Plan anticipated freezing both goldens in Task 3; the villa-websafe digest golden was UAT-independent and frozen in Task 1 (kept the tree green); only the UAT-dependent OWUI golden was frozen in Task 3.
- A real install-wiring gap (websafe secret-env) was discovered and fixed mid-UAT — the intended purpose of the on-hardware gate.

## Residuals (non-blocking, in 31-UAT.md)
- Clean-replace cadence of the web-search collection not independently re-verified (OWUI-managed; isolation proven).
- Install secret/start block lives in the reconcile-changes path (consistent with shipped searxng); units-unchanged re-install won't re-provision a deleted env file.

## Artifacts
- `31-UAT.md` (evidence), resolved distroless digest, confirmed retrieval key, final goldens.

## Verification
`make check` green; `go test ./...` 1423+ pass. Phase 31 success criteria all satisfied on-hardware.
