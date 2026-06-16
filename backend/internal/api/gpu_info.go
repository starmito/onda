package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPUInfoResponse is the response for GET /api/gpu/info.
type GPUInfoResponse struct {
	Name              string `json:"name,omitempty"`
	VRAMTotalMB       int    `json:"vram_total_mb"`
	VRAMUsedMB        int    `json:"vram_used_mb"`
	VRAMFreeMB        int    `json:"vram_free_mb"`
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

// modelCatalogCache holds the loaded UVR model catalog for VRAM lookups.
var (
	modelCatalogCache []UVRModelEntry
	catalogOnce       sync.Once
)

// loadModelCatalog ensures the UVR catalog is loaded into the cache.
func loadModelCatalog() {
	catalogOnce.Do(func() {
		data, err := readProjectFile("uvr_models.json")
		if err == nil {
			json.Unmarshal(data, &modelCatalogCache)
		}
	})
}

// lookupVRAMMB returns the VRAM estimate in MB for a model name.
// It checks the hardcoded vramEstimates map by substring matching, then the UVR catalog.
// Falls back to defaultVRAMMB if nothing is found.
func lookupVRAMMB(modelName string) int {
	lower := strings.ToLower(modelName)
	for key, vram := range vramEstimates {
		if strings.Contains(lower, strings.ToLower(key)) {
			return vram
		}
	}
	loadModelCatalog()
	// Try exact name match in catalog.
	for _, m := range modelCatalogCache {
		if m.Name == modelName {
			return int(m.SizeMB)
		}
	}
	// Try matching by name without common extensions.
	stem := strings.TrimSuffix(modelName, ".onnx")
	stem = strings.TrimSuffix(stem, ".ckpt")
	stem = strings.TrimSuffix(stem, ".pth")
	stem = strings.TrimSuffix(stem, ".th")
	stem = strings.TrimSuffix(stem, ".safetensors")
	for _, m := range modelCatalogCache {
		if m.Name == stem {
			return int(m.SizeMB)
		}
	}
	// Try matching by filename (without extension) in catalog.
	for _, m := range modelCatalogCache {
		if m.Filename == modelName {
			return int(m.SizeMB)
		}
	}
	return defaultVRAMMB
}

// getGPUInfo queries GPU details via PyTorch inside the Docker container.
// The onda container (python:slim) does not have nvidia-smi, so we use torch.cuda.
func getGPUInfo() GPUInfoResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	script := `import torch, json
if not torch.cuda.is_available():
    print(json.dumps({"ok": False, "error": "CUDA not available"}))
else:
    props = torch.cuda.get_device_properties(0)
    total = props.total_memory
    reserved = torch.cuda.memory_reserved(0)
    result = {
        "ok": True,
        "name": props.name,
        "total_mb": total // (1024*1024),
        "used_mb": reserved // (1024*1024),
        "free_mb": (total - reserved) // (1024*1024),
    }
    try:
        import pynvml
        pynvml.nvmlInit()
        handle = pynvml.nvmlDeviceGetHandleByIndex(0)
        result["util_pct"] = pynvml.nvmlDeviceGetUtilizationRates(handle).gpu
        result["temp_c"] = pynvml.nvmlDeviceGetTemperature(handle, pynvml.NVML_TEMPERATURE_GPU)
        pynvml.nvmlShutdown()
    except Exception:
        result["util_pct"] = -1
        result["temp_c"] = -1
    print(json.dumps(result))`

	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		return GPUInfoResponse{
			OK:    false,
			Error: fmt.Sprintf("failed to query GPU via PyTorch: %v", err),
		}
	}

	var result struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
		Name    string `json:"name"`
		TotalMB int    `json:"total_mb"`
		UsedMB  int    `json:"used_mb"`
		FreeMB  int    `json:"free_mb"`
		UtilPct int    `json:"util_pct"`
		TempC   int    `json:"temp_c"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return GPUInfoResponse{
			OK:    false,
			Error: fmt.Sprintf("failed to parse GPU info: %v", err),
		}
	}

	if !result.OK {
		return GPUInfoResponse{
			OK:    false,
			Error: result.Error,
		}
	}

	utilization := result.UtilPct
	if utilization < 0 {
		utilization = 0
	}
	temperature := result.TempC
	if temperature < 0 {
		temperature = 0
	}

	return GPUInfoResponse{
		Name:              result.Name,
		VRAMTotalMB:       result.TotalMB,
		VRAMUsedMB:        result.UsedMB,
		VRAMFreeMB:        result.FreeMB,
		UtilizationGPUPct: utilization,
		TemperatureC:      temperature,
		Runtime:           "pytorch",
		OK:                true,
	}
}

// handleGPUInfo serves GET /api/gpu/info with GPU details from PyTorch.
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
	if !info.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(info)
}

// isViperXOrRoformer returns true for ViperX and Roformer models that
// are sensitive to chunk_size in their VRAM calculation.
// Uses substring matching to recognize full model names like "BS_Roformer_Viperx".
func isViperXOrRoformer(modelName string) bool {
	lower := strings.ToLower(modelName)
	patterns := []string{"viperx", "melband", "polarformer", "roformer"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isDemucsModel returns true for Demucs-family models whose VRAM scales
// with the number of shift-averaging passes.
func isDemucsModel(modelName string) bool {
	lower := strings.ToLower(modelName)
	return strings.Contains(lower, "htdemucs") || strings.Contains(lower, "demucs")
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

	// Parse chunk_size query parameter (affects VRAM for ViperX/Roformer models).
	chunkSize := 0
	chunkSizeParam := r.URL.Query().Get("chunk_size")
	if chunkSizeParam != "" {
		if cs, err := strconv.Atoi(chunkSizeParam); err == nil && cs > 0 {
			chunkSize = cs
		}
	}

	// Parse shifts query parameter (affects VRAM for Demucs models).
	shifts := 0
	shiftsParam := r.URL.Query().Get("shifts")
	if shiftsParam != "" {
		if s, err := strconv.Atoi(shiftsParam); err == nil && s > 1 {
			shifts = s
		}
	}

	// Parse segment_size query parameter (larger segments = more VRAM).
	segmentSize := 0
	segmentSizeParam := r.URL.Query().Get("segment_size")
	if segmentSizeParam != "" {
		if ss, err := strconv.Atoi(segmentSizeParam); err == nil && ss > 0 {
			segmentSize = ss
		}
	}

	// Parse overlap query parameter (small additive VRAM factor).
	overlap := 0.0
	overlapParam := r.URL.Query().Get("overlap")
	if overlapParam != "" {
		if o, err := strconv.ParseFloat(overlapParam, 64); err == nil && o > 0 {
			overlap = o
		}
	}

	// Parse batch_size query parameter (multiplies VRAM for batched processing).
	batchSize := 0
	batchSizeParam := r.URL.Query().Get("batch_size")
	if batchSizeParam != "" {
		if bs, err := strconv.Atoi(batchSizeParam); err == nil && bs > 0 {
			batchSize = bs
		}
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

	// defaultChunkSize is the reference chunk_size (1024) used in the VRAM formula.
	const defaultChunkSize = 1024
	// chunkVRAMFactor controls how strongly chunk_size affects VRAM estimation.
	const chunkVRAMFactor = 0.25

	// Split by comma: "vocal=melband_kj,stems=htdemucs_ft"
	pairs := strings.Split(modelsParam, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		// Split by first "=". If no "=", treat the entire string as a model name.
		eqIdx := strings.Index(pair, "=")
		var modelType, modelName string
		if eqIdx < 0 {
			modelType = "unknown"
			modelName = pair
		} else {
			modelType = strings.TrimSpace(pair[:eqIdx])
			modelName = strings.TrimSpace(pair[eqIdx+1:])
		}

		vramMB := lookupVRAMMB(modelName)

		// Convert to float64 for precise multiplicative adjustments.
		estimated := float64(vramMB)

		// Apply chunk_size factor for ViperX/Roformer models.
		if chunkSize > 0 && isViperXOrRoformer(modelName) {
			estimated *= 1.0 + float64(chunkSize)/float64(defaultChunkSize)*chunkVRAMFactor
		}

		// Apply batch_size factor: batch=4 → ×2 (i.e., batch/2).
		if batchSize > 0 {
			estimated *= float64(batchSize) / 2.0
		}

		// Apply segment_size factor: larger segments use more memory.
		// Reference is 256; every 1024 over that adds 50 %.
		if segmentSize > 0 {
			estimated *= 1.0 + (float64(segmentSize)-256.0)/1024.0*0.5
		}

		// Apply overlap factor: small additive multiplier.
		if overlap > 0 {
			estimated *= 1.0 + overlap*0.3
		}

		// Apply shifts factor for Demucs models: N passes ≈ N× VRAM.
		if shifts > 1 && isDemucsModel(modelName) {
			estimated *= float64(shifts)
		}

		vramMB = int(estimated)

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
