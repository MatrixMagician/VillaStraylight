package main

// work.go is `villa work`: submit one instruction against one registered
// workspace and follow it to its terminal state (spec §2, §5). The verb owns
// no lifecycle logic. It refuses what it can prove wrong before submitting,
// posts, prints the runner's narration, answers approvals at the prompt, and
// exits with the record's own code. Every decision after the POST belongs to
// the runner in villa-dashboard.service.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
	"github.com/MatrixMagician/VillaStraylight/internal/workspace"
)

// workEvent is the SSE payload the task stream carries (internal/taskrun's
// Narration). It is redeclared here rather than imported so the terminal
// depends on the wire contract and not on the runner package.
type workEvent struct {
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"`
	Text string          `json:"text"`
	Task *taskstore.Task `json:"task"`
}

// workOpts are the verb's three flags.
type workOpts struct {
	detach  bool
	auto    bool
	verbose bool
}

// workDeps are the injectable seams: config, workspace resolution, the API
// transport, the approval prompt, and the local log read --verbose replays.
type workDeps struct {
	load    func() (config.VillaConfig, error)
	wd      workspace.Deps
	api     func(config.VillaConfig) taskAPIDeps
	prompt  func() (string, error)
	readLog func(id string) ([]taskstore.LogEvent, error)
}

// liveWorkDeps wires workDeps to the real host.
func liveWorkDeps() *workDeps {
	stdin := bufio.NewReader(os.Stdin)
	return &workDeps{
		load:   config.LoadVilla,
		wd:     liveWorkspaceDeps(),
		api:    liveTaskAPIDeps,
		prompt: func() (string, error) { return stdin.ReadString('\n') },
		readLog: func(id string) ([]taskstore.LogEvent, error) {
			return taskstore.New(pathsafe.DataRoot()).ReadLog(id)
		},
	}
}

// newWork builds `villa work <workspace> <instruction>`.
func newWork() *cobra.Command {
	var opts workOpts
	cmd := &cobra.Command{
		Use:   "work <workspace> <instruction>",
		Short: "Run one instruction against a registered workspace",
		Long: "Submit one instruction to the workspace agent and follow it to its terminal state. " +
			"The instruction may be given inline or, when it is `-`, read from stdin. Exits 0 when the " +
			"task is done, 2 when it finished with grounding flags that need a look, and 1 when it " +
			"failed, was refused, cancelled or interrupted. Refuses before submitting when the path is " +
			"not a registered workspace, when tools mode is off, or when villa-dashboard.service is not " +
			"answering. Write-class actions are approved at the prompt: y once, a for the rest of this " +
			"task, anything else denies.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.verbose = verbose
			os.Exit(runWork(cmd, args[0], args[1], opts, liveWorkDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.detach, "detach", false, "submit, print the task id, and return")
	cmd.Flags().BoolVar(&opts.auto, "auto", false, "this task's writes need no approval (deletion still asks)")
	return cmd
}

// runWork refuses, submits and follows, RETURNING the exit code (no os.Exit)
// so tests assert output + code.
func runWork(cmd *cobra.Command, wsArg, instrArg string, opts workOpts, d *workDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmdContext(cmd)

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "work: load config: %v\n", err)
		return exitBlocked
	}

	resolved, ok := workspace.Registered(cfg, wsArg, d.wd)
	if !ok {
		fmt.Fprintf(errOut, "work: refused. %q is not a registered workspace. Grant it with: villa workspace add %s\n", wsArg, wsArg)
		return exitBlocked
	}
	if !subsystem.ToolsOn(cfg) {
		fmt.Fprintf(errOut, "work: refused. tools mode is off, so the chat unit is not served with the model's own template and the agent has no tool loop. Turn it on with: villa tools-mode enter\n")
		return exitBlocked
	}

	instruction, err := workInstruction(cmd, instrArg)
	if err != nil {
		fmt.Fprintf(errOut, "work: %v\n", err)
		return exitBlocked
	}

	mode := "ask"
	if opts.auto {
		mode = "auto"
	}

	api := d.api(cfg)
	task, err := api.submit(ctx, resolved, instruction, mode)
	if err != nil {
		return reportAPIError(errOut, "work", api, err)
	}

	if opts.detach {
		fmt.Fprintln(out, task.ID)
		return exitPass
	}
	return followTask(ctx, cmd, api, task, opts, d)
}

// workInstruction returns the instruction, reading stdin when the positional
// is "-" so a long prompt need not be shell-quoted.
func workInstruction(cmd *cobra.Command, arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		return "", fmt.Errorf("read instruction from stdin: %w", err)
	}
	instruction := strings.TrimSpace(string(data))
	if instruction == "" {
		return "", errors.New("read instruction from stdin: it is empty")
	}
	return instruction, nil
}

