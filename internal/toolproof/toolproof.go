// Package toolproof is the pure core of `villa verify tools`, the proof of villa's own tool
// server (ADR-0042). It runs seven named legs against the running units and folds them into
// one verify.Proof:
//
//	contract        each unit serves the compiled-in OpenAPI document, byte for byte
//	refusals        no bearer and a wrong bearer are 401, a POST is 405, recall needs identity
//	registration    Open WebUI lists villa-web and villa-recall, enabled
//	web             a real search returns results that are fenced and flagged
//	scope           recall returns a user's own chats and nobody else's
//	closed-network  the recall unit has no route off-box, with a positive control
//	chat            a completion through Open WebUI makes a call the unit's own log shows
//
// The honesty rules are verify's. A leg that cannot be conducted (a unit unreachable, a search
// engine refusing, a model that made no call) is a REJECT, never a pass and never a FAIL of
// the tool server. A leg that ran and found the property absent is a FAIL. A leg that does not
// apply to this install (a unit that is not rendered) is skipped and is not counted.
//
// PURE: no I/O, no os/exec. Every effect arrives through Deps, which the cmd tier binds to the
// host. The core holds no secret: a request names the KIND of bearer to send, and the live
// wiring supplies its value.
package toolproof

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// The legs, in the ADR's order.
const (
	LegContract      = "contract"
	LegRefusals      = "refusals"
	LegRegistration  = "registration"
	LegWeb           = "web"
	LegScope         = "scope"
	LegClosedNetwork = "closed-network"
	LegChat          = "chat"
)

// LegNames lists the legs in the order Prove runs them.
var LegNames = []string{LegContract, LegRefusals, LegRegistration, LegWeb, LegScope, LegClosedNetwork, LegChat}

// ProbeIdentity is a well-formed identity that owns no transcript: the control the scope leg
// queries as, and the identity the refusals leg sends to show a valid one is not refused.
const ProbeIdentity = "villa-verify-probe"

const (
	webQuery    = "wikipedia"
	recallQuery = "conversation"
	// externalProbeURL is the off-box address the closed-network leg must not reach. It is an
	// address, not a name, so a DNS failure cannot read as a block.
	externalProbeURL = "http://1.1.1.1/"
	// maxScopeUsers bounds the cross-user check, which sends one request per user.
	maxScopeUsers = 4
	// completionRunes bounds the model text printed in the verdict.
	completionRunes = 400
)

// Auth is the kind of bearer a request carries.
type Auth int

const (
	// AuthRight sends the unit's own bearer.
	AuthRight Auth = iota
	// AuthNone sends no Authorization header.
	AuthNone
	// AuthWrong sends a bearer that is not the unit's.
	AuthWrong
)

// Request is one request to a tool unit, made from the network the unit joins.
type Request struct {
	Set toolserver.Set
	// Method is empty for GET.
	Method string
	// Path is the path and query.
	Path string
	Auth Auth
	// Identity is the value of the identity header, or empty to send none.
	Identity string
}

// Response is a unit's answer. A non-2xx status is an answer, not an error.
type Response struct {
	Status int
	Body   []byte
}

// Registration is one connection Open WebUI loaded.
type Registration struct {
	ID      string
	Enabled bool
}

// ChatRequest asks for one completion that offers one registered connection to the model.
type ChatRequest struct {
	Set      toolserver.Set
	ServerID string
	Prompt   string
}

// Deps is every effect of the proof. An error from any of them means the question could not
// be asked, which the core reads as a REJECT.
type Deps struct {
	// Unit performs one request on a tool unit from its own network.
	Unit func(ctx context.Context, r Request) (Response, error)
	// Registered reads Open WebUI's tool-server connections.
	Registered func(ctx context.Context) ([]Registration, error)
	// RecallState reads recall-state.json: which user owns which indexed chat.
	RecallState func() (recall.State, error)
	// ReachFrom tries to connect to url from a helper on the named network. reached is true
	// when something answered. An error means the helper could not run, or failed in a way
	// that is neither an answer nor a refusal to connect.
	ReachFrom func(ctx context.Context, network, url string) (reached bool, err error)
	// ContainerNetworks maps each running container to the networks podman puts it on.
	ContainerNetworks func() (map[string][]string, error)
	// NetworkInternal maps each podman network to whether it is internal.
	NetworkInternal func() (map[string]bool, error)
	// Chat drives one completion through Open WebUI and returns the model's text.
	Chat func(ctx context.Context, r ChatRequest) (string, error)
	// ToolLog returns the unit's request log from the start of the proof onward.
	ToolLog func(ctx context.Context, set toolserver.Set) (string, error)
}

