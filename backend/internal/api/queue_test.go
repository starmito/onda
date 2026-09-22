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

	"github.com/starmito/onda/internal/cli"
)

// setupQueueTestRoot creates a temporary project root with input/output dirs
// and a dummy BS_Roformer_Viperx model directory so preset/step resolution
// succeeds in queue-level tests.
func setupQueueTestRoot(t *testing.T) string {
	t.Helper()
	root := setTestRoot(t, "queue-test-")

	for _, dir := range []string{"input", "output", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_Viperx")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "BS_Roformer_Viperx.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy checkpoint: %v", err)
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

	body := `{"input":"/app/input/song.wav","vocal_model":"BS_Roformer_Viperx","demucs":true,"stem_model":"htdemucs_ft"}`
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

	body := `{"input":"/app/input/song.wav","vocal_model":"BS_Roformer_Viperx"}`
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

	body := `{"input":"/app/input/song.wav","vocal_model":"BS_Roformer_Viperx"}`
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

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "processing-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	statusPath := filepath.Join(outputRoot, "pipeline_status.json")
	status := `{"status":"running","step":"demucs","progress":42,"device":"cuda","gpu_type":"NVIDIA GeForce RTX 5060 Ti"}`
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
	if job.GPUType != "NVIDIA GeForce RTX 5060 Ti" {
		t.Errorf("expected gpu_type NVIDIA GeForce RTX 5060 Ti, got %q", job.GPUType)
	}
	if job.RanOnCPU {
		t.Errorf("expected ran_on_cpu false for cuda job")
	}
}

func TestHandleQueueStatus_OverallProgressFallback(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "processing-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	statusPath := filepath.Join(outputRoot, "pipeline_status.json")
	// multi-step mode reports overall_progress as a 0-100 integer.
	status := `{"status":"running","step":"vocal","overall_progress":25,"device":"cpu","gpu_type":"N/A"}`
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
	if !resp.Jobs[0].RanOnCPU {
		t.Errorf("expected ran_on_cpu true for cpu job")
	}
	if resp.Jobs[0].GPUType != "N/A" {
		t.Errorf("expected gpu_type N/A for cpu job, got %q", resp.Jobs[0].GPUType)
	}
}

func TestHandleQueueStatus_OverallProgressClamped(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "processing-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	statusPath := filepath.Join(outputRoot, "pipeline_status.json")
	// An out-of-range overall_progress for a non-done job must be clamped below 100.
	status := `{"status":"running","step":"vocal","overall_progress":150,"device":"cpu","gpu_type":"N/A"}`
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
	if resp.Jobs[0].Progress != 99 {
		t.Errorf("expected progress clamped to 99 for non-done job, got %d", resp.Jobs[0].Progress)
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

	mockResourceProviders(t)

	// Create a fake pipeline.sh that writes a stem file.
	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("vocals.wav")
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
		Args: []string{fakePipeline, "--vocal-model", "/app/data/models/VR_Models/BS_Roformer_Viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
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

	mockResourceProviders(t)

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
		Args: []string{fakePipeline, "--vocal-model", "/app/data/models/VR_Models/BS_Roformer_Viperx", "/app/input/song.wav"},
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

	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("vocals.wav")
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
		Args: []string{fakePipeline, "--vocal-model", "/app/data/models/VR_Models/BS_Roformer_Viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
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

func TestHandleQueueStatus_DoneJobFiltersMissingResultFiles(t *testing.T) {
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
		Steps: []cli.PipelineStep{
			{
				ID: "step-1", Type: "demucs",
				Stems: map[string]cli.StemRoute{
					"vocals": {Action: cli.StemSave, Target: "result"},
					"drums":  {Action: cli.StemSave, Target: "result"},
				},
			},
		},
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

func TestHandleQueueStatus_DoneJobKeepsResultWhenIntermediateMissing(t *testing.T) {
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
		Steps: []cli.PipelineStep{
			{
				ID: "step-1", Type: "vocal",
				Stems: map[string]cli.StemRoute{
					"vocals":       {Action: cli.StemSave, Target: "result"},
					"instrumental": {Action: cli.StemDiscard},
				},
			},
		},
		Files: []FileEntry{
			{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"},
			{Name: "instrumental.wav", Path: "/api/files/song/instrumental.wav"},
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
		t.Errorf("expected job to keep only the existing result vocals.wav, got %+v", resp.Jobs[0].Files)
	}
}

func TestHandleQueueStatus_DoneJobRemovedWhenAllFilesMissing(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{
		Song:   "song",
		Status: "done",
		Steps: []cli.PipelineStep{
			{
				ID: "step-1", Type: "vocal",
				Stems: map[string]cli.StemRoute{
					"vocals": {Action: cli.StemSave, Target: "result"},
				},
			},
		},
		Files: []FileEntry{{Name: "vocals.wav", Path: "/api/files/song/vocals.wav"}},
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

func TestHandleQueueStatus_FinishedJobShowsDeviceAndRanOnCPU(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "cpu-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	status := `{"status":"completed","step":"rubberband","device":"cpu","gpu_type":"N/A"}`
	if err := os.WriteFile(filepath.Join(outputRoot, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["cpu-song"] = &JobState{Song: "cpu-song", Status: "done", Index: 0, TotalSteps: 3}
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
	j := resp.Jobs[0]
	if j.Device != "cpu" {
		t.Errorf("expected device cpu, got %q", j.Device)
	}
	if j.GPUType != "N/A" {
		t.Errorf("expected gpu_type N/A, got %q", j.GPUType)
	}
	if !j.RanOnCPU {
		t.Errorf("expected ran_on_cpu true for finished cpu job")
	}
}

func TestHandleQueueStatus_FinishedCudaJobDoesNotFlagCPU(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "cuda-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	status := `{"status":"completed","step":"rubberband","device":"cuda","gpu_type":"NVIDIA GeForce RTX 5060 Ti"}`
	if err := os.WriteFile(filepath.Join(outputRoot, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["cuda-song"] = &JobState{Song: "cuda-song", Status: "done", Index: 0, TotalSteps: 3}
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
	j := resp.Jobs[0]
	if j.Device != "cuda" {
		t.Errorf("expected device cuda, got %q", j.Device)
	}
	if j.GPUType != "NVIDIA GeForce RTX 5060 Ti" {
		t.Errorf("expected gpu_type NVIDIA GeForce RTX 5060 Ti, got %q", j.GPUType)
	}
	if j.RanOnCPU {
		t.Errorf("expected ran_on_cpu false for finished cuda job")
	}
}

func TestHandleQueueStatus_FailedDeviceStepExposesReason(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "device-fail")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	status := `{"status":"failed","step":"device","error":"CUDA requested but no usable GPU found","exit_code":1,"device":"cpu","gpu_type":"N/A"}`
	if err := os.WriteFile(filepath.Join(outputRoot, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["device-fail"] = &JobState{Song: "device-fail", Status: "error", Index: 0, TotalSteps: 1}
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
	j := resp.Jobs[0]
	if j.Status != "error" {
		t.Errorf("expected status error, got %q", j.Status)
	}
	if j.Device != "cpu" {
		t.Errorf("expected device cpu, got %q", j.Device)
	}
	if !j.RanOnCPU {
		t.Errorf("expected ran_on_cpu true for failed device step")
	}
	if j.FailureDetails == nil {
		t.Fatalf("expected failure_details, got nil")
	}
	if j.FailureDetails.Step != "device" {
		t.Errorf("expected failure step device, got %q", j.FailureDetails.Step)
	}
	if !strings.Contains(j.FailureDetails.Error, "no usable GPU") {
		t.Errorf("expected failure error to mention no usable GPU, got %q", j.FailureDetails.Error)
	}
}

func TestHandleQueueStatus_PipelineETAAndElapsed(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "processing-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	status := `{"status":"running","step":"vocal","progress":58,"eta":120,"elapsed":179,"device":"cuda","gpu_type":"NVIDIA GeForce RTX 3060"}`
	if err := os.WriteFile(filepath.Join(songDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write per-song pipeline status: %v", err)
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
	j := resp.Jobs[0]
	if j.Progress != 58 {
		t.Errorf("expected progress 58, got %d", j.Progress)
	}
	if j.ETA != 120 {
		t.Errorf("expected eta 120, got %d", j.ETA)
	}
	if j.Elapsed != 179 {
		t.Errorf("expected elapsed 179, got %d", j.Elapsed)
	}
}

func TestHandleQueueStatus_DoneJobETAZero(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "done-song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	// Stale eta/elapsed from when the job was running must not leak to the UI.
	status := `{"status":"running","step":"vocal","progress":0.75,"eta":30,"elapsed":90,"device":"cuda","gpu_type":"NVIDIA GeForce RTX 3060"}`
	if err := os.WriteFile(filepath.Join(songDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write per-song pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["done-song"] = &JobState{Song: "done-song", Status: "done", Index: 0, TotalSteps: 2}
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
	j := resp.Jobs[0]
	if j.Progress != 100 {
		t.Errorf("expected progress 100 for done job, got %d", j.Progress)
	}
	if j.ETA != 0 {
		t.Errorf("expected eta 0 for done job, got %d", j.ETA)
	}
	if j.Elapsed != 0 {
		t.Errorf("expected elapsed 0 for done job, got %d", j.Elapsed)
	}
}

func TestHandleQueueStatus_WaitingJobETAZero(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	s.jobsMu.Lock()
	s.jobs["waiting-song"] = &JobState{Song: "waiting-song", Status: "waiting", Index: 0, TotalSteps: 2}
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
	j := resp.Jobs[0]
	if j.Progress != 0 {
		t.Errorf("expected progress 0 for waiting job, got %d", j.Progress)
	}
	if j.ETA != 0 {
		t.Errorf("expected eta 0 for waiting job, got %d", j.ETA)
	}
	if j.Elapsed != 0 {
		t.Errorf("expected elapsed 0 for waiting job, got %d", j.Elapsed)
	}
}

func TestHandleQueueStatus_MissingStatusFileDoesNotBreak(t *testing.T) {
	setupQueueTestRoot(t)
	s := newQueueTestServer(t)

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
	j := resp.Jobs[0]
	if j.Progress != 0 {
		t.Errorf("expected progress 0 when status file missing, got %d", j.Progress)
	}
	if j.ETA != 0 {
		t.Errorf("expected eta 0 when status file missing, got %d", j.ETA)
	}
	if j.Elapsed != 0 {
		t.Errorf("expected elapsed 0 when status file missing, got %d", j.Elapsed)
	}
}

func TestHandleQueueStatus_PerSongValuesDoNotBleed(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	for _, song := range []string{"song-a", "song-b"} {
		songDir := filepath.Join(outputRoot, song)
		if err := os.MkdirAll(songDir, 0o755); err != nil {
			t.Fatalf("failed to create song output dir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "song-a", "pipeline_status.json"), []byte(`{"status":"running","step":"vocal","progress":25,"eta":60,"elapsed":20,"device":"cuda"}`), 0o644); err != nil {
		t.Fatalf("failed to write song-a status: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "song-b", "pipeline_status.json"), []byte(`{"status":"running","step":"demucs","progress":75,"eta":15,"elapsed":45,"device":"cpu"}`), 0o644); err != nil {
		t.Fatalf("failed to write song-b status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["song-a"] = &JobState{Song: "song-a", Status: "processing", Index: 0, TotalSteps: 2}
	s.jobs["song-b"] = &JobState{Song: "song-b", Status: "processing", Index: 1, TotalSteps: 2}
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
	if len(resp.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(resp.Jobs))
	}

	bySong := make(map[string]*JobState)
	for _, j := range resp.Jobs {
		bySong[j.Song] = j
	}

	a, ok := bySong["song-a"]
	if !ok {
		t.Fatal("song-a missing from response")
	}
	if a.Progress != 25 || a.ETA != 60 || a.Elapsed != 20 {
		t.Errorf("song-a mismatch: progress=%d eta=%d elapsed=%d", a.Progress, a.ETA, a.Elapsed)
	}

	b, ok := bySong["song-b"]
	if !ok {
		t.Fatal("song-b missing from response")
	}
	if b.Progress != 75 || b.ETA != 15 || b.Elapsed != 45 {
		t.Errorf("song-b mismatch: progress=%d eta=%d elapsed=%d", b.Progress, b.ETA, b.Elapsed)
	}
}

// TestHandleQueueStatus_ChainedStep2DoesNotShow100WithStemsOnDisk reproduces the
// bug where a chained job (vocal -> demucs) reported 100 % to the UI at the
// very beginning of the second step, just because the first step's stems were
// already on disk. The queue must always derive the progress of an active job
// from pipeline_status.json, never from the presence of result stems.
func TestHandleQueueStatus_ChainedStep2DoesNotShow100WithStemsOnDisk(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "e2e_final")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	// Stems from the first step are already on disk, as happens between steps.
	if err := os.WriteFile(filepath.Join(songDir, "vocals.wav"), []byte("stem"), 0o644); err != nil {
		t.Fatalf("failed to create step-1 stem: %v", err)
	}

	// pipeline_status.json reports the real weighted overall progress at the
	// start of the second step. Values are 0-100 percentages per the tracker contract.
	status := `{"status":"running","step":"demucs","progress":33.3333,"overall_progress":33.3333,"step_progress":0.0,"eta":1,"elapsed":37.32}`
	if err := os.WriteFile(filepath.Join(songDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write per-song pipeline status: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["e2e_final"] = &JobState{
		Song:        "e2e_final",
		Status:      "processing",
		Index:       0,
		TotalSteps:  2,
		CurrentStep: 1,
		StepName:    "Vocal",
		Progress:    99, // previous step completed, non-done cap
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
	j := resp.Jobs[0]
	if j.Status != "processing" {
		t.Errorf("expected status processing, got %q", j.Status)
	}
	if j.Progress == 100 {
		t.Errorf("progress must not be 100 while the job is processing; got %d", j.Progress)
	}
	if j.Progress != 33 {
		t.Errorf("expected progress 33 (33.3333 %% rounded), got %d", j.Progress)
	}
	if j.CurrentStep != 2 {
		t.Errorf("expected current_step 2, got %d", j.CurrentStep)
	}
	if j.StepName != "Demucs" {
		t.Errorf("expected step name Demucs, got %q", j.StepName)
	}
}

// TestHandleQueueStatus_NonDoneProgressIsBelow100 enforces the invariant that
// only finished jobs report 100 %. Every other status must stay strictly below
// 100 even when the status file contains values at or above 100 %.
func TestHandleQueueStatus_NonDoneProgressIsBelow100(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song output dir: %v", err)
	}
	// A pathological status file claims 150 % while still running.
	status := `{"status":"running","step":"demucs","progress":150,"overall_progress":150,"eta":1,"elapsed":10}`
	if err := os.WriteFile(filepath.Join(songDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write per-song pipeline status: %v", err)
	}

	for _, st := range []string{"waiting", "processing", "blocked_no_gpu", "error"} {
		s.jobsMu.Lock()
		s.jobs = map[string]*JobState{
			"song": {Song: "song", Status: st, Index: 0, TotalSteps: 2},
		}
		s.jobsMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)

		var resp struct {
			Jobs []*JobState `json:"jobs"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("status %s: failed to decode response: %v", st, err)
		}
		if len(resp.Jobs) != 1 {
			t.Fatalf("status %s: expected 1 job, got %d", st, len(resp.Jobs))
		}
		j := resp.Jobs[0]
		if j.Status != st {
			t.Errorf("status %s: expected status unchanged, got %q", st, j.Status)
		}
		if j.Progress >= 100 {
			t.Errorf("status %s: progress must be < 100, got %d", st, j.Progress)
		}
	}
}
