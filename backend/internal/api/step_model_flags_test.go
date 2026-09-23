package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestRunSinglePipeline_LogsEffectiveModelAndFlags(t *testing.T) {
	root := setupQueueTestRoot(t)
	clearLogBuffer(t)
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

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--vocal-model", "BS_Roformer_Viperx", "--output", filepath.Join(root, "output", "song"), "/app/input/song.wav"},
		Config: SeparateRequest{
			VocalModel: "BS_Roformer_Viperx",
			Device:     "cuda",
		},
	}

	s.runSinglePipeline(job, s.jobs["song"])

	s.jobsMu.RLock()
	state := s.jobs["song"]
	s.jobsMu.RUnlock()

	if state.Status != "done" {
		t.Fatalf("expected done status, got %q", state.Status)
	}
	if state.CurrentModel != "" || state.CurrentFlags != "" {
		t.Errorf("current_model/current_flags should be cleared after completion, got model=%q flags=%q", state.CurrentModel, state.CurrentFlags)
	}

	if !containsLog("pipeline", "info", "Step 1/1 start") {
		t.Error("expected step start log")
	}
	if !containsLog("pipeline", "info", "model=BS_Roformer_Viperx") {
		t.Error("expected step start log to mention the effective model")
	}
	if !containsLog("pipeline", "info", "--vocal-model") {
		t.Error("expected step start log to mention the effective flags")
	}
}

func TestRunSinglePipeline_SetsCurrentModelAndFlagsInState(t *testing.T) {
	root := setupQueueTestRoot(t)
	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("drums.wav")
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	state := &JobState{Song: "song", Status: "waiting"}
	s := &Server{jobs: map[string]*JobState{"song": state}}

	job := JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--stem-model", "htdemucs_ft", "--shifts", "2", "--demucs-segment", "7", "--jobs", "4", "--output", filepath.Join(root, "output", "song"), "/app/input/song.wav"},
		Config: SeparateRequest{
			StemModel: "htdemucs_ft",
			Shifts:    2,
			Jobs:      4,
			Device:    "cuda",
		},
	}

	s.runSinglePipeline(job, state)

	if state.Status != "done" {
		t.Fatalf("expected done status, got %q", state.Status)
	}
	if state.CurrentModel != "" || state.CurrentFlags != "" {
		t.Errorf("current_model/current_flags should be cleared after completion, got model=%q flags=%q", state.CurrentModel, state.CurrentFlags)
	}
}

func TestBuildStepPipelineArgs_NonDemucsMultiStemUsesVocalKeep(t *testing.T) {
	root := setTestRoot(t, "multi-stem-")

	modelDir := filepath.Join(root, "models", "VR_Models", "SW6Test")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "SW6Test.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write ckpt: %v", err)
	}
	yaml := `inference:
  dim_t: 1101
  num_overlap: 4
  batch_size: 1
  chunk_size: 0
`
	if err := os.WriteFile(filepath.Join(modelDir, "SW6Test.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "demucs",
		Type:    "demucs",
		Model:   "SW6Test",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"bass":     {Action: cli.StemSave, Target: "result"},
			"drums":    {Action: cli.StemSave, Target: "result"},
			"other":    {Action: cli.StemSave, Target: "result"},
			"vocals":   {Action: cli.StemSave, Target: "result"},
			"guitar":   {Action: cli.StemSave, Target: "result"},
			"piano":    {Action: cli.StemSave, Target: "result"},
		},
	}

	args, env, err := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")
	if err != nil {
		t.Fatalf("buildStepPipelineArgs failed: %v", err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--vocal-model") {
		t.Errorf("expected --vocal-model for non-Demucs multi-stem model, got: %s", joined)
	}
	if !strings.Contains(joined, "--vocal-keep drums,bass,other,vocals,guitar,piano") {
		t.Errorf("expected explicit --vocal-keep list when stems are selected, got: %s", joined)
	}
	if strings.Contains(joined, "--stem-model") {
		t.Errorf("did not expect --stem-model for non-Demucs multi-stem model, got: %s", joined)
	}
	_ = env
}

func TestBuildStepPipelineArgs_NonDemucsMultiStemKeepsSubset(t *testing.T) {
	root := setTestRoot(t, "multi-stem-subset-")

	modelDir := filepath.Join(root, "models", "VR_Models", "SW6Test")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "SW6Test.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write ckpt: %v", err)
	}
	yaml := `inference:
  dim_t: 1101
  num_overlap: 4
  batch_size: 1
  chunk_size: 0
`
	if err := os.WriteFile(filepath.Join(modelDir, "SW6Test.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "demucs",
		Type:    "demucs",
		Model:   "SW6Test",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"bass":     {Action: cli.StemSave, Target: "result"},
			"drums":    {Action: cli.StemDiscard},
			"other":    {Action: cli.StemSave, Target: "result"},
			"vocals":   {Action: cli.StemDiscard},
			"guitar":   {Action: cli.StemSave, Target: "result"},
			"piano":    {Action: cli.StemDiscard},
		},
	}

	args, _, err := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")
	if err != nil {
		t.Fatalf("buildStepPipelineArgs failed: %v", err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--vocal-keep bass,other,guitar") {
		t.Errorf("expected --vocal-keep bass,other,guitar, got: %s", joined)
	}
}

