# Phase 22: Control-Plane Fit + Host Gate - Pattern Map

**Mapped:** 2026-06-10
**Files analyzed:** 14 new/modified files (1 new source, 1 new test, 11 modified, 1 golden re-freeze)
**Analogs found:** 13 / 14 (the only partial-novelty item is the concurrent embed-drive sampling — composed from two existing analogs)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/recommend/recommend.go` (mod) | pure-core service (fit math) | transform | itself — `Pick` :123-147, degraded note :136-139, `finalizeRecommendation` :154 | exact (in-file) |
| `internal/recommend/envelope.go` (mod, optional) | pure-core utility | transform | itself — `resolveEnvelope` :45, named-const discipline :10-17 | exact (in-file) |
| `internal/memory/footprint.go` (mod — export conservative default) | pure-core utility | transform | itself — `Footprint` :38, `embedFootprints` :29 | exact (in-file) |
| `internal/preflight/checks_memory.go` (NEW) | pure-core check (preflight) | request-response (probe→verdict) | `internal/preflight/checks_resources.go` (`checkResources` :85) | exact |
| `internal/preflight/checks_memory_test.go` (NEW) | test | — | `internal/preflight/checks_resources_test.go` (table tests, injected statfs) | exact |
| `internal/preflight/preflight.go` (mod — exported memory runner) | pure-core registry | batch | itself — `Run`/`RunWithResources` :142-168, `pass/warn/fail` :171-184 | exact (in-file) |
| `internal/doctor/doctor.go` (mod — Deps growth, memory fold, offload-N/A) | pure-core aggregator | event-fold (worst-wins) | itself — `Deps.RunROCmImage` nil-safe seam :142, supersession down-rank :284-295, `findingFromCheck` :313 | exact (in-file) |
| `cmd/villa/recommend.go` (mod — thread memory inputs, render reservation line) | controller (cobra verb) | request-response | itself :41-76 + `liveLoadedMemoryEnabled` (`cmd/villa/install_memory.go:138`) | exact |
| `cmd/villa/preflight.go` (mod — fail-soft config gate, append memory checks) | controller (cobra verb) | request-response | itself :47-63 + `liveLoadedMemoryEnabled` | exact |
| `cmd/villa/doctor.go` (mod — liveDoctorDeps growth, under-load proof seam) | controller + live wiring | request-response + streaming-load | `liveDoctorDeps` :156 + `runProbeCurl`/`liveMemoryProof` (`install_memory.go:227,322`) + `liveStatusDeps` residency wiring (`status.go:181-196`) | exact (composition) |
| `cmd/villa/install.go` (mod — pick seam memory inputs, gated memory checks, optional MinMemBytes) | controller + live wiring | request-response | itself — `ResourceReq` build :268-273, `pick` seam :980-986 | exact (in-file) |
| Pick call-site ripple: `cmd/villa/status.go:405`, `model.go:331`, `inference.go:144`, `backend.go:392`, `dashboard.go:268` | controller utilities | transform | each site is its own analog (one-line `recommend.Pick(...)` calls) | exact |
| `cmd/villa/testdata/recommend.golden.json` (re-freeze) | test fixture | — | golden `-update` harness (`cmd/villa/detect_test.go:13` package-level flag) | exact |
| Doctor under-load proof core (verdict mapping, wherever planner places it) | pure-core service | event-driven (load + sample) | `evalMemoryProof` (`install_memory.go:180`) for the pure-verdict shape; `RunningOffloadVerdict` (`running_offload.go:327`) for the assert | role-match (composition of two exact analogs) |

## Pattern Assignments

### `internal/recommend/recommend.go` (pure core, transform)

**Analog:** itself. Insertion point is `Pick`, immediately after `resolveEnvelope` and BEFORE the degraded note / pick calls.

**Current `Pick` body — the code being modified** (`recommend.go:123-147`):
```go
func Pick(p detect.HostProfile, c catalog.Catalog, ov Overrides) Recommendation {
	envelope, degraded, ok := resolveEnvelope(p)
	if !ok {
		// No usable envelope and no safe floor derivable — refuse rather than
		// guess high (D-14). Empty Model signals the refusal.
		return finalizeRecommendation(Recommendation{
			Backend: defaultBackend,
			Notes:   []string{"refusing to recommend: usable memory envelope is unknown and no safe floor is derivable (neither GTT envelope nor total RAM detected)"},
		}, p)
	}

	var notes []string
	if degraded {
		notes = append(notes, fmt.Sprintf(
			"DEGRADED ESTIMATE: real GTT envelope unknown; sized against a conservative %.0f%%-of-RAM floor (%s). Verify before relying on this pick (D-14).",
			degradedFloorFraction*100, humanGiB(envelope)))
	}

	// An explicit --model override takes precedence and is re-validated.
	if ov.Model != "" {
		return finalizeRecommendation(pickOverride(c, ov, envelope, degraded, notes), p)
	}

	return finalizeRecommendation(pickBest(c, ov, envelope, degraded, notes), p)
}
```
The D-01 reservation subtracts from `envelope` between the `if !ok` refusal and the degraded-note append, so `headroomBytes(envelope)`, the OOM guard (`total > envelope`, `pickBest` :232), and `UsableEnvelopeBytes` all see the shrunken value. D-01: `Pick`'s signature grows a memory-inputs value (e.g. a 4th param struct; zero value = memory off = byte-identical math).

**Degraded-note template — the D-02 "reserved conservatively" note copies this shape** (lines 135-139 above): `fmt.Sprintf` into `notes` with an ALL-CAPS prefix, the cause, the value via `humanGiB`, and a decision-ID citation.

**Schema-version + last-field rule** (`recommend.go:26-29, 100-102`):
```go
// recommendSchemaVersion is the Recommendation contract self-version. It is the
// LAST tagged field of Recommendation and surfaces unconditionally in --json so
// dashboards can gate on additive growth (D-06/D-07). Start at 1.
const recommendSchemaVersion = 1
...
	// SchemaVersion is the Recommendation contract self-version and MUST stay the
	// LAST tagged field (append-only discipline; new fields go above it, D-06/D-07).
	SchemaVersion int `json:"schema_version"`
