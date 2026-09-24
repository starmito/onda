package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KeyAlternative describes a runner-up key candidate.
type KeyAlternative struct {
	Key      string  `json:"key"`
	Scale    string  `json:"scale"`
	Strength float64 `json:"strength"`
}

// KeyResponse is the JSON response for the key detection endpoint.
type KeyResponse struct {
	Key          string           `json:"key"`
	Scale        string           `json:"scale"`
	Strength     float64          `json:"strength"`
	Alternatives []KeyAlternative `json:"alternatives"`
	Dubious      bool             `json:"dubious"`
}

// keydetectRunner runs keydetect.py against an audio file. It is a variable so
// tests can substitute a mock implementation.
var keydetectRunner = func(inputPath string) ([]byte, error) {
	script, err := keydetectScriptPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("python3", script, inputPath)
	Log("backend", "info", fmt.Sprintf("keydetect: running %s %s %s", cmd.Path, script, inputPath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		Log("backend", "error", fmt.Sprintf("keydetect failed: %v output=%q", err, string(out)))
	} else {
		Log("backend", "info", fmt.Sprintf("keydetect output: %s", string(out)))
	}
	return out, err
}

// keydetectScriptPath returns the path to keydetect.py, preferring the
// container location /app/keydetect.py when it exists and falling back to the
// configured data root or its parent directory for development builds.
func keydetectScriptPath() (string, error) {
	containerPath := "/app/keydetect.py"
	if _, err := os.Stat(containerPath); err == nil {
		return containerPath, nil
	}
	candidates := []string{
		filepath.Join(resolveProjectRoot(), "keydetect.py"),
		filepath.Join(filepath.Dir(resolveProjectRoot()), "keydetect.py"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("keydetect.py not found")
}

// handleKeyDetect detects the musical key of an audio file.
//
//   - POST /api/key with multipart file upload: detects the uploaded file.
//   - GET  /api/key?file=...: detects an existing file in input/ or daw-data/.
func (s *Server) handleKeyDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var inputPath string
	var cleanup func()

	if r.Method == http.MethodPost {
		if err := r.ParseMultipartForm(200 << 20); err != nil {
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
		if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" && ext != ".m4a" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported file extension"})
			return
		}

		tmpDir := filepath.Join(mustSub("input"), "keydetect-tmp")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create temp directory"})
			return
		}

		tmpFile, err := os.CreateTemp(tmpDir, "keydetect-*"+ext)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create temp file"})
			return
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()

		dst, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			os.Remove(tmpPath)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to open temp file"})
			return
		}
		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			os.Remove(tmpPath)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to write temp file"})
			return
		}
		dst.Close()

		inputPath = tmpPath
		cleanup = func() { _ = os.Remove(tmpPath) }
	} else {
		file := r.URL.Query().Get("file")
		if file == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing file parameter"})
			return
		}

		sourcePath, safeName, _, _, err := resolveDAWAudioSource(file)
		if err != nil {
			writeDAWFileNotFound(w, safeName, file)
			return
		}
		inputPath = sourcePath
		cleanup = func() {}
	}

	if cleanup != nil {
		defer cleanup()
	}

	out, err := keydetectRunner(inputPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "key detection failed",
			"detail": strings.TrimSpace(string(out)),
		})
		return
	}

	var resp KeyResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "failed to parse key detection output",
			"detail": strings.TrimSpace(string(out)),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
