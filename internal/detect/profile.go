package detect

// hostProfileSchemaVersion is the self-version of the HostProfile contract.
// Bump it whenever a field's meaning changes incompatibly so the Phase 5
// dashboard can detect a mismatch.
//
// v2 (v1.1 Phase 7, DET-04): APPEND-ONLY bump — a nested rocm_readiness object
// was added AFTER the GPU block; no existing v1 field was renamed, retyped, or
// reordered. Consumers (dashboard, Phase-10 recommend) read schema_version to
// detect the v1.1 contract.
//
// v3 (issue #120, PRE-08): APPEND-ONLY bump — KFDAccess and RenderNodeAccess were
// added AFTER rocm_readiness and BEFORE schema_version; no existing field was
// renamed, retyped, or reordered.
const hostProfileSchemaVersion = 3

// HostProfile is the structured result of Probe — the single source of truth for
// `villa detect --json` AND the struct the Phase 5 dashboard consumes.
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

	// Memory. TotalRAMBytes is the kernel's MemTotal — RAM the kernel can
	// actually hand out, which is slightly below the physical total because
	// firmware reservations are already deducted. It is deliberately the smaller,
	// more honest figure: recommend derives its degraded fallback floor from it,
	// and that floor must never read high. MemAvailableBytes is the live kernel
	// MemAvailable. Neither is the GPU envelope (see UsableEnvelopeBytes).
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

	// ROCmReadiness is the v1.1 (schema 2) ROCm opt-in readiness sub-tree the
	// dashboard and Phase-10 recommend consume. It is appended AFTER the GPU block
	// as a strictly additive contract change; nothing above it moved.
	ROCmReadiness ROCmReadiness `json:"rocm_readiness"`

	// KFDAccess and RenderNodeAccess are the v1.9 (schema 3, issue #120) compute
	// device access probes: can THIS invoking user open /dev/kfd and at least one
	// DRM render node for read+write, as opposed to whether the devices merely
	// exist. PRE-08 gates on these before an install writes anything, rather than
	// letting a missing group membership surface as a failed unit start. Both are
	// appended AFTER rocm_readiness as a strictly additive contract change.
	KFDAccess        Bool `json:"kfd_access"`
	RenderNodeAccess Bool `json:"render_node_access"`

	// SchemaVersion is the HostProfile contract self-version. It MUST stay the
	// LAST field of HostProfile (append-only discipline; new fields go above it).
	SchemaVersion int `json:"schema_version"`
}

// ROCmReadiness is the nested ROCm opt-in readiness signal added in v1.1 (schema
// 2, DET-04). Every field is a typed-Optional Bool (value.go) so an undetectable
// off-hardware signal serializes as UNSET (Known=false), distinct from a real
// false — the no-false-green guarantee. It is backend-neutral by NAME only
// in the sense that it is computed by readiness_rocm.go from already-bounded
// HostProfile facts plus the resolved image (any ROCm-specific host probe lives
// behind the gpu_amd.go seam).
type ROCmReadiness struct {
	// HSAOverrideViable reports whether the HSA_OVERRIDE_GFX_VERSION override ROCm
	// needs on gfx1151 is viable. Unknown off-hardware (override not probed).
	HSAOverrideViable Bool `json:"hsa_override_viable"`
	// FirmwareDateOK reports whether the linux-firmware date is clear of the
	// known-bad build. Unknown off-hardware (firmware date not probed).
	FirmwareDateOK Bool `json:"firmware_date_ok"`
	// KernelFloorOK reports whether the running kernel meets the gfx1151 floor.
	// Known when KernelVersion is Known; else Unknown.
	KernelFloorOK Bool `json:"kernel_floor_ok"`
	// RocminfoGfx1151 reports whether rocminfo enumerates the gfx1151 target.
	// Known when IGPUGfxID is Known; else Unknown (rocminfo absent off-hardware).
	RocminfoGfx1151 Bool `json:"rocminfo_gfx1151"`
	// ImagePolicyOK reports whether the resolved ROCm image obeys the pin policy
	// (the pinned stable image, never a nightly). Config/request-driven, NOT a host
	// probe (Pitfall 5) — computed against the resolved image string (the policy
	// literals live behind the gpu_amd.go seam).
	ImagePolicyOK Bool `json:"image_policy_ok"`
}
