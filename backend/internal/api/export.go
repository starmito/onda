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

// ExportRequest is the JSON body for POST /api/audio/export.
type ExportRequest struct {
	File    string `json:"file"`
	Format  string `json:"format"`
	Bitrate string `json:"bitrate,omitempty"`
}

// ExportResponse is returned by POST /api/audio/export.
type ExportResponse struct {
	File   string `json:"file"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
}

// handleExport returns the requested audio file (WAV metadata) or converts it
// to MP3/FLAC using ffmpeg. Supported formats: "wav", "mp3" and "flac".
// POST /api/audio/export
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.File == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "file is required"})
		return
	}
	format := strings.ToLower(req.Format)
	if format != "wav" && format != "mp3" && format != "flac" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "format must be 'wav', 'mp3' or 'flac'"})
		return
	}

	bitrate := req.Bitrate
	if bitrate == "" {
		bitrate = "192k"
	}
	if format == "mp3" && bitrate != "128k" && bitrate != "192k" && bitrate != "320k" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bitrate must be 128k, 192k or 320k"})
		return
	}

	safeName := filepath.Base(req.File)
	projectRoot := findProjectRoot()

	// Search in daw-data first, then fall back to input.
	searchDirs := []string{
		filepath.Join(projectRoot, "daw-data"),
		filepath.Join(projectRoot, "input"),
	}

	var filePath string
	for _, dir := range searchDirs {
		candidate := filepath.Join(dir, safeName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			filePath = candidate
			break
		}
	}

	if filePath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat file: " + err.Error()})
		return
	}

	if format == "wav" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ExportResponse{
			File:   safeName,
			Format: format,
			Size:   info.Size(),
		})
		return
	}

	// FLAC/MP3 export: convert the source file with ffmpeg and write it to daw-data.
	var outputExt, codec string
	var extraArgs []string
	switch format {
	case "flac":
		outputExt = ".flac"
		codec = "flac"
	case "mp3":
		outputExt = ".mp3"
		codec = "libmp3lame"
		extraArgs = []string{"-b:a", bitrate}
	}

	dawBase := filepath.Join(projectRoot, "daw-data")
	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create daw-data directory"})
		return
	}

	base := strings.TrimSuffix(safeName, filepath.Ext(safeName))
	outputName := "export_" + base + outputExt
	outputPath := filepath.Join(dawBase, outputName)

	args := []string{
		"-y",
		"-i", filePath,
		"-codec:a", codec,
	}
	args = append(args, extraArgs...)
	args = append(args, outputPath)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "ffmpeg conversion failed: " + strings.TrimSpace(string(out)),
		})
		return
	}

	outInfo, err := os.Stat(outputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat exported file"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ExportResponse{
		File:   outputName,
		Format: format,
		Size:   outInfo.Size(),
	})
}
