package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ── Safe queue cancellation tests ───────────────────────────────────────────

func TestCancelCurrentJob_KillsProcessGroupAndTrackedPID(t *testing.T) {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs:     make(map[string]*JobState),
	}

	var cancelled bool
	s.currentCancel = func() { cancelled = true }
	s.currentPID = 12345

	var groupPIDs []int
	origKillGroup := killProcessGroup
	killProcessGroup = func(pgid int) error {
		groupPIDs = append(groupPIDs, pgid)
		return nil
	}
	defer func() { killProcessGroup = origKillGroup }()

	var killedPIDs []int
	origKill := killProcess
	killProcess = func(pid int) error {
		killedPIDs = append(killedPIDs, pid)
		return nil
	}
	defer func() { killProcess = origKill }()

	s.jobsMu.Lock()
	s.cancelCurrentJob()
	s.jobsMu.Unlock()

	if !cancelled {
		t.Error("expected context cancel function to be called")
	}
	if len(groupPIDs) != 1 || groupPIDs[0] != -12345 {
		t.Errorf("expected killProcessGroup to target group -12345, got %v", groupPIDs)
	}
	if len(killedPIDs) != 1 || killedPIDs[0] != 12345 {
		t.Errorf("expected kill to target PID 12345, got %v", killedPIDs)
	}
	if s.currentPID != 0 {
		t.Errorf("expected currentPID to be reset, got %d", s.currentPID)
	}
	if s.currentCancel != nil {
		t.Error("expected currentCancel to be reset")
	}
}

func TestCancelCurrentJob_NoProcess_DoesNothing(t *testing.T) {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs:     make(map[string]*JobState),
	}

	var cancelled bool
	s.currentCancel = func() { cancelled = true }
	s.currentPID = 0

	var groupPIDs []int
	origKillGroup := killProcessGroup
	killProcessGroup = func(pgid int) error {
		groupPIDs = append(groupPIDs, pgid)
		return nil
	}
	defer func() { killProcessGroup = origKillGroup }()

	var killedPIDs []int
	origKill := killProcess
	killProcess = func(pid int) error {
		killedPIDs = append(killedPIDs, pid)
		return nil
	}
	defer func() { killProcess = origKill }()

	s.jobsMu.Lock()
	s.cancelCurrentJob()
	s.jobsMu.Unlock()

	if !cancelled {
		t.Error("expected context cancel function to be called")
	}
	if len(groupPIDs) != 0 {
		t.Errorf("expected no group kill calls without a tracked PID, got %v", groupPIDs)
	}
	if len(killedPIDs) != 0 {
		t.Errorf("expected no kill calls without a tracked PID, got %v", killedPIDs)
	}
}

func TestHandleQueueCancel_WithoutRunningJob_IsSafe(t *testing.T) {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs:     make(map[string]*JobState),
	}

	var groupPIDs []int
	origKillGroup := killProcessGroup
	killProcessGroup = func(pgid int) error {
		groupPIDs = append(groupPIDs, pgid)
		return nil
	}
	defer func() { killProcessGroup = origKillGroup }()

	var killedPIDs []int
	origKill := killProcess
	killProcess = func(pid int) error {
		killedPIDs = append(killedPIDs, pid)
		return nil
	}
	defer func() { killProcess = origKill }()

	req := httptest.NewRequest(http.MethodPost, "/api/queue/cancel", nil)
	rr := httptest.NewRecorder()
	s.handleQueueCancel(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(groupPIDs) != 0 {
		t.Errorf("expected no group kill calls when no job is running, got %v", groupPIDs)
	}
	if len(killedPIDs) != 0 {
		t.Errorf("expected no processes killed when no job is running, got %v", killedPIDs)
	}
}

// ── CORS tests ──────────────────────────────────────────────────────────────

func TestCorsOrigins_DefaultWildcard(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "")
	origins := corsOrigins()
	if len(origins) != 1 || origins[0] != "*" {
		t.Errorf("expected default wildcard, got %v", origins)
	}
}

