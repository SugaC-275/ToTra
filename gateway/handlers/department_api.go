package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// DepartmentQuerier abstracts the DepartmentStore for handler use.
type DepartmentQuerier interface {
	Create(ctx context.Context, tenantID, name, slug string) (*storage.Department, error)
	List(ctx context.Context, tenantID string) ([]*storage.Department, error)
	Get(ctx context.Context, tenantID, id string) (*storage.Department, error)
	SetBudget(ctx context.Context, id string, budgetUSD *float64, rpm, tpm *int) error
	Delete(ctx context.Context, tenantID, id string) error
	GetSpend(ctx context.Context, deptID, periodKey string) (float64, error)
	ListUsers(ctx context.Context, tenantID, deptID string) ([]storage.DeptUser, error)
	AssignUser(ctx context.Context, tenantID, deptID, userID string) error
}

// RegisterDepartmentRoutes mounts all admin department endpoints under the given router.
func RegisterDepartmentRoutes(router fiber.Router, store DepartmentQuerier) {
	router.Get("/departments", listDepartments(store))
	router.Post("/departments", createDepartment(store))
	router.Get("/departments/:id", getDepartment(store))
	router.Put("/departments/:id/budget", setDepartmentBudget(store))
	router.Delete("/departments/:id", deleteDepartment(store))
	router.Get("/departments/:id/users", listDepartmentUsers(store))
	router.Put("/departments/:id/users/:user_id", assignUserToDepartment(store))
}

// requireAdmin returns the UserInfo from context or sends 401/403.
func requireAdmin(c *fiber.Ctx) (*middleware.UserInfo, error) {
	user, ok := c.Locals("user").(*middleware.UserInfo)
	if !ok || user == nil {
		return nil, c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	if user.Role != "admin" {
		return nil, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
	}
	return user, nil
}

func listDepartments(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		depts, err := store.List(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"departments": marshalDepartments(depts)})
	}
}

type createDeptPayload struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func createDepartment(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		var payload createDeptPayload
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if payload.Name == "" || payload.Slug == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name and slug are required"})
		}
		dept, err := store.Create(c.Context(), user.TenantID, payload.Name, payload.Slug)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(marshalDepartment(dept, 0))
	}
}

func getDepartment(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		dept, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if dept == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "department not found"})
		}
		periodKey := deptPeriodKey()
		spend, _ := store.GetSpend(c.Context(), dept.ID, periodKey)
		return c.JSON(marshalDepartment(dept, spend))
	}
}

type setDeptBudgetPayload struct {
	BudgetUSD *float64 `json:"budget_usd"`
	RPMLimit  *int     `json:"rpm_limit"`
	TPMLimit  *int     `json:"tpm_limit"`
}

func setDepartmentBudget(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		dept, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if dept == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "department not found"})
		}
		var payload setDeptBudgetPayload
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if err := store.SetBudget(c.Context(), dept.ID, payload.BudgetUSD, payload.RPMLimit, payload.TPMLimit); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func deleteDepartment(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		if err := store.Delete(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func listDepartmentUsers(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		users, err := store.ListUsers(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"users": users})
	}
}

func assignUserToDepartment(store DepartmentQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := requireAdmin(c)
		if err != nil {
			return err
		}
		if err := store.AssignUser(c.Context(), user.TenantID, c.Params("id"), c.Params("user_id")); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// deptPeriodKey returns the current monthly period key.
func deptPeriodKey() string {
	return time.Now().UTC().Format("2006-01")
}

// marshalDepartment converts a Department to a JSON-friendly map.
func marshalDepartment(d *storage.Department, spendUSD float64) fiber.Map {
	return fiber.Map{
		"id":         d.ID,
		"tenant_id":  d.TenantID,
		"name":       d.Name,
		"slug":       d.Slug,
		"budget_usd": d.BudgetUSD,
		"rpm_limit":  d.RPMLimit,
		"tpm_limit":  d.TPMLimit,
		"is_active":  d.IsActive,
		"created_at": d.CreatedAt,
		"spend_usd":  spendUSD,
	}
}

// marshalDepartments converts a slice without spend info (list endpoint).
func marshalDepartments(depts []*storage.Department) []fiber.Map {
	out := make([]fiber.Map, 0, len(depts))
	for _, d := range depts {
		out = append(out, marshalDepartment(d, 0))
	}
	return out
}
