package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/starmito/onda/internal/cli"
)

const pipelineStatusFile = "/output/pipeline_status.json"

// PipelineStatus represents the current state of the pipeline (mirrored from removed pipeline pkg).
type PipelineStatus struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Step     string  `json:"step"`
	Song     string  `json:"song"`
	Elapsed  int     `json:"elapsed"`
	ETA      int     `json:"eta"`
	Error    string  `json:"error,omitempty"`
	Preset     string `json:"preset,omitempty"`
	VocalModel string `json:"vocal_model,omitempty"`
	StemModel  string `json:"stem_model,omitempty"`
	DrumsModel string `json:"drums_model,omitempty"`
	BassModel  string `json:"bass_model,omitempty"`
	OtherModel string `json:"other_model,omitempty"`
	Pitch      int    `json:"pitch,omitempty"`
	SegmentSize   int     `json:"segment_size"`
	Overlap       float64 `json:"overlap"`
	ChunkSize     int     `json:"chunk_size"`
	BatchSize     int     `json:"batch_size"`
	Device        string  `json:"device"`
	Shifts        int     `json:"shifts"`
	DemucsSegment int     `json:"demucs_segment"`
	Jobs          int     `json:"jobs"`
}

func pipelineStatusFilePath() string { return pipelineStatusFile }

const version = "v2.1.0-alpha"

// Server wraps the HTTP server with routes and middleware.
type Server struct {
	mux *http.ServeMux
}

// NewServer creates a new http.Server with CORS middleware and routes registered.
func NewServer(addr string) *http.Server {
	s := &Server{
		mux: http.NewServeMux(),
	}
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/models/list", s.handleModelsList)
	s.mux.HandleFunc("POST /api/models/download", s.handleModelsDownload)
	s.mux.HandleFunc("GET /api/models/download/status", s.handleModelsDownloadStatus)
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("/api/gpu", s.handleGPU)
	s.mux.HandleFunc("GET /api/gpu/info", s.handleGPUInfo)
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)
	s.mux.HandleFunc("/api/separate", s.handleSeparate)
	s.mux.HandleFunc("POST /api/upload", s.handleUpload)
	s.mux.HandleFunc("GET /api/files/{song}/{file}", s.handleFileServe)
	s.mux.HandleFunc("POST /api/backend/start", s.handleBackendStart)
	s.mux.HandleFunc("POST /api/backend/stop", s.handleBackendStop)
	s.mux.HandleFunc("POST /api/backend/restart", s.handleBackendRestart)
	s.mux.HandleFunc("DELETE /api/files/{song}", s.handleDeleteSong)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	// Frontend is served by Vite dev server separately; no static handler needed.

	return &http.Server{
		Addr:    addr,
		Handler: s.corsMiddleware(s.mux),
	}
}

