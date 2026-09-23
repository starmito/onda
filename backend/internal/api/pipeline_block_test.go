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

	"github.com/starmito/onda/internal/cli"
)

func TestCheckVramHeadroom(t *testing.T) {
	tests := []struct {
		name        string
		freeMB      int
		totalMB     int
		model       string
		stepType    string
		cfg         VRAMConfig
		wantOK      bool
		wantMin     int
		wantReason  bool
		wantWarning bool
	}{
		{
			name:        "vocal fits with margin",
			freeMB:      20000,
			totalMB:     24000,
			model:       "BS_Roformer_Viperx",
			stepType:    "vocal",
			wantOK:      true,
			wantMin:     1400,
			wantReason:  false,
			wantWarning: false,
		},
		{
			name:        "vocal fits comfortably below total vram",
			freeMB:      15475,
			totalMB:     16311,
			model:       "BS_Roformer_Viperx",
			stepType:    "vocal",
			wantOK:      true,
			wantMin:     1400,
			wantReason:  false,
			wantWarning: false,
		},
		{
			name:        "vocal blocked low vram",
			freeMB:      500,
			totalMB:     16311,
			model:       "BS_Roformer_Viperx",
			stepType:    "vocal",
			wantOK:      false,
			wantMin:     1400,
			wantReason:  true,
			wantWarning: false,
		},
		{
			name:        "unknown model conservative",
			freeMB:      3000,
			totalMB:     4096,
			model:       "not_a_known_model_v1",
			stepType:    "vocal",
			wantOK:      true,
			wantMin:     2000,
			wantReason:  false,
			wantWarning: false,
		},
		{
			name:        "unknown model blocked",
			freeMB:      1000,
			totalMB:     4096,
			model:       "not_a_known_model_v1",
			stepType:    "vocal",
			wantOK:      false,
			wantMin:     2000,
			wantReason:  true,
			wantWarning: false,
		},
		{
			name:        "measured peak overrides estimate for demucs",
			freeMB:      1800,
			totalMB:     8192,
			model:       "htdemucs_ft",
			stepType:    "demucs",
			cfg:         VRAMConfig{DemucsSegment: 7},
			wantOK:      true,
			wantMin:     1400,
			wantReason:  false,
			wantWarning: false,
		},
		{
			name:        "analytical fallback when no measured peak",
			freeMB:      3000,
			totalMB:     4096,
			model:       "MDX23C",
			stepType:    "vocal",
			cfg:         VRAMConfig{SegmentSize: 128, BatchSize: 1},
			wantOK:      true,
			wantMin:     2000,
			wantReason:  false,
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, needed, reason, warning := checkVramHeadroom(tt.freeMB, tt.totalMB, tt.model, tt.stepType, tt.cfg, "")
			if ok != tt.wantOK {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) ok = %v, want %v", tt.freeMB, tt.totalMB, tt.model, tt.stepType, ok, tt.wantOK)
			}
			if needed < tt.wantMin {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) needed = %d, want >= %d", tt.freeMB, tt.totalMB, tt.model, tt.stepType, needed, tt.wantMin)
			}
			if tt.wantReason && reason == "" {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) reason empty, want non-empty", tt.freeMB, tt.totalMB, tt.model, tt.stepType)
			}
			if !tt.wantReason && reason != "" {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) reason = %q, want empty", tt.freeMB, tt.totalMB, tt.model, tt.stepType, reason)
			}
			if tt.wantWarning && warning == "" {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) warning empty, want non-empty", tt.freeMB, tt.totalMB, tt.model, tt.stepType)
			}
			if !tt.wantWarning && warning != "" {
				t.Errorf("checkVramHeadroom(%d, %d, %q, %q) warning = %q, want empty", tt.freeMB, tt.totalMB, tt.model, tt.stepType, warning)
			}
		})
	}
}

func TestRunSinglePipeline_BlockedNoGPU(t *testing.T) {
	orig := gpuInfoProvider
	defer func() { gpuInfoProvider = orig }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMFreeMB: 500}
	}
	mockLowRAMProvider(t, 32000)

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	job := JobRequest{
		Song: "test",
		Config: SeparateRequest{
			VocalModel: "BS_Roformer_Viperx",
		},
	}

	s.runSinglePipeline(job, state)

	if state.Status != "blocked_no_gpu" {
		t.Errorf("status = %q, want blocked_no_gpu", state.Status)
	}
	if state.Error == "" {
		t.Errorf("expected non-empty error reason")
	}
	if state.BlockedReason != "insufficient_vram" {
		t.Errorf("blocked_reason = %q, want insufficient_vram", state.BlockedReason)
	}
	if state.BlockedReasonMsg == "" {
		t.Errorf("expected non-empty blocked_reason_msg")
	}
	if state.Progress != 0 {
		t.Errorf("progress = %d, want 0", state.Progress)
	}
}

