package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestMonthlySavingsReport_ServiceCreation(t *testing.T) {
	svc := services.NewCostSavingsReportService(nil)
	assert.NotNil(t, svc)
}

func TestMonthlySavingsReport_NewFields(t *testing.T) {
	usd := 42.50
	avg := 38.0
	r := services.MonthlySavingsReport{
		YearMonth:          "2026-05",
		RoutingEventCount:  100,
		TotalUSDSaved:      &usd,
		AvgComplexityScore: &avg,
		RoutingEventModels: []services.RoutedModelStat{},
		GeneratedAt:        "2026-05-15T00:00:00Z",
	}
	assert.Equal(t, 42.50, *r.TotalUSDSaved)
	assert.Equal(t, 38.0, *r.AvgComplexityScore)
}

func TestMonthlySavingsReport_NilPricesAllowed(t *testing.T) {
	r := services.MonthlySavingsReport{
		YearMonth:   "2026-05",
		GeneratedAt: "2026-05-15T00:00:00Z",
	}
	assert.Nil(t, r.TotalUSDSaved)
	assert.Nil(t, r.AvgComplexityScore)
}

func TestRoutedModelStat_Fields(t *testing.T) {
	s := services.RoutedModelStat{OriginalModel: "claude-opus-4-7", RoutedModel: "claude-sonnet-4-6", Count: 42}
	assert.Equal(t, "claude-opus-4-7", s.OriginalModel)
	assert.Equal(t, int64(42), s.Count)
}
