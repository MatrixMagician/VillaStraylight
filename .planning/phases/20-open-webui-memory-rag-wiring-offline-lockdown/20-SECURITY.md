---
phase: 20
slug: open-webui-memory-rag-wiring-offline-lockdown
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-10
---

# Phase 20 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| config.toml → orchestrate render | user-controlled memory fields flow into rendered env values | config values (addrs/ports/model id) |
| rendered Quadlet unit → OWUI process | env values become OWUI runtime config consumed at process start | RAG/memory configuration |
| OWUI container ↔ villa-qdrant / villa-embed | RAG traffic over villa.network (container-DNS only) | document chunks, embeddings, memories |
| host ↔ public internet | the egress boundary the negative control proves is blocked | none permitted at runtime (PRIV-05) |
| verify command ↔ OWUI loopback REST API | the drive uses the existing loopback PublishPort, no new port | test doc + service-account token |
| user-uploaded document → villa-embed → Qdrant | untrusted content chunked/embedded/stored entirely locally | document content (potentially sensitive) |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-20-01 | Tampering/Repudiation | OWUI PersistentConfig (DB shadows env after first boot) | mitigate | `ENABLE_PERSISTENT_CONFIG=False` emitted in the memory-on block (D-03); frozen in `villa-openwebui.container.memory.golden`; asserted by memory-aware `TestRenderOpenWebUITelemetryFrozen`; confirmed in the LIVE container env (2026-06-10 UAT) | closed |
| T-20-02 | Information Disclosure | telemetry/offline posture regressing during env-block edit | mitigate | `TestRenderOpenWebUITelemetryFrozen` re-audits the full telemetry-kill set (ANONYMIZED_TELEMETRY/DO_NOT_TRACK/SCARF_NO_ANALYTICS/OFFLINE_MODE/HF_HUB_OFFLINE) in BOTH views + exact env-line counts (11 off / 24 on); green 2026-06-10 | closed |
| T-20-03 | Information Disclosure | runtime embedding-model auto-download (HF egress) | mitigate | `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False` + `RAG_EMBEDDING_ENGINE=openai` → local villa-embed; live env confirms; runtime proof (T-20-12) passed with egress blocked | closed |
| T-20-04 | Tampering | shell injection via rendered env value | mitigate | values config-resolved + `fmt.Sprintf` into `{{range .Env}}` template; no shell interpolation, no exec; `TestSeamGrepGate` green | closed |
| T-20-05 | Spoofing/Elevation | new published host port | accept | No PublishPort change — OWUI keeps its one loopback PublishPort; Qdrant/embed container-DNS only (D-11); `TestRenderOpenWebUILoopbackOnly` green; `villa status` loopback-only true (live 2026-06-10) | closed |
| T-20-06 | Information Disclosure | `RAG_OPENAI_API_KEY=sk-no-key-required` flagged as leaked credential | accept | Non-secret sentinel (same precedent as existing `OPENAI_API_KEY` placeholder); llama-server does no auth; documented in env comment | closed |
| T-20-07 | Repudiation/Info Disclosure | false-green zero-outbound (egress never actually blocked) | mitigate | Negative-control probe asserted FIRST in `evalRagSmoke`; probe-cannot-run = FAIL; `TestEvalRagSmoke` + `TestEvalRagSmokeNegativeControlFirst` green; live: egress-open run FAILed exit 1 (2026-06-10) | closed |
| T-20-08 | Information Disclosure | runtime model download / data exfil during RAG drive | mitigate | Drive runs under host-egress block; PASS proves chunk→embed→retrieve completed on villa.network only; live PASS exit 0 (2026-06-10) | closed |
| T-20-09 | Tampering | shell injection via podman/curl args | mitigate | `runProbeCurl` + loopback curl runners are fixed-arg exec; URLs/ids config-/REST-resolved, never shell-interpolated (T-19-10 pattern) | closed |
| T-20-10 | Spoofing/Elevation | new published host port for the smoke test | accept | Drive uses EXISTING loopback PublishPort (127.0.0.1:3000); negative control runs inside villa.network; no new port (D-11) | closed |
| T-20-11 | Information Disclosure | sentinel `RAG_OPENAI_API_KEY` / empty `QDRANT_API_KEY` mistaken for secrets | accept | Non-secret sentinel / empty on a private container-DNS net; documented; verify command introduces no real credential | closed |
| T-20-SC | Tampering | npm/pip/cargo supply-chain installs | mitigate | Zero new packages this phase (env-only + Go test/proof code); `tech-stack.added: []` in all summaries; Package Legitimacy Audit N/A | closed |
| T-20-12 | Information Disclosure | runtime data exfil / model download during a real upload (PRIV-05 headline) | mitigate | Real RAG path run with host egress blocked + negative control that MUST fail; live PASS "document upload retrieved + cited with zero outbound" exit 0 (2026-06-10, re-verified at UAT) | closed |
| T-20-13 | Repudiation | false-green privacy claim (gate vacuous) | mitigate | Deliberate egress-open run confirms FAIL exit 1 — negative control proven real, not vacuous (20-03 Task 5 + re-proven live 2026-06-10) | closed |
| T-20-14 | Tampering | enabling auto-extraction silently changes memory/citation behavior | mitigate | MEM-03 default-off confirmed live (store empty before any manual save, 2026-06-10 UAT); `docs/MEMORY.md` documents Native FC interaction and deterministic-citation guidance | closed |
| T-20-15 | Spoofing/Elevation | new published host port introduced on the live host | accept | `villa status` privacy audit green live (loopback-only true, no telemetry); verify drive uses existing loopback PublishPort + villa.network probe (D-11) | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-20-01 | T-20-05 / T-20-10 / T-20-15 | No new published host port anywhere in the phase; OWUI's single pre-existing loopback PublishPort is the only host bind, guarded by `TestRenderOpenWebUILoopbackOnly` and the live `villa status` privacy audit | operator (plan-time disposition, verified on-hardware) | 2026-06-10 |
| AR-20-02 | T-20-06 / T-20-11 | `sk-no-key-required` sentinel and empty `QDRANT_API_KEY` are non-secrets on a private container-DNS network; llama-server performs no auth; documented inline | operator (plan-time disposition) | 2026-06-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-10 | 16 | 16 | 0 | claude (gsd-secure-phase, plan-time register verified against implementation + live gfx1151 host) |

Evidence basis: 36 guard tests green (`OpenWebUI|Telemetry|SeamGrepGate|EvalRagSmoke|VerifyMemoryGate|LoopbackOnly` across orchestrate/inference/cmd), memory golden carries the PersistentConfig kill switch, and the 2026-06-10 on-hardware UAT (20-UAT.md, 6/6 pass) re-proved the runtime gates live: egress-open FAIL exit 1, egress-blocked PASS exit 0, loopback-only/no-telemetry status, MEM-03 default-off.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-10