// corsMiddleware adds CORS headers and handles OPTIONS preflight.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealth returns the health status of the Onda service.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	containerStatus, _ := checkDockerContainer()
	gpuAvailable, gpuInfo, _ := checkGPU()

	// ── Read frontend version ──
	frontendVersion := ""
	if data, err := os.ReadFile("/usr/share/nginx/html/VERSION"); err == nil {
		frontendVersion = strings.TrimSpace(string(data))
	}

	// ── Read pipeline version ──
	pipelineVersion := ""
	if data, err := os.ReadFile("/VERSION"); err == nil {
		pipelineVersion = strings.TrimSpace(string(data))
	}

	// ── Version mismatch detection ──
	var mismatches []map[string]string
	if frontendVersion != "" && frontendVersion != version {
		mismatches = append(mismatches, map[string]string{
			"component": "frontend",
			"expected":  version,
			"actual":    frontendVersion,
		})
	}
	if pipelineVersion != "" && pipelineVersion != version {
		mismatches = append(mismatches, map[string]string{
			"component": "pipeline",
			"expected":  version,
			"actual":    pipelineVersion,
		})
	}

	// ── Overall status ──
	status := "ok"
	backendOK := containerStatus == "running"
	if !backendOK || !gpuAvailable || len(mismatches) > 0 {
		status = "degraded"
	}

	// ── Build components ──
	backendDetail := "onda container " + containerStatus
	if containerStatus == "" {
		backendDetail = "onda container not found"
	}

	var gpuObj map[string]interface{}
	if gpuAvailable {
		gpuObj = map[string]interface{}{"ok": true, "detail": gpuInfo}
	} else {
		gpuObj = map[string]interface{}{"ok": false, "code": "E3", "detail": gpuInfo}
	}

	frontendOK := frontendVersion == version
	frontendObj := map[string]interface{}{
		"ok":      frontendOK,
		"version": frontendVersion,
	}
	if !frontendOK && frontendVersion != "" {
		frontendObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", version, frontendVersion)
	}

	pipelineOK := pipelineVersion == version
	pipelineObj := map[string]interface{}{
		"ok":      pipelineOK,
		"version": pipelineVersion,
	}
	if !pipelineOK && pipelineVersion != "" {
		pipelineObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", version, pipelineVersion)
	}

	mismatchObj := map[string]interface{}{"ok": true}
	if len(mismatches) > 0 {
		mismatchObj = map[string]interface{}{
			"ok":     false,
			"detail": mismatches,
		}
	}

	resp := map[string]interface{}{
		"status":  status,
		"version": version,
		"backend": map[string]interface{}{
			"ok":      backendOK,
			"detail":  backendDetail,
			"version": version,
		},
		"frontend":         frontendObj,
		"pipeline":         pipelineObj,
		"gpu":              gpuObj,
		"disk":             checkDisk(),
		"docker":           checkDocker(),
		"version_mismatch": mismatchObj,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleStatus returns the current pipeline progress from the JSON status file.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := readPipelineStatus()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "idle"})
		return
	}

	// Build response map so we can add files when done
	resp := map[string]interface{}{
		"status":   status.Status,
		"progress": status.Progress,
		"step":     status.Step,
		"song":     status.Song,
		"elapsed":  status.Elapsed,
		"eta":      status.ETA,
	}
	if status.Error != "" {
		resp["error"] = status.Error
	}

	// Include model information so the UI can show which model is being used
	if status.Preset != "" {
		resp["preset"] = status.Preset
	}
	if status.VocalModel != "" {
		resp["vocal_model"] = status.VocalModel
	}
	if status.StemModel != "" {
		resp["stem_model"] = status.StemModel
	}
	if status.DrumsModel != "" {
		resp["drums_model"] = status.DrumsModel
	}
	if status.BassModel != "" {
		resp["bass_model"] = status.BassModel
	}
	if status.Pitch != 0 {
		resp["pitch"] = status.Pitch
	}
	resp["segment_size"] = status.SegmentSize
	resp["overlap"] = status.Overlap
	resp["chunk_size"] = status.ChunkSize
	resp["batch_size"] = status.BatchSize
	resp["device"] = status.Device
	resp["shifts"] = status.Shifts
	resp["demucs_segment"] = status.DemucsSegment
	resp["jobs"] = status.Jobs

	// When pipeline is done, include the list of generated files
	if status.Status == "done" {
		projectRoot := findProjectRoot()
		outputDir := filepath.Join(projectRoot, "output", status.Song)
		entries, _ := os.ReadDir(outputDir)
		var fileList []map[string]string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			fileList = append(fileList, map[string]string{
				"name": name,
				"path": "/api/files/" + status.Song + "/" + name,
			})
		}
		if fileList != nil {
			resp["files"] = fileList
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// readPipelineStatus reads and parses the pipeline status JSON file.
func readPipelineStatus() (*PipelineStatus, error) {
	data, err := os.ReadFile(pipelineStatusFilePath())
	if err != nil {
		return nil, err
	}
	var s PipelineStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// handleEvents streams pipeline progress via Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Flush headers immediately so client receives them
	flusher.Flush()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastData string

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			data, err := os.ReadFile(pipelineStatusFilePath())
			if err != nil {
				continue
			}
			dataStr := string(data)
			if dataStr == lastData {
				continue
			}
			lastData = dataStr

			var status PipelineStatus
			if err := json.Unmarshal(data, &status); err != nil {
				continue
			}

			eventData, _ := json.Marshal(map[string]interface{}{
				"progress": status.Progress,
				"step":     status.Step,
				"status":   status.Status,
			})
			fmt.Fprintf(w, "data: %s\n\n", string(eventData))
			flusher.Flush()

			if status.Status == "done" || status.Status == "error" {
				return
			}
		}
	}
}

