package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// TrimRequest is the JSON body for POST /api/audio/trim.
type TrimRequest struct {
	File  string  `json:"file"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// TrimResponse is returned by POST /api/audio/trim.
type TrimResponse struct {
	File string `json:"file"`
}

// handleTrim extracts a segment from a WAV file and writes it to daw-data/trim_<nombre>.wav.
// POST /api/audio/trim
func (s *Server) handleTrim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req TrimRequest
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
	if req.Start < 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "start must be >= 0"})
		return
	}
	if req.End <= req.Start {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "end must be greater than start"})
		return
	}

	safeName := filepath.Base(req.File)
	projectRoot := findProjectRoot()
	inputPath := filepath.Join(projectRoot, "input", safeName)
	dawBase := filepath.Join(projectRoot, "daw-data")
	dawPath := filepath.Join(dawBase, safeName)

	sourcePath := inputPath
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		if _, err := os.Stat(dawPath); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
			return
		}
		sourcePath = dawPath
	}

	duration, err := detectDuration(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read duration: " + err.Error()})
		return
	}

	if req.End > duration {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("end (%.3f) exceeds duration (%.3f)", req.End, duration),
		})
		return
	}

	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}

	outputName := "trim_" + safeName
	outputPath := filepath.Join(dawBase, outputName)

	if err := ApplySox(sourcePath, outputPath, []SoxEffect{
		{Name: "trim", Params: []string{fmt.Sprintf("%f", req.Start), "=" + fmt.Sprintf("%f", req.End)}},
	}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to trim audio: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TrimResponse{File: outputName})
}
