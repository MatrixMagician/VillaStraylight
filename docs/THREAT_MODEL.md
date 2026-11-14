# Threat model

villa runs a local AI stack on one machine for one person. This page states who
villa trusts, what it does not trust, the boundaries between the two, and the
gaps it has accepted. To report a vulnerability, see [SECURITY.md](../SECURITY.md).

Each control names the file or test that holds it. "Measured" means a hardware
run or a network capture showed the behaviour for the versions named, and no
test holds it between runs.

## Who is trusted

The operator is trusted. The operator is the one local user who runs `villa`,
owns `config.toml`, registers each workspace and answers each approval. A process
that runs as the operator's UID is the operator. villa does not defend against
that process, or against root.

The `villa` binary is trusted, with the vetted pins and the manifest verification
key compiled into it. A pin manifest is no more trusted than the binary that
verifies it.

## What is not trusted

- **Fetched web content.** A page that web search loads can carry a prompt
  injection.
- **Model output.** A tool call a task proposes, a document a task writes and
  the markdown the chat UI renders all come from the model. A clean grounding
  audit means the auditor found none, and the auditor is the same model.
- **The contents of a sandbox.** The agent runs as root inside the task VM.
  Everything the guest writes is untrusted, symlinks in the workspace included.
- **Documents in a workspace.** A downloaded PDF or a cloned repository is input
  the operator did not write.
- **Pulled images.** A pin proves which bytes run. It does not prove the bytes
  are safe.
- **A launched coding agent's own network behaviour.** Crush, Claude Code and
  Qwen Code run on the host and decide what they contact.
- **Other local accounts and web pages in the operator's browser.** Both can
  reach a loopback port.

## Boundaries and controls

