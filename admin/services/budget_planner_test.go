package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestProjectedMonthlySpend_ZeroBase(t *testing.T) {
	assert.Equal(t, 0.0, services.ProjectedMonthlySpend(0, 5, 3))
}

func TestProjectedMonthlySpend_NoGrowth(t *testing.T) {
	assert.InDelta(t, 100.0, services.ProjectedMonthlySpend(100, 0, 6), 0.001)
}

func TestProjectedMonthlySpend_PositiveGrowth(t *testing.T) {
	// 100 * (1.10)^1 = 110
	result := services.ProjectedMonthlySpend(100, 10, 1)
	assert.InDelta(t, 110.0, result, 0.001)
}

func TestProjectedMonthlySpend_NegativeGrowth(t *testing.T) {
	// 100 * (0.90)^1 = 90
	result := services.ProjectedMonthlySpend(100, -10, 1)
	assert.InDelta(t, 90.0, result, 0.001)
}

func TestAnnualisedGrowthRate_ZeroMean(t *testing.T) {
	assert.Equal(t, 0.0, services.AnnualisedGrowthRate(5, 0))
}

func TestAnnualisedGrowthRate_Flat(t *testing.T) {
	assert.InDelta(t, 0.0, services.AnnualisedGrowthRate(0, 100), 0.001)
}

func TestAnnualisedGrowthRate_Growing(t *testing.T) {
	// slope=10, mean=100 → 10%/month
	assert.InDelta(t, 10.0, services.AnnualisedGrowthRate(10, 100), 0.001)
}

func TestAnnualisedGrowthRate_Shrinking(t *testing.T) {
	assert.InDelta(t, -5.0, services.AnnualisedGrowthRate(-5, 100), 0.001)
}
