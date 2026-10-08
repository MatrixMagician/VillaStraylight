// Package voice is the pure core of the voice subsystem (ADR-0029): the network
// identity of villa-stt and villa-tts, the memory each reserves, and the verdict of
// the spoken round trip that proves them together.
//
// It is a leaf. orchestrate renders the units from it, recommend reserves from it,
// and the command tier drives the proof's two curl legs into it, so it imports none
// of those and does no I/O. Every URL villa composes for a voice unit comes from a
// Service method, so the base URL Open WebUI is given, the route the proof posts to
// and the path status probes cannot name different hosts or ports.
package voice

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// Service is one voice unit's network identity: its container-DNS host and port
// from internal/config, the OpenAI route it serves and the path a health probe reads.
type Service struct {
	// Name is the reservation row's name and the unit's short name.
	Name       string
	Host       string
	Port       int
	Route      string
	HealthPath string
}

// STT is villa-stt. whisper-server answers its health probe at the root.
var STT = Service{Name: "stt", Host: config.SttAddr, Port: config.SttPort, Route: "/v1/audio/transcriptions", HealthPath: "/"}

// TTS is villa-tts. Kokoro-FastAPI serves a dedicated health route.
var TTS = Service{Name: "tts", Host: config.TtsAddr, Port: config.TtsPort, Route: "/v1/audio/speech", HealthPath: "/health"}

func (s Service) base() string { return fmt.Sprintf("http://%s:%d", s.Host, s.Port) }

// RouteURL is the OpenAI audio route the proof posts to.
func (s Service) RouteURL() string { return s.base() + s.Route }

// HealthURL is what status probes.
func (s Service) HealthURL() string { return s.base() + s.HealthPath }

// OpenAIBase is the base URL Open WebUI's AUDIO_*_OPENAI_API_BASE_URL takes; Open
// WebUI appends the audio route itself.
func (s Service) OpenAIBase() string { return s.base() + "/v1" }
