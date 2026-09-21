package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupKeyTestRoot(t *testing.T) string {
	t.Helper()
	root := setTestRoot(t, "key-test-")
	for _, dir := range []string{"input", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func TestHandleKeyDetect_POST(t *testing.T) {
	setupKeyTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/key", s.handleKeyDetect)

	origRunner := keydetectRunner
	keydetectRunner = func(inputPath string) ([]byte, error) {
		return []byte(`{"key":"C","scale":"major","strength":0.85,"alternatives":[{"key":"A","scale":"minor","strength":0.82}],"dubious":false}`), nil
	}
	defer func() { keydetectRunner = origRunner }()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "scale.wav")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.WriteString(fw, "dummy audio"); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/key", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	want := `{"key":"C","scale":"major","strength":0.85,"alternatives":[{"key":"A","scale":"minor","strength":0.82}],"dubious":false}` + "\n"
	if rr.Body.String() != want {
		t.Errorf("unexpected response body: got %q, want %q", rr.Body.String(), want)
	}
}

func TestHandleKeyDetect_GET(t *testing.T) {
	root := setupKeyTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/key", s.handleKeyDetect)

	dummyPath := filepath.Join(root, "input", "song.wav")
	if err := os.WriteFile(dummyPath, []byte("dummy audio"), 0o644); err != nil {
		t.Fatalf("failed to create dummy audio: %v", err)
	}

	origRunner := keydetectRunner
	keydetectRunner = func(inputPath string) ([]byte, error) {
		return []byte(`{"key":"A","scale":"minor","strength":0.9,"alternatives":[{"key":"C","scale":"major","strength":0.85}],"dubious":false}`), nil
	}
	defer func() { keydetectRunner = origRunner }()

	req := httptest.NewRequest(http.MethodGet, "/api/key?file=song.wav", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	want := `{"key":"A","scale":"minor","strength":0.9,"alternatives":[{"key":"C","scale":"major","strength":0.85}],"dubious":false}` + "\n"
	if rr.Body.String() != want {
		t.Errorf("unexpected response body: got %q, want %q", rr.Body.String(), want)
	}
}

func TestHandleKeyDetect_MethodNotAllowed(t *testing.T) {
	setupKeyTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/key", s.handleKeyDetect)

	req := httptest.NewRequest(http.MethodDelete, "/api/key", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleKeyDetect_POSTMissingFile(t *testing.T) {
	setupKeyTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/key", s.handleKeyDetect)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/key", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleKeyDetect_GETMissingFile(t *testing.T) {
	setupKeyTestRoot(t)
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/key", s.handleKeyDetect)

	req := httptest.NewRequest(http.MethodGet, "/api/key", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
