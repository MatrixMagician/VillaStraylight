# Results: Qwen Code as a third `villa code` agent

Run 2026-11-03 on the gfx1151 dev host. Qwen Code 0.26.6 (npm), headless, against the live
stack's model (Qwen3.6-35B-A3B Heretic Q6_K, ngram, 131072 ctx). Raw captures in `results/`.

## Verdict

**Ship as an opt-in `--agent qwen`, conditional.** With the opt-out set, the headless
run made no off-box connection of any kind and its only DNS lookup was `villa-llama`. Without
it, Qwen Code tries to reach an Alibaba Cloud RUM host on start-up and every few seconds
after. The five document tasks all completed, at parity with Crush on correctness.

Two conditions before any code: (1) an interactive-mode capture, because only headless was
traced; (2) villa must own the config, not the user's `~/.qwen`.

## Egress (the blocker)

Capture: `tcpdump` in the rootless netns, filter `src host <container> and (not dst net
<villa subnet> or dst port 53)`. **The first filter was wrong** (it excluded the container's own
IP) and recorded zero packets while the control request succeeded. `control.sh` caught it;
the control now shows 14 packets for one deliberate `fetch()`, and every figure below is from the
fixed filter. T5-only smoke result from the broken filter is discarded.

| Mode | Tasks | Off-subnet packets | DNS names asked | TCP to a non-villa IP |
|---|---|--:|---|--:|
| Opt-out (`QWEN_USAGE_STATISTICS_ENABLED=false` + settings below) | T1–T5 | 188, all DNS | `villa-llama` only | **0** |
| Default (provider config only) | T1–T5 | 324, all DNS | `villa-llama`, **`gb4w8c3ygj-default-sea.rum.aliyuncs.com`** (90 A + 90 AAAA) | 0 |

- The RUM lookup starts in the first second of a session and repeats about once a minute.
  That is the documented usage-statistics endpoint, on a build whose provider was my own
  local server, so "your own API key" really is not an exemption.
- **No TCP followed the default-mode lookup, and that is this network's doing, not Qwen
  Code's.** The host resolver answers that name with `0.0.0.0` / `::`. So the capture proves
  the attempt, not the payload, and says nothing about what a resolving network would carry.
- Opt-out settings used: `privacy.usageStatisticsEnabled=false`,
  `general.enableAutoUpdate=false`, `ui.enableUserFeedback=false`, `telemetry.enabled=false`,
  plus the env var. I did not bisect which one suffices; the docs say the env var takes
  precedence and that disabling usage statistics also stops the models.dev catalog refresh.

## Capability (live model, T1–T5, one run per task and mode)

| Task | Wall opt-out / default | Crush (earlier, Q4_K_M) | Outcome (default-mode outputs graded; opt-out outputs were not kept) |
|---|--:|--:|---|
| T1 organise + index | 122 s / 88 s | 77 s | 13 files into folders, meetings renamed ISO, **`Sync-June-1.md` correctly filed and renamed** (Crush missed this), `INDEX.md` written, nothing deleted. Default run also converted `letter_draft.txt` and `todo.txt` to `.md`, which was not asked |
| T2 reconcile | 69 s / 89 s | 55 s | All 7 statuses right, £50.00 short payment caught, draft flagged, **outstanding £1,885.50 correct**. Totals table wrong: invoiced £11,446.75 (is £10,646.75), paid in full £5,720.50 (is £5,660.50). "32 days overdue" uses an invented current date; an April payment is matched to a May–June export with a hand-wave |
| T3 action items | 65 s / 90 s | 55 s | 6 actions, 1 decision, net-30 contingency resolved from the later meeting, `INV-1045?` ambiguity carried and flagged for a human rather than resolved |
| T4 memo | 71 s / 48 s | 22 s | Requested shape. Mild invention: "hardware retains resale value", "desktop-oriented" front end, "Vulkan may indicate instability". Nothing as bad as the Claude Code stand-in's T4 |
| T5 duplicates, report-only | 93 s / 64 s | 50 s | Two byte-identical pairs found with keep rationale; photos reported as differing, kept; only `reports/duplicates.md` added. Crush counted a third identical pair |

All ten runs exited 0, none hit the timeout. Totals 420 s opt-out, 379 s default, against
Crush's 259 s. The model, quant and a shared live server differ from the Crush runs, so the
wall-time gap is indicative only.

The arithmetic slip in T2's totals table is the same class of miss as Crush's, and the
grounding audit (`internal/grounding`) is the control for it, not the agent choice.

## What this does not settle

