package api

import (
	"encoding/json"
	"fmt"
	"io"
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
	Path   string `json:"path,omitempty"`
	URL    string `json:"url,omitempty"`
	Name   string `json:"name,omitempty"`
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

	projectRoot := dataRoot()

	// Resolve the source inside the daw-data tree first, then fall back to input/.
	filePath, safeName, song, subdir, err := resolveDAWAudioSource(req.File)
	if err != nil {
		// Legacy flat fallback for input files.
		safeName = filepath.Base(req.File)
		candidate := filepath.Join(projectRoot, "input", safeName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			filePath = candidate
			song = ""
			subdir = ""
		} else {
			writeDAWFileNotFound(w, safeName, req.File)
			return
		}
	}

	info, err := os.Stat(filePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to stat file: " + err.Error()})
		return
	}

	// Determine the song directory for the converted output.
	if song == "" {
		song = songDirName(strings.TrimSuffix(safeName, filepath.Ext(safeName)))
	}

	if format == "wav" {
		if configuredExportDir := exportDir(); configuredExportDir != "" {
			outputName := "export_" + strings.TrimSuffix(safeName, filepath.Ext(safeName)) + ".wav"
			outputPath := filepath.Join(configuredExportDir, outputName)
			if err := os.MkdirAll(configuredExportDir, 0o755); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to create export directory"})
				return
			}
			if err := copyFile(filePath, outputPath); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to copy exported file: " + err.Error()})
				return
			}
			registerExportFile(song, outputName)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ExportResponse{
				File:   outputName,
				Path:   outputName,
				URL:    exportDirFileURL(outputName),
				Name:   song,
				Format: format,
				Size:   info.Size(),
			})
			return
		}

		var relPath, name string
		if song != "" {
			if subdir == "" {
				subdir = dawOriginalSubdir
			}
			relPath = filepath.Join(dawDataDirName, song, subdir, safeName)
			name = song
		} else {
			relPath = filepath.Join("input", safeName)
			name = strings.TrimSuffix(safeName, filepath.Ext(safeName))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ExportResponse{
			File:   safeName,
			Path:   relPath,
			URL:    dawDataURL(relPath),
			Name:   name,
			Format: format,
			Size:   info.Size(),
		})
		return
	}

	// FLAC/MP3 export: convert the source file with ffmpeg and write it to
	// either the configured export directory or daw-data/{song}/edits/.
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

	base := strings.TrimSuffix(safeName, filepath.Ext(safeName))
	outputName := "export_" + base + outputExt

	var outputPath string
	var relPath string
	if configuredExportDir := exportDir(); configuredExportDir != "" {
		if err := os.MkdirAll(configuredExportDir, 0o755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create export directory"})
			return
		}
		outputPath = filepath.Join(configuredExportDir, outputName)
		relPath = outputName
	} else {
		editsDir := filepath.Join(projectRoot, dawDataDirName, song, dawEditsSubdir)
		if err := os.MkdirAll(editsDir, 0o755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create daw-data directory"})
			return
		}
		outputPath = filepath.Join(editsDir, outputName)
		relPath = filepath.Join(dawDataDirName, song, dawEditsSubdir, outputName)
	}

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

	registerExportFile(song, outputName)

	var downloadURL string
	if exportDir() != "" {
		downloadURL = exportDirFileURL(outputName)
	} else {
		downloadURL = dawDataURL(relPath)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ExportResponse{
		File:   outputName,
		Path:   relPath,
		URL:    downloadURL,
		Name:   song,
		Format: format,
		Size:   outInfo.Size(),
	})
}

// copyFile copies src to dst, creating dst's parent directory if needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
