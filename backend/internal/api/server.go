package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/starmito/onda/internal/cli"
)

// FileEntry describes a generated stem file.
type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// UVRModelEntry represents a model entry from the UVR catalog (uvr_models.json).
type UVRModelEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	SizeMB      int64  `json:"size_mb"`
	Description string `json:"description,omitempty"`
	Downloaded  bool   `json:"downloaded"`
	Source      string `json:"source"`
}

// JobRequest represents a queued separation job.
type JobRequest struct {
	Song   string          `json:"song"`
	Args   []string        `json:"args"`
	Config SeparateRequest `json:"config"`
}

// JobState tracks the status of a separation job.
type JobState struct {
	Song     string      `json:"song"`
	Status   string      `json:"status"` // waiting, processing, done, error
	Progress int         `json:"progress"`
	Error    string      `json:"error,omitempty"`
	Files    []FileEntry `json:"files,omitempty"`
	Index    int         `json:"index"`
}

// Server wraps the HTTP server with routes, middleware, and a sequential job queue.
type Server struct {
	mux       *http.ServeMux
	jobQueue  chan JobRequest
	jobs      map[string]*JobState
	jobsMu    sync.RWMutex
	nextIndex int
}

// NewServer creates a new http.Server with CORS middleware and routes registered.
func NewServer(addr string) *http.Server {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 20),
		jobs:     make(map[string]*JobState),
	}
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.mux.HandleFunc("GET /api/results", s.handleResults)
	s.mux.HandleFunc("GET /api/inputs", s.handleInputs)
	// Presets API (must be BEFORE /api/models catch-all)
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("POST /api/presets", s.handleSavePreset)
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/models/list", s.handleModelsList)
	s.mux.HandleFunc("POST /api/models/download", s.handleModelsDownload)
	s.mux.HandleFunc("GET /api/models/download/status", s.handleModelsDownloadStatus)
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("GET /api/models/catalog", s.handleModelsCatalog)
	s.mux.HandleFunc("GET /api/models/catalog/hf", s.handleModelsCatalogHF)
	s.mux.HandleFunc("/api/gpu", s.handleGPU)
	s.mux.HandleFunc("GET /api/gpu/info", s.handleGPUInfo)
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)
	s.mux.HandleFunc("/api/separate", s.handleSeparate)
	s.mux.HandleFunc("POST /api/pitch", s.handlePitchShift)
	s.mux.HandleFunc("GET /api/pitch/{song}", s.handleListPitchSubgroups)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}", s.handleDeletePitchSubgroup)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}/{file}", s.handleDeletePitchStem)
	s.mux.HandleFunc("GET /api/pitch/files/{song}/{pitch}/{file}", s.handlePitchFileServe)
	s.mux.HandleFunc("POST /api/upload", s.handleUpload)
	s.mux.HandleFunc("GET /api/files/{song}/{file}", s.handleFileServe)
	s.mux.HandleFunc("POST /api/backend/start", s.handleBackendStart)
	s.mux.HandleFunc("POST /api/backend/stop", s.handleBackendStop)
	s.mux.HandleFunc("POST /api/backend/restart", s.handleBackendRestart)
	s.mux.HandleFunc("DELETE /api/files/{song}", s.handleDeleteSong)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	s.mux.HandleFunc("DELETE /api/models/{name}", s.handleDeleteModel)
	s.mux.HandleFunc("DELETE /api/inputs/{name}", s.handleDeleteInput)
	// Frontend is served by Vite dev server separately; no static handler needed.

	go s.worker()

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
	if frontendVersion != "" && frontendVersion != Version {
		mismatches = append(mismatches, map[string]string{
			"component": "frontend",
			"expected":  Version,
			"actual":    frontendVersion,
		})
	}
	if pipelineVersion != "" && pipelineVersion != Version {
		mismatches = append(mismatches, map[string]string{
			"component": "pipeline",
			"expected":  Version,
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

	frontendOK := frontendVersion == Version
	frontendObj := map[string]interface{}{
		"ok":      frontendOK,
		"version": frontendVersion,
	}
	if !frontendOK && frontendVersion != "" {
		frontendObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", Version, frontendVersion)
	}

	pipelineOK := pipelineVersion == Version
	pipelineObj := map[string]interface{}{
		"ok":      pipelineOK,
		"version": pipelineVersion,
	}
	if !pipelineOK && pipelineVersion != "" {
		pipelineObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", Version, pipelineVersion)
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
		"version": Version,
		"backend": map[string]interface{}{
			"ok":      backendOK,
			"detail":  backendDetail,
			"version": Version,
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

// ResultsGroup groups stem files by song.
type ResultsGroup struct {
	Song  string      `json:"song"`
	Files []FileEntry `json:"files"`
}

// handleResults lists all songs and their stems from the output directory.
// GET /api/results
func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := findProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode([]ResultsGroup{})
		return
	}

	results := make([]ResultsGroup, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		song := entry.Name()
		stemDir := filepath.Join(outputDir, song)
		stemEntries, err := os.ReadDir(stemDir)
		if err != nil {
			continue
		}
		var files []FileEntry
		for _, stemEntry := range stemEntries {
			if stemEntry.IsDir() {
				continue
			}
			name := stemEntry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" && ext != ".m4a" {
				continue
			}
			files = append(files, FileEntry{
				Name: name,
				Path: "/api/files/" + song + "/" + name,
			})
		}
		if len(files) > 0 {
			results = append(results, ResultsGroup{Song: song, Files: files})
		}
	}
	// Sort by song name for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Song < results[j].Song
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(results)
}

// InputEntry describes an uploaded input file.
type InputEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// handleInputs lists uploaded input files from the input directory.
// GET /api/inputs
func (s *Server) handleInputs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := findProjectRoot()
	inputDir := filepath.Join(projectRoot, "input")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode([]InputEntry{})
		return
	}

	var inputs []InputEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" && ext != ".m4a" {
			continue
		}
		inputs = append(inputs, InputEntry{
			Name: name,
			Path: "/input/" + name,
		})
	}
	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Name < inputs[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(inputs)
}

