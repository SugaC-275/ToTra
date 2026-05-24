package api

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type PolicyRulesServiceIface interface {
	List(ctx context.Context, tenantID string) ([]*services.PolicyRule, error)
	Create(ctx context.Context, tenantID, name, pattern, action string) (*services.PolicyRule, error)
	Update(ctx context.Context, tenantID string, id int64, name, pattern, action string, isActive bool) (*services.PolicyRule, error)
	Delete(ctx context.Context, tenantID string, id int64) error
}

func RegisterPolicyRulesRoutes(r fiber.Router, svc PolicyRulesServiceIface) {
	r.Get("/api/admin/compliance/policy-rules", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		rules, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(rules)
	})

	r.Post("/api/admin/compliance/policy-rules", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Name    string `json:"name"`
			Pattern string `json:"pattern"`
			Action  string `json:"action"`
		}
		if err := c.BodyParser(&body); err != nil || body.Name == "" || body.Pattern == "" || body.Action == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name, pattern, action required"})
		}
		rule, err := svc.Create(c.Context(), claims.TenantID, body.Name, body.Pattern, body.Action)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(rule)
	})

	r.Put("/api/admin/compliance/policy-rules/:id", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
		}
		var body struct {
			Name     string `json:"name"`
			Pattern  string `json:"pattern"`
			Action   string `json:"action"`
			IsActive bool   `json:"is_active"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		rule, err := svc.Update(c.Context(), claims.TenantID, id, body.Name, body.Pattern, body.Action, body.IsActive)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(rule)
	})

	r.Delete("/api/admin/compliance/policy-rules/:id", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*services.Claims)
		if !ok || claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(204)
	})
}
