package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// absTestRoot creates a temporary project root and guarantees it is absolute,
// because several deletion handlers compare absolute paths against prefixes.
func absTestRoot(t *testing.T, setup func(*testing.T) string) string {
	t.Helper()
	root := setup(t)
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to get absolute test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", abs)
	return abs
}

// newDeletionTestServer registers every deletion endpoint under test.
func newDeletionTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("DELETE /api/files/{song}", s.handleDeleteSong)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	s.mux.HandleFunc("DELETE /api/inputs/{name}", s.handleDeleteInput)
	s.mux.HandleFunc("DELETE /api/uploads/pitch/{name}", s.handleDeletePitchUpload)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}", s.handleDeletePitchSubgroup)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}/{file}", s.handleDeletePitchStem)
	s.mux.HandleFunc("DELETE /api/models/{name}", s.handleDeleteModel)
	s.mux.HandleFunc("POST /api/storage/clean", s.handleStorageClean)
	return s
}

// lastDeletionMessage returns the most recent backend deletion audit entry.
func lastDeletionMessage(t *testing.T) string {
	t.Helper()
	entries := defaultLogStore.readRecent(0)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Service == "backend" && strings.HasPrefix(entries[i].Message, "Deletion: ") {
			return entries[i].Message
		}
	}
	t.Fatalf("no deletion log entry found")
	return ""
}

// hasDeletionMessage reports whether any backend deletion audit entry exists.
func hasDeletionMessage() bool {
	entries := defaultLogStore.readRecent(0)
	for _, e := range entries {
		if e.Service == "backend" && strings.HasPrefix(e.Message, "Deletion: ") {
			return true
		}
	}
	return false
}

// assertDeletionLog checks the latest deletion log contains the expected fields.
func assertDeletionLog(t *testing.T, kind, name string, files, bytes int64) {
	t.Helper()
	msg := lastDeletionMessage(t)
	checks := []struct{ key, want string }{
		{"kind", "kind=" + kind},
		{"name", `name="` + name + `"`},
		{"files", "files=" + strconv.FormatInt(files, 10)},
		{"bytes", "bytes=" + strconv.FormatInt(bytes, 10)},
		{"ip", "ip=192.0.2.1"},
		{"ua", `ua="DeletionTestAgent/1.0"`},
	}
	for _, c := range checks {
		if !strings.Contains(msg, c.want) {
			t.Errorf("expected log to contain %s=%q, got: %s", c.key, c.want, msg)
		}
	}
}

func newDeletionRequest(t *testing.T, method, target string, body []byte) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, bodyReader)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("User-Agent", "DeletionTestAgent/1.0")
	return req
}

func TestHandleDeleteSong_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	song := "output_song"
	writeTestFile(t, filepath.Join(root, "output", song, "vocals.wav"), []byte("vocals"))
	writeTestFile(t, filepath.Join(root, "output", song, "drums.wav"), []byte("drums"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/files/"+song, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "output-song", "output/"+song, 2, 11)
}

func TestHandleDeleteSong_NotFoundDoesNotLogDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	absTestRoot(t, setupDAWTestRoot)

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/files/no-such-song", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
	if hasDeletionMessage() {
		t.Fatalf("not-found deletion must not leave a deletion log")
	}
}

