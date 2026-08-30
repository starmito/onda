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
	}{
		{"viperx 64 b1", "BS_Roformer_Viperx", 64, 1, 0, 1662},
		{"viperx 128 b1", "BS_Roformer_Viperx", 128, 1, 0, 2152},
		{"viperx 256 b1", "BS_Roformer_Viperx", 256, 1, 0, 2898},
		{"viperx 512 b1", "BS_Roformer_Viperx", 512, 1, 0, 4656},
		{"viperx 1024 b1", "BS_Roformer_Viperx", 1024, 1, 0, 8116},
		{"viperx 256 b2", "BS_Roformer_Viperx", 256, 2, 0, 4724},
		{"viperx 512 b2", "BS_Roformer_Viperx", 512, 2, 0, 8178},
		{"viperx 1024 b2", "BS_Roformer_Viperx", 1024, 2, 0, 15108},
		{"demucs seg0", "htdemucs_ft", 0, 0, 0, 1572},
		{"demucs seg7", "htdemucs_ft", 0, 0, 7, 1106},
		{"mdx", "MDX23C", 0, 0, 0, 2476},
		{"mdxnet", "MDXNet_Vocals", 0, 0, 0, 2476},
		{"onnx", "UVR_MDXNET_3_9662", 0, 0, 0, 2476},
		{"scnet", "SCNet", 0, 0, 0, 1828},
		{"unknown fallback", "not_a_known_model_v1", 0, 0, 0, 2000},
	}

	const tolerance = 0.05
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateVRAMMB(tt.model, tt.segmentSize, tt.batchSize, tt.demucsSegment)
			lower := float64(tt.want) * (1 - tolerance)
			upper := float64(tt.want) * (1 + tolerance)
			if float64(got) < lower || float64(got) > upper {
				t.Errorf(
					"estimateVRAMMB(%q, %d, %d, %d) = %d; want within %.0f%% of %d (%.2f..%.2f)",
					tt.model, tt.segmentSize, tt.batchSize, tt.demucsSegment,
					got, tolerance*100, tt.want, lower, upper,
				)
			}
		})
	}
}