// handleGPU returns GPU availability and info from the Docker container.
func (s *Server) handleGPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	available, info, _ := checkGPU()

	resp := GPUPresenceResponse{
		Available: available,
		Info:      info,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleModels returns the available presets as JSON.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(cli.Presets)
}

// SeparateRequest is the JSON body for POST /api/separate.
type SeparateRequest struct {
	Preset     string `json:"preset"`
	Input      string `json:"input"`
	Output     string `json:"output,omitempty"`
	VocalModel string `json:"vocal_model,omitempty"`
	StemModel  string `json:"stem_model,omitempty"`
	Pitch      int    `json:"pitch,omitempty"`

	Viperx     bool     `json:"viperx"`
	ViperxKeep string   `json:"viperx_keep,omitempty"`
	Demucs     bool     `json:"demucs"`
	DemucsKeep []string `json:"demucs_keep,omitempty"`
}

// ModelConfig holds inference parameter configuration saved by the frontend.
type ModelConfig struct {
	SegmentSize int     `json:"segment_size"`
	Overlap     float64 `json:"overlap"`
	ChunkSize   int     `json:"chunk_size"`
	BatchSize   int     `json:"batch_size"`
	Device      string  `json:"device"`
	// Demucs PyTorch-specific parameters (only used when model is htdemucs_ft)
	Shifts  int `json:"shifts"`  // number of shift-averaging passes (default 1, paper uses 10)
	Segment int `json:"segment"` // demucs segment duration in seconds (0 = auto)
	Jobs    int `json:"jobs"`    // number of parallel workers (0 = auto)
}

