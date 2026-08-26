// Package manifest is the wire format for a pin manifest: the serialised form of
// the compiled-in pins.Table, signed offline by villa's maintainer and published as
// a release asset.
//
// # What a manifest may and may not do
//
// It supplies VALUES ONLY. New digests for components the compiled-in table already
// names, new version tags, new floors, new checksums — all fine. A component the
// table does not name, a registry host not already present, a changed URL template
// — all refused.
//
// That rule is the whole security model of the thing. A manifest that could
// introduce components and URL templates would be a remote-code-execution channel
// with extra steps: sign a document naming a new "component" whose bytes come from
// anywhere, and villa fetches and runs it. Under the values-only rule a stolen
// signing key can move a host to a bad VERSION of a component it already runs —
// bounded, and the prove step still has to pass — but cannot make villa fetch from
// a host the operator never trusted.
//
// The consequence is deliberate: adding a component to the stack always requires a
// new villa release. Introducing a new executable is exactly the decision that
// should require shipping a binary you signed, not publishing a JSON file.
//
// # Canonical bytes
//
// A signature covers BYTES, so the signer and the verifier must agree exactly on
// which ones. The rule here is the simplest one that cannot drift: THE SIGNATURE
// COVERS THE MANIFEST FILE VERBATIM, byte for byte as published. Villa never
// re-serialises a manifest before verifying it, and the signer signs exactly the
// bytes it writes.
//
// The alternative — canonicalise, then sign the canonical form — was rejected. It
// requires both ends to implement the same canonicalisation (key ordering, number
// formatting, unicode escaping), and Go's encoding/json makes no stability promise
// across versions for any of those. A mismatch there produces a manifest that fails
// to verify for no visible reason, which is the worst possible failure mode for a
// security check: indistinguishable from an attack.
//
// Verbatim signing means a manifest is an opaque blob plus a signature until the
// signature checks out, which is also the right order of operations — nothing is
// parsed as meaningful until it has been shown to be authentic.
//
// # Signing is not villa's job
//
// The signing tool is a separate binary (cmd/villa-manifest-sign), not a `villa`
// subcommand. It runs on the maintainer's machine beside the private key, and villa
// itself only ever VERIFIES. Shipping a sign verb in the binary every user runs
// would put key-handling code on every machine that will never hold a key.
//
// crypto/ed25519 is standard library, so none of this costs a dependency.
package manifest

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// SchemaVersion is the manifest format's own version, independent of every store's.
// A manifest declaring another version is refused rather than reinterpreted: an
// unknown format is not a format villa knows how to be safe about.
const SchemaVersion = 1

// Component is one component's row in a manifest — the serialised form of a
// pins.Entry, minus the accessor.
type Component struct {
	// ID must name a component the compiled-in table already carries. This is the
	// allowlist's first half.
	ID string `json:"id"`
	// Registry must be a host the compiled-in table already pulls from. This is the
	// allowlist's second half.
	Registry string `json:"registry"`
	// Shape is what a moved pin means for this component. It must match the
	// compiled-in shape: a manifest that could restyle a version_tag component as a
	// rolling_digest would change how --check DESCRIBES a change, which is a claim
	// about hardware testing that a manifest has no standing to make.
	Shape string `json:"shape"`
	// Version is the declared upstream version, for a version_tag or
	// checksummed_asset component.
	Version string `json:"version,omitempty"`
	// Ref is the new pin: a digest-pinned image reference, or a release version for
	// a checksummed asset.
	Ref string `json:"ref"`
	// Checksum is the release artifact checksum, for a checksummed asset only.
	Checksum string `json:"checksum,omitempty"`
	// Floors are the host thresholds this pin demands, where the component has
	// them. They travel with the pin because a registry cannot report the floor a
	// digest needs — a floor is a claim about what was tested on hardware.
	Floors *Floors `json:"floors,omitempty"`
}

