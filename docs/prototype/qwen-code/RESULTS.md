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
