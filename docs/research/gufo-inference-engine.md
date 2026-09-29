# Is gufo worth adopting in VillaStraylight?

Findings on [gufo-org/gufo](https://github.com/gufo-org/gufo) at commit `8eedee6f`, release `v0.2.0`, read on 2026-09-29 from the gfx1151 dev box. Every claim below cites the file it came from in the cloned tree or a live command run here. Anything I did not run myself is labelled.

## Verdict

**Adopt it as a fourth backend, on an opt-in config value, gated behind a catalog qualification. Do not make it the default and do not remove the llama.cpp path.**

Gufo is a same-hardware, same-posture project that is measurably faster than what villa runs today on prompt processing and cold load. It is also a closed model set with a placeholder `/slots`, no `/v1/embeddings`, and one month of history. Those two facts together point at exactly one shape: villa's `Backend` seam already exists for this, and a fourth `BackendFor` case costs far less than any rewrite.

## What gufo is

A from-scratch C++20 inference engine built only for Strix Halo `gfx1151`, MIT licensed (`LICENSE`), serving an OpenAI-compatible HTTP API. It is not a llama.cpp fork. Model graphs, tokenizers and HIP kernels are compiled into one `gufo` binary (`docs/SERVER.md:41`), and the CMake build refuses any HIP architecture other than `gfx1151` (`CMakeLists.txt:35`).

Project age and staffing, from the GitHub API on 2026-09-29: created 2026-08-11, 382 stars, 35 forks, 41 open issues, three releases (`v0.1.0` on 2026-09-28 through `v0.2.0` on 2026-09-29). Two contributors account for 427 of ~440 commits.

## Why it is interesting for this project

The posture matches villa's almost line for line, which is unusual.

- **Same host, same constraints.** `gfx1151`, 128 GiB unified memory, rootless Podman with `--device /dev/kfd`, `--group-add keep-groups` and `crun` (`README.md:57-79`). That is villa's `backendROCm.ContainerArgs` argument set, independently arrived at.
- **Same SELinux trap.** gufo's README documents `setsebool -P container_use_devices true` against a misleading ROCr "Memory critical" error. Villa already gates on this as PRE-05 (`internal/preflight/checks_selinux.go:24`), with the identical remediation string.
- **Loopback by default, auth opt-in.** Binds `127.0.0.1` unless told otherwise, and `--api-key` covers every route including `/health` and `/metrics` (`docs/SERVER.md:728-734`). Villa's `InferenceSecret` wiring (ADR-0011) transfers without change.
- **llama.cpp-compatible metrics.** `/metrics` emits `llamacpp:prompt_tokens_total`, `llamacpp:tokens_predicted_total`, `llamacpp:prompt_tokens_seconds` and `llamacpp:predicted_tokens_seconds` under those exact names (`src/cli/serve/http_server.cpp:1070-1090`). Those are four of the six literals `internal/metrics/llamacpp.go` already parses.
- **Honest measurement culture.** Each model carries a `QUALITY.md` that separates execution consistency from conversion parity and states what is unqualified. The Qwen27B report is explicit that its BF16 comparison "is not an independent upstream oracle" (`docs/models/qwen3.8-27b/QUALITY.md:26`). That is the same standard villa's `ngram_provenance` strings hold themselves to.

## The performance case

gufo publishes head-to-head numbers against llama.cpp `b11069` on the same GGUFs, measured 2026-09-23 (`docs/models/qwen3.8-27b/BENCHMARKS.md`). These are **gufo's measurements, not reproduced here**.

| Workload, Qwen3.8-27B Q4 | gufo | llama.cpp | Gain |
|---|---:|---:|---:|
| pp, depth 0 | 656.33 tok/s | 357.54 tok/s | +83.6% |
| pp, depth 131,072 | 287.12 tok/s | 135.95 tok/s | +111.2% |
| tg single user, AR | 12.37 tok/s | 12.06 tok/s | +2.6% |
| tg 8 concurrent, AR | 74.46 tok/s | 50.14 tok/s | +48.5% |
| Cold load to HTTP ready | 5.87 s | 23.33 s | +297% |
| Peak memory, pp2048 + tg128 | 37.63 GiB | 36.19 GiB | -3.8% |

The shape is consistent and it maps onto villa's actual bottlenecks.

**Prompt processing is where it wins, and that is villa's agent path.** Single-user token generation is a wash, roughly +2.6% on AR. Prompt processing is nearly double at every depth. `villa work` re-sends a large tool-and-instruction prefix on every turn, and coding-mode context is 16,384 on the 35B entry, so prefill is the dominant cost there, not decode.

**Cold load at 4x matters for `model swap` and `resident`.** Villa built the resident set specifically because a cold load is expensive (v1.7). A 5.87 s load instead of 23.33 s changes the cost basis of that whole feature.

**Concurrency scales where llama.cpp flattens.** At 8 users llama.cpp reaches 50.14 tok/s against gufo's 74.46. Villa's rendered units do not currently pass `-np`, so the served unit is effectively single-slot today. If multi-user chat ever matters, this is the gap.

The tradeoff is honest and stated: gufo uses 3.8% to 8.2% more peak memory, and a few DFlash2 long-depth cells regress (Q8 mixed at 131K is -23.7%).

## What blocks it from being the default

Four things, each verified in the tree.

**1. The model set is closed, and villa's catalog barely intersects it.** Loading dispatches on `general.architecture`: `deepseek4`, `qwen4exp`, then a Qwen path gated on `qwen35.embedding_length == 5120` (`src/cli/serve/inference_backend.cpp:2965-3092`). The Qwen runtime is documented as "the dense Qwen3.8 27B runtime" (`src/models/qwen/README.md:3`).

I read the architecture string out of every GGUF on this box with `strings` on the header:

| Villa catalog entry | `general.architecture` | gufo support |
|---|---|---|
| `qwen3.8-27b` | `qwen35` | yes, the dense 27B path |
| `qwen3.5-0.8b` | `qwen35` | unverified, same arch family but a different geometry |
| `qwen3.5-2b` | not on disk here | unverified |
| `qwen3.6-35b-a3b` | `qwen35moe` | no |
| `qwen3-coder-30b-a3b` | `qwen3moe` | no |
| `qwen3-coder-next-q4` / `-q3` | `qwen3next` | no |

Plus the embedder, `nomic-embed-text-v1.5.Q8_0` at `nomic-bert`, which is moot because `/v1/embeddings` is 501.

One catalog entry is confidently servable and it is the one this host already has on disk, which makes the experiment cheap. The two small Qwen3.5 entries share the `qwen35` architecture string but not the 27B's geometry, and gufo's Qwen tree is documented as the 27B runtime, so whether they load is a question for the probe rather than the source. The four MoE and `qwen3next` entries, which include both coding models and the current default, are out.

**2. `/v1/embeddings` returns 501** (`docs/SERVER.md:353`). The memory stack's `RAG_OPENAI_API_BASE_URL` points at an embeddings `llama-server` with `--pooling mean` (`internal/orchestrate/memory.go:194-206`). That unit cannot move, whatever happens to the chat unit.

**3. `/slots` and `/props` are static placeholders.** `/slots` returns one hardcoded object with `state: 0` and `/props` returns an empty template (`src/cli/serve/http_server.cpp:1041-1065`). `docs/SERVER.md:785` says so and adds "do not use them for capacity or admission decisions". `/metrics` also hardcodes `kv_cache_usage_ratio 0.0` and emits neither `requests_processing` nor `requests_deferred`, which is what `metrics.IsGenerating` folds. On gufo the dashboard would read a permanently-idle stack and present stale gauges as a live rate, which is exactly Pitfall 3.

**4. The residency proof does not transfer.** `ResidencyMarkers` keys on `load_tensors:`, `ROCm0` and `offloaded N/N`. gufo emits none of them. Its load line is `event=load_completed kind=... gpu_device_used_mib=N gpu_device_total_mib=N` (`src/cli/serve/serve.cpp:66-83`), and there is no `load_tensors` string anywhere in `src/`. A gufo backend needs its own `ResidencyMarkers`, and ADR-0001 means that is a new proof, not a renamed literal. In gufo's favour: it cannot silently fall back to CPU the way the proof exists to catch, because `ENGINE_ENABLE_HIP` is a compile-time gate and `diagnose` refuses anything that is not `gfx1151` (`src/core/diagnostics/compatibility.cpp:73-78`).

## Two smaller things worth knowing

**The runtime image has version tags.** `skopeo list-tags ghcr.io/gufo-org/toolboxes/gufo-runtime` returned `0.2.0`, `0.2`, `0.1.1`, plus dated and `sha-` tags. Pinning `0.2.0@sha256:...` fits `internal/pins` as-is, unlike the rolling `:main` and `:latest` refs villa already carries.

**There is one new outbound surface, and it is guarded.** `image_url` accepts an HTTPS URL, and `src/core/image.cpp:455-508` fetches it: HTTPS only, proxy disabled, 3 redirects, size and time budgets, and an `OPENSOCKETFUNCTION` that rejects any non-public IP after resolution. That is a real SSRF guard, similar in intent to `villa-websafe`. It also cannot be disabled by a flag I could find, so under villa's zero-outbound-by-default posture it needs either a network-namespace bound or an explicit statement in `villa verify`.

## The cheapest experiment

Everything needed is on this box. `Qwen3.8-27B-UD-Q4_K_XL.gguf` is already in `~/.local/share/villa/models/` (17.5 GB, downloaded 2026-09-07) and `/home` has 440 G free. The one missing file is the DFlash2 draft, 1,143,006,816 bytes per a live `curl -I` against the pinned Hugging Face revision.

So the measurement is: pull the pinned `gufo-runtime:0.2.0`, download the 1.1 GB draft, serve the 27B GGUF villa already has, and run villa's own `bench` methodology against both engines on the same file. That reproduces gufo's claim on this host, at a cost of one image pull and 1.1 GB, without touching the tree.

Worth measuring alongside it, because gufo's tables do not cover them and they decide whether the win is real for villa's workloads:

- pp at the 16,384 context coding mode actually uses, not just 4,096 and 131,072.
- Whether `--sessions N` changes the residency picture, since villa's units do not pass `-np` today.
- Time to first token on a cold `villa work` turn end to end, which is where the 4x load win and the 2x prefill win compound.

## If it measures out

The integration is a fourth `BackendFor` case and a catalog flag, not a migration.

- `backend = "gufo"` returns a `backendGufo` with its own image, its own `ContainerArgs` (`gufo serve --host 0.0.0.0 --port 8080 llm --model ...`) and its own `ResidencyMarkers` keyed on `event=load_completed` plus `gpu_device_used_mib`. The seam's single polymorphism point is already the right shape for this.
- A catalog entry declares `gufo_safe` the same way it declares `ngram_safe` today, so the six entries gufo cannot load are refused at the boundary rather than failing at load. The existing fail-closed `BackendFor` default already refuses a typo'd value.
- The embedder, Open WebUI, Qdrant and SearXNG units do not move. Only the chat unit's backend changes, and `backend set` is already the transactional capture-prove-rollback verb for exactly that.
- The dashboard's performance panel needs a typed-Unknown path for the gauges gufo does not emit, which is the degradation `PerfSnapshot` was built for. Presenting a fabricated 0 would be the bug.

## What I would not do

Do not replace llama.cpp. Six of the seven catalog entries need it, the embedder needs it, and the dashboard's live signals need it. A project one month old with two contributors and three releases in three days is not a foundation to move a proven stack onto, however good its numbers are.

Do not adopt on the published numbers alone. They are gufo's, measured on gufo's host, against a llama.cpp build that is not one of villa's four vetted images. Villa's own standard is an on-hardware probe recorded as provenance, and that standard should apply here too.
