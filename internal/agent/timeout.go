package agent

// timeout.go is the ONE home of the request_timeout formula (#351, ADR-0044). Crush
// aborted a request that sent nothing for options.request_timeout seconds, with a
// 60 s default. Crush's own prompt and tool list are about 17k tokens, which
// gemma-4-31b prefills at 219 to 221 tok/s: 71 to 77 s before the first token, so
// no round could start. Since v0.96 the value bounds the silence between streamed
// tokens, the first token waits max(request_timeout, 5 min), and a non-streaming
// call gets one deadline of request_timeout. The formula still holds under those
// rules and still stretches the first-token wait for windows over 60k. The timeout
// is a pure function of the context window the file renders, never of a live
// reading, so the golden-pinned Render stays
// deterministic and drift does not flap with GPU load.

const (
	// prefillFloorTokPerSec is slower than the slowest prefill villa has vetted
	// (gemma-4-31b, 219 tok/s measured), so a whole-window prefill at this rate
	// outlasts any vetted model's.
	prefillFloorTokPerSec = 200
	// minRequestTimeoutSeconds keeps a small window from rendering a bound below the
	// time Crush's own prompt needs to prefill at the floor rate.
	minRequestTimeoutSeconds = 120
	// maxRequestTimeoutSeconds caps the wait: Crush reads 0 as "no timeout", which
	// would hang it forever on a dead server, so a huge window does not get one.
	maxRequestTimeoutSeconds = 600
)

// RequestTimeoutSeconds is clamp(ceil(ctxWindow / 200), 120, 600): the idle-time
// bound Render writes to options.request_timeout for a rendered context window. A
// window of zero or less is the one Render defaults to, so the result is never zero
// (Crush: no timeout) or negative (Crush: invalid).
func RequestTimeoutSeconds(ctxWindow int) int {
	if ctxWindow <= 0 {
		ctxWindow = defaultContextWindow
	}
	secs := (ctxWindow + prefillFloorTokPerSec - 1) / prefillFloorTokPerSec
	return min(max(secs, minRequestTimeoutSeconds), maxRequestTimeoutSeconds)
}
