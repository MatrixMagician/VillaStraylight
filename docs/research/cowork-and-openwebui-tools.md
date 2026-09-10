# Claude Cowork's capability surface, and Open WebUI's tools contract

Two questions for one design: what Anthropic's Cowork actually does (so a local equivalent targets the real product, not the marketing), and precisely how Open WebUI lets a model call tools against an OpenAI-compatible backend (so villa, a Go control plane, knows where its integration seam is). Every claim traces to Anthropic's help center, Open WebUI's docs or source, or llama.cpp's server docs. No secondary write-ups were used; where a primary source is thin, that is said.

Verified on 2026-09-10. Web pages were fetched with `pullmd`; the one exception is the bullet lists in Anthropic's "Get started" article, which both of pullmd's extractors dropped, so those were re-read with `WebFetch` (noted inline as [A1†]). Open WebUI source was read from a shallow clone at commit `0a7c15832fb30b1903753e83f81dc7d27e5b0944` (package.json `0.11.3`, 2026-09-04) and, for the pinned revision, from `02dc3e689ceac915a870b373318b99c029ddf603` (package.json `0.9.6`, 2026-06-02) via raw.githubusercontent. llama.cpp docs were read at master `72797e89198ab564fd0e6baa54ab196e8dd1d884` (2026-09-10). Villa refs are at `9bf72d057c18af507d3349b6567358d41c54f104`.

> **Placement note.** As with `update-version-checks.md`, the old `.planning/` tree is gone, so this lives in `docs/research/`. It is input to a design interview, not an ADR.

## Summary for the tickets waiting on this

1. **Cowork is Claude Code's agent loop with a permission layer, a folder grant, and a VM for code.** Files are reachable only in folders the user connected; shell/code runs in a hypervisor VM (local) or an Anthropic sandbox (cloud); deletion always needs explicit approval. That three-part shape (gated file tools, isolated code, per-action approval) is the thing to copy. [A2][A3]
2. **Open WebUI's only supported tool-calling mode is Native: it sends the OpenAI `tools` array and accumulates streamed `tool_calls` deltas.** The prompt-based mode is renamed Legacy and unsupported. [O1] `middleware.py:3030-3036`, `:5026-5051`
3. **Villa's pinned Open WebUI (`sha256:7f1b0a1a…`, built 2026-06-02, v0.9.6) is 345 commits BEFORE v0.10.0, where Native became the default.** At the pin, an unset `function_calling` means the prompt-based mode (`!= 'native'` tests). Any tools work either bumps the pin (a re-vet, per `docs/RELEASING.md`) or sets the per-model param explicitly. See "The pin gap".
4. **The Go seam is an OpenAPI tool server.** Open WebUI ingests any OpenAPI 3.x spec, names tools by `operationId`, and registers servers globally from the `TOOL_SERVER_CONNECTIONS` env JSON with `auth_type` `bearer`/`session`/`system_oauth`/`none`. This exists at villa's pin. Limits: no interactive confirmation events and no streamed output from external tools. [O4][O5][O8]
5. **Villa's `ENABLE_PERSISTENT_CONFIG=False` already makes env authoritative every boot**, which is exactly what provisioning `TOOL_SERVER_CONNECTIONS` from `config.toml` needs. The one thing NOT reachable from env is the per-model `function_calling` param (a model record in the DB). [O8] `openwebui.go:34-41`
6. **Open WebUI's own "computer substrate" is Open Terminal**: a separate container the model drives through `run_command`-style tools, registered via `TERMINAL_SERVER_CONNECTIONS` (present at the pin). Pyodide/Jupyter code execution is now documented as legacy. [O10][O9]
7. **llama-server needs `--jinja` for `tools` on both `/v1/chat/completions` and `/v1/messages`**; Qwen 2.5 templates are handled natively (Hermes 2 Pro format), unknown templates fall back to a generic handler. This is why coding mode gates `villa code`; a Cowork-style chat needs the same flag on the chat model. [L1][L2]

## Question 1: what Cowork is

### Definition and platforms

