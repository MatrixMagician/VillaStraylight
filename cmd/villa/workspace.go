package main

// workspace.go wires `villa workspace add|list|remove`: the cmd-tier surface
// over internal/workspace's pure Register/Remove/Registered core. add/remove
// mutate config.toml through the SAME load/save seam every other config-editing
// verb uses, so tests drive them off-hardware with fakes; list only reads.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/workspace"
)

// workspaceDeps are the injectable config load/save + workspace resolution
// seams. workspace_test.go replaces them with stubs so a table test never
// touches the user's real XDG config or a live symlink.
type workspaceDeps struct {
	load func() (config.VillaConfig, error)
	save func(config.VillaConfig) error
	wd   workspace.Deps
}

// liveWorkspaceCmdDeps wires workspaceDeps to the real XDG config store and
// the real filesystem.
func liveWorkspaceCmdDeps() *workspaceDeps {
	return &workspaceDeps{
		load: config.LoadVilla,
		save: config.SaveVilla,
		wd:   liveWorkspaceDeps(),
	}
}

// liveWorkspaceDeps wires workspace.Deps to the real host: this is the ONE
// place internal/workspace's injected filesystem calls are bound.
func liveWorkspaceDeps() workspace.Deps {
	return workspace.Deps{
		Home: os.UserHomeDir,
		ConfigRoot: func() string {
			p, err := config.Path()
			if err != nil {
				return ""
			}
			return filepath.Dir(p)
		},
		DataRoot:     pathsafe.DataRoot,
		Stat:         os.Stat,
		EvalSymlinks: filepath.EvalSymlinks,
	}
}

// newWorkspace builds the `villa workspace` noun and its add/list/remove
// subcommands.
func newWorkspace() *cobra.Command {
	ws := &cobra.Command{
		Use:   "workspace",
		Short: "Manage the registered workspace grant list",
		Long: "Register (add), list, or forget (remove) folders the workspace agent may operate on " +
			"(spec §3.1). `villa work` refuses any path that is not on this list.",
		Args: cobra.NoArgs,
	}
	ws.AddCommand(newWorkspaceAdd(), newWorkspaceList(), newWorkspaceRemove())
	return ws
}

// newWorkspaceAdd builds `villa workspace add <path>`.
func newWorkspaceAdd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Register a folder as a workspace",
		Long: "Register path as a workspace grant. Refuses: a relative path; one villa cannot resolve " +
			"and contain (symlinks followed once); the home directory itself, or a path outside it; " +
			"a path overlapping villa's XDG config or data root; a path nested with an existing grant, " +
			"either direction; a path that does not exist or is not a directory. Re-adding an already-" +
			"granted path is a no-op.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runWorkspaceAdd(cmd, args[0], liveWorkspaceCmdDeps()))
			return nil
		},
	}
}

// runWorkspaceAdd loads config, registers path, and persists on success,
// RETURNING the exit code (no os.Exit) so tests assert output + code.
func runWorkspaceAdd(cmd *cobra.Command, path string, d *workspaceDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "workspace add: load config: %v\n", err)
		return exitBlocked
	}

	next, err := workspace.Register(cfg, path, d.wd)
	if err != nil {
		var refusal workspace.Refusal
		if errors.As(err, &refusal) {
			fmt.Fprintf(errOut, "workspace add: refused (%s) — %s\n", refusal.Kind, refusal.Remediation)
		} else {
			fmt.Fprintf(errOut, "workspace add: %v\n", err)
		}
		return exitBlocked
	}

	if err := d.save(next); err != nil {
		fmt.Fprintf(errOut, "workspace add: save config: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(out, "registered %s\n", path)
	return exitPass
}

// newWorkspaceList builds `villa workspace list [--json]`.
func newWorkspaceList() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered workspaces",
		Long:  "Print the registered workspace grants read from config.toml. --json emits the machine-readable form.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runWorkspaceList(cmd, asJSON, liveWorkspaceCmdDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the workspace list as JSON")
	return cmd
}

// workspaceListEntry is one row of the `workspace list --json` shape.
type workspaceListEntry struct {
	Path string `json:"path"`
}

// workspaceListView is the `workspace list --json` contract: byte-frozen by
// cmd/villa/testdata/workspace-list.golden.json.
type workspaceListView struct {
	Schema     int                  `json:"schema"`
	Workspaces []workspaceListEntry `json:"workspaces"`
}

// runWorkspaceList reads config and renders the grant list, RETURNING the exit
// code (no os.Exit) so tests assert output + code.
func runWorkspaceList(cmd *cobra.Command, asJSON bool, d *workspaceDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "workspace list: load config: %v\n", err)
		return exitBlocked
	}

	if asJSON {
		view := workspaceListView{Schema: 1, Workspaces: []workspaceListEntry{}}
		for _, p := range cfg.Workspace {
			view.Workspaces = append(view.Workspaces, workspaceListEntry{Path: p})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(view); err != nil {
			fmt.Fprintf(errOut, "workspace list: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}

	if len(cfg.Workspace) == 0 {
		fmt.Fprintln(out, "no registered workspaces")
		return exitPass
	}
	for _, p := range cfg.Workspace {
		fmt.Fprintln(out, p)
	}
	return exitPass
}

// newWorkspaceRemove builds `villa workspace remove <path>`.
func newWorkspaceRemove() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <path>",
		Short: "Forget a registered workspace",
		Long:  "Forget the workspace grant at path. Resolves path the same way add does, so any form " + "that once registered it works here too. Touches no file. Refuses an unregistered path.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runWorkspaceRemove(cmd, args[0], liveWorkspaceCmdDeps()))
			return nil
		},
	}
}

// runWorkspaceRemove loads config, resolves path against the grant list, and
// persists the removal, RETURNING the exit code (no os.Exit) so tests assert
// output + code.
func runWorkspaceRemove(cmd *cobra.Command, path string, d *workspaceDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "workspace remove: load config: %v\n", err)
		return exitBlocked
	}

	resolved, ok := workspace.Registered(cfg, path, d.wd)
	if !ok {
		fmt.Fprintf(errOut, "workspace remove: %q is not a registered workspace\n", path)
		return exitBlocked
	}

	next, err := workspace.Remove(cfg, resolved)
	if err != nil {
		fmt.Fprintf(errOut, "workspace remove: %v\n", err)
		return exitBlocked
	}
	if err := d.save(next); err != nil {
		fmt.Fprintf(errOut, "workspace remove: save config: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(out, "removed %s\n", resolved)
	return exitPass
}
