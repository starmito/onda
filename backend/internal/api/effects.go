package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// EffectResponse is the common JSON response for all DAW effect endpoints.
type EffectResponse struct {
	File       string                 `json:"file"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// CompressorRequest is the JSON body for POST /api/daw/compressor.
type CompressorRequest struct {
	File      string  `json:"file"`
	Threshold float64 `json:"threshold"`
	Ratio     float64 `json:"ratio"`
	Attack    float64 `json:"attack"`
	Release   float64 `json:"release"`
	Makeup    float64 `json:"makeup"`
}

// ReverbRequest is the JSON body for POST /api/daw/reverb.
type ReverbRequest struct {
	File     string  `json:"file"`
	RoomSize float64 `json:"room_size"`
	Decay    float64 `json:"decay"`
	WetDry   float64 `json:"wet_dry"`
}

// DelayRequest is the JSON body for POST /api/daw/delay.
type DelayRequest struct {
	File      string  `json:"file"`
	DelayTime float64 `json:"delay_time"`
	Feedback  float64 `json:"feedback"`
	WetDry    float64 `json:"wet_dry"`
}

// ChorusRequest is the JSON body for POST /api/daw/chorus.
type ChorusRequest struct {
	File    string  `json:"file"`
	Depth   float64 `json:"depth"`
	Rate    float64 `json:"rate"`
	DelayMs float64 `json:"delay_ms"`
	WetDry  float64 `json:"wet_dry"`
}

// FlangerRequest is the JSON body for POST /api/daw/flanger.
type FlangerRequest struct {
	File   string  `json:"file"`
	Depth  float64 `json:"depth"`
	Rate   float64 `json:"rate"`
	WetDry float64 `json:"wet_dry"`
}

// PhaserRequest is the JSON body for POST /api/daw/phaser.
type PhaserRequest struct {
	File   string  `json:"file"`
	Depth  float64 `json:"depth"`
	Rate   float64 `json:"rate"`
	WetDry float64 `json:"wet_dry"`
}

// TremoloRequest is the JSON body for POST /api/daw/tremolo.
type TremoloRequest struct {
	File  string  `json:"file"`
	Speed float64 `json:"speed"`
	Depth float64 `json:"depth"`
}

// NoiseGateRequest is the JSON body for POST /api/daw/noisegate.
type NoiseGateRequest struct {
	File     string  `json:"file"`
	Threshold float64 `json:"threshold"`
	Attack   float64 `json:"attack"`
	Release  float64 `json:"release"`
}

// locateEffectInput resolves the absolute path of an input file by looking
// first in input/ and then in daw-data/. It returns an empty string if not found.
func locateEffectInput(file string) string {
	safeName := filepath.Base(file)
	projectRoot := findProjectRoot()
	inputPath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(inputPath); err == nil {
		return inputPath
	}
	dawPath := filepath.Join(projectRoot, "daw-data", safeName)
	if _, err := os.Stat(dawPath); err == nil {
		return dawPath
	}
	return ""
}

// resolveEffectOutput returns the absolute output path in daw-data/ and the
// generated file name with the given effect prefix.
func resolveEffectOutput(prefix, safeName string) (string, string, error) {
	projectRoot := findProjectRoot()
	dawBase := filepath.Join(projectRoot, "daw-data")
	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		return "", "", err
	}
	outputName := prefix + "_" + safeName
	return filepath.Join(dawBase, outputName), outputName, nil
}

// writeEffectError sends a JSON error response with the given status code.
func writeEffectError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// handleCompressor applies a dynamic range compressor using SoX compand.
// POST /api/daw/compressor
func (s *Server) handleCompressor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req CompressorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Threshold == 0 {
		req.Threshold = -20
	}
	if req.Ratio == 0 {
		req.Ratio = 4
	}
	if req.Attack == 0 {
		req.Attack = 5
	}
	if req.Release == 0 {
		req.Release = 100
	}
	if req.Makeup == 0 {
		req.Makeup = 0
	}
	if req.Threshold < -60 || req.Threshold > 0 {
		writeEffectError(w, http.StatusBadRequest, "threshold must be between -60 and 0 dB")
		return
	}
	if req.Ratio < 1 || req.Ratio > 20 {
		writeEffectError(w, http.StatusBadRequest, "ratio must be between 1 and 20")
		return
	}
	if req.Attack < 0.1 || req.Attack > 100 {
		writeEffectError(w, http.StatusBadRequest, "attack must be between 0.1 and 100 ms")
		return
	}
	if req.Release < 10 || req.Release > 1000 {
		writeEffectError(w, http.StatusBadRequest, "release must be between 10 and 1000 ms")
		return
	}
	if req.Makeup < 0 || req.Makeup > 24 {
		writeEffectError(w, http.StatusBadRequest, "makeup must be between 0 and 24 dB")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("compressor", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	attackS := req.Attack / 1000.0
	releaseS := req.Release / 1000.0
	gain := req.Makeup / 10.0
	if gain <= 0 {
		gain = 0.2
	}

	effects := []SoxEffect{
		{
			Name: "compand",
			Params: []string{
				fmt.Sprintf("%f,%f", attackS, releaseS),
				fmt.Sprintf("%.1f,%.1f,%.1f,%.1f,%.1f,%.1f",
					req.Threshold-40, req.Threshold-40,
					req.Threshold-10, req.Threshold-10-((req.Threshold-40)-(req.Threshold-10))/req.Ratio,
					req.Threshold, req.Threshold),
				fmt.Sprintf("%f", gain),
				"-90",
				"0.2",
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply compressor: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{
		File: outputName,
		Parameters: map[string]interface{}{
			"threshold": req.Threshold,
			"ratio":     req.Ratio,
		},
	})
}

// handleReverb applies a reverberation effect using SoX reverb.
// POST /api/daw/reverb
func (s *Server) handleReverb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req ReverbRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.RoomSize == 0 {
		req.RoomSize = 50
	}
	if req.Decay == 0 {
		req.Decay = 50
	}
	if req.WetDry == 0 {
		req.WetDry = 50
	}
	if req.RoomSize < 0 || req.RoomSize > 100 {
		writeEffectError(w, http.StatusBadRequest, "room_size must be between 0 and 100")
		return
	}
	if req.Decay < 0 || req.Decay > 100 {
		writeEffectError(w, http.StatusBadRequest, "decay must be between 0 and 100")
		return
	}
	if req.WetDry < 0 || req.WetDry > 100 {
		writeEffectError(w, http.StatusBadRequest, "wet_dry must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("reverb", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	preDelay := req.WetDry / 10.0
	effects := []SoxEffect{
		{
			Name: "reverb",
			Params: []string{
				fmt.Sprintf("%f", req.RoomSize),
				fmt.Sprintf("%f", req.Decay),
				fmt.Sprintf("%f", preDelay),
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply reverb: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handleDelay applies an echo/delay effect using SoX echo.
// POST /api/daw/delay
func (s *Server) handleDelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req DelayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.DelayTime == 0 {
		req.DelayTime = 0.5
	}
	if req.Feedback == 0 {
		req.Feedback = 30
	}
	if req.WetDry == 0 {
		req.WetDry = 50
	}
	if req.DelayTime < 0.01 || req.DelayTime > 5 {
		writeEffectError(w, http.StatusBadRequest, "delay_time must be between 0.01 and 5 seconds")
		return
	}
	if req.Feedback < 0 || req.Feedback > 100 {
		writeEffectError(w, http.StatusBadRequest, "feedback must be between 0 and 100")
		return
	}
	if req.WetDry < 0 || req.WetDry > 100 {
		writeEffectError(w, http.StatusBadRequest, "wet_dry must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("delay", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := req.WetDry / 100.0
	gainOut := req.Feedback / 100.0
	delay := req.DelayTime
	decay := req.DelayTime * 0.5
	effects := []SoxEffect{
		{
			Name: "echo",
			Params: []string{
				fmt.Sprintf("%f", gainIn),
				fmt.Sprintf("%f", gainOut),
				fmt.Sprintf("%f", delay),
				fmt.Sprintf("%f", decay),
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply delay: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handleChorus applies a chorus effect using SoX chorus.
// POST /api/daw/chorus
func (s *Server) handleChorus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req ChorusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Depth == 0 {
		req.Depth = 3
	}
	if req.Rate == 0 {
		req.Rate = 0.5
	}
	if req.DelayMs == 0 {
		req.DelayMs = 40
	}
	if req.WetDry == 0 {
		req.WetDry = 50
	}
	if req.Depth < 0 || req.Depth > 10 {
		writeEffectError(w, http.StatusBadRequest, "depth must be between 0 and 10")
		return
	}
	if req.Rate < 0.1 || req.Rate > 10 {
		writeEffectError(w, http.StatusBadRequest, "rate must be between 0.1 and 10 Hz")
		return
	}
	if req.DelayMs < 10 || req.DelayMs > 100 {
		writeEffectError(w, http.StatusBadRequest, "delay_ms must be between 10 and 100")
		return
	}
	if req.WetDry < 0 || req.WetDry > 100 {
		writeEffectError(w, http.StatusBadRequest, "wet_dry must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("chorus", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := req.WetDry / 100.0
	gainOut := req.WetDry / 100.0
	effects := []SoxEffect{
		{
			Name: "chorus",
			Params: []string{
				fmt.Sprintf("%f", gainIn),
				fmt.Sprintf("%f", gainOut),
				fmt.Sprintf("%f", req.DelayMs),
				"0.5",
				fmt.Sprintf("%f", req.Rate),
				fmt.Sprintf("%f", req.Depth),
				"-t",
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply chorus: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handleFlanger applies a flanger effect using SoX flanger.
// POST /api/daw/flanger
func (s *Server) handleFlanger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req FlangerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Depth == 0 {
		req.Depth = 2
	}
	if req.Rate == 0 {
		req.Rate = 0.5
	}
	if req.WetDry == 0 {
		req.WetDry = 50
	}
	if req.Depth < 0 || req.Depth > 10 {
		writeEffectError(w, http.StatusBadRequest, "depth must be between 0 and 10")
		return
	}
	if req.Rate < 0.1 || req.Rate > 10 {
		writeEffectError(w, http.StatusBadRequest, "rate must be between 0.1 and 10 Hz")
		return
	}
	if req.WetDry < 0 || req.WetDry > 100 {
		writeEffectError(w, http.StatusBadRequest, "wet_dry must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("flanger", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	delayMs := req.WetDry / 5.0
	if delayMs < 1 {
		delayMs = 1
	}
	if delayMs > 20 {
		delayMs = 20
	}
	effects := []SoxEffect{
		{
			Name: "flanger",
			Params: []string{
				"-t",
				"0",
				fmt.Sprintf("%f", delayMs),
				fmt.Sprintf("%f", req.Depth),
				"0",
				fmt.Sprintf("%f", req.Rate),
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply flanger: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handlePhaser applies a phaser effect using SoX phaser.
// POST /api/daw/phaser
func (s *Server) handlePhaser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req PhaserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Depth == 0 {
		req.Depth = 3
	}
	if req.Rate == 0 {
		req.Rate = 0.5
	}
	if req.WetDry == 0 {
		req.WetDry = 50
	}
	if req.Depth < 0 || req.Depth > 10 {
		writeEffectError(w, http.StatusBadRequest, "depth must be between 0 and 10")
		return
	}
	if req.Rate < 0.1 || req.Rate > 10 {
		writeEffectError(w, http.StatusBadRequest, "rate must be between 0.1 and 10 Hz")
		return
	}
	if req.WetDry < 0 || req.WetDry > 100 {
		writeEffectError(w, http.StatusBadRequest, "wet_dry must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("phaser", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := req.WetDry / 100.0
	gainOut := req.WetDry / 100.0
	delay := req.WetDry / 100.0
	if delay < 0.1 {
		delay = 0.1
	}
	if delay > 5 {
		delay = 5
	}
	effects := []SoxEffect{
		{
			Name: "phaser",
			Params: []string{
				fmt.Sprintf("%f", gainIn),
				fmt.Sprintf("%f", gainOut),
				fmt.Sprintf("%f", delay),
				fmt.Sprintf("%f", req.Rate),
				fmt.Sprintf("%f", req.Depth),
				"-t",
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply phaser: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handleTremolo applies a tremolo (amplitude modulation) effect using SoX tremolo.
// POST /api/daw/tremolo
func (s *Server) handleTremolo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req TremoloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Speed == 0 {
		req.Speed = 5
	}
	if req.Depth == 0 {
		req.Depth = 40
	}
	if req.Speed < 0.1 || req.Speed > 30 {
		writeEffectError(w, http.StatusBadRequest, "speed must be between 0.1 and 30 Hz")
		return
	}
	if req.Depth < 0 || req.Depth > 100 {
		writeEffectError(w, http.StatusBadRequest, "depth must be between 0 and 100")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("tremolo", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	effects := []SoxEffect{
		{
			Name: "tremolo",
			Params: []string{
				fmt.Sprintf("%f", req.Speed),
				fmt.Sprintf("%f", req.Depth),
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply tremolo: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}

// handleNoiseGate applies a noise gate using SoX compand.
// POST /api/daw/noisegate
func (s *Server) handleNoiseGate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEffectError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	var req NoiseGateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEffectError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.File == "" {
		writeEffectError(w, http.StatusBadRequest, "file is required")
		return
	}
	if req.Threshold == 0 {
		req.Threshold = -40
	}
	if req.Attack == 0 {
		req.Attack = 1
	}
	if req.Release == 0 {
		req.Release = 50
	}
	if req.Threshold < -80 || req.Threshold > 0 {
		writeEffectError(w, http.StatusBadRequest, "threshold must be between -80 and 0 dB")
		return
	}
	if req.Attack < 0.1 || req.Attack > 100 {
		writeEffectError(w, http.StatusBadRequest, "attack must be between 0.1 and 100 ms")
		return
	}
	if req.Release < 10 || req.Release > 1000 {
		writeEffectError(w, http.StatusBadRequest, "release must be between 10 and 1000 ms")
		return
	}

	safeName := filepath.Base(req.File)
	sourcePath := locateEffectInput(req.File)
	if sourcePath == "" {
		writeEffectError(w, http.StatusNotFound, "file not found")
		return
	}

	outputPath, outputName, err := resolveEffectOutput("noisegate", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	attackS := req.Attack / 1000.0
	releaseS := req.Release / 1000.0
	effects := []SoxEffect{
		{
			Name: "compand",
			Params: []string{
				fmt.Sprintf("%f,%f", attackS, releaseS),
				fmt.Sprintf("-80,-80,-80,%s,-40,-40", strconv.FormatFloat(req.Threshold, 'f', -1, 64)),
				"-5",
				"0",
				"0.2",
			},
		},
	}

	if err := ApplySox(sourcePath, outputPath, effects); err != nil {
		writeEffectError(w, http.StatusInternalServerError, "failed to apply noise gate: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EffectResponse{File: outputName})
}
