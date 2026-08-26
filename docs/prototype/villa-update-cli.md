# Paper prototype: the `villa update` CLI surface

For [Prototype: the villa update CLI surface and output](https://github.com/MatrixMagician/VillaStraylight/issues/90), on the map [v1.8 — villa update](https://github.com/MatrixMagician/VillaStraylight/issues/83).

**These are mock transcripts, not implementation.** Every digest, timing and version below is invented except where it reflects the real drift [Research](https://github.com/MatrixMagician/VillaStraylight/issues/85) measured on 2026-08-26. React to the shapes; the decisions they encode are already closed.

## What the closed tickets force

Before the transcripts, the constraints they have to satisfy:

| Decision | Source | Consequence for output |
|---|---|---|
| Working set is the **installed footprint** | [#84](https://github.com/MatrixMagician/VillaStraylight/issues/84) | rows only for what this host runs; skipped things say why |
| Only the **active backend** | [#84](https://github.com/MatrixMagician/VillaStraylight/issues/84) | three backend images are never rows |
| **Vetted vs effective** pin | [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86) | two pins per component, and they can differ |
| Manifest **serial + `valid_until`** | [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86) | a downgrade is a loud refusal; expiry is silence |
| villa **reported, never applied** | [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86) | one row that deliberately cannot be acted on |
| **Per-subsystem** sequencing, halt on first failure | [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) | progress is a sequence, and it can stop midway |
| Proof runs **before and after** | [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) | two proofs narrated per subsystem |
| **Reject ≠ Fail** | [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) | "cannot show it is" wording, never "is broken" |
| Keep **one previous**, reference-counted prune | [#88](https://github.com/MatrixMagician/VillaStraylight/issues/88) | prune can no-op; that must be visible |
| Prune failure is a **WARN** | [#88](https://github.com/MatrixMagician/VillaStraylight/issues/88) | must not read as a failed update |
| **Silence over fingerprinting** | [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87) | "could not check" ≠ "nothing available" |
| **On-command only** | [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87) | passive surfaces show a cached age |

## Exit codes

Reuses `doctor`/`preflight`'s established vocabulary rather than inventing one — `cmd/villa/doctor.go`: *"a confident BLOCK-class FAIL → exitBlocked (=1); any WARN / drift / typed-Unknown → exitWarn (=2); all healthy → exitPass (=0)"*.

| Code | `villa update --check` | `villa update` (apply) |
|---|---|---|
| `0` | checked; everything at its vetted pin | every attempted subsystem updated and proven |
| `2` | checked; updates are available | updated, with a WARN (e.g. prune could not reclaim) |
| `1` | **could not check** (no valid manifest, refused downgrade) | a subsystem failed its proof and rolled back, or the run was refused |
| `130` | Ctrl-C | Ctrl-C |

**The `2`-means-updates-available choice is deliberate and worth reacting to.** It makes `villa update --check` usable in a script (`if villa update --check; then` is "you are current"), and it matches `doctor`'s "something wants your attention" reading of `2`. The alternative — `0` whether or not updates exist — makes the common case scriptless.

**Note the asymmetry:** for `--check`, `1` means *villa could not answer*. For apply, `1` means *villa answered and it went wrong*. Both are "confident BLOCK-class", but a reader could reasonably expect `--check` to exit `1` when updates exist. It does not.

---

## 1. `villa update --check` — the ordinary case

Everything current except two subsystems. Reflects the real 2026-08-26 drift.

```
$ villa update --check

Pin manifest   serial 41, published 2026-08-19, valid until 2026-11-19
Last checked   just now

COMPONENT          EFFECTIVE                  AVAILABLE                  STATE
inference (rocm)   rocm-7.2.4  2da150c1       rocm-7.2.4  1e666bff       rebuilt
chat               open-webui  7f1b0a1a       open-webui  c7929533       rebuilt
memory             qdrant      b79aaa49       —                          current
                   embedder    9a74e555       —                          current
search             searxng     ed29454e       —                          current
                   websafe     b669b9df       —                          current
agent              crush       v0.76.0        crush       v0.91.1        new version
villa              v1.7                       v1.8                       new version (not applied by update)

2 subsystems have updates available; 1 new villa release.

  villa update                 apply to inference, chat and agent
  villa update memory          apply to one subsystem
  villa update --dry-run       show what would happen, change nothing

villa is updated by replacing the binary, not by this command:
  make build-static && install -m 0755 ./villa ~/.local/bin/villa

exit 2
```

**Choices to react to:**

- **"rebuilt" vs "new version"** is the distinction [Research](https://github.com/MatrixMagician/VillaStraylight/issues/85) insisted on. `rocm-7.2.4` moving digest is *the same declared version, rebuilt upstream* — not a version bump. Flattening both into "update available" would be the dishonest shortcut.
- **Short digests (8 chars).** Full digests are unreadable in a table. The `--json` output carries them in full.
- **The `villa` row is deliberately awkward** — [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86) decided report-but-never-apply, so the row states that inline and the remediation is printed separately. **This is the weakest part of the design and the most worth your reaction.** It may be better as a separate stanza below the table rather than a row that lies about being actionable.
- **Vetted is absent from this view.** With effective == vetted for every row here, showing a third column would be noise. It appears only when they diverge (§2).

## 2. `--check` after a partial update — vetted and effective diverge

```
$ villa update --check

Pin manifest   serial 41, published 2026-08-19, valid until 2026-11-19
Last checked   just now

COMPONENT          VETTED                     EFFECTIVE                  STATE
inference (rocm)   rocm-7.2.4  1e666bff       rocm-7.2.4  1e666bff       current
chat               open-webui  c7929533       open-webui  7f1b0a1a       update available
memory             qdrant      b79aaa49       qdrant      b79aaa49       current
                   embedder    9a74e555       embedder    9a74e555       current
search             —                          —                          skipped: web search disabled
agent              crush       v0.91.1        crush       v0.76.0        update available

2 subsystems have updates available.
1 subsystem skipped — see `villa update --check --all` to list skipped detail.

exit 2
```

**Choices to react to:**

- **The third column appears only when it earns its place.** Divergence is the interesting state, so the table grows a column exactly when there is something to see.
- **Skipped rows are present but empty.** [#84](https://github.com/MatrixMagician/VillaStraylight/issues/84) decided disabled components are skipped *with a stated reason*; omitting the row entirely would hide the decision. The alternative is a footnote, which is quieter but easier to miss.

## 3. `--check` with no valid manifest — the Reject

The state [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87) cares most about. **This must not read like "you are up to date".**

```
$ villa update --check

Could not check for updates.

  No valid pin manifest. The last one villa holds expired on 2026-05-19
  (serial 39). Without it, villa does not know what newer pins exist.

  This is not "you are up to date" — villa could not determine anything.

Your stack is running the pins villa shipped with, which were vetted on
gfx1151 hardware. Nothing is wrong; nothing was checked.

  villa update --check --from-registries
      Ask each registry directly instead. This contacts one endpoint per
      installed component, which reveals to those registries which addons
      you have enabled. The manifest check does not.

Last successful check: 2026-04-02 (146 days ago), serial 39.

exit 1
```

**Choices to react to:**

- **The phrase "This is not 'you are up to date'" is doing real work.** It names the misreading and refuses it. Verbose, deliberately — this is a Reject and the cost of being misread is a user believing they are current for months.
- **The opt-in states its cost inline**, per [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87)'s requirement that `--from-registries` surface the fingerprint consequence at the point of use, not only in docs.
- **"146 days ago"** exists because [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87) made staleness the honest signal in place of automation.

## 4. `--check` refusing a downgrade

A signed manifest with a serial below the recorded floor. [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86): refuse loudly.

```
$ villa update --check

Refused the pin manifest.

  Fetched manifest has serial 38; this host has already seen serial 41.
  A validly-signed manifest that goes backwards is how a downgrade attack
  looks, so villa refuses it rather than acting on it.

  The signature verified. The content is older than what you have.

No pins were changed. Your stack is untouched.

  If you believe this is legitimate (for example, you deliberately
  republished an older manifest), the recorded floor lives in the
  effective-pin store and clearing it is a manual, deliberate act.

exit 1
```

**Choices to react to:**

- **"The signature verified. The content is older."** Separating these matters — a user who sees "refused" may assume a broken signature and go looking for the wrong problem.
- **No automatic override flag.** A `--allow-downgrade` would be the obvious affordance and is deliberately absent: an attacker who can serve you a manifest can also read your docs. Making it a manual store edit keeps it deliberate.

## 5. `villa update` — the apply flow

Two subsystems, both proven, one prune shared. The narration [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) requires: two proofs per subsystem.

```
$ villa update

Pin manifest   serial 41, published 2026-08-19, valid until 2026-11-19
Updating       inference, chat        (2 of 5 subsystems; 3 already current)

inference (rocm-7.2.4)
  proving current state ............................ pass    (18.4s)
  capturing rollback point ......................... done
    unit villa-llama.container, config, digest 2da150c1
  pulling rocm-7.2.4 1e666bff ...................... done    (4.2 GB, 3m11s)
  restarting villa-llama ........................... done
  proving new state ................................ pass    (21.7s)
    generation probe ok; residency proof: 2 signals
  committing effective pin ......................... done
  pruning previous ................................. retained
    2da150c1 kept as the known-good previous

chat (open-webui)
  proving current state ............................ pass    (2.1s)
  capturing rollback point ......................... done
  pulling open-webui c7929533 ...................... done    (1.1 GB, 47s)
  restarting villa-openwebui ....................... done
  proving new state ................................ pass    (6.9s)
  committing effective pin ......................... done
  pruning previous ................................. removed
    7f1b0a1a released; b3c81f22 (two versions back) removed, 1.1 GB reclaimed

Updated 2 subsystems. 3 were already current.
A newer villa (v1.8) is available; this command does not apply it.

exit 0
```

**Choices to react to:**

- **"proving current state" comes first**, per [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89)'s prove-before-mutate. It has to be visibly *before* the capture so it does not read as part of the update.
- **Prune has two distinct outcomes on screen** — `retained` (this is the new known-good previous) and `removed` (the one before it, now unreferenced). [#88](https://github.com/MatrixMagician/VillaStraylight/issues/88)'s one-previous rule is legible from the output alone.
- **The residency proof's cost is not hidden.** 18.4s + 21.7s for inference — ADR-0001 calls the proof "the expensive part", and the output owns it.
- **The villa line is a footer**, not a row. Compare §1's table row. **Which is better is a real open question for you.**

## 6. Apply, halting on a Reject — the marker-drift case

The failure ADR-0001 predicted: a rebuilt image whose logs no longer match villa's markers. [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) decided this is a Reject that rolls back.

```
$ villa update

Pin manifest   serial 41, published 2026-08-19, valid until 2026-11-19
Updating       inference, chat, memory   (3 of 5 subsystems)

inference (rocm-7.2.4)
  proving current state ............................ pass    (18.1s)
  capturing rollback point ......................... done
  pulling rocm-7.2.4 1e666bff ...................... done    (4.2 GB, 3m04s)
  restarting villa-llama ........................... done
  proving new state ................................ REJECT  (31.2s)

    The new image could not be proven.

    Residency markers were not found in the server's startup log. The
    llama.cpp log format changes as the upstream image is rebuilt, and
    villa's markers are pinned to the format it was tested against.

    This image may be perfectly fine. Villa cannot show that it is, and
    will not commit a pin on evidence it does not have.

  rolling back ..................................... done
    unit, config and digest 2da150c1 restored verbatim
  re-proving restored state ........................ pass    (17.9s)

Stopped. chat and memory were not attempted.

  Your stack is running exactly what it was before this command.
  Nothing was committed.

  Report the marker drift — a vetted pin that cannot be proven is a
  release-blocking defect, not a local misconfiguration:
  https://github.com/MatrixMagician/VillaStraylight/issues/new

exit 1
```

**Choices to react to:**

- **"This image may be perfectly fine."** [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89)'s wording verbatim. A Reject is not a Fail, and the user must not conclude upstream shipped a broken image.
- **Re-proving after rollback.** The restored state is proven, so "rolled back" is a demonstrated claim rather than an assumption. ADR-0003 demands honesty when rollback is incomplete; proving it is how that is earned.
- **"chat and memory were not attempted"** — [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89)'s halt-on-first-failure made visible, so the user knows the scope of what did not happen.
- **Pointing at the issue tracker is unusual for this CLI.** Justified here because marker drift is genuinely a defect in the shipped pin, not something the operator can fix. React to whether that is a step too far.

## 7. Apply, halting mid-sequence with earlier subsystems committed

The state a user must reason about afterwards.

```
$ villa update

Updating       inference, chat, memory, agent   (4 of 5 subsystems)

inference (rocm-7.2.4)
  ... pass; committed; previous retained

chat (open-webui)
  ... pass; committed; previous retained

memory (qdrant + embedder)
  proving current state ............................ pass    (3.4s)
  capturing rollback point ......................... done
  pulling qdrant b3f9021a .......................... done    (312 MB, 22s)
  pulling embedder f0c7b61f ........................ done    (2.8 GB, 2m14s)
  restarting villa-qdrant, villa-embed .............. done
  proving new state ................................ FAIL    (12.0s)
    verify memory: upload succeeded, retrieval returned no citation
  rolling back ..................................... done
  re-proving restored state ........................ pass    (3.6s)

Stopped after 2 of 4 subsystems.

  COMMITTED    inference    rocm-7.2.4  1e666bff
               chat         open-webui  c7929533
  ROLLED BACK  memory       qdrant b79aaa49, embedder 9a74e555
  NOT TRIED    agent

  The two committed subsystems were each proven before commit and are
  running normally. Re-run `villa update` after investigating memory;
  the committed subsystems will be skipped as already current.

exit 1
```

**Choices to react to:**

- **The three-state summary block** is the most important output in this document. It is the answer to "what state am I in?", which the user will ask immediately.
- **This is a FAIL, not a Reject** — `verify memory` ran and the property did not hold, so the wording is confident where §6's was careful.
- **Reassurance is explicit.** Exit `1` on a run that committed two subsystems could read as "everything is broken". It is not, and the output says so.

## 8. Refusals before anything happens

### Stopped stack

[#89](https://github.com/MatrixMagician/VillaStraylight/issues/89): apply requires a running stack; `--check` does not.

```
$ villa update

Refused: the stack is not running.

  villa update proves each subsystem before and after it changes it, and
  it cannot prove a subsystem that is not running.

  villa up          start the stack, then re-run
  villa update --check   works on a stopped stack — it changes nothing

exit 1
```

### Already-unhealthy subsystem

[#89](https://github.com/MatrixMagician/VillaStraylight/issues/89): a pre-existing failure is a refusal, **not** an update failure.

```
$ villa update

Refused: memory is not healthy right now.

  verify memory failed before any update was attempted. Nothing has been
  changed.

  Villa refuses rather than reporting an update failure it did not cause,
  and rather than rolling back to a state that was already broken.

  villa doctor      diagnose the current stack

exit 1
```

**Choices to react to:**

- **"an update failure it did not cause"** is the whole point of [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89)'s prove-before-mutate, surfaced in one line.
- **It points at `doctor` and does not diagnose**, keeping the boundary [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) drew: `doctor` diagnoses, `update` refuses.

### Missing rollback protection

[#88](https://github.com/MatrixMagician/VillaStraylight/issues/88): a recorded previous that is gone from disk is surfaced, not fail-softed.

```
$ villa update

Warning: rollback protection is incomplete.

  The known-good previous image for chat (7f1b0a1a) is recorded but is no
  longer in the image store — something removed it outside villa.

  Updating chat is still safe: villa captures a fresh rollback point
  before it changes anything. But the older fallback is gone.

Continue? [y/N]
```

**Choice to react to:** this is the **one prompt** in the whole surface. Everything else refuses or proceeds. It could equally be a WARN that proceeds unattended, since the fresh capture still happens — the prompt may be over-cautious. **Worth your reaction**, especially since a prompt breaks non-interactive use.

## 9. `--dry-run`

```
$ villa update --dry-run

Pin manifest   serial 41, published 2026-08-19, valid until 2026-11-19

Would update 2 of 5 subsystems, in this order:

  1. inference (rocm-7.2.4)
       pull    rocm-7.2.4 2da150c1 → 1e666bff        ~4.2 GB
       prove   generation probe + residency proof     ~40s (runs twice)
       retain  2da150c1 as the known-good previous
       prune   nothing — b3c81f22 is still referenced by the embedder

  2. chat (open-webui)
       pull    open-webui 7f1b0a1a → c7929533         ~1.1 GB
       prove   protocol probes                        ~14s (runs twice)
       retain  7f1b0a1a as the known-good previous
       prune   b3c81f22, 1.1 GB reclaimed

Skipped:
  memory, search    already at the vetted pin
  agent             disabled (agent_enabled = false)

Estimated download 5.3 GB. Nothing has been changed.

exit 0
```

**Choices to react to:**

- **The reference-counted prune is visible before it happens** — "nothing: still referenced by the embedder" is [#88](https://github.com/MatrixMagician/VillaStraylight/issues/88)'s shared-digest hazard rendered as a plain sentence, and it pre-empts "why is the old image still there?".
- **"runs twice"** makes the doubled proof cost explicit rather than a surprise in the timings.
- **Download total up front**, since 5.3 GB is a decision input on a metered connection.

## 10. `--json`

Machine shape for `--check`. Full digests here, unlike the tables.

```json
{
  "schema_version": 1,
  "checked_at": "2026-08-26T14:31:07Z",
  "manifest": {
    "state": "valid",
    "serial": 41,
    "published_at": "2026-08-19T00:00:00Z",
    "valid_until": "2026-11-19T00:00:00Z",
    "signature": "verified"
  },
  "subsystems": [
    {
      "name": "inference",
      "state": "update_available",
      "components": [
        {
          "name": "llama-server",
          "kind": "image",
          "pin_shape": "version_tag",
          "vetted": "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89",
          "effective": "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89",
          "available": "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:1e666bff85b85e1fc877531c51ea7459a40429c8e48daee1b99eaece1c90c247",
          "change": "rebuilt",
          "previous_retained": null
        }
      ]
    },
    {
      "name": "search",
      "state": "skipped",
      "skip_reason": "web_search_enabled = false",
      "components": []
    }
  ],
  "villa": {
    "current": "v1.7",
    "available": "v1.8",
    "change": "new_version",
    "applied_by_update": false
  },
  "summary": { "updatable": 2, "current": 2, "skipped": 1 }
}
```

The Reject case, which a script must not mistake for "current":

```json
{
  "schema_version": 1,
  "checked_at": null,
  "manifest": {
    "state": "expired",
    "serial": 39,
    "valid_until": "2026-05-19T00:00:00Z",
    "signature": "verified"
  },
  "result": "could_not_check",
  "last_successful_check": "2026-04-02T09:12:44Z",
  "subsystems": [],
  "summary": null
}
```

**Choices to react to:**

- **`"result": "could_not_check"` with `"summary": null`** — a script reading `summary.updatable` gets a null rather than a `0` that reads as "current". The absent-is-not-zero discipline `verifystate` already applies.
- **`"change"` is an enum**, not a boolean: `rebuilt` / `new_version` / `none`. The `--json` consumer gets the same distinction the human does.
- **`pin_shape`** is exposed so a consumer can tell why a `rebuilt` is not a version bump.

## 11. Per-component selection

```
$ villa update memory
$ villa update inference chat
$ villa update --all
```

Arguments are **subsystem** names, not container names, per [#89](https://github.com/MatrixMagician/VillaStraylight/issues/89) (the proof unit is the verify verb's scope). `villa update qdrant` is rejected:

```
$ villa update qdrant

Unknown subsystem "qdrant".

  qdrant is part of the memory subsystem, which villa updates as a unit
  because verify memory proves Qdrant and the embedder together.

  villa update memory

Subsystems: inference, chat, memory, search, agent

exit 1
```

**Choice to react to:** the error teaches the model rather than just rejecting. The allowlist from [#86](https://github.com/MatrixMagician/VillaStraylight/issues/86) makes this checkable — the CLI validates against a known set, not a free-form string.

## 12. Passive surfaces

[#87](https://github.com/MatrixMagician/VillaStraylight/issues/87): `status`/`doctor`/dashboard read the **last recorded check**, never triggering a live one.

```
$ villa status
...
Updates        2 available, last checked 3 days ago
```

```
$ villa status        # stale
...
Updates        last checked 146 days ago — run `villa update --check`
```

```
$ villa status        # never checked
...
Updates        never checked — run `villa update --check`
```

**Choices to react to:**

- **Never-checked is its own state**, not "0 available". Same absent-is-not-zero discipline as the JSON.
- **The age is always shown**, even when fresh, because [#87](https://github.com/MatrixMagician/VillaStraylight/issues/87) traded automation away for honest staleness and this is where that shows up.

## Open questions for your reaction

1. **The `villa` row.** §1 puts it in the table (a row that announces it is not actionable); §5 puts it in a footer. Which?
2. **Exit `2` for "updates available".** Script-friendly, or surprising?
3. **The §8 prompt.** The only interactive moment in the surface. Keep it, or make it a WARN that proceeds?
4. **Verbosity of the Reject (§3).** Four paragraphs to say "could not check". Justified by the cost of being misread, or too much?
5. **Pointing at the issue tracker (§6).** Right for a release-blocking defect, or scope creep for a CLI?
6. **Skipped rows in the table (§2)** versus a footnote. Present-but-empty rows, or quieter?