// Input is what the config and the rendered units say, so the proof compares the host with
// them and not with constants of its own.
type Input struct {
	// Sets lists the units the config renders, web first. Empty means no unit renders.
	Sets []toolserver.Set
	// Container is each set's container name.
	Container map[toolserver.Set]string
	// Placement is each rendered container's networks, from the rendered units.
	Placement map[string][]string
	// Routed and Closed are the podman names of villa.network and villa-closed.network.
	Routed, Closed string
	// QdrantURL is the positive control of the closed-network leg.
	QdrantURL string
	// HealthURL is each unit's unauthenticated /healthz.
	HealthURL map[toolserver.Set]string
}

func (in Input) has(s toolserver.Set) bool { return slices.Contains(in.Sets, s) }

// Leg is one leg's outcome. Skip means the leg does not apply to this install.
type Leg struct {
	Name   string
	Status verify.Status
	Detail string
}

// Report is the seven legs and the note that stands in for them when none ran.
type Report struct {
	Legs []Leg
	Note string
}

// Leg returns the named leg.
func (r Report) Leg(name string) (Leg, bool) {
	for _, l := range r.Legs {
		if l.Name == name {
			return l, true
		}
	}
	return Leg{}, false
}

// Prove runs every leg and returns the report. A FAIL in one leg never stops the others, so
// one run reports everything it can.
func Prove(ctx context.Context, in Input, d Deps) Report {
	if len(in.Sets) == 0 {
		return Report{Note: "no tool unit renders (tool_server is on but neither web search nor the memory stack is), so there is nothing to prove: enable one with `villa install --web-search --tool-server` or set memory_enabled=true, or set tool_server=false"}
	}
	return Report{Legs: []Leg{
		contractLeg(ctx, in, d),
		refusalsLeg(ctx, in, d),
		registrationLeg(ctx, in, d),
		webLeg(ctx, in, d),
		scopeLeg(ctx, in, d),
		closedNetworkLeg(ctx, in, d),
		chatLeg(ctx, in, d),
	}}
}

// Proof folds the legs into one verdict. Any FAIL is a FAIL, naming the legs. Otherwise any
// REJECT is a REJECT: nothing was disproven, but not everything was shown. Otherwise PASS. A
// report with no leg that was conducted proves nothing and is a REJECT.
func (r Report) Proof() verify.Proof {
	var failed, rejected, passed, skipped []string
	for _, l := range r.Legs {
		switch l.Status {
		case verify.Fail:
			failed = append(failed, l.Name)
		case verify.Reject:
			rejected = append(rejected, l.Name)
		case verify.Pass:
			passed = append(passed, l.Name)
		default:
			skipped = append(skipped, l.Name)
		}
	}
	var head string
	status := verify.Pass
	switch {
	case len(failed) > 0:
		status, head = verify.Fail, "FAILED legs: "+strings.Join(failed, ", ")
		if len(rejected) > 0 {
			head += "; could not be conducted: " + strings.Join(rejected, ", ")
		}
	case len(rejected) > 0:
		status, head = verify.Reject, "legs that could not be conducted: "+strings.Join(rejected, ", ")
	case len(passed) == 0:
		note := r.Note
		if note == "" {
			note = "no leg was conducted, so nothing was proven"
		}
		return verify.Proof{Status: verify.Reject, Detail: note}
	default:
		head = fmt.Sprintf("%d of %d legs passed", len(passed), len(r.Legs))
		if len(skipped) > 0 {
			head += " (not applicable here: " + strings.Join(skipped, ", ") + ")"
		}
	}
	lines := []string{head}
	for _, l := range r.Legs {
		lines = append(lines, "  "+l.Name+": "+legWord(l.Status)+" "+l.Detail)
	}
	return verify.Proof{Status: status, Detail: strings.Join(lines, "\n")}
}

func legWord(s verify.Status) string {
	switch s {
	case verify.Pass:
		return "PASS"
	case verify.Fail:
		return "FAIL"
	case verify.Reject:
		return "REJECT"
	}
	return "n/a"
}

