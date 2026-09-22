package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// modelsBasePath returns the root directory where models live. It defaults to
// the "models" subdirectory under the current data root so the Go backend and
// the pipeline share a single path space and follow runtime data-root changes.
func modelsBasePath() string {
	return mustSub("models")
}

// modelSubdirs lists the known model subdirectories to scan.
var modelSubdirs = []string{
	"VR_Models",
	"MDX_Net_Models",
	"RoFormer_Models",
	"Demucs_Models",
	"Demucs_ONNX",
}

// modelExtensions are the file extensions considered valid model files.
var modelExtensions = map[string]bool{
	".pth":         true,
	".onnx":        true,
	".ckpt":        true,
	".th":          true,
	".safetensors": true,
}

// modelWeightExtensions lists extensions for model weight files (used for base-name stripping).
var modelWeightExtensions = []string{".ckpt", ".pth", ".onnx", ".th", ".safetensors"}

// dependencyExtensions lists extensions for dependency files (yaml configs, supplemental weights).
var dependencyExtensions = []string{".yaml", ".ckpt", ".pth", ".onnx", ".th"}

// categoryMap translates directory names to human-readable category labels.
// Note: VR_Models/ contains different model architectures; category is refined
// by detectCategory() from the subdirectory name (Roformer, MelBand, SCnet, etc.)
var categoryMap = map[string]string{
	"VR_Models":       "VR_Arch",
	"MDX_Net_Models":  "MDXNet",
	"RoFormer_Models": "Roformer",
	"Demucs_Models":   "Demucs",
	"Demucs_ONNX":     "Demucs ONNX",
}

// detectCategory refines the category based on the model subdirectory name.
// VR_Models/ contains Roformers (BS_Roformer_Viperx), MelBands, SCNets, etc.
func detectCategory(subdir, relPath string) string {
	baseCat := categoryMap[subdir]
	if subdir != "VR_Models" {
		return baseCat
	}
	// Under VR_Models/, detect from the model-specific subdirectory
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) >= 2 {
		modelDir := strings.ToLower(parts[0])
		switch {
		case strings.Contains(modelDir, "roformer"):
			return "Roformer"
		case strings.Contains(modelDir, "melband"):
			return "Roformer/MelBand"
		case strings.Contains(modelDir, "scnet"):
			return "SCnet"
		}
	}
	return baseCat
}

// modelManifestStems holds the stem metadata written by the Python manifest
// generator.
type modelManifestStems struct {
	Stems    []string `json:"stems"`
	Target   *string  `json:"target"`
	NumStems int      `json:"num_stems"`
}

// modelManifest is the JSON written next to each model by
// ``python3 -m onda.cli manifest --regenerate``.
type modelManifest struct {
	Name     string                  `json:"name"`
	Type     string                  `json:"type"`
	Stems    modelManifestStems      `json:"stems"`
	Flags    map[string]modelFlagDef `json:"flags,omitempty"`
	Origin   string                  `json:"origin,omitempty"`
	Inferred bool                    `json:"inferred,omitempty"`
}

// loadModelManifest reads and parses the manifest for a model directory.
// It returns the parsed manifest and true when the file exists and is valid.
func loadModelManifest(modelDir string) (*modelManifest, bool) {
	p := filepath.Join(modelDir, "model.manifest.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var m modelManifest
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("[models] failed to parse manifest %s: %v", p, err)
		return nil, false
	}
	return &m, true
}

// categoryFromType derives the user-facing category from the real model type
// stored in the manifest. This is the single source of truth for categories;
// no part of the code invents categories from filenames or folder names.
func categoryFromType(modelType string) string {
	switch modelType {
	case "bs_roformer", "mel_band_roformer":
		return "Roformer"
	case "mdx23c":
		return "MDX"
	case "mdx_net":
		return "MDXNet"
	case "scnet":
		return "SCNet"
	case "demucs", "htdemucs":
		return "Demucs"
	case "":
		return ""
	default:
		return strings.ToUpper(modelType[:1]) + modelType[1:]
	}
}

// strPtr returns a pointer to a string literal.
func strPtr(s string) *string { return &s }

// inferManifestType guesses the canonical model type for a newly-uploaded model
// from its filename. It prefers the existing heuristic helpers and falls back
// to keyword matching so every uploaded file gets a usable manifest.
func inferManifestType(filename string) string {
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	if mt, ok := detectModelTypeByName("", name); ok {
		mt = normalizeModelType(mt)
		// Canonicalise demucs variants to a single type understood by the UI.
		if mt == "htdemucs" {
			mt = "demucs"
		}
		return mt
	}
	if mt := classifyModelType(name); mt != "unknown" {
		if mt == "vocal" {
			return "bs_roformer"
		}
		if mt == "mdx" {
			return "mdx23c"
		}
		if mt == "mdxnet" {
			return "mdx_net"
		}
		return mt
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "melband") || strings.Contains(lower, "mel_band"):
		return "mel_band_roformer"
	case strings.Contains(lower, "roformer"):
		return "bs_roformer"
	case strings.Contains(lower, "mdx23c") || strings.Contains(lower, "mdx-c"):
		return "mdx23c"
	case strings.Contains(lower, "mdxnet") || strings.Contains(lower, "mdx_net") || strings.HasSuffix(lower, ".onnx"):
		return "mdx_net"
	case strings.Contains(lower, "scnet"):
		return "scnet"
	case strings.Contains(lower, "demucs") || strings.Contains(lower, "htdemucs"):
		return "demucs"
	}
	return "bs_roformer"
}

// inferManifestStems returns a sensible stem list for a model type and name.
// It never invents exotic stems: vocals/instrumental for vocal models and the
// standard Demucs set for Demucs models.
func inferManifestStems(modelType, filename string) modelManifestStems {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	switch modelType {
	case "demucs":
		if strings.Contains(name, "6s") || strings.Contains(name, "six") || strings.Contains(name, "6_stem") {
			return modelManifestStems{Stems: []string{"drums", "bass", "other", "vocals", "guitar", "piano"}, NumStems: 6}
		}
		return modelManifestStems{Stems: []string{"drums", "bass", "other", "vocals"}, NumStems: 4}
	case "mdx_net":
		if strings.Contains(name, "inst") {
			return modelManifestStems{Stems: []string{"instrumental"}, NumStems: 1, Target: strPtr("instrumental")}
		}
		if strings.Contains(name, "vocal") {
			return modelManifestStems{Stems: []string{"vocals"}, NumStems: 1, Target: strPtr("vocals")}
		}
		return modelManifestStems{Stems: []string{"vocals", "instrumental"}, NumStems: 2, Target: strPtr("vocals")}
	case "mdx23c", "scnet", "bs_roformer", "mel_band_roformer":
		return modelManifestStems{Stems: []string{"vocals", "instrumental"}, NumStems: 2, Target: strPtr("vocals")}
	default:
		return modelManifestStems{Stems: []string{"vocals", "instrumental"}, NumStems: 2, Target: strPtr("vocals")}
	}
}

