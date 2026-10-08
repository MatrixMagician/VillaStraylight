package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestEvalMemoryProofCoversTheExtractor: with an extractor probe the memory proof
// passes only when the extractor hands back the probe sentence; an error or an
// empty extraction fails with a remediation naming the unit; with no probe
// (extractor off) the verdict and detail are what they were (ADR-0029).
func TestEvalMemoryProofCoversTheExtractor(t *testing.T) {
	embedProbe := func() (int, error) { return 768, nil }
	qdrantProbe := func() (bool, error) { return true, nil }
	rerankFirst := func() (int, error) { return 0, nil }

	cases := []struct {
		name       string
		rerank     func() (int, error)
		extract    func() (string, error)
		wantStatus preflight.Status
		wantDetail string
	}{
		{"extractor off", nil, nil, preflight.StatusPass, "768-dim embeddings + Qdrant writable"},
		{"extractor returns the probe sentence", nil,
			func() (string, error) { return "\nvilla extractor readiness probe\n", nil },
			preflight.StatusPass, "768-dim embeddings + Qdrant writable + extractor text"},
		{"reranker and extractor both proved", rerankFirst,
			func() (string, error) { return "\nvilla extractor readiness probe\n", nil },
			preflight.StatusPass, "768-dim embeddings + Qdrant writable + reranker ranking + extractor text"},
		{"extractor returns no text", nil,
			func() (string, error) { return "", nil },
			preflight.StatusFail, "the extractor returned no text for the probe document — check `systemctl --user status villa-extract.service`"},
		{"extractor does not answer", nil,
			func() (string, error) { return "", errors.New("connection refused") },
			preflight.StatusFail, "the extractor did not answer (connection refused) — check `systemctl --user status villa-extract.service`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalMemoryProof(t.Context(), embedProbe, qdrantProbe, tc.rerank, tc.extract, 768)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusPass && got.detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", got.detail, tc.wantDetail)
			}
			if !strings.Contains(got.detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", got.detail, tc.wantDetail)
			}
		})
	}
}

// TestParseExtractTextReadsTheTikaContent: Tika's /tika/text answers a JSON object
// whose X-TIKA:content key carries the extracted text; a reply without that key,
// or one that is not JSON, is refused rather than read as an empty extraction.
func TestParseExtractTextReadsTheTikaContent(t *testing.T) {
	out := []byte(`{"Content-Encoding":"ISO-8859-1","Content-Type":"text/plain; charset=ISO-8859-1",` +
		`"X-TIKA:Parsed-By":["org.apache.tika.parser.DefaultParser"],"X-TIKA:content":"\nvilla extractor readiness probe\n"}`)
	got, err := parseExtractText(out)
	if err != nil {
		t.Fatalf("parseExtractText: %v", err)
	}
	if got != "\nvilla extractor readiness probe\n" {
		t.Errorf("text = %q, want the X-TIKA:content value", got)
	}

	for name, bad := range map[string]string{
		"not json":         `{`,
		"no content key":   `{"Content-Type":"text/plain"}`,
		"content not text": `{"X-TIKA:content":null}`,
	} {
		if _, err := parseExtractText([]byte(bad)); err == nil {
			t.Errorf("%s: parseExtractText accepted %s", name, bad)
		}
	}
}

// TestExtractRequestCarriesTheContentType: Tika sniffs a PUT that names no
// Content-Type as a form body and returns an empty content, so every request
// villa sends names the document's type and streams the bytes on stdin.
func TestExtractRequestCarriesTheContentType(t *testing.T) {
	got := extractCurlArgs("villa-extract", 9998, "application/pdf")
	want := "-sf -X PUT http://villa-extract:9998/tika/text -H Content-Type: application/pdf --data-binary @-"
	if strings.Join(got, " ") != want {
		t.Errorf("curl args = %q, want %q", strings.Join(got, " "), want)
	}
}
