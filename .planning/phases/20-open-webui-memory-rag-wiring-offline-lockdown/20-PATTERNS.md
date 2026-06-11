# Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown - Pattern Map

**Mapped:** 2026-06-09
**Files analyzed:** 5 modified + 2 new (1 golden, 1 test/proof split)
**Analogs found:** 7 / 7 (all touchpoints have an in-repo precedent)

> This is an **env-wiring + runtime-proof** phase — no new package, no new published
> port. Every change mirrors an existing in-repo pattern. Concrete file:line excerpts
> below; the planner should make each plan action reference these directly.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/orchestrate/openwebui.go` (MOD — parameterize `buildOpenWebUIView`) | config/render-view builder | transform (config→env slice) | `internal/orchestrate/memory.go` `buildQdrantView`/`buildEmbedView` (config-resolved view builders) | exact |
| `internal/orchestrate/render.go` (MOD — pass `mv`+`memoryEnabled` to `buildOpenWebUIView`) | render orchestration | transform | `render.go:160-185` existing `if in.Cfg.MemoryEnabled` memory branch | exact |
| `internal/orchestrate/testdata/villa-openwebui.container.memory.golden` (NEW) | test fixture | golden | `testdata/villa-openwebui.container.golden` (existing memory-off golden) | exact |
| `internal/orchestrate/render_test.go` (MOD — memory-aware telemetry test + new memory-on golden test) | test | golden + frozen-count | `TestRenderOpenWebUITelemetryFrozen` (render_test.go:300) + `TestRenderOpenWebUIContainerGolden` (render_test.go:275) | exact |
| `cmd/villa/install_memory.go` (MOD) or `cmd/villa/verify_memory.go` (NEW) — `evalRagSmoke` pure core + `liveRagSmoke` seam | proof seam (impure edge + pure core) | request-response (REST drive) + event (egress assert) | `evalMemoryProof`/`liveMemoryProof`/`runProbeCurl`/`qdrantWritableProbe` (install_memory.go:180-341) | exact |
| `internal/config/villaconfig.go` (READ-ONLY source) | config (source of truth) | — | self (fields already exist) | n/a — no edit |
| `internal/inference/seam_test.go` (`TestSeamGrepGate`) | test gate | — | self (must stay green; no new image literal needed) | n/a — no edit |

## Pattern Assignments

### `internal/orchestrate/openwebui.go` (parameterize `buildOpenWebUIView`)

**Analog:** `internal/orchestrate/memory.go` `buildQdrantView`/`buildEmbedView` — config-resolved view builders that compose endpoint pieces from resolved values, never re-typing host literals.

**Current signature (no args, UNCONDITIONAL)** — `openwebui.go:99-130`:
```go
func buildOpenWebUIView() openWebUIView {
	return openWebUIView{
		ContainerName: openWebUIContainerName,
		Image:         openWebUIImage,
		Network:       networkAttach,
		PublishPort:   openWebUIPublishPort,
		Volume:        openWebUIVolumeMount,
		Env: []envPair{
			{Key: "OPENAI_API_BASE_URL", Value: "http://" + containerName + ":8080/v1"},
			{Key: "ENABLE_OPENAI_API", Value: "True"},
			// ... 11 ordered entries total, ending:
			{Key: "WEBUI_AUTH", Value: "True"},
		},
	}
}
```

**Pattern to apply (RESEARCH Pattern 1, D-02/D-04):** make it
`buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool)`; keep the existing
11 entries byte-identical, then `if memoryEnabled { env = append(env, ...D-09 block...) }`.

**URL composition precedent (no re-typed literals)** — mirror `memory.go` `buildEmbedExec`/`buildQdrantView` which compose from `mv.EmbedAddr`/`mv.EmbedPort`/`mv.QdrantAddr`/`mv.QdrantPort`:
```go
// from RESEARCH §"Composing the Qdrant/embed URLs", consistent with memory.go
{Key: "QDRANT_URI",              Value: fmt.Sprintf("http://%s:%d", mv.QdrantAddr, mv.QdrantPort)},
{Key: "RAG_OPENAI_API_BASE_URL", Value: fmt.Sprintf("http://%s:%d/v1", mv.EmbedAddr, mv.EmbedPort)},
{Key: "RAG_EMBEDDING_MODEL",     Value: mv.EmbeddingModel},
```
**`fmt` import** must be added to `openwebui.go` (currently has none; `memory.go` uses `strconv`/`strings` for the same job).

**Sentinel-key precedent** for `RAG_OPENAI_API_KEY=sk-no-key-required`: the existing
`OPENAI_API_KEY=sk-no-key-required` at `openwebui.go:118` (a non-secret placeholder, frozen by golden) — reuse the exact same comment rationale.

**Full D-09 appended block** (verbatim values, from RESEARCH env-contract table; ordering is Claude's discretion within the group, D-02):
`VECTOR_DB=qdrant`, `QDRANT_URI` (composed), `ENABLE_QDRANT_MULTITENANCY_MODE=True`,
`QDRANT_COLLECTION_PREFIX=open-webui`, `RAG_EMBEDDING_ENGINE=openai`,
`RAG_OPENAI_API_BASE_URL` (composed), `RAG_OPENAI_API_KEY=sk-no-key-required`,
`RAG_EMBEDDING_MODEL` (from mv), `RAG_EMBEDDING_QUERY_PREFIX=search_query:`,
`RAG_EMBEDDING_CONTENT_PREFIX=search_document:`, `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`,
`ENABLE_MEMORIES=True`, **`ENABLE_PERSISTENT_CONFIG=False`** (mandatory, D-03).
`QDRANT_API_KEY` omitted (empty default accepted). No `RAG_EMBEDDING_PREFIX_FIELD_NAME`.

**Seam note (D-04):** values flow from `mv` (config) — NO `villa-qdrant`/`villa-embed`/port literals re-typed in `openwebui.go`; `TestSeamGrepGate` stays green untouched (these are not GPU/image tokens; the image const `openWebUIImage` already lives here behind the managed-service seam, openwebui.go:20, and is allowlisted as a managed-service const, not inference).

---

### `internal/orchestrate/render.go` (pass the view inputs)

**Analog:** the existing memory branch in the SAME function — `render.go:160-185`:
```go
if in.Cfg.MemoryEnabled {
	mv := memory.RenderView(in.Cfg) // D-11 resolved-values handoff
	qdrantContainerText, err := execTemplate(tmpl, "qdrant.container.tmpl", buildQdrantView(mv.QdrantAddr))
	...
	embedContainerText, err := execTemplate(tmpl, "embed.container.tmpl", buildEmbedView(embedGGUFFilename, mv.EmbedAddr, mv.EmbedPort))
	...
}
```

**Change site** — `render.go:125` (OWUI is rendered UNCONDITIONALLY, ABOVE the memory branch):
```go
owuiContainerText, err := execTemplate(tmpl, "openwebui.container.tmpl", buildOpenWebUIView())
```
becomes (compute `mv` + flag once and pass both):
```go
mv := memory.RenderView(in.Cfg)
owuiContainerText, err := execTemplate(tmpl, "openwebui.container.tmpl", buildOpenWebUIView(mv, in.Cfg.MemoryEnabled))
```
Note: the existing memory branch (render.go:160) re-derives `mv` locally; the planner may hoist `mv := memory.RenderView(in.Cfg)` above line 125 and reuse it in both places (it is pure, cheap, and identical). `memory` is already imported in render.go (used at line 161). No template change — `openwebui.container.tmpl`'s `{{range .Env}}Environment=` loop already handles the extra keys.

---

### `internal/orchestrate/testdata/villa-openwebui.container.memory.golden` (NEW)

**Analog:** the existing memory-off golden `testdata/villa-openwebui.container.golden`
(29 lines; 11 `Environment=` lines, header `# … source: config.toml`, `[Unit]`/`[Container]`/`[Service]`/`[Install]` blocks).

