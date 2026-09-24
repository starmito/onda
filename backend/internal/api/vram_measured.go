package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// vramMeasuredFileName is the name of the persistent measured-VRAM store. It
// lives in the configuration directory so it survives container restarts.
const vramMeasuredFileName = "vram_measured.json"

// vramSampleInterval is the interval between GPU memory samples while a
// pipeline step is running.
const vramSampleInterval = 500 * time.Millisecond

// vramBaselineSamples is the number of initial samples used to establish the
// baseline VRAM level before the model is fully loaded.
const vramBaselineSamples = 3

// vramDurationBucketSec is the granularity used to bucket audio duration when
// matching whole-song measurements. Chunked models are insensitive to total
// duration, but whole-song models scale with it.
const vramDurationBucketSec = 30

// MeasuredVRAMRecord stores one measured VRAM footprint in the persistent
// store. Both the historical maximum and the last observed peak are kept so
// the calculator can report the conservative maximum while still tracking
// recent values.
type MeasuredVRAMRecord struct {
	PeakMBMax int       `json:"peak_mb_max"`
	PeakMBLast int      `json:"peak_mb_last"`
	N         int       `json:"n"`
	LastTS    time.Time `json:"last_ts"`
}

// vramMeasuredStore persists measured VRAM peaks keyed by model, step type,
// device, effective flags and duration bucket.
type vramMeasuredStore struct {
	mu   sync.RWMutex
	path string
	data map[string]MeasuredVRAMRecord
}

// newVRAMMeasuredStore creates a store backed by path.
func newVRAMMeasuredStore(path string) *vramMeasuredStore {
	return &vramMeasuredStore{path: path, data: make(map[string]MeasuredVRAMRecord)}
}

// vramMeasuredStoreInstance is the singleton store. It is initialized lazily
// because configDir() depends on environment and tests may need to override it
// by resetting this variable.
var vramMeasuredStoreInstance *vramMeasuredStore
var vramMeasuredStoreOnce sync.Once

// getVRAMMeasuredStore returns the singleton store, creating it on first call.
// Tests may pre-assign vramMeasuredStoreInstance; in that case the lazy
// initialization is skipped so the test path is preserved.
func getVRAMMeasuredStore() *vramMeasuredStore {
	vramMeasuredStoreOnce.Do(func() {
		if vramMeasuredStoreInstance == nil {
			vramMeasuredStoreInstance = newVRAMMeasuredStore(filepath.Join(mustConfigDir(), vramMeasuredFileName))
			_ = vramMeasuredStoreInstance.load()
		}
	})
	return vramMeasuredStoreInstance
}

// resetVRAMMeasuredStoreForTests clears the singleton so the next call to
// getVRAMMeasuredStore() creates a fresh store. Tests must call this before
// overriding vramMeasuredStoreInstance directly.
func resetVRAMMeasuredStoreForTests() {
	vramMeasuredStoreOnce = sync.Once{}
	vramMeasuredStoreInstance = nil
}

func (s *vramMeasuredStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var loaded map[string]MeasuredVRAMRecord
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	s.data = loaded
	return nil
}

func (s *vramMeasuredStore) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// get returns the record for key, if any.
func (s *vramMeasuredStore) get(key string) (MeasuredVRAMRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[key]
	return r, ok
}

// record adds a new peak measurement for key, updating max/last counters,
// sample count and timestamp. nSamples is the number of GPU readings that
// contributed to the peak. It is safe for concurrent use.
func (s *vramMeasuredStore) record(key string, peakMB, nSamples int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.data[key]
	if peakMB > r.PeakMBMax {
		r.PeakMBMax = peakMB
	}
	r.PeakMBLast = peakMB
	r.N += nSamples
	r.LastTS = time.Now().UTC()
	s.data[key] = r
}

// vramMeasuredKey builds a stable lookup key for a model/step/device/flags/
// duration combination.
//
// The flag hash is deterministic: known flag names are sorted and concatenated
// with their integer values. The duration is rounded to a coarse bucket so
// small differences in clip length do not fragment the cache.
func vramMeasuredKey(modelName, stepType, device string, cfg VRAMConfig) string {
	flagHash := vramFlagsHash(cfg)
	return fmt.Sprintf("%s|%s|%s|%s|%d",
		strings.ToLower(modelName),
		strings.ToLower(stepType),
		strings.ToLower(device),
		flagHash,
		vramDurationBucket(cfg.Duration),
	)
}

