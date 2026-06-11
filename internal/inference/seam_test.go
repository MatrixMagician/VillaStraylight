package inference

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSeamGrepGate enforces Phase-2 Success Criterion 4 (D-03/SC#4, INF-03): no
// IMPERATIVE/RUNTIME backend assumption leaks outside the seam. The seam is
// internal/inference/ + internal/detect/gpu_amd.go; everywhere else in internal/
// must be backend-neutral so a future ROCm/Metal backend (and macOS) drops in
// without touching callers.
//
// SCOPING (deliberate, verified against the live tree). The naive token list
// `/dev/dri|vulkan|podman|GOOS` over-matches legitimate Phase-1 PROVENANCE and
// REMEDIATION *strings* that merely NAME these tools as findings — e.g.
// checks_gpu.go remediation text naming vulkaninfo/`/dev/dri`, checks_podman.go
// naming `podman`, recommend.go naming the "vulkan" backend default, and
// detect.KnownInt(2, "/dev/dri") provenance labels. Those are DATA, not backend
// assumptions, and pre-date this phase. Flagging them would WEAKEN the gate into
// noise a reviewer learns to ignore. Instead this gate matches only the four
// imperative leaks that actually break backend-neutrality:
//
//	(a) runtime.GOOS / GOOS== platform branching,
//	(b) the container IMAGE literal (kyuz0 / docker.io/ / server-vulkan),
//	(c) container DEVICE args (--device /dev/dri, --group-add, keep-groups),
//	(d) `podman` PROCESS invocations (exec.Command("podman", …) / "podman" run|stop|logs).
//
// SC#4 intent (no silent CPU/Linux assumption in callers) is preserved, not
// relaxed: every one of these is an imperative behavior, not a printed finding.
func TestSeamGrepGate(t *testing.T) {
	// Imperative backend-leak patterns. Each must appear ZERO times in non-test
	// .go files outside the seam.
	patterns := map[string]*regexp.Regexp{
		"runtime.GOOS / GOOS branch": regexp.MustCompile(`runtime\.GOOS|GOOS\s*==`),
		// kyuz0|docker.io/ already bind BOTH the Vulkan and the ROCm image tokens (the
		// rocm image is docker.io/kyuz0/…:rocm-7.2.4@sha256:…). The rocm tag alternatives
		// are added for EXPLICIT intent — a ROCm image tag leaking outside the seam must
		// fail CI.
		//
		// Image-CONTEXT anchoring (12-02): the ROCm tag alternatives are anchored to a
		// `:` tag-separator prefix and/or an `@sha256` digest suffix so they match a real
		// container IMAGE literal (`…:rocm-6.4.4@sha256:…`) but NOT a bare backend NAME
		// config-VALUE (e.g. `case "rocm-6.4.4":` in render.go / a `--backend` help line).
		// A config-value name is the same seam-clean class as bench.go's `vulkan`/`rocm`
		// consts — it carries no image/device imperative. `rocm-6\.4\.4` covers BOTH the
		// plain tag and the rocm-6.4.4-rocwmma suffix superset. The new image digest
		// literals still land ONLY in the seam (backend_rocm.go), and the regex extended
		// in the SAME commit (D-10/SC#4/T-12-02); the kyuz0|docker.io/ alternatives remain
		// an un-anchored backstop that catches any image string regardless of tag.
		"container image literal": regexp.MustCompile(`kyuz0|docker\.io/|server-vulkan|:rocm-7\.2\.4|rocm-7\.2\.4@|:rocm-6\.4\.4|rocm-6\.4\.4@|rocm7-nightlies`),
		"container device args":   regexp.MustCompile(`--device\s+/dev/dri|--group-add|keep-groups`),
		"podman invocation":       regexp.MustCompile(`exec\.Command\(\s*"podman"|"podman".*\b(run|stop|logs)\b`),
	}

	// matchFile reports every pattern in pats that leaks in the file at path, calling
	// report(rel, label) per leak. Factored so the internal/ and cmd/villa walks share
	// identical match logic and _test.go/dir skips (the only difference is the seam
	// allowlist and which subset of patterns applies — see each walk below).
	matchFile := func(root, path string, pats map[string]*regexp.Regexp, report func(rel, label string)) error {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		src := string(data)
		for label, re := range pats {
			if re.MatchString(src) {
				report(filepath.ToSlash(rel), label)
			}
		}
		return nil
	}

	isGoSource := func(path string, info os.FileInfo) bool {
		if info.IsDir() {
			return false
		}
		return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
	}

	// --- Walk 1: internal/ (unchanged) -------------------------------------------
	internalRoot := ".." // internal/ (this test lives in internal/inference)

	// The seam: paths allowed to hold these imperative literals.
	isSeam := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		// orchestrate/memory.go (Phase-19 D-10): the villa-qdrant + villa-embed
		// MANAGED-SERVICE image literals (docker.io/qdrant/…, docker.io/kyuz0/… == the
		// embed image) live here, the SAME category as openWebUIImage — NOT a
		// GPU-backend token. The two docker.io/ literals would trip the "container
		// image literal" regex without this allowlist; it is extended in the SAME
		// commit as the consts, mirroring the 12-02 ROCm-tag same-commit precedent.
		return strings.HasPrefix(rel, "inference/") ||
			rel == "detect/gpu_amd.go" ||
			rel == "orchestrate/memory.go"
	}

	err := filepath.Walk(internalRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !isGoSource(path, info) {
			return nil
		}
		rel, relErr := filepath.Rel(internalRoot, path)
		if relErr != nil {
			return relErr
		}
		if isSeam(filepath.ToSlash(rel)) {
			return nil
		}
		return matchFile(internalRoot, path, patterns, func(rel, label string) {
			t.Errorf("seam leak in %s: imperative backend pattern %q matched outside the seam (move it into internal/inference/ or internal/detect/gpu_amd.go)", rel, label)
		})
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}

	// --- Walk 2: cmd/villa (the cmd-tier backend-marker guard) -------------------
	// The cmd/villa tier is the OS-orchestration layer that LEGITIMATELY invokes
	// podman (lifecycle up/down/logs, uninstall volume rm — fixed-arg, never a shell).
	// So the "podman invocation" pattern does NOT apply here. What MUST stay out of
	// cmd/villa is any INFERENCE-BACKEND assumption — a platform branch, a container
	// IMAGE/DEVICE literal, or a raw backend MARKER token (ROCm0/HSA-override/fault) —
	// so the Plan-02 `cmd/villa/backend.go` noun (and every sibling) composes the
	// backendswap core + the EXPORTED inference prove primitives WITHOUT retyping a
	// backend literal. This makes the cmd-tier literal-free property a COMMITTED
	// regression test, not merely the one-shot 08-02 acceptance grep.
	cmdPatterns := map[string]*regexp.Regexp{
		"runtime.GOOS / GOOS branch": patterns["runtime.GOOS / GOOS branch"],
		"container image literal":    patterns["container image literal"],
		"container device args":      patterns["container device args"],
		// Raw backend MARKER literals: the ROCm residency token, the HSA override env,
		// and the memory-access-fault marker MUST live behind the inference seam
		// (backend_rocm.go), never in a cmd/villa caller.
		"backend marker literal": regexp.MustCompile(`ROCm0|HSA_OVERRIDE_GFX_VERSION|Memory access fault`),
	}

	cmdRoot := "../../cmd/villa" // relative to internal/inference
	err = filepath.Walk(cmdRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !isGoSource(path, info) {
			return nil
		}
		// cmd/villa has no seam files — every non-test .go is gated.
		return matchFile(cmdRoot, path, cmdPatterns, func(rel, label string) {
			t.Errorf("cmd-tier seam leak in cmd/villa/%s: backend pattern %q matched (keep inference-backend literals behind internal/inference/ or internal/detect/gpu_amd.go)", rel, label)
		})
	})
	if err != nil {
		t.Fatalf("walk cmd/villa: %v", err)
	}
}

// TestROCmMarkerPresence is the POSITIVE grep-gate (D-10): the ROCm backend's
// privilege/residency literals MUST live in backend_rocm.go (the seam), so a refactor
// that drops or relocates them fails CI. It gates on "ROCm0" (NOT "ggml_cuda" — that
// init prefix is SHARED with the CUDA path and would not distinguish a dropped ROCm
// descriptor), plus the HSA override env and the /dev/kfd device — the two imperative
// ROCm-only tokens. This is the dual of TestSeamGrepGate: the negative gate keeps these
// literals OUT of callers; this one keeps them IN the seam.
func TestROCmMarkerPresence(t *testing.T) {
	data, err := os.ReadFile("backend_rocm.go")
	if err != nil {
		t.Fatalf("read backend_rocm.go: %v", err)
	}
	src := string(data)
	for _, want := range []string{"ROCm0", "HSA_OVERRIDE_GFX_VERSION", "/dev/kfd"} {
		if !strings.Contains(src, want) {
			t.Errorf("backend_rocm.go is missing required ROCm marker %q (the seam must hold it — D-10)", want)
		}
	}
}
