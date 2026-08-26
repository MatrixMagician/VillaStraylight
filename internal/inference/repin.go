package inference

// repin.go lets a caller run a backend on a pin OTHER than its compiled-in one,
// without any caller learning what either pin is.
//
// `villa update` records an EFFECTIVE pin per component — what this host is
// actually running — which may differ from the vetted pin compiled into
// backend_rocm.go / backend_vulkan.go. The rendered unit must carry the effective
// one, or an update would record a pin nothing ever uses.
//
// The seam discipline is what makes this a wrapper rather than a parameter.
// Backend.Image() and Backend.ContainerArgs() are the ONLY places an image literal
// is assembled, and TestSeamGrepGate exists to keep it that way. Threading a pin
// down into ContainerArgs would put an image string in RunSpec, which every caller
// constructs — precisely the leak the gate forbids. Wrapping keeps the substitution
// inside the seam: a caller hands in a Backend and a replacement ref and gets a
// Backend back, having never seen either image.

// Repinned returns b running on ref instead of its own image.
//
// An empty ref returns b UNCHANGED, and that is the load-bearing case rather than
// defensive habit: it is the fresh-install path, where no effective pin is recorded
// and the vetted one must be used. It is also the path every existing caller takes,
// which is why the rendered units stay byte-identical.
//
// Everything except the image is delegated, INCLUDING ResidencyProof. The markers
// belong to the backend family — a repinned ROCm image still emits ROCm0 — so
// substituting them along with the image would break the residency proof on exactly
// the runs an update most needs it to work.
func Repinned(b Backend, ref string) Backend {
	if b == nil || ref == "" || ref == b.Image() {
		return b
	}
	return repinned{inner: b, ref: ref}
}

// repinned is the wrapper. It holds the wrapped backend and the replacement ref,
// and nothing else: there is no state to get out of step.
type repinned struct {
	inner Backend
	ref   string
}

// Compile-time assertion that repinned satisfies Backend, so a method added to the
// interface fails the build here rather than silently falling through.
var _ Backend = repinned{}

// Name is the wrapped backend's identity, unchanged. The name answers "which
// backend family is this" — a question the pin does not change — and it selects the
// Description= label, the residency markers and the ROCm preflight gate. Renaming a
// repinned backend would route it around the gate its family requires.
func (r repinned) Name() string { return r.inner.Name() }

// Image is the replacement ref. This is the whole substitution.
func (r repinned) Image() string { return r.ref }

// ContainerArgs is the wrapped backend's argument slice with the image token
// swapped, so every device arg, env var, publish mapping and llama-server flag
// still comes from the seam file that owns them.
//
// The swap is by IDENTITY against the inner image rather than by position, which
// matters because the argument slice is assembled differently per backend (ROCm
// inserts device and env args before the image) and a positional swap would silently
// replace a device flag on a backend whose layout changed.
func (r repinned) ContainerArgs(spec RunSpec) []string {
	args := r.inner.ContainerArgs(spec)
	inner := r.inner.Image()
	out := make([]string, len(args))
	for i, a := range args {
		if a == inner {
			out[i] = r.ref
			continue
		}
		out[i] = a
	}
	return out
}

// ResidencyProof is the wrapped backend's markers, unchanged — see the type doc.
func (r repinned) ResidencyProof() ResidencyMarkers { return r.inner.ResidencyProof() }
