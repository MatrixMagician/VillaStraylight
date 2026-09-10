package crushapi

// protocol.go is the bridge/runner stdio contract: one JSON object per line,
// events out of the sandbox, commands in.
//
// It lives beside the client rather than in its own package because the two are
// one vocabulary — a Command is answered by an API call and an Event is what the
// API produced. Splitting them would make the bridge import two packages to say
// one thing, and would let the two halves of a single wire format drift.
//
// The transport is the container's stdin/stdout and nothing else: no unix
// socket, no published port, no `podman exec`, all three of which were measured
// not to work across a krun boundary (spec 3.4).

// CommandKind is the discriminator of a line coming IN to the bridge.
type CommandKind string

const (
	// CmdPrompt submits the task instruction. One per task.
	CmdPrompt CommandKind = "prompt"
	// CmdGrant answers one pending permission request.
	CmdGrant CommandKind = "grant"
	// CmdCancel ends the run. The bridge exits after the child terminates.
	CmdCancel CommandKind = "cancel"
)

// GrantAnswer is Crush's permission action, verbatim
// (internal/proto/proto.go: PermissionAllow / PermissionAllowForSession /
// PermissionDeny). villa does not translate it: the strings ARE the wire values,
// so a rename upstream fails loudly here instead of silently denying.
type GrantAnswer string

const (
	Allow        GrantAnswer = "allow"
	AllowSession GrantAnswer = "allow_session"
	Deny         GrantAnswer = "deny"
)

// Command is one instruction from the host-side runner to the bridge.
type Command struct {
	Kind         CommandKind `json:"kind"`
	PermissionID string      `json:"permission_id,omitempty"`
	Answer       GrantAnswer `json:"answer,omitempty"`
	Prompt       string      `json:"prompt,omitempty"`
}

// Line is one stdio line. Exactly one half is set; the other key is absent, so a
// reader knows which direction it holds without consulting a kind table.
type Line struct {
	Event   *Event   `json:"event,omitempty"`
	Command *Command `json:"command,omitempty"`
}
