---
status: testing
phase: 29-searxng-search-service
source: [29-VERIFICATION.md]
started: 2026-06-18T00:00:00Z
updated: 2026-06-18T00:00:00Z
---

## Current Test

number: 1
name: Live SearXNG `format=json` readiness round-trip + no-host-port surface (SC#2)
expected: |
  On the live gfx1151 host, after `make build`, set `web_search_enabled=true` and run `villa install`.
  - The SearXNG readiness step prints `search service ready: real format=json query returned N result(s)` with N>=1 (the live `liveSearxngProof` round-trip over `villa.network`, never a health-200).
  - `ss -ltnp` and `podman port villa-searxng` confirm NO host port is published (container-DNS-only).
  - A FAIL must print the refuse-with-remediation message and exit non-zero — never a false-green.
awaiting: user response

## Tests

### 1. Live SearXNG `format=json` readiness round-trip + no-host-port surface (SC#2)
expected: |
  After `make build`, with `web_search_enabled=true`, `villa install` prints
  `search service ready: real format=json query returned N result(s)` (N>=1) from the live
  `format=json` round-trip; `ss -ltnp` / `podman port villa-searxng` show NO host port
  (container-DNS-only on `villa.network`); the FAIL path refuses-with-remediation and exits non-zero.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
