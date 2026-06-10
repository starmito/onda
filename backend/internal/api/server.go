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
	Song        string      `json:"song"`
	Status      string      `json:"status"` // waiting, processing, done, error
	Progress    int         `json:"progress"`
	Error       string      `json:"error,omitempty"`
	Files       []FileEntry `json:"files,omitempty"`
	Index       int         `json:"index"`
	CurrentStep int         `json:"current_step"`
	TotalSteps  int         `json:"total_steps"`
	StepName    string      `json:"step_name"`
	Device      string      `json:"device,omitempty"`
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
	s.mux.HandleFunc("GET /api/presets/default", s.handleGetDefaultPreset)
	s.mux.HandleFunc("POST /api/presets/default", s.handleSetDefaultPreset)
	s.mux.HandleFunc("GET /api/logs", s.handleGetLogs)
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)
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
	s.mux.HandleFunc("POST /api/upload/pitch", s.handleUploadPitch)
	s.mux.HandleFunc("GET /api/files/{song}/{file}", s.handleFileServe)
	s.mux.HandleFunc("POST /api/backend/start", s.handleBackendStart)
	s.mux.HandleFunc("POST /api/backend/stop", s.handleBackendStop)
	s.mux.HandleFunc("POST /api/backend/restart", s.handleBackendRestart)
	s.mux.HandleFunc("DELETE /api/files/{song}", s.handleDeleteSong)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	s.mux.HandleFunc("DELETE /api/models/{name}", s.handleDeleteModel)
	s.mux.HandleFunc("DELETE /api/inputs/{name}", s.handleDeleteInput)
	s.mux.HandleFunc("DELETE /api/uploads/pitch/{name}", s.handleDeletePitchUpload)
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

	// Read pipeline status file for live step/progress info
	type PipelineStatusJSON struct {
		Status   string  `json:"status"`
		Step     string  `json:"step"`
		Progress float64 `json:"progress"`
		Device   string  `json:"device"`
	}
	var pipelineStatus PipelineStatusJSON
	projectRoot := findProjectRoot()
	statusPath := filepath.Join(projectRoot, "output", "pipeline_status.json")
	if data, err := os.ReadFile(statusPath); err == nil {
		json.Unmarshal(data, &pipelineStatus)
	}

	// Step name mapping and ordering
	stepOrder := map[string]int{"viperx": 1, "demucs": 2, "rubberband": 3}

	var jobList []*JobState
	for _, j := range s.jobs {
		// For the processing job, inject live step/progress from pipeline_status.json
		if j.Status == "processing" && pipelineStatus.Status != "" {
			j.StepName = capitalizeStep(pipelineStatus.Step)
			j.CurrentStep = stepOrder[pipelineStatus.Step]
			if j.CurrentStep == 0 {
				j.CurrentStep = 1
			}
			j.Progress = int(pipelineStatus.Progress * 100)
			j.Device = pipelineStatus.Device
			// Ensure total_steps is at least current_step
			if j.TotalSteps < j.CurrentStep {
				j.TotalSteps = j.CurrentStep
			}
		} else if j.Status == "done" {
			j.Progress = 100
			j.StepName = "Completado"
			j.CurrentStep = j.TotalSteps
		}
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

// capitalizeStep returns a display-friendly step name.
func capitalizeStep(step string) string {
	switch step {
	case "viperx":
		return "ViperX"
	case "demucs":
		return "Demucs"
	case "rubberband":
		return "Rubberband"
	case "complete":
		return "Complete"
	default:
		return step
	}
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

		// Log all pipeline output to ring buffer with distinct timestamps
		baseNano := time.Now().UnixNano()
		lines := strings.Split(string(out), "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			LogWithNano("pipeline", "info", line, baseNano-int64(i))
		}

		s.jobsMu.Lock()
		if state, ok := s.jobs[job.Song]; ok {
			if err != nil {
				state.Status = "error"
				state.Error = strings.TrimSpace(string(out))
				Log("pipeline", "error", "Pipeline failed for "+job.Song+": "+strings.TrimSpace(string(out)))
			} else {
				state.Status = "done"
				state.Files = listStems(job.Song)
				Log("pipeline", "success", fmt.Sprintf("Pipeline completed: %s (%d stems)", job.Song, len(state.Files)))
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

	// Resolve model paths (pipeline.sh reads inference params from model's YAML)
	vocalModel := req.VocalModel
	if vocalModel == "" {
		vocalModel = req.ViperxModel
	}
	if vocalModel != "" {
		modelDir := resolveModelDir(vocalModel)
		if modelDir != "" {
			args = append(args, "--viperx-model", modelDir)
		}
	}
	stemModel := req.StemModel
	if stemModel == "" {
		stemModel = req.DemucsModel
	}
	if stemModel == "" {
		preset := getAllPresets()[req.Preset]
		stemModel = preset.StemModel
	}
	if stemModel != "" {
		args = append(args, "--demucs-model", stemModel)
	}

	// Demucs-specific flags (no YAML config — use CLI args)
	if stemModel != "" && (strings.HasPrefix(stemModel, "htdemucs") || strings.Contains(stemModel, "htdemucs")) {
		if req.Shifts > 1 {
			args = append(args, "--shifts", fmt.Sprintf("%d", req.Shifts))
		}
		if req.DemucsSegment > 0 {
			args = append(args, "--demucs-segment", fmt.Sprintf("%d", req.DemucsSegment))
		}
		if req.Jobs > 0 {
			args = append(args, "--jobs", fmt.Sprintf("%d", req.Jobs))
		}
	}
	// Device override (defaults to cuda in pipeline.sh)
	if req.Device != "" && req.Device != "cuda" {
		args = append(args, "--device", req.Device)
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
	json.NewEncoder(w).Encode(getAllPresets())
}

// SeparateRequest is the JSON body for POST /api/separate.
type SeparateRequest struct {
	Preset     string `json:"preset"`
	Input      string `json:"input"`
	Output     string `json:"output,omitempty"`
	VocalModel string `json:"vocal_model,omitempty"`
	ViperxModel string `json:"viperx_model,omitempty"` // alias for VocalModel
	StemModel   string `json:"stem_model,omitempty"`
	DemucsModel string `json:"demucs_model,omitempty"` // alias for StemModel
	Pitch      int    `json:"pitch,omitempty"`

	Viperx     bool     `json:"viperx"`
	ViperxKeep string   `json:"viperx_keep,omitempty"`
	Demucs     bool     `json:"demucs"`
	DemucsKeep []string `json:"demucs_keep,omitempty"`

	// Demucs-specific overrides (optional, only used when model is htdemucs*)
	Shifts        int `json:"shifts,omitempty"`
	DemucsSegment int `json:"demucs_segment,omitempty"`
	Jobs          int `json:"jobs,omitempty"`
	// Device override (defaults to cuda)
	Device string `json:"device,omitempty"`
}

// ModelConfigResponse is what the config API returns (read from model YAML or defaults).
type ModelConfigResponse struct {
	SegmentSize int     `json:"segment_size"`
	Overlap     float64 `json:"overlap"`
	ChunkSize   int     `json:"chunk_size"`
	BatchSize   int     `json:"batch_size"`
	Device      string  `json:"device"`
	Shifts      int     `json:"shifts"`
	Segment     int     `json:"segment"`
	Jobs        int     `json:"jobs"`
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

	// Validate preset (optional — if provided, must exist in user presets)
	if req.Preset != "" {
		_, ok := getAllPresets()[req.Preset]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("unknown preset %q", req.Preset),
			})
			return
		}
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
	// Compute total pipeline steps
	totalSteps := 0
	if req.Viperx {
		totalSteps++
	}
	if req.Demucs {
		totalSteps++
	}
	if req.Pitch != 0 {
		totalSteps++
	}
	if totalSteps == 0 {
		totalSteps = 2 // default: viperx + demucs
	}

	s.jobs[song] = &JobState{Song: song, Status: "waiting", Index: s.nextIndex, TotalSteps: totalSteps}
	s.nextIndex++
	s.jobsMu.Unlock()

	// Enqueue the job
	s.jobQueue <- JobRequest{Song: song, Args: pipelineArgs, Config: req}

	Log("backend", "success", "Job queued: "+song)

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
// findModelYaml returns the path to the model's YAML config file, or empty string.
func findModelYaml(modelName string) string {
	modelDir := resolveModelDir(modelName)
	if modelDir == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(modelDir, "*.yaml"))
	if err != nil || len(matches) == 0 {
		matches, err = filepath.Glob(filepath.Join(modelDir, "*.yml"))
		if err != nil || len(matches) == 0 {
			return ""
		}
	}
	return matches[0]
}

// readModelConfigFromYaml reads inference parameters from a model's YAML file using Python.
// Returns a ModelConfigResponse with defaults if reading fails.
func readModelConfigFromYaml(name string) ModelConfigResponse {
	yamlPath := findModelYaml(name)
	if yamlPath == "" {
		// Demucs or unknown model — return defaults
		return ModelConfigResponse{
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

	// Use Python to parse YAML and extract inference section
	script := fmt.Sprintf(`
import json, yaml
with open('%s') as f:
    data = yaml.safe_load(f)
inf = data.get('inference', {})
dim_t = inf.get('dim_t', 801)
num_overlap = inf.get('num_overlap', 4)
batch_size = inf.get('batch_size', 1)
# Convert dim_t → "segment_size" (approximate: segment = (dim_t - 33) / 3)
seg_size = max(1, (dim_t - 33) // 3)
overlap_val = 1.0 / num_overlap if num_overlap > 0 else 0.25
print(json.dumps({
    'segment_size': seg_size,
    'overlap': round(overlap_val, 4),
    'chunk_size': 0,
    'batch_size': batch_size,
    'device': 'cuda',
    'shifts': 1,
    'segment': 0,
    'jobs': 0,
}))
`, yamlPath)

	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("WARN: failed to read model YAML %s: %v", yamlPath, err)
		return ModelConfigResponse{
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

	var cfg ModelConfigResponse
	if err := json.Unmarshal(out, &cfg); err != nil {
		log.Printf("WARN: failed to parse model config from YAML: %v", err)
		return ModelConfigResponse{
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
	return cfg
}

// writeModelConfigToYaml writes inference parameters to a model's YAML file using Python.
func writeModelConfigToYaml(name string, cfg ModelConfigResponse) error {
	yamlPath := findModelYaml(name)
	if yamlPath == "" {
		return fmt.Errorf("model YAML not found for %s", name)
	}

	// Convert segment_size → dim_t, overlap → num_overlap
	dimT := cfg.SegmentSize*3 + 33
	numOverlap := 0
	if cfg.Overlap > 0 {
		numOverlap = int(1.0 / cfg.Overlap)
	}
	if numOverlap < 1 {
		numOverlap = 4
	}
	batchSize := cfg.BatchSize
	if batchSize < 1 {
		batchSize = 1
	}

	script := fmt.Sprintf(`
import json, yaml
with open('%s') as f:
    data = yaml.safe_load(f)
if 'inference' not in data:
    data['inference'] = {}
data['inference']['dim_t'] = %d
data['inference']['num_overlap'] = %d
data['inference']['batch_size'] = %d
with open('%s', 'w') as f:
    yaml.dump(data, f, default_flow_style=False)
print('ok')
`, yamlPath, dimT, numOverlap, batchSize, yamlPath)

	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to write YAML: %v — %s", err, string(out))
	}
	return nil
}

// handleModelsConfig saves or retrieves per-model inference configuration.
// GET  /api/models/{name}/config  — reads inference params from the model's YAML
// POST /api/models/{name}/config  — writes inference params to the model's YAML
func (s *Server) handleModelsConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.PathValue("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing model name"})
		return
	}

	if r.Method == http.MethodGet {
		cfg := readModelConfigFromYaml(name)
		json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg ModelConfigResponse
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
		if cfg.Device != "" && cfg.Device != "cpu" && cfg.Device != "cuda" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "device must be 'cpu' or 'cuda'"})
			return
		}

		if err := writeModelConfigToYaml(name, cfg); err != nil {
			log.Printf("ERROR: failed to save model config for %s: %v", name, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		Log("backend", "success", "Config saved to YAML: "+name)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"ok":     "true",
			"detail": fmt.Sprintf("config saved to YAML for model %s", name),
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
	// Fix permissions so container user (1000:1000) can write
	os.Chmod(inputDir, 0777)

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
		Log("backend", "error", "Upload failed: "+err.Error())
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
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file"})
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}

	Log("backend", "success", "Uploaded: "+safeName)

	// The path inside the container is /input/filename
	containerPath := "/input/" + safeName
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": containerPath})
}

// handleUploadPitch accepts a multipart file upload and saves it to input_rubberband/.
func (s *Server) handleUploadPitch(w http.ResponseWriter, r *http.Request) {
	projectRoot := findProjectRoot()
	inputDir := filepath.Join(projectRoot, "input_rubberband")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		os.MkdirAll(inputDir, 0755)
	}
	// Fix permissions so container user (1000:1000) can write
	os.Chmod(inputDir, 0777)

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

	r.ParseMultipartForm(500 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}
	defer file.Close()

	safeName := filepath.Base(header.Filename)
	destPath := filepath.Join(inputDir, safeName)
	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save file"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}

	Log("backend", "success", "Pitch upload: "+safeName)

	containerPath := "/input_rubberband/" + safeName
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

	Log("backend", "info", "Deleted input: "+name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": name})
}

// handleDeletePitchUpload deletes a file from input_rubberband/.
// DELETE /api/uploads/pitch/{name}
func (s *Server) handleDeletePitchUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	name = filepath.Clean(name)

	projectRoot := findProjectRoot()
	filePath := filepath.Join(projectRoot, "input_rubberband", name)

	// Verify inside input_rubberband/
	inputPrefix := filepath.Join(projectRoot, "input_rubberband")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, inputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "pitch upload not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "info", "Deleted pitch upload: "+name)

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
