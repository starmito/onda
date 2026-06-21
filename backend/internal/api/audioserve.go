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
	safe := filepath.Base(filename)
	projectRoot := resolveProjectRoot()
	paths := []string{
		filepath.Join(projectRoot, "daw-data", safe),
		filepath.Join(projectRoot, "input", safe),
	}
	var found string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			found = p
			break
		}
	}
	if found == "" {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, found)
}
