package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleProcessStatus_ReportsDeviceVRAM verifies that the GPU memory fields
// exposed by /api/processes/status come from the device (nvidia-smi), not from
// our own process allocation. We inject a known reading and check it propagates
// unchanged.
func TestHandleProcessStatus_ReportsDeviceVRAM(t *testing.T) {
	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{
			OK:                true,
			Name:              "NVIDIA GeForce RTX 5060 Ti",
			Runtime:           "nvidia-smi",
			VRAMTotalMB:       16311,
			VRAMUsedMB:        376,
			VRAMFreeMB:        15475,
			UtilizationGPUPct: 5,
			TemperatureC:      42,
		}
	}

	s := &Server{mux: http.NewServeMux(), jobs: make(map[string]*JobState)}
	s.mux.HandleFunc("GET /api/processes/status", s.handleProcessStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/processes/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	gpu, ok := resp["gpu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected gpu object, got %T", resp["gpu"])
	}
	if gpu["used_mb"] != 376.0 {
		t.Errorf("used_mb = %v, want 376 (device real usage)", gpu["used_mb"])
	}
	if gpu["free_mb"] != 15475.0 {
		t.Errorf("free_mb = %v, want 15475 (device real free)", gpu["free_mb"])
	}
	if gpu["total_mb"] != 16311.0 {
		t.Errorf("total_mb = %v, want 16311 (device real total)", gpu["total_mb"])
	}
}
