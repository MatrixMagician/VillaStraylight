package inference

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// This file implements the dual offload assert: two INDEPENDENT signals,
// both required for a PASS, that together catch the silent-CPU-fallback this whole
// phase exists to prevent. Either alone is foolable — a server can log a GPU device
// yet offload nothing, or sysfs can move for unrelated reasons — so a PASS requires
// BOTH the startup-log scrape AND the amdgpu sysfs GTT-used delta.
//
// Every signal degrades to a typed Unknown (StatusWarn) on an unevaluable input
// (unreadable stderr, missing sysfs file), DISTINCT from a confidently-false
// offload (StatusFail). That distinction is the contract: uncertainty must never be
// silently reported as either success or failure.

// OffloadResult is one signal's outcome (log-scrape OR sysfs delta) before the two
// are combined into a Verdict. It carries the typed-Unknown offload boolean and,
// for the sysfs signal, the observed byte delta (recorded in --json for Phase-5
// threshold calibration, A1).
type OffloadResult struct {
	// Status is this signal's verdict (PASS/WARN/FAIL).
	Status Status
	// Signal is the typed-Unknown offload boolean: Known=true,Value=true → proven;
	// Known=true,Value=false → confirmed-absent (FAIL); Known=false → could not
	// evaluate (WARN).
	Signal detect.Bool
	// Detail is a one-line human explanation.
	Detail string
	// DeltaBytes is the observed GTT-used delta (sysfs signal only; 0 otherwise).
	DeltaBytes uint64
	// Raw is the offending raw input captured on a parse miss (never serialized).
	Raw string
}

// Threshold band for the sysfs GTT-used delta vs the model's on-disk weight (A1,
// MEDIUM — calibrate against the live run, observed delta recorded in --json).
// With --no-mmap the weights are resident in unified memory, so the GTT-used delta
// should approach the weight size.
const (
	offloadPassFraction = 0.5 // delta ≥ 0.5×weight → PASS
	offloadFailFraction = 0.1 // delta < 0.1×weight → FAIL; in-between → WARN
)

// scrapeOffloadLog parses captured llama-server stderr for the markers that prove a
// real Vulkan GPU was selected, rejecting software renderers
// (llvmpipe/softpipe/lavapipe/swrast via the reused detect.IsSoftwareRendererName).
// It accepts BOTH llama-server log formats, because the kyuz0 image auto-rebuilds on
// llama.cpp master and the format drifted:
//
//   - OLD: "ggml_vulkan: 0 = AMD … (RADV …) (radv)" + "load_tensors: offloaded N/N
//     layers to GPU". When the offloaded line is present it is the strong signal:
//     N==0 → FAIL.
//   - NEW (auto-fit builds): a "device_info:" block listing "- Vulkan0 : AMD … (RADV
//     GFX1151) (…)" with NO "offloaded N/N" line emitted at the default verbosity.
//
// The log signal's claim is "a real RADV GPU was enumerated and selected (not a
// software renderer)". It is deliberately ONE of the two independent signals:
// the sysfs GTT-used delta (offloadSysfsDelta) is what proves the weights actually
// became resident on that device. Both are required for a PASS, so a real-device log
// + a ~zero sysfs delta still FAILs (GPU present but unused) — coverage the dropped
// per-layer count used to give is preserved by the sysfs signal.
//
//   - real RADV device (either format), offloaded N/N absent or N>0   → PASS
//   - a software-renderer device line                                 → FAIL
//   - offloaded 0/N explicitly                                        → FAIL
//   - no real Vulkan device and no offload evidence / stderr empty    → Unknown (WARN)
//
// When draftExpected is true (ADR-0009), the target verdict above is folded with
// the draft sidecar's own judgment from the log's second model-load block (see
// judgeDraftBlock and foldDraft) — a draft that fell back to the CPU is a residency
// FAIL just as a target CPU fallback is. When false the result is byte-identical to
// the pre-draft behavior.
func scrapeOffloadLog(stderr string, m ResidencyMarkers, draftExpected bool) OffloadResult {
	target := scrapeOffloadLogTarget(stderr, m)
	if !draftExpected {
		return target
	}
	return foldDraft(target, judgeDraftBlock(modelBlocks(stderr), m))
}

