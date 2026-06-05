package preflight

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
)

// podmanDeps are the injectable seams for the PRE-02 check so tests run against
// fixtures with no real podman/systemd and no real /etc/subuid.
type podmanDeps struct {
	// username and uid identify the current user; subuid/subgid are matched on
	// BOTH forms (Pitfall 6).
	username string
	uid      string
	// subuidPath/subgidPath point at the subordinate-id files (live: /etc/sub*).
	subuidPath string
	subgidPath string
	// podmanVersion returns podman's --version output, whether the binary was
	// found, and whether it exited cleanly. A missing binary (found=false) is
	// unevaluable → WARN downgrade (D-15).
	podmanVersion func() (out string, found, ok bool)
	// systemdUserOK reports whether `systemctl --user` is reachable, plus whether
	// the systemctl binary was found at all.
	systemdUserOK func() (ok, found bool)
}

// livePodmanDeps wires the PRE-02 check to the real host.
func livePodmanDeps() podmanDeps {
	uname := os.Getenv("USER")
	uidStr := strconv.Itoa(os.Getuid())
	if u, err := user.Current(); err == nil {
		if u.Username != "" {
			uname = u.Username
		}
		if u.Uid != "" {
			uidStr = u.Uid
		}
	}
	return podmanDeps{
		username:   uname,
		uid:        uidStr,
		subuidPath: "/etc/subuid",
		subgidPath: "/etc/subgid",
		podmanVersion: func() (string, bool, bool) {
			return runTool("podman", "--version")
		},
		systemdUserOK: func() (bool, bool) {
			// is-system-running returns non-zero for "degraded"/"starting" yet the
			// user manager is still reachable; treat a found binary that produced
			// any output as reachable. A truly unreachable user bus surfaces as the
			// binary being absent or producing no output.
			_, found, _ := runTool("systemctl", "--user", "is-system-running")
			return found, found
		},
	}
}

// checkPodmanRootless is PRE-02 (BLOCK): podman must be installed, the user must be
// rootless-ready (a non-empty subuid AND subgid range mapped for BOTH the username
// and the numeric UID — Pitfall 6), and `systemctl --user` must be reachable. Any
// of these missing means a rootless install cannot proceed.
//
// Degradation (D-15): if podman or systemctl is not even installed, the
// requirement cannot be evaluated as "ready vs not-ready" — that is a tooling gap,
// downgraded to WARN ("could not verify") rather than a confident FAIL. A present
// podman with absent subuid/subgid entries IS a confident known-bad → FAIL.
func checkPodmanRootless(d podmanDeps) CheckResult {
	const (
		id   = "PRE-02"
		name = "Podman rootless-ready"
	)
	remediation := fmt.Sprintf("Install podman, add subuid/subgid ranges for %q (`sudo usermod --add-subuids 524288-589823 --add-subgids 524288-589823 %s`), and ensure `systemctl --user` is reachable.", d.username, d.username)

	verOut, podmanFound, _ := d.podmanVersion()
	if !podmanFound {
		return warn(id, name, TierBlock,
			"podman is not installed — could not verify rootless readiness",
			remediation, "exec.LookPath(podman)", "")
	}

	sysOK, sysFound := d.systemdUserOK()
	if !sysFound {
		return warn(id, name, TierBlock,
			"systemctl not installed — could not verify the systemd --user manager",
			remediation, "exec.LookPath(systemctl)", "")
	}

	subuidOK, subuidErr := subuidReady(d.username, d.uid, d.subuidPath)
	subgidOK, subgidErr := subuidReady(d.username, d.uid, d.subgidPath)

	// A read/scan error means we could not EVALUATE the range (an I/O failure is
	// not a confident "no range"); downgrade to WARN per D-15 rather than
	// manufacturing a false BLOCK FAIL out of a transient error (WR-05).
	if subuidErr != nil || subgidErr != nil {
		errored := d.subuidPath
		readErr := subuidErr
		if subuidErr == nil {
			errored = d.subgidPath
			readErr = subgidErr
		}
		return warn(id, name, TierBlock,
			fmt.Sprintf("could not read %s to verify rootless readiness", errored),
			remediation, errored, readErr.Error())
	}

	if !subuidOK || !subgidOK {
		missing := "/etc/subuid"
		if subuidOK {
			missing = "/etc/subgid"
		}
		return fail(id, name,
			fmt.Sprintf("no subordinate-id range for %q (uid %s) in %s — rootless not ready", d.username, d.uid, missing),
			remediation, missing, "")
	}

	if !sysOK {
		return warn(id, name, TierBlock,
			"systemd --user manager is not reachable",
			remediation, "systemctl --user", "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("podman present (%s); subuid/subgid mapped for %q; systemd --user reachable", firstLine(verOut), d.username),
		joinProvenance("podman --version", d.subuidPath, d.subgidPath, "systemctl --user"))
}

// subuidReady reports whether path (an /etc/subuid- or /etc/subgid-formatted file)
// contains a non-empty subordinate range keyed by EITHER the username OR the
// numeric uid in its first field (Pitfall 6: entries may be keyed by either form,
// and checking only one yields a false "not ready"). A line looks like:
//
//	oliverh:524288:65536
//
// The match requires a count (third field) > 0 so a present-but-empty range does
// not count as ready.
//
// It returns a non-nil error only for a SCAN error (a truncated/interrupted read),
// which the caller must treat as unevaluable (WARN per D-15) rather than a
// confident "not ready" — an I/O failure must not manufacture a false BLOCK FAIL
// (WR-05). A genuinely absent file is (false, nil): a confident absence, not an
// error.
func subuidReady(username, uid, path string) (bool, error) {
	f, err := os.Open(path) //nolint:gosec // path is a fixed system file or a test seam
	if err != nil {
		// Absent/unopenable file → confidently not ready (no range), not an error.
		return false, nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 3 {
			continue
		}
		key := strings.TrimSpace(fields[0])
		if key != username && key != uid {
			continue
		}
		count, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil || count == 0 {
			continue
		}
		return true, nil
	}
	// Distinguish a real read error from a clean EOF that simply found no range.
	if err := sc.Err(); err != nil {
		return false, err
	}
	return false, nil
}

// firstLine returns the first line of s, trimmed — used to compress podman's
// --version output into a single table cell.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