func TestRunMultiStepPipeline_LogsEffectiveModelAndFlags(t *testing.T) {
	root := setupQueueTestRoot(t)
	clearLogBuffer(t)
	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("vocals.wav", "drums.wav")
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	state := &JobState{Song: "song", Status: "waiting"}
	s := &Server{
		jobs: map[string]*JobState{"song": state},
	}

	steps := []cli.PipelineStep{
		{ID: "vocal", Type: "vocal", Model: "BS_Roformer_Viperx", Enabled: true},
		{ID: "demucs", Type: "demucs", Model: "htdemucs_ft", Enabled: true},
	}

	job := JobRequest{
		Song:   "song",
		Args:   []string{fakePipeline},
		Config: SeparateRequest{Input: "/app/input/song.wav", Device: "cpu"},
		Steps:  steps,
	}

	s.runMultiStepPipeline(job, steps, state)

	if state.Status != "done" {
		t.Fatalf("expected done status, got %q", state.Status)
	}
	if state.CurrentModel != "" || state.CurrentFlags != "" {
		t.Errorf("current_model/current_flags should be cleared after completion, got model=%q flags=%q", state.CurrentModel, state.CurrentFlags)
	}

	if !containsLog("pipeline", "info", "Step 1/2 start") {
		t.Error("expected step 1 start log")
	}
	if !containsLog("pipeline", "info", "Step 2/2 start") {
		t.Error("expected step 2 start log")
	}
	if !containsLog("pipeline", "info", "model=BS_Roformer_Viperx") {
		t.Error("expected vocal model in step start log")
	}
	if !containsLog("pipeline", "info", "model=htdemucs_ft") {
		t.Error("expected demucs model in step start log")
	}
	if !containsLog("pipeline", "info", "device=cpu") && !containsLog("pipeline", "info", "--device cpu") {
		t.Error("expected non-default device in step start log")
	}
}

func TestHandleProcessStatus_IncludesCurrentModelAndFlags(t *testing.T) {
	setupQueueTestRoot(t)
	s := &Server{
		mux:  http.NewServeMux(),
		jobs: make(map[string]*JobState),
	}
	s.mux.HandleFunc("GET /api/processes/status", s.handleProcessStatus)
	s.jobs["running_song"] = &JobState{
		Song:         "running_song",
		Status:       "processing",
		CurrentStep:  2,
		TotalSteps:   4,
		StepName:     "Demucs",
		Device:       "cuda",
		CurrentModel: "htdemucs_ft",
		CurrentFlags: "--stem-model htdemucs_ft --shifts 2",
		Index:        0,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/processes/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		QueueJobs []JobState `json:"queue_jobs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.QueueJobs) != 1 {
		t.Fatalf("expected 1 queue job, got %d", len(resp.QueueJobs))
	}
	j := resp.QueueJobs[0]
	if j.CurrentModel != "htdemucs_ft" {
		t.Errorf("current_model = %q, want htdemucs_ft", j.CurrentModel)
	}
	if !strings.Contains(j.CurrentFlags, "--stem-model") {
		t.Errorf("current_flags should contain --stem-model, got %q", j.CurrentFlags)
	}
}

func TestCompactFlags_DeduplicatesModelFlags(t *testing.T) {
	// Vocal/Roformer: default model name followed by the resolved directory path.
	vocalArgs := []string{
		"--vocal-model", "BS_Roformer_Viperx",
		"--vocal-model", "/data/models/BS_Roformer_Viperx",
		"--device", "cuda",
		"--output", "/data/output/song",
		"/app/input/song.wav",
	}
	vocalFlags := compactFlags(vocalArgs)
	t.Logf("vocal compacted flags: %s", vocalFlags)
	want := "--vocal-model BS_Roformer_Viperx --device cuda"
	if vocalFlags != want {
		t.Errorf("vocal compactFlags = %q, want %q", vocalFlags, want)
	}

	// Demucs: single model flag plus other effective flags.
	demucsArgs := []string{
		"--stem-model", "htdemucs_ft",
		"--shifts", "10",
		"--demucs-segment", "7",
		"--jobs", "8",
		"--output", "/data/output/song",
		"/app/input/song.wav",
	}
	demucsFlags := compactFlags(demucsArgs)
	t.Logf("demucs compacted flags: %s", demucsFlags)
	want = "--stem-model htdemucs_ft --shifts 10 --demucs-segment 7 --jobs 8"
	if demucsFlags != want {
		t.Errorf("demucs compactFlags = %q, want %q", demucsFlags, want)
	}
}

func TestRunSinglePipeline_PresetResolvesRealModel(t *testing.T) {
	root := setupQueueTestRoot(t)
	clearLogBuffer(t)
	mockResourceProviders(t)

	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := fakePipelineScript("instrumental.wav")
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	req := SeparateRequest{
		Preset: "Eliminador de Voz",
		Input:  "/app/input/song.wav",
		Device: "cpu",
	}
	song, args, steps, _, _ := buildPipelineArgs(&req)

	state := &JobState{Song: song, Status: "waiting"}
	s := &Server{jobs: map[string]*JobState{song: state}}

	job := JobRequest{
		Song:   song,
		Args:   append([]string{fakePipeline}, args...),
		Config: req,
		Steps:  steps,
	}

	s.runSinglePipeline(job, state)

	if state.Status != "done" {
		t.Fatalf("expected done status, got %q", state.Status)
	}
	if state.CurrentModel != "" || state.CurrentFlags != "" {
		t.Errorf("current_model/current_flags should be cleared after completion, got model=%q flags=%q", state.CurrentModel, state.CurrentFlags)
	}

	if !containsLog("pipeline", "info", "model=BS_Roformer_Viperx") {
		t.Error("expected step start log to show the real model (BS_Roformer_Viperx)")
	}
	if containsLog("pipeline", "info", "model=Eliminador de Voz") {
		t.Error("step start log should not show the preset name as the model")
	}
}