```
New D-03 fields (reservation bytes + memory-considered marker) go ABOVE `SchemaVersion`; bump the const 1→2. Field-comment style: every field carries a doc comment naming the decision ID, matching `Recommendation` :67-103.

**Contract-stamp pattern** (`finalizeRecommendation`, :154-162) — stamps `SchemaVersion` unconditionally on every return path, including refusals. The new fields must populate on ALL paths the same way (zero/false when memory off so the off-path JSON shape changes only by the new keys).

**Footprint consumption (single source, typed-Unknown)** — copy from `internal/memory/footprint.go:38-46`:
```go
func Footprint(modelID string) detect.Bytes {
	if b, ok := embedFootprints[modelID]; ok {
		return detect.KnownBytes(b, "memory: pinned embedding footprint reservation")
	}
	if modelID == "" {
		return detect.UnknownBytes("memory: no footprint known for empty embedding model id", modelID)
	}
	return detect.UnknownBytes("memory: no footprint known for embedding model "+modelID, modelID)
}
```
D-02: on `.Known == false`, reserve a conservative default exported from `internal/memory` (NEW exported accessor next to `embedFootprints` :29-31 — never re-type `512 << 20` in recommend). The accessor-export pattern to copy: `orchestrate.EmbedImage()` (`internal/orchestrate/memory.go:45-47`): unexported const + one-line exported func with a "so downstream readers never re-type the literal" doc comment.

**Underflow guard:** envelope is `uint64` — guard `if reservation >= envelope` before subtracting (mirror `conservativeFloor`'s zero-check discipline, `envelope.go:28-37`); the resulting 0 envelope falls into `pickBest`'s existing no-fit refusal path (:244-252).

---

### `internal/preflight/checks_memory.go` (NEW — pure-core checks)

**Analog:** `internal/preflight/checks_resources.go` — mirror this file end to end.

**Injectable statfs seam + existing-ancestor walk** (`checks_resources.go:28-62`):
```go
// statfsFunc is the injectable free-disk seam so tests assert disk-too-small
// without needing a real undersized filesystem. It returns the free bytes at path
// and whether the statfs succeeded.
type statfsFunc func(path string) (freeBytes uint64, ok bool)

