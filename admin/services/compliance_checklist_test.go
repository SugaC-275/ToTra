package services_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestChecklistService_UpdateItem_InvalidStatus(t *testing.T) {
	svc := services.NewChecklistService(nil)
	err := svc.UpdateItem(context.Background(), "tid", "data_governance", "wrong_status", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestChecklistService_UpdateItem_InvalidKey(t *testing.T) {
	svc := services.NewChecklistService(nil)
	err := svc.UpdateItem(context.Background(), "tid", "nonexistent_key", "compliant", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid checklist item")
}
