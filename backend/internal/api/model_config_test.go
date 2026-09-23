package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
	"gopkg.in/yaml.v3"
)

func TestWriteModelConfigToYaml_CreatesFallbackForHtdemucsFt(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     5,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	expectedPath := filepath.Join(root, "config", "model_configs", "htdemucs_ft.yaml")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected fallback YAML at %s: %v", expectedPath, err)
	}

	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read created YAML: %v", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("created YAML is invalid: %v", err)
	}

	rootNode := doc.Content[0]
	infNode := findYamlChildNode(rootNode, "inference")
	if infNode == nil {
		t.Fatal("missing inference section in created YAML")
	}
	if n := findYamlChildNode(infNode, "dim_t"); n == nil || n.Value != "256" {
		t.Errorf("expected dim_t=256, got %v", n)
	}

	demNode := findYamlChildNode(rootNode, "demucs")
	if demNode == nil {
		t.Fatal("missing demucs section in created YAML")
	}
	for _, kv := range []struct{ key, want string }{
		{"shifts", "4"},
		{"segment", "5"},
		{"jobs", "2"},
	} {
		n := findYamlChildNode(demNode, kv.key)
		if n == nil {
			t.Errorf("missing demucs.%s", kv.key)
			continue
		}
		if n.Value != kv.want {
			t.Errorf("demucs.%s = %q, want %q", kv.key, n.Value, kv.want)
		}
	}
}

func TestWriteModelConfigToYaml_PreservesValidSegment(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     5.2,
		Jobs:        0,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	expectedPath := filepath.Join(root, "config", "model_configs", "htdemucs_ft.yaml")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read created YAML: %v", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("created YAML is invalid: %v", err)
	}

	demNode := findYamlChildNode(doc.Content[0], "demucs")
	if demNode == nil {
		t.Fatal("missing demucs section in created YAML")
	}
	n := findYamlChildNode(demNode, "segment")
	if n == nil {
		t.Fatal("missing demucs.segment")
	}
	if n.Value != "5" {
		t.Errorf("demucs.segment = %q, want 5", n.Value)
	}
	if n.Tag != "!!int" {
		t.Errorf("demucs.segment tag = %q, want !!int", n.Tag)
	}

	// Read back must return the clamped integer value.
	read := readModelConfigFromYaml("htdemucs_ft")
	if read.Segment != 5 {
		t.Errorf("read segment = %v, want 5", read.Segment)
	}
}