// scrapeOffloadLogTarget is the unchanged, pre-draft target-only scrape: the whole
// stderr text scanned for the device/offload markers. It is unaffected by a second
// model-load block, since those markers (ggml_vulkan/device_info/"offloaded N/N")
// never appear in a draft's own block.
func scrapeOffloadLogTarget(stderr string, m ResidencyMarkers) OffloadResult {
	if strings.TrimSpace(stderr) == "" {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("llama-server stderr empty/unreadable (could not evaluate offload)", ""),
			Detail: "offload log markers not found (stderr empty)",
		}
	}

	var (
		sawVulkanDevice  bool
		deviceName       string
		softwareDevice   string
		offloaded, total int
		sawOffloadLine   bool
	)

	noteDevice := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		sawVulkanDevice = true
		deviceName = name
		// Only the Vulkan backend has a software-renderer (llvmpipe) ICD analog; a
		// backend with no such analog (ROCm) sets RejectSoftwareRenderer=false so this
		// check is skipped.
		if m.RejectSoftwareRenderer && detect.IsSoftwareRendererName(name) {
			softwareDevice = name
		}
	}

	sc := bufio.NewScanner(strings.NewReader(stderr))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// OLD device line: "ggml_vulkan: 0 = AMD Radeon … (RADV …) (radv) | …".
		// Name is the segment after "N =" up to the first " | ". The prefix is the
		// backend-owned StartLogDevicePrefix; empty disables this branch.
		if m.StartLogDevicePrefix != "" && strings.HasPrefix(line, m.StartLogDevicePrefix) && strings.Contains(line, "=") {
			if _, after, ok := strings.Cut(line, "="); ok {
				name := after
				if idx := strings.Index(after, "|"); idx >= 0 {
					name = after[:idx]
				}
				noteDevice(name)
			}
		}

		// NEW device_info entry: "- Vulkan0 : AMD Radeon 8060S Graphics (RADV GFX1151)
		// (64550 MiB, …)". The label prefix ("- Vulkan"/"- ROCm") is backend-owned via
		// m.DeviceLabel. Name is the text after the "<device> :" separator (the trailing
		// memory parenthetical is harmless for the software-renderer check and is kept
		// as the device label).
		if m.DeviceLabel != "" {
			if idx := strings.Index(line, m.DeviceLabel); idx >= 0 {
				if c := strings.Index(line[idx:], ":"); c >= 0 {
					noteDevice(line[idx+c+1:])
				}
			}
		}

		// Offload line (old format only): "load_tensors: offloaded N/N layers to GPU".
		if strings.Contains(line, "offloaded") && strings.Contains(line, "layers to GPU") {
			if n, tot, ok := parseOffloadedLayers(line); ok {
				offloaded, total = n, tot
				sawOffloadLine = true
			}
		}
	}

	// A software renderer is a confident FAIL — the silent-CPU-fallback path.
	if softwareDevice != "" {
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "vulkan device line"),
			Detail: fmt.Sprintf("software renderer %q enumerated, not a real GPU", softwareDevice),
			Raw:    softwareDevice,
		}
	}

	// Explicit "offloaded 0/N" → confident FAIL (the old format reported it directly).
	if sawOffloadLine && offloaded == 0 {
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "load_tensors offloaded line"),
			Detail: fmt.Sprintf("offloaded %d/%d layers — GPU offload did not engage", offloaded, total),
		}
	}

	// Partial offload "offloaded N/M" with 0 < N < M → confident FAIL (Pitfall 3:
	// today's scrape only caught offloaded==0, but a partial offload is also a CPU
	// fallback for the un-offloaded layers). This is GATED on sawOffloadLine && total > 0
	// so a Vulkan auto-fit run (no "offloaded N/N" line emitted, so total stays 0) still
	// PASSes — the rule only fires when an offloaded line IS present with N < M.
	if sawOffloadLine && total > 0 && offloaded < total {
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "load_tensors offloaded line"),
			Detail: fmt.Sprintf("offloaded only %d/%d layers — %d layers stayed on the CPU (partial offload)", offloaded, total, total-offloaded),
		}
	}

	// A real RADV device was enumerated and selected → log signal PASS. The sysfs
	// delta is the second, independent residency proof.
	if sawVulkanDevice {
		if sawOffloadLine {
			return OffloadResult{
				Status: StatusPass,
				Signal: detect.KnownBool(true, "vulkan device + load_tensors offloaded line"),
				Detail: fmt.Sprintf("RADV Vulkan device + offloaded %d/%d layers to GPU", offloaded, total),
			}
		}
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "device_info vulkan device"),
			Detail: fmt.Sprintf("real Vulkan GPU enumerated (%s); layer count not reported by this llama.cpp build — sysfs delta confirms residency", strings.TrimSpace(deviceName)),
		}
	}

	// No real Vulkan device and no offload evidence → could not evaluate → WARN.
	return OffloadResult{
		Status: StatusWarn,
		Signal: detect.UnknownBool("no real Vulkan device or 'offloaded N/N' line found in stderr", ""),
		Detail: "offload could not be confirmed from stderr (no Vulkan device line)",
	}
}

