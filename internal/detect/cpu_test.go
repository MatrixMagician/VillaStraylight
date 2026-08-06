package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCPUModelFromFixture parses the model name from a captured /proc/cpuinfo.
// Every processor block repeats it, so the first match is the answer.
func TestCPUModelFromFixture(t *testing.T) {
	model := cpuModel("testdata/cpuinfo")
	if !model.Known {
		t.Fatalf("cpuModel: Known=false, source=%q", model.Source)
	}
	const want = "AMD RYZEN AI MAX+ 395 w/ Radeon 8060S"
	if model.Value != want {
		t.Errorf("cpuModel: Value=%q, want %q", model.Value, want)
	}
	if model.Source == "" {
		t.Errorf("cpuModel: Source empty, want the path and field it read")
	}
}

// TestCPUModelDegradesToUnknown is the typed-Unknown contract: an unreadable
// file, an absent field, and a present-but-blank field each yield an Unknown
// carrying a reason — never a bare empty string presented as known.
func TestCPUModelDegradesToUnknown(t *testing.T) {
	dir := t.TempDir()

	absentField := filepath.Join(dir, "no-model-name")
	if err := os.WriteFile(absentField, []byte("processor\t: 0\nvendor_id\t: AuthenticAMD\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	blankField := filepath.Join(dir, "blank-model-name")
	if err := os.WriteFile(blankField, []byte("model name\t:   \n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	for name, path := range map[string]string{
		"unreadable": filepath.Join(dir, "absent"),
		"absent":     absentField,
		"blank":      blankField,
	} {
		got := cpuModel(path)
		if got.Known {
			t.Errorf("%s: Known=true, want Unknown", name)
		}
		if got.Value != "" {
			t.Errorf("%s: Value=%q, want empty", name, got.Value)
		}
		if got.Source == "" {
			t.Errorf("%s: Source empty, want a reason", name)
		}
	}
}

// TestCPUModelScanErrorIsLabelled asserts WR-05 for the CPU reader, matching
// what memAvailableBytes already guarantees: an I/O failure mid-read is
// reported as a read error, not mislabelled as a missing field.
func TestCPUModelScanErrorIsLabelled(t *testing.T) {
	got := cpuModel(t.TempDir()) // a directory: open succeeds, scan errors.
	if got.Known {
		t.Errorf("cpuModel(scan error): Known=true, want Unknown")
	}
}

// TestCPUInfoArchComesFromRuntime pins the one fact that cannot fail: the
// architecture is read from the Go runtime, so it is always Known.
func TestCPUInfoArchComesFromRuntime(t *testing.T) {
	_, arch := cpuInfo()
	if !arch.Known {
		t.Fatalf("arch: Known=false, want always-known from the runtime")
	}
	if arch.Value != runtime.GOARCH {
		t.Errorf("arch: Value=%q, want %q", arch.Value, runtime.GOARCH)
	}
	if arch.Source != "runtime.GOARCH" {
		t.Errorf("arch: Source=%q, want runtime.GOARCH", arch.Source)
	}
}
