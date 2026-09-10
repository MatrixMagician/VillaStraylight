package main

// task.go is the `villa task` noun: read the records the runner wrote, and
// answer or stop a task from a second terminal (spec §2). Every verb is one
// call to the loopback API. --json emits the API's bytes VERBATIM, so the
// terminal's contract and internal/taskstore's are the same contract and
// cannot drift; the tables are villa's own rendering of the same records.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// taskDeps are the injectable config + transport seams.
type taskDeps struct {
	load func() (config.VillaConfig, error)
	api  func(config.VillaConfig) taskAPIDeps
}

func liveTaskDeps() *taskDeps {
	return &taskDeps{load: config.LoadVilla, api: liveTaskAPIDeps}
}

// newTask builds the `villa task` noun and its five subcommands.
func newTask() *cobra.Command {
	task := &cobra.Command{
		Use:   "task",
		Short: "Inspect and answer workspace-agent tasks",
		Long: "List the task records, show one, or answer a task parked awaiting approval. " +
			"Every verb talks to the loopback task API served by villa-dashboard.service, so a task " +
			"submitted with `villa work --detach` is answerable from any terminal.",
		Args: cobra.NoArgs,
	}
	task.AddCommand(newTaskList(), newTaskShow(), newTaskApprove(), newTaskDeny(), newTaskCancel())
	return task
}

// newTaskList builds `villa task list [--json]`.
func newTaskList() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the task records",
		Long: "Print every task record in submission order: id, workspace, state, submission time and " +
			"exit code. --json emits the byte-frozen list contract.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runTaskList(cmd, asJSON, liveTaskDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the task list as JSON")
	return cmd
}

// newTaskShow builds `villa task show <id> [--json]`.
func newTaskShow() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one task record",
		Long: "Print one task record: its state and exit code, the files it wrote, the approvals it " +
			"asked for, and what the grounding audit flagged. --json emits the record verbatim.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runTaskShow(cmd, args[0], asJSON, liveTaskDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the task record as JSON")
	return cmd
}

// newTaskApprove builds `villa task approve <id> [--all]`.
func newTaskApprove() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Allow a task's pending approval",
		Long: "Allow the action a task is parked on. --all allows the rest of this task's requests too, " +
			"which is the prompt's `a`. Deletion still asks in every mode.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runTaskAnswer(cmd, "approve", args[0], all, liveTaskDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "allow the rest of this task's requests as well")
	return cmd
}

// newTaskDeny builds `villa task deny <id>`.
func newTaskDeny() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <id>",
		Short: "Deny a task's pending approval",
		Long: "Deny the action a task is parked on. The denial is fed back to the model, recorded and " +
			"narrated, and does not change the exit code.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runTaskAnswer(cmd, "deny", args[0], false, liveTaskDeps()))
			return nil
		},
	}
}

// newTaskCancel builds `villa task cancel <id>`.
func newTaskCancel() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a queued or running task",
		Long: "Drop a queued task or kill a running one and mark it cancelled. The workspace keeps what " +
			"was already written; undoing partial work is a new task, not villa's guess.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runTaskAnswer(cmd, "cancel", args[0], false, liveTaskDeps()))
			return nil
		},
	}
}

// runTaskList prints the record list, RETURNING the exit code.
func runTaskList(cmd *cobra.Command, asJSON bool, d *taskDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	api, code := taskAPIFor(d, errOut)
	if code != exitPass {
		return code
	}
	data, err := api.list(cmdContext(cmd))
	if err != nil {
		return reportAPIError(errOut, "task list", api, err)
	}
	if asJSON {
		_, _ = out.Write(data)
		return exitPass
	}

	var view taskstore.ListView
	if err := json.Unmarshal(data, &view); err != nil {
		fmt.Fprintf(errOut, "task list: unreadable list: %v\n", err)
		return exitBlocked
	}
	if len(view.Tasks) == 0 {
		fmt.Fprintln(out, "no tasks")
		return exitPass
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tWORKSPACE\tSTATE\tSUBMITTED\tEXIT")
	for _, t := range view.Tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Workspace, t.State, t.SubmittedAt, exitCell(t.Exit))
	}
	_ = tw.Flush()
	return exitPass
}

