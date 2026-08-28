package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCategory(t *testing.T) {
	cases := []struct {
		subdir  string
		relPath string
		want    string
	}{
		{"VR_Models", "BS_Roformer_Viperx/model.ckpt", "Roformer"},
		{"VR_Models", "MelBand/model.ckpt", "Roformer/MelBand"},
		{"VR_Models", "SCNet/model.ckpt", "SCnet"},
		{"MDX_Net_Models", "mdx/model.ckpt", "MDX"},
		{"Demucs_Models", "model.th", "Demucs"},
		{"Demucs_ONNX", "vocals.onnx", "Demucs ONNX"},
	}
	for _, c := range cases {
		got := detectCategory(c.subdir, c.relPath)
		if got != c.want {
			t.Errorf("detectCategory(%q, %q) = %q, want %q", c.subdir, c.relPath, got, c.want)
		}
	}
}

func TestComputeDisplayName(t *testing.T) {
	cases := []struct {
		subdir string
		rel    string
		name   string
		want   string
	}{
		{"VR_Models", "BS_Roformer_Viperx/model.ckpt", "model", "BS_Roformer_Viperx"},
		{"Demucs_ONNX", "htdemucs_ft_vocals.onnx", "htdemucs_ft_vocals", "htdemucs_ft (vocals)"},
		{"MDX_Net_Models", "model.onnx", "model", "model"},
	}
	for _, c := range cases {
		got := computeDisplayName(c.subdir, c.rel, c.name)
		if got != c.want {
			t.Errorf("computeDisplayName(%q, %q, %q) = %q, want %q", c.subdir, c.rel, c.name, got, c.want)
		}
	}
}

func TestDemucsONNXDisplayName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"htdemucs_ft_vocals", "htdemucs_ft (vocals)"},
		{"htdemucs_ft_drums", "htdemucs_ft (drums)"},
		{"model", "model"},
	}
	for _, c := range cases {
		got := demucsONNXDisplayName(c.name)
		if got != c.want {
			t.Errorf("demucsONNXDisplayName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEstimateVRAM(t *testing.T) {
	cases := []struct {
		name     string
		category string
		sizeMB   int64
		want     int64
	}{
		{"htdemucs_ft", "Demucs", 0, 2800},
		{"model", "Demucs ONNX", 100, 200},
		{"model", "Roformer", 500, 500},
		{"model", "VR_Arch", 0, 500},
	}
	for _, c := range cases {
		got := estimateVRAM(c.name, c.category, c.sizeMB)
		if got != c.want {
			t.Errorf("estimateVRAM(%q, %q, %d) = %d, want %d", c.name, c.category, c.sizeMB, got, c.want)
		}
	}
}

func TestResolveModelDir(t *testing.T) {
	origModels := modelsBasePath
	tmpModels := t.TempDir()
	modelsBasePath = tmpModels
	defer func() { modelsBasePath = origModels }()

	if got := resolveModelDir("htdemucs_ft"); got != "htdemucs_ft" {
		t.Errorf("resolveModelDir(htdemucs_ft) = %q, want htdemucs_ft", got)
	}

	modelDir := filepath.Join(modelsBasePath, "VR_Models", "MyModel")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelFile := filepath.Join(modelDir, "weights.ckpt")
	if err := os.WriteFile(modelFile, []byte("weights"), 0o644); err != nil {
		t.Fatalf("failed to write model file: %v", err)
	}

	got := resolveModelDir("MyModel")
	want := filepath.Join(tmpModels, "VR_Models", "MyModel")
	if got != want {
		t.Errorf("resolveModelDir(MyModel) = %q, want %q", got, want)
	}
}

func TestStripExtension(t *testing.T) {
	cases := []struct {
		filename string
		exts     []string
		want     string
	}{
		{"model.ckpt", []string{".ckpt", ".pth"}, "model"},
		{"model.pth", []string{".ckpt", ".pth"}, "model"},
		{"model", []string{".ckpt"}, "model"},
	}
	for _, c := range cases {
		got := stripExtension(c.filename, c.exts)
		if got != c.want {
			t.Errorf("stripExtension(%q, %v) = %q, want %q", c.filename, c.exts, got, c.want)
		}
	}
}

func TestDetectCategoryFromFilename(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"scnet_model.ckpt", "VR_Models"},
		{"viperx_vocals.pth", "VR_Models"},
		{"melband_model.ckpt", "VR_Models"},
		{"mdx23c.onnx", "MDX_Net_Models"},
		{"htdemucs_ft.th", "Demucs_Models"},
		{"unknown.bin", "VR_Models"},
	}
	for _, c := range cases {
		got := detectCategoryFromFilename(c.filename)
		if got != c.want {
			t.Errorf("detectCategoryFromFilename(%q) = %q, want %q", c.filename, got, c.want)
		}
	}
}