// handleSeparate launches the audio separation pipeline asynchronously.
func (s *Server) handleSeparate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req SeparateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid JSON: %v", err),
		})
		return
	}

	// Validate preset
	_, ok := cli.Presets[req.Preset]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("unknown preset %q", req.Preset),
		})
		return
	}

	// Build pipeline.sh arguments for execution inside the 'onda' Docker container.
	// pipeline.sh, demucs, and inference_universal.py all live inside the container.
	song := strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))
	containerOutput := "/output/" + song

	// Pipeline args passed directly to the container's /pipeline.sh
	pipelineArgs := []string{}

	if req.Viperx {
		pipelineArgs = append(pipelineArgs, "--viperx")
		if req.ViperxKeep != "" {
			pipelineArgs = append(pipelineArgs, "--viperx-keep", req.ViperxKeep)
		}
	}
	if req.Demucs {
		pipelineArgs = append(pipelineArgs, "--demucs")
		if len(req.DemucsKeep) > 0 {
			pipelineArgs = append(pipelineArgs, "--demucs-keep", strings.Join(req.DemucsKeep, ","))
		}
	}
	if req.Pitch != 0 {
		pipelineArgs = append(pipelineArgs, "--rubberband", "--pitch", fmt.Sprintf("%d", req.Pitch))
	}

	// Pass model flags (vocal → ViperX, stem → Demucs) from the preset/request
	preset := cli.Presets[req.Preset]
	if req.VocalModel != "" {
		modelDir := resolveModelDir(req.VocalModel)
		if modelDir != "" {
			pipelineArgs = append(pipelineArgs, "--viperx-model", modelDir)
		}
	}
	stemModel := req.StemModel
	if stemModel == "" {
		stemModel = preset.StemModel
	}
	if stemModel != "" {
		pipelineArgs = append(pipelineArgs, "--demucs-model", stemModel)
	}

	// ── Dual config loading ──
	// ViperX config → segment_size, overlap, chunk_size, batch_size, device
	if req.VocalModel != "" {
		viperxCfg, err := loadModelConfig(req.VocalModel)
		if err == nil {
			pipelineArgs = append(pipelineArgs, "--segment-size", fmt.Sprintf("%d", viperxCfg.SegmentSize))
			pipelineArgs = append(pipelineArgs, "--overlap", fmt.Sprintf("%.2f", viperxCfg.Overlap))
			if viperxCfg.ChunkSize > 0 {
				pipelineArgs = append(pipelineArgs, "--chunk-size", fmt.Sprintf("%d", viperxCfg.ChunkSize))
			}
			if viperxCfg.BatchSize > 0 {
				pipelineArgs = append(pipelineArgs, "--batch-size", fmt.Sprintf("%d", viperxCfg.BatchSize))
			}
			if viperxCfg.Device != "" && viperxCfg.Device != "cuda" {
				pipelineArgs = append(pipelineArgs, "--device", viperxCfg.Device)
			}
		}
	}
	// Demucs config → shifts, segment, jobs, device (fallback)
	if stemModel != "" {
		demucsCfg, err := loadModelConfig(stemModel)
		if err == nil {
			if demucsCfg.Shifts > 1 {
				pipelineArgs = append(pipelineArgs, "--shifts", fmt.Sprintf("%d", demucsCfg.Shifts))
			}
			if demucsCfg.Segment > 0 {
				pipelineArgs = append(pipelineArgs, "--demucs-segment", fmt.Sprintf("%d", demucsCfg.Segment))
			}
			if demucsCfg.Jobs > 0 {
				pipelineArgs = append(pipelineArgs, "--jobs", fmt.Sprintf("%d", demucsCfg.Jobs))
			}
			if demucsCfg.Device != "" && demucsCfg.Device != "cuda" {
				pipelineArgs = append(pipelineArgs, "--device", demucsCfg.Device)
			}
		}
	}

	// Append output and input paths (container paths)
	pipelineArgs = append(pipelineArgs, "--output", containerOutput)
	pipelineArgs = append(pipelineArgs, req.Input)

	// Clean previous status file before launching new pipeline
	os.Remove(pipelineStatusFilePath())

	// Launch pipeline inside the 'onda' Docker container
	go func() {
		dockerArgs := append([]string{"exec", "onda", "bash", "/pipeline.sh"}, pipelineArgs...)
		cmd := exec.Command("docker", dockerArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Try to read existing JSON (written by pipeline.sh via trap);
			// update only status and error so flags are preserved.
			existing, readErr := os.ReadFile(pipelineStatusFilePath())
			var status map[string]interface{}
			if readErr == nil && json.Unmarshal(existing, &status) == nil {
				status["status"] = "error"
				status["error"] = strings.TrimSpace(string(out))
				if updated, marshalErr := json.Marshal(status); marshalErr == nil {
					os.WriteFile(pipelineStatusFilePath(), updated, 0644)
					return
				}
			}
			// Fallback: write minimal error status
			errStatus := fmt.Sprintf(`{"status":"error","step":"pipeline","progress":0,"song":"%s","elapsed":0,"eta":0,"error":%q}`+"\n",
				song, strings.TrimSpace(string(out)))
			os.WriteFile(pipelineStatusFilePath(), []byte(errStatus), 0644)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "started",
		"song":   song,
	})
}

