// Package manifestverify tests are a truth table.
//
// Each row is a manifest villa might be served and the verdict it must produce. The
// rows that matter most are not the tampered ones — those are the easy case — but
// the three where the SIGNATURE IS PERFECT and villa must still decline: replay,
// freeze, and a manifest that oversteps. A signature proves authorship, not
// currency, and these are what that sentence means in practice.
//
// No network, no HTTP, no clock: the time is a parameter, so "what happens the day
// after valid_until" is a test rather than something you find out by waiting.
package manifestverify

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// fixedNow is the clock every case is judged against.
var fixedNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

// signed builds a manifest, signs it, and returns everything Verify needs.
func signed(t *testing.T, mutate func(*manifest.Document)) ([]byte, []byte, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	doc := manifest.FromTable(10, fixedNow.AddDate(0, 6, 0))
	if mutate != nil {
		mutate(&doc)
	}
	data, err := manifest.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	sig, err := manifest.Sign(priv, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return data, sig, pub
}

// TestAValidManifestIsAccepted is the happy path: correctly signed, in date, serial
// at or above the floor.
func TestAValidManifestIsAccepted(t *testing.T) {
	data, sig, pub := signed(t, nil)

	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 5, Now: fixedNow})
	if got.Outcome != Accepted {
		t.Fatalf("outcome = %q (%s), want accepted", got.Outcome, got.Message)
	}
	if got.Doc.Serial != 10 {
		t.Errorf("the accepted document was not returned: %+v", got.Doc)
	}
	if len(got.Doc.Components) != len(pins.Table()) {
		t.Errorf("the accepted document carries %d components, want %d", len(got.Doc.Components), len(pins.Table()))
	}
}

// TestASerialEqualToTheFloorIsAccepted: the floor is a floor, not a strict
// minimum. Re-fetching the manifest a host already accepted must not read as a
// downgrade, or every second check would refuse.
func TestASerialEqualToTheFloorIsAccepted(t *testing.T) {
	data, sig, pub := signed(t, nil)
	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 10, Now: fixedNow})
	if got.Outcome != Accepted {
		t.Errorf("a manifest at exactly the floor was %q: %s", got.Outcome, got.Message)
	}
}

// TestNoManifestIsAbsentNotAnError is the state of every host before the first
// manifest is published. It is not an alarm and must not read as one.
func TestNoManifestIsAbsentNotAnError(t *testing.T) {
	_, _, pub := signed(t, nil)
	got := Verify(Input{Data: nil, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Absent || got.Reason != ReasonNotPublished {
		t.Errorf("outcome = %q/%q, want absent/not_published", got.Outcome, got.Reason)
	}
}

// TestABadSignatureIsRefusedAndNamesTheSignature. A user who sees a bare "refused"
// goes hunting for the wrong problem, so the message must say which gate failed.
func TestABadSignatureIsRefusedAndNamesTheSignature(t *testing.T) {
	data, sig, pub := signed(t, nil)
	tampered := append([]byte(nil), data...)
	tampered[len(tampered)/2] ^= 0x01

	got := Verify(Input{Data: tampered, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Refused || got.Reason != ReasonBadSignature {
		t.Fatalf("outcome = %q/%q, want refused/bad_signature", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Message, "SIGNATURE") {
		t.Errorf("the message does not name the signature: %q", got.Message)
	}
	if got.Doc.Serial != 0 {
		t.Error("a refused document was returned; a caller could reach for a pin out of a manifest villa declined to act on")
	}
}

// TestAMissingSignatureIsRefused: unsigned bytes are just bytes.
func TestAMissingSignatureIsRefused(t *testing.T) {
	data, _, pub := signed(t, nil)
	got := Verify(Input{Data: data, Signature: nil, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Refused || got.Reason != ReasonBadSignature {
		t.Errorf("outcome = %q/%q, want refused/bad_signature", got.Outcome, got.Reason)
	}
}

// TestADowngradeSaysTheSignatureVerified is the most important message in the
// package.
//
// This is the replay attack: a document villa GENUINELY SIGNED, served to move a
// host back to older pins. Every signature check passes. A user told only "refused"
// will assume a broken signature and go looking at their download, which is the
// wrong problem and one they cannot fix. The message has to distinguish "this is
// not from villa" from "this is from villa, and it is old".
func TestADowngradeSaysTheSignatureVerified(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) { d.Serial = 3 })

	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 9, Now: fixedNow})
	if got.Outcome != Refused || got.Reason != ReasonDowngrade {
		t.Fatalf("outcome = %q/%q, want refused/downgrade: %s", got.Outcome, got.Reason, got.Message)
	}
	if !strings.Contains(got.Message, "SIGNATURE VERIFIED") {
		t.Errorf("the message does not say the signature verified, so a user will suspect a broken download: %q", got.Message)
	}
	if !strings.Contains(got.Message, "OLDER") {
		t.Errorf("the message does not say the content is older: %q", got.Message)
	}
	if !strings.Contains(got.Message, "no flag to override") {
		t.Errorf("the message does not say the refusal cannot be overridden, which invites a search for a flag that must not exist: %q", got.Message)
	}
}

// TestAnExpiredManifestIsAbsentNotRefused is the freeze attack degrading to
// fail-closed.
//
// Expired means villa falls back to compiled-in pins EXACTLY as if none had been
// published. Treating it as a refusal instead would raise an alarm on every host
// whenever a maintainer simply stopped publishing for a while, which trains users
// to ignore the alarm that matters.
func TestAnExpiredManifestIsAbsentNotRefused(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) {
		d.ValidUntil = fixedNow.AddDate(0, 0, -1).Format(time.RFC3339)
	})

	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Absent {
		t.Fatalf("outcome = %q, want absent — an expired manifest is treated as if none was published: %s", got.Outcome, got.Message)
	}
	if got.Reason != ReasonExpired {
		t.Errorf("reason = %q, want expired", got.Reason)
	}
	if strings.Contains(got.Message, "refus") {
		t.Errorf("the message reads as a refusal: %q", got.Message)
	}
	if !strings.Contains(got.Message, "not a fault") {
		t.Errorf("the message does not reassure that expiry is normal: %q", got.Message)
	}
	if got.Doc.Serial != 0 {
		t.Error("an expired document was returned; villa must fall back to vetted pins, not read this one")
	}
}

// TestAManifestExpiringExactlyNowIsStillValid pins the boundary. `now.After(expiry)`
// rather than `!now.Before(expiry)` means the instant of expiry is still in date,
// which matters because a check firing at exactly the boundary is otherwise a coin
// flip on clock resolution.
func TestAManifestExpiringExactlyNowIsStillValid(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) {
		d.ValidUntil = fixedNow.Format(time.RFC3339)
	})
	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Accepted {
		t.Errorf("a manifest expiring at exactly now was %q: %s", got.Outcome, got.Message)
	}
}