// inferManifestFlags returns the editable flag set for a canonical model type.
func inferManifestFlags(modelType string) map[string]modelFlagDef {
	flagNames, ok := flagsByType[modelType]
	if !ok {
		flagNames = []string{"segment_size", "num_overlap", "chunk_size", "batch_size", "device"}
	}
	flags := make(map[string]modelFlagDef, len(flagNames))
	for _, fn := range flagNames {
		if def, ok := knownFlags[fn]; ok {
			flags[fn] = copyFlagDef(def)
		}
	}
	return flags
}

// configManifestInfo holds type and stems extracted from a sidecar config file.
type configManifestInfo struct {
	Type      string
	Stems     []string
	Target    *string
	NumStems  int
	FromConfig bool
}

// parseConfigForManifest scans the model directory for a YAML or JSON sidecar
// config and extracts the real model type and stem list when present.
func parseConfigForManifest(modelDir string) (configManifestInfo, bool) {
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return configManifestInfo{}, false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		path := filepath.Join(modelDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		info, ok := parseConfigBytesForManifest(data, ext)
		if ok {
			return info, true
		}
	}
	return configManifestInfo{}, false
}

// parseConfigBytesForManifest parses a single config buffer and extracts the
// real type/stems when they are clearly declared.
func parseConfigBytesForManifest(data []byte, ext string) (configManifestInfo, bool) {
	if ext == ".json" {
		var root map[string]interface{}
		if err := json.Unmarshal(data, &root); err != nil {
			return configManifestInfo{}, false
		}
		return extractManifestInfoFromMap(root)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return configManifestInfo{}, false
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return configManifestInfo{}, false
	}
	root := doc.Content[0]

	info := configManifestInfo{FromConfig: true}
	found := false

	if modelNode := findYamlChildNode(root, "model"); modelNode != nil && modelNode.Kind == yaml.MappingNode {
		if n := findYamlChildNode(modelNode, "type"); n != nil && n.Kind == yaml.ScalarNode && n.Value != "" {
			info.Type = normalizeModelType(n.Value)
			found = true
		}
		if n := findYamlChildNode(modelNode, "num_stems"); n != nil && n.Kind == yaml.ScalarNode {
			if v, err := strconv.Atoi(n.Value); err == nil {
				info.NumStems = v
			}
		}
	}

	if trainingNode := findYamlChildNode(root, "training"); trainingNode != nil && trainingNode.Kind == yaml.MappingNode {
		if instNode := findYamlChildNode(trainingNode, "instruments"); instNode != nil && instNode.Kind == yaml.SequenceNode {
			stems := make([]string, 0, len(instNode.Content))
			for _, child := range instNode.Content {
				if child.Kind == yaml.ScalarNode && child.Value != "" {
					stems = append(stems, strings.ToLower(child.Value))
				}
			}
			if len(stems) > 0 {
				info.Stems = stems
				info.NumStems = len(stems)
				found = true
			}
		}
		if n := findYamlChildNode(trainingNode, "target_instrument"); n != nil && n.Kind == yaml.ScalarNode && n.Value != "" {
			target := strings.ToLower(n.Value)
			info.Target = &target
		}
	}

	return info, found
}

// extractManifestInfoFromMap extracts type and stems from a JSON config map.
func extractManifestInfoFromMap(root map[string]interface{}) (configManifestInfo, bool) {
	info := configManifestInfo{FromConfig: true}
	found := false

	if model, ok := root["model"].(map[string]interface{}); ok {
		if t, ok := model["type"].(string); ok && t != "" {
			info.Type = normalizeModelType(t)
			found = true
		}
		if n, ok := model["num_stems"].(float64); ok {
			info.NumStems = int(n)
		}
	}

	if training, ok := root["training"].(map[string]interface{}); ok {
		if inst, ok := training["instruments"].([]interface{}); ok && len(inst) > 0 {
			stems := make([]string, 0, len(inst))
			for _, v := range inst {
				if s, ok := v.(string); ok && s != "" {
					stems = append(stems, strings.ToLower(s))
				}
			}
			if len(stems) > 0 {
				info.Stems = stems
				info.NumStems = len(stems)
				found = true
			}
		}
		if t, ok := training["target_instrument"].(string); ok && t != "" {
			target := strings.ToLower(t)
			info.Target = &target
		}
	}

	return info, found
}

// generateModelManifest writes a model.manifest.json next to an uploaded model.
// It derives name, type, stems and flags from the filename so the model is
// immediately usable by the pipeline, model editor and preset editor.
// When origin is "upload" the manifest records that the model was uploaded
// manually; if a sidecar config is present its declared type and stems are
// used and the manifest is not marked as inferred.
func generateModelManifest(modelDir, filename, origin string) error {
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	var modelType string
	var stems modelManifestStems
	configFound := false

	if cfg, ok := parseConfigForManifest(modelDir); ok {
		configFound = true
		if cfg.Type != "" {
			modelType = cfg.Type
		}
		if len(cfg.Stems) > 0 {
			stems = modelManifestStems{
				Stems:    cfg.Stems,
				Target:   cfg.Target,
				NumStems: cfg.NumStems,
			}
		} else if cfg.NumStems > 0 {
			stems.NumStems = cfg.NumStems
		}
		if stems.NumStems == 0 {
			stems.NumStems = len(stems.Stems)
		}
	}

	if modelType == "" {
		modelType = inferManifestType(filename)
	}
	if len(stems.Stems) == 0 {
		stems = inferManifestStems(modelType, filename)
	}
	if stems.NumStems == 0 {
		stems.NumStems = len(stems.Stems)
	}

	manifest := modelManifest{
		Name:     name,
		Type:     modelType,
		Stems:    stems,
		Flags:    inferManifestFlags(modelType),
		Origin:   origin,
		Inferred: !configFound,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(modelDir, "model.manifest.json"), data, 0644)
}

