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

const pipelineStatusFile = "/tmp/onda_pipeline_status.json"

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
}

func pipelineStatusFilePath() string { return pipelineStatusFile }

const version = "v2.0.0-alpha"

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

	status := "ok"
	if containerStatus != "running" || !gpuAvailable {
		status = "degraded"
	}

	// Build backend sub-object
	backendDetail := "onda container " + containerStatus
	if containerStatus == "" {
		backendDetail = "onda container not found"
	}

	// Build gpu sub-object: code=E3 only when ok=false
	var gpuObj map[string]interface{}
	if gpuAvailable {
		gpuObj = map[string]interface{}{"ok": true, "detail": gpuInfo}
	} else {
		gpuObj = map[string]interface{}{"ok": false, "code": "E3", "detail": gpuInfo}
	}

	resp := map[string]interface{}{
		"status":  status,
		"version": version,
		"backend": map[string]interface{}{
			"ok":     containerStatus == "running",
			"detail": backendDetail,
		},
		"gpu":    gpuObj,
		"disk":   checkDisk(),
		"docker": checkDocker(),
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

	// Build pipeline.sh arguments from request flags
	projectRoot := findProjectRoot()
	pipelineScript := filepath.Join(projectRoot, "pipeline.sh")

	// Convert container paths to host paths for pipeline.sh.
	// pipeline.sh runs on the HOST, so /input/... and /output/... won't resolve.
	// The bind mounts are: ./input -> /input, ./output -> /output.
	song := strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))
	hostInput := req.Input
	hostOutput := req.Output

	if strings.HasPrefix(req.Input, "/input/") {
		hostInput = filepath.Join(projectRoot, "input", filepath.Base(req.Input))
	}
	if hostOutput == "" {
		hostOutput = filepath.Join(projectRoot, "output", song)
	} else if strings.HasPrefix(req.Output, "/output/") {
		hostOutput = filepath.Join(projectRoot, "output", strings.TrimPrefix(req.Output, "/output/"))
	}

	args := []string{pipelineScript}

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

	// Pasamos los flags de modelo (vocal y stem) desde el preset/request
	preset := cli.Presets[req.Preset]
	if req.VocalModel != "" {
		args = append(args, "--viperx-model", req.VocalModel)
	}
	stemModel := req.StemModel
	if stemModel == "" {
		stemModel = preset.StemModel
	}
	if stemModel != "" {
		args = append(args, "--demucs-model", stemModel)
	}

	// Read per-model config and apply inference flags.
	// Prefer the vocal model's config; fall back to stem model's config.
	modelName := req.VocalModel
	if modelName == "" {
		modelName = req.StemModel
	}
	if modelName != "" {
		cfg, err := loadModelConfig(modelName)
		if err == nil {
			args = applyModelConfigToArgs(args, cfg)
		}
	} else {
		// No specific model, apply defaults
		args = applyModelConfigToArgs(args, modelConfigDefaults())
	}

	args = append(args, "--output", hostOutput)
	args = append(args, hostInput)

	// Clean previous status file before launching new pipeline
	os.Remove(pipelineStatusFilePath())

	// Launch pipeline in background
	go func() {
		cmd := exec.Command("bash", args...)
		cmd.Dir = projectRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Write error to status file on failure
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

// modelConfigDefaults returns default inference parameters.
func modelConfigDefaults() ModelConfig {
	return ModelConfig{
		SegmentSize: 256,
		Overlap:     0.25,
		ChunkSize:   0,
		BatchSize:   0,
		Device:      "cuda",
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
