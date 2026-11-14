# Security review: villa's own tool server

Draft for the maintainer's sign-off (#360). It reviews the design in
[ADR-0042](../adr/0042-the-tool-server-is-two-fixed-units-one-per-privilege.md). None of
the design is built. The sign-off is the maintainer's, and an agent drafted this page.

How to read the control tables:

- **Exists** means the file and the test were opened on 2026-11-14 at commit `08a4edc`.
- **To add** names the issue that adds the control and the test proposed to hold it.
- **Read at the pin** means Open WebUI source read out of the pinned image
  (`sha256:1a6399d2…8dc8b924`, revision `0a7c1583`, version 0.11.3), with paths under
  `backend/open_webui/`. It was not observed on a running container.
- **Measured** names what was run and on what.

## What is reviewed

Two units run the villa binary in the distroless base image.

1. `villa-tools-web` joins `villa.network`. Open WebUI calls `GET /web/search`. The unit
   sends the query to SearXNG, fetches the top results through the SSRF-guarded loader
   and returns guarded, fenced text.
2. `villa-tools-recall` joins `villa-closed.network`. Open WebUI calls
   `GET /recall/search` with the session user's id in `X-Villa-User-Id`. The unit embeds
   the query on `villa-embed`, searches Qdrant for that user's transcripts and returns
   fenced passages.
3. The host writes a recall scope file from `recall-state.json`. The recall unit mounts
   its directory read-only.

What is at stake: each user's chat transcripts in the recall index, the two bearers, the
text in a chat that a model can copy into a tool argument, the closed network's property
that its services have no route off-box, and the host's compute and bandwidth.

## Assumptions

- The operator is trusted, and a process that runs as the operator is the operator
  (`docs/THREAT_MODEL.md`).
- The model is not trusted. Once any untrusted text is in a chat, every tool argument
  the model writes may be an attacker's.
- Open WebUI is trusted to authenticate its users and to fill the identity header from
  the session. It is a pinned third-party image. A pin proves which bytes run, not that
  they are correct.
- An Open WebUI admin can already read every chat. Villa's own service account is an
  admin whose password is a constant in `cmd/villa/openwebui_live.go`, and the chat port
  is reachable from anything that can reach loopback. `docs/MEMORY.md` documents the
  account. `docs/THREAT_MODEL.md` does not list it. This review does not change it. It
  bounds what "user A cannot read user B" can mean on this stack. The tool does not
  return B's transcripts to a call made as A, and B's chats are still not private from
  an admin.

## Boundary 1: model output to tool arguments

The model chooses `q` and `count`. A prompt injection in a fetched page, a recall
passage or a pasted document can choose them instead.

| Threat | Control | Status and holder |
|---|---|---|
| The model names a URL to fetch, to reach an internal service or to carry chat text to an attacker's host | No tool takes a URL. The only parameters are `q` and `count`, and an unknown parameter is a 400 | To add, #361: `TestWebSearchTakesNoURL`, `TestOpenAPIGolden` |
| The model sets the identity to read another user's recall | The user id is a header Open WebUI fills, not a parameter. `execute_tool_server` maps the model's arguments to path and query parameters only | Read at the pin: `utils/tools.py:1622-1774`. To add, #361: `TestOpenAPIGolden` shows no identity parameter |
| The model steers SearXNG with extra parameters (engines, language, format) | The unit builds the SearXNG request itself. `q` is URL-encoded into one parameter, and `format=json` is fixed | To add, #361: `TestWebSearchSendsOnlyTheQueryToSearXNG` |
| The model calls a write or admin route | Every route is a `GET` and the mux registers nothing else. The Qdrant client has one call, a query | To add, #361: `TestToolServerServesOnlyGET`, `TestRecallIssuesOnlyAQuery` |
| The model calls a tool in a loop | The rate and concurrency caps. There is no approval | To add, #361: `TestRateCap`, `TestConcurrencyCap`. Residual 1 |
| The model puts chat or recall text into `q` | None. The query is cut to 512 runes and the rate cap bounds the volume | Residual 2 |

## Boundary 2: Open WebUI to the tool server

Open WebUI is the only intended caller. Other containers share each network.
`villa.network` holds `villa-llama`, SearXNG, `villa-websafe` and, with the workspace
agent on, `villa-inferproxy`. `villa-closed.network` holds Qdrant, the embedder and the
other closed services.

