// Package updatecheck is the read-only half of `villa update`: compare what this
// host runs against what a manifest offers, and report — changing nothing.
//
// # The most important output is the one that says nothing
//
// With no valid manifest, `--check` reports COULD NOT CHECK. It must never read as
// "you are up to date". The cost of that misreading is a user believing they are
// current for months, so the Reject is verbose on purpose and names the misreading
// it is refusing.
//
// This is also why there is no fallback to per-component registry calls. The SUBSET
// of images villa would ask about leaks which addons are enabled, and a fingerprint
// cannot be un-emitted. Silence is the decision; `--from-registries` exists for a
// user who consciously chooses otherwise, with the cost stated where they choose it.
//
// # Rebuilt is not a new version
//
// A moved digest on `rocm-7.2.4` means THE SAME DECLARED VERSION WAS REBUILT
// upstream — the image villa validated on hardware is no longer the image that tag
// names, while nothing about "ROCm 7.2.4" changed. A moved digest on `:main` means
// a new upstream build, with no version to name. A new Crush release is a genuine
// version bump. Flattening all three into "update available" is the dishonest
// shortcut this verb exists to avoid, so Change is an enum carried into both the
// table and the JSON.
//
// # It works on a stopped stack
//
// Nothing here touches the running stack: it reads config, the pin state store and
// a manifest. That asymmetry with the apply path is real and is stated in help
// text, because "villa up first" is a confusing thing to be told by a command that
// changes nothing.
//
// PURE: config, resolver and verdict in, report out. No HTTP, no clock, no store
// access. The fetch is the caller's, which is what makes "villa contacts exactly
// one host" assertable against a single fake.
package updatecheck

