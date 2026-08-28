package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupPitchTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "pitch-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"output", "input", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func newPitchTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/pitch", s.handlePitchShift)
	return s
}

func TestHandlePitchShift_MissingSong(t *testing.T) {
	setupPitchTestRoot(t)
	s := newPitchTestServer(t)

	body := `{"song":"","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_ZeroPitch(t *testing.T) {
	setupPitchTestRoot(t)
	s := newPitchTestServer(t)

	body := `{"song":"song","pitch":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_InvalidJSON(t *testing.T) {
	setupPitchTestRoot(t)
	s := newPitchTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_MissingSongDir(t *testing.T) {
	setupPitchTestRoot(t)
	s := newPitchTestServer(t)

	body := `{"song":"missing","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_PathTraversal(t *testing.T) {
	root := setupPitchTestRoot(t)
	// Create a directory outside output that the traversal attempt would target.
	os.MkdirAll(filepath.Join(root, "output", "..", "safe"), 0o755)

	s := newPitchTestServer(t)
	body := `{"song":"../../safe","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_NoAudioFiles(t *testing.T) {
	root := setupPitchTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "output", "song"), 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "output", "song", "readme.txt"), []byte("txt"), 0o644); err != nil {
		t.Fatalf("failed to write text file: %v", err)
	}

	s := newPitchTestServer(t)
	body := `{"song":"song","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlePitchShift_CopiesDrums(t *testing.T) {
	skipIfMissingBinary(t, "rubberband")
	root := setupPitchTestRoot(t)
	songDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	drums := []byte("drums-audio")
	if err := os.WriteFile(filepath.Join(songDir, "drums.wav"), drums, 0o644); err != nil {
		t.Fatalf("failed to write drums: %v", err)
	}

	s := newPitchTestServer(t)
	body := `{"song":"song","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	copied := filepath.Join(songDir, "song_pitch+2", "drums.wav")
	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("drums not copied: %v", err)
	}
	if !bytes.Equal(data, drums) {
		t.Errorf("drums content changed after copy")
	}
}

func TestHandlePitchShift_ShiftsNonDrums(t *testing.T) {
	skipIfMissingBinary(t, "rubberband")
	root := setupPitchTestRoot(t)
	songDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	// Write a minimal valid WAV (1s silence, mono, 16-bit, 44100 Hz).
	writeTestWAV(t, filepath.Join(songDir, "vocals.wav"))

	s := newPitchTestServer(t)
	body := `{"song":"song","pitch":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/pitch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp PitchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Pitch != 2 {
		t.Errorf("expected pitch 2, got %d", resp.Pitch)
	}

	pitched := filepath.Join(songDir, "song_pitch+2", "vocals_pitch+2.wav")
	if _, err := os.Stat(pitched); err != nil {
		t.Fatalf("expected pitched vocals file: %v", err)
	}
}

// writeTestWAV writes a tiny mono 16-bit WAV header + silence.
func writeTestWAV(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create wav dir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()

	const (
		sampleRate = 44100
		channels   = 1
		bits       = 16
		duration   = 1
	)
	dataSize := sampleRate * duration * channels * bits / 8
	header := []byte{
		'R', 'I', 'F', 'F',
		byte((36 + dataSize) & 0xff), byte((36 + dataSize) >> 8 & 0xff), byte((36 + dataSize) >> 16 & 0xff), byte((36 + dataSize) >> 24 & 0xff),
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0,
		1, 0,
		channels, 0,
		sampleRate & 0xff, sampleRate >> 8 & 0xff, sampleRate >> 16 & 0xff, sampleRate >> 24 & 0xff,
		(sampleRate * channels * bits / 8) & 0xff, (sampleRate * channels * bits / 8) >> 8 & 0xff, (sampleRate * channels * bits / 8) >> 16 & 0xff, (sampleRate * channels * bits / 8) >> 24 & 0xff,
		channels * bits / 8, 0,
		bits, 0,
		'd', 'a', 't', 'a',
		byte(dataSize & 0xff), byte(dataSize >> 8 & 0xff), byte(dataSize >> 16 & 0xff), byte(dataSize >> 24 & 0xff),
	}
	if _, err := f.Write(header); err != nil {
		t.Fatalf("failed to write wav header: %v", err)
	}
	if _, err := f.Write(make([]byte, dataSize)); err != nil {
		t.Fatalf("failed to write wav data: %v", err)
	}
}
