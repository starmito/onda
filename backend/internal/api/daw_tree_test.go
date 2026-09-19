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

// newDAWTreeTestServer registers the handlers needed for tree integration tests.
func newDAWTreeTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/inputs", s.handleInputs)
	s.mux.HandleFunc("POST /api/daw/import", s.handleImportStem)
	s.mux.HandleFunc("POST /api/daw/upload", s.handleUploadAudio)
	s.mux.HandleFunc("POST /api/daw/eq", s.handleEQ)
	s.mux.HandleFunc("POST /api/daw/reverb", s.handleReverb)
	s.mux.HandleFunc("GET /daw-data/", http.StripPrefix("/daw-data/", http.FileServer(http.Dir(filepath.Join(resolveProjectRoot(), "daw-data")))).ServeHTTP)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDAWTree_UploadCreatesOriginal(t *testing.T) {
	root := setupDAWTestRoot(t)
	srv := newDAWTreeTestServer(t)

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

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/daw/upload", &buf)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	originalPath := filepath.Join(root, "daw-data", "mi_cancion", "original.wav")
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("expected uploaded original at %s: %v", originalPath, err)
	}
}

func TestDAWTree_ImportInputCreatesImports(t *testing.T) {
	root := setupDAWTestRoot(t)
	srcContent := []byte("input-stem-content")
	writeTestFile(t, filepath.Join(root, "input", "mi_stem.wav"), srcContent)
	srv := newDAWTreeTestServer(t)

	body := `{"source":"input","file":"mi_stem.wav","song":"mi_cancion"}`
	resp, err := http.Post(srv.URL+"/api/daw/import", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	importPath := filepath.Join(root, "daw-data", "mi_cancion", "imports", "import_mi_stem.wav")
	if _, err := os.Stat(importPath); err != nil {
		t.Fatalf("expected imported stem at %s: %v", importPath, err)
	}
}

func TestDAWTree_EQEffectCreatesEdits(t *testing.T) {
	skipIfMissingBinary(t, "ffprobe")

	root := setupDAWTestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "daw-data", "mi_cancion", "original", "original.wav"), 2.0)
	srv := newDAWTreeTestServer(t)

	body := `{"file":"original.wav","filters":[{"type":"peak","freq":1000,"gain":0,"q":1}]}`
	resp, err := http.Post(srv.URL+"/api/daw/eq", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	editPath := filepath.Join(root, "daw-data", "mi_cancion", "edits", "eq_original.wav")
	if _, err := os.Stat(editPath); err != nil {
		t.Fatalf("expected eq edit at %s: %v", editPath, err)
	}
}

func TestDAWTree_ChainedEffectStaysInEdits(t *testing.T) {
	skipIfMissingBinary(t, "sox")
	skipIfMissingBinary(t, "ffprobe")

	root := setupDAWTestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "daw-data", "mi_cancion", "original", "original.wav"), 2.0)
	srv := newDAWTreeTestServer(t)

	// First effect: EQ.
	body1 := `{"file":"original.wav","filters":[{"type":"peak","freq":1000,"gain":0,"q":1}]}`
	resp1, err := http.Post(srv.URL+"/api/daw/eq", "application/json", strings.NewReader(body1))
	if err != nil {
		t.Fatalf("eq request failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected eq 200, got %d", resp1.StatusCode)
	}

	// Second effect: reverb on the eq output.
	body2 := `{"file":"eq_original.wav","room_size":50,"decay":50,"wet_dry":50}`
	resp2, err := http.Post(srv.URL+"/api/daw/reverb", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatalf("reverb request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected reverb 200, got %d: %s", resp2.StatusCode, string(b))
	}

	chainedPath := filepath.Join(root, "daw-data", "mi_cancion", "edits", "reverb_eq_original.wav")
	if _, err := os.Stat(chainedPath); err != nil {
		t.Fatalf("expected chained edit at %s: %v", chainedPath, err)
	}
}

func TestDAWTree_TraversalRejected(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "input", "ok.wav"), []byte("ok"))
	writeTestFile(t, filepath.Join(root, "secret.txt"), []byte("secret"))
	srv := newDAWTreeTestServer(t)

	body := `{"source":"input","file":"../secret.txt"}`
	resp, err := http.Post(srv.URL+"/api/daw/import", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal, got %d", resp.StatusCode)
	}
}

func TestDAWTree_ListOrdersOriginalsFirst(t *testing.T) {
	root := setupDAWTestRoot(t)
	song := "mi_cancion"
	writeTestFile(t, filepath.Join(root, "daw-data", song, "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", song, "imports", "import_stem.wav"), []byte("import"))
	writeTestFile(t, filepath.Join(root, "daw-data", song, "edits", "eq_original.wav"), []byte("eq"))

	now := time.Now()
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "edits", "eq_original.wav"), now, now)
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "original", "original.wav"), now, now.Add(-10*time.Minute))
	_ = os.Chtimes(filepath.Join(root, "daw-data", song, "imports", "import_stem.wav"), now, now.Add(-5*time.Minute))

	srv := newDAWTreeTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/inputs?include=daw-data")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var entries []InputEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Originals first, then edits. Within each group, newest first.
	want := []struct {
		name      string
		processed bool
	}{
		{"import_stem.wav", false},
		{"original.wav", false},
		{"eq_original.wav", true},
	}
	for i, w := range want {
		if entries[i].Name != w.name {
			t.Errorf("position %d: expected name %q, got %q", i, w.name, entries[i].Name)
		}
		if entries[i].Processed != w.processed {
			t.Errorf("position %d: expected processed=%v, got %v", i, w.processed, entries[i].Processed)
		}
		if entries[i].Song != song {
			t.Errorf("position %d: expected song %q, got %q", i, song, entries[i].Song)
		}
	}
}

func TestDAWTree_ServeAudioTreePath(t *testing.T) {
	root := setupDAWTestRoot(t)
	content := []byte("audio-content")
	writeTestFile(t, filepath.Join(root, "daw-data", "mi_cancion", "original", "original.wav"), content)
	srv := newDAWTreeTestServer(t)

	resp, err := srv.Client().Get(srv.URL + "/daw-data/mi_cancion/original/original.wav")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, content) {
		t.Fatalf("served content does not match")
	}
}

func TestDAWTree_ServeAudioTraversalRejected(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWTreeTestServer(t)

	resp, err := srv.Client().Get(srv.URL + "/daw-data/../secret.txt")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for traversal, got %d", resp.StatusCode)
	}
}