| Boundary | Control | Decided in | Held by |
|---|---|---|---|
| Listeners bind loopback only (PRIV-01) | The dashboard refuses a non-loopback address. A rendered unit publishes on `127.0.0.1` or publishes no port. `villa status` reports `LoopbackOnly` from the installed units. | No ADR. PRIV-01 is the requirement id. | `TestNewServerRefusesNonLoopback`, `TestRunDashboardRefusesNonLoopbackBind`, `TestEndpointsNeverWidenBind`, `TestRenderedPublishLoopbackOnly`, `TestRenderOpenWebUILoopbackOnly`; `allLoopback` in `internal/status/status.go` |
| Dashboard API callers | A Host allowlist refuses a DNS-rebinding page. A state-changing request needs a JSON content type and a same-origin signal. Each accepted connection is checked against the owning UID through `/proc/net/tcp`, and a peer that cannot be resolved is refused. | No ADR. GHSA-3r95, GHSA-6mq8. | `internal/dashboard/middleware.go`, `internal/dashboard/peercred.go`; `TestSameOriginGuardRejectsSpoofedHost`, `TestSameOriginGuardRejectsCrossOrigin`, `TestPeerUIDListenerAccept` |
| Inference key | llama-server is started with an api key. villa generates the key once and hands it to units in a 0600 `EnvironmentFile=`, never on an `Exec=` line or a podman command line. `/health` and `/v1/models` stay public. One exception is listed under known gaps. | [ADR-0011], [ADR-0014] | `TestWriteInferenceSecretEnvRoundTrip`, `TestInferenceSecretEnvFilePathContract`, `TestContainerArgsSecretEnvFile` |
| Inference client gate | One client owns the unit's address, the key and every route villa calls. A read of `InferenceSecret`, an address accessor or a route literal anywhere else fails the build unless the gate lists the file with a reason. An in-network curl probe receives the key on stdin, not in argv. | [ADR-0014] | `TestInferenceReachedOnlyThroughClient` in `cmd/villa/inference_apikey_gate_test.go` |
| Networks | `villa.network` routes off-box. `villa-closed.network` and `villa-sandbox.network` are `Internal=true`. The eight services with no runtime egress (the seven, and `villa-tools-recall`), `villa-llama` and each resident slot join the closed network and nothing else. `villa-inferproxy` joins the closed and sandbox networks. Open WebUI joins `villa.network` and the closed network. Known gap 4 says what is unproven. | [ADR-0036], [ADR-0043], [ADR-0011] | `TestClosedServicesJoinOnlyTheClosedNetwork`, `TestInferenceUnitsJoinOnlyTheClosedNetwork`, `TestInferproxyBridgesTheClosedAndSandboxNetworks`, `TestInferenceUnitsCarryNoFetchFlag`, `TestRoutedUnitsKeepTheirNetworks`, `TestRenderToolUnitsNetworks`, `TestRenderClosedNetworkGolden` |
| Doctor's `networks` finding | `villa doctor` compares each running container's networks and each network's `Internal` flag with the rendered units. A mismatch is a BLOCK FAIL, and a read that fails is a WARN. It runs on command, not continuously. | [ADR-0036] | `networks` in `internal/doctor/decide.go`; `internal/doctor/networks_test.go` |
| Tool server | Two fixed units serve read-only tools to Open WebUI, one per privilege: `villa-tools-web` (routed) searches the web and `villa-tools-recall` (closed) searches the caller's own transcripts. No unit joins both networks, none publishes a port, and each requires its own bearer, compared in constant time, on every route but `/healthz`. No tool takes a URL, every route is a `GET`, and every returned text is guarded, fenced and flagged. Recall is scoped by `X-Villa-User-Id`, which Open WebUI fills from the session through the connection's header template, to the file ids in a scope file villa writes from `recall-state.json`; a call with no identity is a 403 and a missing scope a 503. The bearers sit in 0600 env files, never a 0644 unit, and the registration is the rendered environment, with no admin-API write. | [ADR-0042] | `TestToolBearerRequiredOnEveryRouteButHealthz`, `TestToolServerServesOnlyGET`, `TestWebSearchTakesNoURL`, `TestWebSearchFetchesThroughTheSSRFGuard`, `TestRecallRefusesWithoutIdentity`, `TestRecallQueryIsScopedToTheCallersFiles`, `TestRecallDropsHitsOutsideTheCallersFiles`, `TestEveryResultIsFencedAndFlagged`, `TestRateCap` in `internal/toolserver/`; `TestToolSecretNeverInAUnit`, `TestToolUnitsPublishNoPort`, `TestToolsOpenWebUIEnvIsValidJSON`, `TestToolRecallConnectionCarriesIdentityHeader`, `TestToolWebConnectionCarriesNoIdentityHeader`, `TestToolServerGateOffRemovesUnits` |
| `villa-inferproxy` | A sandbox reaches inference only through the proxy. The proxy forwards `POST /v1/chat/completions` and `GET /v1/models`, caps the request body, injects the key, and answers every other route with 403. `villa-llama` does not join the sandbox network. | [ADR-0011] | `TestRunInferproxyRefusesDisallowedRoute`, `TestInferproxyCapsRequestBody`, `TestRenderInferproxyJoinsBothNetworks`; doctor `SBX-02` in `TestSandboxNetworkFindingMatrix` |
| Sandbox | One libkrun microVM per task: a read-only root, one workspace mounted read-write, the sandbox network only, no GPU device and no models volume. | No ADR. [Workspace-agent spec] §3.4. | `TestRenderSandboxRunArgs`, `TestSandboxRunMountsNoDeviceAndNoModels`; preflight `PRE-09` in `internal/preflight/checks_sandbox.go` |
| Workspace | `villa workspace add` refuses the home directory, a path outside it, villa's config and data roots, a nested workspace, a sensitive directory and a folder that contains the running `villa` binary. | No ADR. [Workspace-agent spec] §3.1. GHSA-3q4q, GHSA-45r5. | `internal/workspace/workspace.go`; `TestRegisterRefusals`, `TestRegisterRefusesSensitiveDirectories`, `TestRegisterRefusesContainingTheRunningExecutable` |
| Approval | A pure Action × Mode table answers each tool call of a task. An unknown action asks. Deletion asks in every mode. The deletion rule is a command pattern, not a proof. | No ADR. [Workspace-agent spec] §3.3. GHSA-fpjc, GHSA-mmp6. | `internal/approval/approval.go`; `TestDecideTable`, `TestDecideUnknownActionAsks`, `TestDecideAutoModeNeverBypassesDeletion`, `TestDeletionAsksInAutoMode`, `TestSessionAllowNeverAutoGrantsADeletion` |
| Web guard | `villa-websafe` sanitizes, normalizes, classifies and fences fetched content. Its fetch refuses loopback, private and link-local addresses at connect time and again on each redirect. The guard reduces and flags. It never claims safe: a detection annotates the result, and the content still reaches the model. The tool server runs it on each search snippet and fetched page (sanitize, normalize, classify, fence) and, without the sanitize step, on each recall passage. | [ADR-0002], [ADR-0042] | `TestGuardSeamOrder`, `TestClassifyFlagNotBlock`, `TestSSRFRejectSet`, `TestRedirectRevalidation`, `TestNoInjectionSafeCopy` in `internal/websafe/` |
| Pins and the pin manifest | An image pin is a digest and a release-asset pin is a checksum. `pins.Table()` is the allowlist. A pin manifest is ed25519-signed and may change values only. villa refuses a bad signature, an unknown component, a redirected registry and a serial below the floor, and treats an expired manifest as absent. The signing key is kept offline. | No ADR. [Update spec] §4, [RELEASING.md]. | `TestEveryDigestPinnedImageInTheTreeIsInTheTable`, `TestLookupIsTheAllowlist`, `internal/manifestverify/verify_test.go`. The offline key is a procedure in [RELEASING.md]; no enforcing test found. |
| Launched-agent environment | `villa code` appends a fixed set of telemetry and update switches for each agent. The environment is the control, because a project settings file can loosen a settings-only opt-out. The tests hold the variables. What an agent then sends is measured, by one network capture per vetted version. A different installed version is a WARN and the agent still launches. | [ADR-0039] | `lockdownEnv`, `claudeEnv` and `qwenEnv` in `internal/agent/`; `TestCodeLockdownEnv`, `TestRunClaudeReadyToLaunch`, `TestRunQwenReadyToLaunch`, `TestUnvettedVersionWarnsAndStillLaunches`; captures in `docs/prototype/qwen-code/RESULTS.md` |
| Seam grep gate | Image literals, device-access flags and backend markers stay in `internal/inference`, `internal/detect/gpu_amd.go` and the `internal/orchestrate` files the gate allowlists. The pin test above scans that file list. | No ADR decides it. | `TestSeamGrepGate` in `internal/inference/seam_test.go` |

