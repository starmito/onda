package api

import (
	"encoding/json"
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
		return true, "NVIDIA GeForce RTX 5060 Ti, 376 MiB, 16311 MiB", nil
	}
	t.Cleanup(func() { checkGPU = orig })

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
}

func TestHandleHealth_GPUNotUsableByTorch(t *testing.T) {
	orig := checkGPU
	checkGPU = func() (bool, string, error) {
		return false, "CUDA not available", nil
	}
	t.Cleanup(func() { checkGPU = orig })

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
