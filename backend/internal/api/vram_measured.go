package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// vramDurationBucketSec is kept only for backward compatibility with legacy
// store keys. Duration is no longer part of the lookup key because chunked
// inference does not grow VRAM with song length.
const vramDurationBucketSec = 30

// vramFlagNames enumerate the inference parameters that may be captured in a
// measured VRAM record. Uncaptured values act as wildcards during lookup.
const (
	vramFlagSegmentSize   = "segment_size"
	vramFlagChunkSize     = "chunk_size"
	vramFlagBatchSize     = "batch_size"
	vramFlagDemucsSegment = "demucs_segment"
	vramFlagNumOverlap    = "num_overlap"
	vramFlagShifts        = "shifts"
	vramFlagJobs          = "jobs"
)

// VRAMFlags stores the values of the inference parameters that were in effect
// when a measurement was taken. A field set to measuredParamUnused means the
// parameter was not captured and acts as a wildcard.
type VRAMFlags struct {
	SegmentSize   int `json:"segment_size,omitempty"`
	ChunkSize     int `json:"chunk_size,omitempty"`
	BatchSize     int `json:"batch_size,omitempty"`
	DemucsSegment int `json:"demucs_segment,omitempty"`
	NumOverlap    int `json:"num_overlap,omitempty"`
	Shifts        int `json:"shifts,omitempty"`
	Jobs          int `json:"jobs,omitempty"`
}

// MeasuredVRAMRecord stores one measured VRAM footprint in the persistent
// store. Both the historical maximum and the last observed peak are kept so
// the calculator can report the conservative maximum while still tracking
// recent values.
type MeasuredVRAMRecord struct {
	PeakMBMax  int            `json:"peak_mb_max"`
	PeakMBLast int            `json:"peak_mb_last"`
	N          int            `json:"n"`
	LastTS     time.Time      `json:"last_ts"`
	Duration   int            `json:"duration,omitempty"` // informational metadata only
	Flags      VRAMFlags      `json:"flags,omitempty"`
	Captured   map[string]bool `json:"captured,omitempty"` // empty = wildcard
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
			seedMeasuredVRAMPeaks(vramMeasuredStoreInstance)
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
	// The store file has always been a JSON object; legacy keys carried a flag
	// hash and a duration bucket. Read it as the current type (the new fields
	// are optional) and migrate legacy keys to the wildcard format.
	var loaded map[string]MeasuredVRAMRecord
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	s.data = make(map[string]MeasuredVRAMRecord)
	for oldKey, rec := range loaded {
		newKey := migrateVRAMMeasuredKey(oldKey)
		// Old records folded all flags into the hash, so we cannot know which
		// flags were captured. Treat them as wildcards so they still match
		// future lookups for the same model/step/device.
		rec.Captured = nil
		rec.Flags = VRAMFlags{}
		s.data[newKey] = rec
	}
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

// findByPrefix returns all records whose key is exactly prefix or starts with
// prefix followed by "|" (i.e. records for the same model/step/device with
// additional captured flags).
func (s *vramMeasuredStore) findByPrefix(prefix string) []MeasuredVRAMRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var res []MeasuredVRAMRecord
	for k, v := range s.data {
		if k == prefix || strings.HasPrefix(k, prefix+"|") {
			res = append(res, v)
		}
	}
	return res
}

// hasKey reports whether the store has any record under the exact key.
func (s *vramMeasuredStore) hasKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok
}

// record adds a new wildcard peak measurement for key. It is a convenience
// wrapper for callers that do not track per-flag metadata (mainly tests).
func (s *vramMeasuredStore) record(key string, peakMB, nSamples int) {
	s.recordMeasured(key, MeasuredVRAMRecord{
		PeakMBMax:  peakMB,
		PeakMBLast: peakMB,
		N:          nSamples,
		LastTS:     time.Now().UTC(),
	})
}

