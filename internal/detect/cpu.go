package detect

import (
	"bufio"
	"os"
	"runtime"
	"strings"
)

// cpuInfo returns the CPU model and architecture, degrading to typed Unknown
// when the host cannot be read.
//
// The architecture comes from the Go runtime, which cannot fail. The model
// string is the "model name" field of /proc/cpuinfo — the same procfs the
// memory probe already parses by hand, read directly rather than through a
// cross-platform inventory library whose PCI-ID database and Windows backends
// never execute on the Fedora target.
func cpuInfo() (model Str, arch Str) {
	arch = KnownStr(runtime.GOARCH, "runtime.GOARCH")
	return cpuModel(liveProcCPUInfo), arch
}

// cpuModel parses the first "model name" line of a /proc/cpuinfo-shaped file.
// The path is a seam so tests can point it at a fixture.
//
// Every processor block repeats the same model name on a single-socket host, so
// the first is enough; a host with heterogeneous sockets would need more, and is
// out of scope for a Strix Halo target.
func cpuModel(path string) Str {
	f, err := os.Open(path) //nolint:gosec // fixed procfs path, or a test fixture
	if err != nil {
		return UnknownStr("cpuinfo unreadable", errString(err))
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "model name" {
			continue
		}
		name := strings.TrimSpace(value)
		if name == "" {
			return UnknownStr("cpuinfo model name blank", line)
		}
		return KnownStr(name, path+":model name")
	}
	// A scan error is otherwise indistinguishable from a clean EOF; surface the
	// real reason rather than mislabeling an I/O failure as a missing field
	// (the same distinction memAvailableBytes draws, WR-05/D-16).
	if err := sc.Err(); err != nil {
		return UnknownStr("cpuinfo read error", err.Error())
	}
	return UnknownStr("cpuinfo model name not found", "")
}

// errString renders an error for the Raw capture field without panicking on nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
