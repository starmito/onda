package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckGPU_Available(t *testing.T) {
	available, info, err := checkGPU()
	if err != nil {
		t.Skipf("GPU check not available: %v", err)
	}
	if !available {
		t.Logf("GPU not available: %s", info)
	}
}

func TestHandleHealth_GPUUsableByTorch(t *testing.T) {
	orig := checkGPU
	checkGPU = func() (bool, string, error) {
		return true, "CUDA available", nil
	}
	t.Cleanup(func() { checkGPU = orig })

	origGPU := gpuInfoProvider
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{
			OK: true, Name: "NVIDIA GeForce RTX 5060 Ti",
			VRAMTotalMB: 16311, VRAMUsedMB: 376, VRAMFreeMB: 15475,
		}
	}
	t.Cleanup(func() { gpuInfoProvider = origGPU })

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/health", s.handleHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
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
	if gpu["ok"] != true {
		t.Errorf("expected gpu.ok true, got %v", gpu["ok"])
	}
	if gpu["usable_by_torch"] != true {
		t.Errorf("expected gpu.usable_by_torch true, got %v", gpu["usable_by_torch"])
	}
	if gpu["used_mb"] != 376.0 {
		t.Errorf("expected used_mb from device (376), got %v", gpu["used_mb"])
	}
	if gpu["free_mb"] != 15475.0 {
		t.Errorf("expected free_mb from device (15475), got %v", gpu["free_mb"])
	}
	if gpu["total_mb"] != 16311.0 {
		t.Errorf("expected total_mb from device (16311), got %v", gpu["total_mb"])
	}
}

func TestHandleHealth_ONNXRuntimeCUDA(t *testing.T) {
	orig := checkONNXRuntime
	checkONNXRuntime = func() (map[string]interface{}, error) {
		return map[string]interface{}{
			"available":      true,
			"version":        "1.30.0",
			"providers":      []interface{}{"CUDAExecutionProvider", "CPUExecutionProvider"},
			"cuda":           true,
			"cuda_supported": true,
			"cuda_requested": true,
			"error":          nil,
		}, nil
	}
	t.Cleanup(func() { checkONNXRuntime = orig })

	origGPU := checkGPU
	checkGPU = func() (bool, string, error) { return true, "CUDA available", nil }
	t.Cleanup(func() { checkGPU = origGPU })

	origGPUInfo := gpuInfoProvider
	gpuInfoProvider = func() GPUInfoResponse { return GPUInfoResponse{OK: true} }
	t.Cleanup(func() { gpuInfoProvider = origGPUInfo })

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/health", s.handleHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	onnx, ok := resp["onnxruntime"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected onnxruntime object, got %T", resp["onnxruntime"])
	}
	if onnx["available"] != true {
		t.Errorf("expected onnxruntime.available true, got %v", onnx["available"])
	}
	if onnx["cuda"] != true {
		t.Errorf("expected onnxruntime.cuda true, got %v", onnx["cuda"])
	}
	if onnx["cuda_supported"] != true {
		t.Errorf("expected onnxruntime.cuda_supported true, got %v", onnx["cuda_supported"])
	}
	providers, ok := onnx["providers"].([]interface{})
	if !ok || len(providers) == 0 || providers[0] != "CUDAExecutionProvider" {
		t.Errorf("expected CUDA first in providers, got %v", onnx["providers"])
	}
}

func TestHandleHealth_ONNXRuntimeProbeFails(t *testing.T) {
	orig := checkONNXRuntime
	checkONNXRuntime = func() (map[string]interface{}, error) {
		return nil, fmt.Errorf("python import failed")
	}
	t.Cleanup(func() { checkONNXRuntime = orig })

	origGPU := checkGPU
	checkGPU = func() (bool, string, error) { return true, "CUDA available", nil }
	t.Cleanup(func() { checkGPU = origGPU })

	origGPUInfo := gpuInfoProvider
	gpuInfoProvider = func() GPUInfoResponse { return GPUInfoResponse{OK: true} }
	t.Cleanup(func() { gpuInfoProvider = origGPUInfo })

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/health", s.handleHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	onnx, ok := resp["onnxruntime"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected onnxruntime object, got %T", resp["onnxruntime"])
	}
	if onnx["available"] != false {
		t.Errorf("expected onnxruntime.available false, got %v", onnx["available"])
	}
	if onnx["error"] == nil {
		t.Errorf("expected onnxruntime.error to be set")
	}
}

func TestHandleHealth_GPUNotUsableByTorch(t *testing.T) {
	orig := checkGPU
	checkGPU = func() (bool, string, error) {
		return false, "CUDA not available", nil
	}
	t.Cleanup(func() { checkGPU = orig })

	origGPU := gpuInfoProvider
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: false, Error: "nvidia-smi failed: command not found"}
	}
	t.Cleanup(func() { gpuInfoProvider = origGPU })

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/health", s.handleHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
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
	if gpu["ok"] != false {
		t.Errorf("expected gpu.ok false, got %v", gpu["ok"])
	}
	if gpu["usable_by_torch"] != false {
		t.Errorf("expected gpu.usable_by_torch false, got %v", gpu["usable_by_torch"])
	}
	if gpu["code"] != "E3" {
		t.Errorf("expected gpu.code E3, got %v", gpu["code"])
	}
}
