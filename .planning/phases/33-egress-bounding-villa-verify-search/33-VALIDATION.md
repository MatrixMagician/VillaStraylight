---
phase: 33
slug: egress-bounding-villa-verify-search
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-19
---

# Phase 33 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`) |
| **Config file** | none — Go modules; `Makefile` targets |
| **Quick run command** | `go test ./cmd/villa/... -run VerifySearch` |
| **Full suite command** | `make check` (vet + `go test ./...`, incl. `-race`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/villa/... -run VerifySearch`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 33-verdict | 01 | 1 | PRIV-08 | T-33-* | pure inverse-framing verdict core: canary reachable-unguarded→blocked-under-bound; ineffective/unroutable block ⇒ REJECT (distinct from FAIL), never fabricated PASS | unit | `go test ./cmd/villa/... -run VerifySearch` | ❌ W0 | ⬜ pending |
| 33-families-bc | 01 | 1 | PRIV-08 | T-33-* | in-process assertions: planted-injection page → stripped+fenced+flagged (websafe Page.Verdict); SSRF internal-host rejected (ssrf.go); secret-in-query exfil case | unit | `go test ./cmd/villa/... -run VerifySearch` | ❌ W0 | ⬜ pending |
| 33-json | 01 | 1 | PRIV-08 | — | byte-frozen `--json` contract for `verify search` (append-only golden) | golden | `go test ./cmd/villa/... -run VerifySearch -update` (freeze), then assert | ❌ W0 | ⬜ pending |
| 33-priv-regress | 02 | 2 | PRIV-07, PRIV-09 | — | regression assertions: web-search-OFF render byte-identical to v1.4 (no config/env change); OWUI outbound-kill env present (HF_HUB_OFFLINE + telemetry kills) in both goldens — DO NOT re-add/duplicate | unit/golden | `go test ./internal/orchestrate/... -run 'TelemetryFrozen\|RenderOpenWebUI'` | ✅ (existing goldens) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red. Task IDs finalized by the planner.*
*Critical: per RESEARCH, do NOT modify the OWUI env block or VillaConfig schema — PRIV-07/PRIV-09 are already shipped; this phase PROVES them, it does not re-implement them.*

---

## Wave 0 Requirements

- [ ] `cmd/villa/verify_search.go` + `verify_search_test.go` — 3-state verdict core + inverse-framing unit tests
- [ ] `cmd/villa/testdata/` — `verify search --json` golden fixture
- [ ] No framework install needed — `go test` built in

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The live rootless-netns + nft egress bound actually blocks an off-allowlist canary while leaving SearXNG/websafe reachable, with guaranteed teardown | PRIV-08 | requires the live Strix Halo host (`unshare -rn` / `nft` / podman netns); the negative control must run where egress genuinely flows (loopback-only netns trivially "blocks" for the wrong reason) | On-hardware bound-mechanics task: run `villa verify search`; confirm PASS with a routable allowlist + canary-reachable-unguarded control; confirm an unroutable/ineffective bound yields REJECT (exit 2), not PASS; confirm netns is torn down |
| Web search under the bound makes no HuggingFace pull (weights pre-staged: `nomic-embed-text-v1.5`) | PRIV-09 | requires live OWUI + network observation | Enable web search, run a query, confirm no outbound HF pull beyond SearXNG upstreams + result fetches |

*The pure verdict core + families (b)/(c) have automated unit coverage; the live netns bound mechanics are the on-hardware human verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies (live netns mechanics flagged manual/on-hardware)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
