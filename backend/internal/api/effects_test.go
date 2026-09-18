package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// newEffectsTestServer registers the DAW effect handlers on a fresh mux.
func newEffectsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/daw/eq", s.handleEQ)
	s.mux.HandleFunc("POST /api/daw/compressor", s.handleCompressor)
	s.mux.HandleFunc("POST /api/daw/reverb", s.handleReverb)
	s.mux.HandleFunc("POST /api/daw/delay", s.handleDelay)
	s.mux.HandleFunc("POST /api/daw/chorus", s.handleChorus)
	s.mux.HandleFunc("POST /api/daw/flanger", s.handleFlanger)
	s.mux.HandleFunc("POST /api/daw/phaser", s.handlePhaser)
	s.mux.HandleFunc("POST /api/daw/tremolo", s.handleTremolo)
	s.mux.HandleFunc("POST /api/daw/noisegate", s.handleNoiseGate)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

// writeEffectsTestWAV creates a tiny stereo PCM WAV in the temporary project root.
func writeEffectsTestWAV(t *testing.T, root string) string {
	t.Helper()
	dawDir := filepath.Join(root, "daw-data")
	if err := os.MkdirAll(dawDir, 0o755); err != nil {
		t.Fatalf("failed to create daw-data: %v", err)
	}
	path := filepath.Join(dawDir, "test.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()

	buf := &audio.IntBuffer{
		Data:   make([]int, 44100*2), // 1 second stereo silence
		Format: &audio.Format{SampleRate: 44100, NumChannels: 2},
	}
	enc := wav.NewEncoder(f, 44100, 16, 2, 1)
	if err := enc.Write(buf); err != nil {
		t.Fatalf("failed to write wav: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close wav encoder: %v", err)
	}
	return path
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	return resp
}

func TestDefaultFloat(t *testing.T) {
	v := 3.14
	if got := defaultFloat(nil, 1.0); got != 1.0 {
		t.Errorf("defaultFloat(nil, 1.0) = %v, want 1.0", got)
	}
	if got := defaultFloat(&v, 1.0); got != 3.14 {
		t.Errorf("defaultFloat(&v, 1.0) = %v, want 3.14", got)
	}
	zero := 0.0
	if got := defaultFloat(&zero, 1.0); got != 0.0 {
		t.Errorf("defaultFloat(&zero, 1.0) = %v, want 0.0", got)
	}
}

func TestValidateRange(t *testing.T) {
	if err := validateRange("x", 5, 0, 10, ""); err != nil {
		t.Errorf("validateRange(5, 0, 10) unexpected error: %v", err)
	}
	if err := validateRange("x", -1, 0, 10, ""); err == nil {
		t.Error("validateRange(-1, 0, 10) expected error")
	}
	if err := validateRange("x", 11, 0, 10, ""); err == nil {
		t.Error("validateRange(11, 0, 10) expected error")
	}
	msg := validateRange("gain", 25, -24, 24, "dB").Error()
	if !strings.Contains(msg, "gain") || !strings.Contains(msg, "-24") || !strings.Contains(msg, "24") {
		t.Errorf("validateRange error message unclear: %q", msg)
	}
}

func TestEQValidation(t *testing.T) {
	root := setupFase10TestRoot(t)
	srv := newEffectsTestServer(t)
	writeEffectsTestWAV(t, root)

	cases := []struct {
		name       string
		filters    []EqFilter
		wantStatus int
		wantErr    string
	}{
		{
			name:       "valid peak",
			filters:    []EqFilter{{Type: "peak", Freq: 1000, Gain: 0, Q: 1}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "freq too low",
			filters:    []EqFilter{{Type: "peak", Freq: 10, Gain: 0, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "freq must be between 20 and 20000 Hz",
		},
		{
			name:       "freq too high",
			filters:    []EqFilter{{Type: "peak", Freq: 25000, Gain: 0, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "freq must be between 20 and 20000 Hz",
		},
		{
			name:       "q too low",
			filters:    []EqFilter{{Type: "peak", Freq: 1000, Gain: 0, Q: 0.05}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "q must be between 0.1 and 10",
		},
		{
			name:       "q too high",
			filters:    []EqFilter{{Type: "peak", Freq: 1000, Gain: 0, Q: 11}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "q must be between 0.1 and 10",
		},
		{
			name:       "gain too low",
			filters:    []EqFilter{{Type: "peak", Freq: 1000, Gain: -30, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "gain must be between -24 and 24 dB",
		},
		{
			name:       "gain too high",
			filters:    []EqFilter{{Type: "peak", Freq: 1000, Gain: 30, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "gain must be between -24 and 24 dB",
		},
		{
			name:       "unknown filter type",
			filters:    []EqFilter{{Type: "butterworth", Freq: 1000, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "unknown filter type",
		},
		{
			name:       "missing type",
			filters:    []EqFilter{{Type: "", Freq: 1000, Q: 1}},
			wantStatus: http.StatusBadRequest,
			wantErr:    "type is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv, "/api/daw/eq", map[string]any{
				"file":    "test.wav",
				"filters": tc.filters,
			})
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantErr != "" {
				var body map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if !strings.Contains(body["error"], tc.wantErr) {
					t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
				}
			}
		})
	}
}

func TestEffectValidation400(t *testing.T) {
	root := setupFase10TestRoot(t)
	srv := newEffectsTestServer(t)
	writeEffectsTestWAV(t, root)

	cases := []struct {
		name       string
		path       string
		body       map[string]any
		wantErr    string
	}{
		{
			name: "compressor threshold out of range",
			path: "/api/daw/compressor",
			body: map[string]any{"file": "test.wav", "threshold": -70, "ratio": 4, "attack": 5, "release": 50, "makeup": 0},
			wantErr: "threshold must be between -60.00 and 0.00 dB",
		},
		{
			name: "compressor explicit zero threshold allowed",
			path: "/api/daw/compressor",
			body: map[string]any{"file": "test.wav", "threshold": 0, "ratio": 4, "attack": 5, "release": 50, "makeup": 0},
			wantErr: "",
		},
		{
			name: "compressor threshold positive out of range",
			path: "/api/daw/compressor",
			body: map[string]any{"file": "test.wav", "threshold": 1, "ratio": 4, "attack": 5, "release": 50, "makeup": 0},
			wantErr: "threshold must be between -60.00 and 0.00 dB",
		},
		{
			name: "delay time too low",
			path: "/api/daw/delay",
			body: map[string]any{"file": "test.wav", "delay_time": 0.01, "feedback": 30, "wet_dry": 50},
			wantErr: "delay_time must be between 0.03 and 5.00 seconds",
		},
		{
			name: "delay feedback out of range",
			path: "/api/daw/delay",
			body: map[string]any{"file": "test.wav", "delay_time": 0.3, "feedback": 101, "wet_dry": 50},
			wantErr: "feedback must be between 0.00 and 100.00 %",
		},
		{
			name: "chorus delay too low",
			path: "/api/daw/chorus",
			body: map[string]any{"file": "test.wav", "depth": 3, "rate": 0.5, "delay_ms": 15, "wet_dry": 50},
			wantErr: "delay_ms must be between 20.00 and 100.00 ms",
		},
		{
			name: "chorus rate too high",
			path: "/api/daw/chorus",
			body: map[string]any{"file": "test.wav", "depth": 3, "rate": 6, "delay_ms": 40, "wet_dry": 50},
			wantErr: "rate must be between 0.10 and 5.00 Hz",
		},
		{
			name: "reverb room_size out of range",
			path: "/api/daw/reverb",
			body: map[string]any{"file": "test.wav", "room_size": 101, "decay": 50, "wet_dry": 50},
			wantErr: "room_size must be between 0.00 and 100.00",
		},
		{
			name: "reverb decay out of range",
			path: "/api/daw/reverb",
			body: map[string]any{"file": "test.wav", "room_size": 50, "decay": 101, "wet_dry": 50},
			wantErr: "decay must be between 0.00 and 100.00",
		},
		{
			name: "flanger depth out of range",
			path: "/api/daw/flanger",
			body: map[string]any{"file": "test.wav", "depth": 11, "rate": 0.5, "wet_dry": 50},
			wantErr: "depth must be between 0.00 and 10.00",
		},
		{
			name: "phaser rate out of range",
			path: "/api/daw/phaser",
			body: map[string]any{"file": "test.wav", "depth": 3, "rate": 11, "wet_dry": 50},
			wantErr: "rate must be between 0.10 and 10.00 Hz",
		},
		{
			name: "tremolo speed out of range",
			path: "/api/daw/tremolo",
			body: map[string]any{"file": "test.wav", "speed": 31, "depth": 50},
			wantErr: "speed must be between 0.10 and 30.00 Hz",
		},
		{
			name: "noisegate explicit zero threshold allowed",
			path: "/api/daw/noisegate",
			body: map[string]any{"file": "test.wav", "threshold": 0, "attack": 5, "release": 50},
			wantErr: "",
		},
		{
			name: "noisegate release out of range",
			path: "/api/daw/noisegate",
			body: map[string]any{"file": "test.wav", "threshold": -40, "attack": 5, "release": 1001},
			wantErr: "release must be between 10.00 and 1000.00 ms",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv, tc.path, tc.body)
			defer resp.Body.Close()
			if tc.wantErr == "" {
				if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
					t.Errorf("status = %d, want 200 or 500 (sox may be missing)", resp.StatusCode)
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			if !strings.Contains(body["error"], tc.wantErr) {
				t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
			}
		})
	}
}

func TestEffectValidParametersNo500(t *testing.T) {
	skipIfMissingBinary(t, "sox")

	root := setupFase10TestRoot(t)
	srv := newEffectsTestServer(t)
	writeEffectsTestWAV(t, root)

	cases := []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "eq extremes",
			path: "/api/daw/eq",
			body: map[string]any{
				"file": "test.wav",
				"filters": []EqFilter{
					{Type: "peak", Freq: 20, Gain: -24, Q: 0.1},
					{Type: "highshelf", Freq: 20000, Gain: 24, Q: 10},
					{Type: "lowpass", Freq: 1000, Q: 1},
				},
			},
		},
		{
			name: "compressor extremes",
			path: "/api/daw/compressor",
			body: map[string]any{"file": "test.wav", "threshold": -60, "ratio": 1, "attack": 0.1, "release": 10, "makeup": 0},
		},
		{
			name: "compressor explicit zeros",
			path: "/api/daw/compressor",
			body: map[string]any{"file": "test.wav", "threshold": 0, "ratio": 4, "attack": 5, "release": 50, "makeup": 0},
		},
		{
			name: "reverb extremes",
			path: "/api/daw/reverb",
			body: map[string]any{"file": "test.wav", "room_size": 0, "decay": 0, "wet_dry": 0},
		},
		{
			name: "delay extremes",
			path: "/api/daw/delay",
			body: map[string]any{"file": "test.wav", "delay_time": 0.03, "feedback": 0, "wet_dry": 0},
		},
		{
			name: "delay long",
			path: "/api/daw/delay",
			body: map[string]any{"file": "test.wav", "delay_time": 5, "feedback": 100, "wet_dry": 100},
		},
		{
			name: "chorus extremes",
			path: "/api/daw/chorus",
			body: map[string]any{"file": "test.wav", "depth": 0, "rate": 0.1, "delay_ms": 20, "wet_dry": 0},
		},
		{
			name: "chorus max rate",
			path: "/api/daw/chorus",
			body: map[string]any{"file": "test.wav", "depth": 10, "rate": 5, "delay_ms": 100, "wet_dry": 100},
		},
		{
			name: "flanger extremes",
			path: "/api/daw/flanger",
			body: map[string]any{"file": "test.wav", "depth": 10, "rate": 10, "wet_dry": 100},
		},
		{
			name: "phaser extremes",
			path: "/api/daw/phaser",
			body: map[string]any{"file": "test.wav", "depth": 10, "rate": 10, "wet_dry": 100},
		},
		{
			name: "tremolo extremes",
			path: "/api/daw/tremolo",
			body: map[string]any{"file": "test.wav", "speed": 30, "depth": 100},
		},
		{
			name: "noisegate extremes",
			path: "/api/daw/noisegate",
			body: map[string]any{"file": "test.wav", "threshold": -80, "attack": 0.1, "release": 10},
		},
		{
			name: "noisegate explicit zero threshold",
			path: "/api/daw/noisegate",
			body: map[string]any{"file": "test.wav", "threshold": 0, "attack": 5, "release": 50},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv, tc.path, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				var body map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&body)
				t.Errorf("status = %d, want 200; error=%q", resp.StatusCode, body["error"])
			}
		})
	}
}

// rmsLevelDB returns the "RMS lev dB" value reported by `sox <file> -n stat`.
func rmsLevelDB(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("sox", path, "-n", "stat").CombinedOutput()
	if err != nil {
		t.Fatalf("sox stat failed for %s: %v\n%s", path, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "RMS") && strings.Contains(line, "lev dB") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err == nil {
				return v
			}
		}
	}
	t.Fatalf("could not parse RMS lev dB from sox stat output for %s:\n%s", path, out)
	return 0
}

// TestCompressorExplicitZeroThreshold verifies that threshold=0 is treated as
// an explicit value, not as a missing field, and is returned in the response.
func TestCompressorExplicitZeroThreshold(t *testing.T) {
	skipIfMissingBinary(t, "sox")

	root := setupFase10TestRoot(t)
	srv := newEffectsTestServer(t)
	writeSynthWAV(t, filepath.Join(root, "input", "test.wav"), 2.0)

	resp := postJSON(t, srv, "/api/daw/compressor", map[string]any{
		"file":      "test.wav",
		"threshold": 0,
		"ratio":     4,
		"attack":    5,
		"release":   50,
		"makeup":    0,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body EffectResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	got, ok := body.Parameters["threshold"]
	if !ok {
		t.Fatalf("response missing threshold parameter")
	}
	if got != 0 {
		t.Errorf("threshold = %v, want 0", got)
	}
}

// TestReverbHighValuesAudible applies reverb with the maximum SoX-range values
// and checks that the output file is larger and has a different RMS level than
// the input, proving the effect is actually audible.
func TestReverbHighValuesAudible(t *testing.T) {
	skipIfMissingBinary(t, "sox")

	root := setupFase10TestRoot(t)
	srv := newEffectsTestServer(t)
	inputPath := filepath.Join(root, "daw-data", "reverb_test_input.wav")
	writeSynthWAV(t, inputPath, 2.0)

	resp := postJSON(t, srv, "/api/daw/reverb", map[string]any{
		"file":      "reverb_test_input.wav",
		"room_size": 100,
		"decay":     100,
		"wet_dry":   100,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status = %d, want 200; error=%q", resp.StatusCode, body["error"])
	}
	var body EffectResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	outputPath := filepath.Join(root, "daw-data", body.File)
	inInfo, err := os.Stat(inputPath)
	if err != nil {
		t.Fatalf("failed to stat input: %v", err)
	}
	outInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("failed to stat output: %v", err)
	}

	inRMS := rmsLevelDB(t, inputPath)
	outRMS := rmsLevelDB(t, outputPath)

	t.Logf("input size=%d output size=%d input RMS=%.2f dB output RMS=%.2f dB",
		inInfo.Size(), outInfo.Size(), inRMS, outRMS)

	if outInfo.Size() <= inInfo.Size() {
		t.Errorf("output size %d is not larger than input size %d; reverb tail not present", outInfo.Size(), inInfo.Size())
	}
	if math.Abs(outRMS-inRMS) < 1.0 {
		t.Errorf("RMS difference too small (%.2f dB); reverb did not audibly change the signal", math.Abs(outRMS-inRMS))
	}
}
