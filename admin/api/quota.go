package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterQuotaRoutes(app fiber.Router, svc services.QuotaServiceInterface) {
	app.Get("/api/quota/requests", listPendingRequests(svc))
	app.Post("/api/quota/requests/:id/approve", approveRequest(svc))
	app.Post("/api/quota/requests/:id/reject", rejectRequest(svc))
	app.Post("/api/quota/request", requestIncrease(svc))
	app.Get("/api/me/quota", getMyQuotaStatus(svc))
}

func listPendingRequests(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		reqs, err := svc.ListPending(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

func approveRequest(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		err := svc.Approve(c.Context(), claims.TenantID, c.Params("id"), claims.UserID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "approved"})
	}
}

func rejectRequest(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		err := svc.Reject(c.Context(), claims.TenantID, c.Params("id"), claims.UserID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "rejected"})
	}
}

func getMyQuotaStatus(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := time.Now().Format("2006-01")
		qs, err := svc.GetMyStatus(c.Context(), claims.TenantID, claims.UserID, month)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(qs)
	}
}

func requestIncrease(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var body struct {
			UserID   string `json:"user_id"`
			NewQuota int    `json:"new_quota"`
			Reason   string `json:"reason"`
		}
		if err := c.BodyParser(&body); err != nil || body.NewQuota <= 0 || body.Reason == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user_id, new_quota, reason required"})
		}
		targetUser := body.UserID
		if targetUser == "" {
			targetUser = claims.UserID
		}
		err := svc.RequestIncrease(c.Context(), claims.TenantID, targetUser, claims.UserID, body.NewQuota, body.Reason)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "pending"})
	}
}
