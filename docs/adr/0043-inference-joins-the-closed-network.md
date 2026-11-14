---
status: accepted
---

# Inference joins the closed network

ADR-0036 put the seven services with no runtime egress on `villa-closed`
(`Internal=true`) and left one follow-up open: "`villa-llama` and the resident slots
stay on `villa.network`." Inference is the one service whose zero runtime egress is
the product claim, and it was still a measured property plus an absent download flag,
not something the network enforced. `villa-llama` is also the privileged unit
(`seccomp=unconfined`, `/dev/kfd`, `/dev/dri`), so a route off-box from it is worth
more to an attacker than one from any closed service. This is the follow-up (#349).

llama-server needs no route off-box at runtime. The rendered argv carries no `-hf`,
`--model-url` or other fetch flag, and the weights are a read-only bind mount from
the models volume. `TestInferenceUnitsCarryNoFetchFlag` enforces the first, so a
future flag fails the build instead of failing at start on a network with no route.

## Decision

- **`villa-llama` and every resident slot `villa-llama-<slug>` join `villa-closed`
  and nothing else.** `parseContainerArgs` is the one construction point for both, so
  one line moves them together. `After=` names `villa-closed-network.service`, the
  same as the closed services.
- **The loopback host publish is unchanged** (`127.0.0.1:8080`, and one port per
  slot). `villa code`, the CLI, the dashboard and the residency proof reach inference
  through it, and the units keep it. Whether a published port works from an
  `Internal=true` network is the premise this change rests on; it is measured below.
- **`villa-inferproxy` joins `villa-closed` and `villa-sandbox`.** It reaches llama on
  the closed network and the task VMs on the sandbox network, and drops `villa.network`:
  nothing it does needs a route off-box. `villa-llama` still does not join the sandbox
  network (ADR-0011), so a compromised task has no path to the privileged unit except
  the proxy's two routes. Both of the proxy's networks are now internal.
- **Open WebUI is unchanged.** It already joins both networks, so it reaches llama by
  container DNS on the closed side (the dual-homed DNS prototype of ADR-0036). Its
  `After=villa-llama.service` already orders it after llama, and so after the closed
  network llama pulls in.
- **An in-network probe still runs on its target's network** (ADR-0036). The probes of
  `villa-llama` and `villa-inferproxy` move to `villa-closed`: `runProbeCurlIn` (the one
  door for a request `inference.Client` built, used by doctor's search-load residency
  drive and `verify agent`) and `liveInferproxyHealth`. Without this the inference
  probes would read down on a healthy stack, and the status row for the proxy with them.
- **`verify agent` keeps its egress negative control on the routed network and
  changes its positive sanity target.** The control asks whether the routed network's
  egress is blocked, and Open WebUI, SearXNG and the web loader still sit there, so its
  meaning is unchanged. Its sanity probe used to curl `villa-llama`, which is no longer
  on that network. It now curls Open WebUI's `/health` route
  (`orchestrate.OpenWebUIInNetworkHealthURL`): Open WebUI renders unconditionally, so it
  is the one routed service always present. The probe omits `-f` because the question
  is whether a peer on the network answers at all, not whether the chat UI has finished
  starting. The sanity probe no longer proves llama is up. The agent task and the
  llama-down control that follow already fail loudly when it is not, so no check is
  lost, only moved.
- **Doctor's `networks` finding needs no code change.** It reads the rendered units
  through `orchestrate.NetworkTopology`, so it asserts the new topology as soon as the
  render changes. Its guard tests (`TestRoutedUnitsKeepTheirNetworks`,
  `TestNetworkTopologyReadsTheRenderedUnits`) now state the new expectation and a
  second test states the inference side (`TestInferenceUnitsJoinOnlyTheClosedNetwork`).
  The report schema does not move.
- **This amends ADR-0036 and ADR-0011.** ADR-0036's bullet "`villa-llama` and the
  resident slots stay on `villa.network`" is superseded. ADR-0011's proxy paragraph
  ("joins BOTH `villa.network` and `villa-sandbox.network`") now reads closed, not
  routed. Each carries an "Amended by" note pointing here.

## Considered options

- **Move only the unit render and the probes (chosen).** The smallest change that
  makes the topology true: the render, the two probe doors, the one control whose
  target moved.
- **Also rename the `routedNetwork` doors.** Rejected as noise. `runProbeCurl*` and
  `statusProber` already take the network as an argument, and a rename would touch
  every verify verb for no change in behavior.
- **Keep `villa-llama` dual-homed.** Rejected: a unit on a routed network is exactly
  the property being removed. A client that needs both sides is dual-homed, as Open
  WebUI is; a server needs only the side its clients are on.
- **Keep the sanity probe on llama and run it on `villa-closed`, with a second routed
  sanity.** Rejected: the control's whole point is the routed network, and a second
  probe adds a failure mode without adding a check the later steps lack.

## Consequences

- `villa-llama`, each resident slot and `villa-inferproxy` have no route off-box:
  a curl to 1.1.1.1 from inside one fails with a connect error and a public hostname
  does not resolve. The rendered and running topology are compared by doctor's
  `networks` finding.
- Every rendered llama and resident unit and the inference proxy unit change, so a
  host's first `villa up` after the upgrade restarts them and the model reloads. A
  stack apply or update that rewrites them restarts them too (ADR-0038); an update
  should therefore wait for readiness before it proves the current state (#359).
  Until a changed unit is restarted, doctor reports a running container on a network
  its unit no longer names as a BLOCK FAIL, which is the finding working as intended.
- Residuals this change does not close:
  - Open WebUI, SearXNG and the web loader still route off-box, and every egress
    negative control still measures them.
  - The published loopback port is reachable by any local process on the host. The
    inference key (ADR-0011) is the control there, unchanged.
  - Weight and image pulls happen on the host through podman, never inside llama.
  - `villa inference run` and `validate` start their own throwaway container with
    `podman run`, not the Quadlet unit, so they are outside this topology.
- A future inference-like unit joins `villa-closed` unless it must fetch, and says why
  in its ADR.

## Measured: a published port on an internal network

Rootless Podman has to forward `127.0.0.1:8080` to a container whose only network is
`Internal=true`. Two records in the tree disagreed. ADR-0036 states that a prototype on
this host (podman 5.8.7, netavark 1.17.2, aardvark-dns 1.17.1) "showed a published
loopback port still works on an internal-only network". The v1.11 spec (section 3.4,
from #161, 2026-09) says such a port "is unreachable from the host under crun and krun
alike". No test in this repository can settle it, because it is a property of the
host's rootless networking, not of villa's render.

Measured on the dev host on 2026-11-14 (Fedora 45, podman 6.1.3, netavark with pasta,
crun 1.31), first on a throwaway internal network and then on the live stack (#364):

- a container on an internal-only network answered `200` on its loopback publish from
  the host, could not reach `1.1.1.1`, and was resolved by name from an internal-only
  and from a dual-homed container;
- after `villa up`, `villa-llama` was on `villa-closed` only and `127.0.0.1:8080/health`
  answered; a completion was read by eye; `curl https://1.1.1.1` from inside
  `villa-llama` failed to connect (exit 7);
- `villa doctor` passed overall, with `networks`, the offload proof, the agent tool-call
  round trip, and the memory and search residency findings;
- `villa verify memory` (Open WebUI to the embedder, Qdrant and llama), `villa verify
  agent` and `villa verify search` passed;
- with the workspace agent on, `villa-inferproxy` was on `villa-closed` and
  `villa-sandbox`, and one workspace task ran to `done` through it.

So ADR-0036's reading holds on podman 5.8.7 (its prototype) and on 6.1.3 (this run).
The spec's sentence was not reproduced under crun; the krun half of it concerns a task
VM, which publishes no port. This is evidence for those two versions, not a guarantee
for every rootless network backend: a host where the publish fails loses `villa code`,
the CLI and the dashboard at once, and `villa doctor`'s health finding names it.

Accepted on 2026-11-14: the maintainer filed this move as ADR-0036's follow-up pending
an on-hardware proof, and directed it merged once the proofs above passed.