// handleQueueStatus returns all jobs ordered by status priority.
// GET /api/queue/status
func (s *Server) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()

	var jobList []*JobState
	for _, j := range s.jobs {
		jobList = append(jobList, j)
	}
	sort.Slice(jobList, func(i, j int) bool {
		order := map[string]int{"processing": 0, "waiting": 1, "done": 2, "error": 3}
		if order[jobList[i].Status] != order[jobList[j].Status] {
			return order[jobList[i].Status] < order[jobList[j].Status]
		}
		return jobList[i].Index < jobList[j].Index
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobList})
}

// worker processes jobs sequentially from the queue.
func (s *Server) worker() {
	for job := range s.jobQueue {
		s.jobsMu.Lock()
		if state, ok := s.jobs[job.Song]; ok {
			state.Status = "processing"
		}
		s.jobsMu.Unlock()

		dockerArgs := append([]string{"exec", "onda", "bash", "/pipeline.sh"}, job.Args...)
		cmd := exec.Command("docker", dockerArgs...)
		out, err := cmd.CombinedOutput()

		s.jobsMu.Lock()
		if state, ok := s.jobs[job.Song]; ok {
			if err != nil {
				state.Status = "error"
				state.Error = strings.TrimSpace(string(out))
			} else {
				state.Status = "done"
				state.Files = listStems(job.Song)
			}
		}
		s.jobsMu.Unlock()
	}
}

// listStems reads the output directory for a song and returns the generated files.
func listStems(song string) []FileEntry {
	projectRoot := findProjectRoot()
	outputDir := filepath.Join(projectRoot, "output", song)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil
	}
	var files []FileEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		files = append(files, FileEntry{
			Name: name,
			Path: "/api/files/" + song + "/" + name,
		})
	}
	return files
}

