---
status: accepted
---

# Services with no runtime egress join a closed network

The security reviews of PRs #317, #322 and #324 found the same gap (#333). Every
auxiliary unit (`villa-qdrant`, `villa-embed`, `villa-rerank`, `villa-extract`,
`villa-stt`, `villa-tts`, `villa-image`) joined `villa.network`, which routes to the
internet. Their zero runtime egress was a measured property plus env gating
(`HF_HUB_OFFLINE`, `DOWNLOAD_MODEL=false`, explicit weight paths), not something the
network enforced. The live stack showed what that is worth: on 2026-10-09
`villa-qdrant` logged `Telemetry reporting enabled` at start, because Qdrant reports
telemetry unless `QDRANT__TELEMETRY_DISABLED` is set and villa never set it. Env
gating covers the leaks someone thought of.

## Decision

- **A third podman network, `villa-closed` (`Internal=true`), rendered
  unconditionally.** It is rendered the way `villa-sandbox.network` is: always, and
  appended last, so no existing unit moves. One `network.tmpl` now renders all three
  network units from `{UnitFileName, NetworkName, Internal}`; the two existing network
  goldens are byte-identical, which is the proof the merge changed nothing.
- **Every service with no runtime need to reach off-box joins it, and only it.**
  That is the seven units above. Each was checked: the embedder, reranker, whisper and
  sd-server load explicit paths from the read-only models volume; Kokoro's image
  carries its weights and runs with `DOWNLOAD_MODEL=false` and `HF_HUB_OFFLINE=1`;
  Tika serves a local parser; Qdrant needs nothing off-box and wants to send telemetry.
  Weight and image pulls happen at install and update through podman on the host,
  never inside these containers. Their `After=` names `villa-closed-network.service`.
- **Open WebUI joins both networks, unconditionally.** It is the one client of the
  closed services, and it keeps `villa.network` for `villa-llama`, SearXNG and the web
  loader. Dual-homed DNS was settled by a prototype on this host (podman 5.8.7,
  netavark 1.17.2, aardvark-dns 1.17.1): a container on a routed and an internal
  network resolves names on both, in either `Network=` order, and its default route
  stays the routed network's. Joining only when a closed service is on would be a
  second rendered shape for no gain: an empty network costs one bridge.
- **SearXNG and the web loader keep the routed network.** They exist to fetch.
- **`villa-llama` and the resident slots stay on `villa.network`.** The same prototype
  showed a published loopback port still works on an internal-only network, so moving
  them is possible. It is not this change: it moves the inference proof, the inference
  proxy's topology (ADR-0011) and every resident slot, and needs its own on-hardware
  proof. It is filed as a follow-up.
- **An in-network probe runs on its target's network.** `probeCurl` takes the network.
  The probes of the seven closed services (memory, reranker, extractor, voice and
  image readiness, the status rows, eval, doctor's embed drive) run on `villa-closed`;
  the probes of `villa-llama`, SearXNG, the web loader and the inference proxy, and
  every egress negative control, run on `villa` as before. The negative controls
  measure Open WebUI's network, which still routes, so their meaning is unchanged.
- **Doctor asserts the boundary on the host (finding `networks`, report schema 12).**
  Two host facts can break the boundary while every unit file is right. Quadlet creates
  a network with `podman network create --ignore`, so a `villa-closed` that already
  existed without `Internal` stays routed; and a unit rewritten without a restart
  leaves its container on the old network. Doctor reads every running container's
  networks (`podman ps`) and every network's `Internal` flag (`podman network ls`) and
  compares them with the units the loaded config renders: a running container on a
  network its unit does not join, or missing one it does, is a BLOCK FAIL, and so is a
  rendered `Internal=true` network that exists on the host without it. A read that
  fails is a typed-Unknown WARN. `orchestrate.Networks` is the one parse of the
  rendered units' `ContainerName=`, `Network=`, `NetworkName=` and `Internal=` lines.
- **A host upgrades through the existing stack apply.** `villa up` writes the new
  network unit and the changed units and restarts every running service whose unit
  changed (ADR-0013); each restarted closed service pulls in
  `villa-closed-network.service`, which creates the network. A swap does the same
  inside its transaction frame (ADR-0015).

## Considered options

- **Keep env gating and measurement.** Rejected: the Qdrant telemetry line is the
  counterexample, and each new service would need its own audit of what it might
  fetch.
- **Name the network `villa-internal` or `villa-aux`.** Rejected: CONTEXT.md already
  calls `villa-sandbox` the internal network, and "aux" names a role, not the property
  the operator relies on. `villa-closed` says what it is.
- **Run every probe on both networks.** Rejected: `podman run --network villa-closed`
  fails on a host where nothing has created the network yet (memory, voice and image
  all off before the first `villa up`), which would turn every doctor and status probe
  of `villa-llama` into an Unknown.
- **Pick the probe network from the URL's host.** Rejected: a hidden mapping from curl
  arguments to topology, where an explicit argument at each probe reads directly.
- **Report the networks as a status row.** Rejected: status rows are per-service
  health for the dashboard poll; this is an enforcement assertion with a remediation,
  which is doctor's shape (SBX-02, TMD-01).
- **Disable Qdrant telemetry by env in this change.** Deferred to its own issue: the
  closed network already stops the traffic, and the env line is belt and braces that
  belongs with the other per-service env.

## Consequences

- The seven closed services have no route off-box: a curl to 1.1.1.1 from inside one
  fails with a connect error, and a public hostname does not resolve. `villa verify
  memory` still needs the host egress block for Open WebUI, which stays routed.
- Every rendered closed-service unit and the Open WebUI unit change, and
  `villa-closed.network` is new: a host's first `villa up` after the upgrade restarts
  them.
- Doctor runs two more read-only podman commands and every report carries the
  `networks` finding; the schema moves to 12 with no other field changed.
- A future service joins `villa-closed` unless it must fetch, and a service that must
  fetch says why in its ADR.
