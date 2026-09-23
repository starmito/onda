package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestEstimateVRAMMB_Empirical(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		segmentSize   int
		chunkSize     int
		batchSize     int
		demucsSegment int
		duration      int
		want          int
		tolerance     float64
	}{
		{"vocal 64 b1", "BS_Roformer_Viperx", 64, 0, 1, 0, 0, 2096, 0.05},
		{"vocal 128 b1", "BS_Roformer_Viperx", 128, 0, 1, 0, 0, 2192, 0.05},
		{"vocal 256 b1", "BS_Roformer_Viperx", 256, 0, 1, 0, 0, 2384, 0.05},
		{"vocal 512 b1", "BS_Roformer_Viperx", 512, 0, 1, 0, 0, 2768, 0.05},
		{"vocal 1024 b1", "BS_Roformer_Viperx", 1024, 0, 1, 0, 0, 3536, 0.05},
		{"vocal 256 b2", "BS_Roformer_Viperx", 256, 0, 2, 0, 0, 3268, 0.05},
		{"vocal 512 b2", "BS_Roformer_Viperx", 512, 0, 2, 0, 0, 4036, 0.05},
		{"vocal 1024 b2", "BS_Roformer_Viperx", 1024, 0, 2, 0, 0, 5572, 0.05},
		{"demucs seg0", "htdemucs_ft", 0, 0, 0, 0, 0, 1572, 0.05},
		{"demucs seg7", "htdemucs_ft", 0, 0, 0, 7, 0, 1500, 0.05},

		// MDX23C measured peaks (batch 1). segment_size is dim_t directly.
		{"mdx dim_t256 b1", "MDX23C", 256, 0, 1, 0, 0, 2080, 0.05},
		{"mdx dim_t512 b1", "MDX23C", 512, 0, 1, 0, 0, 2178, 0.05},
		{"mdx dim_t768 b1", "MDX23C", 768, 0, 1, 0, 0, 2444, 0.05},
		{"mdx dim_t1024 b1", "MDX23C", 1024, 0, 1, 0, 0, 3716, 0.05},
		{"mdx dim_t256 b2", "MDX23C", 256, 0, 2, 0, 0, 4160, 0.05},
		{"mdx default segment0", "MDX23C", 0, 0, 1, 0, 0, 2080, 0.05},
		{"mdxnet dim_t256 b1", "MDXNet_Vocals", 256, 0, 1, 0, 0, 2080, 0.05},
		{"onnx dim_t256 b1", "UVR_MDXNET_3_9662", 256, 0, 1, 0, 0, 2080, 0.05},

		// SCNet measured peaks. Base is linear in chunk_size (batch 1),
		// then multiplied by batch. Overlap is ignored.
		{"scnet chunk242550 b1", "SCNet", 0, 242550, 1, 0, 0, 600, 0.05},
		{"scnet chunk485100 b1", "SCNet", 0, 485100, 1, 0, 0, 948, 0.05},
		{"scnet chunk970200 b1", "SCNet", 0, 970200, 1, 0, 0, 1762, 0.05},
		{"scnet chunk485100 b2", "SCNet", 0, 485100, 2, 0, 0, 1896, 0.05},
		{"scnet chunk485100 b4", "SCNet", 0, 485100, 4, 0, 0, 3792, 0.05},
		{"scnet chunk485100 b8", "SCNet", 0, 485100, 8, 0, 0, 7584, 0.05},

		{"unknown fallback", "not_a_known_model_v1", 0, 0, 0, 0, 0, 2000, 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateVRAMMB(tt.model, tt.segmentSize, tt.chunkSize, tt.batchSize, tt.demucsSegment, tt.duration)
			tol := tt.tolerance
			if tol <= 0 {
				tol = 0.05
			}
			lower := float64(tt.want) * (1 - tol)
			upper := float64(tt.want) * (1 + tol)
			if float64(got) < lower || float64(got) > upper {
				t.Errorf(
					"estimateVRAMMB(%q, %d, %d, %d, %d, %d) = %d; want within %.0f%% of %d (%.2f..%.2f)",
					tt.model, tt.segmentSize, tt.chunkSize, tt.batchSize, tt.demucsSegment, tt.duration,
					got, tol*100, tt.want, lower, upper,
				)
			}
		})
	}
}

