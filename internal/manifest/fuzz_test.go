package manifest

import (
	"testing"
	"time"
)

// FuzzParse feeds arbitrary bytes to Parse, the entry point for a manifest
// fetched over the network (internal/updatefetch) before anything about it
// is trusted -- Parse deliberately does not verify the signature, allowlist
// or serial (that is manifestverify's job), so it must survive genuinely
// hostile bytes on its own. The invariant is "never panics, and a successful
// parse round-trips through Marshal", since a Document that cannot be
// re-rendered is not one any caller downstream can act on safely.
func FuzzParse(f *testing.F) {
	valid, err := Marshal(FromTable(42, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		f.Fatalf("Marshal(validDoc): %v", err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schema_version":99,"serial":1,"components":[]}`))
	f.Add([]byte(`{"schema_version":1,"serial":1,"components":[],"future_field":true}`))
	f.Add([]byte(`{"schema_version":1,"serial":1,"valid_until":"not-a-time","components":[]}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"schema_version":1,"components":`))

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := Parse(data)
		if err != nil {
			return
		}
		// A successful parse must be safe to re-render: Marshal must not panic
		// or fail on whatever Parse accepted.
		if _, mErr := Marshal(doc); mErr != nil {
			t.Fatalf("Marshal(Parse(data)) failed on an accepted document: %v", mErr)
		}
	})
}
