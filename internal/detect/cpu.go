package detect

import (
	"runtime"
	"strings"
)

// cpuInfo returns the CPU model and architecture, degrading to typed Unknown
// when the host cannot be read.
//
// The architecture comes from the Go runtime, which cannot fail. The model
// string is the "model name" field of /proc/cpuinfo — the same procfs the
// memory probe already parses, read directly rather than through a
// cross-platform inventory library whose PCI-ID database and Windows backends
// never execute on the Fedora target.
func cpuInfo() (model Str, arch Str) {
	arch = KnownStr(runtime.GOARCH, "runtime.GOARCH")
	return cpuModel(liveProcCPUInfo), arch
}

// cpuModelField is the /proc/cpuinfo key holding the human-readable CPU name.
const cpuModelField = "model name"

// cpuModel parses the first "model name" line of a /proc/cpuinfo-shaped file.
// The path is a seam so tests can point it at a fixture.
//
// Every processor block repeats the same model name on a single-socket host, so
// the first is enough; a host with heterogeneous sockets would need more, and is
// out of scope for a Strix Halo target.
func cpuModel(path string) Str {
	line, res, err := findLine(path, cpuModelField)
	switch res {
	case lineUnopenable:
		return UnknownStr("cpuinfo unreadable", errString(err))
	case lineReadFailed:
		return UnknownStr("cpuinfo read error", errString(err))
	case lineAbsent:
		return UnknownStr("cpuinfo "+cpuModelField+" not found", "")
	}

	_, value, ok := strings.Cut(line, ":")
	if !ok {
		return UnknownStr("cpuinfo "+cpuModelField+" malformed", line)
	}
	name := strings.TrimSpace(value)
	if name == "" {
		return UnknownStr("cpuinfo "+cpuModelField+" blank", line)
	}
	return KnownStr(name, path+":"+cpuModelField)
}

// errString renders an error for the Raw capture field without panicking on nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
