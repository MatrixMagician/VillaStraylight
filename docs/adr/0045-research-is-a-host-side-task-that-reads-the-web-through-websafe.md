---
status: proposed
---

# Research is a host-side task that reads the web through websafe

Chat web search works today: Open WebUI asks SearXNG, hands the result URLs to
`villa-websafe`, and puts the guarded pages in front of the model. It is a chat
feature, and it answers one question in one turn. #408 asks for a bounded research
task the operator can start from a terminal: search, read, search again from what was
read, write a report with numbered sources, and have `grounding` audit the report. The
maintainer decided on 2026-11-14 to build it as a host-side runner, blocked by the
per-source taint ([ADR-0040]). This ADR records that decision, the options it beat, and
the questions the build had to settle.

## Options

- **Host-side runner.** The loop runs in `taskrun` inside `villa-dashboard.service`. It
  searches through SearXNG, reads through `villa-websafe`, and asks the served model
  through the inference client. There is no task VM, no file tool and no shell, so
  injected content has nothing to act on except the text of the report.
- **A search proxy into the sandbox VM.** Give the workspace agent a web route and let
  Crush research. It puts a web-reading model in the one place that has file tools, a
  shell and a workspace, and it needs a new egress path across the `Internal=true`
  sandbox network ([ADR-0011]). Rejected: it is the case [ADR-0040] exists to make
  expensive, and it widens the VM's route instead of removing the thing an injection
  could act on.