**Pattern (D-05):** the existing golden stays **byte-identical** (memory-off, must keep
passing `TestRenderOpenWebUIContainerGolden`). The NEW `.memory.golden` is the memory-ON
render: same 29-line shell + the appended D-09 `Environment=` lines (≈13 more). Generate
via the `-update` flag pattern (see `goldenCompare` / `var update` convention in render_test.go).

---

### `internal/orchestrate/render_test.go` (memory-aware telemetry + new golden test)

**Analog 1 — telemetry-frozen test** `TestRenderOpenWebUITelemetryFrozen` (render_test.go:300-325). It derives expected env from the builder and bidirectionally freezes count:
```go
env := buildOpenWebUIView().Env          // render_test.go:314 — WILL NOT COMPILE after parameterization
for _, p := range env { want := "Environment=" + p.Key + "=" + p.Value; if !strings.Contains(c.Text, want) {...} }
got := strings.Count(c.Text, "Environment=")
if got != len(env) { t.Errorf("...want exactly %d", len(env)) }
```
**Pattern to apply (RESEARCH Pattern 2):** call the builder with a memory-on view for the
memory-on unit:
```go
env := buildOpenWebUIView(memory.RenderView(memoryOnCfg), true).Env
```
and add a memory-OFF assertion against the memory-off unit (exactly 11 lines). This IS the
D-05 "re-audit." Note `fixtureInput()` (the existing helper at render_test.go:276/301) is
memory-OFF; a memory-ON fixture (set `MemoryEnabled:true` + the default memory fields) is
needed — config defaults already provide valid values (villaconfig.go:96-102).

