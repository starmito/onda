package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DownloadHFRequest is the JSON body for POST /api/models/download-hf.
// It carries the candidate pair chosen by the caller from /api/models/resolve.
type DownloadHFRequest struct {
	Repo      string             `json:"repo"`
	Branch    string             `json:"branch,omitempty"`
	Nombre    string             `json:"nombre"`
	Tipo      string             `json:"tipo"`
	Candidato ResolveHFCandidate `json:"candidato"`
}

// sanitizeName strips unsafe characters from a model name so it can be used as
// a directory name and a config key.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	repl := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"..", "_",
		"\x00", "",
		" ", "_",
	)
	name = repl.Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "model"
	}
	return name
}

// hfTypeToSubdir maps the detected HF model type to the on-disk category root.
func hfTypeToSubdir(modelType string) string {
	switch modelType {
	case "htdemucs":
		return "Demucs_Models"
	case "mdx23c":
		return "MDX_Net_Models"
	default:
		// Roformer-based models (bs_roformer, mel_band_roformer, scnet, ...)
		// and any unknown type default to VR_Models, matching the existing layout.
		return "VR_Models"
	}
}

// targetDirForHFModel returns the directory where a HuggingFace-downloaded
// model should be stored. For Demucs models the repo's internal path under
// models/Demucs is preserved; for other types a model-specific folder is used.
func targetDirForHFModel(modelType, nombre, weightPath string) string {
	base := modelsBasePath()
	subdir := hfTypeToSubdir(modelType)

	if subdir == "Demucs_Models" {
		// Preserve the HuggingFace repo structure under Demucs_Models when the
		// weight already lives under models/Demucs (e.g. models/Demucs/Demucs_v4/...).
		lowerPath := strings.ToLower(weightPath)
		prefix := "models/demucs/"
		if strings.HasPrefix(lowerPath, prefix) {
			rel := filepath.Dir(strings.TrimPrefix(weightPath, "models/Demucs/"))
			return filepath.Join(base, "Demucs_Models", "models", "Demucs", rel)
		}
		return filepath.Join(base, "Demucs_Models", "models", "Demucs", "Demucs_v4")
	}

	return filepath.Join(base, subdir, sanitizeName(nombre))
}

// weightDestPath decides the final filename for the downloaded weight.
func weightDestPath(targetDir, nombre, weightPath string) string {
	base := filepath.Base(weightPath)
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" {
		ext = ".ckpt"
	}
	return filepath.Join(targetDir, sanitizeName(nombre)+ext)
}

// configDestPath returns the path where the architecture YAML should be saved.
// It mirrors the weight filename so the model directory contains both files.
func configDestPath(targetDir, nombre string, configPath string) string {
	if configPath == "" {
		return ""
	}
	ext := filepath.Ext(configPath)
	if ext == "" {
		ext = ".yaml"
	}
	return filepath.Join(targetDir, sanitizeName(nombre)+ext)
}

// writeModelConfigJSON writes the sanitized inference configuration to
// config/model_configs/<name>.json so the pipeline can read it without
// parsing YAML at runtime.
func writeModelConfigJSON(name string, cfg ModelConfigResponse) error {
	path := uvrModelConfigJSONPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create model config directory: %w", err)
	}

	numOverlap := 4
	if cfg.Overlap > 0 && cfg.Overlap < 1 {
		numOverlap = int(math.Round(1.0 / cfg.Overlap))
	}
	if numOverlap < 1 {
		numOverlap = 1
	}

	payload := map[string]interface{}{
		"segment_size": cfg.SegmentSize,
		"overlap":      cfg.Overlap,
		"chunk_size":   cfg.ChunkSize,
		"batch_size":   cfg.BatchSize,
		"device":       cfg.Device,
		"dim_t":        cfg.SegmentSize,
		"num_overlap":  numOverlap,
	}
	if cfg.Shifts > 0 {
		payload["shifts"] = cfg.Shifts
	}
	if cfg.Segment > 0 {
		payload["segment"] = cfg.Segment
	}
	if cfg.Jobs > 0 {
		payload["jobs"] = cfg.Jobs
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model config JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write model config JSON: %w", err)
	}
	return nil
}

