package orchestrate

// toolserver.go holds villa's own tool server's managed-service view builders and its
// three 0600 env files (#313, ADR-0042). Two units run the villa binary in the distroless
// base villa-websafe already pins: villa-tools-web on villa.network (it must reach SearXNG
// and fetch pages) and villa-tools-recall on villa-closed.network (it needs Qdrant and the
// embedder only). No unit joins both, so the recall index is never one hop from a unit
// that parses untrusted pages.
//
// Both reuse ComponentWebsafe and websafeBinaryPath, so this file adds no image literal,
// no pin row and no seam-gate surface.
//
// Secret discipline, as for villa-websafe: a bearer never reaches a 0644 file. Each unit
// names its own 0600 EnvironmentFile= by path, and the render never reads a secret. The
// connection list Open WebUI registers the units from is a third 0600 file, because it
// carries both bearers. The files are produced by RenderToolEnvFiles and written by
// WriteToolEnvFile, never by the Unit render.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
)

const (
	toolsWebContainerUnitName    = "villa-tools-web.container"
	toolsRecallContainerUnitName = "villa-tools-recall.container"

	// ToolsKeyEnv is the environment variable `villa tools-serve` reads its bearer from.
	// The command tier reads it from here, so the env file and the reader cannot drift.
	ToolsKeyEnv = "VILLA_TOOLS_KEY"

	// toolsEnvDirName is the directory under $XDG_CONFIG_HOME/villa that holds the three
	// 0600 files, and toolsEnvDirPath is its Quadlet spelling (%h is resolved at unit load).
	toolsEnvDirName = "tools"
	toolsEnvDirPath = "%h/.config/villa/" + toolsEnvDirName + "/"

	toolsWebEnvFileName       = "web.env"
	toolsRecallEnvFileName    = "recall.env"
	toolsOpenWebUIEnvFileName = "openwebui.env"

	// ToolsScopeMountDir is where the recall unit sees the scope directory. It is the
	// directory the unit mounts, never the file: an atomic rename replaces the inode a
	// file mount would hold. `villa tools-serve --scope-file` defaults to a file in it.
	ToolsScopeMountDir = "/villa/recall-scope"

	// toolsConnectionsKey is the Open WebUI environment key the connection list is read
	// from. It lives in the 0600 file only.
	toolsConnectionsKey = "TOOL_SERVER_CONNECTIONS"
)

// ToolsWebContainerUnitName and ToolsRecallContainerUnitName return the two unit
// filenames, exported so the install flow can assert each is in the written plan before
// it starts the service.
func ToolsWebContainerUnitName() string    { return toolsWebContainerUnitName }
func ToolsRecallContainerUnitName() string { return toolsRecallContainerUnitName }

// toolsEmbeddingModelPattern is the shape of an embedding model id that may be rendered
// as one argv token of the recall unit's Exec. The id is a config value an operator can
// hand-edit, and an Exec line splits on whitespace.
var toolsEmbeddingModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// toolsView is the data tools.container.tmpl renders. A tool unit has no published
// port, one network, one EnvironmentFile= and the read-only binary mount, plus the
// scope mount on the recall unit alone.
type toolsView struct {
	UnitFileName  string
	Description   string
	AfterService  string
	ContainerName string
	Image         string
	Network       string
	SecretEnvFile string
	BinaryMount   string
	// ScopeMount is empty on the web unit, so the template renders no second Volume=.
	ScopeMount string
	Exec       string
}

// buildToolsWebView assembles villa-tools-web: the routed network, because a search that
// returns page text must fetch.
func buildToolsWebView(image, hostVillaPath string) toolsView {
	return toolsView{
		UnitFileName:  toolsWebContainerUnitName,
		Description:   "VillaStraylight tool server, web search (OpenAPI, read-only)",
		AfterService:  "villa-network.service",
		ContainerName: config.ToolsWebAddr,
		Image:         image,
		Network:       networkAttach,
		SecretEnvFile: toolsEnvDirPath + toolsWebEnvFileName,
		BinaryMount:   hostVillaPath + ":" + websafeBinaryPath + ":ro,z",
		Exec:          buildToolsExec(toolserver.SetWeb, config.ToolsWebPort, ""),
	}
}

