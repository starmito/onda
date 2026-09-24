package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

func TestCodecAndArgsForExt_Extended(t *testing.T) {
	cases := []struct {
		ext      string
		wantCodec string
		wantArgs  []string
	}{
		{".mp3", "libmp3lame", []string{"-b:a", "192k"}},
		{".MP3", "libmp3lame", []string{"-b:a", "192k"}},
		{".flac", "flac", []string{"-compression_level", "5"}},
		{".wav", "pcm_s16le", nil},
		{".ogg", "pcm_s16le", nil},
	}
	for _, tc := range cases {
		codec, args := codecAndArgsForExt(tc.ext)
		if codec != tc.wantCodec {
			t.Errorf("codecAndArgsForExt(%q) codec = %q, want %q", tc.ext, codec, tc.wantCodec)
		}
		if len(args) != len(tc.wantArgs) {
			t.Errorf("codecAndArgsForExt(%q) args = %v, want %v", tc.ext, args, tc.wantArgs)
			continue
		}
		for i := range args {
			if args[i] != tc.wantArgs[i] {
				t.Errorf("codecAndArgsForExt(%q) args[%d] = %q, want %q", tc.ext, i, args[i], tc.wantArgs[i])
			}
		}
	}
}

func TestClampSample(t *testing.T) {
	cases := []struct {
		value  float64
		maxVal float64
		want   int
	}{
		{0, 32768, 0},
		{32600, 32768, 32600},
		{32768, 32768, 32767},
		{-32768, 32768, -32768},
		{-40000, 32768, -32768},
		{15.4, 32768, 15},
		{-15.6, 32768, -16},
	}
	for _, tc := range cases {
		got := clampSample(tc.value, tc.maxVal)
		if got != tc.want {
			t.Errorf("clampSample(%f, %f) = %d, want %d", tc.value, tc.maxVal, got, tc.want)
		}
	}
}

func TestIsKnownEQFilterType(t *testing.T) {
	known := []string{"lowpass", "highpass", "bandpass", "notch", "peak", "lowshelf", "highshelf"}
	for _, name := range known {
		if !isKnownEQFilterType(name) {
			t.Errorf("isKnownEQFilterType(%q) = false, want true", name)
		}
		if !isKnownEQFilterType(strings.ToUpper(name)) {
			t.Errorf("isKnownEQFilterType(%q) = false, want true (case insensitive)", strings.ToUpper(name))
		}
	}
	if isKnownEQFilterType("butterworth") {
		t.Error("isKnownEQFilterType(butterworth) = true, want false")
	}
}

func TestBuildEQFilter_SilenceStaysSilent(t *testing.T) {
	const sr = 44100.0
	filters := []EqFilter{
		{Type: "peak", Freq: 1000, Gain: 12, Q: 1},
		{Type: "lowpass", Freq: 5000, Q: 1},
	}
	chains := make([][]eqProcessor, 2)
	for ch := 0; ch < 2; ch++ {
		for _, f := range filters {
			proc, err := buildEQFilter(sr, f)
			if err != nil {
				t.Fatalf("buildEQFilter failed: %v", err)
			}
			chains[ch] = append(chains[ch], proc)
		}
	}
	// Zero input through any filter chain must remain zero.
	for ch, chain := range chains {
		sample := 0.0
		for _, proc := range chain {
			sample = proc.Apply(sample)
		}
		if sample != 0 {
			t.Errorf("channel %d: silence produced %f", ch, sample)
		}
	}
}

func TestBuildEQFilter_UnknownType(t *testing.T) {
	_, err := buildEQFilter(44100, EqFilter{Type: "butterworth", Freq: 1000, Q: 1})
	if err == nil {
		t.Fatal("expected error for unknown filter type")
	}
}

func TestValidateRange_Extended(t *testing.T) {
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

func TestDefaultFloat_Extended(t *testing.T) {
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

func TestToFloat64(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
	}{
		{42, 42},
		{int64(42), 42},
		{3.14, 3.14},
		{"2.5", 2.5},
		{json.Number("7"), 7},
		{nil, 0},
		{"not-a-number", 0},
	}
	for _, tc := range cases {
		got := toFloat64(tc.in)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("toFloat64(%v (%T)) = %v, want %v", tc.in, tc.in, got, tc.want)
		}
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int
	}{
		{42, 42},
		{3.7, 4},
		{int64(5), 5},
		{"9", 9},
		{json.Number("11"), 11},
		{nil, 0},
	}
	for _, tc := range cases {
		got := toInt(tc.in)
		if got != tc.want {
			t.Errorf("toInt(%v (%T)) = %d, want %d", tc.in, tc.in, got, tc.want)
		}
	}
}

func TestToString(t *testing.T) {
	if got := toString("hello"); got != "hello" {
		t.Errorf("toString(hello) = %q, want hello", got)
	}
	if got := toString(42); got != "42" {
		t.Errorf("toString(42) = %q, want 42", got)
	}
}

// writeSilentWAVForAPI writes a tiny mono silent WAV under the given path.
func writeSilentWAVForAPI(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()
	buf := &audio.IntBuffer{
		Data:   make([]int, 4410),
		Format: &audio.Format{SampleRate: 44100, NumChannels: 1},
	}
	enc := wav.NewEncoder(f, 44100, 16, 1, 1)
	if err := enc.Write(buf); err != nil {
		t.Fatalf("failed to write wav: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close wav: %v", err)
	}
}

