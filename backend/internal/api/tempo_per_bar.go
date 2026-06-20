package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// TempoPerBarRequest is the JSON body for POST /api/audio/tempo-per-bar.
type TempoPerBarRequest struct {
	File string     `json:"file"`
	Bars []BarRatio `json:"bars"`
}

// BarRatio maps a 1-indexed bar number to a tempo ratio.
type BarRatio struct {
	Bar   int     `json:"bar"`
	Ratio float64 `json:"ratio"`
}

// TempoPerBarResponse is returned by POST /api/audio/tempo-per-bar.
type TempoPerBarResponse struct {
	File   string    `json:"file"`
	Bars   []int     `json:"bars"`
	Ratios []float64 `json:"ratios"`
}

// wavFormat holds the format details we need to preserve across segments.
type wavFormat struct {
	SampleRate  int
	NumChannels int
	BitDepth    int
	AudioFormat int
}

// handleTempoPerBar changes the tempo of individual bars using aubio for beat
// detection and rubberband for per-bar time stretching. Bars that are not
// listed in the request are copied unchanged.
func (s *Server) handleTempoPerBar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req TempoPerBarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if req.File == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "file is required"})
		return
	}
	if len(req.Bars) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bars cannot be empty"})
		return
	}

	seen := make(map[int]bool)
	for _, br := range req.Bars {
		if br.Bar <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "bar numbers must be greater than 0"})
			return
		}
		if br.Ratio <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "ratio must be greater than 0"})
			return
		}
		if seen[br.Bar] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("duplicate bar %d", br.Bar)})
			return
		}
		seen[br.Bar] = true
	}

	safeName := filepath.Base(req.File)
	projectRoot := findProjectRoot()
	dawBase := filepath.Join(projectRoot, "daw-data")

	sourcePath := filepath.Join(projectRoot, "input", safeName)
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		dawPath := filepath.Join(dawBase, safeName)
		if _, err := os.Stat(dawPath); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
			return
		}
		sourcePath = dawPath
	}

	if err := os.MkdirAll(dawBase, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create output dir: %v", err)})
		return
	}
	tmpDir := filepath.Join(dawBase, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to create tmp dir: %v", err)})
		return
	}

	beats, err := detectBeats(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "aubio beat failed: " + err.Error()})
		return
	}
	if len(beats) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "no beats detected"})
		return
	}

	duration, err := detectDuration(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read duration: " + err.Error()})
		return
	}

	numBars := (len(beats) + 3) / 4
	if numBars < 1 {
		numBars = 1
	}
	for _, br := range req.Bars {
		if br.Bar > numBars {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("bar %d exceeds available bars (%d)", br.Bar, numBars),
			})
			return
		}
	}

	ratioMap := make(map[int]float64)
	for _, br := range req.Bars {
		ratioMap[br.Bar] = br.Ratio
	}

	inputBuf, inputFmt, err := readWAV(sourcePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read input WAV: " + err.Error()})
		return
	}

	var tempFiles []string
	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	var outputBuffers []*audio.IntBuffer

	for bar := 1; bar <= numBars; bar++ {
		startTime, endTime := barTimeRange(beats, duration, bar)

		startSample := int(startTime * float64(inputFmt.SampleRate) * float64(inputFmt.NumChannels))
		endSample := int(endTime * float64(inputFmt.SampleRate) * float64(inputFmt.NumChannels))
		if endSample > len(inputBuf.Data) {
			endSample = len(inputBuf.Data)
		}
		if startSample < 0 {
			startSample = 0
		}
		if startSample > endSample {
			startSample = endSample
		}

		segment := &audio.IntBuffer{
			Data:   inputBuf.Data[startSample:endSample],
			Format: inputBuf.Format,
		}

		ratio, process := ratioMap[bar]
		if !process {
			ratio = 1.0
		}

		if ratio != 1.0 {
			segName := fmt.Sprintf("t4_seg_%d_%s", bar, safeName)
			segPath := filepath.Join(tmpDir, segName)
			procName := fmt.Sprintf("t4_proc_%d_%s", bar, safeName)
			procPath := filepath.Join(tmpDir, procName)

			if err := writeWAV(segPath, segment, inputFmt); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("failed to write segment for bar %d: %v", bar, err),
				})
				return
			}
			tempFiles = append(tempFiles, segPath)

			cmd := exec.Command("rubberband", "--tempo", fmt.Sprintf("%f", ratio), "--pitch", "0", segPath, procPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("rubberband failed for bar %d: %s", bar, strings.TrimSpace(string(out))),
				})
				return
			}
			tempFiles = append(tempFiles, procPath)

			procBuf, procFmt, err := readWAV(procPath)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("failed to read processed segment for bar %d: %v", bar, err),
				})
				return
			}
			if procFmt.SampleRate != inputFmt.SampleRate || procFmt.NumChannels != inputFmt.NumChannels {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("processed segment for bar %d has incompatible format", bar),
				})
				return
			}
			outputBuffers = append(outputBuffers, procBuf)
		} else {
			outputBuffers = append(outputBuffers, segment)
		}
	}

	totalLen := 0
	for _, b := range outputBuffers {
		totalLen += len(b.Data)
	}
	outputData := make([]int, 0, totalLen)
	for _, b := range outputBuffers {
		outputData = append(outputData, b.Data...)
	}
	outputBuf := &audio.IntBuffer{
		Data:   outputData,
		Format: inputBuf.Format,
	}

	baseName := safeName[:len(safeName)-len(filepath.Ext(safeName))]
	ext := filepath.Ext(safeName)
	outputName := "tempo_per_bar_" + baseName + ext
	outputPath := filepath.Join(dawBase, outputName)

	if err := writeWAV(outputPath, outputBuf, inputFmt); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to write output WAV: %v", err)})
		return
	}

	bars := make([]int, 0, len(req.Bars))
	ratios := make([]float64, 0, len(req.Bars))
	sort.Slice(req.Bars, func(i, j int) bool {
		return req.Bars[i].Bar < req.Bars[j].Bar
	})
	for _, br := range req.Bars {
		bars = append(bars, br.Bar)
		ratios = append(ratios, br.Ratio)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TempoPerBarResponse{
		File:   outputName,
		Bars:   bars,
		Ratios: ratios,
	})
}

