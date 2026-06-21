package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-audio/audio"
	"github.com/moutend/go-equalizer/pkg/equalizer"
)

// EqFilter describes a single parametric EQ stage.
type EqFilter struct {
	Type string  `json:"type"`
	Freq float64 `json:"freq"`
	Gain float64 `json:"gain,omitempty"`
	Q    float64 `json:"q"`
}

// EqRequest is the JSON body for POST /api/daw/eq.
type EqRequest struct {
	File    string     `json:"file"`
	Filters []EqFilter `json:"filters"`
}

// EqResponse is returned by POST /api/daw/eq.
type EqResponse struct {
	File           string `json:"file"`
	FiltersApplied int    `json:"filters_applied"`
}

// eqProcessor is the common operation exposed by every go-equalizer filter.
type eqProcessor interface {
	Apply(input float64) float64
}

// handleEQ applies a chain of parametric EQ filters to a WAV file.
// POST /api/daw/eq
func (s *Server) handleEQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req EqRequest
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
	if len(req.Filters) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "filters cannot be empty"})
		return
	}

	for i, f := range req.Filters {
		if f.Type == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("filter %d: type is required", i),
			})
			return
		}
		if f.Freq <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("filter %d: freq must be > 0", i),
			})
			return
		}
		if f.Q <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("filter %d: q must be > 0", i),
			})
			return
		}
		needsGain := map[string]bool{
			"peak":      true,
			"lowshelf":  true,
			"highshelf": true,
		}
		if needsGain[strings.ToLower(f.Type)] && f.Gain == 0 {
			// Gain=0 is technically valid (no change), but we still accept it.
			// No-op filters are allowed for API symmetry.
		}
	}

	safeName := filepath.Base(req.File)
	projectRoot := resolveProjectRoot()
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

	_, err := detectDuration(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read duration: " + err.Error()})
		return
	}

	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}
	tmpDir := filepath.Join(dawBase, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create tmp dir: %v", err)})
		return
	}

	inputBuf, inputFmt, err := readWAV(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read input WAV: " + err.Error()})
		return
	}

	sr := float64(inputFmt.SampleRate)
	numChannels := inputFmt.NumChannels
	if numChannels < 1 {
		numChannels = 1
	}

	// Build a per-channel chain of filters to keep state independent across channels.
	filterChains := make([][]eqProcessor, numChannels)
	for ch := 0; ch < numChannels; ch++ {
		chain := make([]eqProcessor, 0, len(req.Filters))
		for _, f := range req.Filters {
			filter, err := buildEQFilter(sr, f)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			chain = append(chain, filter)
		}
		filterChains[ch] = chain
	}

	maxVal := float64(int64(1) << (inputFmt.BitDepth - 1))
	if maxVal <= 0 {
		maxVal = 32768
	}

	outputData := make([]int, len(inputBuf.Data))
	for i, sample := range inputBuf.Data {
		ch := i % numChannels
		normalized := float64(sample) / maxVal
		for _, f := range filterChains[ch] {
			normalized = f.Apply(normalized)
		}
		outputData[i] = clampSample(normalized*maxVal, maxVal)
	}

	outputBuf := &audio.IntBuffer{
		Data:   outputData,
		Format: inputBuf.Format,
	}

	outputName := "eq_" + safeName
	outputPath := filepath.Join(dawBase, outputName)

	if err := writeWAV(outputPath, outputBuf, inputFmt); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write output WAV: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EqResponse{
		File:           outputName,
		FiltersApplied: len(req.Filters),
	})
}

// buildEQFilter creates the appropriate go-equalizer filter from an EqFilter.
func buildEQFilter(sr float64, f EqFilter) (eqProcessor, error) {
	switch strings.ToLower(f.Type) {
	case "lowpass":
		return equalizer.NewLowPass(sr, f.Freq, f.Q), nil
	case "highpass":
		return equalizer.NewHighPass(sr, f.Freq, f.Q), nil
	case "bandpass":
		return equalizer.NewBandPass(sr, f.Freq, f.Q), nil
	case "notch":
		return equalizer.NewBandReject(sr, f.Freq, f.Q), nil
	case "peak":
		return equalizer.NewPeaking(sr, f.Freq, f.Gain, f.Q), nil
	case "lowshelf":
		return equalizer.NewLowShelf(sr, f.Freq, f.Gain, f.Q), nil
	case "highshelf":
		return equalizer.NewHighShelf(sr, f.Freq, f.Gain, f.Q), nil
	default:
		return nil, fmt.Errorf("unknown filter type: %s", f.Type)
	}
}

// clampSample converts a normalized float back to an integer sample value,
// clamping to the valid range for the original bit depth.
func clampSample(value, maxVal float64) int {
	maxInt := int(maxVal - 1)
	minInt := -int(maxVal)
	rounded := int(math.Round(value))
	if rounded > maxInt {
		return maxInt
	}
	if rounded < minInt {
		return minInt
	}
	return rounded
}