func TestBuildPipelineArgs_DemucsUsesSavedConfig(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     5,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:     "/app/input/song.wav",
		Demucs:    true,
		StemModel: "htdemucs_ft",
	}
	_, args, _, _, _ := buildPipelineArgs(req)

	if !contains(args, "--stem-model") {
		t.Fatal("expected --stem-model flag")
	}
	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected --demucs-segment 5, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "2" {
		t.Errorf("expected --jobs 2, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsDefaultModelUsesSavedConfig(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     5,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	// Legacy request with demucs and no explicit stem model should still pick
	// up the saved htdemucs_ft config.
	req := &SeparateRequest{
		Input:  "/app/input/song.wav",
		Demucs: true,
	}
	_, args, _, _, _ := buildPipelineArgs(req)

	if !contains(args, "--stem-model") {
		t.Fatal("expected --stem-model flag")
	}
	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected --demucs-segment 5, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "2" {
		t.Errorf("expected --jobs 2, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsRequestOverridesIgnored(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     5,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	// Simulate a request body that still carries the legacy override fields.
	// SeparateRequest no longer has those fields, so the decoder ignores them
	// and the pipeline must use the saved model config.
	body := `{"input":"/app/input/song.wav","demucs":true,"stem_model":"htdemucs_ft","shifts":2,"demucs_segment":8,"jobs":1}`
	var req SeparateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}

	_, args, _, _, _ := buildPipelineArgs(&req)

	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected saved --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected saved --demucs-segment 5, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "2" {
		t.Errorf("expected saved --jobs 2, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsDecimalSegmentRounded(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     5.2,
		Jobs:        0,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:     "/app/input/song.wav",
		Demucs:    true,
		StemModel: "htdemucs_ft",
	}
	_, args, _, _, _ := buildPipelineArgs(req)

	// Decimal segments are rounded to the nearest whole second.
	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected --demucs-segment 5, got %q", got)
	}
}

func TestBuildStepPipelineArgs_DemucsUsesSavedConfig(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      6,
		Segment:     5,
		Jobs:        3,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "demucs",
		Type:    "demucs",
		Model:   "htdemucs_ft",
		Enabled: true,
	}
	args, _, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")

	if got := argValue(args, "--shifts"); got != "6" {
		t.Errorf("expected --shifts 6, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected --demucs-segment 5, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "3" {
		t.Errorf("expected --jobs 3, got %q", got)
	}
}

// argValue returns the value immediately following flag, or "" if missing.
func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestHandleModelsConfig_DecimalSegment(t *testing.T) {
	setTestRoot(t, "model-config-")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	body := []byte(`{"flags":{"segment":5.8}}`)
	resp, err := http.Post(srv.URL+"/api/models/htdemucs_ft/config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}

	getResp, err := http.Get(srv.URL + "/api/models/htdemucs_ft/config")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}

	var got ModelFlagsResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}
	// Decimal segment is rounded to the nearest integer.
	seg := flagValue(got.Flags, "segment")
	if toInt(seg) != 6 {
		t.Errorf("GET segment = %v, want 6", seg)
	}
}

func TestHandleModelsConfig_RejectsOutOfRange(t *testing.T) {
	setTestRoot(t, "model-config-")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	body := []byte(`{"flags":{"segment":99}}`)
	resp, err := http.Post(srv.URL+"/api/models/htdemucs_ft/config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST status = %d, want 400", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	msg := got["error"]
	if !strings.Contains(msg, "segment") || !strings.Contains(msg, "0") || !strings.Contains(msg, "7") {
		t.Errorf("error message should name the flag and range, got %q", msg)
	}
}

func TestHandleModelsConfig_HtdemucsFtShiftsTwentyPreserved(t *testing.T) {
	setTestRoot(t, "model-config-")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	// Save the real-world htdemucs_ft configuration: shifts=20.
	body := []byte(`{"flags":{"shifts":20}}`)
	resp, err := http.Post(srv.URL+"/api/models/htdemucs_ft/config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}

	getResp, err := http.Get(srv.URL + "/api/models/htdemucs_ft/config")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	var got ModelFlagsResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}

	var shifts ModelFlagValue
	for _, f := range got.Flags {
		if f.Name == "shifts" {
			shifts = f
			break
		}
	}
	if shifts.Name == "" {
		t.Fatal("htdemucs_ft: shifts flag not found")
	}
	if toInt(shifts.Value) != 20 {
		t.Errorf("shifts value = %v, want 20", shifts.Value)
	}
	if toInt(shifts.Max) != 20 {
		t.Errorf("shifts max = %v, want 20", shifts.Max)
	}

	// And the pipeline must actually use shifts=20.
	req := &SeparateRequest{Input: "/app/input/song.wav", Demucs: true}
	_, args, _, _, _ := buildPipelineArgs(req)
	if got := argValue(args, "--shifts"); got != "20" {
		t.Errorf("pipeline --shifts = %q, want 20", got)
	}
}

func flagValue(flags []ModelFlagValue, name string) interface{} {
	for _, f := range flags {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

func TestClampDemucsSegment(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{"auto zero", 0, 0},
		{"negative auto", -1, 0},
		{"small fraction rounds up to 1", 0.1, 1},
		{"one stays one", 1, 1},
		{"round down", 2.4, 2},
		{"round up", 2.5, 3},
		{"seven stays seven", 7, 7},
		{"7.1 clamps to 7", 7.1, 7},
		{"7.5 clamps to 7", 7.5, 7},
		{"model limit 7.8 clamps to 7", 7.8, 7},
		{"large value clamps to 7", 100, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampDemucsSegment(tt.input)
			if got != tt.want {
				t.Errorf("clampDemucsSegment(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildPipelineArgs_DemucsInvalidSegmentRejected(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	// Write a user config with an out-of-range segment directly (bypassing
	// save-time validation) to ensure the pipeline builder reports the error
	// instead of silently clamping it.
	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     8,
		Jobs:        0,
	}
	mcDir := filepath.Join(root, "config", "model_configs")
	if err := os.MkdirAll(mcDir, 0o755); err != nil {
		t.Fatalf("failed to create model config dir: %v", err)
	}
	writeModelConfigYamlAt(t, mcDir, "htdemucs_ft", cfg)

	req := &SeparateRequest{
		Input:     "/app/input/song.wav",
		Demucs:    true,
		StemModel: "htdemucs_ft",
	}
	_, _, _, _, err := buildPipelineArgs(req)
	if err == nil {
		t.Fatal("expected error for invalid demucs segment, got nil")
	}
	if !strings.Contains(err.Error(), "segment") {
		t.Errorf("error should mention segment, got %q", err.Error())
	}
}

func TestBuildStepPipelineArgs_DemucsDecimalSegmentRounded(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     5.2,
		Jobs:        0,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "demucs",
		Type:    "demucs",
		Model:   "htdemucs_ft",
		Enabled: true,
	}
	args, _, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")

	if got := argValue(args, "--demucs-segment"); got != "5" {
		t.Errorf("expected --demucs-segment 5, got %q", got)
	}
}

func TestReadModelConfigFromYaml_ReadsChunkSize(t *testing.T) {
	setTestRoot(t, "model-config-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		ChunkSize:   120,
	}
	if err := writeModelConfigToYaml("TestRoformer", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	read := readModelConfigFromYaml("TestRoformer")
	if read.ChunkSize != 120 {
		t.Errorf("ChunkSize = %d, want 120", read.ChunkSize)
	}
}

func TestBuildPipelineArgs_LegacyVocalChunkSizeEnv(t *testing.T) {
	root := setTestRoot(t, "model-config-")
	modelDir := filepath.Join(root, "models", "VR_Models", "TestRoformer")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformer.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy checkpoint: %v", err)
	}

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		ChunkSize:   90,
	}
	if err := writeModelConfigToYaml("TestRoformer", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:      "/app/input/song.wav",
		VocalModel: "TestRoformer",
	}
	_, args, _, env, _ := buildPipelineArgs(req)

	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
	if !contains(env, "ONDA_CHUNK_SIZE=90") {
		t.Errorf("expected ONDA_CHUNK_SIZE=90 in env, got %v", env)
	}
}

func TestBuildStepPipelineArgs_VocalChunkSizeEnv(t *testing.T) {
	root := setTestRoot(t, "model-config-")
	modelDir := filepath.Join(root, "models", "VR_Models", "TestRoformer")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformer.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy checkpoint: %v", err)
	}

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		ChunkSize:   45,
	}
	if err := writeModelConfigToYaml("TestRoformer", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "vocal",
		Type:    "vocal",
		Model:   "TestRoformer",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"vocals":       {Action: cli.StemSave, Target: "result"},
			"instrumental": {Action: cli.StemSave, Target: "result"},
		},
	}
	args, env, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")

	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
	if !contains(env, "ONDA_CHUNK_SIZE=45") {
		t.Errorf("expected ONDA_CHUNK_SIZE=45 in env, got %v", env)
	}
}

func TestBuildStepPipelineArgs_VocalNoChunkSizeOmitsEnv(t *testing.T) {
	root := setTestRoot(t, "model-config-")
	modelDir := filepath.Join(root, "models", "VR_Models", "TestRoformerZero")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformerZero.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy checkpoint: %v", err)
	}

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		ChunkSize:   0,
	}
	if err := writeModelConfigToYaml("TestRoformerZero", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "vocal",
		Type:    "vocal",
		Model:   "TestRoformerZero",
		Enabled: true,
	}
	_, env, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")

	for _, e := range env {
		if strings.HasPrefix(e, "ONDA_CHUNK_SIZE") {
			t.Errorf("expected no ONDA_CHUNK_SIZE env var, got %v", env)
		}
	}
}