// TestSerialIsCheckedBeforeExpiry: an attacker replaying an old manifest controls
// both its serial and its expiry, so an expired-and-downgraded document must report
// the downgrade. Reporting the expiry would let the more alarming finding be masked
// by the less alarming one, at the attacker's choosing.
func TestSerialIsCheckedBeforeExpiry(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) {
		d.Serial = 2
		d.ValidUntil = fixedNow.AddDate(0, 0, -1).Format(time.RFC3339)
	})
	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 9, Now: fixedNow})
	if got.Reason != ReasonDowngrade {
		t.Errorf("reason = %q, want downgrade — an expired old manifest must report the replay, not the expiry", got.Reason)
	}
}

// TestSignatureIsCheckedBeforeAnythingElse: until the bytes are authentic, every
// field in them is a number an attacker chose. A document with an attacker-supplied
// serial and expiry must fail on the SIGNATURE, not on either of those.
func TestSignatureIsCheckedBeforeAnythingElse(t *testing.T) {
	_, _, pub := signed(t, nil)
	// A well-formed document, never signed by this key.
	doc := manifest.FromTable(1, fixedNow.AddDate(0, 0, -1))
	doc.Serial = 1
	data, err := manifest.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := Verify(Input{Data: data, Signature: []byte("not a signature"), PublicKey: pub, SerialFloor: 99, Now: fixedNow})
	if got.Reason != ReasonBadSignature {
		t.Errorf("reason = %q, want bad_signature — nothing may be read out of an unverified document", got.Reason)
	}
}

// TestAnUnknownComponentIsRefusedAndNamed: the allowlist, reached only by a
// correctly-signed document. The publishing tool refuses to sign one of these, so
// arriving here means that check was bypassed or the compiled-in table changed
// after signing — either way the offending entry must be named.
func TestAnUnknownComponentIsRefusedAndNamed(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) {
		d.Components = append(d.Components, manifest.Component{
			ID: "attacker-supplied-tool", Registry: "docker.io", Shape: "rolling_digest",
			Ref: "docker.io/evil/tool@sha256:00",
		})
	})

	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Refused || got.Reason != ReasonAllowlist {
		t.Fatalf("outcome = %q/%q, want refused/allowlist: %s", got.Outcome, got.Reason, got.Message)
	}
	if !strings.Contains(got.Message, "attacker-supplied-tool") {
		t.Errorf("the refusal does not name the offending component: %q", got.Message)
	}
}

