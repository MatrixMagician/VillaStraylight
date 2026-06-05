package detect

import (
	"runtime"
	"strings"

	"github.com/jaypipes/ghw"
)

// cpuInfo returns the CPU model and architecture via ghw, degrading to typed
// Unknown when ghw cannot read the host (it never hard-errors on missing perms,
// but may return an error or an empty inventory).
func cpuInfo() (model Str, arch Str) {
	// Architecture is always knowable from the Go runtime; ghw is only needed
	// for the human-readable CPU model string.
	arch = KnownStr(runtime.GOARCH, "runtime.GOARCH")

	cpu, err := ghw.CPU()
	if err != nil || cpu == nil || len(cpu.Processors) == 0 {
		reason := "ghw CPU inventory empty"
		if err != nil {
			reason = "ghw.CPU error"
		}
		return UnknownStr(reason, errString(err)), arch
	}

	name := strings.TrimSpace(cpu.Processors[0].Model)
	if name == "" {
		name = strings.TrimSpace(cpu.Processors[0].Vendor)
	}
	if name == "" {
		return UnknownStr("ghw CPU model blank", ""), arch
	}
	return KnownStr(name, "ghw.CPU"), arch
}

// errString renders an error for the Raw capture field without panicking on nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
