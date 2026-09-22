package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func loadHFCatalog() ([]byte, error) {
	return readImageFile("hf_models.json")
}

// hfCatalogModel mirrors a single model entry in hf_models.json with the
// additional downloaded flag computed from disk.
type hfCatalogModel struct {
	Name       string  `json:"name"`
	Filename   string  `json:"filename"`
	HFPath     string  `json:"hf_path"`
	SizeMB     float64 `json:"size_mb"`
	Downloaded bool    `json:"downloaded"`
}

type hfCatalogCategory struct {
	Parent string           `json:"parent"`
	Models []hfCatalogModel `json:"models"`
}

type hfCatalogRoot struct {
	Source     string                       `json:"source"`
	Repo       string                       `json:"repo"`
	Categories map[string]hfCatalogCategory `json:"categories"`
}

// isHFCatalogEntryDownloaded reports whether an HF catalog entry already exists
// on disk. It first checks the default Demucs_Models download location and then
// walks the known model roots looking for the filename anywhere underneath,
// including inside model-specific subdirectories created by manual uploads.
func isHFCatalogEntryDownloaded(entry hfCatalogModel) bool {
	if entry.HFPath != "" {
		expected := filepath.Join(modelsBasePath(), "Demucs_Models", entry.HFPath)
		if _, err := os.Stat(expected); err == nil {
			return true
		}
	}
	if entry.Filename == "" {
		return false
	}
	for _, subdir := range modelSubdirs {
		root := filepath.Join(modelsBasePath(), subdir)
		found := false
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if info.Name() == entry.Filename {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

func (s *Server) handleModelsCatalogHF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	data, err := loadHFCatalog()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "HF catalog not available"})
		return
	}

	var catalog hfCatalogRoot
	if err := json.Unmarshal(data, &catalog); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse HF catalog"})
		return
	}

	for catName, cat := range catalog.Categories {
		for i := range cat.Models {
			cat.Models[i].Downloaded = isHFCatalogEntryDownloaded(cat.Models[i])
		}
		catalog.Categories[catName] = cat
	}

	out, err := json.Marshal(catalog)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to encode HF catalog"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}
