package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// newDetect builds the `villa detect` command: probe the host, then render a
// human table (default), structured JSON (--json), or a provenance-annotated
// table (-v). Read-only — no host state is written.
func newDetect() *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Print a hardware profile of this host",
		Long:  "Detect CPU/arch, the AMD iGPU, Vulkan/ROCm backend availability, total RAM, and the real usable GTT envelope. Read-only.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := detect.Probe()
			return renderDetect(cmd.OutOrStdout(), profile, jsonOut, verbose)
		},
	}
}

// renderDetect writes the profile to w. It is separated from RunE so the golden
// test can inject a fixture profile and capture the exact JSON bytes (the D-05
// dashboard-contract guard).
func renderDetect(w io.Writer, p detect.HostProfile, asJSON, withProvenance bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	return renderDetectTable(w, p, withProvenance)
}

func renderDetectTable(w io.Writer, p detect.HostProfile, withProvenance bool) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	rows := []struct {
		label  string
		val    string
		source string
	}{
		{"CPU", strVal(p.CPUModel.Value, p.CPUModel.Known, p.CPUModel.Source), p.CPUModel.Source},
		{"Arch", strVal(p.Arch.Value, p.Arch.Known, p.Arch.Source), p.Arch.Source},
		{"Total RAM", bytesVal(p.TotalRAMBytes), p.TotalRAMBytes.Source},
		{"Mem available", bytesVal(p.MemAvailableBytes), p.MemAvailableBytes.Source},
		{"iGPU", strVal(p.IGPUName.Value, p.IGPUName.Known, p.IGPUName.Source), p.IGPUName.Source},
		{"iGPU gfx id", strVal(p.IGPUGfxID.Value, p.IGPUGfxID.Known, p.IGPUGfxID.Source), p.IGPUGfxID.Source},
		{"Vulkan ICD", strVal(p.VulkanICDPath.Value, p.VulkanICDPath.Known, p.VulkanICDPath.Source), p.VulkanICDPath.Source},
		{"Vulkan device", strVal(p.VulkanDevice.Value, p.VulkanDevice.Known, p.VulkanDevice.Source), p.VulkanDevice.Source},
		{"/dev/dri nodes", driVal(p), p.DRINodeCount.Source},
		{"ROCm present", boolVal(p.ROCmPresent), p.ROCmPresent.Source},
		{"Usable envelope", bytesVal(p.UsableEnvelopeBytes), p.UsableEnvelopeBytes.Source},
		{"  GTT total", bytesVal(p.GTTTotalBytes), p.GTTTotalBytes.Source},
		{"  ttm limit", bytesVal(p.TTMLimitBytes), p.TTMLimitBytes.Source},
		{"  BIOS VRAM", bytesVal(p.BIOSVRAMBytes), p.BIOSVRAMBytes.Source},
		{"Kernel", strVal(p.KernelVersion.Value, p.KernelVersion.Known, p.KernelVersion.Source), p.KernelVersion.Source},
		{"Mesa", strVal(p.MesaVersion.Value, p.MesaVersion.Known, p.MesaVersion.Source), p.MesaVersion.Source},
	}

	for _, r := range rows {
		if withProvenance {
			fmt.Fprintf(tw, "%s\t%s\t(%s)\n", r.label, r.val, r.source)
		} else {
			fmt.Fprintf(tw, "%s\t%s\n", r.label, r.val)
		}
	}
	return tw.Flush()
}

// strVal renders an optional string: the value when known, else "unknown (reason)".
func strVal(v string, known bool, reason string) string {
	if known {
		return v
	}
	return unknownLabel(reason)
}

func boolVal(b detect.Bool) string {
	if !b.Known {
		return unknownLabel(b.Source)
	}
	return strconv.FormatBool(b.Value)
}

func driVal(p detect.HostProfile) string {
	if !p.DRINodeCount.Known {
		return unknownLabel(p.DRINodeCount.Source)
	}
	return fmt.Sprintf("%d [%s]", p.DRINodeCount.Value, strings.Join(p.DRINodes, " "))
}

// bytesVal renders an optional byte count in GiB (3 dp) plus raw bytes, or
// "unknown (reason)".
func bytesVal(b detect.Bytes) string {
	if !b.Known {
		return unknownLabel(b.Source)
	}
	gib := float64(b.Value) / (1 << 30)
	return fmt.Sprintf("%.3f GiB (%d bytes)", gib, b.Value)
}

func unknownLabel(reason string) string {
	if reason == "" {
		return "unknown"
	}
	return "unknown (" + reason + ")"
}
