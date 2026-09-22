package orchestrate

// inferproxy.go holds the v1.11.1 villa-inferproxy managed-service constants and
// view builder (GHSA-gvp9, ADR-0011): the sandbox's ONLY route to inference, now
// that villa-llama no longer joins villa-sandbox.network (render.go).
//
// It reuses villa-websafe's EXACT image/bind-mount shape (Area 1: bind-mount the
// host villa binary read-only into gcr.io/distroless/static-debian12 and exec a
// hidden subcommand) rather than inventing a second one — the websafeImage
// literal and websafeBinaryPath mount point are shared verbatim, so this file
// adds no new image and no new seam-gate surface. It IS a different category
// from villa-websafe, though: villa-websafe joins villa.network alone,
// villa-inferproxy joins BOTH villa.network (to reach villa-llama) and
// villa-sandbox.network (Internal=true — the network the task VMs are on), which
// is the whole point of it existing.
//
// Like villa-websafe, it does NOT flow through parseContainerArgs (no GPU
// device/group/exec args), so it needs no seam-gate allowlist entry of its own —
// it never trips the "container image literal" regex differently than
// villa-websafe already does.

import (
	"strconv"
	"strings"
)

// InferproxyContainerUnitName returns the villa-inferproxy .container unit
// filename Render appends only when subsystem.SandboxOn is true.
func InferproxyContainerUnitName() string { return inferproxyContainerUnitName }

const inferproxyContainerUnitName = "villa-inferproxy.container"

// InferproxyContainerName returns the villa-inferproxy container-DNS name (the
// host the task-bridge's provider config must target instead of villa-llama).
func InferproxyContainerName() string { return inferproxyContainerName }

const inferproxyContainerName = "villa-inferproxy"

// inferproxyView is the data inferproxy.container.tmpl renders: BOTH networks,
// the shared inference secret (to inject on the forwarded leg to villa-llama),
// the read-only bind-mounted villa binary, and the fixed-token Exec.
type inferproxyView struct {
	ContainerName  string
	Image          string
	Network        string
	SandboxNetwork string
	SecretEnvFile  string
	BinaryMount    string
	Exec           string
}

// buildInferproxyView assembles the villa-inferproxy container view. image is the
// RESOLVED pin — villa-inferproxy shares villa-websafe's ComponentWebsafe pin
// identity (same base image, same bind-mount contract; a separate pin id would
// track the identical digest twice). hostVillaPath is the captured
// os.Executable() path (RenderInput.HostVillaPath), NEVER shell-interpolated.
func buildInferproxyView(image, hostVillaPath string, port int) inferproxyView {
	return inferproxyView{
		ContainerName:  inferproxyContainerName,
		Image:          image,
		Network:        networkAttach,
		SandboxNetwork: sandboxNetworkAttach,
		SecretEnvFile:  inferenceSecretEnvFilePath,
		BinaryMount:    hostVillaPath + ":" + websafeBinaryPath + ":ro,z",
		Exec:           buildInferproxyExec(port),
	}
}

// buildInferproxyExec assembles the inferproxy-serve Exec from FIXED tokens
// joined on a single space (NO shell interpolation, mirrors websafe.go's
// buildWebsafeExec). --host 0.0.0.0 is container-internal ONLY (no host bind);
// --port is the config-resolved in-network port.
func buildInferproxyExec(port int) string {
	tokens := []string{
		websafeBinaryPath,
		"inferproxy-serve",
		"--host", "0.0.0.0", // container-internal only; no host bind
		"--port", strconv.Itoa(port),
	}
	return strings.Join(tokens, " ")
}
