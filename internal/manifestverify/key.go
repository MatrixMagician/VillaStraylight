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
// # The unset state is honest, not broken
//
// No keypair has been generated yet: that is an open item for a human, recorded in
// the spec, because it must happen on a machine that is not this one and not CI.
// Until it does, publicKeyHex is empty, and an empty key means villa can verify
// NOTHING — every manifest is refused for a bad signature and every host falls back
// to compiled-in pins.
//
// That is the correct behaviour for the unset state, and it is worth being explicit
// about why. The alternative failure mode — treating "no key configured" as "skip
// the signature check" — would turn a missing key into an open door, and it would
// do so silently, on every host, at exactly the moment nobody was looking. A villa
// that can check nothing is strictly better than a villa that accepts anything.
//
// To set it: run `villa-manifest-sign keygen`, which prints the public key, and
// paste the hex here in the same release that publishes the first manifest.

import (
	"crypto/ed25519"
	"encoding/hex"
)

// publicKeyHex is the hex-encoded ed25519 public key manifests are verified
// against. Empty until a keypair is generated offline — see the file doc.
const publicKeyHex = ""

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
