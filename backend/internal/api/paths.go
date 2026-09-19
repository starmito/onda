package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dataRoot returns the single data root directory for the application.
// Precedence:
//   1. ONDA_DATA_DIR environment variable.
//   2. data_root value persisted in the settings file.
//   3. Current behaviour (findProjectRoot) as a fallback.
func dataRoot() string {
	if root := os.Getenv("ONDA_DATA_DIR"); root != "" {
		return root
	}
	if root := persistedDataRoot(); root != "" {
		return root
	}
	return findProjectRoot()
}

// sub returns the absolute path for a simple subdirectory name under dataRoot.
// The name must be a single path component: no separators, no traversal,
// and not empty.
func sub(name string) (string, error) {
	if name == "" || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", errors.New("invalid subpath: must be a simple directory name")
	}
	return filepath.Join(dataRoot(), name), nil
}

// mustSub is like sub but panics with a clear message when the subpath is
// invalid. It is intended for hardcoded, known-valid subdirectory names.
func mustSub(name string) string {
	p, err := sub(name)
	if err != nil {
		panic(fmt.Sprintf("invalid data subdirectory %q: %v", name, err))
	}
	return p
}

// isContainerMode reports whether the process is running inside a Docker
// container by checking for the /.dockerenv marker file.
func isContainerMode() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// appDir returns the application directory where persisted settings are kept.
// In a container this is /app; otherwise it falls back to the project root.
func appDir() string {
	if dir := os.Getenv("ONDA_APP_DIR"); dir != "" {
		return dir
	}
	if isContainerMode() {
		return "/app"
	}
	return findProjectRoot()
}
