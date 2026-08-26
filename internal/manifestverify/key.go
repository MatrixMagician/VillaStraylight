package manifestverify

// key.go holds villa's compiled-in manifest verification key.
//
// # Why compiled in
//
// Manifest trust is anchored exactly where every other pin's trust already lives:
// in the binary. That sets the right ceiling deliberately — THE MANIFEST IS NO MORE
// TRUSTED THAN THE BINARY, and a compromised binary is already game over, so
// nothing is lost by tying one to the other. A key fetched at runtime, or read from
// a config file, would be a key an attacker who can write to the host can replace,
// which would make the whole signature scheme decorative.
//
// The private half never touches CI. A key in Actions secrets is reachable by
// anyone who can land a workflow change, which would lower manifest trust to "repo
// write access" — the exact property this arrangement exists to avoid. See
// docs/RELEASING.md § Key custody.
//
// # The unset state fails closed, and that still matters
//
// A key IS set below. But the empty case stays reachable — a fork that strips it, a
// build made before one was generated — so the behaviour is worth stating: an empty
// key means villa can verify NOTHING, so every manifest is refused and every host
// falls back to compiled-in pins.
//
// The alternative would be catastrophic. Treating "no key configured" as "skip the
// signature check" would turn a missing key into an open door, silently, on every
// host, at exactly the moment nobody was looking. A villa that can check nothing is
// strictly better than a villa that accepts anything.

import (
	"crypto/ed25519"
	"encoding/hex"
)

// publicKeyHex is the hex-encoded ed25519 public key manifests are verified
// against. Its private half was generated offline on the maintainer's machine by
// `villa-manifest-sign keygen` and has never been in this repository, in CI, or in
// any secrets store — see docs/RELEASING.md § Key custody.
//
// Rotating it is a RELEASE, not a config change, and that is the point: a key a
// running villa could be talked into changing would make the whole scheme
// decorative. See the note above on why the empty value fails closed.
const publicKeyHex = "d9d4cba59b62a7bc614c493610cf21a5f2cefed49bd7e5a74b730f419c918451"

// PublicKey returns the compiled-in verification key, or nil when none is set.
//
// A nil key verifies nothing, so every manifest is refused and every host uses its
// compiled-in pins. Callers do not need to special-case it: the refusal path is the
// same one a tampered manifest takes, which is the honest reading — villa cannot
// show that these bytes are authentic.
func PublicKey() ed25519.PublicKey {
	if publicKeyHex == "" {
		return nil
	}
	raw, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		// A malformed compiled-in key is a build-time programming error, the same
		// class as a malformed embedded policy. Returning nil rather than panicking
		// is the safer failure here: a panic would make every villa verb unusable,
		// while nil degrades to "cannot verify", which only disables update
		// signalling. HasPublicKey lets a caller report the difference.
		return nil
	}
	return ed25519.PublicKey(raw)
}

// HasPublicKey reports whether this binary can verify a manifest at all.
//
// It exists so `villa update --check` can say "this build carries no verification
// key" rather than "the signature failed", which would send a user looking for a
// tampered download when the truth is that no key has been published yet.
func HasPublicKey() bool { return PublicKey() != nil }
