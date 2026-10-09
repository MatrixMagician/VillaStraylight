package inference

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// whisper_offload.go is ADR-0001's two-signal offload assert for whisper-server
// (#332, ADR-0037). whisper.cpp prints one buffer line per weight buffer it loads,
// the llama.cpp load_tensors shape under its own prefix, so the placement half is
// scrapeBufferResidency with whisper's grammar. On a lost device the measured
// journal reads `ggml_vulkan: No devices found.` and then
//
//	whisper_model_load:          CPU total size =  1623.92 MB
//
// A software renderer enumerates as Vulkan0 too, so the ggml_vulkan device line
// is read through startLogDeviceName, the scrape the image proof shares.

// whisperModelLoadLine is whisper.cpp's buffer line at the pinned digest.
var whisperModelLoadLine = bufferLine{prefix: "whisper_model_load:", phrase: "total size", unit: "MB"}

// WhisperOffloadVerdict folds a whisper-server run into the offload Verdict. It is
// the residency.Deps.Fold for the voice proof and takes the same input as
// RunningOffloadVerdict, so residency.Prove is reused without change.
//
//   - a ggml_vulkan device line naming a software renderer → FAIL;
//   - otherwise scrapeBufferResidency over whisper_model_load: a device buffer
//     over zero → PASS, a CPU buffer or a zero device buffer → FAIL, no line or
//     no journal → WARN.
//
// The GTT floor and the busy fold are foldFloors, shared with the chat verdict.
// Props and ConfigContext are ignored: whisper-server has no /props.
func WhisperOffloadVerdict(in RunningOffloadInput) Verdict {
	placement, ok := softwareRenderer(in.JournalText, in.Markers)
	if !ok {
		placement = scrapeBufferResidency(in.JournalText, in.Markers, whisperModelLoadLine)
	}
	return foldFloors(placement, in, "journald whisper_model_load "+in.Markers.DeviceToken+" placement + point-in-time mem_info_gtt_used floor")
}

// softwareRenderer is the FAIL for a journal whose ggml_vulkan device line names a
// software renderer, read through startLogDeviceName. ok is false when no device
// line does, or when the markers do not reject one. The image and whisper scrapes
// share it.
func softwareRenderer(journal string, m ResidencyMarkers) (OffloadResult, bool) {
	if !m.RejectSoftwareRenderer {
		return OffloadResult{}, false
	}
	sc := bufio.NewScanner(strings.NewReader(journal))
	for sc.Scan() {
		if name, ok := startLogDeviceName(sc.Text(), m); ok && detect.IsSoftwareRendererName(name) {
			return OffloadResult{
				Status: StatusFail,
				Signal: detect.KnownBool(false, "vulkan device line"),
				Detail: fmt.Sprintf("software renderer %q enumerated, not a real GPU", name),
				Raw:    name,
			}, true
		}
	}
	return OffloadResult{}, false
}
