package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTempoTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "tempo-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"input", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func TestDetectBPM_UnknownBPMSetsUndetectableError(t *testing.T) {
	origRunner := aubioTempoRunner
	aubioTempoRunner = func(inputPath string) ([]byte, error) {
		return []byte("unknown bpm\n"), nil
	}
	defer func() { aubioTempoRunner = origRunner }()

	_, err := detectBPM("/fake/path.wav")
	if err == nil {
		t.Fatal("expected error for unknown bpm")
	}
	if !strings.Contains(err.Error(), "unknown bpm") {
		t.Errorf("expected error to mention unknown bpm, got %v", err)
	}
}

func TestDetectBPM_ParsesValidOutput(t *testing.T) {
	origRunner := aubioTempoRunner
	aubioTempoRunner = func(inputPath string) ([]byte, error) {
		return []byte("120.00 bpm\n"), nil
	}
	defer func() { aubioTempoRunner = origRunner }()

	bpm, err := detectBPM("/fake/path.wav")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bpm != 120.0 {
		t.Errorf("expected BPM 120.0, got %v", bpm)
	}
}

func TestHandleTempo_UnknownBPMReturnsReadableError(t *testing.T) {
	root := setupTempoTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/audio/tempo", s.handleTempo)

	dummyPath := filepath.Join(root, "input", "sine.wav")
	if err := os.WriteFile(dummyPath, []byte("dummy audio"), 0o644); err != nil {
		t.Fatalf("failed to create dummy audio: %v", err)
	}

	origRunner := aubioTempoRunner
	aubioTempoRunner = func(inputPath string) ([]byte, error) {
		return []byte("unknown bpm\n"), nil
	}
	defer func() { aubioTempoRunner = origRunner }()

	req := httptest.NewRequest(http.MethodGet, "/api/audio/tempo?file=sine.wav", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if body["error"] != "no se pudo detectar el tempo del archivo" {
		t.Errorf("unexpected error message: %q", body["error"])
	}
	if !strings.Contains(body["detail"], "unknown bpm") {
		t.Errorf("expected detail to mention unknown bpm, got %q", body["detail"])
	}
}
