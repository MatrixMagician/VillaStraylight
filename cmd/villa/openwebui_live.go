// openwebui_live.go binds the Open WebUI protocol module to the real host: one
// transport, built once, over the loopback PublishPort the chat UI already serves.
//
// It replaces twelve named seams the recall command used to inject, each a
// one-to-one rename of a function defined beside it, plus three different strategies
// for shelling out to curl across the memory verify, memory install and status
// paths. There is now one way this control plane talks to the chat UI.
//
// The honesty invariants of the original seam are properties of this adapter:
// every request is a fixed-arg exec.CommandContext curl with no shell, ever; JSON
// bodies are marshaled, never interpolated; and upload content travels via stdin
// multipart, never argv and never a temp file.
//
// No image or backend literal lives here — loopback URLs and endpoint paths are not
// gate-relevant, so TestSeamGrepGate stays green.
package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
)

// owuiServiceAccountEmail / owuiServiceAccountPassword / owuiServiceAccountName are
// the ONE identity villa signs in as. The JWT it mints is held in memory only and is
// never persisted.
//
// The recall indexer deterministically excludes this account from the chat universe:
// all remaining users on this single-operator box are the operator.
const (
	owuiServiceAccountEmail    = "villa-verify@localhost"
	owuiServiceAccountPassword = "villa-verify-memory"
	owuiServiceAccountName     = "villa-verify"
)

// liveOpenWebUITransport builds the production transport against a loopback base
// URL. Every request is a fixed-arg curl invocation; nothing here builds a command
// string.
func liveOpenWebUITransport(base string) openwebui.Transport {
	return func(ctx context.Context, req openwebui.Request) ([]byte, error) {
		args := []string{"-sf"}
		if req.Method != "" && req.Method != "GET" {
			args = append(args, "-X", req.Method)
		}
		args = append(args, base+req.Path)
		if req.TimeoutSeconds > 0 {
			args = append(args, "--max-time", strconv.Itoa(req.TimeoutSeconds))
		}
		if req.Token != "" {
			args = append(args, "-H", "Authorization: Bearer "+req.Token)
		}

		stdin := ""
		switch {
		case req.Upload != nil:
			// Content on STDIN via `-F file=@-`: a transcript can be hundreds of KiB
			// and must never reach argv or a temp file.
			stdin = req.Upload.Content
			args = append(args, "-F", "file=@-;filename="+req.Upload.Filename+";type=text/plain")
		case req.Body != nil:
			args = append(args, "-H", "Content-Type: application/json", "-d", string(req.Body))
		}

		return runLoopbackCurlStdin(ctx, stdin, args...)
	}
}

// liveOpenWebUIClient builds a protocol client against the loopback chat port.
func liveOpenWebUIClient(base string) *openwebui.Client {
	return openwebui.New(liveOpenWebUITransport(base))
}

// owuiLoopbackBase composes the loopback base URL for the chat UI on the given port.
// It is the ONE place that shape is written.
func owuiLoopbackBase(port int) string {
	return fmt.Sprintf("http://%s:%d", verifyMemoryLoopbackAddr, port)
}

// liveOpenWebUISignIn mints the admin JWT for the villa service account.
func liveOpenWebUISignIn(ctx context.Context, c *openwebui.Client) (string, error) {
	return c.SignIn(ctx, owuiServiceAccountEmail, owuiServiceAccountPassword, owuiServiceAccountName)
}
