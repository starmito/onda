package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIsOOMPipelineError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		output string
		want   bool
	}{
		{
			name:   "cuda OOM in stderr",
			err:    &exec.ExitError{ProcessState: &os.ProcessState{}},
			output: "RuntimeError: CUDA out of memory",
			want:   true,
		},
		{
			name:   "generic OOM",
			err:    &exec.ExitError{ProcessState: &os.ProcessState{}},
			output: "out of memory while allocating",
			want:   true,
		},
		{
			name:   "oom abbreviation",
			err:    &exec.ExitError{ProcessState: &os.ProcessState{}},
			output: "process killed by OOM killer",
			want:   true,
		},
		{
			name:   "SIGKILL exit",
			err:    fakeExitError(syscall.SIGKILL),
			output: "",
			want:   true,
		},
		{
			name:   "normal error",
			err:    &exec.ExitError{ProcessState: &os.ProcessState{}},
			output: "model file not found",
			want:   false,
		},
		{
			name:   "no error",
			err:    nil,
			output: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isOOMPipelineError(tc.err, tc.output)
			if got != tc.want {
				t.Fatalf("isOOMPipelineError(%v, %q) = %v, want %v", tc.err, tc.output, got, tc.want)
			}
		})
	}
}

func fakeExitError(sig syscall.Signal) *exec.ExitError {
	// exec.ExitError does not expose a public constructor; we use a real
	// process that exits with the requested signal.
	if sig == 0 {
		return &exec.ExitError{ProcessState: &os.ProcessState{}}
	}
	cmd := exec.Command("sh", "-c", "kill -"+strconv.Itoa(int(sig))+" $$")
	_ = cmd.Run()
	return &exec.ExitError{ProcessState: cmd.ProcessState}
}

func TestVRAMTestStateMachine(t *testing.T) {
	setTestRoot(t, "vram-test-")
	defer func() {
		activeVRAMTest.mu.Lock()
		activeVRAMTest.running = false
		activeVRAMTest.model = ""
		activeVRAMTest.stepType = ""
		activeVRAMTest.status = ""
		activeVRAMTest.progress = 0
		activeVRAMTest.peakMB = 0
		activeVRAMTest.n = 0
		activeVRAMTest.errMsg = ""
		activeVRAMTest.startedAt = time.Time{}
		activeVRAMTest.finished = time.Time{}
		activeVRAMTest.mu.Unlock()
	}()

	if !startVRAMTest("test-model", "vocal", VRAMConfig{SegmentSize: 1}) {
		t.Fatal("expected startVRAMTest to succeed")
	}
	st := getVRAMTestStatus()
	if !st.Running || st.Status != "running" || st.Model != "test-model" {
		t.Fatalf("unexpected running status: %+v", st)
	}

	setVRAMTestProgress(42)
	st = getVRAMTestStatus()
	if st.Progress != 42 {
		t.Fatalf("expected progress 42, got %d", st.Progress)
	}

	if startVRAMTest("other", "vocal", VRAMConfig{}) {
		t.Fatal("expected startVRAMTest to fail while another test is running")
	}

	finishVRAMTest("success", 2048, 12, "")
	st = getVRAMTestStatus()
	if st.Running || st.Status != "success" || st.PeakMB != 2048 || st.N != 12 {
		t.Fatalf("unexpected finished status: %+v", st)
	}
}

func TestHandleModelsVRAMTest_NotFound(t *testing.T) {
	setTestRoot(t, "vram-test-")
	srv := &Server{mux: http.NewServeMux()}

	body, _ := json.Marshal(map[string]interface{}{"flags": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/nonexistent-model/vram-test", bytes.NewReader(body))
	req.SetPathValue("name", "nonexistent-model")
	w := httptest.NewRecorder()
	srv.handleModelsVRAMTest(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleModelsVRAMTest_ConflictWhenRunning(t *testing.T) {
	setTestRoot(t, "vram-test-")
	srv := &Server{mux: http.NewServeMux()}

	// Seed a running test.
	startVRAMTest("busy-model", "vocal", VRAMConfig{})
	defer func() {
		activeVRAMTest.mu.Lock()
		activeVRAMTest.running = false
		activeVRAMTest.model = ""
		activeVRAMTest.stepType = ""
		activeVRAMTest.status = ""
		activeVRAMTest.progress = 0
		activeVRAMTest.peakMB = 0
		activeVRAMTest.n = 0
		activeVRAMTest.errMsg = ""
		activeVRAMTest.startedAt = time.Time{}
		activeVRAMTest.finished = time.Time{}
		activeVRAMTest.mu.Unlock()
	}()

	body, _ := json.Marshal(map[string]interface{}{"flags": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/some-model/vram-test", bytes.NewReader(body))
	req.SetPathValue("name", "some-model")
	w := httptest.NewRecorder()
	srv.handleModelsVRAMTest(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleModelsVRAMTest_AcceptsExistingModel(t *testing.T) {
	setTestRoot(t, "vram-test-")
	srv := &Server{mux: http.NewServeMux()}

	// Create a fake model directory so modelExistsOnDisk returns true.
	modelDir := filepath.Join(modelsBasePath(), "VR_Models", "test-roformer")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("cannot create fake model dir: %v", err)
	}
	manifest := `{"type":"roformer","stems":["vocals","instrumental"],"flags":[]}`
	if err := os.WriteFile(filepath.Join(modelDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("cannot write manifest: %v", err)
	}
	// searchModelOnDisk requires a checkpoint file to consider the dir a model.
	if err := os.WriteFile(filepath.Join(modelDir, "test-roformer.pth"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("cannot write fake checkpoint: %v", err)
	}

	// Ensure no leftover running test interferes.
	activeVRAMTest.mu.Lock()
	activeVRAMTest.running = false
	activeVRAMTest.model = ""
	activeVRAMTest.stepType = ""
	activeVRAMTest.status = ""
	activeVRAMTest.progress = 0
	activeVRAMTest.peakMB = 0
	activeVRAMTest.n = 0
	activeVRAMTest.errMsg = ""
	activeVRAMTest.startedAt = time.Time{}
	activeVRAMTest.finished = time.Time{}
	activeVRAMTest.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{"flags": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/test-roformer/vram-test", bytes.NewReader(body))
	req.SetPathValue("name", "test-roformer")
	w := httptest.NewRecorder()
	srv.handleModelsVRAMTest(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "started") {
		t.Fatalf("expected started response, got %s", w.Body.String())
	}
}

func TestHandleVRAMTestStatus(t *testing.T) {
	setTestRoot(t, "vram-test-")
	srv := &Server{mux: http.NewServeMux()}

	activeVRAMTest.mu.Lock()
	activeVRAMTest.running = false
	activeVRAMTest.model = ""
	activeVRAMTest.stepType = ""
	activeVRAMTest.status = ""
	activeVRAMTest.progress = 0
	activeVRAMTest.peakMB = 0
	activeVRAMTest.n = 0
	activeVRAMTest.errMsg = ""
	activeVRAMTest.startedAt = time.Time{}
	activeVRAMTest.finished = time.Time{}
	activeVRAMTest.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/models/vram-test/status", nil)
	w := httptest.NewRecorder()
	srv.handleVRAMTestStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var st VRAMTestStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if st.Running {
		t.Fatal("expected no running test")
	}
}