// buildToolsRecallView assembles villa-tools-recall: the closed network and nothing
// else. It is refused for an embedding model id that would split the Exec.
func buildToolsRecallView(image, hostVillaPath, scopeDir, embeddingModel string) (toolsView, error) {
	if !toolsEmbeddingModelPattern.MatchString(embeddingModel) {
		return toolsView{}, fmt.Errorf("orchestrate: embedding model %q cannot be rendered as an argument of %s; "+
			"it must match %s", embeddingModel, toolsRecallContainerUnitName, toolsEmbeddingModelPattern)
	}
	return toolsView{
		UnitFileName:  toolsRecallContainerUnitName,
		Description:   "VillaStraylight tool server, recall search (OpenAPI, read-only)",
		AfterService:  "villa-closed-network.service",
		ContainerName: config.ToolsRecallAddr,
		Image:         image,
		Network:       closedNetworkAttach,
		SecretEnvFile: toolsEnvDirPath + toolsRecallEnvFileName,
		BinaryMount:   hostVillaPath + ":" + websafeBinaryPath + ":ro,z",
		ScopeMount:    scopeDir + ":" + ToolsScopeMountDir + ":ro,z",
		Exec:          buildToolsExec(toolserver.SetRecall, config.ToolsRecallPort, embeddingModel),
	}, nil
}

// buildToolsExec assembles the tools-serve Exec from fixed tokens joined on one space (no
// shell interpolation). --host 0.0.0.0 is container-internal only: the unit publishes no
// port. The scope file is left at the flag's default, which sits in ToolsScopeMountDir.
func buildToolsExec(set toolserver.Set, port int, embeddingModel string) string {
	tokens := []string{
		websafeBinaryPath,
		"tools-serve",
		"--set", string(set),
		"--host", "0.0.0.0",
		"--port", strconv.Itoa(port),
	}
	if embeddingModel != "" {
		tokens = append(tokens, "--embedding-model", embeddingModel)
	}
	return strings.Join(tokens, " ")
}

// withToolServer adds the tool server to the chat unit's view: one EnvironmentFile= for the
// connection list, and ENABLE_PERSISTENT_CONFIG=False if no other group already carries it.
// The connection list is a DB-backed ConfigVar, so without that key Open WebUI would seed
// it on first boot and ignore the environment after, and the rendered list would no longer
// be authoritative (ADR-0042, question 8). A tool unit needs web search or memory, which
// already render the key, so the append is normally a no-op.
func withToolServer(v openWebUIView, cfg config.VillaConfig) openWebUIView {
	if !subsystem.ToolWebOn(cfg) && !subsystem.ToolRecallOn(cfg) {
		return v
	}
	v.ToolsEnvFile = toolsEnvDirPath + toolsOpenWebUIEnvFileName
	for _, e := range v.Env {
		if e.Key == "ENABLE_PERSISTENT_CONFIG" {
			return v
		}
	}
	v.Env = append(v.Env, envPair{Key: "ENABLE_PERSISTENT_CONFIG", Value: "False"})
	return v
}

// ToolEnvFile is one 0600 file under the tools config dir: its bare name and its body.
type ToolEnvFile struct {
	Name string
	Text string
}

// toolConnection is one entry of Open WebUI's TOOL_SERVER_CONNECTIONS. Field order is the
// order the ADR shows, and json.Marshal keeps it.
type toolConnection struct {
	Type     string            `json:"type"`
	URL      string            `json:"url"`
	SpecType string            `json:"spec_type"`
	Spec     string            `json:"spec"`
	Path     string            `json:"path"`
	AuthType string            `json:"auth_type"`
	Key      string            `json:"key"`
	Headers  map[string]string `json:"headers,omitempty"`
	Config   toolConnectionCfg `json:"config"`
	Info     toolConnectionInf `json:"info"`
}

// toolConnectionCfg carries no access_grants: a connection with none is private to Open
// WebUI admins, which is every account on a villa box today.
type toolConnectionCfg struct {
	Enable bool `json:"enable"`
}

type toolConnectionInf struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// userIDPlaceholder is the token Open WebUI replaces with the authenticated user's id when
// it expands a connection's headers. It must reach the container verbatim, which is why the
// list is built with encoding/json and never through text/template.
const userIDPlaceholder = "{{USER_ID}}"

