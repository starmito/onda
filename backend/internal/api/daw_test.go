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
	"strings"
	"testing"
)

func setupDAWTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "daw-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"output", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func newDAWTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)
	s.mux.HandleFunc("POST /api/daw/import", s.handleImportStem)
	s.mux.HandleFunc("POST /api/daw/upload", s.handleUploadAudio)
	return s
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}

func TestHandleListStems(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "vocals.wav"), []byte("vocals"))
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "instrumental.wav"), []byte("instrumental"))
	writeTestFile(t, filepath.Join(root, "input_rubberband", "cancion1_pitch.wav"), []byte("pitch"))
	writeTestFile(t, filepath.Join(root, "input_rubberband", "readme.txt"), []byte("ignore"))

	srv := newDAWTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/daw/stems", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp StemsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if got, want := len(resp.Output), 1; got != want {
		t.Fatalf("expected %d output songs, got %d", want, got)
	}
	if got, want := resp.Output["cancion1"], []string{"instrumental.wav", "vocals.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected stems %v, got %v", want, got)
	}
	if got, want := resp.Pitch, []string{"cancion1_pitch.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected pitch %v, got %v", want, got)
	}
}

func TestHandleImportStem_Output(t *testing.T) {
	root := setupDAWTestRoot(t)
	srcContent := []byte("vocals-stem-content")
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "vocals.wav"), srcContent)

	srv := newDAWTestServer(t)
	body := `{"source":"output","song":"cancion1","stem":"vocals.wav"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ImportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.File != "import_cancion1_vocals.wav" {
		t.Fatalf("expected file import_cancion1_vocals.wav, got %s", resp.File)
	}
	if resp.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), resp.Size)
	}

	// Second import should return the existing file, not duplicate it.
	req2 := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 on re-import, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var resp2 ImportResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}
	if resp2.Size != resp.Size {
		t.Fatalf("re-import size changed: %d vs %d", resp2.Size, resp.Size)
	}
}

func TestHandleImportStem_Pitch(t *testing.T) {
	root := setupDAWTestRoot(t)
	srcContent := []byte("pitch-content")
	writeTestFile(t, filepath.Join(root, "input_rubberband", "mi_pitch.wav"), srcContent)

	srv := newDAWTestServer(t)
	body := `{"source":"pitch","stem":"mi_pitch.wav"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ImportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.File != "import_mi_pitch.wav" {
		t.Fatalf("expected file import_mi_pitch.wav, got %s", resp.File)
	}
	if resp.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), resp.Size)
	}
}

func TestHandleImportStem_Validation(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	cases := []string{
		`{"source":"output","song":"cancion1"}`,
		`{"source":"output","stem":"vocals.wav"}`,
		`{"source":"pitch"}`,
		`{"source":"bad"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", body, rr.Code, rr.Body.String())
		}
	}
}

func TestHandleUploadAudio(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "mi_cancion.wav")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	content := []byte("uploaded-audio-content")
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/daw/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.File != "upload_mi_cancion.wav" {
		t.Fatalf("expected file upload_mi_cancion.wav, got %s", resp.File)
	}
	if resp.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), resp.Size)
	}
}

func TestHandleUploadAudio_InvalidExtension(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.WriteString(fw, "not audio"); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/daw/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
