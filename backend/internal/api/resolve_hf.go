package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ResolveHFRequest is the JSON body for POST /api/models/resolve.
type ResolveHFRequest struct {
	URL string `json:"url"`
}

// ResolveHFFile describes a downloadable file.
type ResolveHFFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// ResolveHFCandidate is a weight file plus its optional architecture config.
type ResolveHFCandidate struct {
	Peso   ResolveHFFile  `json:"peso"`
	Config *ResolveHFFile `json:"config"` // nil when the repo has no config
}

// ResolveHFResponse is the metadata returned for a HuggingFace link.
type ResolveHFResponse struct {
	Repo         string               `json:"repo"`
	Nombre       string               `json:"nombre"`
	Autor        string               `json:"autor"`
	Privado      bool                 `json:"privado"`
	Gated        bool                 `json:"gated"`
	TamañoTotal  int64                `json:"tamaño_total"`
	Candidatos   []ResolveHFCandidate `json:"candidatos"`
	Tipo         string               `json:"tipo"`
	Razon        string               `json:"razon"`
	YaDescargado bool                 `json:"ya_descargado"`
	Error        string               `json:"error,omitempty"`
}

// supportedModelTypes is the official list of types accepted by the MSST engine.
var supportedModelTypes = map[string]bool{
	"bs_roformer":                   true,
	"bs_conformer":                  true,
	"bs_roformer_experimental":      true,
	"bs_mamba2":                     true,
	"mel_band_roformer":             true,
	"mel_band_conformer":            true,
	"mel_band_roformer_experimental": true,
	"mdx23c":                        true,
	"htdemucs":                      true,
	"scnet":                         true,
	"scnet_masked":                  true,
	"scnet_tran":                    true,
	"scnet_unofficial":              true,
	"apollo":                        true,
	"bandit":                        true,
	"bandit_v2":                     true,
	"conformer":                     true,
	"dttnet":                        true,
	"moises_light":                  true,
	"segm_models":                   true,
	"torchseg":                      true,
	"swin_upernet":                  true,
	"experimental_mdx23c_stht":      true,
}

// hfHTTPClient is used for all HuggingFace API calls.
var hfHTTPClient = &http.Client{Timeout: 15 * time.Second}

// parseHFURL extracts the repo id, branch and optional file path from a
// HuggingFace URL. It accepts https://huggingface.co/<org>/<repo> and the
// blob/resolve/tree variants, with or without a trailing slash/query string.
func parseHFURL(raw string) (repo, branch, filePath string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errors.New("enlace vacío")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, "huggingface.co") {
		return "", "", "", errors.New("el enlace no pertenece a HuggingFace")
	}

	path := strings.Trim(u.Path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return "", "", "", errors.New("enlace de HuggingFace incompleto")
	}
	repo = parts[0] + "/" + parts[1]
	branch = "main"
	if len(parts) >= 3 {
		sub := strings.SplitN(parts[2], "/", 3)
		if len(sub) >= 2 && (sub[0] == "blob" || sub[0] == "resolve" || sub[0] == "tree") {
			branch = sub[1]
			if len(sub) >= 3 {
				filePath = sub[2]
			}
		}
	}
	return repo, branch, filePath, nil
}

// hfRepoInfo is a subset of the HuggingFace /api/models response.
type hfRepoInfo struct {
	ID      string          `json:"id"`
	Author  string          `json:"author"`
	Private bool            `json:"private"`
	Gated   json.RawMessage `json:"gated"`
}

