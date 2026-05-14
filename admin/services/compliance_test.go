package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestComputeRiskScore_NoViolations(t *testing.T) {
	assert.Equal(t, 0, services.ComputeRiskScore(0, 0))
}
func TestComputeRiskScore_LowRisk(t *testing.T) {
	score := services.ComputeRiskScore(1, 1000)
	assert.True(t, score < 20, "got %d", score)
}
func TestComputeRiskScore_HighRisk(t *testing.T) {
	score := services.ComputeRiskScore(50, 100)
	assert.True(t, score >= 80, "got %d", score)
}
func TestComputeRiskScore_CapsAt100(t *testing.T) {
	assert.Equal(t, 100, services.ComputeRiskScore(9999, 1))
}
func TestRiskLevel_Labels(t *testing.T) {
	assert.Equal(t, "low", services.RiskLevel(0))
	assert.Equal(t, "low", services.RiskLevel(29))
	assert.Equal(t, "medium", services.RiskLevel(30))
	assert.Equal(t, "medium", services.RiskLevel(69))
	assert.Equal(t, "high", services.RiskLevel(70))
	assert.Equal(t, "critical", services.RiskLevel(90))
}
