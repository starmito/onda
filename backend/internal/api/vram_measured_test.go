package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mapKeys(m map[string]MeasuredVRAMRecord) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestVramMeasuredKey_IsStableAndSensitiveToModelStepDevice(t *testing.T) {
	base := VRAMConfig{
		SegmentSize:   512,
		ChunkSize:     35,
		BatchSize:     1,
		DemucsSegment: 7,
		NumOverlap:    4,
		Shifts:        10,
		Jobs:          8,
		Duration:      30,
	}
	captured := vramFlagsCaptured(base, "demucs")

	key1 := vramMeasuredKey("htdemucs_ft", "demucs", "cuda", base, captured)
	key2 := vramMeasuredKey("htdemucs_ft", "demucs", "cuda", base, captured)
	if key1 != key2 {
		t.Fatalf("expected identical keys for identical config, got %q vs %q", key1, key2)
	}

	// Different model -> different key.
	if got := vramMeasuredKey("other", "demucs", "cuda", base, captured); got == key1 {
		t.Errorf("different model should produce different key, got %q", got)
	}

	// Different step type -> different key.
	if got := vramMeasuredKey("htdemucs_ft", "vocal", "cuda", base, captured); got == key1 {
		t.Errorf("different step type should produce different key, got %q", got)
	}

	// Different device -> different key.
	if got := vramMeasuredKey("htdemucs_ft", "demucs", "cpu", base, captured); got == key1 {
		t.Errorf("different device should produce different key, got %q", got)
	}

	// Different captured flag value -> different key.
	changed := base
	changed.DemucsSegment = 3
	if got := vramMeasuredKey("htdemucs_ft", "demucs", "cuda", changed, captured); got == key1 {
		t.Errorf("different captured flag should produce different key, got %q", got)
	}

	// Wildcard key (no captured flags) is just model|step|device.
	wildcard := vramMeasuredKey("htdemucs_ft", "demucs", "cuda", base, nil)
	if want := "htdemucs_ft|demucs|cuda"; wildcard != want {
		t.Errorf("wildcard key = %q, want %q", wildcard, want)
	}
}

func TestVramMeasuredStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, vramMeasuredFileName)
	store := newVRAMMeasuredStore(path)

	cfg := VRAMConfig{
		SegmentSize:   3105,
		ChunkSize:     0,
		BatchSize:     1,
		DemucsSegment: 0,
		NumOverlap:    4,
		Shifts:        1,
		Jobs:          1,
		Duration:      30,
	}
	captured := vramFlagsCaptured(cfg, "roformer")
	key := vramMeasuredKey("BS_Roformer_Viperx", "roformer", "cuda", cfg, captured)

	store.record(key, 2048, 5)
	if err := store.save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file exists and can be read back.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read persisted file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("persisted file is empty")
	}

	fresh := newVRAMMeasuredStore(path)
	if err := fresh.load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	r, ok := fresh.get(key)
	if !ok {
		t.Fatalf("record not found after round-trip")
	}
	if r.PeakMBMax != 2048 {
		t.Errorf("PeakMBMax = %d, want 2048", r.PeakMBMax)
	}
	if r.PeakMBLast != 2048 {
		t.Errorf("PeakMBLast = %d, want 2048", r.PeakMBLast)
	}
	if r.N != 5 {
		t.Errorf("N = %d, want 5", r.N)
	}
	if r.LastTS.IsZero() {
		t.Error("LastTS is zero")
	}

	// Recording a higher peak updates the max; a lower peak updates only last.
	fresh.record(key, 3000, 4)
	fresh.record(key, 2500, 6)
	r, _ = fresh.get(key)
	if r.PeakMBMax != 3000 {
		t.Errorf("PeakMBMax after updates = %d, want 3000", r.PeakMBMax)
	}
	if r.PeakMBLast != 2500 {
		t.Errorf("PeakMBLast after updates = %d, want 2500", r.PeakMBLast)
	}
	if r.N != 15 {
		t.Errorf("N after updates = %d, want 15", r.N)
	}
}

