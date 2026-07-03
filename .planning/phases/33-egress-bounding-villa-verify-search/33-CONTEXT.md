# Phase 33: Egress-Bounding + `villa verify search` - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning
**Mode:** Smart discuss (autonomous) — all 4 grey areas accepted as recommended

<domain>
## Phase Boundary

Deliver `villa verify search` — an operator-runnable proof that web-search outbound is bounded — plus the egress-bounding scaffolding it asserts on. Web search is opt-in / default-OFF; with it off the install renders byte-identical to v1.4. The proof is negative-control-first and inverse-framed under a real rootless-netns nft block, and OWUI's lazy/background outbound (HuggingFace pulls, telemetry) is killed so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches.

Requirements: PRIV-07, PRIV-08, PRIV-09. Depends on Phase 32 (the proof asserts on the guard's stripped + fenced + flagged output).

**In scope:** the `verify search` command + verdicts; the verify-time transient nft bound + canary/allowlist proof; the OWUI outbound-kill env (gated on web-search-on); web-search opt-in config + byte-identical-when-off.
**Out of scope (deferred):** a persistent runtime egress firewall (this phase's bound is verify-time-only); Phase 34 surfacing (`status`/dashboard/`doctor`/`backup` reflecting web search).
</domain>

<decisions>
## Implementation Decisions

### Area 1 — `villa verify search` command surface & output
- Implement as a `verify search` subcommand, mirroring the existing `verify agent` / `verify memory` family (`cmd/villa/verify_agent.go`, `verify_memory.go`).
- Output: human-readable table + a byte-frozen `--json` contract (append-only + schema-bump; golden-tested), consistent with the other verify commands.
- Verdict taxonomy is **PASS / FAIL / REJECT** — an ineffective/ineffectual nft block is a distinct **REJECT** (honest infra-fail), NEVER a fabricated PASS (SC2).
- Exit codes: 0 = PASS; distinct nonzero codes for FAIL vs REJECT (mirrors the verify-agent exit-22 honest-infra-fail convention).

### Area 2 — Opt-in toggle & egress-bound enforcement
- Web search opt-in lives in config as `web_search.enabled`, default **false** (SC1: off ⇒ install renders byte-identical to v1.4, zero-outbound posture unchanged).
- The nft egress bound is **verify-time-only**: the proof constructs a transient rootless-netns nft block, runs assertions, and tears it down. A persistent runtime egress firewall is explicitly **deferred** (not this phase).
- The allowlist is derived from the sanctioned outbound set — SearXNG upstream engines + villa-websafe result-page fetch. The canary is an off-allowlist host.
- Inverse framing (locked by SC2, easy to get backwards): the off-allowlist canary is reachable **UNGUARDED** (negative control must pass first), then blocked **UNDER the bound**. If the canary is still reachable under the bound, the block is ineffective ⇒ **REJECT** (never invert this).

### Area 3 — OWUI lazy/background outbound kill (SC4) + SC1↔SC4 reconciliation
- Kill OWUI's lazy/background outbound with `HF_HUB_OFFLINE=1` plus telemetry kill switches (`ANONYMIZED_TELEMETRY=false`, `DO_NOT_TRACK=1`, `SCARF_NO_ANALYTICS=true`) on the villa-openwebui unit.
- **SC1↔SC4 reconciliation:** the new outbound-kill env is **gated on web-search-ON**. When web search is disabled, the rendered openwebui unit stays byte-identical to v1.4 (SC1). The golden is re-frozen only for the web-search-ON rendering. (If a future audit shows v1.4 already carried some of these env vars, keep whichever subset preserves byte-identical-when-off.)
- Pre-stage any web-search-required weights into the models volume at install; if none are required, the plan asserts "none needed."

### Area 4 — Verify harness structure
- Structure `verify_search` on the v1.4 verify-agent four-layer harness pattern (`verify_agent.go`) as the structural template.
- Assert all four families: (a) canary negative-control (reachable unguarded → blocked under bound, else REJECT); (b) a planted-injection page returns stripped + fenced + flagged (asserts on Phase 32's guard output); (c) SSRF internal-host cases are blocked; (d) a secret-in-query-string exfil case.
- Use a **real** rootless-netns + nft block (per goal). If netns/nft tooling is unavailable, REJECT/WARN honestly with remediation — typed-Unknown, never a false PASS (consistent with the project's offload-asserting honesty).

### Claude's Discretion
- Exact config struct field naming/placement for `web_search.enabled`; the specific canary host/IP; the precise `--json` schema shape and golden fixtures; exact nft rule syntax and netns setup mechanics; how the four assertion layers are decomposed into functions — all at the planner/executor's discretion within the decisions above.
</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/villa/verify_agent.go` (+ `verify_agent_test.go`) — the v1.4 four-layer verify harness; the structural template for `verify search`.
- `cmd/villa/verify_memory.go`, `cmd/villa/verify.go` — the `villa verify <subcommand>` command family + dispatch.
- `cmd/villa/websafe.go`, `internal/websafe/` — the Phase 31/32 fetch + guard path the proof asserts on (sanitize→normalize→classify→fence; `Page.Verdict`; `/load metadata.guard`).
- `internal/orchestrate/searxng.go`, `openwebui.go`, `render.go` — SearXNG/OWUI Quadlet rendering (where the OWUI outbound-kill env + byte-frozen golden live).
- `internal/config/villaconfig.go` — `VillaConfig` (where `web_search.enabled` is added; load/save with defaults).
- Phase-27 egress-probe (`runProbeCurlCode`, exit-22 classification — noted in STATE as having open advisories) is prior art for honest infra-fail exit codes.

### Established Patterns
- Pure-core + injectable-seam; host effects behind `Deps`/`live*Deps`; `internal/orchestrate` is the only intentionally impure module.
- Byte-frozen `--json`/golden contracts evolve append-only + schema-bump; refreeze intentionally with `-update`.
- Honesty-by-construction: typed-Unknown → WARN; confident-negative → FAIL; never a false-green. Refuse-with-remediation in gates.
- No shell interpolation; fixed-arg `exec.Command`.

### Integration Points
- New `verify search` wired into the verify command group (`cmd/villa/verify.go` / `root.go`).
- OWUI outbound-kill env added in the openwebui Quadlet render path, gated on `web_search.enabled`.
- `web_search.enabled` added to `VillaConfig`.
</code_context>

<specifics>
## Specific Ideas

- The negative-control-first, inverse-framed canary assertion is the load-bearing, easy-to-invert part — the research note and SC2 both warn: off-allowlist canary reachable UNGUARDED, blocked UNDER the bound; ineffective block ⇒ REJECT, never a fabricated PASS.
- Proof must also exercise: planted-injection page → stripped+fenced+flagged (Phase 32 guard), SSRF internal-host cases, and a secret-in-query-string exfil case.
</specifics>

<deferred>
## Deferred Ideas

- A persistent runtime egress firewall (this phase's nft bound is verify-time-only).
- Phase 34 surfacing of web search in `status`/`--json`/dashboard/`doctor`/`backup`/`restore` (lands last, single schema 4→5 bump).
</deferred>
