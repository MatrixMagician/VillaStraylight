---
status: accepted
---

# `villa update` prunes container images that install never would

Nothing in this project has ever deleted a container image. `villa uninstall`
removes generated units, non-model volumes, the agent binary and — only on
`--remove-models` — the weights, and leaves every image in place. ADR-0003 states
the rule directly: *"Model weights and container images are NOT captured or
removed. They are large, expensive to re-acquire, and inert on their own — an
unused GGUF on disk harms nothing."*

`villa update` breaks that rule in both directions. It **captures** an image as
part of a rollback point, and it **removes** one once that rollback point is
superseded. This ADR records why the exception is principled rather than a
convenience, and the three constraints that keep it safe.

## Why update is a different case from install

ADR-0003 reasons about images that are **incidental** to the operation. A failed
install leaves an image nothing references; deleting it buys nothing and
re-pulling it later costs gigabytes. Inertness is the whole argument: the image
harms nothing by existing.

Under `update` the image **is the subject of the transaction**. The operation is
"move this component from digest A to digest B", so A is not incidental — it is
the state being changed, and therefore the state a rollback must restore. An
update that does not capture the prior image has no rollback target at all.

> **Correction, from a live incident: the image is not always the state being
> changed.** The paragraph above is true of the backends, SearXNG and the websafe
> base, whose images carry no data of their own. It is **false of Open WebUI and
> Qdrant**, which own a mutable data volume. A real `villa update chat` on gfx1151
> migrated Open WebUI's SQLite `config` table from `id/data/version` to
> `key/value`; the retained image could not roll that back, because the image was
> never the thing that changed. The restore put the old digest onto a database it
> could no longer read, and it crash-looped 24 times.
>
> The reasoning below is untouched by this — reference counting, one previous, and
> WARN-on-failed-prune are all still right, and are the decisions this ADR was
> written to make. What the incident changed is the **premise about scope**: for a
> component that owns persistent state, "the state a rollback must restore" is the
> image **plus its data**. That is now what the retained tuple carries, and the
> capture and restore of the data half are described in
> [the v1.8 spec](../spec/v1.8-villa-update.md) §5.5 and §5.6.
>
> The transferable lesson is not about one image. **A rolling tag on a stateful
> component carries migration risk on every rebuild.** Open WebUI is pinned
> `:main`, so nothing announces that a schema moved — the digest simply changes and
> the data changes with it. A version-tagged stateful component (Qdrant, at
> `v1.18.2-unprivileged`) at least signals the possibility. A future decision to
> pin a stateful component to a rolling tag should be made knowing that.

The same reasoning that made ADR-0003 keep images is what makes this ADR keep one
previous rather than pruning eagerly. Expensive to re-acquire is not a reason to
avoid deleting *any* image; it is a reason to keep the one you might need. The
principle is unchanged; the operation it applies to is new.

There is a sharper reason than cost. A digest that upstream has stopped serving
cannot be re-pulled at any price. The backend images are rebuilt continuously —
`vulkan-radv` moved digest on the day this was written — so "we can always fetch
it again" is not reliably true of a rebuilt tag. A GGUF at a stable URL is far
more re-acquirable than a digest behind a rolling tag.

## What is retained, and for how long

**Exactly one known-good previous per subsystem.** It is recorded explicitly in
the effective-pin store, not inferred by scanning the image store.