// liveStatfs reads real free space via syscall.Statfs — structured, locale-proof,
// and NOT shelling to `df` ("Don't Hand-Roll", T-03-01). It walks up to an
// existing ancestor so a not-yet-created data dir still reports its filesystem's
// free space.
func liveStatfs(path string) (uint64, bool) {
	p := existingAncestor(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
```
Reuse `statfsFunc`/`liveStatfs`/`existingAncestor` directly (same package) — do not redeclare. The NEW seam this file adds is the volume-root resolver (`volumeRootFn func() (string, bool)` mirroring `statfsFunc`), live-wired as a fixed-arg `exec.Command("podman", "system", "info", "--format", "{{.Store.VolumePath}}")`.

**Check function shape — classification core with injected probes, WARN-on-unevaluable, FAIL-on-confident-bad** (`checkResources`, `checks_resources.go:85-130`):
```go
func checkResources(p detect.HostProfile, req ResourceReq, statfs statfsFunc) CheckResult {
	const (
		id   = "PRE-04"
		name = "Free disk + free memory"
	)
	remediation := "Free up disk under the model data dir and/or close memory-heavy processes; ..."

	// --- Memory: free RAM must clear the envelope. ---
	if req.MinMemBytes == 0 || !p.MemAvailableBytes.Known {
		// Cannot evaluate the memory requirement (unknown envelope or MemAvailable).
		return warn(id, name, TierBlock,
			"could not verify free memory against the runtime envelope",
			remediation,
			joinProvenance("usable_envelope_bytes", p.MemAvailableBytes.Source),
			p.MemAvailableBytes.Raw)
	}
	if p.MemAvailableBytes.Value < req.MinMemBytes {
		return fail(id, name,
			fmt.Sprintf("free memory %s < required envelope %s", humanGiB(p.MemAvailableBytes.Value), humanGiB(req.MinMemBytes)),
			remediation, p.MemAvailableBytes.Source, "")
	}

	// --- Disk: free space at the data dir must clear the model weight size. ---
	freeDisk, ok := statfs(req.DataDir)
	if !ok {
		return warn(id, name, TierBlock,
			fmt.Sprintf("could not statfs %q to verify free disk", req.DataDir),
			remediation, "syscall.Statfs", "")
	}
	if freeDisk < req.MinDiskBytes {
		return fail(id, name,
			fmt.Sprintf("free disk %s at %q < required %s", humanGiB(freeDisk), req.DataDir, humanGiB(req.MinDiskBytes)),
			remediation, "syscall.Statfs:"+req.DataDir, "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("free memory %s ≥ %s; free disk %s ≥ %s at %q", ...),
		joinProvenance(p.MemAvailableBytes.Source, "syscall.Statfs:"+req.DataDir))
}
```
D-07b headroom gate reuses `p.MemAvailableBytes` exactly as above, with floor = `memory.Footprint(embeddingModel)` (conservative default on Unknown — same D-02 source as recommend). Provenance always names the tool/path; `Raw` carries unparsed output.

**Floor-constant discipline** — copy the named-const-with-rationale style (`preflight.go:117-128`):
```go
const (
	// minRunnableMemFloorBytes is a conservative minimal free-memory floor: a host
	// below this cannot run even the smallest catalog model (...). It is
	// intentionally small — the precise per-model memory requirement is checked at
	// install time, not here.
	minRunnableMemFloorBytes uint64 = 4 << 30 // 4 GiB
	...
)
```

**Check IDs:** opt-in-subsystem topic prefix per the ROCm precedent (`ROCM-PRE-gfx/...`) — recommend `MEM-PRE-disk` / `MEM-PRE-headroom`; never renumber the frozen `PRE-01..07` sequence.

---

### `internal/preflight/preflight.go` (mod — exported memory-checks runner)

**Analog:** `Run`/`RunWithResources` (`preflight.go:142-168`):
```go
func Run(p detect.HostProfile) []CheckResult {
	return RunWithResources(p, ResourceReq{
		MinDiskBytes: minModelDiskFloorBytes,
		MinMemBytes:  minRunnableMemFloorBytes,
		DataDir:      defaultDataDir(),
	})
}

func RunWithResources(p detect.HostProfile, req ResourceReq) []CheckResult {
	return []CheckResult{
		checkVulkanIGPU(p),
		checkPodmanRootless(livePodmanDeps()),
		...
		checkResources(p, req, liveStatfs),
		...
	}
}
```
New exported runner (e.g. `RunMemory(p detect.HostProfile, in MemoryGateInput) []CheckResult`) follows this shape: stable check ordering, live seams bound inside, an input struct mirroring `ResourceReq` (:19-23) for thresholds + the embedding model id. Do NOT load config here and do NOT change `Run`'s signature — emission is the caller's decision (D-06).

**Result-constructor helpers to use** (`preflight.go:171-184`):
```go
func pass(id, name string, tier Tier, detail, provenance string) CheckResult { ... }
func warn(id, name string, tier Tier, detail, remediation, provenance, raw string) CheckResult { ... }
func fail(id, name string, detail, remediation, provenance, raw string) CheckResult { ... } // fail is always TierBlock
```

---

### `internal/doctor/doctor.go` (mod — Deps growth, memory fold, offload-N/A, under-load finding)

**Analog:** itself.

**Nil-safe optional Deps seam — copy `RunROCmImage`'s pattern for every new field** (`doctor.go:134-142`):
```go
	// RunROCmImage is the image-AWARE ROCm host-prep gate (Option B): ...
	// ... NIL-SAFE: when
	// nil (e.g. the newDoctorDeps test double, or a non-ROCm backend), Aggregate falls
	// back to preflight.RunROCm(profile) exactly as before.
	RunROCmImage func(detect.HostProfile) []preflight.CheckResult
```
New fields per D-08/D-09 (`MemoryEnabled bool`, memory service names, `RunMemoryChecks func(detect.HostProfile) []preflight.CheckResult`, `ResidencyUnderLoad func() inference.Verdict` or similar) must all be nil/zero-safe: nil → no memory findings → memory-off doctor output byte-identical.

**Preflight-check fold — reuse verbatim** (`findingFromCheck`, `doctor.go:313-324`):
```go
func findingFromCheck(c preflight.CheckResult) Finding {
	return Finding{
		ID:          c.ID,
		Name:        c.Name,
		Tier:        c.Tier.String(),   // "BLOCK" | "WARN"
		Status:      c.Status.String(), // "PASS" | "WARN" | "FAIL"
		Detail:      c.Detail,
		Remediation: c.Remediation,
		Provenance:  c.Provenance,
		Raw:         c.Raw,
	}
}
```
The memory disk/headroom findings are `for _, c := range d.RunMemoryChecks(profile) { findings = append(findings, findingFromCheck(c)) }` — no new aggregation logic (D-08).

**Verdict→Finding mapping for the under-load proof — copy `offloadFinding`'s opaque-Verdict consumption** (`doctor.go:361-384`):
```go
func offloadFinding(s status.ServiceStatus) Finding {
	v := s.Offload // inference.Verdict — read Status/Detail/Remediation ONLY (seam-clean)
	f := Finding{
		ID:         "offload:" + s.Service,
		Name:       s.Service + " GPU offload",
		Detail:     v.Detail,
		Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback / degraded backend = a real fault (BLOCK FAIL).
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "GPU offload is not happening — check the backend (`villa backend set`) and `villa logs`")
	default: // StatusWarn — offload could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "offload could not be verified — ensure the stack is running, then re-run `villa doctor`")
	}
	return f
}
```
D-09 semantics map 1:1: confident CPU fallback under embed load → BLOCK FAIL; unevaluable (services down / scrape failed) → typed-Unknown WARN; never PASS-by-default. Use `nonEmpty` (:388) so every non-PASS finding carries remediation (D-11).

**Non-GPU offload N/A handling (Pitfall 1 fix) — the down-rank precedent is the ROCm supersession** (`doctor.go:284-295`):
```go
	superseded := func(f Finding) bool {
		return rocmResidencyProven && f.Status == statusWarn && supersededROCmHostPrepID(f.ID)
	}
	worst := 0
	for _, f := range findings {
		if superseded(f) {
			continue // visible but non-rank-raising under proven ROCm residency
		}
		if r := statusRank(f.Status); r > worst {
			worst = r
		}
	}
```
Two sanctioned shapes for villa-qdrant/villa-embed `offload:*` findings: (a) skip emitting `offloadFinding` when `s.Service` matches a Deps-supplied memory service name (predicate keyed on names from `orchestrate.QdrantContainerUnitName()`/`EmbedContainerUnitName()` — see the ID-string-not-marker precedent at `doctor.go:56-66`), or (b) down-rank-but-visible exactly like `superseded` above. CRITICAL invariant to preserve either way: the conjunction with `Status == statusWarn` — a confident `statusFail` on the same finding is NEVER suppressed (no-false-green, DOCTOR-02 comment block :276-283). Health rows for the memory services already exist via the status fold — do not duplicate them.

**Schema note:** findings are data, not schema — `reportSchemaVersion = 1` (:54) stays 1; no doctor `Report`/`Finding` struct fields are needed.

---

### `cmd/villa/preflight.go` (mod — gated memory-check emission)

**Analog:** itself, `newPreflight` RunE (`cmd/villa/preflight.go:47-63`):
```go
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := detect.Probe()
			var results []preflight.CheckResult
			if inference.IsROCmFamily(backend) {
				results = preflight.RunROCm(profile)
			} else {
				// Standalone host preflight — WARN-only, behaviorally unchanged (D-03).
				results = preflight.Run(profile)
			}
			code := renderPreflight(cmd.OutOrStdout(), results, jsonOut, verbose, force)
			os.Exit(code)
			return nil
		},
```
The change appends memory checks (`results = append(results, preflight.RunMemory(...)...)`) only when memory is enabled, then flows through the unchanged `renderPreflight`. Memory-off path must stay byte-identical (frozen `preflight-pass.golden`/`preflight-warn.golden`).

**Fail-soft config gate — copy `liveLoadedMemoryEnabled`** (`cmd/villa/install_memory.go:132-144`):
```go
// liveLoadedMemoryEnabled returns the PERSISTED config.LoadVilla().MemoryEnabled — the
// AUTHORITATIVE memory gate source ... A config load error fails SOFT to false so a
// broken config never silently enables the memory stack ...
func liveLoadedMemoryEnabled() bool {
	c, err := config.LoadVilla()
	if err != nil {
		return false
	}
	return c.MemoryEnabled
}
```

**Authoritative exit constants** (`cmd/villa/preflight.go:19-23`) — never re-derive:
```go
const (
	exitPass    = 0 // all checks pass
	exitWarn    = 2 // passed with warnings (or an overridden block)
	exitBlocked = 1 // an un-overridden BLOCK check failed
)
```

---

### `cmd/villa/recommend.go` (mod — thread memory inputs + render reservation line)

**Analog:** itself. The verb already does a fail-soft config load for the catalog path (`recommend.go:48-53`) — extend the same load to source memory inputs:
```go
			catalogPath := f.catalogPath
			if catalogPath == "" {
				if cfg, err := config.LoadVilla(); err == nil {
					catalogPath = cfg.CatalogPath
				}
			}
			...
			rec := recommend.Pick(profile, cat, recommend.Overrides{
				Model: f.model,
				Quant: f.quant,
				Ctx:   f.ctx,
			})
```

**Conditional table rendering — copy the gated-line pattern so memory-off table output stays byte-identical** (`renderRecommendTable`, fit-math block :149-157 and the gated ROCm block :173-178):
```go
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  model_bytes\t%s\n", gib(rec.WeightBytes))
	fmt.Fprintf(tw, "+ KV-cache @ ctx %d\t%s\n", rec.ContextLen, gib(rec.KVCacheBytes))
	fmt.Fprintf(tw, "+ headroom\t%s\n", gib(rec.HeadroomBytes))
	fmt.Fprintf(tw, "= total\t%s\n", gib(rec.TotalBytes))
	fmt.Fprintf(tw, "%s usable envelope\t%s\n", fitsGlyph(rec.Fits), gib(rec.UsableEnvelopeBytes))
	...
	if rec.ROCmAdvice != "" {            // ← gate new output on a non-zero field,
		fmt.Fprintf(w, "\nROCm advice: %s\n", rec.ROCmAdvice)  //   exactly like this
	}
```
Print a reservation row only when reserved > 0 (D-03 / Pitfall 4). JSON path (`renderRecommend` :119-126) needs no change — the struct growth flows through.

---

### `cmd/villa/doctor.go` (mod — liveDoctorDeps growth + under-load proof live seam)

**Analog 1 — Deps wiring:** `liveDoctorDeps` (`cmd/villa/doctor.go:156-236`). Copy the seam-clean conditional-gate construction used for `rocmImageGate` (:172-187):
```go
	var rocmImageGate func(detect.HostProfile) []preflight.CheckResult
	if inference.IsROCmFamily(cfg.Backend) {
		b, berr := inference.BackendFor(cfg.Backend)
		if berr != nil {
			return doctor.Deps{}, fmt.Errorf("resolve ROCm backend image: %w", berr)
		}
		image := b.Image()
		rocmImageGate = func(p detect.HostProfile) []preflight.CheckResult {
			return preflight.RunROCmForImage(p, image)
		}
	}
	return doctor.Deps{
		Probe:        detect.Probe,
		LoadConfig:   config.LoadVilla,
		StatusReport: func() status.Report { return status.Run(*sd) },
		Backend:      cfg.Backend,
		RunROCmImage: rocmImageGate,
		...
	}, nil
```
The memory seams follow the same shape: bind closures only when `cfg.MemoryEnabled`, leave them nil otherwise (memory-off byte-identical). Service names come from `orchestrate.QdrantContainerUnitName()`/`EmbedContainerUnitName()` (`internal/orchestrate/memory.go:114-117`) — never typed literals.

**Analog 2 — the embed-load drive:** `liveMemoryProof` + `runProbeCurl` (`cmd/villa/install_memory.go:227-273, 322-341`):
```go
	// embedProbe POSTs the fixed /v1/embeddings body ...
	body, err := json.Marshal(map[string]any{
		"input":           "villa memory readiness probe",
		"model":           in.embedModel,
		"encoding_format": "float",
	})
	url := fmt.Sprintf("http://%s:%d/v1/embeddings", in.embedAddr, in.embedPort)
	out, err := runProbeCurl(ctx, helperImage,
		"-sf", "-X", "POST", url,
		"-H", "Content-Type: application/json",
		"-d", string(body),
	)
```
```go
// runProbeCurl runs `podman run --rm --network villa <helperImage> curl <args...>` as a
// FIXED-ARG exec (never a shell, T-19-10) ...
func runProbeCurl(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, error) {
	args := []string{
		"run", "--rm",
		"--network", memoryProofNetwork,
		"--entrypoint", "curl",
		helperImage,
	}
	args = append(args, curlArgs...)
	cmd := exec.CommandContext(ctx, "podman", args...) // fixed args; no shell
	...
}
```
Helper image only via `orchestrate.EmbedImage()` (:47); model id JSON-marshaled, never interpolated; bounded by `exec.CommandContext`. N requests with multi-KiB inputs, drive in a goroutine so the residency sample reads DURING load (Pitfall 6).

**Analog 3 — the residency assert inputs:** `liveStatusDeps` (`cmd/villa/status.go:181-196`) + `liveWeightBytes` (:400-407) + `RunningOffloadVerdict` (`internal/inference/running_offload.go:327-331`):
```go
		// ResidencyJournal (not JournalText) — the offload assert needs the CURRENT
		// invocation's startup, where the load_tensors residency line lives ...
		JournalText: sys.ResidencyJournal,
		Props:       liveProps,
		GTTUsed:     detect.GTTUsedBytes,
		WeightBytes: liveWeightBytes,
```
```go
func RunningOffloadVerdict(in RunningOffloadInput) Verdict {
	residency := scrapeLoadTensorsResidency(in.JournalText, in.Markers)
	floor := gttFloor(in.GTTUsedBytes, in.WeightBytes)
	v := combineOffload(residency, floor)
	...
}
```
`gttFloor` (`running_offload.go:234-263`) is the dynamic eviction signal: `used.Value >= weight → PASS`, `used.Value < weight → FAIL (weights not resident)`, Unknown/zero-weight → WARN. Markers come ONLY via `inference.BackendFor(cfg.Backend).ResidencyProof()`; weight reference via `liveWeightBytes(cfg)` — pass zero-value memory inputs there to keep `status.json.golden` provably untouched (RESEARCH call-site table).

**D-10 precondition→WARN shape** — copy `evalMemoryProof`'s pure verdict mapping with injected probes (`install_memory.go:180-208`): each failed precondition returns a typed FAIL/WARN with named-service remediation (`systemctl --user status villa-embed.service`-style text); the proof core stays unit-testable off-hardware via injected funcs.

---

### `cmd/villa/install.go` (mod — pick-seam memory inputs + gated memory checks)

**Analog:** itself. The pick seam to grow (`install.go:980-986`):
```go
		pick: func(p detect.HostProfile, ov recommend.Overrides) recommend.Recommendation {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return recommend.Recommendation{}
			}
			return recommend.Pick(p, cat, ov)
		},
