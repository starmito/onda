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
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     10,
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
	if n := findYamlChildNode(infNode, "dim_t"); n == nil || n.Value != "801" {
		t.Errorf("expected dim_t=801, got %v", n)
	}

	demNode := findYamlChildNode(rootNode, "demucs")
	if demNode == nil {
		t.Fatal("missing demucs section in created YAML")
	}
	// Segment above the CLI limit (7) is clamped to 7.
	for _, kv := range []struct{ key, want string }{
		{"shifts", "4"},
		{"segment", "7"},
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
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

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
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     10,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:    "/app/input/song.wav",
		Demucs:   true,
		StemModel: "htdemucs_ft",
	}
	_, args, _, _ := buildPipelineArgs(req)

	if !contains(args, "--stem-model") {
		t.Fatal("expected --stem-model flag")
	}
	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "2" {
		t.Errorf("expected --jobs 2, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsDefaultModelUsesSavedConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     10,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	// Scenario from the bug report: legacy request with viperx + demucs and no
	// explicit stem model should still pick up the saved htdemucs_ft config.
	req := &SeparateRequest{
		Input:  "/app/input/song.wav",
		Viperx: true,
		Demucs: true,
	}
	_, args, _, _ := buildPipelineArgs(req)

	if !contains(args, "--stem-model") {
		t.Fatal("expected --stem-model flag")
	}
	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "2" {
		t.Errorf("expected --jobs 2, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsRequestOverridesConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     10,
		Jobs:        2,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:         "/app/input/song.wav",
		Demucs:        true,
		StemModel:     "htdemucs_ft",
		Shifts:        2,
		DemucsSegment: 8,
		Jobs:          1,
	}
	_, args, _, _ := buildPipelineArgs(req)

	if got := argValue(args, "--shifts"); got != "2" {
		t.Errorf("expected request --shifts 2, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected request --demucs-segment 7, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "1" {
		t.Errorf("expected request --jobs 1, got %q", got)
	}
}

func TestBuildPipelineArgs_DemucsDecimalSegmentClamped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     7.8,
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
	_, args, _, _ := buildPipelineArgs(req)

	// 7.8 exceeds the integer CLI limit of 7 and is clamped.
	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
	}
}

func TestBuildStepPipelineArgs_DemucsUsesSavedConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      6,
		Segment:     12,
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
	args, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")

	if got := argValue(args, "--shifts"); got != "6" {
		t.Errorf("expected --shifts 6, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
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
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Device:      "cuda",
		Shifts:      1,
		Segment:     7.8,
		Jobs:        0,
	}
	body, _ := json.Marshal(cfg)
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

	var got ModelConfigResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}
	// POST clamps values above the CLI limit to 7.
	if got.Segment != 7 {
		t.Errorf("GET segment = %v, want 7", got.Segment)
	}
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

func TestBuildPipelineArgs_DemucsSegmentRequestClamped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     0,
		Jobs:        0,
	}
	if err := writeModelConfigToYaml("htdemucs_ft", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	req := &SeparateRequest{
		Input:         "/app/input/song.wav",
		Demucs:        true,
		StemModel:     "htdemucs_ft",
		DemucsSegment: 7.8,
	}
	_, args, _, _ := buildPipelineArgs(req)

	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
	}
}

func TestBuildStepPipelineArgs_DemucsSegmentClamped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      1,
		Segment:     7.8,
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
	args, _ := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")

	if got := argValue(args, "--demucs-segment"); got != "7" {
		t.Errorf("expected --demucs-segment 7, got %q", got)
	}
}

func TestReadModelConfigFromYaml_ReadsChunkSize(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

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
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

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
		Viperx:     true,
		VocalModel: "TestRoformer",
	}
	_, args, _, env := buildPipelineArgs(req)

	if !contains(args, "--viperx-model") {
		t.Error("expected --viperx-model flag")
	}
	if !contains(env, "ONDA_CHUNK_SIZE=90") {
		t.Errorf("expected ONDA_CHUNK_SIZE=90 in env, got %v", env)
	}
}

func TestBuildStepPipelineArgs_VocalChunkSizeEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

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
	args, env := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")

	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
	if !contains(env, "ONDA_CHUNK_SIZE=45") {
		t.Errorf("expected ONDA_CHUNK_SIZE=45 in env, got %v", env)
	}
}

func TestBuildStepPipelineArgs_VocalNoChunkSizeOmitsEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

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
	_, env := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")

	for _, e := range env {
		if strings.HasPrefix(e, "ONDA_CHUNK_SIZE") {
			t.Errorf("expected no ONDA_CHUNK_SIZE env var, got %v", env)
		}
	}
}

