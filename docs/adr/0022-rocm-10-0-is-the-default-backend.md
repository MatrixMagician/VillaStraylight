---
status: proposed
---

# ROCm 10.0 becomes the default inference backend

The default backend's pin, kyuz0's `rocm-7.2.4` at `sha256:2da150c1…`, carries
llama.cpp build 9536. That build has no `qwen4exp` architecture, so Qwen3.8-Flash-Next
and anything else on it cannot load on the default backend (#297). The two other
pinned llama.cpp images already have it: `rocm-6.4.4` (build 10664) and
`vulkan-radv` (build 10906).

The default cannot catch up by moving its digest. kyuz0 stopped updating the
`rocm-7.2.4` tag on 2026-08-01, and the one rebuild it did get was refused in the
#205 re-vet (build 10217 streamed a degenerate reasoning block under ngram-mod). kyuz0
now publishes `rocm-10.0`, `vulkan-radv` and `therock-nightly`, each rebuilt daily on
llama.cpp master, with dated snapshot tags beside them.

The #296 prototype ran `rocm-10.0` (build 11430, `sha256:3893b3e5…`) on the dev
host: it loaded an 88 GB Qwen3.8-Flash-Next GGUF fully offloaded (so it does not have
the 64 GB allocation cap the ROCm nightlies tag had), decoded sanely and scored 19/20
on the eval suite. Under `--spec-type ngram-mod` it hung once at token 32 on that
model.

## Decision

- **A new backend, `rocm-10.0`, is added as a new pins component**
  (`backend-rocm-10.0`), not as a moved digest of `backend-rocm-7.2.4`. ROCm 10.0
  is a different upstream release, and `ComponentID`s are added, never renamed.

- **Its shape is `RollingDigest`, like `vulkan-radv`.** kyuz0 rebuilds the
  `rocm-10.0` tag daily with a newer llama.cpp, so a moved digest means a newer build
  of the same channel, not a rebuilt release. `VersionTag` would report every one of
  those as "this release was rebuilt", which would be wrong most days. Villa pins
  the digest; `villa update --check` offers a newer one only when a signed manifest
  carries it, and nothing moves without fetch and prove. It carries the ROCm floors,
  as every ROCm pin does.

- **It becomes the default.** `BackendFor("")` and `BackendFor("rocm")` resolve to
  it, and so does `IsROCmFamily`'s view of them: the CLAUDE.md invariant that the two
  agree holds unchanged. The backend keeps the `Name()` `"rocm"`, so the name in
  `status --json`, bench reports and eval provenance still means "the default ROCm
  backend"; the image and the unit's `Description=` label are what change.
  `recommend`'s default and `seed.json`'s `backend_default` stay the string `"rocm"`
  and follow.

- **7.2.4 stays reachable as an explicit `rocm-7.2.4`.** An operator who needs the
  old build sets it by name. `"rocm"` is the default's name, not a version.

- **The flip lands only after a full on-hardware vet** (the `docs/RELEASING.md`
  step 4 method): every catalog entry that defaults to ROCm, in each speculation
  mode it is qualified for, with vision and tools mode where the entry has them. Each
  run gets the residency proof, `villa eval` against a baseline recorded on 7.2.4,
  and one completion read by hand. A model or mode that fails is either fixed in the
  same change (a marker, a flag spelling, a catalog qualification) or the flip waits.

## Rejected

**Keep 7.2.4 as the default and add 10.0 beside it.** Safe, but it leaves every
operator on a llama.cpp build that cannot load the newest models and that its
upstream will never rebuild. The default would only get further behind.

**`"rocm"` keeps meaning 7.2.4, and only `""` moves.** Nobody who wrote `backend =
"rocm"` would move silently, but `""` and `"rocm"` would then name different images.
That breaks the invariant that keeps an unset config inside the ROCm preflight gate,
and `seed.json`'s `"rocm"` default would have to be re-pointed too.

**Pin a dated snapshot tag (`rocm-10.0_YYYYMMDDTHHMMSS`).** The name would be
immutable, but villa already pins by digest, so the name adds nothing a digest does
not. It would also need a new `Shape` and update logic that discovers newer dated
tags.

**Wait for kyuz0 to rebuild `rocm-7.2.4`.** It has not moved in two months and the
publishing set no longer includes it.

## Consequences

- On the next stack apply, every host with an unset backend or `backend = "rocm"`
  re-renders `villa-llama` on the new image, the dev host included. `villa
  backend set rocm-7.2.4` is the way back.
- An effective pin recorded for `backend-rocm-7.2.4` no longer applies to a host
  on `"rocm"`; it applies again if the operator selects `rocm-7.2.4`.
- The signed manifest (serial 2) does not name `backend-rocm-10.0`. That is fine:
  a component the manifest does not offer reports as current against its compiled-in
  pin. A later manifest can offer newer `rocm-10.0` digests.
- Each enumeration of backend names grows by two (`rocm-10.0`, `rocm-7.2.4`):
  `BackendFor`, `IsROCmFamily`, the pins table, `cmd/villa`'s `backendComponent`,
  `updatecheck`'s `isActiveBackend`, `orchestrate`'s `backendLabel` and
  `recommend`'s family check. That spread is the cost of adding any backend today.
- ngram-mod hung once on build 11430 with Qwen3.8-Flash-Next. Every ngram-qualified
  catalog entry is re-proven on the new build before its qualification carries over.
