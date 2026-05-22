package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/services"
)

func TestRBACService_GetUserRoles(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	now := time.Now()
	rows := pgxmock.NewRows([]string{"id", "user_id", "role", "department", "granted_by", "created_at"}).
		AddRow("role-1", "user-1", "auditor", "", "admin-1", now).
		AddRow("role-2", "user-1", "compliance_officer", "legal", "", now)

	mock.ExpectQuery(`SELECT`).
		WithArgs("tenant-1", "user-1").
		WillReturnRows(rows)

	svc := services.NewRBACService(mock)
	roles, err := svc.GetUserRoles(context.Background(), "tenant-1", "user-1")
	require.NoError(t, err)
	require.Len(t, roles, 2)
	assert.Equal(t, services.RoleAuditor, roles[0].Role)
	assert.Equal(t, "", roles[0].Department)
	assert.Equal(t, services.RoleComplianceOfficer, roles[1].Role)
	assert.Equal(t, "legal", roles[1].Department)
}

func TestRBACService_GetUserRoles_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "user_id", "role", "department", "granted_by", "created_at"})
	mock.ExpectQuery(`SELECT`).
		WithArgs("tenant-1", "user-99").
		WillReturnRows(rows)

	svc := services.NewRBACService(mock)
	roles, err := svc.GetUserRoles(context.Background(), "tenant-1", "user-99")
	require.NoError(t, err)
	assert.Empty(t, roles)
}

func TestRBACService_AssignRole(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	engineering := "engineering"
	adminID := "admin-1"
	mock.ExpectExec(`INSERT INTO user_roles`).
		WithArgs(pgxmock.AnyArg(), "tenant-1", "user-1", "dept_admin", &engineering, &adminID).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	svc := services.NewRBACService(mock)
	err = svc.AssignRole(context.Background(), "tenant-1", "user-1", services.RoleDeptAdmin, "engineering", "admin-1")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRBACService_AssignRole_OrgWide(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	adminID := "admin-1"
	mock.ExpectExec(`INSERT INTO user_roles`).
		WithArgs(pgxmock.AnyArg(), "tenant-1", "user-2", "auditor", (*string)(nil), &adminID).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	svc := services.NewRBACService(mock)
	err = svc.AssignRole(context.Background(), "tenant-1", "user-2", services.RoleAuditor, "", "admin-1")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRBACService_RevokeRole_OrgWide(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM user_roles`).
		WithArgs("tenant-1", "user-1", "auditor").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	svc := services.NewRBACService(mock)
	err = svc.RevokeRole(context.Background(), "tenant-1", "user-1", services.RoleAuditor, "")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRBACService_RevokeRole_Department(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM user_roles`).
		WithArgs("tenant-1", "user-1", "dept_admin", "engineering").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	svc := services.NewRBACService(mock)
	err = svc.RevokeRole(context.Background(), "tenant-1", "user-1", services.RoleDeptAdmin, "engineering")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRBACService_HasRole_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(`SELECT COUNT`).
		WithArgs("tenant-1", "user-1", "compliance_officer").
		WillReturnRows(rows)

	svc := services.NewRBACService(mock)
	has, err := svc.HasRole(context.Background(), "tenant-1", "user-1", services.RoleComplianceOfficer)
	require.NoError(t, err)
	assert.True(t, has)
}

func TestRBACService_HasRole_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery(`SELECT COUNT`).
		WithArgs("tenant-1", "user-1", "admin").
		WillReturnRows(rows)

	svc := services.NewRBACService(mock)
	has, err := svc.HasRole(context.Background(), "tenant-1", "user-1", services.RoleAdmin)
	require.NoError(t, err)
	assert.False(t, has)
}
