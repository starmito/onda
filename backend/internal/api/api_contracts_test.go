package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertFieldExistsAndIs checks that m[key] exists and has one of the wanted types.
func assertFieldExistsAndIs(t *testing.T, m map[string]interface{}, key string, wantTypes ...string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("missing field %q", key)
		return
	}
	got := getTypeName(v)
	for _, want := range wantTypes {
		if got == want {
			return
		}
	}
	t.Errorf("field %q has type %q, want one of %v", key, got, wantTypes)
}

func getTypeName(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	case int:
		return "number"
	case int64:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// newContractsTestServer registers the API contract endpoints on a fresh mux.
func newContractsTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux(), jobs: make(map[string]*JobState)}
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/models/list", s.handleModelsList)
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.mux.HandleFunc("GET /api/results", s.handleResults)
	s.mux.HandleFunc("GET /api/storage/usage", s.handleStorageUsage)
	return s
}

func TestContract_Health(t *testing.T) {
	setTestRoot(t, "contract-health-")

	// Mock GPU checks so the test does not depend on the host GPU.
	origCheckGPU := checkGPU
	checkGPU = func() (bool, string, error) { return true, "CUDA available", nil }
	t.Cleanup(func() { checkGPU = origCheckGPU })

	origGPUInfo := gpuInfoProvider
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, Name: "Test GPU", VRAMTotalMB: 16000, VRAMUsedMB: 100, VRAMFreeMB: 15900}
	}
	t.Cleanup(func() { gpuInfoProvider = origGPUInfo })

	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode health: %v", err)
	}

	assertFieldExistsAndIs(t, resp, "status", "string")
	assertFieldExistsAndIs(t, resp, "version", "string")

	for _, key := range []string{"backend", "frontend", "pipeline", "gpu", "disk", "version_mismatch"} {
		assertFieldExistsAndIs(t, resp, key, "object")
	}

	backend := resp["backend"].(map[string]interface{})
	assertFieldExistsAndIs(t, backend, "ok", "bool")
	assertFieldExistsAndIs(t, backend, "version", "string")

	gpu := resp["gpu"].(map[string]interface{})
	assertFieldExistsAndIs(t, gpu, "ok", "bool")
	assertFieldExistsAndIs(t, gpu, "usable_by_torch", "bool")
	assertFieldExistsAndIs(t, gpu, "type", "string")
	if gpu["ok"].(bool) {
		assertFieldExistsAndIs(t, gpu, "detail", "string")
		assertFieldExistsAndIs(t, gpu, "total_mb", "number")
		assertFieldExistsAndIs(t, gpu, "used_mb", "number")
		assertFieldExistsAndIs(t, gpu, "free_mb", "number")
	}

	disk := resp["disk"].(map[string]interface{})
	assertFieldExistsAndIs(t, disk, "ok", "bool")
	assertFieldExistsAndIs(t, disk, "detail", "string")

	mismatch := resp["version_mismatch"].(map[string]interface{})
	assertFieldExistsAndIs(t, mismatch, "ok", "bool")
}

func TestContract_ModelsList(t *testing.T) {
	root := setTestRoot(t, "contract-models-list-")

	// Create a fake model with a manifest so the response is fully populated.
	modelDir := filepath.Join(root, "models", "VR_Models", "TestModel")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "TestModel.ckpt"), []byte("weights"), 0o644); err != nil {
		t.Fatalf("failed to create model file: %v", err)
	}
	manifest := `{"name":"Test Model","type":"bs_roformer","stems":{"stems":["vocals","instrumental"],"num_stems":2}}`
	if err := os.WriteFile(filepath.Join(modelDir, "model.manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/models/list", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode models list: %v", err)
	}
	assertFieldExistsAndIs(t, resp, "models", "array")
	assertFieldExistsAndIs(t, resp, "categories", "array")

	models := resp["models"].([]interface{})
	if len(models) == 0 {
		t.Fatal("expected at least one model")
	}
	first := models[0].(map[string]interface{})
	for _, key := range []string{"name", "installed_name", "display_name", "category", "type", "path", "size_mb", "vram_estimate_mb", "stems", "num_stems", "manifest_missing", "inferred"} {
		assertFieldExistsAndIs(t, first, key, "string", "number", "array", "bool")
	}
}

func TestContract_ModelConfig(t *testing.T) {
	root := setTestRoot(t, "contract-model-config-")

	modelDir := filepath.Join(root, "models", "Demucs_Models", "htdemucs_ft")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "htdemucs_ft.th"), []byte("weights"), 0o644); err != nil {
		t.Fatalf("failed to create model file: %v", err)
	}

	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/models/htdemucs_ft/config", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode model config: %v", err)
	}
	assertFieldExistsAndIs(t, resp, "model", "string")
	assertFieldExistsAndIs(t, resp, "flags", "array")
	assertFieldExistsAndIs(t, resp, "stems", "array")
	assertFieldExistsAndIs(t, resp, "num_stems", "number")

	flags := resp["flags"].([]interface{})
	if len(flags) == 0 {
		t.Fatal("expected at least one flag")
	}
	first := flags[0].(map[string]interface{})
	for _, key := range []string{"name", "value", "default", "editable"} {
		assertFieldExistsAndIs(t, first, key, "string", "number", "bool")
	}
}