Cowork "uses the same agentic architecture that powers Claude Code, with no terminal required"; the user describes an outcome and Claude "can take on complex, multi-step tasks and execute them". Paid plans only. Surfaces: Claude Desktop (macOS, Windows, and a Linux beta named in [A8] and [A10]), web, mobile, and the Chrome side panel. [A1][A5]

Sessions run in the cloud by default (beta); "local execution remains available for existing desktop deployments". Anything on the user's machine (folders, browser, screen) is reached through the desktop app over an Anthropic-brokered connection, and only while it is online. [A2][A5]

> Villa-equivalent: strictly-local by constraint, so villa is the "local session" architecture only. The cloud/desktop split does not apply, but the *broker* idea does: the chat UI never touches the host directly; a villa-owned process does.

### File access

- "On desktop, Claude can read from and write to your local files" in folders the user selects; "Local file access is limited to folders the member has connected", and "each local tool call is checked against the member's permissions before it runs". [A1†][A2]
- "Deletion protection: Cowork requires your explicit permission before permanently deleting any files", in every mode. [A3]
- Folder instructions: a per-folder instruction file Claude "can also update on its own during a session". Projects add files, links, instructions and project-scoped memory. [A1][A9]
- Understood file types include docx/xlsx/pptx/pdf/csv/md/json/yaml/toml/ipynb and code. [A12]

> Villa-equivalent: nothing today grants a folder to the model. `villa code` gives Crush/Claude Code the cwd, which is broader and unguarded. A "connected folder" maps naturally to a `Volume=` on a villa-rendered Quadlet unit, so the grant is a config.toml field, not a UI click.

### Sandbox shape

Local sessions use two environments: "the agent loop runs natively on the device" (conversation, file reads/writes in connected folders, web fetches, local MCP servers) behind "an application-layer permission system", while "code execution runs in an isolated virtual machine": Apple Virtualization.framework on macOS, Hyper-V on Windows, with "its own network egress filtering, syscall restrictions, and per-session user isolation". If the VM cannot start, file and web tools keep working and shell/code reports "workspace unavailable". [A2]

Cloud sessions: a per-session sandbox, "no access to your network by default", egress through a mandatory proxy with an allowlist, short-lived credentials, connector calls made server-side. [A2]

Computer use has no sandbox at all: "Claude interacts directly with your desktop, apps, and browser", with per-app permission prompts and a blocklist. It is Pro/Max only and "isn't available in the Linux beta". [A7][A10]

> Villa-equivalent: villa's isolation primitive is a rootless Podman container, not a VM. That is weaker than Hyper-V but it is the same *topology*: agent loop outside, code inside, files through a gated mount. Villa already ships a distroless container that bind-mounts the villa binary (`internal/orchestrate/quadlet/websafe.container.tmpl`), which is the template for a sandboxed tool server.

### Task types

The help center's examples: file and document management (organise, process receipts, batch rename), research synthesis, document creation ("Excel files with working VLOOKUP", slide decks from notes, reports from voice memos), and data analysis (statistics, visualisation, transformation). Browser tasks run in a built-in browser in the desktop app side panel or via Claude in Chrome. [A1†][A8][A12]

> Villa-equivalent: document/spreadsheet generation is model-plus-libraries work that runs *inside* the sandbox; villa supplies the sandbox and the file grant. Browser use has no villa equivalent; `villa-websafe` is a fetch-and-sanitise loader, not a browser. Out of scope for a first cut.

### Extension: plugins, skills, connectors

A plugin "bundles skills, connectors, and sub-agents into a single package"; "hooks and sub-agents run only in Cowork". Connectors are MCP; "in Cowork, connectors reach external services through Anthropic's cloud", and local MCP servers "run on your computer with the same permissions as any other program you run". Marketplaces can be added from a GitHub repo or git URL. [A6]

> Villa-equivalent: none. Open WebUI has Skills (a system-prompt manifest the model can `view`), MCP HTTP connections, and OpenAPI tool servers [O1]; a villa "plugin" would be a catalog-style JSON that renders into `TOOL_SERVER_CONNECTIONS` entries. Not needed for the first cut.

### Background, parallel and scheduled execution

