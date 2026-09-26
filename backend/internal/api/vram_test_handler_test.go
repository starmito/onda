package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/starmito/onda/internal/cli"
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

func TestHandleModelsVRAMTest_UnknownFlag(t *testing.T) {
	setTestRoot(t, "vram-test-")
	srv := &Server{mux: http.NewServeMux()}

	modelDir := filepath.Join(modelsBasePath(), "VR_Models", "test-roformer")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("cannot create fake model dir: %v", err)
	}
	writeTestModelManifest(t, modelDir, "segment_size", "num_overlap", "chunk_size", "batch_size")
	if err := os.WriteFile(filepath.Join(modelDir, "test-roformer.pth"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("cannot write fake checkpoint: %v", err)
	}

	activeVRAMTest.mu.Lock()
	activeVRAMTest.running = false
	activeVRAMTest.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{"flags": map[string]interface{}{
		"batch_size":     1,
		"chunk_size":     0,
		"segment_size":   1101,
		"num_overlap":    3,
		"flag_inventado": 5,
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/test-roformer/vram-test", bytes.NewReader(body))
	req.SetPathValue("name", "test-roformer")
	w := httptest.NewRecorder()
	srv.handleModelsVRAMTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "flag_inventado") {
		t.Fatalf("expected error to name the unknown flag, got %s", w.Body.String())
	}
	if activeVRAMTest.running {
		t.Fatal("test should not have been started")
	}
}

// writeTestModelManifest writes a minimal model.manifest.json that declares
// the supplied flag names as editable so the VRAM test accepts them.
func writeTestModelManifest(t *testing.T, modelDir string, flagNames ...string) {
	t.Helper()
	flags := make(map[string]interface{}, len(flagNames))
	for _, name := range flagNames {
		flags[name] = map[string]interface{}{"editable": true}
	}
	manifest := map[string]interface{}{
		"type":  "test",
		"stems": map[string]interface{}{"stems": []string{"vocals", "instrumental"}},
		"flags": flags,
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modelDir, "model.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("cannot write manifest: %v", err)
	}
}

// TestBuildStepPipelineArgsFromConfig_VRAMTestFlagsReachPipeline verifies that
// the VRAM test passes its temporary flag overrides to pipeline.sh through the
// same argument-building path used by normal jobs. With two different segment
// sizes the generated environment (which pipeline.sh reads) must differ.
func TestBuildStepPipelineArgsFromConfig_VRAMTestFlagsReachPipeline(t *testing.T) {
	root := setTestRoot(t, "vram-flags-")
	modelDir := filepath.Join(modelsBasePath(), "VR_Models", "TestRoformerVRAM")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("cannot create fake model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformerVRAM.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("cannot write fake checkpoint: %v", err)
	}
	// A model-shipped YAML with default values different from the overrides,
	// so the test proves the override reaches the pipeline instead of the YAML.
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformerVRAM.yaml"), []byte("model:\n  num_bands: 4\naudio:\n  hop_length: 512\ninference:\n  dim_t: 1101\n  num_overlap: 4\n  batch_size: 1\n  chunk_size: 600\ntraining:\n  instruments: [vocals, other]\n"), 0o644); err != nil {
		t.Fatalf("cannot write fake yaml: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "vram-test",
		Type:    "vocal",
		Model:   "TestRoformerVRAM",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"vocals":       {Action: cli.StemSave, Target: "result"},
			"instrumental": {Action: cli.StemSave, Target: "result"},
		},
	}

	cfg2048 := ModelConfigResponse{
		SegmentSize: 2048,
		BatchSize:   2,
		NumOverlap:  6,
		ChunkSize:   0,
		Overlap:     1.0 / 6.0,
	}
	args2048, env2048, err := buildStepPipelineArgsFromConfig(step, filepath.Join(root, "input.wav"), filepath.Join(root, "out"), "cuda", cfg2048)
	if err != nil {
		t.Fatalf("buildStepPipelineArgsFromConfig(2048) failed: %v", err)
	}

	cfg512 := cfg2048
	cfg512.SegmentSize = 512
	cfg512.Overlap = 1.0 / 6.0
	args512, env512, err := buildStepPipelineArgsFromConfig(step, filepath.Join(root, "input.wav"), filepath.Join(root, "out"), "cuda", cfg512)
	if err != nil {
		t.Fatalf("buildStepPipelineArgsFromConfig(512) failed: %v", err)
	}

	mustContainEnv := func(t *testing.T, env []string, want string) {
		t.Helper()
		for _, e := range env {
			if e == want {
				return
			}
		}
		t.Errorf("expected env to contain %q, got %v", want, env)
	}

	mustContainEnv(t, env2048, "VOCAL_DIM_T=2048")
	mustContainEnv(t, env2048, "VOCAL_BATCH_SIZE=2")
	mustContainEnv(t, env2048, "VOCAL_NUM_OVERLAP=6")
	mustContainEnv(t, env2048, "VOCAL_CHUNK_SIZE=0")

	mustContainEnv(t, env512, "VOCAL_DIM_T=512")
	mustContainEnv(t, env512, "VOCAL_BATCH_SIZE=2")
	mustContainEnv(t, env512, "VOCAL_NUM_OVERLAP=6")
	mustContainEnv(t, env512, "VOCAL_CHUNK_SIZE=0")

	// The two configurations must produce different argument/env output so the
	// pipeline receives a different segment size.
	if reflect.DeepEqual(args2048, args512) && reflect.DeepEqual(env2048, env512) {
		t.Errorf("expected args/env to differ between segment_size 2048 and 512")
	}

	t.Logf("segment_size=2048 args: %v", args2048)
	t.Logf("segment_size=2048 env:  %v", env2048)
	t.Logf("segment_size=512 args:  %v", args512)
	t.Logf("segment_size=512 env:   %v", env512)
}

