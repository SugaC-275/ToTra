package handlers

import (
	"context"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// sessionItem is the shape returned in list and detail responses.
type sessionItem struct {
	ID           string  `json:"id"`
	UserID       string  `json:"user_id"`
	Title        string  `json:"title,omitempty"`
	Model        string  `json:"model,omitempty"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	PIIEvents    int     `json:"pii_events"`
	TurnCount    int     `json:"turn_count"`
	FirstAt      string  `json:"first_at"`
	LastAt       string  `json:"last_at"`
	ClosedAt     string  `json:"closed_at,omitempty"`
	IsActive     bool    `json:"is_active"`
}

// RegisterSessionRoutes mounts session endpoints.
//
//	GET    /v1/sessions           user's sessions
//	GET    /v1/sessions/:id       session detail
//	DELETE /v1/sessions/:id       close session (GDPR)
//	GET    /admin/sessions        admin: all tenant sessions
func RegisterSessionRoutes(v1 fiber.Router, admin fiber.Router, store *storage.SessionStore) {
	v1.Get("/sessions", listSessions(store))
	v1.Get("/sessions/:id", getSession(store))
	v1.Delete("/sessions/:id", closeSession(store))
	admin.Get("/sessions", adminListSessions(store))
}

func listSessions(store *storage.SessionStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "50"))
		sessions, err := store.List(c.Context(), user.TenantID, user.UserID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		items := make([]sessionItem, 0, len(sessions))
		for _, s := range sessions {
			items = append(items, toSessionItem(s))
		}
		return c.JSON(fiber.Map{"data": items, "count": len(items)})
	}
}

func getSession(store *storage.SessionStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		ctx := context.Background()
		sessions, err := store.List(ctx, user.TenantID, user.UserID, 1000)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		id := c.Params("id")
		for _, s := range sessions {
			if s.ID == id {
				return c.JSON(toSessionItem(s))
			}
		}
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": "session not found"}})
	}
}

func closeSession(store *storage.SessionStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		if err := store.Close(c.Context(), c.Params("id"), user.TenantID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error()}})
		}
		return c.Status(fiber.StatusNoContent).Send(nil)
	}
}

func adminListSessions(store *storage.SessionStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": fiber.Map{"message": "admin only"}})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "100"))
		filterUser := c.Query("user_id")
		sessions, err := store.ListAll(c.Context(), user.TenantID, filterUser, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		items := make([]sessionItem, 0, len(sessions))
		for _, s := range sessions {
			items = append(items, toSessionItem(s))
		}
		return c.JSON(fiber.Map{"data": items, "count": len(items)})
	}
}

func toSessionItem(s *storage.Session) sessionItem {
	item := sessionItem{
		ID:           s.ID,
		UserID:       s.UserID,
		Title:        s.Title,
		Model:        s.Model,
		TotalTokens:  s.TotalTokens,
		TotalCostUSD: s.TotalCostUSD,
		PIIEvents:    s.PIIEvents,
		TurnCount:    s.TurnCount,
		FirstAt:      s.FirstAt.UTC().Format("2006-01-02T15:04:05Z"),
		LastAt:       s.LastAt.UTC().Format("2006-01-02T15:04:05Z"),
		IsActive:     s.IsActive,
	}
	if s.ClosedAt != nil {
		item.ClosedAt = s.ClosedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return item
}
