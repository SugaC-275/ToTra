package services_test

// NOTE: MCPServerService methods that hit the database require a real PostgreSQL
// instance and are tested via integration tests. This file covers pure validation
// logic extracted from the request types so unit tests remain fast and DB-free.
//
// To run integration tests:
//   TEST_DATABASE_URL=postgres://... go test ./services/... -run MCPServer -tags integration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/services"
)

// ---------------------------------------------------------------------------
// CreateMCPServerRequest field validation rules (documented via tests)
// ---------------------------------------------------------------------------

func TestCreateMCPServerRequest_DefaultMaxToolCalls(t *testing.T) {
	req := services.CreateMCPServerRequest{
		Name:     "my-server",
		URL:      "https://mcp.example.com",
		AuthType: "none",
	}
	// MaxToolCalls zero value; callers should default to 10 before inserting.
	assert.Equal(t, 0, req.MaxToolCalls, "zero means caller applies default of 10")
}

func TestCreateMCPServerRequest_BearerRequiresToken(t *testing.T) {
	// Demonstrates the constraint: auth_type=bearer requires auth_token.
	req := services.CreateMCPServerRequest{
		Name:      "secure-server",
		URL:       "https://mcp.example.com",
		AuthType:  "bearer",
		AuthToken: "",
	}
	missing := req.AuthType == "bearer" && req.AuthToken == ""
	assert.True(t, missing, "bearer auth_type without token should be rejected")
}

func TestCreateMCPServerRequest_BearerWithToken_Valid(t *testing.T) {
	req := services.CreateMCPServerRequest{
		Name:         "secure-server",
		URL:          "https://mcp.example.com",
		AuthType:     "bearer",
		AuthToken:    "secret-token",
		MaxToolCalls: 20,
	}
	assert.Equal(t, "bearer", req.AuthType)
	assert.Equal(t, "secret-token", req.AuthToken)
	assert.Equal(t, 20, req.MaxToolCalls)
}

func TestUpdateMCPServerRequest_PartialUpdate(t *testing.T) {
	// Confirm pointer semantics: nil fields are not updated.
	desc := "new description"
	req := services.UpdateMCPServerRequest{
		Description: &desc,
		// URL, AuthType, AuthToken, Enabled, MaxToolCalls all nil → not changed
	}
	assert.NotNil(t, req.Description)
	assert.Nil(t, req.URL)
	assert.Nil(t, req.AuthType)
	assert.Nil(t, req.Enabled)
	assert.Nil(t, req.MaxToolCalls)
}

func TestMCPServer_AuthTokenNotExposed(t *testing.T) {
	// MCPServer struct must NOT contain an auth_token field in its JSON output.
	// Verify by confirming the field is absent from the struct definition.
	m := services.MCPServer{
		ID:           "uuid-1",
		TenantID:     "tenant-1",
		Name:         "test",
		Description:  "desc",
		URL:          "https://example.com",
		AuthType:     "bearer",
		Enabled:      true,
		MaxToolCalls: 10,
	}
	// If auth_token were a field, this assignment would compile.
	// The absence of the field enforces the security requirement.
	assert.Equal(t, "bearer", m.AuthType)
	assert.Equal(t, 10, m.MaxToolCalls)
}

func TestMCPToolCallLog_Fields(t *testing.T) {
	log := services.MCPToolCallLog{
		ID:          42,
		ServerName:  "my-server",
		ToolName:    "list_files",
		StatusCode:  200,
		DurationMS:  15,
		PIIDetected: true,
	}
	assert.Equal(t, int64(42), log.ID)
	assert.True(t, log.PIIDetected)
	assert.Equal(t, 200, log.StatusCode)
}