- "Sub-agent coordination: Claude breaks complex work into smaller tasks"; when a task starts, Claude "coordinates multiple workstreams in parallel if appropriate". [A1†]
- Scheduled tasks: `/schedule` or the Scheduled page; hourly/daily/weekly/weekdays/manual; each run "is its own Cowork session"; runs in the cloud and "can't be tied to a folder on your computer" (a local folder forces a local run). [A4]
- Dispatch: one persistent thread from mobile driving the desktop; "there's no way to start a new thread or manage multiple threads". [A10]

> Villa-equivalent: none. Open WebUI has Automations (cron/RRULE, each run a real chat through the normal pipeline, "always run with full access") and, post-pin, sub-agents via a `delegate_task` builtin tool [O12][O11]. On a single llama-server, "parallel" is bounded by `--parallel` slots and shares one context budget; that is a `recommend` question before it is a UI question.

### Permission model

Three modes, switchable mid-task: **Manual** ("pauses and asks for approval for actions"), **Auto** ("reviews each action for safety … and automatically blocks anything it determines to be unsafe"; read-only connector tools auto-approved, write/delete "Claude decides"), **Skip** (nothing checks). Auto "consumes more of your usage limit". Connector tools carry a per-tool "Always allow / Needs approval / Blocked" policy that intersects with the mode. Deletion always asks. Admins can require "fresh approval for every permission-gated tool call". [A1][A2][A11]

The safety article's framing is the useful design input: risk is the product of "what Claude can read and see" and "what Claude is allowed to do"; tools are classed as **read** vs **write**, and write tools get the extra gate. [A3]

> Villa-equivalent: `websafe` is the "what it reads" half (ADR-0002: it flags, never blocks). There is no "what it may do" half. The read/write tool split is the cheapest thing to adopt: a Go tool server can expose read tools ungated and make every write/delete tool require an approval token, or refuse. Open WebUI's own per-call Allow/Deny (`ENABLE_TOOL_PERMISSIONS`) is post-pin and experimental [O1].

### Stated limits

"No session sharing"; some artifact features desktop-only; usage "consumes more of your usage allocation than chatting"; computer use Pro/Max only and not on Linux; scheduled tasks cannot touch local folders. [A1†][A4][A7]

### Where Anthropic's docs are thin

There is no primary document describing the tool set inside the VM (which commands, which runtimes, disk layout), the wire protocol between agent loop and VM, or how "Claude decides" is implemented in Auto mode beyond "reviews each action for safety". The architecture article defers to a Trust Center PDF that is not publicly linked. Anything more specific than the above would be inference.

## Question 2: the Open WebUI tools contract

### (a) Function calling against an OpenAI-compatible backend

Two modes, set at **Model Settings → Advanced Params → Function Calling** (admin, per model) or per request via `params.function_calling`. "Native (Agentic) Mode is now the default. As of v0.10.0, every chat and model that has not explicitly chosen a tool-calling mode runs Native". Legacy (formerly "Default") "injects the tool definitions into the prompt and calls tools once before generating", is "unsupported", and gets no builtin tools. [O1][O5]

What the backend must support, from source at `0a7c1583`:

- Native: Open WebUI puts every resolved tool into the request as `form_data['tools'] = [{'type': 'function', 'function': spec}, …]` (`backend/open_webui/utils/middleware.py:3030-3036`). Legacy instead runs `chat_completion_tools_handler` (`:1280`), a separate completion against a task model with the `TOOLS_FUNCTION_CALLING_PROMPT_TEMPLATE` prompt that asks for a `{"tool_calls": [...]}` JSON object. [O8]
- Streaming: Native accumulates `delta.tool_calls` by `index`, synthesising an `id` if the backend omits one and coercing non-string `arguments` to JSON (`middleware.py:5026-5051`). So the backend must stream OpenAI-shaped `tool_calls` deltas; a backend that returns tool calls only in a non-streamed final message is not what the loop expects.
- Default resolution: `metadata.params.function_calling = form param or model param or 'native'` (`backend/open_webui/main.py:1268-1271`).

Model guidance from the docs: "Small local models (under ~30B parameters) often produce malformed JSON or fail multi-step tool chains even in Native Mode"; named local minimums include "Qwen 3.6 27B" and "Muse Glimmer 30B". [O1]