func TestCorsOrigins_ParseList(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "http://localhost:3000, https://onda.example.com ")
	origins := corsOrigins()
	want := []string{"http://localhost:3000", "https://onda.example.com"}
	if len(origins) != len(want) {
		t.Fatalf("expected %v, got %v", want, origins)
	}
	for i, o := range want {
		if origins[i] != o {
			t.Errorf("origin %d: expected %q, got %q", i, o, origins[i])
		}
	}
}

func TestCorsMiddleware_AllowsConfiguredOrigin(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "http://localhost:3000")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := s.corsMiddleware(s.mux)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("expected allowed origin header, got %q", got)
	}
}

func TestCorsMiddleware_RejectsUnknownOrigin(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "http://localhost:3000")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := s.corsMiddleware(s.mux)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for unknown origin, got %q", got)
	}
}

func TestCorsMiddleware_DefaultWildcard(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := s.corsMiddleware(s.mux)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected wildcard CORS header, got %q", got)
	}
}

func TestCorsMiddleware_OptionsAllowed(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "http://localhost:3000")

	s := &Server{mux: http.NewServeMux()}
	handler := s.corsMiddleware(s.mux)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestCorsMiddleware_OptionsRejected(t *testing.T) {
	t.Setenv("ONDA_CORS_ORIGINS", "http://localhost:3000")

	s := &Server{mux: http.NewServeMux()}
	handler := s.corsMiddleware(s.mux)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// ── Upload filename sanitization tests ──────────────────────────────────────

func TestSanitizeUploadFilename_AcceptsValidAudioName(t *testing.T) {
	for _, name := range []string{"song.wav", "my-track_2.mp3", "concert (live).flac"} {
		got, err := sanitizeUploadFilename(name)
		if err != nil {
			t.Errorf("%q should be valid, got %v", name, err)
			continue
		}
		if got != name {
			t.Errorf("%q should be preserved, got %q", name, got)
		}
	}
}

func TestSanitizeUploadFilename_AcceptsRealSongNames(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Así Fué (-3)(A) & mas.wav", "Así Fué (-3)(A) & mas.wav"},
		{"Cañón (Live) [2024].mp3", "Cañón (Live) [2024].mp3"},
		{"Don't Stop Believin'.flac", "Don't Stop Believin'.flac"},
		{"Rock & Roll, Part II.ogg", "Rock & Roll, Part II.ogg"},
		{"Mi    canción  favorita.m4a", "Mi canción favorita.m4a"},
	}
	for _, c := range cases {
		got, err := sanitizeUploadFilename(c.input)
		if err != nil {
			t.Errorf("%q should be valid, got %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("sanitizeUploadFilename(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSanitizeUploadFilename_ReplacesUnsafeCharacters(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"song;rm -rf .wav", "song_rm -rf.wav"},
		{"song|pipe.wav", "song_pipe.wav"},
		{"song$HOME.wav", "song_HOME.wav"},
		{"song`whoami`.wav", "song_whoami_.wav"},
		{"song  with\ttabs.wav", "song with tabs.wav"},
		{"  leading spaces.wav", "leading spaces.wav"},
		{"trailing spaces  .mp3", "trailing spaces.mp3"},
		{"only___special#@!.wav", "only___special#@!.wav"},
	}
	for _, c := range cases {
		got, err := sanitizeUploadFilename(c.input)
		if err != nil {
			t.Errorf("%q should be valid, got %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("sanitizeUploadFilename(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSanitizeUploadFilename_RejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../song.wav", "..\\song.wav", "foo/../../etc/passwd.wav", "../../../etc/shadow.mp3"} {
		if _, err := sanitizeUploadFilename(name); err == nil {
			t.Errorf("%q should be rejected", name)
		}
	}
}

func TestSanitizeUploadFilename_RejectsDoubleAudioExtension(t *testing.T) {
	for _, name := range []string{"song.mp3.flac", "song.wav.mp3", "song.flac.ogg"} {
		if _, err := sanitizeUploadFilename(name); err == nil {
			t.Errorf("%q should be rejected", name)
		}
	}
}

func TestSanitizeUploadFilename_RejectsNonAudioExtension(t *testing.T) {
	for _, name := range []string{"song.exe", "song.txt", "song.pdf", "song"} {
		if _, err := sanitizeUploadFilename(name); err == nil {
			t.Errorf("%q should be rejected", name)
		}
	}
}

func TestSanitizeUploadFilename_CaseInsensitiveExtension(t *testing.T) {
	got, err := sanitizeUploadFilename("song.WAV")
	if err != nil {
		t.Errorf("song.WAV should be valid, got %v", err)
	}
	if got != "song.WAV" {
		t.Errorf("song.WAV should be preserved, got %q", got)
	}
	if _, err := sanitizeUploadFilename("song.MP3"); err != nil {
		t.Errorf("song.MP3 should be valid, got %v", err)
	}
}

func TestHandleUpload_RejectPathTraversal(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	body, contentType := buildUploadBody(t, "../evil.wav", []byte("audio"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "input", "evil.wav")); !os.IsNotExist(err) {
		t.Error("path traversal upload should not be saved")
	}
}

func TestHandleUpload_RejectDoubleExtension(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	body, contentType := buildUploadBody(t, "song.mp3.wav", []byte("audio"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "input", "song.mp3.wav")); !os.IsNotExist(err) {
		t.Error("double-extension file should not be saved")
	}
}

func TestHandleUpload_SanitizesSpecialCharacters(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	cases := []struct {
		filename string
		savedAs  string
	}{
		{"song;cmd.wav", "song_cmd.wav"},
		{"song&echo.wav", "song&echo.wav"},
		{"song|pipe.wav", "song_pipe.wav"},
		{"song`whoami`.wav", "song_whoami_.wav"},
	}
	for _, c := range cases {
		body, contentType := buildUploadBody(t, c.filename, []byte("audio"))
		req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
		req.Header.Set("Content-Type", contentType)
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%q: expected 200, got %d: %s", c.filename, rr.Code, rr.Body.String())
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "input", c.savedAs)); err != nil {
			t.Errorf("%q: expected file %q to be saved: %v", c.filename, c.savedAs, err)
		}
	}
}

func TestHandleUpload_AcceptsValidAudio(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	body, contentType := buildUploadBody(t, "vocals.wav", []byte("audio"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "input", "vocals.wav")); err != nil {
		t.Errorf("valid upload should be saved: %v", err)
	}
}

func TestHandleUpload_AcceptsRealSongFilename(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	filename := "Así Fué (-3)(A) & mas.wav"
	body, contentType := buildUploadBody(t, filename, []byte("audio"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	wantSaved := "Así Fué (-3)(A) & mas.wav"
	if _, err := os.Stat(filepath.Join(root, "input", wantSaved)); err != nil {
		t.Errorf("real song upload should be saved as %q: %v", wantSaved, err)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	expectedPath := filepath.Join(mustSub("input"), wantSaved)
	if resp["path"] != expectedPath {
		t.Errorf("response path should be %q, got %q", expectedPath, resp["path"])
	}
}

func TestHandleUploadPitch_RejectPathTraversal(t *testing.T) {
	root := setupSecurityTestRoot(t)
	s := newSecurityTestServer(t)

	body, contentType := buildUploadBody(t, "../evil.wav", []byte("audio"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/pitch", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "input_rubberband", "evil.wav")); !os.IsNotExist(err) {
		t.Error("path traversal pitch upload should not be saved")
	}
}

// ── Test helpers ────────────────────────────────────────────────────────────

func setupSecurityTestRoot(t *testing.T) string {
	t.Helper()
	root := setTestRoot(t, "security-test-")

	for _, dir := range []string{"output", "input", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func newSecurityTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/upload", s.handleUpload)
	s.mux.HandleFunc("POST /api/upload/pitch", s.handleUploadPitch)
	return s
}

func buildUploadBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(content)); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
