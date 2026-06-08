package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// modelsBasePath is the root directory where models live inside the container.
// Both onda and onda-gui use /models (bind-mounted from host).
const modelsBasePath = "/models"

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
	"MDX_Net_Models":  "MDX",
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
		modelDir := strings.ToLower(parts[1])
		switch {
		case strings.Contains(modelDir, "roformer") || strings.Contains(modelDir, "viperx"):
			return "Roformer"
		case strings.Contains(modelDir, "melband"):
			return "Roformer/MelBand"
		case strings.Contains(modelDir, "scnet"):
			return "SCnet"
		}
	}
	return baseCat
}

// computeDisplayName derives a human-friendly display name from the file's
// relative path and its parent directory structure.
func computeDisplayName(subdir, rel, name string) string {
	parentDir := filepath.Base(filepath.Dir(rel))
	if parentDir == subdir {
		// File sits directly in the category directory (no model-specific subdir).
		// This happens for Demucs ONNX stems: htdemucs_ft_vocals → "htdemucs_ft (vocals)"
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
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Category       string `json:"category"`
	Path           string `json:"path"`
	SizeMB         int64  `json:"size_mb"`
	VramEstimateMB int64  `json:"vram_estimate_mb"`
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
}

// DownloadStatus tracks the progress of an async model download.
type DownloadStatus struct {
	Status   string `json:"status"`   // "downloading", "done", "error"
	Repo     string `json:"repo"`
	Target   string `json:"target,omitempty"`
	Progress string `json:"progress,omitempty"`
	Error    string `json:"error,omitempty"`
}

// downloadTracker holds in-flight download statuses keyed by repo name.
var (
	downloadMu      sync.RWMutex
	downloadJobs    = make(map[string]*DownloadStatus)
)

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

// listModels walks the model directories and builds a ModelsListResponse.
func listModels() ModelsListResponse {
	var models []ModelEntry
	categorySet := make(map[string]bool)

	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath, subdir)

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

			// Build path relative to /models/ for Docker access
			rel, err := filepath.Rel(modelsBasePath, path)
			if err != nil {
				rel = filepath.Join(subdir, info.Name())
			}
			dockerPath := "/models/" + filepath.ToSlash(rel)

			name := strings.TrimSuffix(info.Name(), ext)
			category := detectCategory(subdir, rel)
			displayName := computeDisplayName(subdir, rel, name)

			models = append(models, ModelEntry{
				Name:           name,
				DisplayName:    displayName,
				Category:       category,
				Path:           dockerPath,
				SizeMB:         info.Size() / (1024 * 1024),
				VramEstimateMB: estimateVRAM(name, category, info.Size()/(1024*1024)),
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
			Name:           "htdemucs_ft",
			DisplayName:    "HTDemucs FT",
			Category:       "Demucs",
			Path:           "",
			SizeMB:         2800,
			VramEstimateMB: 2800,
		})
		categorySet["Demucs"] = true
	}

	var categories []string
	for _, cat := range []string{"VR_Arch", "MDX", "Roformer", "Roformer/MelBand", "SCnet", "Demucs", "Demucs ONNX"} {
		if categorySet[cat] {
			categories = append(categories, cat)
		}
	}
	// If none found in subdirs, categories stays empty (not nil)
	if categories == nil {
		categories = []string{}
	}

	return ModelsListResponse{
		Models:     models,
		Categories: categories,
	}
}

