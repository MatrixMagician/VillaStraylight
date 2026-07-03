---
status: complete
phase: 29-searxng-search-service
source: [29-VERIFICATION.md]
started: 2026-06-18T00:00:00Z
updated: 2026-06-18T22:23:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Live SearXNG `format=json` readiness round-trip + no-host-port surface (SC#2)
expected: |
  After `make build`, with `web_search_enabled=true`, `villa install` prints
  `search service ready: real format=json query returned N result(s)` (N>=1) from the live
  `format=json` round-trip; `ss -ltnp` / `podman port villa-searxng` show NO host port
  (container-DNS-only on `villa.network`); the FAIL path refuses-with-remediation and exits non-zero.
result: pass
evidence: |
  On the live gfx1151 host (2026-06-18), `make build` (v1.4-36-gca4a09d-dirty) →
  `web_search_enabled = true` in config.toml → `./villa install` (exit 0) printed:
    "search service ready: real format=json query returned 10 result(s)"
  N=10 >= 1 — the live `liveSearxngProof` format=json round-trip over villa.network, never a health-200.
  No-host-port surface confirmed: `podman port villa-searxng` empty (no published ports);
  `podman inspect villa-searxng` HostConfig.PortBindings = {} (container-DNS-only on `villa` network);
  the `127.0.0.1:8080` host listener in `ss -ltnp` belongs to villa-llama (inference endpoint),
  not searxng (searxng's `8080/tcp` in `podman ps` is the exposed/unpublished container port).
  FAIL/refuse-with-remediation path not exercised on the happy path (proof logic is unit-tested
  fail-closed off-hardware per 29-VERIFICATION.md); the live happy-path readiness is the item confirmed here.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]
