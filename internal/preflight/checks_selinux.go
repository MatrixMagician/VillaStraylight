package preflight

import "strings"

// checks_selinux.go adds PRE-05: the SELinux container_use_devices boolean gate.
//
// On an SELinux-enforcing Fedora host, a rootless container cannot pass /dev/dri
// through to the iGPU unless the container_use_devices boolean is on (Pitfall 5).
// With it off, the Quadlet-managed inference container is denied the device at
// start and llama.cpp silently falls back to CPU — the exact silent-failure this
// project exists to prevent. So this is a BLOCK-tier candidate: install offers to
// fix it (consent) and otherwise treats it as a block (overridable via --force).
//
// 03-RESEARCH A5 confirmed the boolean was not yet a preflight check; it is added
// here and wired into RunWithResources so install gates on it.
//
// Degradation (D-15): if getsebool is missing or its output is unparseable, the
// requirement cannot be EVALUATED and the BLOCK downgrades to a WARN ("could not
// verify"), never a false PASS — consistent with the rest of the package.

// setseboolRemediation is the exact privileged command install offers (D-04) and
// otherwise prints for copy-paste. It is the single source for both the preflight
// remediation string and the install consent prompt.
const setseboolRemediation = "setsebool -P container_use_devices=true"

// selinuxDeps is the injectable seam for PRE-05 so tests feed fixture getsebool
// output instead of shelling to a real SELinux host.
type selinuxDeps struct {
	// getsebool returns the `getsebool container_use_devices` output, whether the
	// binary was found on PATH, and whether it exited cleanly.
	getsebool func() (out string, found, ok bool)
}

// liveSELinuxDeps wires PRE-05 to the real host via the fixed-arg runTool seam.
func liveSELinuxDeps() selinuxDeps {
	return selinuxDeps{
		getsebool: func() (string, bool, bool) {
			// Fixed-arg exec (threat T-03-01): getsebool container_use_devices.
			return runTool("getsebool", "container_use_devices")
		},
	}
}

// checkSELinuxContainerDevices is PRE-05 (BLOCK): the SELinux container_use_devices
// boolean must be on so a rootless container can use /dev/dri. `getsebool` reports
// "container_use_devices --> on" / "--> off". On → PASS; off → WARN (BLOCK-tier,
// with the setsebool remediation); missing/unparseable → WARN (typed-Unknown
// degrade, never PASS).
func checkSELinuxContainerDevices(d selinuxDeps) CheckResult {
	const (
		id   = "PRE-05"
		name = "SELinux container_use_devices boolean"
	)
	remediation := "Allow rootless containers to use device nodes (needed for /dev/dri passthrough): `" + setseboolRemediation + "`."

	out, found, ok := d.getsebool()
	if !found {
		// getsebool absent — could be a non-SELinux host or missing policycoreutils.
		// Cannot evaluate → WARN, never a false PASS (D-15).
		return warn(id, name, TierBlock,
			"getsebool not available — could not verify the container_use_devices boolean",
			remediation, "exec.LookPath(getsebool)", "")
	}
	if !ok || strings.TrimSpace(out) == "" {
		return warn(id, name, TierBlock,
			"getsebool produced no value — could not verify the container_use_devices boolean",
			remediation, "getsebool container_use_devices", capRaw(out))
	}

	// Output form: "container_use_devices --> on" or "... --> off".
	_, val, cut := strings.Cut(strings.TrimSpace(out), "-->")
	if !cut {
		return warn(id, name, TierBlock,
			"getsebool output unparseable — could not verify the container_use_devices boolean",
			remediation, "getsebool container_use_devices", capRaw(out))
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "on":
		return pass(id, name, TierBlock,
			"SELinux container_use_devices is on (rootless /dev/dri passthrough allowed)",
			"getsebool container_use_devices")
	case "off":
		return warn(id, name, TierBlock,
			"SELinux container_use_devices is OFF — rootless containers cannot use /dev/dri; the iGPU will be denied and inference falls back to CPU",
			remediation, "getsebool container_use_devices", "")
	default:
		return warn(id, name, TierBlock,
			"SELinux container_use_devices value unrecognized — could not verify",
			remediation, "getsebool container_use_devices", capRaw(out))
	}
}
