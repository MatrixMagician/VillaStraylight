package inference

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// running_offload.go answers the SAME silent-CPU-fallback question as offload.go
// (D-09) but for an ALREADY-RUNNING server, where the Phase-2 before/after GTT
// delta is impossible (the server is up; there is no "before"). It reuses the
// inference.Verdict (PASS/WARN/FAIL + typed-Unknown) vocabulary and combineOffload
// verbatim (D-12) — it does NOT re-roll the offload math.
//
// The two carried-in Phase-2 hardening findings are closed here:
//
//   - WR-05: auto-fit llama.cpp builds emit no "offloaded N/N" line, so a device
//     line proves only ENUMERATION, not per-layer RESIDENCY. The load-bearing
//     residency proof is instead the higher-verbosity journald line
//     "load_tensors: Vulkan0 model buffer size = N MiB" — a non-zero Vulkan0 model
//     buffer means real weight bytes are resident on the Vulkan device. llama.cpp
//     /props is explicitly NOT the placement proof (Pitfall 1): it is folded in
//     only as a config-identity (drift) corroboration overlay.
//   - CR-03: mem_info_gtt_used is a host-wide counter, so a fragile before/after
//     delta is unreliable on a long-running host. Instead a POINT-IN-TIME floor —
//     used ≥ the model's weight footprint — corroborates residency.
//
// This file is PURE: it accepts the journal text, the parsed /props, and the
// already-read GTT bytes as inputs and returns a Verdict. All journald/HTTP/sysfs
// I/O lives in the cmd layer (cmd/villa/status.go), exactly as offload.go keeps the
// stderr capture in the Runner — so this stays table-testable with fixtures.

// RunningOffloadInput is the pure input to RunningOffloadVerdict: the recovered
// journal text (residency), the parsed /props (config-identity corroboration), the
// point-in-time GTT-used reading (CR-03 floor), and the model's expected weight
// footprint plus the configured model/context for the drift overlay.
type RunningOffloadInput struct {
	// JournalText is the bounded user-journal text of the inference service, scanned
	// for the load_tensors Vulkan0 residency line. Empty/unreadable → typed-Unknown.
	JournalText string
	// Props is the parsed llama.cpp /props response (model_path + n_ctx) for the
	// config-drift overlay. nil means /props was unavailable (Unknown → never a
	// false PASS, never a FAIL — it is corroboration only).
	Props *PropsInfo
	// GTTUsedBytes is the point-in-time mem_info_gtt_used reading (CR-03 floor),
	// already read by the cmd layer through detect.GTTUsedBytes.
	GTTUsedBytes detect.Bytes
	// WeightBytes is the loaded model's expected on-disk weight footprint (from the
	// recommend fit terms), the reference the GTT floor compares against.
	WeightBytes uint64
	// ConfigModel / ConfigContext are the configured model path + context the /props
	// response is checked against for drift. Empty ConfigModel disables the model
	// drift check; zero ConfigContext disables the ctx drift check.
	ConfigModel   string
	ConfigContext int

	// Markers is the backend-owned residency descriptor (D-04/D-05). The running
	// scrape keys its device-token match and fault scan on it instead of hardcoded
	// "Vulkan0" literals, so a ROCm backend slots in (Plan 02) without re-rolling the
	// offload math. The cmd layer sets it from BackendFor(cfg.Backend).ResidencyProof().
	Markers ResidencyMarkers
	// GPUBusyPercent is the point-in-time sysfs gpu_busy_percent reading (D-06), read
	// by the cmd layer via detect.GPUBusyPercent. It is folded through combineOffload
	// as a residency CORROBORATOR: Known non-zero corroborates a PASS, Known-zero on a
	// claimed-healthy decode FAILs (silent CPU fallback), absent/Unknown is
	// combine-neutral (the fold is SKIPPED — Vulkan supplies no busy signal so its
	// verdict stays byte-identical). The live decode-time read lands in Phase 8 (D-07);
	// Phase 6 wires the input + verdict logic, fixture-driven.
	GPUBusyPercent detect.Int
}