func TestClassifyModelType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"htdemucs_ft", "demucs"},
		{"htdemucs", "demucs"},
		{"BS_Roformer_Viperx", "vocal"},
		{"melband_kj", "vocal"},
		{"MDX23C", "mdx"},
		{"MDXNet_Vocals", "mdxnet"},
		{"UVR_MDXNET_3_9662", "mdxnet"},
		{"SCNet", "scnet"},
		{"unknown_thing", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyModelType(tt.name); got != tt.want {
				t.Errorf("classifyModelType(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestParseNvidiaSmiMemory(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTotal  int
		wantUsed   int
		wantFree   int
		wantErr    bool
	}{
		{
			name:      "plain csv",
			input:     "16311, 376, 15475\n",
			wantTotal: 16311,
			wantUsed:  376,
			wantFree:  15475,
		},
		{
			name:      "with unit suffixes",
			input:     "16311 MiB, 376 MiB, 15475 MiB",
			wantTotal: 16311,
			wantUsed:  376,
			wantFree:  15475,
		},
		{
			name:      "extra whitespace and trailing garbage line",
			input:     "  8192 , 1024 , 7168  \n[Not Supported]\n",
			wantTotal: 8192,
			wantUsed:  1024,
			wantFree:  7168,
		},
		{
			name:    "empty",
			input:   "   \n",
			wantErr: true,
		},
		{
			name:    "too few fields",
			input:   "16311, 376\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, used, free, err := parseNvidiaSmiMemory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNvidiaSmiMemory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if total != tt.wantTotal || used != tt.wantUsed || free != tt.wantFree {
				t.Errorf("parseNvidiaSmiMemory(%q) = (%d,%d,%d), want (%d,%d,%d)",
					tt.input, total, used, free, tt.wantTotal, tt.wantUsed, tt.wantFree)
			}
		})
	}
}

func TestCheckVramHeadroom_NeverRequiresMoreThanTotal(t *testing.T) {
	tests := []struct {
		name          string
		freeMB        int
		totalMB       int
		model         string
		stepType      string
		fallbackModel string
		cfg           VRAMConfig
		wantOK        bool
		wantRequired  int
		wantReason    string
		wantWarning   string
	}{
		{
			name:         "vocal ample free fits with margin",
			freeMB:       16000,
			totalMB:      16311,
			model:        "BS_Roformer_Viperx",
			stepType:     "vocal",
			wantOK:       true,
			wantRequired: 2400,
			wantWarning:  "",
		},
		{
			name:         "vocal free below requirement blocks",
			freeMB:       950,
			totalMB:      16311,
			model:        "BS_Roformer_Viperx",
			stepType:     "vocal",
			wantOK:       false,
			wantRequired: 2400,
			wantReason:   `insufficient VRAM: model "BS_Roformer_Viperx" (step "vocal") needs ~2400 MiB (with 20% margin), only 950 MiB free`,
		},
		{
			name:         "demucs margin fits free above requirement",
			freeMB:       1800,
			totalMB:      8192,
			model:        "htdemucs_ft",
			stepType:     "demucs",
			cfg:          VRAMConfig{DemucsSegment: 7},
			wantOK:       true,
			wantRequired: 1800,
		},
		{
			name:         "demucs margin fits free below requirement blocks",
			freeMB:       1799,
			totalMB:      8192,
			model:        "htdemucs_ft",
			stepType:     "demucs",
			cfg:          VRAMConfig{DemucsSegment: 7},
			wantOK:       false,
			wantRequired: 1800,
			wantReason:   `insufficient VRAM: model "htdemucs_ft" (step "demucs") needs ~1800 MiB (with 20% margin), only 1799 MiB free`,
		},
		{
			name:          "unknown resolved by fallback model",
			freeMB:        16000,
			totalMB:       16311,
			model:         "unknown",
			stepType:      "vocal",
			fallbackModel: "BS_Roformer_Viperx",
			wantOK:        true,
			wantRequired:  2400,
			wantWarning:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, required, reason, warning := checkVramHeadroom(tt.freeMB, tt.totalMB, tt.model, tt.stepType, tt.cfg, tt.fallbackModel)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if required != tt.wantRequired {
				t.Errorf("required = %d, want %d", required, tt.wantRequired)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			if warning != tt.wantWarning {
				t.Errorf("warning = %q, want %q", warning, tt.wantWarning)
			}
		})
	}
}