// scrapeProjectorLog reads the vision projector's start-time load line. It is
// deliberately NOT one of the two residency signals (ADR-0001 is untouched): the
// projector's device says how fast images encode, not whether the model's weights
// are resident, so its worst outcome here is a WARN.
//
//   - "CLIP using <device token> backend"  → PASS, the projector is on the GPU
//   - "CLIP using CPU backend"             → WARN, a confident but non-fatal finding
//   - no clip_ctx line at all              → WARN Unknown (could not evaluate)
func scrapeProjectorLog(stderr string, m ResidencyMarkers) OffloadResult {
	if m.DeviceToken != "" && strings.Contains(stderr, "CLIP using "+m.DeviceToken+" backend") {
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "clip_ctx backend line"),
			Detail: "projector loaded on " + m.DeviceToken,
		}
	}
	if strings.Contains(stderr, "CLIP using CPU backend") {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.KnownBool(false, "clip_ctx backend line"),
			Detail: "projector loaded on the CPU: image encoding will be slow",
		}
	}
	return OffloadResult{
		Status: StatusWarn,
		Signal: detect.UnknownBool("no clip_ctx line in stderr", ""),
		Detail: "projector load line absent from stderr",
	}
}

// parseOffloadedLayers extracts N and total from a "offloaded N/N layers to GPU"
// line. Returns ok=false on a shape it cannot parse.
func parseOffloadedLayers(line string) (offloaded, total int, ok bool) {
	idx := strings.Index(line, "offloaded")
	if idx < 0 {
		return 0, 0, false
	}
	rest := strings.TrimSpace(line[idx+len("offloaded"):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, 0, false
	}
	frac := fields[0] // "N/N"
	n, t, found := strings.Cut(frac, "/")
	if !found {
		return 0, 0, false
	}
	ni, err1 := strconv.Atoi(strings.TrimSpace(n))
	ti, err2 := strconv.Atoi(strings.TrimSpace(t))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return ni, ti, true
}

// offloadSysfsDelta classifies the GTT-used before/after delta against the model
// weight (A1 band). If either read is a typed Unknown the signal degrades
// to WARN (never FAIL). The observed delta is recorded for Phase-5 calibration.
//
//   - delta ≥ 0.5×weight        → PASS
//   - delta < 0.1×weight        → FAIL (≈zero movement while the server claims healthy)
//   - in-between                → WARN
//   - either read Unknown       → WARN (could not evaluate)
func offloadSysfsDelta(before, after detect.Bytes, weightBytes uint64) OffloadResult {
	if !before.Known || !after.Known {
		reason := "gtt_used before-read unevaluable"
		if before.Known {
			reason = "gtt_used after-read unevaluable"
		}
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool(reason, ""),
			Detail: "sysfs GTT-used delta could not be evaluated",
		}
	}

	// A zero/unknown weight makes the band uncomputable: passFloor and failCeil
	// both collapse to 0, so any delta ≥ 0 would fail-open to PASS — defeating the
	// whole assert. Degrade to a typed Unknown (WARN) instead, exactly as for an
	// unevaluable sysfs read above. Never report uncertainty as success.
	if weightBytes == 0 {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("model weight unknown (weightBytes==0) — sysfs delta band not computable", ""),
			Detail: "sysfs GTT-used delta could not be evaluated (no reference weight)",
		}
	}

	// Guard against a non-monotonic read (after < before): treat as ~zero movement.
	var delta uint64
	if after.Value > before.Value {
		delta = after.Value - before.Value
	}

	passFloor := uint64(offloadPassFraction * float64(weightBytes))
	failCeil := uint64(offloadFailFraction * float64(weightBytes))

	switch {
	case delta >= passFloor:
		return OffloadResult{
			Status:     StatusPass,
			Signal:     detect.KnownBool(true, "mem_info_gtt_used before/after delta"),
			Detail:     fmt.Sprintf("GTT-used grew %d bytes (≥ %.0f%% of %d weight)", delta, offloadPassFraction*100, weightBytes),
			DeltaBytes: delta,
		}
	case delta < failCeil:
		return OffloadResult{
			Status:     StatusFail,
			Signal:     detect.KnownBool(false, "mem_info_gtt_used before/after delta"),
			Detail:     fmt.Sprintf("GTT-used grew only %d bytes (< %.0f%% of %d weight) — weights not resident on GPU", delta, offloadFailFraction*100, weightBytes),
			DeltaBytes: delta,
		}
	default:
		return OffloadResult{
			Status:     StatusWarn,
			Signal:     detect.KnownBool(true, "mem_info_gtt_used before/after delta"),
			Detail:     fmt.Sprintf("GTT-used grew %d bytes (between %.0f%% and %.0f%% of weight) — inconclusive", delta, offloadFailFraction*100, offloadPassFraction*100),
			DeltaBytes: delta,
		}
	}
}

