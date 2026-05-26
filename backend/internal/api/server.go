package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("/api/gpu", s.handleGPU)
	s.mux.HandleFunc("/api/separate", s.handleSeparate)

	return &http.Server{
		Addr:    addr,
		Handler: s.corsMiddleware(s.mux),
	}
}

// corsMiddleware adds CORS headers and handles OPTIONS preflight.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
	if containerStatus != "running" {
		status = "degraded"
	}

	resp := HealthResponse{
		Status:    status,
		Container: containerStatus,
		GPU:       gpuAvailable,
		GPUInfo:   gpuInfo,
		Version:   version,
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
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