// safePositiveInt converts a raw YAML value to a positive integer. It discards
// trailing garbage such as the `35}` reported in ViperX configs.
func safePositiveInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return n, true
		}
	case int64:
		if n > 0 {
			return int(n), true
		}
	case float64:
		if n > 0 {
			return int(n), true
		}
	case string:
		// Strip non-numeric suffixes like `35}`.
		n = strings.TrimSpace(n)
		var digits strings.Builder
		for _, r := range n {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			} else if digits.Len() > 0 {
				break
			}
		}
		if digits.Len() == 0 {
			return 0, false
		}
		if v, err := strconv.Atoi(digits.String()); err == nil && v > 0 {
			return v, true
		}
	}
	return 0, false
}

// safeOverlap returns a valid overlap value in (0, 1).
func safeOverlap(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 && n < 1 {
			return n, true
		}
	case int:
		if n > 1 {
			return 1.0 / float64(n), true
		}
	case int64:
		if n > 1 {
			return 1.0 / float64(n), true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil && f > 0 && f < 1 {
			return f, true
		}
	}
	return 0, false
}

// inferenceDefaults returns type-specific safe defaults when the config cannot
// be parsed or contains absurd training values.
func inferenceDefaults(modelType string) ModelConfigResponse {
	cfg := ModelConfigResponse{
		SegmentSize: 512,
		Overlap:     0.25,
		ChunkSize:   0,
		BatchSize:   1,
		Device:      "cuda",
		Shifts:      1,
		Segment:     0,
		Jobs:        0,
		DimT:        512,
		NumOverlap:  4,
	}
	switch modelType {
	case "htdemucs":
		cfg.SegmentSize = 0
		cfg.DimT = 0
		cfg.Shifts = 1
		cfg.Segment = 0
	case "mdx23c":
		cfg.SegmentSize = 256
		cfg.DimT = 256
	}
	return cfg
}

// sanitizeModelConfig extracts inference parameters from a model-shipped YAML
// config. Training-only values (e.g. dim_t > 2048, absurd overlap) are replaced
// by safe defaults for the detected model type. If the YAML cannot be parsed,
// defaults are returned.
func sanitizeModelConfig(data []byte, modelType string) (ModelConfigResponse, bool) {
	defaults := inferenceDefaults(modelType)

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		log.Printf("[models] failed to parse model config YAML: %v", err)
		return defaults, false
	}

	cfg := defaults
	parsed := false

	if inf, ok := root["inference"].(map[string]interface{}); ok {
		parsed = true
		if v, ok := safePositiveInt(inf["dim_t"]); ok {
			cfg.SegmentSize = v
			cfg.DimT = v
		}
		if v, ok := safePositiveInt(inf["num_overlap"]); ok {
			cfg.NumOverlap = v
			cfg.Overlap = 1.0 / float64(v)
		}
		if v, ok := safePositiveInt(inf["batch_size"]); ok {
			cfg.BatchSize = v
		}
		if v, ok := safePositiveInt(inf["chunk_size"]); ok {
			cfg.ChunkSize = v
		}
		if v, ok := safeOverlap(inf["overlap"]); ok {
			cfg.Overlap = v
			if cfg.Overlap > 0 {
				cfg.NumOverlap = int(math.Round(1.0 / cfg.Overlap))
			}
		}
	}

	if dem, ok := root["demucs"].(map[string]interface{}); ok {
		parsed = true
		if v, ok := safePositiveInt(dem["shifts"]); ok {
			cfg.Shifts = v
		}
		if v, ok := safePositiveInt(dem["segment"]); ok {
			cfg.Segment = clampDemucsSegment(float64(v))
		}
		if v, ok := safePositiveInt(dem["jobs"]); ok {
			cfg.Jobs = v
		}
	}

	// Also accept top-level keys used by some MSST training configs.
	if v, ok := safePositiveInt(root["dim_t"]); ok {
		cfg.SegmentSize = v
		cfg.DimT = v
		parsed = true
	}
	if v, ok := safePositiveInt(root["num_overlap"]); ok {
		cfg.NumOverlap = v
		cfg.Overlap = 1.0 / float64(v)
		parsed = true
	}
	if v, ok := safePositiveInt(root["batch_size"]); ok {
		cfg.BatchSize = v
		parsed = true
	}
	if v, ok := safePositiveInt(root["chunk_size"]); ok {
		cfg.ChunkSize = v
		parsed = true
	}
	if v, ok := safeOverlap(root["overlap"]); ok {
		cfg.Overlap = v
		if cfg.Overlap > 0 {
			cfg.NumOverlap = int(math.Round(1.0 / cfg.Overlap))
		}
		parsed = true
	}

	// Sanitize: training configs sometimes contain huge dim_t values that are
	// not valid for inference (e.g. 3105 for ViperX). Values above 2048 are
	// clamped to the type default; any positive integer below that is accepted
	// so that cleaned values like `35` are preserved.
	if cfg.SegmentSize > 2048 {
		if defaults.SegmentSize > 0 {
			log.Printf("[models] config dim_t=%d looks like a training value; using default %d", cfg.SegmentSize, defaults.SegmentSize)
			cfg.SegmentSize = defaults.SegmentSize
			cfg.DimT = defaults.SegmentSize
		}
	}
	if cfg.SegmentSize < 1 {
		cfg.SegmentSize = defaults.SegmentSize
		cfg.DimT = defaults.SegmentSize
	}
	if cfg.Overlap <= 0 || cfg.Overlap >= 1 {
		cfg.Overlap = defaults.Overlap
		cfg.NumOverlap = defaults.NumOverlap
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 1
	}
	if cfg.Shifts < 1 {
		cfg.Shifts = 1
	}
	cfg.Segment = clampDemucsSegment(cfg.Segment)

	return cfg, parsed
}