// combineOffload is the dual-assert combiner: a PASS requires BOTH signals
// to PASS. Any FAIL → FAIL (FAIL dominates Unknown). Otherwise any Unknown → WARN.
// The resulting Verdict carries both typed signals and the observed sysfs delta for
// the --json contract.
func combineOffload(log, sysfs OffloadResult) Verdict {
	v := Verdict{
		LogOffload:    log.Signal,
		SysfsOffload:  sysfs.Signal,
		GTTDeltaBytes: sysfs.DeltaBytes,
	}

	switch {
	case log.Status == StatusFail || sysfs.Status == StatusFail:
		v.Status = StatusFail
		v.Detail = joinDetail("offload FAILED", log, sysfs)
		v.Remediation = "GPU offload did not engage — check /dev/dri passthrough, keep-groups, and that the RADV ICD is present (not llvmpipe)"
		v.Raw = firstNonEmpty(log.Raw, sysfs.Raw)
	case log.Status == StatusWarn || sysfs.Status == StatusWarn:
		v.Status = StatusWarn
		v.Detail = joinDetail("offload could not be fully verified", log, sysfs)
		v.Remediation = "re-run with readable stderr and sysfs access to confirm offload"
	default:
		v.Status = StatusPass
		v.Detail = joinDetail("offload proven (log + sysfs)", log, sysfs)
	}
	v.Provenance = "log-scrape + amdgpu mem_info_gtt_used delta"
	return v
}

// joinDetail combines the headline with each signal's detail for -v.
func joinDetail(headline string, log, sysfs OffloadResult) string {
	return fmt.Sprintf("%s — log: %s; sysfs: %s", headline, log.Detail, sysfs.Detail)
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// modelLoaderOpener is the llama.cpp line that opens every model's own
// load_tensors block, the target's first and a draft sidecar's second (ADR-0009).
const modelLoaderOpener = "llama_model_loader: loaded meta data"

// modelBlocks splits a log (start-time stderr or a running journal — the same
// helper serves both scrapes) into one slice of lines per model load, each opened
// by a modelLoaderOpener line: block 0 is the target, block 1 (when present) is
// the draft sidecar, and so on. Lines before the FIRST opener — the backend banner
// and device enumeration scrapeOffloadLogTarget depends on — are folded into block
// 0's prefix rather than ignored, so scoping to a block never drops evidence the
// unscoped target scan already relies on. A log with no opener at all yields no
// blocks.
func modelBlocks(log string) [][]string {
	var blocks [][]string
	var prefix []string
	sc := bufio.NewScanner(strings.NewReader(log))
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, modelLoaderOpener) {
			if len(blocks) == 0 {
				blocks = append(blocks, append(prefix, line))
			} else {
				blocks = append(blocks, []string{line})
			}
			continue
		}
		if len(blocks) == 0 {
			prefix = append(prefix, line)
			continue
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], line)
	}
	return blocks
}