// outcome accumulates one leg's findings.
type outcome struct {
	name    string
	fails   []string
	rejects []string
	notes   []string
}

func (o *outcome) fail(format string, a ...any) { o.fails = append(o.fails, fmt.Sprintf(format, a...)) }
func (o *outcome) reject(format string, a ...any) {
	o.rejects = append(o.rejects, fmt.Sprintf(format, a...))
}
func (o *outcome) note(format string, a ...any) { o.notes = append(o.notes, fmt.Sprintf(format, a...)) }

// done resolves the leg: a FAIL if anything failed, else a REJECT if anything could not be
// conducted, else a PASS saying pass, with any notes appended.
func (o *outcome) done(pass string) Leg {
	switch {
	case len(o.fails) > 0:
		d := strings.Join(o.fails, "; ")
		if len(o.rejects) > 0 {
			d += "; also could not be conducted: " + strings.Join(o.rejects, "; ")
		}
		return Leg{Name: o.name, Status: verify.Fail, Detail: d}
	case len(o.rejects) > 0:
		return Leg{Name: o.name, Status: verify.Reject, Detail: strings.Join(o.rejects, "; ")}
	}
	d := pass
	if len(o.notes) > 0 {
		d += "; " + strings.Join(o.notes, "; ")
	}
	return Leg{Name: o.name, Status: verify.Pass, Detail: d}
}

func skipped(name, why string) Leg { return Leg{Name: name, Status: verify.Skip, Detail: why} }

