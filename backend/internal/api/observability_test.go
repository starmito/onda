package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"os/exec"
)

// clearLogBuffer resets the in-memory log buffer so observability tests do not
// see entries from previous tests.
func clearLogBuffer(t *testing.T) {
	t.Helper()
	logBufferMu.Lock()
	logBuffer = nil
	logBufferMu.Unlock()
}

// containsLog reports whether the in-memory log buffer contains an entry whose
// service/level/message all match the given substrings.
func containsLog(service, level, messageSub string) bool {
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()
	for _, e := range logBuffer {
		if e.Service == service && e.Level == level &&
			(messageSub == "" || strings.Contains(e.Message, messageSub)) {
			return true
		}
	}
	return false
}

func TestTailOutput(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i+1)
	}
	out := strings.Join(lines, "\n")

	tail := tailOutput(out, 40, 8192)
	if strings.Contains(tail, "line 001") {
		t.Error("tail should not contain the very first line")
	}
	if !strings.Contains(tail, "line 050") {
		t.Error("tail should contain the last line")
	}
	if !strings.Contains(tail, "line 011") {
		t.Error("tail should contain line 011 (the 40th from the end)")
	}
	if strings.Contains(tail, "line 010") {
		t.Error("tail should not contain line 010 (older than the 40-line window)")
	}

	// Byte limit: keep only the last ~8 KB.
	huge := strings.Repeat("a", 5000) + "\n" + strings.Repeat("b", 5000) + "\n" + strings.Repeat("c", 5000)
	trimmed := tailOutput(huge, 40, 8192)
	if strings.Contains(trimmed, "aaaa") {
		t.Error("byte-limited tail should drop the oldest block")
	}
	if !strings.Contains(trimmed, "cccc") {
		t.Error("byte-limited tail should keep the newest block")
	}
}

func TestExtractExitInfo(t *testing.T) {
	cmd := exec.Command("bash", "-c", "exit 7")
	err := cmd.Run()
	code, signal, ok := extractExitInfo(err)
	if !ok {
		t.Fatal("expected ok=true for exit 7")
	}
	if code != 7 {
		t.Errorf("expected exit code 7, got %d", code)
	}
	if signal != "" {
		t.Errorf("expected no signal, got %q", signal)
	}

	cmd = exec.Command("bash", "-c", "kill -9 $$")
	err = cmd.Run()
	code, signal, ok = extractExitInfo(err)
	if !ok {
		t.Fatal("expected ok=true for SIGKILL")
	}
	if signal != "killed" {
		t.Errorf("expected signal killed, got %q", signal)
	}
	if code != 128+int(syscall.SIGKILL) {
		t.Errorf("expected exit code %d, got %d", 128+int(syscall.SIGKILL), code)
	}
}

func TestRunSinglePipeline_ErrorIncludesOutputTail(t *testing.T) {
	root := setupQueueTestRoot(t)
	clearLogBuffer(t)

	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := `#!/bin/bash
for i in $(seq 1 50); do
  echo "output line $i"
done
exit 1
`
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs: map[string]*JobState{
			"song": {Song: "song", Status: "waiting", StartedAt: time.Now()},
		},
	}

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
	}

	s.runSinglePipeline(job, s.jobs["song"])

	s.jobsMu.RLock()
	state := s.jobs["song"]
	s.jobsMu.RUnlock()

	if state.Status != "error" {
		t.Errorf("expected error status, got %q", state.Status)
	}
	if strings.Contains(state.Error, "output line 001") {
		t.Error("error message should not contain the first output line")
	}
	if !strings.Contains(state.Error, "output line 50") {
		t.Errorf("error message should contain the last output line; got %q", state.Error)
	}
	if !strings.Contains(state.Error, "output line 11") {
		t.Errorf("error message should contain line 11 (40th from the end); got %q", state.Error)
	}
	if len(state.Error) > 9000 {
		t.Errorf("error message exceeded 8 KB tail limit: %d bytes", len(state.Error))
	}

	if !containsLog("pipeline", "error", "exit=1") {
		t.Error("expected pipeline error log with exit code")
	}
	if !containsLog("pipeline", "error", "output line 50") {
		t.Error("expected pipeline error log with output tail")
	}
}

func TestRunSinglePipeline_SignalWritesFailedStatus(t *testing.T) {
	root := setupQueueTestRoot(t)
	clearLogBuffer(t)

	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := "#!/bin/bash\necho 'about to die'\nkill -9 $$\n"
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs: map[string]*JobState{
			"song": {Song: "song", Status: "waiting", StartedAt: time.Now()},
		},
	}

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--viperx", "/app/input/song.wav", "--output", filepath.Join(root, "output", "song")},
	}

	s.runSinglePipeline(job, s.jobs["song"])

	statusPath := filepath.Join(root, "output", "pipeline_status.json")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("failed to read pipeline_status.json: %v", err)
	}
	var st struct {
		Status   string `json:"status"`
		Step     string `json:"step"`
		ExitCode int    `json:"exit_code"`
		Signal   string `json:"signal"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("failed to decode pipeline_status.json: %v", err)
	}
	if st.Status != "failed" {
		t.Errorf("expected status failed, got %q", st.Status)
	}
	if st.Signal != "killed" {
		t.Errorf("expected signal killed, got %q", st.Signal)
	}
	if st.ExitCode != 128+int(syscall.SIGKILL) {
		t.Errorf("expected exit code %d, got %d", 128+int(syscall.SIGKILL), st.ExitCode)
	}
}

func TestHandleSeparate_LogsJobConfig(t *testing.T) {
	setupQueueTestRoot(t)
	clearLogBuffer(t)

	s := newQueueTestServer(t)

	body := `{"input":"/app/input/song.wav","viperx":true,"demucs":true,"vocal_model":"BS_Roformer_Viperx","stem_model":"htdemucs_ft","device":"cuda","shifts":2,"jobs":4,"demucs_segment":7}`
	req := httptest.NewRequest(http.MethodPost, "/api/separate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	if !containsLog("backend", "success", "Job queued: song") {
		t.Error("expected backend success log for queued job")
	}
	if !containsLog("backend", "success", "preset=(ninguno)") {
		t.Error("expected queued log to mention preset")
	}
	if !containsLog("backend", "success", "modelos y flags se resuelven al arrancar cada paso") {
		t.Error("expected queued log to clarify that models/flags are resolved per step")
	}
}
