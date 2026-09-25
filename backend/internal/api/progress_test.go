package api

import "testing"

// TestComputeJobProgress_WeightsStepsEqually verifies that a song's progress is
// the simple arithmetic mean of its sequential steps (weight 1 per step).
func TestComputeJobProgress_WeightsStepsEqually(t *testing.T) {
	steps := []Step{
		{ID: "vocal", Name: "Voz", Status: "done", Progress: 100},
		{ID: "demucs", Name: "Demucs", Status: "running", Progress: 40},
	}
	got := computeJobProgress(steps, 0, "processing")
	if got != 70 {
		t.Errorf("expected 70 for (100+40)/2, got %d", got)
	}
}

// TestComputeQueueProgress_ProportionalAcrossSongs verifies the global queue
// progress required by Adri: consecutive, proportional, one bar per song.
func TestComputeQueueProgress_ProportionalAcrossSongs(t *testing.T) {
	tests := []struct {
		name  string
		jobs  []*JobState
		want  int
	}{
		{
			name: "2 songs, first done, second waiting",
			jobs: []*JobState{
				{Song: "a", Status: "done", Progress: 100},
				{Song: "b", Status: "waiting", Progress: 0},
			},
			want: 50,
		},
		{
			name: "2 songs, first at 28, second waiting",
			jobs: []*JobState{
				{Song: "a", Status: "processing", Progress: 28},
				{Song: "b", Status: "waiting", Progress: 0},
			},
			want: 14,
		},
		{
			name: "2 songs, both done",
			jobs: []*JobState{
				{Song: "a", Status: "done", Progress: 100},
				{Song: "b", Status: "done", Progress: 100},
			},
			want: 100,
		},
		{
			name: "3 songs, first done, others waiting",
			jobs: []*JobState{
				{Song: "a", Status: "done", Progress: 100},
				{Song: "b", Status: "waiting", Progress: 0},
				{Song: "c", Status: "waiting", Progress: 0},
			},
			want: 33,
		},
		{
			name: "empty queue",
			jobs: []*JobState{},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeQueueProgress(tt.jobs)
			if got != tt.want {
				t.Errorf("computeQueueProgress() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestComputeQueueProgress_ClampsBelow100UntilAllDone keeps the deliberate
// honesty rule: the global bar may not show 100% while any job is unfinished.
func TestComputeQueueProgress_ClampsBelow100UntilAllDone(t *testing.T) {
	jobs := []*JobState{
		{Song: "a", Status: "done", Progress: 100},
		{Song: "b", Status: "processing", Progress: 100},
	}
	got := computeQueueProgress(jobs)
	if got != 99 {
		t.Errorf("expected 99 when one job reports 100 but is not done, got %d", got)
	}
}

// TestComputeQueueProgress_HandlesOutOfRangeProgress clamps individual job
// progress before averaging so a corrupted value cannot distort the global bar.
func TestComputeQueueProgress_HandlesOutOfRangeProgress(t *testing.T) {
	jobs := []*JobState{
		{Song: "a", Status: "processing", Progress: 150},
		{Song: "b", Status: "waiting", Progress: -10},
	}
	got := computeQueueProgress(jobs)
	if got != 50 {
		t.Errorf("expected 50 after clamping (100+0)/2, got %d", got)
	}
}