// hfDownloadKey returns the in-progress job key for an HF pair download.
func hfDownloadKey(repo, weightPath string) string {
	return repo + "#" + weightPath
}

// hfAuthHeader returns the Authorization header value from HF_TOKEN, or empty.
func hfAuthHeader() string {
	if token := os.Getenv("HF_TOKEN"); token != "" {
		return "Bearer " + token
	}
	return ""
}

// downloadHFURL downloads a single URL to destPath, injecting the HF token if
// available. It reuses the progress tracker in models.go but does not mark the
// job as done because the caller still has to fetch the config and write the
// JSON override.
func downloadHFURL(ctx context.Context, url, destPath, jobKey string) error {
	return downloadWithProgressAuth(ctx, url, destPath, jobKey, hfAuthHeader(), false)
}

// runHFModelDownload downloads a weight+config pair from HuggingFace and places
// it in the correct on-disk model directory.
func runHFModelDownload(req DownloadHFRequest) {
	key := hfDownloadKey(req.Repo, req.Candidato.Peso.Path)

	downloadMu.Lock()
	status, ok := downloadJobs[key]
	if !ok {
		downloadMu.Unlock()
		log.Printf("[models] no download job for %s", key)
		return
	}
	status.DestPath = filepath.Join(targetDirForHFModel(req.Tipo, req.Nombre, req.Candidato.Peso.Path), sanitizeName(req.Nombre)+filepath.Ext(req.Candidato.Peso.Path))
	downloadMu.Unlock()

	err := downloadHFPair(context.Background(), status, req)

	downloadMu.Lock()
	defer downloadMu.Unlock()
	if status.Status == "cancelled" {
		removePartialFiles(status.DestPath)
		return
	}
	if err != nil {
		status.Status = "error"
		status.Progress = "Download failed"
		status.Error = err.Error()
		log.Printf("[models] HF pair download error for %s: %v", key, err)
		return
	}
	status.Status = "done"
	status.Progress = "Download complete"
	status.Percentage = 100
	log.Printf("[models] HF pair download complete for %s", key)
}