// TestRecordMeasuredVRAMPeak_Integration verifies that a running pipeline step
// is sampled, the peak is persisted, and the calculator later reports it as
// "measured". This uses a fake pipeline and a mocked GPU provider so it does
// not need a real GPU.
func TestRecordMeasuredVRAMPeak_Integration(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()
	root := setTestRoot(t, "vram-measured-integ-")
	mockResourceProviders(t)

	// Fake pipeline that sleeps long enough for several samples.
	fakePipeline := filepath.Join(root, "pipeline.sh")
	script := `#!/bin/bash
sleep 3
`
	if err := os.WriteFile(fakePipeline, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pipeline: %v", err)
	}

	// Override the singleton store to use a temporary config dir.
	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	// Mock GPU memory so the footprint is deterministic.
	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	values := []int{400, 400, 400, 1200, 1200, 1200}
	idx := 0
	gpuInfoProvider = func() GPUInfoResponse {
		v := values[idx]
		if idx < len(values)-1 {
			idx++
		}
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 16000 - v, VRAMUsedMB: v}
	}

	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs: map[string]*JobState{
			"song": {Song: "song", Status: "waiting"},
		},
	}
	go s.worker()
	s.jobQueue <- JobRequest{
		Song: "song",
		Args: []string{fakePipeline, "--vocal-model", "BS_Roformer_Viperx", "--device", "cuda", filepath.Join(root, "input", "song.wav"), "--output", filepath.Join(root, "output", "song")},
		Config: SeparateRequest{VocalModel: "BS_Roformer_Viperx"},
	}

	deadline := time.Now().Add(8 * time.Second)
	for {
		s.jobsMu.RLock()
		status := s.jobs["song"].Status
		s.jobsMu.RUnlock()
		if status == "done" || status == "error" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for worker to process job")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Give the sampler goroutine a moment to finish saving.
	time.Sleep(300 * time.Millisecond)

	fresh := newVRAMMeasuredStore(storePath)
	if err := fresh.load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	var foundKey string
	var r MeasuredVRAMRecord
	var ok bool
	prefix := "bs_roformer_viperx|vocal|cuda"
	for k, v := range fresh.data {
		if k == prefix || strings.HasPrefix(k, prefix+"|") {
			foundKey = k
			r = v
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("no measured record found for BS_Roformer_Viperx/vocal/cuda, store keys: %v", mapKeys(fresh.data))
	}
	want := 1200 - 400 // peak - baseline
	if r.PeakMBMax != want {
		t.Errorf("PeakMBMax for %q = %d, want %d", foundKey, r.PeakMBMax, want)
	}
	if r.N < 2 {
		t.Errorf("N for %q = %d, want at least 2 samples", foundKey, r.N)
	}
}

// TestHandleVRAMCalculator_MeasuredSource_RealStoreKey reproduces the bug
// reported in task 50: the UI queries the calculator with only the model name,
// but the persistent store contains a real measurement keyed with the old
// model|step|device|flag-hash|duration format. Before the fix the calculator
// falls back to the analytical estimate (2000 MB); after the fix it must
// return the measured peak (4137 MB).
func TestHandleVRAMCalculator_MeasuredSource_RealStoreKey(t *testing.T) {
	setupTestLogStore(t)
	root := setTestRoot(t, "vram-calc-real-key-")

	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	// Key copied from the real deployed store (flag hash + duration bucket).
	key := "bs_roformer_sw_6stem|vocal|cuda|fb6dc349e617a901|240"
	getVRAMMeasuredStore().record(key, 4137, 92)
	if err := getVRAMMeasuredStore().save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 12000, VRAMUsedMB: 4000}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	// Query exactly as the deployed UI does for this model.
	req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=BS_Roformer_SW_6stem", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VRAMCalculatorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}
	m := resp.Models[0]
	if m.Source != "measured" {
		t.Errorf("source = %q, want measured", m.Source)
	}
	if m.VRAMMB != 4137 {
		t.Errorf("vram_mb = %d, want 4137", m.VRAMMB)
	}
	if m.MeasuredMB != 4137 {
		t.Errorf("measured_mb = %d, want 4137", m.MeasuredMB)
	}
	if m.MeasuredN != 92 {
		t.Errorf("measured_n = %d, want 92", m.MeasuredN)
	}
}