// buildPipelineArgs constructs the argument list for pipeline.sh from a SeparateRequest.
func buildPipelineArgs(req SeparateRequest) (song string, args []string) {
	song = strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))
	containerOutput := "/output/" + song

	if req.Viperx {
		args = append(args, "--viperx")
		if req.ViperxKeep != "" {
			args = append(args, "--viperx-keep", req.ViperxKeep)
		}
	}
	if req.Demucs {
		args = append(args, "--demucs")
		if len(req.DemucsKeep) > 0 {
			args = append(args, "--demucs-keep", strings.Join(req.DemucsKeep, ","))
		}
	}
	if req.Pitch != 0 {
		args = append(args, "--rubberband", "--pitch", fmt.Sprintf("%d", req.Pitch))
	}

	preset := cli.Presets[req.Preset]
	if req.VocalModel != "" {
		modelDir := resolveModelDir(req.VocalModel)
		if modelDir != "" {
			args = append(args, "--viperx-model", modelDir)
		}
	}
	stemModel := req.StemModel
	if stemModel == "" {
		stemModel = preset.StemModel
	}
	if stemModel != "" {
		args = append(args, "--demucs-model", stemModel)
	}

	// ViperX config
	if req.VocalModel != "" {
		viperxCfg, err := loadModelConfig(req.VocalModel)
		if err == nil {
			args = append(args, "--segment-size", fmt.Sprintf("%d", viperxCfg.SegmentSize))
			args = append(args, "--overlap", fmt.Sprintf("%.2f", viperxCfg.Overlap))
			if viperxCfg.ChunkSize > 0 {
				args = append(args, "--chunk-size", fmt.Sprintf("%d", viperxCfg.ChunkSize))
			}
			if viperxCfg.BatchSize > 0 {
				args = append(args, "--batch-size", fmt.Sprintf("%d", viperxCfg.BatchSize))
			}
			if viperxCfg.Device != "" && viperxCfg.Device != "cuda" {
				args = append(args, "--device", viperxCfg.Device)
			}
		}
	}
	// Demucs config
	if stemModel != "" {
		demucsCfg, err := loadModelConfig(stemModel)
		if err == nil {
			if demucsCfg.Shifts > 1 {
				args = append(args, "--shifts", fmt.Sprintf("%d", demucsCfg.Shifts))
			}
			if demucsCfg.Segment > 0 {
				args = append(args, "--demucs-segment", fmt.Sprintf("%d", demucsCfg.Segment))
			}
			if demucsCfg.Jobs > 0 {
				args = append(args, "--jobs", fmt.Sprintf("%d", demucsCfg.Jobs))
			}
			if demucsCfg.Device != "" && demucsCfg.Device != "cuda" {
				args = append(args, "--device", demucsCfg.Device)
			}
		}
	}

	args = append(args, "--output", containerOutput)
	args = append(args, req.Input)
	return song, args
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

// handleSeparate validates and enqueues a separation job to the sequential queue.
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

	// Build pipeline arguments and extract song name
	song, pipelineArgs := buildPipelineArgs(req)

	// Check if song is already in the queue
	s.jobsMu.Lock()
	if _, exists := s.jobs[song]; exists {
		s.jobsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "song already queued",
		})
		return
	}
	s.jobs[song] = &JobState{Song: song, Status: "waiting", Index: s.nextIndex}
	s.nextIndex++
	s.jobsMu.Unlock()

	// Enqueue the job
	s.jobQueue <- JobRequest{Song: song, Args: pipelineArgs, Config: req}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "queued",
		"song":   song,
	})
}

// resolveModelDir resolves a model name to a directory path usable inside the
// Docker container. For Demucs PyTorch models (htdemucs_ft, htdemucs, etc.)
// the name is returned as-is (loaded by name, not path). For all other models
// (ViperX, Roformer, MDX, etc.), the model is looked up in listModels() and
// its /models/ path is returned directly (both containers use /models).
func resolveModelDir(name string) string {
	if name == "htdemucs_ft" || (strings.HasPrefix(name, "htdemucs") && !strings.Contains(name, ".onnx")) {
		return name
	}
	models := listModels()
	for _, m := range models.Models {
		if m.Name == name || m.DisplayName == name {
			return filepath.Dir(m.Path)
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
		log.Printf("WARN: failed to parse model config %s: %v, using defaults", path, err)
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
	// Determine input directory: prefer project root /input,
	// fall back to a temp dir if it doesn't exist.
	projectRoot := findProjectRoot()
	inputDir := filepath.Join(projectRoot, "input")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		os.MkdirAll(inputDir, 0755)
	}

	// Check disk space before parsing the form (500MB max)
	var diskStat syscall.Statfs_t
	if err := syscall.Statfs(inputDir, &diskStat); err == nil {
		freeBytes := diskStat.Bavail * uint64(diskStat.Bsize)
		if freeBytes < 500<<20 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInsufficientStorage)
			json.NewEncoder(w).Encode(map[string]string{"error": "disk space too low for upload"})
			return
		}
	}

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

	// Sanitize filename to prevent path traversal
	safeName := filepath.Base(header.Filename)
	destPath := filepath.Join(inputDir, safeName)
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
	containerPath := "/input/" + safeName
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": containerPath})
}

