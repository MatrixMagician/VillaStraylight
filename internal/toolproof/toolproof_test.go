package toolproof

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// fake is a healthy host: two tool units that behave as ADR-0042 says, a registration that
// lists both, networks as the units render them, and a model that calls the tool it is
// offered. A test breaks one thing through a hook and reads the verdict of the one leg that
// owns it. Nothing here opens a socket.
type fake struct {
	state recall.State

	// Hooks. Each runs after the healthy answer is built and may replace it.
	unit      func(Request, Response) (Response, error)
	registers func([]Registration) ([]Registration, error)
	networks  func(map[string][]string) (map[string][]string, error)
	internal  func(map[string]bool) (map[string]bool, error)
	reach     func(network, url string, reached bool) (bool, error)
	chatCalls func(set toolserver.Set) []string // log lines the model's call adds to the unit
	chatErr   error
	logErr    error
	stateErr  error

	log   map[toolserver.Set][]string
	asked []Request
}

const (
	routed = "villa"
	closed = "villa-closed"
)

func newFake() *fake {
	return &fake{
		state: recall.State{KnowledgeID: "kb", Chats: map[string]recall.ChatState{
			"chat-a": {UserID: "u1", FileID: "f-a"},
			"chat-b": {UserID: "u1", FileID: "f-b"},
		}},
		log: map[toolserver.Set][]string{},
	}
}

func fenced(label, text string) string {
	return "preamble\n[" + label + " nonce=0123456789abcdef]\n" + text + "\n[/" + label + " nonce=0123456789abcdef]"
}

func input(sets ...toolserver.Set) Input {
	if len(sets) == 0 {
		sets = []toolserver.Set{toolserver.SetWeb, toolserver.SetRecall}
	}
	in := Input{
		Sets:      sets,
		Container: map[toolserver.Set]string{toolserver.SetWeb: "villa-tools-web", toolserver.SetRecall: "villa-tools-recall"},
		Placement: map[string][]string{"villa-tools-web": {routed}, "villa-tools-recall": {closed}},
		Routed:    routed, Closed: closed,
		QdrantURL: "http://villa-qdrant:6333/healthz",
		HealthURL: map[toolserver.Set]string{
			toolserver.SetWeb:    "http://villa-tools-web:8092/healthz",
			toolserver.SetRecall: "http://villa-tools-recall:8093/healthz",
		},
	}
	return in
}

func (f *fake) owners() map[string][]string {
	by := map[string][]string{}
	for chat, c := range f.state.Chats {
		by[c.UserID] = append(by[c.UserID], chat)
	}
	for _, v := range by {
		slices.Sort(v)
	}
	return by
}

func (f *fake) healthyUnit(r Request) Response {
	if r.Path == "/openapi.json" {
		if r.Auth != AuthRight {
			return Response{Status: 401, Body: []byte(`{"error":"unauthorized"}`)}
		}
		spec, _ := toolserver.OpenAPI(r.Set)
		return Response{Status: 200, Body: spec}
	}
	if r.Method != "" && r.Method != "GET" {
		return Response{Status: 405, Body: []byte(`{"error":"method not allowed"}`)}
	}
	if r.Auth != AuthRight {
		return Response{Status: 401, Body: []byte(`{"error":"unauthorized"}`)}
	}
	if r.Set == toolserver.SetWeb {
		body, _ := json.Marshal(map[string]any{"results": []any{map[string]any{
			"title": "t", "url": "https://example.org", "fetched": true, "truncated": false,
			"content": fenced("UNTRUSTED_WEB_CONTENT", "page text"), "guard": map[string]any{"detected": false},
		}}})
		return Response{Status: 200, Body: body}
	}
	if r.Identity == "" || strings.ContainsAny(r.Identity, " !") {
		return Response{Status: 403, Body: []byte(`{"error":"forbidden"}`)}
	}
	results := []any{}
	for _, chat := range f.owners()[r.Identity] {
		results = append(results, map[string]any{
			"chat_id": chat, "score": 0.5, "truncated": false,
			"content": fenced("RECALL_CONTENT", "User: hi"), "guard": map[string]any{"detected": true, "rules": []string{"delimiter-turn-spoofing"}},
		})
	}
	body, _ := json.Marshal(map[string]any{"results": results})
	return Response{Status: 200, Body: body}
}