func TestRunSinglePipeline_BlockedNoGPU_PresetFallback(t *testing.T) {
	orig := gpuInfoProvider
	defer func() { gpuInfoProvider = orig }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMFreeMB: 500}
	}
	mockLowRAMProvider(t, 32000)

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	job := JobRequest{
		Song: "test",
		Config: SeparateRequest{
			Preset: "fast",
			Input:  "/app/input/test.wav",
		},
	}

	s.runSinglePipeline(job, state)

	if state.Status != "blocked_no_gpu" {
		t.Errorf("status = %q, want blocked_no_gpu", state.Status)
	}
	if !strings.Contains(state.BlockedReasonMsg, `"fast"`) {
		t.Errorf("blocked reason should mention preset, got %q", state.BlockedReasonMsg)
	}
}

func TestRunSinglePipeline_ForceVRAM(t *testing.T) {
	orig := gpuInfoProvider
	defer func() { gpuInfoProvider = orig }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMFreeMB: 500}
	}

	// Create a fake pipeline script in the package directory so it can be
	// referenced as the first argument without touching /tmp.
	scriptPath := filepath.Join("testdata", "fake_pipeline_force_vram.sh")
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("failed to create testdata: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho ok\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake script: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove("testdata")
	})

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	job := JobRequest{
		Song: "test",
		Args: []string{scriptPath},
		Config: SeparateRequest{
			VocalModel: "BS_Roformer_Viperx",
			ForceVRAM:  true,
			ForceRAM:   true,
		},
	}

	s.runSinglePipeline(job, state)

	if state.Status == "blocked_no_gpu" {
		t.Errorf("ForceVRAM=true should not block the job")
	}
}

func TestRunMultiStepPipeline_BlockedNoGPU(t *testing.T) {
	orig := gpuInfoProvider
	defer func() { gpuInfoProvider = orig }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMFreeMB: 500}
	}
	mockLowRAMProvider(t, 32000)

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	s.jobs["test"] = state
	steps := []cli.PipelineStep{
		{ID: "vocal", Type: "vocal", Model: "BS_Roformer_Viperx", Enabled: true},
	}
	job := JobRequest{
		Song:   "test",
		Config: SeparateRequest{Input: "/app/input/test.wav"},
		Steps:  steps,
	}

	s.runMultiStepPipeline(job, steps, state)

	if state.Status != "blocked_no_gpu" {
		t.Errorf("status = %q, want blocked_no_gpu", state.Status)
	}
	if !strings.Contains(state.Error, "insufficient VRAM") {
		t.Errorf("error = %q, want to contain 'insufficient VRAM'", state.Error)
	}
}

func TestHandleQueueStatus_BlockedNoGPUFields(t *testing.T) {
	s := &Server{
		mux:  http.NewServeMux(),
		jobs: make(map[string]*JobState),
	}
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.jobs["blocked_song"] = &JobState{
		Song:             "blocked_song",
		Status:           "blocked_no_gpu",
		Error:            "insufficient VRAM",
		BlockedReason:    "insufficient_vram",
		BlockedReasonMsg: "insufficient VRAM: test",
		Index:            0,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Jobs []JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	j := resp.Jobs[0]
	if j.Status != "blocked_no_gpu" {
		t.Errorf("status = %q, want blocked_no_gpu", j.Status)
	}
	if j.BlockedReason != "insufficient_vram" {
		t.Errorf("blocked_reason = %q, want insufficient_vram", j.BlockedReason)
	}
	if j.BlockedReasonMsg == "" {
		t.Errorf("blocked_reason_msg empty")
	}
}

func TestCheckRamHeadroom(t *testing.T) {
	tests := []struct {
		name       string
		available  int
		model      string
		stepType   string
		wantOK     bool
		wantReason bool
	}{
		{"vocal fits", 8000, "BS_Roformer_Viperx", "vocal", true, false},
		{"vocal below measured", 2000, "BS_Roformer_Viperx", "vocal", false, true},
		{"vocal real measurement 4700 MB", 4700, "BS_Roformer_Viperx", "vocal", true, false},
		{"demucs fits", 5000, "htdemucs_ft", "demucs", true, false},
		{"demucs below estimate", 3000, "htdemucs_ft", "demucs", false, true},
		{"unknown conservative", 5000, "unknown_model", "vocal", true, false},
		{"unknown below estimate", 2000, "unknown_model", "vocal", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, needed, reason := checkRamHeadroom(tt.available, tt.model, tt.stepType)
			if ok != tt.wantOK {
				t.Errorf("checkRamHeadroom(%d, %q, %q) ok = %v, want %v", tt.available, tt.model, tt.stepType, ok, tt.wantOK)
			}
			if needed <= 0 {
				t.Errorf("checkRamHeadroom(%d, %q, %q) needed = %d, want > 0", tt.available, tt.model, tt.stepType, needed)
			}
			if tt.wantReason && reason == "" {
				t.Errorf("checkRamHeadroom(%d, %q, %q) reason empty, want non-empty", tt.available, tt.model, tt.stepType)
			}
			if !tt.wantReason && reason != "" {
				t.Errorf("checkRamHeadroom(%d, %q, %q) reason = %q, want empty", tt.available, tt.model, tt.stepType, reason)
			}
		})
	}
}

