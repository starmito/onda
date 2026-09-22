package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
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
	UsableByTorch     bool   `json:"usable_by_torch"`
	TorchInfo         string `json:"torch_info,omitempty"`
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
	Reliable        bool             `json:"reliable"`
	Warning         string           `json:"warning,omitempty"`
}

// defaultVRAMMB is used when a model is not catalogued by estimateVRAMMB.
const defaultVRAMMB = 2000

// vramHeadroomMargin is the safety margin applied on top of the model estimate.
const vramHeadroomMargin = 1.20 // +20%

// VRAMConfig holds the inference parameters that influence VRAM usage. It is
// used both for analytical estimates and for matching measured peaks.
type VRAMConfig struct {
	SegmentSize   int
	ChunkSize     int
	BatchSize     int
	DemucsSegment int
	Duration      int // audio duration in seconds (0 = unknown)
}

// measuredVRAMPeak stores an observed real peak for a model/step combination
// under specific inference settings. When a match exists it overrides the
// analytical estimator so the UI can show a number grounded in reality.
type measuredVRAMPeak struct {
	ModelName string
	StepType  string
	PeakMB    int
	ChunkSize int // 0 = whole-song / not relevant
	BatchSize int // 0 = not relevant
	Duration  int // seconds, 0 = not relevant
}

// measuredVRAMPeaks is the live table of observed VRAM peaks. Values are
// conservative maxima measured on real jobs; when a match exists they override
// the analytical estimator so the guard reflects reality instead of an
// optimistic formula.
var measuredVRAMPeaks = []measuredVRAMPeak{
	// Measured 2026-09-19: htdemucs_ft with --shifts 20 --segment 7 -j 8
	// stays around 1.5 GiB after the vocal model is released.
	{ModelName: "htdemucs_ft", StepType: "demucs", PeakMB: 1500, ChunkSize: 0, BatchSize: 0, Duration: 0},
	// Measured 2026-09-21: BS_Roformer_SW_6stem, 30 s stereo 44.1 kHz,
	// chunk_size=485100 samples, overlap=4 (step=121275), batch_size=1.
	{ModelName: "BS_Roformer_SW_6stem", StepType: "vocal", PeakMB: 2803, ChunkSize: 485100, BatchSize: 1, Duration: 30},
	// Measured 2026-09-21: SCNet_MUSDB18 with chunk_size=0 (whole song),
	// 296 s stereo 44.1 kHz, batch_size=1.
	{ModelName: "SCNet_MUSDB18", StepType: "scnet", PeakMB: 6372, ChunkSize: 0, BatchSize: 1, Duration: 296},
}

// findMeasuredVRAMPeak returns the measured peak in MiB for a model/step
// combination when the requested settings match a real measurement exactly.
// It returns 0 otherwise so the analytical estimator can run (and warn).
func findMeasuredVRAMPeak(modelName, stepType string, cfg VRAMConfig) int {
	lowerModel := strings.ToLower(modelName)
	lowerStep := strings.ToLower(stepType)
	for _, m := range measuredVRAMPeaks {
		if strings.ToLower(m.ModelName) != lowerModel || strings.ToLower(m.StepType) != lowerStep {
			continue
		}
		if m.ChunkSize > 0 && cfg.ChunkSize != m.ChunkSize {
			continue
		}
		if m.BatchSize > 0 && cfg.BatchSize != m.BatchSize {
			continue
		}
		// Duration only matters for whole-song (chunk_size=0) measurements.
		// Chunked measurements are representative regardless of total song length.
		if m.Duration > 0 && cfg.Duration != m.Duration && m.ChunkSize <= 0 {
			continue
		}
		return m.PeakMB
	}
	return 0
}

// resolveModelName returns the most useful model name available. If modelName
// is empty or "unknown" it falls back to fallbackModel; if neither is usable it
// returns modelName (which may be "unknown") so the message stays honest.
func resolveModelName(modelName, fallbackModel string) string {
	if modelName != "" && !strings.EqualFold(modelName, "unknown") {
		return modelName
	}
	if fallbackModel != "" && !strings.EqualFold(fallbackModel, "unknown") {
		return fallbackModel
	}
	if modelName != "" {
		return modelName
	}
	return "unknown"
}