func isGated(info hfRepoInfo) bool {
	if len(info.Gated) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(info.Gated, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(info.Gated, &s); err == nil {
		return strings.ToLower(s) != "false"
	}
	// Any object value means the repo is gated.
	return true
}

// hfTreeEntry is a file returned by the /api/models/{repo}/tree/{branch} endpoint.
type hfTreeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type hfAPIError struct {
	status int
	msg    string
}

func (e hfAPIError) Error() string { return e.msg }

func hfGetJSON(ctx context.Context, url string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := hfHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return hfAPIError{status: resp.StatusCode, msg: resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func fetchHFRepoMetadata(ctx context.Context, repo string) (hfRepoInfo, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return hfRepoInfo{}, errors.New("repo inválido")
	}
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/%s",
		url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	var info hfRepoInfo
	if err := hfGetJSON(ctx, url, &info); err != nil {
		if apiErr, ok := err.(hfAPIError); ok {
			switch apiErr.status {
			case http.StatusNotFound:
				return hfRepoInfo{}, errors.New("repositorio no encontrado")
			case http.StatusUnauthorized, http.StatusForbidden:
				// HF returns 401/403 both for private/gated repos and for repos that do
				// not exist, so we report a single clear message without panicking.
				return hfRepoInfo{}, errors.New("repositorio no accesible: privado, gated o no existe")
			}
		}
		return hfRepoInfo{}, fmt.Errorf("no se pudo leer el repositorio: %w", err)
	}
	return info, nil
}

func fetchHFRepoTree(ctx context.Context, repo, branch string) ([]hfTreeEntry, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, errors.New("repo inválido")
	}
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/%s/tree/%s",
		url.PathEscape(parts[0]), url.PathEscape(parts[1]), url.PathEscape(branch))
	var entries []hfTreeEntry
	if err := hfGetJSON(ctx, url, &entries); err != nil {
		if apiErr, ok := err.(hfAPIError); ok && apiErr.status == http.StatusNotFound {
			return nil, fmt.Errorf("rama %q no encontrada", branch)
		}
		return nil, err
	}
	var files []hfTreeEntry
	for _, e := range entries {
		if e.Type == "file" {
			files = append(files, e)
		}
	}
	return files, nil
}

// hfResolveURLWithBranch builds a raw-file URL for a repo path and branch.
func hfResolveURLWithBranch(repo, branch, filename string) string {
	escape := func(segments []string) []string {
		out := make([]string, len(segments))
		for i, s := range segments {
			out[i] = url.PathEscape(s)
		}
		return out
	}
	repoParts := escape(strings.Split(repo, "/"))
	fileParts := escape(strings.Split(filename, "/"))
	return fmt.Sprintf("%s/%s/resolve/%s/%s", hfResolveBase,
		strings.Join(repoParts, "/"), url.PathEscape(branch), strings.Join(fileParts, "/"))
}

func fetchHFConfig(ctx context.Context, repo, branch, path string) ([]byte, error) {
	url := hfResolveURLWithBranch(repo, branch, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hfHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("no se pudo leer %s: %s", path, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// isWeightFile reports whether a path looks like a model weights file.
func isWeightFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ckpt", ".pth", ".onnx", ".th", ".safetensors":
		return true
	}
	return false
}

// isConfigFile reports whether a path looks like an architecture config.
func isConfigFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// normalizeModelName strips extension and keeps only letters/digits, lowercased.
func normalizeModelName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	var sb strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// detectModelTypeFromConfig inspects the YAML for training.model_type or model
// block keys that identify the architecture.
func detectModelTypeFromConfig(data []byte) (modelType, reason string, ok bool) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", "", false
	}

	if training, ok := root["training"].(map[string]interface{}); ok {
		if v, ok := training["model_type"].(string); ok && v != "" {
			if mt := normalizeModelType(v); supportedModelTypes[mt] {
				return mt, "config", true
			}
		}
	}

	model, _ := root["model"].(map[string]interface{})
	if model == nil {
		return "", "", false
	}

	// Second priority: explicit model_type inside the model block.
	if v, ok := model["model_type"].(string); ok && v != "" {
		if mt := normalizeModelType(v); supportedModelTypes[mt] {
			return mt, "config", true
		}
	}

	_, hasUsePope := model["use_pope"]
	_, hasFreqs := model["freqs_per_bands"]
	_, hasDimFreqs := model["dim_freqs_in"]
	_, hasNumBands := model["num_bands"]
	_, hasLinearTransformer := model["linear_transformer_depth"]
	_, hasBandSR := model["band_SR"]
	_, hasBandStride := model["band_stride"]
	_, hasBandKernel := model["band_kernel"]
	_, hasNumScales := model["num_scales"]
	_, hasNumSubbands := model["num_subbands"]
	_, hasNumBlocks := model["num_blocks_per_scale"]

	if hasUsePope {
		return "bs_roformer", "claves_del_modelo", true
	}
	if hasNumScales || hasNumSubbands || hasNumBlocks {
		return "mdx23c", "claves_del_modelo", true
	}
	if hasBandSR || hasBandStride || hasBandKernel {
		return "scnet", "claves_del_modelo", true
	}
	if hasNumBands || hasLinearTransformer {
		return "mel_band_roformer", "claves_del_modelo", true
	}
	if hasFreqs || hasDimFreqs {
		return "bs_roformer", "claves_del_modelo", true
	}
	return "", "", false
}

