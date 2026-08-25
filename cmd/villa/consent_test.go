package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// TestNoAccidentalConsent is the adversarial guard on the consent path. The gaps
// this loop collects consent for run PRIVILEGED host commands (setsebool -P, and
// the loginctl enable-linger fix), so an accidentally-recorded yes is not a UX bug
// — it is villa running a privileged command the operator never approved.
//
// The rule: nothing but an explicit y/yes may produce consent. Not an empty line,
// not EOF, not whitespace, not a NUL byte, not "yes please", not "true", not a
// stray number. The uppercase-YES case is included deliberately so the test cannot
// pass vacuously — a loop that denied everything would satisfy every other case
// here, and that case proves consent is still reachable when actually given.
func TestNoAccidentalConsent(t *testing.T) {
	backend, _ := inference.BackendFor("vulkan")
	in := wizardInput{
		rec:     recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 1},
		backend: backend,
		checks:  []preflight.CheckResult{seloffCheck()},
	}

	// Every one of these must NOT produce consent=true for PRE-05 and must NOT
	// produce a successful (non-error) result.
	hostile := []string{
		"",                       // empty stdin
		"\n",                     // just a newline at the consent question
		"\n\n\n\n",               // all defaults
		"\x00\n\x00\n",           // NUL bytes
		"Y E S\n",                // spaced
		"yes please\n",           // extra words
		"1\n",                    // a number at a y/n question
		"true\n",                 // programmer yes
		"YES\nYES\n",             // uppercase (this one SHOULD consent - checked below)
		strings.Repeat("x\n", 5), // junk then EOF
		"\t\n",                   // whitespace
		"n\ny\n",                 // decline consent, approve install
	}

	for _, script := range hostile {
		res, err := runWizard(t.Context(), in, strings.NewReader(script), &strings.Builder{})
		consented := res.consentDecisions["PRE-05"]

		switch script {
		case "YES\nYES\n":
			// Explicit uppercase yes IS consent; assert it works so the test is not
			// vacuous (a loop that always denies would pass everything else).
			if !consented || err != nil {
				t.Errorf("explicit YES did not consent: consent=%v err=%v", consented, err)
			}
		case "n\ny\n":
			// Declined the gap but approved install: consent must be false, and the
			// run succeeds (gateInstall then blocks on the unconsented BLOCK gap).
			if consented {
				t.Errorf("a declined gap recorded consent=true")
			}
		default:
			if consented {
				t.Errorf("script %q produced consent=true for a PRIVILEGED command", script)
			}
			if err == nil {
				t.Errorf("script %q completed without an explicit approval (err=nil)", script)
			}
		}
	}
}
