package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ImportRequest is the JSON body for POST /api/daw/import.
type ImportRequest struct {
	Source string `json:"source"`
	Song   string `json:"song,omitempty"`
	Stem   string `json:"stem,omitempty"`
}

// ImportResponse is returned by POST /api/daw/import.
type ImportResponse struct {
	File string `json:"file"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// handleImportStem copies a stem from output/ or input_rubberband/ into daw-data/.
// POST /api/daw/import
func (s *Server) handleImportStem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Source == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "source is required"})
		return
	}

	projectRoot := resolveProjectRoot()
	var srcPath, destFile string

	switch req.Source {
	case "output":
		if req.Song == "" || req.Stem == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "song and stem are required for output source"})
			return
		}
		safeSong := filepath.Base(req.Song)
		safeStem := filepath.Base(req.Stem)
		srcPath = filepath.Join(projectRoot, "output", safeSong, safeStem)
		ext := strings.ToLower(filepath.Ext(safeStem))
		destFile = fmt.Sprintf("import_%s_%s", safeSong, strings.TrimSuffix(safeStem, ext))
		if ext != "" {
			destFile += ext
		}
	case "pitch":
		if req.Stem == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "stem is required for pitch source"})
			return
		}
		safeStem := filepath.Base(req.Stem)
		srcPath = filepath.Join(projectRoot, "input_rubberband", safeStem)
		ext := strings.ToLower(filepath.Ext(safeStem))
		destFile = fmt.Sprintf("import_%s", safeStem)
		if ext == "" {
			destFile += ".wav"
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("unsupported source %q", req.Source)})
		return
	}

	dawDir := filepath.Join(projectRoot, "daw-data")
	if err := os.MkdirAll(dawDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create daw-data directory"})
		return
	}

	destPath := filepath.Join(dawDir, filepath.Base(destFile))

	// If already imported, return the existing file.
	if info, err := os.Stat(destPath); err == nil && !info.IsDir() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ImportResponse{
			File: filepath.Base(destPath),
			Path: "daw-data/" + filepath.Base(destPath),
			Size: info.Size(),
		})
		return
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "source file not found"})
		return
	}

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write imported file"})
		return
	}

	info, err := os.Stat(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat imported file"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ImportResponse{
		File: filepath.Base(destPath),
		Path: "daw-data/" + filepath.Base(destPath),
		Size: info.Size(),
	})
}