func (f *fake) deps() Deps {
	return Deps{
		Unit: func(_ context.Context, r Request) (Response, error) {
			f.asked = append(f.asked, r)
			resp := f.healthyUnit(r)
			if f.unit != nil {
				return f.unit(r, resp)
			}
			return resp, nil
		},
		Registered: func(context.Context) ([]Registration, error) {
			regs := []Registration{{ID: "villa-web", Enabled: true}, {ID: "villa-recall", Enabled: true}}
			if f.registers != nil {
				return f.registers(regs)
			}
			return regs, nil
		},
		RecallState: func() (recall.State, error) { return f.state, f.stateErr },
		ContainerNetworks: func() (map[string][]string, error) {
			m := map[string][]string{"villa-tools-web": {routed}, "villa-tools-recall": {closed}}
			if f.networks != nil {
				return f.networks(m)
			}
			return m, nil
		},
		NetworkInternal: func() (map[string]bool, error) {
			m := map[string]bool{closed: true, routed: false}
			if f.internal != nil {
				return f.internal(m)
			}
			return m, nil
		},
		ReachFrom: func(_ context.Context, network, url string) (bool, error) {
			reached := (network == closed && strings.Contains(url, "villa-qdrant")) ||
				(network == routed && strings.Contains(url, "villa-tools-web"))
			if f.reach != nil {
				return f.reach(network, url, reached)
			}
			return reached, nil
		},
		Chat: func(_ context.Context, r ChatRequest) (string, error) {
			if f.chatErr != nil {
				return "", f.chatErr
			}
			lines := []string{"tools: tool=" + toolserver.OperationID(r.Set) + " status=200 duration=1ms results=1 flagged=0"}
			if f.chatCalls != nil {
				lines = f.chatCalls(r.Set)
			}
			f.log[r.Set] = append(f.log[r.Set], lines...)
			return "the first result is t", nil
		},
		ToolLog: func(_ context.Context, set toolserver.Set) (string, error) {
			if f.logErr != nil {
				return "", f.logErr
			}
			return strings.Join(f.log[set], "\n"), nil
		},
	}
}

func (f *fake) run(in Input) Report { return Prove(context.Background(), in, f.deps()) }

func mustLeg(t *testing.T, r Report, name string) Leg {
	t.Helper()
	l, ok := r.Leg(name)
	if !ok {
		t.Fatalf("no leg %q in %+v", name, r.Legs)
	}
	return l
}

func wantLeg(t *testing.T, r Report, name string, status verify.Status, substr ...string) {
	t.Helper()
	l := mustLeg(t, r, name)
	if l.Status != status {
		t.Errorf("leg %s = %s (%s), want %s", name, l.Status, l.Detail, status)
		return
	}
	for _, s := range substr {
		if !strings.Contains(l.Detail, s) {
			t.Errorf("leg %s detail %q lacks %q", name, l.Detail, s)
		}
	}
}

// TestHealthyHostPasses: with every unit behaving as the ADR says, all seven legs run, none
// fails or rejects, and the verdict is a PASS. The PASS says what it did not show.
func TestHealthyHostPasses(t *testing.T) {
	f := newFake()
	r := f.run(input())
	for _, name := range LegNames {
		wantLeg(t, r, name, verify.Pass)
	}
	if len(r.Legs) != 7 {
		t.Errorf("legs = %d, want the ADR's seven", len(r.Legs))
	}
	p := r.Proof()
	if p.Status != verify.Pass {
		t.Fatalf("verdict = %s: %s", p.Status, p.Detail)
	}
	for _, name := range LegNames {
		if !strings.Contains(p.Detail, name) {
			t.Errorf("the verdict does not name leg %s: %s", name, p.Detail)
		}
	}
	if !strings.Contains(p.Detail, "cross-user isolation was not shown") {
		t.Errorf("a one-user PASS must say the cross-user check was not shown: %s", p.Detail)
	}
}

// TestLegOrderIsTheADRs: the legs run in the ADR's order.
func TestLegOrderIsTheADRs(t *testing.T) {
	want := []string{"contract", "refusals", "registration", "web", "scope", "closed-network", "chat"}
	if !slices.Equal(LegNames, want) {
		t.Errorf("LegNames = %v, want %v", LegNames, want)
	}
	r := newFake().run(input())
	for i, l := range r.Legs {
		if l.Name != want[i] {
			t.Errorf("leg %d = %s, want %s", i, l.Name, want[i])
		}
	}
}

