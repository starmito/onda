package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		want          int
		tolerance     float64
	}{
		{"viperx 64 b1", "BS_Roformer_Viperx", 64, 0, 1, 0, 1662, 0.05},
		{"viperx 128 b1", "BS_Roformer_Viperx", 128, 0, 1, 0, 2152, 0.05},
		{"viperx 256 b1", "BS_Roformer_Viperx", 256, 0, 1, 0, 2898, 0.05},
		{"viperx 512 b1", "BS_Roformer_Viperx", 512, 0, 1, 0, 4656, 0.05},
		{"viperx 1024 b1", "BS_Roformer_Viperx", 1024, 0, 1, 0, 8116, 0.05},
		{"viperx 256 b2", "BS_Roformer_Viperx", 256, 0, 2, 0, 4724, 0.05},
		{"viperx 512 b2", "BS_Roformer_Viperx", 512, 0, 2, 0, 8178, 0.05},
		{"viperx 1024 b2", "BS_Roformer_Viperx", 1024, 0, 2, 0, 15108, 0.05},
		{"demucs seg0", "htdemucs_ft", 0, 0, 0, 0, 1572, 0.05},
		{"demucs seg7", "htdemucs_ft", 0, 0, 0, 7, 1106, 0.05},

		// MDX23C measured peaks (batch 1). dim_t = segment_size*3 + 33.
		{"mdx dim_t417 b1", "MDX23C", 128, 0, 1, 0, 2080, 0.05},
		{"mdx dim_t801 b1", "MDX23C", 256, 0, 1, 0, 2478, 0.05},
		{"mdx dim_t1569 b1", "MDX23C", 512, 0, 1, 0, 6748, 0.05},
		{"mdx dim_t2337 b1", "MDX23C", 768, 0, 1, 0, 9872, 0.05},
		{"mdx dim_t801 b2", "MDX23C", 256, 0, 2, 0, 4956, 0.05},
		{"mdx default segment0", "MDX23C", 0, 0, 1, 0, 2080, 0.05},
		{"mdxnet dim_t801 b1", "MDXNet_Vocals", 256, 0, 1, 0, 2478, 0.05},
		{"onnx dim_t801 b1", "UVR_MDXNET_3_9662", 256, 0, 1, 0, 2478, 0.05},

		// SCNet measured peaks. Base is linear in chunk_size (batch 1),
		// then multiplied by batch. Overlap is ignored.
		{"scnet chunk242550 b1", "SCNet", 0, 242550, 1, 0, 600, 0.05},
		{"scnet chunk485100 b1", "SCNet", 0, 485100, 1, 0, 948, 0.05},
		{"scnet chunk970200 b1", "SCNet", 0, 970200, 1, 0, 1762, 0.05},
		{"scnet chunk485100 b2", "SCNet", 0, 485100, 2, 0, 1896, 0.05},
		{"scnet chunk485100 b4", "SCNet", 0, 485100, 4, 0, 3792, 0.05},
		{"scnet chunk485100 b8", "SCNet", 0, 485100, 8, 0, 7584, 0.05},

		{"unknown fallback", "not_a_known_model_v1", 0, 0, 0, 0, 2000, 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateVRAMMB(tt.model, tt.segmentSize, tt.chunkSize, tt.batchSize, tt.demucsSegment)
			tol := tt.tolerance
			if tol <= 0 {
				tol = 0.05
			}
			lower := float64(tt.want) * (1 - tol)
			upper := float64(tt.want) * (1 + tol)
			if float64(got) < lower || float64(got) > upper {
				t.Errorf(
					"estimateVRAMMB(%q, %d, %d, %d, %d) = %d; want within %.0f%% of %d (%.2f..%.2f)",
					tt.model, tt.segmentSize, tt.chunkSize, tt.batchSize, tt.demucsSegment,
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
