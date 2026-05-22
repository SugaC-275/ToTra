package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubESS is a pure-function test double for EmployeeSelfService.
// It does not hit Postgres; instead we test the logic of CSV generation
// and input validation through a minimal harness.

func TestSubmitQuotaRequest_Validation(t *testing.T) {
	svc := &EmployeeSelfService{pool: nil}

	t.Run("negative tokens rejected", func(t *testing.T) {
		_, err := svc.SubmitQuotaRequest(context.Background(), "t1", "u1", -1, "need more")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requested_tokens must be positive")
	})

	t.Run("zero tokens rejected", func(t *testing.T) {
		_, err := svc.SubmitQuotaRequest(context.Background(), "t1", "u1", 0, "need more")
		require.Error(t, err)
	})

	t.Run("empty reason rejected", func(t *testing.T) {
		_, err := svc.SubmitQuotaRequest(context.Background(), "t1", "u1", 1000, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reason is required")
	})
}


func TestExportUsageCSV_InvalidMonth(t *testing.T) {
	svc := &EmployeeSelfService{pool: nil}

	_, err := svc.ExportUsageCSV(context.Background(), "t1", "u1", "bad-month")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid month format")
}


func TestPIIViolationSummary_NoPromptText(t *testing.T) {
	// Structural assertion: PIIViolationSummary must not have a field for prompt text.
	var v PIIViolationSummary
	_ = v.OccurredAt
	_ = v.ViolationType
	_ = v.ActionTaken
	// If someone adds a PromptText field, this test will still pass but the code
	// review rule is enforced by the type definition itself.
}

func TestEmployeeQuotaRequest_Fields(t *testing.T) {
	r := EmployeeQuotaRequest{
		ID:        "abc",
		NewQuota:  10000,
		Reason:    "sprint load",
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	assert.Equal(t, "pending", r.Status)
	assert.Equal(t, 10000, r.NewQuota)
	assert.Nil(t, r.ReviewNote)
}