### The pin gap

Villa pins `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…` (`internal/orchestrate/openwebui.go:66`). Resolving that digest at ghcr.io gives an image created `2026-06-02T02:15:45Z` from revision `02dc3e68`, package.json `0.9.6`. GitHub's compare API reports that commit **345 behind `v0.10.0`** (published 2026-06-29). At the pin, `middleware.py:2474,2488,2566-2576` test `function_calling != 'native'`, so an unset param is the prompt-based mode. `TOOL_SERVER_CONNECTIONS` (`config.py:442`), `TERMINAL_SERVER_CONNECTIONS` (`:460`) and `ENABLE_AUTOMATIONS` (`:2918`) exist at the pin; `ENABLE_SUBAGENTS` and `ENABLE_TOOL_PERMISSIONS` do not.

### (b) The Python `Tools` contract

Documented in [O2]: a single `.py` file with a YAML-frontmatter docstring (`title`, `description`, `requirements`, `required_open_webui_version`, …) and a class named `Tools`. `__init__` conventionally sets `self.valves = self.Valves()`. `Valves` (admin) and `UserValves` (per user, delivered as `__user__["valves"]`) are nested Pydantic `BaseModel`s. Each method's docstring becomes the tool description; "only the reST `:param name:` form is read" for argument descriptions (Google-style `Args:` is ignored); type hints generate the JSON schema. Methods should be `async`.

Reserved arguments: `__event_emitter__`, `__event_call__`, `__user__`, `__metadata__`, `__messages__`, `__files__`, `__model__`, `__oauth_token__`. Both event helpers take one dict `{"type": …, "data": …}`; `__event_call__` awaits a browser answer. Event types: `status`, `message`/`chat:message:delta`, `replace`/`chat:message`, `files`, `embeds`, `source`/`citation`, `notification`, `confirmation` and `input` (need `__event_call__`), `execute`, `chat:title`, `chat:tags`. Under Native mode `message`/`replace` deltas "flicker/disappear"; return the final content instead. [O2][O3]

These run in-process via `exec()` on the Open WebUI server; the docs say granting tool creation "is equivalent to giving them shell access to the server". [O2][O13] `ENABLE_PLUGINS=False` hides and stops them but does not gate the create/update endpoints. [O8]

> Villa reading: irrelevant as an implementation target (Python, in-process, DB-stored), but the docstring→schema rule is the same contract an OpenAPI spec must satisfy: name, description, typed parameters with per-parameter descriptions.

### (c) The OpenAPI tool-server contract

The `openapi-servers` project positions OpenAPI 3.x as the protocol: "if you build REST APIs or use OpenAPI today, you're already set". Parser rules: only the eight HTTP method keys are operations; path-level `parameters` are honoured; path and query values are URL-encoded; the spec is fetched as JSON first, YAML on failure. [O4]

Source (`backend/open_webui/utils/tools.py:1071` `convert_openapi_to_tool_payload`): tool `name` is `operationId` (operations without one are skipped), `description` is `description` else `summary`, parameters are merged path-level + operation-level and schema'd from `parameters` and `requestBody`. Auth (`:1395-1409`): `bearer` sends `key` as `Authorization: Bearer`, `session` forwards the user's own Open WebUI token, `system_oauth` the user's OAuth access token, `none` sends nothing; every call also carries `X-User-Id`. The spec is read from `path` (default `/openapi.json`).

Registration: **User** tool servers (Settings → Integrations) are called *from the browser*, need the `USER_PERMISSIONS_FEATURES_DIRECT_TOOL_SERVERS` permission (default `False`), and are private to that user. **Global** tool servers (Admin → Integrations) are called *from the backend*, "treated similarly to Open WebUI's built-in tools", hidden per chat until toggled on, and gated by role permissions. Global is the one that can be provisioned: `TOOL_SERVER_CONNECTIONS` is a JSON array of `{type, url, spec_type, spec, path, auth_type, key, config:{enable}, info:{id,name,description}}` and a `ConfigVar`; the same list is `GET/POST /api/v1/configs/tool_servers` (admin) (`routers/configs.py:233-238`). [O5][O8]

