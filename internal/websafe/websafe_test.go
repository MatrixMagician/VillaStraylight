// websafe_test.go guards the fetch core (websafe.go) and the Phase-32 guard stubs
// (guard_stubs.go): bounded body/timeout, scheme allowlist, skip-and-continue batch
// behaviour, honest empty-on-all-fail, and the identity pass-through of the stubs.
//
// The fetch core is the sole producer of page_content (GUARD-01) under the conservative
// resource bounds (GROUND-01 partial). It must NEVER fabricate context: an all-fail
// batch returns a non-nil empty slice (D-06 honesty), not an invented page.
package websafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testLoader builds a Loader over a real SafeClient with the given Bounds. The httptest
// servers in these tests bind 127.0.0.1, which the SSRF Control hook would normally
// reject — so these tests inject a permissive client (DefaultBounds without the Control
// hook) to exercise the fetch-core logic in isolation. The Control hook itself is proven
// in ssrf_test.go.
func testLoader(b Bounds) *Loader {
	// A plain client with the redirect cap but no Control hook, so loopback httptest
	// servers are reachable for fetch-core unit tests.
	client := &http.Client{
		Timeout: b.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= b.MaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	return NewLoader(Deps{Client: client}, b)
}

// TestLoadHappyPath: a 200 HTML server yields one Page with non-empty Content and
// Source == the request URL.
func TestLoadHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>hello grounded world</body></html>"))
	}))
	defer srv.Close()

	l := testLoader(DefaultBounds())
	pages := l.Load(context.Background(), []string{srv.URL})
	if len(pages) != 1 {
		t.Fatalf("Load returned %d pages, want 1", len(pages))
	}
	if pages[0].Content == "" {
		t.Error("page Content is empty, want extracted text")
	}
	if pages[0].Source != srv.URL {
		t.Errorf("page Source = %q, want %q", pages[0].Source, srv.URL)
	}
}

// TestFetchBounds: a body larger than MaxBytes is truncated (no error/panic); a server
// slower than the timeout is omitted (skip-and-continue, no hang); a non-http(s) scheme
// is omitted.
func TestFetchBounds(t *testing.T) {
	t.Run("body-truncated-at-maxbytes", func(t *testing.T) {
		big := strings.Repeat("A", 5<<20) // 5 MiB, well over the cap
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(big))
		}))
		defer srv.Close()

		b := DefaultBounds()
		l := testLoader(b)
		pages := l.Load(context.Background(), []string{srv.URL})
		if len(pages) != 1 {
			t.Fatalf("Load returned %d pages, want 1", len(pages))
		}
		if int64(len(pages[0].Content)) > b.MaxBytes {
			t.Errorf("Content length %d exceeds MaxBytes %d (not truncated)", len(pages[0].Content), b.MaxBytes)
		}
	})

	t.Run("timeout-skips-slow-url", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			_, _ = w.Write([]byte("too slow"))
		}))
		defer srv.Close()

		b := DefaultBounds()
		b.Timeout = 50 * time.Millisecond
		l := testLoader(b)
		start := time.Now()
		pages := l.Load(context.Background(), []string{srv.URL})
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("Load took %v, want bounded by timeout", elapsed)
		}
		if len(pages) != 0 {
			t.Errorf("Load returned %d pages, want 0 (slow URL skipped)", len(pages))
		}
	})

	t.Run("non-http-scheme-omitted", func(t *testing.T) {
		l := testLoader(DefaultBounds())
		pages := l.Load(context.Background(), []string{"file:///etc/passwd", "ftp://x/y"})
		if len(pages) != 0 {
			t.Errorf("Load returned %d pages, want 0 (non-http(s) schemes omitted)", len(pages))
		}
	})
}

