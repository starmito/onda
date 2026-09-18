package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

// errDAWAudioNotFound is returned by resolveDAWAudioSource when the requested
// audio file does not exist in either the input/ or daw-data/ directories.
var errDAWAudioNotFound = errors.New("daw audio file not found")

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

// resolveDAWAudioSource looks for an audio file by base name first in input/
// and then in daw-data/ under the project root. It returns the absolute path
// to the existing file, the safe base name, and nil on success. If the file is
// not found it returns errDAWAudioNotFound.
func resolveDAWAudioSource(file string) (sourcePath, safeName string, err error) {
	safeName = filepath.Base(file)
	projectRoot := findProjectRoot()

	inputPath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(inputPath); err == nil {
		return inputPath, safeName, nil
	}

	dawPath := filepath.Join(projectRoot, "daw-data", safeName)
	if _, err := os.Stat(dawPath); err == nil {
		return dawPath, safeName, nil
	}

	return "", safeName, errDAWAudioNotFound
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
