package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestPercentileRank_Middle(t *testing.T) {
	rank := services.PercentileRank(5.0, []float64{1.0, 3.0, 5.0, 7.0, 9.0})
	assert.InDelta(t, 40.0, rank, 1.0)
}

func TestPercentileRank_Lowest(t *testing.T) {
	rank := services.PercentileRank(1.0, []float64{1.0, 5.0, 10.0})
	assert.InDelta(t, 0.0, rank, 0.1)
}

func TestPercentileRank_Highest(t *testing.T) {
	rank := services.PercentileRank(10.0, []float64{1.0, 5.0, 10.0})
	assert.InDelta(t, 66.7, rank, 1.0)
}

func TestPercentileRank_EmptySlice(t *testing.T) {
	rank := services.PercentileRank(5.0, []float64{})
	assert.Equal(t, 0.0, rank)
}
