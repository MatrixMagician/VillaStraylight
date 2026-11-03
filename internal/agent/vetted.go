// vetted.go records which version of each PATH-resolved coding agent the launch
// contract's egress claim was captured for (ADR-0039), and turns a mismatch with the
// installed version into a non-blocking Warning. An egress capture proves one build:
// a newer release can add a network path, so villa says so when the version it is
// about to launch is not the one that was measured. It never refuses a launch.
//
// Crush is not here: it is the villa-pinned binary and the policy table governs it.
package agent

import (
	"errors"
	"fmt"
	"regexp"
)

// The versions the launch contracts were captured against. A bump is a re-vet, not
// an edit: see "Re-vetting a launched coding agent" in docs/RELEASING.md.
const (
	// VettedClaudeVersion is the Claude Code build captured under claudeEnv, with a
	// settled home and the marketplace autoinstall off: no host contacted off the
	// model server (docs/prototype/qwen-code/RESULTS.md, "Crush and Claude Code", #385).
	VettedClaudeVersion = "2.1.317"
	// VettedQwenVersion is the Qwen Code build captured under qwenEnv plus villa's
	// settings.json: no host contacted off the model server (RESULTS.md, "Interactive
	// mode", #373, and the acceptance capture for `villa code --agent qwen`, #375).
	VettedQwenVersion = "0.26.6"
)

// versionRe finds the first semver-looking token in `--version` output: Claude Code
// prints "2.1.317 (Claude Code)", Qwen Code prints "0.26.6". A prerelease suffix is
// kept, since a prerelease is a different build from the vetted one.
var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?`)

// ParseAgentVersion returns the first semver-looking token of raw `--version` output.
func ParseAgentVersion(raw string) (string, bool) {
	v := versionRe.FindString(raw)
	return v, v != ""
}

// checkVetted probes the installed version of the agent at bin and returns the
// Warning to attach, if any. It must run only after every refusal gate has passed.
// A nil AgentVersion seam reads as unreadable, never as a silent pass.
func checkVetted(d Deps, bin string, env []string, label, vetted string) []Warning {
	var raw string
	err := errors.New("no version probe wired")
	if d.AgentVersion != nil {
		raw, err = d.AgentVersion(bin, env)
	}
	installed, ok := ParseAgentVersion(raw)
	switch {
	case err != nil || !ok:
		return []Warning{{
			Code: "agent_version_unknown",
			Msg: fmt.Sprintf("could not read the installed %s version, so villa cannot tell whether it is the vetted %s. "+
				"The privacy claim for this launch (ADR-0039) was captured for the vetted version only. "+
				"To re-vet, see \"Re-vetting a launched coding agent\" in docs/RELEASING.md.", label, vetted),
		}}
	case installed != vetted:
		return []Warning{{
			Code: "agent_version_unvetted",
			Msg: fmt.Sprintf("%s %s is installed, but villa's no-telemetry launch contract was captured for %s. "+
				"The privacy claim (ADR-0039) was captured for the vetted version only; a newer release can add a network path the capture did not see. "+
				"To re-vet, see \"Re-vetting a launched coding agent\" in docs/RELEASING.md.", label, installed, vetted),
		}}
	}
	return nil
}