func TestRamRequiredMB_MeasuredVocal(t *testing.T) {
	got := ramRequiredMB("BS_Roformer_Viperx", "vocal")
	want := 2352 // round(2045 * 1.15)
	if got != want {
		t.Errorf("ramRequiredMB(%q, %q) = %d, want %d", "BS_Roformer_Viperx", "vocal", got, want)
	}

	ok, _, reason := checkRamHeadroom(4700, "BS_Roformer_Viperx", "vocal")
	if !ok {
		t.Errorf("checkRamHeadroom(4700, BS_Roformer_Viperx, vocal) ok = false, want true")
	}
	if reason != "" {
		t.Errorf("checkRamHeadroom(4700, BS_Roformer_Viperx, vocal) reason = %q, want empty", reason)
	}
}

func TestRamRequiredMB_UnknownVocalUsesConservativeHeuristic(t *testing.T) {
	got := ramRequiredMB("unknown_vocal_model", "vocal")
	want := 3533 // round(3072 * 1.15)
	if got != want {
		t.Errorf("ramRequiredMB(%q, %q) = %d, want %d", "unknown_vocal_model", "vocal", got, want)
	}
}

func TestHandleVRAMCalculator_ClassifiesModelType(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=htdemucs_ft", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VRAMCalculatorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}
	if resp.Models[0].Type != "demucs" {
		t.Errorf("expected type demucs, got %q", resp.Models[0].Type)
	}
}

func TestHandleVRAMCalculator_UsesAnalyticalEstimates(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	tests := []struct {
		name     string
		query    string
		wantVRAM int
		reliable bool
	}{
		{
			name:     "BS_Roformer_SW_6stem uses estimate so segment_size can move",
			query:    "models=BS_Roformer_SW_6stem&chunk_size=485100&batch_size=1&duration=30",
			wantVRAM: 2000, // analytical estimate with segment_size=0
			reliable: false, // chunk_size is ignored by the vocal estimate
		},
		{
			name:     "SCNet_MUSDB18 whole song matches reference estimate",
			query:    "models=SCNet_MUSDB18&chunk_size=0&batch_size=1&duration=296",
			wantVRAM: 6372,
			reliable: true,
		},
		{
			name:     "SCNet_MUSDB18 whole song scales with duration",
			query:    "models=SCNet_MUSDB18&chunk_size=0&batch_size=1&duration=148",
			wantVRAM: 3186,
			reliable: true,
		},
		{
			name:     "Roformer chunk_size ignored in analytical estimate triggers warning",
			query:    "models=BS_Roformer_SW_6stem&segment_size=1101&chunk_size=100000&batch_size=1",
			wantVRAM: 3652, // round(1500 + (500 + 1.5*1101) * 1)
			reliable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?"+tt.query, nil)
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			var resp VRAMCalculatorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if len(resp.Models) != 1 {
				t.Fatalf("expected 1 model, got %d", len(resp.Models))
			}
			if resp.Models[0].VRAMMB != tt.wantVRAM {
				t.Errorf("VRAM = %d, want %d", resp.Models[0].VRAMMB, tt.wantVRAM)
			}
			if resp.Reliable != tt.reliable {
				t.Errorf("reliable = %v, want %v", resp.Reliable, tt.reliable)
			}
		})
	}
}