Limits: "one-way events only" (status, notification, message, files, source) via `POST /api/v1/chats/{chat_id}/messages/{message_id}/event` (`routers/chats.py:1514`), and only when `ENABLE_FORWARD_USER_INFO_HEADERS=True` (default `False`, `env.py:976`) so the tool sees `X-OpenWebUI-Chat-Id`/`-Message-Id`; "interactive events (user input prompts, confirmations) are only available for native Python tools"; "no streaming output". Tool execution timeout is `AIOHTTP_CLIENT_TIMEOUT_TOOL_SERVER`. [O3][O4][O8]

Note the `localhost` trap: a global server's URL is resolved from the Open WebUI *container*, so on villa it must be the in-network name on `villa.network`, exactly as `OPENAI_API_BASE_URL` already is. [O5]

### (d) Functions and Pipelines

Functions are admin-only, DB-stored Python, type auto-detected from the class name: **Pipe** (registers as a model, owns the whole request), **Filter** (`inlet()` before the model, `stream()` per chunk, `outlet()` after), **Action** (a button on a message), **Event** (reacts to system events; new in 0.10.0). Valves as for tools. [O6] Provisioning from outside the UI exists only as API calls: `POST /api/v1/functions/create`, `/load/url`, `/sync`, `/id/{id}/toggle` (`routers/functions.py:106,162,199,287`); there is no env var that installs a Function. `ENABLE_PLUGINS=False` removes the surface entirely. [O8]

Pipelines (the separate `ghcr.io/open-webui/pipelines` container, added as an OpenAI connection) are "legacy and are no longer recommended"; the docs redirect a pipe to a Pipe Function, a filter to a Filter Function, and "connecting an external HTTP service" to "an OpenAPI or MCP tool server". [O7]

> Villa reading: a Filter is the only place to run pre/post logic *inside* Open WebUI (for instance forcing `tools` on or injecting a system prompt), but it is Python installed by API, so it contradicts "never hand-edit the UI" in spirit and adds a second language. Prefer configuration (`TOOL_SERVER_CONNECTIONS`, `OPENAI_API_CONFIGS`) and keep logic in the Go tool server.

### (e) Environment variables (exact names, [O8] and `config.py`/`env.py` at `0a7c1583`)

| Variable | Default | ConfigVar | Purpose |
|---|---|---|---|
| `TOOL_SERVER_CONNECTIONS` | `[]` | yes | global OpenAPI/MCP tool servers (JSON array) |
| `TERMINAL_SERVER_CONNECTIONS` | `[]` | yes | Open Terminal / Terminals connections, proxied by the backend |
| `ENABLE_DIRECT_CONNECTIONS` | `False` | yes | user-added model/tool connections from the browser |
| `USER_PERMISSIONS_FEATURES_DIRECT_TOOL_SERVERS` | `False` | yes | non-admins may add user tool servers |
| `USER_PERMISSIONS_WORKSPACE_TOOLS_ACCESS` | `False` | yes | non-admins may create Python Tools |
| `ENABLE_TOOL_PERMISSIONS` | `False` | yes | per-call Allow/Deny mode (experimental; post-pin) |
| `TOOLS_FUNCTION_CALLING_PROMPT_TEMPLATE` | built-in | yes | Legacy-mode prompt only |
| `ENABLE_CODE_EXECUTION` / `CODE_EXECUTION_ENGINE` | `True` / `pyodide` | yes | manual Run button (legacy engines) |
| `ENABLE_CODE_INTERPRETER` / `CODE_INTERPRETER_ENGINE` | `True` / `pyodide` | yes | `execute_code` builtin (legacy engines) |
| `CODE_EXECUTION_JUPYTER_URL`, `_AUTH`, `_AUTH_TOKEN`, `_TIMEOUT` | `""`, `""`, `""`, `60` | yes | Jupyter engine |
| `ENABLE_AUTOMATIONS` / `USER_PERMISSIONS_FEATURES_AUTOMATIONS` | `True` / `False` | yes | scheduled runs |
| `ENABLE_SUBAGENTS` / `SUBAGENTS_BACKGROUND_ENABLED` | `False` / `False` | yes | `delegate_task` (post-pin) |
| `ENABLE_MEMORIES` | `True` | yes | builtin memory tools |
| `ENABLE_FORWARD_USER_INFO_HEADERS` | `False` | no | needed for external tool events |
| `ENABLE_PLUGINS` | `True` | no | Python Tools/Functions surface |
| `ENABLE_PERSISTENT_CONFIG` | `True` | no | `False` ⇒ env wins every boot |