// normalizeModelType maps common aliases to the canonical MSST type name.
func normalizeModelType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "-", "_")
	switch v {
	case "bsroformer", "bs_roformer":
		return "bs_roformer"
	case "melbandroformer", "mel_band_roformer", "melband_roformer":
		return "mel_band_roformer"
	case "mdx", "mdx_c", "mdx_net", "mdxnet":
		return "mdx23c"
	case "scnet":
		return "scnet"
	case "htdemucs", "demucs":
		return "htdemucs"
	}
	return v
}

// detectModelTypeByName uses repo and filename heuristics.
func detectModelTypeByName(repo, filename string) (string, bool) {
	s := strings.ToLower(repo + " " + filename)

	if strings.Contains(s, "melband") || strings.Contains(s, "mel_band") || strings.Contains(s, "mel band") {
		return "mel_band_roformer", true
	}
	if strings.Contains(s, "scnet") {
		return "scnet", true
	}
	if strings.Contains(s, "mdx23c") || strings.Contains(s, "mdx-c") || strings.Contains(s, "drumsep") {
		return "mdx23c", true
	}
	if strings.Contains(s, "htdemucs") || strings.Contains(s, "demucs") {
		return "htdemucs", true
	}
	if strings.Contains(s, "roformer") || strings.Contains(s, "viperx") {
		return "bs_roformer", true
	}
	// ONNX vocal models without a better signal are treated as MDX-Net.
	if strings.HasSuffix(strings.ToLower(filename), ".onnx") && (strings.Contains(s, "vocal") || strings.Contains(s, "kim_vocal") || strings.Contains(s, "inst")) {
		return "mdx23c", true
	}
	return "", false
}

// detectModelType applies the fixed deduction order: config type, config keys,
// filename/repo heuristics, and finally unknown.
func detectModelType(files []hfTreeEntry, repo string, configs map[string][]byte) (string, string) {
	configPaths := make([]string, 0, len(configs))
	for p := range configs {
		configPaths = append(configPaths, p)
	}
	sort.Strings(configPaths)

	// 1) training.model_type (and model.model_type) from any config.
	for _, p := range configPaths {
		if mt, reason, ok := detectModelTypeFromConfig(configs[p]); ok && reason == "config" {
			return mt, reason
		}
	}
	// 2) Model block keys.
	for _, p := range configPaths {
		if mt, reason, ok := detectModelTypeFromConfig(configs[p]); ok {
			return mt, reason
		}
	}
	// 3) Name/repo heuristics.
	for _, f := range files {
		if isWeightFile(f.Path) {
			if mt, ok := detectModelTypeByName(repo, f.Path); ok {
				return mt, "nombre"
			}
		}
	}
	// 4) No checkpoint keys are inspected remotely (would require a download).
	return "desconocido", "desconocido"
}

// pairCandidates matches each weight file with its most likely config.
func pairCandidates(files []hfTreeEntry, repo, branch string) []ResolveHFCandidate {
	var weights, configs []hfTreeEntry
	for _, f := range files {
		switch {
		case isWeightFile(f.Path):
			weights = append(weights, f)
		case isConfigFile(f.Path):
			configs = append(configs, f)
		}
	}
	sort.Slice(weights, func(i, j int) bool { return weights[i].Path < weights[j].Path })

	makeFile := func(f hfTreeEntry) ResolveHFFile {
		return ResolveHFFile{
			Path: f.Path,
			Size: f.Size,
			URL:  hfResolveURLWithBranch(repo, branch, f.Path),
		}
	}

	candidates := make([]ResolveHFCandidate, 0, len(weights))
	for _, w := range weights {
		cfg := pickConfigForWeight(w, weights, configs)
		var cfgPtr *ResolveHFFile
		if cfg != nil {
			f := makeFile(*cfg)
			cfgPtr = &f
		}
		candidates = append(candidates, ResolveHFCandidate{
			Peso:   makeFile(w),
			Config: cfgPtr,
		})
	}
	return candidates
}

