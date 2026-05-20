package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestProviderSharePercent_Zero(t *testing.T) {
	assert.Equal(t, 0.0, services.ProviderSharePercent(100, 0))
}

func TestProviderSharePercent_Half(t *testing.T) {
	assert.InDelta(t, 50.0, services.ProviderSharePercent(50, 100), 0.001)
}

func TestProviderSharePercent_Full(t *testing.T) {
	assert.InDelta(t, 100.0, services.ProviderSharePercent(200, 200), 0.001)
}

func TestMoMGrowth_NoPrior(t *testing.T) {
	assert.Equal(t, 0.0, services.MoMGrowth(100, 0))
}

func TestMoMGrowth_Increase(t *testing.T) {
	assert.InDelta(t, 50.0, services.MoMGrowth(150, 100), 0.001)
}

func TestMoMGrowth_Decrease(t *testing.T) {
	assert.InDelta(t, -25.0, services.MoMGrowth(75, 100), 0.001)
}

func TestMoMGrowth_Flat(t *testing.T) {
	assert.InDelta(t, 0.0, services.MoMGrowth(100, 100), 0.001)
}
