# Releasing

How a villa release is cut, and how the signed pin manifest that `villa update`
depends on is published.

> Status: **both halves are implemented as of v1.8.** The release artifact is built
> by `.github/workflows/release.yml`; the manifest format, its allowlist check and
> the offline signer ship as `internal/manifest`, `internal/manifestverify` and
> `cmd/villa-manifest-sign`, and the public key is compiled in. **The first
> manifest is published** (#207): serial 2, valid until 2027-09-11, carrying the
> table re-vetted on 2026-09-11 (#205), signed offline and attached to the v1.11
> release. `pins.VettedSerial` is 2 to match. The fetch URL names the *latest*
> release, so the release workflow carries `pins.json` and `pins.json.sig`
> forward to each new tag (step 2).
> The key custody steps below are deliberately not automated.

## Why any of this is unusual

Two decisions from [the v1.8 map](https://github.com/MatrixMagician/VillaStraylight/issues/83)
shape this procedure, and both cut against what a release pipeline normally does.

**The signing key never touches CI.** The pin manifest is signed with ed25519 and
the public key is compiled into the villa binary, which means manifest trust is
anchored in the artifact you already chose to run. That ceiling is deliberate: the
manifest is no more trustworthy than the binary, and a compromised binary is
already game over. Putting the private key in GitHub Actions secrets would lower
the ceiling to "anyone with workflow-write access", which is the exact property
the anchor exists to avoid.

**A vetted pin is a claim about hardware, not a claim about a registry.** When
villa says a pin is vetted it means *someone ran it on a gfx1151 box and proved
it*. A digest that merely resolves is not vetted. This is why steps 3 and 4 below
cannot be automated away: no CI runner has the GPU.

## Cutting a release

### 1. Pre-flight

```bash
make check          # vet + test + test-race
make build-static   # the CGO-free gate CI enforces; also what the release ships
make lint
```

`make check` does **not** cover the CGO-free build, so run `build-static`
separately if you touched imports. The binary must stay CGO-free because
`villa-websafe` bind-mounts it into a distroless container with no libc.

### 2. Tag

The release notes live in the **annotated tag**, which is where every release from
v1.0 on has carried them. The workflow reads the tag's subject as the release title
and its body as the notes, so a lightweight tag fails the run rather than silently
publishing a generated changelog.

```bash
git tag -a v1.8            # write the notes in the editor, subject line first
git push origin v1.8
```

The tag triggers `.github/workflows/release.yml`, which builds the static binary,
**refuses to publish an unstamped or dirty build**, creates the release from the tag
if it does not exist yet, and attaches `villa_v1.8_linux_amd64` plus `checksums.txt`.

The refusal matters: `VERSION` comes from `git describe`, so a shallow clone or a
dirty tree yields something like `v1.7-21-g4f9b4f6-dirty`. An asset reporting that
would break the version comparison `villa update --check` performs, and it is far
cheaper to fail the workflow than to ship it.

Both halves of the attach step are idempotent (create is skipped when the release
exists, upload uses `--clobber`), so re-running a partially failed workflow
converges rather than erroring.

The workflow does not **sign** the pin manifest, because signing is offline, but
it does **carry it forward**: `villa update --check` fetches
`releases/latest/download/pins.json`, and a new tag moves *latest*, so the last
step copies `pins.json` and `pins.json.sig` byte for byte from the newest earlier
release that carries them. Check the new release lists both files; if no earlier
release carried a manifest the step prints a warning and every install reports
"could not check" until one is attached. Publish a *new* manifest (steps 3 to 6)
over the carried copy only when a pin moved; carrying the old one forward is the
usual case.

## Publishing the pin manifest

### 3. Re-vet the pins, on hardware

For each component whose pin you intend to move, resolve the current digest:

```bash
skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv
```

This is the method `internal/inference/backend_rocm.go` already documents, and it
handles the docker.io / ghcr.io / gcr.io auth flows uniformly. Anonymous access is
sufficient for every component; Docker Hub allows 100 manifest requests per hour
per IP, which is far more than a full pass needs.

**Resolving the digest is not vetting it.** Vetting is step 4.

### 4. Prove each candidate pin before it ships

Bring the candidate up on the gfx1151 box and run the proof that component
actually has:

| Component | Proof |
|---|---|
| backend image | `villa backend set <target>`, a real generation probe **and** the residency proof |
| Open WebUI | `villa status` protocol probes |
| Qdrant + embedder | `villa verify memory` |
| SearXNG + websafe | `villa verify search` |
| Crush | `villa verify agent` |

**A candidate that cannot be proven must not ship as vetted.** This is not
bureaucracy: `villa update` treats an unprovable component as a Reject that rolls
back, so a pin that fails here would make that component **un-updatable for every
user** until a new villa ships.

The specific hazard is **marker drift**. The residency proof scrapes llama.cpp
startup logs for markers pinned in `backend_rocm.go` / `backend_vulkan.go`, and
ADR-0001 warns the log format drifts as the upstream image is rebuilt on llama.cpp
master. A rebuilt backend image is exactly when that happens. If a candidate pin
fails the residency proof:

- update the markers in the same release, **or**
- do not vet that pin.

Shipping it regardless would push a release-blocking defect onto users, who cannot
fix it locally.

For a ROCm pin, also confirm the **floors** still hold: `kernelFloor`,
`firmwareFloor` in `internal/preflight/rocm-policy.json`. A newer
ROCm image can demand a newer kernel or firmware than the shipped floors encode, and
floors travel with the pin: `villa update` re-runs the preflight gate against the
new pin's floors before mutating anything.

### 5. Build the manifest

Generate it from the compiled-in table rather than writing one by hand: every id,
registry and shape is then correct by construction, and the allowlist check has
nothing to catch except the values you deliberately changed:

```bash
go build -o villa-manifest-sign ./cmd/villa-manifest-sign
./villa-manifest-sign build --serial 3 --valid-until 180d > pins.json
```

`--valid-until` takes an RFC3339 timestamp or a bare day count (`180d`). The day
form exists because the window is measured in months and Go durations top out at
hours; doing calendar arithmetic by hand is how a six-**day** window ships when six
months was meant.

The manifest is the serialised form of the compiled-in pin table. Per component it
carries the component id, the pin shape (`version_tag` / `rolling_digest` /
`checksummed_asset`), the pin value, the registry host, and floors where the
component has them. Plus, at document level:

- **`serial`**, a monotonically increasing integer. Villa refuses a manifest whose
  serial is below the last one it saw, so this is the anti-downgrade floor.
  **Allocate it as a simple counter and never reuse or regress it**; if two
  manifests ever share a serial, the newer one is unusable on any host that saw
  the older. `build` refuses a zero serial: zero means "no floor".
- **`valid_until`**, after which villa treats the manifest as **absent** and falls
  back to compiled-in pins. `build` refuses to omit it: a manifest with no expiry
  can be frozen and served forever.

**Pick a generous `valid_until`: months, not days.** Expiry is fail-closed, so
nothing breaks, but because `--check` stays silent rather than falling back to
per-component registry checks, an expired manifest ends update signalling
entirely for every install until you publish again. Combined with checks being
strictly on-command, a user with a lapsed manifest gets no signal at all and has
to think to run `--check` to discover why. The window length is a user-visible
safety parameter, not hygiene.

Edit the generated `pins.json` to carry the pins you re-vetted in step 4, then
check it before signing:

```bash
./villa-manifest-sign check pins.json
```

A manifest may supply new *values* only, for components the compiled-in table
already names; it may never introduce a component, a registry host, a shape, or a
URL template. A manifest that violates this is refused on **every** host, so
`check` catches it here rather than in the field. It reports every problem at once,
not the first, fixing one refusal per round trip is how the sixth one ships.

`check` needs no key, so you can iterate on a draft without one.

### 6. Sign it, offline

```bash
# On the machine that holds the key — NOT in CI, NOT on a shared runner.
./villa-manifest-sign sign --key ~/.villa-signing/ed25519.key pins.json
```

This re-runs the allowlist check and refuses to sign on any violation, then writes
`pins.json.sig` beside the manifest. Attach both to the release.

The signature covers the manifest file **verbatim**, byte for byte as published.
Villa never re-serialises a manifest before verifying it, so do not reformat
`pins.json` after signing: even a trailing newline invalidates the signature.
Verbatim signing is deliberate: canonicalising instead would require both ends to
agree on key ordering and number formatting, and a mismatch there fails
verification for no visible reason, which is indistinguishable from an attack.

If you do not yet have a key:

```bash
./villa-manifest-sign keygen --out ~/.villa-signing
```

It writes `ed25519.key` at 0600 and prints the public key to compile into villa. It
refuses to overwrite an existing key; see Key custody below for why that is
unrecoverable.

## Key custody

The ed25519 private key is the one secret in this project whose loss or exposure
cannot be repaired by a config change.

- **Store it offline**, outside the repo and outside any CI secret store. It is
  used a handful of times a year.
- **Back it up** somewhere you would still have after losing the release machine.
- **If it is lost**, no further manifests can be published until a villa release
  ships carrying a new embedded public key. Existing installs keep working on
  compiled-in pins; they simply stop learning about new ones.
- **If it is exposed**, an attacker can forge pins for every install until a new
  villa ships with a new key. The blast radius is bounded by the allowlist: a
  forged manifest can move you to a bad *version of a component you already run*,
  and cannot introduce a new component, registry, or URL, and the prove step still
  has to pass. Bounded is not harmless.

**Rotation** rides along with releases: a villa release can carry both the
outgoing and incoming public key, accepting manifests signed by either, and a
later release drops the old one. That gives a window where both are valid, so
rotation never requires every install to upgrade on the same day.

## Refreshing the compiled-in vetted pins

The compiled-in table is the fallback when no manifest is available, so it is the
floor on how stale a fresh install can be. It should be re-vetted periodically,
not only when a manifest is published.

The table was last re-vetted on 2026-09-11 (#205), the method above applied on the
dev host to every pin the registry had moved. Six moved and were proven: the
`rocm-6.4.4` and `vulkan-radv` backends (`backend set` plus the residency
proof), Open WebUI (`verify memory` and the status probes), the embedder image,
SearXNG and the websafe base (`verify search`), and Crush v0.93.1 (`verify
agent` and one `villa work` task through the bridge). One moved and was
**refused**: the `rocm-7.2.4` tag had been rebuilt on llama.cpp build 10217,
which streams a degenerate reasoning block under `--spec-type ngram-mod` on the
default model (sane with speculation off, and sane on builds 10664 and 10906),
so the default backend stays on the build 9496 digest until the tag moves again.
Two had not moved (`rocm-6.4.4-rocwmma`, Qdrant). The earlier measurement of
2026-08-26 is in
[`docs/research/update-version-checks.md`](research/update-version-checks.md).

Two things the re-vet taught, both now encoded in the tree: llama.cpp replaced
`--no-mmap` with `--load-mode` between those builds, so the resident-weights flag
is spelled per image (`internal/inference`, `TestLoadFlagIsSpelledPerImage`); and
Open WebUI now refuses a file whose content hash a collection already holds, so
`verify memory` empties its collection before it uploads.

Refreshing them is steps 3 and 4 above, applied to the constants in the tree,
followed by an ordinary release. A pin in the compiled-in table carries the same
claim as one in a manifest, proven on gfx1151, so it earns that claim the same
way.
