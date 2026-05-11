package middleware

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// AgentLoopIncr is the narrow interface the middleware needs from AgentStore.
type AgentLoopIncr interface {
	IncrLoop(ctx context.Context, conversationID string) (int64, bool, error)
}

type agentRequestBody struct {
	Tools    []json.RawMessage `json:"tools"`
	Messages []struct {
		ToolCalls []json.RawMessage `json:"tool_calls"`
	} `json:"messages"`
}

func isAgentRequest(body []byte) bool {
	var req agentRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	if len(req.Tools) > 0 {
		return true
	}
	for _, msg := range req.Messages {
		if len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func countToolCalls(body []byte) int {
	var req agentRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, msg := range req.Messages {
		total += len(msg.ToolCalls)
	}
	return total
}

// NewAgentMiddleware detects Agent-mode requests and circuit-breaks at the loop limit.
func NewAgentMiddleware(store AgentLoopIncr) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()

		if !isAgentRequest(body) {
			c.Locals("agent_mode", false)
			c.Locals("agent_tool_call_count", 0)
			return c.Next()
		}

		c.Locals("agent_mode", true)
		c.Locals("agent_tool_call_count", countToolCalls(body))

		conversationID := c.Get("X-Conversation-ID")
		if conversationID == "" {
			return c.Next()
		}

		_, exceeded, err := store.IncrLoop(c.Context(), conversationID)
		if err != nil {
			return c.Next()
		}

		if exceeded {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "Agent loop limit exceeded",
					"type":    "agent_loop_exceeded",
					"code":    "agent_loop_exceeded",
				},
			})
		}

		return c.Next()
	}
}
