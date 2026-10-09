---
status: accepted
---

# Image generation is an eager reservation served from the image table

Issue #312 adds local image generation: an opt-in `villa-image` unit running
stable-diffusion.cpp's `sd-server` on Vulkan RADV, wired into Open WebUI, done when
Open WebUI generates an image from a prompt, an offload proof of ADR-0001's shape
passes, and the chat model still fits and serves while the image unit is loaded.
Six calls were open, and the measurement decided most of them.

The measurement (2026-10-08, gfx1151, Radeon 8060S, 128 GiB unified, the live
stack's qwen3.6-35b-a3b resident at ctx 131072): sd-server at
`ghcr.io/leejet/stable-diffusion.cpp:master-vulkan@sha256:367ccc4e…` (commit
e16d26a) with Z-Image-Turbo Q8_0 held 8808.62 MiB of params on the device
(`total params memory size = 8808.62MB (VRAM 8808.62MB, RAM 0.00MB)`), 9.19 GB of
GTT at ready and 9.36 GB after a 1024x1024 8-step generation, and the GPU read
100% busy through every drive (67 s at 1024x1024, 19 s at 512x512). Q4_K held
6221.12 MiB for a 5% slower, marginally less clean image. The chat model kept
answering beside it. Three facts shaped the design: sd-server's default auto-fit
logged `auto-fit: no GPU devices; using the default backend` before placing on
Vulkan anyway; its default lazy load answers `/sdapi/v1/options` 200 with 8 KiB
resident, printing the placement line but holding nothing until the first request;
and without `--vae-tiling` the 1024x1024 VAE decode asks for a 5.8 GB buffer the
device refuses.

The reservation is sized from the peak during a generation, not from the ready
state. `mem_info_gtt_used` sampled through two 1024x1024 8-step generations on a
quiet GPU, with the rendered flags, peaked 10,129,330,176 B above the pre-start
baseline for Q8_0 (every 0.25 s) and 7,506,677,760 B for Q4_K; the PR review's own
sampling of a Q8_0 generation (every 0.5 s) peaked at 10,457,440,256 B, and the
larger figure is used. The row is the peak plus a 10% margin, so
`compute_bytes = ceil(1.10 × peak) − weight_bytes`, rounded up to a MiB:
2,267,021,312 B for Q8_0 (a row of 11,503,612,928 B) and 1,734,344,704 B for Q4_K
(8,257,659,904 B). The first rows had been sized from the compute buffers sd-server
logs (1200 MiB), which left a 37 MB margin under the reviewed peak: noise, not a
reservation.

## Decisions

| call | options | verdict |
|------|---------|---------|
| 1. Footprint shape | a resident-set member; an always-loaded reservation row | **reservation row, eager-loaded** |
| 2. Footprint source | a pinned constant in its own package (the embedder's shape, ADR-0028); a catalog entry with a witness (ADR-0007) | **a compiled-in image table with provenance, witnessed at every proof** |
| 3. Open WebUI engine | `openai`; `automatic1111` | **`automatic1111`, preset in the entry** |
| 4. The proof | widen `ResidencyMarkers` with a line grammar; a second protocol package; a second `Fold` over the unchanged `residency.Prove` | **a second `Fold`, `inference.ImageOffloadVerdict`** |
| 5. Catalog shape | `role: "image"` rows in `seed.json`; a separate typed table | **a separate table, `internal/catalog/images.json`** |
| 6. Surfaces | a status JSON section and schema bump; a `villa verify image` verb; the memory subsystem's set | **the memory subsystem's set: `install --image`, a status row, a doctor finding, `update image`** |

**1. An eager reservation row, not a resident-set member.** The resident set admits
llama-server slots: a model, a quant, a ctx, a loopback port, an Open WebUI
endpoint, LRU eviction. None of that describes sd-server, nothing in the tree can
unload and reload a unit on demand, and sd-server has no idle unload. Lazy loading
would only move the same footprint to the first request, which makes the
reservation true at some moments and false at others; worse, the measured lazy
journal prints the placement line and answers ready while holding nothing, so a
readiness 200 would prove nothing. `--eager-load` makes the row true from the
moment the unit starts and puts the placement line in the invocation journal
before the proof's first request. `ReservationsFor` gains one row, `image`, last,
after the voice rows (ADR-0027, ADR-0030), sized `weight_bytes + compute_bytes`, with the memory row's
miss semantics (a conservative 24 GiB and a note naming the id).

**2. The footprint comes from a compiled-in image table, not a constant.** ADR-0027
left open where a new service's footprint comes from, and ADR-0028 (the reranker)
answered it for a service villa pins: a measured constant in its own package, with
"a catalog entry with a GGUF witness" reserved for a model the operator picks. The
image model is operator-pickable (`image_model` names `z-image-turbo` or
`z-image-turbo-q4`), so its measured footprint lives in a table in
`internal/catalog` with provenance per row, read through `catalog.Image(id)`, and
the `catalog_path` override never replaces it. That split, a pinned constant for a
service the operator cannot choose and a provenanced table row for one they can,
is the rule the two ADRs state together; the next service picks by which side it
falls on. The table carries no GGUF witness (the diffusion GGUF has no KV geometry
keys and the VAE is safetensors); its witness is the running server's placement
figure, compared against `weight_bytes` at every proof with a 5% tolerance, which
downgrades a PASS to a WARN naming both numbers and never auto-corrects (ADR-0007).

**3. `automatic1111`, with the preset in the entry.** Its verify is a real
`GET /sdapi/v1/options`, its model list names the real file stem, and it generates
through `POST /sdapi/v1/txt2img`; the `openai` engine's verify is a no-op and its
model list is a static DALL-E list. Steps, cfg scale and size are model facts
(Z-Image-Turbo is distilled to cfg 1.0 and about 8 steps; another value is a
different, unmeasured footprint), so they are rows in the entry, rendered twice
from one `ImageServe`: as sd-server's argv defaults and as `IMAGE_SIZE` and
`IMAGE_STEPS`. Open WebUI's request body carries no `cfg_scale`, so argv is the
value used; `AUTOMATIC1111_PARAMS` (a JSON object inside an `Environment=` line)
and `IMAGE_GENERATION_MODEL` (a checkpoint switch POSTed at a single-model
server) stay unset.

**4. A second `Fold`, `residency.Prove` unchanged.** sd-server prints no
`load_tensors` line; its placement line always contains both `VRAM` and `RAM`, so a
substring match on the device token would read a fully RAM-placed load as a device
buffer. `ImageOffloadVerdict` parses the two figures separately: VRAM > 0 and
RAM == 0 is a PASS, RAM > 0 is a FAIL (a partial CPU fallback is a FAIL in this
repo, with no WARN band), VRAM == 0 is a FAIL, no line or no journal is a WARN, and
a software renderer on the `ggml_vulkan:` device line is a FAIL through the Vulkan
markers it already shares. The GTT floor and the busy fold are the chat verdict's
own, extracted into `foldFloors` so there is one copy of the busy branch. The
floor is corroborative for this unit: `mem_info_gtt_used` is host-wide and the chat
model's 20 GB sits beside the image's 9 GB, so the floor against the image bytes
alone cannot fail while the chat model is resident. The two load-bearing signals
are the placement line (RAM must be 0) and `gpu_busy_percent` sampled during the
drive (100% in every measured generation). Summing the chat weight into the
reference was rejected: it reads FAIL for a healthy image unit whenever
villa-llama is down. The drive is one 512x512 generation at the entry's steps and
cfg, fixed prompt and seed, so install, doctor and update each pay about 20 s
rather than 65 s; the reservation stays sized at the 1024x1024 preset.

**5. A separate typed table.** `role: "image"` rows in `seed.json` would need a
filter in every walker of `Catalog.Models` (`autoEligible`, `pickOverride`,
`pickCoder`, model swap, resident add, model list, the dashboard picker); one
unfiltered walker serves a diffusion GGUF through llama-server. `Model` also has no
slot for three named files, and a positional `Shards` convention is one reorder
away from serving the VAE as the diffusion model. `ImageModel` names `Diffusion`,
`TextEncoder` and `VAE` as fields, and the role is the type. The VAE is renamed on
disk (`z-image-turbo-ae.safetensors`) because every FLUX-family VAE upstream is
called `ae.safetensors`, and the models dir is shared; a build-time test asserts
every image filename is disjoint from the chat catalog's.

**6. The memory subsystem's surfaces.** `villa install --image` is
persist-and-inherit like `--web-search`; the render refuses an on-gate with no
resolved model; `villa status` carries a `villa-image.service` row whose health is
the readiness GET; `villa doctor` adds `IMG-DOC-residency` from the same proof,
unevaluable when the unit is not active (doctor never starts a service);
`villa update image` maps PASS, WARN and FAIL onto Pass, Reject and Fail. No status
JSON section and no schema bump: a section earns a key for state the row cannot
carry, and whether the params sit on the device needs a generation a status run
must not drive. No `villa verify image`: the residency proof already is the image
proof, and the verify family proves with a negative control.

## The Exec, and what the seam owns

The rendered Exec is the measured set, with the device token read through the
seam the way `appendSpeculationArgs` reads it, never a literal in orchestrate:

```
--listen-ip 0.0.0.0 --listen-port 1234 --diffusion-model /models/<d> --llm /models/<te> --vae /models/<vae>
--backend Vulkan0 --params-backend Vulkan0 --eager-load --vae-tiling --diffusion-fa --cfg-scale <cfg> --steps <steps> -W <w> -H <h>
```

The explicit placement pair is load-bearing: sd-server's help says auto-fit is
"disabled by explicit --params-backend", and auto-fit is exactly the silent CPU
placement ADR-0001 forbids. `--eager-load` is load-bearing for the reason in
decision 1. `--vae-tiling` is load-bearing for the measured 5.8 GB buffer.
`--auto-fit off` is not rendered: the pair disables it, and a flag that restates
what another flag already decides is a second place to get it wrong.
`--offload-to-cpu` is never rendered, and a test asserts both.

The flag vocabulary lives in `internal/inference/image_server.go`
(`ImageServerArgs`), not in orchestrate: the seam gate forbids `--params-backend`,
`--diffusion-model` and `--eager-load` outside the seam, and allowlisting
`orchestrate/image.go` for every pattern would have exempted it from the device-args
check too. The device access is one value, `inference.VulkanGPUAccess()`
(`/dev/dri`, `keep-groups`, `seccomp=unconfined`), that the Vulkan chat unit's
`ContainerArgs` and the image unit both render, so the two GPU consumers cannot
drift apart on the rootless device contract. The image literal is the one thing
`orchestrate/image.go` holds, allowlisted like `searxng.go`.

The chat model's "still fits" half is one more proof, not the readiness poll.
`PollReady` answers 200 before the image unit's 9 GB allocation finishes, so on an
existing stack a passing poll says nothing about the chat model beside a loaded
image unit. `install --image` therefore runs the chat model's cutover gate
(`Deps.ProveChat`, wired to `liveProve`: the residency fold plus one generation)
right after the image proof passes, and a FAIL refuses and rolls back like any
other proof; doctor's existing offload finding runs with the image unit loaded,
which is the same check at a later time. The image gate becomes a config field through `Gates.Persist`, the one
writer of gates onto a config (#317), and `install.PlannedReservations(cfg, opts)`
sizes the pick against the config the run will persist, so a first `--image`
install fits the chat model against the image row it is about to add rather than
against a config that does not yet carry the opt-in; the image row rides that
existing path with no change to `Deps.Pick`.

## Rejected

- **`ReservationsFor(cfg, cat)`.** Ten call sites; `cmd/villa/inference.go` passes
  a one-model catalog beside it; `recommendConfigInputs` builds reservations before
  it loads a catalog; a `catalog_path` override replaces the seed wholesale.
- **`internal/imagegen` with a footprint constant.** Two homes for one measured
  number, bound only by a drift test, and the issue asked for catalog content.
- **A host-published port and a host-side image client.** Every managed service is
  probed in-network through the curl helper; a second probe doctrine and a port no
  consumer needs.
- **A 10 to 90% RAM band read as WARN.** A 50% RAM placement is a confidently known
  partial CPU fallback.
- **`StopTimeout=` on the unit.** sd-server ignores SIGTERM and podman SIGKILLs
  after 10 s; accepted as is and noted in `docs/CONFIGURATION.md`.
- **`ENABLE_IMAGE_PROMPT_GENERATION=False` as a default.** A product choice the
  measurement does not license; Open WebUI's default stands.

## Consequences

- `subsystem.Image` is appended at iota 8, after `Voice`, in `All` and `Every`,
  with inference's update budget (an eager load plus a generation, before and
  after the mutation). `config.ImageEnabled` and `ImageModel` are tail-appended
  and omitted on disk while off; `ImageAddr`/`ImagePort` are constants.
  `recommend.golden.json` and every existing rendered-unit and cmd golden are unchanged; two goldens are added
  (`villa-image.container`, `villa-openwebui.container.image`).
- The image server's pin, `image-server`, is a `RollingDigest` on `ghcr.io`
  (master-vulkan is rebuilt on every upstream push). It is not yet named in the
  signed manifest (serial 2); as with `backend-rocm-10.0` (ADR-0022), a component
  the manifest does not offer reports as current against its compiled-in pin, and
  a later manifest, signed offline per `docs/RELEASING.md`, can offer a newer
  digest.
- The status read-model gains an `ImageServe` seam beside `ResidentUnits`, for the
  reason that one exists: it assembles its own render input and must hand the
  renderer the same translation `stackapply.ImageServe` gives every other verb,
  failing closed when the gate is on and the seam is unwired.
- `--security-opt seccomp=unconfined` is rendered because the measurement ran with
  it; whether sd-server on RADV needs it is unmeasured, and dropping it is a
  separate measurement.
- ADR-0027's open footprint question is answered here for image generation: a
  provenanced row in a compiled-in table, because the operator picks the model.
  #308 (ADR-0028) chose a pinned constant for the reranker, which the operator does
  not pick. The two are one rule, not a contradiction.
- Disabling is four steps, not one. Reconcile never deletes a unit (the memory
  precedent), so `image_enabled = false` plus `villa up` re-renders Open WebUI
  without the image group but leaves `villa-image.container` on disk with
  `WantedBy=default.target`, and the next reboot eager-loads 9 GB that no fit
  counts. The operator then runs `systemctl --user stop villa-image.service`,
  removes `~/.config/containers/systemd/villa-image.container` and runs
  `systemctl --user daemon-reload` (`docs/CONFIGURATION.md`). `systemctl disable`
  is not a step: the service is Quadlet-generated (`UnitFileState=generated`), the
  generator applies `WantedBy=` itself, and removing the `.container` file is what
  takes it out of the boot set. Until that
  is done, `villa doctor` WARNs with `IMG-DOC-stale` whenever the service is
  active while the gate is off. A general reconcile-side fix is filed separately.
  Superseded by ADR-0035 (#330): `villa up` now stops and removes the unit itself,
  and doctor's `orphan-units` replaces `IMG-DOC-stale`.
