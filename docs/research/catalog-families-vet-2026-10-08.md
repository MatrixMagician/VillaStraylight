# The non-Qwen catalog vet, 2026-10-08

The on-hardware vet #315 requires before a catalog entry from another model family
ships. Dev host: Ryzen AI Max+ 395, 128 GB, Fedora 44, kernel 7.2.8. Backend: the
default `rocm` (ROCm 10.0, `sha256:3893b3e5…fc79bb1`, llama.cpp build 11430).
Method: the one the ROCm 10.0 vet used (`docs/research/rocm-10-vet-2026-10-05.md`),
driven through villa's transactional verbs from a static build of this branch.

## Candidates

| entry | family | file | why |
|---|---|---|---|
| `gemma-4-31b` | Google Gemma 4 | unsloth/gemma-4-31B-it-GGUF@c1ac76e, UD-Q4_K_XL (18.8 GB) + mmproj-F16 | dense, first-party vision: the second vision model and the second dense general model |
| `muse-glimmer-30b` | Meta Muse Glimmer | meta-models/Muse-Glimmer-30B-GGUF@70bf1b6, KQuant-Dynamic-Q4_K_XL (19.7 GB) + mmproj Q4_K_M | dense agentic coder with vision; the first pick for the second coder (dropped below) |
| `devstral-small-2-24b` | Mistral Devstral 2 | unsloth/Devstral-Small-2-24B-Instruct-2512-GGUF@6e458b8, UD-Q4_K_XL (14.5 GB) + mmproj-F16 | dense coder, no reasoning channel; the second pick for the coder after Muse fell to the probe (dropped below) |
| `glm-4.7-flash` | Z.ai GLM 4.7 | unsloth/GLM-4.7-Flash-GGUF@0d32489, UD-Q4_K_XL (17.5 GB), text-only | 30B-A3B MoE coder whose template honours `enable_thinking`; the third pick for the coder |

Rejected: Devstral 2 24B (text-only in llama.cpp), GLM-4.6V-Flash (community
projector only), Mistral Small 4 (119B MoE, open-weight status disputed),
gpt-oss-20b (no first-party vision), Gemma 4 26B-A4B (MoE, behind 31B on every
published number), Muse's 17 GB Q4_K_M build (1.0% degradation against 0.2% for the
Dynamic build). Muse's DFlash drafter is not shipped: the draft allowlist names the
two spec types ADR-0009 measured, and DFlash is neither.

## The witness

Every dimension was read off the GGUF files (ADR-0007), not a model card. Both
architectures interleave sliding-window and global attention, which the witness did
not read; ADR-0029 records the rule that landed with this vet.

| entry | arch | blocks | pattern | n_layers | n_kv_heads | head_dim |
|---|---|---|---|---|---|---|
| gemma-4-31b | gemma4 | 60 | 5 sliding : 1 global | 10 | 4 (per-layer array, 16 on sliding blocks) | 512 (`key_length_swa` 256) |
| muse-glimmer-30b | muse-glimmer | 52 | 3 sliding : 1 global | 13 | 2 | 128 |

The SHA-256 of every download matched Hugging Face's LFS oid, and both shard URLs
pin a revision.

## Method

1. Lock, back up `config.toml`, `pin-state.json`, the unit dir and
   `eval-baselines.json`; stage the verified files under their catalog names.
2. `villa speculation set off`, `villa model swap <entry>` (ADR-0023 turns vision on
   for a projector entry), the residency proof (`villa status` offload), `villa
   doctor` CAT-01 on the branch binary, one hand-read completion.
