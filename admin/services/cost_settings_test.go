package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestValidateWorkHours_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateWorkHours(9, 18))
	assert.NoError(t, services.ValidateWorkHours(0, 23))
}

func TestValidateWorkHours_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateWorkHours(18, 9))
	assert.Error(t, services.ValidateWorkHours(-1, 10))
	assert.Error(t, services.ValidateWorkHours(0, 25))
}
