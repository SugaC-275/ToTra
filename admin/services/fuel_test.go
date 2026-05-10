package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestRewardTier(t *testing.T) {
	settings := services.FuelSettings{
		Top10PctBonus: 0.20,
		Top25PctBonus: 0.10,
		Top50PctBonus: 0.05,
	}
	// rank 1/10 = 10th percentile → top 10%
	assert.Equal(t, "top_10pct", services.RewardTier(1, 10, settings))
	// rank 2/10 = 20th percentile → top 25%
	assert.Equal(t, "top_25pct", services.RewardTier(2, 10, settings))
	// rank 4/10 = 40th percentile → top 50%
	assert.Equal(t, "top_50pct", services.RewardTier(4, 10, settings))
	// rank 6/10 = 60th percentile → no reward
	assert.Equal(t, "", services.RewardTier(6, 10, settings))
	// peerCount=0 edge case
	assert.Equal(t, "", services.RewardTier(1, 0, settings))
}

func TestAwardedSCU(t *testing.T) {
	assert.Equal(t, 2000, services.AwardedSCU(10000, 0.20))
	assert.Equal(t, 1000, services.AwardedSCU(10000, 0.10))
	assert.Equal(t, 500, services.AwardedSCU(10000, 0.05))
	assert.Equal(t, 0, services.AwardedSCU(10000, 0.0))
}
