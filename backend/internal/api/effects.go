package api

import (
	"encoding/json"
	"fmt"
	"math"
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
// Pointer fields distinguish a missing value (use default) from an explicit
// zero, which is a valid slider value for several parameters.
type CompressorRequest struct {
	File      string   `json:"file"`
	Threshold *float64 `json:"threshold,omitempty"`
	Ratio     *float64 `json:"ratio,omitempty"`
	Attack    *float64 `json:"attack,omitempty"`
	Release   *float64 `json:"release,omitempty"`
	Makeup    *float64 `json:"makeup,omitempty"`
}

// ReverbRequest is the JSON body for POST /api/daw/reverb.
type ReverbRequest struct {
	File     string   `json:"file"`
	RoomSize *float64 `json:"room_size,omitempty"`
	Decay    *float64 `json:"decay,omitempty"`
	WetDry   *float64 `json:"wet_dry,omitempty"`
}

// DelayRequest is the JSON body for POST /api/daw/delay.
type DelayRequest struct {
	File      string   `json:"file"`
	DelayTime *float64 `json:"delay_time,omitempty"`
	Feedback  *float64 `json:"feedback,omitempty"`
	WetDry    *float64 `json:"wet_dry,omitempty"`
}

// ChorusRequest is the JSON body for POST /api/daw/chorus.
type ChorusRequest struct {
	File    string   `json:"file"`
	Depth   *float64 `json:"depth,omitempty"`
	Rate    *float64 `json:"rate,omitempty"`
	DelayMs *float64 `json:"delay_ms,omitempty"`
	WetDry  *float64 `json:"wet_dry,omitempty"`
}

// FlangerRequest is the JSON body for POST /api/daw/flanger.
type FlangerRequest struct {
	File   string   `json:"file"`
	Depth  *float64 `json:"depth,omitempty"`
	Rate   *float64 `json:"rate,omitempty"`
	WetDry *float64 `json:"wet_dry,omitempty"`
}

// PhaserRequest is the JSON body for POST /api/daw/phaser.
type PhaserRequest struct {
	File   string   `json:"file"`
	Depth  *float64 `json:"depth,omitempty"`
	Rate   *float64 `json:"rate,omitempty"`
	WetDry *float64 `json:"wet_dry,omitempty"`
}

// TremoloRequest is the JSON body for POST /api/daw/tremolo.
type TremoloRequest struct {
	File  string   `json:"file"`
	Speed *float64 `json:"speed,omitempty"`
	Depth *float64 `json:"depth,omitempty"`
}

// NoiseGateRequest is the JSON body for POST /api/daw/noisegate.
type NoiseGateRequest struct {
	File      string   `json:"file"`
	Threshold *float64 `json:"threshold,omitempty"`
	Attack    *float64 `json:"attack,omitempty"`
	Release   *float64 `json:"release,omitempty"`
}

// defaultFloat returns the pointed value or the supplied default when nil.
func defaultFloat(p *float64, d float64) float64 {
	if p == nil {
		return d
	}
	return *p
}

// validateRange returns a clear error when value is outside [min, max].
func validateRange(name string, value, min, max float64, unit string) error {
	if value < min || value > max {
		if unit != "" {
			return fmt.Errorf("%s must be between %.2f and %.2f %s (got %.2f)", name, min, max, unit, value)
		}
		return fmt.Errorf("%s must be between %.2f and %.2f (got %.2f)", name, min, max, value)
	}
	return nil
}

// writeValidationError sends a 400 JSON response with a clear error message.
func writeValidationError(w http.ResponseWriter, err error) {
	writeEffectError(w, http.StatusBadRequest, err.Error())
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
	threshold := defaultFloat(req.Threshold, -20)
	ratio := defaultFloat(req.Ratio, 4)
	attack := defaultFloat(req.Attack, 5)
	release := defaultFloat(req.Release, 100)
	makeup := defaultFloat(req.Makeup, 0)
	if err := validateRange("threshold", threshold, -60, 0, "dB"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("ratio", ratio, 1, 20, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("attack", attack, 0.1, 100, "ms"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("release", release, 10, 1000, "ms"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("makeup", makeup, 0, 24, "dB"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("compressor", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	attackS := attack / 1000.0
	releaseS := release / 1000.0
	gain := makeup / 10.0
	if gain <= 0 {
		gain = 0.2
	}

	effects := []SoxEffect{
		{
			Name: "compand",
			Params: []string{
				fmt.Sprintf("%f,%f", attackS, releaseS),
				fmt.Sprintf("%.1f,%.1f,%.1f,%.1f,%.1f,%.1f",
					threshold-40, threshold-40,
					threshold-10, threshold-10-((threshold-40)-(threshold-10))/ratio,
					threshold, threshold),
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
			"threshold": threshold,
			"ratio":     ratio,
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
	roomSize := defaultFloat(req.RoomSize, 50)
	decay := defaultFloat(req.Decay, 50)
	wetDry := defaultFloat(req.WetDry, 50)
	if err := validateRange("room_size", roomSize, 0, 100, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("decay", decay, 0, 100, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("wet_dry", wetDry, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("reverb", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	preDelay := wetDry / 10.0
	effects := []SoxEffect{
		{
			Name: "reverb",
			Params: []string{
				fmt.Sprintf("%f", roomSize),
				fmt.Sprintf("%f", decay),
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
	delayTime := defaultFloat(req.DelayTime, 0.5)
	feedback := defaultFloat(req.Feedback, 30)
	wetDry := defaultFloat(req.WetDry, 50)
	if err := validateRange("delay_time", delayTime, 0.03, 5, "seconds"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("feedback", feedback, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("wet_dry", wetDry, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("delay", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := wetDry / 100.0
	gainOut := feedback / 100.0
	delay := delayTime
	// SoX echo requires the decay argument to be < 1.0; clamp to keep any
	// slider value inside the supported range without failing.
	decay := math.Min(delayTime*0.5, 0.99)
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
	depth := defaultFloat(req.Depth, 3)
	rate := defaultFloat(req.Rate, 0.5)
	delayMs := defaultFloat(req.DelayMs, 40)
	wetDry := defaultFloat(req.WetDry, 50)
	if err := validateRange("depth", depth, 0, 10, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	// SoX chorus requires speed < 5 Hz (5.0 is accepted, anything above fails).
	if err := validateRange("rate", rate, 0.1, 5, "Hz"); err != nil {
		writeValidationError(w, err)
		return
	}
	// SoX chorus requires delay >= 20 ms.
	if err := validateRange("delay_ms", delayMs, 20, 100, "ms"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("wet_dry", wetDry, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("chorus", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := wetDry / 100.0
	gainOut := wetDry / 100.0
	effects := []SoxEffect{
		{
			Name: "chorus",
			Params: []string{
				fmt.Sprintf("%f", gainIn),
				fmt.Sprintf("%f", gainOut),
				fmt.Sprintf("%f", delayMs),
				"0.5",
				fmt.Sprintf("%f", rate),
				fmt.Sprintf("%f", depth),
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
	depth := defaultFloat(req.Depth, 2)
	rate := defaultFloat(req.Rate, 0.5)
	wetDry := defaultFloat(req.WetDry, 50)
	if err := validateRange("depth", depth, 0, 10, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("rate", rate, 0.1, 10, "Hz"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("wet_dry", wetDry, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("flanger", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	effects := []SoxEffect{
		{
			Name: "flanger",
			Params: []string{
				"0",                       // delay base (0ms)
				fmt.Sprintf("%f", depth),  // depth (swept delay)
				"0",                       // regen (sin feedback)
				"71",                      // width (default)
				fmt.Sprintf("%f", rate),   // speed
				"sine",                    // shape
				"25",                      // phase
				"linear",                  // interpolation
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
	depth := defaultFloat(req.Depth, 3)
	rate := defaultFloat(req.Rate, 0.5)
	wetDry := defaultFloat(req.WetDry, 50)
	if err := validateRange("depth", depth, 0, 10, ""); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("rate", rate, 0.1, 10, "Hz"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("wet_dry", wetDry, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("phaser", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	gainIn := wetDry / 100.0
	gainOut := wetDry / 100.0
	decay := depth / 10.0
	if decay > 0.99 {
		decay = 0.99
	}
	speed := rate
	if speed > 2 {
		speed = 2
	}
	effects := []SoxEffect{
		{
			Name: "phaser",
			Params: []string{
				fmt.Sprintf("%f", gainIn), // gain-in
				fmt.Sprintf("%f", gainOut), // gain-out
				"3",                        // delay (ms)
				fmt.Sprintf("%f", decay),   // decay
				fmt.Sprintf("%f", speed),   // speed
				"-t",                       // triangular
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
	speed := defaultFloat(req.Speed, 5)
	depth := defaultFloat(req.Depth, 40)
	if err := validateRange("speed", speed, 0.1, 30, "Hz"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("depth", depth, 0, 100, "%"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
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
				fmt.Sprintf("%f", speed),
				fmt.Sprintf("%f", depth),
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
	threshold := defaultFloat(req.Threshold, -40)
	attack := defaultFloat(req.Attack, 1)
	release := defaultFloat(req.Release, 50)
	if err := validateRange("threshold", threshold, -80, 0, "dB"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("attack", attack, 0.1, 100, "ms"); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateRange("release", release, 10, 1000, "ms"); err != nil {
		writeValidationError(w, err)
		return
	}

	sourcePath, safeName, err := resolveDAWAudioSource(req.File)
	if err != nil {
		writeDAWFileNotFound(w, safeName)
		return
	}

	outputPath, outputName, err := resolveEffectOutput("noisegate", safeName)
	if err != nil {
		writeEffectError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
		return
	}

	attackS := attack / 1000.0
	releaseS := release / 1000.0
	effects := []SoxEffect{
		{
			Name: "compand",
			Params: []string{
				fmt.Sprintf("%f,%f", attackS, releaseS),
				fmt.Sprintf("-80,-80,-80,%s,-40,-40", strconv.FormatFloat(threshold, 'f', -1, 64)),
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
