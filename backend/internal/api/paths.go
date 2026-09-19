package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dataRoot returns the single data root directory for the application.
// If ONDA_DATA_DIR is set, it is used as the root; otherwise the current
// behaviour (findProjectRoot) is preserved so nothing changes until the
// variable is configured explicitly.
func dataRoot() string {
	if root := os.Getenv("ONDA_DATA_DIR"); root != "" {
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
