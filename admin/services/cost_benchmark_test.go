package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestCostPercentileRank_Empty(t *testing.T) {
	assert.Equal(t, 0.0, services.CostPercentileRank(10, []float64{}))
}

func TestCostPercentileRank_AllBelow(t *testing.T) {
	assert.InDelta(t, 100.0, services.CostPercentileRank(10, []float64{1, 2, 3, 4}), 0.001)
}

func TestCostPercentileRank_NoneBelow(t *testing.T) {
	assert.InDelta(t, 0.0, services.CostPercentileRank(1, []float64{1, 2, 3, 4}), 0.001)
}

func TestCostPercentileRank_Middle(t *testing.T) {
	assert.InDelta(t, 50.0, services.CostPercentileRank(3, []float64{1, 2, 3, 4}), 0.001)
}
