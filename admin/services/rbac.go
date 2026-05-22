package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Role represents a fine-grained RBAC role.
type Role string

const (
	RoleAdmin             Role = "admin"
	RoleDeptAdmin         Role = "dept_admin"
	RoleComplianceOfficer Role = "compliance_officer"
	RoleAuditor           Role = "auditor"
	RoleUser              Role = "user"
)

// UserRole is a role assignment for a user within a tenant.
type UserRole struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Role       Role      `json:"role"`
	Department string    `json:"department,omitempty"` // empty = org-wide
	GrantedBy  string    `json:"granted_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// RBACServiceIface is the interface required by middleware and API handlers.
type RBACServiceIface interface {
	GetUserRoles(ctx context.Context, tenantID, userID string) ([]UserRole, error)
	AssignRole(ctx context.Context, tenantID, userID string, role Role, department string, grantedBy string) error
	RevokeRole(ctx context.Context, tenantID, userID string, role Role, department string) error
	HasRole(ctx context.Context, tenantID, userID string, role Role) (bool, error)
}

// RBACService reads/writes the user_roles table.
type RBACService struct {
	db dbQuerier
}

// NewRBACService creates a production RBACService backed by a pgxpool.Pool.
func NewRBACService(db dbQuerier) *RBACService {
	return &RBACService{db: db}
}

// GetUserRoles returns all role assignments for a user within a tenant.
func (s *RBACService) GetUserRoles(ctx context.Context, tenantID, userID string) ([]UserRole, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, role, COALESCE(department,''), COALESCE(granted_by::text,''), created_at
		 FROM user_roles
		 WHERE tenant_id = $1 AND user_id = $2
		 ORDER BY created_at`,
		tenantID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	defer rows.Close()

	var out []UserRole
	for rows.Next() {
		var r UserRole
		var roleStr string
		if err := rows.Scan(&r.ID, &r.UserID, &roleStr, &r.Department, &r.GrantedBy, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		r.Role = Role(roleStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AssignRole grants a role to a user within a tenant. Department may be empty for org-wide scope.
func (s *RBACService) AssignRole(ctx context.Context, tenantID, userID string, role Role, department string, grantedBy string) error {
	id := uuid.New().String()
	var deptParam, grantedByParam *string
	if department != "" {
		deptParam = &department
	}
	if grantedBy != "" {
		grantedByParam = &grantedBy
	}
	_, err := s.db.Exec(ctx,
		`INSERT INTO user_roles (id, tenant_id, user_id, role, department, granted_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, user_id, role, department) DO NOTHING`,
		id, tenantID, userID, string(role), deptParam, grantedByParam,
	)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

// RevokeRole removes a role from a user. Department must match the original grant.
func (s *RBACService) RevokeRole(ctx context.Context, tenantID, userID string, role Role, department string) error {
	var deptClause string
	var args []any
	args = append(args, tenantID, userID, string(role))
	if department == "" {
		deptClause = "department IS NULL"
	} else {
		deptClause = "department = $4"
		args = append(args, department)
	}
	_, err := s.db.Exec(ctx,
		`DELETE FROM user_roles
		 WHERE tenant_id = $1 AND user_id = $2 AND role = $3 AND `+deptClause,
		args...,
	)
	if err != nil {
		return fmt.Errorf("revoke role: %w", err)
	}
	return nil
}

// HasRole reports whether the user holds the specified role in the tenant (any department scope).
func (s *RBACService) HasRole(ctx context.Context, tenantID, userID string, role Role) (bool, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_roles
		 WHERE tenant_id = $1 AND user_id = $2 AND role = $3`,
		tenantID, userID, string(role),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("has role: %w", err)
	}
	return count > 0, nil
}
