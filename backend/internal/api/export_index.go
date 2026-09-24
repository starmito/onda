package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// exportIndexFileName is the name of the per-song export registry persisted in
// the configuration directory. It keeps track of files produced by export
// endpoints so they can be hidden from stem and pitch listings without
// guessing prefixes or templates.
const exportIndexFileName = "export_index.json"

// exportIndexMinParenthesisedGroups is the minimum number of parenthesised
// groups that a legacy filename must contain after the song name to be treated
// as an export produced by the configurable template. The default template is
// "{song} ({pitches}) ({suffix})", so one group is enough to cover real
// exports while still making the rule obvious in the logs.
const exportIndexMinParenthesisedGroups = 1

// exportIndex records which filenames are exports for each song. It is the
// source of truth used by stem/pitch listings to hide exported mixdowns.
//
// The index is seeded once from existing files whose names match the export
// template shape, and updated every time an export endpoint writes a file.
type exportIndex struct {
	mu   sync.RWMutex
	dict map[string]map[string]struct{}
}

var (
	// globalExportIndex is the package-level export registry. It is lazily
	// loaded and cached by the path returned by exportIndexPath() so that
	// changes to the data root (and therefore to the config directory) pick up
	// the right index automatically. This matters for tests that isolate each
	// case under a different temporary root.
	globalExportIndex     *exportIndex
	globalExportIndexMu   sync.Mutex
	globalExportIndexPath string
)

// exportIndexPath returns the absolute path to the persisted export index in
// the configuration directory. The configuration directory is resolved through
// mustConfigDir() so it follows env/settings/data-root precedence.
func exportIndexPath() string {
	return filepath.Join(mustConfigDir(), exportIndexFileName)
}

// getExportIndex returns the package-level export index, loading it from disk
// (and seeding it from the current output directory when absent) whenever the
// effective index path changes.
func getExportIndex() *exportIndex {
	globalExportIndexMu.Lock()
	defer globalExportIndexMu.Unlock()

	path := exportIndexPath()
	if globalExportIndex == nil || globalExportIndexPath != path {
		globalExportIndex = loadExportIndex()
		globalExportIndexPath = path
	}
	return globalExportIndex
}

// resetExportIndexForTests clears the cached index. It is only used by tests
// that need to re-seed against a fresh data root.
func resetExportIndexForTests() {
	globalExportIndexMu.Lock()
	defer globalExportIndexMu.Unlock()
	globalExportIndex = nil
	globalExportIndexPath = ""
}

// loadExportIndex reads the persisted index, or creates a new one seeded from
// the output directory when the file does not exist. Seeding is a one-off
// migration for exports that were written before this index existed.
func loadExportIndex() *exportIndex {
	idx := &exportIndex{dict: make(map[string]map[string]struct{})}

	path := exportIndexPath()
	data, err := readConfigFile(path)
	if err == nil && len(data) > 0 {
		var loaded map[string][]string
		if err := json.Unmarshal(data, &loaded); err == nil {
			for song, files := range loaded {
				for _, f := range files {
					idx.add(song, f)
				}
			}
			return idx
		}
		Log("backend", "warn", fmt.Sprintf("export index %s is corrupt, re-seeding: %v", path, err))
	}

	// No index yet (or corrupt): scan output/<song>/ directories once and mark
	// legacy exports whose names look like the export template.
	idx.seedFromOutput()
	if err := idx.save(); err != nil {
		Log("backend", "warn", fmt.Sprintf("failed to save seeded export index: %v", err))
	}
	return idx
}

