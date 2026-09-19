package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupPipelineStatusTestRoot creates a temporary project root with the
// required directory layout and sets ONDA_ROOT.
func setupPipelineStatusTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "status-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"input", "output"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func TestWorker_RemovesPipelineStatusAtJobStart(t *testing.T) {
	root := setupPipelineStatusTestRoot(t)
	mockResourceProviders(t)

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	stale := `{"status":"running","step":"vocal","progress":0.5,"song":"old_song","shifts":20,"jobs":8,"chunk_size":35,"batch_size":2}`
	if err := os.WriteFile(statusPath, []byte(stale), 0o644); err != nil {
		t.Fatalf("failed to write stale status: %v", err)
	}

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := "#!/bin/bash\necho ok\n"
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

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
		Args: []string{fakePipeline, "--viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		s.jobsMu.RLock()
		status := s.jobs["song"].Status
		s.jobsMu.RUnlock()
		if status == "done" || status == "error" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for worker to process job")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// The worker must have removed the stale status file before starting the
	// new job, so no field from the previous job remains.
	if _, err := os.Stat(statusPath); err == nil {
		t.Fatalf("stale pipeline_status.json should have been removed at job start")
	}
}

func TestPipelineStatusNoStaleFields_AfterRemoval(t *testing.T) {
	root := setupPipelineStatusTestRoot(t)

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	stale := `{"status":"running","step":"vocal","progress":0.5,"song":"old_song","shifts":20,"jobs":8,"chunk_size":35,"batch_size":2}`
	if err := os.WriteFile(statusPath, []byte(stale), 0o644); err != nil {
		t.Fatalf("failed to write stale status: %v", err)
	}

	// Simulate what worker() does at the start of a new job.
	if projectRoot := resolveProjectRoot(); projectRoot != "" {
		sp := filepath.Join(projectRoot, "output", "pipeline_status.json")
		os.Remove(sp)
	}

	if _, err := os.Stat(statusPath); err == nil {
		t.Fatalf("pipeline_status.json should have been removed")
	}
}
