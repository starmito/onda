package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAutoCleanTmpFiles_OnlyCleansTmpDecoys creates decoys in every nearby
// directory and verifies that only daw-data/<song>/tmp/ files are removed.
func TestAutoCleanTmpFiles_OnlyCleansTmpDecoys(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-decoys-")
	song := "cancion"
	old := time.Now().Add(-10 * time.Minute)

	// Files that must be deleted.
	tmpFiles := []string{
		filepath.Join(root, "daw-data", song, "tmp", "scratch.wav"),
		filepath.Join(root, "daw-data", song, "tmp", "tmp.txt"),
	}
	for _, p := range tmpFiles {
		writeTestFile(t, p, []byte("tmp"))
		os.Chtimes(p, old, old)
	}

	// Decoys that must survive.
	decoys := []string{
		filepath.Join(root, "daw-data", song, "imports", "import.wav"),
		filepath.Join(root, "daw-data", song, "edits", "edit.wav"),
		filepath.Join(root, "daw-data", song, "original", "original.wav"),
		filepath.Join(root, "output", song, "vocals.wav"),
		filepath.Join(root, "input", "upload.wav"),
	}
	for _, p := range decoys {
		writeTestFile(t, p, []byte("decoy"))
	}

	s := &Server{jobs: make(map[string]*JobState)}
	s.autoCleanTmpFiles("test")

	for _, p := range tmpFiles {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("tmp file %s should have been deleted", p)
		}
	}
	for _, p := range decoys {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("decoy %s should not have been deleted: %v", p, err)
		}
	}

	msg := lastDeletionMessage(t)
	if !strings.Contains(msg, "kind=auto-tmp") {
		t.Errorf("expected auto-tmp deletion log, got %s", msg)
	}
}

// TestAutoCleanTmpFiles_SkipsActiveJobSong verifies that temporaries belonging
// to a song with an active job are preserved, while idle songs are cleaned.
func TestAutoCleanTmpFiles_SkipsActiveJobSong(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-active-")
	old := time.Now().Add(-10 * time.Minute)

	activeTmp := filepath.Join(root, "daw-data", "active-song", "tmp", "old.tmp")
	idleTmp := filepath.Join(root, "daw-data", "idle-song", "tmp", "old.tmp")
	writeTestFile(t, activeTmp, []byte("old"))
	writeTestFile(t, idleTmp, []byte("old"))
	os.Chtimes(activeTmp, old, old)
	os.Chtimes(idleTmp, old, old)

	s := &Server{jobs: map[string]*JobState{
		"active-song": {Song: "active-song", Status: "processing"},
	}}
	s.autoCleanTmpFiles("test")

	if _, err := os.Stat(activeTmp); err != nil {
		t.Errorf("active-song tmp file should have been preserved: %v", err)
	}
	if _, err := os.Stat(idleTmp); !os.IsNotExist(err) {
		t.Errorf("idle-song tmp file should have been deleted")
	}
}

// TestAutoCleanTmpFiles_SkipsYoungFiles verifies the minimum-age gate:
// recently-created temporaries are kept, stale ones are removed.
func TestAutoCleanTmpFiles_SkipsYoungFiles(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-age-")
	old := time.Now().Add(-10 * time.Minute)

	youngTmp := filepath.Join(root, "daw-data", "song", "tmp", "young.tmp")
	oldTmp := filepath.Join(root, "daw-data", "song", "tmp", "old.tmp")
	writeTestFile(t, youngTmp, []byte("young"))
	writeTestFile(t, oldTmp, []byte("old"))
	os.Chtimes(oldTmp, old, old)

	s := &Server{jobs: make(map[string]*JobState)}
	s.autoCleanTmpFiles("test")

	if _, err := os.Stat(youngTmp); err != nil {
		t.Errorf("young tmp file should have been preserved: %v", err)
	}
	if _, err := os.Stat(oldTmp); !os.IsNotExist(err) {
		t.Errorf("old tmp file should have been deleted")
	}
}

// TestAutoCleanTmpFiles_LogsSummary verifies that a successful cleanup emits
// both per-file Deletion: entries and a summary line.
func TestAutoCleanTmpFiles_LogsSummary(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-summary-")
	old := time.Now().Add(-10 * time.Minute)

	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "tmp", "a.tmp"), []byte("a"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song2", "tmp", "b.tmp"), []byte("bb"))
	os.Chtimes(filepath.Join(root, "daw-data", "song1", "tmp", "a.tmp"), old, old)
	os.Chtimes(filepath.Join(root, "daw-data", "song2", "tmp", "b.tmp"), old, old)

	s := &Server{jobs: make(map[string]*JobState)}
	s.autoCleanTmpFiles("test")

	// Per-file Deletion: lines.
	if !hasDeletionMessage() {
		t.Fatal("expected at least one Deletion: log entry")
	}

	// Summary line.
	var summary string
	logBufferMu.RLock()
	for _, e := range logBuffer {
		if e.Service == "backend" && strings.HasPrefix(e.Message, "Temp cleanup summary:") {
			summary = e.Message
			break
		}
	}
	logBufferMu.RUnlock()
	if summary == "" {
		t.Fatal("expected a Temp cleanup summary log entry")
	}
	for _, want := range []string{"trigger=test", "songs=2", "files=2", "bytes=3"} {
		if !strings.Contains(summary, want) {
			t.Errorf("expected summary to contain %q, got %s", want, summary)
		}
	}
}

// TestAutoCleanTmpFiles_StartupHook verifies that NewServer triggers a startup
// cleanup of stale temporaries.
func TestAutoCleanTmpFiles_StartupHook(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-startup-")
	old := time.Now().Add(-10 * time.Minute)

	writeTestFile(t, filepath.Join(root, "daw-data", "song", "tmp", "stale.tmp"), []byte("stale"))
	os.Chtimes(filepath.Join(root, "daw-data", "song", "tmp", "stale.tmp"), old, old)

	server := NewServer(":0")
	defer server.Close()

	if _, err := os.Stat(filepath.Join(root, "daw-data", "song", "tmp", "stale.tmp")); !os.IsNotExist(err) {
		t.Errorf("startup cleanup should have removed stale tmp file")
	}

	msg := lastDeletionMessage(t)
	if !strings.Contains(msg, "trigger=startup") {
		t.Errorf("expected startup trigger in deletion log, got %s", msg)
	}
}

// TestAutoCleanTmpFiles_JobFinishHook verifies that the worker cleans stale
// temporaries of other songs after a job finishes.
func TestAutoCleanTmpFiles_JobFinishHook(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "autoclean-jobfinish-")

	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("vocals.wav")
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	// Stale temporary file for a different song; must be cleaned after the job.
	otherTmp := filepath.Join(root, "daw-data", "other-song", "tmp", "stale.tmp")
	writeTestFile(t, otherTmp, []byte("stale"))
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(otherTmp, old, old)

	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs: map[string]*JobState{
			"song": {Song: "song", Status: "waiting"},
		},
	}

	go s.worker()

	s.jobQueue <- JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--vocal-model", "/app/data/models/VR_Models/BS_Roformer_Viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		s.jobsMu.RLock()
		status := s.jobs["song"].Status
		s.jobsMu.RUnlock()
		if status == "done" || status == "error" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for worker to process job")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(otherTmp); !os.IsNotExist(err) {
		t.Errorf("job-finish cleanup should have removed stale tmp file of other song")
	}

	msg := lastDeletionMessage(t)
	if !strings.Contains(msg, "trigger=job-finish") {
		t.Errorf("expected job-finish trigger in deletion log, got %s", msg)
	}
}
