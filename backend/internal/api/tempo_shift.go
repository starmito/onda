package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// TempoShiftRequest is the JSON body for POST /api/audio/tempo.
type TempoShiftRequest struct {
	File  string  `json:"file"`
	Ratio float64 `json:"ratio"`
}

// TempoShiftResponse is returned by POST /api/audio/tempo.
type TempoShiftResponse struct {
	File  string  `json:"file"`
	Ratio float64 `json:"ratio"`
}

// handleTempoShift applies a global tempo change to an input audio file using
// the rubberband CLI. It expects a POST request to /api/audio/tempo with a JSON
// body: {"file": "name.wav", "ratio": 1.2}.
func (s *Server) handleTempoShift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req TempoShiftRequest
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

	if req.Ratio <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ratio must be greater than 0"})
		return
	}

	// Prevent path traversal by using only the base name.
	safeName := filepath.Base(req.File)
	projectRoot := findProjectRoot()
	dawBase := filepath.Join(projectRoot, "daw-data")

	sourcePath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		dawPath := filepath.Join(dawBase, safeName)
		if _, err := os.Stat(dawPath); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
			return
		}
		sourcePath = dawPath
	}

	// Ensure DAW data directory exists.
	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}
	if err := os.MkdirAll(filepath.Join(dawBase, "tmp"), 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create tmp dir: %v", err)})
		return
	}

	baseName := safeName[:len(safeName)-len(filepath.Ext(safeName))]
	ext := filepath.Ext(safeName)
	outputName := baseName + "_tempo" + ext
	tmpPath := filepath.Join(dawBase, "tmp", outputName)
	outputPath := filepath.Join(dawBase, outputName)

	cmd := exec.Command("rubberband", "--tempo", fmt.Sprintf("%f", req.Ratio), sourcePath, tmpPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "rubberband failed: " + string(out)})
		return
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to move output file: %v", err)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TempoShiftResponse{
		File:  outputName,
		Ratio: req.Ratio,
	})
}