func TestHandleModelsConfig_ReturnsFlagMetadata(t *testing.T) {
	root := setTestRoot(t, "model-config-meta-")

	// 1. A model with its own manifest.
	modelDir := filepath.Join(root, "models", "VR_Models", "TestRoformer")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestRoformer.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write ckpt: %v", err)
	}
	manifest := modelManifest{
		Name: "TestRoformer",
		Type: "bs_roformer",
		Stems: modelManifestStems{
			Stems:    []string{"vocals", "instrumental"},
			Target:   strPtr("vocals"),
			NumStems: 2,
		},
		Flags: map[string]modelFlagDef{
			"segment_size": copyFlagDef(knownFlags["segment_size"]),
			"num_overlap":  copyFlagDef(knownFlags["num_overlap"]),
			"batch_size":   copyFlagDef(knownFlags["batch_size"]),
			"chunk_size":   copyFlagDef(knownFlags["chunk_size"]),
			"device":       copyFlagDef(knownFlags["device"]),
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.manifest.json"), manifestData, 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// 2. The built-in htdemucs_ft model (no on-disk directory in this root).
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	cases := []struct {
		model string
	}{
		{"TestRoformer"},
		{"htdemucs_ft"},
	}
	for _, tc := range cases {
		resp, err := http.Get(srv.URL + "/api/models/" + tc.model + "/config")
		if err != nil {
			t.Fatalf("GET %s failed: %v", tc.model, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", tc.model, resp.StatusCode)
		}
		var got ModelFlagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode %s config: %v", tc.model, err)
		}
		resp.Body.Close()

		if got.Model != tc.model {
			t.Errorf("%s: model = %q, want %q", tc.model, got.Model, tc.model)
		}
		if len(got.Flags) == 0 {
			t.Fatalf("%s: expected flags, got none", tc.model)
		}
		for _, f := range got.Flags {
			if f.Name == "device" {
				// Device has no quality/VRAM/speed tags.
				if f.Description == "" {
					t.Errorf("%s/%s: device should still have a description", tc.model, f.Name)
				}
				continue
			}
			if f.Description == "" {
				t.Errorf("%s/%s: missing description", tc.model, f.Name)
			}
			if len(f.Affects) == 0 {
				t.Errorf("%s/%s: missing affects", tc.model, f.Name)
			}
			if f.BetterSide == "" {
				t.Errorf("%s/%s: missing better_side", tc.model, f.Name)
			}
		}
	}

	// Demucs-specific range check for htdemucs_ft.
	resp, err := http.Get(srv.URL + "/api/models/htdemucs_ft/config")
	if err != nil {
		t.Fatalf("GET htdemucs_ft failed: %v", err)
	}
	defer resp.Body.Close()
	var demucs ModelFlagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&demucs); err != nil {
		t.Fatalf("failed to decode demucs config: %v", err)
	}
	var shifts ModelFlagValue
	for _, f := range demucs.Flags {
		if f.Name == "shifts" {
			shifts = f
			break
		}
	}
	if shifts.Name == "" {
		t.Fatal("htdemucs_ft: shifts flag not found")
	}
	if toInt(shifts.Min) != 1 || toInt(shifts.Max) != 20 {
		t.Errorf("htdemucs_ft shifts range = %v..%v, want 1..20", shifts.Min, shifts.Max)
	}
	if shiftsDefault := toInt(shifts.Default); shiftsDefault < 1 || shiftsDefault > 20 {
		t.Errorf("htdemucs_ft shifts default = %v, want inside 1..20", shifts.Default)
	}
}