// TestHandleVRAMCalculator_MeasuredSource_CascadeLevel1 reproduces FALLO B from
// task 53: the UI queries with all model flags, but the store record was saved
// with a key that only contains batch/chunk/segment while its captured set also
// includes overlap and shifts. The lookup must still return the measured value
// because the queried flags that the record does capture match exactly.
func TestHandleVRAMCalculator_MeasuredSource_CascadeLevel1(t *testing.T) {
	setupTestLogStore(t)
	root := setTestRoot(t, "vram-calc-cascade-l1-")

	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	// Store key with only batch/chunk/segment, but captured includes overlap/shifts
	// as observed in the deployed store (task 53).
	key := "bs_roformer_sw_6stem|vocal|cuda|batch_size=1|chunk_size=0|segment_size=1101"
	rec := MeasuredVRAMRecord{
		PeakMBMax:  2441,
		PeakMBLast: 2441,
		N:          128,
		LastTS:     time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
		Flags: VRAMFlags{
			SegmentSize: 1101,
			ChunkSize:   0,
			BatchSize:   1,
			NumOverlap:  2,
			Shifts:      1,
		},
		Captured: map[string]bool{
			vramFlagSegmentSize: true,
			vramFlagChunkSize:   true,
			vramFlagBatchSize:   true,
			vramFlagNumOverlap:  true,
			vramFlagShifts:      true,
		},
	}
	getVRAMMeasuredStore().recordMeasured(key, rec)
	if err := getVRAMMeasuredStore().save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 12000, VRAMUsedMB: 4000}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	// Query exactly as the model settings UI does for BS_Roformer_SW_6stem.
	req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=BS_Roformer_SW_6stem&segment_size=1101&batch_size=1&chunk_size=0&num_overlap=4", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VRAMCalculatorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}
	m := resp.Models[0]
	if m.Source != "measured" {
		t.Errorf("source = %q, want measured", m.Source)
	}
	if m.VRAMMB != 2441 {
		t.Errorf("vram_mb = %d, want 2441", m.VRAMMB)
	}
	if m.MeasuredMB != 2441 {
		t.Errorf("measured_mb = %d, want 2441", m.MeasuredMB)
	}
	if m.MeasuredN != 128 {
		t.Errorf("measured_n = %d, want 128", m.MeasuredN)
	}
	if m.MeasuredFlags == "" {
		t.Error("measured_flags should be non-empty for a flag-level match")
	}
}

// TestHandleVRAMCalculator_MeasuredSource_CascadeLevel2 reproduces FALLO A from
// task 53: there is a model-level wildcard measurement (4137 MB) and a more
// specific measurement that does not match the queried flags. The calculator
// must fall back to the model-level measurement and report it as measured.
func TestHandleVRAMCalculator_MeasuredSource_CascadeLevel2(t *testing.T) {
	setupTestLogStore(t)
	root := setTestRoot(t, "vram-calc-cascade-l2-")

	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	// Wildcard measurement for the model.
	getVRAMMeasuredStore().record("bs_roformer_sw_6stem|vocal|cuda", 4137, 92)
	if err := getVRAMMeasuredStore().save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 12000, VRAMUsedMB: 4000}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=BS_Roformer_SW_6stem&segment_size=1101&batch_size=1&chunk_size=0&num_overlap=4", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VRAMCalculatorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}
	m := resp.Models[0]
	if m.Source != "measured" {
		t.Errorf("source = %q, want measured", m.Source)
	}
	if m.VRAMMB != 4137 {
		t.Errorf("vram_mb = %d, want 4137", m.VRAMMB)
	}
	if m.MeasuredMB != 4137 {
		t.Errorf("measured_mb = %d, want 4137", m.MeasuredMB)
	}
	if m.MeasuredN != 92 {
		t.Errorf("measured_n = %d, want 92", m.MeasuredN)
	}
}