// resolveModelDir resolves a model name to a directory path usable inside the
// Docker container. For Demucs PyTorch models (htdemucs_ft, htdemucs, etc.)
// the name is returned as-is (loaded by name, not path). For all other models
// (ViperX, Roformer, MDX, etc.), the model is looked up in listModels() and
// its /models/ path is translated to /app/models/ (the container's mount).
func resolveModelDir(name string) string {
	if name == "htdemucs_ft" || (strings.HasPrefix(name, "htdemucs") && !strings.Contains(name, ".onnx")) {
		return name
	}
	models := listModels()
	for _, m := range models.Models {
		if m.Name == name || m.DisplayName == name {
			dir := filepath.Dir(m.Path)
			return strings.Replace(dir, "/models/", "/app/models/", 1)
		}
	}
	return ""
}

// modelConfigDefaults returns default inference parameters.
func modelConfigDefaults() ModelConfig {
	return ModelConfig{
		SegmentSize: 256,
		Overlap:     0.25,
		ChunkSize:   0,
		BatchSize:   0,
		Device:      "cuda",
		Shifts:      1,
		Segment:     0,
		Jobs:        0,
	}
}

// sanitizeModelName replaces path separators and other unsafe chars for use in filenames.
func sanitizeModelName(name string) string {
	r := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"..", "_",
		" ", "_",
	)
	return r.Replace(name)
}

// modelConfigDir returns the path to the per-model config directory (ensures it exists).
func modelConfigDir() string {
	projectRoot := findProjectRoot()
	dir := filepath.Join(projectRoot, "model_configs")
	os.MkdirAll(dir, 0755)
	return dir
}

// modelConfigPath builds the JSON file path for a given model name.
func modelConfigPath(name string) string {
	safe := sanitizeModelName(name)
	return filepath.Join(modelConfigDir(), safe+".json")
}

// loadModelConfig reads per-model config, returning defaults if the file doesn't exist.
func loadModelConfig(name string) (ModelConfig, error) {
	if name == "" {
		return modelConfigDefaults(), nil
	}
	path := modelConfigPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return modelConfigDefaults(), nil
	}
	var cfg ModelConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return modelConfigDefaults(), nil
	}
	return cfg, nil
}

// applyModelConfigToArgs appends inference flags from a ModelConfig to an args slice.
func applyModelConfigToArgs(args []string, cfg ModelConfig) []string {
	args = append(args, "--segment-size", fmt.Sprintf("%d", cfg.SegmentSize))
	args = append(args, "--overlap", fmt.Sprintf("%.2f", cfg.Overlap))
	if cfg.ChunkSize > 0 {
		args = append(args, "--chunk-size", fmt.Sprintf("%d", cfg.ChunkSize))
	}
	if cfg.BatchSize > 0 {
		args = append(args, "--batch-size", fmt.Sprintf("%d", cfg.BatchSize))
	}
	if cfg.Device != "" {
		args = append(args, "--device", cfg.Device)
	}
	// Demucs PyTorch-specific flags (only meaningful for htdemucs_ft)
	if cfg.Shifts > 0 {
		args = append(args, "--shifts", fmt.Sprintf("%d", cfg.Shifts))
	}
	if cfg.Segment > 0 {
		args = append(args, "--demucs-segment", fmt.Sprintf("%d", cfg.Segment))
	}
	if cfg.Jobs > 0 {
		args = append(args, "--jobs", fmt.Sprintf("%d", cfg.Jobs))
	}
	return args
}

