// Package manifestverify decides whether villa may act on manifest bytes, and when
// it may not, says precisely why.
//
// # A signature proves authorship, not currency
//
// This is the whole reason the package is more than one ed25519 call. Three attacks
// survive signature verification using a manifest villa GENUINELY SIGNED:
//
//   - REPLAY / DOWNGRADE — serve an old signed manifest to pin a known-bad version.
//   - FREEZE — serve the current one forever, so a fix is never learned about.
//   - ROLLBACK-AFTER-FIX — serve the pre-fix manifest once a fix has shipped.
//
// Every one of them presents a document with a perfect signature. So verification
// has three gates, not one: signature, serial, expiry.
//
// # The outcomes are not interchangeable
//
// The distinction that matters most is between REFUSED and ABSENT.
//
// An expired manifest counts as ABSENT — villa falls back silently to compiled-in
// vetted pins, exactly as if none had been published. A freeze attack therefore
// degrades to fail-closed rather than leaving a host trusting a stale document
// forever, and a maintainer who simply stopped publishing does not generate alarms
// on every user's machine.
//
// A downgrade is REFUSED, loudly, and the message says the signature DID verify and
// the content is older. Collapsing that into a bare "refused" sends a user hunting
// for a broken signature, which is the wrong problem and one they cannot fix.
//
// # No --allow-downgrade
//
// There is deliberately no flag to override the serial floor. An attacker who can
// serve a manifest can also read the docs, so a flag would be a documented
// instruction for how to bypass the protection. Overriding is a manual, deliberate
// act.
//
// PURE: bytes in, verdict out. No network, no HTTP, no clock of its own — the time
// is a parameter, which is what makes the expiry truth table testable.
package manifestverify

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
)

// Outcome is what villa may do with a set of manifest bytes.
type Outcome string

const (
	// Accepted: signed by the compiled-in key, in date, and carrying a serial at or
	// above the floor. Villa may act on its pins.
	Accepted Outcome = "accepted"
	// Absent: there is no manifest villa can use, and that is not an alarm. It
	// covers both "nothing was published" and "what was published has expired",
	// because the correct behaviour is identical — fall back to vetted pins.
	Absent Outcome = "absent"
	// Refused: a manifest was present and villa will not act on it. This is always
	// loud, because something is wrong that a user may need to know about.
	Refused Outcome = "refused"
)

// Reason names WHY, at a granularity a user can act on. Outcome alone is not
// enough: "refused" tells a user to worry, and only the reason tells them whether
// to suspect their network, their disk, or an attacker.
type Reason string

const (
	// ReasonNone accompanies Accepted.
	ReasonNone Reason = ""
	// ReasonNotPublished: no bytes at all.
	ReasonNotPublished Reason = "not_published"
	// ReasonExpired: past valid_until. An Absent reason, not a Refused one.
	ReasonExpired Reason = "expired"
	// ReasonBadSignature: the bytes are not what villa's maintainer signed.
	ReasonBadSignature Reason = "bad_signature"
	// ReasonNoKey: this build carries no verification key, so it can verify
	// nothing. Distinct from a bad signature because the remedies are opposite —
	// a bad signature says "distrust this download", no key says "this villa
	// predates manifest publishing".
	ReasonNoKey Reason = "no_key"
	// ReasonMalformed: the bytes did not parse, or claimed an unknown schema.
	ReasonMalformed Reason = "malformed"
	// ReasonDowngrade: correctly signed, but older than what this host has seen.
	ReasonDowngrade Reason = "downgrade"
	// ReasonAllowlist: correctly signed, but oversteps the compiled-in table.
	ReasonAllowlist Reason = "allowlist"
)

// Verdict is the answer, and everything a caller needs to explain it.
type Verdict struct {
	Outcome Outcome
	Reason  Reason
	// Message is the user-facing explanation. It is written here rather than at the
	// command tier because the DISTINCTIONS are the product of this package —
	// "the signature verified and the content is older" is a sentence only the
	// verifier is in a position to write, and re-deriving it from an enum at each
	// call site is how one caller ends up saying "invalid manifest" instead.
	Message string
	// Doc is the parsed manifest, populated ONLY when Outcome is Accepted. A
	// refused or expired document is not returned, so no caller can reach for a pin
	// out of a manifest villa declined to act on.
	Doc manifest.Document
}

// Input is everything a verdict depends on.
type Input struct {
	// Data is the manifest bytes exactly as fetched. Nil or empty means nothing was
	// published, which is Absent rather than an error.
	Data []byte
	// Signature is the detached signature bytes.
	Signature []byte
	// PublicKey is villa's compiled-in verification key.
	PublicKey ed25519.PublicKey
	// SerialFloor is the anti-downgrade floor, which the caller reads from
	// pinstate.SerialFloor — never from the store's raw Serial field, since an
	// absent store carries zero and zero means "accept anything".
	SerialFloor uint64
	// Now is the clock, injected so the expiry table is testable. A package-level
	// time.Now would make "what happens the day after valid_until" a question only
	// answerable by waiting.
	Now time.Time
}

