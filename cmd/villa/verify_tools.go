package main

// verify_tools.go is `villa verify tools`: the proof of villa's own tool server (ADR-0042).
// The legs and the verdict fold are internal/toolproof's; this file is the cobra caller, the
// verify family's gate and exit codes, the record doctor reads, and the live binding of the
// proof's Deps to the host: the in-network curl legs, podman's network reads, Open WebUI's
// protocol client, recall-state.json and the units' journals.
//
// It reads and never mutates the stack, so it takes no stack lock. It does sign in to Open
// WebUI as villa's own service account (the one `verify memory` and the recall indexer
// already know, which the indexer excludes from the chat universe) and leaves one chat per
// rendered unit under it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/toolproof"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// verifyToolsDeps are the host seams of `villa verify tools`.
type verifyToolsDeps struct {
	loadedConfig func() (config.VillaConfig, error)
	proveFn      func(ctx context.Context, cfg config.VillaConfig) verify.Proof
	// persistFn records the verdict for doctor. Its failure never changes the verdict. Nil is
	// a no-op.
	persistFn func(verifystate.State) error
}

func liveVerifyToolsDeps() verifyToolsDeps {
	return verifyToolsDeps{
		loadedConfig: liveLoadedConfig,
		proveFn:      liveToolsProof,
		persistFn:    liveVerifyToolsStatePersist,
	}
}

// liveVerifyToolsStatePersist writes verify-tools-state.json through the traversal-guarded
// atomic writer.
func liveVerifyToolsStatePersist(s verifystate.State) error {
	return verifystate.SaveTools(verifystate.Deps{
		WriteAll: func(data []byte) error {
			return verifystate.ToolsWriteFileAtomic(verifystate.ToolsPath(), data)
		},
	}, s)
}

// liveReadToolsVerifyState loads verify-tools-state.json read-only, as liveReadVerifyState
// does the search record: an absent or corrupt file is the zero State (the freshness gate reads
// it as no result), and a read error is nil, which doctor reads as unreadable.
func liveReadToolsVerifyState() *verifystate.State {
	st, err := verifystate.LoadTools(verifystate.Deps{ReadAll: storeReader(verifystate.ToolsPath())})
	if err != nil {
		return nil
	}
	return &st
}

func newVerifyTools() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "Prove villa's own tool server: contract, refusals, registration, one real search, recall scope, network and one chat",
		Long: "Run seven legs against the running tool units (ADR-0042): each unit serves the compiled-in " +
			"OpenAPI document byte for byte, refuses a missing or wrong bearer (and recall a missing identity), " +
			"Open WebUI lists both connections, a real search comes back fenced and flagged, recall returns a " +
			"user's own chats only, the recall unit has no route off-box (a positive control runs first), and one " +
			"chat through Open WebUI makes a tool call the unit's own log shows. A leg that cannot be conducted " +
			"is a REJECT, never a pass. The cross-user check needs two accounts with indexed chats and is run by " +
			"hand when this run has only one. Gated on the persisted tool_server; exits 0 (passed, or the gate " +
			"is off), 1 (a leg failed) or 2 (a leg could not be conducted). Records its verdict for `villa " +
			"doctor`; mutates nothing in the stack.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runVerifyTools(cmd, args, liveVerifyToolsDeps()))
			return nil
		},
	}
}

