package toolserver

// openapi.go builds the OpenAPI 3.0.3 document of one set from the tool table. Open WebUI
// fetches it from /openapi.json with the bearer and registers each operation as a tool, so
// the descriptions here are read by the model: they are the contract, frozen by a golden.
// The document is deterministic (struct fields in a fixed order, map keys sorted by
// encoding/json), which is what lets `villa verify tools` compare it byte for byte.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type specDoc struct {
	OpenAPI    string                  `json:"openapi"`
	Info       specInfo                `json:"info"`
	Paths      map[string]specPathItem `json:"paths"`
	Components specComponents          `json:"components"`
}

type specInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// specPathItem has only a get operation: there is no other method to document.
type specPathItem struct {
	Get specOperation `json:"get"`
}

type specOperation struct {
	OperationID string                  `json:"operationId"`
	Summary     string                  `json:"summary,omitempty"`
	Description string                  `json:"description"`
	Parameters  []specParam             `json:"parameters"`
	Responses   map[string]specResponse `json:"responses"`
	Security    []map[string][]string   `json:"security"`
}

type specParam struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
}

type specResponse struct {
	Description string                   `json:"description"`
	Content     map[string]specMediaType `json:"content,omitempty"`
}

type specMediaType struct {
	Schema map[string]any `json:"schema"`
}

type specComponents struct {
	Schemas         map[string]map[string]any    `json:"schemas"`
	SecuritySchemes map[string]map[string]string `json:"securitySchemes"`
}

var setInfo = map[Set]specInfo{
	SetWeb: {
		Title:       "villa web search",
		Description: "Web search through villa's web guard. Read-only.",
	},
	SetRecall: {
		Title:       "villa recall",
		Description: "Search of the caller's own past conversations. Read-only.",
	},
}

// errorRef points every refusal at one shared schema, the {"error": "..."} body.
var errorRef = map[string]any{"$ref": "#/components/schemas/Error"}

// errorSchema is that schema.
var errorSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"error": map[string]any{"type": "string"}},
	"required":   []string{"error"},
}

// errorResponses are the refusals a tool can return, with the status each is for.
func errorResponses(t Tool) map[string]specResponse {
	jsonErr := map[string]specMediaType{"application/json": {Schema: errorRef}}
	codes := []struct {
		status int
		text   string
		only   bool // only tools with an identity
	}{
		{http.StatusBadRequest, "A parameter is missing, out of range or unknown.", false},
		{http.StatusUnauthorized, "The bearer is missing or wrong.", false},
		{http.StatusForbidden, "The caller's identity is missing or malformed.", true},
		{http.StatusTooManyRequests, "The rate or concurrency cap is reached. Retry after the Retry-After seconds.", false},
		{http.StatusBadGateway, "An upstream service failed or answered out of shape.", false},
		{http.StatusServiceUnavailable, "The recall index has not been built.", true},
		{http.StatusGatewayTimeout, "The request deadline passed.", false},
	}
	out := map[string]specResponse{}
	for _, c := range codes {
		if c.only && !t.Identity {
			continue
		}
		out[strconv.Itoa(c.status)] = specResponse{Description: c.text, Content: jsonErr}
	}
	return out
}

func (t Tool) operation() specOperation {
	var params []specParam
	for _, p := range t.params() {
		params = append(params, specParam{Name: p.Name, In: "query", Required: p.Required, Description: p.Description, Schema: p.Schema})
	}
	resp := errorResponses(t)
	schema := t.Response
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	resp["200"] = specResponse{
		Description: "The results.",
		Content:     map[string]specMediaType{"application/json": {Schema: schema}},
	}
	return specOperation{
		OperationID: t.OperationID,
		Summary:     t.Summary,
		Description: t.Description,
		Parameters:  params,
		Responses:   resp,
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}
}

// buildSpec renders the document for set over tools.
func buildSpec(set Set, tools []Tool) ([]byte, error) {
	info, ok := setInfo[set]
	if !ok {
		return nil, fmt.Errorf("toolserver: no document info for set %q", set)
	}
	info.Version = ContractVersion
	doc := specDoc{
		OpenAPI:    "3.0.3",
		Info:       info,
		Paths:      map[string]specPathItem{},
		Components: specComponents{Schemas: map[string]map[string]any{"Error": errorSchema}, SecuritySchemes: map[string]map[string]string{"bearerAuth": {"type": "http", "scheme": "bearer"}}},
	}
	for _, t := range tools {
		doc.Paths[t.Path] = specPathItem{Get: t.operation()}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// OpenAPI returns the compiled-in document of set, the bytes a unit serves at
// /openapi.json. `villa verify tools` compares a live unit's answer with it.
func OpenAPI(set Set) ([]byte, error) { return buildSpec(set, toolsFor(set)) }
