package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/services"
)

func TestJWTService_IssueAndVerify(t *testing.T) {
	svc := services.NewJWTService("test-secret", 1*time.Hour)

	token, err := svc.Issue("user-1", "tenant-1", "admin")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := svc.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "tenant-1", claims.TenantID)
	assert.Equal(t, "admin", claims.Role)
}

func TestJWTService_ExpiredToken(t *testing.T) {
	svc := services.NewJWTService("test-secret", -1*time.Second)
	token, _ := svc.Issue("user-1", "tenant-1", "admin")
	_, err := svc.Verify(token)
	assert.Error(t, err)
}
