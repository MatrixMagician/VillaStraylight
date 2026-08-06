package websafe

// sanitize.go is the markup-sanitization policy: it reduces raw fetched
// HTML to plain, model-readable text by stripping every tag/attribute/script via
// bluemonday's StrictPolicy (an empty allowlist) and then entity-decoding the result.
//
// This REPLACES the Phase-31 hand-rolled extractText stripper (which carried the
// DoS-panic and content-swallow bugs on attacker-controlled input).
// bluemonday is an audited, allowlist-based, parser-backed sanitizer (built on the
// Go team's golang.org/x/net/html), so the durable replacement is library-backed.
//
// HONESTY POSTURE (carried forward from the Phase-31 guard seam): this does NOT
// confer immunity to prompt injection. The guard layer REDUCES and FLAGS; it does
// not eliminate. Sanitization removes active markup; it does not make untrusted
// page text safe to treat as instructions.

import (
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// strictPolicy is built once at package init; bluemonday policies are immutable
// after construction and safe for concurrent use across fetch goroutines.
var strictPolicy = bluemonday.StrictPolicy()

// sanitize strips all HTML markup from rawHTML (StrictPolicy = empty allowlist) and
// entity-decodes the sanitizer's output to plain, model-readable text.
//
// bluemonday's StrictPolicy emits HTML ENTITIES (&amp;, &lt;, ...) in its output
// safe HTML is its job, not plain text — so html.UnescapeString is MANDATORY here
// (Pitfall 1), otherwise the model would read "&amp;" instead of "&".
//
// TrimSpace of an all-markup input legitimately yields "" (there was no text); we
// never blackhole real content (anti-pattern) — for any input carrying visible
// text the text survives.
func sanitize(rawHTML string) string {
	return strings.TrimSpace(html.UnescapeString(strictPolicy.Sanitize(rawHTML)))
}