// pickConfigForWeight chooses the best config for a weight file.
func pickConfigForWeight(weight hfTreeEntry, weights, configs []hfTreeEntry) *hfTreeEntry {
	if len(configs) == 0 {
		return nil
	}
	weightBase := strings.TrimSuffix(filepath.Base(weight.Path), filepath.Ext(weight.Path))
	weightNorm := normalizeModelName(weight.Path)

	// 1) Exact base-name match (e.g. model.ckpt + model.yaml).
	for _, c := range configs {
		cfgBase := strings.TrimSuffix(filepath.Base(c.Path), filepath.Ext(c.Path))
		if strings.EqualFold(weightBase, cfgBase) {
			return &c
		}
	}

	// 2) Single config in the repo: every weight uses it.
	if len(configs) == 1 {
		return &configs[0]
	}

	// 3) Substring match in either direction.
	for _, c := range configs {
		cfgNorm := normalizeModelName(c.Path)
		if strings.Contains(cfgNorm, weightNorm) || strings.Contains(weightNorm, cfgNorm) {
			return &c
		}
	}

	// 4) Match by detected type keyword shared between weight and config names.
	if mt, ok := detectModelTypeByName("", weight.Path); ok {
		keyword := typeNameKeyword(mt)
		for _, c := range configs {
			cfgNorm := normalizeModelName(c.Path)
			if strings.Contains(cfgNorm, keyword) {
				return &c
			}
		}
	}

	// Too ambiguous: report no config rather than guess wrong.
	return nil
}

// typeNameKeyword returns a normalized keyword used to pair configs by type.
func typeNameKeyword(modelType string) string {
	switch modelType {
	case "mel_band_roformer":
		return "melband"
	case "bs_roformer":
		return "roformer"
	case "mdx23c":
		return "mdx"
	case "scnet":
		return "scnet"
	case "htdemucs":
		return "demucs"
	}
	return modelType
}

// humanizeRepoName turns a repo slug into a readable model name.
func humanizeRepoName(repo string) string {
	parts := strings.Split(repo, "/")
	name := parts[len(parts)-1]
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return strings.TrimSpace(name)
}

// isHFWeightDownloaded reports whether a file with the same basename already
// exists in any of the model directories.
func isHFWeightDownloaded(path string) bool {
	base := filepath.Base(path)
	for _, subdir := range modelSubdirs {
		dir := filepath.Join(modelsBasePath(), subdir)
		if _, err := os.Stat(filepath.Join(dir, base)); err == nil {
			return true
		}
		found := false
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if info.Name() == base {
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

// resolveHFRepo gathers metadata for a HuggingFace URL without downloading weights.
func resolveHFRepo(ctx context.Context, rawURL string) (*ResolveHFResponse, error) {
	repo, branch, _, err := parseHFURL(rawURL)
	if err != nil {
		return nil, err
	}

	info, err := fetchHFRepoMetadata(ctx, repo)
	if err != nil {
		return nil, err
	}
	if info.Private || isGated(info) {
		return nil, errors.New("el modelo es privado o gated; no se puede descargar sin autenticación")
	}

	files, err := fetchHFRepoTree(ctx, repo, branch)
	if err != nil && branch == "main" {
		files, err = fetchHFRepoTree(ctx, repo, "master")
	}
	if err != nil {
		return nil, fmt.Errorf("no se pudo listar los ficheros: %w", err)
	}

	// Fetch and parse all YAML configs to determine the model type.
	configs := make(map[string][]byte)
	for _, f := range files {
		if isConfigFile(f.Path) {
			data, err := fetchHFConfig(ctx, repo, branch, f.Path)
			if err == nil {
				configs[f.Path] = data
			}
		}
	}

	modelType, reason := detectModelType(files, repo, configs)
	candidates := pairCandidates(files, repo, branch)

	var total int64
	downloaded := false
	for _, c := range candidates {
		total += c.Peso.Size
		if c.Config != nil {
			total += c.Config.Size
		}
		if isHFWeightDownloaded(c.Peso.Path) {
			downloaded = true
		}
	}

	return &ResolveHFResponse{
		Repo:         repo,
		Nombre:       humanizeRepoName(repo),
		Autor:        info.Author,
		Privado:      info.Private,
		Gated:        isGated(info),
		TamañoTotal:  total,
		Candidatos:   candidates,
		Tipo:         modelType,
		Razon:        reason,
		YaDescargado: downloaded,
	}, nil
}

// handleModelsResolve resolves a pasted HuggingFace link into model metadata.
// POST /api/models/resolve
func (s *Server) handleModelsResolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("método %s no permitido", r.Method)})
		return
	}

	var req ResolveHFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("JSON inválido: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := resolveHFRepo(ctx, req.URL)
	if err != nil {
		msg := err.Error()
		code := http.StatusInternalServerError
		switch {
		case strings.Contains(msg, "enlace vacío") || strings.Contains(msg, "no pertenece a HuggingFace") || strings.Contains(msg, "incompleto"):
			code = http.StatusBadRequest
		case strings.Contains(msg, "no encontrado") || strings.Contains(msg, "no accesible"):
			code = http.StatusNotFound
		case strings.Contains(msg, "privado o gated"):
			code = http.StatusForbidden
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
