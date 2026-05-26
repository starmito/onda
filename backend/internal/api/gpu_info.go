package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GPUInfoResponse is the response for GET /api/gpu/info.
type GPUInfoResponse struct {
	Name              string `json:"name,omitempty"`
	VRAMTotalMB       int    `json:"vram_total_mb,omitempty"`
	VRAMUsedMB        int    `json:"vram_used_mb,omitempty"`
	VRAMFreeMB        int    `json:"vram_free_mb,omitempty"`
	UtilizationGPUPct int    `json:"utilization_gpu_pct,omitempty"`
	TemperatureC      int    `json:"temperature_c,omitempty"`
	Runtime           string `json:"runtime,omitempty"`
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
}

// VRAMModelEntry represents one model in the VRAM calculator response.
type VRAMModelEntry struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	VRAMMB int    `json:"vram_mb"`
}

// VRAMCalculatorResponse is the response for GET /api/gpu/vram-calculator.
type VRAMCalculatorResponse struct {
	Models          []VRAMModelEntry `json:"models"`
	TotalVRAMMB     int              `json:"total_vram_mb"`
	AvailableVRAMMB int              `json:"available_vram_mb"`
	FreeAfterMB     int              `json:"free_after_mb"`
	Fits            bool             `json:"fits"`
}

// vramEstimates maps model names to their approximate VRAM usage in MB.
var vramEstimates = map[string]int{
	"melband_kj":        3200,
	"melband_roformer":  4200,
	"polarformer":       4800,
	"viperx":            3800,
	"viperx_other":      3800,
	"htdemucs_ft":       2800,
	"htdemucs_drums":    800,
	"htdemucs_bass":     800,
	"htdemucs_other":    800,
	"htdemucs_vocals":   800,
	"mdx_kim_vocal_2":   800,
	"mdx_uvr_main":      800,
}

// defaultVRAMMB is used when a model is not found in vramEstimates.
const defaultVRAMMB = 2000

// fallbackAvailableVRAMMB is used when GPU info cannot be obtained.
const fallbackAvailableVRAMMB = 16311

// getGPUInfo runs nvidia-smi and checks the Docker runtime to build a GPUInfoResponse.
// This is the internal function callable by other handlers without making an HTTP request.
func getGPUInfo() GPUInfoResponse {
	// Check Docker runtime first.
	rt, rtErr := getDockerRuntime()
	if rtErr != nil {
		return GPUInfoResponse{
			OK:    false,
			Error: "nvidia runtime not active",
		}
	}
	if rt != "nvidia" {
		return GPUInfoResponse{
			OK:    false,
			Error: "nvidia runtime not active",
		}
	}

	// Run nvidia-smi.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,memory.free,utilization.gpu,temperature.gpu",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return GPUInfoResponse{
			OK:    false,
			Error: "nvidia-smi not available",
		}
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return GPUInfoResponse{
			OK:    false,
			Error: "nvidia-smi returned empty output",
		}
	}

	fields := strings.Split(line, ",")
	if len(fields) < 6 {
		return GPUInfoResponse{
			OK:    false,
			Error: fmt.Sprintf("nvidia-smi unexpected output: %s", line),
		}
	}

	name := strings.TrimSpace(fields[0])
	total, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
	used, _ := strconv.Atoi(strings.TrimSpace(fields[2]))
	free, _ := strconv.Atoi(strings.TrimSpace(fields[3]))
	util, _ := strconv.Atoi(strings.TrimSpace(fields[4]))
	temp, _ := strconv.Atoi(strings.TrimSpace(fields[5]))

	return GPUInfoResponse{
		Name:              name,
		VRAMTotalMB:       total,
		VRAMUsedMB:        used,
		VRAMFreeMB:        free,
		UtilizationGPUPct: util,
		TemperatureC:      temp,
		Runtime:           rt,
		OK:                true,
	}
}

// getDockerRuntime returns the runtime configured for the Onda container.
func getDockerRuntime() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", dockerContainer,
		"--format", "{{.HostConfig.Runtime}}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// handleGPUInfo serves GET /api/gpu/info with detailed nvidia-smi output.
func (s *Server) handleGPUInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	info := getGPUInfo()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}

// handleVRAMCalculator serves GET /api/gpu/vram-calculator with VRAM estimates
// for the requested models and available GPU memory.
func (s *Server) handleVRAMCalculator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	// Parse models query parameter: models=vocal=melband_kj,stems=htdemucs_ft
	modelsParam := r.URL.Query().Get("models")
	if modelsParam == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "missing required query parameter: models",
		})
		return
	}

	var models []VRAMModelEntry
	totalVRAM := 0

	// Split by comma: "vocal=melband_kj,stems=htdemucs_ft"
	pairs := strings.Split(modelsParam, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		// Split by first "=": "vocal=melband_kj" -> ["vocal", "melband_kj"]
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			continue
		}
		modelType := strings.TrimSpace(pair[:eqIdx])
		modelName := strings.TrimSpace(pair[eqIdx+1:])

		vramMB, ok := vramEstimates[modelName]
		if !ok {
			vramMB = defaultVRAMMB
		}

		models = append(models, VRAMModelEntry{
			Name:   modelName,
			Type:   modelType,
			VRAMMB: vramMB,
		})
		totalVRAM += vramMB
	}

	// Get available VRAM from GPU info (internal call, not HTTP).
	gpuInfo := getGPUInfo()
	availableVRAM := fallbackAvailableVRAMMB
	if gpuInfo.OK {
		availableVRAM = gpuInfo.VRAMFreeMB
	}

	freeAfter := availableVRAM - totalVRAM

	resp := VRAMCalculatorResponse{
		Models:          models,
		TotalVRAMMB:     totalVRAM,
		AvailableVRAMMB: availableVRAM,
		FreeAfterMB:     freeAfter,
		Fits:            freeAfter >= 0,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