// exitCell renders a nil exit (a task that has not finished) as a dash rather
// than a misleading 0.
func exitCell(exit *int) string {
	if exit == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *exit)
}

// runTaskShow prints one record, RETURNING the exit code.
func runTaskShow(cmd *cobra.Command, id string, asJSON bool, d *taskDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	api, code := taskAPIFor(d, errOut)
	if code != exitPass {
		return code
	}
	data, err := api.show(cmdContext(cmd), id)
	if err != nil {
		return reportAPIError(errOut, "task show", api, err)
	}
	if asJSON {
		_, _ = out.Write(data)
		return exitPass
	}

	var t taskstore.Task
	if err := json.Unmarshal(data, &t); err != nil {
		fmt.Fprintf(errOut, "task show: unreadable record: %v\n", err)
		return exitBlocked
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "id\t%s\n", t.ID)
	fmt.Fprintf(tw, "workspace\t%s\n", t.Workspace)
	fmt.Fprintf(tw, "instruction\t%s\n", t.Instruction)
	fmt.Fprintf(tw, "mode\t%s\n", t.Mode)
	fmt.Fprintf(tw, "state\t%s (exit %s)\n", t.State, exitCell(t.Exit))
	fmt.Fprintf(tw, "harness\t%s %s\n", t.Harness.Name, t.Harness.Version)
	fmt.Fprintf(tw, "submitted\t%s\n", t.SubmittedAt)
	if t.FinishedAt != "" {
		fmt.Fprintf(tw, "finished\t%s\n", t.FinishedAt)
	}
	for _, f := range t.Files {
		fmt.Fprintf(tw, "file\t%s %s\n", f.Action, f.Path)
	}
	for _, a := range t.Approvals {
		fmt.Fprintf(tw, "approval #%d\t%s %s %s: %s\n", a.Seq, a.Tool, a.Action, approvalTarget(a), approvalAnswerCell(a))
	}
	for _, doc := range t.Grounding.Documents {
		fmt.Fprintf(tw, "grounding\t%s: %d claims, %d unsupported\n", doc.Path, doc.Claims, len(doc.Unsupported))
		for _, u := range doc.Unsupported {
			fmt.Fprintf(tw, "flagged\t%s: %q (%s)\n", doc.Path, u.Claim, u.Reason)
		}
	}
	if !t.Grounding.Checked {
		fmt.Fprintf(tw, "grounding\tnot checked\n")
	}
	_ = tw.Flush()
	return exitPass
}

// approvalAnswerCell renders an approval's answer, naming an unanswered one as
// pending rather than leaving the column blank.
func approvalAnswerCell(a taskstore.Approval) string {
	if a.Answer == "" {
		return "pending"
	}
	return a.Answer + " by " + a.By
}

// runTaskAnswer applies approve, deny or cancel and prints the resulting state,
// RETURNING the exit code.
func runTaskAnswer(cmd *cobra.Command, verb, id string, all bool, d *taskDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	api, code := taskAPIFor(d, errOut)
	if code != exitPass {
		return code
	}
	var body any
	if verb == "approve" {
		body = map[string]bool{"all": all}
	}
	if err := api.answer(cmdContext(cmd), verb, id, body); err != nil {
		return reportAPIError(errOut, "task "+verb, api, err)
	}
	fmt.Fprintf(out, "%s %s\n", verb+"d", id)
	return exitPass
}

// taskAPIFor loads config and builds the transport, reporting a config failure
// as an exit code so every verb handles it identically.
func taskAPIFor(d *taskDeps, errOut io.Writer) (taskAPIDeps, int) {
	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "task: load config: %v\n", err)
		return taskAPIDeps{}, exitBlocked
	}
	return d.api(cfg), exitPass
}