There is **no** env var for the default `function_calling` mode; it is a per-model param (or a per-request param). [O2][O8]

### (f) Sandbox and host filesystem

- **Pyodide**: in-browser WebAssembly, fixed package list, "cannot install additional libraries", virtual `/mnt/uploads/` in IndexedDB. No host access at all. **Jupyter**: server-side, the admin's Jupyter's filesystem. Both are "legacy … Pyodide may be deprecated in a future release". `execute_code` is a Native-mode builtin that requires `ENABLE_CODE_INTERPRETER` plus the model capability. [O9]
- **Open Terminal** (`ghcr.io/open-webui/open-terminal`, `OPEN_TERMINAL_API_KEY`, port 8000): "a real computer substrate", "run it in Docker for isolation, or bare metal when the agent should work directly on the host"; the model gets shell, files, packages, servers, previews; the file browser sidebar appears only when registered under Admin → Integrations → Open Terminal (not as a tool server). Chat uploads can be written straight into the terminal's cwd. Automations can carry a terminal via the model's Terminal capability. It can also run as an MCP server. [O10][O10b][O10c][O12]

> Villa reading: Open Terminal is the closest ready-made "VM" and it is registrable from env at the pin. It is a Python container with root-of-workspace power and no read/write tool split; the sandbox boundary is whatever Podman gives it. A Go OpenAPI server gives villa the read/write split and the deletion gate, at the cost of writing the tools.

### llama-server requirements

"OpenAI-style function calling is supported with the `--jinja` flag (and may require a `--chat-template-file` override to get the right tool-use compatible Jinja template; worst case, `--chat-template chatml` may also work)". For `/v1/messages`: "Tool use requires `--jinja` flag … `tools`: Array of tool definitions (requires `--jinja`)". `parallel_tool_calls` is per request and "only supported on some models, verification is based on jinja template". [L1] Native handlers include "Hermes 2/3, Qwen 2.5, Qwen 2.5 Coder"; unrecognised templates get "Generic tool call … may consume more tokens"; "beware of extreme KV quantizations (e.g. `-ctk q4_0`), they can substantially degrade the model's tool calling performance" (villa keeps f16, ADR-0008). `/props` exposes `chat_template` and `chat_template_caps`. [L2]

In villa, `--jinja` is emitted only when `CodingMode` is non-nil (`internal/inference/inference.go:122`), and `RunClaude` refuses without it because "tool use over /v1/messages needs the --jinja template" (`internal/agent/claude.go`). The chat model served to Open WebUI is the same unit, so Native tool calling in chat has the same gate.

## Decisions this unblocks

