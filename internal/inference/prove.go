package inference

import (
	"context"
	"net/http"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// prove.go exports the two thin running-server probe wrappers that the Plan-02
// `liveProve` cutover composition needs. The existing EXPORTED running-server
// entry points (NewContainerRunner / Validate / RunningOffloadVerdict / BackendFor)
// could NOT serve a cutover prove against an ALREADY-running server: Validate spins
// a NEW `--rm` container at a transient name (validate.go), so a `villa backend set`
// cutover that wants to prove the server it just restarted cannot use it. These two
// wrappers fill that gap — they probe the already-running `villa-llama.service`
// over its loopback endpoint WITHOUT starting any container, delegating to the
// package-private pollHealth/chatProbe (probe.go) so the interval/client/probe
// behavior is not re-invented.
//
// Both are backend-neutral (no podman/image/device/backend-marker literal): exactly
// like the private probes they wrap, so the seam grep-gate stays green. Plan-02
// `liveProve` composes PollHealth (readiness) + GenerationProbe (real generation)
// with RunningOffloadVerdict (residency) into the cutover verdict the transactional
// backendswap core gates on.

// PollHealth polls the ALREADY-running server at <endpoint>/health until it returns
// 200, bounded by timeout, treating a 503 ("Loading model") as still-loading. It is
// the EXPORTED, NON-container readiness gate ONLY (Pitfall 5) — a 200 means
// "accepting requests", NOT "offload happened"; the residency verdict is
// RunningOffloadVerdict. It delegates to the package-private pollHealth with a fresh
// default http.Client and the package pollInterval so the interval is not
// re-invented. Plan-02 liveProve calls this as the first stage of the cutover prove.
func PollHealth(ctx context.Context, endpoint string, timeout time.Duration) detect.Bool {
	return pollHealth(ctx, &http.Client{}, endpoint, timeout, pollInterval)
}

// GenerationProbe sends a real, small chat completion to the ALREADY-running
// server's <endpoint>/v1/chat/completions through the REUSED internal/llm
// OpenAIClient (D-04 — reuse, never rebuild) and reports the streamed token result.
// It is the EXPORTED real generation probe; it starts NO container (unlike Validate)
// so a `villa backend set` cutover can prove generation against the server it just
// restarted. It delegates straight to the package-private chatProbe. Plan-02
// liveProve composes this with PollHealth + RunningOffloadVerdict.
func GenerationProbe(ctx context.Context, endpoint, modelID string) ChatResult {
	return chatProbe(ctx, endpoint, modelID)
}
