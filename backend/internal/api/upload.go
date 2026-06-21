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
}

// UploadResponse is returned by POST /api/daw/upload.
type UploadResponse struct {
	File string `json:"file"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// handleUploadAudio accepts a multipart audio upload and saves it to daw-data/.
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

	projectRoot := findProjectRoot()
	uploadDir := filepath.Join(projectRoot, "daw-data")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create upload directory"})
		return
	}

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
	destFile := fmt.Sprintf("upload_%s%s", base, ext)
	destPath := filepath.Join(uploadDir, destFile)

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

	Log("backend", "success", "DAW upload: "+destFile)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		File: destFile,
		Path: "daw-data/" + destFile,
		Size: written,
	})
}
