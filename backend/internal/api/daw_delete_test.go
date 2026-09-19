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

func newDAWDeleteTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("DELETE /api/daw/songs/{song}", s.handleDeleteDAWSong)
	return s
}

func TestHandleDeleteDAWSong_DeletesWholeTree(t *testing.T) {
	root := setupDAWTestRoot(t)
	song := "mi_cancion"

	contents := map[string][]byte{
		filepath.Join(root, "daw-data", song, "original.wav"):             []byte("original"),
		filepath.Join(root, "daw-data", song, "imports", "import1.wav"):   []byte("import-one"),
		filepath.Join(root, "daw-data", song, "edits", "eq_original.wav"): []byte("edit-one"),
		filepath.Join(root, "daw-data", song, "tmp", "tmp.txt"):           []byte("temp"),
	}
	var wantFiles int
	var wantBytes int64
	for p, c := range contents {
		writeTestFile(t, p, c)
		wantFiles++
		wantBytes += int64(len(c))
	}

	srv := newDAWDeleteTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/"+song, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp dawSongDeleteResult
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Deleted {
		t.Fatalf("expected deleted=true, got %v", resp.Deleted)
	}
	if resp.Song != song {
		t.Fatalf("expected song %q, got %q", song, resp.Song)
	}
	if resp.Files != wantFiles {
		t.Fatalf("expected %d files, got %d", wantFiles, resp.Files)
	}
	if resp.Bytes != wantBytes {
		t.Fatalf("expected %d bytes, got %d", wantBytes, resp.Bytes)
	}

	if _, err := os.Stat(filepath.Join(root, "daw-data", song)); !os.IsNotExist(err) {
		t.Fatalf("expected song directory to be removed, got err=%v", err)
	}
}

func TestHandleDeleteDAWSong_NotFound(t *testing.T) {
	setupDAWTestRoot(t)
	srv := newDAWDeleteTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/no_existe", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.Contains(body["error"], "not found") {
		t.Fatalf("expected 'not found' message, got %q", body["error"])
	}
}

func TestHandleDeleteDAWSong_TraversalRejected(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "secret.txt"), []byte("secret"))
	srv := newDAWDeleteTestServer(t)

	cases := []string{
		"..%2Fsecret.txt",
		"foo%2Fbar",
		"%2Fetc%2Fpasswd",
	}
	for _, name := range cases {
		req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/"+name, nil)
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d: %s", name, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "invalid song name") {
			t.Fatalf("expected invalid song name error for %q, got %s", name, rr.Body.String())
		}
	}

	if _, err := os.Stat(filepath.Join(root, "secret.txt")); err != nil {
		t.Fatalf("secret.txt should not have been deleted: %v", err)
	}
}

func TestSafeDAWSongDir_RejectsTraversal(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "secret.txt"), []byte("secret"))

	cases := []string{
		"../secret.txt",
		"..\\secret.txt",
		"foo/../bar",
		"foo/bar",
		"/etc/passwd",
		"..",
		".",
		"",
	}
	for _, song := range cases {
		if _, err := safeDAWSongDir(root, song); err == nil {
			t.Fatalf("expected error for song %q, got nil", song)
		}
	}
}

func TestHandleDeleteDAWSong_DoesNotAffectOtherSongs(t *testing.T) {
	root := setupDAWTestRoot(t)
	writeTestFile(t, filepath.Join(root, "daw-data", "keep", "original.wav"), []byte("keep-original"))
	writeTestFile(t, filepath.Join(root, "daw-data", "delete", "original.wav"), []byte("delete-original"))

	srv := newDAWDeleteTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/delete", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if _, err := os.Stat(filepath.Join(root, "daw-data", "delete")); !os.IsNotExist(err) {
		t.Fatalf("delete song directory should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "daw-data", "keep", "original.wav")); err != nil {
		t.Fatalf("keep song should remain untouched: %v", err)
	}
}

func TestHandleDeleteDAWSong_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	root := setupDAWTestRoot(t)
	song := "logged_song"
	writeTestFile(t, filepath.Join(root, "daw-data", song, "original.wav"), []byte("original"))

	srv := newDAWDeleteTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/"+song, nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("User-Agent", "DAWTestAgent/1.0")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	msg := lastDeletionMessage(t)
	wantParts := []string{
		`kind=daw-song`,
		`name="daw-data/` + song + `"`,
		`files=1`,
		`bytes=8`,
		`ip=192.0.2.1`,
		`ua="DAWTestAgent/1.0"`,
	}
	for _, part := range wantParts {
		if !strings.Contains(msg, part) {
			t.Fatalf("expected log to contain %q, got: %s", part, msg)
		}
	}
}

func TestHandleDeleteDAWSong_InvalidNameDoesNotLogDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	setupDAWTestRoot(t)
	srv := newDAWDeleteTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/daw/songs/%2Fetc%2Fpasswd", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	if hasDeletionMessage() {
		t.Fatalf("invalid deletion request must not leave a deletion log")
	}
}
