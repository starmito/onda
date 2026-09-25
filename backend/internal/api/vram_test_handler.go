package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/starmito/onda/internal/cli"
)

const (
	vramTestOutputDirName = "test_output"
	vramTestStatusPath    = "/api/models/vram-test/status"
)

// vramTestJob tracks the state of an in-flight or just-finished VRAM test.
type vramTestJob struct {
	mu        sync.RWMutex
	model     string
	stepType  string
	running   bool
	status    string // running, success, oom, error
	progress  int
	peakMB    int
	n         int
	errMsg    string
	startedAt time.Time
	finished  time.Time
}

var activeVRAMTest vramTestJob

func startVRAMTest(model, stepType string, cfg VRAMConfig) bool {
	activeVRAMTest.mu.Lock()
	defer activeVRAMTest.mu.Unlock()
	if activeVRAMTest.running {
		return false
	}
	activeVRAMTest.model = model
	activeVRAMTest.stepType = stepType
	activeVRAMTest.running = true
	activeVRAMTest.status = "running"
	activeVRAMTest.progress = 0
	activeVRAMTest.peakMB = 0
	activeVRAMTest.n = 0
	activeVRAMTest.errMsg = ""
	activeVRAMTest.startedAt = time.Now()
	activeVRAMTest.finished = time.Time{}
	return true
}

func setVRAMTestProgress(p int) {
	activeVRAMTest.mu.Lock()
	defer activeVRAMTest.mu.Unlock()
	if !activeVRAMTest.running {
		return
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	activeVRAMTest.progress = p
}

func finishVRAMTest(status string, peakMB, n int, errMsg string) {
	activeVRAMTest.mu.Lock()
	defer activeVRAMTest.mu.Unlock()
	activeVRAMTest.running = false
	activeVRAMTest.status = status
	activeVRAMTest.peakMB = peakMB
	activeVRAMTest.n = n
	activeVRAMTest.errMsg = errMsg
	activeVRAMTest.progress = 100
	activeVRAMTest.finished = time.Now()
}

func vramTestIsRunning() bool {
	activeVRAMTest.mu.RLock()
	defer activeVRAMTest.mu.RUnlock()
	return activeVRAMTest.running
}

// VRAMTestStatusResponse is the JSON returned by GET /api/models/vram-test/status.
type VRAMTestStatusResponse struct {
	Running  bool   `json:"running"`
	Model    string `json:"model,omitempty"`
	StepType string `json:"step_type,omitempty"`
	Progress int    `json:"progress"`
	Status   string `json:"status"`
	PeakMB   int    `json:"peak_mb"`
	N        int    `json:"n"`
	Error    string `json:"error,omitempty"`
}

func getVRAMTestStatus() VRAMTestStatusResponse {
	activeVRAMTest.mu.RLock()
	defer activeVRAMTest.mu.RUnlock()
	return VRAMTestStatusResponse{
		Running:  activeVRAMTest.running,
		Model:    activeVRAMTest.model,
		StepType: activeVRAMTest.stepType,
		Progress: activeVRAMTest.progress,
		Status:   activeVRAMTest.status,
		PeakMB:   activeVRAMTest.peakMB,
		N:        activeVRAMTest.n,
		Error:    activeVRAMTest.errMsg,
	}
}

// handleVRAMTestStatus returns the current state of the VRAM test button.
func (s *Server) handleVRAMTestStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, getVRAMTestStatus())
}

