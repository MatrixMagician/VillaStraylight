package websafe

// websafe.go is the pure fetch core: the injected Deps func-field seam (the live
// *http.Client is wired by the cmd tier in Plan 03, never constructed at package
// scope), the Page value it produces, and the Loader that fetches a batch of URLs
// under the conservative resource Bounds (defined in ssrf.go) with skip-and-continue.
//
// This is the sole producer of OWUI page_content (GUARD-01): every byte that reaches
// OWUI passes through fetchOne, including the (Phase-31 stubbed) guard seam. The core
// is unit-testable off-hardware because the HTTP client is injected — keeping the
// "orchestrate is the only intentionally-impure module" invariant (CLAUDE.md).
//
// Resource bounds (CONTEXT Area 3, GROUND-01): each body is capped at Bounds.MaxBytes
// (truncate beyond), each fetch is bounded by Bounds.Timeout, in-flight fetches are
// bounded by Bounds.MaxConcurrent, and only http(s) schemes are fetched. A failed URL
// is OMITTED (skip-and-continue, honest partial); an all-fail batch returns a non-nil
// EMPTY slice — never a fabricated page (Phase-30 D-06 honesty).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Deps is the injected fetch seam. The live SSRF-guarded client (SafeClient) is wired
// in the cmd tier (Plan 03); the core never constructs its own client at package scope,
// which keeps it pure and unit-testable with a stub client.
type Deps struct {
	// Client performs the outbound HTTP fetch. The live wiring is SafeClient(Bounds)
	// so the connect-time SSRF Control hook validates every dialed IP.
	Client *http.Client
}

// Page is one fetched-and-produced page. Content is the GUARD-02/03 sanitized + Unicode-
// normalized + provenance-fenced page text, Source is the fetched URL (flows into OWUI's
// `sources` citation field — GROUND-01), and Title is the same-defanged document title.
type Page struct {
	Content string
	Source  string
	Title   string
	// Verdict is the GUARD-04 heuristic injection classifier outcome over the normalized
	// text (flag-not-block: Detected never drops Content; it is surfaced additively in the
	// /load response metadata.guard sub-key for Phase 34 to count).
	Verdict Verdict
}

// Loader is the fetch core: it holds the injected Deps and the resource Bounds.
type Loader struct {
	deps   Deps
	bounds Bounds
}

// NewLoader constructs a Loader over the injected Deps and Bounds.
func NewLoader(deps Deps, bounds Bounds) *Loader {
	return &Loader{deps: deps, bounds: bounds}
}

// Load fetches every URL under bounded concurrency and returns the successfully
// produced Pages in input order for the survivors (skip-and-continue: a failed URL is
// omitted). The result is always non-nil; an empty slice means honest no-results
// (never a fabricated page). A nil/empty input returns a non-nil empty slice.
func (l *Loader) Load(ctx context.Context, urls []string) []Page {
	pages := make([]Page, 0, len(urls))
	if len(urls) == 0 {
		return pages
	}

	// Bounded-concurrency semaphore sized to min(len(urls), MaxConcurrent).
	limit := l.bounds.MaxConcurrent
	if limit < 1 {
		limit = 1
	}
	if limit > len(urls) {
		limit = len(urls)
	}

	type result struct {
		idx  int
		page Page
		ok   bool
	}
	results := make([]result, len(urls))
	sem := make(chan struct{}, limit)
	done := make(chan int, len(urls))

	for i, raw := range urls {
		sem <- struct{}{}
		go func(idx int, rawURL string) {
			defer func() { <-sem; done <- idx }()
			page, err := l.fetchOne(ctx, rawURL)
			if err != nil {
				results[idx] = result{idx: idx, ok: false}
				return
			}
			results[idx] = result{idx: idx, page: page, ok: true}
		}(i, raw)
	}

	for range urls {
		<-done
	}

	// Preserve input order for the survivors.
	for _, r := range results {
		if r.ok {
			pages = append(pages, r.page)
		}
	}
	return pages
}

