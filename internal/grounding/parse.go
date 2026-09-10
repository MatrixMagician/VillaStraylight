package grounding

import (
	"regexp"
	"strings"
)

// claimStart matches a numbered ("1.", "2)") or bulleted ("-", "*") claim
// line at column 0; everything after the marker is the claim text. A
// continuation line is never at column 0 in the fixtures this parses. An
// indented "- SOURCE: ..." or "- UNSUPPORTED (...)" citation line uses the
// SAME leading punctuation as a bulleted claim, so requiring no indentation
// is what tells a new claim apart from a bulleted citation under one.
var claimStart = regexp.MustCompile(`^(?:\d+[.)]|[-*])\s+(.+)$`)

// totalLine matches the prompt-mandated closing line; its presence is what
// tells a complete response apart from one truncated mid-claim.
var totalLine = regexp.MustCompile(`(?i)^\s*TOTAL:`)

// unsupportedWord matches the model's UNSUPPORTED verdict token, case- and
// word-bounded so it doesn't fire on a claim's own prose ("...is unsupported
// unless...") using the word in lowercase reasoning text.
var unsupportedWord = regexp.MustCompile(`\bUNSUPPORTED\b`)

// parseReport turns a raw model answer into a DocumentReport (Path unset;
// Audit stamps it). Tolerant of numbering/bullet style and wrapped lines, per
// the ticket: a claim is any recognised claimStart line plus the text up to
// the next one; a claim is UNSUPPORTED if that block contains the token
// anywhere, with the trailing text on its last occurrence's line kept as the
// reason.
func parseReport(out string) DocumentReport {
	if strings.TrimSpace(out) == "" {
		return DocumentReport{Checked: false, Err: "empty audit response"}
	}

	claims, sawTotal := splitClaims(out)
	if len(claims) == 0 {
		return DocumentReport{Checked: false, Err: "no recognisable claims in audit response"}
	}

	rep := DocumentReport{Checked: true, Claims: len(claims)}
	for _, c := range claims {
		if unsupported, reason := unsupportedVerdict(c); unsupported {
			rep.Unsupported = append(rep.Unsupported, Unsupported{Claim: c.text, Reason: reason})
		}
	}
	if !sawTotal {
		rep.Err = "truncated: audit response ended before a TOTAL line"
	}
	return rep
}

type claimBlock struct {
	text string // the claim itself, from its numbered/bulleted line
	body string // that line's continuation lines, joined by "\n"
}

// splitClaims walks out line by line, starting a new block at each
// claimStart match and ending claim parsing at the TOTAL line (dropping any
// trailing usage/debug output after it, as in the prototype's fixtures).
func splitClaims(out string) (claims []claimBlock, sawTotal bool) {
	var cur *claimBlock
	var body []string

	flush := func() {
		if cur == nil {
			return
		}
		cur.body = strings.Join(body, "\n")
		claims = append(claims, *cur)
		cur, body = nil, nil
	}

	for _, line := range strings.Split(out, "\n") {
		if totalLine.MatchString(line) {
			sawTotal = true
			flush()
			break
		}
		if m := claimStart.FindStringSubmatch(line); m != nil {
			flush()
			cur = &claimBlock{text: strings.TrimSpace(m[1])}
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	flush()
	return claims, sawTotal
}

// unsupportedVerdict reports whether c's body carries an UNSUPPORTED token,
// and the trailing text on that token's line (parenthesised reason stripped
// of its wrapping parens), which may be empty.
func unsupportedVerdict(c claimBlock) (bool, string) {
	matches := unsupportedWord.FindAllStringIndex(c.body, -1)
	if len(matches) == 0 {
		return false, ""
	}
	last := matches[len(matches)-1]
	rest := c.body[last[1]:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	return true, rest
}