// findModelDirByBaseName searches the models tree for an existing model
// directory whose name matches base and that contains at least one weight file.
// It returns the model directory, the name of the first weight file found and
// true on success.
func findModelDirByBaseName(base string) (string, string, bool) {
	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath(), subdir)
		found := false
		modelDir := ""
		weightFile := ""
		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() || found {
				return nil
			}
			if filepath.Base(path) != base {
				return nil
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if modelUploadExts[ext] {
					modelDir = path
					weightFile = entry.Name()
					found = true
					return filepath.SkipAll
				}
			}
			return nil
		})
		if found {
			return modelDir, weightFile, true
		}
	}
	return "", "", false
}

// computeDisplayName derives a human-friendly display name from the file's
// relative path and its parent directory structure.
func computeDisplayName(subdir, rel, name string) string {
	parentDir := filepath.Base(filepath.Dir(rel))
	// File sits directly in the category directory (no model-specific subdir).
	// This happens for Demucs ONNX stems: htdemucs_ft_vocals → "htdemucs_ft (vocals)"
	if parentDir == subdir || parentDir == "." {
		if subdir == "Demucs_ONNX" {
			return demucsONNXDisplayName(name)
		}
		return name
	}
	// Use the model-specific subdirectory name (already friendly: "BS_Roformer_Viperx", etc.)
	return parentDir
}

// demucsONNXDisplayName converts a Demucs ONNX stem filename to a display name.
// E.g., "htdemucs_ft_vocals" → "htdemucs_ft (vocals)"
func demucsONNXDisplayName(name string) string {
	demucsStems := []string{"vocals", "drums", "bass", "other", "guitar", "piano"}
	for _, stem := range demucsStems {
		if strings.HasSuffix(name, "_"+stem) {
			base := strings.TrimSuffix(name, "_"+stem)
			return base + " (" + stem + ")"
		}
	}
	return name
}

// ModelEntry describes a single model file found on disk.
type ModelEntry struct {
	Name            string   `json:"name"`
	InstalledName   string   `json:"installed_name"`
	DisplayName     string   `json:"display_name"`
	Category        string   `json:"category"`
	Type            string   `json:"type"`
	Path            string   `json:"path"`
	SizeMB          int64    `json:"size_mb"`
	VramEstimateMB  int64    `json:"vram_estimate_mb"`
	Stems           []string `json:"stems"`
	NumStems        int      `json:"num_stems"`
	Target          *string  `json:"target"`
	ManifestMissing bool     `json:"manifest_missing"`
	Inferred        bool     `json:"inferred"`
	Origin          string   `json:"origin,omitempty"`
}

// estimateVRAM returns an estimated VRAM usage in MB for a model based on its
// name, category, and on-disk size. Modern models (.ckpt/.pth/.safetensors)
// use fp16 weights and load roughly 1:1 from disk to VRAM. The frontend
// calculates inference activation overhead separately, so this only accounts
// for the base model weights.
func estimateVRAM(name string, category string, sizeMB int64) int64 {
	lower := strings.ToLower(name)

	// Built-in PyTorch model with no on-disk file
	if lower == "htdemucs_ft" && sizeMB == 0 {
		return 2800
	}

	// ONNX expands roughly 2× in VRAM vs disk
	if category == "Demucs ONNX" {
		if sizeMB > 0 {
			return sizeMB * 2
		}
		return 500
	}

	// fp16 .ckpt/.pth/.safetensors → 1:1 disk-to-VRAM
	if sizeMB > 0 {
		return sizeMB
	}

	// Fallback minimum
	return 500
}
type ModelsListResponse struct {
	Models     []ModelEntry `json:"models"`
	Categories []string     `json:"categories"`
}