// Verify runs the three gates in order and returns the verdict.
//
// The ORDER is deliberate. Signature first, because until the bytes are shown to be
// authentic they are attacker-controlled and nothing in them means anything — a
// serial or an expiry read out of an unverified document is a number an attacker
// chose. Serial before expiry, because a downgrade is the more alarming finding and
// should not be masked by an expiry that the same attacker also controls.
func Verify(in Input) Verdict {
	// Gate 0: is there anything at all? Not an attack, not an error — the state of
	// every host before the first manifest is published.
	if len(in.Data) == 0 {
		return Verdict{
			Outcome: Absent,
			Reason:  ReasonNotPublished,
			Message: "No pin manifest is available. Villa is using the pins compiled into this binary.",
		}
	}

	// Gate 1: SIGNATURE. Nothing below this line may read a field out of the
	// document before this passes.
	//
	// The no-key case is separated out first. Folding it into the bad-signature
	// branch would be technically true and actively misleading: a bad signature
	// tells a user to distrust what they downloaded, while a missing key means this
	// villa build simply predates manifest publishing. Opposite remedies deserve
	// different messages.
	if len(in.PublicKey) == 0 {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonNoKey,
			Message: "A pin manifest is available, but this villa build carries no verification key, so it cannot check whether " +
				"the manifest is genuine. Villa will not act on a manifest it cannot verify, and is using the pins compiled into " +
				"this binary. Upgrade villa to a build that carries the key.",
		}
	}
	if len(in.Signature) == 0 {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonBadSignature,
			Message: "The pin manifest arrived without a signature. Villa only acts on a manifest signed by the key compiled into this binary, so it is being ignored and the compiled-in pins are in use.",
		}
	}
	if err := manifest.Verify(in.PublicKey, in.Data, in.Signature); err != nil {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonBadSignature,
			Message: "The pin manifest's SIGNATURE does not verify against the key compiled into this binary. " +
				"The manifest was not signed by villa's maintainer, or it was modified after signing. " +
				"Villa is ignoring it and using the compiled-in pins.",
		}
	}

	// Only now are the bytes trustworthy enough to parse.
	doc, err := manifest.Parse(in.Data)
	if err != nil {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonMalformed,
			Message: fmt.Sprintf("The pin manifest is correctly signed but villa cannot read it: %v. "+
				"This usually means the manifest was published for a newer villa. Villa is using the compiled-in pins.", err),
		}
	}

	// Gate 2: SERIAL. The replay/downgrade gate, and the one a signature cannot
	// close on its own.
	if doc.Serial < in.SerialFloor {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonDowngrade,
			Message: fmt.Sprintf("The pin manifest's SIGNATURE VERIFIED, but its CONTENT IS OLDER than one this host has already seen "+
				"(serial %d, and this host has accepted serial %d). This is what a replayed manifest looks like: a document villa "+
				"genuinely signed, served to move you back to older pins. Villa is refusing it and using the pins it already has. "+
				"There is no flag to override this.", doc.Serial, in.SerialFloor),
		}
	}

	// Gate 3: EXPIRY. ABSENT, not refused — see the package doc.
	expired, expiry, perr := isExpired(doc.ValidUntil, in.Now)
	if perr != nil {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonMalformed,
			Message: fmt.Sprintf("The pin manifest is correctly signed but its expiry is unreadable (%v). "+
				"Villa cannot tell whether it is current, so it is using the compiled-in pins.", perr),
		}
	}
	if expired {
		return Verdict{
			Outcome: Absent,
			Reason:  ReasonExpired,
			Message: fmt.Sprintf("The pin manifest expired on %s. Villa treats an expired manifest exactly as if none had been "+
				"published, and is using the pins compiled into this binary. This is not a fault: it means no newer manifest has "+
				"been published for a while.", expiry.Format("2006-01-02")),
		}
	}

	// The allowlist. Last, because it is the most detailed refusal and the least
	// likely — the publishing tool refuses to sign a manifest that would fail here,
	// so reaching this point means either that check was bypassed or the compiled-in
	// table has changed since signing.
	if problems := manifest.CheckAllowlist(doc); len(problems) > 0 {
		return Verdict{
			Outcome: Refused,
			Reason:  ReasonAllowlist,
			Message: "The pin manifest is correctly signed but oversteps what a manifest is allowed to do. " +
				"A manifest may supply new values for components villa already ships; it may not introduce a component, " +
				"redirect one to another registry, or change what a pin means. Villa is using the compiled-in pins.\n" +
				bullets(problems),
		}
	}

	return Verdict{
		Outcome: Accepted,
		Reason:  ReasonNone,
		Message: fmt.Sprintf("Pin manifest serial %d, valid until %s.", doc.Serial, doc.ValidUntil),
		Doc:     doc,
	}
}

// isExpired compares valid_until to now.
//
// An EMPTY valid_until is a parse error rather than "never expires", because a
// manifest with no expiry is precisely what a freeze attack wants: sign one
// document, serve it forever, and no host ever learns about a fix. The publishing
// tool refuses to emit one, and this refuses to honour one if it somehow arrives.
func isExpired(validUntil string, now time.Time) (bool, time.Time, error) {
	if strings.TrimSpace(validUntil) == "" {
		return false, time.Time{}, fmt.Errorf("the manifest carries no valid_until; a manifest with no expiry could be served forever")
	}
	expiry, err := time.Parse(time.RFC3339, validUntil)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("valid_until %q is not an RFC3339 timestamp", validUntil)
	}
	return now.After(expiry), expiry, nil
}

// bullets renders the allowlist problems one per line.
func bullets(problems []error) string {
	var b strings.Builder
	for _, p := range problems {
		b.WriteString("  - ")
		b.WriteString(p.Error())
		b.WriteString("\n")
	}
	return b.String()
}