- **Tool-call success rate and malformed calls were not measured.** `-p` text output carries
  no tool trace; the proxy for it is that all ten runs completed with the right file changes.
- Interactive TUI start-up, `qwen serve`, MCP and extension fetches, and update checks that
  only run interactively were not exercised or captured.
- Task T6 (xlsx) needs the office sandbox image, which is not on the host. Not run.
- Sandboxing: yolo mode ran unsandboxed inside the container. Crush's krun sandbox path
  and villa's approval table were not tried with Qwen Code.
- `prompt_tokens`/`generated` deltas in the logs are live-server counters and include any
  concurrent use of the stack.
- One run per task and mode: no variance.

## Follow-up (not filed; needs your sign-off)

1. ADR under `docs/adr/`: the privacy claim becomes "villa launches third-party agents only
   with their telemetry switched off and proves it", with the egress capture as the evidence.
2. A repeat of this capture in interactive mode, with `qwen serve` and chat channels confirmed
   unused.
3. If it ships: `TargetQwen` mirroring `internal/agent/claude.go`, villa writing an isolated
   settings file (provider, opt-out keys) under its own config dir and launching with that
   as `HOME`, plus `QWEN_USAGE_STATISTICS_ENABLED=false` appended in the `lockdownEnv` style.

## Interactive mode (#373)

Run 2026-11-03. The headless trial above made no off-box connection with the opt-out set.
The interactive TUI does, so the headless result does not carry over. Harness:
`interactive.py` (pty-driven TUI in a throwaway node:22 container, two real turns including
a shell tool call, `/quit`), `run-interactive.sh`, `run-extra.sh`, `serve.sh`, `sink.js`.
Model: the Heretic Q6_K on a throwaway llama-server on a private podman network (the live
stack was stopped and masked at the time). Each mode starts from an empty `HOME`.
`tcpdump` in the rootless netns records everything except traffic to the model server. A local
TCP sink stands in for the usage-statistics host so its connection is visible instead of
sinkholed (the capture sees the connect and a 1616-byte TLS ClientHello with the SNI, not
the payload: the leg is TLS). The capture's own positive control is the default mode,
which shows all three paths below. `results/interactive/turns.txt`: all ten sessions
answered both turns and exited cleanly.

### Three egress paths, three switches

| Path | What it does | Suppressed by |
|---|---|---|
| Usage statistics | HTTPS to `gb4w8c3ygj-default-sea.rum.aliyuncs.com`, repeated about once a minute | `QWEN_USAGE_STATISTICS_ENABLED=false` **or** `privacy.usageStatisticsEnabled=false` |
| Model catalog | HTTPS to `models.dev` (about 110 KB) on start | `QWEN_CODE_MODELS_DEV_REFRESH=off` only |
| Update check | Lookup and HTTPS to `registry.npmjs.org` | `general.enableAutoUpdate=false` |

| Mode (user settings / env) | RUM | models.dev | npm registry |
|---|:-:|:-:|:-:|
| default | **yes** (126 pkts to the sink) | yes | yes |
| env var only | no | yes | yes |
| `usageStatisticsEnabled=false` only | no | yes | yes |
| all four opt-out settings + usage env | no | **yes** | no |
| user setting `true` + usage env `false` | no | yes | yes |
| all opt-out + `catalogRefresh=false` setting | no | **yes** | no |
| all opt-out + `QWEN_CODE_MODELS_DEV_REFRESH=off` | no | no | no |
| lockdown (all of the above) | no | no | no |

Only the model server's DNS appears in the last two rows (12 packets, all `proto-llama`).

### Findings that change the design

1. **The documented opt-out is not enough.** The docs say disabling usage statistics also
   stops the models.dev refresh. Measured: it does not (row 4). Neither does the documented
   `model.catalogRefresh=false` setting in user scope (row 6). Only the env var does.
2. **A project-level settings file re-enables the telemetry.** With the user-level opt-out
   in place, a `.qwen/settings.json` inside the workspace containing
   `privacy.usageStatisticsEnabled: true` brought back the RUM connection (128 packets to the
   sink). With the usage env var also set, the same session made none. The docs mark
   `usageStatisticsEnabled` as not workspace-restricted, which is this. A cloned repository
   can therefore switch the telemetry on, so **the env var is mandatory, not optional**.
3. **The env var beats a user-level `true`** (row 5), which is the precedence the docs state.
4. **`qwen serve`** under the lockdown env, 40 s idle: zero packets off the model server. It
   binds 127.0.0.1:4170 inside the container with no auth, as documented. Nothing in a
   `villa code` launch starts it.

