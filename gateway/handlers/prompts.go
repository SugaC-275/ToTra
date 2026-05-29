package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

func RegisterPromptRoutes(router fiber.Router, store *storage.PromptStore) {
	router.Post("/prompts", createPrompt(store))
	router.Get("/prompts", listPrompts(store))
	router.Get("/prompts/:name", getPrompt(store))
	router.Get("/prompts/:name/versions/:version", getPromptVersion(store))
	router.Post("/prompts/:name/render", renderPrompt(store))
}

func createPrompt(store *storage.PromptStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		var p storage.PromptTemplate
		if err := c.BodyParser(&p); err != nil || p.Name == "" || p.Content == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "name and content are required"},
			})
		}
		p.TenantID = user.TenantID
		p.CreatedBy = user.UserID
		if err := store.Create(c.Context(), &p); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(&p)
	}
}

func listPrompts(store *storage.PromptStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		prompts, err := store.List(c.Context(), user.TenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if prompts == nil {
			prompts = []*storage.PromptTemplate{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": prompts})
	}
}

func getPrompt(store *storage.PromptStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		p, err := store.GetLatest(c.Context(), user.TenantID, c.Params("name"))
		if err != nil || p == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "prompt not found"})
		}
		return c.JSON(p)
	}
}

func getPromptVersion(store *storage.PromptStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		version, err := strconv.Atoi(c.Params("version"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid version"})
		}
		p, err := store.GetVersion(c.Context(), user.TenantID, c.Params("name"), version)
		if err != nil || p == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "prompt version not found"})
		}
		return c.JSON(p)
	}
}

func renderPrompt(store *storage.PromptStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		var payload struct {
			Variables map[string]string `json:"variables"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		p, err := store.GetLatest(c.Context(), user.TenantID, c.Params("name"))
		if err != nil || p == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "prompt not found"})
		}
		rendered := p.Render(payload.Variables)
		return c.JSON(fiber.Map{
			"prompt":    p.Name,
			"version":   p.Version,
			"rendered":  rendered,
		})
	}
}
