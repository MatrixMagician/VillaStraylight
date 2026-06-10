---
phase: 21
slug: conversational-recall-indexer
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-10
---

# Phase 21 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| OWUI chat content → `internal/recall` inputs | untrusted user/assistant text enters the pure core via parsed JSON (typed structs, no exec) | chat text |
| `internal/recall` → `$XDG_DATA_HOME/villa/recall-state.json` | villa-side persistence of index metadata on the host filesystem | ids/timestamps/counts only |
| villa (host) ↔ OWUI loopback REST API | admin-scoped reads of users' chats + Knowledge/Model writes over the existing 127.0.0.1 PublishPort | chat content, KB/model writes, in-memory JWT |
| untrusted chat content → curl/exec layer | user/assistant text flows into multipart uploads and JSON bodies | chat text |
| OWUI ↔ villa-embed / Qdrant (villa.network) | embedding/vector traffic stays container-DNS only (Phase 19/20 posture) | embeddings, vectors |
| host ↔ public internet | zero-outbound posture must hold during real indexing | none permitted |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-21-01 | Information Disclosure | chat content leaking into recall-state.json | mitigate | State schema is ids/timestamps/counts only — JSON-key denylist unit test bans title/content/message keys; file 0600 / dir 0700. Verified live in 21-03 Task 1 (`stat` 600, content-free) + unit tests | closed |
| T-21-02 | Tampering | recall-state.json path traversal / write outside XDG root | mitigate | `assertInsideDir` guard against the fixed store root + atomic temp+rename writer (cloned from `internal/usage`); code review confirmed store hygiene sound | closed |
| T-21-03 | Tampering/Repudiation | tampered/corrupt state fabricating a "current" index | mitigate | Fail-closed `Load` (corrupt/unknown-schema ⇒ empty state); staleness recomputed against the LIVE list every status call — state alone can never claim currency | closed |
| T-21-04 | Repudiation | stale index reported current when live list unevaluable | mitigate | `recall.Classify` makes Unknown structurally distinct (`StaleKnown=false` + reason); unit-tested; proven live (OWUI-down → "Unknown — could not evaluate" WARN, never stale=0, 21-03 Task 3) | closed |
| T-21-05 | Tampering | malicious chat content breaking the transcript renderer | mitigate | Visited-set cycle guard in the chain walk; reasoning-strip handles unclosed blocks; pure string transform, no exec/eval; Task-2 tests. (IN-01/IN-02 robustness deferred as Info — pinned digest, low risk) | closed |
| T-21-06 | Tampering | shell injection via chat titles/content reaching curl argv | mitigate | All bodies `json.Marshal`; transcript content via stdin multipart (`-F file=@-`), never argv/temp file; URLs from config + API-returned ids only; fixed-arg `exec.CommandContext`. Code review (deep) confirmed no shell interpolation | closed |
| T-21-07 | Elevation/Information Disclosure | admin-scoped chat read pools all users into one shared model-global KB | mitigate | Originally accept (single-operator box). **Hardened in code review (WR-05, commit `693de5c`):** now FAIL-CLOSED — `villa recall index` refuses with remediation when >1 non-service human user exists unless `--i-understand-shared-recall` is passed. Service account `villa-verify@localhost` deterministically excluded. JWT in memory only. Widened scope documented in `docs/MEMORY.md` | closed |
| T-21-08 | Information Disclosure | chat content leaving the box during indexing | mitigate | All REST loopback-only (existing PublishPort, no new host port); vectors OWUI→villa-embed→Qdrant on villa.network; zero-outbound re-confirmed live (21-03), Phase-20 runtime proof posture unchanged | closed |
| T-21-09 | Repudiation | silent staleness / false-current status | mitigate | Typed-Unknown rendering + per-chat incremental persist + completed-stamp-only-on-reconciled-clean-pass (hardened by CR-01 fix, commit `173bfb4`); attachment state folded into status. Live drill 21-03 Task 3 | closed |
| T-21-10 | Tampering | attach clobbering operator-set Model meta | mitigate | Read-merge-write attach with foreign-key preservation; **WR-04 fix (commit `440c4d4`)** re-GETs the row to confirm the KB id persisted before reporting Attached (catches silent detach). A3 verified on-hardware | closed |
| T-21-11 | Tampering | stale vectors / duplicate growth on re-index | mitigate | Delete-then-re-add via `file/remove?delete_file=true` (vectors cleaned by file_id AND hash); `--rebuild` uses id-preserving `reset`. **WR-01 fix:** remove-then-fail clears stale state so the chat re-qualifies (no silent retrieval gap). Live clean-replace proven (old file → 404, 21-03 Task 3) | closed |
| T-21-12 | Denial of Service | bulk index contending with chat model on shared gfx1151 envelope | mitigate | Strictly sequential indexing; size-aware poll timeouts; throughput measured (A1 ~1.3s/chat, A2 no tuning needed, 21-03) | closed |
| T-21-13 | Spoofing | wrong identity indexed (service-account noise / missed operator chats) | mitigate | Admin list-by-user (archived-inclusive); `villa-verify@localhost` excluded; per-chat user_id recorded. A4 (no completion pollution) verified live | closed |
| T-21-14 | Repudiation | on-hardware false-green for SC#2 | mitigate | SC#2 required a paraphrase (zero keyword overlap) + `sources` citation + never-discussed negative control, verified via REST AND the real web UI (blocking checkpoint, operator/orchestrator approved 21-03 Task 2) | closed |
| T-21-15 | Information Disclosure | real chat content handled during the live drill | mitigate | Loopback/container-DNS only; state file audited 0600 + content-free on the real artifact; no new outbound | closed |
| T-21-16 | Tampering | timeout tuning regressing the proven seam | mitigate | No tuning was needed (A2); `recall_live.go` timeout constant unchanged; `TestRecall` green | closed |
| T-21-17 | Denial of Service | bulk index degrading the live chat model (shared envelope) | accept | Sequential by construction; throughput measured + recorded. The full residency-under-embed-load gate is Phase 22 scope (CTRL-03), not this phase | closed |
| T-21-SC | Tampering | npm/pip/cargo supply-chain installs | mitigate | Zero new packages this phase (all summaries `tech-stack.added: []`); Package Legitimacy Audit N/A | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-21-01 | T-21-17 | Sequential indexing bounds GPU contention; the offload-asserting residency-under-embed-load gate is deliberately Phase-22 (CTRL-03) scope. Throughput measured (~1.3s/chat) shows no practical degradation at realistic history sizes on gfx1151 | operator (plan-time disposition, on-hardware throughput recorded) | 2026-06-10 |

*Note: T-21-07 (cross-user chat read) was a plan-time `accept` but was HARDENED to a fail-closed guard during code review — it is now `mitigate`/closed, not an accepted risk.*

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-10 | 18 | 18 | 0 | claude (gsd-secure-phase short-circuit: plan-time register, verified against deep code review + fixes + on-hardware Plan 03) |

Evidence basis: deep code review (21-REVIEW.md) confirmed seam discipline (no shell interpolation, fixed-arg curl, stdin multipart) and store hygiene (0600/0700, atomic write, traversal guard, fail-closed Load) are sound with no findings; its 1 blocker + 6 warnings were all fixed with tests (commits `173bfb4`, `693de5c`, `440c4d4`) — notably WR-05 converting the cross-user read scope (T-21-07) from an accepted risk into a fail-closed guard. On-hardware Plan 03 proved T-21-08 (zero outbound), T-21-10 (meta preserved), T-21-11 (clean-replace leak-free), T-21-13 (A4 no pollution), and T-21-14 (semantic round-trip with citation + clean negative control). 450 tests green incl. `TestSeamGrepGate`.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-10
