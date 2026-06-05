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
		"container image literal":    regexp.MustCompile(`kyuz0|docker\.io/|server-vulkan`),
		"container device args":      regexp.MustCompile(`--device\s+/dev/dri|--group-add|keep-groups`),
		"podman invocation":          regexp.MustCompile(`exec\.Command\(\s*"podman"|"podman".*\b(run|stop|logs)\b`),
	}

	internalRoot := ".." // internal/ (this test lives in internal/inference)

	// The seam: paths allowed to hold these imperative literals.
	isSeam := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		return strings.HasPrefix(rel, "inference/") || rel == "detect/gpu_amd.go"
	}

	err := filepath.Walk(internalRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(internalRoot, path)
		if relErr != nil {
			return relErr
		}
		if isSeam(rel) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		src := string(data)
		for label, re := range patterns {
			if re.MatchString(src) {
				t.Errorf("seam leak in %s: imperative backend pattern %q matched outside the seam (move it into internal/inference/ or internal/detect/gpu_amd.go)", rel, label)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}
