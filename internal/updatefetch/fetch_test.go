// Package updatefetch tests are the two honesty claims from the outbound design,
// made assertable rather than documented.
//
//   - "A check contacts EXACTLY ONE HOST." Counted against the single transport
//     seam, which is why the seam is one function: three methods would need three
//     fakes and a count spread across them, which is a count that stops counting.
//   - "The allowlist rejects any registry not compiled in."
//
// Both were deferred here from the outbound-honesty ticket, which could only
// document them until there was a fetch path to assert against.
package updatefetch

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// wellFormedSignature is a syntactically valid detached signature: 64 bytes of hex,
// the shape villa-manifest-sign publishes.
//
// The fixtures here used "deadbeef" until the wire-format validation landed. That
// is not a signature any signer could emit, and a fixture that cannot exist in the
// field is exactly what let the hex-decode bug reach hardware. These tests care
// about REQUEST SHAPE rather than cryptography, so the bytes need not verify — but
// they must be a shape the transport would really see.
func wellFormedSignature() []byte {
	return []byte(strings.Repeat("ab", ed25519.SignatureSize) + "\n")
}

// recordingDeps counts requests and remembers every URL, so a fan-out is
// impossible to miss.
func recordingDeps(body map[string][]byte) (*[]string, Deps) {
	var seen []string
	return &seen, Deps{
		Get: func(_ context.Context, raw string) ([]byte, error) {
			seen = append(seen, raw)
			return body[raw], nil
		},
	}
}

// TestACheckContactsExactlyOneHost is the claim the privacy design rests on.
//
// Two URLs are fetched — the manifest and its detached signature — and that is not
// a violation of the claim. The claim is about the HOST and about what the request
// reveals: two files from one release asset endpoint reveal nothing a single file
// would not. What is forbidden is a PER-COMPONENT fan-out, because the SUBSET of
// images villa asks about is a fingerprint of which addons are enabled, and a
// fingerprint cannot be un-emitted.
func TestACheckContactsExactlyOneHost(t *testing.T) {
	seen, deps := recordingDeps(map[string][]byte{
		manifestURL:  []byte(`{"schema_version":1}`),
		signatureURL: wellFormedSignature(),
	})

	if _, err := Fetch(context.Background(), deps); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	hosts := map[string]bool{}
	for _, raw := range *seen {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		hosts[u.Hostname()] = true
	}
	if len(hosts) != 1 {
		t.Errorf("a check contacted %d hosts (%v); it must contact exactly one, or the set of hosts asked becomes a fingerprint", len(hosts), hosts)
	}

	// And the number of requests must stay bounded at the manifest plus its
	// signature. A third would mean something started fanning out.
	if len(*seen) != 2 {
		t.Errorf("a check made %d requests (%v); it must fetch only the manifest and its signature", len(*seen), *seen)
	}
}

// TestNoRequestNamesAComponent: the fingerprint hazard is not about the COUNT of
// requests but about their CONTENT. A URL naming a component would leak which
// addons are enabled even if it were the only request made.
func TestNoRequestNamesAComponent(t *testing.T) {
	seen, deps := recordingDeps(map[string][]byte{
		manifestURL:  []byte(`{"schema_version":1}`),
		signatureURL: wellFormedSignature(),
	})
	if _, err := Fetch(context.Background(), deps); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, raw := range *seen {
		for _, e := range pins.Table() {
			if strings.Contains(raw, string(e.Component)) {
				t.Errorf("a request URL names component %q:\n  %s\nThe set of components villa asks about is a fingerprint of the stack.",
					e.Component, raw)
			}
		}
		if u, err := url.Parse(raw); err == nil && u.RawQuery != "" {
			t.Errorf("a request carries query parameters (%q); the check must carry nothing about this host", u.RawQuery)
		}
	}
}

// TestNothingPublishedIsAbsentNotAnError: before the first release carrying a
// manifest, there is nothing to fetch. That is the state of the world, not a fault,
// and it must not surface as an error a user tries to fix.
func TestNothingPublishedIsAbsentNotAnError(t *testing.T) {
	_, deps := recordingDeps(map[string][]byte{}) // every URL returns nil

	got, err := Fetch(context.Background(), deps)
	if err != nil {
		t.Fatalf("an absent manifest surfaced as an error: %v", err)
	}
	if len(got.Manifest) != 0 {
		t.Error("bytes were returned for an absent manifest")
	}
}

// TestNoSignatureIsFetchedForAnAbsentManifest: fetching a signature for a manifest
// that does not exist is a second pointless request, and this asserts the short
// circuit rather than trusting it.
func TestNoSignatureIsFetchedForAnAbsentManifest(t *testing.T) {
	seen, deps := recordingDeps(map[string][]byte{})
	if _, err := Fetch(context.Background(), deps); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(*seen) != 1 {
		t.Errorf("an absent manifest triggered %d requests (%v); the signature fetch must be skipped", len(*seen), *seen)
	}
}