import (
	"sort"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
	"github.com/MatrixMagician/VillaStraylight/internal/manifestverify"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// SchemaVersion is the --json contract's version. It is byte-frozen by a golden
// test and evolves append-only, like every other --json surface in this tree.
const SchemaVersion = 1

// Result is the top-level outcome of a check.
type Result string

const (
	// ResultChecked: villa reached a conclusion. Whether updates exist is a
	// separate question, answered by the summary.
	ResultChecked Result = "checked"
	// ResultCouldNotCheck: villa reached no conclusion. NOT "nothing available" —
	// the two are different facts and only one of them is knowledge.
	ResultCouldNotCheck Result = "could_not_check"
)

// Change is what a moved pin MEANS for one component.
type Change string

const (
	// ChangeNone: the available pin is the one already running.
	ChangeNone Change = "none"
	// ChangeRebuilt: the same declared version was rebuilt upstream. Not a bump.
	ChangeRebuilt Change = "rebuilt"
	// ChangeNewVersion: a genuinely newer release.
	ChangeNewVersion Change = "new_version"
)

// SubsystemState is what a whole subsystem's row says.
type SubsystemState string

const (
	// StateCurrent: every component is at the pin the manifest offers.
	StateCurrent SubsystemState = "current"
	// StateUpdateAvailable: at least one component has somewhere to move.
	StateUpdateAvailable SubsystemState = "update_available"
	// StateSkipped: this subsystem is not part of the installed footprint. The row
	// is still present, carrying its reason — omitting it would hide the decision.
	StateSkipped SubsystemState = "skipped"
)

// Component is one component's row.
type Component struct {
	Name string `json:"name"`
	// PinShape is exposed so a consumer can tell WHY a rebuilt is not a version
	// bump, rather than having to trust villa's classification.
	PinShape string `json:"pin_shape"`
	// Vetted is what villa shipped, Effective what this host runs, Available what
	// the manifest offers. Three values because they are three different claims,
	// and any pair of them can differ.
	Vetted    string `json:"vetted"`
	Effective string `json:"effective"`
	Available string `json:"available,omitempty"`
	Change    Change `json:"change"`
}

// Subsystem is one subsystem's row, holding its components.
type Subsystem struct {
	Name       string         `json:"name"`
	State      SubsystemState `json:"state"`
	SkipReason string         `json:"skip_reason,omitempty"`
	Components []Component    `json:"components"`
}

// ManifestInfo describes the manifest the check was made against.
type ManifestInfo struct {
	// State is the verifier's outcome, verbatim: accepted, absent or refused.
	State string `json:"state"`
	// Reason is the verifier's reason, so a consumer can distinguish an expired
	// manifest from a refused downgrade without parsing prose.
	Reason     string `json:"reason,omitempty"`
	Serial     uint64 `json:"serial,omitempty"`
	ValidUntil string `json:"valid_until,omitempty"`
}

// Summary counts the subsystems. It is a POINTER on Report so it can be null: a
// script reading summary.updatable on a Reject must get a null, never a zero that
// reads as "you are current". The absent-is-not-zero discipline verifystate already
// applies.
type Summary struct {
	Updatable int `json:"updatable"`
	Current   int `json:"current"`
	Skipped   int `json:"skipped"`
}

// VillaRelease is the one row that deliberately cannot be acted on.
//
// villa reports a newer version of itself and never installs it: self-replacement
// breaks the prove step (the binary proving the update is the binary being
// replaced) and the websafe bind-mount (the running binary is mounted into a
// container). Saying so inline is better than a row that looks actionable.
type VillaRelease struct {
	Current         string `json:"current"`
	Available       string `json:"available,omitempty"`
	Change          Change `json:"change"`
	AppliedByUpdate bool   `json:"applied_by_update"`
}

// Report is the whole check outcome, and the --json document.
type Report struct {
	SchemaVersion int    `json:"schema_version"`
	Result        Result `json:"result"`
	// CheckedAt is when THIS check completed, or empty on a Reject — villa did not
	// check, so recording a time would imply it had.
	CheckedAt string `json:"checked_at,omitempty"`
	// LastSuccessfulCheck is the previously-recorded check and is what makes
	// staleness legible: "last checked 146 days ago" is the honest signal villa
	// traded automation away for.
	LastSuccessfulCheck string        `json:"last_successful_check,omitempty"`
	Manifest            ManifestInfo  `json:"manifest"`
	Subsystems          []Subsystem   `json:"subsystems"`
	Villa               *VillaRelease `json:"villa,omitempty"`
	// Summary is nil on a Reject. See the Summary doc.
	Summary *Summary `json:"summary"`
	// Message is the human explanation, carried on the report so the Reject wording
	// has exactly one home. Rendering it at the command tier would let the most
	// important sentence in the verb drift per call site.
	Message string `json:"-"`
}

// Input is everything a report is derived from.
type Input struct {
	// Cfg decides the installed footprint: which addons are on.
	Cfg config.VillaConfig
	// Resolver answers what this host runs, per component.
	Resolver pinresolve.Resolver
	// Verdict is the manifest verifier's answer. It is passed in rather than
	// computed, because verifying needs bytes and fetching bytes is I/O.
	Verdict manifestverify.Verdict
	// CheckedAt is this check's timestamp, injected so the report is deterministic.
	CheckedAt string
	// LastSuccessfulCheck is what the pin state store recorded previously.
	LastSuccessfulCheck string
	// VillaVersion is this binary's version, for the report-but-never-apply row.
	VillaVersion string
}

// Check builds the report.
func Check(in Input) Report {
	if in.Verdict.Outcome != manifestverify.Accepted {
		return reject(in)
	}

	available := map[string]manifest.Component{}
	for _, c := range in.Verdict.Doc.Components {
		available[c.ID] = c
	}

	report := Report{
		SchemaVersion: SchemaVersion,
		Result:        ResultChecked,
		CheckedAt:     in.CheckedAt,
		Manifest: ManifestInfo{
			State:      string(in.Verdict.Outcome),
			Serial:     in.Verdict.Doc.Serial,
			ValidUntil: in.Verdict.Doc.ValidUntil,
		},
		Summary: &Summary{},
	}

	for _, k := range subsystem.Every {
		row, counted := subsystemRow(in, k, available)
		if row.Name == "" {
			continue
		}
		report.Subsystems = append(report.Subsystems, row)
		switch counted {
		case StateUpdateAvailable:
			report.Summary.Updatable++
		case StateCurrent:
			report.Summary.Current++
		case StateSkipped:
			report.Summary.Skipped++
		}
	}

	report.Villa = villaRow(in)
	report.Message = summaryLine(report)
	return report
}

// reject builds the could-not-check report.
//
// Summary stays nil and CheckedAt stays empty. Both are the absent-is-not-zero
// discipline: a script reading summary.updatable gets null, and nothing in the
// document implies villa learned anything.
func reject(in Input) Report {
	return Report{
		SchemaVersion:       SchemaVersion,
		Result:              ResultCouldNotCheck,
		LastSuccessfulCheck: in.LastSuccessfulCheck,
		Manifest: ManifestInfo{
			State:  string(in.Verdict.Outcome),
			Reason: string(in.Verdict.Reason),
		},
		Subsystems: nil,
		Summary:    nil,
		Message:    in.Verdict.Message,
	}
}

// subsystemRow builds one subsystem's row, or an empty one for a subsystem with no
// pinned components at all.
func subsystemRow(in Input, k subsystem.Kind, available map[string]manifest.Component) (Subsystem, SubsystemState) {
	entries := pins.For(k)
	if len(entries) == 0 {
		// Coding mode is a configuration of the stack, not a component of it.
		return Subsystem{}, ""
	}

	// The working set is the INSTALLED FOOTPRINT, not the pin table. A disabled
	// addon is skipped WITH ITS REASON, because omitting the row would hide the
	// decision — and because `update` can only prove what is installed.
	if !subsystem.On(in.Cfg, k) {
		return Subsystem{
			Name:       k.String(),
			State:      StateSkipped,
			SkipReason: k.ConfigKey() + " = false",
			Components: []Component{},
		}, StateSkipped
	}

	row := Subsystem{Name: k.String(), State: StateCurrent, Components: []Component{}}
	for _, e := range entries {
		// Only the ACTIVE backend is a row. Proving a non-active backend would
		// require swapping to it, so four backends would mean four swaps and four
		// residency proofs in one run — and freshening the vulkan landing spot
		// unproven would degrade the very thing rollback depends on.
		if k == subsystem.Inference && !isActiveBackend(in.Cfg, e.Component) {
			continue
		}
		res, ok := in.Resolver.Resolve(e.Component)
		if !ok {
			continue
		}
		c := Component{
			Name:      string(e.Component),
			PinShape:  string(e.Shape),
			Vetted:    res.Vetted.Ref,
			Effective: res.Current.Ref,
			Change:    ChangeNone,
		}
		if offer, has := available[string(e.Component)]; has && offer.Ref != res.Current.Ref {
			c.Available = offer.Ref
			c.Change = classify(e.Shape, res.Current, offer)
			row.State = StateUpdateAvailable
		}
		row.Components = append(row.Components, c)
	}
	return row, row.State
}

// classify decides whether a moved pin is a rebuild or a genuine new version.
//
// The rule follows the SHAPE, which is why shape is data on the table rather than
// something re-derived here:
//
//   - A version_tag whose declared version is unchanged is a REBUILD. This is the
//     row that matters: `rocm-7.2.4` moving digest means the image villa validated
//     on hardware is no longer the image that tag names, while nothing about the
//     version changed. Calling it a new version would be false.
//   - A version_tag whose version DID change, and any checksummed asset with a new
//     ref, is a new version.
//   - A rolling_digest has no version to compare, so a moved digest is a rebuild by
//     definition — that is what "rolling" means.
func classify(shape pins.Shape, current pins.Pin, offer manifest.Component) Change {
	switch shape {
	case pins.RollingDigest:
		return ChangeRebuilt
	case pins.ChecksummedAsset:
		if offer.Ref != current.Ref {
			return ChangeNewVersion
		}
		return ChangeNone
	case pins.VersionTag:
		if declaredVersion(offer.Ref) == declaredVersion(current.Ref) {
			return ChangeRebuilt
		}
		return ChangeNewVersion
	}
	return ChangeNone
}

// declaredVersion extracts the tag from an image reference, which is the version a
// version_tag component declares. A reference with no tag has no declared version,
// and two of those compare equal — which is correct: without a tag there is nothing
// to say a version changed.
func declaredVersion(ref string) string {
	_, rest, found := strings.Cut(ref, "/")
	if !found {
		rest = ref
	}
	// Strip the digest before looking for the tag, so the sha256: colon is not
	// mistaken for the tag separator.
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[:at]
	}
	if colon := strings.LastIndex(rest, ":"); colon >= 0 {
		return rest[colon+1:]
	}
	return ""
}

