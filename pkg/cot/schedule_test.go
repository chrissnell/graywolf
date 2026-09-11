package cot

import (
	"testing"
	"time"
)

// TestComputeOffsets_MatchesConfirmedExample locks in the schedule the
// user confirmed: 4 transmits, a 300s second-delay, decay factor 2
// yields offsets 0s / 300s / 600s / 1200s (00:00 / 00:05 / 00:10 /
// 00:20 from the first send).
func TestComputeOffsets_MatchesConfirmedExample(t *testing.T) {
	got := ComputeOffsets(4, 300*time.Second, 2)
	want := []time.Duration{0, 300 * time.Second, 600 * time.Second, 1200 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("offsets[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestComputeOffsets_SingleTransmit(t *testing.T) {
	got := ComputeOffsets(1, 300*time.Second, 2)
	want := []time.Duration{0}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("ComputeOffsets(1, ...) = %v, want %v", got, want)
	}
}

func TestComputeOffsets_DecayOne_ConstantSpacing(t *testing.T) {
	got := ComputeOffsets(4, 300*time.Second, 1)
	want := []time.Duration{0, 300 * time.Second, 300 * time.Second, 300 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("offsets[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestComputeOffsets_ZeroOrNegative_ReturnsNil(t *testing.T) {
	if got := ComputeOffsets(0, 300*time.Second, 2); got != nil {
		t.Errorf("ComputeOffsets(0, ...) = %v, want nil", got)
	}
	if got := ComputeOffsets(-1, 300*time.Second, 2); got != nil {
		t.Errorf("ComputeOffsets(-1, ...) = %v, want nil", got)
	}
}