// fetchOne fetches a single URL under the SSRF + resource guards and returns the
// produced Page. It enforces the http(s) scheme allowlist and the hostname reject-set
// up front (defense-in-depth; the connect-time Control hook is the authoritative IP
// check in the injected client), bounds the fetch by Bounds.Timeout, rejects non-2xx,
// caps the body at Bounds.MaxBytes via io.LimitReader, then runs the raw body through the
// load-bearing GUARD-02/03/04 pipeline in the order sanitize → normalize → classify →
// fence (sanitize-first on the RAW HTML, classify on the NORMALIZED text so no fence
// delimiter self-matches — Pitfall 5), USING the verdict (it is stored on Page.Verdict,
// never discarded). The title is defanged through the same sanitize+normalize path.
func (l *Loader) fetchOne(ctx context.Context, rawURL string) (Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Page{}, fmt.Errorf("unparseable url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Page{}, fmt.Errorf("scheme not allowed: %q", rawURL)
	}
	if hostRejected(u.Hostname()) {
		return Page{}, fmt.Errorf("host rejected: %q", u.Hostname())
	}

	ctx, cancel := context.WithTimeout(ctx, l.bounds.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Page{}, err
	}

	resp, err := l.deps.Client.Do(req) // Control hook validates the connected IP here.
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return Page{}, fmt.Errorf("non-2xx status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, l.bounds.MaxBytes))
	if err != nil {
		return Page{}, err
	}

	// GUARD-02/03/04 pipeline, load-bearing order (T-32-10):
	//  1. sanitize  — bluemonday StrictPolicy strips markup off the RAW HTML (+ entity-
	//                 decode); replaces the old hand-rolled extractText.
	//  2. normalize — NFKC fold + strip invisible/bidi runes (defangs Trojan-Source +
	//                 fullwidth/homoglyph obfuscation BEFORE the classifier sees it).
	//  3. classify  — heuristic rule-family verdict over the NORMALIZED text (no fence
	//                 delimiters in the classifier input, since fence is last — Pitfall 5);
	//                 the verdict is USED (stored on Page.Verdict), NOT discarded.
	//  4. fence     — wrap in the crypto/rand-nonced UNTRUSTED_WEB_CONTENT provenance fence.
	// flag-not-block: the fenced text is the Page Content regardless of the verdict.
	clean := sanitize(string(body))
	clean = normalize(clean)
	verdict := classify(clean)

	// The title is untrusted web content that reaches model context via metadata.title
	// (OWUI citation), so it gets the SAME defang as the body (sanitize+normalize) AND is
	// run through the classifier (WR-01) — title-borne injection (e.g. a forged
	// [/UNTRUSTED_WEB_CONTENT] close or "ignore previous instructions" in a <title>) must
	// not enter metadata unflagged. extractTitle now ignores <title> inside HTML comments
	// so a commented-out decoy title cannot be scraped over the real one (WR-01/IN-02).
	// We fold the title verdict INTO the page verdict (a title hit flags the page) rather
	// than fence the title, because metadata.title is a human-facing citation label that
	// must stay verbatim; classification is what makes title injection visible to Phase 34.
	title := normalize(sanitize(extractTitle(body)))
	verdict = mergeVerdicts(verdict, classify(title))

	fenced, err := fence(clean)
	if err != nil {
		// FAIL-CLOSED (WR-02): a fence with no crypto/rand nonce would carry a forgeable
		// constant delimiter, defeating the fence's sole security property. Omit the page
		// (skip-and-continue, honest partial) rather than ship a breakout-able fence.
		return Page{}, err
	}

	return Page{Content: fenced, Source: rawURL, Title: title, Verdict: verdict}, nil
}

// NOTE: the Phase-31 hand-rolled extractText stripper was DELETED in Phase 32 — the
// GUARD-02 `sanitize` (bluemonday StrictPolicy, sanitize.go) is now the sole body
// stripper. sanitize is parser-backed, so it does not suffer the unterminated-'<'
// blackhole the CR-02 fix guarded against; its never-return-empty posture is covered in
// sanitize_test.go / normalize_test.go. extractTitle (+ asciiLower) is RETAINED below —
// it scans the <title> element and is now routed through sanitize+normalize in fetchOne.

// extractTitle returns a best-effort document title from the <title> element, or "" if
// none is found. Simple substring scan; full parsing is out of scope for Phase 31.
//
// CR-01: the case-insensitive search MUST be length-preserving. strings.ToLower is NOT
// byte-length-identical for some Unicode (e.g. U+023A/U+023E grow 2->3 bytes), so
// computing match indices on a strings.ToLower(body) copy and then slicing the ORIGINAL
// body with them can index past len(body) -> "slice bounds out of range" panic. The body
// is attacker-controlled fetched web content, so a crafted <title> would DoS the loader
// goroutine (no recover). We fold ONLY ASCII A-Z -> a-z into a byte-length-identical copy
// (asciiLower) for the match — HTML tag names are ASCII, so ASCII-only folding is correct
// — then slice the original body with those still-valid indices.
func extractTitle(body []byte) string {
	orig := string(body)
	s := asciiLower(orig) // byte-length-identical to orig (only A-Z folded)

	// WR-01: skip <title> elements that live inside an HTML comment. A naive raw scan
	// would pick `<!-- <title>HIDDEN</title> -->` over the real document title, letting
	// an attacker plant an arbitrary metadata.title. We advance past any comment span
	// that would otherwise contain the candidate match. blankComments replaces every
	// comment span with spaces of the SAME byte length, so s stays byte-length-identical
	// to orig and the indices computed against it remain valid slices into orig.
	s = blankComments(s)

	// WR-01/IN-02: require a tag terminator after "<title" so "<titlebar>"/"<titlexyz>"
	// cannot match. A real <title> is immediately followed by '>' (no attrs) or
	// whitespace (before attrs); anything else is a different element name.
	open := -1
	for from := 0; ; {
		i := strings.Index(s[from:], "<title")
		if i < 0 {
			return ""
		}
		i += from
		next := i + len("<title")
		if next >= len(s) {
			return ""
		}
		if c := s[next]; c == '>' || c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
			open = i
			break
		}
		from = next // not a real <title*>; keep scanning
	}

	gt := strings.IndexByte(s[open:], '>')
	if gt < 0 {
		return ""
	}
	start := open + gt + 1
	end := strings.Index(s[start:], "</title>")
	if end < 0 {
		return ""
	}
	// start/end are offsets into s, which is byte-length-identical to orig, so they are
	// valid indices into orig — slice the ORIGINAL (case-preserving) body for the title.
	return strings.TrimSpace(orig[start : start+end])
}