func setNames(sets []toolserver.Set) string {
	names := make([]string, len(sets))
	for i, s := range sets {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// contractLeg: each unit's /openapi.json, fetched with its bearer, equals the compiled-in
// document byte for byte. A unit built from another villa binary, or a hand-edited document,
// differs here.
func contractLeg(ctx context.Context, in Input, d Deps) Leg {
	o := &outcome{name: LegContract}
	for _, set := range in.Sets {
		want, err := toolserver.OpenAPI(set)
		if err != nil {
			o.reject("%s: the compiled-in document could not be built: %v", set, err)
			continue
		}
		resp, err := d.Unit(ctx, Request{Set: set, Path: "/openapi.json", Auth: AuthRight})
		switch {
		case err != nil:
			o.reject("%s: %v", set, err)
		case resp.Status != 200:
			o.fail("%s: /openapi.json answered %d to the bearer villa holds, so the unit and config.toml disagree on the key: restart the unit with `villa restart`", set, resp.Status)
		case !bytes.Equal(resp.Body, want):
			o.fail("%s: /openapi.json differs from the compiled-in document (%d bytes served, %d expected): the unit runs another villa binary, restart it with `villa restart`", set, len(resp.Body), len(want))
		}
	}
	return o.done(setNames(in.Sets) + " serve the compiled-in OpenAPI document byte for byte")
}

// refusalsLeg: the checks that a request is refused when it should be. It sends only requests
// the unit must refuse, plus one well-formed identity it must not refuse for the identity.
func refusalsLeg(ctx context.Context, in Input, d Deps) Leg {
	o := &outcome{name: LegRefusals}
	expect := func(set toolserver.Set, what string, r Request, want ...int) {
		r.Set = set
		resp, err := d.Unit(ctx, r)
		switch {
		case err != nil:
			o.reject("%s: %s: %v", set, what, err)
		case !slices.Contains(want, resp.Status):
			o.fail("%s: %s answered %d, want %s", set, what, resp.Status, joinInts(want))
		}
	}
	for _, set := range in.Sets {
		path := toolserver.SearchPath(set) + "?q=x"
		expect(set, "/openapi.json with no bearer", Request{Path: "/openapi.json", Auth: AuthNone}, 401)
		expect(set, "no bearer", Request{Path: path, Auth: AuthNone}, 401)
		expect(set, "wrong bearer", Request{Path: path, Auth: AuthWrong}, 401)
		expect(set, "a POST", Request{Method: "POST", Path: path, Auth: AuthRight}, 405)
		if set == toolserver.SetRecall {
			expect(set, "a request without the identity header", Request{Path: path, Auth: AuthRight}, 403)
			expect(set, "a malformed identity", Request{Path: path, Auth: AuthRight, Identity: "bad id!"}, 403)
			expect(set, "a well-formed identity", Request{Path: path, Auth: AuthRight, Identity: ProbeIdentity}, 200, 503)
		}
	}
	return o.done("no bearer and a wrong bearer are 401, a POST is 405, and recall refuses a missing or malformed identity (403) while accepting a well-formed one")
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, " or ")
}

// registrationLeg: Open WebUI lists each rendered unit's connection, enabled. A malformed
// TOOL_SERVER_CONNECTIONS line is read by Open WebUI as an empty list and no error anywhere,
// which is the failure this leg exists to catch.
func registrationLeg(ctx context.Context, in Input, d Deps) Leg {
	o := &outcome{name: LegRegistration}
	regs, err := d.Registered(ctx)
	if err != nil {
		o.reject("Open WebUI's tool-server list could not be read: %v", err)
		return o.done("")
	}
	var ids []string
	for _, set := range in.Sets {
		id := toolserver.ConnectionID(set)
		ids = append(ids, id)
		idx := slices.IndexFunc(regs, func(r Registration) bool { return r.ID == id })
		switch {
		case idx < 0 && len(regs) == 0:
			o.fail("%s is not registered: Open WebUI holds an empty list, which is how it reads a malformed TOOL_SERVER_CONNECTIONS line; restart the chat with `villa restart villa-openwebui`", id)
		case idx < 0:
			o.fail("%s is not registered (Open WebUI lists %d other connection(s)); run `villa up` to rewrite the connection list and restart the chat", id, len(regs))
		case !regs[idx].Enabled:
			o.fail("%s is registered but disabled", id)
		}
	}
	return o.done(strings.Join(ids, " and ") + " registered and enabled")
}

// fenceOf returns the first opening and the last closing nonce of label's fence in text, and
// whether each was found.
func fenceOf(label, text string) (open, closeNonce string, hasOpen, hasClose bool) {
	o := regexp.MustCompile(`\[` + label + ` nonce=([0-9a-f]+)\]`).FindStringSubmatch(text)
	cs := regexp.MustCompile(`\[/`+label+` nonce=([0-9a-f]+)\]`).FindAllStringSubmatch(text, -1)
	if o != nil {
		open, hasOpen = o[1], true
	}
	if len(cs) > 0 {
		closeNonce, hasClose = cs[len(cs)-1][1], true
	}
	return
}

// checkResult reports what is wrong with one result of either tool: its content must be
// fenced with one nonce on both delimiters and it must carry a guard verdict. A flagged
// result is not a fault.
func checkResult(label string, content string, guard json.RawMessage) string {
	open, closeN, hasOpen, hasClose := fenceOf(label, content)
	if !hasOpen || !hasClose {
		return "is not fenced (no opening and closing " + label + " delimiter)"
	}
	if open != closeN {
		return "has a fence whose opening and closing nonces differ"
	}
	var g map[string]json.RawMessage
	if json.Unmarshal(guard, &g) != nil || g == nil {
		return "carries no guard verdict"
	}
	var detected bool
	if raw, ok := g["detected"]; !ok || json.Unmarshal(raw, &detected) != nil {
		return "carries no guard verdict"
	}
	return ""
}

// enginesRefused matches the web tool's 502 when SearXNG's engines all refused.
const enginesRefused = "search engines refused"

// webLeg: one real search through the web tool. The query is fixed and goes to the live search
// engines, as `villa verify search` already does.
func webLeg(ctx context.Context, in Input, d Deps) Leg {
	if !in.has(toolserver.SetWeb) {
		return skipped(LegWeb, "the web unit is not rendered")
	}
	o := &outcome{name: LegWeb}
	resp, err := d.Unit(ctx, Request{Set: toolserver.SetWeb, Path: toolserver.SearchPath(toolserver.SetWeb) + "?q=" + webQuery + "&count=2", Auth: AuthRight})
	if err != nil {
		o.reject("the search could not be made: %v", err)
		return o.done("")
	}
	switch resp.Status {
	case 200:
	case 502:
		if strings.Contains(string(resp.Body), enginesRefused) {
			o.reject("could not evaluate: the search engines refused, so no search ran (%s); retry later", errorText(resp.Body))
		} else {
			o.reject("the search answered 502: SearXNG or a page fetch failed, so the tool's output was not seen; check `villa status`")
		}
		return o.done("")
	case 429, 504:
		o.reject("the search answered %d (a rate or deadline cap), so the tool's output was not seen; retry", resp.Status)
		return o.done("")
	default:
		o.fail("a valid search answered %d", resp.Status)
		return o.done("")
	}
	var body struct {
		Results *[]struct {
			Content string          `json:"content"`
			Guard   json.RawMessage `json:"guard"`
		} `json:"results"`
	}
	if json.Unmarshal(resp.Body, &body) != nil || body.Results == nil {
		o.fail("the 200 body is not the results shape")
		return o.done("")
	}
	if len(*body.Results) == 0 {
		o.reject("the search returned no result, so there is no text to inspect for a fence")
		return o.done("")
	}
	for i, r := range *body.Results {
		if why := checkResult("UNTRUSTED_WEB_CONTENT", r.Content, r.Guard); why != "" {
			o.fail("result %d %s", i+1, why)
		}
	}
	n := len(*body.Results)
	return o.done(fmt.Sprintf("%d result(s), each fenced with one nonce and carrying a guard verdict", n))
}

// errorText reads the {"error": "..."} text of a refusal, or "" when there is none.
func errorText(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return strings.TrimSpace(e.Error)
}

// scopeLeg: recall returns a user's own chats and nobody else's. It queries as each of up to
// four users who own indexed transcripts, and as an identity that owns none, which must get
// the empty result. With one such user the cross-user check cannot be shown, and the leg says
// so rather than implying it.
func scopeLeg(ctx context.Context, in Input, d Deps) Leg {
	if !in.has(toolserver.SetRecall) {
		return skipped(LegScope, "the recall unit is not rendered")
	}
	o := &outcome{name: LegScope}
	state, err := d.RecallState()
	if err != nil {
		o.reject("recall-state.json could not be read: %v", err)
		return o.done("")
	}
	owned := map[string][]string{}
	for chat, c := range state.Chats {
		if c.UserID != "" && c.FileID != "" {
			owned[c.UserID] = append(owned[c.UserID], chat)
		}
	}
	if len(owned) == 0 {
		o.reject("no user owns an indexed transcript, so recall has nothing to return: run `villa recall index`")
		return o.done("")
	}
	users := make([]string, 0, len(owned))
	for u := range owned {
		users = append(users, u)
	}
	slices.Sort(users)
	users = users[:min(len(users), maxScopeUsers)]

	path := toolserver.SearchPath(toolserver.SetRecall) + "?q=" + recallQuery + "&count=10"
	for _, u := range users {
		resp, err := d.Unit(ctx, Request{Set: toolserver.SetRecall, Path: path, Auth: AuthRight, Identity: u})
		if err != nil {
			o.reject("the query as %s could not be made: %v", u, err)
			continue
		}
		if resp.Status != 200 {
			o.fail("the query as %s answered %d although %d of their chats are indexed (503 means the scope file is missing: run `villa up`; 502 means the embedder or Qdrant failed)", u, resp.Status, len(owned[u]))
			continue
		}
		var body struct {
			Results *[]struct {
				ChatID  string          `json:"chat_id"`
				Content string          `json:"content"`
				Guard   json.RawMessage `json:"guard"`
			} `json:"results"`
		}
		if json.Unmarshal(resp.Body, &body) != nil || body.Results == nil {
			o.fail("the query as %s did not answer the results shape", u)
			continue
		}
		if len(*body.Results) == 0 {
			o.fail("%s owns %d indexed chat(s) and got no hit: the scope file or the recall index is stale; run `villa recall index`", u, len(owned[u]))
			continue
		}
		for i, r := range *body.Results {
			switch {
			case r.ChatID == "":
				o.fail("%s: result %d has no chat_id, so it cannot be shown to be theirs", u, i+1)
			case !slices.Contains(owned[u], r.ChatID):
				o.fail("the query as %s returned %s, which is not one of their chats", u, r.ChatID)
			}
			if why := checkResult("RECALL_CONTENT", r.Content, r.Guard); why != "" {
				o.fail("%s: result %d %s", u, i+1, why)
			}
		}
	}

	ctl, err := d.Unit(ctx, Request{Set: toolserver.SetRecall, Path: path, Auth: AuthRight, Identity: ProbeIdentity})
	switch {
	case err != nil:
		o.reject("the control query could not be made: %v", err)
	case ctl.Status != 200:
		o.fail("the control identity, which owns nothing, was answered %d, want 200 with no result", ctl.Status)
	default:
		var body struct {
			Results *[]json.RawMessage `json:"results"`
		}
		if json.Unmarshal(ctl.Body, &body) != nil || body.Results == nil {
			o.fail("the control identity did not get the results shape")
		} else if len(*body.Results) != 0 {
			o.fail("the control identity, which owns nothing, was returned %d result(s): recall is not scoped to the caller", len(*body.Results))
		}
	}

	pass := fmt.Sprintf("as %s every hit is the caller's own fenced chat and an identity that owns nothing gets the empty result", strings.Join(users, ", "))
	if len(users) < 2 {
		o.note("cross-user isolation was not shown: only one user owns indexed chats, so run the two-account check by hand (docs/DEVELOPMENT.md, tool server proof)")
	}
	return o.done(pass)
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// closedNetworkLeg: placement and reach. Placement is read from podman and compared with both
// the rendered units and the ADR's fixed placement (web on villa only, recall on villa-closed
// only). Reach runs from helpers on the units' networks, each with a positive control asked
// first, so a helper that cannot reach anything is never read as a block.
func closedNetworkLeg(ctx context.Context, in Input, d Deps) Leg {
	o := &outcome{name: LegClosedNetwork}

	running, rerr := d.ContainerNetworks()
	internal, ierr := d.NetworkInternal()
	if rerr != nil {
		o.reject("podman's containers could not be read: %v", rerr)
	}
	if ierr != nil {
		o.reject("podman's networks could not be read: %v", ierr)
	}
	for _, set := range in.Sets {
		container := in.Container[set]
		adr := []string{in.Routed}
		if set == toolserver.SetRecall {
			adr = []string{in.Closed}
		}
		rendered := in.Placement[container]
		if !sameSet(rendered, adr) {
			o.fail("%s's unit joins %s, but ADR-0042 fixes %s", container, strings.Join(rendered, ", "), strings.Join(adr, ", "))
		}
		if rerr != nil {
			continue
		}
		got, up := running[container]
		switch {
		case !up:
			o.reject("%s is not running, so its networks cannot be read", container)
		case !sameSet(got, rendered):
			o.fail("%s is on %s, its unit joins %s: restart it with `villa restart %s`", container, strings.Join(got, ", "), strings.Join(rendered, ", "), container)
		}
	}
	if in.has(toolserver.SetRecall) && ierr == nil {
		switch isInternal, exists := internal[in.Closed]; {
		case !exists:
			o.reject("%s does not exist on this host", in.Closed)
		case !isInternal:
			o.fail("%s exists without Internal, so it routes off-box: see `villa doctor` (networks) for the fix", in.Closed)
		}
	}

	if in.has(toolserver.SetRecall) {
		reached, err := d.ReachFrom(ctx, in.Closed, in.QdrantURL)
		switch {
		case err != nil:
			o.reject("the helper on %s could not run: %v", in.Closed, err)
		case !reached:
			o.reject("the positive control failed: a helper on %s cannot reach villa-qdrant, so a failed external probe would prove nothing", in.Closed)
		default:
			ext, err := d.ReachFrom(ctx, in.Closed, externalProbeURL)
			switch {
			case err != nil:
				o.reject("the external probe from %s could not be conducted: %v", in.Closed, err)
			case ext:
				o.fail("a helper on %s reached 1.1.1.1: the recall unit's network has a route off-box", in.Closed)
			}
		}
	}

	if in.has(toolserver.SetWeb) && in.has(toolserver.SetRecall) {
		reached, err := d.ReachFrom(ctx, in.Routed, in.HealthURL[toolserver.SetWeb])
		switch {
		case err != nil:
			o.reject("the helper on %s could not run: %v", in.Routed, err)
		case !reached:
			o.reject("the positive control failed: a helper on %s cannot reach the web unit, so a failed probe of the recall unit would prove nothing", in.Routed)
		default:
			cross, err := d.ReachFrom(ctx, in.Routed, in.HealthURL[toolserver.SetRecall])
			switch {
			case err != nil:
				o.reject("the probe of the recall unit from %s could not be conducted: %v", in.Routed, err)
			case cross:
				o.fail("%s answered from %s: the recall unit is reachable from the routed network", in.Container[toolserver.SetRecall], in.Routed)
			}
		}
	}
	return o.done("each unit is on exactly its network, the recall network is internal, a helper there reaches Qdrant and does not reach 1.1.1.1, and the recall unit is not reachable from " + in.Routed)
}

// LogEntry is one request line of a tool unit's log.
type LogEntry struct {
	Tool   string
	Status int
}

var logLine = regexp.MustCompile(`tool=(\S+) status=(\d+)`)

// ParseToolLog reads the request lines a unit writes ("tool=<id> status=<n> ..."). Text that
// is not such a line is ignored.
func ParseToolLog(text string) []LogEntry {
	var out []LogEntry
	for _, m := range logLine.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[2])
		out = append(out, LogEntry{Tool: m[1], Status: n})
	}
	return out
}

