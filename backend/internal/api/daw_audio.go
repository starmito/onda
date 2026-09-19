package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Subdirectory names inside each daw-data/{song}/ tree.
const (
	dawDataDirName    = "daw-data"
	dawOriginalSubdir = "original"
	dawImportsSubdir  = "imports"
	dawEditsSubdir    = "edits"
	dawTmpSubdir      = "tmp"
)

// errDAWAudioNotFound is returned by resolveDAWAudioSource when the requested
// audio file does not exist in either the input/ or daw-data/ directories.
var errDAWAudioNotFound = errors.New("daw audio file not found")

// errDAWPathTraversal is returned when a user-supplied path escapes daw-data/.
var errDAWPathTraversal = errors.New("path traversal detected")

// dawFileNotFoundResponse is the structured JSON body returned by DAW audio
// endpoints when the source file is missing. It keeps the legacy "error" field
// for backwards compatibility and adds stable "code", "file" and "help" fields
// so the frontend can show a clear recovery message.
type dawFileNotFoundResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	File  string `json:"file"`
	Help  string `json:"help"`
}

// dawFileSource describes a file located inside the daw-data/{song}/ tree.
type dawFileSource struct {
	Song    string
	Subdir  string // original, imports, edits or tmp
	Name    string
	AbsPath string
}

// songDirName returns a sanitized directory name for a song. It reuses
// sanitizeFilenameStem so the naming rules stay consistent with imports.
func songDirName(name string) string {
	s := sanitizeFilenameStem(name)
	s = strings.TrimSpace(s)
	if s == "" {
		s = "audio"
	}
	return s
}