func TestHandleDeleteFile_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	song := "song1"
	stem := "vocals.wav"
	writeTestFile(t, filepath.Join(root, "output", song, stem), []byte("vocals"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/delete?file="+song+"/"+stem, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "stem", "output/"+song+"/"+stem, 1, 6)
}

func TestHandleDeleteFile_PitchSubdirectoryDoesNotLogDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	writeTestFile(t, filepath.Join(root, "output", "song1", "song1_pitch-1", "vocals.wav"), []byte("vocals"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/delete?file=song1/song1_pitch-1/vocals.wav", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if hasDeletionMessage() {
		t.Fatalf("rejected deletion must not leave a deletion log")
	}
}

func TestHandleDeleteFile_TraversalDoesNotLogDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	writeTestFile(t, filepath.Join(root, "secret.txt"), []byte("secret"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/delete?file=../secret.txt", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "secret.txt")); err != nil {
		t.Fatalf("secret.txt should not have been deleted: %v", err)
	}
	if hasDeletionMessage() {
		t.Fatalf("forbidden deletion must not leave a deletion log")
	}
}

func TestHandleDeleteInput_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	name := "upload.wav"
	writeTestFile(t, filepath.Join(root, "input", name), []byte("upload"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/inputs/"+name, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "input-upload", "input/"+name, 1, 6)
}

func TestHandleDeleteInput_NotFoundDoesNotLogDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	absTestRoot(t, setupDAWTestRoot)

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/inputs/no-such-file.wav", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
	if hasDeletionMessage() {
		t.Fatalf("not-found deletion must not leave a deletion log")
	}
}

func TestHandleDeletePitchUpload_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	name := "pitch.wav"
	writeTestFile(t, filepath.Join(root, "input_rubberband", name), []byte("pitch!"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/uploads/pitch/"+name, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "pitch-upload", "input_rubberband/"+name, 1, 6)
}

func TestHandleDeletePitchSubgroup_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	song := "song1"
	pitch := "-1"
	subgroup := song + "_pitch" + pitch
	writeTestFile(t, filepath.Join(root, "output", song, subgroup, "vocals.wav"), []byte("vocals"))
	writeTestFile(t, filepath.Join(root, "output", song, subgroup, "drums.wav"), []byte("drums"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/pitch/"+song+"/"+pitch, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "pitch-subgroup", "output/"+song+"/"+subgroup, 2, 11)
}

func TestHandleDeletePitchStem_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupDAWTestRoot)
	song := "song1"
	pitch := "+2"
	file := "vocals.wav"
	subgroup := song + "_pitch" + pitch
	writeTestFile(t, filepath.Join(root, "output", song, subgroup, file), []byte("vocals"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/pitch/"+song+"/"+pitch+"/"+file, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "pitch-stem", "output/"+song+"/"+subgroup+"/"+file, 1, 6)
}

func TestHandleDeleteModel_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	absTestRoot(t, setupDAWTestRoot)

	name := "testmodel"
	subdir := "VR_Models"
	writeTestFile(t, filepath.Join(modelsBasePath(), subdir, name+".pth"), []byte("modeldata"))

	srv := newDeletionTestServer(t)
	req := newDeletionRequest(t, http.MethodDelete, "/api/models/"+name, nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "model", "models/"+subdir+"/"+name+".pth", 1, 9)
}

func TestHandleStorageClean_Tmp_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupStorageTestRoot)
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "tmp", "tmp.txt"), []byte("temp"))

	srv := newDeletionTestServer(t)
	body := []byte(`{"action":"tmp"}`)
	req := newDeletionRequest(t, http.MethodPost, "/api/storage/clean", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "storage-clean", "tmp", 1, 4)
}

func TestHandleStorageClean_AllEdits_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupStorageTestRoot)
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"), []byte("edit"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "reverb.wav"), []byte("reverb"))

	srv := newDeletionTestServer(t)
	body := []byte(`{"action":"all-edits"}`)
	req := newDeletionRequest(t, http.MethodPost, "/api/storage/clean", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "storage-clean", "all-edits", 2, 10)
}

func TestHandleStorageClean_OrphanEdits_LogsDeletion(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := absTestRoot(t, setupStorageTestRoot)
	writeTestFile(t, filepath.Join(root, "daw-data", "orphan", "edits", "reverb.wav"), []byte("reverb"))

	srv := newDeletionTestServer(t)
	body := []byte(`{"action":"orphan-edits"}`)
	req := newDeletionRequest(t, http.MethodPost, "/api/storage/clean", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertDeletionLog(t, "storage-clean", "orphan-edits", 1, 6)
}