// chatPrompt is what the model is asked per unit. Each names the tool so a model that has it
// has every reason to call it.
func chatPrompt(set toolserver.Set) string {
	if set == toolserver.SetWeb {
		return "Use the " + toolserver.OperationID(set) + " tool to search the web for \"" + webQuery + "\", then tell me the title of the first result."
	}
	return "Use the " + toolserver.OperationID(set) + " tool to search my past conversations for \"" + recallQuery + "\", then tell me how many passages it returned."
}

// callsOf returns the log entries of the set's tool, in order.
func callsOf(set toolserver.Set, text string) []LogEntry {
	var out []LogEntry
	for _, e := range ParseToolLog(text) {
		if e.Tool == toolserver.OperationID(set) {
			out = append(out, e)
		}
	}
	return out
}

// chatLeg: one completion per rendered unit through Open WebUI, offering that unit's tool. The
// evidence is the unit's own request log, read before and after: a completion that read as if
// it had searched, but left no new line, is a REJECT. The line also shows what the unit made of
// the call, which is how the identity header Open WebUI fills is observed.
func chatLeg(ctx context.Context, in Input, d Deps) Leg {
	o := &outcome{name: LegChat}
	var said []string
	for _, set := range in.Sets {
		before, err := d.ToolLog(ctx, set)
		if err != nil {
			o.reject("%s: the unit's log could not be read: %v", set, err)
			continue
		}
		answer, err := d.Chat(ctx, ChatRequest{Set: set, ServerID: toolserver.ConnectionID(set), Prompt: chatPrompt(set)})
		if err != nil {
			o.reject("%s: the completion through Open WebUI could not be driven: %v", set, err)
			continue
		}
		after, err := d.ToolLog(ctx, set)
		if err != nil {
			o.reject("%s: the unit's log could not be read after the chat: %v", set, err)
			continue
		}
		prev, now := callsOf(set, before), callsOf(set, after)
		said = append(said, fmt.Sprintf("%s completion: %q", set, printable(answer)))
		if len(now) <= len(prev) {
			o.reject("%s: the model made no tool call (the unit logged no new %s request), whatever its text says: %q", set, toolserver.OperationID(set), printable(answer))
			continue
		}
		o.judgeCalls(set, now[len(prev):])
	}
	return o.done("the model called each tool and the unit's own log shows it: " + strings.Join(said, "; "))
}