// DownloadRequest is the JSON body for POST /api/models/download.
type DownloadRequest struct {
	Source   string `json:"source"`
	Repo     string `json:"repo"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
	Category string `json:"category,omitempty"`
	Name     string `json:"name,omitempty"`
}

// DownloadStatus tracks the progress of an async model download.
type DownloadStatus struct {
	ID               string `json:"id"`
	Status           string `json:"status"`     // "downloading", "done", "error", "cancelled"
	Repo             string `json:"repo"`
	Target           string `json:"target,omitempty"`
	Progress         string `json:"progress,omitempty"`
	Percentage       float64 `json:"percentage"` // 0.0 to 100.0 — real-time progress
	Total            int64   `json:"total_bytes"`
	Downloaded       int64   `json:"downloaded_bytes"`
	SpeedBytesPerSec float64 `json:"speed_bytes_per_sec"` // moving-average download speed
	Error            string `json:"error,omitempty"`
	Filename         string `json:"filename,omitempty"`
	Source           string `json:"source"`
	DestPath         string `json:"-"`
	Cancel           context.CancelFunc `json:"-"`
}

// downloadTracker holds in-flight download statuses keyed by repo name.
var (
	downloadMu      sync.RWMutex
	downloadJobs    = make(map[string]*DownloadStatus)
	downloadIDCounter atomic.Int64
)

// nextDownloadID returns a unique identifier for a download job.
func nextDownloadID() string {
	return fmt.Sprintf("dl-%d", downloadIDCounter.Add(1))
}

// removePartialFiles deletes any partial/incomplete files left by a download.
func removePartialFiles(destPath string) {
	if destPath == "" {
		return
	}
	_ = os.Remove(destPath)
	_ = os.Remove(destPath + ".incomplete")
}

// findExistingDownload returns an in-flight download matching the request.
// For huggingface sources it matches by repo; for direct sources by URL.
func findExistingDownload(req DownloadRequest) *DownloadStatus {
	downloadMu.RLock()
	defer downloadMu.RUnlock()

	if req.Source == "huggingface" && req.Repo != "" {
		if job, ok := downloadJobs[req.Repo]; ok && job.Status == "downloading" {
			return job
		}
	}
	if req.Source == "direct" && req.URL != "" {
		if job, ok := downloadJobs[req.URL]; ok && job.Status == "downloading" {
			return job
		}
		// Also check dependency-style keys (filename@url).
		if req.Filename != "" {
			if job, ok := downloadJobs[req.Filename+"@"+req.URL]; ok && job.Status == "downloading" {
				return job
			}
		}
	}
	return nil
}

// handleModelsList scans the models directory and returns a JSON listing.
// GET /api/models/list
func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	resp := listModels()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// modelUsage counts actual model entries (weight files) and their on-disk
// bytes. It excludes auxiliary files such as configs, impulses, caches and
// download metadata. The result is used by the storage usage endpoint to
// report realistic model counts instead of raw file counts.
func modelUsage() (entries int64, bytes int64) {
	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath(), subdir)
		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if !modelExtensions[ext] {
				return nil
			}
			entries++
			bytes += info.Size()
			return nil
		})
	}
	return
}

// ModelsUploadsResponse is returned by GET /api/models/uploads.
type ModelsUploadsResponse struct {
	Models []ModelEntry `json:"models"`
}

// handleModelsUploads returns only models that were uploaded manually (origin == "upload").
// GET /api/models/uploads
func (s *Server) handleModelsUploads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	all := listModels()
	uploads := make([]ModelEntry, 0, len(all.Models))
	for _, m := range all.Models {
		if m.Origin == "upload" {
			uploads = append(uploads, m)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ModelsUploadsResponse{Models: uploads})
}

// listModels walks the model directories and builds a ModelsListResponse.
func listModels() ModelsListResponse {
	var models []ModelEntry
	categorySet := make(map[string]bool)

	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath(), subdir)

		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				// Skip inaccessible paths silently
				return nil
			}
			if info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if !modelExtensions[ext] {
				return nil
			}

			// Build path relative to the models root.
			rel, err := filepath.Rel(modelsBasePath(), path)
			if err != nil {
				rel = filepath.Join(subdir, info.Name())
			}
			modelPath := filepath.ToSlash(filepath.Join(modelsBasePath(), rel))

		name := strings.TrimSuffix(info.Name(), ext)
		installedName := name
		modelDir := filepath.Dir(path)
		if modelDir != filepath.Join(modelsBasePath(), subdir) {
			installedName = filepath.Base(modelDir)
		}
		category := detectCategory(subdir, rel)
		displayName := computeDisplayName(subdir, rel, name)
		modelType := ""
		var stems []string
		numStems := 0
		var target *string
		manifestMissing := true
		inferred := false
		origin := ""

		if manifest, ok := loadModelManifest(modelDir); ok {
			manifestMissing = false
			if manifest.Name != "" {
				displayName = manifest.Name
			}
			modelType = manifest.Type
			category = categoryFromType(modelType)
			stems = manifest.Stems.Stems
			numStems = manifest.Stems.NumStems
			target = manifest.Stems.Target
			inferred = manifest.Inferred
			origin = manifest.Origin
		}

		models = append(models, ModelEntry{
			Name:            name,
			InstalledName:   installedName,
			DisplayName:     displayName,
			Category:        category,
			Type:            modelType,
			Path:            modelPath,
			SizeMB:          info.Size() / (1024 * 1024),
			VramEstimateMB:  estimateVRAM(installedName, category, info.Size()/(1024*1024)),
			Stems:           stems,
			NumStems:        numStems,
			Target:          target,
			ManifestMissing: manifestMissing,
			Inferred:        inferred,
			Origin:          origin,
		})
			categorySet[category] = true
			return nil
		})
	}

	// Ensure htdemucs_ft is always listed as a Demucs model.
	// It's a PyTorch model loaded by the demucs CLI, not a file on disk,
	// so it won't be picked up by the filesystem scan.
	hasHtdemucsFT := false
	for _, m := range models {
		if m.Name == "htdemucs_ft" {
			hasHtdemucsFT = true
			break
		}
	}
	if !hasHtdemucsFT {
		models = append(models, ModelEntry{
			Name:            "htdemucs_ft",
			InstalledName:   "htdemucs_ft",
			DisplayName:     "HTDemucs FT",
			Category:        "Demucs",
			Type:            "demucs",
			Path:            "",
			SizeMB:          2800,
			VramEstimateMB:  2800,
			Stems:           []string{"drums", "bass", "other", "vocals"},
			NumStems:        4,
			ManifestMissing: true,
		})
		categorySet["Demucs"] = true
	}

	var categories []string
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	// If none found in subdirs, categories stays empty (not nil)
	if categories == nil {
		categories = []string{}
	}

	return ModelsListResponse{
		Models:     models,
		Categories: categories,
	}
}

// loadUVRCatalog reads and parses the UVR model catalog (uvr_models.json)
// from the application image, falling back to the data root.
func loadUVRCatalog() ([]UVRModelEntry, error) {
	data, err := readImageFile("uvr_models.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read uvr_models.json: %w", err)
	}
	var catalog []UVRModelEntry
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse uvr_models.json: %w", err)
	}
	return catalog, nil
}

// stripExtension tries each given extension and returns the base name
// (filename without extension) if a match is found. Returns the original
// filename unchanged if no extension matches.
func stripExtension(filename string, exts []string) string {
	lower := strings.ToLower(filename)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) && len(filename) > len(ext) {
			return filename[:len(filename)-len(ext)]
		}
	}
	return filename
}

// findDependencies searches the UVR catalog for dependency entries (size_mb=0)
// that share the same base name (after stripping extensions) as the given
// model filename. These are typically .yaml config files needed alongside
// model weights.
func findDependencies(modelFilename string, catalog []UVRModelEntry) []UVRModelEntry {
	modelBase := stripExtension(modelFilename, modelWeightExtensions)

	var deps []UVRModelEntry
	for _, entry := range catalog {
		if entry.SizeMB != 0 {
			continue
		}
		if entry.Filename == modelFilename {
			continue // skip self
		}
		entryBase := stripExtension(entry.Filename, dependencyExtensions)
		if entryBase == modelBase {
			deps = append(deps, entry)
		}
	}
	return deps
}

// handleModelsDownload initiates an async download from HuggingFace.
// POST /api/models/download
func (s *Server) handleModelsDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid JSON: %v", err),
		})
		return
	}

	if req.Source == "huggingface" {
		if req.Repo == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "repo is required for huggingface source",
			})
			return
		}

		// If the same repo is already downloading, return the existing job.
		if existing := findExistingDownload(req); existing != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(existing)
			return
		}

		// Determine target directory — Demucs_ONNX for ONNX repos, Demucs_Models otherwise
		targetSubdir := "Demucs_Models"
		if strings.Contains(strings.ToLower(req.Repo), "onnx") {
			targetSubdir = "Demucs_ONNX"
		}
		targetDir := filepath.Join(modelsBasePath(), targetSubdir)

		// Register the download job
		status := &DownloadStatus{
			ID:       nextDownloadID(),
			Status:   "downloading",
			Repo:     req.Repo,
			Target:   filepath.ToSlash(filepath.Join(modelsBasePath(), targetSubdir)),
			Filename: req.Filename,
			Source:   "huggingface",
		}
		downloadMu.Lock()
		downloadJobs[req.Repo] = status
		downloadMu.Unlock()

		// Launch async download
		go runHuggingFaceDownload(req.Repo, req.Filename, targetDir)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(status)
		return
	}

	if req.Source == "direct" {
		// Resolve the model from the UVR catalog when only a name or filename is provided.
		if req.URL == "" && (req.Name != "" || req.Filename != "") {
			catalog, err := loadUVRCatalog()
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("failed to load UVR catalog: %v", err),
				})
				return
			}
			lookup := req.Filename
			if lookup == "" {
				lookup = req.Name
			}
			var found *UVRModelEntry
			for i := range catalog {
				if catalog[i].Filename == lookup || catalog[i].Name == lookup {
					found = &catalog[i]
					break
				}
			}
			if found == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("model %q not found in catalog", lookup),
				})
				return
			}
			if found.DownloadURL == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("model %q has no direct download URL", found.Name),
				})
				return
			}
			req.URL = found.DownloadURL
			if req.Filename == "" {
				req.Filename = found.Filename
			}
		}

		if req.URL == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "url, filename or name is required for direct source",
			})
			return
		}
		if req.Filename == "" {
			// Derive filename from URL
			req.Filename = filepath.Base(req.URL)
		}

		// If the same URL is already downloading, return the existing job.
		if existing := findExistingDownload(req); existing != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(existing)
			return
		}

		// Determine category from filename if not provided
		category := req.Category
		if category == "" {
			category = detectCategoryFromFilename(req.Filename)
		}
		targetDir := filepath.Join(modelsBasePath(), category)

		// Register the download job keyed by URL
		status := &DownloadStatus{
			ID:       nextDownloadID(),
			Status:   "downloading",
			Repo:     req.URL,
			Target:   filepath.ToSlash(filepath.Join(modelsBasePath(), category)),
			Filename: req.Filename,
			Source:   "direct",
		}
		downloadMu.Lock()
		downloadJobs[req.URL] = status
		downloadMu.Unlock()

		// Launch async download
		go runDirectDownload(req.URL, req.Filename, targetDir)

		// Also download any dependency files (e.g., .yaml configs) that share
		// the same base name as the model being downloaded.
		if catalog, err := loadUVRCatalog(); err == nil {
			deps := findDependencies(req.Filename, catalog)
			for _, dep := range deps {
				if dep.DownloadURL == "" {
					continue
				}
				depCategory := detectCategoryFromFilename(dep.Filename)
				depDir := filepath.Join(modelsBasePath(), depCategory)

				// Register a download job for this dependency.
				// Use a composite key (filename + "@" + URL) to avoid collisions
				// when two models share the same dependency URL.
				depKey := req.Filename + "@" + dep.DownloadURL
				depStatus := &DownloadStatus{
					ID:       nextDownloadID(),
					Status:   "downloading",
					Repo:     depKey,
					Target:   filepath.ToSlash(filepath.Join(modelsBasePath(), depCategory)),
					Filename: dep.Filename,
					Source:   "direct",
				}
				downloadMu.Lock()
				// Do not start a duplicate dependency download.
				if _, exists := downloadJobs[depKey]; !exists {
					downloadJobs[depKey] = depStatus
					downloadMu.Unlock()
					go runDirectDownload(dep.DownloadURL, dep.Filename, depDir)
					log.Printf("[models] also downloading dependency: %s → %s", dep.Filename, dep.DownloadURL)
				} else {
					downloadMu.Unlock()
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{
		"error": fmt.Sprintf("unsupported source %q, expected 'huggingface' or 'direct'", req.Source),
	})
}

// handleModelsDownloadStatus returns the progress of a download job.
// GET /api/models/download/status?repo=... or ?url=...
func (s *Server) handleModelsDownloadStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	repo := r.URL.Query().Get("repo")
	url := r.URL.Query().Get("url")

	var job *DownloadStatus
	downloadMu.RLock()
	if repo != "" {
		job = downloadJobs[repo]
		// HF pair downloads are keyed as repo#weightPath; allow lookup by repo.
		if job == nil {
			for key, candidate := range downloadJobs {
				if strings.HasPrefix(key, repo+"#") {
					job = candidate
					break
				}
			}
		}
	}
	if job == nil && url != "" {
		job = downloadJobs[url]
	}
	// Dependencies are keyed as filename@url; fall back if the caller only has the URL.
	if job == nil && url != "" {
		for key, candidate := range downloadJobs {
			if strings.HasSuffix(key, "@"+url) {
				job = candidate
				break
			}
		}
	}
	downloadMu.RUnlock()

	if job == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		lookup := repo
		if lookup == "" {
			lookup = url
		}
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("no download job found for %q", lookup),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

// handleModelsDownloadCancel cancels an in-progress download and cleans up
// any partial files. DELETE /api/models/download?repo=... or ?url=...
func (s *Server) handleModelsDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	repo := r.URL.Query().Get("repo")
	url := r.URL.Query().Get("url")

	downloadMu.Lock()
	var job *DownloadStatus
	if repo != "" {
		job = downloadJobs[repo]
		// HF pair downloads are keyed as repo#weightPath; allow lookup by repo.
		if job == nil {
			for key, candidate := range downloadJobs {
				if strings.HasPrefix(key, repo+"#") {
					job = candidate
					break
				}
			}
		}
	}
	if job == nil && url != "" {
		job = downloadJobs[url]
	}
	if job == nil && url != "" {
		for key, candidate := range downloadJobs {
			if strings.HasSuffix(key, "@"+url) {
				job = candidate
				break
			}
		}
	}

	if job == nil {
		downloadMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		lookup := repo
		if lookup == "" {
			lookup = url
		}
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("no download job found for %q", lookup),
		})
		return
	}

	// If already finished, just return the current state.
	if job.Status != "downloading" {
		downloadMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(job)
		return
	}

	// Cancel the context, clean up partial files and mark as cancelled.
	if job.Cancel != nil {
		job.Cancel()
	}
	job.Status = "cancelled"
	job.Progress = "Cancelled"
	job.Error = "cancelled by user"
	job.Percentage = 0
	downloadMu.Unlock()

	removePartialFiles(job.DestPath)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

// runHuggingFaceDownload executes a HuggingFace download. When filename is
// provided it downloads that single file via plain HTTP; otherwise it falls back
// to huggingface_hub snapshot_download for the whole repo.
func runHuggingFaceDownload(repo, filename, targetDir string) {
	if filename != "" {
		runHuggingFaceFileDownload(repo, filename, targetDir)
		return
	}
	runHuggingFaceRepoDownload(repo, targetDir)
}

const hfResolveBase = "https://huggingface.co"

// hfResolveURL builds a HF raw-file URL for a repo and relative path.
func hfResolveURL(repo, filename string) string {
	escape := func(segments []string) []string {
		out := make([]string, len(segments))
		for i, s := range segments {
			out[i] = url.PathEscape(s)
		}
		return out
	}
	repoParts := escape(strings.Split(repo, "/"))
	fileParts := escape(strings.Split(filename, "/"))
	return fmt.Sprintf("%s/%s/resolve/main/%s", hfResolveBase, strings.Join(repoParts, "/"), strings.Join(fileParts, "/"))
}

// runHuggingFaceFileDownload downloads a single file from a HuggingFace repo
// using plain HTTP. This avoids pulling the entire repo and gives real progress.
func runHuggingFaceFileDownload(repo, filename, targetDir string) {
	destPath := filepath.Join(targetDir, filename)
	url := hfResolveURL(repo, filename)

	log.Printf("[models] downloading HF file %s → %s", url, destPath)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	downloadMu.Lock()
	if status, ok := downloadJobs[repo]; ok {
		status.Cancel = cancel
		status.DestPath = destPath
	}
	downloadMu.Unlock()

	if err := downloadWithProgress(ctx, url, destPath, repo); err != nil {
		handleDownloadFinished(repo, err, destPath)
	}
}

// runHuggingFaceRepoDownload downloads a whole HF repo using huggingface_hub.
// It is the fallback when no specific file is requested. The temporary script
// is created under the data root, never under /tmp, and cleaned up on exit.
func runHuggingFaceRepoDownload(repo, targetDir string) {
	tmpDir, err := os.MkdirTemp(dataRoot(), "onda-hf-repo-*")
	if err != nil {
		updateDownloadErrorByRepo(repo, fmt.Sprintf("failed to create temp dir: %v", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "download.py")
	scriptContent := `import sys, json, os
from huggingface_hub import snapshot_download

repo = sys.argv[1]
target = sys.argv[2]
os.makedirs(target, exist_ok=True)

try:
    result = snapshot_download(repo, local_dir=target, resume_download=True)
    print(json.dumps({"status": "done", "path": result}), flush=True)
except Exception as e:
    print(json.dumps({"status": "error", "error": str(e)}), flush=True)
    sys.exit(1)
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
		updateDownloadErrorByRepo(repo, fmt.Sprintf("failed to write HF download script: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", scriptPath, repo, targetDir)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		updateDownloadErrorByRepo(repo, fmt.Sprintf("failed to get stderr pipe: %v", err))
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		updateDownloadErrorByRepo(repo, fmt.Sprintf("failed to get stdout pipe: %v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		updateDownloadErrorByRepo(repo, fmt.Sprintf("failed to start HF download: %v", err))
		return
	}

	hfPercentRe := regexp.MustCompile(`(\d+)%\s*\|`)
	hfBytesRe := regexp.MustCompile(`\|?\s*([\d.]+)/([\d.]+)\s*(B|[KMGT]i?B?/s?)`)

	type scriptResult struct {
		Status string `json:"status"`
		Path   string `json:"path"`
		Error  string `json:"error"`
	}
	resultCh := make(chan scriptResult, 1)
	go func() {
		defer close(resultCh)
		stdoutBuf, _ := io.ReadAll(stdout)
		var res scriptResult
		if err := json.Unmarshal(stdoutBuf, &res); err != nil {
			res.Status = "error"
			res.Error = fmt.Sprintf("failed to parse script output: %v", err)
		}
		resultCh <- res
	}()

	stderrCh := make(chan struct{}, 1)
	go func() {
		defer close(stderrCh)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 4096), 4096)
		for scanner.Scan() {
			line := scanner.Text()

			pctMatch := hfPercentRe.FindStringSubmatch(line)
			if pctMatch == nil {
				continue
			}
			pct, _ := strconv.ParseFloat(pctMatch[1], 64)

			var downloaded, total int64
			bytesMatch := hfBytesRe.FindStringSubmatch(line)
			if len(bytesMatch) >= 4 {
				dlVal, _ := strconv.ParseFloat(bytesMatch[1], 64)
				totalVal, _ := strconv.ParseFloat(bytesMatch[2], 64)
				unit := bytesMatch[3]

				var multiplier int64 = 1
				switch {
				case strings.HasPrefix(unit, "K"):
					multiplier = 1024
				case strings.HasPrefix(unit, "M"):
					multiplier = 1024 * 1024
				case strings.HasPrefix(unit, "G"):
					multiplier = 1024 * 1024 * 1024
				}

				downloaded = int64(dlVal * float64(multiplier))
				total = int64(totalVal * float64(multiplier))
			}

			downloadMu.Lock()
			if status, ok := downloadJobs[repo]; ok {
				status.Percentage = pct
				status.Downloaded = downloaded
				if total > 0 {
					status.Total = total
				} else if downloaded > status.Total {
					status.Total = downloaded
				}
			}
			downloadMu.Unlock()
		}
	}()

	waitErr := cmd.Wait()

	// Consume remaining stderr
	<-stderrCh

	// Get the final result from stdout
	res := <-resultCh

	downloadMu.Lock()

	status, ok := downloadJobs[repo]
	if !ok {
		downloadMu.Unlock()
		log.Printf("[models] no download job found for repo %q", repo)
		return
	}

	if waitErr != nil || res.Status == "error" {
		status.Status = "error"
		status.Progress = "Download failed"
		errMsg := res.Error
		if errMsg == "" && waitErr != nil {
			errMsg = waitErr.Error()
		}
		status.Error = errMsg
		log.Printf("[models] download error for %s: %s", repo, errMsg)

		// If python3 fails, try installing huggingface_hub and retry
		if strings.Contains(errMsg, "No module named") || strings.Contains(errMsg, "ModuleNotFoundError") {
			log.Printf("[models] huggingface_hub not found — trying pip install")
			downloadMu.Unlock()
			tryInstallAndRetryHF(repo, targetDir, scriptPath)
			return
		}
	} else {
		status.Status = "done"
		status.Progress = "Download complete"
		status.Percentage = 100
		log.Printf("[models] download complete for %s", repo)
	}

	downloadMu.Unlock()
}