// TestContractLeg: each unit's /openapi.json, fetched with its bearer, equals the compiled-in
// document byte for byte. A different document, a refusal of the right bearer and a one-byte
// difference are all FAILs, and a unit that cannot be reached is a REJECT.
func TestContractLeg(t *testing.T) {
	spec, _ := toolserver.OpenAPI(toolserver.SetWeb)
	cases := []struct {
		name   string
		hook   func(Request, Response) (Response, error)
		status verify.Status
		want   string
	}{
		{"served and equal", nil, verify.Pass, "byte for byte"},
		{"another document", func(r Request, resp Response) (Response, error) {
			if r.Path == "/openapi.json" && r.Set == toolserver.SetWeb {
				return Response{Status: 200, Body: []byte(`{"openapi":"3.0.3"}`)}, nil
			}
			return resp, nil
		}, verify.Fail, "web"},
		{"one byte off", func(r Request, resp Response) (Response, error) {
			if r.Path == "/openapi.json" && r.Set == toolserver.SetRecall {
				return Response{Status: 200, Body: append(bytes.Clone(resp.Body), ' ')}, nil
			}
			return resp, nil
		}, verify.Fail, "recall"},
		{"trailing newline stripped", func(r Request, resp Response) (Response, error) {
			if r.Path == "/openapi.json" && r.Set == toolserver.SetWeb {
				return Response{Status: 200, Body: bytes.TrimRight(spec, "\n")}, nil
			}
			return resp, nil
		}, verify.Fail, "web"},
		{"the right bearer is refused", func(r Request, resp Response) (Response, error) {
			if r.Path == "/openapi.json" && r.Auth == AuthRight && r.Set == toolserver.SetRecall {
				return Response{Status: 401}, nil
			}
			return resp, nil
		}, verify.Fail, "401"},
		{"unit unreachable", func(r Request, resp Response) (Response, error) {
			if r.Path == "/openapi.json" && r.Set == toolserver.SetWeb {
				return Response{}, errors.New("curl exit 7")
			}
			return resp, nil
		}, verify.Reject, "curl exit 7"},
	}
	for _, c := range cases {
		f := newFake()
		f.unit = c.hook
		wantLeg(t, f.run(input()), "contract", c.status, c.want)
	}
}

// TestRefusalsLeg: no bearer and a wrong bearer are 401, a POST is 405, and the recall unit
// answers 403 without a well-formed identity and does not refuse one that is. A status that
// differs is a FAIL naming the request; an unreachable unit is a REJECT.
func TestRefusalsLeg(t *testing.T) {
	type tc struct {
		name   string
		hook   func(Request, Response) (Response, error)
		status verify.Status
		want   string
	}
	set := func(s toolserver.Set, pred func(Request) bool, resp Response) func(Request, Response) (Response, error) {
		return func(r Request, orig Response) (Response, error) {
			if r.Set == s && pred(r) {
				return resp, nil
			}
			return orig, nil
		}
	}
	cases := []tc{
		{"healthy", nil, verify.Pass, ""},
		{"no bearer accepted", set(toolserver.SetWeb, func(r Request) bool { return r.Auth == AuthNone && r.Path != "/openapi.json" }, Response{Status: 200, Body: []byte(`{"results":[]}`)}), verify.Fail, "web: no bearer"},
		{"wrong bearer accepted", set(toolserver.SetRecall, func(r Request) bool { return r.Auth == AuthWrong && r.Path != "/openapi.json" }, Response{Status: 200}), verify.Fail, "recall: wrong bearer"},
		{"POST served", set(toolserver.SetWeb, func(r Request) bool { return r.Method == "POST" }, Response{Status: 200}), verify.Fail, "POST"},
		{"recall without identity answered", set(toolserver.SetRecall, func(r Request) bool { return r.Auth == AuthRight && r.Identity == "" && r.Method == "" }, Response{Status: 200, Body: []byte(`{"results":[]}`)}), verify.Fail, "without the identity header"},
		{"recall malformed identity answered", set(toolserver.SetRecall, func(r Request) bool { return strings.ContainsAny(r.Identity, " !") }, Response{Status: 200}), verify.Fail, "malformed identity"},
		{"well-formed identity refused", set(toolserver.SetRecall, func(r Request) bool { return r.Identity == ProbeIdentity && r.Path != "/openapi.json" }, Response{Status: 403}), verify.Fail, "well-formed identity"},
		{"well-formed identity, no scope yet (503)", set(toolserver.SetRecall, func(r Request) bool { return r.Identity == ProbeIdentity && r.Path != "/openapi.json" }, Response{Status: 503}), verify.Pass, ""},
	}
	for _, c := range cases {
		f := newFake()
		f.unit = c.hook
		wantLeg(t, f.run(input()), "refusals", c.status, c.want)
	}
	f := newFake()
	f.unit = func(r Request, resp Response) (Response, error) {
		if r.Set == toolserver.SetRecall && r.Auth == AuthNone {
			return Response{}, errors.New("helper never started")
		}
		return resp, nil
	}
	wantLeg(t, f.run(input()), "refusals", verify.Reject, "helper never started")
}