// isActiveBackend reports whether a backend component is the one this config runs.
func isActiveBackend(cfg config.VillaConfig, id pins.ComponentID) bool {
	want := map[string]pins.ComponentID{
		"":                   pins.BackendROCm724,
		"rocm":               pins.BackendROCm724,
		"rocm-6.4.4":         pins.BackendROCm644,
		"rocm-6.4.4-rocwmma": pins.BackendROCm644WMMA,
		"vulkan":             pins.BackendVulkan,
	}
	active, ok := want[cfg.Backend]
	if !ok {
		// An unknown backend string never reaches here in practice — BackendFor
		// fails closed on it long before — but reporting no active backend is the
		// safe reading: a row villa cannot identify is a row it should not offer to
		// update.
		return false
	}
	return active == id
}

// villaRow builds the report-but-never-apply row.
//
// It reports whatever version the accepted manifest names, and AppliedByUpdate is a
// hard false rather than a field a future change might flip: villa does not replace
// itself.
func villaRow(in Input) *VillaRelease {
	v := &VillaRelease{
		Current:         in.VillaVersion,
		Change:          ChangeNone,
		AppliedByUpdate: false,
	}
	if offered := in.Verdict.Doc.VillaVersion; offered != "" && offered != in.VillaVersion {
		v.Available = offered
		v.Change = ChangeNewVersion
	}
	return v
}

// summaryLine is the one-sentence human summary under the table.
func summaryLine(r Report) string {
	if r.Summary == nil {
		return ""
	}
	switch {
	case r.Summary.Updatable == 0:
		return "Everything installed is at the pin the manifest offers."
	case r.Summary.Updatable == 1:
		return "1 subsystem has updates available."
	default:
		return sortableCount(r.Summary.Updatable) + " subsystems have updates available."
	}
}

// sortableCount renders a small count without pulling strconv in for one call.
func sortableCount(n int) string {
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if len(digits) == 0 {
		return "0"
	}
	return string(digits)
}

// Updatable lists the subsystems with somewhere to move, in stack order. It is what
// the apply path consumes, so `villa update` with no arguments and `--check` can
// never disagree about what would be updated.
func Updatable(r Report) []string {
	var out []string
	for _, s := range r.Subsystems {
		if s.State == StateUpdateAvailable {
			out = append(out, s.Name)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return order(out[i]) < order(out[j]) })
	return out
}

// order is the apply sequence: inference → chat → memory → search → agent.
func order(name string) int {
	for i, k := range subsystem.Every {
		if k.String() == name {
			return i
		}
	}
	return len(subsystem.Every)
}
