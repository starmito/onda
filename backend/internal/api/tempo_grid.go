package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Bar represents a musical bar grouping four detected beats.
type Bar struct {
	Bar   int     `json:"bar"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// TempoGridResponse is the JSON response for the tempo grid endpoint.
type TempoGridResponse struct {
	BPM      float64   `json:"bpm"`
	Beats    []float64 `json:"beats"`
	Bars     []Bar     `json:"bars"`
	Duration float64   `json:"duration"`
}

// handleTempoGrid detects BPM, beat positions and bar groups for an input audio file.
// It expects a GET request to /api/audio/tempo-grid?file=name.wav.
func (s *Server) handleTempoGrid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	file := r.URL.Query().Get("file")
	if file == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing file parameter"})
		return
	}

	// Prevent path traversal by using only the base name.
	safeName := filepath.Base(file)
	projectRoot := findProjectRoot()

	// Look for the file in input/ first, then fall back to daw-data/.
	inputPath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		dawPath := filepath.Join(projectRoot, "daw-data", safeName)
		if _, err := os.Stat(dawPath); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
			return
		}
		inputPath = dawPath
	}

	bpm, err := detectBPM(inputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "aubio tempo failed: " + err.Error()})
		return
	}

	beats, err := detectBeats(inputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "aubio beat failed: " + err.Error()})
		return
	}

	duration, err := detectDuration(inputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read duration: " + err.Error()})
		return
	}

	// Build bars as groups of four beats.
	var bars []Bar
	for i := 0; i < len(beats); i += 4 {
		start := beats[i]
		end := duration
		if i+4 < len(beats) {
			end = beats[i+4]
		}
		bars = append(bars, Bar{
			Bar:   i/4 + 1,
			Start: start,
			End:   end,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TempoGridResponse{
		BPM:      bpm,
		Beats:    beats,
		Bars:     bars,
		Duration: duration,
	})
}