// TestRegistrationLeg: Open WebUI must list villa-web and villa-recall, enabled. A missing or
// disabled entry is a FAIL, an empty list names the malformed-environment cause, and a list
// that cannot be read is a REJECT. A connection no unit renders is not expected.
func TestRegistrationLeg(t *testing.T) {
	cases := []struct {
		name   string
		hook   func([]Registration) ([]Registration, error)
		in     Input
		status verify.Status
		want   string
	}{
		{"both registered", nil, input(), verify.Pass, "villa-web"},
		{"recall missing", func(r []Registration) ([]Registration, error) { return r[:1], nil }, input(), verify.Fail, "villa-recall"},
		{"web disabled", func(r []Registration) ([]Registration, error) { r[0].Enabled = false; return r, nil }, input(), verify.Fail, "villa-web is registered but disabled"},
		{"empty list", func([]Registration) ([]Registration, error) { return nil, nil }, input(), verify.Fail, "empty list"},
		{"unreadable", func([]Registration) ([]Registration, error) { return nil, errors.New("sign in failed") }, input(), verify.Reject, "sign in failed"},
		{"only the web unit renders", func(r []Registration) ([]Registration, error) { return r[:1], nil }, input(toolserver.SetWeb), verify.Pass, "villa-web"},
	}
	for _, c := range cases {
		f := newFake()
		f.registers = c.hook
		wantLeg(t, f.run(c.in), "registration", c.status, c.want)
	}
}

func webBody(contents ...string) []byte {
	rs := []any{}
	for _, c := range contents {
		rs = append(rs, map[string]any{"title": "t", "url": "https://example.org", "content": c, "guard": map[string]any{"detected": false}})
	}
	b, _ := json.Marshal(map[string]any{"results": rs})
	return b
}

// TestWebLeg: a direct search returns at least one result, each fenced with one nonce and
// carrying a guard verdict. A result that is not fenced or not flagged is a FAIL. The search
// engines refusing, or no result at all, means the leg could not be evaluated: a REJECT, never
// a FAIL of the tool server and never a pass.
func TestWebLeg(t *testing.T) {
	web := func(resp Response, err error) func(Request, Response) (Response, error) {
		return func(r Request, orig Response) (Response, error) {
			if r.Set == toolserver.SetWeb && r.Path != "/openapi.json" && r.Auth == AuthRight && r.Method == "" {
				return resp, err
			}
			return orig, nil
		}
	}
	good := fenced("UNTRUSTED_WEB_CONTENT", "text")
	cases := []struct {
		name   string
		hook   func(Request, Response) (Response, error)
		status verify.Status
		want   string
	}{
		{"fenced and flagged", nil, verify.Pass, "1 result"},
		{"not fenced", web(Response{Status: 200, Body: webBody("plain text")}, nil), verify.Fail, "not fenced"},
		{"nonce differs", web(Response{Status: 200, Body: webBody("[UNTRUSTED_WEB_CONTENT nonce=aaaa]\nx\n[/UNTRUSTED_WEB_CONTENT nonce=bbbb]")}, nil), verify.Fail, "nonce"},
		{"opened and never closed", web(Response{Status: 200, Body: webBody("[UNTRUSTED_WEB_CONTENT nonce=aaaa]\nx")}, nil), verify.Fail, "not fenced"},
		{"second result unfenced", web(Response{Status: 200, Body: webBody(good, "bare")}, nil), verify.Fail, "result 2"},
		{"no guard verdict", web(Response{Status: 200, Body: []byte(`{"results":[{"title":"t","url":"u","content":` + mustJSON(good) + `}]}`)}, nil), verify.Fail, "guard"},
		{"zero results", web(Response{Status: 200, Body: []byte(`{"results":[]}`)}, nil), verify.Reject, "no result"},
		{"engines refused", web(Response{Status: 502, Body: []byte(`{"error":"search engines refused: brave (CAPTCHA)"}`)}, nil), verify.Reject, "could not evaluate: the search engines refused"},
		{"another upstream failure", web(Response{Status: 502, Body: []byte(`{"error":"the upstream service failed or answered out of shape"}`)}, nil), verify.Reject, "502"},
		{"deadline", web(Response{Status: 504}, nil), verify.Reject, "504"},
		{"rate cap", web(Response{Status: 429}, nil), verify.Reject, "429"},
		{"the unit refuses a valid request", web(Response{Status: 400}, nil), verify.Fail, "400"},
		{"not json", web(Response{Status: 200, Body: []byte("<html>")}, nil), verify.Fail, "not the results shape"},
		{"unreachable", web(Response{}, errors.New("curl exit 28")), verify.Reject, "curl exit 28"},
	}
	for _, c := range cases {
		f := newFake()
		f.unit = c.hook
		wantLeg(t, f.run(input()), "web", c.status, c.want)
	}
	wantLeg(t, newFake().run(input(toolserver.SetRecall)), "web", verify.Skip)
}