### Minimal sufficient set

Env: `QWEN_USAGE_STATISTICS_ENABLED=false` and `QWEN_CODE_MODELS_DEV_REFRESH=off`.
Settings (villa-written, user scope): `general.enableAutoUpdate=false`. Add the other three
opt-out keys as belt and braces; they changed nothing measurable here.

### Still not covered

Extension and marketplace fetches, MCP servers, web-fetch and web-search tools (the model can
call them; they are outbound by design), IDE integrations, the feedback dialog (it never
appeared), and any path a newer release adds. The structural no-route launch (#377) is the
answer to the last one.

### Verdict update

Conditions (1) and (2) from the verdict hold, and the first is now met: interactive mode has
two more egress paths than headless, and both are suppressible. The launch contract for #375
is the env pair above, a villa-written settings file, and an isolated `HOME` (so a user-level
file cannot loosen it). A repository's own `.qwen/settings.json` is the reason the env vars
cannot be dropped.

## Crush and Claude Code (#385)

Run 2026-11-03. The same harness as #373 (`agent_capture.py`, `run-agents.sh`, `run-bisect.sh`,
`run-proxy.sh`): pty-driven interactive sessions of two turns including a shell tool call, tcpdump
off the model server, a throwaway llama-server with a dummy API key (villa's real inference
secret never entered an agent). Crush v0.93.1 is the pinned binary run unmodified; Claude Code
is 2.1.317. The contract is villa's own: `lockdownEnv` plus the rendered `crush.json` options, and
`claudeEnv`. Raw captures and per-run dumps: `results/agents/` (`*.pcap`, `*.pcap.txt`,
`bisect.txt`, `proxy.txt`).

### Crush

| Mode | Hosts contacted off the model server |
|---|---|
| default (no kill switches, no `options`) | `data.charm.land`, `hyper.charm.land`, `catwalk.charm.land`, `api.github.com`, CloudFront and Google CDN addresses |
| villa contract | **`api.github.com` only** (DNS plus about 7 TLS packets) |
| contract + project-level `crush.json` re-enabling the options | the same: `api.github.com` only |
| contract, second launch on a settled home | the same |
| contract + `HTTPS_PROXY` at a dead port, `NO_PROXY` the model server (full session, 5 model requests) | **none** |

### Claude Code

| Mode | Hosts contacted off the model server |
|---|---|
| default (`ANTHROPIC_*` only) | `downloads.claude.ai` (a full binary download, about 5800 packets), `api.anthropic.com` (about 340), `raw.githubusercontent.com` |
| villa contract, fresh home | **`downloads.claude.ai` only** (about 470 to 640 packets) |
| contract + project-level `.claude/settings.json` setting the switches to `0` | the same: no loosening |
| contract, second launch on a settled home | **none** (88-byte capture) |
| contract + `DISABLE_UPDATES=1`, fresh home | the `downloads.claude.ai` fetch remains |
| contract + `CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1`, fresh home | **none** |
| contract + dead-port proxy env (full session, 4 model requests) | **none** |

The cause of the surviving `downloads.claude.ai` fetch is inferred from the variable that removes
it (the documentation ties that variable to first-launch marketplace registration); the leg is
TLS and was not decoded. It does not repeat on a settled home, which is the home
`villa code --agent claude` runs on.

### What this settles, and what it does not

- Both contracts are tighter than the Qwen Code one: a repository-level settings file did not
  loosen either. The environment-variable-first rule from #373 was not contradicted.
- One switch was missing (Claude Code's marketplace autoinstall): fixed in PR #388.
- Crush's release check cannot be switched off by anything the contract can set (#389).
- **Not covered:** the agents' own model-callable network tools (Crush's are disabled in
  `crush.json`; villa's Claude Code launch does not disable Claude Code's), extension and MCP fetches, and traffic that appears only
  after a long idle session. Sessions were 185 s.
- **Model compatibility, unrelated to egress:** the Heretic GGUF's chat template raises `System
  message must be at the beginning` when Claude Code sends a system message mid-conversation, so
  `villa code --agent claude` returned HTTP 500 on that model until the check was removed from a
  copy of the template for this run. The first Claude Code capture was discarded for that reason.
  Not filed as a villa bug here: it depends on the catalog entry's template.
- Full-session turn completion is shown by the llama-server request counts only for the proxy
  runs (5 and 4); the earlier contract runs were not counted, and the TUI's diff rendering made
  a text check unreliable. Their start-up traffic is not in doubt; turn-time traffic in those
  runs is less certain than in the proxy runs.
