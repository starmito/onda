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
	"time"
)

func setupDAWTestRoot(t *testing.T) string {
	t.Helper()
	root := setTestRoot(t, "daw-test-")

	for _, dir := range []string{"output", "input", "input_rubberband", "daw-data"} {
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
	s.mux.HandleFunc("GET /api/inputs", s.handleInputs)
	s.mux.HandleFunc("POST /api/daw/import", s.handleImportStem)
	s.mux.HandleFunc("POST /api/daw/upload", s.handleUploadAudio)
	s.mux.HandleFunc("POST /api/audio/trim", s.handleTrim)
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
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "cancion1_pitch-1", "bass_pitch-1.wav"), []byte("bass"))
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "cancion1_pitch-1", "drums.wav"), []byte("drums"))
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

	if got, want := len(resp.Output), 2; got != want {
		t.Fatalf("expected %d output songs, got %d", want, got)
	}
	if got, want := resp.Output["cancion1"], []string{"instrumental.wav", "vocals.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected stems %v, got %v", want, got)
	}
	if got, want := resp.Output["cancion1 (pitch -1)"], []string{"bass_pitch-1.wav", "drums.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected pitch subgroup stems %v, got %v", want, got)
	}

	// The pitch subgroup must still be reported through resp.Pitch for other views.
	wantPitch := []PitchStemEntry{
		{Song: "cancion1", Pitch: "-1", Stem: "bass_pitch-1.wav"},
		{Song: "cancion1", Pitch: "-1", Stem: "drums.wav"},
	}
	if !pitchEqual(resp.Pitch, wantPitch) {
		t.Fatalf("expected pitch entries %v, got %v", wantPitch, resp.Pitch)
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
	wantPath := filepath.Join("daw-data", "cancion1", "imports", "import_cancion1_vocals.wav")
	if resp.Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, resp.Path)
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
	writeTestFile(t, filepath.Join(root, "output", "cancion1", "cancion1_pitch0", "mi_pitch.wav"), srcContent)

	srv := newDAWTestServer(t)
	body := `{"source":"pitch","song":"cancion1","pitch":"0","stem":"mi_pitch.wav"}`
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
	wantPath := filepath.Join("daw-data", "cancion1", "imports", "import_mi_pitch.wav")
	if resp.Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, resp.Path)
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
		`{"source":"input"}`,
		`{"source":"daw-data"}`,
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

func TestHandleImportStem_Input(t *testing.T) {
	root := setupDAWTestRoot(t)
	srcContent := []byte("input-song-content")
	writeTestFile(t, filepath.Join(root, "input", "mi_cancion.wav"), srcContent)

	srv := newDAWTestServer(t)
	body := `{"source":"input","file":"mi_cancion.wav"}`
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
	if resp.File != "import_mi_cancion.wav" {
		t.Fatalf("expected file import_mi_cancion.wav, got %s", resp.File)
	}
	wantPath := filepath.Join("daw-data", "mi_cancion", "imports", "import_mi_cancion.wav")
	if resp.Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, resp.Path)
	}
	if resp.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), resp.Size)
	}

	// Re-importing should return the already imported file.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
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