// tryInstallAndRetryHF installs huggingface_hub via pip and retries the download.
func tryInstallAndRetryHF(repo, targetDir, scriptPath string) {
	installCtx, installCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer installCancel()
	installCmd := exec.CommandContext(installCtx, "pip", "install", "huggingface_hub")
	if installOutput, installErr := installCmd.CombinedOutput(); installErr != nil {
		log.Printf("[models] pip install failed: %v — output: %s", installErr, string(installOutput))
		downloadMu.Lock()
		if status, ok := downloadJobs[repo]; ok {
			status.Status = "error"
			status.Progress = "Download failed"
			status.Error = fmt.Sprintf("pip install failed: %v", installErr)
		}
		downloadMu.Unlock()
		return
	}

	// Retry download using the same script
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer retryCancel()
	cmd := exec.CommandContext(retryCtx, "python3", scriptPath, repo, targetDir)
	output, err := cmd.CombinedOutput()

	downloadMu.Lock()
	defer downloadMu.Unlock()

	status, ok := downloadJobs[repo]
	if !ok {
		log.Printf("[models] no download job found for repo %q after retry", repo)
		return
	}

	if err != nil {
		status.Status = "error"
		status.Progress = "Download failed"
		errMsg := err.Error()
		if len(output) > 0 {
			errMsg = string(output)
		}
		status.Error = errMsg
		log.Printf("[models] download error (retry) for %s: %s", repo, errMsg)
	} else {
		status.Status = "done"
		status.Progress = "Download complete"
		status.Percentage = 100
		log.Printf("[models] download complete (retry) for %s", repo)
	}
}

