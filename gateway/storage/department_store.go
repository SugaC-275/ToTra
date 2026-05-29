package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Department represents a sub-tenant grouping within an organization (tenant).
type Department struct {
	ID        string
	TenantID  string
	Name      string
	Slug      string
	BudgetUSD *float64
	RPMLimit  *int
	TPMLimit  *int
	IsActive  bool
	CreatedAt time.Time
}

// DepartmentStore provides CRUD and spend-query operations for departments.
type DepartmentStore struct{ pool *pgxpool.Pool }

// NewDepartmentStore constructs a DepartmentStore backed by the given pool.
func NewDepartmentStore(pool *pgxpool.Pool) *DepartmentStore {
	return &DepartmentStore{pool: pool}
}

// Create inserts a new active department for the given tenant.
func (s *DepartmentStore) Create(ctx context.Context, tenantID, name, slug string) (*Department, error) {
	dept := &Department{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO departments (tenant_id, name, slug)
		 VALUES ($1, $2, $3)
		 RETURNING id, tenant_id, name, slug, budget_usd, rpm_limit, tpm_limit, is_active, created_at`,
		tenantID, name, slug,
	).Scan(&dept.ID, &dept.TenantID, &dept.Name, &dept.Slug,
		&dept.BudgetUSD, &dept.RPMLimit, &dept.TPMLimit, &dept.IsActive, &dept.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("department create: %w", err)
	}
	return dept, nil
}

// List returns all departments for the given tenant, ordered by name.
func (s *DepartmentStore) List(ctx context.Context, tenantID string) ([]*Department, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, slug, budget_usd, rpm_limit, tpm_limit, is_active, created_at
		 FROM departments WHERE tenant_id=$1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("department list: %w", err)
	}
	defer rows.Close()

	var depts []*Department
	for rows.Next() {
		d := &Department{}
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Name, &d.Slug,
			&d.BudgetUSD, &d.RPMLimit, &d.TPMLimit, &d.IsActive, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("department list scan: %w", err)
		}
		depts = append(depts, d)
	}
	return depts, rows.Err()
}

// Get fetches a single department by (tenantID, id), enforcing tenant isolation.
func (s *DepartmentStore) Get(ctx context.Context, tenantID, id string) (*Department, error) {
	d := &Department{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, slug, budget_usd, rpm_limit, tpm_limit, is_active, created_at
		 FROM departments WHERE tenant_id=$1 AND id=$2`,
		tenantID, id,
	).Scan(&d.ID, &d.TenantID, &d.Name, &d.Slug,
		&d.BudgetUSD, &d.RPMLimit, &d.TPMLimit, &d.IsActive, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("department get: %w", err)
	}
	return d, nil
}

// SetBudget updates the budget and rate limits for a department by id.
func (s *DepartmentStore) SetBudget(ctx context.Context, id string, budgetUSD *float64, rpm, tpm *int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE departments SET budget_usd=$1, rpm_limit=$2, tpm_limit=$3 WHERE id=$4`,
		budgetUSD, rpm, tpm, id,
	)
	if err != nil {
		return fmt.Errorf("department set budget: %w", err)
	}
	return nil
}

// Delete removes a department, enforcing tenant isolation.
func (s *DepartmentStore) Delete(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM departments WHERE tenant_id=$1 AND id=$2`,
		tenantID, id,
	)
	if err != nil {
		return fmt.Errorf("department delete: %w", err)
	}
	return nil
}

// GetSpend returns total USD spent by users in the department for the given period key
// (e.g. "2026-05" for monthly, "2026-05-25" for daily, "total" for all time).
// It sums from budget_consumption_log which is incremented by the per-key budget middleware.
func (s *DepartmentStore) GetSpend(ctx context.Context, deptID, periodKey string) (float64, error) {
	var spent float64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(bcl.usd_spent), 0)
		 FROM budget_consumption_log bcl
		 JOIN users u ON u.id = bcl.user_id
		 WHERE u.department_id = $1
		   AND ($2 = 'total' OR bcl.period_key = $2)`,
		deptID, periodKey,
	).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("department get spend: %w", err)
	}
	return spent, nil
}

// GetDeptBudget returns the budget_usd for a department by id.
// Returns (nil, nil) when the department has no budget set.
func (s *DepartmentStore) GetDeptBudget(ctx context.Context, deptID string) (*float64, error) {
	var budget *float64
	err := s.pool.QueryRow(ctx,
		`SELECT budget_usd FROM departments WHERE id=$1 AND is_active=true`,
		deptID,
	).Scan(&budget)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("dept budget lookup: %w", err)
	}
	return budget, nil
}

// ListUsers returns users assigned to a specific department, enforcing tenant isolation.
func (s *DepartmentStore) ListUsers(ctx context.Context, tenantID, deptID string) ([]DeptUser, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.name, u.email, u.role, u.is_active
		 FROM users u
		 JOIN departments d ON d.id = u.department_id
		 WHERE d.tenant_id=$1 AND u.department_id=$2
		 ORDER BY u.name`,
		tenantID, deptID,
	)
	if err != nil {
		return nil, fmt.Errorf("dept list users: %w", err)
	}
	defer rows.Close()

	var users []DeptUser
	for rows.Next() {
		var u DeptUser
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.IsActive); err != nil {
			return nil, fmt.Errorf("dept list users scan: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// AssignUser sets (or clears) the department_id for a user, enforcing tenant isolation.
func (s *DepartmentStore) AssignUser(ctx context.Context, tenantID, deptID, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET department_id=$1 WHERE id=$2 AND tenant_id=$3`,
		deptID, userID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("dept assign user: %w", err)
	}
	return nil
}

// DeptUser is a lightweight user record returned by ListUsers.
type DeptUser struct {
	ID       string
	Name     string
	Email    string
	Role     string
	IsActive bool
}
