package api

import (
	"os"
	"path/filepath"
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
	for _, kv := range []struct{ key, want string }{
		{"shifts", "4"},
		{"segment", "10"},
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
	_, args, _ := buildPipelineArgs(req)

	if !contains(args, "--stem-model") {
		t.Fatal("expected --stem-model flag")
	}
	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "10" {
		t.Errorf("expected --demucs-segment 10, got %q", got)
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
	_, args, _ := buildPipelineArgs(req)

	if got := argValue(args, "--shifts"); got != "2" {
		t.Errorf("expected request --shifts 2, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "8" {
		t.Errorf("expected request --demucs-segment 8, got %q", got)
	}
	if got := argValue(args, "--jobs"); got != "1" {
		t.Errorf("expected request --jobs 1, got %q", got)
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
	args := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")

	if got := argValue(args, "--shifts"); got != "6" {
		t.Errorf("expected --shifts 6, got %q", got)
	}
	if got := argValue(args, "--demucs-segment"); got != "12" {
		t.Errorf("expected --demucs-segment 12, got %q", got)
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

