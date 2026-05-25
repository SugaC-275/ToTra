package api

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/yourorg/totra/admin/services"
)

// MCPServerServiceInterface is satisfied by services.MCPServerService.
type MCPServerServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*services.MCPServer, error)
	Get(ctx context.Context, tenantID, id string) (*services.MCPServer, error)
	Create(ctx context.Context, tenantID string, req services.CreateMCPServerRequest) (*services.MCPServer, error)
	Update(ctx context.Context, tenantID, id string, req services.UpdateMCPServerRequest) (*services.MCPServer, error)
	Delete(ctx context.Context, tenantID, id string) error
	ListToolCalls(ctx context.Context, tenantID string, limit, offset int) ([]*services.MCPToolCallLog, int64, error)
}

var mcpNameRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

// RegisterMCPServerRoutes wires up MCP server management endpoints.
// NOTE: /tool-calls must be registered BEFORE /:id to avoid route shadowing.
func RegisterMCPServerRoutes(app fiber.Router, svc MCPServerServiceInterface) {
	app.Get("/api/mcp-servers/tool-calls", listMCPToolCalls(svc))
	app.Get("/api/mcp-servers", listMCPServers(svc))
	app.Post("/api/mcp-servers", createMCPServer(svc))
	app.Get("/api/mcp-servers/:id", getMCPServer(svc))
	app.Put("/api/mcp-servers/:id", updateMCPServer(svc))
	app.Delete("/api/mcp-servers/:id", deleteMCPServer(svc))
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

func validateMCPName(name string) string {
	if name == "" {
		return "name is required"
	}
	if !mcpNameRE.MatchString(name) {
		return "name must match [a-z0-9_-]"
	}
	return ""
}

func validateMCPURL(url string) string {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "url must start with http:// or https://"
	}
	return ""
}

func validateAuthType(authType, authToken string) string {
	if authType != "none" && authType != "bearer" {
		return "auth_type must be 'none' or 'bearer'"
	}
	if authType == "bearer" && authToken == "" {
		return "auth_token required when auth_type is 'bearer'"
	}
	return ""
}

func validateMaxToolCalls(n int) string {
	if n < 1 || n > 100 {
		return "max_tool_calls must be between 1 and 100"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func listMCPServers(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		servers, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"total": len(servers), "servers": servers})
	}
}

func getMCPServer(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		id := c.Params("id")
		server, err := svc.Get(c.Context(), claims.TenantID, id)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(server)
	}
}

func createMCPServer(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}

		var req services.CreateMCPServerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		// Apply default auth_type.
		if req.AuthType == "" {
			req.AuthType = "none"
		}
		// Apply default max_tool_calls.
		if req.MaxToolCalls == 0 {
			req.MaxToolCalls = 10
		}

		// Validate fields.
		if msg := validateMCPName(req.Name); msg != "" {
			return c.Status(400).JSON(fiber.Map{"error": msg})
		}
		if req.URL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "url is required"})
		}
		if msg := validateMCPURL(req.URL); msg != "" {
			return c.Status(400).JSON(fiber.Map{"error": msg})
		}
		if msg := validateAuthType(req.AuthType, req.AuthToken); msg != "" {
			return c.Status(400).JSON(fiber.Map{"error": msg})
		}
		if msg := validateMaxToolCalls(req.MaxToolCalls); msg != "" {
			return c.Status(400).JSON(fiber.Map{"error": msg})
		}

		server, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(server)
	}
}

func updateMCPServer(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		id := c.Params("id")

		var req services.UpdateMCPServerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		// Validate changed fields.
		if req.URL != nil {
			if msg := validateMCPURL(*req.URL); msg != "" {
				return c.Status(400).JSON(fiber.Map{"error": msg})
			}
		}
		if req.AuthType != nil {
			token := ""
			if req.AuthToken != nil {
				token = *req.AuthToken
			}
			if msg := validateAuthType(*req.AuthType, token); msg != "" {
				return c.Status(400).JSON(fiber.Map{"error": msg})
			}
		}
		if req.MaxToolCalls != nil {
			if msg := validateMaxToolCalls(*req.MaxToolCalls); msg != "" {
				return c.Status(400).JSON(fiber.Map{"error": msg})
			}
		}

		server, err := svc.Update(c.Context(), claims.TenantID, id, req)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(server)
	}
}

func deleteMCPServer(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := adminOnly(c)
		if !ok {
			return nil
		}
		id := c.Params("id")
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"message": "deleted"})
	}
}

func listMCPToolCalls(svc MCPServerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)

		limit := 50
		if l := c.Query("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		offset := 0
		if o := c.Query("offset"); o != "" {
			if n, err := strconv.Atoi(o); err == nil && n >= 0 {
				offset = n
			}
		}

		logs, total, err := svc.ListToolCalls(c.Context(), claims.TenantID, limit, offset)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"total": total, "tool_calls": logs})
	}
}