**Analog 2 — golden test** `TestRenderOpenWebUIContainerGolden` (render_test.go:275-282):
```go
units, _ := Render(fixtureInput())
c := unitByName(t, units, "villa-openwebui.container")
goldenCompare(t, "villa-openwebui.container.golden", c.Text)
```
Mirror it as `TestRenderOpenWebUIMemoryContainerGolden` driving a memory-on fixture and
comparing `villa-openwebui.container.memory.golden`.

**Analog 3 — frozen-count discipline reference:** `TestRenderROCmEnvGroupFrozen` (render_test.go:155-202) is the same `strings.Count(...)==len(want)` env-freeze pattern for a conditional (ROCm) env set — proves the "count exactly N" idiom is the house style for conditional env blocks.

---

### `cmd/villa/install_memory.go` (or NEW `cmd/villa/verify_memory.go`) — runtime RAG smoke proof

**Analog:** the entire Phase-19 proof seam in `install_memory.go:155-341`. Mirror its
**four-layer** shape exactly:

1. **Verdict type** — `memoryProof` (install_memory.go:159-162): `{status preflight.Status; detail string}`. PASS/FAIL only, no WARN; FAIL carries refuse-with-remediation detail. Reuse this type or define an identical `ragProof`.

2. **Pure core** — `evalMemoryProof` (install_memory.go:180-208): maps injected probe outcomes → verdict; every failure path is a `StatusFail` with a `check systemctl … then re-run` remediation. RESEARCH Pattern 3 gives the new core shape:
```go
func evalRagSmoke(uploadCite func() (answer string, citedSource bool, err error),
                  egressBlocked func() (blocked bool, err error)) memoryProof {
	blocked, err := egressBlocked()
	if err != nil { return fail("could not run the egress negative-control probe — refusing to declare zero-outbound") }
	if !blocked { return fail("egress is NOT blocked: an external host was reachable during the test") }
	ans, cited, err := uploadCite()
	if err != nil { return fail("the document-upload RAG path did not complete") }
	if !strings.Contains(ans, wantFact) || !cited { return fail("answer did not use the uploaded content / no citation") }
	return pass("document upload retrieved + cited with zero outbound")
}
```
The **negative-control-first** ordering (assert egress actually blocked BEFORE trusting a clean drive) is the honesty-by-construction discipline; a silent skip = FAIL, mirroring `evalMemoryProof`'s no-WARN posture.

3. **Live seam** — `liveMemoryProof` (install_memory.go:227-273) builds closures over `runProbeCurl` and sources the helper image from `orchestrate.EmbedImage()` (no re-typed image literal). The new `liveRagSmoke`:
   - **`uploadCite`** drives the OWUI REST RAG path over loopback `127.0.0.1:3000` (the existing PublishPort, no new port — D-11): signin→`POST /api/v1/knowledge/create`→`POST /api/v1/files/` (poll `process/status`)→`POST /api/v1/knowledge/{id}/file/add`→`POST /api/chat/completions` with `files:[{type:collection,id}]`. Sequence in RESEARCH §"Driving the OWUI RAG path".
   - **`egressBlocked`** is a negative control mirroring `runProbeCurl` (install_memory.go:322-341) — `podman run --rm --network villa <img> curl -sf --max-time 5 https://huggingface.co/` that MUST fail (`blocked := err != nil`). From RESEARCH §"Negative control".

4. **Fixed-arg podman exec** — `runProbeCurl` (install_memory.go:322-341): `podman run --rm --network villa --entrypoint curl <helperImage> <args...>`, every arg fixed, no shell. Reuse verbatim; the new probes are additional `runProbeCurl` calls.

**Input struct** — mirror `memoryProofInput` (install_memory.go:167-174); new `ragSmokeInput{ owuiAddr string; owuiPort int; question, wantFact string }` (loopback addr/port). Config-resolved, never shell-interpolated.

