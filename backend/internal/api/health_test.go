package api

import "testing"

func TestCheckDockerContainer_Running(t *testing.T) {
	// Skip if docker not available
	status, err := checkDockerContainer()
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if status != "running" {
		t.Errorf("expected running, got %s", status)
	}
}

func TestCheckGPU_Available(t *testing.T) {
	available, info, err := checkGPU()
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if !available {
		t.Logf("GPU not available: %s", info)
	}
}