func TestHandleVRAMCalculator_ViperxDurationAware(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	tests := []struct {
		name     string
		query    string
		wantVRAM int
		maxVRAM  int
		minVRAM  int
		fits     bool
		reliable bool
	}{
		{
			name:     "Viperx short audio is not rejected",
			query:    "models=BS_Roformer_Viperx&segment_size=1276&batch_size=4&duration=3",
			maxVRAM:  6000,
			minVRAM:  3000,
			fits:     true,
			reliable: false,
		},
		{
			name:     "Viperx long audio warns honestly",
			query:    "models=BS_Roformer_Viperx&segment_size=1276&batch_size=4&duration=300",
			maxVRAM:  15000,
			minVRAM:  9000,
			fits:     true,
			reliable: false,
		},
		{
			name:     "SW estimate used with long audio instead of stale measured peak",
			query:    "models=BS_Roformer_SW_6stem&chunk_size=485100&batch_size=1&duration=300",
			wantVRAM: 2000,
			fits:     true,
			reliable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?"+tt.query, nil)
			rr := httptest.NewRecorder()
			s.mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			var resp VRAMCalculatorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if len(resp.Models) != 1 {
				t.Fatalf("expected 1 model, got %d", len(resp.Models))
			}
			vram := resp.Models[0].VRAMMB
			if tt.wantVRAM > 0 && vram != tt.wantVRAM {
				t.Errorf("VRAM = %d, want %d", vram, tt.wantVRAM)
			}
			if tt.minVRAM > 0 && vram < tt.minVRAM {
				t.Errorf("VRAM = %d, want at least %d", vram, tt.minVRAM)
			}
			if tt.maxVRAM > 0 && vram > tt.maxVRAM {
				t.Errorf("VRAM = %d, want at most %d", vram, tt.maxVRAM)
			}
			if resp.Fits != tt.fits {
				t.Errorf("fits = %v, want %v", resp.Fits, tt.fits)
			}
			if resp.Reliable != tt.reliable {
				t.Errorf("reliable = %v, want %v", resp.Reliable, tt.reliable)
			}
		})
	}
}

func TestHandleVRAMCalculator_SegmentSizeAffectsVRAM(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	models := []string{"BS_Roformer_Viperx", "MDX23C"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			reqLow := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models="+model+"&segment_size=128&batch_size=1", nil)
			reqHigh := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models="+model+"&segment_size=1024&batch_size=1", nil)

			low := mustVRAMCalc(t, s, reqLow)
			high := mustVRAMCalc(t, s, reqHigh)

			if low.Models[0].VRAMMB <= 0 || high.Models[0].VRAMMB <= 0 {
				t.Fatalf("VRAM must be positive: low=%d high=%d", low.Models[0].VRAMMB, high.Models[0].VRAMMB)
			}
			if high.Models[0].VRAMMB <= low.Models[0].VRAMMB {
				t.Errorf("higher segment_size must increase VRAM: low=%d high=%d", low.Models[0].VRAMMB, high.Models[0].VRAMMB)
			}
		})
	}
}

func TestHandleVRAMCalculator_DemucsSegmentAffectsVRAM(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	models := []string{"htdemucs_ft"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			// The calculator accepts both "segment" (manifest name) and "demucs_segment".
			reqLow := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models="+model+"&demucs_segment=0", nil)
			reqMid := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models="+model+"&segment=3", nil)
			reqHigh := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models="+model+"&demucs_segment=7", nil)

			low := mustVRAMCalc(t, s, reqLow)
			mid := mustVRAMCalc(t, s, reqMid)
			high := mustVRAMCalc(t, s, reqHigh)

			for _, r := range []VRAMCalculatorResponse{low, mid, high} {
				if r.Models[0].VRAMMB <= 0 {
					t.Fatalf("VRAM must be positive: %d", r.Models[0].VRAMMB)
				}
			}
			if low.Models[0].VRAMMB == mid.Models[0].VRAMMB || mid.Models[0].VRAMMB == high.Models[0].VRAMMB {
				t.Errorf("demucs_segment must change VRAM: 0=%d 3=%d 7=%d", low.Models[0].VRAMMB, mid.Models[0].VRAMMB, high.Models[0].VRAMMB)
			}
		})
	}
}

func TestHandleVRAMCalculator_SCNetDurationAffectsVRAM(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	reqShort := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=SCNet_MUSDB18&chunk_size=0&batch_size=1&duration=100", nil)
	reqLong := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?models=SCNet_MUSDB18&chunk_size=0&batch_size=1&duration=296", nil)

	short := mustVRAMCalc(t, s, reqShort)
	long := mustVRAMCalc(t, s, reqLong)

	if short.Models[0].VRAMMB <= 0 || long.Models[0].VRAMMB <= 0 {
		t.Fatalf("VRAM must be positive: short=%d long=%d", short.Models[0].VRAMMB, long.Models[0].VRAMMB)
	}
	if long.Models[0].VRAMMB <= short.Models[0].VRAMMB {
		t.Errorf("longer duration must increase SCNet VRAM: short=%d long=%d", short.Models[0].VRAMMB, long.Models[0].VRAMMB)
	}
}

