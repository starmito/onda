package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleStemsMerge_Validation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	cases := []struct {
		name string
		body string
		code int
	}{
		{
			name: "missing song",
			body: `{"stems":["vocals.wav"],"format":"flac"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "missing stems",
			body: `{"song":"cancion1","format":"flac"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "empty stems",
			body: `{"song":"cancion1","stems":[],"format":"flac"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "invalid format",
			body: `{"song":"cancion1","stems":["vocals.wav"],"format":"ogg"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "missing stem file",
			body: `{"song":"cancion1","stems":["vocals.wav"],"format":"flac"}`,
			code: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/stems/merge", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)

			if rr.Code != tc.code {
				t.Fatalf("expected %d, got %d: %s", tc.code, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHandleStemsMerge_DefaultFormat(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	songDir := filepath.Join(root, "output", "cancion1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, "vocals.wav"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test wav: %v\n%s", err, string(out))
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	body := `{"song":"cancion1","stems":["vocals.wav"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/stems/merge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp MergeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Format != "flac" {
		t.Errorf("expected default format flac, got %q", resp.Format)
	}
	if !strings.HasPrefix(resp.File, "merge_cancion1") {
		t.Errorf("expected file to start with merge_cancion1, got %q", resp.File)
	}
}

func TestHandleStemsMerge_HappyPath(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	songDir := filepath.Join(root, "output", "cancion1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	// Create two short silent WAV files with ffmpeg.
	for _, stem := range []string{"vocals.wav", "instrumental.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	reqBody := MergeRequest{
		Song:   "cancion1",
		Stems:  []string{"vocals.wav", "instrumental.wav"},
		Format: "flac",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/stems/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp MergeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Format != "flac" {
		t.Errorf("expected format flac, got %q", resp.Format)
	}
	if !strings.HasSuffix(resp.File, ".flac") {
		t.Errorf("expected flac file, got %q", resp.File)
	}
	if resp.Size <= 0 {
		t.Errorf("expected positive size, got %d", resp.Size)
	}

	mergePath := filepath.Join(songDir, resp.File)
	if _, err := os.Stat(mergePath); err != nil {
		t.Errorf("merged file not found at %s: %v", mergePath, err)
	}
}