// PropsInfo is the subset of llama.cpp /props the running-offload Verdict consults:
// the loaded model path and the active context length. It is config-identity
// corroboration ONLY (Pitfall 1) — never the residency proof.
type PropsInfo struct {
	ModelPath string
	NCtx      int
}

// loadTensors marker fragments — assembled (not a single contiguous literal) so
// they describe the parsed journald shape without being mistaken for a backend
// assumption. These two are backend-NEUTRAL (every llama.cpp backend emits the same
// "load_tensors: <device> model buffer size = N MiB" shape); the device token that
// distinguishes a real GPU buffer from a CPU buffer is backend-owned and supplied via
// ResidencyMarkers.DeviceToken. The residency line looks like:
//
//	load_tensors:      Vulkan0 model buffer size = 21504.49 MiB
const (
	loadTensorsPrefix = "load_tensors:"
	bufferSizePhrase  = "model buffer size"
)

// scrapeLoadTensorsResidency parses the journal for the load_tensors device-buffer
// residency line (WR-05), keyed on the backend-owned ResidencyMarkers (D-05) so the
// scrape is backend-neutral. It transfers the offload.go scrapeOffloadLog
// bufio.Scanner skeleton:
//
//   - a non-empty m.FaultString found anywhere in the journal                → FAIL
//     (an abort VOIDS residency BEFORE the buffer-line switch, D-06). Empty
//     FaultString (Vulkan) makes this a no-op → Vulkan stays byte-identical.
//   - a "load_tensors: ... <DeviceToken> model buffer size = N MiB" with N>0 → PASS
//     (real weight bytes resident on the GPU device)
//   - the same line with N == 0, OR only a CPU buffer line and no DeviceToken → FAIL
//     (the silent-CPU-fallback this exists to catch)
//   - no load_tensors buffer line at all / empty journal                     → WARN
//     (typed-Unknown — could not evaluate; NEVER a false PASS)
func scrapeLoadTensorsResidency(journal string, m ResidencyMarkers) OffloadResult {
	if strings.TrimSpace(journal) == "" {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("journal empty/unreadable (could not evaluate residency)", ""),
			Detail: "load_tensors residency line not found (journal empty)",
		}
	}

	// Fault scan FIRST (D-06): an abort marker voids residency before any buffer-line
	// PASS. Empty FaultString (Vulkan) skips this entirely → byte-identical.
	if m.FaultString != "" && strings.Contains(journal, m.FaultString) {
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "journal "+m.FaultString),
			Detail: fmt.Sprintf("%q found in the journal — GPU fault voids residency", m.FaultString),
			Raw:    m.FaultString,
		}
	}

	var (
		sawDeviceBuffer bool
		deviceMiB       float64
		sawCPUBuffer    bool
	)

	sc := bufio.NewScanner(strings.NewReader(journal))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, loadTensorsPrefix) || !strings.Contains(line, bufferSizePhrase) {
			continue
		}
		mib, ok := parseBufferMiB(line)
		if !ok {
			continue
		}
		// Per Pitfall 2, strings.Contains(line, "ROCm0") does NOT match "ROCm_Host",
		// so the descriptor-driven substring match is the correct direct port.
		if strings.Contains(line, m.DeviceToken) {
			sawDeviceBuffer = true
			deviceMiB = mib
		} else {
			// A non-device buffer line (CPU_Mapped / CPU model buffer size).
			sawCPUBuffer = true
		}
	}

	switch {
	case sawDeviceBuffer && deviceMiB > 0:
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "load_tensors "+m.DeviceToken+" model buffer size"),
			Detail: fmt.Sprintf("%s model buffer %.2f MiB resident on the iGPU", m.DeviceToken, deviceMiB),
		}
	case sawDeviceBuffer && deviceMiB == 0:
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "load_tensors "+m.DeviceToken+" model buffer size"),
			Detail: fmt.Sprintf("%s model buffer size = 0 — no weights resident on the iGPU", m.DeviceToken),
		}
	case sawCPUBuffer:
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "load_tensors CPU buffer only"),
			Detail: "only a CPU model buffer was loaded — server fell back to CPU",
		}
	default:
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("no load_tensors buffer line found in journal", ""),
			Detail: "residency could not be confirmed from the journal (no load_tensors buffer line)",
		}
	}
}