- **An Open WebUI tool only.** A research tool on the tool server (#313) or an OWUI
  pipeline. The report would live in a chat, `grounding` could not audit it, there is no
  task record, exit code or budget, and a second client would have to reach SearXNG.
  Rejected, and listed out of scope in #408.

## Decision

- **Surface.** `villa research "<question>"`, a loopback client of the dashboard-hosted
  runner like `villa work` and `villa task`, with `--rounds`, `--minutes` and `--detach`.
  Submit is `POST /api/research`; follow, show, list and cancel are the existing task
  routes.
- **One new package, `internal/research`**, the pure round loop behind a `Deps` struct. It
  does no host I/O; the runner binds it. A research task is a task: the same record, the
  same state machine, the same `taskstore.Exit`, one at a time.
- **The task is tainted before the first fetch**, `Runner.Taint(id, approval.TaintWeb)`.
  The taint has nothing to gate inside a research task (it has no write or execute
  action). It is on the record because a report is web-derived text, and anything that
  later takes a research task as input must be able to see where it came from.
- **Fetched text reaches the model only inside the websafe fence, in a user-role
  message.** Search results contribute URLs and nothing else: no snippet and no result
  title enters any prompt. The model never sees a URL either. It cites `[n]`, and the host
  renders the number-to-source list from its own data.
- **Queries leave the box through SearXNG exactly as chat web search does.** The engine
  allowlist, the egress bound `villa verify search` proves, and the privacy wording do not
  change. A research task sends more queries than a chat turn, up to the budget below.

## Questions settled

**1. How does the runner reach SearXNG and websafe?** Neither publishes a host port;
both are containers on `villa.network`. The runner lives on the host.

- *In-network helper calls (chosen).* The existing doctrine: a fixed-arg `podman run --rm`
  of the probe-helper curl image on the target's network (`probeCurl`, `internal/inprobe`).
  Voice, extract and the memory proof already use it. No unit changes, no new port, no
  new threat-model row. The cost is a container start per call, about a second, which is
  noise against a model call. The helper is not a pin ([update spec] §7.1), so it
  does not drag the update path in. One difference from the probes: nothing goes on
  curl's argv. The question, the search query and the websafe bearer all travel in a curl
  config file read from stdin (`-K -`), so none of them is readable from `/proc` by
  another local user (#252's reason for `-H @-`).
- *A new loopback publish.* Publish SearXNG or websafe on `127.0.0.1`. It changes two
  units and their goldens, puts a fetch-anything service on the host's loopback where any
  local process can reach it, and the `villa-websafe` bearer is no longer the only guard.
  Rejected: the in-network path costs nothing on the threat model and this costs a row.
- *Run the loop inside a container on `villa.network`.* A third bind-mounted `villa`
  subcommand like `websafe-serve`. It removes the per-call container start and adds a unit,
  a registry entry, an image pin question and a second long-lived process with the
  inference key. Rejected as more than the feature needs.

`verify search` does not drive a search (it asserts the egress bound from a throwaway
network namespace), so there is nothing in it to reuse. The websafe call is the contract
Open WebUI makes: `POST /load` with `{"urls":[...]}` and `Authorization: Bearer
<web_loader_secret>`, answered with `page_content` (already fenced) and
`metadata.{source,title,guard.{detected,rules}}`. The SearXNG call is
`GET /search?q=<q>&format=json`, which the rendered `settings.yml` allows (`formats: html,
json`).

**2. The task record.** `kind` is a new omitted-when-empty field; empty means a workspace
task, so every record written before it loads unchanged and serialises to the same bytes.
`kind: "research"` carries a `research` object, nil for every other task:

```
"kind":"research",
"research":{"budget":{"rounds":2,"minutes":10},
            "report":"tasks/<id>.report.txt",
            "sources":[{"n":1,"url":"...","title":"...","flagged":true,"rules":["..."]}]}
```

`workspace` is `""` and `mode` is `""`: a research task has no grant and no approval.
`instruction` is the question. `harness` is `{"name":"research","version":"1"}`, where
the version is the loop's prompt version, so a record says which prompts wrote it. The
report is a file at `<data root>/tasks/<id>.report.txt`, written through `pathsafe` by
`taskstore.Store.WriteReport`, never deleted with the record. `villa task show` prints the
absolute path and `villa research` prints the report from it. There is no report route,
for the reason the log has none: the report is a host-local file, and a client that wants
it reads it (a route is a later decision, not a precondition). The schema version stays 1,
as in [ADR-0040]: the fields are omitted for every existing record and no v1 reader's
reading of a workspace task changed. The `--json` goldens for existing records are
byte-identical; the research record and the list row get new goldens.

**3. The round loop.** `research.Run(ctx, Deps, Request) Result`, pure.

```
Deps{ Search(ctx, query) ([]string, error)           // result URLs, nothing else
      Fetch(ctx, urls) ([]Page, error)               // websafe /load
      Complete(ctx, llm.ChatRequest) (string, error) // inference.Client
      Audit(ctx, grounding.Document, []grounding.Source) grounding.DocumentReport
      Taint() error; Narrate(string); Now() time.Time }
```

Round 1 plans the queries from the question alone (a system prompt, no web content). Each
round then searches, fetches the new URLs, and reads: the question, then one user message
per page, each holding only the page's `[n]` label and the fenced content, and the model
answers with notes ending in `[n]` and up to two `FOLLOWUP:` queries. Rounds 2 and on use
the previous round's follow-ups; none ends the loop early. The report call gets the
question and the notes, and cites `[n]`. The host appends the source list. A planning
answer with no `QUERY:` line falls back to the question as the one query, narrated. The
loop checks, before the model is asked to cite anything it was not shown, that each `[n]`
in the report names a fetched source and that the report cites at least one. Those
deterministic findings join the grounding audit's unsupported claims; neither replaces the
other. The core sets the taint once, immediately before the first `Fetch`, and a taint it
cannot write ends the task `failed`: web content does not reach the model on a record that
could not be marked.

The notes are the model's words about attacker text, so an injection can steer them, and
through them the report. That is the residual: the report can be wrong in the direction a
page pushed it, and the operator is shown the source list and the websafe flags to judge
it. The audit checks the report's claims against the fenced page text, with the
same blind spot the grounding spec states: the auditor is the model that wrote the report.

**The report is plain text, its body is flagged and defanged, and villa's list sits behind a
token the body cannot contain.** The report is model output produced after reading
untrusted pages. That makes it a route for the markdown-image exfiltration residual of
[ADR-0002] (an image whose URL carries data, fetched by whatever renders the file, outside
every container) and for a body that impersonates villa's own source list. Three controls,
all in the pure core (`research/defang.go`, `report.go`), none of which depends on how a
viewer parses Markdown:

- *Flag first.* `inspect` normalises the body with the page guard's own NFKC and
  invisible-character removal (`websafe.Normalize`), so a fullwidth `ｈｔｔｐｓ：／／` or a
  zero-width character spliced into `https://` cannot hide a scheme. It then looks for substrings,
  with no word boundary and no Markdown structure: `://`, `www.`, a `//host` token, `<`
  followed by a letter, `/`, `!` or `?`, `](`, `![`, `]:`, and the `javascript:`,
  `vbscript:` and `mailto:` schemes. Any hit ends the task `flagged`, names the kinds in the
  reason, and puts one finding per kind (with a defanged snippet) in the record's grounding.
  The check is conservative on purpose: a body that says `and//or` is flagged too.
- *Plain text, defanged.* The report is `tasks/<id>.report.txt`. A text file is not
  rendered, so nothing in it loads. The body is written from the normalised text with the
  shapes above broken as a belt for a viewer that renders it anyway: in one pass every
  backslash is doubled and then every escape villa adds is emitted (`://` becomes `:/\/`,
  any other `//` becomes `/\/`, `www.` becomes `www\.`, a tag-opening `<` and every
  non-citation square bracket are backslash-escaped). Backslashes go first, in the same
  pass, so a backslash the model wrote cannot cancel one of villa's. The audit is shown
  this defanged text, the text that was written.
- *An unforgeable list.* villa's source list is the last thing in the file, after a delimiter
  line, `----- villa sources <token> -----`, whose token is 128 bits from `crypto/rand`
  made per report (`Deps.Token`; the page fence's nonce is the same idea, but it is not
  exported). A body that contains the token is flagged and has it removed. No heading, list,
  quote or fence in the body can stand in for a line it cannot spell, so forging the list
  does not depend on any heading style or container marker.

Citations stay `[n]`, including adjacent ones, unless one is followed by `(` or `:`, which
is escaped. A `[n]` whose n is not a fetched source, or a report that cites nothing, is a
grounding finding and flags the task.

**Why not a Markdown sanitiser.** The first version of this guard rewrote the body as
Markdown: removed links and images, escaped brackets, and demoted headings that read as a
source list. Review found three bypasses of it, all the same fault, a regular expression
racing a Markdown parser: a model-written backslash cancelled villa's bracket escape
(an escaped bracket followed by a protocol-relative destination stayed a live link); a
container marker hid a forged heading (`> ## Sources`, `- ## Sources`); and a `\b`-anchored
URL pattern missed a scheme glued to emphasis (`_https://host_`) that autolinkers still
link. Each patch would invite the next, because the set of things a viewer might act on is
the viewer's, not ours. Flagging is decided on the text, before any viewer, and a plain-text
file has no parser to race.

**Residual.** None of this makes the report true. Prose can still mislead in the direction a
page pushed it, a model can still write a persuasive false claim under a valid `[n]` marker,
and a viewer that linkifies plain text, or a person who retypes a defanged URL, can still
reach an address the body named. The report is model output over untrusted input. villa
flags and defangs what it finds and never claims the report is safe.

**4. Budgets and caps.** Printed at start, from the record.

| Knob | Default | Hard cap | Set by |
|---|---|---|---|
| Rounds | 2 | 4 | `--rounds` |
| Soft time budget | 10 min | 30 min | `--minutes` |
| Queries per round | 3 | fixed | the loop |
| Pages fetched per round | 4 | fixed | the loop |
| Page text per source | 8 KiB | fixed | `websafe.ClipFenced` |
| Sources in total | rounds x 4 | 16 | derived |
| Per-call timeouts | search 30 s, fetch 90 s, completion 5 min | fixed | the loop |

A flag above its cap is refused in the terminal and again by the runner. The time budget is
soft: no new round starts after it passes, a call in flight finishes under its own timeout,
and the report and the audit always run on what was read. The page cap is a first
estimate chosen so the audit call, which sees every source, stays near 16k tokens at the
defaults. It is not measured, and the on-hardware proof is where it gets tuned. The clip
keeps the fence intact (`websafe.ClipFenced` cuts the text between the nonced delimiters
and returns nothing for text it cannot recognise as a fence, so an unframed page is
dropped, never sent).

**5. Terminal states.** No new state and no new exit code.

| Ends | When | Exit |
|---|---|---|
| `done` | grounding checked and clean, every citation resolves | 0 |
| `flagged` | the audit found unsupported claims or could not run; a citation names no source; the report cites nothing; the report carried a URL, link, image, tag or `//host` that villa defanged; the search returned no results; no page survived the loader; every source was flagged by websafe | 2 |
| `failed` | no search, loader or model reachable on round 1; a model call returned nothing; the taint could not be written | 1 |
| `refused` | at submit: web search is off, or the task is malformed | 1 |
| `cancelled`, `interrupted` | the operator, or the service stopping | 1 |

An empty answer and an unreachable one are different outcomes. A SearXNG that answers with
no results is `flagged` ("zero sources"), a SearXNG that cannot be reached is `failed`, and
a later round that loses a service ends the loop and reports on what was read. **This
differs from #408's done-when line**, which asks for the web-search-unreachable run to end
`flagged`. This ADR ends it `failed`: exit 2 means "finished, needs a look", and a stack
that could not search has finished nothing. The maintainer settles it when accepting this
ADR; the change is one case in `research.Run`.

**6. Gating.** `villa research` and the runner's `Submit` require `subsystem.WebSearchOn`
and refuse with `villa install --web-search`. It does not require the workspace agent: a
research task has no sandbox, no tools mode and no workspace. It does use the runner that
`dashboard.go` builds only `if subsystem.SandboxOn(cfg)` at startup, so the runner is now
built when either gate is on. Assumed: a nil runner is the "not running" state the routes
already answer with 503, and `Submit` re-reads the config per request, so a gate that
changed after the dashboard started is refused honestly rather than trusted. The
`fix/418-dashboard-gate-after-apply` branch moves the workspace gate to a per-request
`TasksOn` check; the design holds under either shape, provided that check admits a
research-only host (`TasksOn` true when web search is on), which is the one line that
branch's author has to know. The workspace routes keep refusing a submission on a host with
tools mode off, as a `refused` record, exactly as before.

**7. Restart.** A research task interrupted by a dashboard restart is not resumed.
`Runner.Recover` marks it `interrupted` once, as for any task, and spec §3.2 stands:
re-running is the operator's command. Nothing about the loop is cheap to resume honestly (a
model's reading is not a checkpoint), so the narration already written to the task's log
is what survives, with the queries asked and the sources fetched. Recovery skips the
container kill for a research task, which has none. A stop of the service mid-task
cancels the job's context the way `Close` does today, ending the record `interrupted`; a
probe helper container that outlives its caller is bounded by its own `--max-time`.

## Consequences

- The runner gains a second kind of job and `dashboard.go` a changed gate; the workspace
  path, its approvals and its sandbox are untouched, and every existing golden holds.
- A research run costs up to 3 queries and 4 page fetches per round, a model call per
  query plan, per round and for the report, and one audit. Chat search remains the cheap
  path.
- The round budget bounds how much outbound traffic a question can cause; it is also what
  the operator is told before it starts.
- The in-network helper puts a podman container start on every search and every fetch. If
  the on-hardware proof finds that too slow, the third option above is the follow-up, as its
  own ADR.
- It is a pattern, not a proof, like the deletion rule and the taint: the fence and the
  flags reduce what an injection does, and the report remains text a person has to read.

[ADR-0002]: 0002-the-web-guard-flags-injection-it-never-blocks.md
[ADR-0011]: 0011-the-sandbox-reaches-inference-through-a-v1-proxy.md
[ADR-0040]: 0040-web-content-in-a-task-taints-it-per-source.md
[update spec]: ../spec/v1.8-villa-update.md