// toolConnectionsFor lists one connection per tool unit cfg renders, web first.
func toolConnectionsFor(cfg config.VillaConfig) []toolConnection {
	var out []toolConnection
	if subsystem.ToolWebOn(cfg) {
		out = append(out, toolConnection{
			Type: "openapi", URL: fmt.Sprintf("http://%s:%d", config.ToolsWebAddr, config.ToolsWebPort),
			SpecType: "url", Path: "openapi.json", AuthType: "bearer", Key: cfg.ToolWebSecret,
			Config: toolConnectionCfg{Enable: true},
			Info:   toolConnectionInf{ID: toolserver.ConnectionID(toolserver.SetWeb), Name: "villa web search", Description: "Web search through villa's web guard"},
		})
	}
	if subsystem.ToolRecallOn(cfg) {
		out = append(out, toolConnection{
			Type: "openapi", URL: fmt.Sprintf("http://%s:%d", config.ToolsRecallAddr, config.ToolsRecallPort),
			SpecType: "url", Path: "openapi.json", AuthType: "bearer", Key: cfg.ToolRecallSecret,
			Headers: map[string]string{toolserver.IdentityHeader: userIDPlaceholder},
			Config:  toolConnectionCfg{Enable: true},
			Info:    toolConnectionInf{ID: toolserver.ConnectionID(toolserver.SetRecall), Name: "villa recall", Description: "Search of your own past conversations"},
		})
	}
	return out
}

// RenderToolEnvFiles renders the 0600 files the tool units and the chat unit read: one
// key file per rendered unit and the connection list for Open WebUI. It is the single
// source of their format. It refuses a rendered unit whose bearer is empty, because an
// env file with no key would start a server that the core then refuses, or register a
// connection with no credential. A gate that renders no unit returns no files.
func RenderToolEnvFiles(cfg config.VillaConfig) ([]ToolEnvFile, error) {
	var files []ToolEnvFile
	if subsystem.ToolWebOn(cfg) {
		if cfg.ToolWebSecret == "" {
			return nil, fmt.Errorf("orchestrate: %s needs its bearer but tool_web_secret is empty", toolsWebContainerUnitName)
		}
		files = append(files, ToolEnvFile{Name: toolsWebEnvFileName, Text: ToolsKeyEnv + "=" + cfg.ToolWebSecret + "\n"})
	}
	if subsystem.ToolRecallOn(cfg) {
		if cfg.ToolRecallSecret == "" {
			return nil, fmt.Errorf("orchestrate: %s needs its bearer but tool_recall_secret is empty", toolsRecallContainerUnitName)
		}
		files = append(files, ToolEnvFile{Name: toolsRecallEnvFileName, Text: ToolsKeyEnv + "=" + cfg.ToolRecallSecret + "\n"})
	}
	conns := toolConnectionsFor(cfg)
	if len(conns) == 0 {
		return files, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(conns); err != nil {
		return nil, fmt.Errorf("orchestrate: marshal tool server connections: %w", err)
	}
	// Encode ends the value with the one newline the env file needs, and compact JSON
	// holds no other, so the value is one line.
	files = append(files, ToolEnvFile{Name: toolsOpenWebUIEnvFileName, Text: toolsConnectionsKey + "=" + buf.String()})
	return files, nil
}

// toolsEnvDir resolves the villa-owned tools config dir, $XDG_CONFIG_HOME/villa/tools: the
// host side of toolsEnvDirPath.
func toolsEnvDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("orchestrate: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "villa", toolsEnvDirName), nil
}

// WriteToolEnvFile writes one rendered file atomically, traversal-guarded and 0600 into
// the live tools config dir (directory 0700). Idempotent: the same bytes again are a
// content swap, never a rotation.
func WriteToolEnvFile(name, text string) error {
	dir, err := toolsEnvDir()
	if err != nil {
		return err
	}
	return WriteToolEnvFileTo(dir, name, text)
}

// WriteToolEnvFileTo is the explicit-dir seam behind WriteToolEnvFile. The body is never
// logged and never wrapped into the error.
func WriteToolEnvFileTo(dir, name, text string) error {
	if err := writeSearxngFile(dir, name, text); err != nil {
		return fmt.Errorf("orchestrate: write tool env %q: %w", name, err)
	}
	return nil
}