| Threat | Control | Status and holder |
|---|---|---|
| Another container on the network calls a tool | A bearer on every route but `/healthz`, compared in constant time, as `Server.authOK` does in `internal/websafe/loader.go` | To add, #361: `TestToolBearerRequiredOnEveryRouteButHealthz` |
| The unit starts with no key and accepts anyone | The serve refuses an empty key, and the core has no accept-any path. `runWebsafe` is the precedent (`TestRunWebsafeRefusesEmptyBearer`, exists) | To add, #361: `TestToolServeRefusesEmptyBearer` |
| A key leaks from a unit file | No key is in a rendered unit. Each reaches its container through a 0600 `EnvironmentFile=`, written by the websafe writer (`TestWriteSearxngFilesMode` holds the mode, exists) | To add, #362: `TestToolSecretNeverInAUnit`, `TestWriteToolEnvRoundTrip` |
| One leaked key opens both units | Two keys. The web unit, which parses untrusted pages, never holds the recall key | To add, #362: `TestWriteToolEnvRoundTrip` asserts each file's one key |
| A key is read back out of Open WebUI | None. An admin reads the stored connections through `GET /api/v1/configs/tool_servers` | Read at the pin: `routers/configs.py:233-235`. Residual 6 |
| A host port exposes a unit | The units publish no port | To add, #362: `TestToolUnitsPublishNoPort`. The existing `TestRenderedPublishLoopbackOnly` checks `villa-llama` only. Exists at run time: `villa status` reports `LoopbackOnly` from the installed units (`allLoopback`, `internal/status/status.go`) |
| A slow client holds connections open | `ReadHeaderTimeout`, `IdleTimeout`, a 16 KiB header cap and four requests in flight | Exists for the shared server shape: `TestWebsafeServeSetsReadDeadlines`. To add, #361: `TestRequestCaps` |
| A refusal leaks input or an upstream body to the model | Refusal bodies are fixed strings. Open WebUI hands an error body to the model | Read at the pin: `utils/tools.py:1753-1755`. To add, #361: `TestRefusalsDoNotEchoInput` |
| The connection line is malformed and Open WebUI drops it without an error | Open WebUI reads a bad `TOOL_SERVER_CONNECTIONS` as an empty list. The proof reads the registered list back | Read at the pin: `config.py:379-383`. To add, #362: `TestToolsOpenWebUIEnvIsValidJSON`. To add, #363: `villa verify tools` leg 3. Measured: podman 6.1.3 passes a one-line JSON value through `--env-file` unchanged |

## Boundary 3: the web unit to SearXNG and the web

| Threat | Control | Status and holder |
|---|---|---|
| A search result points at an internal address (SSRF) | Result URLs are fetched only through `websafe.SafeClient`. It refuses loopback, private and link-local addresses at connect time, re-checks each redirect, and refuses `villa-` hostnames by name | Exists: `internal/websafe/ssrf.go`; `TestSSRFRejectSet`, `TestHostRejected`, `TestControlConnectTime`, `TestRedirectRevalidation`. To add, #361: `TestWebSearchFetchesThroughTheSSRFGuard` |
| DNS rebinding moves a public name to an internal address | The connect-time hook checks the address actually dialed | Exists: `control` in `internal/websafe/ssrf.go`; `TestControlConnectTime` |
| The SearXNG client becomes a second, unguarded fetcher | Its target is a compiled-in address. No request field from the model or from a result reaches its URL except `q` | To add, #361: `TestWebSearchSendsOnlyTheQueryToSearXNG` |
| A huge or slow page exhausts the unit | `websafe.DefaultBounds`: 2 MiB per body, 10 s per fetch, four fetches in flight, five redirects. The SearXNG body is read to 1 MiB | Exists: `TestFetchBounds`, `TestDefaultBoundsConservative`. To add, #361: `TestResponseCaps` |
| A hostile page crashes the parser | The existing loader path | Exists: `TestExtractTitleLengthPreserving`, `TestSkipAndContinue` |
| The unit is compromised through a page it parses | It holds one key, mounts the binary read-only and joins one network. It reaches what `villa-websafe` already reaches | Residual 9 |

## Boundary 4: the recall unit to the embedder and Qdrant

| Threat | Control | Status and holder |
|---|---|---|
| The recall unit reaches the internet | It joins `villa-closed.network` and nothing else | To add, #362: the unit joins `closedServiceUnits`, so `TestClosedServicesJoinOnlyTheClosedNetwork` (exists) covers it. Exists on the host: doctor's `networks` finding (`internal/doctor/decide.go`, `TestNetworksFailWhenAClosedServiceStillRoutes`). To add, #363: `villa verify tools` leg 6 |
| A tool unit bridges the closed network to a routed one | No unit joins two networks | To add, #362: `TestRenderToolUnitsNetworks` |
| The unit writes to or deletes from Qdrant | The client has one call, a query. Qdrant itself has no key, so the network does not enforce it | To add, #361: `TestRecallIssuesOnlyAQuery`. Residual 7 |
| A pin bump changes Open WebUI's Qdrant layout | The post-filter is villa's. A changed layout returns nothing or a 502 | To add, #361: `TestRecallDropsHitsOutsideTheCallersFiles`. Residual 8 |
| An embedding request starves the chat model | The caps. The embedder is the one already serving Open WebUI | To add, #361: `TestRateCap`. Residual 10 |

