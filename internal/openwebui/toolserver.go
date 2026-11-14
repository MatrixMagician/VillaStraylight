package openwebui

// toolserver.go is the two Open WebUI exchanges `villa verify tools` needs (ADR-0042): the
// admin list of registered tool-server connections, and the body of a completion that
// selects one of them.

import (
	"context"
	"errors"
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

// ToolCompletionBody is the body of a non-streaming completion that offers the model one
// registered tool server. Open WebUI addresses a server connection as `server:<id>` in
// tool_ids, and native function calling makes the model itself emit the call. The request
// carries no tool definition: the server's own document supplies it.
func ToolCompletionBody(model, serverID, prompt string) ([]byte, error) {
	return jsonBody(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"stream":   false,
		"tool_ids": []string{"server:" + serverID},
		"params":   map[string]any{"function_calling": "native"},
	})
}
