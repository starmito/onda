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

// configDir returns the directory where user configuration files are stored.
// Precedence:
//   1. ONDA_CONFIG_DIR environment variable.
//   2. config_dir value persisted in the settings file.
//   3. <dataRoot>/config as the default.
func configDir() string {
	if dir := os.Getenv("ONDA_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := persistedConfigDir(); dir != "" {
		return dir
	}
	return mustSub("config")
}

// configDirWithSource returns the effective config directory and where it comes
// from according to the precedence rules.
func configDirWithSource() (string, string) {
	if dir := os.Getenv("ONDA_CONFIG_DIR"); dir != "" {
		return dir, "env"
	}
	if dir := persistedConfigDir(); dir != "" {
		return dir, "settings"
	}
	return mustSub("config"), "default"
}

// mustConfigDir is like configDir but panics if the directory cannot be
// resolved. It is intended for callers that already know the data root is set.
func mustConfigDir() string {
	dir := configDir()
	if dir == "" {
		panic("configDir resolved to empty path")
	}
	return dir
}

// legacyConfigDir returns the historical configuration directory under the
// current data root. It is used as a read-only fallback when the user has
// chosen a different config dir so no existing configuration is lost.
func legacyConfigDir() string {
	return mustSub("config")
}

// fallbackConfigPath maps a primary config file path to its legacy counterpart
// under <dataRoot>/config. If the primary path is not inside the current
// config directory, or if the legacy directory is the same as the current one,
// it returns an empty string.
func fallbackConfigPath(primary string) string {
	cfg := configDir()
	leg := legacyConfigDir()
	if cfg == leg {
		return ""
	}
	rel, err := filepath.Rel(cfg, primary)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.Join(leg, rel)
}

// readConfigFile reads a configuration file from the primary config directory,
// falling back to the legacy <dataRoot>/config directory when the file does not
// exist there. Other errors from the primary path are returned as-is.
func readConfigFile(primary string) ([]byte, error) {
	data, err := os.ReadFile(primary)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	legacy := fallbackConfigPath(primary)
	if legacy == "" {
		return nil, err
	}
	data, lerr := os.ReadFile(legacy)
	if lerr != nil {
		return nil, err
	}
	return data, nil
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