// checkVramHeadroom returns whether freeMB can accommodate the peak VRAM
// expected for modelName/stepType. It prefers a measured peak when one exists;
// otherwise it falls back to the analytical estimator.
//
// The safety margin is applied only when it physically fits on the card
// (base * margin <= totalMB). When it does not fit, the requirement is reduced
// to base and a warning is returned so the caller can log that the job is
// proceeding without a safety margin.
//
// It returns: ok, requiredMB, blockReason, warning. Only one of blockReason and
// warning is non-empty: a block reason when ok is false, or a warning when ok
// is true but the margin could not be applied.
func checkVramHeadroom(freeMB, totalMB int, modelName, stepType string, cfg VRAMConfig, fallbackModel string) (bool, int, string, string) {
	effectiveModel := resolveModelName(modelName, fallbackModel)

	var base int
	if peak := findMeasuredVRAMPeak(effectiveModel, stepType, cfg); peak > 0 {
		base = peak
	} else {
		base = estimateVRAMMB(effectiveModel, cfg.SegmentSize, cfg.ChunkSize, cfg.BatchSize, cfg.DemucsSegment, cfg.Duration)
	}

	withMargin := int(math.Round(float64(base) * vramHeadroomMargin))
	required := withMargin
	marginApplied := true
	if totalMB > 0 && withMargin > totalMB {
		required = base
		marginApplied = false
	}

	if freeMB >= required {
		if !marginApplied {
			warning := fmt.Sprintf("VRAM tight: model %q (step %q) needs ~%d MiB and the card has %d MiB total - proceeding without safety margin",
				effectiveModel, stepType, base, totalMB)
			return true, required, "", warning
		}
		return true, required, "", ""
	}

	var reason string
	if marginApplied {
		reason = fmt.Sprintf("insufficient VRAM: model %q (step %q) needs ~%d MiB (with %.0f%% margin), only %d MiB free",
			effectiveModel, stepType, required, (vramHeadroomMargin-1.0)*100, freeMB)
	} else {
		reason = fmt.Sprintf("insufficient VRAM: model %q (step %q) needs ~%d MiB, only %d MiB free",
			effectiveModel, stepType, required, freeMB)
	}
	return false, required, reason, ""
}

// fallbackAvailableVRAMMB is used when GPU info cannot be obtained.
// It is kept only as a last-resort fallback for the VRAM calculator; getGPUInfo
// no longer reports this hardcoded value as real GPU memory.
const fallbackAvailableVRAMMB = 16311

// parseNvidiaSmiMemory parses the CSV output of nvidia-smi for
// memory.total,memory.used,memory.free and returns the three values in MiB.
// It tolerates surrounding whitespace and the "nounits" suffix.
func parseNvidiaSmiMemory(out string) (int, int, int, error) {
	out = strings.TrimSpace(out)
	// Some nvidia-smi versions emit a trailing line such as "[Not Supported]"
	// or an empty line. Use only the first non-empty line.
	lines := strings.Split(out, "\n")
	var line string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			line = l
			break
		}
	}
	if line == "" {
		return 0, 0, 0, fmt.Errorf("empty nvidia-smi output")
	}

	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected nvidia-smi format: %q", line)
	}

	parse := func(s string) (int, error) {
		s = strings.TrimSpace(s)
		// Strip the " MiB" / " MB" unit suffix if present.
		if idx := strings.Index(s, " "); idx >= 0 {
			s = s[:idx]
		}
		return strconv.Atoi(s)
	}

	total, err := parse(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse total memory: %w", err)
	}
	used, err := parse(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse used memory: %w", err)
	}
	free, err := parse(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse free memory: %w", err)
	}

	return total, used, free, nil
}