1. **Go OpenAPI tool server, not Python Tools.** Finding 4: OpenAPI is a first-class, env-provisionable, backend-called seam; Python Tools are in-process `exec()` with a "shell access" warning. Cost accepted: no confirmation dialog and no streaming from the tool. Ship it as a `villa-tools` Quadlet unit in the websafe shape (distroless, bind-mounted `villa` binary, `Volume=` for the granted folder).
2. **Native function calling; the pin must move or the model param must be set.** Findings 2 and 3: at the pinned 0.9.6 an unset param is prompt mode. Options: (a) bump the Open WebUI pin past v0.10.0 (re-vet per `docs/RELEASING.md`; also brings `ENABLE_TOOL_PERMISSIONS` and sub-agents), or (b) stay and set `function_calling=native` on the villa model via the models API at install. (a) is cleaner because (b) is a DB write villa does not otherwise perform.
3. **Coding mode becomes "tools mode".** Finding 7: `--jinja` is the prerequisite for any tool use on llama-server, chat included. Either the gate is widened (rename, keep the sampling preset separate) or a Cowork-style chat requires coding mode on. Catalog entries need a `tool_calling` capability field backed by the template family (`/props` `chat_template_caps` is the witness, ADR-0007 style).
4. **Sandbox shape: container + folder grant, read/write split enforced in Go.** Findings 1, 4 and 6: Cowork's topology is loop outside, code inside, files through a gate. Villa's version is the tool server in a rootless container with exactly one `Volume=` (the "connected folder"), read tools free, write tools policy-gated, delete refused unless a per-task token is present. Open Terminal is the fallback if the maintainer would rather not write tools, at the cost of a new pinned image and no gate.
5. **Approval model: policy at the server, UI approval later.** Finding 4's limit (no interactive events) means the first cut cannot ask; it can only allow/refuse-with-remediation, which is already the preflight idiom. Per-call Allow/Deny arrives with decision 2(a) via `ENABLE_TOOL_PERMISSIONS`.
6. **Provisioning stays env-only.** Finding 5: `TOOL_SERVER_CONNECTIONS` (and `TERMINAL_SERVER_CONNECTIONS` if Open Terminal is chosen) render from `config.toml` into the existing ordered `Env` block behind the existing `ENABLE_PERSISTENT_CONFIG=False` trailer, byte-frozen by the golden. No Functions, no Pipelines. `ENABLE_FORWARD_USER_INFO_HEADERS=True` only if the tool server will post status events.
7. **Scheduled and parallel work: defer, and measure first.** Automations exist at the pin and always run with full access (so decision 4's policy is the only guard); sub-agents are post-pin, off by default, and fan out completions against one llama-server. Whether either is worth enabling is a `--parallel`/context-budget measurement for `recommend`, not a UI toggle.

## Sources

Anthropic (support.claude.com unless noted):
[A1] `/en/articles/13345190-get-started-with-claude-cowork` (†bullets via WebFetch) ·
[A2] `/en/articles/14479288-claude-cowork-architecture-overview` ·
[A3] `/en/articles/13364135-use-claude-cowork-safely` ·
[A4] `/en/articles/13854387-schedule-recurring-tasks-in-claude-cowork` ·
[A5] `/en/articles/15520349-use-claude-cowork-on-web-desktop-and-mobile` ·
[A6] `/en/articles/13837440-use-plugins-in-cowork` ·
[A7] `/en/articles/14128542-let-claude-use-your-computer-in-cowork` ·
[A8] `/en/articles/16607400-use-the-built-in-browser-in-claude-cowork` ·
[A9] `/en/articles/14116274-organize-your-tasks-with-projects-in-claude-cowork` ·
[A10] `/en/articles/13947068-assign-tasks-from-anywhere-in-claude-cowork` ·
[A11] `/en/articles/13455879-use-claude-cowork-on-team-and-enterprise-plans` ·
[A12] https://claude.com/product/cowork

Open WebUI docs (docs.openwebui.com):
[O1] `/features/extensibility/plugin/tools/` ·
[O2] `/features/extensibility/plugin/tools/development/` ·
[O3] `/features/extensibility/plugin/development/events` ·
[O4] `/features/extensibility/plugin/tools/openapi-servers/` ·
[O5] `/features/extensibility/plugin/tools/openapi-servers/open-webui/` ·
[O6] `/features/extensibility/plugin/functions/` ·
[O7] `/features/extensibility/pipelines/` ·
[O8] `/reference/env-configuration` ·
[O9] `/features/chat-conversations/chat-features/code-execution/python/` ·
[O10] `/features/open-terminal/`, [O10b] `…/setup/connecting`, [O10c] `…/setup/installation` ·
[O11] `/features/chat-conversations/chat-features/subagents` ·
[O12] `/features/chat-conversations/chat-features/automations/` ·
[O13] `/features/extensibility/plugin/`

Open WebUI source: https://github.com/open-webui/open-webui at `0a7c15832fb30b1903753e83f81dc7d27e5b0944` (paths under `backend/open_webui/`) and at villa's pinned revision `02dc3e689ceac915a870b373318b99c029ddf603`.

llama.cpp at `72797e89198ab564fd0e6baa54ab196e8dd1d884`:
[L1] `tools/server/README.md` · [L2] `docs/function-calling.md`.