func TestHandleModelsVRAMTest_AcceptsValidFlagsPerFamily(t *testing.T) {
	setTestRoot(t, "vram-test-")

	cases := []struct {
		name  string
		flags map[string]interface{}
	}{
		{
			name: "roformer",
			flags: map[string]interface{}{
				"batch_size":   1,
				"chunk_size":   0,
				"segment_size": 1101,
				"num_overlap":  3,
			},
		},
		{
			name: "mdx23c",
			flags: map[string]interface{}{
				"batch_size":   1,
				"segment_size": 256,
				"num_overlap":  2,
			},
		},
		{
			name: "mdx_net",
			flags: map[string]interface{}{
				"batch_size":   1,
				"segment_size": 256,
				"num_overlap":  2,
			},
		},
		{
			name: "demucs",
			flags: map[string]interface{}{
				"shifts":  10,
				"segment": 7,
				"jobs":    8,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{mux: http.NewServeMux()}
			modelName := "test-" + tc.name
			modelDir := filepath.Join(modelsBasePath(), "VR_Models", modelName)
			if err := os.MkdirAll(modelDir, 0o755); err != nil {
				t.Fatalf("cannot create fake model dir: %v", err)
			}
			flagNames := make([]string, 0, len(tc.flags))
			for k := range tc.flags {
				flagNames = append(flagNames, k)
			}
			writeTestModelManifest(t, modelDir, flagNames...)
			if err := os.WriteFile(filepath.Join(modelDir, modelName+".pth"), []byte("fake"), 0o644); err != nil {
				t.Fatalf("cannot write fake checkpoint: %v", err)
			}

			activeVRAMTest.mu.Lock()
			activeVRAMTest.running = false
			activeVRAMTest.mu.Unlock()

			body, _ := json.Marshal(map[string]interface{}{"flags": tc.flags})
			req := httptest.NewRequest(http.MethodPost, "/api/models/"+modelName+"/vram-test", bytes.NewReader(body))
			req.SetPathValue("name", modelName)
			w := httptest.NewRecorder()
			srv.handleModelsVRAMTest(w, req)

			if w.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