// getGPUInfoNvidiaSmi queries GPU details via nvidia-smi. It returns real VRAM
// and utilization/temperature when available. This is the primary source of
// truth because the onda container has torch without CUDA.
func getGPUInfoNvidiaSmi() GPUInfoResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=memory.total,memory.used,memory.free,utilization.gpu,temperature.gpu",
		"--format=csv,noheader,nounits")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return GPUInfoResponse{
			OK:      false,
			Error:   fmt.Sprintf("nvidia-smi failed: %v: %s", err, strings.TrimSpace(stderr.String())),
			Runtime: "nvidia-smi",
		}
	}

	total, used, free, err := parseNvidiaSmiMemory(string(out))
	if err != nil {
		return GPUInfoResponse{
			OK:      false,
			Error:   fmt.Sprintf("failed to parse nvidia-smi output: %v", err),
			Runtime: "nvidia-smi",
		}
	}

	// The remaining fields are optional and may be "[Not Supported]".
	utilization := 0
	temperature := 0
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) >= 4 {
		if v, pErr := strconv.Atoi(strings.TrimSpace(parts[3])); pErr == nil {
			utilization = v
		}
	}
	if len(parts) >= 5 {
		if v, pErr := strconv.Atoi(strings.TrimSpace(parts[4])); pErr == nil {
			temperature = v
		}
	}

	return GPUInfoResponse{
		Name:              "nvidia-smi",
		Runtime:           "nvidia-smi",
		OK:                true,
		VRAMTotalMB:       total,
		VRAMUsedMB:        used,
		VRAMFreeMB:        free,
		UtilizationGPUPct: utilization,
		TemperatureC:      temperature,
	}
}