```
Install MUST thread the persisted memory inputs here (Pitfall 3: a memory-blind install pick defeats CTRL-01). The gate source already exists: the `loadedMemoryEnabled` seam (live wiring `liveLoadedMemoryEnabled`, `install_memory.go:138`).

**Resource-gate composition point** (`install.go:267-273`):
```go
	// (3) Preflight gate with the concrete model's resource requirement.
	req := preflight.ResourceReq{
		MinDiskBytes: rec.WeightBytes,
		MinMemBytes:  rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes,
		DataDir:      d.modelsDir(),
	}
	checks := d.runChecks(profile, req)
```
Append the memory checks to `checks` when enabled (same emission rule as the standalone verb). Open Question 2 (adding the reservation to `MinMemBytes`) is decided here — either way `memory.Footprint` is the shared source.

---

### Pick call-site ripple (5 remaining sites)

Each is a one-line edit deciding what memory inputs to pass:

| Site | Current call | What to thread (per RESEARCH decision table) |
|------|-------------|---------------------------------------------|
| `cmd/villa/status.go:405` (`liveWeightBytes`) | `recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})` | **zero-value memory inputs** — provably keeps `status.json.golden` byte-identical (`WeightBytes` is envelope-independent for overrides) |
| `cmd/villa/model.go:331` | `recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: m.ID})` | persisted config, fail-soft (swap fit re-validation should see the shrunken envelope) |
| `cmd/villa/inference.go:144` | `recommend.Pick(profile, cat, recommend.Overrides{Model: m.ID})` | persisted config, fail-soft |
| `cmd/villa/backend.go:392` | `recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})` | persisted config, fail-soft |
| `cmd/villa/dashboard.go:268` | `recommend.Pick(profile, cat, recommend.Overrides{Model: m.ID})` | persisted config, fail-soft (NOTE: dashboard binary trap — restart `villa-dashboard.service` after rebuild for on-box verification) |

The fail-soft load shape at each site is `liveLoadedMemoryEnabled`/`liveLoadedConfig` (`install_memory.go:117-144`): load error → memory-off zero value, never an error path change.

---

### `cmd/villa/testdata/recommend.golden.json` (re-freeze)

**Analog:** the package-level golden harness. The `-update` flag is shared package-wide (`cmd/villa/detect_test.go:13` — `var update = flag.Bool(...)` convention). Re-freeze ONCE with a targeted filter to avoid golden blast radius (Pitfall 2):
```bash
go test ./cmd/villa -run TestRecommend -update
git status --porcelain cmd/villa/testdata/   # must show ONLY recommend.golden.json (+ intentionally NEW fixtures)
```

## Shared Patterns

### Typed-Unknown degradation (never bare 0, never false-green)
**Source:** `internal/memory/footprint.go:38-46`, `internal/preflight/checks_resources.go:93-100`, `internal/inference/running_offload.go:234-247`
**Apply to:** recommend reservation (D-02), both preflight gates (D-07), every doctor proof branch (D-09/D-10)
```go
	if !used.Known {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("mem_info_gtt_used unreadable", used.Raw),
			Detail: "point-in-time GTT-used could not be evaluated",
		}
	}
