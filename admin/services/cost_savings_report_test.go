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

func TestRoutedModelStat_Fields(t *testing.T) {
	s := services.RoutedModelStat{OriginalModel: "claude-opus-4-7", RoutedModel: "claude-sonnet-4-6", Count: 42}
	assert.Equal(t, "claude-opus-4-7", s.OriginalModel)
	assert.Equal(t, int64(42), s.Count)
}