// estimateVRAMMB returns the analytical VRAM peak in MB for a model name.
// It does NOT use measured peaks; callers that want measured overrides should
// call findMeasuredVRAMPeak first. duration is the audio length in seconds
// (0 = unknown) and matters for whole-song models like SCNet when chunk_size is 0.
func estimateVRAMMB(modelName string, segmentSize, chunkSize, batchSize, demucsSegment, duration int) int {
	lower := strings.ToLower(modelName)

	// MDX / MDXNet / ONNX: empirical peak depends on dim_t (derived from
	// segment_size) and scales with batch size. Checked before Vocal/Roformer
	// because names like "MDXNet_Vocals" contain the "vocal" substring but are
	// still MDX-family models.
	if strings.Contains(lower, "mdx") || strings.Contains(lower, "onnx") {
		return mdxEstimateVRAMMB(segmentSize, batchSize)
	}

	// SCNet: empirical peak scales with chunk_size and batch size when chunked.
	// With chunk_size=0 the whole song is processed at once, so the peak scales
	// with audio duration.
	if strings.Contains(lower, "scnet") {
		return scnetEstimateVRAMMB(chunkSize, batchSize, duration)
	}

	// Roformer / ViperX / Vocal: measured peak with real long audio:
	// pico ≈ 1100 + (106 + 6.72*segment_size) * effective_batch.
	// batch_size is multiplicative because chunks are processed in parallel,
	// but only when the audio is long enough to feed that many batches. When
	// the duration (or an explicit chunk_size) is short we cap the batch count
	// so short songs are not rejected with an impossible peak.
	if isVocalOrRoformer(lower) {
		return roformerEstimateVRAMMB(segmentSize, chunkSize, batchSize, duration)
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

// roformerEstimateVRAMMB returns an analytical VRAM peak for RoFormer/Vocal
// models. The base formula is calibrated for a full batch; when the audio
// duration (or an explicit chunk_size in samples) is known, the number of
// batches that can actually be in flight is capped so short clips do not
// inherit the peak of a long song.
func roformerEstimateVRAMMB(segmentSize, chunkSize, batchSize, duration int) int {
	b := batchSize
	if b < 1 {
		b = 1
	}
	// Calibrated formula: fixed overhead + per-batch term.
	baseFixed := 1100.0
	basePerBatch := 106.0 + 6.72*float64(segmentSize)

	// If we have duration or chunk_size information, limit the effective batch.
	effectiveDurationSec := 0
	if duration > 0 {
		effectiveDurationSec = duration
	}
	if chunkSize > 0 {
		// chunk_size is in samples at 44.1 kHz.
		chunkSec := chunkSize / 44100
		if chunkSec < 1 {
			chunkSec = 1
		}
		if effectiveDurationSec == 0 || chunkSec < effectiveDurationSec {
			effectiveDurationSec = chunkSec
		}
	}

	if effectiveDurationSec > 0 && segmentSize > 0 {
		// Approximate time covered by one model step. Typical values:
		// hop_length = 512, overlap = 4, sample_rate = 44100.
		segmentDurationSec := float64(segmentSize) * 512.0 / (4.0 * 44100.0)
		if segmentDurationSec <= 0 {
			segmentDurationSec = 1.0
		}
		maxBatches := int(math.Ceil(float64(effectiveDurationSec) / segmentDurationSec))
		if maxBatches < 1 {
			maxBatches = 1
		}
		if maxBatches < b {
			b = maxBatches
		}
	}

	return int(math.Round(baseFixed + basePerBatch*float64(b)))
}

// ramHeadroomMargin is the safety margin applied on top of the RAM estimate.
const ramHeadroomMargin = 1.15 // +15%

// measuredRAMPeak stores an observed real RAM peak for a model/step combination.
type measuredRAMPeak struct {
	ModelName string
	StepType  string
	PeakMB    int
}

// measuredRAMPeaks is the live table of observed host RAM peaks. When a match
// exists it is used directly; otherwise estimateRAMMB provides a conservative
// fallback.
var measuredRAMPeaks = []measuredRAMPeak{
	// Measured 2026-09-19 on a real job: BS_Roformer_Viperx (preset
	// "Eliminador de Voz", 1 step, chunk 35 s) on a 60 s stereo 44.1 kHz
	// synthetic clip. The inference python process peaked at 2,094,200 KB
	// RSS ≈ 2,045 MiB, sampled at 1 Hz from the host during the whole
	// pipeline. Because processing is chunked, the RAM usage does not grow
	// with input duration.
	{ModelName: "BS_Roformer_Viperx", StepType: "vocal", PeakMB: 2045},
	// Conservative observed host RAM usage for long audio jobs.
	{ModelName: "htdemucs_ft", StepType: "demucs", PeakMB: 4096},
}

// findMeasuredRAMPeak returns the measured RAM peak in MiB for a model/step
// combination, or 0 when no measurement is available.
func findMeasuredRAMPeak(modelName, stepType string) int {
	lowerModel := strings.ToLower(modelName)
	lowerStep := strings.ToLower(stepType)
	for _, m := range measuredRAMPeaks {
		if strings.ToLower(m.ModelName) == lowerModel && strings.ToLower(m.StepType) == lowerStep {
			return m.PeakMB
		}
	}
	return 0
}

// estimateRAMMB returns a conservative host RAM estimate in MB for a model
// name and step type. It is used as a fallback when no measured peak exists.
func estimateRAMMB(modelName, stepType string) int {
	lower := strings.ToLower(modelName)
	if isDemucsModel(lower) || stepType == "demucs" {
		return 4096
	}
	if isVocalOrRoformer(lower) {
		// Conservative fallback for the Vocal/Roformer family when no
		// measured peak exists. The only measured case so far is
		// BS_Roformer_Viperx at 2,045 MiB (60 s, chunk 35 s). We keep a
		// ~50 % headroom above that measured peak (3,072 MiB) so the guard
		// stays conservative for unknown models in the same family without
		// reintroducing the previous 6,144 MiB heuristic that produced false
		// warnings with ~4.7 GB of free RAM.
		return 3072
	}
	if strings.Contains(lower, "mdx") || strings.Contains(lower, "onnx") {
		return 6144
	}
	if strings.Contains(lower, "scnet") {
		return 4096
	}
	return 4096
}

// ramRequiredMB returns the host RAM required for a model/step, preferring a
// measured peak and applying the RAM safety margin.
func ramRequiredMB(modelName, stepType string) int {
	var base int
	if peak := findMeasuredRAMPeak(modelName, stepType); peak > 0 {
		base = peak
	} else {
		base = estimateRAMMB(modelName, stepType)
	}
	return int(math.Round(float64(base) * ramHeadroomMargin))
}

// checkRamHeadroom returns whether availableMB can accommodate the host RAM
// expected for modelName/stepType. It returns the required memory and a
// human-readable warning when there is not enough headroom. The RAM guard is
// advisory only: callers log the warning but still proceed with the job.
func checkRamHeadroom(availableMB int, modelName, stepType string) (bool, int, string) {
	needed := ramRequiredMB(modelName, stepType)
	if availableMB >= needed {
		return true, needed, ""
	}
	reason := fmt.Sprintf("RAM low: model %q (step %q) estimated ~%d MiB, only %d MiB available - proceeding anyway",
		modelName, stepType, needed, availableMB)
	return false, needed, reason
}

// getHostMemoryInfo reads /proc/meminfo and returns total and available RAM
// in MiB. It returns ok=false when /proc/meminfo cannot be parsed.
func getHostMemoryInfo() (totalMB int, availableMB int, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		valKB, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalMB = valKB / 1024
		case "MemAvailable:":
			availableMB = valKB / 1024
		}
	}
	if availableMB == 0 && totalMB > 0 {
		// /proc/meminfo very old kernels may lack MemAvailable. Use a
		// conservative fallback so the guard does not silently pass.
		availableMB = totalMB / 4
	}
	return totalMB, availableMB, totalMB > 0
}

