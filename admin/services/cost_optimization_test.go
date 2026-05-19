package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestOffHoursSuggestion_BelowThreshold(t *testing.T) {
	assert.Nil(t, services.OffHoursSuggestion(10, 50))
}

func TestOffHoursSuggestion_Medium(t *testing.T) {
	s := services.OffHoursSuggestion(20, 100)
	assert.NotNil(t, s)
	assert.Equal(t, "medium", s.Priority)
	assert.Equal(t, "scheduling", s.Category)
	assert.InDelta(t, 50.0, s.EstimatedSavingsUSD, 0.01)
}

func TestOffHoursSuggestion_High(t *testing.T) {
	s := services.OffHoursSuggestion(35, 200)
	assert.NotNil(t, s)
	assert.Equal(t, "high", s.Priority)
}

func TestTopSpenderSuggestion_BelowThreshold(t *testing.T) {
	assert.Nil(t, services.TopSpenderSuggestion(20))
}

func TestTopSpenderSuggestion_Medium(t *testing.T) {
	s := services.TopSpenderSuggestion(30)
	assert.NotNil(t, s)
	assert.Equal(t, "medium", s.Priority)
	assert.Equal(t, "quota", s.Category)
}

func TestTopSpenderSuggestion_High(t *testing.T) {
	s := services.TopSpenderSuggestion(45)
	assert.NotNil(t, s)
	assert.Equal(t, "high", s.Priority)
}

func TestRoutingSuggestion_NoData(t *testing.T) {
	assert.Nil(t, services.RoutingSuggestion(0, 0))
}

func TestRoutingSuggestion_HighComplexity_NoSuggestion(t *testing.T) {
	assert.Nil(t, services.RoutingSuggestion(60, 500))
}

func TestRoutingSuggestion_LowComplexity(t *testing.T) {
	s := services.RoutingSuggestion(30, 100)
	assert.NotNil(t, s)
	assert.Equal(t, "routing", s.Category)
	assert.Equal(t, "medium", s.Priority)
	assert.InDelta(t, 30.0, s.EstimatedSavingsUSD, 0.01)
}

func TestBudgetSuggestion_BelowThreshold(t *testing.T) {
	assert.Nil(t, services.BudgetSuggestion(50))
}

func TestBudgetSuggestion_Medium(t *testing.T) {
	s := services.BudgetSuggestion(80)
	assert.NotNil(t, s)
	assert.Equal(t, "medium", s.Priority)
	assert.Equal(t, "budget", s.Category)
}

func TestBudgetSuggestion_High(t *testing.T) {
	s := services.BudgetSuggestion(95)
	assert.NotNil(t, s)
	assert.Equal(t, "high", s.Priority)
}

func TestBenchmarkSuggestion_BelowThreshold(t *testing.T) {
	assert.Nil(t, services.BenchmarkSuggestion(50))
}

func TestBenchmarkSuggestion_Low(t *testing.T) {
	s := services.BenchmarkSuggestion(65)
	assert.NotNil(t, s)
	assert.Equal(t, "low", s.Priority)
	assert.Equal(t, "benchmark", s.Category)
}

func TestBenchmarkSuggestion_Medium(t *testing.T) {
	s := services.BenchmarkSuggestion(85)
	assert.NotNil(t, s)
	assert.Equal(t, "medium", s.Priority)
}

func TestSortSuggestions_HighFirst(t *testing.T) {
	input := []*services.OptSuggestion{
		{Priority: "low", EstimatedSavingsUSD: 100},
		{Priority: "high", EstimatedSavingsUSD: 10},
		{Priority: "medium", EstimatedSavingsUSD: 50},
	}
	sorted := services.SortSuggestions(input)
	assert.Equal(t, "high", sorted[0].Priority)
	assert.Equal(t, "medium", sorted[1].Priority)
	assert.Equal(t, "low", sorted[2].Priority)
}

func TestSortSuggestions_SamePrioritySortedBySavings(t *testing.T) {
	input := []*services.OptSuggestion{
		{Priority: "medium", EstimatedSavingsUSD: 20},
		{Priority: "medium", EstimatedSavingsUSD: 80},
		{Priority: "medium", EstimatedSavingsUSD: 50},
	}
	sorted := services.SortSuggestions(input)
	assert.Equal(t, 80.0, sorted[0].EstimatedSavingsUSD)
	assert.Equal(t, 50.0, sorted[1].EstimatedSavingsUSD)
	assert.Equal(t, 20.0, sorted[2].EstimatedSavingsUSD)
}
