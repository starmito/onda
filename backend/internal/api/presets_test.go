package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

// resetPresetsState snapshots the global preset state and restores it after the test.
// It also re-initialises the state to a clean factory-only baseline under the test root.
func resetPresetsState(t *testing.T) {
	t.Helper()

	oldUserPresets := make(map[string]cli.Preset)
	userPresetsMu.RLock()
	for k, v := range userPresets {
		oldUserPresets[k] = v
	}
	userPresetsMu.RUnlock()

	oldDeleted := make(map[string]struct{})
	deletedPresetsMu.RLock()
	for k := range deletedPresets {
		oldDeleted[k] = struct{}{}
	}
	deletedPresetsMu.RUnlock()

	oldBuiltIns := make(map[string]cli.Preset)
	for k, v := range cli.Presets {
		oldBuiltIns[k] = v
	}

	t.Cleanup(func() {
		userPresetsMu.Lock()
		userPresets = oldUserPresets
		userPresetsMu.Unlock()

		deletedPresetsMu.Lock()
		deletedPresets = oldDeleted
		deletedPresetsMu.Unlock()

		cli.Presets = oldBuiltIns
	})

	userPresetsMu.Lock()
	userPresets = make(map[string]cli.Preset)
	userPresetsMu.Unlock()

	deletedPresetsMu.Lock()
	deletedPresets = make(map[string]struct{})
	deletedPresetsMu.Unlock()

	cli.Presets = make(map[string]cli.Preset)
	seedPresets()
}

func presetNamesFromBody(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	var parsed map[string]map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to decode presets body: %v", err)
	}
	names := make(map[string]bool, len(parsed))
	for name := range parsed {
		names[name] = true
	}
	return names
}

func TestPresetDeleteFactory_AllowedAndTombstoned(t *testing.T) {
	root := setTestRoot(t, "presets-delete-factory-")
	t.Setenv("ONDA_DATA_DIR", root)
	resetPresetsState(t)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)

	// Before: factory preset is present.
	reqBefore := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rrBefore := httptest.NewRecorder()
	s.mux.ServeHTTP(rrBefore, reqBefore)
	if rrBefore.Code != http.StatusOK {
		t.Fatalf("GET /api/presets before delete returned %d", rrBefore.Code)
	}
	before := presetNamesFromBody(t, rrBefore.Body.Bytes())
	if !before["Voces Total"] {
		t.Fatalf("expected 'Voces Total' before deletion, got %v", before)
	}

	// Delete factory preset — must succeed (was 403).
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/presets/Voces%20Total", nil)
	rrDel := httptest.NewRecorder()
	s.mux.ServeHTTP(rrDel, reqDel)
	if rrDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/presets/Voces%%20Total returned %d: %s", rrDel.Code, rrDel.Body.String())
	}

	// After: factory preset is gone from GET /api/presets.
	reqAfter := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rrAfter := httptest.NewRecorder()
	s.mux.ServeHTTP(rrAfter, reqAfter)
	if rrAfter.Code != http.StatusOK {
		t.Fatalf("GET /api/presets after delete returned %d", rrAfter.Code)
	}
	after := presetNamesFromBody(t, rrAfter.Body.Bytes())
	if after["Voces Total"] {
		t.Fatalf("expected 'Voces Total' to be removed, got %v", after)
	}

	// Tombstone file must exist and contain the deleted name.
	tombstonePath := filepath.Join(mustConfigDir(), "presets_deleted.json")
	data, err := os.ReadFile(tombstonePath)
	if err != nil {
		t.Fatalf("failed to read tombstone file: %v", err)
	}
	var tombstones []string
	if err := json.Unmarshal(data, &tombstones); err != nil {
		t.Fatalf("failed to decode tombstone file: %v", err)
	}
	found := false
	for _, name := range tombstones {
		if name == "Voces Total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tombstone file did not contain 'Voces Total': %v", tombstones)
	}
}

