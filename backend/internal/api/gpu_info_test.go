package api

import "testing"

func TestEstimateVRAMMB_Empirical(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		segmentSize   int
		batchSize     int
		demucsSegment int
		want          int
		tolerance     float64
	}{
		{"viperx 64 b1", "BS_Roformer_Viperx", 64, 1, 0, 1662, 0.05},
		{"viperx 128 b1", "BS_Roformer_Viperx", 128, 1, 0, 2152, 0.05},
		{"viperx 256 b1", "BS_Roformer_Viperx", 256, 1, 0, 2898, 0.05},
		{"viperx 512 b1", "BS_Roformer_Viperx", 512, 1, 0, 4656, 0.05},
		{"viperx 1024 b1", "BS_Roformer_Viperx", 1024, 1, 0, 8116, 0.05},
		{"viperx 256 b2", "BS_Roformer_Viperx", 256, 2, 0, 4724, 0.05},
		{"viperx 512 b2", "BS_Roformer_Viperx", 512, 2, 0, 8178, 0.05},
		{"viperx 1024 b2", "BS_Roformer_Viperx", 1024, 2, 0, 15108, 0.05},
		{"demucs seg0", "htdemucs_ft", 0, 0, 0, 1572, 0.05},
		{"demucs seg7", "htdemucs_ft", 0, 0, 7, 1106, 0.05},

		// MDX23C measured peaks (batch 1). dim_t = segment_size*3 + 33.
		{"mdx dim_t417 b1", "MDX23C", 128, 1, 0, 2080, 0.05},
		{"mdx dim_t801 b1", "MDX23C", 256, 1, 0, 2478, 0.05},
		{"mdx dim_t1569 b1", "MDX23C", 512, 1, 0, 6748, 0.05},
		{"mdx dim_t2337 b1", "MDX23C", 768, 1, 0, 9872, 0.05},
		{"mdx dim_t801 b2", "MDX23C", 256, 2, 0, 4956, 0.05},
		{"mdx default segment0", "MDX23C", 0, 1, 0, 2080, 0.05},
		{"mdxnet dim_t801 b1", "MDXNet_Vocals", 256, 1, 0, 2478, 0.05},
		{"onnx dim_t801 b1", "UVR_MDXNET_3_9662", 256, 1, 0, 2478, 0.05},

		// SCNet measured peaks. Base is linear in chunk_size (batch 1),
		// then multiplied by batch. Overlap is ignored.
		{"scnet chunk242550 b1", "SCNet", 242550, 1, 0, 600, 0.05},
		{"scnet chunk485100 b1", "SCNet", 485100, 1, 0, 948, 0.05},
		{"scnet chunk970200 b1", "SCNet", 970200, 1, 0, 1762, 0.05},
		{"scnet chunk485100 b2", "SCNet", 485100, 2, 0, 1828, 0.20},
		{"scnet chunk485100 b4", "SCNet", 485100, 4, 0, 3316, 0.20},
		{"scnet chunk485100 b8", "SCNet", 485100, 8, 0, 6372, 0.20},

		{"unknown fallback", "not_a_known_model_v1", 0, 0, 0, 2000, 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateVRAMMB(tt.model, tt.segmentSize, tt.batchSize, tt.demucsSegment)
			tol := tt.tolerance
			if tol <= 0 {
				tol = 0.05
			}
			lower := float64(tt.want) * (1 - tol)
			upper := float64(tt.want) * (1 + tol)
			if float64(got) < lower || float64(got) > upper {
				t.Errorf(
					"estimateVRAMMB(%q, %d, %d, %d) = %d; want within %.0f%% of %d (%.2f..%.2f)",
					tt.model, tt.segmentSize, tt.batchSize, tt.demucsSegment,
					got, tol*100, tt.want, lower, upper,
				)
			}
		})
	}
}
