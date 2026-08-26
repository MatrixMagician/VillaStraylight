// Package updatefetch is the ONE outbound request `villa update --check` makes.
//
// # The seam is the transport, one thing
//
// Following internal/openwebui's discipline: the seam is a single function that
// takes a URL and returns bytes. That is what makes "villa contacts exactly one
// host" directly assertable against a single fake — a seam with three methods
// would need three fakes and a test that counts across them, which is a test that
// eventually stops counting.
//
// # Strictly on-command
//
// Nothing here runs on a timer, and nothing calls it from inside another verb.
// `status` is polled by the dashboard, so an "opportunistic" check inside it would
// mean network access on a UI refresh loop — telemetry-shaped even with an innocent
// payload. Passive surfaces read the LAST RECORDED check and its age; they never
// trigger a live one.
//
// This is what keeps "zero telemetry" honest and unqualified. A check the operator
// invokes, carrying no body and no identifier, is not telemetry. If checks ever
// become automatic, that claim has to change with them.
//
// # The request carries nothing about you
//
// One HTTPS GET to a fixed release endpoint. No body, no query parameters, no
// identifier, no list of installed components. The response is the same bytes for
// every host, so the request reveals nothing beyond "a villa asked".
package updatefetch

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// manifestURL is the fixed release endpoint the manifest is published at.
//
// A constant, not a config field. A configurable endpoint would be a
// config-file-shaped way to point villa at an attacker's manifest, which the
// signature check would catch — but relying on the signature to catch a redirect
// villa itself accepted is one layer of defence where two are cheap.
const (
	manifestURL  = "https://github.com/MatrixMagician/VillaStraylight/releases/latest/download/pins.json"
	signatureURL = manifestURL + ".sig"
)

// maxManifestBytes bounds a response. A manifest for ten components is under 4 KB,
// so a megabyte is generous by two orders of magnitude and still stops a hostile or
// broken endpoint from streaming until villa runs out of memory.
const maxManifestBytes = 1 << 20

// timeout bounds one request. A check is an interactive command, so a user waiting
// on a hung endpoint is worse than being told villa could not check — which is a
// verdict this verb already knows how to report honestly.
const timeout = 30 * time.Second

// Deps is the transport seam: one function, bytes in, bytes out.
type Deps struct {
	// Get fetches one URL. It returns (nil, nil) when the resource is absent,
	// because "no manifest has been published" is not an error — it is the state of
	// the world before the first release that carries one.
	Get func(ctx context.Context, url string) ([]byte, error)
}

// Fetched is the manifest and its detached signature.
type Fetched struct {
	Manifest  []byte
	Signature []byte
	// Host is the host actually contacted, so a caller can assert the allowlist
	// held rather than trusting that it did.
	Host string
}

// Fetch retrieves the manifest and its signature.
//
// TWO requests, not one, and the distinction is worth being precise about: the
// ticket's "exactly one outbound request" is about the CHECK contacting exactly one
// HOST and asking one question. The signature is a detached file at the same
// endpoint on the same host, fetched unconditionally in the same exchange. What is
// forbidden is a per-component fan-out, because THAT is what fingerprints the host
// by revealing which addons are enabled. Two files from one release asset URL
// reveal nothing a single file would not.
func Fetch(ctx context.Context, d Deps) (Fetched, error) {
	if d.Get == nil {
		return Fetched{}, fmt.Errorf("updatefetch: no transport seam wired")
	}
	if err := assertAllowed(manifestURL); err != nil {
		return Fetched{}, err
	}

	data, err := d.Get(ctx, manifestURL)
	if err != nil {
		return Fetched{}, fmt.Errorf("updatefetch: fetch manifest: %w", err)
	}
	if len(data) == 0 {
		// Nothing published. Absent, not an error.
		return Fetched{Host: Host()}, nil
	}

	sigText, err := d.Get(ctx, signatureURL)
	if err != nil {
		return Fetched{}, fmt.Errorf("updatefetch: fetch signature: %w", err)
	}

	// The published signature is HEX TEXT — that is what villa-manifest-sign writes,
	// so a detached .sig is inspectable and diffable rather than a binary blob. It
	// is decoded HERE, at the transport boundary, because this package owns the wire
	// format and the verifier owns the cryptography.
	//
	// Handing the undecoded ASCII to ed25519.Verify does not error, it just returns
	// false — so the failure surfaces as "the signature does not verify", which
	// reads exactly like a tampered manifest. That cost a real debugging detour on
	// hardware, which is why the decode failure below is a DISTINCT, named error.
	sig, err := decodeSignature(sigText)
	if err != nil {
		return Fetched{}, err
	}

	return Fetched{Manifest: data, Signature: sig, Host: Host()}, nil
}

// decodeSignature turns the published hex text into the raw bytes ed25519 needs.
//
// A malformed signature is its own error rather than a silent empty slice. An
// empty or truncated signature would fail verification identically to a tampered
// manifest, and those are different problems: one means "distrust this download",
// the other means "the publisher's tooling is broken".
func decodeSignature(text []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(text))
	if trimmed == "" {
		return nil, fmt.Errorf("updatefetch: the manifest signature is empty")
	}
	sig, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("updatefetch: the manifest signature is not valid hex: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("updatefetch: the manifest signature is %d bytes, want %d",
			len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}

// Host is the single host a check contacts.
func Host() string {
	u, err := url.Parse(manifestURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// assertAllowed refuses any URL whose host is not in the compiled-in allowlist.
//
// The endpoint here is a constant, so this can never fire today — which is exactly
// why it exists. It is the structural guarantee that if the URL ever becomes a
// variable, the allowlist is already in the path rather than something a later
// change has to remember to add. The compiled-in table is the only source of
// trusted hosts, and nothing at runtime can extend it.
func assertAllowed(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("updatefetch: unparseable endpoint %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("updatefetch: refusing a non-HTTPS endpoint %q", raw)
	}
	if !pins.RegistryAllowed(u.Hostname()) {
		return fmt.Errorf("updatefetch: refusing to contact %q: it is not a host villa's compiled-in pin table already uses. "+
			"The allowlist is compiled in and nothing at runtime can extend it", u.Hostname())
	}
	return nil
}

// AssertAllowedForTest exposes the allowlist check so the unit test can drive hosts
// the constant endpoint cannot produce, rather than widening the real API.
func AssertAllowedForTest(raw string) error { return assertAllowed(raw) }

// LiveDeps wires the real HTTP transport.
//
// It is deliberately minimal: no cookie jar, no redirect rewriting beyond the
// stdlib default, no custom headers beyond a plain user agent. Every one of those
// is a place a request could start carrying something about the host, and this
// request must carry nothing.
func LiveDeps() Deps {
	client := &http.Client{Timeout: timeout}
	return Deps{
		Get: func(ctx context.Context, raw string) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
			if err != nil {
				return nil, err
			}
			// A fixed user agent naming the product and nothing else. No version,
			// no platform, no machine id: a per-host string would make the request
			// a fingerprint by the back door.
			req.Header.Set("User-Agent", "villa")

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusNotFound {
				return nil, nil // not published ⇒ absent, not an error
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("%s: %s", strings.TrimSpace(resp.Status), raw)
			}
			return io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
		},
	}
}