// downloadWithProgress downloads url to destPath, updating downloadJobs[key]
// with real-time bytes, percentage and moving-average speed. The destination is
// written atomically via a .incomplete sibling file that is removed on failure
// or cancellation.
func downloadWithProgress(ctx context.Context, url, destPath, key string) error {
	return downloadWithProgressAuth(ctx, url, destPath, key, "", true)
}

// downloadWithProgressAuth is the token-aware core of downloadWithProgress.
// If authHeader is non-empty it is sent as the Authorization header.
// When markDone is false the status is left as "downloading" so the caller
// can finalize it after any additional work (e.g. downloading a config file).
func downloadWithProgressAuth(ctx context.Context, url, destPath, key, authHeader string, markDone bool) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}

	tmpPath := destPath + ".incomplete"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	var downloaded atomic.Int64
	stopUpdate := make(chan struct{})
	updateDone := make(chan struct{})

	// speed samples maintains a short sliding window of (bytes, timestamp) for
	// a smooth moving-average speed estimate.
	type speedSample struct {
		bytes int64
		t     time.Time
	}
	const speedWindow = 2 * time.Second
	var samplesMu sync.Mutex
	var samples []speedSample
	startTime := time.Now()

	go func() {
		defer close(updateDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopUpdate:
				return
			case <-ticker.C:
				dl := downloaded.Load()
				now := time.Now()

				samplesMu.Lock()
				samples = append(samples, speedSample{bytes: dl, t: now})
				cutoff := now.Add(-speedWindow)
				for len(samples) > 0 && samples[0].t.Before(cutoff) {
					samples = samples[1:]
				}
				var speed float64
				if len(samples) > 1 {
					deltaBytes := float64(samples[len(samples)-1].bytes - samples[0].bytes)
					deltaSecs := samples[len(samples)-1].t.Sub(samples[0].t).Seconds()
					if deltaSecs > 0 {
						speed = deltaBytes / deltaSecs
					}
				} else if elapsed := now.Sub(startTime).Seconds(); elapsed > 0 {
					speed = float64(dl) / elapsed
				}
				samplesMu.Unlock()

				downloadMu.Lock()
				if status, ok := downloadJobs[key]; ok {
					status.Downloaded = dl
					status.SpeedBytesPerSec = speed
					if total > 0 {
						pct := float64(dl*100) / float64(total)
						if pct > 100 {
							pct = 100
						}
						if pct < 0 {
							pct = 0
						}
						status.Percentage = pct
						status.Total = total
					} else {
						status.Total = dl
					}
				}
				downloadMu.Unlock()
			}
		}
	}()

	cleanup := func() {
		f.Close()
		removePartialFiles(destPath)
		close(stopUpdate)
		<-updateDone
	}

	buf := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			cleanup()
			return err
		}

		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			nw, werr := f.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				downloaded.Add(int64(nw))
			}
			if werr != nil {
				cleanup()
				return fmt.Errorf("write failed: %w", werr)
			}
			if nr != nw {
				cleanup()
				return io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				cleanup()
				return fmt.Errorf("download failed: %w", rerr)
			}
			break
		}
	}

	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("failed to close file: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		cleanup()
		return fmt.Errorf("failed to finalize file: %w", err)
	}

	// If the user cancelled just before the rename finished, remove the final file.
	if ctx.Err() != nil {
		removePartialFiles(destPath)
		close(stopUpdate)
		<-updateDone
		return ctx.Err()
	}

	close(stopUpdate)
	<-updateDone

	// Final speed: total bytes over total elapsed time.
	var finalSpeed float64
	if elapsed := time.Since(startTime).Seconds(); elapsed > 0 {
		finalSpeed = float64(written) / elapsed
	}

	downloadMu.Lock()
	if status, ok := downloadJobs[key]; ok {
		status.Downloaded = written
		status.SpeedBytesPerSec = finalSpeed
		if total > 0 {
			status.Total = total
		} else {
			status.Total = written
		}
		if markDone {
			status.Status = "done"
			status.Progress = "Download complete"
			status.Percentage = 100
		}
	}
	downloadMu.Unlock()

	return nil
}

