package handlers

import (
	"encoding/json"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// RegisterRequestLogRoutes mounts the logs endpoints on the given router.
//   GET /v1/logs             — paginated list with filters (supports ?tag=)
//   GET /v1/logs/:id         — full detail for a single log
//   GET /v1/spend/by-tag     — aggregate cost/tokens grouped by tag
func RegisterRequestLogRoutes(router fiber.Router, store *storage.RequestLogStore) {
	router.Get("/logs", listRequestLogs(store))
	router.Get("/logs/:id", getRequestLog(store))
	router.Get("/spend/by-tag", spendByTag(store))
}

// logItem is the shape returned in the list response.
type logItem struct {
	ID               string   `json:"id"`
	UserID           string   `json:"user_id"`
	Model            string   `json:"model"`
	Provider         string   `json:"provider"`
	StatusCode       int      `json:"status_code"`
	LatencyMS        int      `json:"latency_ms"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	CostUSD          float64  `json:"cost_usd"`
	Tags             []string `json:"tags"`
	CreatedAt        string   `json:"created_at"`
	RequestPreview   string   `json:"request_preview"`
	ResponsePreview  string   `json:"response_preview"`
}

// logDetail extends logItem with full bodies for the single-record endpoint.
type logDetail struct {
	logItem
	RequestBody  json.RawMessage `json:"request_body"`
	ResponseBody json.RawMessage `json:"response_body"`
}

func listRequestLogs(store *storage.RequestLogStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}

		limit, _ := strconv.Atoi(c.Query("limit", "50"))
		offset, _ := strconv.Atoi(c.Query("offset", "0"))

		f := storage.RequestLogFilter{
			UserID:   c.Query("user_id"),
			Model:    c.Query("model"),
			Provider: c.Query("provider"),
			Status:   c.Query("status"),
			Search:   c.Query("search"),
			Tag:      c.Query("tag"),
			Limit:    limit,
			Offset:   offset,
		}

		logs, total, err := store.List(c.Context(), user.TenantID, f)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		items := make([]logItem, 0, len(logs))
		for _, l := range logs {
			items = append(items, toLogItem(l))
		}

		return c.JSON(fiber.Map{
			"data":   items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

func getRequestLog(store *storage.RequestLogStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}

		l, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": "log not found"}})
		}

		detail := logDetail{
			logItem:      toLogItem(l),
			RequestBody:  json.RawMessage(l.RequestBody),
			ResponseBody: json.RawMessage(l.ResponseBody),
		}
		return c.JSON(detail)
	}
}

// toLogItem converts a storage record to the API list shape with truncated previews.
func toLogItem(l *storage.RequestLog) logItem {
	tags := l.Tags
	if tags == nil {
		tags = []string{}
	}
	return logItem{
		ID:               l.ID,
		UserID:           l.UserID,
		Model:            l.Model,
		Provider:         l.Provider,
		StatusCode:       l.StatusCode,
		LatencyMS:        l.LatencyMS,
		PromptTokens:     l.PromptTokens,
		CompletionTokens: l.CompletionTokens,
		CostUSD:          l.CostUSD,
		Tags:             tags,
		CreatedAt:        l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		RequestPreview:   extractPreview(l.RequestBody, "messages", "content"),
		ResponsePreview:  extractPreview(l.ResponseBody, "choices", "message", "content"),
	}
}

func spendByTag(store *storage.RequestLogStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}

		result, err := store.AggregateSpendByTag(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if result == nil {
			result = []*storage.TagSpend{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": result})
	}
}

// extractPreview digs into a JSONB blob following a chain of keys and returns
// the first 200 characters of the string value found, or an empty string.
//
// For request:  messages[0].content
// For response: choices[0].message.content
func extractPreview(data []byte, arrayKey string, nestedKeys ...string) string {
	if len(data) == 0 {
		return ""
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return ""
	}

	raw, ok := obj[arrayKey]
	if !ok {
		return ""
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return ""
	}

	// Walk the remaining key chain through the first array element.
	cur := arr[0]
	for _, key := range nestedKeys {
		var node map[string]json.RawMessage
		if err := json.Unmarshal(cur, &node); err != nil {
			return ""
		}
		next, exists := node[key]
		if !exists {
			return ""
		}
		cur = next
	}

	// cur should be a JSON string.
	var s string
	if err := json.Unmarshal(cur, &s); err != nil {
		return ""
	}

	if len(s) > 200 {
		return s[:200]
	}
	return s
}
