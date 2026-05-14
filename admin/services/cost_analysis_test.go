package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestClassifyModelTier_Cheap(t *testing.T) {
	assert.Equal(t, "cheap", services.ClassifyModelTier("gpt-4o-mini"))
}
func TestClassifyModelTier_Standard(t *testing.T) {
	assert.Equal(t, "standard", services.ClassifyModelTier("gpt-4o"))
}
func TestClassifyModelTier_Premium(t *testing.T) {
	assert.Equal(t, "premium", services.ClassifyModelTier("claude-opus-4-7"))
}
func TestClassifyModelTier_Unknown(t *testing.T) {
	assert.Equal(t, "standard", services.ClassifyModelTier("some-unknown-model"))
}
func TestIsOffHours_Weekday_Night(t *testing.T) {
	ts := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	assert.True(t, services.IsOffHours(ts))
}
func TestIsOffHours_Weekday_WorkHours(t *testing.T) {
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	assert.False(t, services.IsOffHours(ts))
}
func TestIsOffHours_Weekend(t *testing.T) {
	ts := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	assert.True(t, services.IsOffHours(ts))
}
func TestWasteSavingsEstimate_OverspecifiedModel(t *testing.T) {
	savings := services.WasteSavingsEstimate("gpt-4o", 1.50, "overspecified_model")
	assert.InDelta(t, 1.35, savings, 0.05)
}
func TestWasteSavingsEstimate_DuplicateRequest(t *testing.T) {
	savings := services.WasteSavingsEstimate("gpt-4o-mini", 0.10, "duplicate_request")
	assert.InDelta(t, 0.10, savings, 0.001)
}
