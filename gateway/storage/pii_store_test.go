package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/storage"
)

func TestBuildViolationRecord_AllFields(t *testing.T) {
	r := storage.BuildViolationRecord("tenant-1", "user-1", "china_phone", "blocked", "/v1/chat/completions")
	assert.Equal(t, "tenant-1", r.TenantID)
	assert.Equal(t, "user-1", r.UserID)
	assert.Equal(t, "china_phone", r.PIIType)
	assert.Equal(t, "blocked", r.Action)
	assert.Equal(t, "/v1/chat/completions", r.RequestPath)
}

func TestBuildViolationRecord_EmptyUser(t *testing.T) {
	r := storage.BuildViolationRecord("tenant-1", "", "credit_card", "blocked", "/v1/messages")
	assert.Equal(t, "", r.UserID)
	assert.Equal(t, "credit_card", r.PIIType)
}
