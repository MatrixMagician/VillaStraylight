package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/toolproof"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

func toolsCfg() config.VillaConfig {
	return config.VillaConfig{
		ToolServer: true, WebSearchEnabled: true, MemoryEnabled: true,
		ToolWebSecret: "web-secret-value", ToolRecallSecret: "recall-secret-value", ChatPort: 3000,
	}
}

// TestRunVerifyTools: each verdict reaches the operator on the stream its severity belongs to
// with the family's exit code, and is recorded for doctor. An off gate is a clean exit that
// records nothing, an unreadable config refuses, and a record that cannot be written never
// changes the verdict.
func TestRunVerifyTools(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         config.VillaConfig
		cfgErr      error
		proof       verify.Proof
		persistErr  error
		wantCode    int
		wantOut     string
		wantErr     string
		wantProved  bool
		wantVerdict string
	}{
		{name: "gate off", cfg: config.VillaConfig{WebSearchEnabled: true}, wantCode: exitPass,
			wantOut: "verify tools: the tool server is not enabled (tool_server=false), nothing to verify. Enable it with `villa install --tool-server`, then re-run.\n"},
		{name: "pass", cfg: toolsCfg(), proof: verify.Proof{Status: verify.Pass, Detail: "7 of 7 legs passed"},
			wantCode: exitPass, wantOut: "verify tools: 7 of 7 legs passed\n", wantProved: true, wantVerdict: "PASS"},
		{name: "fail", cfg: toolsCfg(), proof: verify.Proof{Status: verify.Fail, Detail: "FAILED legs: contract"},
			wantCode: exitBlocked, wantErr: "verify tools: tool server proof FAILED: FAILED legs: contract\n", wantProved: true, wantVerdict: "FAIL"},
		{name: "reject", cfg: toolsCfg(), proof: verify.Proof{Status: verify.Reject, Detail: "legs that could not be conducted: web"},
			wantCode: exitWarn, wantErr: "verify tools: tool server proof could not be conducted (REJECT): legs that could not be conducted: web\n", wantProved: true, wantVerdict: "REJECT"},
		{name: "record fails, verdict stands", cfg: toolsCfg(), proof: verify.Proof{Status: verify.Pass, Detail: "ok"}, persistErr: errors.New("disk full"),
			wantCode: exitPass, wantOut: "verify tools: ok\n", wantErr: "warning: could not record the result", wantProved: true, wantVerdict: "PASS"},
		{name: "unreadable config", cfgErr: errors.New("toml: bad"), wantCode: exitBlocked,
			wantErr: "verify tools: cannot read config.toml (toml: bad), fix or remove it, then re-run.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proved, verdict := false, ""
			deps := verifyToolsDeps{
				loadedConfig: func() (config.VillaConfig, error) { return tc.cfg, tc.cfgErr },
				proveFn: func(context.Context, config.VillaConfig) verify.Proof {
					proved = true
					return tc.proof
				},
				persistFn: func(s verifystate.State) error {
					verdict = s.Verdict
					if _, err := time.Parse(time.RFC3339, s.CheckedAt); err != nil {
						t.Errorf("checked_at %q is not RFC3339", s.CheckedAt)
					}
					return tc.persistErr
				},
			}
			cmd := &cobra.Command{}
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			if got := runVerifyTools(cmd, nil, deps); got != tc.wantCode {
				t.Errorf("exit = %d, want %d", got, tc.wantCode)
			}
			if tc.wantOut != "" && out.String() != tc.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tc.wantOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr = %q, want it to hold %q", errOut.String(), tc.wantErr)
			}
			if proved != tc.wantProved {
				t.Errorf("proved = %v, want %v", proved, tc.wantProved)
			}
			if verdict != tc.wantVerdict {
				t.Errorf("recorded verdict = %q, want %q", verdict, tc.wantVerdict)
			}
		})
	}
}

// TestVerifyToolsIsRegisteredUnderVerify: the verb is a sibling of the others in the family.
func TestVerifyToolsIsRegisteredUnderVerify(t *testing.T) {
	var found bool
	for _, c := range newVerify().Commands() {
		if c.Name() == "tools" {
			found = true
		}
	}
	if !found {
		t.Error("`villa verify tools` is not registered")
	}
}

