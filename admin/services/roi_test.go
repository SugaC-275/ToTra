package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

func TestComputeROI(t *testing.T) {
	assert.InDelta(t, 2.5, services.ComputeROI(5.0, 2.0), 0.001)
}

func TestComputeROI_ZeroUSD(t *testing.T) {
	assert.Equal(t, 0.0, services.ComputeROI(5.0, 0.0))
}

func TestComputePercentile_Top10(t *testing.T) {
	pct := services.ComputePercentile(0.50, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 90, pct)
}

func TestComputePercentile_Between50And75(t *testing.T) {
	pct := services.ComputePercentile(0.20, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 50, pct)
}

func TestComputePercentile_Bottom(t *testing.T) {
	pct := services.ComputePercentile(0.02, 0.05, 0.12, 0.25, 0.45)
	assert.Equal(t, 10, pct)
}

func TestBenchmarkLabel(t *testing.T) {
	assert.Equal(t, "Top 10%", services.BenchmarkLabel(90))
	assert.Equal(t, "Top 25%", services.BenchmarkLabel(75))
	assert.Equal(t, "Top 50%", services.BenchmarkLabel(50))
	assert.Equal(t, "Bottom 50%", services.BenchmarkLabel(25))
	assert.Equal(t, "Bottom 25%", services.BenchmarkLabel(10))
}