## Boundary 5: user to user

| Threat | Control | Status and holder |
|---|---|---|
| User A's call returns user B's transcripts | The Qdrant filter names the collection and only the caller's file ids. The unit then drops any hit whose file id is not the caller's | To add, #361: `TestRecallQueryIsScopedToTheCallersFiles`, `TestRecallDropsHitsOutsideTheCallersFiles` |
| A call arrives with no identity | 403, before any embed or query | To add, #361: `TestRecallRefusesWithoutIdentity` |
| The identity is malformed or repeated | 403. The id is used as a map key and is never placed in a query string | To add, #361: `TestRecallRefusesMalformedIdentity` |
| The scope is missing and the unit falls back to the whole collection | 503. There is no unscoped query | To add, #361: `TestRecallRefusesWithoutScope` |
| A user spoofs the header | Open WebUI builds the header from the authenticated request. A browser cannot reach the unit, which publishes no port, and a caller needs the bearer | Read at the pin: `utils/tools.py:127-186`, `utils/headers.py:105-154`. Not observed. First step of #361: the capture the ADR names |
| A peer on the closed network asserts an identity | It needs the recall bearer | To add, #361: `TestToolBearerRequiredOnEveryRouteButHealthz` |
| A user plants a file that poses as a transcript | The filter and the post-filter take file ids from villa's scope file. A file that villa's index run did not upload is in no user's set, in any collection | To add, #361: `TestRecallQueryIsScopedToTheCallersFiles`, `TestRecallDropsHitsOutsideTheCallersFiles` |
| The identity reaches backends that do not need it | The header is a template on the recall connection. The global forward flag is not set | To add, #362: `TestToolRecallConnectionCarriesIdentityHeader`, `TestToolWebConnectionCarriesNoIdentityHeader` |
| A non-admin account uses the tools | The connections carry no access grants, so they are private to admins | Read at the pin: `utils/access_control/__init__.py:148-176` |

On a box with one user this boundary has nobody on the other side. The existing
shared-recall refusal still guards the index run (`recall.PrepareIndexRun`,
`TestPrepareIndexRun`, exists).

## Boundary 6: a tool result back into the chat

| Threat | Control | Status and holder |
|---|---|---|
| A fetched page carries an injection | Sanitize, normalize, classify, fence, in that order. A detection annotates the result and the text is still returned | Exists for the loader: `TestGuardSeamOrder`, `TestClassifyFlagNotBlock`, `TestFetchGuardVerdict`. To add, #361: `TestEveryResultIsFencedAndFlagged`, `TestGuardKeepsTheLoadBytes` |
| A search snippet or a title carries one | The same steps for a snippet. A title is sanitized, normalized and classified, and its verdict folds into the result's | Exists for titles: `TestTitleInjectionFlagged`. To add, #361: `TestEveryResultIsFencedAndFlagged` |
| A recall passage carries one, stored from an earlier chat | Normalize, classify, fence with the recall label | To add, #361: `TestEveryResultIsFencedAndFlagged` |
| Content forges the closing delimiter | A per-result nonce from `crypto/rand`. A fence that cannot get one fails closed | Exists: `TestFenceNonced`, `TestFenceNonceUnique` |
| The cut breaks a fence | The text is cut before the fence is applied | To add, #361: `TestResponseCaps` |
| Product copy or a tool description claims the text is safe | The honesty ban is extended to the new package | Exists for `internal/websafe`: `TestNoInjectionSafeCopy`. To add, #361: `TestToolServerCopyIsHonest` |
| A tool description is itself an injection | The descriptions are constants in the binary, frozen by the golden. Open WebUI fetches the document with the bearer | Read at the pin: `utils/tools.py:1553-1556`. To add, #361: `TestOpenAPIGolden` |
| The injected model exfiltrates through a markdown image | None | Residual 4 |

## Boundary 7: the host to the recall unit

| Threat | Control | Status and holder |
|---|---|---|
| The mount exposes more than the scope | The unit mounts one directory that holds one file of ids. It does not mount the data root, which also holds task records and models | To add, #362: `TestRenderToolUnitsNetworks` asserts the one extra mount |
| The scope file leaks to another local account | Directory 0700, file 0600, ids only | To add, #362: `TestRecallIndexWritesTheScope` asserts the mode and the content |
| The scope lags behind the index | It is rewritten at each state save and at each stack apply. An owner never changes, so a lag cannot cross users | Residual 11 |
| The unit writes the scope | The mount is read-only | To add, #362: the golden shows `:ro,z` |