func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

// TestWebLegUsesAFixedQuery: the one search is a GET with a fixed query and the bearer, and
// carries no identity header.
func TestWebLegUsesAFixedQuery(t *testing.T) {
	f := newFake()
	f.run(input(toolserver.SetWeb))
	var searches []Request
	for _, r := range f.asked {
		if r.Set == toolserver.SetWeb && strings.HasPrefix(r.Path, "/web/search") && r.Auth == AuthRight && r.Method == "" {
			searches = append(searches, r)
		}
	}
	if len(searches) != 1 || !strings.Contains(searches[0].Path, "q=") || searches[0].Identity != "" {
		t.Errorf("web searches = %+v", searches)
	}
}

// TestScopeLeg: a user who owns indexed transcripts gets hits whose chat_id is theirs, each
// fenced with the recall label and carrying a guard verdict (a flagged recall result is
// normal and not a failure), and an identity that owns nothing gets the empty result.
func TestScopeLeg(t *testing.T) {
	cases := []struct {
		name   string
		hook   func(Request, Response) (Response, error)
		mut    func(*fake)
		status verify.Status
		want   string
	}{
		{"owner's hits and an empty control", nil, nil, verify.Pass, "u1"},
		{"a hit of another user's chat", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 200, Body: []byte(`{"results":[{"chat_id":"chat-x","content":` + mustJSON(fenced("RECALL_CONTENT", "t")) + `,"guard":{"detected":false}}]}`)}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "chat-x"},
		{"an unattributable hit", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 200, Body: []byte(`{"results":[{"chat_id":"","content":` + mustJSON(fenced("RECALL_CONTENT", "t")) + `,"guard":{"detected":false}}]}`)}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "no chat_id"},
		{"the control identity gets a result", func(r Request, resp Response) (Response, error) {
			if r.Identity == ProbeIdentity {
				return Response{Status: 200, Body: []byte(`{"results":[{"chat_id":"chat-a","content":"x","guard":{}}]}`)}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "owns nothing"},
		{"the owner gets no hit", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 200, Body: []byte(`{"results":[]}`)}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "no hit"},
		{"the scope is missing (503) though chats are indexed", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 503}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "503"},
		{"recall query fails upstream (502)", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 502}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "502"},
		{"hit not fenced", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{Status: 200, Body: []byte(`{"results":[{"chat_id":"chat-a","content":"bare","guard":{"detected":false}}]}`)}, nil
			}
			return resp, nil
		}, nil, verify.Fail, "not fenced"},
		{"no transcript is indexed", nil, func(f *fake) { f.state.Chats = nil }, verify.Reject, "villa recall index"},
		{"the state cannot be read", nil, func(f *fake) { f.stateErr = errors.New("permission denied") }, verify.Reject, "permission denied"},
		{"unreachable", func(r Request, resp Response) (Response, error) {
			if r.Identity == "u1" {
				return Response{}, errors.New("curl exit 7")
			}
			return resp, nil
		}, nil, verify.Reject, "curl exit 7"},
	}
	for _, c := range cases {
		f := newFake()
		f.unit = c.hook
		if c.mut != nil {
			c.mut(f)
		}
		wantLeg(t, f.run(input()), "scope", c.status, c.want)
	}
	wantLeg(t, newFake().run(input(toolserver.SetWeb)), "scope", verify.Skip)
}