```
Unevaluable → WARN with provenance; confident known-bad → FAIL; Known-good → PASS. Distinguish via `.Known`, never a zero sentinel.

### Fail-soft cmd-tier config gate (pure cores never load config)
**Source:** `cmd/villa/install_memory.go:132-144` (`liveLoadedMemoryEnabled`)
**Apply to:** `cmd/villa/preflight.go`, `cmd/villa/recommend.go`, `cmd/villa/doctor.go`, all Pick ripple sites
```go
	c, err := config.LoadVilla()
	if err != nil {
		return false // broken config never silently enables the memory stack
	}
	return c.MemoryEnabled
```

### Single-source accessors (no re-typed literals; TestSeamGrepGate)
**Source:** `internal/orchestrate/memory.go:45-47, 106-117`; `internal/memory/footprint.go:29-31`
**Apply to:** every new file — volume name, unit names, helper image, footprint constant
```go
// EmbedImage returns the digest-pinned villa-embed image (mirrors QdrantImage()/
// OpenWebUIImage()) so downstream readers never re-type the literal.
func EmbedImage() string { return embedImage }
```
The D-02 conservative-default export in `internal/memory` copies this exact shape. Backend markers only via `inference.BackendFor(...).ResidencyProof()` in the cmd tier (`cmd/villa/doctor.go:179-183` precedent).

### Fixed-arg exec, no shell interpolation
**Source:** `cmd/villa/install_memory.go:322-341` (`runProbeCurl`), body marshaling :232-244
**Apply to:** the embed-load drive, the podman volume-root resolver
Fixed string-slice args into `exec.CommandContext`; user/config-sourced values go into JSON-marshaled bodies, never into command strings.

### Refuse-with-remediation + worst-wins exit mapping
**Source:** `internal/preflight/preflight.go:171-184` (pass/warn/fail constructors), `cmd/villa/preflight.go:19-23, 73-113` (`renderPreflight` + exit constants), `cmd/villa/doctor.go:81-111` (`renderDoctor`)
**Apply to:** memory checks and doctor findings — every non-PASS result carries actionable remediation text; exit mapping stays exclusively in the existing renderers (no new render paths).

### File/test conventions
- Every new file opens with a package/file doc comment stating its role + decision IDs (see `checks_resources.go`, `doctor.go`, `install_memory.go` headers).
- Tests are table-driven with injected seams (`statfsFunc` fakes, stub `doctor.Deps`); test funcs carry a doc comment naming the guarded invariant.
- `internal/recommend` uses table tests (not goldens) — add cases, refreeze nothing there.

## No Analog Found

| Capability | Role | Data Flow | Closest partial match | Note |
|------------|------|-----------|----------------------|------|
| Concurrent embed-drive + mid-load GTT sampling | live seam orchestration | event-driven | `liveMemoryProof` (sequential drive) + `liveStatusDeps` (point-in-time sample) | No existing code samples residency DURING a driven load. Compose: goroutine the `runProbeCurl` loop, sample `detect.GTTUsedBytes()` + `sys.ResidencyJournal` while in flight, bound with one overall context (≤ ~60 s). All pieces exist; only the interleaving is new. Pitfall 6 applies: the sample must be mid-drive, not before/after. |

## Metadata

**Analog search scope:** `internal/recommend`, `internal/memory`, `internal/preflight`, `internal/doctor`, `internal/inference`, `internal/orchestrate`, `cmd/villa` (+ `cmd/villa/testdata` golden harness)
**Files scanned:** 14 source files read (line-anchored excerpts); 7 production `recommend.Pick` call sites confirmed by grep
**Pattern extraction date:** 2026-06-10
