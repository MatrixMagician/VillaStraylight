package llm

import (
	"encoding/json"
	"testing"
)

// TestChatRequestUnsetFieldsMarshalUnchanged guards the grounding package's
// addition of Temperature and ChatTemplateKwargs to ChatRequest: every
// existing caller that leaves them unset must marshal byte-identical to the
// pre-addition wire shape.
func TestChatRequestUnsetFieldsMarshalUnchanged(t *testing.T) {
	req := ChatRequest{
		Model:    "qwen3.6-35b-a3b",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"model":"qwen3.6-35b-a3b","messages":[{"role":"user","content":"hello"}]}`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
