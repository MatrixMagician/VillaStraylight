//go:build ignore

// gen_villa_bytes.go writes the two values the recall-identity capture must take from villa
// rather than retype: the recall unit's OpenAPI document (toolserver.OpenAPI) and Open WebUI's
// TOOL_SERVER_CONNECTIONS line (orchestrate.RenderToolEnvFiles) for a config with only recall
// on. The build tag keeps it out of ./... ; run it with `go run` from the repo root.
//
//	go run ./docs/prototype/recall-identity/gen_villa_bytes.go <out-dir> <bearer>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen_villa_bytes <out-dir> <bearer>")
		os.Exit(2)
	}
	out, key := os.Args[1], os.Args[2]

	spec, err := toolserver.OpenAPI(toolserver.SetRecall)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(out, "recall-openapi.json"), spec, 0o644); err != nil {
		fail(err)
	}

	cfg := config.VillaConfig{ToolServer: true, MemoryEnabled: true, ToolRecallSecret: key}
	files, err := orchestrate.RenderToolEnvFiles(cfg)
	if err != nil {
		fail(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Text, "TOOL_SERVER_CONNECTIONS=") {
			v := strings.TrimSuffix(strings.TrimPrefix(f.Text, "TOOL_SERVER_CONNECTIONS="), "\n")
			if err := os.WriteFile(filepath.Join(out, "connections.villa.json"), []byte(v), 0o600); err != nil {
				fail(err)
			}
			fmt.Printf("connection host %s:%d\n", config.ToolsRecallAddr, config.ToolsRecallPort)
			return
		}
	}
	fail(fmt.Errorf("RenderToolEnvFiles rendered no TOOL_SERVER_CONNECTIONS line"))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
