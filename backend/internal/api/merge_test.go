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
	setTestRoot(t, "merge-")

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

	root := setTestRoot(t, "merge-")

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

func TestHandleStemsMerge_OutputName(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "merge-")

	songDir := filepath.Join(root, "output", "Mi Canción")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, "vocals.wav"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test wav: %v\n%s", err, string(out))
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	cases := []struct {
		name       string
		outputName string
		wantFile   string
	}{
		{
			name:       "custom output name",
			outputName: "Mi Canción - mezcla (mix)",
			wantFile:   "Mi Canción - mezcla (mix).mp3",
		},
		{
			name:       "empty output name falls back to merge_song",
			outputName: "",
			wantFile:   "merge_Mi Canción.mp3",
		},
		{
			name:       "traversal sanitized",
			outputName: "../malicious/name",
			wantFile:   ".._malicious_name.mp3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := MergeRequest{
				Song:       "Mi Canción",
				Stems:      []string{"vocals.wav"},
				Format:     "mp3",
				OutputName: tc.outputName,
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
			if resp.File != tc.wantFile {
				t.Errorf("expected file %q, got %q", tc.wantFile, resp.File)
			}

			mergePath := filepath.Join(songDir, resp.File)
			if _, err := os.Stat(mergePath); err != nil {
				t.Errorf("merged file not found at %s: %v", mergePath, err)
			}
		})
	}
}

func TestHandleStemsMerge_HappyPath(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "merge-")

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

func TestHandleStemsMerge_PitchSubgroup(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "merge-")

	songDir := filepath.Join(root, "output", "Base", "Base_pitch-1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create pitch subgroup dir: %v", err)
	}

	for _, stem := range []string{"bass_pitch-1.wav", "drums.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	reqBody := MergeRequest{
		Song:   "Base (pitch -1)",
		Stems:  []string{"bass_pitch-1.wav", "drums.wav"},
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

	// The merged file must live under the base song dir, not inside the
	// pitch-shifted stem subdirectory, so the static handler can serve it.
	baseSongDir := filepath.Join(root, "output", "Base")
	mergePath := filepath.Join(baseSongDir, resp.File)
	if _, err := os.Stat(mergePath); err != nil {
		t.Errorf("merged file not found at %s: %v", mergePath, err)
	}

	wantURL := "/output/Base/" + resp.File
	if resp.URL != wantURL {
		t.Errorf("expected download URL %q, got %q", wantURL, resp.URL)
	}
}

func TestHandleStemsMerge_PitchSubgroupWithoutPitchStemsStillWorks(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "merge-")

	songDir := filepath.Join(root, "output", "Normal")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create normal song dir: %v", err)
	}

	for _, stem := range []string{"bass.wav", "drums.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	reqBody := MergeRequest{
		Song:   "Normal",
		Stems:  []string{"bass.wav", "drums.wav"},
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

	mergePath := filepath.Join(songDir, "merge_Normal.flac")
	if _, err := os.Stat(mergePath); err != nil {
		t.Errorf("merged file not found at %s: %v", mergePath, err)
	}
}

func TestHandleStemsMerge_ResolutionGroupAndSubgroup(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "merge-")

	baseSong := "Mi Canción"
	baseDir := filepath.Join(root, "output", baseSong)
	pitchStemsDir := filepath.Join(baseDir, baseSong+"_pitch+1")
	if err := os.MkdirAll(pitchStemsDir, 0o755); err != nil {
		t.Fatalf("failed to create pitch subgroup dir: %v", err)
	}

	for _, stem := range []string{"bass_pitch+1.wav", "drums.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(pitchStemsDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)

	cases := []struct {
		name       string
		song       string
		stems      []string
		outputName string
		wantPath   string
		wantURL    string
	}{
		{
			name:       "original group",
			song:       "Otra",
			stems:      []string{"vocals.wav"},
			outputName: "Otra (0).flac",
			wantPath:   filepath.Join(root, "output", "Otra", "Otra (0).flac"),
			wantURL:    "/output/Otra/Otra (0).flac",
		},
		{
			name:       "pitch subgroup",
			song:       baseSong + " (pitch +1)",
			stems:      []string{"bass_pitch+1.wav", "drums.wav"},
			outputName: baseSong + " (+1).flac",
			wantPath:   filepath.Join(baseDir, baseSong+" (+1).flac"),
			wantURL:    "/output/" + baseSong + "/" + baseSong + " (+1).flac",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "original group" {
				otherDir := filepath.Join(root, "output", "Otra")
				if err := os.MkdirAll(otherDir, 0o755); err != nil {
					t.Fatalf("failed to create group dir: %v", err)
				}
				cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(otherDir, "vocals.wav"))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("failed to create test wav: %v\n%s", err, string(out))
				}
			}

			reqBody := MergeRequest{
				Song:       tc.song,
				Stems:      tc.stems,
				Format:     "flac",
				OutputName: tc.outputName,
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
			if resp.URL != tc.wantURL {
				t.Errorf("expected URL %q, got %q", tc.wantURL, resp.URL)
			}
			if _, err := os.Stat(tc.wantPath); err != nil {
				t.Errorf("merged file not found at %s: %v", tc.wantPath, err)
			}
		})
	}
}