// detectCategoryFromFilename determines the model category directory from the
// filename using keyword matching. This mirrors how UVR organizes its models.
// Mapping: roformer/melband → VR_Models, mdx/mdx23c → MDX_Net_Models,
// demucs/htdemucs → Demucs_Models, scnet → VR_Models.
func detectCategoryFromFilename(filename string) string {
	lower := strings.ToLower(filename)

	// SCnet models go to VR_Models
	if strings.Contains(lower, "scnet") {
		return "VR_Models"
	}
	// Roformer-based models (MelBand, Bandit, etc.) go to VR_Models.
	// ViperX is not a category; a model named BS_Roformer_Viperx is already
	// matched by the "roformer" check.
	if strings.Contains(lower, "roformer") ||
		strings.Contains(lower, "melband") ||
		strings.Contains(lower, "mel_band") ||
		strings.Contains(lower, "bandit") ||
		strings.Contains(lower, "deverb") {
		return "VR_Models"
	}
	// MDX models
	if strings.Contains(lower, "mdx") {
		return "MDX_Net_Models"
	}
	// Demucs models (htdemucs, demucs, tasnet, etc.)
	if strings.Contains(lower, "demucs") ||
		strings.Contains(lower, "htdemucs") ||
		strings.Contains(lower, "hdemucs") ||
		strings.Contains(lower, "tasnet") ||
		strings.Contains(lower, "light") ||
		strings.Contains(lower, "repro_mdx") {
		return "Demucs_Models"
	}

	// MDXNet ONNX models are classified under the MDX_Net_Models directory.
	if strings.HasSuffix(lower, ".onnx") {
		return "MDX_Net_Models"
	}

	// Default fallback
	return "VR_Models"
}

