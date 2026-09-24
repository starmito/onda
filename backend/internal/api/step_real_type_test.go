package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

// writeFakeModel creates a minimal model directory with a dummy weight and a
// manifest so resolveModelAlias / searchModelOnDisk can find it.
func writeFakeModel(t *testing.T, category, name, modelType string, weightExt string) {
	t.Helper()
	root := setTestRoot(t, "realtype")
	modelDir := filepath.Join(root, "models", category, name)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", modelDir, err)
	}
	// A dummy weight file so hasModelCheckpoint passes.
	if err := os.WriteFile(filepath.Join(modelDir, "model."+weightExt), []byte("weights"), 0o644); err != nil {
		t.Fatalf("write weight: %v", err)
	}
	manifest := `{"name":"` + name + `","type":"` + modelType + `","stems":{"stems":["vocals","instrumental"],"num_stems":2}}`
	if err := os.WriteFile(filepath.Join(modelDir, "model.manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeFakeYAMLModel(t *testing.T, category, name string, yamlContent string, weightExt string) {
	t.Helper()
	root := setTestRoot(t, "realtype")
	modelDir := filepath.Join(root, "models", category, name)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", modelDir, err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model."+weightExt), []byte("weights"), 0o644); err != nil {
		t.Fatalf("write weight: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

func TestDeriveRealStepType_RoFormerFromDemucsPreset(t *testing.T) {
	writeFakeModel(t, "VR_Models", "BS_Roformer_SW_6stem", "bs_roformer", "ckpt")

	step := cli.PipelineStep{ID: "demucs", Model: "BS_Roformer_SW_6stem", Type: "demucs"}
	got := deriveRealStepType(step)
	if got != "roformer" {
		t.Errorf("deriveRealStepType(demucs+BS_Roformer_SW_6stem) = %q, want roformer", got)
	}
}

func TestDeriveRealStepType_Demucs(t *testing.T) {
	// htdemucs_ft is special-cased and does not need an on-disk directory.
	step := cli.PipelineStep{ID: "demucs", Model: "htdemucs_ft", Type: "demucs"}
	if got := deriveRealStepType(step); got != "demucs" {
		t.Errorf("deriveRealStepType(demucs+htdemucs_ft) = %q, want demucs", got)
	}
}

func TestDeriveRealStepType_MDX(t *testing.T) {
	writeFakeYAMLModel(t, "VR_Models", "MDX23C_Fake", "model:\n  num_scales: 4\n", "ckpt")

	step := cli.PipelineStep{ID: "demucs", Model: "MDX23C_Fake", Type: "demucs"}
	if got := deriveRealStepType(step); got != "mdx" {
		t.Errorf("deriveRealStepType(demucs+MDX23C_Fake) = %q, want mdx", got)
	}
}

func TestDeriveRealStepType_SCNet(t *testing.T) {
	writeFakeYAMLModel(t, "VR_Models", "SCNet_Fake", "model:\n  band_SR: 1\n", "ckpt")

	step := cli.PipelineStep{ID: "demucs", Model: "SCNet_Fake", Type: "demucs"}
	if got := deriveRealStepType(step); got != "scnet" {
		t.Errorf("deriveRealStepType(demucs+SCNet_Fake) = %q, want scnet", got)
	}
}

func TestDeriveRealStepType_MDXNetONNX(t *testing.T) {
	writeFakeYAMLModel(t, "MDX_Net_Models", "Kim_Vocal_Fake", "model:\n  type: mdx_net\n", "onnx")

	step := cli.PipelineStep{ID: "demucs", Model: "Kim_Vocal_Fake", Type: "demucs"}
	if got := deriveRealStepType(step); got != "mdxnet" {
		t.Errorf("deriveRealStepType(demucs+Kim_Vocal_Fake) = %q, want mdxnet", got)
	}
}

func TestDeriveRealStepType_PolarFormer(t *testing.T) {
	writeFakeYAMLModel(t, "VR_Models", "BS_PolarFormer_Fake", "model:\n  use_pope: true\n", "onnx")

	step := cli.PipelineStep{ID: "demucs", Model: "BS_PolarFormer_Fake", Type: "demucs"}
	if got := deriveRealStepType(step); got != "polarformer" {
		t.Errorf("deriveRealStepType(demucs+BS_PolarFormer_Fake) = %q, want polarformer", got)
	}
}

func TestStepDisplayName_DerivesFromModel(t *testing.T) {
	writeFakeModel(t, "VR_Models", "BS_Roformer_SW_6stem", "bs_roformer", "ckpt")

	step := cli.PipelineStep{ID: "demucs", Model: "BS_Roformer_SW_6stem", Type: "demucs"}
	got := stepDisplayName(step)
	want := "Voz (BS_Roformer_SW_6stem)"
	if got != want {
		t.Errorf("stepDisplayName = %q, want %q", got, want)
	}
}

func TestStepDisplayName_Demucs(t *testing.T) {
	step := cli.PipelineStep{ID: "demucs", Model: "htdemucs_ft", Type: "demucs"}
	got := stepDisplayName(step)
	want := "Demucs (htdemucs_ft)"
	if got != want {
		t.Errorf("stepDisplayName = %q, want %q", got, want)
	}
}

func TestRamEstimate_UsesRealStepType(t *testing.T) {
	// A RoFormer model masquerading as a "demucs" step must be estimated as
	// RoFormer (lower fallback) rather than Demucs.
	roformerNeeded := estimateRAMMB("BS_Roformer_SW_6stem", "roformer")
	demucsNeeded := estimateRAMMB("BS_Roformer_SW_6stem", "demucs")
	if roformerNeeded >= demucsNeeded {
		t.Errorf("RoFormer RAM estimate (%d MiB) should be lower than Demucs estimate (%d MiB)", roformerNeeded, demucsNeeded)
	}
}
