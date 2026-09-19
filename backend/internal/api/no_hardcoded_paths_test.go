package api

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoHardcodedDataPaths is a guardian test: it walks the production source
// tree and fails if it finds hardcoded data paths or data-root lookups outside
// the helper in paths.go.
//
// Scope (production only, excludes *_test.go, node_modules, .git, dist):
//   - backend/**/*.go
//   - pipeline.sh, entrypoint.sh, deploy.sh
//   - docker-compose*.yml
//   - onda/*.py and the inference_*.py, separate.py, UVR.py files at repo root
//
// TODO: extend the scan to frontend sources once the DAW hardcoded-path
// cleanup (tarea separada) is complete. Add frontend/** here and remove this
// comment.
func TestNoHardcodedDataPaths(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	repoRoot := findProjectRootFrom(cwd)
	if repoRoot == "" {
		t.Fatal("cannot find repository root (no VERSION marker)")
	}

	var violations []string

	// Go backend.
	backendDir := filepath.Join(repoRoot, "backend")
	if err := filepath.Walk(backendDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if strings.HasSuffix(p, "_test.go") {
			return nil
		}
		checkGoFile(repoRoot, p, &violations)
		return nil
	}); err != nil {
		t.Fatalf("walk backend go files: %v", err)
	}

	// Shell scripts.
	for _, name := range []string{"pipeline.sh", "entrypoint.sh", "deploy.sh"} {
		p := filepath.Join(repoRoot, name)
		if _, err := os.Stat(p); err == nil {
			checkShellFile(repoRoot, p, &violations)
		}
	}

	// Compose files.
	composeFiles, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yml"))
	if err != nil {
		t.Fatalf("glob compose files: %v", err)
	}
	for _, p := range composeFiles {
		checkComposeFile(repoRoot, p, &violations)
	}

	// Python pipeline files.
	pythonFiles, err := filepath.Glob(filepath.Join(repoRoot, "onda", "*.py"))
	if err != nil {
		t.Fatalf("glob onda python files: %v", err)
	}
	for _, pattern := range []string{"inference_*.py", "separate.py", "UVR.py"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		pythonFiles = append(pythonFiles, matches...)
	}
	for _, p := range pythonFiles {
		checkPythonFile(repoRoot, p, &violations)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("hardcoded data paths found:\n%s", strings.Join(violations, "\n"))
	}
}

// dataPathPatterns are the literal data subpaths that must never be hardcoded.
// The /app variants are listed first so the error message names the full,
// absolute variant when both match.
var dataPathPatterns = []string{
	"/app/input", "/app/output", "/app/daw-data", "/app/input_rubberband",
	"/app/models", "/app/config", "/app/logs", "/app/data",
	"/input", "/output", "/daw-data", "/input_rubberband",
	"/models", "/config", "/logs",
}

// allowedAppPathPrefixes are paths that belong to the application image, not
// to user data. A string that starts with one of these is ignored.
var allowedAppPathPrefixes = []string{
	"/app/pipeline.sh",
	"/app/uvr_models.json",
	"/app/VERSION",
	"/app/lib_v5",
	"/app/detect_gpu.sh",
	"/app/.cache/",
	"/opt/pytorch-backends",
}

var inferencePyRE = regexp.MustCompile(`^/app/inference_[^/]+\.py`)

// legacyFindProjectRootCalls lists the remaining direct findProjectRoot() calls
// outside paths.go. Kept empty after the migration: every data path must go
// through dataRoot()/mustSub().
var legacyFindProjectRootCalls = map[string][]int{}

// legacyHardcodedDataPaths lists remaining hardcoded data paths that pre-date
// the ONDA_DATA_DIR refactor. Kept empty after the migration.
var legacyHardcodedDataPaths = map[string][]int{}

func checkGoFile(repoRoot, path string, violations *[]string) {
	rel, _ := filepath.Rel(repoRoot, path)
	base := filepath.Base(path)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		// A parse error is not a path violation; report it separately.
		*violations = append(*violations, fmt.Sprintf("%s:0: parse error: %v", rel, err))
		return
	}

	isPathsGo := base == "paths.go"
	isThisTest := base == "no_hardcoded_paths_test.go"
	allowedLines := legacyFindProjectRootCalls[rel]

	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && !isPathsGo && !isThisTest {
			if isFindProjectRootCall(call) {
				pos := fset.Position(call.Pos())
				if !intSliceContains(allowedLines, pos.Line) {
					*violations = append(*violations, fmt.Sprintf("%s:%d: findProjectRoot() called outside paths.go", rel, pos.Line))
				}
			}
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && !isThisTest {
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if reason := checkStringForDataPath(s, false); reason != "" {
				pos := fset.Position(lit.Pos())
				if !intSliceContains(legacyHardcodedDataPaths[rel], pos.Line) {
					*violations = append(*violations, fmt.Sprintf("%s:%d: %s in %q", rel, pos.Line, reason, s))
				}
			}
		}
		return true
	})
}

func isFindProjectRootCall(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "findProjectRoot"
}

