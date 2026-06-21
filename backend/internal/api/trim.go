package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-audio/audio"
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

	buf, fmtInfo, err := readWAV(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read input WAV: " + err.Error()})
		return
	}

	samplesPerSecond := float64(fmtInfo.SampleRate * fmtInfo.NumChannels)
	totalDuration := float64(len(buf.Data)) / samplesPerSecond

	if req.End > totalDuration {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("end (%.3f) exceeds duration (%.3f)", req.End, totalDuration),
		})
		return
	}

	startSample := int(req.Start * samplesPerSecond)
	endSample := int(req.End * samplesPerSecond)
	if startSample < 0 {
		startSample = 0
	}
	if endSample > len(buf.Data) {
		endSample = len(buf.Data)
	}
	if startSample > endSample {
		startSample = endSample
	}

	trimmed := &audio.IntBuffer{
		Data:   buf.Data[startSample:endSample],
		Format: buf.Format,
	}

	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}

	outputName := "trim_" + safeName
	outputPath := filepath.Join(dawBase, outputName)

	if err := writeWAV(outputPath, trimmed, fmtInfo); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write output WAV: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TrimResponse{File: outputName})
}
