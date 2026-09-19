package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	ondaTestRoot string
	realRepoRoot string
)

// TestMain sets up a single hermetic project root for the whole api test
// package. It lives outside the repository and outside /tmp, is guarded by a
// VERSION marker, and is removed after the suite finishes. Any failure that
// would make findProjectRoot() resolve to the real repository aborts the run.
func TestMain(m *testing.M) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot get working directory: %v\n", err)
		os.Exit(1)
	}
	realRepoRoot = findProjectRootFrom(cwd)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot determine user home directory: %v\n", err)
		os.Exit(1)
	}
	ondaTestRoot = filepath.Join(home, fmt.Sprintf(".onda-test-root-%d", os.Getpid()))

	// Start from a clean slate in case a previous run left debris.
	if err := os.RemoveAll(ondaTestRoot); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot clean stale test root %s: %v\n", ondaTestRoot, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(ondaTestRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot create test root %s: %v\n", ondaTestRoot, err)
		os.Exit(1)
	}

	// Place the same marker findProjectRoot() looks for so it resolves here.
	if err := os.WriteFile(filepath.Join(ondaTestRoot, "VERSION"), []byte("test\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot write VERSION marker in test root: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("ONDA_ROOT", ondaTestRoot)

	if got := findProjectRoot(); got != ondaTestRoot {
		fmt.Fprintf(
			os.Stderr,
			"FATAL: findProjectRoot() resolved to %q instead of the hermetic test root %q; refusing to run tests against the real repository %q\n",
			got, ondaTestRoot, realRepoRoot,
		)
		os.Exit(1)
	}

	// Snapshot the package directory so we can detect tests that write outside
	// the temporary test root.
	packageBefore := map[string]bool{}
	beforeEntries, err := os.ReadDir(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot read package directory %s: %v\n", cwd, err)
		os.Exit(1)
	}
	for _, e := range beforeEntries {
		packageBefore[e.Name()] = true
	}

	code := m.Run()

	if err := os.RemoveAll(ondaTestRoot); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot remove test root %s: %v\n", ondaTestRoot, err)
	}

	afterEntries, err := os.ReadDir(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot read package directory %s after tests: %v\n", cwd, err)
		os.Exit(1)
	}
	var left []string
	for _, e := range afterEntries {
		if !packageBefore[e.Name()] {
			left = append(left, e.Name())
		}
	}
	if len(left) > 0 {
		fmt.Fprintf(os.Stderr, "FATAL: tests left new entries in package directory %s: %v\n", cwd, left)
		code = 1
	}

	os.Exit(code)
}

// findProjectRootFrom walks up from start until it finds a VERSION file.
// It does not consult ONDA_ROOT, so it can discover the real repo root
// before tests override the environment.
func findProjectRootFrom(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "VERSION")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// newTestRoot creates a fresh isolated subdirectory under the package-level
// test root and schedules its removal. Tests should use this instead of
// os.MkdirTemp(".", ...) or t.TempDir(), which either write inside the repo
// or may land in /tmp.
func newTestRoot(t *testing.T, prefix string) string {
	t.Helper()
	assertTestRoot(t)
	root, err := os.MkdirTemp(ondaTestRoot, prefix)
	if err != nil {
		t.Fatalf("failed to create test root under %s: %v", ondaTestRoot, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// setTestRoot creates a fresh isolated subdirectory and points ONDA_ROOT at it.
// Helper functions that previously created their own temp root and set the
// environment should migrate to this function.
func setTestRoot(t *testing.T, prefix string) string {
	t.Helper()
	root := newTestRoot(t, prefix)
	t.Setenv("ONDA_ROOT", root)
	return root
}

// assertTestRoot fails the test if findProjectRoot() resolves to the real
// repository or otherwise leaves the hermetic test root. Call it from any
// helper or test that wants to guarantee isolation.
func assertTestRoot(t *testing.T) {
	t.Helper()
	got := findProjectRoot()
	if realRepoRoot != "" && got == realRepoRoot {
		t.Fatalf("findProjectRoot() points to real repository %q; test is not isolated", got)
	}
	if ondaTestRoot != "" && !strings.HasPrefix(got, ondaTestRoot) {
		t.Fatalf("findProjectRoot() = %q is outside the package test root %q", got, ondaTestRoot)
	}
}

// fakePipelineScript returns a bash script suitable for tests that need a
// pipeline stand-in. It parses --output from its arguments, refuses to run if
// --output is missing or relative, and writes the requested stem files into
// the output directory. This prevents tests from accidentally writing to the
// package directory when the output argument shifts position.
func fakePipelineScript(stems ...string) string {
	var writes strings.Builder
	for _, s := range stems {
		fmt.Fprintf(&writes, "echo \"stem\" > \"$output/%s\"\n", s)
	}
	return `#!/bin/bash
set -euo pipefail
output=""
while [[ $# -gt 0 ]]; do
	case "$1" in
		--output)
			shift
			output="${1:-}"
			;;
	esac
	shift
done
if [[ -z "$output" ]]; then
	echo "fake pipeline: missing required --output" >&2
	exit 1
fi
case "$output" in
	/*) ;;
	*)
		echo "fake pipeline: --output must be an absolute path, got: $output" >&2
		exit 1
		;;
esac
mkdir -p "$output"
` + writes.String()
}
