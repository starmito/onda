package api

import (
	"os"
	"strings"
)

// Version is injected at build time via -ldflags from the matching git tag
// (onda-vX.Y.Z). If not injected, it falls back to the VERSION file so local
// development still reports a version.
var Version string

func init() {
	// If ldflags injected a non-empty value, keep it as the source of truth.
	if Version != "" {
		return
	}

	// /app/VERSION is the application version marker inside the container image,
	// not a data path, so it is intentionally left as an absolute container path.
	for _, path := range []string{"VERSION", "/app/VERSION"} {
		data, err := os.ReadFile(path)
		if err == nil {
			Version = strings.TrimSpace(string(data))
			if Version != "" {
				return
			}
		}
	}

	Version = "unknown"
}