**Wiring (Claude's discretion per D-10):** the existing memory proof is wired as `d.memoryProofFn(...)` invoked at install.go:525-542 gated on `cfg.MemoryEnabled`, refusing with `exitBlocked` on FAIL. The runtime RAG smoke is **on-hardware by nature** (needs the live container + egress control) — RESEARCH recommends an on-hardware verification-wave step or a dedicated verify subcommand rather than blocking every install. The **pure `evalRagSmoke` core is unit-testable off-hardware** with injected probes (mirror the existing `evalMemoryProof` unit tests). Match whichever wiring the planner chooses to the install.go:525 gating precedent.

---

### `internal/config/villaconfig.go` (read-only source)

Resolved memory fields the env values source from already exist with defaults
(villaconfig.go:60-102): `MemoryEnabled` (default false), `EmbeddingModel`
(`nomic-embed-text-v1.5`), `EmbeddingDim` (768), `QdrantAddr` (`villa-qdrant`),
`QdrantPort` (6333), `EmbedAddr` (`villa-embed`), `EmbedPort` (8080). **No edit** —
consumed via `memory.RenderView(cfg)`. `EmbeddingDim`/`EmbeddingModel` carried through
but only `EmbeddingModel` is rendered into OWUI env (dim is an OWUI-runtime concern).

## Shared Patterns

### Config-sourced values, no re-typed host literals (WR-01 / D-04)
**Source:** `internal/orchestrate/memory.go:153-205` (`buildQdrantView`/`buildEmbedView`/`buildEmbedExec` compose from `mv.*` pieces) and `internal/memory/memory.go:95-104` (`RenderView`).
**Apply to:** `buildOpenWebUIView` (compose `QDRANT_URI`/`RAG_OPENAI_API_BASE_URL`/`RAG_EMBEDDING_MODEL` from `mv`), `liveRagSmoke` (config-resolved loopback addr/port).

### Byte-frozen render contract — append-only + single deliberate re-freeze (D-05)
**Source:** `internal/orchestrate/render_test.go:294-325` (`TestRenderOpenWebUITelemetryFrozen` bidirectional count-freeze) + `goldenCompare`/`-update` convention; `TestRenderROCmEnvGroupFrozen` (155-202) as the conditional-env-block precedent.
**Apply to:** keep `villa-openwebui.container.golden` byte-identical (memory-off); add `.memory.golden`; make the telemetry test memory-aware.

### Honesty-by-construction proof seam — pure core + injected probes, FAIL never skip (D-10)
**Source:** `cmd/villa/install_memory.go:159-341` (`memoryProof` type → `evalMemoryProof` pure core → `liveMemoryProof`/`runProbeCurl`/`qdrantWritableProbe` injected fixed-arg seams) wired at `cmd/villa/install.go:525-542` (refuse-with-remediation → `exitBlocked`).
**Apply to:** the runtime zero-outbound RAG smoke (`evalRagSmoke` + `liveRagSmoke`), with a negative-control egress probe that MUST fail.

### Fixed-arg `podman`/`curl` over `villa.network`, no shell, no host port (D-11 / T-19-10/11)
**Source:** `cmd/villa/install_memory.go:317-341` (`runProbeCurl`), `memoryProofNetwork = "villa"` (214), helper image via `orchestrate.EmbedImage()` (228).
**Apply to:** both the RAG-drive curls (loopback REST) and the egress negative-control probe.

### Seam-gate cleanliness (TestSeamGrepGate stays green)
**Source:** `internal/inference/seam_test.go:92-116` (`isSeam` allowlist includes `orchestrate/memory.go`; `openWebUIImage` is a managed-service const, openwebui.go:20).
**Apply to:** NO new image/backend literal is introduced (env values are config-sourced; helper image reused via `EmbedImage()`). `seam_test.go` needs **no edit** — confirm green, do not extend the allowlist.

## No Analog Found

None. Every touchpoint has a direct in-repo precedent (the Phase-18/19 memory spine + the
Phase-4 OWUI render + the Phase-19 proof seam). This phase is deliberately
"extend-the-pattern," not "introduce-new-pattern."

## Metadata

**Analog search scope:** `internal/orchestrate/` (openwebui.go, memory.go, render.go, render_test.go, testdata/), `internal/memory/`, `internal/config/`, `cmd/villa/` (install.go, install_memory.go), `internal/inference/seam_test.go`.
**Files scanned:** 9 read in full or targeted; all 7 touchpoints matched.
**Pattern extraction date:** 2026-06-09