// recordMeasured adds or updates a peak measurement for key, preserving the
// captured flags when the record already exists.
func (s *vramMeasuredStore) recordMeasured(key string, rec MeasuredVRAMRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.data[key]
	if rec.PeakMBMax > r.PeakMBMax {
		r.PeakMBMax = rec.PeakMBMax
	}
	if rec.PeakMBLast > 0 {
		r.PeakMBLast = rec.PeakMBLast
	} else if r.PeakMBLast == 0 {
		r.PeakMBLast = rec.PeakMBMax
	}
	r.N += rec.N
	if !rec.LastTS.IsZero() {
		r.LastTS = rec.LastTS
	}
	if r.Captured == nil {
		r.Captured = rec.Captured
		r.Flags = rec.Flags
		r.Duration = rec.Duration
	}
	s.data[key] = r
}

// vramMeasuredKey builds a stable lookup key for a model/step/device
// combination. Captured flags are appended deterministically so multiple
// measurements for the same model can coexist at different specificity levels.
// When captured is empty the key is a wildcard (model|step|device only).
func vramMeasuredKey(modelName, stepType, device string, cfg VRAMConfig, captured map[string]bool) string {
	base := fmt.Sprintf("%s|%s|%s",
		strings.ToLower(modelName),
		strings.ToLower(stepType),
		strings.ToLower(device),
	)
	if len(captured) == 0 {
		return base
	}
	names := make([]string, 0, len(captured))
	for name := range captured {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(base)
	for _, name := range names {
		b.WriteString("|")
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(strconv.Itoa(vramFlagValue(cfg, name)))
	}
	return b.String()
}

// vramFlagValue returns the value of a named flag from a VRAMConfig.
func vramFlagValue(cfg VRAMConfig, name string) int {
	switch name {
	case vramFlagSegmentSize:
		return cfg.SegmentSize
	case vramFlagChunkSize:
		return cfg.ChunkSize
	case vramFlagBatchSize:
		return cfg.BatchSize
	case vramFlagDemucsSegment:
		return cfg.DemucsSegment
	case vramFlagNumOverlap:
		return cfg.NumOverlap
	case vramFlagShifts:
		return cfg.Shifts
	case vramFlagJobs:
		return cfg.Jobs
	}
	return 0
}

// vramFlagValueFromFlags returns the value of a named flag from a VRAMFlags.
func vramFlagValueFromFlags(flags VRAMFlags, name string) int {
	switch name {
	case vramFlagSegmentSize:
		return flags.SegmentSize
	case vramFlagChunkSize:
		return flags.ChunkSize
	case vramFlagBatchSize:
		return flags.BatchSize
	case vramFlagDemucsSegment:
		return flags.DemucsSegment
	case vramFlagNumOverlap:
		return flags.NumOverlap
	case vramFlagShifts:
		return flags.Shifts
	case vramFlagJobs:
		return flags.Jobs
	}
	return 0
}

// vramFlagsFromConfig converts a VRAMConfig to VRAMFlags.
func vramFlagsFromConfig(cfg VRAMConfig) VRAMFlags {
	return VRAMFlags{
		SegmentSize:   cfg.SegmentSize,
		ChunkSize:     cfg.ChunkSize,
		BatchSize:     cfg.BatchSize,
		DemucsSegment: cfg.DemucsSegment,
		NumOverlap:    cfg.NumOverlap,
		Shifts:        cfg.Shifts,
		Jobs:          cfg.Jobs,
	}
}

// vramFlagsCaptured decides which flags are relevant enough to be captured for
// a given step type. Uncaptured flags act as wildcards when matching.
func vramFlagsCaptured(cfg VRAMConfig, stepType string) map[string]bool {
	captured := make(map[string]bool)
	lowerStep := strings.ToLower(stepType)
	switch lowerStep {
	case "vocal", "roformer":
		captured[vramFlagSegmentSize] = true
		captured[vramFlagBatchSize] = true
		captured[vramFlagChunkSize] = true
	case "demucs":
		captured[vramFlagDemucsSegment] = true
		if cfg.Shifts > 0 {
			captured[vramFlagShifts] = true
		}
		if cfg.Jobs > 0 {
			captured[vramFlagJobs] = true
		}
	case "scnet":
		captured[vramFlagChunkSize] = true
		captured[vramFlagBatchSize] = true
	case "mdx", "mdxnet":
		captured[vramFlagSegmentSize] = true
		captured[vramFlagBatchSize] = true
	}
	return captured
}

// migrateVRAMMeasuredKey converts a legacy key (model|step|device|hash|duration)
// to the new wildcard key (model|step|device). Keys that already conform to the
// new format (segments after the third one are "flag=value") are returned
// unchanged.
func migrateVRAMMeasuredKey(oldKey string) string {
	parts := strings.Split(oldKey, "|")
	if len(parts) < 3 {
		return oldKey
	}
	// Legacy keys have exactly 5 segments and the last two are a hash and a
	// duration bucket (no '='). New keys use "flag=value" segments.
	if len(parts) == 5 && !strings.Contains(parts[3], "=") && !strings.Contains(parts[4], "=") {
		return vramMeasuredKey(parts[0], parts[1], parts[2], VRAMConfig{}, nil)
	}
	return oldKey
}

// vramStepTypeForModel returns the canonical step type used for VRAM
// measurements and lookups. It matches classifyModelType so the calculator and
// the sampler agree on the same key.
func vramStepTypeForModel(modelName string) string {
	return classifyModelType(modelName)
}

// vramFlagsMatch reports whether a stored record matches the requested flags.
// Captured flags must match exactly; uncaptured flags are wildcards.
func vramFlagsMatch(rec MeasuredVRAMRecord, req VRAMFlags) bool {
	if len(rec.Captured) == 0 {
		return true
	}
	for name := range rec.Captured {
		stored := vramFlagValueFromFlags(rec.Flags, name)
		requested := vramFlagValueFromFlags(req, name)
		if stored != requested {
			return false
		}
	}
	return true
}

// findMeasuredVRAMPeakInStore returns the conservative maximum measured peak
// and its record for the requested configuration. The second result is false
// when no measurement matches.
func findMeasuredVRAMPeakInStore(modelName, device string, cfg VRAMConfig) (MeasuredVRAMRecord, bool) {
	stepType := vramStepTypeForModel(modelName)
	prefix := vramMeasuredKey(modelName, stepType, device, VRAMConfig{}, nil)
	records := getVRAMMeasuredStore().findByPrefix(prefix)
	if len(records) == 0 {
		return MeasuredVRAMRecord{}, false
	}
	reqFlags := vramFlagsFromConfig(cfg)
	var matches []MeasuredVRAMRecord
	for _, r := range records {
		if vramFlagsMatch(r, reqFlags) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return MeasuredVRAMRecord{}, false
	}
	// Prefer the most specific match (most captured flags), then the highest
	// peak (conservative), then the most recent.
	sort.SliceStable(matches, func(i, j int) bool {
		si, sj := len(matches[i].Captured), len(matches[j].Captured)
		if si != sj {
			return si > sj
		}
		if matches[i].PeakMBMax != matches[j].PeakMBMax {
			return matches[i].PeakMBMax > matches[j].PeakMBMax
		}
		return matches[i].LastTS.After(matches[j].LastTS)
	})
	return matches[0], true
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
	captured := vramFlagsCaptured(cfg, stepType)
	key := vramMeasuredKey(modelName, stepType, device, cfg, captured)
	rec := MeasuredVRAMRecord{
		PeakMBMax:  footprint,
		PeakMBLast: footprint,
		N:          res.N,
		LastTS:     time.Now().UTC(),
		Duration:   cfg.Duration,
		Flags:      vramFlagsFromConfig(cfg),
		Captured:   captured,
	}
	store := getVRAMMeasuredStore()
	store.recordMeasured(key, rec)
	if err := store.save(); err != nil {
		Log("pipeline", "warn", fmt.Sprintf("Failed to save measured VRAM for %s: %v", modelName, err))
	}
	Log("pipeline", "info", fmt.Sprintf("VRAM measured: model=%s step=%s device=%s baseline=%d MB peak=%d MB footprint=%d MB n=%d",
		modelName, stepType, device, res.BaselineMB, res.PeakMB, footprint, res.N))
}