// TestRecordMeasuredVRAMPeak_DiscardsZeroPeak reproduces FALLO C from task 53:
// a failed job that reports a footprint of 0 MB must not overwrite or shadow a
// previously valid measurement.
func TestRecordMeasuredVRAMPeak_DiscardsZeroPeak(t *testing.T) {
	setupTestLogStore(t)
	root := setTestRoot(t, "vram-zero-poison-")

	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	// Pre-existing valid measurement.
	getVRAMMeasuredStore().record("bs_roformer_sw_6stem|vocal|cuda", 4137, 92)
	if err := getVRAMMeasuredStore().save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Mock a job that exits immediately: GPU usage never changes.
	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 15600, VRAMUsedMB: 400}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Simulate a process that fails before doing any real work.
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	cfg := VRAMConfig{
		SegmentSize: 1101,
		ChunkSize:   0,
		BatchSize:   1,
		NumOverlap:  4,
	}
	recordMeasuredVRAMPeak(ctx, "BS_Roformer_SW_6stem", "cuda", cfg)

	// Verify the wildcard measurement is untouched.
	fresh := newVRAMMeasuredStore(storePath)
	if err := fresh.load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	r, ok := fresh.get("bs_roformer_sw_6stem|vocal|cuda")
	if !ok {
		t.Fatal("wildcard measurement disappeared")
	}
	if r.PeakMBMax != 4137 {
		t.Errorf("PeakMBMax = %d, want 4137 (zero peak must not overwrite)", r.PeakMBMax)
	}

	// No zero-footprint record should have been created for the specific flags.
	for k, v := range fresh.data {
		if v.PeakMBMax == 0 {
			t.Errorf("zero-footprint record created for %q", k)
		}
	}
}

// TestHandleVRAMCalculator_MeasuredSource verifies that the calculator reports
// a measured peak with source="measured" and metadata when the store contains
// a matching entry.
func TestHandleVRAMCalculator_MeasuredSource(t *testing.T) {
	setupTestLogStore(t)
	root := setTestRoot(t, "vram-calc-measured-")
	_ = root

	storeDir := filepath.Join(root, "config")
	storePath := filepath.Join(storeDir, vramMeasuredFileName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	resetVRAMMeasuredStoreForTests()
	vramMeasuredStoreInstance = newVRAMMeasuredStore(storePath)

	cfg := VRAMConfig{
		SegmentSize: 512,
		ChunkSize:   0,
		BatchSize:   1,
		NumOverlap:  4,
		Duration:    30,
	}
	captured := vramFlagsCaptured(cfg, "vocal")
	key := vramMeasuredKey("BS_Roformer_Viperx", "vocal", "cuda", cfg, captured)
	getVRAMMeasuredStore().record(key, 2800, 12)
	if err := getVRAMMeasuredStore().save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16000, VRAMFreeMB: 12000, VRAMUsedMB: 4000}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=BS_Roformer_Viperx&segment_size=512&batch_size=1&num_overlap=4&duration=30", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VRAMCalculatorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}
	m := resp.Models[0]
	if m.Source != "measured" {
		t.Errorf("source = %q, want measured", m.Source)
	}
	if m.MeasuredMB != 2800 {
		t.Errorf("measured_mb = %d, want 2800", m.MeasuredMB)
	}
	if m.MeasuredN != 12 {
		t.Errorf("measured_n = %d, want 12", m.MeasuredN)
	}
	if m.EstimatedMB == 0 {
		t.Error("estimated_mb should be non-zero fallback")
	}
	if m.VRAMMB != 2800 {
		t.Errorf("vram_mb = %d, want 2800", m.VRAMMB)
	}
}