// blankComments returns a byte-length-identical copy of s with every HTML comment span
// (`<!-- ... -->`) replaced by spaces, so a <title> hidden inside a comment is not
// scraped (WR-01). Preserving byte length keeps the indices extractTitle computes against
// the result valid as slices into the ORIGINAL body. An unterminated comment blanks to
// end-of-input (an attacker cannot smuggle a real title after an open-but-unclosed
// comment). The input is already asciiLower-folded, so the markers are lowercase-safe.
func blankComments(s string) string {
	const openTok, closeTok = "<!--", "-->"
	var b []byte
	for from := 0; ; {
		i := strings.Index(s[from:], openTok)
		if i < 0 {
			break
		}
		i += from
		if b == nil {
			b = []byte(s)
		}
		end := strings.Index(s[i+len(openTok):], closeTok)
		var stop int
		if end < 0 {
			stop = len(s) // unterminated comment: blank to EOF
		} else {
			stop = i + len(openTok) + end + len(closeTok)
		}
		for j := i; j < stop; j++ {
			b[j] = ' '
		}
		from = stop
	}
	if b == nil {
		return s
	}
	return string(b)
}

// asciiLower returns a byte-length-identical copy of s with ASCII 'A'..'Z' folded to
// 'a'..'z' and every other byte left untouched. Unlike strings.ToLower it never changes
// the byte length, so offsets computed against the result remain valid indices into the
// original string (CR-01). HTML tag names are ASCII, so ASCII-only folding suffices for
// case-insensitive tag matching.
func asciiLower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}