// handleModelsConfig saves or retrieves per-model inference configuration.
// GET  /api/models/{name}/config  — returns the config for a model (defaults if none saved)
// POST /api/models/{name}/config  — saves the config for a model
func (s *Server) handleModelsConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.PathValue("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing model name"})
		return
	}

	if r.Method == http.MethodGet {
		cfg, _ := loadModelConfig(name)
		json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg ModelConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}

		// Validate
		if cfg.SegmentSize <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "segment_size must be > 0"})
			return
		}
		if cfg.Overlap < 0 || cfg.Overlap >= 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "overlap must be >= 0 and < 1"})
			return
		}
		if cfg.Device != "cpu" && cfg.Device != "cuda" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "device must be 'cpu' or 'cuda'"})
			return
		}
		if cfg.Shifts < 0 || cfg.Shifts > 20 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "shifts must be between 0 and 20"})
			return
		}
		if cfg.Segment < 0 || cfg.Segment > 60 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "demucs segment must be between 0 and 60"})
			return
		}
		if cfg.Jobs < 0 || cfg.Jobs > 8 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "jobs must be between 0 and 8"})
			return
		}

		path := modelConfigPath(name)
		data, err := json.Marshal(cfg)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to serialize config"})
			return
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to write config file"})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"ok":     "true",
			"detail": fmt.Sprintf("config saved for model %s", name),
		})
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
	json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
}

// handleUpload accepts a multipart file upload and saves it to disk.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Limit to 500MB
	r.ParseMultipartForm(500 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		return
	}
	defer file.Close()

	// Determine input directory: prefer /home/starmito/projects/onda/input,
	// fall back to a temp dir if it doesn't exist.
	projectRoot := findProjectRoot()
	inputDir := filepath.Join(projectRoot, "input")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		os.MkdirAll(inputDir, 0755)
	}

	destPath := filepath.Join(inputDir, header.Filename)
	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file"})
		return
	}

	// The path inside the container is /input/filename
	containerPath := "/input/" + header.Filename
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": containerPath})
}

// handleFileServe serves generated output files from the project output directory.
func (s *Server) handleFileServe(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	file := r.PathValue("file")

	// Prevent directory traversal
	song = filepath.Clean(song)
	file = filepath.Clean(file)

	projectRoot := findProjectRoot()
	filePath := filepath.Join(projectRoot, "output", song, file)

	// Verify the file is inside the output directory
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absPath)
}

// handleBackendStart starts the Onda Docker container.
func (s *Server) handleBackendStart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "start", dockerContainer)
	out, err := cmd.CombinedOutput()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		detail := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = fmt.Sprintf("exit code %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(out)))
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"detail":  detail,
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"detail":  "Backend started",
	})
}

// handleBackendStop stops the Onda Docker container.
func (s *Server) handleBackendStop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "stop", dockerContainer)
	out, err := cmd.CombinedOutput()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		detail := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = fmt.Sprintf("exit code %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(out)))
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"detail":  detail,
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"detail":  "Backend stopped",
	})
}

// handleBackendRestart restarts the Onda Docker container.
func (s *Server) handleBackendRestart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "restart", dockerContainer)
	out, err := cmd.CombinedOutput()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		detail := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = fmt.Sprintf("exit code %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(out)))
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"detail":  detail,
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"detail":  "Backend restarted",
	})
}

// handleDeleteSong deletes the entire output directory for a song.
func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	song = filepath.Clean(song)

	projectRoot := findProjectRoot()
	dirPath := filepath.Join(projectRoot, "output", song)

	// Verify inside output/
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(dirPath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "song not found"})
		return
	}

	if err := os.RemoveAll(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Clear status file if it references the deleted song
	if st, err := readPipelineStatus(); err == nil && st.Song == song {
		os.Remove(pipelineStatusFilePath())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "song": song})
}

// handleDeleteFile deletes a single file within the output directory.
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing file parameter"})
		return
	}

	file = filepath.Clean(file)
	projectRoot := findProjectRoot()
	filePath := filepath.Join(projectRoot, "output", file)

	// Verify inside output/
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Don't clear the status file for single-stem deletion; the pipeline is still valid.
	// Only handleDeleteSong (the whole directory delete) clears the status.

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": file})
}

// findProjectRoot walks up from the current directory until it finds a VERSION file,
// then returns that directory. If ONDA_ROOT is set, it uses that directly.
// Returns "." if not found.
func findProjectRoot() string {
	if root := os.Getenv("ONDA_ROOT"); root != "" {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "VERSION")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}