func TestPresetRestoreDefaults_BringsBackFactory(t *testing.T) {
	root := setTestRoot(t, "presets-restore-")
	t.Setenv("ONDA_DATA_DIR", root)
	resetPresetsState(t)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)
	s.mux.HandleFunc("POST /api/presets/restore-defaults", s.handleRestoreDefaultPresets)

	// Delete two factory presets.
	for _, name := range []string{"Voces Total", "Solo Instrumentos"} {
		req := httptest.NewRequest(http.MethodDelete, "/api/presets/"+url.PathEscape(name), nil)
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("DELETE %q returned %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	// Restore defaults.
	reqRestore := httptest.NewRequest(http.MethodPost, "/api/presets/restore-defaults", nil)
	rrRestore := httptest.NewRecorder()
	s.mux.ServeHTTP(rrRestore, reqRestore)
	if rrRestore.Code != http.StatusOK {
		t.Fatalf("POST /api/presets/restore-defaults returned %d: %s", rrRestore.Code, rrRestore.Body.String())
	}

	// Both presets are back.
	reqList := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rrList := httptest.NewRecorder()
	s.mux.ServeHTTP(rrList, reqList)
	if rrList.Code != http.StatusOK {
		t.Fatalf("GET /api/presets after restore returned %d", rrList.Code)
	}
	list := presetNamesFromBody(t, rrList.Body.Bytes())
	for _, name := range []string{"Voces Total", "Solo Instrumentos"} {
		if !list[name] {
			t.Fatalf("expected %q after restore, got %v", name, list)
		}
	}

	// Tombstone file must be empty (or absent).
	tombstonePath := filepath.Join(mustConfigDir(), "presets_deleted.json")
	if data, err := os.ReadFile(tombstonePath); err == nil {
		var tombstones []string
		if err := json.Unmarshal(data, &tombstones); err != nil {
			t.Fatalf("failed to decode tombstone file: %v", err)
		}
		if len(tombstones) != 0 {
			t.Fatalf("expected empty tombstone file after restore, got %v", tombstones)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error reading tombstone file: %v", err)
	}
}

func TestPresetDeleteUser_StillWorks(t *testing.T) {
	root := setTestRoot(t, "presets-delete-user-")
	t.Setenv("ONDA_DATA_DIR", root)
	resetPresetsState(t)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("POST /api/presets", s.handleSavePreset)
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)

	// Save a user preset.
	body := `{"name":"Mi Preset","steps":[{"id":"vocal","model":"BS_Roformer_Viperx","type":"vocal","enabled":true,"stems":{"vocals":{"action":"save","target":"result"}}}],"locked":false}`
	reqSave := httptest.NewRequest(http.MethodPost, "/api/presets", strings.NewReader(body))
	rrSave := httptest.NewRecorder()
	s.mux.ServeHTTP(rrSave, reqSave)
	if rrSave.Code != http.StatusOK {
		t.Fatalf("POST /api/presets returned %d: %s", rrSave.Code, rrSave.Body.String())
	}

	// Confirm it appears.
	reqList := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rrList := httptest.NewRecorder()
	s.mux.ServeHTTP(rrList, reqList)
	if rrList.Code != http.StatusOK {
		t.Fatalf("GET /api/presets returned %d", rrList.Code)
	}
	list := presetNamesFromBody(t, rrList.Body.Bytes())
	if !list["Mi Preset"] {
		t.Fatalf("expected 'Mi Preset' before deletion, got %v", list)
	}

	// Delete it.
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/presets/Mi%20Preset", nil)
	rrDel := httptest.NewRecorder()
	s.mux.ServeHTTP(rrDel, reqDel)
	if rrDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/presets/Mi%%20Preset returned %d: %s", rrDel.Code, rrDel.Body.String())
	}

	// Confirm it is gone.
	reqAfter := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rrAfter := httptest.NewRecorder()
	s.mux.ServeHTTP(rrAfter, reqAfter)
	if rrAfter.Code != http.StatusOK {
		t.Fatalf("GET /api/presets after delete returned %d", rrAfter.Code)
	}
	after := presetNamesFromBody(t, rrAfter.Body.Bytes())
	if after["Mi Preset"] {
		t.Fatalf("expected 'Mi Preset' to be removed, got %v", after)
	}

	// User presets file must not contain it.
	userPath := userPresetsFile()
	data, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("failed to read user presets file: %v", err)
	}
	var userPresetsFileContents map[string]interface{}
	if err := json.Unmarshal(data, &userPresetsFileContents); err != nil {
		t.Fatalf("failed to decode user presets file: %v", err)
	}
	if _, ok := userPresetsFileContents["Mi Preset"]; ok {
		t.Fatalf("user presets file still contained 'Mi Preset': %s", string(data))
	}
}