func TestHandleVRAMCalculator_FlagSweep_NoException(t *testing.T) {
	origGPU := gpuInfoProvider
	defer func() { gpuInfoProvider = origGPU }()
	gpuInfoProvider = func() GPUInfoResponse {
		return GPUInfoResponse{OK: true, VRAMTotalMB: 16311, VRAMUsedMB: 1000, VRAMFreeMB: 15311}
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)

	// Representative model for each family and the flags that affect its VRAM.
	// duration is not part of knownFlags, so it is swept over a practical range.
	sweeps := []struct {
		model string
		flags []string
	}{
		{"BS_Roformer_Viperx", []string{"segment_size", "batch_size"}},
		{"MDX23C", []string{"segment_size", "batch_size"}},
		{"SCNet_MUSDB18", []string{"chunk_size", "batch_size"}},
		{"htdemucs_ft", []string{"segment", "demucs_segment"}},
	}

	for _, sw := range sweeps {
		t.Run(sw.model, func(t *testing.T) {
			for _, flag := range sw.flags {
				var min, max, step int
				switch flag {
				case "duration":
					min, max, step = 1, 600, 30
				case "demucs_segment":
					def := knownFlags["segment"]
					min, max, step = int(toFloat64(def.Min)), int(toFloat64(def.Max)), int(toFloat64(def.Step))
				default:
					def, ok := knownFlags[flag]
					if !ok {
						t.Fatalf("unknown flag %q", flag)
					}
					min, max, step = int(toFloat64(def.Min)), int(toFloat64(def.Max)), int(toFloat64(def.Step))
				}
				if step < 1 {
					step = 1
				}
				for value := min; value <= max; value += step {
					q := "models=" + sw.model + "&" + flag + "=" + strconv.Itoa(value)
					req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?"+q, nil)
					resp := mustVRAMCalc(t, s, req)
					if len(resp.Models) != 1 {
						t.Fatalf("expected 1 model for %s, got %d", q, len(resp.Models))
					}
					if resp.Models[0].VRAMMB <= 0 {
						t.Errorf("VRAM must be positive for %s: got %d", q, resp.Models[0].VRAMMB)
					}
				}
			}
		})
	}

	// Sweep SCNet duration separately because it is not declared in knownFlags.
	t.Run("SCNet_MUSDB18_duration", func(t *testing.T) {
		for dur := 1; dur <= 600; dur += 30 {
			q := "models=SCNet_MUSDB18&chunk_size=0&batch_size=1&duration=" + strconv.Itoa(dur)
			req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?"+q, nil)
			resp := mustVRAMCalc(t, s, req)
			if len(resp.Models) != 1 {
				t.Fatalf("expected 1 model for %s, got %d", q, len(resp.Models))
			}
			if resp.Models[0].VRAMMB <= 0 {
				t.Errorf("VRAM must be positive for %s: got %d", q, resp.Models[0].VRAMMB)
			}
		}
	})

	// Edge combination sweep: min/max/default values together must not explode.
	combos := []string{
		"models=BS_Roformer_Viperx&segment_size=128&batch_size=8&duration=600",
		"models=BS_Roformer_Viperx&segment_size=2048&batch_size=8&duration=600",
		"models=MDX23C&segment_size=2048&batch_size=8",
		"models=SCNet_MUSDB18&chunk_size=600&batch_size=8&duration=600",
		"models=SCNet_MUSDB18&chunk_size=0&batch_size=8&duration=600",
		"models=htdemucs_ft&demucs_segment=7&segment=7",
	}
	for _, q := range combos {
		t.Run("combo_"+q, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/gpu/vram-calculator?"+q, nil)
			resp := mustVRAMCalc(t, s, req)
			if len(resp.Models) < 1 {
				t.Fatalf("expected at least 1 model for %s, got %d", q, len(resp.Models))
			}
			for _, m := range resp.Models {
				if m.VRAMMB <= 0 {
					t.Errorf("VRAM must be positive for %s: model %s got %d", q, m.Name, m.VRAMMB)
				}
			}
		})
	}
}

func mustVRAMCalc(t *testing.T, s *Server, req *http.Request) VRAMCalculatorResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp VRAMCalculatorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}
