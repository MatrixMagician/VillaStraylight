package voice

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestServiceURLsAreComposedFromOneIdentity: the base URL Open WebUI is given, the
// route the proof posts to and the path status probes all come from one Service, so
// they cannot name different hosts or ports.
func TestServiceURLsAreComposedFromOneIdentity(t *testing.T) {
	cases := []struct {
		svc                      Service
		route, health, openAIURL string
	}{
		{STT, "http://villa-stt:8081/v1/audio/transcriptions", "http://villa-stt:8081/", "http://villa-stt:8081/v1"},
		{TTS, "http://villa-tts:8880/v1/audio/speech", "http://villa-tts:8880/health", "http://villa-tts:8880/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.svc.Name, func(t *testing.T) {
			if got := tc.svc.RouteURL(); got != tc.route {
				t.Errorf("RouteURL = %q, want %q", got, tc.route)
			}
			if got := tc.svc.HealthURL(); got != tc.health {
				t.Errorf("HealthURL = %q, want %q", got, tc.health)
			}
			if got := tc.svc.OpenAIBase(); got != tc.openAIURL {
				t.Errorf("OpenAIBase = %q, want %q", got, tc.openAIURL)
			}
		})
	}
}

// TestFootprintsAreTheMeasuredReservations: the two reservation rows recommend
// subtracts before the chat-model fit, sized from the ADR-0030 measurements and
// rounded up. A smaller number would let the chat model claim memory the voice
// units hold.
func TestFootprintsAreTheMeasuredReservations(t *testing.T) {
	if got := STTFootprintBytes(); got != 2684354560 {
		t.Errorf("STTFootprintBytes = %d, want 2684354560 (2.5 GiB)", got)
	}
	if got := TTSFootprintBytes(); got != 2147483648 {
		t.Errorf("TTSFootprintBytes = %d, want 2147483648 (2 GiB)", got)
	}
}

// TestThePackageStaysAPureLeaf: voice is imported by orchestrate, recommend and the
// command tier, so it may import none of them, and it does no I/O of its own. The
// live curl legs are the command tier's. The residency protocol the proof drives
// (#332) takes every host read as a seam, so importing it adds no I/O.
func TestThePackageStaysAPureLeaf(t *testing.T) {
	allowed := []string{
		"context",
		"errors",
		"fmt",
		"strings",
		"time",
		"unicode",
		"github.com/MatrixMagician/VillaStraylight/internal/config",
		"github.com/MatrixMagician/VillaStraylight/internal/detect",
		"github.com/MatrixMagician/VillaStraylight/internal/inference",
		"github.com/MatrixMagician/VillaStraylight/internal/residency",
		"github.com/MatrixMagician/VillaStraylight/internal/verify",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, file, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !slices.Contains(allowed, p) {
				t.Errorf("%s imports %q; internal/voice imports only config, verify, the residency protocol and pure stdlib", file, p)
			}
		}
	}
}
