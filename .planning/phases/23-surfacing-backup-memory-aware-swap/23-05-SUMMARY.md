---
phase: 23-surfacing-backup-memory-aware-swap
plan: 05
subsystem: on-hardware verification (gfx1151 live drill)
tags: [verification, on-hardware, ctrl-02, ctrl-04, ctrl-05, oq2, oq4, t-23-01-closure, drill]
requires:
  - 23-01 (status v3 + per-service memory health + TTL cache)
  - 23-02 (memory-aware backup/restore, manifest v2, clean-recreate)
  - 23-03 (dashboard Memory panel)
  - 23-04 (D-09 invariants + D-10 recall-index refusal / --rebuild)
provides:
  - CTRL-02/CTRL-04/CTRL-05 proven end-to-end on the live Strix Halo box (real data, real services)
  - OQ2 closed: memoryHealthTTL=15s confirmed on hardware (no churn, acceptable latency; 30s fallback NOT needed)
  - OQ4 closed: --rebuild proven live (bypasses refusal, id-preserving KB clean-replace, fresh stamp records the new identity)
  - T-23-01 closed on hardware: stopped villa-embed is health=down, never ready (false-green eliminated)
affects:
  - phase 23 close (operator sign-off checkpoint pending)
tech-stack:
  added: []
  patterns: []
key-files:
  created: []
  modified: []
decisions:
  - "OQ2 FINAL: memoryHealthTTL stays 15s — on-hardware: dashboard poll cold 0.78s / TTL-warm 0.33s, exactly one podman probe pair per 15s window, zero helper-container accumulation over 60s of 2.5s-cadence polling; the documented 30s fallback is NOT needed"
  - "OQ4 FINAL: `villa recall index --rebuild` proven live — with a skewed config it bypasses the refusal, completes (1 added, retrieval re-attached), preserves the KB id (23e667e7… unchanged = id-preserving clean-replace), and re-stamps the NEW embedding identity; the subsequent real-identity --rebuild restored a healthy index"
  - "exitBlocked == 1 (cmd/villa/preflight.go:44) — the recall-index refusal exit observed live (1) IS exitBlocked, matching the plan's contract"
  - "Drill-pulled qwen2.5-0.5b weights (468.6 MiB) removed post-drill — no leftover drill artifacts; safety archive ~/villa-safety-2305.tar deliberately retained (legit backup, operator's call to delete)"
metrics:
  duration: ~9 min (drill) — Task 2 checkpoint pending operator sign-off
  completed: 2026-06-10
  tasks: 1 of 2 (Task 2 = checkpoint:human-verify, awaiting operator)
  commits: 1 (docs — verification-only plan, no code changes)
---

# Phase 23 Plan 05: On-Hardware Verification Drill Summary

**One-liner:** All three Phase-23 success criteria proven FOR REAL on the live gfx1151 box — status v3 with honest per-service memory health (stopped embed = down, never ready), real Qdrant volume backup/restore through clean-recreate with the skew WARN+confirm firing on provocation, and the swap drill closing D-09 (byte-identical memory units) + D-10/OQ4 (refusal then --rebuild) — box restored to its exact pre-drill state (config byte-identical, backend ROCm untouched, doctor PASS).

## Drill Evidence (all commands run live on the box, 2026-06-10 21:36–21:45 UTC)

### SAFETY (pre-drill capture + safety backup)

- Pre-drill state recorded: backend **rocm**, model qwen3.6-35b-a3b/UD-Q4_K_M/ctx 131072, memory_enabled=true, all 5 services active, recall state = 1 chat indexed (complete run 12:00:46Z). Config snapshot sha256 `48e40044…b2d1f`.
- Old `./villa` binary confirmed pre-v3 (qdrant/embed rows still OFFLOAD WARN) — binary-trap real; `make build` run BEFORE the safety backup because the qdrant-volume backup path is 23-02 code.
- `villa backup -o ~/villa-safety-2305.tar` (OUTSIDE drill artifacts): manifest **schema_version 2** with entries incl. **qdrant-volume.tar** + **recall-state.json**, **embedding_model "nomic-embed-text-v1.5"**, **embedding_dim 768**, **recall_schema_version 1** — first CTRL-04 evidence captured for free.

### CTRL-02 — status v3 + dashboard + negative control (SC#1)

