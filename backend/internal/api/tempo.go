package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// bpmRe matches the first floating point number in a line of text.
var bpmRe = regexp.MustCompile(`([0-9]+\.[0-9]+)`)

// TempoResponse is the JSON response for the BPM detection endpoint.
type TempoResponse struct {
	BPM      float64   `json:"bpm"`
	Beats    []float64 `json:"beats"`
	Duration float64   `json:"duration"`
}

// handleTempo detects the BPM and beat positions of an input audio file using
// the aubio CLI tools. It expects a GET request to /api/audio/tempo?file=name.wav.
func (s *Server) handleTempo(w http.ResponseWriter, r *http.Request) {
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
	inputBase := filepath.Join(projectRoot, "input")
	inputPath := filepath.Join(inputBase, safeName)

	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
		return
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TempoResponse{
		BPM:      bpm,
		Beats:    beats,
		Duration: duration,
	})
}

// detectBPM runs `aubio tempo` and returns the detected BPM.
// It extracts the first floating point number from the output, supporting
// formats like "120.00 bpm", "112.75 bpm (uncertain)", and "BPM: 120.0".
func detectBPM(inputPath string) (float64, error) {
	out, err := exec.Command("aubio", "tempo", inputPath).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	line := strings.TrimSpace(string(out))
	matches := bpmRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not find BPM value in output: %s", line)
	}

	bpm, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse BPM from %q: %w", matches[1], err)
	}
	return bpm, nil
}

// detectBeats runs `aubio beat` and returns the detected beat timestamps.
func detectBeats(inputPath string) ([]float64, error) {
	out, err := exec.Command("aubio", "beat", inputPath).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	var beats []float64
	fields := strings.Fields(string(out))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		t, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse beat %q: %w", f, err)
		}
		beats = append(beats, t)
	}
	return beats, nil
}

// detectDuration uses ffprobe to obtain the audio file duration in seconds.
func detectDuration(inputPath string) (float64, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	durationStr := strings.TrimSpace(string(out))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration %q: %w", durationStr, err)
	}
	return duration, nil
}
