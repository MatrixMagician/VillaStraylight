// websafe_test.go guards the fetch core (websafe.go) and the Phase-32 guard stubs
// (guard_stubs.go): bounded body/timeout, scheme allowlist, skip-and-continue batch
// behaviour, honest empty-on-all-fail, and the identity pass-through of the stubs.
//
// The fetch core is the sole producer of page_content (GUARD-01) under the conservative
// resource bounds (GROUND-01 partial). It must NEVER fabricate context: an all-fail
// batch returns a non-nil empty slice (D-06 honesty), not an invented page.
package websafe

import (
	"context"
	"fmt"
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