// parseBufferMiB extracts the MiB value from a "... model buffer size = N MiB"
// line. Returns ok=false on a shape it cannot parse.
func parseBufferMiB(line string) (mib float64, ok bool) {
	_, after, found := strings.Cut(line, "=")
	if !found {
		return 0, false
	}
	fields := strings.Fields(after) // e.g. ["21504.49", "MiB"]
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// gttFloor classifies the POINT-IN-TIME mem_info_gtt_used reading against the
// model's weight footprint (CR-03 — a floor, NOT a before/after delta). With
// --no-mmap the weights are resident in unified memory, so a healthy running server
// must show at least the weight footprint in GTT-used.
//
//   - used ≥ weight        → PASS (corroborates residency)
//   - used < weight        → FAIL (weights not resident — silent CPU fallback)
//   - Unknown used          → WARN (unreadable sysfs — could not evaluate)
//   - weight == 0           → WARN (no reference footprint — not a computable floor)
func gttFloor(used detect.Bytes, weight uint64) OffloadResult {
	if !used.Known {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("mem_info_gtt_used unreadable", used.Raw),
			Detail: "point-in-time GTT-used could not be evaluated",
		}
	}
	if weight == 0 {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("model weight unknown (weightBytes==0) — GTT floor not computable", ""),
			Detail: "GTT floor could not be evaluated (no reference weight)",
		}
	}
	if used.Value >= weight {
		return OffloadResult{
			Status:     StatusPass,
			Signal:     detect.KnownBool(true, "mem_info_gtt_used point-in-time floor"),
			Detail:     fmt.Sprintf("GTT-used %d bytes ≥ %d weight footprint (resident)", used.Value, weight),
			DeltaBytes: used.Value,
		}
	}
	return OffloadResult{
		Status:     StatusFail,
		Signal:     detect.KnownBool(false, "mem_info_gtt_used point-in-time floor"),
		Detail:     fmt.Sprintf("GTT-used %d bytes < %d weight footprint — weights not resident on GPU", used.Value, weight),
		DeltaBytes: used.Value,
	}
}

// gpuBusyFloor classifies the point-in-time sysfs gpu_busy_percent reading as a
// residency CORROBORATOR (D-06), mirroring gttFloor's typed-Unknown discipline:
//
//   - busy.Known && busy.Value > 0  → PASS (a real decode is using the GPU)
//   - busy.Known && busy.Value == 0 → FAIL (a claimed-healthy decode at 0% busy is a
//     silent CPU fallback, R1)
//   - !busy.Known                   → PASS-equivalent typed-Unknown (NEVER WARN).
//     combineOffload has NO neutral state — a WARN would downgrade every Vulkan PASS —
//     so the caller SKIPS folding this case entirely; this branch returns a
//     PASS-equivalent result only as a defensive fallback if invoked unconditionally.
//
// It is pure and never panics.
func gpuBusyFloor(busy detect.Int) OffloadResult {
	if !busy.Known {
		// Defensive: the caller does NOT fold this case (see RunningOffloadVerdict).
		// If invoked anyway it must be PASS-equivalent, never WARN (D-07/Q2): an
		// unavailable busy reading must not flip a residency-proven PASS.
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.UnknownBool("gpu_busy_percent unavailable (busy signal absent — neutral)", busy.Raw),
			Detail: "gpu_busy_percent unavailable — not folded (residency-neutral)",
		}
	}
	if busy.Value > 0 {
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "gpu_busy_percent"),
			Detail: fmt.Sprintf("gpu_busy_percent %d%% during decode — corroborates GPU residency", busy.Value),
		}
	}
	return OffloadResult{
		Status: StatusFail,
		Signal: detect.KnownBool(false, "gpu_busy_percent"),
		Detail: "gpu_busy_percent 0% during a claimed-healthy decode — silent CPU fallback",
	}
}

