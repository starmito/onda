package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MergeRequest is the JSON body for POST /api/stems/merge.
type MergeRequest struct {
	Song       string   `json:"song"`
	Stems      []string `json:"stems"`
	Format     string   `json:"format,omitempty"`
	OutputName string   `json:"outputName,omitempty"`
}

// MergeResponse is returned by POST /api/stems/merge.
type MergeResponse struct {
	File   string `json:"file"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url,omitempty"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
}

// handleStemsMerge mixes multiple stems of a song into a single file.
// POST /api/stems/merge
func (s *Server) handleStemsMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req MergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Song == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "song is required"})
		return
	}
	if len(req.Stems) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "stems are required"})
		return
	}

	format := strings.ToLower(req.Format)
	if format == "" {
		format = "flac"
	}
	if format != "wav" && format != "flac" && format != "mp3" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "format must be 'wav', 'flac' or 'mp3'"})
		return
	}

	projectRoot := dataRoot()
	baseSong := req.Song
	pitchSuffix := ""
	if idx := strings.LastIndex(req.Song, " (pitch "); idx >= 0 {
		baseSong = req.Song[:idx]
		rest := strings.TrimSuffix(req.Song[idx+len(" (pitch "):], ")")
		pitchSuffix = filepath.Base(baseSong) + "_pitch" + rest
	}
	safeSong := filepath.Base(baseSong)

	// Directory that holds the actual stem files to mix.
	stemsDir := filepath.Join(projectRoot, "output", safeSong)
	if pitchSuffix != "" {
		stemsDir = filepath.Join(stemsDir, pitchSuffix)
	}
	// mergeDir is the legacy fallback location used only when exportDir() is
	// empty. Since the default export directory is now the exports subdirectory
	// under the data root, normal merges go there and do not pollute the song
	// directory.
	mergeDir := filepath.Join(projectRoot, "output", safeSong)

	var inputFiles []string
	for _, stem := range req.Stems {
		safeStem := filepath.Base(stem)
		stemPath := filepath.Join(stemsDir, safeStem)
		if info, err := os.Stat(stemPath); err != nil || info.IsDir() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "stem not found: " + safeStem})
			return
		}
		inputFiles = append(inputFiles, stemPath)
	}

	base := "merge_" + safeSong
	if strings.TrimSpace(req.OutputName) != "" {
		sanitized := strings.ReplaceAll(req.OutputName, "/", "_")
		base = filepath.Base(sanitized)
	}
	ext := "." + format
	outputName := base
	if !strings.HasSuffix(strings.ToLower(outputName), ext) {
		outputName += ext
	}

	var outputPath string
	var outputRelPath string
	var downloadURL string
	if configuredExportDir := exportDir(); configuredExportDir != "" {
		if err := os.MkdirAll(configuredExportDir, 0o755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create export directory"})
			return
		}
		outputPath = filepath.Join(configuredExportDir, outputName)
		outputRelPath = outputName
		downloadURL = exportDirFileURL(outputName)
	} else {
		outputPath = filepath.Join(mergeDir, outputName)
		outputRelPath = filepath.Join("output", safeSong, outputName)
		downloadURL = "/" + filepath.ToSlash(outputRelPath)
	}

	args := []string{"-y"}
	for _, input := range inputFiles {
		args = append(args, "-i", input)
	}
	args = append(args,
		"-filter_complex", fmt.Sprintf("amix=inputs=%d:normalize=0", len(inputFiles)),
		"-codec:a", codecForMergeFormat(format),
	)

	switch format {
	case "flac":
		args = append(args, "-compression_level", "5")
	case "mp3":
		bitrate := "320k"
		exportProfilesMu.RLock()
		if profiles := exportProfiles.Formats; profiles != nil {
			if mp3 := profiles["mp3"]; mp3 != nil && mp3.Bitrate != "" {
				bitrate = mp3.Bitrate
			}
		}
		exportProfilesMu.RUnlock()
		args = append(args, "-b:a", bitrate)
	case "wav":
		// Keep source sample rate; no -ar flag.
	}

	args = append(args, outputPath)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "ffmpeg merge failed: " + strings.TrimSpace(string(out)),
		})
		return
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat merged file"})
		return
	}

	registerExportFile(safeSong, outputName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MergeResponse{
		File:   outputName,
		Path:   outputRelPath,
		URL:    downloadURL,
		Format: format,
		Size:   info.Size(),
	})
}

func codecForMergeFormat(format string) string {
	switch format {
	case "wav":
		return "pcm_s32le"
	case "mp3":
		return "libmp3lame"
	default:
		return "flac"
	}
}