func TestHandleEQ_ValidationErrors(t *testing.T) {
	root := setTestRoot(t, "eq-validation-")
	if err := os.MkdirAll(filepath.Join(root, "daw-data", "testsong", "original"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	writeSilentWAVForAPI(t, filepath.Join(root, "daw-data", "testsong", "original", "test.wav"))

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/daw/eq", s.handleEQ)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{"method not allowed", `{"file":"test.wav","filters":[{"type":"peak","freq":1000,"gain":0,"q":1}]}`, http.StatusMethodNotAllowed, ""},
		{"invalid json", `not json`, http.StatusBadRequest, "invalid JSON"},
		{"missing file", `{"filters":[{"type":"peak","freq":1000,"gain":0,"q":1}]}`, http.StatusBadRequest, "file is required"},
		{"empty filters", `{"file":"test.wav"}`, http.StatusBadRequest, "filters cannot be empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodPost
			if tc.name == "method not allowed" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, "/api/daw/eq", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if tc.wantErr != "" {
				var body map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if !strings.Contains(body["error"], tc.wantErr) {
					t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
				}
			}
		})
	}
}

func TestHandleFade_ValidationErrors(t *testing.T) {
	root := setTestRoot(t, "fade-validation-")
	if err := os.MkdirAll(filepath.Join(root, "daw-data", "testsong", "original"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	writeSilentWAVForAPI(t, filepath.Join(root, "daw-data", "testsong", "original", "test.wav"))

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/audio/fade", s.handleFade)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{"invalid json", `not json`, http.StatusBadRequest, "invalid JSON"},
		{"missing file", `{"type":"in","start":0,"duration":1}`, http.StatusBadRequest, "file is required"},
		{"invalid type", `{"file":"test.wav","type":"up","start":0,"duration":1}`, http.StatusBadRequest, "type must be"},
		{"negative start", `{"file":"test.wav","type":"in","start":-1,"duration":1}`, http.StatusBadRequest, "start must be >= 0"},
		{"no duration", `{"file":"test.wav","type":"in","start":0}`, http.StatusBadRequest, "duration"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/audio/fade", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantErr != "" {
				var body map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if !strings.Contains(body["error"], tc.wantErr) {
					t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
				}
			}
		})
	}
}

func TestHandleTrim_ValidationErrors(t *testing.T) {
	root := setTestRoot(t, "trim-validation-")
	if err := os.MkdirAll(filepath.Join(root, "daw-data", "testsong", "original"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	writeSilentWAVForAPI(t, filepath.Join(root, "daw-data", "testsong", "original", "test.wav"))

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/audio/trim", s.handleTrim)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{"invalid json", `not json`, http.StatusBadRequest, "invalid JSON"},
		{"missing file", `{"start":0,"end":1}`, http.StatusBadRequest, "file is required"},
		{"negative start", `{"file":"test.wav","start":-1,"end":1}`, http.StatusBadRequest, "start must be >= 0"},
		{"end before start", `{"file":"test.wav","start":2,"end":1}`, http.StatusBadRequest, "end must be greater than start"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/audio/trim", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantErr != "" {
				var body map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if !strings.Contains(body["error"], tc.wantErr) {
					t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
				}
			}
		})
	}
}

func TestHandleStorageUsage_MethodNotAllowed(t *testing.T) {
	setTestRoot(t, "storage-method-")
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/storage/usage", s.handleStorageUsage)
	req := httptest.NewRequest(http.MethodPost, "/api/storage/usage", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleStorageClean_InvalidJSON(t *testing.T) {
	setTestRoot(t, "storage-clean-json-")
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/storage/clean", s.handleStorageClean)
	req := httptest.NewRequest(http.MethodPost, "/api/storage/clean", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("invalid JSON")) {
		t.Errorf("expected invalid JSON error, got %s", rr.Body.String())
	}
}

func TestHandleGetPresets_ReturnsBuiltIns(t *testing.T) {
	setTestRoot(t, "presets-get-")
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	req := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	for _, name := range []string{"Voces Total", "Eliminador de Voz", "Separador Completo", "Solo Instrumentos"} {
		if _, ok := body[name]; !ok {
			t.Errorf("missing built-in preset %q", name)
		}
	}
}

func TestHandleSavePreset_Validation(t *testing.T) {
	setTestRoot(t, "presets-save-")
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/presets", s.handleSavePreset)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{"invalid json", `not json`, http.StatusBadRequest, "invalid JSON"},
		{"missing name", `{"steps":[]}`, http.StatusBadRequest, "preset name is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/presets", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if tc.wantErr != "" {
				var body map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if !strings.Contains(body["error"], tc.wantErr) {
					t.Errorf("error = %q, want substring %q", body["error"], tc.wantErr)
				}
			}
		})
	}
}

func TestHandleDeletePreset_FactoryAllowed(t *testing.T) {
	root := setTestRoot(t, "presets-delete-")
	t.Setenv("ONDA_DATA_DIR", root)
	resetPresetsState(t)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)

	// Deleting a locked built-in preset is now allowed and tombstoned.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/presets/Voces%20Total", nil)
	rr2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr2.Code)
	}
}

