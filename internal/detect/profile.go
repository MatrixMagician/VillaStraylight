package detect

// hostProfileSchemaVersion is the self-version of the HostProfile contract.
// Bump it whenever a field's meaning changes incompatibly so the Phase 5
// dashboard can detect a mismatch.
const hostProfileSchemaVersion = 1

// HostProfile is the structured result of Probe — the single source of truth for
// `villa detect --json` AND the struct the Phase 5 dashboard consumes (D-05).
//
// Field names are deliberate and stable: the dashboard reads them verbatim, so
// the golden JSON test guards this contract. Every field is a typed Optional
// (value.go) so consumers can tell "couldn't detect" from a real zero, and the
// struct is backend-neutral — nothing here names Vulkan/ROCm/AMD specifics;
// those live only in gpu_amd.go (the backend seam).
type HostProfile struct {
	// CPU / system.
	CPUModel Str `json:"cpu_model"`
	Arch     Str `json:"arch"`

	// Memory. TotalRAMBytes is physical RAM; MemAvailableBytes is the live
	// kernel MemAvailable. Neither is the GPU envelope (see UsableEnvelopeBytes).
	TotalRAMBytes     Bytes `json:"total_ram_bytes"`
	MemAvailableBytes Bytes `json:"mem_available_bytes"`

	// Integrated GPU identity and backend availability.
	IGPUName      Str      `json:"igpu_name"`
	IGPUGfxID     Str      `json:"igpu_gfx_id"` // e.g. "gfx1151"
	VulkanICDPath Str      `json:"vulkan_icd_path"`
	VulkanDevice  Str      `json:"vulkan_device"`
	DRINodes      []string `json:"dri_nodes"`
	DRINodeCount  Int      `json:"dri_node_count"`
	ROCmPresent   Bool     `json:"rocm_present"`

	// The memory envelope story. UsableEnvelopeBytes is the authoritative usable
	// ceiling and is sourced from GTTTotalBytes (mem_info_gtt_total) — NEVER from
	// TotalRAMBytes and NEVER from BIOSVRAMBytes. TTMLimitBytes is a cross-check.
	UsableEnvelopeBytes Bytes `json:"usable_envelope_bytes"`
	GTTTotalBytes       Bytes `json:"gtt_total_bytes"`
	TTMLimitBytes       Bytes `json:"ttm_limit_bytes"`
	BIOSVRAMBytes       Bytes `json:"bios_vram_bytes"`

	// Floor-gate data (kernel/Mesa version). Used as gates by preflight, never as
	// an envelope multiplier.
	KernelVersion Str `json:"kernel_version"`
	MesaVersion   Str `json:"mesa_version"`

	// SchemaVersion is the HostProfile contract self-version.
	SchemaVersion int `json:"schema_version"`
}