The retained thing is a **tuple** — the digest, the verbatim prior unit bytes, and
the config it was proven under — not a bare digest. This is the same capture the
transactional swaps already perform (`internal/backendswap` captures *"the
verbatim prior `villa-llama.container` bytes and the prior VillaConfig… STRICTLY
BEFORE any mutation"*). A digest alone is not restorable: weeks later the image
may still be present while the unit renders differently or the config has moved on.

For a subsystem that **owns persistent state** the tuple carries a fourth member: a
**data snapshot**, exported while the service is stopped. Chat and memory are the
two, and for them the three members above are not a rollback target at all — a
forward data migration makes the retained image unusable on its own. The stop is
load-bearing rather than incidental: a volume exported from under a running service
is a torn copy, which is a safety net that only fails when used.

Recording it explicitly rather than inferring it is load-bearing. An inferred
previous — whatever other villa-ish image happens to be in the store — would let
an unrelated image become a rollback candidate, and a user who ran `podman image
prune` by hand would silently lose rollback protection with nothing to notice it.
Recorded, the guarantee is checkable, and a recorded previous that has gone
missing is **surfaced rather than fail-softed**.

**"Known-good" means known-good-when-captured, not known-good-now.** The retained
tuple is never re-proven on a schedule: that would mean taking down a working
stack to prove a version nobody is running. If the host has changed underneath —
a kernel upgrade past a floor, a driver change — the retained previous may no
longer work either, and villa will not discover that until a rollback attempt.
This is the same honesty `verifystate` applies to a cached PASS, which is read
with a freshness check rather than as an eternal truth.

## Considered options

**Prune immediately once the proof passes** — rejected. The proof passes at a
*moment*; the failures that matter most appear later. A leak after six hours, a
regression under real load, a behaviour change no proof covers. Villa proves
residency and health, not correctness-forever, so the window in which the previous
version is valuable extends well past the commit.

**Keep N previous versions, or keep by age** — rejected as unfounded. The
second-previous has never been proven on this host in its current configuration,
so it is not a known-good landing spot, merely an older one. N is a number nobody
would tune with evidence.

**Keep everything, never prune** — rejected. This is ADR-0003's posture, and it is
right for install. Under update it means unbounded growth: every rebuild of a
rolling tag leaves another multi-gigabyte image behind, and the stack has four
image-bearing subsystems. The operator would be left to prune by hand, which is
precisely the act that silently breaks rollback protection.

## Removal is reference-counted, never per-component

Two components in this project share one image. `embedImage` in
`internal/orchestrate/memory.go` and `vulkanImage` in
`internal/inference/backend_vulkan.go` are byte-identical digests: one image
serving two roles, the inference backend and the embeddings server.

A per-component prune would therefore be capable of deleting an image a **running
service still depends on**. When memory updates, the old embedder digest becomes
memory's retained previous — while that same digest may be the inference
backend's *current* effective pin. Removing it is not a lost rollback; it breaks a
running stack.

**An image is removable only when no current pin and no retained previous —
anywhere in the effective-pin store — references it.** Villa computes the live
reference set before any removal, over resolved digest values rather than over the
source constants, so two accessors that happen to return the same string count as
two references without needing to know about each other.

A consequence worth stating because it will look like a bug: prune will sometimes
no-op right after a successful update, because the digest it would have removed is
still referenced elsewhere. That is reported (*"retained — still referenced by the
inference backend"*), never silent.

## A failed prune is a WARN, never a rollback

Prune runs after the post-mutation proof has already passed, so the update has
succeeded before prune is attempted. Rolling back a proven-good update because a
cleanup step failed would be perverse.

**This is the one step in the update lifecycle where fail-soft is correct**, and
the reason is precise: the failure leaves *more* safety, not less. An image that
could not be removed is exactly the inert leftover ADR-0003 says harms nothing.
Villa reports the disk it could not reclaim and moves on.

Everywhere else in the lifecycle the posture is the opposite — an unprovable
component rolls back rather than committing on evidence villa does not have.

## Consequences

`villa uninstall` is unchanged and still removes no images. This ADR licenses
removal for `villa update` only, and only for an image the store shows is
referenced by nothing.

The effective-pin store becomes the authority for what may be deleted. That makes
it security-relevant state in a way the other stores are not: a store that has
lost its retained-previous records does not merely forget a convenience, it loses
the record of what is safe to remove. Its fail-closed Load must therefore fall
back to "retain everything" rather than to an empty reference set — the zero value
here is the unsafe direction, and this is the one place `jsonstore`'s usual
absent-means-empty reading must not be applied naively.

An operator who prunes images by hand can still break rollback protection. Villa
detects and reports this rather than preventing it; the image store is the
operator's, and villa's claim is limited to what it can see. The same is true of
the data snapshots, which live under villa's data root and are equally removable by
hand — a recorded snapshot that has gone missing is surfaced for the same reason a
missing image is.

One consequence of the correction above is stricter than anything else in this ADR:
**a stateful subsystem whose data villa could not snapshot is not updated at all.**
That is the opposite posture from the WARN-on-failed-prune rule, and deliberately
so. Prune runs after the update has already succeeded, so its failure leaves more
safety; a failed capture runs *before* any mutation, so proceeding would mutate
data with nothing to go back to — which is exactly what produced the incident. The
accepted cost is that a full disk or an unavailable Podman blocks updating chat and
memory entirely.
