# Requirements: VillaStraylight — Milestone v1.2 (Operability)

**Defined:** 2026-06-07
**Core Value:** Run a capable local AI workspace that "just works" after install — extended in v1.2 to "and stays operable, recoverable, and measurable over time," with zero data leaving the box.

## v1.2 Requirements

Requirements for the Operability milestone. Each maps to exactly one roadmap phase.
All requirements inherit the standing invariants: strictly local / zero telemetry,
single static binary (CGO-free), config is the single source of truth, `--json`/dashboard
outputs are byte-frozen (append-only + schema bump), offload-asserting (no false-green),
backend literals stay behind the `internal/inference` seam.

### Diagnostics & Health

- [ ] **DOCTOR-01**: User can run `villa doctor` to get a one-shot health diagnosis of a running install, composing the existing preflight + status + residency-proof cores, exiting `0` (healthy) / `2` (blocking fault) / `1` (warning) — mirroring the preflight exit contract.
- [ ] **DOCTOR-02**: Every non-healthy `villa doctor` finding carries actionable remediation text, and a silent or partial CPU fallback is reported as a FAIL (offload-asserting — never a false-green health 200).
- [ ] **DOCTOR-03**: `villa doctor` detects and reports config-vs-disk drift (rendered Quadlet units that no longer match the config source of truth).

### Backup & Recovery

- [ ] **BAK-01**: User can back up their workspace (config `.toml` + the Open WebUI data volume) to a single archive that **excludes** re-pullable model weights and includes a manifest of versions, image digests, and checksums.
- [ ] **BAK-02**: User can restore from a backup archive transactionally (capture → quiesce → swap → restart → prove → rollback-on-failure), so a failed or partial restore never corrupts a running stack.
- [ ] **BAK-03**: `villa restore` warns on version/digest skew between the archive manifest and the current install before applying.

### Benchmark Reports

- [ ] **BENCH-03**: `villa bench` persists each run as a versioned saved report under `$XDG_DATA_HOME/villa/`, keeping prompt-processing and token-generation tok/s separate (never blended) and recording residency-void state.
- [ ] **BENCH-04**: User can run `villa bench --compare` to compare saved reports, gated by a comparability guard (same model/quant/host fingerprint) that refuses to print deltas across non-comparable runs.

### Usage Tracking

- [ ] **USAGE-01**: villa accumulates cumulative token counts (prompt + generated, per model) locally over time, reset-aware (survives `llama-server` counter resets), counts-only with no prompt/response content and no new outbound traffic.
- [ ] **USAGE-02**: `villa status` and the control dashboard surface cumulative usage over time as an append-only, schema-bumped contract change (live tok/s remains; cumulative totals added).

### Guided Install

- [ ] **INSTALL-01**: User can run a guided interactive TUI install that composes the existing detect → recommend → preflight → install pipeline with confirmation/consent steps, adding presentation only (no decision logic in any pure core).
- [ ] **INSTALL-02**: The guided install degrades gracefully on a non-TTY environment and via an explicit `--no-tui` flag to the existing flag-driven path, and the binary remains a single static CGO-free build.

### Inference Backend

- [ ] **ROCM-ALT-01**: User can opt into a digest-pinned `rocm-6.4.4` (or its `-rocwmma` variant, bench-decided) ROCm image as an alternate backend via `villa backend set`, gated by `rocm-policy.json` floors and kept behind the `internal/inference` seam (incl. extending `seam_test.go` so the new image literal cannot leak) — addressing the v1.1 Δtg −11.15 regression. Never auto-switches; Vulkan stays default. *(Off-hardware implementation complete + full suite green, Phase 12 plans 12-01/12-02/12-03; on-hardware UAT — transactional switch + residency proof (SC#1) and Δtg-recovery bench (SC#3) on the gfx1151 host — PENDING operator verification before this can be marked validated.)*

## Future Requirements

Deferred beyond v1.2. Tracked but not in this roadmap.

### Memory & Search (Milestone 2)

- **MEM-01**: Qdrant persistent memory integration
- **SRCH-01**: SearXNG local search integration
- **CODE-01**: OpenCode (local-model coding agent) wiring

### Platform & Access (Future)

- **PLAT-01**: macOS / Apple-Silicon (Metal) inference backend
- **REMOTE-01**: authenticated remote / multi-user access
- **ROCM-TUNE-01**: ROCm perf-tuning knobs (hipBLASLt / rocWMMA-FA / batch)

## Out of Scope

Explicitly excluded for v1.2. Anti-features surfaced by research are flagged here with warnings.

| Feature | Reason |
|---------|--------|
| Cloud / remote backup destinations | Breaks strictly-local posture; v1.2 backup is local-archive only |
| Uploading usage data / telemetry of any kind | Violates zero-telemetry core value (research-flagged anti-feature) |
| Public bench leaderboard / sharing saved reports off-box | Off-box data flow; reports stay local |
| `doctor` diagnostic-bundle upload | Off-box data flow; doctor output stays local |
| Backing up model weights | Re-pullable, content-addressed, GB-scale — manifest identity + re-pull on restore instead |
| Logging prompt/response content for usage tracking | Privacy breach; usage is counts-only |
| CGO-based TUI toolkit | Breaks single-static-binary constraint; `charmbracelet/huh` is pure-Go |
| `doctor` mutating/repairing the install automatically | doctor diagnoses + remediates-by-advice; mutation stays in explicit verbs (install/backend set/restore) |

## Traceability

Which phases cover which requirements. Populated during roadmap creation (`/gsd-new-milestone` → roadmapper).

| Requirement | Phase | Status |
|-------------|-------|--------|
| ROCM-ALT-01 | Phase 12 | In Progress (on-hardware UAT pending) |
| DOCTOR-01 | Phase 13 | Pending |
| DOCTOR-02 | Phase 13 | Pending |
| DOCTOR-03 | Phase 13 | Pending |
| BENCH-03 | Phase 14 | Pending |
| BENCH-04 | Phase 14 | Pending |
| USAGE-01 | Phase 15 | Pending |
| USAGE-02 | Phase 15 | Pending |
| BAK-01 | Phase 16 | Pending |
| BAK-02 | Phase 16 | Pending |
| BAK-03 | Phase 16 | Pending |
| INSTALL-01 | Phase 17 | Pending |
| INSTALL-02 | Phase 17 | Pending |

**Coverage:**

- v1.2 requirements: 13 total
- Mapped to phases: 13 ✓
- Unmapped: 0 ✓

**Phase distribution:**

- Phase 12 (`rocm-6.4.4` Alternate Backend): ROCM-ALT-01 (1)
- Phase 13 (`villa doctor` Health Diagnosis): DOCTOR-01, DOCTOR-02, DOCTOR-03 (3)
- Phase 14 (Saved Bench Reports + `--compare`): BENCH-03, BENCH-04 (2)
- Phase 15 (Cumulative Usage Tracking): USAGE-01, USAGE-02 (2)
- Phase 16 (Backup / Restore): BAK-01, BAK-02, BAK-03 (3)
- Phase 17 (Guided TUI Install): INSTALL-01, INSTALL-02 (2)

---
*Requirements defined: 2026-06-07*
*Last updated: 2026-06-07 after milestone v1.2 (Operability) roadmap creation — 13/13 requirements mapped to Phases 12–17.*
