package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type CostAnalysisHandler struct {
	svc    *services.CostAnalysisService
	botSvc *services.BotService
}

func NewCostAnalysisHandler(svc *services.CostAnalysisService, botSvc *services.BotService) *CostAnalysisHandler {
	return &CostAnalysisHandler{svc: svc, botSvc: botSvc}
}

func RegisterCostAnalysisRoutes(app fiber.Router, svc *services.CostAnalysisService, botSvc *services.BotService) {
	h := NewCostAnalysisHandler(svc, botSvc)
	app.Get("/api/admin/cost/breakdown", h.getBreakdown)
	app.Get("/api/admin/cost/waste", h.getWaste)
	app.Get("/api/admin/cost/suggestions", h.getSuggestions)
	app.Post("/api/admin/cost/alert-test", h.testAlert)
}

func (h *CostAnalysisHandler) getBreakdown(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "admin only"})
	}
	month := c.Query("month", costAPICurrentYearMonth())
	data, err := h.svc.GetCostBreakdown(c.Context(), claims.TenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(data)
}

func (h *CostAnalysisHandler) getWaste(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "admin only"})
	}
	month := c.Query("month", costAPICurrentYearMonth())
	data, err := h.svc.GetWasteReport(c.Context(), claims.TenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if data == nil {
		data = []*services.WasteItem{}
	}
	return c.JSON(data)
}

func (h *CostAnalysisHandler) getSuggestions(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "admin only"})
	}
	month := c.Query("month", costAPICurrentYearMonth())
	data, err := h.svc.GetOptimizationSuggestions(c.Context(), claims.TenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if data == nil {
		data = []*services.OptimizationSuggestion{}
	}
	return c.JSON(data)
}

func (h *CostAnalysisHandler) testAlert(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*services.Claims)
	if !ok || claims.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "admin only"})
	}
	month := costAPICurrentYearMonth()
	breakdown, err := h.svc.GetCostBreakdown(c.Context(), claims.TenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	suggestions, err := h.svc.GetOptimizationSuggestions(c.Context(), claims.TenantID, month)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	msg := formatCostAlertMsg(month, breakdown.TotalUSD, suggestions)
	if h.botSvc != nil {
		_ = h.botSvc.BroadcastAlert(c.Context(), claims.TenantID, msg)
	}
	return c.JSON(fiber.Map{"sent": true, "message": msg})
}

func formatCostAlertMsg(month string, totalUSD float64, suggestions []*services.OptimizationSuggestion) string {
	msg := fmt.Sprintf("💰 %s AI 成本报告\n总支出: $%.2f\n\n优化建议:\n", month, totalUSD)
	for i, s := range suggestions {
		if i >= 3 {
			break
		}
		msg += fmt.Sprintf("• [%s] %s（可节省 $%.2f）\n", s.Priority, s.Action, s.SavingsUSD)
	}
	return msg
}

func costAPICurrentYearMonth() string {
	return time.Now().UTC().Format("2006-01")
}