// TestSkipAndContinue: a non-2xx URL is omitted; a mixed batch returns exactly the good
// pages with Source preserved; an all-fail batch returns a non-nil empty slice.
func TestSkipAndContinue(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("good page content"))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	l := testLoader(DefaultBounds())

	t.Run("mixed-batch-returns-survivors", func(t *testing.T) {
		urls := []string{good.URL, bad.URL, good.URL}
		pages := l.Load(context.Background(), urls)
		if len(pages) != 2 {
			t.Fatalf("Load returned %d pages, want 2 (the 2 good URLs)", len(pages))
		}
		for _, p := range pages {
			if p.Source != good.URL {
				t.Errorf("survivor Source = %q, want %q", p.Source, good.URL)
			}
		}
	})

	t.Run("all-fail-returns-non-nil-empty", func(t *testing.T) {
		pages := l.Load(context.Background(), []string{bad.URL, bad.URL})
		if pages == nil {
			t.Error("Load returned nil, want non-nil empty slice (honest no-results)")
		}
		if len(pages) != 0 {
			t.Errorf("Load returned %d pages, want 0 (all failed)", len(pages))
		}
	})

	t.Run("empty-input-returns-non-nil-empty", func(t *testing.T) {
		pages := l.Load(context.Background(), nil)
		if pages == nil {
			t.Error("Load(nil) returned nil, want non-nil empty slice")
		}
		if len(pages) != 0 {
			t.Errorf("Load(nil) returned %d pages, want 0", len(pages))
		}
	})
}

// TestGuardStubsIdentity: the Phase-32 hooks are identity pass-throughs in Phase 31 —
// sanitize/normalize/fence return their input unchanged and classify reports no
// detection. The seam exists; the policy lands in Phase 32.
func TestGuardStubsIdentity(t *testing.T) {
	in := "  some <b>fetched</b> text ‮ with tricks  "
	if got := sanitize(in); got != in {
		t.Errorf("sanitize mutated input: got %q", got)
	}
	if got := normalize(in); got != in {
		t.Errorf("normalize mutated input: got %q", got)
	}
	if got := fence(in); got != in {
		t.Errorf("fence mutated input: got %q", got)
	}
	if classify(in) {
		t.Error("classify reported a detection, want false (no-detection stub in Phase 31)")
	}
}

// TestDefaultBoundsConservative documents the conservative v1.5 defaults so a future
// loosening is an intentional, reviewed change.
func TestDefaultBoundsConservative(t *testing.T) {
	b := DefaultBounds()
	if b.MaxBytes != 2<<20 {
		t.Errorf("MaxBytes = %d, want %d (~2 MiB)", b.MaxBytes, 2<<20)
	}
	if b.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", b.Timeout)
	}
	if b.MaxConcurrent < 1 {
		t.Errorf("MaxConcurrent = %d, want a positive small bound", b.MaxConcurrent)
	}
	if b.MaxRedirects != 5 {
		t.Errorf("MaxRedirects = %d, want 5", b.MaxRedirects)
	}
}

// TestExtractTitleLengthPreserving is the CR-01 regression: a <title> containing a rune
// whose strings.ToLower form GROWS in byte length (U+023A "Ⱥ" -> 2 bytes -> U+2C65 3
// bytes) previously made extractTitle slice the original body with indices computed on a
// LONGER lowercased copy -> "slice bounds out of range" panic. The body is attacker-
// controlled fetched content, so this was a remote DoS. After the length-preserving
// ASCII-fold fix it must extract the title cleanly with no panic.
func TestExtractTitleLengthPreserving(t *testing.T) {
	// 20 growing runes + an ASCII tail, the exact shape the review reproduced as a panic.
	titleText := strings.Repeat("Ⱥ", 20) + "X"
	body := []byte("<title>" + titleText + "</title>")

	// A panic here (pre-fix) fails the test via the testing harness; assert correctness too.
	got := extractTitle(body)
	if got != titleText {
		t.Errorf("extractTitle = %q, want %q (length-preserving ASCII fold must not corrupt non-ASCII bytes)", got, titleText)
	}

	// Mixed-case ASCII tag with a growing rune inside: case-insensitive match still works.
	body2 := []byte("<TITLE>" + strings.Repeat("Ⱦ", 5) + "ok</TITLE>")
	if got := extractTitle(body2); got != strings.Repeat("Ⱦ", 5)+"ok" {
		t.Errorf("extractTitle(mixed-case) = %q, want %q", got, strings.Repeat("Ⱦ", 5)+"ok")
	}
}

// --- loader.go: OWUI external-loader contract glue ---

const testSecret = "test-bearer-secret"

// testServer builds a websafe Server whose loader fetches via the permissive
// (non-Control) client so loopback httptest result servers are reachable in the
// contract tests. The Bearer secret path is exercised independently.
func testServer(t *testing.T, secret string) *Server {
	t.Helper()
	client := &http.Client{Timeout: DefaultBounds().Timeout}
	loader := NewLoader(Deps{Client: client}, DefaultBounds())
	return NewServer(loader, secret)
}

