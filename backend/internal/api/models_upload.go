package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// modelUploadExts lists the file extensions accepted as model weight files.
var modelUploadExts = map[string]bool{
	".ckpt":         true,
	".pth":          true,
	".onnx":         true,
	".safetensors":  true,
	".pt":           true,
}

// modelConfigExts lists the file extensions accepted as sidecar config files.
var modelConfigExts = map[string]bool{
	".yaml": true,
	".yml":  true,
	".json": true,
}

// ModelUploadResponse is returned by POST /api/models/upload.
type ModelUploadResponse struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	Category        string   `json:"category"`
	Type            string   `json:"type"`
	Path            string   `json:"path"`
	SizeMB          int64    `json:"size_mb"`
	Stems           []string `json:"stems"`
	NumStems        int      `json:"num_stems"`
	ManifestMissing bool     `json:"manifest_missing"`
	Inferred        bool     `json:"inferred"`
}

// sanitizeModelFilename validates and cleans a model upload filename. It rejects
// path traversal and unsupported extensions while preserving the original
// extension case.
func sanitizeModelFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty filename")
	}
	base := filepath.Base(name)
	if strings.Contains(base, "..") || strings.ContainsAny(base, "/\\") {
		return "", fmt.Errorf("invalid filename")
	}
	ext := strings.ToLower(filepath.Ext(base))
	if !modelUploadExts[ext] && !modelConfigExts[ext] {
		return "", fmt.Errorf("unsupported extension %q", ext)
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = sanitizeFilenameStem(stem)
	if stem == "" {
		stem = "model"
	}
	return stem + filepath.Ext(base), nil
}

// saveUploadPart streams a multipart file to dest and returns the bytes written.
func saveUploadPart(part *multipart.FileHeader, dest string) (int64, error) {
	src, err := part.Open()
	if err != nil {
		return 0, err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return 0, err
	}
	dst, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

// handleModelsUpload accepts a model weight file (.ckpt/.pth/.onnx/.safetensors/.pt)
// plus optional sidecar configs (.yaml/.yml/.json), places them under the correct
// models/ category directory, generates a model.manifest.json, and returns the
// model metadata so the UI can list it immediately.
// POST /api/models/upload
func (s *Server) handleModelsUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	if err := r.ParseMultipartForm(2 << 30); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse multipart form"})
		return
	}

	var parts []*multipart.FileHeader
	if fh := r.MultipartForm.File["files"]; len(fh) > 0 {
		parts = fh
	} else if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
		parts = fh
	}
	if len(parts) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		return
	}

	var weightParts []*multipart.FileHeader
	var configParts []*multipart.FileHeader
	for _, p := range parts {
		safe, err := sanitizeModelFilename(p.Filename)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		p.Filename = safe
		ext := strings.ToLower(filepath.Ext(safe))
		if modelUploadExts[ext] {
			weightParts = append(weightParts, p)
		} else if modelConfigExts[ext] {
			configParts = append(configParts, p)
		}
	}

	if len(weightParts) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no model weight file provided"})
		return
	}

	// Process one weight file per request. The frontend uploads files one by one.
	weight := weightParts[0]
	safeName := weight.Filename
	ext := strings.ToLower(filepath.Ext(safeName))
	modelName := strings.TrimSuffix(safeName, ext)
	categoryDir := detectCategoryFromFilename(safeName)
	modelDir := filepath.Join(modelsBasePath(), categoryDir, modelName)

	if err := os.MkdirAll(modelDir, 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create model directory"})
		return
	}

	destPath := filepath.Join(modelDir, safeName)
	size, err := saveUploadPart(weight, destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save model file"})
		return
	}

	// Save every sidecar config sent in the same request; the manifest parser
	// will look at all YAML/JSON files in the model directory.
	for _, cfg := range configParts {
		cfgName := cfg.Filename
		cfgDest := filepath.Join(modelDir, cfgName)
		if _, err := saveUploadPart(cfg, cfgDest); err != nil {
			Log("backend", "warn", fmt.Sprintf("failed to save config %s: %v", cfgName, err))
		}
	}

	// Generate manifest so the model is immediately usable.
	if err := generateModelManifest(modelDir, safeName, "upload"); err != nil {
		Log("backend", "warn", fmt.Sprintf("failed to generate manifest for %s: %v", modelName, err))
	}

	manifest, ok := loadModelManifest(modelDir)
	displayName := modelName
	modelType := ""
	category := categoryDir
	var stems []string
	numStems := 0
	manifestMissing := true
	inferred := false
	if ok {
		manifestMissing = false
		if manifest.Name != "" {
			displayName = manifest.Name
		}
		modelType = manifest.Type
		category = categoryFromType(modelType)
		stems = manifest.Stems.Stems
		numStems = manifest.Stems.NumStems
		inferred = manifest.Inferred
	}

	resp := ModelUploadResponse{
		Name:            modelName,
		DisplayName:     displayName,
		Category:        category,
		Type:            modelType,
		Path:            destPath,
		SizeMB:          size / (1024 * 1024),
		Stems:           stems,
		NumStems:        numStems,
		ManifestMissing: manifestMissing,
		Inferred:        inferred,
	}

	Log("backend", "success", fmt.Sprintf("Model uploaded: %s -> %s", modelName, destPath))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
