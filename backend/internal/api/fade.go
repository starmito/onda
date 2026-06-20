package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-audio/audio"
)

// FadeRequest is the JSON body for POST /api/audio/fade.
type FadeRequest struct {
	File     string  `json:"file"`
	Type     string  `json:"type"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
}

// FadeResponse is returned by POST /api/audio/fade.
type FadeResponse struct {
	File string `json:"file"`
}

// handleFade applies a linear fade-in or fade-out envelope to a WAV file segment.
// POST /api/audio/fade
func (s *Server) handleFade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req FadeRequest
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
	if req.Type != "in" && req.Type != "out" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "type must be 'in' or 'out'"})
		return
	}
	if req.Start < 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "start must be >= 0"})
		return
	}
	if req.Duration <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "duration must be > 0"})
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

	endTime := req.Start + req.Duration
	if endTime > totalDuration {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("start+duration (%.3f) exceeds duration (%.3f)", endTime, totalDuration),
		})
		return
	}

	startSample := int(req.Start * samplesPerSecond)
	endSample := int(endTime * samplesPerSecond)
	if startSample < 0 {
		startSample = 0
	}
	if endSample > len(buf.Data) {
		endSample = len(buf.Data)
	}
	if startSample > endSample {
		startSample = endSample
	}

	// Clone the original data so we only modify the requested segment.
	outputData := make([]int, len(buf.Data))
	copy(outputData, buf.Data)

	fadeSamples := endSample - startSample
	for i := 0; i < fadeSamples; i++ {
		var gain float64
		if req.Type == "in" {
			gain = float64(i) / float64(fadeSamples)
		} else {
			gain = 1.0 - (float64(i) / float64(fadeSamples))
		}
		// Clamp gain to [0,1] to avoid tiny negative values at the end.
		if gain < 0 {
			gain = 0
		}
		if gain > 1 {
			gain = 1
		}
		idx := startSample + i
		outputData[idx] = int(math.Round(float64(outputData[idx]) * gain))
	}

	faded := &audio.IntBuffer{
		Data:   outputData,
		Format: buf.Format,
	}

	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}

	outputName := "fade_" + req.Type + "_" + safeName
	outputPath := filepath.Join(dawBase, outputName)

	if err := writeWAV(outputPath, faded, fmtInfo); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write output WAV: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(FadeResponse{File: outputName})
}
