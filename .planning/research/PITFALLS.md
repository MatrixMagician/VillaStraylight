# Pitfalls Research

**Domain:** Adding a strictly-local coding agent + coding model + codebase memory to an existing self-hosted llama.cpp stack (VillaStraylight v1.4)
**Researched:** 2026-06-12
**Confidence:** HIGH overall (agent telemetry/offline behaviors verified against official docs + GitHub issues mid-2026; memory math MEDIUM where computed; Qdrant-for-code effectiveness MEDIUM — industry consensus, not yet measured on this stack)

> Scope note: this is integration-pitfall research — every item is scoped to THIS stack's invariants: strictly-local/zero-telemetry posture (PRIV-*), offload-asserting honesty (D-11), fit-math-before-install (CTRL-01 pattern), byte-frozen `status.Report`/golden contracts, orchestrate-as-the-only-impure-module, and the digest-pin discipline. Generic agent advice is omitted.

## Critical Pitfalls

### Pitfall 1: Agent phone-home defaults silently violate the zero-telemetry posture **(NON-NEGOTIABLE THREAT — zero-outbound)**

**What goes wrong:**
The agent is "wired to local endpoints" but still makes outbound connections the stack's privacy claim forbids. All three candidates phone home **by default**, each differently:

- **OpenCode (leading candidate):** (a) **unconditional startup fetch of `https://models.dev/api.json`** for provider/model discovery — hangs or fails in no-internet setups, doesn't respect `HTTP_PROXY` (Bun fetch), and is the subject of multiple open air-gap issues ([#16117](https://github.com/anomalyco/opencode/issues/16117), [#18492](https://github.com/anomalyco/opencode/issues/18492), [#4959](https://github.com/anomalyco/opencode/issues/4959)); `OPENCODE_DISABLE_MODELS_FETCH=1` and `OPENCODE_MODELS_URL` exist but are reported as *partial* — crashes/500s still occur. (b) **`autoupdate` downloads new versions at startup** by default. (c) **`share` feature uploads conversations to opencode.ai** (default `"manual"` — one keystroke from upload, not disabled). (d) Provider plumbing can pull `@ai-sdk/*` npm packages at runtime. Community verdict as of mid-2026: "doesn't work fully offline yet… depends on pulling stuff from remote resources" ([llama.cpp discussion #19619](https://github.com/ggml-org/llama.cpp/discussions/19619)).
- **Aider:** PostHog analytics enabled **for a random subset of users** unless `--no-analytics` / `analytics-disable` is set ([aider analytics docs](https://aider.chat/docs/more/analytics.html)) — a given install may or may not be phoning home, which is worse for a *provable* posture than always-on.
- **Crush:** pseudonymous usage metrics to PostHog at `data.charm.land` by default; opt-out via `CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`, or `options.disable_metrics` ([Crush metrics & privacy](https://mintlify.com/charmbracelet/crush/guides/metrics)).

**Why it happens:**
Teams treat "points at a local baseURL" as equivalent to "local-only." The model endpoint is only one of the agent's outbound surfaces — update checks, model registries, share/sync, analytics, and runtime package fetches are separate code paths that no provider config touches.

**How to avoid:**
1. The agent-selection research phase must produce a **complete outbound-surface inventory** per candidate (model endpoint, registry fetch, update check, share, analytics, plugin/npm fetch, auth) — not just "supports OpenAI-compatible API."
2. Ship a **frozen kill-switch config** the install renders (mirror of the OWUI `ENABLE_PERSISTENT_CONFIG=False` pattern): for OpenCode at minimum `autoupdate: false`, `share: "disabled"`, `OPENCODE_DISABLE_MODELS_FETCH=1` + a local `OPENCODE_MODELS_URL` mirror (or vendored models manifest), explicit provider with only the local endpoint, `disabled_providers` for everything else. Golden-freeze this config.
3. **Reuse the v1.3 `villa verify` runtime negative-control pattern**: an egress-open run must FAIL (gate proven real), then under a scoped nft egress block the agent must complete a real edit-loop task. Flag-trusting the agent's own config is exactly the vacuous-green the v1.3 decision log warns against — and OpenCode's air-gap issues prove the flags are *known incomplete* here.

**Warning signs:**
Agent startup latency that disappears when offline-blocked; `models.dev`, `data.charm.land`, npm registry, or GitHub-release hosts in nft/conntrack logs; agent failing to start with networking restricted (means it depends on a remote resource); a "share" URL appearing in agent output.

**Phase to address:**
Agent-selection/research phase defines the kill-set as a hard selection criterion; install-addon phase renders + freezes it; verification phase extends `villa verify` to the agent (negative-control-first).

---

### Pitfall 2: Cloud-provider fallback — the agent's *default* model is a hosted API **(NON-NEGOTIABLE THREAT — code exfiltration)**

**What goes wrong:**
The agent runs, but inference goes to a cloud API instead of `villa-llama`. OpenCode's out-of-box default model is **`gpt-5-nano` hosted by OpenCode Zen** (their cloud gateway, behind `opencode auth login`) ([OpenCode Zen docs](https://opencode.ai/docs/zen/), [models docs](https://opencode.ai/docs/models/)). If the local provider config is missing/typoed, or the user has ever run `opencode auth login` for another tool, the agent can silently resolve to a cloud provider — code and prompts leave the box while everything *appears* to work (better than the local model would, even, which masks the failure).

**Why it happens:**
Agents are built provider-first; local models are a configured exception. Provider resolution falls back to whatever credentials/registry entries exist, and "it answered" looks like success.

**How to avoid:**
- Pin exactly one provider (the local llama-server endpoint) and the exact model id in the rendered agent config; disable all other providers (`disabled_providers` / equivalent).
- Treat **presence of cloud credentials as a preflight WARN** for the addon (check the agent's auth store, e.g. `~/.local/share/opencode/auth.json`).
- The runtime egress-block verify (Pitfall 1) catches this class structurally: a cloud-resolved model cannot answer under the nft block.
- Surface the agent's *configured* provider+model in `villa status` (append-only) so drift is visible — mirroring how the active backend/image tag is surfaced today.

**Warning signs:**
Agent responses faster/smarter than the local model plausibly is; **agent works while `villa-llama` is down** (smoking gun — make this an explicit negative control: stop `villa-llama`, agent task MUST fail); `auth.json` exists with non-local entries.

**Phase to address:**
Install-addon phase (config rendering + preflight credential check); verification phase (llama-down negative control + egress proof).

---

### Pitfall 3: Tool-calling / chat-template breakage through llama-server's OpenAI endpoint

**What goes wrong:**
The chosen coding model "supports tool calling" on paper but is unusable agentically through `llama-server`'s `/v1` endpoint. Verified failure modes (mid-2026):

- llama-server needs **`--jinja`** to honor tool definitions at all; without it, agents sending `tools` get 500s — and *with* it, some shipped templates crash (e.g. "Value is not callable: null" on the `reject` filter; OpenCode hit exactly this against llama.cpp: [opencode#1890](https://github.com/anomalyco/opencode/issues/1890)).
- **Qwen3-Coder uses XML-style tool calls** (`<tool_call><function=...><parameter=...>`), which llama.cpp must parse back into OpenAI-shape `tool_calls`; specific builds crashed with 500 when a tool definition had no `properties` field, and template fixes shipped repeatedly through quant re-uploads ([Unsloth Qwen3-Coder template fixes](https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF/discussions/10)). The **GGUF's embedded chat template is part of the artifact** — two quants of "the same model" can differ in agentic usability.
- OpenAI-compat bugs in llama-server itself, e.g. `tool_calls.arguments` returned as a JSON object instead of a string ([llama.cpp#20198](https://github.com/ggml-org/llama.cpp/issues/20198)) — agents with strict OpenAI clients reject the response.
- Some agents/models effectively require provider-specific APIs or wrappers (Qwen docs recommend Qwen-Agent for tool-template handling; Anthropic-native agents need an Anthropic-shape endpoint) — "OpenAI-compatible" is a spectrum, and the integration contract here is strictly llama-server's `/v1`.

**Why it happens:**
Tool calling lives in the seam between the GGUF's chat template, llama.cpp's Jinja engine + format-specific parsers, and the agent's client expectations. All three move independently; benchmark pages test none of this.

**How to avoid:**
- Model selection must be **agent-in-the-loop**: the research spike runs the actual chosen agent against the actual quant on the actual pinned llama.cpp image through a real multi-step tool loop (read file → edit → run command). A model that can't survive that loop is disqualified regardless of coding-benchmark scores.
- Pin the **GGUF artifact** (specific repo + revision, prefer fixed-template quants for Qwen3-Coder-class models) the way images are digest-pinned; record template provenance in the catalog entry.
- The coder llama-server unit needs its own flag set (`--jinja`, possibly a `--chat-template-file` override) rendered behind the orchestrate seam — don't assume the chat model's unit flags transfer.
- Add a **tool-call smoke proof** to the addon's install/readiness gate (mirror of the v1.3 768-dim `/v1/embeddings` readiness proof): POST a canned `tools` request, assert a well-formed `tool_calls` response with string `arguments`. Health-200 is not tool-calling success — same honesty rule as everywhere else in this stack.

**Warning signs:**
Agent "narrates" tool calls as plain text instead of executing them; 500s only when `tools` is present in the request; agent loops re-asking for the same file; tool arguments arriving malformed; works in OWUI chat but not in the agent.

**Phase to address:**
Research/model-selection phase (agent-in-the-loop disqualification testing); install-addon phase (tool-call readiness proof as a gate).

---

### Pitfall 4: Memory-envelope OOM — agent-scale KV cache joins chat + embed on unified memory

**What goes wrong:**
The coder model is sized like a chat model and blows the GTT envelope. Agents are not chat: they routinely fill **64k–128k+ contexts** with file contents and tool transcripts. For a Qwen3-Coder-30B-A3B-class model (48 layers, GQA 4 KV heads, head_dim 128), KV cache is ~96 KiB/token at f16 → **~6 GiB at 64k, ~12 GiB at 128k** *on top of* ~18–20 GiB of Q4 weights (computed from the model card; MEDIUM confidence — verify per catalog entry). On the 62.5 GiB GTT envelope with the current chat model (qwen3.6-35b-a3b) + villa-embed already resident, a co-resident coder at agent context does not fit at naive sizing. Failure is at *allocation time under load*, possibly mid-task days after install — or worse, it "fits" only because something fell back to CPU (Pitfall 5). Also: default GTT is ~50% of system RAM, the usable envelope is `mem_info_gtt_total` not MemTotal (already handled by `detect`), and the ttm `pages_limit`/`page_pool_size` tuning question resurfaces if co-residency is attempted ([llama.cpp#18159](https://github.com/ggml-org/llama.cpp/issues/18159), [discussion #22372](https://github.com/ggml-org/llama.cpp/discussions/22372), [ROCm Strix Halo system optimization](https://rocm.docs.amd.com/en/latest/how-to/system-optimization/strixhalo.html)).

**Why it happens:**
Fit math anchored on chat-style contexts (8–16k) undercounts KV by 4–10×; and "the model loaded fine" at install says nothing about peak KV occupancy at the agent's configured `--ctx-size`.

**How to avoid:**
- Extend `recommend.Pick()` with the **same reservation discipline as v1.3 CTRL-01**: when the coding addon is enabled, the fit is computed at the *agent-profile context* (the ctx the agent config will actually request), reserving embed + chat (if co-resident) first. Typed-Unknown anything → conservative constant, never silent 0.
- The **co-resident vs swap-based decision must be an output of this fit math**, not a preference: on a 64 GiB-class envelope, swap-based coding mode (chat model down while coding) is almost certainly the honest recommendation; co-residency is a ≥96–128 GiB outcome. Encode the threshold; don't hand-wave it.
- KV-cache quantization (`-ctk q8_0 -ctv q8_0`, roughly halves KV) is a legitimate lever but must be a *catalog-declared, benched* choice — some models degrade agentically with quantized V-cache; never apply it silently to make the fit math pass.
- Preflight BLOCK (MEM-PRE pattern) when the configured agent ctx cannot fit the envelope in the chosen residency mode.
- **Render the agent's context setting and the server's `--ctx-size` from the same config value** — a mismatch silently truncates the agent's context (a separate honesty bug).

**Warning signs:**
`ggml_vulkan: Device memory allocation … failed` in the journal mid-task; agent tasks dying only on large repos / long sessions; fit math passing at install ctx while the agent config requests a larger context than the server flag.

**Phase to address:**
Control-plane fit phase (recommend/preflight extension) — must land **before** the install addon can gate on it, mirroring the v1.3 fit-before-surfacing ordering.

---

### Pitfall 5: Silent CPU fallback under multi-model load — residency proven only at idle **(NON-NEGOTIABLE THREAT — D-11 honesty)**

**What goes wrong:**
The coder model's residency is proven at install (idle), but under real concurrent load — chat generation + embed traffic + agent prompt-processing — GTT pressure pushes allocations to host memory, or the coder server boots with partial offload after a memory-tight start. llama.cpp does not fail loudly in every such case; the result is a 3–10× slowdown that reads as "the coding model is just slow on this hardware." On this stack, a silent/partial CPU fallback reported as healthy is the cardinal false-green.

**Why it happens:**
Residency is treated as a boot-time property. On unified memory with multiple resident models it is a *load-time* property: the marginal allocation (KV growth during a long agent session) is what spills.

**How to avoid:**
- The coder service gets its own **dual-assert residency proof** (log device_info + sysfs GTT delta) through the existing `Backend`/`ResidencyProof` seam — new marker literals stay behind `internal/inference` (`TestSeamGrepGate` will enforce this; extend the gate in the same commit that introduces the literals).
- Reuse the v1.3 **MEM-DOC under-load pattern**: doctor drives a real bounded workload (an actual tool-call generation on the coder endpoint) while sampling residency mid-flight, with chat+embed up if co-resident. Idle-green is not green.
- If swap-based mode wins (Pitfall 4), the coding-mode switch must be **transactional via the existing `backendswap`/`modelswap` discipline** (capture → prove residency → cutover → rollback), never a bare unit start/stop.

**Warning signs:**
Coder tok/s far below the benched figure for the same quant; GTT-used delta on model load smaller than model size; effective `--n-gpu-layers` < layer count in logs; system RAM ballooning while GTT stays flat.

**Phase to address:**
Same control-plane phase as Pitfall 4 (doctor residency-under-load check); coding-mode-switch phase if swap-based (transactional discipline).

---

### Pitfall 6: Qdrant-for-code ineffectiveness — text embeddings + naive chunking + stale index

**What goes wrong:**
The "default: reuse villa-qdrant + villa-embed with a code collection" plan ships, retrieval quality is poor, and the agent ignores or is misled by it. Three compounding causes, all verified:

1. **nomic-embed-text-v1.5 is a TEXT model.** Code retrieval is a distinct task; Nomic themselves ship separate code models because general text embedders "struggle with real-world challenges like finding bugs in GitHub repositories" ([Nomic Embed Code announcement](https://www.nomic.ai/news/introducing-state-of-the-art-nomic-embed-code)). The viable code-trained options are **CodeRankEmbed (137M, CoRNStack-trained — small enough for this envelope, but requires a specific query prefix)** and **nomic-embed-code (7B — a non-starter footprint here)** ([code embedding model comparison](https://modal.com/blog/6-best-code-embedding-models-compared)). Reusing villa-embed as-is means text-model embeddings over code — the single most likely route to the "RAG proves ineffective" outcome the milestone hedges against.
2. **Naive chunking destroys code structure.** Fixed-size/text chunking splits functions mid-body and severs signature from implementation; code RAG that works uses AST/tree-sitter-aligned chunks. OWUI's document chunker (the v1.3 path) is a *text* chunker.
3. **Stale index vs working tree.** The agent *edits the code it retrieves over*. A vector index is stale the moment the agent writes a file; retrieval then actively lies (returns the pre-edit version). This is the structural reason **Claude Code — and, per Augment's SWE-bench post-mortem, most top agents — dropped vector RAG for grep/agentic search**: freshness and precision beat semantic recall for code-edit workloads ([Claude Code no-indexing](https://vadim.blog/claude-code-no-indexing/), [why grep beat embeddings](https://jxnl.co/writing/2025/09/11/why-grep-beat-embeddings-in-our-swe-bench-agent-lessons-from-augment/)). Note OpenCode itself ships grep/glob/LSP tools and uses **no** vector index — bolting Qdrant retrieval onto it swims against its design.

**Why it happens:**
"We already have Qdrant + an embedder" makes reuse look free; the per-component reuse *is* free, but the task fit is what's wrong.

**How to avoid:**
- The milestone already plans research-validated effectiveness — make that validation **real and decision-grade**: a small fixed eval (15–20 retrieval questions against this very repo: "where is residency asserted", "what writes Quadlet units") scoring text-embed-RAG vs the agent's native grep/LSP loop vs (optionally) CodeRankEmbed-RAG. Define the fallback trigger numerically *before* running it.
- Plan for the likelihood that the honest default is the **fallback**: the agent's built-in grep/glob/LSP + a generated repo map (Graphmind-style code graph, per the milestone's own hedge) — zero new resident models, zero staleness, matches agent design. Qdrant code-RAG should have to *win* the eval to ship, not lose it to be removed.
- If code-RAG does ship: dedicated collection, code-trained embedder (CodeRankEmbed-class), AST-aware chunking, and **index-refresh-on-write** wired into the agent loop (or staleness surfaced with the typed-Unknown honesty used by `recall status`).
- **Embedding-skew interaction (this stack specifically):** a second embedding model/dimension for the code collection must not trip the v1.3 fail-closed `EmbeddingSkew` gate or the backup manifest-v2 dimension check, both designed around a single embedder. Skew logic must become collection-scoped *before* a second embedder exists, or `recall index` and restores will refuse incorrectly.

**Warning signs:**
Agent retrieves chunks then re-greps anyway (retrieval adds tokens, not signal); retrieved chunks are mid-function fragments; retrieval returns pre-edit code during a session; eval recall@5 below the grep baseline.

**Phase to address:**
The milestone's biggest *ordering* risk: the effectiveness eval belongs in the **first research phase**, before any code-memory integration is built — its outcome can delete an entire phase's worth of work from the plan.

---

### Pitfall 7: Agent workspace escape and arbitrary command execution — no sandbox by default

**What goes wrong:**
**OpenCode's default permission posture allows all operations without approval** — bash, file edits, anywhere the process can reach ([OpenCode config docs](https://opencode.ai/docs/config/): "By default, opencode allows all operations without requiring explicit approval"). A locally-run agent driven by a mid-size local model — which follows instructions less reliably than frontier models and is more susceptible to prompt injection from repo contents it reads — can write outside the workspace, touch `~/.config/villa/config.toml`, the Quadlet units, or the agent's own config (including re-enabling the outbound surfaces from Pitfall 1). The stack's own state files are inside the blast radius.

**Why it happens:**
Agent defaults are tuned for frontier cloud models and developer convenience; "local" gets conflated with "safe." The threat isn't a malicious agent — it's a weaker model misfiring plus untrusted text (README, code comments, fetched docs) steering tool calls.

**How to avoid:**
- Treat the **delivery-mode decision (host binary vs container) as a security decision**, made in research with STRIDE input: a rootless Podman container with only the workspace bind-mounted, joined to `villa.network` (or loopback-only), no access to `$XDG_CONFIG_HOME/villa` or the Quadlet dir, gives workspace confinement + the egress story (Pitfall 1) *by construction* — consistent with how every other service in this stack is contained. A host binary makes both properties config-trust-based.
- Render restrictive `permission` config (`bash: "ask"`, `edit: "ask"` outside the workspace, at minimum) in the frozen agent config regardless of delivery mode — defense in depth, not the primary control.
- villa-owned state (config.toml, units, auth) must be **unreachable**, not merely "not asked about."

**Warning signs:**
Agent config or villa config mtime changing during agent sessions; files appearing outside the project dir; the agent running network commands (curl/pip/npm) as part of "fixing" something.

**Phase to address:**
Research phase decides delivery mode with an explicit security rationale; install-addon phase implements confinement; that phase's STRIDE pass covers the prompt-injection→tool-call escalation path.

---

### Pitfall 8: Version churn in the agent project breaks the pinned install

**What goes wrong:**
OpenCode is the fastest-moving dependency this stack would ever take: in mid-2026 alone the repo **moved organizations (sst → anomalyco)** ([HN thread](https://news.ycombinator.com/item?id=46552218)), shipped **breaking SDK/data-model changes** (v1.4.0), and runs a near-daily release cadence ([releases](https://github.com/anomalyco/opencode/releases)); there's even a reported bug where a plugin cache silently serves stale versions despite `@latest` ([#25293](https://github.com/anomalyco/opencode/issues/25293)). Its default `autoupdate` self-replaces the binary at startup. An unpinned or curl-script-installed agent means the installed artifact drifts under the user, config schemas break, and `villa doctor`'s view of the addon goes stale — the same failure class as the v1.2 rolling-tag drift that digest-pinning already solved for images.

**Why it happens:**
Agent projects optimize for velocity; their install paths (curl | sh, npm latest, self-update) are anti-pin by design.

**How to avoid:**
- Apply the **existing digest-pin discipline**: install a specific released version by checksum (container image digest if containerized — another argument for container delivery; versioned release artifact + sha256 if host binary). Never the install script, never `latest`.
- `autoupdate: false` in the frozen config (also required by Pitfall 1); upgrades happen only through a deliberate `villa`-mediated path, like image updates today.
- `villa doctor` checks installed-agent-version vs config-expected (drift detection, same as unit drift today).
- Keep the integration surface thin: depend on the agent's *config file + OpenAI-compatible provider contract*, not its SDK/plugin API — the SDK is what broke at 1.4.0.

**Warning signs:**
`--version` differing from the pinned version; agent config keys producing deprecation warnings after an update; upstream repo URL changing (it already happened once).

**Phase to address:**
Install-addon phase (pinned acquisition + doctor drift check); research phase records the exact pinned version + acquisition channel as a decision.

---

### Pitfall 9: Contract-freeze violations when surfacing the addon

**What goes wrong:**
Surfacing the coding addon in `status`/dashboard/doctor breaks the byte-frozen contracts: `status.Report` fields reshaped instead of appended, goldens re-frozen multiple times across phases, doctor's separate schema accidentally entangled, or — the v1.3-audit-flagged variant — new service-name constants duplicated in `cmd/villa` without a drift test (repeating the advisory WARN recorded for the memory service names). Separately: the new agent/coder container image and any backend marker literals placed outside `internal/inference`/`internal/orchestrate` will fail `TestSeamGrepGate` — or worse, pass because they hide somewhere the gate doesn't walk, recreating the leak the gate exists to prevent.

**Why it happens:**
Each feature's surfacing feels small in isolation; the discipline (ONE schema bump, append-only, surfacing lands LAST, single golden re-freeze) only holds if the roadmap enforces it structurally — which is exactly why v1.2 and v1.3 each pushed their single contract evolution into the final phase.

**How to avoid:**
- Reuse the proven pattern verbatim: **all v1.4 surfacing lands in the milestone's final phase as the single `status.Report` 3→4 evolution**, append-only (a `coding` block: enabled, agent version, model, residency, mode), one golden re-freeze, with coding-on/off golden variants. Recommend/doctor schemas bump only if they actually gain fields, each at most once.
- Coding-addon-off renders must be **byte-identical** to the v1.3 unit goldens (the `memory_enabled` off-render precedent).
- New service names get accessor functions in orchestrate + the drift test v1.3 deferred — close that debt class rather than extend it.
- Coder-model/agent image literals go behind the existing seams from day one; extend `TestSeamGrepGate`'s marker list in the same commit that introduces the literals.

**Warning signs:**
More than one golden re-freeze appearing across the milestone's phases; a `testdata` diff that isn't a pure addition; a service-name string literal in `cmd/villa`; `schema_version` bumped without an appended field (or vice versa).

**Phase to address:**
Final surfacing phase (by construction); every earlier phase carries an explicit "no contract changes yet" constraint.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Reuse villa-embed (text model) for the code collection "to start" | Zero new model, instant integration | Poor retrieval blamed on "RAG doesn't work"; rework after users distrust the feature | Never as a shipped default — only inside the eval spike, as the baseline arm |
| Install agent via its curl/npm script | Fast bring-up | Unpinned, self-updating artifact; doctor can't reason about it; breaks on upstream churn | Never (violates the digest-pin discipline) |
| Trust agent config flags for offline posture | No firewall test harness needed | Vacuous green on the privacy claim; OpenCode's flags are documented-incomplete | Never — runtime negative-control proof is the established bar |
| Prove coder residency at idle only | Simpler doctor check | Silent CPU fallback under concurrent load ships as "slow hardware" | Only if doctor reports under-load residency as typed-Unknown, never as PASS |
| Co-residency "because it loaded once" | No mode-switch machinery | Mid-session OOM days later; worst possible failure UX | Never — residency mode must come out of fit math |
| KV-cache q8_0 by default to make fit pass | Bigger ctx fits | Unbenched agentic-quality regression, invisible | Only as a catalog-declared option validated in the model-selection spike |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| OpenCode ↔ llama-server | Point baseURL at `/v1`, assume done | Also: `--jinja` on the server, tool-call smoke proof, pinned GGUF with known-good template, agent context and server `--ctx-size` rendered from the same config value |
| OpenCode ↔ network | Believe `share: disabled` + provider config = offline | models.dev fetch, autoupdate, npm provider pulls are separate paths; need env kills + container egress restriction + runtime proof |
| Agent ↔ existing Qdrant | New code collection with a different embedder under single-embedder skew logic | Make `EmbeddingSkew` + backup manifest dimension checks collection-scoped BEFORE a second embedder exists |
| Agent ↔ `villa model swap` / `backend set` | Coding mode flips units ad hoc | Route through the transactional swap discipline; reflect-pinned Deps test that coding-mode ops can't touch memory/OWUI units (D-09 precedent) |
| Addon ↔ `villa install` TUI | New screen computing its own decisions | Pure-collector pattern: TUI only collects `coding_enabled` + consent; the pipeline computes; byte-identical to the flag path |
| Coder unit ↔ orchestrate | Render agent/coder units in the cmd tier "just this once" | All rendering stays in `internal/orchestrate` templates; cmd tier stays thin |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Agent ctx sized to model maximum (256k native) | OOM, or massive KV reservation crowding out chat | Fit-math-derived ctx cap written into both server flag and agent config | Immediately on 64 GiB-class envelopes |
| Prompt re-processing per agent turn (no cache reuse) | Each tool round re-pays full pp cost; agent feels 10× slower than chat | Keep llama-server prompt caching effective (single slot, stable prefix); bench an actual agent loop, not bare tok/s | Any multi-turn agent session |
| Embed + coder + chat contending for GTT bandwidth | tok/s collapse during concurrent RAG + generation | Bench the concurrent case (v1.3 D-05 measurement pattern); document expected degradation | Co-resident mode under real use |
| Vector-indexing the whole repo on every change | Index lag, constant embed load | If code-RAG ships: incremental clean-replace (recall pattern); else moot | Repos beyond a few hundred files |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Agent runs unconfined on host with default allow-all permissions | Prompt-injected tool calls write outside workspace, mutate villa config/units | Container confinement + workspace-only mount + restrictive `permission` config; STRIDE pass on the injection→tool-call path |
| Cloud credentials present in agent auth store | Silent cloud fallback exfiltrates code | Preflight WARN on auth-store contents; llama-down negative control |
| Agent can reach villa's XDG config/data dirs | Agent re-enables its own telemetry/update surfaces | Mount/permission boundary makes villa state unreachable, not just un-asked-about |
| Trusting agent-reported "local only" | Vacuous privacy claim | Negative-control-first runtime egress proof (`villa verify` extension) — including agent *startup*, where the registry/update fetches happen |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Coding mode silently swaps the chat model out | User's chat dies mid-conversation, unexplained | Explicit mode-switch verb with status surfacing; refuse-with-remediation if chat is busy |
| Agent slow due to unbenched pp-heavy workload | "This product is broken" perception | Set expectations from bench data for the *agent loop*, not bare tg tok/s; surface live tok/s as today |
| Addon install pulls GBs (agent + coder model) without a size gate | Disk surprise, half-installed addon | Reuse the MEM-PRE disk-gate pattern with coder-model size |
| Code-RAG shipped but quietly useless | User attributes agent failures to the product | Ship the eval winner only; if grep/LSP wins, say so in docs — honesty is the brand |

## "Looks Done But Isn't" Checklist

- [ ] **Agent answers prompts:** may be resolving a *cloud* model. Verify: stop `villa-llama`, agent task must FAIL; egress-blocked run must PASS.
- [ ] **Tool calling "works":** one toy call ≠ agentic; breaks on tool defs without `properties`, parallel calls, long sessions. Verify: scripted multi-step edit-loop proof.
- [ ] **Model fits:** fits at install-test ctx, not agent ctx. Verify: fit math evaluated at the rendered agent context; allocation proven at that ctx.
- [ ] **Residency PASS:** proven idle-only. Verify: doctor under-load residency sample (MEM-DOC pattern) with all resident services active.
- [ ] **Offline posture:** config-flag-trusted. Verify: negative-control-first nft proof covering agent *startup* (models.dev/update fetches fire at startup, not first prompt).
- [ ] **Pinned install:** agent self-updated since install. Verify: doctor version-drift check.
- [ ] **Code memory effective:** indexed but never beats the agent's own grep. Verify: the predefined eval, fallback trigger honored.
- [ ] **Contracts intact:** verify single golden re-freeze, addon-off renders byte-identical to v1.3 goldens, SeamGrepGate extended for new literals, service-name drift test present.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Telemetry/egress leak found post-ship | MEDIUM | Hotfix frozen config + verify extension; disclose in release notes (zero-telemetry is the stated core value — treat as a security fix) |
| Tool-calling broken for shipped model | MEDIUM | Catalog-side fix: re-pin to fixed-template GGUF revision; tool-call proof gate prevents recurrence |
| OOM in the field | LOW-MEDIUM | `recommend` re-run with corrected agent-ctx fit; swap-based mode as remediation; fit-gate fix |
| Qdrant code-RAG ineffective | HIGH if shipped, LOW if caught in eval | Pre-declared fallback (grep/LSP + repo map / code graph); this is why the eval must precede the build |
| Agent escaped workspace | HIGH (trust) | Container confinement retrofit; audit villa state for mutation; restore via v1.2 backup machinery |
| Upstream breaking release | LOW (if pinned) | Stay on pin; upgrade deliberately, re-running tool-call + egress proofs |

## Pitfall-to-Phase Mapping

Suggested phase roles for the v1.4 roadmap (continuing the proven v1.2/v1.3 shape: research/selection spike → control-plane gates → integration → surfacing-last):

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 6. Code-memory ineffectiveness | **Phase R (first): agent/model/memory selection spike** — eval with pre-declared fallback trigger | Eval scorecard vs grep baseline; go/no-go recorded as a decision |
| 1. Phone-home defaults | Phase R defines the kill-set as a selection criterion; install phase freezes it | Egress-blocked agent startup + task completes; egress-open control FAILs |
| 2. Cloud fallback | Install-addon phase (config + preflight credential WARN) | llama-down negative control |
| 3. Tool-call breakage | Phase R (agent-in-the-loop model disqualification); install phase readiness proof | Scripted multi-step tool-loop proof passes on pinned artifacts |
| 4. Memory-envelope OOM | **Control-plane phase (before the install addon):** recommend/preflight extension at agent ctx; residency-mode decision | Fit-math golden; on-hardware allocation at agent ctx |
| 5. Silent CPU fallback | Control-plane phase (doctor under-load residency); mode-switch phase if swap-based | Induced-pressure negative control → FAIL, not false-green |
| 7. Workspace escape | Phase R (delivery-mode decision) + install phase (confinement) + STRIDE | Write-outside-workspace attempt blocked; villa state unreachable |
| 8. Version churn | Install phase (pinned acquisition, autoupdate off, doctor drift check) | Doctor flags an injected version drift |
| 9. Contract violations | **Final surfacing phase only** — single `status.Report` 3→4 | One golden re-freeze in the milestone diff; addon-off renders byte-identical; SeamGrepGate green |

## Sources

**Agent telemetry / offline behavior (HIGH — official docs; MEDIUM — GitHub issues):**
- [OpenCode config docs](https://opencode.ai/docs/config/) — autoupdate, share, permission defaults
- [OpenCode Zen](https://opencode.ai/docs/zen/), [Models](https://opencode.ai/docs/models/), [Providers](https://opencode.ai/docs/providers/) — default gpt-5-nano via Zen, auth flows
- OpenCode air-gap issues: [#16117](https://github.com/anomalyco/opencode/issues/16117), [#18492](https://github.com/anomalyco/opencode/issues/18492), [#4959](https://github.com/anomalyco/opencode/issues/4959), [#10766](https://github.com/anomalyco/opencode/issues/10766), [#11385](https://github.com/anomalyco/opencode/issues/11385)
- [Aider analytics docs](https://aider.chat/docs/more/analytics.html) — random-subset opt-out PostHog
- [Crush metrics & privacy](https://mintlify.com/charmbracelet/crush/guides/metrics) — PostHog data.charm.land; CRUSH_DISABLE_METRICS / DO_NOT_TRACK; [Crush local-model config discussions](https://github.com/charmbracelet/crush/discussions/775)
- [llama.cpp discussion #19619 — privacy-friendly coding agents](https://github.com/ggml-org/llama.cpp/discussions/19619); [#14758 — offline agentic coding tutorial](https://github.com/ggml-org/llama.cpp/discussions/14758)

**Tool calling / templates (HIGH — upstream issues/docs):**
- [llama.cpp function-calling docs](https://github.com/ggml-org/llama.cpp/blob/master/docs/function-calling.md); [llama.cpp#20198 — arguments-as-object OpenAI-compat bug](https://github.com/ggml-org/llama.cpp/issues/20198)
- [opencode#1890 — jinja tool-template crash vs llama.cpp](https://github.com/anomalyco/opencode/issues/1890)
- [Unsloth Qwen3-Coder chat-template + tool-calling fixes](https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF/discussions/10)

**Memory envelope / Strix Halo (HIGH — upstream + AMD docs; KV math computed, MEDIUM):**
- [llama.cpp#18159 — UMA detection vs TTM on AMD APUs](https://github.com/ggml-org/llama.cpp/issues/18159); [discussion #22372 — Strix Halo OOM with free memory](https://github.com/ggml-org/llama.cpp/discussions/22372)
- [ROCm Strix Halo system optimization (GTT/TTM limits)](https://rocm.docs.amd.com/en/latest/how-to/system-optimization/strixhalo.html)
- [Qwen3-30B-A3B model card (arch params for KV math)](https://huggingface.co/Qwen/Qwen3-30B-A3B-Instruct-2507); [Unsloth Qwen3-Coder run-locally guide](https://unsloth.ai/docs/models/tutorials/qwen3-coder-how-to-run-locally)

**Code RAG effectiveness (MEDIUM — industry consensus across multiple independent sources):**
- [Why grep beat embeddings — Augment SWE-bench lessons](https://jxnl.co/writing/2025/09/11/why-grep-beat-embeddings-in-our-swe-bench-agent-lessons-from-augment/)
- [Claude Code no-indexing analysis](https://vadim.blog/claude-code-no-indexing/); [grep vs semantic search nuance](https://www.nuss-and-bolts.com/p/on-the-lost-nuance-of-grep-vs-semantic)
- [Nomic Embed Code announcement](https://www.nomic.ai/news/introducing-state-of-the-art-nomic-embed-code); [CodeRankEmbed](https://huggingface.co/nomic-ai/CodeRankEmbed); [code embedding model comparison](https://modal.com/blog/6-best-code-embedding-models-compared)

**Version churn (HIGH — releases/HN):**
- [anomalyco/opencode releases](https://github.com/anomalyco/opencode/releases) (v1.4.0 breaking SDK/data-model changes); [HN: sst→anomalyco org move](https://news.ycombinator.com/item?id=46552218); [#25293 — stale plugin pin cache](https://github.com/anomalyco/opencode/issues/25293)

**Stack-internal (HIGH — first-party):**
- `.planning/PROJECT.md` — v1.3 validated patterns (CTRL-01 reservation, MEM-DOC under-load proof, EmbeddingSkew, D-09 swap isolation, single-schema-bump discipline, `villa verify` negative-control), v1.3 audit advisory (service-name drift test)

---
*Pitfalls research for: VillaStraylight v1.4 Coding Agent milestone*
*Researched: 2026-06-12*
