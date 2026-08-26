# How local-first tools check upstream versions without telemetry

Findings for [Research: how local-first tools check upstream versions without telemetry](https://github.com/MatrixMagician/VillaStraylight/issues/85), on the map [Wayfinder map: v1.8 — villa update](https://github.com/MatrixMagician/VillaStraylight/issues/83).

Every claim below was verified against the live endpoint on 2026-08-26 from the gfx1151 dev box, not read from a write-up. Commands are reproducible as given.

> **Placement note.** The repo's historical convention was `.planning/phases/NN-RESEARCH.md`, but that tree was deliberately removed from the working tree (see `CLAUDE.md`, "Historical planning artifacts"). This file goes in `docs/research/` as the nearest sensible home.

## Summary for the tickets waiting on this

1. **Every component villa pins can be checked anonymously, over HTTPS, with no account and no identifying payload.** No component forces a credentialled or telemetry-bearing path.
2. **A registry check is a `HEAD` returning one header.** `docker-content-digest` is the whole answer; no layer is fetched, so a check costs roughly a kilobyte rather than the gigabytes a pull costs.
3. **`podman auto-update` is not available to villa**, and this is structural, not a preference. It requires a fully-qualified *tag* reference and is documented as incompatible with pinning by ID/digest. Villa pins digests, so this mechanism is out.
4. **All but two of villa's pins have already drifted** — eight of ten, including every rolling ref and three of four backend images. The check is not hypothetical: `vulkan-radv` was rebuilt hours before this research ran.
5. **Version-tag components cannot be checked by tag→digest at all.** `tags/list` is unordered and, for the backend repo, 97% noise. "Newest" requires client-side parsing of a namespace villa does not control.
6. **Registries see the client IP by construction, and Docker Hub echoes it back.** This is the outbound fact [Outbound honesty](https://github.com/MatrixMagician/VillaStraylight/issues/87) must reckon with.

## Live drift as of 2026-08-26

Pinned values from the tree versus what the tag resolves to now:

| Component | Pinned | Tag resolves to now | Drifted |
|---|---|---|---|
| `rocm-7.2.4` (`backend_rocm.go:36`) | `sha256:2da150c1…` | `sha256:1e666bff…` | **yes** |
| `rocm-6.4.4` (`backend_rocm.go:37`) | `sha256:c81f30a7…` | `sha256:ba706809…` | **yes** |
| `rocm-6.4.4-rocwmma` (`backend_rocm.go:38`) | `sha256:9a97129a…` | `sha256:9a97129a…` | no |
| `vulkan-radv` (`backend_vulkan.go:19`) | `sha256:9a74e555…` | `sha256:f0c7b61f…` | **yes** |
| embedder (`memory.go:43`, same image) | `sha256:9a74e555…` | `sha256:f0c7b61f…` | **yes** |
| Qdrant `v1.18.2-unprivileged` (`memory.go:29`) | `sha256:b79aaa49…` | `sha256:b79aaa49…` | no |
| Open WebUI `:main` (`openwebui.go:66`) | `sha256:7f1b0a1a…` | `sha256:c7929533…` | **yes** |
| SearXNG (`searxng.go:40`) | `sha256:ed29454e…` | `sha256:11a9b34c…` | **yes** |
| distroless `static-debian12` (`websafe.go:48`) | `sha256:b669b9df…` | `nonroot` → `sha256:afa5c872…` | **yes** |
| Crush (`crush-policy.json`) | `v0.76.0` | `v0.91.1` | **yes** |

Two observations that matter more than the individual rows:

- **`vulkan-radv` was rebuilt at 2026-08-26T13:10:45Z**, minutes-to-hours before this check (`skopeo inspect` `Created` field). The upstream publishes continuously.
- **A drifted digest on a `-rocwmma`-style tag is not the same event as a drifted `:main`.** `rocm-6.4.4` moving means the *same declared ROCm version* was rebuilt — an upstream rebuild under a stable name, not a version bump. See "What a moved digest means" below.

## Mechanism 1: OCI registry manifest resolution

The core operation is `HEAD /v2/<name>/manifests/<reference>` with an `Accept` header listing the manifest media types. The registry answers with the `docker-content-digest` header. **No image data is transferred.**

### docker.io (backend images, Qdrant)

Requires an anonymous bearer token first — a public, unauthenticated exchange:

```bash
TOK=$(curl -sS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:kyuz0/amd-strix-halo-toolboxes:pull" | jq -r .token)
curl -sS -I -H "Authorization: Bearer $TOK" \
  -H "Accept: application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json" \
  "https://registry-1.docker.io/v2/kyuz0/amd-strix-halo-toolboxes/manifests/vulkan-radv"
```

Verified response headers:

```
HTTP/2 200
content-type: application/vnd.docker.distribution.manifest.v2+json
docker-content-digest: sha256:f0c7b61fba2741f92dfce9008fb03faf9972a8cdd7c0f6d41063e0e01e373480
docker-ratelimit-source: 140.228.65.63
ratelimit-limit: 100;w=3600
ratelimit-remaining: 100;w=3600
```

**Rate limit: 100 manifest requests per hour, anonymous, per source IP.** Villa's entire working set is under ten components, so a check run costs well under 10% of the hourly budget. Rate limiting is not a design constraint here.

**`docker-ratelimit-source: 140.228.65.63` is the client's own public IP, echoed back.** Docker Hub tracks anonymous callers by IP to enforce the quota. This is inherent to any HTTP request and not something villa sends deliberately, but it is the honest answer to "what does the registry learn": *that this IP asked whether a specific public image had changed, and when*.

### ghcr.io (Open WebUI, SearXNG)

Same shape, different token endpoint, and the token is trivially obtainable (60 characters, no credentials):

```bash
GT=$(curl -sS "https://ghcr.io/token?scope=repository:open-webui/open-webui:pull" | jq -r .token)
curl -sS -I -H "Authorization: Bearer $GT" -H "Accept: ..." \
  "https://ghcr.io/v2/open-webui/open-webui/manifests/main"
```

Verified: `HTTP/2 200`, `content-type: application/vnd.oci.image.index.v1+json`, `docker-content-digest: sha256:c792953395b4…`. **No rate-limit headers returned.** GHCR did not advertise a quota on anonymous manifest HEADs.

### gcr.io (distroless websafe base)

**No token step at all** — the plain request succeeds:

```bash
curl -sS -I -H "Accept: ..." "https://gcr.io/v2/distroless/static-debian12/manifests/nonroot"
# HTTP/2 200, docker-content-digest: sha256:afa5c872c891…
```

Simplest of the three. Note `latest` (`sha256:d75cdd72…`) and `nonroot` (`sha256:afa5c872…`) are different images; `websafe.go`'s comment records the pin was resolved from **`nonroot`**, so that is the tag to check.

### skopeo: the same answer, already on the box

`skopeo` is installed (`/usr/bin/skopeo`) and is the method `backend_rocm.go:32` already documents:

```bash
skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv
# → .Digest = sha256:f0c7b61f…, .Created = 2026-08-26T13:10:45Z
```

It handles all three auth flows uniformly and yields `Created` as a bonus. **Trade-off:** it is an external binary dependency and a subprocess, against villa's single-static-binary posture. Direct HTTPS from Go needs no new dependency (`net/http` suffices) and keeps the outbound surface visible in villa's own code, which matters for #87. Recommendation: **direct HTTPS in Go, with skopeo remaining the documented manual cross-check** — a `verify`-style claim can then be reproduced by hand.

## Mechanism 2: tag enumeration, and why it fails for version tags

`GET /v2/<name>/tags/list` returns every tag. For the backend repo this is **1478 tags**, of which **1430 (97%) are dated build variants** like `rocm-7.2.4_20260731T064922`. Filtering to clean version tags leaves:

```
rocm-6.4.2, rocm-6.4.2-rocwmma, rocm-6.4.3, rocm-6.4.3-rocwmma,
rocm-6.4.4, rocm-6.4.4-rocwmma, rocm-7.1, rocm-7.1-rocwmma,
rocm-7.1.1, rocm-7.1.1-rocwmma, rocm-7.14, rocm-7.2, rocm-7.2.1,
rocm-7.2.2, rocm-7.2.3, rocm-7.2.4
```

Three problems, all disqualifying for automated selection:

1. **The list is not ordered by version.** Qdrant's `tags/list` ends with `v1.9.0-unprivileged` … `v1.9.7-unprivileged`, all *older* than the pinned `v1.18.2`. Taking the tail gives a downgrade.
2. **Ordering requires semver parsing villa would have to own.** Note `rocm-7.14` in that list: naive numeric comparison ranks it below `rocm-7.2.4`, and it is genuinely ambiguous whether it means 7.14 (newer) or a typo'd 7.1.4. There is also `vulkan-radv-perfromance` — a **misspelled tag** shipped upstream. This namespace is not machine-trustworthy.
3. **Nothing in a tag name states host requirements.** This is the constraint [Scope](https://github.com/MatrixMagician/VillaStraylight/issues/84) already handed to [Trust model](https://github.com/MatrixMagician/VillaStraylight/issues/86): a registry can report that `rocm-7.14` exists, but not the kernel/Mesa floors it needs. Floors are a claim about what was tested on hardware.

**Conclusion: tag enumeration can inform a human curator; it cannot drive an automated update.** For ROCm and Vulkan the "what version should we be on" decision is irreducibly curatorial. This is direct support for the hybrid (c) option in #86.

## Mechanism 3: GitHub Releases (Crush)

```bash
curl -sS "https://api.github.com/repos/charmbracelet/crush/releases/latest"
```

Verified: `tag_name: v0.91.1` (pinned: **v0.76.0** — 15 minor versions behind). Assets include `crush_0.91.1_Linux_x86_64.tar.gz`, `checksums.txt`, `checksums.txt.sigstore.json`, and an SBOM.

**Rate limit: 60 requests/hour unauthenticated, per IP** (`x-ratelimit-limit: 60`). One component, one request per check — comfortable, but an order of magnitude tighter than Docker Hub, and shared with anything else on that IP hitting the GitHub API.

The verify story is **better than what villa currently uses**. `checksums.txt` gives the tarball SHA-256 (`4d85889585023f587bd11dcd69954bfcc573e43ff03b78664e747282f8770d5f` for v0.91.1), matching the `sha256`/`binarySha256` fields `crush-policy.json` already carries. Villa's existing checksum-before-extract gate transfers to a new version with no redesign — only the values change.

## Mechanism 4: signatures and attestation

- **Crush ships a sigstore bundle** (`checksums.txt.sigstore.json`, 10 KB) alongside `checksums.txt`. Verifying it proves the checksums file came from Charm's release workflow, not just that the bytes match what a checksums file *claims*. Villa's current model trusts a checksum a human copied into `crush-policy.json`; sigstore would let the *fetched* checksums be verified in-band.
- **`cosign` is not installed on the dev box** (`which cosign` → not found), and adding it contradicts the single-static-binary posture. Verifying a sigstore bundle in-process needs a Go dependency (`sigstore-go`), which is a real dependency decision, not a free win.
- **Recommendation: out of scope for v1.8, and worth stating as a deliberate deferral.** The pinned-checksum model already gives villa integrity for the artifact it *intends* to fetch. Sigstore raises trust in *upstream authorship*, which only becomes load-bearing under option (b) of #86 where villa auto-resolves pins it never vetted. Under a curated model, the human curator is the trust anchor and a compiled-in checksum is the mechanism.

## Mechanism 5: podman auto-update — ruled out structurally

`man podman-auto-update` on this box, verbatim:

> The `registry` policy requires a **fully-qualified image reference** (e.g., `quay.io/podman/stable:latest`) to be used to create the container. This enforcement is necessary to know which image to actually check and pull. **If an image ID was used, Podman would not know which image to check/pull anymore.**

Villa's units are digest-pinned, so `podman auto-update` cannot resolve them. Beyond that, its whole model conflicts with the house pattern:

- It is driven by `podman-auto-update.timer`, **daily at midnight** — automatic, unattended outbound, the opposite of on-command.
- It pulls and **restarts the unit** on any digest change, with no prove step and no residency proof. Under ADR-0003 that is mutate-without-prove.
- `--dry-run` exists and is a genuinely good precedent for `villa update --check`'s read-only shape, but the surrounding machinery is unusable.

**Do not adopt. Cite as prior art for `--dry-run`, and as a worked example of the automatic-update posture villa is rejecting.**

## Mechanism 6: Renovate and Flatpak, briefly

- **Renovate** solves exactly villa's problem — digest-pinned refs, updated by a bot that opens a PR with the new digest — but it solves it **in the repo, not on the host**. That is precisely option (a)/(c) in #86: pins stay compile-time, and a *tooling* step refreshes them before a villa release. Worth noting that if #86 lands on curated pins, Renovate is the obvious way to keep the curator's list fresh without hand-running `skopeo`.
- **Flatpak** pulls from a signed OSTree remote with a summary file listing current commits per ref, and verifies GPG on the metadata. The transferable idea is the **signed manifest of current versions** — one fetch, covering every component, authenticated as a unit. That is the shape a villa-published pin manifest (hybrid (c)) would take, and it collapses N registry round-trips into one fetch from an origin villa controls.

## What a moved digest means, per pin shape

This distinction is the research's main contribution to #86 and #90, because it determines what `villa update --check` can honestly *say*:

| Pin shape | Components | A moved digest means | Check mechanism |
|---|---|---|---|
| Rolling tag | Open WebUI `:main`, SearXNG, distroless | **New upstream build.** Expected, frequent, no version to name. | tag→digest HEAD, sufficient on its own |
| Version tag | ROCm 7.2.4 / 6.4.4, vulkan-radv, Qdrant | **The same declared version was rebuilt.** Not a version bump — the maintainer moved a stable name. | tag→digest detects the rebuild; a *version* bump needs curation |
| Checksummed asset | Crush | **New release** with new checksums. | Releases API, then existing checksum gate |

The middle row is the uncomfortable one and deserves an explicit decision in #86 or #89. `rocm-7.2.4` drifting from `sha256:2da150c1…` to `sha256:1e666bff…` means **the image villa validated on hardware is no longer the image that tag names**, while nothing about "ROCm 7.2.4" changed. Adopting it silently would discard the on-hardware validation the current pin represents — and the tree already anticipates this hazard: `rocm-policy.json` carries `imageDeny: ["rocm7-nightlies"]` and `firmwareDeny: ["20251125"]`, evidence that upstream has shipped bad builds under respectable names before.

So "an update is available" is genuinely ambiguous for four of villa's components, and `--check` should not flatten a rebuild and a version bump into one word.

## Outbound footprint, itemised for #87

Per full `villa update --check` of every in-scope component:

| Destination | Requests | Purpose | What it reveals |
|---|---|---|---|
| `auth.docker.io` | 1 per repo (2 repos) | anonymous pull token | client IP, repo name requested |
| `registry-1.docker.io` | 1 HEAD per image (up to 4) | tag→digest | client IP (echoed in `docker-ratelimit-source`), image name |
| `ghcr.io` | 2 token + 2 HEAD | tag→digest | client IP, image names |
| `gcr.io` | 1 HEAD | tag→digest | client IP, image name |
| `api.github.com` | 1 GET | latest Crush release | client IP, repo name |

**Roughly 8–13 HTTPS requests, a few kilobytes total, no request body, no cookies, no credentials, no user-agent villa need not send.** Nothing identifies the host, the user, the model set, or any usage. The information disclosed is: *an IP asked whether specific public artifacts had changed, at a point in time*.

Two honest caveats for #87 to weigh:

1. **The set of images asked about is itself a fingerprint.** Anyone correlating requests across `kyuz0/amd-strix-halo-toolboxes` + `qdrant` + `searxng` + `open-webui` could infer "this is a VillaStraylight install", and the subset asked about leaks *which addons are enabled*. Faint, but real, and it follows directly from [Scope](https://github.com/MatrixMagician/VillaStraylight/issues/84)'s installed-footprint decision — asking only about what you run means the question describes what you run.
2. **A per-component check reveals more than a single manifest fetch would.** A villa-published pin manifest (hybrid (c)) would be **one** request revealing only "a villa is checking for updates", not which components are installed. That is a genuine privacy argument for (c) over (b), independent of the floors argument.

## Recommendations

1. **Direct HTTPS from Go, not skopeo, not podman auto-update.** No new dependency, no subprocess, outbound visible in villa's own code.
2. **`HEAD` the manifest; never `GET` a layer during check.** Keeps `--check` provably read-only and cheap.
3. **Do not attempt automated version-tag selection.** Tag lists are unordered, noisy, misspelled, and silent on host floors.
4. **Distinguish "rebuilt" from "new version" in `--check` output.** Four components can only ever report the former from a registry.
5. **A villa-published pin manifest is the strongest option** on both the floors argument (#84's constraint) and the fingerprinting argument above. It costs a publishing pipeline villa does not have today.
6. **Defer sigstore/cosign** with a stated reason; revisit if #86 chooses upstream-resolved pins.

## Reproducing this

Every command above runs as given and needs no credentials. Digests will have moved on; the drift table is a snapshot of 2026-08-26, not a claim about any later date.
