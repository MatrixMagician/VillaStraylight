package orchestrate

// sandbox.go owns the workspace agent's task container: the image literal, the
// two names the runner and the network unit share, and the pure render of the
// launch line (spec v1.11 3.4).
//
// The launch is a render rather than a command because the flags ARE the
// boundary. A task gets one workspace grant, a read-only root, a tmpfs, no
// device and no weights; every one of those is a flag, so a dropped flag is a
// silently weaker sandbox that nothing reports. Freezing the slice in a test is
// the only thing that notices.
//
// The image is a const here for the same reason every other managed-service
// image is a const in the package that renders it: internal/pins reads it
// through SandboxImage() and never re-types it.

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// sandboxImage is the villa-built office image, pinned by the digest of the build
// the Containerfile in build/sandbox produced. A dnf build is not byte
// reproducible, so a rebuild at the same package versions yields a new digest:
// the pin table carries this as a rolling digest for exactly that reason.
const sandboxImage = "localhost/villa-sandbox:office@sha256:4b6db115990284cce1b70f73b4c265a85df288f96dcee6178608a9852b572b74"

// SandboxImage returns the digest-pinned sandbox image so internal/pins can name
// it without holding the literal.
func SandboxImage() string { return sandboxImage }

// ComponentSandbox is the pin-table component id for the sandbox image.
// internal/pins derives its ComponentID from this constant rather than re-typing
// the string, the same direction as the render-path ids in orchestrate.go.
const ComponentSandbox = "sandbox-image"

// The sandbox network's identities. It is a SECOND network, deliberately not
// villa.network: villa.network has egress, and a task must reach llama-server and
// nothing else.
const (
	sandboxNetworkUnitName = "villa-sandbox.network"
	sandboxNetworkName     = "villa-sandbox"
	sandboxNetworkAttach   = "villa-sandbox.network"
)

// Defaults for the two tunables config supplies. 8g is above the prototype's 4g
// because LibreOffice recalculates with Java loaded; the spec's open item 4 is to
// measure it.
const (
	sandboxDefaultMemory = "8g"
	sandboxDefaultCPUs   = 4
)

// SandboxNetworkName returns the podman network name a task joins.
func SandboxNetworkName() string { return sandboxNetworkName }

// SandboxContainerName returns the container name for one task, so cancel can
// find it by name rather than by a handle the runner has to keep.
func SandboxContainerName(taskID string) string { return "villa-task-" + taskID }

// SandboxRunInput is the pure input to RenderSandboxRun. Every path is resolved
// by the caller: this package renders, it does not look anything up.
type SandboxRunInput struct {
	// Cfg supplies the two tunables, and nothing else.
	Cfg config.VillaConfig
	// Image is the effective sandbox image, resolved through the cmd tier's pin
	// seam. It is an input rather than SandboxImage() so an effective pin recorded
	// by `villa update` actually reaches the task.
	Image string
	// Workspace is the absolute host path of the registered grant, mounted rw.
	Workspace string
	// TaskID names the container.
	TaskID string
	// HostVillaPath is the running villa binary, bind-mounted read-only so the
	// guest can exec `villa sandbox-bridge`. The CGO-free build gate is what makes
	// that work.
	HostVillaPath string
	// CrushPath is the pinned Crush binary, bind-mounted read-only.
	CrushPath string
}

// RenderSandboxRun renders the arguments after the podman binary for one task's
// microVM. Pure: no filesystem, no exec.
func RenderSandboxRun(in SandboxRunInput) ([]string, error) {
	if in.Image == "" {
		return nil, fmt.Errorf("orchestrate: RenderSandboxRun: no sandbox image")
	}
	if in.TaskID == "" {
		return nil, fmt.Errorf("orchestrate: RenderSandboxRun: no task id")
	}
	for _, p := range []struct{ what, path string }{
		{"workspace", in.Workspace},
		{"villa binary", in.HostVillaPath},
		{"crush binary", in.CrushPath},
	} {
		if p.path == "" || !filepath.IsAbs(p.path) {
			return nil, fmt.Errorf("orchestrate: RenderSandboxRun: %s path %q is not absolute", p.what, p.path)
		}
	}

	memory := in.Cfg.SandboxMemory
	if memory == "" {
		memory = sandboxDefaultMemory
	}
	cpus := in.Cfg.SandboxCPUs
	if cpus <= 0 {
		cpus = sandboxDefaultCPUs
	}

	return []string{
		"run", "--rm", "-i", "--init",
		"--name", SandboxContainerName(in.TaskID),
		"--runtime=krun",
		"--network", sandboxNetworkName,
		"--read-only",
		"--tmpfs", "/tmp",
		"--memory", memory,
		"--cpus", strconv.Itoa(cpus),
		"--volume", in.Workspace + ":/workspace:Z",
		"--volume", in.HostVillaPath + ":/usr/local/bin/villa:ro,z",
		"--volume", in.CrushPath + ":/usr/local/bin/crush:ro,z",
		"-w", "/workspace",
		"-e", "HOME=/tmp",
		// Crush's config and data live on the tmpfs, not on the grant: the
		// prototype found a .crush/ directory left in the operator's workspace.
		"-e", "CRUSH_GLOBAL_CONFIG=/tmp/crushcfg",
		"-e", "CRUSH_GLOBAL_DATA=/tmp/crushdata",
		"-e", "CRUSH_DISABLE_METRICS=1",
		"-e", "DO_NOT_TRACK=1",
		"-e", "CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1",
		in.Image,
		"villa", "sandbox-bridge",
	}, nil
}
