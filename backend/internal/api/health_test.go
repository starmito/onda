package api

import "testing"

func TestCheckGPU_Available(t *testing.T) {
	available, info, err := checkGPU()
	if err != nil {
		t.Skipf("GPU check not available: %v", err)
	}
	if !available {
		t.Logf("GPU not available: %s", info)
	}
}