func TestRunSinglePipeline_LowRAMProceeds(t *testing.T) {
	mockResourceProviders(t)
	mockLowRAMProvider(t, 1024)

	scriptPath := filepath.Join("testdata", "fake_pipeline_low_ram.sh")
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("failed to create testdata: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho ok\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake script: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove("testdata")
	})

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	job := JobRequest{
		Song: "test",
		Args: []string{scriptPath},
		Config: SeparateRequest{
			VocalModel: "BS_Roformer_Viperx",
		},
	}

	s.runSinglePipeline(job, state)

	if state.Status == "blocked_no_gpu" {
		t.Errorf("low RAM should not block the job, got status %q", state.Status)
	}
	if state.BlockedReason == "insufficient_ram" {
		t.Errorf("low RAM should not set insufficient_ram blocked reason")
	}
}

func TestRunMultiStepPipeline_LowRAMProceeds(t *testing.T) {
	mockResourceProviders(t)
	mockLowRAMProvider(t, 1024)

	s := &Server{jobs: make(map[string]*JobState)}
	state := &JobState{Song: "test", Status: "waiting"}
	s.jobs["test"] = state
	steps := []cli.PipelineStep{
		{ID: "vocal", Type: "vocal", Model: "BS_Roformer_Viperx", Enabled: true},
	}
	job := JobRequest{
		Song:   "test",
		Config: SeparateRequest{Input: "/app/input/test.wav", Device: "cpu"},
		Steps:  steps,
	}

	s.runMultiStepPipeline(job, steps, state)

	if state.Status == "blocked_no_gpu" {
		t.Errorf("low RAM should not block the multi-step job, got status %q", state.Status)
	}
	if state.BlockedReason == "insufficient_ram" {
		t.Errorf("low RAM should not set insufficient_ram blocked reason")
	}
}

func TestShouldAutoFailBlocked(t *testing.T) {
	cases := []struct {
		elapsed int
		want    bool
	}{
		{0, false},
		{1, false},
		{120, false},
		{121, true},
		{300, true},
	}
	for _, tc := range cases {
		if got := shouldAutoFailBlocked(tc.elapsed); got != tc.want {
			t.Errorf("shouldAutoFailBlocked(%d) = %v, want %v", tc.elapsed, got, tc.want)
		}
	}
}

func TestHandleQueueStatus_BlockedNoGPU_RecentStaysBlocked(t *testing.T) {
	s := &Server{
		mux:  http.NewServeMux(),
		jobs: make(map[string]*JobState),
	}
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.jobs["blocked_song"] = &JobState{
		Song:             "blocked_song",
		Status:           "blocked_no_gpu",
		Error:            "insufficient VRAM",
		BlockedReason:    "insufficient_vram",
		BlockedReasonMsg: "insufficient VRAM: test",
		Index:            0,
		StartedAt:        time.Now().Add(-30 * time.Second),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var resp struct {
		Jobs []JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	if resp.Jobs[0].Status != "blocked_no_gpu" {
		t.Errorf("recent blocked job changed to %q, want blocked_no_gpu", resp.Jobs[0].Status)
	}
}

func TestHandleQueueStatus_BlockedNoGPU_AutoExpiresAfterTwoMinutes(t *testing.T) {
	s := &Server{
		mux:  http.NewServeMux(),
		jobs: make(map[string]*JobState),
	}
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.jobs["blocked_song"] = &JobState{
		Song:             "blocked_song",
		Status:           "blocked_no_gpu",
		Error:            "insufficient VRAM",
		BlockedReason:    "insufficient_vram",
		BlockedReasonMsg: "insufficient VRAM: test",
		Index:            0,
		StartedAt:        time.Now().Add(-121 * time.Second),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var resp struct {
		Jobs []JobState `json:"jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}
	j := resp.Jobs[0]
	if j.Status != "error" {
		t.Errorf("status = %q, want error", j.Status)
	}
	wantError := "Cancelado automáticamente: VRAM insuficiente durante 2 min"
	if j.Error != wantError {
		t.Errorf("error = %q, want %q", j.Error, wantError)
	}
	if j.BlockedReason != "" || j.BlockedReasonMsg != "" {
		t.Errorf("expected blocked fields cleared, got reason=%q msg=%q", j.BlockedReason, j.BlockedReasonMsg)
	}
}