// runVerifyTools reads the config before the gate because the gate is a config field: an
// unreadable config refuses rather than reading as the tool server off.
func runVerifyTools(cmd *cobra.Command, _ []string, deps verifyToolsDeps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cfg, err := deps.loadedConfig()
	if err != nil {
		fmt.Fprintf(errOut, "verify tools: cannot read config.toml (%v), fix or remove it, then re-run.\n", err)
		return exitBlocked
	}
	var proof verify.Proof
	outcome := verify.Run(verify.Subject{
		Name:            "verify tools",
		Enabled:         func() bool { return subsystem.ToolServerOn(cfg) },
		DisabledMessage: "the tool server is not enabled (tool_server=false), nothing to verify. Enable it with `villa install --tool-server`, then re-run.",
		FailLabel:       "tool server proof",
		RejectLabel:     "tool server proof",
		Prove: func() verify.Proof {
			proof = deps.proveFn(cmd.Context(), cfg)
			return proof
		},
	})
	if outcome.Status != verify.Skip && deps.persistFn != nil {
		if err := deps.persistFn(verifystate.State{
			Verdict:   toolsVerdictName(proof.Status),
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			fmt.Fprintf(errOut, "verify tools: warning: could not record the result (verdict stands): %v\n", err)
		}
	}
	return renderVerifyOutcome(out, errOut, outcome)
}

// toolsVerdictName is the verdictName vocabulary the doctor reads back.
func toolsVerdictName(s verify.Status) string {
	switch s {
	case verify.Pass:
		return "PASS"
	case verify.Fail:
		return "FAIL"
	}
	return "REJECT"
}

// The bounds of the live legs. Each in-network request starts a helper container, so the
// per-request bound allows for that and for the web tool's own 25 s deadline. The chat bound
// matches Open WebUI's 300 s tool timeout, since a model decoding a tool call and then an
// answer is the slowest thing the proof asks for.
const (
	toolUnitCallTimeout = 45 * time.Second
	toolReachTimeout    = 20 * time.Second
	toolChatTimeout     = 5 * time.Minute
	toolsProofTimeout   = 25 * time.Minute
	// wrongBearer is sent where a request must be refused for its key. It is never a key
	// villa generates, which are 64 hex characters.
	wrongBearer = "villa-verify-not-the-key"
)

// toolUnitAddr returns the unit's address, its network and the bearer config.toml holds.
func toolUnitAddr(cfg config.VillaConfig, set toolserver.Set) (addr string, port int, network, key string, err error) {
	switch set {
	case toolserver.SetWeb:
		return config.ToolsWebAddr, config.ToolsWebPort, routedNetwork, cfg.ToolWebSecret, nil
	case toolserver.SetRecall:
		return config.ToolsRecallAddr, config.ToolsRecallPort, closedNetwork, cfg.ToolRecallSecret, nil
	}
	return "", 0, "", "", fmt.Errorf("no tool unit for set %q", set)
}

// toolCurl builds the curl request for one proof request. The bearer and the identity go in a
// header file on stdin (`-H @-`), never on the command line. curl writes the status after the
// body, behind a newline, so a 4xx is an answer and not a curl failure.
func toolCurl(cfg config.VillaConfig, r toolproof.Request) (args []string, stdin []byte, network string, err error) {
	addr, port, network, key, err := toolUnitAddr(cfg, r.Set)
	if err != nil {
		return nil, nil, "", err
	}
	var headers strings.Builder
	switch r.Auth {
	case toolproof.AuthRight:
		if key == "" {
			return nil, nil, "", fmt.Errorf("config.toml holds no bearer for the %s unit: run `villa up` to generate it", r.Set)
		}
		headers.WriteString("Authorization: Bearer " + key + "\n")
	case toolproof.AuthWrong:
		headers.WriteString("Authorization: Bearer " + wrongBearer + "\n")
	}
	if r.Identity != "" {
		headers.WriteString(toolserver.IdentityHeader + ": " + r.Identity + "\n")
	}
	args = []string{"-s", "-w", "\n%{http_code}"}
	if r.Method != "" && r.Method != "GET" {
		args = append(args, "-X", r.Method)
	}
	if headers.Len() > 0 {
		args = append(args, "-H", "@-")
		stdin = []byte(headers.String())
	}
	args = append(args, "http://"+net.JoinHostPort(addr, strconv.Itoa(port))+r.Path)
	return args, stdin, network, nil
}

// parseToolReply splits curl's output into the body and the status of the -w trailer.
func parseToolReply(out []byte) (toolproof.Response, error) {
	i := bytes.LastIndexByte(out, '\n')
	if i < 0 {
		return toolproof.Response{}, errors.New("curl wrote no status line")
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(out[i+1:])))
	if err != nil || code < 100 || code > 599 {
		return toolproof.Response{}, errors.New("curl's status line is not an HTTP status")
	}
	return toolproof.Response{Status: code, Body: out[:i]}, nil
}

// liveToolUnit performs one proof request in-network, from the unit's own network.
func liveToolUnit(cfg config.VillaConfig) func(context.Context, toolproof.Request) (toolproof.Response, error) {
	return func(ctx context.Context, r toolproof.Request) (toolproof.Response, error) {
		args, stdin, network, err := toolCurl(cfg, r)
		if err != nil {
			return toolproof.Response{}, err
		}
		ctx, cancel := context.WithTimeout(ctx, toolUnitCallTimeout)
		defer cancel()
		out, code, err := probeCurl(ctx, network, orchestrate.EmbedImage(), stdin, args)
		if err != nil {
			return toolproof.Response{}, fmt.Errorf("%s unit: curl exit %d: %w", r.Set, code, err)
		}
		return parseToolReply(out)
	}
}

// classifyToolReach reads one reachability probe. The curl exit set that proves a refusal to
// connect is the family's (classifyReachabilityProbe), so this proof cannot drift from the
// others on what counts as a block.
func classifyToolReach(code int, err error) (reached bool, cerr error) {
	blocked, cerr := classifyReachabilityProbe(nil, code, err, nil, func(code int, e error) error {
		return fmt.Errorf("the probe could not run (curl exit %d: %w), which is not proof of a block", code, e)
	})
	return err == nil && !blocked, cerr
}

// liveToolReach asks whether a helper on network can connect to url. Any HTTP answer counts
// as reached, so no -f.
func liveToolReach(ctx context.Context, network, url string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, toolReachTimeout)
	defer cancel()
	_, code, err := probeCurl(ctx, network, orchestrate.EmbedImage(), nil,
		[]string{"-s", "-o", "/dev/null", "--max-time", "5", url})
	return classifyToolReach(code, err)
}