func postLoad(t *testing.T, srv *Server, secret string, body []byte) *http.Response {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/load", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestExternalLoaderContract: a valid Bearer + {urls:[good]} returns 200 with a JSON
// array whose element has page_content and metadata.source == the URL (GROUND-01).
func TestExternalLoaderContract(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><title>T</title><body>grounded content</body></html>"))
	}))
	defer good.Close()

	srv := testServer(t, testSecret)
	reqBody, _ := json.Marshal(LoadRequest{URLs: []string{good.URL}})
	resp := postLoad(t, srv, testSecret, reqBody)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var out []LoadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("response array len = %d, want 1", len(out))
	}
	if out[0].PageContent == "" {
		t.Error("page_content empty, want extracted text")
	}
	if got := out[0].Metadata["source"]; got != good.URL {
		t.Errorf("metadata.source = %v, want %q", got, good.URL)
	}
}

// TestLoaderAuth: a missing or wrong Bearer returns 401 and never attempts a fetch.
func TestLoaderAuth(t *testing.T) {
	fetched := false
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		_, _ = w.Write([]byte("should not be fetched"))
	}))
	defer good.Close()

	srv := testServer(t, testSecret)
	reqBody, _ := json.Marshal(LoadRequest{URLs: []string{good.URL}})

	t.Run("missing-bearer", func(t *testing.T) {
		resp := postLoad(t, srv, "", reqBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})
	t.Run("wrong-bearer", func(t *testing.T) {
		resp := postLoad(t, srv, "wrong-secret", reqBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})
	if fetched {
		t.Error("an unauthenticated request triggered a fetch, want no fetch before auth")
	}
}

// TestLoaderMalformedBody: a malformed JSON body returns 400; an oversize body is
// bounded by io.LimitReader and decodes to 400 (no OOM, no panic).
func TestLoaderMalformedBody(t *testing.T) {
	srv := testServer(t, testSecret)

	t.Run("malformed-json", func(t *testing.T) {
		resp := postLoad(t, srv, testSecret, []byte("{not json"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("oversize-body-bounded", func(t *testing.T) {
		// A body far larger than the request cap: the LimitReader truncates it so the
		// JSON decode fails gracefully (400), never an unbounded read.
		huge := append([]byte(`{"urls":["`), bytes.Repeat([]byte("A"), 4<<20)...)
		resp := postLoad(t, srv, testSecret, huge)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
	})
}

// TestLoaderAlways200: per-URL failures NEVER produce a non-2xx (OWUI raise_for_status
// would abort the whole batch). A good+bad batch returns 200 with the bad URL omitted;
// an all-bad batch returns 200 with an empty JSON array `[]`.
func TestLoaderAlways200(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("good page"))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	srv := testServer(t, testSecret)

	t.Run("partial-batch-200-omits-bad", func(t *testing.T) {
		reqBody, _ := json.Marshal(LoadRequest{URLs: []string{good.URL, bad.URL}})
		resp := postLoad(t, srv, testSecret, reqBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var out []LoadResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out) != 1 {
			t.Errorf("array len = %d, want 1 (bad URL omitted)", len(out))
		}
	})

	t.Run("all-bad-batch-200-empty-array", func(t *testing.T) {
		reqBody, _ := json.Marshal(LoadRequest{URLs: []string{bad.URL, bad.URL}})
		resp := postLoad(t, srv, testSecret, reqBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (never non-2xx for per-URL failures)", resp.StatusCode)
		}
		raw, _ := io.ReadAll(resp.Body)
		var out []LoadResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out) != 0 {
			t.Errorf("array len = %d, want 0", len(out))
		}
		if strings.TrimSpace(string(raw)) != "[]" {
			t.Errorf("body = %q, want a JSON array (e.g. []), never null", strings.TrimSpace(string(raw)))
		}
	})
}

// TestLoaderEmptySecretAcceptsAny documents the empty-secret posture: when the
// configured secret is empty, any villa.network caller is accepted (the recommended
// GUARD-01 posture supplies a real crypto/rand secret in Plan 03).
func TestLoaderEmptySecretAcceptsAny(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer good.Close()

	srv := testServer(t, "") // empty secret
	reqBody, _ := json.Marshal(LoadRequest{URLs: []string{good.URL}})
	resp := postLoad(t, srv, "", reqBody) // no Authorization header
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (empty secret accepts any caller)", resp.StatusCode)
	}
}
