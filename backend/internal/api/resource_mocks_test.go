package api

import "testing"

// mockResourceProviders replaces the GPU and host-memory providers with
// generous mocks so pipeline tests are not affected by the real machine's
// hardware. Tests that explicitly need low VRAM/RAM set the providers
// directly after calling this helper.
func mockResourceProviders(t *testing.T) {
	t.Helper()
	origGPU := gpuInfoProvider
	origRAM := hostMemoryProvider
	t.Cleanup(func() {
		gpuInfoProvider = origGPU
		hostMemoryProvider = origRAM
	})
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 24000, VRAMFreeMB: 24000}
	}
	hostMemoryProvider = func() (int, int, bool) {
		return 32000, 32000, true
	}
}

// mockLowRAMProvider replaces only the host-memory provider with a low-RAM
// mock. Use it together with the normal GPU mock to exercise the RAM guard.
func mockLowRAMProvider(t *testing.T, availableMB int) {
	t.Helper()
	origRAM := hostMemoryProvider
	t.Cleanup(func() { hostMemoryProvider = origRAM })
	hostMemoryProvider = func() (int, int, bool) {
		return availableMB * 2, availableMB, true
	}
}