3. `villa eval --record`: the baseline, speculation off, tools mode on (the
   operator's config), ctx 131072.
4. A generated 128x64 PNG (red left half, blue right half) as an `image_url`, and
   one tool-call request.
5. `villa speculation set ngram`, a cold and two warm greedy 256-token probes on the
   same prompt, `villa eval` against the recorded baseline, one hand-read
   completion, `villa speculation set off`, the no-speculation reference on the same
   prompt.
6. Restore from the backups, `villa restart villa-llama.service` with the main
   binary, `diff -r` on the unit dir, `diff` on the config, doctor, one completion.

## Results

### gemma-4-31b

| step | result |
|---|---|
| `villa model swap gemma-4-31b` | swapped; config `vision = true`, ctx 131072 kept; `load_tensors: offloaded 61/61 layers to GPU`; `clip_ctx: CLIP using ROCm0 backend`; `[mtmd] estimated worst-case memory usage of mmproj is 1300.79 MiB` |
| residency | `villa status` overall PASS, villa-llama offload PASS |
| CAT-01 (branch binary) | PASS: `catalog n_layers=10 n_kv_heads=4 head_dim=512; gemma-4-31B-it-UD-Q4_K_XL.gguf header kv_layers=10 head_count_kv=4 key_length=512`; `llama_kv_cache_iswa: creating non-SWA KV cache, size = 131072 cells` |
| `villa eval --record`, speculation off | **20/20** recorded (every Qwen entry scores at most 19/20) |
| vision | the 128x64 PNG read as "The left half is red and the right half is blue." |
| tool call | `get_weather({"city":"Lisbon"})`, empty content |
| hand-read, speculation off | three correct sentences on Rayleigh scattering after a 1206-char thinking block; `reasoning_content` separated by the server |
| `villa speculation set ngram` | cutover proven; cold 7.4 tok/s (256 tokens, all thinking); warm 145.8 and 148.7 tok/s with 248 of 248 drafts accepted (greedy, identical prompt); the sky prompt 10.2 tok/s with 6 of 108 drafts accepted |
| `villa eval` under ngram | PASS, 20/20 against the baseline |
| speculation off reference | 10.3 tok/s on the same list prompt |
| `villa doctor` under Gemma | **overall FAIL**: `agent-tool-call BLOCK FAIL crush run: signal: killed`; the Crush round-trip has a 90 s budget (`agentProofBudget`) and Gemma thinks by default at about 10 tok/s decode. Judged below with a control on qwen3.8-27b. |

The first hand-read completion measured 1.7 tok/s and the eval decode 4.4 tok/s
because the killed Crush request was still generating on another slot; the
uncontended figures are the 10.2 and 10.3 above, the same as qwen3.8-27b's 11.

**The doctor finding, judged.** The control: `villa model swap qwen3.8-27b` (the
existing dense entry) and `villa doctor` on the same build the same evening gave
`agent-tool-call BLOCK PASS the agent completed a real read→edit tool-call
round-trip`, doctor 211 s overall, the round trip's generation 512 tokens at 11.55
tok/s in 44 s with no reasoning block. Gemma decodes at 10.3 and thinks by default
(805 to 1206 characters before each answer here), so the same round trip cannot
finish inside `agentProofBudget`. Under Gemma with `agent_enabled = true`, doctor
therefore reports overall FAIL. The entry ships because every criterion the issue
names holds and `recommend` never picks it automatically (its 18.8 GB weight ranks
below qwen3.6-35b-a3b wherever both fit); the budget-versus-thinking gap is filed
as a doctor defect (#318) rather than hidden by dropping the entry.

A side server on port 8081 from the same image (never the stack) measured a
3027-token prompt: Gemma `-fa 1` 264.9 tok/s prefill and 8.7 decode, Gemma `-fa 0`
91.7 and 4.2, qwen3.8-27b `-fa 1` 303.5 and 11.3. The unit's `-fa 1` is the faster
rendering, and Gemma runs where a 31B dense model should on this hardware.

### devstral-small-2-24b

| step | result |
|---|---|
| `villa model swap devstral-small-2-24b` | swapped (the cutover probe passed: no reasoning channel); ctx 131072 kept; `load_tensors: offloaded 41/41 layers to GPU`; `model params = 23.57 B` |
| residency | `villa status` overall PASS, villa-llama offload PASS |
| CAT-01 (branch binary) | PASS: `catalog n_layers=40 n_kv_heads=8 head_dim=128; Devstral-Small-2-24B-Instruct-2512-UD-Q4_K_XL.gguf header kv_layers=40 head_count_kv=8 key_length=128` |
| `villa eval --record`, speculation off | **14/20** recorded: misses `format-reverse-order`, `code-python-loop`, `code-python-slice`, `code-go-filter`, `units-journey-minutes`, `units-weekday`; qwen3-coder-30b-a3b misses 4 of these 20 (three of them the same cases) |
| vision | the projector (`mmproj-F16`, worst-case 909.98 MiB, `CLIP using ROCm0 backend`) loads, but the red/blue PNG read as "Left half: black. Right half: red." Unsloth's guide says llama.cpp does not support Devstral 2's vision. **The entry ships text-only**, with no projector block. |
| tool call | `get_weather({"city": "Lisbon"})`, empty content |
| hand-read, speculation off | a correct Rayleigh-scattering answer, no reasoning block, 14.0 tok/s; the template injects Mistral's 550-token system prompt (`cached_tokens` 554 on every call) |
| `villa speculation set ngram` | cutover proven; cold 14.0 tok/s; warm 23.3 and 32.5 tok/s with 95 and 123 of 192 drafts accepted (the list prompt varies slightly between runs, so acceptance is partial rather than total) |
| `villa eval` under ngram | PASS, 14/20 against the baseline |
| speculation off reference | 13.7 tok/s on the same prompt |
| `villa doctor` under Devstral | **overall FAIL**: `agent-tool-call BLOCK FAIL crush run: exit status 1` (doctor 112 s, so not the budget). The server log has the cause: `Jinja Exception: After the optional system message, conversation roles must alternate user and assistant roles except for tool calls and results`, HTTP 500 on Crush's requests. Devstral's embedded template refuses Crush's message shape, so every `villa code` turn would fail the same way. **Dropped**: a coder that cannot complete the agent round trip is not a coder, and the agent check runs against whatever is served, so it cannot ship as a chat entry either. Its files were moved back out of the models volume and its baseline removed. |

### glm-4.7-flash

| step | result |
|---|---|
| `villa model swap glm-4.7-flash` | swapped (its template honours `enable_thinking`, so the probe saw "ok"); ctx 131072 kept; `vision` turned off for a text-only target (ADR-0023); `load_tensors: offloaded 48/48 layers to GPU`; `model params = 29.94 B` |
| residency | `villa status` overall PASS, villa-llama offload PASS |
| CAT-01 (branch binary) | PASS: `catalog n_layers=47 n_kv_heads=1 head_dim=576; GLM-4.7-Flash-UD-Q4_K_XL.gguf header kv_layers=47 head_count_kv=1 key_length=576` (MLA: one latent KV head of 576; the catalog's `2 x` term over-reserves against llama.cpp's MLA cache, a conservative error) |
| `villa doctor` under GLM | **overall WARN, no FAIL**: the agent tool-call round trip completed (the only WARNs are the pre-existing stale `verify search` and the crush.json drift every swap shows) |
| `villa eval --record`, speculation off | **16/20** recorded, the same score as qwen3-coder-30b-a3b; misses `format-reverse-order`, `code-python-slice`, `code-go-filter`, `units-weekday` (the last three are the Qwen coder's misses too) |
| vision | `image input is not supported` (HTTP 500) on the text-only unit, as expected; no projector shipped |
| tool call | `get_weather({"city":"Lisbon"})` with content "I'll get the current weather in Lisbon for you." |
| hand-read, speculation off | 56.5 tok/s decode, 215 tok/s prefill; a 400-token probe was still inside the model's thinking block (1620 chars), as the Qwen thinking entries are |
| `villa speculation set ngram` | cutover proven; cold 56.6 tok/s; warm 150.6 and 371.5 tok/s with 180 of 192 and 242 of 242 drafts accepted |
| `villa eval` under ngram | PASS, 16/20 against the baseline |
| speculation off reference | 56.5 tok/s on the same prompt |

### The sliding-window cache, measured (review round 1)

The first version of ADR-0029 called Gemma's sliding-window caches "under 1 GB" and
left them to the headroom. The journal of the Gemma unit at ctx 131072 (2026-10-08
22:11:47, `villa model swap gemma-4-31b` on the committed head) says otherwise:

```
srv  llama_server: n_parallel is set to auto, using n_parallel = 4 and kv_unified = true
llama_context: n_seq_max             = 4
llama_context: n_ubatch              = 512
llama_context: kv_unified            = true
llama_kv_cache_iswa: creating non-SWA KV cache, size = 131072 cells
llama_kv_cache:      ROCm0 KV buffer size = 10240.00 MiB
llama_kv_cache:      ROCm0 KV buffer size =  3600.00 MiB
```

The second buffer is the sliding-window cache: llama.cpp sizes it as
`GGML_PAD(min(n_ctx, n_swa x n_seq_max + n_ubatch), 256)` cells when the KV is
unified (`src/llama-kv-cache-iswa.cpp`), which with villa's rendering (no `-np`,
`-kvu`, `-ub` or `--swa-full`, so llama-server's `n_parallel = 4`, `n_ubatch = 512`)
is 1024 x 4 + 512 = 4608 cells, at 50 layers x 16 KV heads x 256 dims x K+V x 2 bytes
= 819,200 bytes per cell: 3,774,873,600 bytes, the 3600 MiB logged. The same 3600 MiB
appeared on the side server at `-c 32768`. The fit reserved nothing for it; on a
33 GB envelope at `--ctx 16384` the old fit admitted Gemma with about 0.29 GB of slack.
The fix: the catalog entry carries the sliding-layer block (`swa`: 50 / 16 / 256 /
window 1024), the header witnesses it (CAT-01 compares `SWALayers`, `SWAHeadCountKV`,
`SWAKeyLength`, `SWAWindow`), and `recommend` adds the bounded term with the two
llama-server defaults it depends on pinned by an inference test. Gemma's KV term at
131072 is now 10.7 + 3.77 GB.

**Context checkpoints.** The same journal shows llama-server creating SWA context
checkpoints per slot, `created context checkpoint N of 32 (... n_tokens = T, size = S)`,
with S growing at 0.78 MiB per token (6.25 MiB at 8 tokens, 157.0 MiB at 201 tokens:
the sliding-layer state for the tokens held), so a checkpoint tops out near 800 MiB
at the 1024-token window. `--ctx-checkpoints` defaults to 32 per slot and villa does
not render it. A checkpoint of an active slot lives in host memory and is not under
`--cache-ram`; only when an idle slot is saved to the prompt cache do its checkpoints
count against the 8192 MiB (`update: - prompt ...: 186 tokens, checkpoints: 3, 541.109 MiB`).
Measured today the server made 2 to 3 checkpoints per prompt. The worst case, 32 x
800 MiB x 4 slots, is not bounded by anything villa renders; that is filed as #323
rather than folded into this fit.

### GLM as the coder default on 46 to 54 GB envelopes (review round 1)

With `agent_ctx` 131072, GLM's coder total (17.5 GB weights + 14.2 GB KV at the
catalog's MLA term + 8 GiB prompt cache) outranks qwen3-coder-30b-a3b's under the
"largest coder total that fits" rule on post-reservation envelopes of about 45.8 to
54.65 GB, so hosts in that band change their default coder. That is kept, stated here
and in the PR, and pinned by `TestSeedPicksAt50GB` on the embedded seed. The
alternative, an `agent_ctx` of 32768 chosen so GLM ranks below the Qwen coder, was
rejected as sizing to lose a ranking.

Because coding mode renders `--cache-reuse 256` only for an entry with
`cache_reuse_safe`, GLM was given the probe the Qwen coders had (commit e18ce7b: a
two-turn conversation under `--cache-reuse 256`, turn 2 must report `cache_n > 0`
and the log must carry no degrade warning). Side server from the same image, GLM at
`-c 65536 -fa 1 --cache-reuse 256 --cache-ram 8192 --jinja`, thinking off, 2026-10-08
22:51: turn 1, a 13,230-token prompt, answered "10" (correct) with `cache_n 0` at
308 tok/s prefill; turn 2, the same conversation plus one question, `prompt_n 16,
cache_n 13231`, answered "fox" (correct); the log's only reuse lines were `graphs
reused = 1`, no warning. `cache_reuse_safe: true` is therefore set. As with the Qwen
hybrids in e18ce7b, what the probe observes is prefix reuse; `--cache-reuse 256`
itself is harmless on this build, and the catalog claim is the probe's literal verdict.

### The committed head, confirmed

With the static build of the PR's head (`villa version v1.17.1-14-g6b934b3`), each
shipped entry was swapped in once more: `gemma-4-31b` swapped, status PASS, CAT-01
PASS, answered "Earth" to the one-word planet question at 10.2 tok/s (thinking off
for the probe); `glm-4.7-flash` swapped, status PASS, CAT-01 PASS, "Earth" at 53.3
tok/s, doctor WARN with no FAIL. Doctor under Gemma reported the #318 FAIL as
before. The restore left the operator's stack as it was found: no unit or config
diff, websafe on the main binary, status PASS, doctor's one finding the stale
`verify search` that predates this vet.

### muse-glimmer-30b: dropped

`villa model swap muse-glimmer-30b` loaded the model and its projector
(`load_hparams: projector: muse-glimmer`, `loaded multimodal model`) and then
**failed at "prove" and rolled back**: `generation probe failed: chat completion
returned no tokens (server responded but produced nothing)`. The cutover probe
(`inference.Client.GenerationProbe`) disables thinking through the chat template
kwarg `enable_thinking: false` and expects "ok" within 32 content tokens. Muse's
embedded ATEM template has no off state: `reasoning_strength` defaults to `high`
and every reply opens a `to=self` reasoning channel, so all 32 tokens are reasoning
and `content` stays empty. The rollback restored qwen3.6-35b-a3b cleanly.

Before the kill, the branch binary's doctor had witnessed the file: `catalog
n_layers=13 n_kv_heads=2 head_dim=128; Muse-Glimmer-30B-KQuant-Dynamic-Q4_K_XL.gguf
header kv_layers=13 head_count_kv=2 key_length=128`, a second architecture for the
ADR-0029 rule. The files were moved back out of the models volume. Shipping Muse
needs a probe decision (pass `reasoning_strength`, or count reasoning tokens), which
is not this issue's.
