package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
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

// defaultVRAMMB is used when a model is not catalogued by estimateVRAMMB.
const defaultVRAMMB = 2000

// fallbackAvailableVRAMMB is used when GPU info cannot be obtained.
const fallbackAvailableVRAMMB = 16311

// estimateVRAMMB returns the empirical VRAM peak in MB for a model name.
// It uses measured peaks for Roformer/ViperX/Vocal, Demucs, MDX/MDXNet and SCNet.
// Falls back to defaultVRAMMB for unknown models.
func estimateVRAMMB(modelName string, segmentSize, chunkSize, batchSize, demucsSegment int) int {
	lower := strings.ToLower(modelName)

	// MDX / MDXNet / ONNX: empirical peak depends on dim_t (derived from
	// segment_size) and scales with batch size. Checked before Vocal/Roformer
	// because names like "MDXNet_Vocals" contain the "vocal" substring but are
	// still MDX-family models.
	if strings.Contains(lower, "mdx") || strings.Contains(lower, "onnx") {
		return mdxEstimateVRAMMB(segmentSize, batchSize)
	}

	// SCNet: empirical peak scales linearly with chunk_size and batch size.
	// Overlap does not affect the estimate.
	if strings.Contains(lower, "scnet") {
		return scnetEstimateVRAMMB(chunkSize, batchSize)
	}

	// Roformer / ViperX / Vocal: measured peak with real long audio:
	// pico ≈ 1100 + (106 + 6.72*segment_size) * batch_size.
	// batch_size is multiplicative because chunks are processed in parallel.
	if isVocalOrRoformer(lower) {
		b := batchSize
		if b < 1 {
			b = 1
		}
		return int(math.Round(1100.0 + (106.0+6.72*float64(segmentSize))*float64(b)))
	}

	// Demucs / htdemucs: measured peak depends on demucs segment setting.
	if isDemucsModel(lower) {
		if demucsSegment >= 7 {
			return 1106
		}
		return 1572
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

// mdxDimTPoints are today's measured MDX23C dim_t values ordered increasingly.
// dim_t is derived from segment_size as: dim_t = segment_size*3 + 33.
var mdxDimTPoints = []int{417, 801, 1569, 2337}

// mdxVRAMPoints are the measured VRAM peaks (MiB, batch 1) for mdxDimTPoints.
var mdxVRAMPoints = []int{2080, 2478, 6748, 9872}

// mdxEstimateVRAMMB returns the empirical MDX-family VRAM peak in MB.
// It derives dim_t from segment_size, interpolates the base peak from the
// measured table, and multiplies by batch size.
func mdxEstimateVRAMMB(segmentSize, batchSize int) int {
	b := batchSize
	if b < 1 {
		b = 1
	}
	dimT := segmentSize*3 + 33
	base := interpolatePeak(dimT, mdxDimTPoints, mdxVRAMPoints)
	return base * b
}

// scnetChunkPoints are today's measured SCNet chunk_size values (samples)
// for 100-second audio, ordered increasingly.
var scnetChunkPoints = []int{242550, 485100, 970200}

// scnetVRAMPoints are the measured VRAM peaks (MiB, batch 1) for scnetChunkPoints.
var scnetVRAMPoints = []int{600, 948, 1762}

// scnetEstimateVRAMMB returns the empirical SCNet VRAM peak in MB.
// It uses a base linear in chunk_size (interpolated from batch-1 measurements)
// and multiplies by batch size. Overlap is ignored.
func scnetEstimateVRAMMB(chunkSize, batchSize int) int {
	b := batchSize
	if b < 1 {
		b = 1
	}
	base := interpolatePeak(chunkSize, scnetChunkPoints, scnetVRAMPoints)
	return base * b
}

// interpolatePeak returns the interpolated peak from sorted x/y measurement
// tables. Values below the first point clamp to the first point, values above
// the last point clamp to the last point, and values in between use linear
// interpolation.
func interpolatePeak(x int, xs, ys []int) int {
	if len(xs) == 0 || len(xs) != len(ys) {
		return defaultVRAMMB
	}
	if x <= xs[0] {
		return ys[0]
	}
	for i := 1; i < len(xs); i++ {
		if x <= xs[i] {
			t := float64(x-xs[i-1]) / float64(xs[i]-xs[i-1])
			return int(math.Round(float64(ys[i-1]) + t*float64(ys[i]-ys[i-1])))
		}
	}
	return ys[len(ys)-1]
}

// isVocalOrRoformer returns true for Vocal and Roformer models whose VRAM
// scales with segment_size and batch_size according to the empirical formula.
// Uses substring matching to recognize full model names like "BS_Roformer_Viperx".
func isVocalOrRoformer(modelName string) bool {
	lower := strings.ToLower(modelName)
	patterns := []string{"vocal", "viperx", "melband", "polarformer", "roformer"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isViperXOrRoformer is an alias for backward compatibility.
func isViperXOrRoformer(modelName string) bool {
	return isVocalOrRoformer(modelName)
}

// isDemucsModel returns true for Demucs-family models whose VRAM depends
// on the demucs_segment parameter.
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

	// Parse segment_size query parameter (affects VRAM for Roformer/ViperX/Vocal and MDX models).
	segmentSize := 0
	segmentSizeParam := r.URL.Query().Get("segment_size")
	if segmentSizeParam != "" {
		if ss, err := strconv.Atoi(segmentSizeParam); err == nil && ss > 0 {
			segmentSize = ss
		}
	}

	// Parse chunk_size query parameter (affects VRAM for SCNet models).
	chunkSize := 0
	chunkSizeParam := r.URL.Query().Get("chunk_size")
	if chunkSizeParam != "" {
		if cs, err := strconv.Atoi(chunkSizeParam); err == nil && cs > 0 {
			chunkSize = cs
		}
	}

	// Parse batch_size query parameter (affects VRAM for batched models).
	batchSize := 0
	batchSizeParam := r.URL.Query().Get("batch_size")
	if batchSizeParam != "" {
		if bs, err := strconv.Atoi(batchSizeParam); err == nil && bs > 0 {
			batchSize = bs
		}
	}

	// Parse demucs_segment query parameter (affects VRAM for Demucs models).
	demucsSegment := 0
	demucsSegmentParam := r.URL.Query().Get("demucs_segment")
	if demucsSegmentParam != "" {
		if ds, err := strconv.Atoi(demucsSegmentParam); err == nil && ds >= 0 {
			demucsSegment = ds
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

		vramMB := estimateVRAMMB(modelName, segmentSize, chunkSize, batchSize, demucsSegment)

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