func TestIsOnnxModel(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	modelDir := filepath.Join(root, "models", "MDX_Net_Models", "MyMDXNet")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.onnx"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write onnx file: %v", err)
	}

	if !isOnnxModel("MyMDXNet") {
		t.Error("expected MyMDXNet to be detected as ONNX model")
	}
	if isOnnxModel("NonExistent") {
		t.Error("expected non-existent model not to be ONNX")
	}
}

func TestBuildPipelineArgs_OnnxModel(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	modelDir := filepath.Join(root, "models", "MDX_Net_Models", "MyMDXNet")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.onnx"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write onnx file: %v", err)
	}

	req := &SeparateRequest{Input: "/app/input/song.wav", VocalModel: "MyMDXNet"}
	_, args, _, _, _ := buildPipelineArgs(req)
	if got := argValue(args, "--vocal-type"); got != "mdxnet" {
		t.Errorf("expected --vocal-type mdxnet, got %q", got)
	}
	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
}

func TestBuildStepPipelineArgs_OnnxModel(t *testing.T) {
	root := setTestRoot(t, "model-config-")

	modelDir := filepath.Join(root, "models", "MDX_Net_Models", "MyMDXNet")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.onnx"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to write onnx file: %v", err)
	}

	step := cli.PipelineStep{
		ID:      "vocal",
		Type:    "vocal",
		Model:   "MyMDXNet",
		Enabled: true,
	}
	args, _, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")
	if got := argValue(args, "--vocal-type"); got != "mdxnet" {
		t.Errorf("expected --vocal-type mdxnet, got %q", got)
	}
	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
}