func TestHandleImportStem_InputNotFound_StructuredError(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	body := `{"source":"input","file":"no_existe.wav"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp dawFileNotFoundResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != "file_not_found" {
		t.Fatalf("expected code file_not_found, got %q", resp.Code)
	}
	if resp.File != "no_existe.wav" {
		t.Fatalf("expected file no_existe.wav, got %q", resp.File)
	}
}

func TestHandleImportStem_DawData(t *testing.T) {
	root := setupDAWTestRoot(t)
	srcContent := []byte("daw-data-song-content")
	writeTestFile(t, filepath.Join(root, "daw-data", "mi_cancion", "original", "upload_mi_cancion.wav"), srcContent)

	srv := newDAWTestServer(t)
	body := `{"source":"daw-data","file":"upload_mi_cancion.wav"}`
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
	if resp.File != "upload_mi_cancion.wav" {
		t.Fatalf("expected file upload_mi_cancion.wav, got %s", resp.File)
	}
	wantPath := filepath.Join("daw-data", "mi_cancion", "imports", "upload_mi_cancion.wav")
	if resp.Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, resp.Path)
	}
	if resp.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), resp.Size)
	}
}

func TestHandleImportStem_DawData_NotFound(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	body := `{"source":"daw-data","file":"no_existe.wav"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp dawFileNotFoundResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != "file_not_found" {
		t.Fatalf("expected code file_not_found, got %q", resp.Code)
	}
	if resp.File != "no_existe.wav" {
		t.Fatalf("expected file no_existe.wav, got %q", resp.File)
	}
}

func TestHandleImportStem_Input_Traversal(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "input", "mi_cancion.wav"), []byte("ok"))
	writeTestFile(t, filepath.Join(root, "secret.txt"), []byte("secret"))
	srv := newDAWTestServer(t)

	body := `{"source":"input","file":"../secret.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daw/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid file name") {
		t.Fatalf("expected invalid file name error, got %s", rr.Body.String())
	}
}

func TestHandleInputs_ProcessedAndSorted(t *testing.T) {
	root := setupDAWTestRoot(t)

	// Create input/ files.
	writeTestFile(t, filepath.Join(root, "input", "a_input.wav"), []byte("a"))
	writeTestFile(t, filepath.Join(root, "input", "b_input.wav"), []byte("b"))

	// Create daw-data tree for one song.
	song := "testsong"
	writeTestFile(t, filepath.Join(root, "daw-data", song, "original", "upload_original.wav"), []byte("upload"))
	writeTestFile(t, filepath.Join(root, "daw-data", song, "imports", "import_original.wav"), []byte("import"))
	writeTestFile(t, filepath.Join(root, "daw-data", song, "edits", "reverb_original.wav"), []byte("reverb"))
	writeTestFile(t, filepath.Join(root, "daw-data", song, "edits", "eq_original.wav"), []byte("eq"))

	// Touch files to enforce a predictable modification order.
	now := time.Now()
	_ = os.Chtimes(filepath.Join(root, "input", "a_input.wav"), now, now.Add(-2*time.Hour))
	_ = os.Chtimes(filepath.Join(root, "input", "b_input.wav"), now, now.Add(-1*time.Hour))
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "original", "upload_original.wav"), now, now.Add(-30*time.Minute))
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "imports", "import_original.wav"), now, now)
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "edits", "reverb_original.wav"), now, now.Add(-15*time.Minute))
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "edits", "eq_original.wav"), now, now.Add(-45*time.Minute))

	srv := newDAWTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/inputs?include=all", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []InputEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 6 {
		t.Fatalf("expected 6 inputs, got %d", len(resp))
	}

	// Originals first, then most recently modified first.
	wantOrder := []string{"import_original.wav", "upload_original.wav", "b_input.wav", "a_input.wav", "reverb_original.wav", "eq_original.wav"}
	for i, want := range wantOrder {
		if resp[i].Name != want {
			t.Fatalf("position %d: expected %q, got %q", i, want, resp[i].Name)
		}
	}

	// Verify processed flag.
	processed := make(map[string]bool)
	for _, e := range resp {
		processed[e.Name] = e.Processed
	}
	if processed["reverb_original.wav"] != true {
		t.Fatalf("expected reverb_original.wav to be processed")
	}
	if processed["eq_original.wav"] != true {
		t.Fatalf("expected eq_original.wav to be processed")
	}
	if processed["upload_original.wav"] != false {
		t.Fatalf("expected upload_original.wav to be original")
	}
	if processed["import_original.wav"] != false {
		t.Fatalf("expected import_original.wav to be original")
	}
	if processed["a_input.wav"] != false {
		t.Fatalf("expected input files to be original")
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
	if resp.File != "original.wav" {
		t.Fatalf("expected file original.wav, got %s", resp.File)
	}
	wantPath := filepath.Join("daw-data", "mi_cancion", "original.wav")
	if resp.Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, resp.Path)
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

func pitchEqual(a, b []PitchStemEntry) bool {
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

func TestHandleTrim_MissingFile_StructuredError(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTestServer(t)

	body := `{"file":"archivo_perdido.wav","start":0,"end":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/audio/trim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp dawFileNotFoundResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != "file_not_found" {
		t.Fatalf("expected code file_not_found, got %q", resp.Code)
	}
	if resp.File != "archivo_perdido.wav" {
		t.Fatalf("expected file archivo_perdido.wav, got %q", resp.File)
	}
	if !strings.Contains(resp.Help, "Vuelve a subirlo") {
		t.Fatalf("expected recovery hint, got %q", resp.Help)
	}
}
