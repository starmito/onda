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
	"github.com/starmito/onda/internal/pipeline"
)

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
func readPipelineStatus() (*pipeline.Status, error) {
	data, err := os.ReadFile(pipeline.StatusFile())
	if err != nil {
		return nil, err
	}
	var s pipeline.Status
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
			data, err := os.ReadFile(pipeline.StatusFile())
			if err != nil {
				continue
			}
			dataStr := string(data)
			if dataStr == lastData {
				continue
			}
			lastData = dataStr

			var status pipeline.Status
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
	Pitch      int    `json:"pitch,omitempty"`

	Viperx     bool     `json:"viperx"`
	ViperxKeep string   `json:"viperx_keep,omitempty"`
	Demucs     bool     `json:"demucs"`
	DemucsKeep []string `json:"demucs_keep,omitempty"`
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
	preset, ok := cli.Presets[req.Preset]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("unknown preset %q", req.Preset),
		})
		return
	}

	// Build PipelineFlags from preset + request overrides
	flags := &cli.PipelineFlags{
		Preset:       req.Preset,
		VocalModel:   req.VocalModel,
		VocalOverlap: preset.VocalOverlap,
		VocalKeep:    "both",
		StemModel:    preset.StemModel,
		DrumsModel:   preset.DrumsModel,
		BassModel:    preset.BassModel,
		OtherModel:   preset.OtherModel,
		Pitch:        req.Pitch,
		Input:        req.Input,
		Output:       req.Output,
	}

	// If no vocal model override, use the preset's default
	if flags.VocalModel == "" {
		flags.VocalModel = preset.VocalModel
	}

	// Override with PipelineConfig flags from request
	if !req.Viperx {
		flags.VocalModel = "" // Skip vocal separation
	}
	if !req.Demucs {
		flags.StemModel = "" // Skip Demucs stem separation
	}
	if len(req.DemucsKeep) > 0 {
		flags.StemKeep = req.DemucsKeep
	}
	if req.ViperxKeep != "" {
		flags.VocalKeep = req.ViperxKeep
	}

	// Clean previous status file before launching new pipeline
	os.Remove(pipeline.StatusFile())

	// Launch pipeline in background
	song := strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))
	go pipeline.Run(flags)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "started",
		"song":   song,
	})
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
		os.Remove(pipeline.StatusFile())
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

	// Clear status file if the deleted file's song matches current status
	if st, err := readPipelineStatus(); err == nil {
		parts := strings.SplitN(file, "/", 2)
		if len(parts) > 0 && parts[0] == st.Song {
			os.Remove(pipeline.StatusFile())
		}
	}

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
