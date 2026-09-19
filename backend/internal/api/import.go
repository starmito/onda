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
	Pitch  string `json:"pitch,omitempty"`
	File   string `json:"file,omitempty"`
}

// ImportResponse is returned by POST /api/daw/import.
type ImportResponse struct {
	File string `json:"file"`
	Path string `json:"path"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// handleImportStem copies a stem into daw-data/{song}/imports/.
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
	outputBase := filepath.Join(projectRoot, "output")
	absOutputBase, err := filepath.Abs(outputBase)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve output path"})
		return
	}
	inputBase := filepath.Join(projectRoot, "input")
	absInputBase, err := filepath.Abs(inputBase)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve input path"})
		return
	}

	var srcPath, destFile, song string
	var absBase string

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
		srcPath = filepath.Join(outputBase, safeSong, safeStem)
		absBase = absOutputBase
		song = songDirName(safeSong)
		ext := strings.ToLower(filepath.Ext(safeStem))
		destFile = fmt.Sprintf("import_%s_%s", safeSong, strings.TrimSuffix(safeStem, ext))
		if ext != "" {
			destFile += ext
		}
	case "pitch":
		if req.Song == "" || req.Stem == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "song and stem are required for pitch source"})
			return
		}
		safeSong := filepath.Base(req.Song)
		safeStem := filepath.Base(req.Stem)
		resolved, err := resolvePitchSourcePath(absOutputBase, safeSong, req.Pitch, safeStem)
		if err != nil {
			writeDAWFileNotFound(w, safeStem)
			return
		}
		srcPath = resolved
		absBase = absOutputBase
		song = songDirName(safeSong)
		ext := strings.ToLower(filepath.Ext(safeStem))
		destFile = fmt.Sprintf("import_%s", safeStem)
		if ext == "" {
			destFile += ".wav"
		}
	case "input":
		if req.File == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "file is required for input source"})
			return
		}
		if strings.Contains(req.File, "..") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid file name"})
			return
		}
		safeName := filepath.Base(req.File)
		srcPath = filepath.Join(inputBase, safeName)
		absBase = absInputBase
		if req.Song != "" {
			song = songDirName(req.Song)
		} else {
			song = songDirName(strings.TrimSuffix(safeName, filepath.Ext(safeName)))
		}
		destFile = fmt.Sprintf("import_%s", safeName)
	case "daw-data":
		if req.File == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "file is required for daw-data source"})
			return
		}
		if strings.Contains(req.File, "..") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid file name"})
			return
		}
		// Resolve the source inside the tree to learn its song and subdir.
		srcAbs, srcName, srcSong, srcSubdir, err := resolveDAWAudioSource(req.File)
		if err != nil {
			writeDAWFileNotFound(w, filepath.Base(req.File))
			return
		}
		srcPath = srcAbs
		absBase, _ = filepath.Abs(filepath.Join(projectRoot, dawDataDirName))
		if req.Song != "" {
			song = songDirName(req.Song)
		} else {
			song = srcSong
		}
	if srcSubdir == dawImportsSubdir {
		// Already imported: return it where it actually lives.
		info, err := os.Stat(srcPath)
		if err != nil {
			writeDAWFileNotFound(w, srcName)
			return
		}
		relPath := filepath.Join(dawDataDirName, srcSong, dawImportsSubdir, srcName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ImportResponse{
			File: srcName,
			Path: relPath,
			URL:  dawDataURL(relPath),
			Name: srcSong,
			Size: info.Size(),
		})
		return
	}
		destFile = srcName
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("unsupported source %q", req.Source)})
		return
	}

	// Defense in depth: verify the resolved source path stays inside its base dir.
	absSrc, err := filepath.Abs(srcPath)
	if err != nil || !strings.HasPrefix(absSrc, absBase+string(filepath.Separator)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid source path"})
		return
	}

	importsDir := filepath.Join(projectRoot, dawDataDirName, song, dawImportsSubdir)
	if err := os.MkdirAll(importsDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create imports directory"})
		return
	}

	destPath := filepath.Join(importsDir, filepath.Base(destFile))

	// If already imported, return the existing file.
	if info, err := os.Stat(destPath); err == nil && !info.IsDir() {
		relPath := filepath.Join(dawDataDirName, song, dawImportsSubdir, filepath.Base(destPath))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ImportResponse{
			File: filepath.Base(destPath),
			Path: relPath,
			URL:  dawDataURL(relPath),
			Name: song,
			Size: info.Size(),
		})
		return
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		writeDAWFileNotFound(w, filepath.Base(srcPath))
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

	relPath := filepath.Join(dawDataDirName, song, dawImportsSubdir, filepath.Base(destPath))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ImportResponse{
		File: filepath.Base(destPath),
		Path: relPath,
		URL:  dawDataURL(relPath),
		Name: song,
		Size: info.Size(),
	})
}

// resolvePitchSourcePath locates a pitch-shifted stem under output/{song}/.
// If pitch is non-empty, it returns output/{song}/{song}_pitch{pitch}/{stem}.
// Otherwise it scans subdirectories of output/{song} whose name contains "_pitch"
// and returns the first one that contains the requested stem.
// All inputs are expected to have been sanitized with filepath.Base.
func resolvePitchSourcePath(absOutputBase, song, pitch, stem string) (string, error) {
	songDir := filepath.Join(absOutputBase, song)
	absSongDir, err := filepath.Abs(songDir)
	if err != nil || !strings.HasPrefix(absSongDir, absOutputBase+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid song path")
	}

	if pitch != "" {
		safePitch := filepath.Base(pitch)
		pitchDir := filepath.Join(absSongDir, song+"_pitch"+safePitch)
		absPitchDir, err := filepath.Abs(pitchDir)
		if err != nil || !strings.HasPrefix(absPitchDir, absOutputBase+string(filepath.Separator)) {
			return "", fmt.Errorf("invalid pitch path")
		}
		candidate := filepath.Join(absPitchDir, stem)
		if _, err := os.Stat(candidate); err != nil {
			return "", err
		}
		return candidate, nil
	}

	entries, err := os.ReadDir(absSongDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, "_pitch") {
			continue
		}
		candidate := filepath.Join(absSongDir, name, stem)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("source file not found")
}
