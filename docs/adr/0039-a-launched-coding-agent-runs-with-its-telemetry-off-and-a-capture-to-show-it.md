---
status: accepted
---

# A launched coding agent runs with its telemetry off, and a capture shows it

The privacy constraint reads "no telemetry from first-party components; outbound
limited to image/model pulls". `villa code` launches third-party agents (Crush, and
Claude Code with `--agent claude`) and passes each a set of kill switches
(`CRUSH_DISABLE_METRICS`, `DISABLE_TELEMETRY`, `DO_NOT_TRACK`, and so on). Nothing
measures what those agents then send. The claim covers villa's own code and is silent on
the agents villa starts, which is where a user's prompts and code live.

The Qwen Code trial (#372, #373, PR #384) is the first time an agent was captured, and it
shows how far a documented opt-out is from a measured one. Qwen Code sends usage
statistics to an Alibaba Cloud RUM host by default, with a local model server configured.
In interactive mode it also fetches the `models.dev` catalog and checks the npm registry
for updates at start. The documentation says disabling usage statistics also stops the
`models.dev` fetch; the capture shows it does not, and that the documented
`model.catalogRefresh=false` setting does not either. Only `QWEN_CODE_MODELS_DEV_REFRESH=off`
does. A repository's own `.qwen/settings.json` containing
`privacy.usageStatisticsEnabled: true` switched the usage statistics back on over a
user-level opt-out; only the environment variable held. Headless mode showed none of the
interactive paths, so a capture of one mode proves nothing about the other.

This is the lesson of ADR-0036 again: env gating covers the leaks someone thought of, and
Qdrant reported telemetry on every memory install from v1.3 to v1.17.1 because nobody had looked.

## Decision

- **Widen the claim, and make it falsifiable.** When this is accepted, the privacy
  wording in `README.md` and `CLAUDE.md` becomes: zero telemetry from first-party
  components; the coding agents villa launches run with their telemetry switched off,
  verified by a network capture for each vetted version; outbound limited to image and
  model pulls. The wording changes with the acceptance and not before: until Crush and
  Claude Code have a capture on record, the wider sentence would be untrue.
- **A launched agent has a launch contract, kept in code.** Per agent, a constant set of
  environment variables and a villa-written configuration, living next to the existing
  kill switches in `internal/agent`. The environment is the control that holds. Settings
  files are belt and braces only, because an agent may read a project-level file from the
  repository the user is working in, and that file is not villa's to trust.
- **The agent runs on a villa-owned configuration root.** An isolated `HOME` (or the
  agent's own config-dir variable), so a user-level file cannot loosen the contract. The
  agent's own files are never read, matching how `crush.json` is already villa's.
- **Every vetted agent version has a capture on record.** A capture is a pty-driven
  interactive session with real turns, including a tool call, under the launch contract,
  recorded off the model server, with a positive control proving the capture can see
  egress. The harness is `docs/prototype/qwen-code/` and becomes a `villa verify` step
  (#376), reusing the rootless-netns egress block `verify memory` and `verify agent`
  already use. A version mismatch with the vetted one is a WARN, not a silent pass.
- **An agent that cannot be locked down is not launched.** If a capture shows traffic the
  contract cannot suppress, the agent stays a documented recipe and gets no `--agent`
  target.
- **What the claim does not cover.** An agent's model-callable tools (web fetch, web
  search) are outbound by design and are not telemetry. Where the agent lets villa turn
  them off, villa does (Crush's are disabled in `crush.json`); where it does not, the
  README says so. Extension and marketplace fetches, MCP servers and IDE integrations
  were not captured for Qwen Code and are out of the claim until they are.

## Consequences

- Each pinned agent version costs one capture per bump. That is the price of a claim a
  user can check, and `villa update` already has the shape for it (prove, mutate, prove).
- Crush and Claude Code were launched on trust. They need a capture before the wider
  wording lands (#385); a capture that finds traffic is a bug in the
  contract, fixed there.
- `--agent qwen` (#375) is unblocked by this ADR and by #373. Its launch contract is the
  environment pair `QWEN_USAGE_STATISTICS_ENABLED=false` and
  `QWEN_CODE_MODELS_DEV_REFRESH=off`, plus `general.enableAutoUpdate=false` in a
  villa-written settings file under an isolated `HOME`.
- The capture is evidence for the version it ran on. A newer release can add a path; the
  per-bump capture is what catches it, and the structural no-route launch (#377) is what
  would make it unnecessary.

## Considered and not chosen

- **First-party only.** Leave the claim as written and call third-party agents out of
  scope. Cheapest, and honest, but it leaves the single largest holder of user data
  outside the promise, and Qwen Code could not be offered at all.
- **Opt-out, proof and a no-route launch.** Agents run where only the model server is
  reachable, so a missed switch or a new upstream path cannot leave the box. The strongest
  claim, but it removes agent web tools and adds a launch path for host-run agents. Not
  rejected: it needs its own design interview (#377), and the per-bump capture stays
  valuable either way.

## Evidence

`docs/prototype/qwen-code/RESULTS.md`, sections "Egress" and "Interactive mode (#373)";
raw captures in `docs/prototype/qwen-code/results/`. Related: ADR-0036 (closed network),
ADR-0019 (villa owns the agent configuration), #375, #376, #377.
