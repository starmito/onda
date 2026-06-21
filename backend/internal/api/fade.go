package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FadeRequest is the JSON body for POST /api/audio/fade.
// Accepts either start+duration or start+end; if both are present, duration is used.
type FadeRequest struct {
	File     string  `json:"file"`
	Type     string  `json:"type"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	End      float64 `json:"end"`
}

// FadeResponse is returned by POST /api/audio/fade.
type FadeResponse struct {
	File string `json:"file"`
}

// handleFade applies a linear fade-in or fade-out envelope to a WAV file segment
// using SoX, by extracting the segment, fading it, and recombining it with the
// unchanged prefix and suffix.
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
	// Support start+end as an alternative to start+duration.
	if req.Duration <= 0 && req.End > req.Start {
		req.Duration = req.End - req.Start
	}
	if req.Duration <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "duration or a valid end time must be provided"})
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

	endTime := req.Start + req.Duration
	if endTime > duration {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("start+duration (%.3f) exceeds duration (%.3f)", endTime, duration),
		})
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

	tag := strconv.FormatInt(time.Now().UnixNano(), 10)
	prefixPath := filepath.Join(tmpDir, "fade_"+tag+"_prefix.wav")
	segmentPath := filepath.Join(tmpDir, "fade_"+tag+"_segment.wav")
	fadedPath := filepath.Join(tmpDir, "fade_"+tag+"_faded.wav")
	suffixPath := filepath.Join(tmpDir, "fade_"+tag+"_suffix.wav")

	cleanup := func() {
		_ = os.Remove(prefixPath)
		_ = os.Remove(segmentPath)
		_ = os.Remove(fadedPath)
		_ = os.Remove(suffixPath)
	}
	defer cleanup()

	startStr := fmt.Sprintf("%f", req.Start)
	durationStr := fmt.Sprintf("%f", req.Duration)
	endStr := fmt.Sprintf("%f", endTime)

	// Extract the unchanged prefix [0, start).
	if req.Start > 0 {
		if err := ApplySox(sourcePath, prefixPath, []SoxEffect{
			{Name: "trim", Params: []string{"0", startStr}},
		}); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to extract prefix: " + err.Error()})
			return
		}
	}

	// Extract the segment to fade.
	if err := ApplySox(sourcePath, segmentPath, []SoxEffect{
		{Name: "trim", Params: []string{startStr, durationStr}},
	}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to extract segment: " + err.Error()})
		return
	}

	// Apply fade across the whole segment.
	var fadeParams []string
	if req.Type == "in" {
		fadeParams = []string{"t", durationStr, "0", "0"}
	} else {
		fadeParams = []string{"t", "0", "0", durationStr}
	}
	if err := ApplySox(segmentPath, fadedPath, []SoxEffect{
		{Name: "fade", Params: fadeParams},
	}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to apply fade: " + err.Error()})
		return
	}

	// Extract the unchanged suffix [end, duration).
	if endTime < duration {
		if err := ApplySox(sourcePath, suffixPath, []SoxEffect{
			{Name: "trim", Params: []string{"=" + endStr}},
		}); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to extract suffix: " + err.Error()})
			return
		}
	}

	// Build the input list for concatenation.
	outputName := "fade_" + req.Type + "_" + safeName
	outputPath := filepath.Join(dawBase, outputName)

	var concatInputs []string
	if req.Start > 0 {
		concatInputs = append(concatInputs, prefixPath)
	}
	concatInputs = append(concatInputs, fadedPath)
	if endTime < duration {
		concatInputs = append(concatInputs, suffixPath)
	}

	if err := concatSox(concatInputs, outputPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to concatenate audio: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(FadeResponse{File: outputName})
}

// concatSox concatenates multiple WAV files (with identical format) into one
// output file using SoX: `sox input1 input2 ... output`.
func concatSox(inputs []string, outputPath string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs to concatenate")
	}
	if len(inputs) == 1 {
		return ApplySox(inputs[0], outputPath, nil)
	}

	args := append([]string{}, inputs...)
	args = append(args, outputPath)
	out, err := exec.Command("sox", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sox concat failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