// hostMemoryProvider is the function used by the pipeline workers to query
// host RAM. It is a variable so tests can substitute a mock implementation.
var hostMemoryProvider = getHostMemoryInfo

// getGPUInfo queries GPU details. It prefers nvidia-smi because the onda
// container has torch without CUDA. If nvidia-smi is unavailable it returns an
// explicit failure with no fabricated VRAM numbers.
func getGPUInfo() GPUInfoResponse {
	info := getGPUInfoNvidiaSmi()
	if info.OK {
		return info
	}
	return GPUInfoResponse{
		OK:      false,
		Error:   info.Error,
		Runtime: "unknown",
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

	// Expose whether torch actually sees CUDA, because nvidia-smi may report a
	// card while the installed torch runtime cannot use it.
	info.UsableByTorch, info.TorchInfo, _ = checkGPU()

	w.Header().Set("Content-Type", "application/json")
	if !info.OK && !info.UsableByTorch {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(info)
}

// mdxDimTPoints are today's measured MDX23C dim_t values ordered increasingly.
// segment_size is now dim_t directly (UI "Segment Size" == dim_t).
var mdxDimTPoints = []int{417, 801, 1569, 2337}

// mdxVRAMPoints are the measured VRAM peaks (MiB, batch 1) for mdxDimTPoints.
var mdxVRAMPoints = []int{2080, 2478, 6748, 9872}

// mdxEstimateVRAMMB returns the empirical MDX-family VRAM peak in MB.
// segment_size is dim_t directly; it interpolates the base peak from the
// measured table and multiplies by batch size.
func mdxEstimateVRAMMB(segmentSize, batchSize int) int {
	b := batchSize
	if b < 1 {
		b = 1
	}
	base := interpolatePeak(segmentSize, mdxDimTPoints, mdxVRAMPoints)
	return base * b
}

// scnetChunkPoints are today's measured SCNet chunk_size values (samples)
// for 100-second audio, ordered increasingly.
var scnetChunkPoints = []int{242550, 485100, 970200}

// scnetVRAMPoints are the measured VRAM peaks (MiB, batch 1) for scnetChunkPoints.
var scnetVRAMPoints = []int{600, 948, 1762}

// scnetWholeSongReference is the measured peak for a whole-song SCNet run.
const scnetWholeSongReferenceMB = 6372
const scnetWholeSongReferenceDuration = 296 // seconds

// scnetEstimateVRAMMB returns the empirical SCNet VRAM peak in MB.
// It uses a base linear in chunk_size (interpolated from batch-1 measurements)
// and multiplies by batch size. Overlap is ignored.
// When chunk_size is 0 the whole song is processed at once, so the peak scales
// with audio duration.
func scnetEstimateVRAMMB(chunkSize, batchSize, duration int) int {
	b := batchSize
	if b < 1 {
		b = 1
	}
	if chunkSize <= 0 {
		if duration <= 0 {
			duration = scnetWholeSongReferenceDuration
		}
		base := float64(scnetWholeSongReferenceMB) * float64(duration) / float64(scnetWholeSongReferenceDuration)
		return int(math.Round(base)) * b
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
	patterns := []string{"vocal", "melband", "polarformer", "roformer"}
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

// classifyModelType returns a canonical model type for VRAM reporting.
// It overrides any caller-provided type so the response always reflects the
// model family (vocal/roformer, demucs, mdx, mdxnet, scnet).
func classifyModelType(modelName string) string {
	lower := strings.ToLower(modelName)
	if isDemucsModel(lower) {
		return "demucs"
	}
	if strings.Contains(lower, "mdxnet") || strings.Contains(lower, "onnx") {
		return "mdxnet"
	}
	if strings.Contains(lower, "mdx") {
		return "mdx"
	}
	if strings.Contains(lower, "scnet") {
		return "scnet"
	}
	if isVocalOrRoformer(lower) {
		return "vocal"
	}
	return "unknown"
}

// vramEstimateReliable reports whether an analytical estimate is trustworthy
// for the given model family and configuration, and returns a human-readable
// warning when it is not.
func vramEstimateReliable(modelType string, cfg VRAMConfig) (bool, string) {
	switch modelType {
	case "scnet":
		if cfg.ChunkSize <= 0 {
			if cfg.Duration <= 0 {
				return false, "SCNet con chunk_size=0 procesa la canción entera: la estimación asume ~300 s."
			}
			if cfg.Duration > scnetWholeSongReferenceDuration {
				return false, fmt.Sprintf("SCNet con chunk_size=0: el pico real crece con la duración; la medición de referencia es de %d s.", scnetWholeSongReferenceDuration)
			}
		} else if cfg.Duration > 120 {
			return false, "La fórmula de SCNet se midió con clips de ~100 s; para audio muy largo el pico puede subir."
		}
	case "vocal":
		if cfg.ChunkSize > 0 {
			return false, "La estimación no tiene en cuenta chunk_size, que puede cambiar mucho el pico real de VRAM."
		}
		if cfg.Duration > 0 {
			return false, "La estimación tiene en cuenta la duración del audio; para canciones largas el pico real puede ser mayor."
		}
	case "mdx", "mdxnet":
		if cfg.SegmentSize == 0 {
			return false, "Falta segment_size: se usa una estimación genérica que puede no coincidir con este modelo."
		}
	}
	return true, ""
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

	// Parse chunk_size query parameter (affects VRAM for SCNet and Roformer models).
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

	// Parse duration query parameter in seconds (affects whole-song models like SCNet).
	duration := 0
	if d := r.URL.Query().Get("duration"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 {
			duration = v
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

	cfg := VRAMConfig{
		SegmentSize:   segmentSize,
		ChunkSize:     chunkSize,
		BatchSize:     batchSize,
		DemucsSegment: demucsSegment,
		Duration:      duration,
	}

	var models []VRAMModelEntry
	totalVRAM := 0
	allReliable := true
	var warnings []string

	// Split by comma: "vocal=melband_kj,stems=htdemucs_ft"
	pairs := strings.Split(modelsParam, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		// Split by first "=". If no "=", treat the entire string as a model name.
		eqIdx := strings.Index(pair, "=")
		var modelName string
		if eqIdx < 0 {
			modelName = pair
		} else {
			modelName = strings.TrimSpace(pair[eqIdx+1:])
		}
		modelName = strings.TrimSpace(modelName)
		modelType := classifyModelType(modelName)

		var vramMB int
		if peak := findMeasuredVRAMPeak(modelName, modelType, cfg); peak > 0 {
			vramMB = peak
		} else {
			vramMB = estimateVRAMMB(modelName, segmentSize, chunkSize, batchSize, demucsSegment, duration)
			if ok, w := vramEstimateReliable(modelType, cfg); !ok {
				allReliable = false
				if w != "" {
					warnings = append(warnings, modelName+": "+w)
				}
			}
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
		Reliable:        allReliable,
	}
	if !allReliable && len(warnings) > 0 {
		resp.Warning = strings.Join(warnings, " ")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
