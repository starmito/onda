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
		{"viperx 256 b1", "BS_Roformer_Viperx", 256, 1, 0, 2639},
		{"viperx 1024 b1", "BS_Roformer_Viperx", 1024, 1, 0, 7777},
		{"viperx 256 b2", "BS_Roformer_Viperx", 256, 2, 0, 4429},
		{"viperx 256 b4", "BS_Roformer_Viperx", 256, 4, 0, 6221},
		{"demucs seg0", "htdemucs_ft", 0, 0, 0, 1572},
		{"demucs seg7", "htdemucs_ft", 0, 0, 7, 1106},
		{"mdx", "MDX23C", 0, 0, 0, 2476},
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
