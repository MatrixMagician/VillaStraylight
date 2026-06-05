package detect

// Live host paths used by Probe. They are package-level so tests of individual
// probe helpers can supply testdata seams while Probe itself reads the real host.
// Backend-specific paths (device nodes, the GPU ICD) live in gpu_amd.go — the
// seam — so this orchestrator stays backend-neutral.
const (
	liveDRMRoot         = "/sys/class/drm"
	liveTTMPagesLimit   = "/sys/module/ttm/parameters/pages_limit"
	liveProcMeminfo     = "/proc/meminfo"
	liveKernelOSRelease = "/proc/sys/kernel/osrelease"
)

// Probe reads the host and assembles a HostProfile. It never errors and never
// panics: any missing tool or unparseable output becomes a typed Unknown field
// (D-13). This is the compose-and-return orchestrator (cf. server.New()).
func Probe() HostProfile {
	cpuModel, arch := cpuInfo()
	totalRAM, memAvail := memInfo(liveProcMeminfo)

	gtt := gttTotalBytes(liveDRMRoot)
	ttm := ttmLimitBytes(liveTTMPagesLimit)
	envelope := usableEnvelope(gtt, ttm)
	vram := biosVRAMBytes(liveDRMRoot)

	// All backend (Vulkan/ROCm/DRI) probing is funneled through the seam in
	// gpu_amd.go, keeping this orchestrator free of backend assumptions.
	gpu := probeGPU()

	return HostProfile{
		CPUModel:            cpuModel,
		Arch:                arch,
		TotalRAMBytes:       totalRAM,
		MemAvailableBytes:   memAvail,
		IGPUName:            gpu.deviceName,
		IGPUGfxID:           gpu.gfxID,
		VulkanICDPath:       gpu.icdPath,
		VulkanDevice:        gpu.deviceName,
		DRINodes:            gpu.driNodes,
		DRINodeCount:        gpu.driCount,
		ROCmPresent:         gpu.rocmPresent,
		UsableEnvelopeBytes: envelope,
		GTTTotalBytes:       gtt,
		TTMLimitBytes:       ttm,
		BIOSVRAMBytes:       vram,
		KernelVersion:       kernelVersion(liveKernelOSRelease),
		MesaVersion:         gpu.mesaVersion,
		SchemaVersion:       hostProfileSchemaVersion,
	}
}