// judgeCalls decides the leg from the statuses the unit logged for the chat's calls.
func (o *outcome) judgeCalls(set toolserver.Set, calls []LogEntry) {
	has := func(codes ...int) bool {
		for _, c := range calls {
			if slices.Contains(codes, c.Status) {
				return true
			}
		}
		return false
	}
	switch {
	case set == toolserver.SetRecall && has(200, 503):
		if has(503) && !has(200) {
			o.note("recall answered 503 to Open WebUI's call: the identity header arrived, and the scope file names no collection yet")
		}
	case set == toolserver.SetWeb && has(200):
	case has(401):
		o.fail("%s: the unit refused Open WebUI's call (401): the key Open WebUI sends is not the unit's; run `villa up`", set)
	case set == toolserver.SetRecall && has(403):
		o.fail("recall: the unit refused Open WebUI's call (403): the %s header did not arrive or is malformed, so recall cannot be scoped (ADR-0042 'Not established': the identity header)", toolserver.IdentityHeader)
	default:
		o.reject("%s: the unit answered Open WebUI's call %d, so the tool's output was not seen", set, calls[len(calls)-1].Status)
	}
}

// printable cuts model text to what a verdict line should hold and drops control characters.
func printable(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > completionRunes {
		return string(r[:completionRunes]) + "..."
	}
	return s
}
