package metrics

import (
	"os"
	"testing"
)

// TestParseSlotsReadsBothNextTokenShapes guards #328: llama-server b11430 emits
// next_token as a one-element array, older builds as an object. Both must parse to the
// same reading, never to typed-Unknown.
func TestParseSlotsReadsBothNextTokenShapes(t *testing.T) {
	served, err := os.ReadFile("testdata/slots_b11430.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, ok := ParseSlots(served)
	if !ok || len(slots) != 4 {
		t.Fatalf("b11430 body: ok=%v len=%d, want ok with 4 slots", ok, len(slots))
	}
	if slots[0].NCtx == 0 || slots[0].NextToken.NRemain != -1 {
		t.Errorf("b11430 slot[0] = %+v, want n_ctx read and n_remain=-1", slots[0])
	}

	array := []byte(`[{"id":2,"n_ctx":4096,"is_processing":true,"next_token":[{"n_remain":-1,"n_decoded":77}]}]`)
	object := []byte(`[{"id":2,"n_ctx":4096,"is_processing":true,"next_token":{"n_remain":-1,"n_decoded":77}}]`)
	a, aok := ParseSlots(array)
	o, ook := ParseSlots(object)
	if !aok || !ook || a[0] != o[0] || a[0].NextToken.NDecoded != 77 {
		t.Errorf("array=%+v(%v) object=%+v(%v), want identical with n_decoded=77", a, aok, o, ook)
	}

	for name, body := range map[string]string{
		"empty array": `[{"id":0,"next_token":[]}]`,
		"absent":      `[{"id":0}]`,
		"null":        `[{"id":0,"next_token":null}]`,
	} {
		s, ok := ParseSlots([]byte(body))
		if !ok || len(s) != 1 || s[0].NextToken.NDecoded != 0 {
			t.Errorf("%s: ok=%v slots=%+v, want one slot with a zero reading", name, ok, s)
		}
	}
	if _, ok := ParseSlots([]byte(`[{"id":0,"next_token":"x"}]`)); ok {
		t.Errorf("a string next_token parsed ok, want typed-Unknown")
	}
}
