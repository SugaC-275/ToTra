package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// RegisterEmployeeSelfServiceRoutes registers employee self-service endpoints.
// All routes require an authenticated JWT; user_id always comes from claims.
func RegisterEmployeeSelfServiceRoutes(app fiber.Router, svc *services.EmployeeSelfService, quotaSvc services.QuotaServiceInterface) {
	// Employee endpoints — any authenticated user sees only their own data
	app.Get("/api/employee/pii-violations", getMyPIIViolations(svc))
	app.Post("/api/employee/quota-requests", submitQuotaRequest(svc))
	app.Get("/api/employee/quota-requests", getMyQuotaRequests(svc))
	app.Get("/api/employee/usage-export", exportMyUsageCSV(svc))

	// Admin quota-request management
	app.Get("/api/admin/quota-requests", listAllPendingQuotaRequests(quotaSvc))
	app.Put("/api/admin/quota-requests/:id", reviewQuotaRequest(quotaSvc))
}

// GET /api/employee/pii-violations
func getMyPIIViolations(svc *services.EmployeeSelfService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		limit := c.QueryInt("limit", 50)
		violations, err := svc.GetMyPIIViolations(c.Context(), claims.TenantID, claims.UserID, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if violations == nil {
			violations = []services.PIIViolationSummary{}
		}
		return c.JSON(fiber.Map{"violations": violations})
	}
}

// POST /api/employee/quota-requests
func submitQuotaRequest(svc *services.EmployeeSelfService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var body struct {
			RequestedTokens int64  `json:"requested_tokens"`
			Reason          string `json:"reason"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if body.RequestedTokens <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "requested_tokens must be positive"})
		}
		if body.Reason == "" {
			return c.Status(400).JSON(fiber.Map{"error": "reason is required"})
		}
		id, err := svc.SubmitQuotaRequest(c.Context(), claims.TenantID, claims.UserID, body.RequestedTokens, body.Reason)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "pending"})
	}
}

// GET /api/employee/quota-requests
func getMyQuotaRequests(svc *services.EmployeeSelfService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		reqs, err := svc.GetMyQuotaRequests(c.Context(), claims.TenantID, claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if reqs == nil {
			reqs = []services.EmployeeQuotaRequest{}
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

// GET /api/employee/usage-export?month=YYYY-MM
func exportMyUsageCSV(svc *services.EmployeeSelfService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			month = time.Now().Format("2006-01")
		}
		csvData, err := svc.ExportUsageCSV(c.Context(), claims.TenantID, claims.UserID, month)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", "text/csv; charset=utf-8")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="usage-%s.csv"`, month))
		return c.Send(csvData)
	}
}

// GET /api/admin/quota-requests — admin only
func listAllPendingQuotaRequests(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		reqs, err := svc.ListPending(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if reqs == nil {
			reqs = []*services.QuotaRequest{}
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

// PUT /api/admin/quota-requests/:id — admin only; body: {status, review_note}
func reviewQuotaRequest(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Status     string `json:"status"`
			ReviewNote string `json:"review_note"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		id := c.Params("id")
		switch body.Status {
		case "approved":
			if err := svc.Approve(c.Context(), claims.TenantID, id, claims.UserID); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
		case "denied", "rejected":
			if err := svc.Reject(c.Context(), claims.TenantID, id, claims.UserID); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
		default:
			return c.Status(400).JSON(fiber.Map{"error": "status must be 'approved' or 'denied'"})
		}
		return c.JSON(fiber.Map{"id": id, "status": body.Status})
	}
}