## Lifecycle

| Threat | Control | Status and holder |
|---|---|---|
| A gate turned off leaves a unit running | The registry is the removal set (ADR-0035) | Exists: `orchestrate.Orphans`, `TestSubsystemUnitsMatchTheRenderedUnits`. To add, #362: `TestToolServerGateOffRemovesUnits` |
| An off config changes | The off render and the off config file are byte-identical | To add, #362: `TestRenderToolServerOffIsByteIdentical` |
| The gate is read in two places and they disagree | One predicate per unit, in `internal/subsystem` | Exists: `TestModuleIsLoadBearing`. To add, #362: the flag joins its list |
| An image or address literal leaks into the core | The seam gate walks `internal/` and `cmd/villa` | Exists: `TestSeamGrepGate` |
| A missing env file stops the chat | The stack apply rewrites the files before it writes units | To add, #362: `TestHealGeneratesToolSecrets` |

## Accepted residuals

Stated plainly, for the maintainer to accept or refuse.

1. **There is no approval.** A prompt-injected model can call either tool as often as
   the caps allow: four at once, a burst of thirty, then one every two seconds. The pin
   has a per-call approval mode that a user may choose in a chat. Villa does not render
   it and cannot force it.
2. **The search query leaves the box, and the model writes it.** SearXNG sends `q` to
   the engines in `searxngEngines` (`internal/orchestrate/searxng.go`). An injected model
   can put up to 512 runes of chat or recall text into each query. Nothing flags it. The
   text reaches the search engines, not a host the attacker picks. The pages the unit
   then fetches see villa's request and not the chat.
3. **A tool result can carry an injection that the classifier does not flag.** The rule
   set has finite recall (ADR-0002). The fence reduces the risk and does not remove it.
4. **Markdown-image exfiltration is not closed** (`docs/THREAT_MODEL.md`, gap 5). The
   recall tool gives an injected model a new way to fetch private text before it writes
   the image.
5. **Web and recall tools can be on in the same chat.** Villa cannot stop it. The
   pairing already exists without the tool server. The recall collection is attached to
   the served model, and native web search can be on in the same chat.
6. **An Open WebUI admin can read both bearers** through the admin API, and the operator
   can read them with `podman inspect`. A bearer is usable only from inside
   `villa.network` or `villa-closed.network`.
7. **The recall unit can read every vector in Qdrant, and Qdrant would let it write.**
   Qdrant has no API key (`internal/orchestrate/openwebui.go` omits `QDRANT_API_KEY`).
   The per-user scope is the unit's code. Every closed-network peer already has the same
   reach.
8. **Identity and layout depend on Open WebUI.** A changed Open WebUI could assert any
   user id, and a pin bump can change the header path or the Qdrant layout.
   `villa update chat` does not prove the tool server. Each failure the review could
   construct is closed: a 403, an empty result or a 502.
9. **The web unit is one more routed container that parses untrusted input.** A
   compromise there reaches what `villa-websafe` already reaches on `villa.network`,
   including `villa-inferproxy`, which authenticates no inbound caller (ADR-0011).
10. **The tools use shared compute.** A web call makes up to five fetches. A recall call
    runs one embedding on the embedder that Open WebUI uses. The caps bound both, and
    there is no reservation row.
11. **The recall index can be stale.** A chat deleted in Open WebUI is still returned to
    its owner until the next `villa recall index`. This is the existing staleness.
12. **Native recall stays shared.** On a box with two users that acknowledged shared
    recall, the tool is per user and the attached collection is not.
13. **The units log no query.** The record of what a model searched for is the chat
    itself, in Open WebUI.

## What the sign-off covers

- The seven boundaries and their controls, with the "to add" tests as the acceptance
  list for #361 and #362.
- The thirteen residuals.
- The ADR's contestable decisions, which change this review if they change: no URL
  parameter, recall through Qdrant with a scope file, the header template, and
  admin-only connections.
- One blocking check before recall ships: the identity capture that the ADR names as the
  first step of #361.

## What `docs/THREAT_MODEL.md` owes when #362 lands

No statement on that page is wrong while nothing is built, so this change leaves it
alone. The PR that renders the units changes these lines.

- The Networks row: eight closed services, and `villa-tools-web` among the routed units
  that fetch.
- A new row for the tool server: the two bearers, the identity header, the `GET`-only
  routes and the tests above.
- The Web guard row: the guard also runs on search snippets and recall passages.
- Known gaps: residuals 1, 2, 5, 7 and 12, and gap 5's new reach through recall.