// TestToolCurlKeepsTheBearerOffArgv: the bearer reaches curl as a header file on stdin, never
// on the command line where any local user could read it from /proc (#252). A request with no
// headers pipes nothing. The unit is addressed on the network it joins.
func TestToolCurlKeepsTheBearerOffArgv(t *testing.T) {
	cfg := toolsCfg()
	cases := []struct {
		name        string
		req         toolproof.Request
		wantNetwork string
		wantURL     string
		wantStdin   []string
		wantMethod  string
	}{
		{"web right", toolproof.Request{Set: toolserver.SetWeb, Path: "/web/search?q=x", Auth: toolproof.AuthRight}, routedNetwork,
			"http://villa-tools-web:8092/web/search?q=x", []string{"Authorization: Bearer web-secret-value"}, ""},
		{"recall right with identity", toolproof.Request{Set: toolserver.SetRecall, Path: "/recall/search?q=x", Auth: toolproof.AuthRight, Identity: "u1"}, closedNetwork,
			"http://villa-tools-recall:8093/recall/search?q=x", []string{"Authorization: Bearer recall-secret-value", "X-Villa-User-Id: u1"}, ""},
		{"no bearer", toolproof.Request{Set: toolserver.SetWeb, Path: "/openapi.json", Auth: toolproof.AuthNone}, routedNetwork,
			"http://villa-tools-web:8092/openapi.json", nil, ""},
		{"no bearer, identity only", toolproof.Request{Set: toolserver.SetRecall, Path: "/recall/search?q=x", Auth: toolproof.AuthNone, Identity: "u1"}, closedNetwork,
			"http://villa-tools-recall:8093/recall/search?q=x", []string{"X-Villa-User-Id: u1"}, ""},
		{"POST", toolproof.Request{Set: toolserver.SetWeb, Method: "POST", Path: "/web/search?q=x", Auth: toolproof.AuthRight}, routedNetwork,
			"http://villa-tools-web:8092/web/search?q=x", []string{"Authorization: Bearer web-secret-value"}, "POST"},
	}
	for _, c := range cases {
		args, stdin, network, err := toolCurl(cfg, c.req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if network != c.wantNetwork {
			t.Errorf("%s: network = %s, want %s", c.name, network, c.wantNetwork)
		}
		joined := strings.Join(args, " ")
		for _, secret := range []string{"web-secret-value", "recall-secret-value"} {
			if strings.Contains(joined, secret) {
				t.Errorf("%s: a bearer is on curl's command line: %v", c.name, args)
			}
		}
		if !slices.Contains(args, c.wantURL) || !slices.Contains(args, "-w") {
			t.Errorf("%s: args = %v", c.name, args)
		}
		if c.wantStdin == nil {
			if stdin != nil {
				t.Errorf("%s: stdin = %q, want none", c.name, stdin)
			}
		} else {
			if !slices.Contains(args, "@-") {
				t.Errorf("%s: headers are not read from stdin: %v", c.name, args)
			}
			for _, h := range c.wantStdin {
				if !strings.Contains(string(stdin), h+"\n") {
					t.Errorf("%s: stdin %q lacks %q", c.name, stdin, h)
				}
			}
		}
		if c.wantMethod != "" && !slices.Contains(args, c.wantMethod) {
			t.Errorf("%s: method %s missing from %v", c.name, c.wantMethod, args)
		}
	}

	wrong, stdin, _, _ := toolCurl(cfg, toolproof.Request{Set: toolserver.SetWeb, Path: "/x", Auth: toolproof.AuthWrong})
	if bytes.Contains(stdin, []byte("web-secret-value")) || !bytes.Contains(stdin, []byte("Authorization: Bearer ")) || slices.Contains(wrong, "web-secret-value") {
		t.Errorf("the wrong bearer must differ from the real one: %q", stdin)
	}
	if _, _, _, err := toolCurl(cfg, toolproof.Request{Set: "nope", Path: "/x"}); err == nil {
		t.Error("an unknown set has no unit to address")
	}
	if _, _, _, err := toolCurl(config.VillaConfig{ToolServer: true}, toolproof.Request{Set: toolserver.SetWeb, Path: "/x", Auth: toolproof.AuthRight}); err == nil {
		t.Error("a missing bearer in config.toml must not be sent as an empty one")
	}
}

// TestParseToolReply: the status is curl's -w trailer on the last line, and the body is every
// byte before the separating newline, so the contract leg compares exactly what the unit sent,
// trailing newline included.
func TestParseToolReply(t *testing.T) {
	r, err := parseToolReply([]byte("{\"a\":1}\n\n200"))
	if err != nil || r.Status != 200 || string(r.Body) != "{\"a\":1}\n" {
		t.Errorf("reply = %+v, %v", r, err)
	}
	r, err = parseToolReply([]byte("\n401"))
	if err != nil || r.Status != 401 || len(r.Body) != 0 {
		t.Errorf("empty body = %+v, %v", r, err)
	}
	for _, bad := range []string{"", "200", "body\nabc", "body\n0", "body\n99999"} {
		if _, err := parseToolReply([]byte(bad)); err == nil {
			t.Errorf("parseToolReply(%q) succeeded", bad)
		}
	}
}

// TestToolReachClassification: an answer is reached, curl's connection, resolution and timeout
// exits are a refusal to connect, and anything else (a helper that never started) could not be
// conducted. The curl exit set is the family's one.
func TestToolReachClassification(t *testing.T) {
	cases := []struct {
		code        int
		err         error
		wantReached bool
		wantErr     bool
	}{
		{0, nil, true, false},
		{7, errors.New("exit 7"), false, false},
		{6, errors.New("exit 6"), false, false},
		{28, errors.New("exit 28"), false, false},
		{-1, errors.New("no such image"), false, true},
		{127, errors.New("curl absent"), false, true},
	}
	for _, c := range cases {
		reached, err := classifyToolReach(c.code, c.err)
		if reached != c.wantReached || (err != nil) != c.wantErr {
			t.Errorf("code %d: reached=%v err=%v", c.code, reached, err)
		}
	}
}

// TestToolProofInput: the proof compares the host with the rendered units, so the input is
// built from the config's gates and the rendered topology and nothing else.
func TestToolProofInput(t *testing.T) {
	joins := map[string][]string{"villa-tools-web": {"villa"}, "villa-tools-recall": {"villa-closed"}}
	in := toolProofInput(toolsCfg(), joins)
	if !slices.Equal(in.Sets, []toolserver.Set{toolserver.SetWeb, toolserver.SetRecall}) {
		t.Errorf("sets = %v", in.Sets)
	}
	if in.Container[toolserver.SetWeb] != "villa-tools-web" || in.Container[toolserver.SetRecall] != "villa-tools-recall" ||
		in.Routed != routedNetwork || in.Closed != closedNetwork ||
		in.QdrantURL != "http://villa-qdrant:6333/healthz" ||
		in.HealthURL[toolserver.SetWeb] != "http://villa-tools-web:8092/healthz" ||
		in.HealthURL[toolserver.SetRecall] != "http://villa-tools-recall:8093/healthz" ||
		!slices.Equal(in.Placement["villa-tools-recall"], []string{"villa-closed"}) {
		t.Errorf("input = %+v", in)
	}
	web := toolsCfg()
	web.MemoryEnabled = false
	if in := toolProofInput(web, joins); !slices.Equal(in.Sets, []toolserver.Set{toolserver.SetWeb}) {
		t.Errorf("memory off must leave only the web unit: %v", in.Sets)
	}
	none := toolsCfg()
	none.MemoryEnabled, none.WebSearchEnabled = false, false
	if in := toolProofInput(none, joins); len(in.Sets) != 0 {
		t.Errorf("neither unit renders: %v", in.Sets)
	}
}

// fakeToolOWUI answers the Open WebUI routes the two drivers use through the real protocol
// client, so the request each driver builds is read at the transport.
func fakeToolOWUI(t *testing.T, completion string, seen *[]openwebui.Request) openwebui.Transport {
	t.Helper()
	return func(_ context.Context, req openwebui.Request) ([]byte, error) {
		if seen != nil {
			*seen = append(*seen, req)
		}
		switch {
		case strings.HasPrefix(req.Path, "/api/v1/auths/"):
			return []byte(`{"token":"jwt"}`), nil
		case req.Path == "/api/models":
			return []byte(`{"data":[{"id":"model.gguf"}]}`), nil
		case req.Path == "/api/chat/completions":
			return []byte(completion), nil
		case req.Path == "/api/v1/configs/tool_servers":
			return []byte(`{"TOOL_SERVER_CONNECTIONS":[{"config":{"enable":true},"info":{"id":"villa-web"},"key":"SECRET"}]}`), nil
		}
		return nil, errors.New("unrouted " + req.Path)
	}
}

// TestDriveToolChat: the completion offers the unit's own connection and the model's text is
// returned. An error from Open WebUI is returned, not turned into text.
func TestDriveToolChat(t *testing.T) {
	var seen []openwebui.Request
	owui := openwebui.New(fakeToolOWUI(t, `{"choices":[{"message":{"content":"the title is X"}}]}`, &seen))
	got, err := driveToolChat(t.Context(), owui, toolproof.ChatRequest{Set: toolserver.SetWeb, ServerID: "villa-web", Prompt: "search"})
	if err != nil || got != "the title is X" {
		t.Fatalf("answer = %q, %v", got, err)
	}
	var body struct {
		Model   string   `json:"model"`
		ToolIDs []string `json:"tool_ids"`
	}
	for _, r := range seen {
		if r.Path == "/api/chat/completions" {
			_ = json.Unmarshal(r.Body, &body)
			if r.Token != "jwt" {
				t.Errorf("token = %q", r.Token)
			}
		}
	}
	if body.Model != "model.gguf" || !slices.Equal(body.ToolIDs, []string{"server:villa-web"}) {
		t.Errorf("completion body = %+v", body)
	}

	failing := openwebui.New(func(context.Context, openwebui.Request) ([]byte, error) { return nil, errors.New("connection refused") })
	if _, err := driveToolChat(t.Context(), failing, toolproof.ChatRequest{ServerID: "villa-web"}); err == nil {
		t.Error("an unreachable Open WebUI must be an error")
	}
}

// TestDoctorReadsWhatVerifyToolsRecords: the record the verb writes is the record doctor's
// tool-server-proof finding reads, through the live wiring. A fresh PASS passes, a fresh FAIL
// fails, a stale PASS and an absent record warn, and the search record is a different file.
func TestDoctorReadsWhatVerifyToolsRecords(t *testing.T) {
	const cfgTOML = doctorHostBaseTOML + "web_search_enabled = true\ntool_server = true\n"
	okReport := func() status.Report { return status.Report{LoopbackOnly: true} }
	record := func(t *testing.T, verdict string, at time.Time) {
		t.Helper()
		if err := liveVerifyToolsStatePersist(verifystate.State{Verdict: verdict, CheckedAt: at.UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		record func(t *testing.T)
		want   string
	}{
		{"fresh pass", func(t *testing.T) { record(t, "PASS", time.Now()) }, "PASS"},
		{"fresh fail", func(t *testing.T) { record(t, "FAIL", time.Now()) }, "FAIL"},
		{"stale pass", func(t *testing.T) { record(t, "PASS", time.Now().Add(-72*time.Hour)) }, "WARN"},
		{"nothing recorded", func(*testing.T) {}, "WARN"},
		{"only the search proof passed", func(t *testing.T) { recordVerifySearch(t, "PASS", time.Now()) }, "WARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newDoctorHost(t, cfgTOML)
			tc.record(t)
			f, ok := doctorFindingsFor(t, okReport)["tool-server-proof"]
			if !ok {
				t.Fatal("no tool-server-proof finding")
			}
			if f.Status != tc.want {
				t.Errorf("tool-server-proof = %s %q, want %s", f.Status, f.Detail, tc.want)
			}
		})
	}
}

// TestToolRegistrations maps the protocol's list onto the proof's and carries no key.
func TestToolRegistrations(t *testing.T) {
	owui := openwebui.New(fakeToolOWUI(t, "", nil))
	got, err := toolRegistrations(t.Context(), owui)
	if err != nil || len(got) != 1 || got[0] != (toolproof.Registration{ID: "villa-web", Enabled: true}) {
		t.Errorf("registrations = %+v, %v", got, err)
	}
}
