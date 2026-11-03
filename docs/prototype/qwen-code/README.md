# Prototype: Qwen Code as a third `villa code` agent (issue #372)

**PROTOTYPE — throwaway.** Nothing here ships. Findings are in `RESULTS.md`.

## Question

Could [Qwen Code](https://github.com/QwenLM/qwen-code) be an optional `villa code --agent qwen`
target next to Crush and Claude Code, without breaking the zero-telemetry constraint, and does
it do the cowork document tasks as well as Crush did?

## Shape

Qwen Code 0.26.6 headless (`qwen -p … --approval-mode yolo`) in a throwaway `node:22-slim`
container on the `villa` network, an isolated `HOME` and npm prefix, talking to the LIVE
`villa-llama` by name with the stack's key. Nothing in the live stack's config or units was
touched. Tasks T1–T5 are `../cowork/tasks`. `tcpdump` in the rootless netns records everything
the container sends off the villa subnet, plus its DNS queries.

```
./seed.sh                                   # (re)create workspace/ — WIPE ME
PREFIX=… QHOME=… ./run.sh optout [T1 …]     # opt-out settings + QWEN_USAGE_STATISTICS_ENABLED=false
PREFIX=… QHOME=… ./run.sh default [T1 …]    # provider config only, nothing else
./control.sh                                # positive control: proves the capture can see egress
```

`PREFIX` is an npm prefix with `@qwen-code/qwen-code` installed (`npm i -g --prefix`), `QHOME`
an empty directory used as `HOME`.
