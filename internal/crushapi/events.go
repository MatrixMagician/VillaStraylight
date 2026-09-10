package crushapi

// events.go is the typed read-model of Crush's SSE stream.
//
// Every shape here was derived from the Crush source at tag v0.76.0:
//
//   - the envelope and the kind strings: internal/pubsub/events.go
//     (pubsub.Payload{type,payload}, pubsub.Event[T]{type,payload},
//     PayloadType* constants)
//   - which kinds the server emits and what each carries:
//     internal/server/events.go (wrapEvent)
//   - the wire fields: internal/proto/permission.go (PermissionRequest,
//     PermissionNotification), internal/proto/message.go (Message, the
//     {type,data} part wrapper, MarshalParts), internal/proto/history.go (File),
//     internal/proto/proto.go (RunComplete)
//   - the framing: internal/server/proto.go handleGetWorkspaceEvents writes
//     `data: <json>\n\n` with NO event: name, so the kind is the envelope's
//     `type` field and nothing else.
//
// The reduction is deliberate. villa needs the class of an action, the text a
// task produced and the files it touched; it does not need Crush's whole
// message algebra. Everything not lifted into a typed field stays in Raw, so an
// event kind villa has never seen still crosses the bridge intact rather than
// being dropped by the one process that could have reported it.

import (
	"encoding/json"
	"time"
)

// EventKind is the envelope's discriminator. The six Crush kinds are the
// PayloadType constants villa consumes; the two bridge kinds are villa's own,
// emitted by the in-sandbox process and never by Crush.
type EventKind string

const (
	KindPermissionRequest      EventKind = "permission_request"
	KindPermissionNotification EventKind = "permission_notification"
	KindMessage                EventKind = "message"
	KindFile                   EventKind = "file"
	KindAgentEvent             EventKind = "agent_event"
	KindRunComplete            EventKind = "run_complete"

	// KindBridgeReady is the bridge's first line out: Crush started, its version
	// matched the pin, and the workspace exists.
	KindBridgeReady EventKind = "bridge_ready"
	// KindBridgeError is the bridge's refusal. It is emitted BEFORE any workspace
	// is created when the version assertion fails, so a drifted binary is a
	// refusal rather than a task that ran under an unvetted harness.
	KindBridgeError EventKind = "bridge_error"
)

// Event is one item of the relayed stream.
type Event struct {
	Kind EventKind `json:"kind"`
	// ID is the payload's natural identity: the permission id, the message id,
	// the file id, else the tool-call or session id. Empty when the payload has
	// none.
	ID string `json:"id,omitempty"`
	// Time is when villa RECEIVED the event. Crush's envelope carries no
	// timestamp, so this is honestly the bridge's clock, not the agent's.
	Time time.Time `json:"time"`

	Permission  *PermissionRequest `json:"permission,omitempty"`
	Message     *Message           `json:"message,omitempty"`
	File        *FileEvent         `json:"file,omitempty"`
	RunComplete *RunComplete       `json:"run_complete,omitempty"`

	// Version carries the asserted Crush version on a bridge_ready.
	Version string `json:"version,omitempty"`
	// Error carries the refusal on a bridge_error.
	Error string `json:"error,omitempty"`

	// Raw is the inner pubsub.Event payload verbatim. It is set for every event
	// off the wire, so a kind with no typed field still reaches the runner whole.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// PermissionRequest is Crush's tool-call gate. Action is the string Crush sends
// (read | write | execute | list | fetch | download); villa classifies from it
// and never re-derives it (spec 3.3).
type PermissionRequest struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Tool        string          `json:"tool"`
	Action      string          `json:"action"`
	Path        string          `json:"path,omitempty"`
	Description string          `json:"description,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
}

// Message is the assistant/user turn reduced to what narration needs.
type Message struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id,omitempty"`
	Role      string     `json:"role,omitempty"`
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is one tool invocation inside a message.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input,omitempty"`
}

// FileEvent is Crush's file-history record: a file the task wrote. Version is an
// int64 on the wire (internal/proto/history.go), not a tag string; typing it as a
// string made the whole payload fail to unmarshal and silently dropped the typed
// half of every file event. The on-hardware smoke run is what caught that.
type FileEvent struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Path      string `json:"path"`
	Version   int64  `json:"version,omitempty"`
}

// RunComplete is the terminal event of one turn.
type RunComplete struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// envelope is pubsub.Payload: the discriminated outer wrapper.
type envelope struct {
	Type    EventKind       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// inner is pubsub.Event[T]: the created/updated/deleted verb plus the payload.
// villa ignores the verb — a message arrives as created then repeatedly as
// updated, and the runner narrates the latest either way.
type inner struct {
	Payload json.RawMessage `json:"payload"`
}

// decodeEvent turns one `data:` JSON object into an Event. It never fails on an
// unknown kind: the kind passes through and Raw carries the payload.
func decodeEvent(data []byte, now time.Time) (Event, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Event{}, err
	}
	var in inner
	if err := json.Unmarshal(env.Payload, &in); err != nil {
		return Event{}, err
	}

	ev := Event{Kind: env.Type, Time: now, Raw: in.Payload}

	var ids struct {
		ID         string `json:"id"`
		ToolCallID string `json:"tool_call_id"`
		SessionID  string `json:"session_id"`
	}
	_ = json.Unmarshal(in.Payload, &ids)
	switch {
	case ids.ID != "":
		ev.ID = ids.ID
	case ids.ToolCallID != "":
		ev.ID = ids.ToolCallID
	default:
		ev.ID = ids.SessionID
	}

	switch env.Type {
	case KindPermissionRequest:
		var p struct {
			ID          string          `json:"id"`
			SessionID   string          `json:"session_id"`
			ToolCallID  string          `json:"tool_call_id"`
			ToolName    string          `json:"tool_name"`
			Description string          `json:"description"`
			Action      string          `json:"action"`
			Path        string          `json:"path"`
			Params      json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(in.Payload, &p); err != nil {
			return ev, nil
		}
		ev.Permission = &PermissionRequest{
			ID: p.ID, SessionID: p.SessionID, ToolCallID: p.ToolCallID,
			Tool: p.ToolName, Action: p.Action, Path: p.Path,
			Description: p.Description, Params: p.Params,
		}
	case KindMessage:
		ev.Message = decodeMessage(in.Payload)
	case KindFile:
		var f FileEvent
		if err := json.Unmarshal(in.Payload, &f); err == nil {
			ev.File = &f
		}
	case KindRunComplete:
		var rc RunComplete
		if err := json.Unmarshal(in.Payload, &rc); err == nil {
			ev.RunComplete = &rc
		}
	}
	return ev, nil
}

// decodeMessage flattens Crush's {type,data} part wrapper into the text and the
// tool calls. Reasoning, images, binaries and finish parts are deliberately not
// lifted: they are in Raw, and narrating a model's thinking is not what the
// operator asked to see.
func decodeMessage(payload []byte) *Message {
	var m struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Role      string `json:"role"`
		Parts     []struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil
	}
	out := &Message{ID: m.ID, SessionID: m.SessionID, Role: m.Role}
	for _, p := range m.Parts {
		switch p.Type {
		case "text":
			var t struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(p.Data, &t) == nil {
				out.Text += t.Text
			}
		case "tool_call":
			var tc ToolCall
			if json.Unmarshal(p.Data, &tc) == nil {
				out.ToolCalls = append(out.ToolCalls, tc)
			}
		}
	}
	return out
}