// toolProofInput composes what the proof compares the host with, from the gates and the
// rendered units' topology.
func toolProofInput(cfg config.VillaConfig, placement map[string][]string) toolproof.Input {
	in := toolproof.Input{
		Container: map[toolserver.Set]string{
			toolserver.SetWeb: config.ToolsWebAddr, toolserver.SetRecall: config.ToolsRecallAddr,
		},
		Placement: placement,
		Routed:    routedNetwork,
		Closed:    closedNetwork,
		QdrantURL: "http://" + net.JoinHostPort(config.QdrantAddr, strconv.Itoa(config.QdrantPort)) + "/healthz",
		HealthURL: map[toolserver.Set]string{
			toolserver.SetWeb:    toolsHealthURL(config.ToolsWebAddr, config.ToolsWebPort),
			toolserver.SetRecall: toolsHealthURL(config.ToolsRecallAddr, config.ToolsRecallPort),
		},
	}
	if subsystem.ToolWebOn(cfg) {
		in.Sets = append(in.Sets, toolserver.SetWeb)
	}
	if subsystem.ToolRecallOn(cfg) {
		in.Sets = append(in.Sets, toolserver.SetRecall)
	}
	return in
}

// toolRegistrations reads Open WebUI's connection list as the service account.
func toolRegistrations(ctx context.Context, owui *openwebui.Client) ([]toolproof.Registration, error) {
	token, err := liveOpenWebUISignIn(ctx, owui)
	if err != nil {
		return nil, fmt.Errorf("sign in to Open WebUI: %w", err)
	}
	servers, err := owui.ToolServers(ctx, token)
	if err != nil {
		return nil, err
	}
	regs := make([]toolproof.Registration, len(servers))
	for i, s := range servers {
		regs[i] = toolproof.Registration{ID: s.ID, Enabled: s.Enabled}
	}
	return regs, nil
}

// driveToolChat runs one completion that offers the unit's connection and returns the model's
// text. The caller learns whether a tool was called from the unit's own log, not from this.
func driveToolChat(ctx context.Context, owui *openwebui.Client, r toolproof.ChatRequest) (string, error) {
	token, err := liveOpenWebUISignIn(ctx, owui)
	if err != nil {
		return "", fmt.Errorf("sign in to Open WebUI: %w", err)
	}
	model, err := owui.DiscoverModel(ctx, token)
	if err != nil {
		return "", fmt.Errorf("discover the chat model: %w", err)
	}
	body, err := openwebui.ToolCompletionBody(model, r.ServerID, r.Prompt)
	if err != nil {
		return "", err
	}
	raw, err := owui.ChatCompletion(ctx, token, body)
	if err != nil {
		return "", err
	}
	answer, _ := parseChatAnswerAndCitation(raw)
	return answer, nil
}

// toolUnitServiceName is the systemd service of a set's unit, the name its journal is under.
func toolUnitServiceName(set toolserver.Set) string {
	if set == toolserver.SetRecall {
		return unitServiceName(orchestrate.ToolsRecallContainerUnitName())
	}
	return unitServiceName(orchestrate.ToolsWebContainerUnitName())
}

// liveToolsProof binds the proof to the host and runs it.
func liveToolsProof(ctx context.Context, cfg config.VillaConfig) verify.Proof {
	ctx, cancel := context.WithTimeout(ctx, toolsProofTimeout)
	defer cancel()
	units, err := liveRenderUnits(cfg, hostVillaPath())
	if err != nil {
		return verify.Proof{Status: verify.Reject, Detail: "the units cannot be rendered from config.toml to compare with the host: " + err.Error()}
	}
	in := toolProofInput(cfg, orchestrate.NetworkTopology(units).Joins)

	// The journal window opens a little before the proof, so a unit's first request line is
	// never cut by the one-second resolution of journalctl's --since.
	start := time.Now().Add(-2 * time.Second)
	owui := liveOpenWebUIClient(owuiLoopbackBase(cfg.ChatPort))
	sd := orchestrate.NewSystemd()

	return toolproof.Prove(ctx, in, toolproof.Deps{
		Unit: liveToolUnit(cfg),
		Registered: func(ctx context.Context) ([]toolproof.Registration, error) {
			return toolRegistrations(ctx, owui)
		},
		RecallState: func() (recall.State, error) {
			return recall.Load(recall.Deps{ReadAll: storeReader(recall.StatePath())})
		},
		ReachFrom:         liveToolReach,
		ContainerNetworks: liveContainerNetworks,
		NetworkInternal:   liveNetworkInternal,
		Chat: func(ctx context.Context, r toolproof.ChatRequest) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, toolChatTimeout)
			defer cancel()
			return driveToolChat(ctx, owui, r)
		},
		ToolLog: func(_ context.Context, set toolserver.Set) (string, error) {
			return sd.JournalSince(toolUnitServiceName(set), start)
		},
	}).Proof()
}
