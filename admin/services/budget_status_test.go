package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestBudgetPercentage_Normal(t *testing.T) {
	assert.InDelta(t, 50.0, services.BudgetPercentage(50, 100), 0.001)
}

func TestBudgetPercentage_Zero(t *testing.T) {
	assert.Equal(t, 0.0, services.BudgetPercentage(0, 100))
}

func TestBudgetPercentage_NoBudget(t *testing.T) {
	assert.Equal(t, 0.0, services.BudgetPercentage(50, 0))
}

func TestBudgetPercentage_Over(t *testing.T) {
	assert.InDelta(t, 120.0, services.BudgetPercentage(120, 100), 0.001)
}