// hasPathSeparator reports whether s contains a slash or backslash.
func hasPathSeparator(s string) bool {
	return strings.ContainsAny(s, `/\`)
}

// safeJoinDAWData builds an absolute path inside daw-data/{song}/{subdir}/{name}
// and verifies that the resolved path stays inside daw-data. Empty subdir means
// daw-data/{song}/{name}.
func safeJoinDAWData(projectRoot, song, subdir, name string) (string, error) {
	if song == "" || song == "." || song == ".." || hasPathSeparator(song) || strings.Contains(song, "..") {
		return "", errDAWPathTraversal
	}
	if subdir != "" && (subdir == "." || subdir == ".." || hasPathSeparator(subdir) || strings.Contains(subdir, "..")) {
		return "", errDAWPathTraversal
	}
	baseName := filepath.Base(name)
	if baseName == "" || baseName == "." || hasPathSeparator(name) || strings.Contains(name, "..") {
		return "", errDAWPathTraversal
	}

	dir := filepath.Join(projectRoot, dawDataDirName, song)
	if subdir != "" {
		dir = filepath.Join(dir, subdir)
	}
	abs, err := filepath.Abs(filepath.Join(dir, baseName))
	if err != nil {
		return "", err
	}
	absDaw, err := filepath.Abs(filepath.Join(projectRoot, dawDataDirName))
	if err != nil {
		return "", err
	}
	if abs == absDaw || !strings.HasPrefix(abs, absDaw+string(filepath.Separator)) {
		return "", errDAWPathTraversal
	}
	return abs, nil
}

// parseDAWTreePath parses a user-supplied reference that may already be a
// relative path inside the daw-data tree. Accepted forms:
//
//   - "daw-data/{song}/{subdir}/{name}"
//   - "{song}/{subdir}/{name}"
//   - "{song}/{name}" (treated as original/{name})
//
// It returns the parsed source and true only when the path exists.
func parseDAWTreePath(projectRoot, file string) (dawFileSource, bool) {
	file = strings.TrimSpace(file)
	if file == "" {
		return dawFileSource{}, false
	}

	// Strip an optional "daw-data/" or "daw-data\" prefix.
	rel := file
	for _, prefix := range []string{dawDataDirName + "/", dawDataDirName + "\\"} {
		if strings.HasPrefix(rel, prefix) {
			rel = strings.TrimPrefix(rel, prefix)
		}
	}

	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return dawFileSource{}, false
	}

	song := parts[0]
	if song == "" || song == "." || song == ".." || hasPathSeparator(song) || strings.Contains(song, "..") {
		return dawFileSource{}, false
	}

	var subdir, name string
	if len(parts) == 2 {
		// "song/name" is treated as the original subdir.
		subdir = dawOriginalSubdir
		name = parts[1]
	} else {
		subdir = parts[1]
		if subdir != dawOriginalSubdir && subdir != dawImportsSubdir && subdir != dawEditsSubdir && subdir != dawTmpSubdir {
			return dawFileSource{}, false
		}
		name = filepath.Join(parts[2:]...)
	}

	abs, err := safeJoinDAWData(projectRoot, song, subdir, name)
	if err != nil {
		return dawFileSource{}, false
	}
	if _, err := os.Stat(abs); err != nil {
		return dawFileSource{}, false
	}
	return dawFileSource{
		Song:    song,
		Subdir:  subdir,
		Name:    filepath.Base(name),
		AbsPath: abs,
	}, true
}

// resolveDAWAudioSource locates an audio file referenced by name or by a
// relative path inside the daw-data tree. It searches in this order:
//
//  1. input/{name}
//  2. An explicit daw-data tree path (daw-data/{song}/[subdir]/{name})
//  3. The daw-data tree, scanning every song directory for the base name
//
// On success it returns the absolute path, the safe base name, the song
// directory (when inside daw-data) and the subdirectory (original/imports/edits/tmp).
func resolveDAWAudioSource(file string) (absPath, safeName, song, subdir string, err error) {
	projectRoot := findProjectRoot()
	safeName = filepath.Base(file)

	// 1. Flat input/ lookup.
	inputPath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(inputPath); err == nil {
		return inputPath, safeName, "", "", nil
	}

	// 2. Explicit tree path.
	if src, ok := parseDAWTreePath(projectRoot, file); ok {
		return src.AbsPath, src.Name, src.Song, src.Subdir, nil
	}

	// 3. Search the whole daw-data tree for the base name.
	dawRoot := filepath.Join(projectRoot, dawDataDirName)
	if entries, readErr := os.ReadDir(dawRoot); readErr == nil {
		searchOrder := []string{dawOriginalSubdir, dawImportsSubdir, dawEditsSubdir, dawTmpSubdir}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			songDir := entry.Name()
			for _, sd := range searchOrder {
				candidate := filepath.Join(dawRoot, songDir, sd, safeName)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, safeName, songDir, sd, nil
				}
			}
		}
	}

	return "", safeName, "", "", errDAWAudioNotFound
}

// writeDAWFileNotFound writes a 404 JSON response with a structured error that
// identifies the missing file and tells the user to re-upload it.
func writeDAWFileNotFound(w http.ResponseWriter, fileName string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(dawFileNotFoundResponse{
		Error: "file not found",
		Code:  "file_not_found",
		File:  fileName,
		Help:  "El archivo de audio no está disponible. Vuelve a subirlo para continuar.",
	})
}

// isDAWEditFile reports whether a file living inside daw-data is an effect/edit
// output, as opposed to an original upload or imported stem.
func isDAWEditFile(subdir, name string) bool {
	if subdir == dawEditsSubdir {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "eq_") ||
		strings.HasPrefix(lower, "compressor_") ||
		strings.HasPrefix(lower, "reverb_") ||
		strings.HasPrefix(lower, "delay_") ||
		strings.HasPrefix(lower, "chorus_") ||
		strings.HasPrefix(lower, "flanger_") ||
		strings.HasPrefix(lower, "phaser_") ||
		strings.HasPrefix(lower, "tremolo_") ||
		strings.HasPrefix(lower, "noisegate_") ||
		strings.HasPrefix(lower, "trim_") ||
		strings.HasPrefix(lower, "fade_") ||
		strings.HasPrefix(lower, "tempo_") ||
		strings.HasPrefix(lower, "tempo_per_bar_") ||
		strings.HasPrefix(lower, "export_")
}

// songTempDir returns the per-song temporary directory path.
func songTempDir(projectRoot, song string) string {
	return filepath.Join(projectRoot, dawDataDirName, song, dawTmpSubdir)
}

// safeDAWSongDir returns the absolute path to daw-data/{song} after verifying
// that the song name does not contain path separators or traversal sequences
// and that the resolved directory stays inside daw-data/.
func safeDAWSongDir(projectRoot, song string) (string, error) {
	if song == "" || song == "." || song == ".." || hasPathSeparator(song) || strings.Contains(song, "..") {
		return "", errDAWPathTraversal
	}

	song = songDirName(song)
	if song == "" || song == "." || song == ".." {
		return "", errDAWPathTraversal
	}

	dir := filepath.Join(projectRoot, dawDataDirName, song)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absDaw, err := filepath.Abs(filepath.Join(projectRoot, dawDataDirName))
	if err != nil {
		return "", err
	}
	if abs == absDaw || !strings.HasPrefix(abs, absDaw+string(filepath.Separator)) {
		return "", errDAWPathTraversal
	}
	return abs, nil
}

// dawSongDeleteResult is the JSON response for a successful DAW song deletion.
type dawSongDeleteResult struct {
	Deleted bool   `json:"deleted"`
	Song    string `json:"song"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
}

// deleteDAWSongDir removes the entire daw-data/{song}/ directory and returns the
// number of files deleted and the total bytes freed. It refuses to delete paths
// that are not inside daw-data/.
func deleteDAWSongDir(projectRoot, song string) (files int, bytes int64, err error) {
	dir, err := safeDAWSongDir(projectRoot, song)
	if err != nil {
		return 0, 0, err
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, 0, err
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			files++
			bytes += info.Size()
		}
		return nil
	})

	if err := os.RemoveAll(dir); err != nil {
		return 0, 0, err
	}
	return files, bytes, nil
}

// handleDeleteDAWSong deletes the whole daw-data/{song}/ tree, including the
// original upload, imports, edits and tmp subdirectories.
// DELETE /api/daw/songs/{song}
func (s *Server) handleDeleteDAWSong(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	song := r.PathValue("song")
	if song == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing song name"})
		return
	}

	projectRoot := findProjectRoot()
	dir, err := safeDAWSongDir(projectRoot, song)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "invalid song name",
			"details": err.Error(),
		})
		return
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "song not found",
			"song":  song,
		})
		return
	}

	files, bytes, err := deleteDAWSongDir(projectRoot, song)
	if err != nil {
		if errors.Is(err, errDAWPathTraversal) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "invalid song name",
				"details": err.Error(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "info", fmt.Sprintf("Deleted DAW song: %s (%d files, %d bytes freed)", song, files, bytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(dawSongDeleteResult{
		Deleted: true,
		Song:    song,
		Files:   files,
		Bytes:   bytes,
	})
}