- `make build` → `systemctl --user restart villa-dashboard.service` (gotcha honored).
- `./villa status --json`: **schema_version 3**; villa-qdrant + villa-embed rows with real per-service health (`ready`) and **OFFLOAD N/A** (`offload_applies: false`, status detail "N/A — this service has no GPU offload"); memory section `{nomic-embed-text-v1.5, 768, recall_state: indexed, indexed_chats: 1, started/completed 12:00:46Z}` exactly matching live recall-state.json; no `embedding_skew` field on match.
- **T-23-01 negative control (false-green closed on hardware):** `systemctl --user stop villa-embed.service` → after the 15s TTL window: `villa-embed.service active=inactive health=down`, **overall FAIL** — never ready. Restart → `health=ready`, overall PASS. Carried Phase-22 threat closed with its assigned ID.
- Dashboard serves the new assets: `dashboard.js` contains `renderMemory` (3 hits), `/api/status` returns schema 3 + memory object.

### OQ2 — probe-cost measurement (T-23-20)

- CLI: `villa status` cold 0.75s / warm 0.63s (each CLI invocation is a fresh process; the TTL cache lives in the long-lived dashboard).
- Dashboard (the case that matters): 8 polls of `/api/status` at the SPA's 2.5s cadence — poll 1 **0.78s** (cold, spawns the probe pair), polls 2–6 **~0.33s** (TTL-warm), poll 7 **0.65s** (TTL expired at ~17.5s, one re-probe), poll 8 0.33s. Exactly **one podman probe pair per 15s window**.
- `podman ps -a` sampled every 5s for 60s during polling: **zero villa helper containers accumulated** (only pre-existing unrelated `pullmd-*`/`ollama` entries).
- **Verdict: 15s TTL stands; the 30s one-const fallback is NOT needed.**

### CTRL-04 — backup/restore drill on REAL data (SC#2)

- `villa backup` of the real populated volume: journalctl shows the quiesce frame — `Stopping villa-qdrant` 22:41:40.230 → `Stopped` 22:41:40.385 → export window → `Started` 22:41:43.988 (local BST).
- `villa restore drill-backup.tar` (exit 0): honest report printed — "memory: Qdrant volume restored — verify with `villa doctor`; if a dimension skew was confirmed at restore, run `villa recall index --rebuild`", "memory: recall state restored", "memory stack: enabled (restored config)" + stale-units note.
- **Clean-recreate proven:** restore's own quiesce window 22:42:04.387→22:42:06.535; `podman volume inspect villa-qdrant` → **CreatedAt 22:42:05.386** — inside the window, i.e. the volume was removed and recreated, not merge-imported.
- Post-restore retrieval intact: all 5 services active; `villa recall status` → 1 chat indexed, stale 0, **retrieval: attached**.
- **Skew WARN+confirm provoked:** config embedding_model → `fake-embed-drill-v9` → `villa restore` printed the WARN verbatim: "backup vectors were embedded with nomic-embed-text-v1.5 (dim 768); this install is configured for fake-embed-drill-v9 (dim 768)" + remediation ("retrieval stays corrupt until a re-index: run `villa recall index --rebuild` after restore, or align embedding_model/embedding_dim…"); declined at the [y/N] gate → "refusing to apply… restore declined at the skew confirmation", **exit 1**, zero side effects. Config reverted, sha256-identical to pre-drill.

### CTRL-05 — memory-aware swap drill (SC#3)

- **D-09 (chat swap leaves memory intact):** `villa model swap qwen2.5-0.5b` (auto-pull, 468.6 MiB) then `villa model swap qwen3.6-35b-a3b` back. sha256 of `villa-qdrant.container/.volume`, `villa-embed.container`, and both `villa-openwebui.*` units **byte-identical across the full round-trip**; journal over the swap window: **zero** Stop/Start events for qdrant/embed/openwebui, exactly **2** `Started villa-llama` (one per swap); `villa recall status` retrieval still attached; config restored sha256-identical.
- **D-10 refusal:** config embedding_model → fake → `villa recall index` → "REFUSING — the index was built with nomic-embed-text-v1.5 (dim 768) but config now says fake-embed-drill-v9 (dim 768); indexing into a mismatched-dimension collection corrupts retrieval. Re-run with --rebuild to re-index cleanly, or revert the config." → **exit 1 = exitBlocked** (preflight.go:44).
- **OQ4 --rebuild proof (with the skewed config):** `villa recall index --rebuild` → **completed** ("1 added… 1 chats indexed; retrieval attached"), stamp now records **fake-embed-drill-v9 / 768**, **knowledge_id 23e667e7-3496-4d45-9466-c52a143831dc UNCHANGED** — id-preserving clean-replace of the OWUI KB, fresh stamp records the new identity. No misbehavior; no gap to file.
- Recovery: config reverted to nomic-embed-text-v1.5 → `villa recall index --rebuild` → healthy real index (stamp nomic/768, kb id preserved, retrieval attached, completed 21:44:28Z).

