package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupQueueTestRoot creates a temporary project root with input/output dirs.
func setupQueueTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "queue-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"input", "output", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func newQueueTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 10),
		jobs:     make(map[string]*JobState),
	}
	s.mux.HandleFunc("POST /api/separate", s.handleSeparate)
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.mux.HandleFunc("DELETE /api/queue", s.handleQueueClear)
	s.mux.HandleFunc("POST /api/queue/cancel", s.handleQueueCancel)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	return s
}

func TestHandleSeparate_EnqueuesJob(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	body := `{"input":"/app/input/song.wav","viperx":true,"demucs":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "queued" {
		t.Errorf("expected status queued, got %q", resp["status"])
	}
	if resp["song"] != "song" {
		t.Errorf("expected song song, got %q", resp["song"])
	}

	s.jobsMu.RLock()
	job, ok := s.jobs["song"]
	s.jobsMu.RUnlock()
	if !ok {
		t.Fatal("expected job to be registered")
	}
	if job.Status != "waiting" {
		t.Errorf("expected waiting status, got %q", job.Status)
	}
}

func TestHandleSeparate_RejectsDuplicateActiveJob(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	body := `{"input":"/app/input/song.wav","viperx":true}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestHandleSeparate_AllowsRetryAfterTerminal(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	body := `{"input":"/app/input/song.wav","viperx":true}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(httptest.NewRecorder(), req1)

	s.jobsMu.Lock()
	s.jobs["song"].Status = "done"
	s.jobsMu.Unlock()

	req2 := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusAccepted {
		t.Fatalf("expected 202 retry, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestHandleSeparate_UnknownPreset(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	body := `{"input":"/app/input/song.wav","preset":"no-existe"}`
	req := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleQueueStatus_Ordering(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	s.jobsMu.Lock()
	s.jobs["waiting-song"] = &JobState{Song: "waiting-song", Status: "waiting", Index: 2}
	s.jobs["processing-song"] = &JobState{Song: "processing-song", Status: "processing", Index: 1}
	s.jobs["done-song"] = &JobState{Song: "done-song", Status: "done", Index: 0}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(resp.Jobs))
	}
	// processing > waiting > done
	if resp.Jobs[0].Status != "processing" {
		t.Errorf("expected first job processing, got %q", resp.Jobs[0].Status)
	}
	if resp.Jobs[1].Status != "waiting" {
		t.Errorf("expected second job waiting, got %q", resp.Jobs[1].Status)
	}
	if resp.Jobs[2].Status != "done" {
		t.Errorf("expected third job done, got %q", resp.Jobs[2].Status)
	}
}

func TestHandleQueueStatus_PipelineProgress(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	status := `{"status":"running","step":"demucs","progress":0.42,"device":"cuda"}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["processing-song"] = &JobState{Song: "processing-song", Status: "processing", Index: 0, TotalSteps: 2}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	job := resp.Jobs[0]
	if job.Progress != 42 {
		t.Errorf("expected progress 42, got %d", job.Progress)
	}
	if job.StepName != "Demucs" {
		t.Errorf("expected step name Demucs, got %q", job.StepName)
	}
	if job.Device != "cuda" {
		t.Errorf("expected device cuda, got %q", job.Device)
	}
}

func TestHandleQueueStatus_OverallProgressFallback(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	// multi-step mode reports overall_progress as a 0-100 integer.
	status := `{"status":"running","step":"vocal","overall_progress":25,"device":"cpu"}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["processing-song"] = &JobState{Song: "processing-song", Status: "processing", Index: 0, TotalSteps: 2}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	if resp.Jobs[0].Progress != 25 {
		t.Errorf("expected progress 25 from overall_progress, got %d", resp.Jobs[0].Progress)
	}
}

func TestHandleQueueStatus_OverallProgressClamped(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	// An out-of-range overall_progress must be clamped to 0-100.
	status := `{"status":"running","step":"vocal","overall_progress":150,"device":"cpu"}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["processing-song"] = &JobState{Song: "processing-song", Status: "processing", Index: 0, TotalSteps: 2}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	if resp.Jobs[0].Progress != 100 {
		t.Errorf("expected progress clamped to 100, got %d", resp.Jobs[0].Progress)
	}
}

func TestHandleQueueClear(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{Song: "song", Status: "waiting"}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/queue", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	s.jobsMu.RLock()
	if len(s.jobs) != 0 {
		t.Errorf("expected jobs to be cleared, got %d", len(s.jobs))
	}
	s.jobsMu.RUnlock()
}

func TestHandleQueueCancel_CancelsRunningJob(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	var cancelled bool
	s.currentCancel = func() { cancelled = true }
	s.currentPID = 9999

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
	s.jobs["song"] = &JobState{Song: "song", Status: "processing"}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/queue/cancel", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !cancelled {
		t.Error("expected context cancel function to be called")
	}
	if len(groupPIDs) != 1 || groupPIDs[0] != -9999 {
		t.Errorf("expected group kill PID -9999, got %v", groupPIDs)
	}
	if len(killedPIDs) != 1 || killedPIDs[0] != 9999 {
		t.Errorf("expected kill PID 9999, got %v", killedPIDs)
	}
	s.jobsMu.RLock()
	if len(s.jobs) != 0 {
		t.Errorf("expected jobs cleared after cancel, got %d", len(s.jobs))
	}
	s.jobsMu.RUnlock()

	found := false
	logBufferMu.RLock()
	for _, entry := range logBuffer {
		if entry.Service == "backend" && entry.Level == "info" && strings.Contains(entry.Message, "Cancelled job: song") {
			found = true
			break
		}
	}
	logBufferMu.RUnlock()
	if !found {
		t.Error("expected backend info log for cancelled job")
	}
}

