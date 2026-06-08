package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/starmito/onda/internal/audio"
)

// PitchRequest is the JSON body for POST /api/pitch.
type PitchRequest struct {
	Song  string `json:"song"`
	Pitch int    `json:"pitch"`
}

// PitchResponse is returned by POST /api/pitch.
type PitchResponse struct {
	Song  string      `json:"song"`
	Pitch int         `json:"pitch"`
	Files []FileEntry `json:"files"`
}

// handlePitchShift applies rubberband pitch shift to all stems of a song
// except drums, and saves results in a subdirectory.
// POST /api/pitch
func (s *Server) handlePitchShift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req PitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Song == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "song name is required"})
		return
	}

	if req.Pitch == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "pitch must be non-zero"})
		return
	}

	projectRoot := findProjectRoot()
	outputBase := filepath.Join(projectRoot, "output")

	// Source directory: /output/{song}/
	songDir := filepath.Join(outputBase, req.Song)
	info, err := os.Stat(songDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("song %q not found", req.Song)})
		return
	}
	if !info.IsDir() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("%q is not a directory", req.Song)})
		return
	}

	// Output subdirectory: /output/{song}/{song}_pitch{+N}/
	pitchSuffix := fmt.Sprintf("_pitch%+d", req.Pitch)
	outDir := filepath.Join(songDir, req.Song+pitchSuffix)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}

	// Read source stems
	entries, err := os.ReadDir(songDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to read song dir: %v", err)})
		return
	}

	var resultFiles []FileEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip subdirectories (like previous pitch results)
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" {
			continue // only process audio files
		}

		inputPath := filepath.Join(songDir, name)

		// Determine if this is a drums stem (skip pitch processing)
		isDrums := strings.Contains(strings.ToLower(name), "drums")

		var outputName string
		if isDrums {
			outputName = name // drums keep original name
		} else {
			// Add pitch suffix before extension
			baseName := name[:len(name)-len(ext)]
			outputName = baseName + pitchSuffix + ext
		}
		outputPath := filepath.Join(outDir, outputName)

		if isDrums {
			// Copy drums as-is
			if err := copyFileLocal(inputPath, outputPath); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to copy drums: %v", err)})
				return
			}
		} else {
			// Apply rubberband pitch shift
			if err := audio.RubberbandPitch(req.Pitch, inputPath, outputPath); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("rubberband failed for %s: %v", name, err)})
				return
			}
		}

		resultFiles = append(resultFiles, FileEntry{
			Name: outputName,
			Path: outputPath,
		})
	}

	if len(resultFiles) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "no audio files found in song directory"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PitchResponse{
		Song:  req.Song,
		Pitch: req.Pitch,
		Files: resultFiles,
	})
}

// handleListPitchSubgroups returns existing pitch subgroups for a song.
// GET /api/pitch/{song}
func (s *Server) handleListPitchSubgroups(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	if song == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "song is required"})
		return
	}

	projectRoot := findProjectRoot()
	outputBase := filepath.Join(projectRoot, "output")
	songDir := filepath.Join(outputBase, song)

	// Path traversal guard
	if !strings.HasPrefix(filepath.Clean(songDir), filepath.Clean(outputBase)+string(filepath.Separator)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid song name"})
		return
	}

	entries, err := os.ReadDir(songDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	type subgroupInfo struct {
		Pitch int         `json:"pitch"`
		Files []FileEntry `json:"files"`
	}
	var subgroups []subgroupInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, "_pitch") {
			continue
		}

		// Extract pitch value from directory name: {song}_pitch{+N}
		parts := strings.Split(name, "_pitch")
		if len(parts) != 2 {
			continue
		}
		pitchStr := parts[1]
		var pitch int
		n, err := fmt.Sscanf(pitchStr, "%d", &pitch)
		if n != 1 || err != nil || pitch == 0 {
			continue
		}

		// Read files in this subdirectory
		subDir := filepath.Join(songDir, name)
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}

		var files []FileEntry
		for _, se := range subEntries {
			if !se.IsDir() {
				files = append(files, FileEntry{
					Name: se.Name(),
					Path: filepath.Join(subDir, se.Name()),
				})
			}
		}

		subgroups = append(subgroups, subgroupInfo{
			Pitch: pitch,
			Files: files,
		})
	}

	if subgroups == nil {
		subgroups = []subgroupInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subgroups)
}

// copyFileLocal copies a file from src to dst.
func copyFileLocal(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return dstFile.Sync()
}

// handleDeletePitchSubgroup removes a pitched subgroup directory.
// DELETE /api/pitch/{song}/{pitch}
func (s *Server) handleDeletePitchSubgroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	song := r.PathValue("song")
	pitchStr := r.PathValue("pitch")
	if song == "" || pitchStr == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "song and pitch are required"})
		return
	}

	projectRoot := findProjectRoot()
	outputBase := filepath.Join(projectRoot, "output")
	pitchDir := filepath.Join(outputBase, song, song+"_pitch"+pitchStr)

	// Path traversal guard
	if !strings.HasPrefix(filepath.Clean(pitchDir), filepath.Clean(outputBase)+string(filepath.Separator)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid song or pitch"})
		return
	}

	if err := os.RemoveAll(pitchDir); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to delete pitch subgroup: %v", err)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