### RESTORE BOX STATE

- No `villa install`/`recommend --save` ran during the drill → backend **rocm never reverted** (config byte-identical throughout; no `backend set rocm` needed).
- Safety backup never needed (real data restored cleanly by the drill itself).
- Drill artifacts removed: `/tmp/villa-drill-2305/drill-backup.tar` deleted; drill-pulled `qwen2.5-0.5b-instruct-q4_k_m.gguf` deleted from the models dir. Safety archive `~/villa-safety-2305.tar` retained (legit backup — operator's call).
- Final: `villa status` **overall PASS** (backend rocm, llama OFFLOAD PASS, memory rows N/A); `villa doctor` **overall PASS exit 0** (ROCM-PRE/MEM-PRE gates green, MEM-DOC-residency PASS, drift PASS); dashboard left running the new binary.

## Acceptance Criteria → Evidence Map

| Criterion | Result |
|---|---|
| status --json schema 3, memory rows real health, memory section populated | PASS (transcript above) |
| Stopped-embed negative control: health != ready | PASS — `health=down`, overall FAIL |
| Backup manifest: qdrant-volume.tar + nomic-embed-text-v1.5 + 768 + recall_schema_version | PASS — manifest v2 inspected |
| Restore: clean-recreate observed, retrieval works, skew WARN+confirm transcript | PASS — volume CreatedAt inside quiesce window; decline exit 1 |
| Swap: memory units sha256-identical; refusal + --rebuild transcripts | PASS — byte-identical round-trip; exitBlocked refusal; OQ4 rebuild clean |
| OQ2 recorded with chosen TTL | PASS — 15s stands, zero churn |
| Final doctor + make check green; backend restored if applicable | PASS — doctor PASS, make check green, ROCm untouched |

## Deviations from Plan

**1. [Rule 3 - Blocking] `make build` moved BEFORE the safety backup**
- **Issue:** the resident `./villa` predated 23-01/23-02 (status still showed the old OFFLOAD WARN rows) — its `backup` would NOT have included the Qdrant volume, defeating the safety backup's purpose and the manifest assertion.
- **Fix:** built first (non-mutating; the dashboard restart still happened in the CTRL-02 step per plan ordering). Safety-first intent preserved: no mutating drill step ran before the safety archive existed.

**2. [Note] Dashboard poll driven by curl, not a browser**
- No browser tab was open during the churn watch, so the SPA wasn't polling; `/api/status` was driven at the SPA's exact 2.5s cadence via curl for an honest OQ2 measurement. Visual rendering remains the Task 2 operator check.

**3. [Note] Drill-pulled weights removed**
- The swap drill auto-pulled qwen2.5-0.5b (sanctioned outbound); removed post-drill per "no leftover drill artifacts". Not a config/state mutation.

## Authentication Gates

None.

## Known Stubs

None — verification-only plan.

## Threat Flags

None new. Register dispositions from this plan: **T-23-19** mitigated (safety-backup-first ordering executed; box state byte-verified restored); **T-23-20** mitigated/closed (OQ2 measured, 15s TTL confirmed); **T-23-01** **CLOSED on hardware** (stopped-embed negative control: down, never ready).

## Verification

- `./villa status --json | grep -c '"schema_version": 3'` → 1; `make check` green (vet + full suite).
- `villa doctor` overall PASS, exit 0, on the live ROCm stack.
- Config sha256 identical to the pre-drill snapshot at drill end; backend rocm; recall index healthy (nomic/768, retrieval attached).

## Task 2 — checkpoint:human-verify (PENDING)

Operator sign-off on the dashboard visuals (Memory panel per 23-UI-SPEC at http://127.0.0.1:8888) and review of these transcripts is REQUIRED before Phase 23 closes. Returned to the orchestrator as a blocking checkpoint — not self-approved.

## Self-Check: PASSED

Drill evidence files present in /tmp/villa-drill-2305 (transcripts, sha files, config snapshot); no repo source files were modified by this plan (verification-only, as planned).