func TestRunSinglePipeline_MarksDone(t *testing.T) {
	root := setupQueueTestRoot(t)

	// Create a fake pipeline.sh that writes a stem file.
	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := `#!/bin/bash
mkdir -p "$4"
echo "stem" > "$4/vocals.wav"
`
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

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
	}

	// Run synchronously instead of via the worker goroutine.
	s.runSinglePipeline(job, s.jobs["song"])

	s.jobsMu.RLock()
	state := s.jobs["song"]
	s.jobsMu.RUnlock()

	if state.Status != "done" {
		t.Errorf("expected done status, got %q", state.Status)
	}
	if len(state.Files) == 0 {
		t.Error("expected at least one output file")
	}
}

func TestRunSinglePipeline_MarksError(t *testing.T) {
	root := setupQueueTestRoot(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	if err := os.WriteFile(fakePipeline, []byte("#!/bin/bash\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs: map[string]*JobState{
			"song": {Song: "song", Status: "waiting"},
		},
	}

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--viperx", "/app/input/song.wav"},
	}

	s.runSinglePipeline(job, s.jobs["song"])

	s.jobsMu.RLock()
	state := s.jobs["song"]
	s.jobsMu.RUnlock()

	if state.Status != "error" {
		t.Errorf("expected error status, got %q", state.Status)
	}
	if state.Error == "" {
		t.Error("expected error message")
	}
}

func TestWorker_ProcessesJob(t *testing.T) {
	root := setupQueueTestRoot(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := `#!/bin/bash
mkdir -p "$4"
echo "stem" > "$4/vocals.wav"
`
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

	s.jobsMu.RLock()
	state := s.jobs["song"]
	s.jobsMu.RUnlock()
	if state.Status != "done" {
		t.Errorf("expected done, got %q", state.Status)
	}
}

func TestHandleDeleteFile_RemovesFileFromJob(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	songDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	for _, name := range []string{"vocals.wav", "drums.wav"} {
		if err := os.WriteFile(filepath.Join(songDir, name), []byte("stem"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{
		Song:   "song",
		Status: "done",
		Files: []FileEntry{
			{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"},
			{Name: "drums.wav", Path: "/api/files/song/drums.wav"},
		},
	}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/delete?file=song/vocals.wav", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	s.jobsMu.RLock()
	job, ok := s.jobs["song"]
	s.jobsMu.RUnlock()
	if !ok {
		t.Fatal("expected job to remain")
	}
	if len(job.Files) != 1 || job.Files[0].Name != "drums.wav" {
		t.Errorf("expected only drums.wav, got %+v", job.Files)
	}
}

func TestHandleDeleteFile_RemovesJobWhenLastFile(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	songDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(songDir, "vocals.wav"), []byte("stem"), 0o644); err != nil {
		t.Fatalf("failed to create vocals.wav: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{
		Song:   "song",
		Status: "done",
		Files:  []FileEntry{{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"}},
	}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/delete?file=song/vocals.wav", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	s.jobsMu.RLock()
	_, exists := s.jobs["song"]
	s.jobsMu.RUnlock()
	if exists {
		t.Error("expected job to be removed when last file is deleted")
	}
}

func TestHandleQueueStatus_DoneJobFiltersMissingFiles(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	songDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(songDir, "vocals.wav"), []byte("stem"), 0o644); err != nil {
		t.Fatalf("failed to create vocals.wav: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{
		Song:   "song",
		Status: "done",
		Files: []FileEntry{
			{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"},
			{Name: "drums.wav", Path: "/api/files/song/drums.wav"},
		},
	}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	if len(resp.Jobs[0].Files) != 1 || resp.Jobs[0].Files[0].Name != "vocals.wav" {
		t.Errorf("expected only vocals.wav, got %+v", resp.Jobs[0].Files)
	}
}

func TestHandleQueueStatus_DoneJobRemovedWhenAllFilesMissing(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{
		Song:   "song",
		Status: "done",
		Files:  []FileEntry{{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"}},
	}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Jobs []*JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	for _, j := range resp.Jobs {
		if j.Song == "song" {
			t.Errorf("expected song job to be removed, got status %q", j.Status)
		}
	}
}