// verdictAsResult collapses an already-combined Verdict back into a single
// OffloadResult so the busy signal can be re-folded through combineOffload (D-06)
// WITHOUT re-rolling the combine math. It preserves the residency+floor Status and
// the load-scrape Signal as the carried typed-Unknown.
func verdictAsResult(v Verdict) OffloadResult {
	return OffloadResult{
		Status:     v.Status,
		Signal:     v.LogOffload,
		Detail:     v.Detail,
		DeltaBytes: v.GTTDeltaBytes,
		Raw:        v.Raw,
	}
}

// RunningOffloadVerdict combines the running-server signals into one Verdict via the
// reused combineOffload discipline (any FAIL→FAIL; else any Unknown→WARN; else
// PASS). The journald residency scrape is the load-bearing "log" signal; the
// point-in-time GTT floor is the "sysfs" signal. The sysfs gpu_busy_percent reading
// is folded as a residency corroborator (D-06) — but only when Known: an
// absent/Unknown busy reading is combine-neutral (the fold is SKIPPED, never a WARN,
// since combineOffload has no neutral state and a WARN would break the byte-identical
// Vulkan guard). The /props response is folded in ONLY as a config-identity drift
// overlay: a confirmed mismatch downgrades a PASS to WARN (Pitfall 1 — /props is
// identity corroboration, never placement proof), while an unavailable /props (nil)
// is left as Unknown and never upgrades or downgrades the residency-proven verdict.
func RunningOffloadVerdict(in RunningOffloadInput) Verdict {
	residency := scrapeLoadTensorsResidency(in.JournalText, in.Markers)
	floor := gttFloor(in.GTTUsedBytes, in.WeightBytes)

	v := combineOffload(residency, floor)

	// D-06 busy-signal fold — CONDITIONAL on a Known reading. When the busy reading is
	// Unknown/absent (e.g. Vulkan, which supplies none), SKIP the fold entirely so the
	// verdict is exactly residency+floor and stays byte-identical. When Known, re-fold
	// the already-combined verdict with the busy signal through combineOffload (reused,
	// not re-rolled): Known non-zero corroborates a PASS, Known-zero FAILs.
	provenance := "journald load_tensors residency + point-in-time mem_info_gtt_used floor"
	if in.GPUBusyPercent.Known {
		v = combineOffload(verdictAsResult(v), gpuBusyFloor(in.GPUBusyPercent))
		provenance += " + gpu_busy_percent corroboration"
	}
	v.Provenance = provenance

	// /props config-identity drift overlay (T-03-15). Only ever downgrades a PASS to
	// WARN on a CONFIRMED mismatch; it is never a residency proof and never a FAIL.
	if drift := propsDrift(in.Props, in.ConfigModel, in.ConfigContext); drift != "" {
		if v.Status == StatusPass {
			v.Status = StatusWarn
			v.Detail = v.Detail + " — /props config drift: " + drift
			v.Remediation = "loaded model/context differs from config.toml — run `villa restart` to apply the configured selection"
		}
	}
	return v
}

// propsDrift reports a config-identity mismatch between the /props response and the
// configured model/context, or "" when there is no detectable drift. nil props
// (unavailable) is Unknown — it reports no drift (never a false downgrade).
func propsDrift(props *PropsInfo, cfgModel string, cfgCtx int) string {
	if props == nil {
		return ""
	}
	var notes []string
	if cfgModel != "" && props.ModelPath != "" && !sameModelPath(props.ModelPath, cfgModel) {
		notes = append(notes, fmt.Sprintf("loaded %q vs configured %q", props.ModelPath, cfgModel))
	}
	if cfgCtx > 0 && props.NCtx > 0 && props.NCtx != cfgCtx {
		notes = append(notes, fmt.Sprintf("loaded ctx %d vs configured %d", props.NCtx, cfgCtx))
	}
	return strings.Join(notes, "; ")
}

// sameModelPath compares two model paths by their basename so a /props absolute
// container path matches a configured path that differs only in directory prefix.
func sameModelPath(a, b string) bool {
	base := func(p string) string {
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			return p[i+1:]
		}
		return p
	}
	return base(a) == base(b)
}
