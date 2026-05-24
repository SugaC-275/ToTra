package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
)

func serverError(c *fiber.Ctx, err error) error {
	slog.Error("request error", "path", c.Path(), "method", c.Method(), "error", err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
}