// handleModelsCatalog returns the UVR model catalog (uvr_models.json) with
// per-model downloaded flags by comparing against the filesystem.
// GET /api/models/catalog
func (s *Server) handleModelsCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	// Try the container path first, then fall back to the project root
	data, err := os.ReadFile("/app/uvr_models.json")
	if err != nil {
		projectRoot := findProjectRoot()
		data, err = os.ReadFile(filepath.Join(projectRoot, "uvr_models.json"))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "failed to read uvr_models.json catalog file",
			})
			return
		}
	}

	var catalog []UVRModelEntry
	if err := json.Unmarshal(data, &catalog); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to parse uvr_models.json: " + err.Error(),
		})
		return
	}

	// Build a set of downloaded model filenames from the filesystem
	downloadedFiles := make(map[string]bool)
	for _, m := range listModels().Models {
		// Match without extension (m.Name is stripped of extension)
		downloadedFiles[m.Name] = true
		// Also try common extensions to match against Filename in catalog
		for _, ext := range []string{".pth", ".onnx", ".ckpt", ".th", ".safetensors", ".yaml"} {
			downloadedFiles[m.Name+ext] = true
		}
	}

	// Tag each catalog entry as downloaded if its filename exists on disk
	for i := range catalog {
		catalog[i].Source = "uvr"
		if downloadedFiles[catalog[i].Filename] ||
			downloadedFiles[catalog[i].Name] {
			catalog[i].Downloaded = true
		}
	}

	// Filter out entries with size_mb == 0 (yaml configs, dependencies).
	// They are still needed in uvr_models.json for dependency resolution
	// during downloads, but should not appear in the catalog UI.
	filtered := make([]UVRModelEntry, 0, len(catalog))
	for _, entry := range catalog {
		if entry.SizeMB > 0 {
			filtered = append(filtered, entry)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(filtered)
}

// handlePitchFileServe serves generated pitch-shifted files from the output directory.
// GET /api/pitch/files/{song}/{pitch}/{file}
func (s *Server) handlePitchFileServe(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	pitchStr := r.PathValue("pitch")
	file := r.PathValue("file")

	// Path traversal guard
	song = filepath.Clean(song)
	pitchStr = filepath.Clean(pitchStr)
	file = filepath.Clean(file)
	if strings.Contains(file, "..") || strings.Contains(song, "..") || strings.Contains(pitchStr, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	projectRoot := findProjectRoot()
	outputBase := filepath.Join(projectRoot, "output")

	// Build path: /output/{song}/{song}_pitch{pitch}/{file}
	filePath := filepath.Join(outputBase, song, song+"_pitch"+pitchStr, file)

	// Verify the file is inside the output directory
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(outputBase)+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absPath)
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
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
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
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
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

	// Remove job from queue tracking if present
	s.jobsMu.Lock()
	delete(s.jobs, song)
	s.jobsMu.Unlock()

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

	// B6: Prevent deleting files inside pitch subdirectories via this endpoint
	if strings.Contains(filepath.Dir(file), "_pitch") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete files in pitch subdirectories"})
		return
	}

	projectRoot := findProjectRoot()
	filePath := filepath.Join(projectRoot, "output", file)

	// Verify inside output/
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
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

// handleDeleteInput deletes a file from the input directory.
// DELETE /api/inputs/{name}
func (s *Server) handleDeleteInput(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	name = filepath.Clean(name)

	projectRoot := findProjectRoot()
	filePath := filepath.Join(projectRoot, "input", name)

	// Verify inside input/
	inputPrefix := filepath.Join(projectRoot, "input")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, inputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "input file not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": name})
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
