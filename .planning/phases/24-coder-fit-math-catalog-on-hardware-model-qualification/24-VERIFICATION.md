---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
verified: 2026-06-13T00:00:00Z
status: passed
score: 14/14 must-haves verified
overrides_applied: 0
---

# Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification — Verification Report

**Phase Goal:** `villa recommend` produces an honest, hardware-qualified coding-model recommendation — fit computed at agent-profile context, residency mode (`swap`/`shared`) derived purely from fit math — backed by catalog entries that survived a real agent-in-the-loop tool-call loop on the gfx1151 box.
**Verified:** 2026-06-13
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #  | Truth | Status | Evidence |
| -- | ----- | ------ | -------- |
| 1  | Catalog has exactly 3 `role:"coder"` entries with revision-pinned shard URLs + template provenance | VERIFIED | `seed.json`: 3 `"role": "coder"` matches (qwen3-coder-30b-a3b, -next-q4, -next-q3); all shard URLs use `resolve/<commit-hash>/` (b17cb02…, ce09c67…), NOT `/resolve/main/`; sha256 + size_bytes + `template_provenance` present on each |
| 2  | Existing 4 chat entries byte-untouched except top-level schema bump | VERIFIED | `git show 1509717 -- seed.json`: no `+`/`-` lines touch qwen2.5-*/qwen3-30b/qwen3.6-35b/`resolve/main`; only schema_version→3 and 3 new coder entries appended |
| 3  | Catalog `SupportedSchema == 3`; loader uses DisallowUnknownFields exact-match | VERIFIED | `catalog.go:29 const SupportedSchema = 3`; `load.go:43 ext.SchemaVersion != SupportedSchema → warn+seed`; `load.go:149 dec.DisallowUnknownFields()` |
| 4  | Absent role decodes chat; absent cache_reuse_safe decodes false (fail-closed) | VERIFIED | `catalog_test.go` fail-closed default tests pass (fresh `go test ./internal/catalog` ok); zero-value bool = false by construction |
| 5  | `villa recommend` computes coder fit at each entry's agent_ctx, after reservation + chat fit, vs post-reservation envelope | VERIFIED | `recommend.go:179-195` envelope→reservation shrink→`pickCoder(c, envelope)`; `coder.go:83` uses `m.AgentCtx`; golden coder block computed at agent_ctx 65536 (not chat ctx 131072) |
| 6  | Residency is pure inequality: swap when best coder fits standalone, shared when none — incl. refusal path | VERIFIED | `coder.go:84-110` total>envelope→continue, best→`residencySwap`, none→`sharedCoderFit()` (`Fits:false, Residency:shared`); refusal path `recommend.go:173` stamps `sharedCoderFit()` |
| 7  | `--json` always contains coder block above schema_version on every path; schema 3 | VERIFIED | `recommend.go:119 Coder CoderFit` (no omitempty) directly above `SchemaVersion` (last field); `recommendSchemaVersion = 3`; finalizeRecommendation stamps on all 3 paths (170/199/202); golden shows populated `coder` block then `schema_version: 3` |
| 8  | Chat pick bit-identical with coder present: pickBest skips coder; --model override on coder warns-and-allows | VERIFIED | `recommend.go:301 if m.Role == "coder"` skip in pickBest; `recommend.go:378` WARNING note on coder override (uses as chat pick only) |
| 9  | Exactly ONE re-freeze of recommend.golden.json in this phase | VERIFIED | `git log` golden: touched once in phase 24 by be8ee0e (24-02); prior touch dfc4f8c was phase 22; untouched by 24-04 |
| 10 | Each coder entry has a real multi-step agent-in-the-loop tool-call run through llama-server --jinja on pinned digest, with PASS/FAIL + evidence | VERIFIED | 3 `verdict.md` all `VERDICT: PASS`; crush-transcript.txt shows view→edit→bash(go test) loop with git diff + go-test-exit=0; distinct tool types ['bash','edit','view'] (+glob for next-q4) |
| 11 | KV + total GTT footprint MEASURED at each agent_ctx on gfx1151, recorded vs computed | VERIFIED | `kv-gtt.txt` per entry: GTT baseline/after/delta in bytes; `Vulkan0 KV buffer size`; offload 49/49; verdict measured-vs-computed tables (KV exact match 6.00/3.00/3.00 GiB) |
| 12 | Each entry has cache-reuse probe verdict feeding cache_reuse_safe | VERIFIED | `cache-reuse.txt`: WARNING_PRESENT=false + turn-2 cache_n>0 (135/58/58); seed cache_reuse_safe=true for all 3 matches probe verdicts |
| 13 | Every run records served llama-server version proving pinned digest (build 9496) | VERIFIED | `server-version.txt` × 3: `version: 9496 (94a220cd6)` run BY DIGEST `sha256:9a74e555…`; API responses carry `system_fingerprint: b9496-94a220cd6` |
| 14 | Toolbox keep/re-pin recorded as numbered decision with on-box evidence BEFORE freeze | VERIFIED | `24-TOOLBOX-DECISION.md`: D-13 ratifies D-11 → KEEP digest `sha256:9a74e555…` (build 9496), no re-pin; D-11 Checks 1-3 cite on-box evidence; Freeze Ratification (Task 3) before catalog froze |