func TestContract_Presets(t *testing.T) {
	_ = setTestRoot(t, "contract-presets-")
	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode presets: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected at least one preset")
	}
	for name, preset := range resp {
		assertFieldExistsAndIs(t, preset, "name", "string")
		assertFieldExistsAndIs(t, preset, "steps", "array")
		assertFieldExistsAndIs(t, preset, "pitch", "number")
		assertFieldExistsAndIs(t, preset, "description", "string")
		assertFieldExistsAndIs(t, preset, "locked", "bool")
		if preset["name"].(string) != name {
			t.Errorf("preset key %q maps to name %q", name, preset["name"])
		}
	}

	// Verify the structure of a step object.
	for _, preset := range resp {
		steps := preset["steps"].([]interface{})
		if len(steps) == 0 {
			continue
		}
		step := steps[0].(map[string]interface{})
		for _, key := range []string{"id", "model", "type", "enabled", "stems"} {
			assertFieldExistsAndIs(t, step, key, "string", "bool", "object")
		}
		break
	}
}

func TestContract_QueueStatus(t *testing.T) {
	_ = setTestRoot(t, "contract-queue-")
	s := newContractsTestServer(t)

	// Seed one job so the response is populated.
	zero := 0
	s.jobs["testsong"] = &JobState{
		Song:       "testsong",
		Status:     "waiting",
		Progress:   0,
		ETA:        &zero,
		Elapsed:    0,
		Index:      0,
		CurrentStep: 1,
		TotalSteps:  2,
		StepName:   "Vocal",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/queue/status", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode queue status: %v", err)
	}
	assertFieldExistsAndIs(t, resp, "jobs", "array")
	assertFieldExistsAndIs(t, resp, "overall_progress", "number")

	jobs := resp["jobs"].([]interface{})
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	job := jobs[0].(map[string]interface{})
	for _, key := range []string{"song", "status", "progress", "current_step", "total_steps", "step_name"} {
		assertFieldExistsAndIs(t, job, key, "string", "number")
	}
}

func TestContract_Results(t *testing.T) {
	root := setTestRoot(t, "contract-results-")

	// Create result files so the response is populated.
	songDir := filepath.Join(root, "output", "testsong")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	for _, name := range []string{"vocals.wav", "instrumental.wav"} {
		if err := os.WriteFile(filepath.Join(songDir, name), []byte("stem"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/results", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode results: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected at least one results group")
	}
	group := resp[0]
	assertFieldExistsAndIs(t, group, "song", "string")
	assertFieldExistsAndIs(t, group, "files", "array")

	files := group["files"].([]interface{})
	if len(files) == 0 {
		t.Fatal("expected at least one file")
	}
	file := files[0].(map[string]interface{})
	assertFieldExistsAndIs(t, file, "name", "string")
	assertFieldExistsAndIs(t, file, "path", "string")
	if !strings.HasPrefix(file["path"].(string), "/api/files/") {
		t.Errorf("file path %q does not start with /api/files/", file["path"])
	}
}

func TestContract_StorageUsage(t *testing.T) {
	root := setTestRoot(t, "contract-storage-")
	for _, dir := range []string{"input", "input_rubberband", "daw-data", "output", "models", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "input", "a.wav"), []byte("a"), 0o644); err != nil {
		t.Fatalf("failed to create input file: %v", err)
	}

	s := newContractsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/storage/usage", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode storage usage: %v", err)
	}
	assertFieldExistsAndIs(t, resp, "folders", "object")
	assertFieldExistsAndIs(t, resp, "free_bytes", "number")
	assertFieldExistsAndIs(t, resp, "models", "object")
	assertFieldExistsAndIs(t, resp, "cache", "object")

	folders := resp["folders"].(map[string]interface{})
	for _, name := range []string{"input", "input_rubberband", "daw-data", "output", "models", "logs"} {
		assertFieldExistsAndIs(t, folders, name, "object")
		usage := folders[name].(map[string]interface{})
		assertFieldExistsAndIs(t, usage, "files", "number")
		assertFieldExistsAndIs(t, usage, "bytes", "number")
	}

	models := resp["models"].(map[string]interface{})
	assertFieldExistsAndIs(t, models, "entries", "number")
	assertFieldExistsAndIs(t, models, "bytes", "number")

	cache := resp["cache"].(map[string]interface{})
	assertFieldExistsAndIs(t, cache, "files", "number")
	assertFieldExistsAndIs(t, cache, "bytes", "number")
}