// followTask streams the task's narration, answers approvals at the prompt,
// and returns the record's exit code. The final record is re-read from the API
// rather than trusted from the last frame, because a cut stream must not be
// able to invent a terminal state.
func followTask(ctx context.Context, cmd *cobra.Command, api taskAPIDeps, task taskstore.Task, opts workOpts, d *workDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	body, err := api.events(ctx, task.ID)
	if err != nil {
		return reportAPIError(errOut, "work", api, err)
	}
	defer body.Close()

	final := task
	prompted := 0
	if err := scanSSE(body, func(f sseFrame) bool {
		var ev workEvent
		if json.Unmarshal(f.data, &ev) != nil {
			return true
		}
		if ev.Task != nil {
			final = *ev.Task
		}
		if f.event == "state" {
			fmt.Fprintf(out, "[%s]\n", ev.Text)
			if ev.Text == string(taskstore.AwaitingApproval) && ev.Task != nil {
				prompted = answerApproval(ctx, cmd, api, *ev.Task, prompted, d)
			}
			return true
		}
		fmt.Fprintln(out, ev.Text)
		return true
	}); err != nil {
		fmt.Fprintf(errOut, "work: event stream: %v\n", err)
	}

	if data, err := api.show(ctx, task.ID); err == nil {
		var t taskstore.Task
		if json.Unmarshal(data, &t) == nil {
			final = t
		}
	}

	printTaskSummary(out, final)
	if opts.verbose {
		replayLog(out, errOut, final.ID, d)
	}
	return taskstore.Exit(final.State)
}

// answerApproval prompts for the record's pending approval and posts the
// answer, returning the highest seq answered. The seq guard is what keeps a
// repeated awaiting_approval frame from asking the same question twice.
func answerApproval(ctx context.Context, cmd *cobra.Command, api taskAPIDeps, t taskstore.Task, prompted int, d *workDeps) int {
	pending, ok := pendingApproval(t)
	if !ok || pending.Seq <= prompted {
		return prompted
	}
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintf(out, "approval #%d: %s wants to %s %s\n", pending.Seq, pending.Tool, pending.Action, approvalTarget(pending))
	fmt.Fprint(out, "allow? [y once / a for the rest of this task / N deny]: ")

	line, err := d.prompt()
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(errOut, "work: read answer: %v\n", err)
	}

	verb, all := approvalVerb(line)
	var apiBody any
	if verb == "approve" {
		apiBody = map[string]bool{"all": all}
	}
	if err := api.answer(ctx, verb, t.ID, apiBody); err != nil {
		fmt.Fprintf(errOut, "work: %s approval #%d: %v\n", verb, pending.Seq, err)
	}
	return pending.Seq
}

// approvalVerb maps the operator's line to a route: y approves once, a
// approves the rest of the task, and ANYTHING else denies, because the default
// is deny (spec §3.3).
func approvalVerb(line string) (verb string, all bool) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y":
		return "approve", false
	case "a":
		return "approve", true
	default:
		return "deny", false
	}
}

// pendingApproval is the record's unanswered approval, if it has one.
func pendingApproval(t taskstore.Task) (taskstore.Approval, bool) {
	for i := len(t.Approvals) - 1; i >= 0; i-- {
		if t.Approvals[i].Answer == "" {
			return t.Approvals[i], true
		}
	}
	return taskstore.Approval{}, false
}

// approvalTarget names what the approval is over. The task record carries no
// command string, so a bash execute with no path shows as the workspace itself.
func approvalTarget(a taskstore.Approval) string {
	if a.Path != "" {
		return a.Path
	}
	return "the workspace"
}

// printTaskSummary is the closing report: what the task wrote, what the
// grounding audit flagged, and the state the exit code came from.
func printTaskSummary(out io.Writer, t taskstore.Task) {
	for _, f := range t.Files {
		fmt.Fprintf(out, "%s %s\n", f.Action, f.Path)
	}
	for _, doc := range t.Grounding.Documents {
		for _, u := range doc.Unsupported {
			fmt.Fprintf(out, "flagged %s: %q is unsupported (%s)\n", doc.Path, u.Claim, u.Reason)
		}
	}
	fmt.Fprintf(out, "%s %s (exit %d)\n", t.ID, t.State, taskstore.Exit(t.State))
}

// replayLog prints the task's full transcript. The log is a file under this
// host's villa data root, so --verbose only works on the machine that ran the
// task; there is no log route, by design (the terminal prints narration and
// the transcript never crosses the API).
func replayLog(out, errOut io.Writer, id string, d *workDeps) {
	events, err := d.readLog(id)
	if err != nil {
		fmt.Fprintf(errOut, "work: read log: %v\n", err)
		return
	}
	for _, ev := range events {
		text := ev.Text
		if text == "" {
			text = string(ev.Raw)
		}
		fmt.Fprintf(out, "%s %s %s\n", ev.At.UTC().Format(time.RFC3339), ev.Kind, text)
	}
}

// reportAPIError prints an API failure, giving the unreachable case its own
// remediation, and returns the exit code.
func reportAPIError(errOut io.Writer, verb string, api taskAPIDeps, err error) int {
	if errors.Is(err, errAPIUnreachable) {
		fmt.Fprintf(errOut, "%s: refused. %s on %s. %s\n", verb, errAPIUnreachable, api.base, dashboardDownRemediation)
		return exitBlocked
	}
	fmt.Fprintf(errOut, "%s: %v\n", verb, err)
	return exitBlocked
}