// TestScopeLegCrossUser: with two users who own indexed chats, each user's query must hold
// only their own chats, and the PASS no longer carries the not-shown caveat.
func TestScopeLegCrossUser(t *testing.T) {
	f := newFake()
	f.state.Chats["chat-c"] = recall.ChatState{UserID: "u2", FileID: "f-c"}
	r := f.run(input())
	wantLeg(t, r, "scope", verify.Pass, "u1", "u2")
	if strings.Contains(mustLeg(t, r, "scope").Detail, "not shown") {
		t.Errorf("two users were checked, yet the leg says it was not shown: %s", mustLeg(t, r, "scope").Detail)
	}

	// u2's query leaks u1's chat.
	f = newFake()
	f.state.Chats["chat-c"] = recall.ChatState{UserID: "u2", FileID: "f-c"}
	f.unit = func(req Request, resp Response) (Response, error) {
		if req.Identity == "u2" {
			return Response{Status: 200, Body: []byte(`{"results":[{"chat_id":"chat-a","content":` + mustJSON(fenced("RECALL_CONTENT", "t")) + `,"guard":{"detected":false}}]}`)}, nil
		}
		return resp, nil
	}
	wantLeg(t, f.run(input()), "scope", verify.Fail, "chat-a", "u2")
}

// TestClosedNetworkLeg: the recall unit is on villa-closed only and the web unit on villa
// only, as podman reports and as the rendered units say. The helper on villa-closed reaches
// Qdrant (the positive control, run first) and does not reach 1.1.1.1. A positive control that
// fails makes the leg a REJECT, because a failed external probe would then prove nothing.
func TestClosedNetworkLeg(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*fake)
		in     Input
		status verify.Status
		want   string
	}{
		{"healthy", nil, input(), verify.Pass, "1.1.1.1"},
		{"recall also on villa", func(f *fake) {
			f.networks = func(m map[string][]string) (map[string][]string, error) {
				m["villa-tools-recall"] = []string{closed, routed}
				return m, nil
			}
		}, input(), verify.Fail, "villa-tools-recall"},
		{"web also on villa-closed", func(f *fake) {
			f.networks = func(m map[string][]string) (map[string][]string, error) {
				m["villa-tools-web"] = []string{routed, closed}
				return m, nil
			}
		}, input(), verify.Fail, "villa-tools-web"},
		{"running containers differ from the rendered unit", func(f *fake) {
			f.networks = func(m map[string][]string) (map[string][]string, error) {
				m["villa-tools-recall"] = []string{routed}
				return m, nil
			}
		}, input(), verify.Fail, "villa-tools-recall"},
		{"the rendered unit itself joins the wrong network", nil, func() Input {
			in := input()
			in.Placement["villa-tools-recall"] = []string{routed}
			return in
		}(), verify.Fail, "ADR"},
		{"villa-closed is not internal", func(f *fake) {
			f.internal = func(m map[string]bool) (map[string]bool, error) { m[closed] = false; return m, nil }
		}, input(), verify.Fail, "Internal"},
		{"a route off-box", func(f *fake) {
			f.reach = func(n, u string, reached bool) (bool, error) { return reached || strings.Contains(u, "1.1.1.1"), nil }
		}, input(), verify.Fail, "1.1.1.1"},
		{"the positive control fails", func(f *fake) {
			f.reach = func(n, u string, reached bool) (bool, error) {
				return reached && !strings.Contains(u, "villa-qdrant"), nil
			}
		}, input(), verify.Reject, "positive control"},
		{"the helper cannot start", func(f *fake) {
			f.reach = func(n, u string, reached bool) (bool, error) { return false, errors.New("podman: no such image") }
		}, input(), verify.Reject, "no such image"},
		{"recall reachable from villa", func(f *fake) {
			f.reach = func(n, u string, reached bool) (bool, error) {
				return reached || (n == routed && strings.Contains(u, "villa-tools-recall")), nil
			}
		}, input(), verify.Fail, "villa-tools-recall"},
		{"podman cannot be read", func(f *fake) {
			f.networks = func(map[string][]string) (map[string][]string, error) { return nil, errors.New("podman ps: exit 125") }
		}, input(), verify.Reject, "exit 125"},
		{"a unit is not running", func(f *fake) {
			f.networks = func(m map[string][]string) (map[string][]string, error) {
				delete(m, "villa-tools-web")
				return m, nil
			}
		}, input(), verify.Reject, "villa-tools-web"},
	}
	for _, c := range cases {
		f := newFake()
		if c.mut != nil {
			c.mut(f)
		}
		wantLeg(t, f.run(c.in), "closed-network", c.status, c.want)
	}
}