// downloadHFPair downloads the weight and optional config, creates the inference
// config JSON and makes the model discoverable by the scanner.
func downloadHFPair(ctx context.Context, status *DownloadStatus, req DownloadHFRequest) error {
	if req.Candidato.Peso.URL == "" {
		return errors.New("missing weight URL")
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	targetDir := targetDirForHFModel(req.Tipo, req.Nombre, req.Candidato.Peso.Path)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	weightPath := weightDestPath(targetDir, req.Nombre, req.Candidato.Peso.Path)
	configPath := ""
	if req.Candidato.Config != nil && req.Candidato.Config.URL != "" {
		configPath = configDestPath(targetDir, req.Nombre, req.Candidato.Config.Path)
	}

	key := hfDownloadKey(req.Repo, req.Candidato.Peso.Path)

	// Download weight.
	if err := downloadHFURL(ctx, req.Candidato.Peso.URL, weightPath, key); err != nil {
		return fmt.Errorf("failed to download weight %s: %w", req.Candidato.Peso.Path, err)
	}

	// Download config when available.
	var cfgYAML []byte
	if configPath != "" {
		data, err := fetchHFConfigWithAuth(ctx, req.Repo, branch, req.Candidato.Config.Path)
		if err != nil {
			// A missing config is not fatal: fall back to defaults and annotate.
			log.Printf("[models] config %s not downloaded: %v", req.Candidato.Config.Path, err)
		} else {
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}
			if err := os.WriteFile(configPath, data, 0644); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}
			cfgYAML = data
		}
	}

	// Build sanitized inference config.
	cfg, _ := sanitizeModelConfig(cfgYAML, req.Tipo)
	if err := writeModelConfigJSON(sanitizeName(req.Nombre), cfg); err != nil {
		log.Printf("[models] failed to write model config JSON for %s: %v", req.Nombre, err)
	}

	// Update job metadata now that files are on disk.
	downloadMu.Lock()
	status.DestPath = weightPath
	status.Target = filepath.ToSlash(targetDir)
	downloadMu.Unlock()

	return nil
}

// handleModelsDownloadHF accepts a HuggingFace weight+config pair and starts an
// asynchronous download.
func (s *Server) handleModelsDownloadHF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("método %s no permitido", r.Method)})
		return
	}

	var req DownloadHFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("JSON inválido: %v", err)})
		return
	}

	if req.Repo == "" || req.Nombre == "" || req.Candidato.Peso.Path == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "repo, nombre y candidato.peso son obligatorios"})
		return
	}
	if req.Tipo == "" {
		req.Tipo = "desconocido"
	}

	key := hfDownloadKey(req.Repo, req.Candidato.Peso.Path)

	// Reuse an in-flight download for the same pair.
	downloadMu.RLock()
	if existing, ok := downloadJobs[key]; ok && existing.Status == "downloading" {
		downloadMu.RUnlock()
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(existing)
		return
	}
	downloadMu.RUnlock()

	status := &DownloadStatus{
		ID:       nextDownloadID(),
		Status:   "downloading",
		Repo:     req.Repo,
		Target:   filepath.ToSlash(targetDirForHFModel(req.Tipo, req.Nombre, req.Candidato.Peso.Path)),
		Filename: filepath.Base(req.Candidato.Peso.Path),
		Source:   "huggingface",
	}
	downloadMu.Lock()
	downloadJobs[key] = status
	downloadMu.Unlock()

	go runHFModelDownload(req)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(status)
}

// fetchHFConfigWithAuth is like fetchHFConfig but injects the HuggingFace token
// from the environment when present. It is kept separate from the resolve path
// because resolve only queries public repos.
func fetchHFConfigWithAuth(ctx context.Context, repo, branch, path string) ([]byte, error) {
	url := hfResolveURLWithBranch(repo, branch, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if h := hfAuthHeader(); h != "" {
		req.Header.Set("Authorization", h)
	}
	resp, err := hfHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("no se pudo leer %s: %s", path, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// hfHTTPClientForDownload is a longer-timeout client used for weight downloads.
// It is exported as a variable so tests can replace it with a local server.
var hfHTTPClientForDownload = &http.Client{
	Timeout: 600 * time.Second,
}

// downloadWithProgressAuth is implemented in models.go; this reference keeps the
// compiler happy if the token-aware variant is ever moved to a separate file.
var _ = downloadWithProgressAuth