func checkStringForDataPath(s string, allowAppDataRoot bool) string {
	for _, p := range dataPathPatterns {
		if allowAppDataRoot && p == "/app/data" {
			continue
		}
		start := 0
		for {
			idx := strings.Index(s[start:], p)
			if idx < 0 {
				break
			}
			idx += start
			end := idx + len(p)
			// Require a boundary after the matched directory name.
			if end < len(s) && isWordChar(s[end]) {
				start = end
				continue
			}
			// Require the path to start a path component, not be glued to a
			// variable name, relative component, or prose word.
			if idx > 0 && !isPathStartChar(s[idx-1]) {
				start = end
				continue
			}
			if hasAllowedAppPathPrefix(s) {
				return ""
			}
			return fmt.Sprintf("hardcoded data path %q", p)
		}
	}
	return ""
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// isPathStartChar reports whether c can precede a standalone absolute path
// literal inside a string. It filters out relative components, variable names
// and prose words (e.g. "data/input", "$ROOT/input", "input/output").
func isPathStartChar(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '=', '(', '[', '{', '"', '\'', ';', ':', '|', '&', ',', '<', '>', '~', '!', '?':
		return true
	}
	return false
}

func hasAllowedAppPathPrefix(s string) bool {
	for _, p := range allowedAppPathPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return inferencePyRE.MatchString(s)
}

func intSliceContains(haystack []int, needle int) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// shell / python / compose checkers.

func checkShellFile(repoRoot, path string, violations *[]string) {
	rel, _ := filepath.Rel(repoRoot, path)
	content, err := os.ReadFile(path)
	if err != nil {
		*violations = append(*violations, fmt.Sprintf("%s:0: cannot read: %v", rel, err))
		return
	}
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		clean := stripInlineComment(line, "#")
		if reason := checkStringForDataPath(clean, true); reason != "" {
			*violations = append(*violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
		}
	}
}

func checkPythonFile(repoRoot, path string, violations *[]string) {
	rel, _ := filepath.Rel(repoRoot, path)
	content, err := os.ReadFile(path)
	if err != nil {
		*violations = append(*violations, fmt.Sprintf("%s:0: cannot read: %v", rel, err))
		return
	}
	lines := strings.Split(string(content), "\n")
	inTriple := false
	tripleQuote := ""
	for i, line := range lines {
		var clean string
		clean, inTriple, tripleQuote = stripPythonLine(line, inTriple, tripleQuote)
		if inTriple && clean == "" {
			continue
		}
		clean = stripInlineComment(clean, "#")
		if reason := checkStringForDataPath(clean, false); reason != "" {
			*violations = append(*violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
		}
	}
}

var composeVolumeRE = regexp.MustCompile(`^\s*-\s+([^:\s]+):(/app/[^:\s]+)(?::\w+)?\s*$`)

func checkComposeFile(repoRoot, path string, violations *[]string) {
	rel, _ := filepath.Rel(repoRoot, path)
	content, err := os.ReadFile(path)
	if err != nil {
		*violations = append(*violations, fmt.Sprintf("%s:0: cannot read: %v", rel, err))
		return
	}
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		clean := stripInlineComment(line, "#")
		m := composeVolumeRE.FindStringSubmatch(clean)
		if m == nil {
			continue
		}
		source, target := m[1], m[2]
		if hasAllowedAppPathPrefix(target) {
			continue
		}
		// The only allowed data mount is the single root ./data:/app/data.
		if target == "/app/data" {
			if source == "./data" {
				continue
			}
			*violations = append(*violations, fmt.Sprintf("%s:%d: data root mount must be ./data:/app/data: %s", rel, i+1, strings.TrimSpace(line)))
			continue
		}
		if reason := checkStringForDataPath(target, true); reason != "" {
			*violations = append(*violations, fmt.Sprintf("%s:%d: data subpath mount: %s", rel, i+1, strings.TrimSpace(line)))
		}
	}
}

// stripInlineComment removes the first unquoted occurrence of marker and
// everything after it. It understands single and double quotes and backslash
// escapes, which is enough to avoid stripping markers that live inside string
// literals in shell/python/yaml.
func stripInlineComment(line, marker string) string {
	inSingle, inDouble, escape := false, false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if !inSingle && !inDouble && strings.HasPrefix(line[i:], marker) {
			return line[:i]
		}
	}
	return line
}

// stripPythonLine removes Python triple-quoted string content from a line and
// reports whether the parser is still inside a triple-quoted string afterwards.
// It is a best-effort helper to ignore docstrings and multi-line string literals
// when looking for hardcoded paths.
func stripPythonLine(line string, inTriple bool, quote string) (clean string, stillInTriple bool, newQuote string) {
	if !inTriple {
		// Strip single-line triple-quoted strings first.
		for {
			single := tripleQuotedStringRE.ReplaceAllString(line, "")
			if single == line {
				break
			}
			line = single
		}
		// Look for an opening triple quote.
		for _, q := range []string{"\"\"\"", "'''"} {
			if idx := strings.Index(line, q); idx >= 0 {
				rest := line[idx+3:]
				if closeIdx := strings.Index(rest, q); closeIdx >= 0 {
					// Opens and closes on the same line; strip and keep scanning.
					line = line[:idx] + rest[closeIdx+3:]
					return stripPythonLine(line, false, "")
				}
				// Opens a multi-line string.
				return line[:idx], true, q
			}
		}
		return line, false, ""
	}

	// We are inside a triple-quoted string; look for the closing quote.
	if idx := strings.Index(line, quote); idx >= 0 {
		return line[idx+3:], false, ""
	}
	return "", true, quote
}

var tripleQuotedStringRE = regexp.MustCompile(`"""[^"]*"""|\'\'\'[^\']*\'\'\'`)