// TestClosedNetworkLegRunsTheControlFirst: the positive control is asked before the external
// probe, so a probe that fails for want of a working helper is never read as a block.
func TestClosedNetworkLegRunsTheControlFirst(t *testing.T) {
	f := newFake()
	var order []string
	f.reach = func(n, u string, reached bool) (bool, error) {
		order = append(order, n+" "+u)
		return reached, nil
	}
	f.run(input(toolserver.SetRecall))
	iq, ie := -1, -1
	for i, o := range order {
		switch {
		case strings.Contains(o, "villa-qdrant") && iq < 0:
			iq = i
		case strings.Contains(o, "1.1.1.1") && ie < 0:
			ie = i
		}
	}
	if iq < 0 || ie < 0 || iq > ie {
		t.Errorf("probe order = %v, want the Qdrant control before 1.1.1.1", order)
	}
}

// TestChatLeg: one chat per rendered unit offers that unit's tool, and the leg passes only
// when the unit's own request log shows a new call inside the window. A completion with no new
// line is a REJECT however plausible its text. The completion text is in the detail.
func TestChatLeg(t *testing.T) {
	line := func(set toolserver.Set, status int) string {
		return fmt.Sprintf("tools: tool=%s status=%d duration=1ms results=1 flagged=0", toolserver.OperationID(set), status)
	}
	cases := []struct {
		name   string
		calls  func(toolserver.Set) []string
		in     Input
		status verify.Status
		want   string
	}{
		{"called and answered", nil, input(), verify.Pass, "the first result is t"},
		{"no call", func(toolserver.Set) []string { return nil }, input(), verify.Reject, "no tool call"},
		{"a call of another tool does not count", func(s toolserver.Set) []string {
			return []string{"tools: tool=something_else status=200 duration=1ms results=0 flagged=0"}
		}, input(), verify.Reject, "no tool call"},
		{"web call answered 502", func(s toolserver.Set) []string { return []string{line(s, 502)} }, input(toolserver.SetWeb), verify.Reject, "502"},
		{"web call refused 401", func(s toolserver.Set) []string { return []string{line(s, 401)} }, input(toolserver.SetWeb), verify.Fail, "401"},
		{"recall call refused 403: no identity header", func(s toolserver.Set) []string {
			if s == toolserver.SetRecall {
				return []string{line(s, 403)}
			}
			return []string{line(s, 200)}
		}, input(), verify.Fail, "X-Villa-User-Id"},
		{"recall call answered 503: the header arrived", func(s toolserver.Set) []string {
			if s == toolserver.SetRecall {
				return []string{line(s, 503)}
			}
			return []string{line(s, 200)}
		}, input(), verify.Pass, "503"},
	}
	for _, c := range cases {
		f := newFake()
		f.chatCalls = c.calls
		wantLeg(t, f.run(c.in), "chat", c.status, c.want)
	}

	t.Run("a log line from before the window is not a call", func(t *testing.T) {
		f := newFake()
		f.log[toolserver.SetWeb] = []string{line(toolserver.SetWeb, 200)}
		f.chatCalls = func(toolserver.Set) []string { return nil }
		wantLeg(t, f.run(input(toolserver.SetWeb)), "chat", verify.Reject, "no tool call")
	})
	t.Run("chat failure and unreadable log are rejects", func(t *testing.T) {
		f := newFake()
		f.chatErr = errors.New("sign in failed")
		wantLeg(t, f.run(input()), "chat", verify.Reject, "sign in failed")
		g := newFake()
		g.logErr = errors.New("journalctl not found")
		wantLeg(t, g.run(input()), "chat", verify.Reject, "journalctl not found")
	})
	t.Run("the offered connection is the unit's own", func(t *testing.T) {
		f := newFake()
		var got []ChatRequest
		d := f.deps()
		orig := d.Chat
		d.Chat = func(ctx context.Context, r ChatRequest) (string, error) {
			got = append(got, r)
			return orig(ctx, r)
		}
		Prove(context.Background(), input(), d)
		if len(got) != 2 || got[0].ServerID != "villa-web" || got[1].ServerID != "villa-recall" || got[0].Prompt == "" {
			t.Errorf("chat requests = %+v", got)
		}
	})
}

