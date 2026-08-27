---
status: accepted
---

# The web guard flags injection, it never blocks

Fetched web content passes through sanitize → normalize → classify → fence
before reaching the model. The classifier is a heuristic rule matcher, and a
heuristic rule set has finite recall: a novel phrasing gets through. So a
detection **annotates** the response and the cleaned, fenced content is returned
regardless, the guard reduces and flags injection risk; it does not eliminate
it, and nothing in the product may claim otherwise.

## Considered options

**Block on detection**, rejected. It buys a dishonest posture rather than
safety: the operator would reasonably read "blocked" as "we stop this", while
the undetectable phrasings sail past unblocked and unflagged. A tripwire the
operator can see is worth more than a gate they will over-trust. Blocking also
makes every false positive a silently missing page.

**No guard at all, rely on the egress bound**, rejected. The bounded-outbound
limit is the real backstop, but it is coarse; a visible per-fetch verdict is the
signal the operator actually reads.

## Consequences

The honesty posture is load-bearing, not editorial. A directory-walking test
bans the dishonest phrasings ("blocks", "prevents", "immune", "safe from") across
package copy and operator-facing text, so a future contributor cannot quietly
upgrade the claim while the behaviour stays the same. Any change here has to
change the test on purpose.

There is a known residual this layer structurally cannot close: the model may
later emit a markdown image whose URL carries exfiltrated data, and the
operator's **browser** fetches it, outside the inference container, past every
egress control. That channel is accepted and documented, not closed. Do not let
a future reading of "we have an injection guard" imply that it is.