// TestARedirectedRegistryIsRefusedAndNamed: the bound on a stolen key. It may move
// a host to a bad version of something it already runs; it may not point a pull at
// a host the operator never trusted.
func TestARedirectedRegistryIsRefusedAndNamed(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) {
		d.Components[0].Registry = "registry.attacker.invalid"
	})
	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Reason != ReasonAllowlist {
		t.Fatalf("reason = %q, want allowlist: %s", got.Reason, got.Message)
	}
	if !strings.Contains(got.Message, "registry.attacker.invalid") {
		t.Errorf("the refusal does not name the offending registry: %q", got.Message)
	}
}

// TestAManifestWithNoExpiryIsRefused: an empty valid_until is exactly what a freeze
// attack wants — sign once, serve forever. It is a malformed document, not a
// document that never expires.
func TestAManifestWithNoExpiryIsRefused(t *testing.T) {
	data, sig, pub := signed(t, func(d *manifest.Document) { d.ValidUntil = "" })
	got := Verify(Input{Data: data, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Refused {
		t.Errorf("outcome = %q, want refused — a manifest with no expiry could be served forever: %s", got.Outcome, got.Message)
	}
}

// TestGarbageBytesAreRefusedAsMalformedNotAccepted: correctly signed garbage is
// still garbage, and must not crash or half-parse.
func TestGarbageBytesAreRefusedAsMalformedNotAccepted(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	junk := []byte(`{"schema_version": 1, "this is not": "a manifest"}`)
	sig, err := manifest.Sign(priv, junk)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	got := Verify(Input{Data: junk, Signature: sig, PublicKey: pub, SerialFloor: 1, Now: fixedNow})
	if got.Outcome != Refused || got.Reason != ReasonMalformed {
		t.Errorf("outcome = %q/%q, want refused/malformed: %s", got.Outcome, got.Reason, got.Message)
	}
}

// TestABuildWithNoKeyRefusesRatherThanAccepting is the unset-key state, and the
// place a wrong default would be catastrophic.
//
// No keypair has been generated yet — it is an open item for a human, because it
// must happen off this machine and off CI. Until it does, villa can verify nothing.
// The behaviour must be "refuse everything", never "skip the check": the latter
// turns a missing key into an open door, silently, on every host.
func TestABuildWithNoKeyRefusesRatherThanAccepting(t *testing.T) {
	data, sig, _ := signed(t, nil)

	got := Verify(Input{Data: data, Signature: sig, PublicKey: nil, SerialFloor: 1, Now: fixedNow})
	if got.Outcome == Accepted {
		t.Fatal("a build with no verification key ACCEPTED a manifest; a missing key must never mean 'skip the check'")
	}
	if got.Reason != ReasonNoKey {
		t.Errorf("reason = %q, want no_key — a bad signature says 'distrust this download', a missing key says 'upgrade villa', and the remedies are opposite", got.Reason)
	}
	if !strings.Contains(got.Message, "no verification key") {
		t.Errorf("the message does not explain that this build cannot verify: %q", got.Message)
	}
}

// TestPublicKeyIsUnsetOrValid: whatever state key.go is in, it must be one of the
// two coherent ones. A half-set key — malformed hex, wrong length — would make
// every manifest fail with a message about signatures, hiding a build error behind
// what looks like an attack.
func TestPublicKeyIsUnsetOrValid(t *testing.T) {
	key := PublicKey()
	if key == nil {
		if HasPublicKey() {
			t.Error("HasPublicKey disagrees with PublicKey")
		}
		if publicKeyHex != "" {
			t.Errorf("publicKeyHex is set to %q but does not decode to a valid ed25519 key; "+
				"every manifest would be refused with a signature message, hiding a build error behind what looks like an attack", publicKeyHex)
		}
		return
	}
	if len(key) != ed25519.PublicKeySize {
		t.Errorf("the compiled-in key is %d bytes, want %d", len(key), ed25519.PublicKeySize)
	}
	if !HasPublicKey() {
		t.Error("HasPublicKey disagrees with PublicKey")
	}
}

// TestNoNetworkIsReachable is a structural assertion: this package takes bytes and
// returns a verdict, and the moment it grows an HTTP client the truth table above
// stops being a complete account of what it does.
func TestNoNetworkIsReachable(t *testing.T) {
	for _, forbidden := range []string{"net/http", "os/exec", "time.Now()"} {
		if packageImports(t, forbidden) {
			t.Errorf("manifestverify references %q; it must stay pure so the truth table is the whole behaviour", forbidden)
		}
	}
}

// packageImports reports whether any non-test source in this package mentions a
// token. Crude on purpose: it is a tripwire on a property, not a parser.
func packageImports(t *testing.T, token string) bool {
	t.Helper()
	for _, name := range []string{"verify.go", "key.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			code := line
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx]
			}
			if strings.Contains(code, token) {
				return true
			}
		}
	}
	return false
}
