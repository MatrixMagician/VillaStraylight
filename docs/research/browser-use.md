# Browser use on a local sandboxed stack

Findings for [Research: browser use on a local sandboxed stack](https://github.com/MatrixMagician/VillaStraylight/issues/164), on the map [Wayfinder map: v1.11, the workspace agent](https://github.com/MatrixMagician/VillaStraylight/issues/156).

The question was how local OSS agent stacks do browser use, and what it would cost villa's sandbox model: a libkrun microVM container on an internal no-egress podman network, one rw folder grant, no GPU. Every claim below was verified on 2026-09-10 against primary sources only: the Playwright MCP repository and the Playwright monorepo it now delegates to, the browser-use repository and docs, the Open WebUI docs, and Anthropic's help-centre articles for Claude in Chrome and Cowork. Where a claim was tested, it was tested on the gfx1151 dev box with the exact commands given. Nothing rests on a secondary write-up; the two things this note could not verify are marked as such.

> **Placement note.** As with `update-version-checks.md`, this goes in `docs/research/`; the historical `.planning/` tree was removed (see `CLAUDE.md`, "Historical planning artifacts"). Probe scripts were throwaway and are reproduced inline rather than committed.

## Summary for the tickets waiting on this

1. **The browser runs under libkrun, with one flag.** Playwright's Chromium 153 launches headless inside the krun guest (kernel 6.12.91, no `/dev/dri`, no GPU) and renders through SwiftShader, but `Page.captureScreenshot` fails until Chromium is started with `--no-zygote`. With that flag the full Playwright MCP server works over HTTP inside the microVM: 24 tools, navigate, screenshot, origin blocking.
2. **Headless is the only option in a container, and the only one the OSS stacks ship.** Playwright MCP's Docker image is headless-Chromium-only by construction; browser-use auto-detects "no display, so headless". Headed browsing is what Cowork and Claude in Chrome do, and they do it on the user's desktop, outside any sandbox.
3. **Cowork does not put the browser in its VM.** Its VM runs shell and code; the browser is either the Claude Desktop app's built-in browser or the user's Chrome. Egress in Cowork's cloud sessions is "enforced outside the sandbox" by a mandatory allow-listed proxy. That is exactly the shape villa can copy: a browser container on the internal network whose only route out is `--proxy-server` pointing at an allow-listing egress proxy.
4. **Playwright MCP is the fit; browser-use is not.** Playwright MCP is a stateless tool server with no LLM of its own, a digest-pinnable version-tagged image, and honest docs ("not a security boundary"). browser-use is an agent loop that wants its own LLM key, sends task instructions, URLs and results to PostHog **by default**, mirrors sessions to its cloud by default, documents no published image, and its own docs warn that smaller Qwen models get its action schema wrong.
5. **Approval is the harness's job; the server has none.** Playwright MCP exposes read tools (snapshot, screenshot, console) and write tools (click, type, fill, upload, `run_code_unsafe`, `evaluate`) with no gate between them. Cowork's model, per-site grant on first contact, protected actions that always ask, a short prohibited list, maps directly onto the workspace agent's Manual/Auto/Skip tool classes.
6. **Cost: one image, +1.03 GB on disk, and a proxy that does not exist yet.** `mcr.microsoft.com/playwright/mcp:v0.0.80` is `sha256:dda1f7f9…`, 419 MB to pull, 1,025,758,253 bytes installed, a version tag that resolves to the same digest as `:latest`, so it checks like Qdrant. Open WebUI contributes nothing here: its "playwright" is a page loader for `fetch_url`, not browser use.

## Sources, pinned

| Source | What was read | Version / commit |
|---|---|---|
| [microsoft/playwright-mcp](https://github.com/microsoft/playwright-mcp) | README, `Dockerfile`, `cli.js`, `package.json` | `8a13ef8e9f7385a0f89477922127f31cbfde9761`, `@playwright/mcp` 0.0.80, depends on `playwright-core` 1.63.0-alpha-2026-08-31 |
| [microsoft/playwright](https://github.com/microsoft/playwright) | `packages/playwright-core/src/tools/mcp/{config,browserFactory}.ts`, `tools/backend/context.ts` | `main` at `af74c938e45f3e759dc2521993f201389eb16cb6` (the MCP repo's `src/README.md` says the server source moved here) |
| [browser-use/browser-use](https://github.com/browser-use/browser-use) | `browser_use/config.py`, `browser/profile.py`, `Dockerfile`, `docker/README.md` | `50f205533fe10ba35b553d2a3689c77b87bd5d0a`, version 0.13.10, drives Chromium via `cdp-use` 1.4.5, not Playwright |
| [docs.browser-use.com](https://docs.browser-use.com/llms.txt) | all-parameters, basics, mcp-server, telemetry, supported-models | live on 2026-09-10 |
| [docs.openwebui.com](https://docs.openwebui.com) | env-configuration, agentic-search, hardening | live on 2026-09-10 |
| [playwright.dev](https://playwright.dev/docs/docker) | docker, browsers | Playwright 1.63.0 docs |
| support.claude.com | Claude in Chrome [getting started](https://support.claude.com/en/articles/12012173), [permissions](https://support.claude.com/en/articles/12902446), [safety](https://support.claude.com/en/articles/12902428), [admin controls](https://support.claude.com/en/articles/13065128); Cowork [built-in browser](https://support.claude.com/en/articles/16607400), [architecture](https://support.claude.com/en/articles/14479288), [browser setup](https://support.claude.com/en/articles/16635803) | live on 2026-09-10 |

Two Anthropic URLs the ticket suggested (`12494571`, a Cowork getting-started page) returned 404 and were replaced by the articles above. All pages were fetched with `pullmd`; no `WebFetch` fallback was needed.

## Headless versus headed

**Playwright MCP is headed by default and headless on request**: `--headless` ("run browser in headless mode, headed by default", [README](https://github.com/microsoft/playwright-mcp)). When neither is given, `config.ts` sets `headless = os.platform() === 'linux' && !process.env.DISPLAY`, so a Linux container with no display is headless by inference. **The Docker image removes the choice**: the README says "The Docker implementation only supports headless chromium at the moment", and the `Dockerfile`'s entrypoint is `node /app/cli.js --headless --browser chromium --no-sandbox`. The image installs Chromium with `playwright-core install --no-shell chromium`, so it carries the full browser running in Chrome's new headless mode, not the older headless shell; this matters below because `chromium.launch()` must pass `channel: 'chromium'` to find it.

**browser-use auto-detects**: `headless` defaults to `None`, "auto-detects based on display availability" ([all-parameters](https://docs.browser-use.com/open-source/customize/browser/all-parameters.md)); in `profile.py` that is `self.headless = not has_screen_available`. Its MCP server takes `BROWSER_USE_HEADLESS`.

**Anthropic's two browsers are headed and on the user's desktop.** Claude in Chrome "is a browser extension that allows Claude to read, click, and navigate websites alongside you", driving the user's own Chrome through the `debugger` permission ([getting started](https://support.claude.com/en/articles/12012173)). Cowork's built-in browser "lives in the desktop app", opens "in the side panel next to your task", and "Claude Desktop needs to be open and online for Claude to use it, even though your Cowork session runs in the cloud" ([built-in browser](https://support.claude.com/en/articles/16607400)). Neither is sandboxed; both are watched.

**For villa: headless, with screenshots as the watch-while-it-works surface.** There is no display in a microVM and no case for a VNC sidecar. The MCP server returns screenshots as image content (`--image-responses allow` is the default), which the dashboard could show; that is a surface decision for #156, not a finding.

## Where the browser process runs, and whether it runs under libkrun

Both containers below use the digest-pinned MCP image, `--network none`, and a script that launches Chromium through the image's own `playwright-core`, loads a `data:` page, reads the DOM, and takes a screenshot. `podman --runtime=krun` was already on the box from the prototype (`/usr/bin/krun` is a `crun` symlink; `crun-krun` 1.28, `libkrun` 1.19.0 per the [prototype addendum](https://github.com/MatrixMagician/VillaStraylight/blob/prototype/cowork/docs/prototype/cowork/RESULTS.md)).

```bash
IMG=mcr.microsoft.com/playwright/mcp@sha256:dda1f7f9b812e22946635c8af7df9288b96d3b9e3f0f1b8576d6823e2031c1de
podman run --rm --runtime=$RT --network none --init -e EXTRA_ARGS="$FLAGS" \
  -v ./pwcheck.js:/tmp/pwcheck.js:Z --entrypoint node $IMG /tmp/pwcheck.js
# pwcheck.js: chromium.launch({headless:true, channel:'chromium', args:['--no-sandbox', ...EXTRA_ARGS]})
#             goto data:text/html …; textContent('h1'); screenshot(); print JSON
```

| Runtime | Extra Chromium flags | Launch + DOM | Screenshot | Wall time |
|---|---|---|---|---|
| crun | none | ok | 5583 bytes | 0.25 s |
| krun | none | ok | **`Page.captureScreenshot: Unable to capture screenshot`** | 1.27 s |
| krun | `--disable-gpu` | ok | fails | 1.32 s |
| krun | `--disable-dev-shm-usage` | ok | fails | 1.20 s |
| krun | `--disable-gpu-compositing` | ok | fails | 1.22 s |
| krun | `--shm-size=1g` (podman) | ok | fails | 1.27 s |
| krun | **`--no-zygote`** | ok | **5583 bytes** | 1.39 s |

Guest facts under krun, from a node probe: kernel `6.12.91`, uid 0, 16 vCPU, **1017 MB RAM by default**, 509 MB `/dev/shm`, no `/dev/dri`, no `/dev/kfd`, no `/.dockerenv`. Under both runtimes WebGL reports `ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero)))`: software rendering, which is what a GPU-less sandbox should report. The GPU-less part is not a problem at all; Chromium never asked for `/dev/dri`.

**The one real finding is `--no-zygote`.** Without it, the page loads and the DOM is readable but the compositor cannot produce a frame; with it, everything works. The mechanism (the zygote process model against the guest kernel) is **not established** here; the flag is, by bisection. browser-use's own container flag set already includes it (`CHROME_DOCKER_ARGS` in `profile.py`: `--no-sandbox --disable-gpu-sandbox --disable-setuid-sandbox --disable-dev-shm-usage --no-xshm --no-zygote --disable-site-isolation-trials`), which is independent evidence that this is the known container-Chromium flag set. Note that browser-use only applies it when `IN_DOCKER` is detected, and its detection (`/.dockerenv`, "docker" in `/proc/1/cgroup`, a PID-1 heuristic) has nothing to key on inside a krun guest, so it would have to be set explicitly.

Playwright MCP passes flags through its config file (`browser.launchOptions.args`), so no image rebuild is needed. The full server was then run the way a villa unit would run it:

```bash
podman run --rm --runtime=krun --init -p 127.0.0.1:8932:8932 -v ./mcp-config.json:/tmp/mcp-config.json:Z $IMG \
  --port 8932 --host 0.0.0.0 --allowed-hosts 127.0.0.1:8932 --config /tmp/mcp-config.json
# mcp-config.json: launchOptions.args ["--no-zygote", …], isolated: true, network.blockedOrigins ["https://example.com"]
```

A streamable-HTTP client (`initialize`, `tools/list`, `tools/call`) got: server `Playwright 1.63.0-alpha-2026-08-31`; 24 tools; `browser_navigate` to a `data:` page rendered; `browser_take_screenshot` wrote `.playwright-mcp/page-….png`; navigating to the blocked origin returned `isError: true` with `net::ERR_BLOCKED_BY_CLIENT`. Two operational notes: `--allowed-hosts` had to be passed (the default host-header check rejected `127.0.0.1:8931` with "Access is …"), and the container ignored SIGTERM under krun, so `podman stop` fell through to SIGKILL after two seconds; a unit needs a `TimeoutStopSec` it is happy with.

**Inside the microVM or a sibling?** Both are now proven. The sibling shape is the better one, for three reasons that all come from the sources rather than taste:

- Cowork's own topology is browser-outside-the-VM ([architecture](https://support.claude.com/en/articles/14479288): "Code execution runs in an isolated virtual machine"; the browser is a desktop-app component or the Chrome extension).
- Open WebUI's remote-browser pattern is a sibling too: `PLAYWRIGHT_WS_URL` "e.g. `ws://playwright:3000`" so the app container stays small and "browser concerns" are separate ([env-configuration](https://docs.openwebui.com/reference/env-configuration/)).
- It keeps the folder grant away from the browser. Playwright MCP restricts file access to the workspace roots or cwd and blocks `file://` navigation (`checkUrlAllowed`, `checkFile` in `context.ts`; `--allow-unrestricted-file-access` lifts it). A browser container with no folder grant cannot exfiltrate the workspace even if a page persuades it to try; downloads land in its own output dir and reach the workspace only through an explicit tool-server step.

So: a `villa-browser` unit, `--runtime=krun`, its own image, driven by the villa tool server over MCP streamable HTTP on the podman network, with the file sandbox unit as a separate krun container. Memory: the default 1 GB guest sufficed for a `data:` page; real sites need the `--memory` the prototype used (4 GB) or a measured figure, which is a prototype item.

## Egress, and how it breaks the internal-network model

A browser with no egress is a browser for local pages only. The prototype's sandbox network is `--internal`, which is the right posture for the file sandbox and wrong for the browser. The sources give one answer, twice:

- **Cowork's cloud sessions**: "Egress is enforced outside the sandbox. All traffic leaving the sandbox passes through a mandatory proxy the sandbox can't reconfigure or bypass, and only allow-listed destinations are reachable"; the sandbox also "can't reach private, internal, link-local, or cloud-metadata addresses" ([architecture](https://support.claude.com/en/articles/14479288)).
- **Playwright MCP** has the client side of exactly that: `--proxy-server` ("`http://myproxy:3128` or `socks5://myproxy:8080`") and `--proxy-bypass`, applied in `config.ts` to both `launchOptions.proxy` and `contextOptions.proxy`.

The shape for villa is therefore: browser container on an **internal** network; an egress-proxy container on that internal network **and** the default network; Chromium launched with `--proxy-server http://villa-egress:3128`; the proxy holds the allowlist and refuses private ranges. The browser never has a route out; the proxy is the boundary. This is the same "outbound honesty" line `villa update` already draws for its one request, and unlike Playwright MCP's own filters it is a boundary: the README says of `--allowed-origins`/`--blocked-origins` that they do "not serve as a security boundary and do not affect redirects", and the implementation is `context.route('**', route.abort('blockedbyclient'))` plus per-origin `continue()` in `context.ts`, a client-side hint the proxy must back. Whether Chromium resolves names itself or hands the host to the proxy in a `CONNECT` is standard proxy behaviour that this note did **not** verify; a prototype must confirm no DNS leaves the internal network.

What no proxy fixes: the model reads the page. Playwright MCP returns the accessibility snapshot as text and screenshots as images; every byte of it is untrusted. Anthropic runs two classifiers on that path ("One checks incoming content for injection attempts, and another checks every action Claude takes before it runs", [safety](https://support.claude.com/en/articles/12902428)) and still says "The risk is not zero". Villa's `websafe` already sanitises, normalises, fences and classifies fetched text and "reduces and FLAGS, and never claims safe" (`CLAUDE.md`); routing snapshots through it is the obvious move and, honesty-bounded, the most villa can claim.

No image needed, no proxy image chosen: that is the missing piece and the largest cost in this section. It is a pin, a unit, a policy file and a preflight, none of which exist.

## How actions get approved

**Playwright MCP has no approval layer.** The 24 tools returned by `tools/list` under krun were: `browser_close, browser_resize, browser_console_messages, browser_handle_dialog, browser_evaluate, browser_file_upload, browser_drop, browser_find, browser_fill_form, browser_press_key, browser_type, browser_navigate, browser_navigate_back, browser_network_requests, browser_network_request, browser_run_code_unsafe, browser_take_screenshot, browser_snapshot, browser_click, browser_drag, browser_hover, browser_select_option, browser_tabs, browser_wait_for`. The server executes whatever the client sends; the README's security section is one line, "Playwright MCP is **not** a security boundary". `--secrets` (a dotenv file whose values are substituted at fill time and redacted from responses, `lookupSecret`/`redactSecrets`) is the only credential-shaped feature, and it keeps secrets out of the model's context, not out of the page.

**browser-use's MCP server is the same**, plus `retry_with_browser_use_agent`, an autonomous inner loop that needs its own `OPENAI_API_KEY`/`ANTHROPIC_API_KEY` ([mcp-server](https://docs.browser-use.com/open-source/customize/integrations/mcp-server.md)); its guard is `allowed_domains`/`prohibited_domains` on navigation, with TLD wildcards refused ([all-parameters](https://docs.browser-use.com/open-source/customize/browser/all-parameters.md)).

**Anthropic's model is the one worth copying**, from the [permissions guide](https://support.claude.com/en/articles/12902446) and [built-in browser](https://support.claude.com/en/articles/16607400) pages:

| Layer | Claude in Chrome / Cowork | Villa equivalent |
|---|---|---|
| Per-site grant | "Claude asks for your permission before acting on a site for the first time": Allow all for this website / Allow this time only / Deny | `browser_navigate` to a new origin is a grant request; the grant is per origin, per task or persistent |
| Mode | Manually approve (each action), Automatically approve (default; a classifier screens each action, blocks or pauses), Skip all approvals | The workspace agent's Manual/Auto/Skip tool classes; there is no classifier to run in Auto, so Auto here means "the proxy allowlist and websafe are the only checks", and the spec should say so |
| Protected actions | ask regardless of mode: downloading a file, entering sensitive information | `browser_file_upload`, `browser_fill_form` on password-type fields, any download reaching the workspace: always Manual |
| Prohibited | purchases, account creation, captcha bypass, trades, permanent deletion, inputting sensitive data, scraping faces | not expressible as tool classes; a system-prompt rule and, for `browser_run_code_unsafe`/`browser_evaluate`/`browser_network_request`, simply not exposing the tool |
| Blocked categories | adult and pirated sites blocked; financial sites ask | the proxy allowlist, which is stricter: default-deny |

The read/write split is clean: read tools (`snapshot`, `take_screenshot`, `console_messages`, `network_requests`, `find`, `tabs`, `wait_for`) are Auto-safe once an origin is granted; write tools (`click`, `type`, `fill_form`, `press_key`, `select_option`, `drag`, `drop`, `hover`, `handle_dialog`, `navigate`) are the Manual/Auto class. The tool server can present a subset, so the dangerous three need never reach the model.

## Image size and pin cost

| Image | Digest | Pull | On disk | Pin shape |
|---|---|---|---|---|
| `mcr.microsoft.com/playwright/mcp:v0.0.80` (= `:latest`) | `sha256:dda1f7f9b812e22946635c8af7df9288b96d3b9e3f0f1b8576d6823e2031c1de` | 418,612,053 bytes, 11 layers | 1,025,758,253 bytes | version tag; 15 tags `v0.0.64`…`v0.0.80` listed; created 2026-09-01 |
| `mcr.microsoft.com/playwright:v1.58.0-noble` (all three browsers, no MCP) | `sha256:35c7d48b4ccaf3aca5018f5f1bf7f50c7da7d61d176c530741f4f2e9ca336c34` | 903,408,126 bytes | not pulled | version tag; Playwright docs: "always pin your Docker image to a specific version" |
| browser-use | none documented | | | `Dockerfile` is `python:3.12-slim` plus the distro `chromium` package; `docker/README.md` says build it yourself; no `docker pull` in the README; a Docker Hub probe for `browseruse/browser-use` found nothing (**unverified** that no image exists anywhere) |
| Open WebUI `:v0.10.0`, for scale | `sha256:ab9246eb…` | 1,772,551,009 bytes | | |

The MCP image is Debian bookworm-slim, node 22, runs as `node`, and pins `playwright-core` to an **alpha** (`1.63.0-alpha-2026-08-31`); the release cadence is roughly weekly by the tag list. It therefore checks like Qdrant under `villa update --check` (a version tag whose digest can also move under the same name) and, being an alpha train, should be expected to move often. Per the [Playwright Docker docs](https://playwright.dev/docs/docker), `--init` is recommended, `--ipc=host` is recommended for Chromium under Docker (moot under krun, where `--disable-dev-shm-usage` and the 509 MB guest shm did the job), and running as root "will disable the Chromium sandbox"; the krun guest is root regardless (prototype addendum, "`--user` is ignored under krun"), and the image passes `--no-sandbox` anyway. The VM is the sandbox.

Two supply-chain notes. `mcr.microsoft.com` is a fourth registry for `pins`/`updatefetch` (after docker.io, ghcr.io, gcr.io); whether it needs a token step was not tested. And Playwright's base image docs say it "is not recommended to use this Docker image to visit untrusted websites", which is precisely the use; the mitigation is the microVM, not the image.

## Open WebUI: nothing to reuse, one hazard to name

Open WebUI's agentic mode gives the model exactly two web tools, `search_web` and `fetch_url` ([agentic search](https://docs.openwebui.com/features/chat-conversations/web-search/agentic-search/)); `fetch_url` returns page text truncated at 50,000 characters. `WEB_LOADER_ENGINE=playwright` is the renderer behind `fetch_url`, "for rendering pages with JavaScript support", not browsing; the model never clicks. Two facts matter for villa:

- If `PLAYWRIGHT_WS_URL` is unset, "Playwright with Chromium dependencies will be automatically installed in the Open WebUI container on launch" ([env-configuration](https://docs.openwebui.com/reference/env-configuration/)): a runtime download into a digest-pinned container, unpinnable and impossible with the outbound posture. Never set that engine without the remote-browser URL.
- The [hardening guide](https://docs.openwebui.com/getting-started/advanced-topics/hardening/) says the Playwright loader only enforces `validate_url()` and the redirect gate "as of v0.9.6"; villa's pin is exactly v0.9.6, the floor. The same page mentions a "`playwright` Docker variant"; the ghcr tag list for `open-webui/open-webui` contains no playwright-tagged image today, so its existence is **unverified**.

## browser-use: why not

- **Telemetry on by default**, and it is not metadata: "Telemetry may include usage metadata and task-level data such as task instructions, URLs visited, action traces, errors, and final results" to PostHog; opt-out is `ANONYMIZED_TELEMETRY=false` ([telemetry](https://docs.browser-use.com/open-source/development/monitoring/telemetry.md)). In `config.py`, `ANONYMIZED_TELEMETRY` defaults to `'true'` and `BROWSER_USE_CLOUD_SYNC` **defaults to the telemetry value**, so an unconfigured install also syncs to the vendor cloud. That is two env vars villa would have to prove are set, forever.
- It is an agent, not a tool server: it brings its own loop, its own LLM client and its own prompts, and the harness decision on #156 is a villa-owned loop or Crush over a villa tool server.
- Its docs on local models: "Currently, only `qwen-vl-max` is recommended … Smaller Qwen models may return incorrect action schema formats" ([supported models](https://docs.browser-use.com/open-source/supported-models.md)). Villa's catalog is Qwen3.6-35B-A3B.
- No published image to pin (above).

It does carry one useful thing, the container flag set quoted in the libkrun section, which corroborates `--no-zygote`.

## Decisions this unblocks

1. **Browser use is a sibling `villa-browser` krun unit running `mcr.microsoft.com/playwright/mcp`, driven by the tool server over MCP HTTP**, not a browser inside the file sandbox and not a browser-use agent. Proven on hardware with one non-default Chromium flag, `--no-zygote`, passed through the MCP config file.
2. **Egress is a proxy container with an allowlist, on both the internal browser network and the default network**, Cowork's cloud shape. The browser has no route; Chromium gets `--proxy-server`. This is new work: image, pin, unit, policy, preflight, and a prototype to confirm DNS does not leak.
3. **Approvals follow Anthropic's three layers**: per-origin grant on first navigate; read tools Auto once granted, write tools by the task's Manual/Auto/Skip class; `file_upload` and sensitive fills always Manual; `run_code_unsafe`, `evaluate` and `network_request` not exposed. The spec must say that villa's Auto mode has no per-action classifier and rests on the allowlist plus `websafe` over snapshots.
4. **Headless only; screenshots are the watching surface** if the dashboard wants one. No display, no VNC.
5. **The pin is a version tag on a fourth registry, on an alpha train**; `pins` and `updatefetch` gain `mcr.microsoft.com`, and `--check` should expect it to move. Open WebUI's playwright loader stays off; browser-use is out.

## Reproducing this

Every command above runs as given from the dev box; the `podman` probes need `crun-krun`/`libkrun` (present) and pull 419 MB. Digests will have moved on; the table is a snapshot of 2026-09-10.
