package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/storage"
)

// ErrBAARequired is returned when PHI is present but the selected model
// has not been marked BAA-compliant.
var ErrBAARequired = errors.New("phi detected: request must be routed to a BAA-compliant model")

// RequireBAACompliant checks whether BAA enforcement is required for the
// current request. When c.Locals("require_baa") is true and the provided
// ModelConfig is not marked BAA-compliant, it writes a 451 response and
// returns ErrBAARequired. Callers should return immediately on non-nil error.
func RequireBAACompliant(c *fiber.Ctx, modelCfg *storage.ModelConfig) error {
	requireBAA, _ := c.Locals("require_baa").(bool)
	if !requireBAA {
		return nil
	}
	if modelCfg != nil && modelCfg.BAACompliant {
		return nil
	}
	_ = c.Status(451).JSON(fiber.Map{
		"error": fiber.Map{
			"message": "PHI detected: request must be routed to a BAA-compliant model",
			"type":    "compliance_error",
		},
	})
	return ErrBAARequired
}