**Score:** 14/14 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/catalog/catalog.go` | SupportedSchema=3, coder fields, AgentSampling | VERIFIED | SupportedSchema=3; CatalogModel carries Role/AgentCtx/CacheReuseSafe/AgentSampling/TemplateProvenance |
| `internal/catalog/seed.json` | schema 3, 3 coder entries verified | VERIFIED | schema_version 3; 3 revision-pinned coder entries w/ sha256, provenance, agent_ctx, agent_sampling, cache_reuse_safe |
| `internal/catalog/load.go` | DisallowUnknownFields + exact schema window + validateCoderEntries | VERIFIED | line 149 DisallowUnknownFields; line 43 exact-match; line 46/83 validateCoderEntries wired for external catalogs |
| `internal/recommend/coder.go` | pickCoder + CoderFit, reusing kvCacheBytes/addSaturating/headroomBytes | VERIFIED | pickCoder evaluates role:"coder" at m.AgentCtx; saturating add; CoderFit no-omitempty struct |
| `internal/recommend/recommend.go` | schemaVersion=3, Coder above SchemaVersion, stamped all paths | VERIFIED | recommendSchemaVersion=3; Coder field (no omitempty) above last SchemaVersion; finalizeRecommendation stamps 3 paths |
| `cmd/villa/testdata/recommend.golden.json` | schema-3 frozen with populated coder block | VERIFIED | coder block (qwen3-coder-30b-a3b, agent_ctx 65536, fits:true, residency:swap) then schema_version 3 |
| `qualification/*/verdict.md` (×3) | PASS verdict + measured KV/GTT + cache-reuse | VERIFIED | all 3 VERDICT: PASS with measured-vs-computed tables, offload 49/49, PASS-criteria tables |
| `24-TOOLBOX-DECISION.md` | numbered D-11/D-13 KEEP decision citing evidence | VERIFIED | D-13 KEEP; Checks 1-3; Freeze Ratification before freeze |
| `24-QUALIFICATION-EVIDENCE.md` | per-entry summary + reconciliation dispositions | VERIFIED | per-entry table, D-09 reconciliation (no min_envelope fold needed), A3 cache_reuse_safe truth-up, D-10/D-12 dispositions |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| seed.json | catalog.go | DisallowUnknownFields decode, struct+seed in ONE commit | WIRED | both landed in 1509717; agent_ctx field round-trips |
| load.go | SupportedSchema | exact-match schema window | WIRED | line 43 mismatch→seed fallback |
| recommend.go | coder.go | Pick computes coder after envelope shrink, stamps all 3 paths | WIRED | line 195 pickCoder post-shrink; 170/199/202 finalize |
| coder.go | internal/catalog | m.Role / m.AgentCtx fit dims | WIRED | reads Role/AgentCtx/WeightBytes/kv dims |
| cmd/villa/recommend.go | Recommendation.Coder | JSON encoder + gated table section | WIRED | Coder rendered in golden + human-table (be8ee0e) |
| qualification verdicts | seed.json | cache_reuse_safe truth-up, delete/re-pin per verdict | WIRED | all PASS→all KEEP; cache_reuse_safe=true matches probes (e18ce7b) |
| 24-TOOLBOX-DECISION.md | Phase 25 inference seam | KEEP → nothing lands at inference seam | WIRED | KEEP decision; no digest change deferred to Phase 25 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| recommend coder block | CoderFit | pickCoder over real catalog.Models | Yes — golden shows non-empty fit @ agent_ctx 65536 from real seed entry | FLOWING |
| seed.json cache_reuse_safe | bool | on-hardware probe verdict (cache-reuse.txt) | Yes — truth-up from measured turn-2 cache_n>0 | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Catalog package compiles + tests | `go test ./internal/catalog -count=1` | ok | PASS |
| Recommend package tests (coder fit) | `go test ./internal/recommend -count=1` | ok | PASS |
| CLI golden contract | `go test ./cmd/villa -count=1` | ok (3.6s) | PASS |
| Seam grep gate (no literals in catalog/recommend) | `go test ./internal/inference -run TestSeamGrepGate` | PASS | PASS |
| Full gate | `make check` | green (vet + all packages ok) | PASS |
| No backend literals in catalog/recommend Go | grep kyuz0/sha256/HSA_OVERRIDE/podman/Vulkan0/ROCm0 | NONE FOUND | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| CODER-01 | 24-01, 24-04 | Catalog ships role:"coder" entries, revision-pinned + provenance (schema 2→3 append-only) | SATISFIED | seed.json 3 coder entries, revision-pinned, schema 3; chat entries untouched (Truths 1-4) |
| CODER-02 | 24-02 | recommend computes coder fit at agent ctx after embed+chat, honest residency swap/shared | SATISFIED | coder.go pickCoder + residency inequality; recommend schema 3; golden (Truths 5-9) |
| CODER-03 | 24-03, 24-04 | Coder entries qualified agent-in-the-loop on hardware, measured KV; toolbox re-pin decision recorded | SATISFIED | 3 PASS verdicts, build 9496 by digest, measured KV/GTT, crush tool loops, D-13 KEEP (Truths 10-14) |

No orphaned requirements: REQUIREMENTS.md maps CODER-01/02/03 to Phase 24 and all are claimed by plans 24-01/02/03/04.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | none | — | No TBD/FIXME/XXX debt markers, no stubs, no orphaned artifacts in phase-modified files |

### Human Verification Required

None. All success criteria are observable in the codebase and on-disk evidence; behavioral checks pass via fresh `make check`. The on-hardware qualification was performed and recorded in 24-03 (operator-approved, `qualification/REVIEW.md`); its evidence (server versions, KV/GTT measurements, crush transcripts, cache-reuse probes) is verifiable on disk without re-running hardware.

### Gaps Summary

No gaps. All 14 must-haves and all 4 ROADMAP success criteria are verified against the actual codebase and qualification evidence:

- **SC1 (catalog):** 3 revision-pinned coder entries at schema 3, chat entries byte-untouched.
- **SC2 (recommend):** coder fit computed at agent_ctx after reservation+chat fit; residency a pure inequality; coder block always stamped (incl. refusal); ONE golden re-freeze (be8ee0e).
- **SC3 (qualification):** all 3 entries PASS a real multi-step tool-call loop through llama-server --jinja on the pinned digest (build 9496), KV measured at agent ctx on gfx1151.
- **SC4 (toolbox decision):** D-13 KEEP recorded with on-box evidence before the catalog froze.

Code-review WR-01/02/03 fixes confirmed in commit 3df4687 (validateCoderEntries KV-dimension + sampling/temperature bounds, exercised by catalog_test.go). TestSeamGrepGate green; no backend literals leaked into catalog/recommend.

Note: the spec referenced "D-11 KEEP"; the record numbers it D-13 ("ratifies the D-11 toolbox keep/re-pin question with on-hardware evidence") with the same KEEP outcome — substantively identical, properly versioned.

---

_Verified: 2026-06-13_
_Verifier: Claude (gsd-verifier)_