// seedFromOutput scans output/<song>/ directories and their pitch subgroups,
// registering any audio file whose name matches the export-template shape:
// song name followed by one or more parenthesised groups and an audio
// extension. Every matched file is logged so future surprises can be audited.
func (idx *exportIndex) seedFromOutput() {
	outputDir, err := sub("output")
	if err != nil {
		return
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		song := entry.Name()
		songDir := filepath.Join(outputDir, song)
		dirEntries, err := os.ReadDir(songDir)
		if err != nil {
			continue
		}
		for _, de := range dirEntries {
			if de.IsDir() {
				// Pitch-shift subdirectories may also contain exports whose
				// name still starts with the base song name.
				if strings.Contains(de.Name(), "_pitch") {
					idx.seedSongDir(song, filepath.Join(songDir, de.Name()))
				}
				continue
			}
			name := de.Name()
			if !isAudioStem(name) {
				continue
			}
			if looksLikeExportName(song, name) {
				idx.add(song, name)
				Log("backend", "info", fmt.Sprintf("seeded export index: hiding %q for song %q", name, song))
			}
		}
	}
}

// seedSongDir scans a single directory (base song dir or pitch subgroup) and
// registers audio files that look like exports for the given song.
func (idx *exportIndex) seedSongDir(song, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !isAudioStem(name) {
			continue
		}
		if looksLikeExportName(song, name) {
			idx.add(song, name)
			Log("backend", "info", fmt.Sprintf("seeded export index: hiding %q for song %q", name, song))
		}
	}
}

// looksLikeExportName reports whether name follows the configurable export
// name template: the song name followed by one or more parenthesised groups
// and an audio extension. Examples that match the default template:
//   - "Song (0) (mezcla).flac"
//   - "Song (+1) (export).mp3"
//
// Stems like "vocals.wav" do not match because they do not start with the
// song name plus parenthesised groups.
func looksLikeExportName(song, name string) bool {
	if !isAudioStem(name) {
		return false
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	// The base must start with the song name and then contain at least one
	// parenthesised group. Whitespace between the song name and the first
	// group is optional in the check (the template inserts a space, but we
	// accept either to stay robust).
	if !strings.HasPrefix(base, song) {
		return false
	}
	afterSong := strings.TrimSpace(strings.TrimPrefix(base, song))
	if afterSong == "" {
		return false
	}
	return countParenthesisedGroups(afterSong) >= exportIndexMinParenthesisedGroups
}

// countParenthesisedGroups counts contiguous leading groups of the form
// "(text)" optionally separated by whitespace. It returns 0 if the first
// non-whitespace character is not '(' or if any group is unclosed/empty.
func countParenthesisedGroups(s string) int {
	s = strings.TrimSpace(s)
	count := 0
	for s != "" {
		if !strings.HasPrefix(s, "(") {
			return count
		}
		closeIdx := strings.Index(s, ")")
		if closeIdx < 0 {
			return count
		}
		if closeIdx == 1 {
			// Empty group "()" is not a meaningful export segment.
			return count
		}
		count++
		s = strings.TrimSpace(s[closeIdx+1:])
	}
	return count
}

// add registers filename as an export for song in memory.
func (idx *exportIndex) add(song, filename string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.dict[song] == nil {
		idx.dict[song] = make(map[string]struct{})
	}
	idx.dict[song][filename] = struct{}{}
}

// contains reports whether filename is a known export for song.
func (idx *exportIndex) contains(song, filename string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if files, ok := idx.dict[song]; ok {
		_, found := files[filename]
		return found
	}
	return false
}

// save persists the index to disk.
func (idx *exportIndex) save() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	path := exportIndexPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	out := make(map[string][]string, len(idx.dict))
	for song, files := range idx.dict {
		list := make([]string, 0, len(files))
		for f := range files {
			list = append(list, f)
		}
		if len(list) > 0 {
			out[song] = list
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// registerExportFile records a newly written export file for song and persists
// the index. Export endpoints call this after successfully writing the output
// file.
func registerExportFile(song, filename string) {
	idx := getExportIndex()
	idx.add(song, filename)
	if err := idx.save(); err != nil {
		Log("backend", "warn", fmt.Sprintf("failed to save export index after registering %q: %v", filename, err))
	}
}

// isRegisteredExport reports whether filename is a known export for song.
// It is used by stem/pitch listings in addition to the prefix-based filter.
func isRegisteredExport(song, filename string) bool {
	return getExportIndex().contains(song, filename)
}
