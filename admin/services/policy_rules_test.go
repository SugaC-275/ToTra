package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestValidatePattern_Valid(t *testing.T) {
	assert.NoError(t, services.ValidatePattern(`\b\d{3}-\d{2}-\d{4}\b`))
	assert.NoError(t, services.ValidatePattern(`(?i)confidential`))
}

func TestValidatePattern_Invalid(t *testing.T) {
	assert.Error(t, services.ValidatePattern(`[invalid`))
	assert.Error(t, services.ValidatePattern(`(`))
}

func TestValidateAction_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateAction("block"))
	assert.NoError(t, services.ValidateAction("log"))
}

func TestValidateAction_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateAction("deny"))
	assert.Error(t, services.ValidateAction(""))
}
