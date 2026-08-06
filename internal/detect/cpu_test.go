package detect

// cpu_test.go guards the /proc/cpuinfo CPU-model reader and the runtime-sourced
// architecture: the fixture parse, the typed-Unknown degradation at every failure
// path (unreadable, absent, blank, mid-read I/O error), and the one fact that
// cannot fail. It mirrors memory_test.go's coverage of the sibling procfs reader.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if !strings.Contains(got.Source, "read error") {
		t.Errorf("cpuModel(scan error): Source=%q, want a read-error reason distinct from a missing field", got.Source)
	}

	// And the three failure modes must stay distinguishable from one another.
	unopenable := cpuModel(filepath.Join(t.TempDir(), "absent"))
	if unopenable.Source == got.Source {
		t.Errorf("an unopenable file and a failed read share the reason %q", got.Source)
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
