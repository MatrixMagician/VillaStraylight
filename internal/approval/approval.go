// Package approval decides how a task's tool call is answered: allow it, ask
// the operator, or deny it outright. Villa never re-derives the Action from a
// tool name or its params — the harness's tool layer is the classifier, and
// this package only answers the table (spec sec3.3). No Deps, no I/O: every
// decision is a pure function of a Mode and a Request.
package approval

import (
	"path/filepath"
	"strings"
)

// Action is the class of a tool call, as classified by the harness.
type Action string

const (
	Read     Action = "read"
	Write    Action = "write"
	Execute  Action = "execute"
	List     Action = "list"
	Fetch    Action = "fetch"
	Download Action = "download"
)

// Mode is the operator's stance for a task. There are two, never Cowork's
// three: Skip does not exist here, because it would also skip the deletion
// rule below.
type Mode string

const (
	ModeAsk  Mode = "ask"
	ModeAuto Mode = "auto"
)

// Decision is the answer Decide returns.
type Decision int

const (
	Allow Decision = iota
	Ask
	Deny
)

// Request is one tool call awaiting a decision.
type Request struct {
	Action    Action
	Tool      string
	Path      string
	Command   string
	Workspace string
}

// table holds the static Action x Mode cells from spec sec3.3. Execute's
// ModeAuto cell is the Ask fallback: Decide resolves the allowlist/safe-list
// exception before falling back to it.
var table = map[Action]map[Mode]Decision{
	Read:     {ModeAsk: Allow, ModeAuto: Allow},
	List:     {ModeAsk: Allow, ModeAuto: Allow},
	Write:    {ModeAsk: Ask, ModeAuto: Allow},
	Execute:  {ModeAsk: Ask, ModeAuto: Ask},
	Fetch:    {ModeAsk: Deny, ModeAuto: Deny},
	Download: {ModeAsk: Deny, ModeAuto: Deny},
}

// Decide answers one Request under one Mode. Execute carries the two named
// exceptions from spec sec3.3: deletion asks in every mode regardless of
// AutoAllowed, and auto mode allows execute only for the allowlist or
// Crush's safe-command list.
func Decide(mode Mode, req Request) Decision {
	if req.Action == Execute {
		if IsDeletion(req.Command) {
			return Ask
		}
		if mode == ModeAuto && (AutoAllowed(req.Command, req.Workspace) || isSafeCommand(req.Command)) {
			return Allow
		}
	}
	return table[req.Action][mode]
}

// chainOperators split a command line into the segments a shell would run in
// sequence: ;, &&, ||, | (order matters: || and && must match before their
// single-character halves).
var chainOperators = strings.NewReplacer(";", "\x00", "&&", "\x00", "||", "\x00", "|", "\x00")

func chainSegments(command string) []string {
	return strings.Split(chainOperators.Replace(command), "\x00")
}

// launchers are tokens whose following token is the command actually
// executed, not an argument: sudo/env per spec sec3.3, plus xargs (it invokes
// its argument as a command, e.g. "cat a | xargs rm").
var launchers = map[string]bool{"sudo": true, "env": true, "xargs": true}

// effectiveCommand returns the token a segment actually executes, skipping
// any leading launcher tokens, or "" if the segment has none.
func effectiveCommand(tokens []string) string {
	i := 0
	for i < len(tokens) && launchers[tokens[i]] {
		i++
	}
	if i >= len(tokens) {
		return ""
	}
	return filepath.Base(tokens[i])
}

var deletionCommands = map[string]bool{
	"rm": true, "rmdir": true, "unlink": true, "shred": true, "trash": true,
}

func containsToken(tokens []string, want string) bool {
	for _, tok := range tokens {
		if tok == want {
			return true
		}
	}
	return false
}

// IsDeletion reports whether command matches the conservative deletion
// pattern: rm, rmdir, unlink, shred, trash, git clean, find ... -delete,
// matched as the command a segment executes (so "echo rm" and "grep rm file"
// don't match, but "sudo rm x" and "cat a | xargs rm" do). This is a
// pattern, not a proof, the way websafe flags and never claims safe.
func IsDeletion(command string) bool {
	for _, segment := range chainSegments(command) {
		tokens := strings.Fields(segment)
		cmd := effectiveCommand(tokens)
		switch cmd {
		case "":
			continue
		case "git":
			if containsToken(tokens, "clean") {
				return true
			}
		case "find":
			if containsToken(tokens, "-delete") {
				return true
			}
		default:
			if deletionCommands[cmd] {
				return true
			}
		}
	}
	return false
}

// allowlistCommands is the villa-rendered auto-mode execute allowlist from
// spec sec3.3, distinct from Crush's own safe-command list below.
var allowlistCommands = map[string]bool{"python3": true, "soffice": true, "mv": true, "cp": true}

// AutoAllowed reports whether command is on the villa-rendered allowlist
// (python3, soffice, mv, cp) AND every path-shaped argument resolves under
// workspace. A ".." escape or an absolute path outside workspace is not
// allowed.
func AutoAllowed(command, workspace string) bool {
	tokens := strings.Fields(command)
	if len(tokens) == 0 || !allowlistCommands[filepath.Base(tokens[0])] {
		return false
	}
	root := filepath.Clean(workspace)
	for _, tok := range tokens[1:] {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		path := tok
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		cleaned := filepath.Clean(path)
		if cleaned != root && !strings.HasPrefix(cleaned, root+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

// safeCommands is Crush's own read-only allowlist, ported from
// internal/agent/tools/safe.go at v0.76.0 (github.com/charmbracelet/crush).
// Refresh this list, and isSafeCommand's matching rule below, if that file
// changes upstream.
var safeCommands = []string{
	"cal", "date", "df", "du", "echo", "env", "free", "groups", "hostname",
	"id", "kill", "killall", "ls", "nice", "nohup", "printenv", "ps", "pwd",
	"set", "time", "timeout", "top", "type", "uname", "unset", "uptime",
	"whatis", "whereis", "which", "whoami",

	"git blame", "git branch", "git config --get", "git config --list",
	"git describe", "git diff", "git grep", "git log", "git ls-files",
	"git ls-remote", "git remote", "git rev-parse", "git shortlog",
	"git show", "git status", "git tag",
}

var chainingMetacharacters = []string{";", "|", "&&", "$(", "`"}

// isSafeCommand mirrors Crush's own bash.go check: no chaining metacharacter
// anywhere in the command, and the (lowercased) command starts with a safe
// prefix followed by end-of-string, a space, or a flag dash.
func isSafeCommand(command string) bool {
	for _, c := range chainingMetacharacters {
		if strings.Contains(command, c) {
			return false
		}
	}
	lower := strings.ToLower(command)
	for _, safe := range safeCommands {
		if strings.HasPrefix(lower, safe) {
			if len(lower) == len(safe) || lower[len(safe)] == ' ' || lower[len(safe)] == '-' {
				return true
			}
		}
	}
	return false
}
