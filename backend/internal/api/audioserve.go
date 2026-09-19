package api

import (
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) handleServeAudio(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}
	projectRoot := resolveProjectRoot()

	// Resolve inside the daw-data tree first, then fall back to input/.
	sourcePath, _, _, _, err := resolveDAWAudioSource(filename)
	if err == nil {
		http.ServeFile(w, r, sourcePath)
		return
	}

	// Fallback to a flat input lookup for legacy callers.
	inputPath := filepath.Join(projectRoot, "input", filepath.Base(filename))
	if _, err := os.Stat(inputPath); err == nil {
		http.ServeFile(w, r, inputPath)
		return
	}

	http.Error(w, "file not found", http.StatusNotFound)
}