// barTimeRange returns the start and end times in seconds for a 1-indexed bar.
// Bars are grouped as four beats each; the end time is the start of the next
// bar or the total duration when there are no more beats.
func barTimeRange(beats []float64, duration float64, bar int) (float64, float64) {
	startIdx := (bar - 1) * 4
	endIdx := startIdx + 4

	start := 0.0
	if startIdx < len(beats) {
		start = beats[startIdx]
	}

	end := duration
	if endIdx < len(beats) {
		end = beats[endIdx]
	}
	return start, end
}

// readWAV reads a WAV file into an IntBuffer and returns its format.
func readWAV(path string) (*audio.IntBuffer, *wavFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	dec := wav.NewDecoder(f)
	buf, err := dec.FullPCMBuffer()
	if err != nil {
		return nil, nil, err
	}

	fmt := &wavFormat{
		SampleRate:  int(dec.SampleRate),
		NumChannels: int(dec.NumChans),
		BitDepth:    int(dec.BitDepth),
		AudioFormat: int(dec.WavAudioFormat),
	}
	return buf, fmt, nil
}

// writeWAV writes an IntBuffer to a WAV file preserving the source format.
func writeWAV(path string, buf *audio.IntBuffer, fmt *wavFormat) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := wav.NewEncoder(f, fmt.SampleRate, fmt.BitDepth, fmt.NumChannels, fmt.AudioFormat)
	if err := enc.Write(buf); err != nil {
		return err
	}
	return enc.Close()
}