// Floors is the serialised form of preflight.Floor.
type Floors struct {
	Kernel       string `json:"kernel,omitempty"`
	KernelTested string `json:"kernel_tested,omitempty"`
	Mesa         string `json:"mesa,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	FirmwareDeny string `json:"firmware_deny,omitempty"`
}

// Document is a whole manifest.
type Document struct {
	SchemaVersion int `json:"schema_version"`
	// Serial is a monotonically increasing counter. Villa refuses a manifest whose
	// serial is below the last one it accepted, which is what closes the
	// replay/downgrade attack a signature alone cannot: an attacker serving an OLD
	// manifest villa genuinely signed passes every signature check.
	Serial uint64 `json:"serial"`
	// ValidUntil is when this manifest stops being current, RFC3339 UTC. Past it
	// villa treats the manifest as ABSENT — not invalid — and falls back to
	// compiled-in pins, so a freeze attack degrades to fail-closed rather than
	// leaving a host trusting a document indefinitely.
	ValidUntil string `json:"valid_until"`
	// VillaVersion is the villa release these pins were vetted with, and
	// VillaChecksum the checksum of that release's binary asset. They are reported,
	// never acted on: villa does not replace itself, because self-replacement
	// breaks the prove step and the websafe bind-mount.
	VillaVersion  string `json:"villa_version,omitempty"`
	VillaChecksum string `json:"villa_checksum,omitempty"`
	// Components are the pins this manifest supplies.
	Components []Component `json:"components"`
}

// Marshal renders a document as the bytes that will be signed and published.
//
// Indented, because a manifest is a published artifact a human reviews before
// signing, and because the indentation is part of the bytes the signature covers —
// so it is produced here, once, rather than by whatever formatter happened to touch
// the file last.
func Marshal(doc Document) ([]byte, error) {
	doc.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("manifest: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// Parse decodes manifest bytes.
//
// It does NOT verify anything — not the signature, not the allowlist, not the
// serial. Parsing and judging are separate on purpose: internal/manifestverify owns
// every question of whether villa may act on a document, and a Parse that quietly
// enforced half of them would make the verifier's truth table a lie about where the
// checks live.
func Parse(data []byte) (Document, error) {
	var doc Document
	dec := json.NewDecoder(strings.NewReader(string(data)))
	// Unknown fields are refused rather than ignored. A manifest carrying a field
	// this villa does not understand is a manifest from a future format, and
	// silently dropping the field would act on a document while ignoring part of
	// what it says.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("manifest: parse: %w", err)
	}
	if doc.SchemaVersion != SchemaVersion {
		return Document{}, fmt.Errorf("manifest: schema version %d is not %d; this villa does not understand this manifest format",
			doc.SchemaVersion, SchemaVersion)
	}
	return doc, nil
}

// Sign produces a detached ed25519 signature over the manifest bytes VERBATIM.
//
// It takes bytes, not a Document, and that is the canonical-bytes decision made
// structural: a caller physically cannot sign one serialisation and publish
// another, because there is only ever one byte slice.
func Sign(key ed25519.PrivateKey, data []byte) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("manifest: sign: key is %d bytes, want %d", len(key), ed25519.PrivateKeySize)
	}
	return ed25519.Sign(key, data), nil
}

// Verify checks a detached signature against manifest bytes VERBATIM.
//
// Like Sign it takes bytes, so the verifier checks exactly what was published
// rather than a re-serialisation of what it parsed.
func Verify(key ed25519.PublicKey, data, sig []byte) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("manifest: verify: public key is %d bytes, want %d", len(key), ed25519.PublicKeySize)
	}
	if !ed25519.Verify(key, data, sig) {
		return ErrBadSignature
	}
	return nil
}

// ErrBadSignature is returned when a manifest's signature does not check out
// against the public key. It is a distinct error because the caller must be able to
// say "the signature failed" rather than "something was wrong", which sends a user
// hunting for the wrong problem.
var ErrBadSignature = errors.New("manifest: signature does not verify against the compiled-in public key")

// CheckAllowlist reports every way a document oversteps the compiled-in table.
//
// It returns ALL violations rather than the first, because this is what the
// publishing tool runs before signing: a maintainer fixing one refusal at a time
// across five round trips is how a manifest ends up published with the sixth still
// wrong. Villa's verifier calls it too, so a bad manifest is caught at publish time
// and, if that was skipped, again in the field.
func CheckAllowlist(doc Document) []error {
	var problems []error

	for _, c := range doc.Components {
		entry, ok := pins.Lookup(pins.ComponentID(c.ID))
		if !ok {
			problems = append(problems, fmt.Errorf(
				"component %q is not in villa's compiled-in table: a manifest may supply new VALUES for components villa already ships, never introduce one — adding a component requires a new villa release",
				c.ID))
			continue
		}
		if c.Registry != entry.Registry {
			problems = append(problems, fmt.Errorf(
				"component %q names registry %q, but villa pulls it from %q: a manifest may not redirect a component to a host villa does not already trust",
				c.ID, c.Registry, entry.Registry))
		}
		if c.Shape != string(entry.Shape) {
			problems = append(problems, fmt.Errorf(
				"component %q declares shape %q, but villa's table says %q: shape is what a moved pin MEANS, and a manifest has no standing to restyle it",
				c.ID, c.Shape, entry.Shape))
		}
		if strings.TrimSpace(c.Ref) == "" {
			problems = append(problems, fmt.Errorf("component %q supplies an empty pin", c.ID))
		}
		if entry.Shape == pins.ChecksummedAsset && strings.TrimSpace(c.Checksum) == "" {
			problems = append(problems, fmt.Errorf(
				"component %q is a checksummed asset with no checksum: there would be nothing to verify the download against", c.ID))
		}
		if c.Ref != "" && !refMatchesRegistry(entry.Shape, c.Ref, entry.Registry) {
			problems = append(problems, fmt.Errorf(
				"component %q supplies pin %q, whose host is not the declared registry %q",
				c.ID, c.Ref, entry.Registry))
		}
	}

	if doc.Serial == 0 {
		problems = append(problems, errors.New("serial is zero: zero means 'no floor', so a host would accept any later manifest including a replayed one"))
	}
	if doc.ValidUntil == "" {
		problems = append(problems, errors.New("valid_until is empty: a manifest with no expiry can be frozen and served forever"))
	} else if _, err := time.Parse(time.RFC3339, doc.ValidUntil); err != nil {
		problems = append(problems, fmt.Errorf("valid_until %q is not an RFC3339 timestamp: %w", doc.ValidUntil, err))
	}
	if len(doc.Components) == 0 {
		problems = append(problems, errors.New("the manifest supplies no components"))
	}

	return problems
}

// refMatchesRegistry checks that a supplied pin's host is the allowlisted one.
//
// A checksummed asset's ref is a version, not a reference carrying a host, so the
// check does not apply — its host lives in the compiled-in URL template, which a
// manifest cannot touch at all.
func refMatchesRegistry(shape pins.Shape, ref, registry string) bool {
	if shape == pins.ChecksummedAsset {
		return true
	}
	host, _, found := strings.Cut(ref, "/")
	if !found {
		return false
	}
	return host == registry
}

// FromTable renders the compiled-in table as a manifest document, which is the
// starting point a maintainer edits when publishing.
//
// Generating it beats hand-writing one: every id, registry and shape is correct by
// construction, so the allowlist check has nothing to catch except the values the
// maintainer deliberately changed.
func FromTable(serial uint64, validUntil time.Time) Document {
	doc := Document{
		SchemaVersion: SchemaVersion,
		Serial:        serial,
		ValidUntil:    validUntil.UTC().Format(time.RFC3339),
	}
	for _, e := range pins.Table() {
		pin := e.Vetted()
		c := Component{
			ID:       string(e.Component),
			Registry: e.Registry,
			Shape:    string(e.Shape),
			Version:  e.Version,
			Ref:      pin.Ref,
			Checksum: pin.Checksum,
		}
		if e.HasFloors() {
			f := e.Floors()
			c.Floors = &Floors{
				Kernel:       f.Kernel,
				KernelTested: f.KernelTested,
				Mesa:         f.Mesa,
				Firmware:     f.Firmware,
				FirmwareDeny: f.FirmwareDeny,
			}
		}
		doc.Components = append(doc.Components, c)
	}
	return doc
}
