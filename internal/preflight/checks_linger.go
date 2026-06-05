package preflight

import (
	"fmt"
	"os"
	"os/user"
	"strings"
)

// lingerDeps are the injectable seams for PRE-03 so tests feed fixture loginctl
// output instead of shelling to a real systemd.
type lingerDeps struct {
	username string
	// lingerOutput returns the `loginctl show-user $USER --property=Linger`
	// output, whether loginctl was found, and whether it exited cleanly.
	lingerOutput func(username string) (out string, found, ok bool)
}

// liveLingerDeps wires the PRE-03 check to the real host.
func liveLingerDeps() lingerDeps {
	uname := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		uname = u.Username
	}
	return lingerDeps{
		username: uname,
		lingerOutput: func(username string) (string, bool, bool) {
			// Fixed-arg exec (threat T-03-01): loginctl show-user <user> --property=Linger.
			return runTool("loginctl", "show-user", username, "--property=Linger")
		},
	}
}

// checkLinger is PRE-03 (always WARN, D-02): user lingering keeps the rootless
// user manager (and thus the Quadlet-managed services) running across logout /
// reboot. Without it the stack stops when the user logs out — a boot-survival
// degradation, NOT an immediate crash, so this is a WARN tier even when off.
//
// loginctl missing/unparseable → still WARN (the WARN tier subsumes the D-15
// downgrade: an unevaluable WARN is simply a WARN).
func checkLinger(d lingerDeps) CheckResult {
	const (
		id   = "PRE-03"
		name = "User lingering enabled"
	)
	remediation := fmt.Sprintf("Enable lingering so services survive logout/reboot: `loginctl enable-linger %s`.", d.username)

	out, found, ok := d.lingerOutput(d.username)
	if !found {
		return warn(id, name, TierWarn,
			"loginctl not available — could not verify lingering",
			remediation, "exec.LookPath(loginctl)", "")
	}
	if !ok || strings.TrimSpace(out) == "" {
		return warn(id, name, TierWarn,
			"loginctl produced no Linger value — could not verify lingering",
			remediation, "loginctl show-user --property=Linger", capRaw(out))
	}

	// Output is "Linger=yes" or "Linger=no".
	_, val, cut := strings.Cut(strings.TrimSpace(out), "=")
	if !cut {
		return warn(id, name, TierWarn,
			"loginctl Linger value unparseable — could not verify lingering",
			remediation, "loginctl show-user --property=Linger", capRaw(out))
	}
	if strings.EqualFold(strings.TrimSpace(val), "yes") {
		return pass(id, name, TierWarn,
			fmt.Sprintf("lingering is enabled for %q", d.username),
			"loginctl show-user --property=Linger")
	}
	return warn(id, name, TierWarn,
		fmt.Sprintf("lingering is NOT enabled for %q — services will stop on logout", d.username),
		remediation, "loginctl show-user --property=Linger", "")
}

// capRaw bounds a captured raw string for the -v provenance field.
func capRaw(s string) string {
	if len(s) > maxToolOutput {
		return s[:maxToolOutput]
	}
	return s
}
