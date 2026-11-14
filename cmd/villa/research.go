package main

// research.go is `villa research`: ask one question, let the dashboard-hosted
// runner search the web through SearXNG, read the pages through villa-websafe and
// the served model, and follow the task to its terminal state (ADR-0045). Like
// `villa work` it owns no lifecycle logic: it refuses what it can prove wrong
// before submitting, posts, prints the runner's narration, prints the report, and
// exits with the record's own code (taskstore.Exit).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/research"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// researchOpts are the verb's flags. A zero rounds or minutes is the default.
type researchOpts struct {
	detach  bool
	rounds  int
	minutes int
	verbose bool
}

// researchDeps are the injectable seams: config, the API transport, and the local
// reads of the report and the log (both are files under this host's data root,
// with no route, as `villa work --verbose` documents for the log).
type researchDeps struct {
	load       func() (config.VillaConfig, error)
	api        func(config.VillaConfig) taskAPIDeps
	readReport func(id string) ([]byte, error)
	reportPath func(rel string) string
	readLog    func(id string) ([]taskstore.LogEvent, error)
}

func liveResearchDeps() *researchDeps {
	store := taskstore.New(pathsafe.DataRoot())
	return &researchDeps{
		load:       config.LoadVilla,
		api:        liveTaskAPIDeps,
		readReport: store.ReadReport,
		reportPath: store.ReportPath,
		readLog:    store.ReadLog,
	}
}

// newResearch builds `villa research <question>`.
func newResearch() *cobra.Command {
	var opts researchOpts
	cmd := &cobra.Command{
		Use:   "research <question>",
		Short: "Research a question on the web and write a cited report",
		Long: fmt.Sprintf("Search the web through SearXNG, read the result pages through villa-websafe and the served model, "+
			"and write a report with numbered sources, then audit it. The question may be given inline or, when it is `-`, "+
			"read from stdin. The budget is %d rounds and %d minutes (soft) unless raised, up to %d rounds and %d minutes. "+
			"Your queries leave the machine through SearXNG exactly as chat web search's do. The report is a plain-text file "+
			"under the villa data root; a page, and so the report, is model output over untrusted web text, and villa "+
			"flags and defangs what it finds in it, never claiming it is safe. Exits 0 when the report is done, 2 when it was "+
			"flagged (no sources, every source flagged by websafe, a claim with no source, or active content in the report) and 1 "+
			"when it failed, was refused, cancelled or interrupted. Refuses before submitting when web search is off or "+
			"villa-dashboard.service is not answering.",
			research.DefaultRounds, research.DefaultMinutes, research.MaxRounds, research.MaxMinutes),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.verbose = verbose
			os.Exit(runResearch(cmd, args[0], opts, liveResearchDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.detach, "detach", false, "submit, print the task id, and return")
	cmd.Flags().IntVar(&opts.rounds, "rounds", 0, fmt.Sprintf("rounds of search and read (default %d, at most %d)", research.DefaultRounds, research.MaxRounds))
	cmd.Flags().IntVar(&opts.minutes, "minutes", 0, fmt.Sprintf("soft time budget: no new round starts after it (default %d, at most %d)", research.DefaultMinutes, research.MaxMinutes))
	return cmd
}

// runResearch refuses, submits and follows, RETURNING the exit code.
func runResearch(cmd *cobra.Command, questionArg string, opts researchOpts, d *researchDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmdContext(cmd)

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "research: load config: %v\n", err)
		return exitBlocked
	}
	if !subsystem.WebSearchOn(cfg) {
		fmt.Fprintf(errOut, "research: refused. web search is off, so there is no SearXNG to search and no villa-websafe to read pages. Turn it on with: villa install --web-search\n")
		return exitBlocked
	}
	if _, err := (research.Budget{Rounds: opts.rounds, Minutes: opts.minutes}).Resolve(); err != nil {
		fmt.Fprintf(errOut, "research: refused. %v\n", err)
		return exitBlocked
	}
	question, err := workInstruction(cmd, questionArg)
	if err != nil {
		fmt.Fprintf(errOut, "research: %v\n", err)
		return exitBlocked
	}

	api := d.api(cfg)
	task, err := api.submitResearch(ctx, question, opts.rounds, opts.minutes)
	if err != nil {
		return reportAPIError(errOut, "research", api, err)
	}

	if task.State != taskstore.Refused && task.Research != nil {
		b := task.Research.Budget
		line := fmt.Sprintf("research: budget: %d rounds, %d minutes (soft). Queries go to the web through SearXNG, as chat web search's do.", b.Rounds, b.Minutes)
		if opts.detach {
			fmt.Fprintln(errOut, line)
		} else {
			fmt.Fprintln(out, line)
		}
	}
	if opts.detach {
		fmt.Fprintln(out, task.ID)
		return exitPass
	}
	return followResearch(ctx, cmd, api, task, opts, d)
}

// followResearch streams the narration and returns the record's exit code. The
// final record is re-read from the API, so a cut stream cannot invent a terminal
// state, and the report is read from the file the record names.
func followResearch(ctx context.Context, cmd *cobra.Command, api taskAPIDeps, task taskstore.Task, opts researchOpts, d *researchDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	body, err := api.events(ctx, task.ID)
	if err != nil {
		return reportAPIError(errOut, "research", api, err)
	}
	defer body.Close()

	final := task
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
			return true
		}
		fmt.Fprintln(out, ev.Text)
		return true
	}); err != nil {
		fmt.Fprintf(errOut, "research: event stream: %v\n", err)
	}

	if data, err := api.show(ctx, task.ID); err == nil {
		var t taskstore.Task
		if json.Unmarshal(data, &t) == nil {
			final = t
		}
	}

	printResearchSummary(out, errOut, final, d)
	if opts.verbose {
		replayResearchLog(out, errOut, final.ID, d)
	}
	return taskstore.Exit(final.State)
}

// printResearchSummary prints the report (the file is plain text and is printed as
// it is), what the audit and the report check flagged, and the state the exit code
// came from.
func printResearchSummary(out, errOut io.Writer, t taskstore.Task, d *researchDeps) {
	if t.Research != nil && t.Research.Report != "" {
		fmt.Fprintf(out, "report: %s\n\n", d.reportPath(t.Research.Report))
		if data, err := d.readReport(t.ID); err != nil {
			fmt.Fprintf(errOut, "research: read the report: %v\n", err)
		} else {
			fmt.Fprintln(out, strings.TrimRight(string(data), "\n"))
			fmt.Fprintln(out)
		}
	}
	for _, doc := range t.Grounding.Documents {
		for _, u := range doc.Unsupported {
			fmt.Fprintf(out, "flagged: %q (%s)\n", u.Claim, u.Reason)
		}
	}
	fmt.Fprintf(out, "%s %s (exit %d)\n", t.ID, t.State, taskstore.Exit(t.State))
}

// replayResearchLog prints the task's full log, as `villa work --verbose` does.
func replayResearchLog(out, errOut io.Writer, id string, d *researchDeps) {
	events, err := d.readLog(id)
	if err != nil {
		fmt.Fprintf(errOut, "research: read log: %v\n", err)
		return
	}
	for _, ev := range events {
		fmt.Fprintf(out, "%s %s %s\n", ev.At.UTC().Format("2006-01-02T15:04:05Z"), ev.Kind, ev.Text)
	}
}
