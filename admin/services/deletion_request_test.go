package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestValidateDeletionStatus_Valid(t *testing.T) {
	assert.NoError(t, services.ValidateDeletionStatus("pending"))
	assert.NoError(t, services.ValidateDeletionStatus("approved"))
	assert.NoError(t, services.ValidateDeletionStatus("rejected"))
}

func TestValidateDeletionStatus_Invalid(t *testing.T) {
	assert.Error(t, services.ValidateDeletionStatus("deleted"))
	assert.Error(t, services.ValidateDeletionStatus(""))
	assert.Error(t, services.ValidateDeletionStatus("PENDING"))
}
