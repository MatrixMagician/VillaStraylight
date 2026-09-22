---
status: accepted
---

# The sandbox reaches inference through a /v1 proxy, and llama-server gets a key

Tested live at 81eacec: `villa-llama` on 127.0.0.1:8080 echoed back any `Origin`
header and allowed a credentialed cross-origin `OPTIONS` preflight for
`POST /v1/chat/completions`, because the unit is rendered with no `--api-key`
(GHSA-qxg9). Separately, with `workspace_agent` on, `internal/orchestrate/render.go`
joined `villa-llama` to BOTH `villa-sandbox` (`Internal=true`, task VMs only) and
`villa.network` (which egresses), and that container runs
`--security-opt seccomp=unconfined` with `/dev/kfd`/`/dev/dri` passed through
(GHSA-gvp9). A task VM that compromises llama-server — the tokenizer and
prompt-parsing paths have had memory-safety CVEs — lands on a host process that
can also reach the internet, and the task network boundary no longer holds.

## llama-server gets a key

**Generated once via `crypto/rand`, persisted at 0600 (`config.InferenceSecret`,
mirroring `WebLoaderSecret`/`SearxngSecret`), and delivered via a 0600
`EnvironmentFile=`.** llama-server reads `LLAMA_API_KEY` from its environment
natively (`--api-key`'s documented env var) — the chosen option, because it keeps
the secret off the Exec line and out of every rendered 0644 unit without adding a
CLI flag or a Podman-secrets dependency neither backend seam otherwise needs.

**A `--api-key` argument threaded through `Backend.ContainerArgs`**, rejected.
`ContainerArgs` becomes `Exec=` text verbatim (`parseContainerArgs`); any value
placed there lands in the 0644 unit file, which is the exact leak GHSA-qxg9
flags for `--api-key`'s CLI form in the upstream docs.

**A Podman secret (`podman secret create` + `--secret=`)**, rejected for this
pass. It solves the same problem the EnvironmentFile does, but the villa-websafe
and SearXNG secrets already prove the EnvironmentFile shape end-to-end (rootless,
no extra podman feature-gate, one writer at 0600); a second delivery mechanism for
the same guarantee is not a second win.

The key protects every llama-server instance the render seam produces — the
primary unit and every resident slot — because `parseContainerArgs` is their one
shared construction point (`internal/orchestrate/render.go`), so the
`EnvironmentFile=` line is set once there rather than re-added per call site. Open
WebUI's own `OPENAI_API_KEY`/`OPENAI_API_KEYS` env values move from the
`sk-no-key-required` sentinel to the same secret, carried by the same file (a
second line, `OPENAI_API_KEY=<value>`, alongside `LLAMA_API_KEY=<value>` — one
secret, one 0600 file, three consumer processes, mirroring how villa-websafe and
Open WebUI already share `EXTERNAL_WEB_LOADER_API_KEY`). `GET /health` stays
unauthenticated because it is llama.cpp's own documented public exemption
(confirmed against `tools/server/README.md`), so the readiness probe
(`inference.PollHealth`) needs no change; every other endpoint — `/v1/models`,
`/v1/chat/completions`, `/metrics`, `/slots`, `/props` — now requires
`Authorization: Bearer <key>`, so `internal/llm.Options` gains an `APIKey` field
and every villa client that talks `/v1` (`internal/inference`'s generation probe,
the residency drive, the task-bridge grounding audit, `bench`, `verify`) must
supply it.

Open WebUI's OWN connection to villa-llama (`OPENAI_API_KEY` — a SEPARATE key from
the well-known `sk-no-key-required` sentinel it used when llama-server took no
auth at all) moves to the real secret in the render's env block. When exactly one
endpoint is rendered this needs no file: OWUI reads `OPENAI_API_KEY` straight
from the environment the SAME 0600 file already sets on the container (a second
line in the shared file, never a literal in the 0644 unit). The PLURAL form
(`OPENAI_API_KEYS`, a `;`-joined list matched by position to `OPENAI_API_BASE_URLS`)
that a resident set requires has no such path — Quadlet's `EnvironmentFile=`
cannot compose a repeated, count-dependent value from one entry, and a second
generator that DOES know the resident count would couple the generic
secret-file writer to OWUI's specific list-length concern for a value this
render path already treats as render-time-only: the ONLY consumer of the
render-time env block is a FRESH install's first boot (`ENABLE_PERSISTENT_CONFIG`
governs everything after), and `internal/openwebui.SyncEndpointsWithKey` already
reconciles the running connection list's real keys afterward through OWUI's
admin API — no file, no unit, no render. A resident-model install therefore
carries the real secret in that one render-time `Environment=` line until the
next reconciliation, a narrower, ACCEPTED residual scoped to this ticket:
strictly better than the status quo (no auth at all), and the two vulnerabilities
this ADR closes — the browser-reachable no-auth surface and the dual-network
container — are unaffected by it either way.

## The sandbox reaches inference through a /v1 proxy

**A minimal, in-scope reverse proxy — a new hidden `villa` subcommand run in the
existing distroless-plus-bind-mounted-binary shape `villa-websafe` already uses —
joins BOTH `villa.network` and `villa-sandbox.network` and forwards ONLY
`POST /v1/chat/completions` and `GET /v1/models` to `villa-llama`, injecting the
real key on that leg.** `villa-llama` itself drops `villa-sandbox.network`
entirely; nothing else changes about how it is reached from the host or from Open
WebUI. This is the chosen option: it needs no new image, no new build/publish
pipeline (the same single static binary runs one more subcommand), and it is
strictly narrower than what it replaces — a two-route allowlist with a capped body
size, versus the llama-server's entire unauthenticated HTTP surface (`/slots`,
`/metrics`, `/props`, every other `/v1` route).

**A second llama-server instance dedicated to the sandbox**, rejected. It doubles
the resident-model memory cost on a fixed unified-memory envelope for no isolation
gain the proxy does not already give — the task only ever needs the one already-
served model.

**Require `--api-key` alone and leave `villa-llama` on both networks**, rejected
as insufficient on its own (the second remediation the issue names as a floor,
not the fix): a valid key still lets a compromised task read `/slots`, `/metrics`
and `/v1/models`, fingerprinting the host, and it does nothing about the seccomp/
device-passthrough surface reaching a container that can also egress. The network
split is the actual boundary; the key is what closes GHSA-qxg9 on top of it.

The task bridge (`cmd/villa sandbox-bridge`, run inside the task container) must
target the proxy's in-network endpoint instead of `orchestrate.LlamaInNetworkEndpoint()`
— that follow-up call-site change is reported to the workstream owning
`cmd/villa/sandbox_bridge.go`, not made here (out of this workstream's declared
files). The proxy unit itself is rendered ONLY when `subsystem.SandboxOn(cfg)`,
the same gate that used to add `villa-llama`'s second `Network=` line — an
install with the workspace agent off gets no proxy unit and no behavior change,
mirroring the existing byte-identical-off discipline for every optional managed
service in this render path.

The proxy trusts its own network topology for the inbound leg (villa-sandbox is
`Internal=true`; nothing else can dial it) rather than requiring a second bearer
from the task — a second secret the bridge would have to carry into the microVM
buys no isolation the network boundary does not already give, and every request
it forwards is re-authenticated to `villa-llama` with the real key regardless.
