package crushapi

import (
	"encoding/json"
	"testing"
	"time"
)

// FuzzDecodeLine feeds arbitrary bytes to the bridge/runner stdio decode --
// crushapi.Line is the one-object-per-line wire format the task VM's bridge
// process emits over stdout and the host-side runner reads (spec 3.4: stdio
// is the ONLY transport across the krun boundary), so this is untrusted input
// from inside a sandbox a task's own model turn could otherwise compromise.
// The invariant is simply "never panics": a line that fails to decode is
// already handled by its caller as an ordinary error.
func FuzzDecodeLine(f *testing.F) {
	f.Add([]byte(`{"command":{"kind":"prompt","prompt":"find duplicate files"}}`))
	f.Add([]byte(`{"command":{"kind":"grant","permission_id":"perm-1","answer":"allow"}}`))
	f.Add([]byte(`{"command":{"kind":"cancel"}}`))
	f.Add([]byte(`{"event":{"kind":"bridge_ready","time":"2026-09-10T12:00:00Z","version":"v0.76.0"}}`))
	f.Add([]byte(`{"event":{"kind":"bridge_error","time":"2026-09-10T12:00:00Z","error":"crush version drift"}}`))
	f.Add([]byte(`{"event":{"kind":"permission_request","id":"perm-1","time":"2026-09-10T12:00:00Z","permission":{"id":"perm-1","tool":"edit","action":"write","path":"/workspace/a.md"}}}`))
	f.Add([]byte(`{"event":{"kind":"a_kind_villa_has_never_seen","time":"2026-09-10T12:00:00Z","raw":{"x":1}}}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var l Line
		_ = json.Unmarshal(data, &l)
	})
}

// FuzzDecodeEvent feeds arbitrary bytes to decodeEvent, which parses each
// `data: <json>` frame of Crush's SSE stream (testdata/events.sse is the
// recorded fixture these seeds are drawn from). decodeEvent must never panic
// regardless of how the envelope, the inner pubsub wrapper, or the per-kind
// payload are malformed, since an unparseable frame is meant to fall back to
// an ordinary decode error the stream reader already tolerates.
func FuzzDecodeEvent(f *testing.F) {
	seeds := []string{
		`{"type":"permission_request","payload":{"type":"created","payload":{"id":"perm-1","session_id":"sess-1","tool_call_id":"call-1","tool_name":"edit","description":"edit reports/duplicates.md","action":"write","path":"/workspace/reports/duplicates.md","params":{"file_path":"/workspace/reports/duplicates.md"}}}}`,
		`{"type":"permission_notification","payload":{"type":"created","payload":{"tool_call_id":"call-1","granted":true,"denied":false}}}`,
		`{"type":"message","payload":{"type":"updated","payload":{"id":"msg-1","session_id":"sess-1","role":"assistant","parts":[{"type":"text","data":{"text":"Listing the folder."}},{"type":"tool_call","data":{"id":"call-1","name":"edit","input":"{}"}}]}}}`,
		`{"type":"file","payload":{"type":"created","payload":{"id":"file-1","session_id":"sess-1","path":"/workspace/reports/duplicates.md","version":3}}}`,
		`{"type":"agent_event","payload":{"type":"created","payload":{"type":"response","session_id":"sess-1"}}}`,
		`{"type":"run_complete","payload":{"type":"created","payload":{"session_id":"sess-1","run_id":"run-1","message_id":"msg-1","text":"done","cancelled":false}}}`,
		`{"type":"session","payload":{"type":"updated","payload":{"id":"sess-1","title":"villa workspace task"}}}`,
		`not json`,
		``,
		`{"type":"message","payload":`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeEvent(data, time.Now())
	})
}