// loadUVRCatalog reads and parses the UVR model catalog (uvr_models.json).
// It tries /app/uvr_models.json first (container path), then falls back to
// the project root.
func loadUVRCatalog() ([]UVRModelEntry, error) {
	data, err := os.ReadFile("/app/uvr_models.json")
	if err != nil {
		projectRoot := findProjectRoot()
		data, err = os.ReadFile(filepath.Join(projectRoot, "uvr_models.json"))
		if err != nil {
			return nil, fmt.Errorf("failed to read uvr_models.json: %w", err)
		}
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

		// Determine target directory — Demucs_ONNX for ONNX repos, Demucs_Models otherwise
		targetSubdir := "Demucs_Models"
		if strings.Contains(strings.ToLower(req.Repo), "onnx") {
			targetSubdir = "Demucs_ONNX"
		}
		targetDir := filepath.Join(modelsBasePath, targetSubdir)

		// Register the download job
		status := &DownloadStatus{
			Status: "downloading",
			Repo:   req.Repo,
			Target: "/models/" + targetSubdir,
		}
		downloadMu.Lock()
		downloadJobs[req.Repo] = status
		downloadMu.Unlock()

		// Launch async download
		go runHuggingFaceDownload(req.Repo, targetDir)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(status)
		return
	}

	if req.Source == "direct" {
		if req.URL == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "url is required for direct source",
			})
			return
		}
		if req.Filename == "" {
			// Derive filename from URL
			req.Filename = filepath.Base(req.URL)
		}

		// Determine category from filename if not provided
		category := req.Category
		if category == "" {
			category = detectCategoryFromFilename(req.Filename)
		}
		targetDir := filepath.Join(modelsBasePath, category)

		// Register the download job keyed by URL
		status := &DownloadStatus{
			Status: "downloading",
			Repo:   req.URL,
			Target: "/models/" + category,
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
				depDir := filepath.Join(modelsBasePath, depCategory)

				// Register a download job for this dependency.
				// Use a composite key (filename + "@" + URL) to avoid collisions
				// when two models share the same dependency URL.
				depKey := req.Filename + "@" + dep.DownloadURL
				depStatus := &DownloadStatus{
					Status: "downloading",
					Repo:   depKey,
					Target: "/models/" + depCategory,
				}
				downloadMu.Lock()
				downloadJobs[depKey] = depStatus
				downloadMu.Unlock()

				go runDirectDownload(dep.DownloadURL, dep.Filename, depDir)
				log.Printf("[models] also downloading dependency: %s → %s", dep.Filename, dep.DownloadURL)
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
// GET /api/models/download/status?repo=...
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
	if repo == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "query parameter 'repo' is required",
		})
		return
	}

	downloadMu.RLock()
	job, ok := downloadJobs[repo]
	downloadMu.RUnlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("no download job found for repo %q", repo),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

// runHuggingFaceDownload executes the huggingface_hub snapshot_download in a background goroutine.
func runHuggingFaceDownload(repo, targetDir string) {
	// Escape single quotes to prevent Python code injection.
	// python3 -c receives the script directly (no shell), so we use
	// Python string escaping: \' inside single-quoted string.
	safeRepo := strings.ReplaceAll(repo, "'", "\\'")
	safeTarget := strings.ReplaceAll(targetDir, "'", "\\'")
	script := fmt.Sprintf(
		"from huggingface_hub import snapshot_download; snapshot_download('%s', local_dir='%s')",
		safeRepo, safeTarget,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// Try with python3 first
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// If python3 fails, try installing huggingface_hub and retry
		log.Printf("[models] python3 download attempt failed: %v — trying pip install", err)

		installCtx, installCancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer installCancel()
		installCmd := exec.CommandContext(installCtx, "pip", "install", "huggingface_hub")
		if installOutput, installErr := installCmd.CombinedOutput(); installErr != nil {
			log.Printf("[models] pip install failed: %v — output: %s", installErr, string(installOutput))
		}

		// Retry download
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer retryCancel()
		cmd2 := exec.CommandContext(retryCtx, "python3", "-c", script)
		output, err = cmd2.CombinedOutput()
	}

	downloadMu.Lock()
	defer downloadMu.Unlock()

	status := downloadJobs[repo]
	if err != nil {
		status.Status = "error"
		status.Progress = "Download failed"
		errStr := err.Error()
		if len(output) > 0 {
			errStr = string(output)
		}
		status.Error = errStr
		log.Printf("[models] download error for %s: %s", repo, errStr)
	} else {
		status.Status = "done"
		status.Progress = "Download complete"
		log.Printf("[models] download complete for %s", repo)
	}
}