Open WebUI, SearXNG, `villa-websafe` and `villa-tools-web` stay on the routed
network because they fetch. `villa verify search` (`cmd/villa/verify_search.go`) is the on-command
proof that bounds their outbound. It is a measurement, not a standing control.
`villa verify tools` (`cmd/villa/verify_tools.go`) is the same kind of proof for the tool
server: its contract, its refusals, the placement of each unit on its one network with a
positive-controlled probe that the recall unit has no route off-box, and one real call.

## Known gaps and accepted residuals

1. **Crush checks for a release at every start** ([#389]). The pinned Crush
   contacts `api.github.com` on each launch, and v0.101.0 still has no switch for it.
   The request carries the client IP and the Crush version. That is inferred
   from the binary's symbol and the traffic shape, because the leg is TLS and
   was not decoded. The opt-in `villa code --offline` points the proxy variables
   at a closed port and exempts only the model server (`internal/agent/offline.go`,
   `TestOfflineContractEveryAgent`). Measured on 2026-11-14 with the pinned
   Crush: the release check was sent to the proxy and the model turn completed.
   That is a routing check for Crush, not a packet capture of all three agents,
   which [#389] still tracks. The default launch is unchanged.
2. **A host-launched agent has a route off-box** ([#377]). Crush, Claude Code
   and Qwen Code run on the host with the operator's network. The launch
   environment and the capture are evidence for the captured version only. An
   agent's own web tools are outbound by design. villa disables Crush's in
   `crush.json` (`TestRenderRestrictiveTools`) and not the other two agents'.
   A launch with no route is a design question, not code.
3. **ROCm units run with `seccomp=unconfined`** ([#343]). ROCm is the default
   backend and its flag is in `internal/inference/backend_rocm.go`. Nobody has
   measured whether it can go. The Vulkan chat unit and `villa-image` no longer
   carry it (`vulkanGPUAccess`, `internal/inference/backend_vulkan.go`).
   Measured on the dev host on 2026-11-14: the real Vulkan chat unit served the
   live model with its projector and n-gram speculation without the flag, and a
   throwaway container on the pinned image-server image generated two images
   without it. `villa-image` as an installed unit was not run.
4. **The inference port is published on loopback** ([ADR-0043]). `villa-llama` and
   each resident slot join `villa-closed.network` only, and `villa-inferproxy` joins
   it and the sandbox network, so none has a route off-box
   (`TestInferenceUnitsJoinOnlyTheClosedNetwork`; proven on the live stack on
   2026-11-14, [#364]). What remains is the publish itself: `127.0.0.1:8080` is
   reachable by any local process, and the inference key is the control there. The
   publish working from an internal-only network is measured on podman 5.8.7 and
   6.1.3, not guaranteed for every rootless network backend.
5. **Markdown-image exfiltration is not closed** ([ADR-0002]). The model can emit
   a markdown image whose URL carries data, and the operator's browser fetches
   it, outside every container. `TestMarkdownImageResidualDocumented` keeps the
   note in `internal/websafe/doc.go`. [#360] leaves it out of scope, and no issue
   tracks closing it. With the tool server on, the recall tool gives an injected
   model a new way to read private text before it writes the image.
6. **Deletion approval is a pattern, not a proof** ([Workspace-agent spec] §3.3,
   §12). The rule matches `rm`, `rmdir`, `unlink`, `shred`, `trash`, `git clean`
   and `find -delete`. Under `--auto` a write is allowed and so is
   `python3 script.py` on a workspace path (`TestDecideExecuteAutoAllowlist`),
   so a script the task wrote can delete or overwrite files in the workspace
   without asking. The workspace bounds what it reaches.
7. **A workspace file can carry a prompt injection** ([#404]). A task reads
   workspace files, and the approval answer does not change after it has read
   one. The per-source taint decided on 2026-11-14 covers web content only, and
   it is not built. No task has a web route today. The first web source would
   be `villa research` ([#408]), also not built. The sandbox bounds the damage
   to one workspace.
8. **Root inside the task VM** ([Workspace-agent spec] §3.4, §12). krun ignores
   `--user`, so the guest runs as uid 0. The VM is the boundary.
9. **A resident set puts the inference key in a unit file** ([ADR-0011]). With
   resident models, the Open WebUI unit carries the key in an `Environment=`
   line (`OPENAI_API_KEYS`, `internal/orchestrate/openwebui.go`), and unit files
   are written at 0644 (`unitFileMode`, `internal/orchestrate/reconcile.go`).
10. **A tool call has no approval** ([ADR-0042]). Villa cannot make Open WebUI ask
    before a call. A prompt-injected model can call either tool as often as the caps
    allow: four at once, a burst of thirty, then one every two seconds. The pinned
    Open WebUI has a per-call approval mode that a user chooses in a chat, and villa
    does not render or force it.
11. **A web search query leaves the box, and the model writes it** ([ADR-0042]).
    SearXNG sends the query to the engines in `searxngEngines`. An injected model can
    put up to 512 runes of chat or recall text into each one, and nothing flags it.
    It reaches the search engines, not a host the attacker picks.
12. **Web and recall tools can be on in the same chat** ([ADR-0042]). Villa cannot
    stop it, and the pairing already exists without the tool server: the recall
    collection is attached to the served model, and native web search can be on in
    the same chat.
13. **The recall unit can read every vector in Qdrant** ([ADR-0042]). Qdrant has no
    API key, so the per-user scope is the unit's code, and every closed-network peer
    already has the same reach. The identity header and Qdrant's layout are Open
    WebUI's, read at the pinned image and not observed on a running container, so a
    pin bump can change either. `villa update chat` does not prove the tool server.
    Each failure constructed is closed: a 403, an empty result or a 502.
14. **Native recall stays shared** ([ADR-0042]). On a box with two users that
    acknowledged shared recall, the tool is per user and the attached collection is
    not. The tool is narrower than the path it sits beside and does not narrow it.

[ADR-0002]: adr/0002-the-web-guard-flags-injection-it-never-blocks.md
[ADR-0011]: adr/0011-the-sandbox-reaches-inference-through-a-v1-proxy.md
[ADR-0014]: adr/0014-one-authenticated-inference-client.md
[ADR-0036]: adr/0036-services-with-no-runtime-egress-join-a-closed-network.md
[ADR-0039]: adr/0039-a-launched-coding-agent-runs-with-its-telemetry-off-and-a-capture-to-show-it.md
[ADR-0042]: adr/0042-the-tool-server-is-two-fixed-units-one-per-privilege.md
[ADR-0043]: adr/0043-inference-joins-the-closed-network.md
[Workspace-agent spec]: spec/v1.11-workspace-agent.md
[Update spec]: spec/v1.8-villa-update.md
[RELEASING.md]: RELEASING.md
[#343]: https://github.com/MatrixMagician/VillaStraylight/issues/343
[#349]: https://github.com/MatrixMagician/VillaStraylight/issues/349
[#360]: https://github.com/MatrixMagician/VillaStraylight/issues/360
[#364]: https://github.com/MatrixMagician/VillaStraylight/issues/364
[#377]: https://github.com/MatrixMagician/VillaStraylight/issues/377
[#389]: https://github.com/MatrixMagician/VillaStraylight/issues/389
[#404]: https://github.com/MatrixMagician/VillaStraylight/issues/404
[#408]: https://github.com/MatrixMagician/VillaStraylight/issues/408
