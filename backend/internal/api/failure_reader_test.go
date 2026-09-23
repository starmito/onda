package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadFailureDiagnostics(t *testing.T) {
	root := setTestRoot(t, "failure-diagnostics-")
	outputRoot := filepath.Join(root, "output")
	outputDir := filepath.Join(outputRoot, "song")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	status := `{"status":"failed","step":"demucs","exit_code":1,"error":"model not found"}`
	if err := os.WriteFile(filepath.Join(outputDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline_status.json: %v", err)
	}

	failedDir := filepath.Join(outputDir, "_failed_demucs")
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		t.Fatalf("failed to create failed dir: %v", err)
	}
	stderr := "line1\nline2\nline3\nCUDA out of memory\n"
	if err := os.WriteFile(filepath.Join(failedDir, "stderr.log"), []byte(stderr), 0o644); err != nil {
		t.Fatalf("failed to write stderr.log: %v", err)
	}

	diag := readFailureDiagnostics(outputRoot, "song")
	if diag == nil {
		t.Fatal("expected diagnostics, got nil")
	}
	if diag.Step != "demucs" {
		t.Errorf("expected step demucs, got %q", diag.Step)
	}
	if diag.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", diag.ExitCode)
	}
	if diag.Error != "model not found" {
		t.Errorf("expected error 'model not found', got %q", diag.Error)
	}
	if diag.FailedDir != "_failed_demucs" {
		t.Errorf("expected failed dir '_failed_demucs', got %q", diag.FailedDir)
	}
	if !strings.Contains(diag.Stderr, "CUDA out of memory") {
		t.Errorf("expected stderr to contain 'CUDA out of memory', got %q", diag.Stderr)
	}
}

func TestReadFailureDiagnostics_NoFailure(t *testing.T) {
	root := setTestRoot(t, "failure-diagnostics-")
	outputRoot := filepath.Join(root, "output")
	outputDir := filepath.Join(outputRoot, "song")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	status := `{"status":"running","step":"demucs","progress":0.5}`
	if err := os.WriteFile(filepath.Join(outputDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline_status.json: %v", err)
	}

	if diag := readFailureDiagnostics(outputRoot, "song"); diag != nil {
		t.Fatalf("expected nil diagnostics for running status, got %+v", diag)
	}
}

func TestReadFailureDiagnostics_PrefersMatchingStep(t *testing.T) {
	root := setTestRoot(t, "failure-diagnostics-")
	outputRoot := filepath.Join(root, "output")
	outputDir := filepath.Join(outputRoot, "song")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	status := `{"status":"failed","step":"vocal","exit_code":2,"error":"missing model"}`
	if err := os.WriteFile(filepath.Join(outputDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline_status.json: %v", err)
	}

	for _, step := range []string{"demucs", "vocal"} {
		failedDir := filepath.Join(outputDir, "_failed_"+step)
		if err := os.MkdirAll(failedDir, 0o755); err != nil {
			t.Fatalf("failed to create failed dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(failedDir, "stderr.log"), []byte(step+" error"), 0o644); err != nil {
			t.Fatalf("failed to write stderr.log: %v", err)
		}
	}

	diag := readFailureDiagnostics(outputRoot, "song")
	if diag == nil {
		t.Fatal("expected diagnostics, got nil")
	}
	if diag.FailedDir != "_failed_vocal" {
		t.Errorf("expected _failed_vocal, got %q", diag.FailedDir)
	}
	if !strings.Contains(diag.Stderr, "vocal error") {
		t.Errorf("expected vocal stderr, got %q", diag.Stderr)
	}
}

func TestCleanupOldFailedDirs(t *testing.T) {
	root := setTestRoot(t, "failure-cleanup-")
	outputDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	now := time.Now()
	for i := 1; i <= 4; i++ {
		dir := filepath.Join(outputDir, "_failed_step"+string(rune('0'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		// Make step1 oldest, step4 newest.
		if err := os.Chtimes(dir, now.Add(-time.Duration(5-i)*time.Hour), now.Add(-time.Duration(5-i)*time.Hour)); err != nil {
			t.Fatalf("failed to chtimes: %v", err)
		}
	}

	if err := cleanupOldFailedDirs(outputDir, 2, ""); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	var remaining []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "_failed_") {
			remaining = append(remaining, e.Name())
		}
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining dirs, got %d: %v", len(remaining), remaining)
	}
	want := map[string]bool{"_failed_step3": true, "_failed_step4": true}
	for _, r := range remaining {
		if !want[r] {
			t.Errorf("unexpected remaining dir %q", r)
		}
	}
}

func TestCleanupOldFailedDirs_ExcludesStep(t *testing.T) {
	root := setTestRoot(t, "failure-cleanup-")
	outputDir := filepath.Join(root, "output", "song")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	now := time.Now()
	oldDir := filepath.Join(outputDir, "_failed_old")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.Chtimes(oldDir, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("failed to chtimes: %v", err)
	}

	currentDir := filepath.Join(outputDir, "_failed_current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.Chtimes(currentDir, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("failed to chtimes: %v", err)
	}

	if err := cleanupOldFailedDirs(outputDir, 1, "current"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	var remaining []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "_failed_") {
			remaining = append(remaining, e.Name())
		}
	}
	if len(remaining) != 1 || remaining[0] != "_failed_current" {
		t.Fatalf("expected _failed_current to survive, got %v", remaining)
	}
}

func TestHandleQueueStatus_FailureDetails(t *testing.T) {
	root := setupQueueTestRoot(t)
	s := newQueueTestServer(t)

	outputRoot := filepath.Join(root, "output")
	songDir := filepath.Join(outputRoot, "song")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	status := `{"status":"failed","step":"demucs","exit_code":1,"error":"model not found"}`
	if err := os.WriteFile(filepath.Join(songDir, "pipeline_status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write pipeline_status.json: %v", err)
	}
	failedDir := filepath.Join(songDir, "_failed_demucs")
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		t.Fatalf("failed to create failed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(failedDir, "stderr.log"), []byte("CUDA out of memory"), 0o644); err != nil {
		t.Fatalf("failed to write stderr.log: %v", err)
	}

	s.jobsMu.Lock()
	s.jobs["song"] = &JobState{Song: "song", Status: "error", Index: 0}
	s.jobsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	t.Logf("queue/status response: %s", rr.Body.String())

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
	if job.FailureDetails == nil {
		t.Fatal("expected failure_details in response")
	}
	if job.FailureDetails.Step != "demucs" {
		t.Errorf("expected step demucs, got %q", job.FailureDetails.Step)
	}
	if job.FailureDetails.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", job.FailureDetails.ExitCode)
	}
	if !strings.Contains(job.FailureDetails.Stderr, "CUDA out of memory") {
		t.Errorf("expected stderr to contain failure, got %q", job.FailureDetails.Stderr)
	}
}
