package orchestrate

import (
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

func sandboxFixtureInput() SandboxRunInput {
	return SandboxRunInput{
		Cfg:           config.VillaConfig{WorkspaceAgent: true},
		Image:         "localhost/villa-sandbox:office@sha256:" + strings.Repeat("a", 64),
		Workspace:     "/home/villa/Documents/ledger",
		TaskID:        "20260910-120000-ab12",
		HostVillaPath: "/home/villa/.local/bin/villa",
		CrushPath:     "/home/villa/.local/share/villa/bin/crush",
	}
}

// TestRenderSandboxRunArgs freezes the launch line. It is the whole boundary: the
// flags below are what stands between a task and the host, so a dropped one is a
// silently weaker sandbox rather than a failure anything reports.
func TestRenderSandboxRunArgs(t *testing.T) {
	in := sandboxFixtureInput()
	got, err := RenderSandboxRun(in)
	if err != nil {
		t.Fatalf("RenderSandboxRun: %v", err)
	}
	want := []string{
		"run", "--rm", "-i", "--init",
		"--name", "villa-task-20260910-120000-ab12",
		"--runtime=krun",
		"--network", "villa-sandbox",
		"--read-only",
		"--tmpfs", "/tmp",
		"--memory", "8g",
		"--cpus", "4",
		"--volume", "/home/villa/Documents/ledger:/workspace:Z",
		"--volume", "/home/villa/.local/bin/villa:/usr/local/bin/villa:ro,z",
		"--volume", "/home/villa/.local/share/villa/bin/crush:/usr/local/bin/crush:ro,z",
		"-w", "/workspace",
		"-e", "HOME=/tmp",
		"-e", "CRUSH_GLOBAL_CONFIG=/tmp/crushcfg",
		"-e", "CRUSH_GLOBAL_DATA=/tmp/crushdata",
		"-e", "CRUSH_DISABLE_METRICS=1",
		"-e", "DO_NOT_TRACK=1",
		"-e", "CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1",
		in.Image,
		"villa", "sandbox-bridge",
	}
	if !slices.Equal(got, want) {
		t.Errorf("RenderSandboxRun args differ.\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, " "), strings.Join(want, " "))
	}
}

// TestRenderSandboxRunTakesMemoryAndCPUsFromConfig: the two tunables are the only
// values config supplies, and an empty config renders the defaults rather than an
// empty --memory that podman would reject.
func TestRenderSandboxRunTakesMemoryAndCPUsFromConfig(t *testing.T) {
	in := sandboxFixtureInput()
	in.Cfg.SandboxMemory = "12g"
	in.Cfg.SandboxCPUs = 6
	got, err := RenderSandboxRun(in)
	if err != nil {
		t.Fatalf("RenderSandboxRun: %v", err)
	}
	if v := valueAfter(got, "--memory"); v != "12g" {
		t.Errorf("--memory = %q, want 12g", v)
	}
	if v := valueAfter(got, "--cpus"); v != "6" {
		t.Errorf("--cpus = %q, want 6", v)
	}
}

// TestSandboxRunMountsNoDeviceAndNoModels: the task has no GPU and no weights. A
// device or the models volume appearing here would hand the agent the inference
// host's hardware, which is exactly what the microVM exists to withhold.
func TestSandboxRunMountsNoDeviceAndNoModels(t *testing.T) {
	got, err := RenderSandboxRun(sandboxFixtureInput())
	if err != nil {
		t.Fatalf("RenderSandboxRun: %v", err)
	}
	line := strings.Join(got, " ")
	for _, forbidden := range []string{"/dev/dri", "/dev/kfd", "--device", "--gpus", volumeName, "/models", networkAttach} {
		if strings.Contains(line, forbidden) {
			t.Errorf("sandbox run args carry %q, which the sandbox must never have:\n%s", forbidden, line)
		}
	}
}

// TestRenderSandboxRunRefusesAnIncompleteInput: every path is supplied by the
// caller, and an empty one would render a mount podman resolves relative to its
// own cwd. Fail closed, per the untrusted-input rule.
func TestRenderSandboxRunRefusesAnIncompleteInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(*SandboxRunInput)
	}{
		{"no image", func(in *SandboxRunInput) { in.Image = "" }},
		{"no workspace", func(in *SandboxRunInput) { in.Workspace = "" }},
		{"relative workspace", func(in *SandboxRunInput) { in.Workspace = "ledger" }},
		{"no task id", func(in *SandboxRunInput) { in.TaskID = "" }},
		{"no villa binary", func(in *SandboxRunInput) { in.HostVillaPath = "" }},
		{"no crush binary", func(in *SandboxRunInput) { in.CrushPath = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := sandboxFixtureInput()
			tc.spoil(&in)
			if _, err := RenderSandboxRun(in); err == nil {
				t.Error("RenderSandboxRun accepted an incomplete input; it must refuse rather than render a partial mount")
			}
		})
	}
}

// TestSandboxNames: the two identities the runner and the network unit share.
func TestSandboxNames(t *testing.T) {
	if got := SandboxNetworkName(); got != "villa-sandbox" {
		t.Errorf("SandboxNetworkName() = %q, want villa-sandbox", got)
	}
	if got := SandboxContainerName("20260910-120000-ab12"); got != "villa-task-20260910-120000-ab12" {
		t.Errorf("SandboxContainerName() = %q", got)
	}
}

// TestSandboxImageIsDigestPinned: the image the pin table carries must be a digest,
// not a floating tag, or `villa update` has nothing to compare.
func TestSandboxImageIsDigestPinned(t *testing.T) {
	if !strings.Contains(SandboxImage(), "@sha256:") {
		t.Errorf("SandboxImage() = %q, want a digest-pinned reference", SandboxImage())
	}
}

func valueAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