// handleDeleteModel deletes a model file from /models/ and its config JSON.
// DELETE /api/models/{name}
func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.PathValue("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing model name"})
		return
	}

	// Sanitize the name for safety: replace path separators
	safeName := strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_").Replace(name)

	// Find the model file on disk
	var foundPath string
	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath(), subdir)
		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if !modelExtensions[ext] {
				return nil
			}
			modelName := strings.TrimSuffix(info.Name(), ext)
			if modelName == safeName || modelName == name {
				foundPath = path
				return filepath.SkipAll
			}
			return nil
		})
		if foundPath != "" {
			break
		}
	}

	// Also check with display name matching (filepath.Base of parent dir)
	if foundPath == "" {
		for _, subdir := range modelSubdirs {
			dirPath := filepath.Join(modelsBasePath(), subdir)
			_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(info.Name()))
				if !modelExtensions[ext] {
					return nil
				}
				parentDir := filepath.Base(filepath.Dir(path))
				if parentDir == name || parentDir == safeName {
					foundPath = path
					return filepath.SkipAll
				}
				return nil
			})
			if foundPath != "" {
				break
			}
		}
	}

	deletedFiles := false
	var fileBytes int64

	// Delete the model file if found
	if foundPath != "" {
		if info, err := os.Stat(foundPath); err == nil {
			fileBytes = info.Size()
		}
		if err := os.Remove(foundPath); err != nil {
			log.Printf("[models] failed to delete model file %s: %v", foundPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to delete model file: %v", err)})
			return
		}
		deletedFiles = true

		// Clean up the model directory: manifest, sidecar configs and the dir itself.
		modelDir := filepath.Dir(foundPath)
		modelBase := strings.TrimSuffix(filepath.Base(foundPath), filepath.Ext(foundPath))
		_ = os.Remove(filepath.Join(modelDir, "model.manifest.json"))
		if entries, err := os.ReadDir(modelDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				entryName := entry.Name()
				entryExt := strings.ToLower(filepath.Ext(entryName))
				entryBase := strings.TrimSuffix(entryName, entryExt)
				isAux := entryExt == ".yaml" || entryExt == ".yml" || entryExt == ".json" || entryExt == ".txt"
				if isAux && (entryBase == modelBase || entryBase == "model") {
					_ = os.Remove(filepath.Join(modelDir, entryName))
				}
			}
		}
		// Remove the directory if it only contains auxiliary files or is empty.
		if isDirEmptyOrOnlyAux(modelDir) {
			_ = os.RemoveAll(modelDir)
		}
	}

	if !deletedFiles {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("model %q not found on disk", name)})
		return
	}

	relName := foundPath
	if projectRoot := dataRoot(); projectRoot != "" {
		if r, err := filepath.Rel(projectRoot, foundPath); err == nil {
			relName = r
		}
	}
	logDeletion(r, "model", relName, 1, fileBytes)

	resp := map[string]interface{}{
		"ok":     true,
		"detail": fmt.Sprintf("model %q deleted", name),
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// isDirEmptyOrOnlyAux reports whether dir contains no files or only auxiliary
// non-model files (configs, manifests, text files).
func isDirEmptyOrOnlyAux(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return false
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" && ext != ".txt" && name != "model.manifest.json" {
			return false
		}
	}
	return true
}

// runDirectDownload downloads a model file from a direct URL using Go's net/http,
// streaming to disk and reporting real-time progress.
func runDirectDownload(url, filename, targetDir string) {
	destPath := filepath.Join(targetDir, filename)
	log.Printf("[models] downloading %s → %s", url, destPath)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	key := directDownloadKey(url, filename)
	downloadMu.Lock()
	if status, ok := downloadJobs[key]; ok {
		status.Cancel = cancel
		status.DestPath = destPath
	}
	downloadMu.Unlock()

	if err := downloadWithProgress(ctx, url, destPath, key); err != nil {
		handleDownloadFinished(key, err, destPath)
	}
}

// handleDownloadFinished updates a download job after the goroutine terminates.
// It distinguishes user cancellation from real errors and cleans up leftovers.
func handleDownloadFinished(key string, err error, destPath string) {
	downloadMu.Lock()
	defer downloadMu.Unlock()

	status, ok := downloadJobs[key]
	if !ok || status == nil {
		return
	}
	// If the user already cancelled the job via the API, keep that state.
	if status.Status == "cancelled" {
		removePartialFiles(destPath)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status.Status = "cancelled"
		status.Progress = "Cancelled"
		status.Error = "cancelled by user"
		status.Percentage = 0
		removePartialFiles(destPath)
		return
	}
	status.Status = "error"
	status.Progress = "Download failed"
	status.Error = err.Error()
}

// directDownloadKey returns the composite key if it exists, otherwise the plain URL.
func directDownloadKey(url, filename string) string {
	downloadMu.RLock()
	defer downloadMu.RUnlock()
	if _, ok := downloadJobs[filename+"@"+url]; ok {
		return filename + "@" + url
	}
	return url
}

// updateDownloadErrorByRepo sets error status on a HF download job.
func updateDownloadErrorByRepo(repo, errMsg string) {
	downloadMu.Lock()
	defer downloadMu.Unlock()
	if status, ok := downloadJobs[repo]; ok {
		status.Status = "error"
		status.Progress = "Download failed"
		status.Error = errMsg
	}
}