// handleModelsVRAMTest starts a real pipeline run against the built-in test clip
// with the supplied flags, measures peak VRAM and cleans everything up.
func (s *Server) handleModelsVRAMTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	modelName := r.PathValue("name")
	if modelName == "" {
		http.Error(w, "model name required", http.StatusBadRequest)
		return
	}
	var req struct {
		Flags map[string]interface{} `json:"flags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	// Reject the request if another test or a real job is already running.
	// This check has priority over the model-existence check so the UI shows
	// the correct "busy" state even when the requested model does not exist.
	if !canStartVRAMTest(s) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a test or job is already running"})
		return
	}
	if !modelExistsOnDisk(modelName) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("model %q not found", modelName)})
		return
	}
	clipPath, err := ensureVRAMTestClip()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create test clip: %v", err)})
		return
	}
	outputDir, err := createVRAMTestOutputDir(modelName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create test output dir: %v", err)})
		return
	}

	cfg := flagValuesToModelConfig(req.Flags)
	cfg.Device = "cuda" // VRAM test always runs on GPU.

	step := buildVRAMTestStep(modelName)
	stepType := step.Type
	if isDemucsModel(modelName) {
		stepType = "demucs"
	}

	args, env, err := buildStepPipelineArgsFromConfig(step, clipPath, outputDir, "cuda", cfg)
	if err != nil {
		_ = os.RemoveAll(outputDir)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to build pipeline args: %v", err)})
		return
	}
	args = append(args, clipPath)

	vramCfg := vramConfigFromModelConfig(modelName, cfg)
	vramCfg.Duration = vramTestClipDurationSec

	if !startVRAMTest(modelName, stepType, vramCfg) {
		_ = os.RemoveAll(outputDir)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a test or job is already running"})
		return
	}

	go s.runVRAMTest(modelName, "cuda", args, env, vramCfg, outputDir)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status": "started",
		"model":  modelName,
	})
}

// runVRAMTest executes the pipeline against the test clip, samples VRAM, reports
// the result and removes all generated files.
func (s *Server) runVRAMTest(modelName, device string, args []string, env []string, cfg VRAMConfig, outputDir string) {
	defer cleanupVRAMTestOutput(outputDir)

	script := resolvePipelineScript()
	pipelineArgs := append([]string{script}, args...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", pipelineArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = buildPipelineEnv(env)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	statusPath := filepath.Join(outputDir, "pipeline_status.json")
	stopProgress := make(chan struct{})
	var progressWG sync.WaitGroup
	progressWG.Add(1)
	go func() {
		defer progressWG.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				setVRAMTestProgress(readVRAMTestProgress(statusPath))
			}
		}
	}()

	startErr := cmd.Start()
	err := runPipelineWithVRAMSampler(ctx, cmd, startErr, modelName, device, cfg)

	close(stopProgress)
	progressWG.Wait()
	setVRAMTestProgress(100)

	logPipelineOutput(out.String())

	if err != nil {
		status := "error"
		errMsg := "el test falló"
		if isOOMPipelineError(err, out.String()) {
			status = "oom"
			errMsg = "no cabe: sin memoria"
		}
		finishVRAMTest(status, 0, 0, errMsg)
		return
	}

	rec, ok, _ := findMeasuredVRAMPeakInStore(modelName, device, cfg)
	if ok {
		finishVRAMTest("success", measuredVRAMValue(rec), rec.N, "")
	} else {
		finishVRAMTest("success", 0, 0, "")
	}
}

// buildVRAMTestStep creates a single pipeline step that saves all stems to the
// test output directory so the pipeline runs a full forward pass.
func buildVRAMTestStep(modelName string) cli.PipelineStep {
	step := cli.PipelineStep{
		ID:      "vram-test",
		Type:    "vocal",
		Model:   modelName,
		Enabled: true,
		Stems:   map[string]cli.StemRoute{},
	}
	if isDemucsModel(modelName) {
		step.Type = "demucs"
		stems := modelStemsFromManifest(modelName).Stems
		if len(stems) == 0 {
			stems = []string{"drums", "bass", "other", "vocals"}
		}
		for _, stem := range stems {
			step.Stems[stem] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
		}
		return step
	}
	step.Stems["vocals"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
	step.Stems["instrumental"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
	return step
}

// vramConfigFromModelConfig builds the sampler config from UI flag values.
func vramConfigFromModelConfig(modelName string, cfg ModelConfigResponse) VRAMConfig {
	vramCfg := VRAMConfig{
		SegmentSize: cfg.SegmentSize,
		ChunkSize:   cfg.ChunkSize,
		BatchSize:   cfg.BatchSize,
		NumOverlap:  cfg.NumOverlap,
		Shifts:      cfg.Shifts,
		Jobs:        cfg.Jobs,
	}
	if isDemucsModel(modelName) {
		seg := int(roundDemucsSegment(cfg.Segment))
		if seg <= 0 {
			seg = 7
		}
		vramCfg.DemucsSegment = seg
	}
	return vramCfg
}

// createVRAMTestOutputDir returns a unique directory under the data root.
func createVRAMTestOutputDir(modelName string) (string, error) {
	dir := filepath.Join(dataRoot(), vramTestOutputDirName, fmt.Sprintf("onda-vram-test-%s-%d", sanitizeModelName(modelName), time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func cleanupVRAMTestOutput(dir string) {
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		Log("backend", "warn", fmt.Sprintf("failed to clean VRAM test output %s: %v", dir, err))
	}
}

func sanitizeModelName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func modelExistsOnDisk(name string) bool {
	if _, _, found := searchModelOnDisk(name); found {
		return true
	}
	if strings.EqualFold(name, "htdemucs_ft") {
		return true
	}
	return false
}

func canStartVRAMTest(s *Server) bool {
	if vramTestIsRunning() {
		return false
	}
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()
	for _, j := range s.jobs {
		if j.Status == "processing" {
			return false
		}
	}
	return true
}

// readVRAMTestProgress reads the pipeline_status.json produced by pipeline.sh.
func readVRAMTestProgress(statusPath string) int {
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return 0
	}
	var st pipelineStatusJSON
	if err := json.Unmarshal(data, &st); err != nil {
		return 0
	}
	progress := int(math.Round(st.Progress * 100))
	if progress == 0 && st.OverallProgress > 0 {
		progress = int(math.Round(st.OverallProgress))
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	return progress
}

// isOOMPipelineError detects whether a failed pipeline run was killed by the OOM
// killer or explicitly ran out of GPU memory.
func isOOMPipelineError(err error, output string) bool {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "out of memory") ||
		strings.Contains(lower, "cuda out of memory") ||
		strings.Contains(lower, "runtimeerror: out of memory") ||
		strings.Contains(lower, "oom") {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() && (status.Signal() == syscall.SIGKILL || status.Signal() == syscall.SIGSEGV) {
				return true
			}
		}
	}
	return false
}

