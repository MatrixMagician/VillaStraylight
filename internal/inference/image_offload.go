package inference

import (
	"bufio"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// image_offload.go is ADR-0001's two-signal offload assert for stable-diffusion.cpp.
// sd-server prints no llama.cpp load_tensors line. Its placement line, printed
// once at load (before `listening on` under --eager-load), is
//
//	total params memory size = 8808.62MB (VRAM 8808.62MB, RAM 0.00MB): text_encoders 2375.91MB(VRAM), diffusion_model 6272.71MB(VRAM), vae 160.00MB(VRAM), …
//
// The grammar is an sd.cpp log literal pinned to the vetted image, like the
// llama.cpp markers, so it lives here behind the seam. The two figures inside the
// parentheses are read separately: the line always contains both tokens, so a
// substring match on "VRAM" would read a fully RAM-placed load as a device buffer.

// The placement line's grammar at the pinned digest.
const (
	sdParamsPhrase = "total params memory size"
	sdVRAMToken    = "VRAM"
	sdRAMToken     = "RAM"
)

// imageWitnessTolerance is how far the running server's params figure may differ
// from the image table's weight_bytes before the ADR-0007 witness overlay WARNs.
const imageWitnessTolerance = 0.05

// ImageOffloadVerdict folds an sd-server run into the offload Verdict. It is the
// residency.Deps.Fold for the image proof and takes the SAME input as
// RunningOffloadVerdict, so residency.Prove is reused without change.
//
// Placement signal (journal):
//   - empty or unreadable journal → WARN;
//   - FaultString present → FAIL;
//   - a StartLogDevicePrefix line naming a software renderer → FAIL;
//   - VRAM > 0 and RAM == 0 → PASS;
//   - RAM > 0 → FAIL, a partial CPU fallback;
//   - VRAM == 0 → FAIL;
//   - no placement line → WARN.
//
// The GTT floor and the busy fold are foldFloors, shared with the chat verdict.
// Witness overlay (ADR-0007): a PASS whose params figure differs from WeightBytes
// by more than imageWitnessTolerance becomes a WARN naming both numbers; it never
// rescues a FAIL. Props and ConfigContext are ignored: sd-server has no /props.
func ImageOffloadVerdict(in RunningOffloadInput) Verdict {
	placement, vramBytes := scrapeImagePlacement(in.JournalText, in.Markers)
	v := foldFloors(placement, in, "journald sd-server params placement + point-in-time mem_info_gtt_used floor")

	if v.Status == StatusPass && in.WeightBytes > 0 && vramBytes > 0 {
		diff := math.Abs(float64(vramBytes) - float64(in.WeightBytes))
		if diff > imageWitnessTolerance*float64(in.WeightBytes) {
			v.Status = StatusWarn
			v.Detail = fmt.Sprintf("%s — witness: the server holds %s of params on the device but images.json says %s for this entry",
				v.Detail, mib(vramBytes), mib(in.WeightBytes))
			v.Remediation = "the image table's weight_bytes disagrees with the running server — re-measure the entry and correct images.json, never the running server"
		}
	}
	return v
}

// scrapeImagePlacement is the placement half. It returns the parsed VRAM bytes so
// the witness overlay reads the same parse the verdict did.
func scrapeImagePlacement(journal string, m ResidencyMarkers) (OffloadResult, uint64) {
	if strings.TrimSpace(journal) == "" {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("journal empty/unreadable (could not evaluate placement)", ""),
			Detail: "params placement line not found (journal empty)",
		}, 0
	}
	if m.FaultString != "" && strings.Contains(journal, m.FaultString) {
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "journal "+m.FaultString),
			Detail: fmt.Sprintf("%q found in the journal — GPU fault voids residency", m.FaultString),
			Raw:    m.FaultString,
		}, 0
	}

	var (
		sawLine        bool
		vram, ram      uint64
		softwareDevice string
	)
	sc := bufio.NewScanner(strings.NewReader(journal))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, ok := startLogDeviceName(line, m); ok && m.RejectSoftwareRenderer && detect.IsSoftwareRendererName(name) {
			softwareDevice = name
		}
		if !strings.Contains(line, sdParamsPhrase) {
			continue
		}
		if v, r, ok := parsePlacementLine(line); ok {
			sawLine, vram, ram = true, v, r
		}
	}

	switch {
	case softwareDevice != "":
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "vulkan device line"),
			Detail: fmt.Sprintf("software renderer %q enumerated, not a real GPU", softwareDevice),
			Raw:    softwareDevice,
		}, 0
	case !sawLine:
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("no params placement line found in journal", ""),
			Detail: "residency could not be confirmed from the journal (no params placement line; without --eager-load the line is a plan, not a fact, which is why the unit renders eager load)",
		}, 0
	case ram > 0:
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "params placement RAM figure"),
			Detail: fmt.Sprintf("%s of params in system RAM (%s on VRAM) — a partial CPU fallback", mib(ram), mib(vram)),
		}, vram
	case vram == 0:
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "params placement VRAM figure"),
			Detail: "0 B of params on the device — no weights resident on the iGPU",
		}, 0
	default:
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "params placement line"),
			Detail: fmt.Sprintf("params %s on VRAM, 0 B in RAM", mib(vram)),
		}, vram
	}
}

// parsePlacementLine reads the VRAM and RAM figures out of the parenthesised
// segment after the phrase: "(VRAM 8808.62MB, RAM 0.00MB)". The unit may be
// attached (the measured spelling) or spaced, and MB/MiB/GB/GiB are accepted.
func parsePlacementLine(line string) (vram, ram uint64, ok bool) {
	rest := line[strings.Index(line, sdParamsPhrase)+len(sdParamsPhrase):]
	open := strings.Index(rest, "(")
	if open < 0 {
		return 0, 0, false
	}
	closeIdx := strings.Index(rest[open:], ")")
	if closeIdx < 0 {
		return 0, 0, false
	}
	var sawVRAM, sawRAM bool
	for _, part := range strings.Split(rest[open+1:open+closeIdx], ",") {
		key, size, found := strings.Cut(strings.TrimSpace(part), " ")
		if !found {
			continue
		}
		bytes, perr := parseSize(strings.TrimSpace(size))
		if perr != nil {
			continue
		}
		switch key {
		case sdVRAMToken:
			vram, sawVRAM = bytes, true
		case sdRAMToken:
			ram, sawRAM = bytes, true
		}
	}
	return vram, ram, sawVRAM && sawRAM
}

// parseSize parses "8808.62MB", "8808.62 MiB", "0 MB" or "8.6 GiB" into bytes.
// sd.cpp's MB is 2^20 bytes: the measured 8808.62MB line matched a 9.19 GB GTT delta.
func parseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, err
	}
	var scale float64
	switch strings.ToUpper(strings.TrimSpace(s[i:])) {
	case "B", "":
		scale = 1
	case "KB", "KIB":
		scale = 1 << 10
	case "MB", "MIB":
		scale = 1 << 20
	case "GB", "GIB":
		scale = 1 << 30
	default:
		return 0, fmt.Errorf("unknown size unit in %q", s)
	}
	return uint64(v * scale), nil
}

// mib renders bytes in MiB with two decimals, the placement line's own spelling.
func mib(b uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(b)/(1<<20))
}