// TestVerdictFold: all legs pass is a PASS, any FAIL is a FAIL naming the leg, an unevaluable
// leg is not a pass, and a leg that does not apply is not counted. FAIL outranks REJECT.
func TestVerdictFold(t *testing.T) {
	mk := func(sts ...verify.Status) Report {
		var r Report
		for i, s := range sts {
			r.Legs = append(r.Legs, Leg{Name: fmt.Sprintf("leg%d", i), Status: s, Detail: "d"})
		}
		return r
	}
	cases := []struct {
		name   string
		r      Report
		status verify.Status
		want   string
	}{
		{"all pass", mk(verify.Pass, verify.Pass), verify.Pass, ""},
		{"pass and skip", mk(verify.Pass, verify.Skip), verify.Pass, ""},
		{"one fail", mk(verify.Pass, verify.Fail, verify.Pass), verify.Fail, "leg1"},
		{"a reject is not a pass", mk(verify.Pass, verify.Reject), verify.Reject, "leg1"},
		{"fail outranks reject", mk(verify.Reject, verify.Fail), verify.Fail, "leg1"},
		{"every leg skipped proves nothing", mk(verify.Skip, verify.Skip), verify.Reject, "nothing"},
		{"no leg at all proves nothing", mk(), verify.Reject, "nothing"},
	}
	for _, c := range cases {
		p := c.r.Proof()
		if p.Status != c.status {
			t.Errorf("%s: %s (%s), want %s", c.name, p.Status, p.Detail, c.status)
		}
		if !strings.Contains(p.Detail, c.want) {
			t.Errorf("%s: detail %q lacks %q", c.name, p.Detail, c.want)
		}
	}
}

// TestNoUnitRenderedIsNotAPass: tool_server on with neither unit rendered leaves nothing to
// prove, so the proof is a REJECT with the remedy, not a PASS over skipped legs.
func TestNoUnitRenderedIsNotAPass(t *testing.T) {
	p := newFake().run(Input{}).Proof()
	if p.Status != verify.Reject || !strings.Contains(p.Detail, "no tool unit") {
		t.Errorf("verdict = %s: %s", p.Status, p.Detail)
	}
}

// TestAFailedLegDoesNotStopTheOthers: every leg runs, so a FAIL in the first still leaves the
// rest reported.
func TestAFailedLegDoesNotStopTheOthers(t *testing.T) {
	f := newFake()
	f.unit = func(r Request, resp Response) (Response, error) {
		if r.Path == "/openapi.json" {
			return Response{Status: 200, Body: []byte("{}")}, nil
		}
		return resp, nil
	}
	r := f.run(input())
	wantLeg(t, r, "contract", verify.Fail)
	wantLeg(t, r, "web", verify.Pass)
	wantLeg(t, r, "chat", verify.Pass)
	if p := r.Proof(); p.Status != verify.Fail || !strings.Contains(p.Detail, "contract") {
		t.Errorf("verdict = %s: %s", p.Status, p.Detail)
	}
}

// TestProofNeverHoldsABearer: the core is given no secret. The wrong bearer is a request kind,
// and the live wiring supplies its value.
func TestProofNeverHoldsABearer(t *testing.T) {
	f := newFake()
	f.run(input())
	kinds := map[Auth]bool{}
	for _, r := range f.asked {
		kinds[r.Auth] = true
	}
	if !kinds[AuthRight] || !kinds[AuthNone] || !kinds[AuthWrong] {
		t.Errorf("auth kinds exercised = %v", kinds)
	}
}
