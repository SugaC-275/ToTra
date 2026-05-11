package services_test

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/services"
)

func TestAgentService_GetAgentSessions_ReturnsList(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{
		"id", "tenant_id", "user_id", "user_name",
		"conversation_id", "loop_count", "tool_call_count", "is_dead_loop", "last_seen_at", "created_at",
	}).AddRow(
		"sess-1", "tenant-1", "user-1", "Alice",
		"conv-1", 5, 3, false, "2026-05-10T12:00:00Z", "2026-05-10T10:00:00Z",
	)

	mock.ExpectQuery(`SELECT`).
		WithArgs("tenant-1", "2026-05").
		WillReturnRows(rows)

	svc := services.NewAgentService(mock)
	sessions, err := svc.GetAgentSessions(context.Background(), "tenant-1", "2026-05")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "Alice", sessions[0].UserName)
	assert.Equal(t, 5, sessions[0].LoopCount)
	assert.False(t, sessions[0].IsDeadLoop)
}

func TestAgentService_GetMyAgentSessions_ReturnsList(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{
		"id", "tenant_id", "user_id", "user_name",
		"conversation_id", "loop_count", "tool_call_count", "is_dead_loop", "last_seen_at", "created_at",
	}).AddRow(
		"sess-2", "tenant-1", "user-2", "Bob",
		"conv-2", 15, 10, true, "2026-05-11T08:00:00Z", "2026-05-11T07:00:00Z",
	)

	mock.ExpectQuery(`SELECT`).
		WithArgs("user-2", "2026-05").
		WillReturnRows(rows)

	svc := services.NewAgentService(mock)
	sessions, err := svc.GetMyAgentSessions(context.Background(), "user-2", "2026-05")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.True(t, sessions[0].IsDeadLoop)
	assert.Equal(t, 15, sessions[0].LoopCount)
}
