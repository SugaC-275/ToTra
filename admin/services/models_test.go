package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestModelService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewModelService(nil)
	assert.NotNil(t, svc)
}

func TestModelConfig_PriceFields(t *testing.T) {
	input := 5.00
	output := 15.00
	m := services.ModelConfig{
		ID:              "m1",
		Name:            "gpt-4o",
		Provider:        "openai",
		SCURate:         1.0,
		IsActive:        true,
		PricePerMInput:  &input,
		PricePerMOutput: &output,
	}
	assert.Equal(t, "gpt-4o", m.Name)
	assert.Equal(t, 5.00, *m.PricePerMInput)
	assert.Equal(t, 15.00, *m.PricePerMOutput)
}

func TestModelConfig_NilPrices(t *testing.T) {
	m := services.ModelConfig{ID: "m2", Name: "gpt-4o-mini"}
	assert.Nil(t, m.PricePerMInput)
	assert.Nil(t, m.PricePerMOutput)
}
