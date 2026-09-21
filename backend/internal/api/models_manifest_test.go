package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeManifest helper creates a model.manifest.json in the given directory.
func writeManifest(t *testing.T, dir string, manifest map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
}

// TestListModelsReadsManifest verifies that real model metadata (name, type,
// stems, num_stems, target) comes from model.manifest.json and that the
// category is derived from the real type.
func TestListModelsReadsManifest(t *testing.T) {
	root := setTestRoot(t, "models-manifest-")

	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_Viperx")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	weight := filepath.Join(modelDir, "BS_Roformer_Viperx.ckpt")
	if err := os.WriteFile(weight, []byte("weights"), 0o644); err != nil {
		t.Fatalf("failed to write weight: %v", err)
	}

	writeManifest(t, modelDir, map[string]any{
		"name": "BS_Roformer_Viperx",
		"type": "bs_roformer",
		"stems": map[string]any{
			"stems":    []string{"vocals"},
			"target":   "vocals",
			"num_stems": 1,
		},
	})

	resp := listModels()
	if len(resp.Models) != 2 {
		t.Fatalf("expected 2 models (test model + htdemucs_ft), got %d", len(resp.Models))
	}

	var m ModelEntry
	for _, candidate := range resp.Models {
		if candidate.Name == "BS_Roformer_Viperx" {
			m = candidate
			break
		}
	}
	if m.Name == "" {
		t.Fatalf("BS_Roformer_Viperx not found in listing")
	}

	if m.DisplayName != "BS_Roformer_Viperx" {
		t.Errorf("display_name = %q, want BS_Roformer_Viperx", m.DisplayName)
	}
	if m.Type != "bs_roformer" {
		t.Errorf("type = %q, want bs_roformer", m.Type)
	}
	if m.Category != "Roformer" {
		t.Errorf("category = %q, want Roformer", m.Category)
	}
	if m.NumStems != 1 {
		t.Errorf("num_stems = %d, want 1", m.NumStems)
	}
	if !slices.Equal(m.Stems, []string{"vocals"}) {
		t.Errorf("stems = %v, want [vocals]", m.Stems)
	}
	if m.Target == nil || *m.Target != "vocals" {
		t.Errorf("target = %v, want vocals", m.Target)
	}
	if m.ManifestMissing {
		t.Errorf("manifest_missing = true, want false")
	}

	// The old hardcoded detection no longer invents a "viperx" category.
	if m.Category == "viperx" || m.Category == "Viperx" {
		t.Errorf("category must not be viperx")
	}
}

// TestListModelsManifestMissingFallback verifies that a model without a
// manifest is still listed, is flagged as manifest_missing, and keeps the
// fallback category.
func TestListModelsManifestMissingFallback(t *testing.T) {
	root := setTestRoot(t, "models-no-manifest-")

	modelDir := filepath.Join(root, "models", "VR_Models", "Some_Model")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	weight := filepath.Join(modelDir, "Some_Model.pth")
	if err := os.WriteFile(weight, []byte("weights"), 0o644); err != nil {
		t.Fatalf("failed to write weight: %v", err)
	}

	resp := listModels()
	if len(resp.Models) != 2 {
		t.Fatalf("expected 2 models (test model + htdemucs_ft), got %d", len(resp.Models))
	}

	var m ModelEntry
	for _, candidate := range resp.Models {
		if candidate.Name == "Some_Model" {
			m = candidate
			break
		}
	}
	if m.Name == "" {
		t.Fatalf("Some_Model not found in listing")
	}

	if m.Name != "Some_Model" {
		t.Errorf("name = %q, want Some_Model", m.Name)
	}
	if !m.ManifestMissing {
		t.Errorf("manifest_missing = false, want true")
	}
	if m.Category != "VR_Arch" {
		t.Errorf("category = %q, want VR_Arch fallback", m.Category)
	}
}

// TestListModelsCategoriesDerivedFromType verifies that categories come from
// manifest types, not from a hardcoded table.
func TestListModelsCategoriesDerivedFromType(t *testing.T) {
	root := setTestRoot(t, "models-categories-")

	cases := []struct {
		folder   string
		mtype    string
		category string
	}{
		{"VR_Models/BS_Roformer_SW_6stem", "bs_roformer", "Roformer"},
		{"VR_Models/MDX23C_D1581", "mdx23c", "MDX"},
		{"VR_Models/SCNet_MUSDB18", "scnet", "SCNet"},
		{"MDX_Net_Models/Kim_Vocal_1", "mdx_net", "MDXNet"},
	}

	for _, tc := range cases {
		modelDir := filepath.Join(root, "models", tc.folder)
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", tc.folder, err)
		}
		base := filepath.Base(modelDir)
		weight := filepath.Join(modelDir, base+".ckpt")
		if err := os.WriteFile(weight, []byte("weights"), 0o644); err != nil {
			t.Fatalf("failed to write weight %s: %v", tc.folder, err)
		}
		writeManifest(t, modelDir, map[string]any{
			"name": base,
			"type": tc.mtype,
			"stems": map[string]any{
				"stems":     []string{"vocals"},
				"target":    nil,
				"num_stems": 1,
			},
		})
	}

	resp := listModels()
	// Each test case plus the built-in htdemucs_ft placeholder.
	if len(resp.Models) != len(cases)+1 {
		t.Fatalf("expected %d models, got %d", len(cases)+1, len(resp.Models))
	}

	for _, tc := range cases {
		found := false
		for _, m := range resp.Models {
			if m.DisplayName == filepath.Base(tc.folder) {
				found = true
				if m.Type != tc.mtype {
					t.Errorf("%s: type = %q, want %q", tc.folder, m.Type, tc.mtype)
				}
				if m.Category != tc.category {
					t.Errorf("%s: category = %q, want %q", tc.folder, m.Category, tc.category)
				}
				break
			}
		}
		if !found {
			t.Errorf("model %s not found in listing", tc.folder)
		}
	}

	// Categories should be sorted and derived, with no invented entries.
	// htdemucs_ft contributes the Demucs category.
	wantCats := []string{"Demucs", "MDX", "MDXNet", "Roformer", "SCNet"}
	if !slices.Equal(resp.Categories, wantCats) {
		t.Errorf("categories = %v, want %v", resp.Categories, wantCats)
	}
}
