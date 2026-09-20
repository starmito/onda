package api

import (
	"os"
	"path/filepath"
)

// readProjectFile reads a file from the current data root.
func readProjectFile(name string) ([]byte, error) {
	p := filepath.Join(dataRoot(), name)
	data, err := os.ReadFile(p)
	if err == nil {
		return data, nil
	}
	return nil, err
}

// readImageFile reads a bundled file that ships with the application image.
// It tries the application directory first (e.g. /app in a container, or the
// project root in development) and falls back to the data root so local data
// can override or supplement the bundled catalog.
func readImageFile(name string) ([]byte, error) {
	p := filepath.Join(appDir(), name)
	data, err := os.ReadFile(p)
	if err == nil {
		return data, nil
	}
	return readProjectFile(name)
}
