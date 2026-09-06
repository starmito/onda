package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCollectProcessState(t *testing.T) {
	cmd := exec.Command("sleep", "3")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	defer cmd.Process.Kill()

	// Give the kernel a moment to create /proc/<pid>.
	time.Sleep(20 * time.Millisecond)

	info := collectProcessState(cmd.Process.Pid)
	if !info.Alive {
		t.Errorf("expected process to be alive")
	}
	if info.Pid != cmd.Process.Pid {
		t.Errorf("pid = %d, want %d", info.Pid, cmd.Process.Pid)
	}
	if !strings.Contains(info.Cmd, "sleep") {
		t.Errorf("cmd = %q, want to contain 'sleep'", info.Cmd)
	}
	if info.ElapsedSec < 0 {
		t.Errorf("elapsed_sec = %d, want >= 0", info.ElapsedSec)
	}
}

func TestCollectProcessState_NotFound(t *testing.T) {
	// PIDs are positive and the kernel will never assign this one.
	info := collectProcessState(99999999)
	if info.Alive {
		t.Errorf("expected process to be not alive")
	}
	if info.Pid != 99999999 {
		t.Errorf("pid = %d, want 99999999", info.Pid)
	}
}

func TestHandleProcessStatus(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	defer cmd.Process.Kill()

	s := &Server{
		mux:       http.NewServeMux(),
		jobs:      make(map[string]*JobState),
		currentPID: cmd.Process.Pid,
	}
	s.mux.HandleFunc("GET /api/processes/status", s.handleProcessStatus)
	s.jobs["blocked_song"] = &JobState{
		Song:             "blocked_song",
		Status:           "blocked_no_gpu",
		Error:            "insufficient VRAM",
		BlockedReason:    "insufficient_vram",
		BlockedReasonMsg: "insufficient VRAM: test",
		Index:            0,
	}
	s.jobs["waiting_song"] = &JobState{
		Song:   "waiting_song",
		Status: "waiting",
		Index:  1,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/processes/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		QueueJobs       []JobState `json:"queue_jobs"`
		GPU             struct {
			TotalMB int    `json:"total_mb"`
			UsedMB  int    `json:"used_mb"`
			FreeMB  int    `json:"free_mb"`
			Runtime string `json:"runtime"`
			Name    string `json:"name"`
		} `json:"gpu"`
		PipelineProcess struct {
			Alive      bool   `json:"alive"`
			Pid        int    `json:"pid"`
			Cmd        string `json:"cmd"`
			ElapsedSec int    `json:"elapsed_sec"`
		} `json:"pipeline_process"`
		Blocked []struct {
			Song    string `json:"song"`
			Message string `json:"message"`
		} `json:"blocked"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.QueueJobs) != 2 {
		t.Errorf("expected 2 queue_jobs, got %d", len(resp.QueueJobs))
	}
	if resp.PipelineProcess.Pid != cmd.Process.Pid {
		t.Errorf("pipeline_process.pid = %d, want %d", resp.PipelineProcess.Pid, cmd.Process.Pid)
	}
	if !resp.PipelineProcess.Alive {
		t.Errorf("expected pipeline_process.alive = true")
	}
	if len(resp.Blocked) != 1 {
		t.Errorf("expected 1 blocked job, got %d", len(resp.Blocked))
	} else {
		if resp.Blocked[0].Song != "blocked_song" {
			t.Errorf("blocked[0].song = %q, want blocked_song", resp.Blocked[0].Song)
		}
		if resp.Blocked[0].Message == "" {
			t.Errorf("blocked[0].message empty")
		}
	}
}
