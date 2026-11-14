package inference

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// livePropsPayload is /props captured from a live ROCm 10.0 llama-server (keys abridged).
const livePropsPayload = `{"modalities":{"vision":true,"video":true,"audio":false},
"chat_template_caps":{"supports_object_arguments":true,"supports_parallel_tool_calls":true,
"supports_preserve_reasoning":true,"supports_reasoning_effort":false,"supports_string_content":true,
"supports_system_role":true,"supports_tool_calls":true,"supports_tools":true,"supports_typed_content":true},
"model_path":"/models/Qwen3.6-35B-A3B-Abliterated-Heretic-Q6_K.gguf",
"default_generation_settings":{"n_ctx":131072}}`

// TestPropsParsesCapabilities guards that /props capability fields land as typed
// optionals: an absent field is Unknown, never a confident false, and the existing
// model_path and n_ctx reads are unchanged by the new fields.
func TestPropsParsesCapabilities(t *testing.T) {
	yes, no := detect.Bool{Value: true, Known: true}, detect.Bool{Value: false, Known: true}
	unk := detect.Bool{}
	for _, tc := range []struct {
		name         string
		body         string
		status       int
		wantNil      bool
		wantPath     string
		wantNCtx     int
		wantVision   detect.Bool
		wantToolsSup detect.Bool
	}{
		{"live payload", livePropsPayload, 200, false, "/models/Qwen3.6-35B-A3B-Abliterated-Heretic-Q6_K.gguf", 131072, yes, yes},
		{"vision false, tools false",
			`{"modalities":{"vision":false},"chat_template_caps":{"supports_tools":false,"supports_tool_calls":false}}`,
			200, false, "", 0, no, no},
		{"tools true but tool_calls false is not supported",
			`{"chat_template_caps":{"supports_tools":true,"supports_tool_calls":false}}`, 200, false, "", 0, unk, no},
		{"one tools key present and true, other absent is Unknown",
			`{"chat_template_caps":{"supports_tools":true}}`, 200, false, "", 0, unk, unk},
		{"one tools key false, other absent is false",
			`{"chat_template_caps":{"supports_tool_calls":false}}`, 200, false, "", 0, unk, no},
		{"capability fields absent", `{"model_path":"/models/x.gguf","n_ctx":4096}`, 200, false, "/models/x.gguf", 4096, unk, unk},
		{"modalities present without vision", `{"modalities":{"audio":true}}`, 200, false, "", 0, unk, unk},
		{"wrongly typed vision is Unknown", `{"modalities":{"vision":"yes"},"model_path":"/m.gguf"}`, 200, false, "/m.gguf", 0, unk, unk},
		{"malformed body", `{"modalities":`, 200, true, "", 0, unk, unk},
		{"non-200", livePropsPayload, 503, true, "", 0, unk, unk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p := NewClient(srv.URL, "").Props(t.Context())
			if tc.wantNil {
				if p != nil {
					t.Fatalf("Props = %+v, want nil", p)
				}
				return
			}
			if p == nil {
				t.Fatal("Props = nil")
			}
			if p.ModelPath != tc.wantPath || p.NCtx != tc.wantNCtx {
				t.Errorf("path/ctx = %q/%d, want %q/%d", p.ModelPath, p.NCtx, tc.wantPath, tc.wantNCtx)
			}
			if p.Vision.Known != tc.wantVision.Known || p.Vision.Value != tc.wantVision.Value {
				t.Errorf("Vision = %+v, want %+v", p.Vision, tc.wantVision)
			}
			if p.ToolsSupported.Known != tc.wantToolsSup.Known || p.ToolsSupported.Value != tc.wantToolsSup.Value {
				t.Errorf("ToolsSupported = %+v, want %+v", p.ToolsSupported, tc.wantToolsSup)
			}
		})
	}
}
