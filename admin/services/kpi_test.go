package services_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestEfficiencyScore(t *testing.T) {
	// 10 output weight, 100 SCU → 10/log(101) ≈ 2.17
	score := services.ComputeEfficiencyScore(10, 100)
	assert.InDelta(t, 10.0/math.Log(101), score, 0.001)
}

func TestEfficiencyScoreZeroSCU(t *testing.T) {
	// Zero SCU → score 0 (no AI usage)
	score := services.ComputeEfficiencyScore(5, 0)
	assert.Equal(t, 0.0, score)
}

func TestEfficiencyScoreNoOutput(t *testing.T) {
	score := services.ComputeEfficiencyScore(0, 100)
	assert.Equal(t, 0.0, score)
}

func TestIsNewEmployee(t *testing.T) {
	assert.True(t, services.IsNewEmployee(45))   // 45 days old
	assert.False(t, services.IsNewEmployee(91))  // 91 days old → not new
	assert.True(t, services.IsNewEmployee(89))   // 89 days → still new
	assert.False(t, services.IsNewEmployee(90))  // exactly 90 → NOT new (< 90 rule)
}

func TestCohortGroup(t *testing.T) {
	assert.Equal(t, "cohort_2026-03", services.CohortGroup("2026-03"))
}
