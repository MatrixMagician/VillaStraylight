package inference

import (
	"strconv"
	"testing"
)

// TestPromptCacheFlagRenderedOnEveryBackend guards ADR-0021: every chat
// llama-server run carries `--cache-ram 8192` explicitly, on both backends, so an
// image's own default can no longer change what the prompt cache costs. It also
// asserts the flag is never -1 (unbounded) and that the byte term the fit math
// consumes is the same constant, so the rendered cap and the counted cost cannot
// drift apart.
func TestPromptCacheFlagRenderedOnEveryBackend(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			args := b.ContainerArgs(baseSpec())
			i := indexOf(args, "--cache-ram")
			if i == -1 || i+1 >= len(args) {
				t.Fatalf("[%s] --cache-ram not rendered: %v", name, args)
			}
			if args[i+1] != "8192" {
				t.Errorf("[%s] --cache-ram value = %q, want 8192", name, args[i+1])
			}
			if got := countTok(args, "--cache-ram"); got != 1 {
				t.Errorf("[%s] expected exactly one --cache-ram, got %d", name, got)
			}
			mib, err := strconv.Atoi(args[i+1])
			if err != nil || uint64(mib)<<20 != PromptCacheBytes {
				t.Errorf("[%s] rendered cap %q MiB != PromptCacheBytes %d", name, args[i+1], PromptCacheBytes)
			}
		})
	}
	if PromptCacheBytes != 8589934592 {
		t.Errorf("PromptCacheBytes = %d, want 8589934592 (8192 MiB)", PromptCacheBytes)
	}
}