// TestTheAllowlistRejectsAnyRegistryNotCompiledIn is the second deferred claim.
//
// The endpoint is a constant today, so this can never fire in production — which is
// exactly the point of asserting it. The allowlist is in the path, so if the URL
// ever becomes a variable the check is already there rather than something a later
// change must remember to add.
func TestTheAllowlistRejectsAnyRegistryNotCompiledIn(t *testing.T) {
	refused := []string{
		"https://registry.attacker.invalid/pins.json",
		"https://evil.example.com/pins.json",
		"https://localhost:8080/pins.json",
		"https://127.0.0.1/pins.json",
	}
	for _, raw := range refused {
		if err := AssertAllowedForTest(raw); err == nil {
			t.Errorf("the allowlist accepted %q, a host villa's compiled-in table never uses", raw)
		}
	}

	// Every host the table DOES use is accepted, or the allowlist would refuse
	// villa's own endpoint.
	for _, e := range pins.Table() {
		if err := AssertAllowedForTest("https://" + e.Registry + "/x"); err != nil {
			t.Errorf("the allowlist rejected %q, a host the pin table already pulls from: %v", e.Registry, err)
		}
	}
}

// TestPlainHTTPIsRefused: the manifest's authenticity rests on its signature, so
// transport encryption is not what protects it — but an http:// endpoint is a
// downgrade nobody would choose deliberately, and refusing it costs nothing.
func TestPlainHTTPIsRefused(t *testing.T) {
	if err := AssertAllowedForTest("http://github.com/pins.json"); err == nil {
		t.Error("a plain-HTTP endpoint was accepted")
	}
}

// TestTheRealEndpointPassesItsOwnAllowlist: the compiled-in URL must be reachable
// under the compiled-in rules, or every check refuses before it starts.
func TestTheRealEndpointPassesItsOwnAllowlist(t *testing.T) {
	if err := AssertAllowedForTest(manifestURL); err != nil {
		t.Errorf("villa's own endpoint fails its own allowlist: %v", err)
	}
	if Host() == "" {
		t.Error("the endpoint resolves to no host")
	}
	if !pins.RegistryAllowed(Host()) {
		t.Errorf("the endpoint host %q is not in the compiled-in table", Host())
	}
}

// TestAnUnwiredSeamIsAnErrorNotASilentNoCheck: a nil transport is a programming
// error, and reporting it as "nothing published" would silently disable update
// signalling on every host.
func TestAnUnwiredSeamIsAnErrorNotASilentNoCheck(t *testing.T) {
	if _, err := Fetch(context.Background(), Deps{}); err == nil {
		t.Error("Fetch with no transport returned no error; an unwired seam would silently disable checking")
	}
}

// TestNothingRunsOnATimer is a structural assertion of the on-command rule.
//
// A ticker or a background goroutine here would make checks automatic, and villa's
// "zero telemetry" claim depends on a check being something the operator invoked.
// If checks ever become automatic, that claim has to change with them — so this
// fails the build rather than letting the two drift apart quietly.
func TestNothingRunsOnATimer(t *testing.T) {
	src := readSource(t, "fetch.go")
	for _, forbidden := range []string{"time.Tick", "time.NewTicker", "time.AfterFunc", "go func("} {
		if strings.Contains(src, forbidden) {
			t.Errorf("fetch.go contains %q; checks are strictly on-command, and an automatic one would put "+
				"villa's unqualified zero-telemetry claim at risk", forbidden)
		}
	}
}

// readSource reads a file in this package, with comments stripped so a doc comment
// may name a forbidden token freely.
func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var b strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// TestTheSignatureIsDecodedFromHex is a regression test for a bug found by running
// the real thing on hardware.
//
// villa-manifest-sign writes the detached signature as HEX TEXT, so a .sig file is
// inspectable and diffable. Fetch previously handed those ASCII bytes straight to
// the verifier, which expects 64 RAW bytes — and ed25519.Verify does not error on a
// wrong-length signature, it returns false. So every genuine manifest failed with
// "the signature does not verify", which reads exactly like a tampered download.
//
// The round trip is asserted end to end here: sign real bytes, publish the hex the
// way the tool does, fetch, and verify. Nothing in this test decodes by hand — that
// hand-decoding in the earlier probes is precisely what masked the bug.
func TestTheSignatureIsDecodedFromHex(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	manifestBytes := []byte(`{"schema_version":1,"serial":1}`)
	rawSig := ed25519.Sign(priv, manifestBytes)

	// Exactly what villa-manifest-sign writes: hex plus a trailing newline.
	published := []byte(hex.EncodeToString(rawSig) + "\n")

	_, deps := recordingDeps(map[string][]byte{
		manifestURL:  manifestBytes,
		signatureURL: published,
	})
	got, err := Fetch(context.Background(), deps)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(got.Signature) != ed25519.SignatureSize {
		t.Fatalf("fetched signature is %d bytes, want %d — the published hex was not decoded",
			len(got.Signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(pub, got.Manifest, got.Signature) {
		t.Error("a correctly-signed manifest did not verify after a round trip through Fetch")
	}
}

// TestAMalformedSignatureIsItsOwnError: an empty or truncated signature fails
// verification identically to a tampered manifest, and those are different
// problems. One means "distrust this download", the other means "the publisher's
// tooling is broken".
func TestAMalformedSignatureIsItsOwnError(t *testing.T) {
	cases := map[string][]byte{
		"empty":        []byte("   \n"),
		"not hex":      []byte("zzzznothex\n"),
		"wrong length": []byte("abcd\n"),
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			_, deps := recordingDeps(map[string][]byte{
				manifestURL:  []byte(`{"schema_version":1}`),
				signatureURL: sig,
			})
			if _, err := Fetch(context.Background(), deps); err == nil {
				t.Errorf("a %s signature was accepted; it would later fail verification and read as a tampered manifest", name)
			}
		})
	}
}