// vramFlagsHash returns a short, deterministic hash of the flags that affect
// VRAM usage.
func vramFlagsHash(cfg VRAMConfig) string {
	pairs := map[string]int{
		"dim_t":       cfg.SegmentSize,
		"num_overlap": cfg.NumOverlap,
		"batch_size":  cfg.BatchSize,
		"chunk_size":  cfg.ChunkSize,
		"shifts":      cfg.Shifts,
		"segment":     cfg.DemucsSegment,
		"jobs":        cfg.Jobs,
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%d;", k, pairs[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// vramDurationBucket rounds seconds to the nearest coarse bucket. Zero means
// "unknown/irrelevant".
func vramDurationBucket(sec int) int {
	if sec <= 0 {
		return 0
	}
	return ((sec + vramDurationBucketSec - 1) / vramDurationBucketSec) * vramDurationBucketSec
}

// vramStepTypeForModel returns the canonical step type used for VRAM
// measurements and lookups. It matches classifyModelType so the calculator and
// the sampler agree on the same key.
func vramStepTypeForModel(modelName string) string {
	return classifyModelType(modelName)
}

// findMeasuredVRAMPeakInStore returns the conservative maximum measured peak in
// MiB for the exact configuration, or 0 when no measurement exists.
func findMeasuredVRAMPeakInStore(modelName, device string, cfg VRAMConfig) int {
	key := vramMeasuredKey(modelName, vramStepTypeForModel(modelName), device, cfg)
	if r, ok := getVRAMMeasuredStore().get(key); ok && r.N > 0 {
		return r.PeakMBMax
	}
	return 0
}

// vramSampler samples GPU memory while a pipeline step runs.
type vramSampler struct {
	interval        time.Duration
	baselineSamples int
	provider        func() GPUInfoResponse
}

// newVRAMSampler creates a sampler that uses the shared GPU info provider.
// It uses gpuInfoProvider (rather than getGPUInfo directly) so tests can
// substitute a mock implementation.
func newVRAMSampler() *vramSampler {
	return &vramSampler{
		interval:        vramSampleInterval,
		baselineSamples: vramBaselineSamples,
		provider:        gpuInfoProvider,
	}
}

// sampleResult holds the outcome of a sampling session.
type sampleResult struct {
	PeakMB    int
	BaselineMB int
	N         int
	OK        bool
}

// sample reads GPU memory periodically until ctx is cancelled. It returns the
// peak observed VRAM, the baseline (minimum of the first baselineSamples) and
// the number of successful samples.
func (s *vramSampler) sample(ctx context.Context) sampleResult {
	var samples []int
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	readOnce := func() {
		info := s.provider()
		if info.OK {
			samples = append(samples, info.VRAMUsedMB)
		}
	}

	readOnce()
	for {
		select {
		case <-ctx.Done():
			if len(samples) == 0 {
				return sampleResult{OK: false}
			}
			end := s.baselineSamples
			if end > len(samples) {
				end = len(samples)
			}
			baseline := samples[0]
			for i := 1; i < end; i++ {
				if samples[i] < baseline {
					baseline = samples[i]
				}
			}
			peak := samples[0]
			for _, v := range samples {
				if v > peak {
					peak = v
				}
			}
			if peak < baseline {
				peak = baseline
			}
			return sampleResult{PeakMB: peak, BaselineMB: baseline, N: len(samples), OK: true}
		case <-ticker.C:
			readOnce()
		}
	}
}

// recordMeasuredVRAMPeak samples VRAM during the lifetime of ctx and persists
// the measured footprint (peak - baseline) for the given configuration. It is
// intended to be called in a goroutine that starts right after the pipeline
// process starts and is cancelled once the step finishes.
func recordMeasuredVRAMPeak(ctx context.Context, modelName, device string, cfg VRAMConfig) {
	sampler := newVRAMSampler()
	res := sampler.sample(ctx)
	if !res.OK || res.N == 0 {
		return
	}
	footprint := res.PeakMB - res.BaselineMB
	if footprint < 0 {
		footprint = 0
	}
	stepType := vramStepTypeForModel(modelName)
	key := vramMeasuredKey(modelName, stepType, device, cfg)
	store := getVRAMMeasuredStore()
	store.record(key, footprint, res.N)
	if err := store.save(); err != nil {
		Log("pipeline", "warn", fmt.Sprintf("Failed to save measured VRAM for %s: %v", modelName, err))
	}
	Log("pipeline", "info", fmt.Sprintf("VRAM measured: model=%s step=%s device=%s baseline=%d MB peak=%d MB footprint=%d MB n=%d",
		modelName, stepType, device, res.BaselineMB, res.PeakMB, footprint, res.N))
}
