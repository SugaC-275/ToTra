package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestIsAnomaly_BelowMinThreshold(t *testing.T) {
	assert.False(t, services.IsAnomaly(3, 0))
	assert.False(t, services.IsAnomaly(4, 0))
}

func TestIsAnomaly_FirstTimeSpike(t *testing.T) {
	assert.True(t, services.IsAnomaly(5, 0))
	assert.True(t, services.IsAnomaly(10, 0.5))
}

func TestIsAnomaly_SpikeOverBaseline(t *testing.T) {
	assert.True(t, services.IsAnomaly(15, 4))
	assert.False(t, services.IsAnomaly(10, 4))
}

func TestIsAnomaly_NormalRate(t *testing.T) {
	assert.False(t, services.IsAnomaly(6, 3))
}

func TestAnomalySeverity_High_FirstTime(t *testing.T) {
	assert.Equal(t, "high", services.AnomalySeverity(5, 0))
}

func TestAnomalySeverity_High_LargeSpike(t *testing.T) {
	assert.Equal(t, "high", services.AnomalySeverity(25, 4))
}

func TestAnomalySeverity_Medium(t *testing.T) {
	assert.Equal(t, "medium", services.AnomalySeverity(15, 4))
}
