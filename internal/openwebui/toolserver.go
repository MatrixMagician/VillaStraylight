package openwebui

// toolserver.go is the two Open WebUI exchanges `villa verify tools` needs (ADR-0042): the
// admin list of registered tool-server connections, and the body of a completion that
// selects one of them.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
)

// ToolServer is one registered tool-server connection as the proof reads it: its id and
// whether it is enabled. The connection also carries a bearer, which has no field here, so no
// caller can print one.
type ToolServer struct {
	ID      string
	Enabled bool
}

// ToolServers returns the connections Open WebUI loaded, in its order. A connection with no
// id is skipped, since it cannot be selected by id. A body that is not the expected list is an
// error, never an empty registration: Open WebUI reads a malformed TOOL_SERVER_CONNECTIONS as
// an empty list, and the proof must tell that apart from a list it could not read.
func (c *Client) ToolServers(ctx context.Context, token string) ([]ToolServer, error) {
	out, err := c.do(ctx, "configs/tool_servers", Request{Path: pathToolServersConfig, Token: token})
	if err != nil {
		return nil, err
	}
	var r struct {
		Connections *[]struct {
			Config struct {
				Enable bool `json:"enable"`
			} `json:"config"`
			Info struct {
				ID string `json:"id"`
			} `json:"info"`
		} `json:"TOOL_SERVER_CONNECTIONS"`
	}
	if derr := decode("configs/tool_servers", out, &r); derr != nil {
		return nil, derr
	}
	if r.Connections == nil {
		return nil, errors.New("configs/tool_servers: no TOOL_SERVER_CONNECTIONS list (" + bodyDetail(out) + ")")
	}
	servers := []ToolServer{}
	for _, conn := range *r.Connections {
		if conn.Info.ID != "" {
			servers = append(servers, ToolServer{ID: conn.Info.ID, Enabled: conn.Config.Enable})
		}
	}
	return servers, nil
}

// ToolCompletionBody is the body of a completion that offers the model one registered tool
// server. Open WebUI addresses a server connection as `server:<id>` in tool_ids, and native
// function calling makes the model itself emit the call. The request carries no tool
// definition: the server's own document supplies it.
//
// Open WebUI only runs a tool server's loop for a streaming request with an event emitter,
// which needs a chat id and a message id. `parent_id: null` with no chat_id makes it create a
// new chat, and `id` becomes the assistant message's id (issue #363). session_id is left out
// on purpose: with one, Open WebUI also offers the model its built-in tools (notes and
// automations among them) and runs the loop in the background. Without one the request blocks
// until the loop ends and answers 200 with a null body.
func ToolCompletionBody(model, serverID, prompt string) ([]byte, error) {
	return jsonBody(map[string]any{
		"model":     model,
		"messages":  []map[string]string{{"role": "user", "content": prompt}},
		"stream":    true,
		"tool_ids":  []string{"server:" + serverID},
		"params":    map[string]any{"function_calling": "native"},
		"parent_id": nil,
		"id":        newUUID(),
		"user_message": map[string]any{
			"id": newUUID(), "parentId": nil, "childrenIds": []string{},
			"role": "user", "content": prompt, "models": []string{model},
		},
	})
}

// newUUID returns a random RFC 4122 version-4 UUID from crypto/rand.
func newUUID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error as of Go 1.24; it panics instead.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
