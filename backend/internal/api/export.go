package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// ExportRequest is the JSON body for POST /api/audio/export.
type ExportRequest struct {
	File   string `json:"file"`
	Format string `json:"format"`
}

// ExportResponse is returned by POST /api/audio/export.
type ExportResponse struct {
	File   string `json:"file"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
}

// handleExport verifies that a WAV file exists and returns its metadata.
// POST /api/audio/export
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.File == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "file is required"})
		return
	}
	if req.Format != "wav" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "format must be 'wav'"})
		return
	}

	safeName := filepath.Base(req.File)
	projectRoot := findProjectRoot()

	// Search in daw-data first, then fall back to input.
	searchDirs := []string{
		filepath.Join(projectRoot, "daw-data"),
		filepath.Join(projectRoot, "input"),
	}

	var filePath string
	for _, dir := range searchDirs {
		candidate := filepath.Join(dir, safeName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			filePath = candidate
			break
		}
	}

	if filePath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat file: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ExportResponse{
		File:   safeName,
		Format: req.Format,
		Size:   info.Size(),
	})
}