// detectCategoryFromFilename determines the model category directory from the
// filename using keyword matching. This mirrors how UVR organizes its models.
// Mapping: roformer/viperx/melband → VR_Models, mdx/mdx23c → MDX_Net_Models,
// demucs/htdemucs → Demucs_Models, scnet → VR_Models.
func detectCategoryFromFilename(filename string) string {
	lower := strings.ToLower(filename)

	// SCnet models go to VR_Models
	if strings.Contains(lower, "scnet") {
		return "VR_Models"
	}
	// Roformer-based models (including ViperX, MelBand, Bandit) go to VR_Models
	if strings.Contains(lower, "roformer") ||
		strings.Contains(lower, "viperx") ||
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

	// Sanitize the name for safety
	safeName := sanitizeModelName(name)

	// Find the model file on disk
	var foundPath string
	for _, subdir := range modelSubdirs {
		dirPath := filepath.Join(modelsBasePath, subdir)
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
			dirPath := filepath.Join(modelsBasePath, subdir)
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

	// Delete the model file if found
	if foundPath != "" {
		if err := os.Remove(foundPath); err != nil {
			log.Printf("[models] failed to delete model file %s: %v", foundPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to delete model file: %v", err)})
			return
		}
		log.Printf("[models] deleted model file: %s", foundPath)
		deletedFiles = true
	}

	// Delete the config JSON if it exists
	configPath := modelConfigPath(safeName)
	var configWarning string
	if _, err := os.Stat(configPath); err == nil {
		if err := os.Remove(configPath); err != nil {
			log.Printf("[models] failed to delete config %s: %v", configPath, err)
			configWarning = fmt.Sprintf("model deleted but config cleanup failed: %v", err)
		} else {
			log.Printf("[models] deleted config: %s", configPath)
		}
	}

	if !deletedFiles {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("model %q not found on disk", name)})
		return
	}

	resp := map[string]interface{}{
		"ok":     true,
		"detail": fmt.Sprintf("model %q deleted", name),
	}
	if configWarning != "" {
		resp["warning"] = configWarning
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// runDirectDownload downloads a model file from a direct URL using wget.
func runDirectDownload(url, filename, targetDir string) {
	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Printf("[models] failed to create target dir %s: %v", targetDir, err)
		downloadMu.Lock()
		// Try composite key (filename@url) first, then plain URL as fallback.
		if status, ok := downloadJobs[filename+"@"+url]; ok {
			status.Status = "error"
			status.Error = fmt.Sprintf("failed to create target directory: %v", err)
		} else if status, ok := downloadJobs[url]; ok {
			status.Status = "error"
			status.Error = fmt.Sprintf("failed to create target directory: %v", err)
		}
		downloadMu.Unlock()
		return
	}

	destPath := filepath.Join(targetDir, filename)
	log.Printf("[models] downloading %s → %s", url, destPath)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	// Use wget with progress output to stderr
	cmd := exec.CommandContext(ctx, "wget", "-q", "--show-progress", "-O", destPath, url)
	output, err := cmd.CombinedOutput()

	downloadMu.Lock()
	defer downloadMu.Unlock()

	// Try composite key (filename@url) first, then plain URL as fallback.
	// Composite key is used for dependency downloads to avoid key collisions.
	status, ok := downloadJobs[filename+"@"+url]
	if !ok {
		status, ok = downloadJobs[url]
	}
	if !ok {
		log.Printf("[models] no download job found for %s (url=%s)", filename, url)
		return
	}
	if err != nil {
		status.Status = "error"
		status.Progress = "Download failed"
		errStr := err.Error()
		if len(output) > 0 {
			errStr = string(output)
		}
		status.Error = errStr
		log.Printf("[models] direct download error for %s: %s", url, errStr)
	} else {
		status.Status = "done"
		status.Progress = "Download complete"
		log.Printf("[models] direct download complete for %s → %s", url, destPath)
	}
}
