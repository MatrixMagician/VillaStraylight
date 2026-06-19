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

// Page is one fetched-and-produced page. Content is the (Phase-31: lightly extracted)
// page text, Source is the fetched URL (flows into OWUI's `sources` citation field —
// GROUND-01), and Title is a best-effort document title.
type Page struct {
	Content string
	Source  string
	Title   string
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
// caps the body at Bounds.MaxBytes via io.LimitReader, then pipes the extracted text
// through the (Phase-31 stubbed) guard seam.
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

	text := extractText(body)
	title := extractTitle(body)

	// Phase-31 guard seam (identity pass-throughs; policy lands in Phase 32).
	text = sanitize(normalize(text))
	text = fence(text)
	_ = classify(text)

	return Page{Content: text, Source: rawURL, Title: title}, nil
}

// extractText returns a reasonable text rendering of a fetched body. Phase-31 keeps
// extraction deliberately SIMPLE (per RESEARCH "Don't Hand-Roll" HTML->text row): it
// strips HTML tags and collapses whitespace. Full sanitization (bluemonday) is Phase 32.
func extractText(body []byte) string {
	s := string(body)
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

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
	open := strings.Index(s, "<title")
	if open < 0 {
		return ""
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
