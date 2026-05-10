package services_test

import (
	"math"
	"testing"

	"github.com/yourorg/totra/admin/services"
)

func TestMedian(t *testing.T) {
	cases := []struct {
		vals []float64
		want float64
	}{
		{[]float64{}, 0},
		{[]float64{3}, 3},
		{[]float64{1, 2, 3}, 2},
		{[]float64{1, 3}, 2},
		{[]float64{5, 1, 3}, 3},
	}
	for _, tc := range cases {
		got := services.Median(tc.vals)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Median(%v) = %v, want %v", tc.vals, got, tc.want)
		}
	}
}

func TestZScoreNormalize(t *testing.T) {
	vals := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	zs := services.ZScoreNormalize(vals)
	if len(zs) != len(vals) {
		t.Fatalf("length mismatch")
	}
	var sum float64
	for _, z := range zs {
		sum += z
	}
	if math.Abs(sum) > 1e-9 {
		t.Errorf("Z-scores should sum to ~0, got %v", sum)
	}
}

func TestZToScore(t *testing.T) {
	if services.ZToScore(0) != 50 {
		t.Errorf("Z=0 should map to 50")
	}
	if services.ZToScore(3) != 100 {
		t.Errorf("Z=3 should map to 100")
	}
	if services.ZToScore(-3) != 0 {
		t.Errorf("Z=-3 should map to 0")
	}
	if services.ZToScore(99) != 100 {
		t.Errorf("Z>3 should clamp to 100")
	}
}

func TestWorkingDaysInMonth(t *testing.T) {
	// May 2026: 31 days, starts Friday. Weekdays = 21.
	got := services.WorkingDaysInMonth("2026-05")
	if got != 21 {
		t.Errorf("WorkingDaysInMonth(2026-05) = %d, want 21", got)
	}
}

func TestComputeAIQScore(t *testing.T) {
	// Two users in same peer group; user A is clearly better
	metrics := []*services.RawAIQMetrics{
		{UserID: "u1", OutputDensity: 2.0, UsageConsistency: 0.8, TaskDepth: 4.0, CostEfficiency: 100, ActiveDays: 15},
		{UserID: "u2", OutputDensity: 0.5, UsageConsistency: 0.2, TaskDepth: 1.0, CostEfficiency: 20,  ActiveDays: 5},
	}
	scores := services.ComputeAIQScores(metrics)
	if scores["u1"] <= scores["u2"] {
		t.Errorf("u1 (better metrics) should score higher than u2, got u1=%v u2=%v", scores["u1"], scores["u2"])
	}
}
