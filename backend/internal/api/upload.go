package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var allowedUploadExts = map[string]bool{
	".wav":  true,
	".mp3":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".aiff": true,
	".mid":  true,
	".midi": true,
}

// UploadResponse is returned by POST /api/daw/upload.
type UploadResponse struct {
	File string `json:"file"`
	Path string `json:"path"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// handleUploadAudio accepts a multipart audio upload and saves it to
// daw-data/{song}/original.<ext>. The song directory is taken from an optional
// "song" form field, or derived from the uploaded filename.
// POST /api/daw/upload
func (s *Server) handleUploadAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := resolveProjectRoot()

	if err := r.ParseMultipartForm(500 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse multipart form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		return
	}
	defer file.Close()

	originalName := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(originalName))
	if !allowedUploadExts[ext] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "unsupported file extension"})
		return
	}

	base := strings.TrimSuffix(originalName, ext)
	song := strings.TrimSpace(r.FormValue("song"))
	if song == "" {
		song = base
	}
	song = songDirName(song)

	destFile := "original" + ext
	songDir := filepath.Join(projectRoot, dawDataDirName, song)
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create song directory"})
		return
	}
	destPath := filepath.Join(songDir, destFile)

	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create upload file"})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write upload file"})
		return
	}

	Log("backend", "success", "DAW upload: "+song+"/"+destFile)

	relPath := filepath.Join(dawDataDirName, song, destFile)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		File: destFile,
		Path: relPath,
		URL:  dawDataURL(relPath),
		Name: song,
		Size: written,
	})
}
