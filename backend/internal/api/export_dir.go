package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// exportDirFileURL returns the API URL used to download a file that lives in
// the configured export directory. It is used by export endpoints instead of
// the data-root static routes so downloads keep working when the export folder
// is outside the data root.
func exportDirFileURL(file string) string {
	return "/api/export/files/" + url.PathEscape(filepath.Base(file))
}

// errExportDirNotConfigured is returned when an operation needs a configured
// export directory but none is set.
var errExportDirNotConfigured = errors.New("export directory not configured")

// resolveExportDirFile returns the absolute path for a file inside the
// configured export directory, or an error if the export dir is not configured
// or the resulting path escapes it.
func resolveExportDirFile(file string) (string, error) {
	dir := exportDir()
	if dir == "" {
		return "", errExportDirNotConfigured
	}

	name := filepath.Base(file)
	if name == "" || name == "." || name == ".." || strings.Contains(name, "..") {
		return "", errors.New("invalid export file name")
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filepath.Join(absDir, name))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
		return "", errors.New("export file path escapes export directory")
	}
	return absPath, nil
}

// handleExportFileServe serves a single file from the configured export
// directory. This route must remain reachable even when the export directory
// lies outside the data root.
// GET /api/export/files/{file}
func (s *Server) handleExportFileServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	file := r.PathValue("file")
	if file == "" {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}

	path, err := resolveExportDirFile(file)
	if err != nil {
		if errors.Is(err, errExportDirNotConfigured) {
			http.Error(w, "export directory not configured", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.ServeFile(w, r, path)
}
