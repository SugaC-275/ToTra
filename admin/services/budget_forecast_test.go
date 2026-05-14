package services_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestLinearRegression_FlatLine(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{10, 10, 10, 10})
	assert.InDelta(t, 0.0, slope, 0.001)
	assert.InDelta(t, 10.0, intercept, 0.001)
}

func TestLinearRegression_Increasing(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{1, 3, 5, 7})
	assert.InDelta(t, 2.0, slope, 0.001)
	assert.InDelta(t, 1.0, intercept, 0.001)
}

func TestLinearRegression_SinglePoint(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{42})
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 42.0, intercept)
}

func TestLinearRegression_Empty(t *testing.T) {
	slope, intercept := services.LinearRegression([]float64{})
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 0.0, intercept)
}

func TestLinearRegression_NaN(t *testing.T) {
	slope, _ := services.LinearRegression([]float64{1, 3, 5, 7})
	assert.False(t, math.IsNaN(slope))
}