// judgeDraftBlock applies the load_tensors device-buffer-line rule (the running
// scrape's own rule, reused rather than re-rolled) to blocks[1], the draft
// sidecar's model-load block, naming the draft in every Detail so a draft finding
// reads distinctly from a target finding:
//
//   - no blocks[1] (draft expected but never loaded)              → Unknown (WARN)
//   - a DeviceToken buffer line with N>0 (CPU buffer or not)       → PASS
//   - only a CPU/host buffer line, no DeviceToken line             → FAIL
//
// The DeviceToken match is strings.Contains, so "ROCm0" does not match
// "ROCm_Host" (Pitfall 2) — a real draft-simple load carries both lines and PASSes
// on the ROCm0 one.
func judgeDraftBlock(blocks [][]string, m ResidencyMarkers) OffloadResult {
	if len(blocks) < 2 {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("no second load_tensors block found (draft expected)", ""),
			Detail: "draft: no draft load_tensors block found in the log",
		}
	}
	if m.DeviceToken == "" {
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("residency markers missing a device token (could not evaluate draft)", ""),
			Detail: "draft: could not be evaluated (no device token in markers)",
		}
	}

	var (
		sawDeviceBuffer bool
		sawCPUBuffer    bool
		deviceMiB       float64
	)
	for _, raw := range blocks[1] {
		line := strings.TrimSpace(raw)
		if !strings.Contains(line, loadTensorsPrefix) || !strings.Contains(line, bufferSizePhrase) {
			continue
		}
		mib, ok := parseBufferMiB(line)
		if !ok {
			continue
		}
		if strings.Contains(line, m.DeviceToken) {
			sawDeviceBuffer = true
			if mib > deviceMiB {
				deviceMiB = mib
			}
		} else {
			sawCPUBuffer = true
		}
	}

	switch {
	case sawDeviceBuffer && deviceMiB > 0:
		return OffloadResult{
			Status: StatusPass,
			Signal: detect.KnownBool(true, "draft load_tensors "+m.DeviceToken+" model buffer size"),
			Detail: fmt.Sprintf("draft: %s model buffer %.2f MiB resident on the iGPU", m.DeviceToken, deviceMiB),
		}
	case sawCPUBuffer:
		return OffloadResult{
			Status: StatusFail,
			Signal: detect.KnownBool(false, "draft load_tensors CPU buffer only"),
			Detail: "draft: loaded on the CPU",
		}
	default:
		return OffloadResult{
			Status: StatusWarn,
			Signal: detect.UnknownBool("no draft load_tensors buffer line found in the block", ""),
			Detail: "draft: no draft load_tensors buffer line found",
		}
	}
}

// foldDraft folds the draft sidecar's judgment into the already-decided target
// OffloadResult. It follows foldProjector's softening shape (validate.go) with one
// deliberate difference: the projector's failure is never fatal, but the draft is
// proven as a second model (ADR-0009), so a draft FAIL is a real residency FAIL —
// draft Unknown only ever takes a target PASS down to WARN, and never lowers a
// target that is already WARN or FAIL any further.
func foldDraft(target, draft OffloadResult) OffloadResult {
	target.Detail = fmt.Sprintf("%s; %s", target.Detail, draft.Detail)
	switch {
	case draft.Status == StatusFail:
		target.Status = StatusFail
		target.Raw = firstNonEmpty(target.Raw, draft.Raw)
	case draft.Status == StatusWarn && target.Status == StatusPass:
		target.Status = StatusWarn
	}
	return target
}
