package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/mcp"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

const toolNameSep = "__"

// MCPModelLookup is satisfied by *storage.PGModelLookup.
type MCPModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// MCPUsageStore is satisfied by *storage.UsageStore.
type MCPUsageStore interface {
	Record(r *storage.UsageRecord)
}

type mcpChatRequest struct {
	Model        string       `json:"model"`
	Messages     []mcpMessage `json:"messages"`
	MCPServers   []string     `json:"mcp_servers,omitempty"`
	MaxToolCalls int          `json:"max_tool_calls,omitempty"`
	Stream       bool         `json:"stream"`
}

type mcpMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type openAIChoice struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content   *string          `json:"content"`
		ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
}

type openAICompletionResponse struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type mcpMetadata struct {
	ToolCallsMade  int      `json:"tool_calls_made"`
	ServersUsed    []string `json:"servers_used"`
	LoopIterations int      `json:"loop_iterations"`
}

type toolEntry struct {
	serverName, serverID, localName string
	client                          *mcp.Client
}

// NewMCPHandler returns a Fiber handler for POST /v1/mcp/chat.
func NewMCPHandler(
	serverStore *storage.MCPServerStore,
	modelLookup MCPModelLookup,
	usageStore MCPUsageStore,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)

		var req mcpChatRequest
		if err := c.BodyParser(&req); err != nil {
			return badReq(c, "invalid JSON body")
		}
		if req.Model == "" || len(req.Messages) == 0 {
			return badReq(c, "model and messages are required")
		}
		if req.Stream {
			return badReq(c, "streaming is not supported for MCP chat")
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, req.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured", req.Model), "type": "model_not_found",
			}})
		}
		adapter, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return badReq(c, "unsupported provider: "+modelCfg.Provider)
		}

		serverCfgs, err := serverStore.ListEnabled(c.Context(), user.TenantID)
		if err != nil {
			slog.Error("mcp: list servers", "tenant", user.TenantID, "err", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fiber.Map{
				"message": "failed to load MCP server configs", "type": "internal_error",
			}})
		}

		activeCfgs := filterServers(serverCfgs, req.MCPServers)
		maxTC := effectiveMaxToolCalls(activeCfgs, req.MaxToolCalls)
		toolMap, openAITools := discoverTools(c.Context(), activeCfgs)
		messages := convertMessages(req.Messages)

		var totalP, totalC, toolCallsMade, loopIterations int
		serversUsed := make(map[string]struct{})
		var finalResponse *openAICompletionResponse
		start := time.Now()

	loop:
		for {
			loopIterations++
			upBody := map[string]any{"model": req.Model, "messages": messages}
			if len(openAITools) > 0 {
				upBody["tools"] = openAITools
			}
			bodyBytes, _ := json.Marshal(upBody)

			fwd, usage, ferr := adapter.Forward(c.Context(), bodyBytes)
			if ferr != nil {
				slog.Error("mcp: forward", "tenant", user.TenantID, "err", ferr)
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
			}
			if fwd.StatusCode != 200 {
				return c.Status(fwd.StatusCode).Send(fwd.Body)
			}
			if usage != nil {
				totalP += usage.PromptTokens
				totalC += usage.CompletionTokens
			}

			var comp openAICompletionResponse
			if jerr := json.Unmarshal(fwd.Body, &comp); jerr != nil {
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "malformed upstream response"})
			}
			finalResponse = &comp

			if len(comp.Choices) == 0 || comp.Choices[0].FinishReason != "tool_calls" ||
				len(comp.Choices[0].Message.ToolCalls) == 0 {
				break
			}
			choice := comp.Choices[0]
			assistantMsg := map[string]any{"role": "assistant", "tool_calls": choice.Message.ToolCalls}
			if choice.Message.Content != nil {
				assistantMsg["content"] = *choice.Message.Content
			}
			messages = append(messages, assistantMsg)

			for _, tc := range choice.Message.ToolCalls {
				if toolCallsMade >= maxTC {
					note := fmt.Sprintf("[Tool call limit of %d reached.]", maxTC)
					messages = append(messages, map[string]any{"role": "user", "content": note})
					summaryBody, _ := json.Marshal(map[string]any{"model": req.Model, "messages": messages})
					if sr, su, serr := adapter.Forward(c.Context(), summaryBody); serr == nil && sr.StatusCode == 200 {
						var sc openAICompletionResponse
						if json.Unmarshal(sr.Body, &sc) == nil {
							finalResponse = &sc
						}
						if su != nil {
							totalP += su.PromptTokens
							totalC += su.CompletionTokens
						}
					}
					break loop
				}
				resultText, logE := executeToolCall(c.Context(), tc, toolMap)
				logE.TenantID, logE.UserID = user.TenantID, user.UserID
				go func(e storage.MCPToolCallEntry) {
					if err := serverStore.LogToolCall(context.Background(), e); err != nil {
						slog.Warn("mcp: audit log", "err", err)
					}
				}(logE)
				if logE.ServerName != "" {
					serversUsed[logE.ServerName] = struct{}{}
				}
				messages = appendToolResult(messages, tc.ID, tc.Function.Name, resultText)
				toolCallsMade++
			}
		}

		usageStore.Record(&storage.UsageRecord{
			TenantID: user.TenantID, UserID: user.UserID, ModelConfigID: modelCfg.ID,
			PromptTokens: totalP, CompletionTokens: totalC,
			SCUCost: tokenizer.ToSCU(totalP, totalC, modelCfg.SCURate),
			ResponseMS: int(time.Since(start).Milliseconds()),
		})

		usedServers := make([]string, 0, len(serversUsed))
		for name := range serversUsed {
			usedServers = append(usedServers, name)
		}
		var finalContent, respID string
		respID = "chatcmpl-mcp"
		if finalResponse != nil && finalResponse.ID != "" {
			respID = finalResponse.ID
		}
		if finalResponse != nil && len(finalResponse.Choices) > 0 && finalResponse.Choices[0].Message.Content != nil {
			finalContent = *finalResponse.Choices[0].Message.Content
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"id": respID, "object": "chat.completion",
			"choices": []fiber.Map{{
				"index":         0,
				"message":       fiber.Map{"role": "assistant", "content": finalContent},
				"finish_reason": "stop",
			}},
			"usage": fiber.Map{
				"prompt_tokens": totalP, "completion_tokens": totalC, "total_tokens": totalP + totalC,
			},
			"mcp_metadata": mcpMetadata{
				ToolCallsMade: toolCallsMade, ServersUsed: usedServers, LoopIterations: loopIterations,
			},
		})
	}
}

// discoverTools concurrently lists tools from all servers, returning a qualified
// tool map and the OpenAI function-calling schema slice.
func discoverTools(ctx context.Context, cfgs []storage.MCPServerConfig) (map[string]*toolEntry, []map[string]any) {
	type item struct {
		cfg   storage.MCPServerConfig
		tools []mcp.Tool
		err   error
	}
	items := make([]item, len(cfgs))
	var wg sync.WaitGroup
	for i, cfg := range cfgs {
		wg.Add(1)
		go func(idx int, cfg storage.MCPServerConfig) {
			defer wg.Done()
			tools, err := newMCPClient(cfg).ListTools(ctx)
			items[idx] = item{cfg: cfg, tools: tools, err: err}
		}(i, cfg)
	}
	wg.Wait()

	toolMap := make(map[string]*toolEntry)
	var openAITools []map[string]any
	for _, it := range items {
		if it.err != nil {
			slog.Warn("mcp: list tools", "server", it.cfg.Name, "err", it.err)
			continue
		}
		cl := newMCPClient(it.cfg)
		for _, t := range it.tools {
			qName := it.cfg.Name + toolNameSep + t.Name
			toolMap[qName] = &toolEntry{serverName: it.cfg.Name, serverID: it.cfg.ID, client: cl, localName: t.Name}
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			openAITools = append(openAITools, map[string]any{
				"type":     "function",
				"function": map[string]any{"name": qName, "description": t.Description, "parameters": schema},
			})
		}
	}
	return toolMap, openAITools
}

// executeToolCall invokes a single tool and returns the text result plus audit entry.
// TenantID and UserID must be filled in by the caller.
func executeToolCall(ctx context.Context, tc openAIToolCall, toolMap map[string]*toolEntry) (string, storage.MCPToolCallEntry) {
	entry, ok := toolMap[tc.Function.Name]
	if !ok {
		return "error: unknown tool " + tc.Function.Name,
			storage.MCPToolCallEntry{ToolName: tc.Function.Name, StatusCode: 400}
	}

	piiType, _ := middleware.ScanForPII(string(tc.Function.Arguments))
	piiDetected := piiType != ""
	t0 := time.Now()
	result, callErr := entry.client.CallTool(ctx, entry.localName, tc.Function.Arguments)
	durationMS := int(time.Since(t0).Milliseconds())

	statusCode := 200
	var resultText string
	var responseBody json.RawMessage

	if callErr != nil {
		statusCode = 502
		resultText = "error: " + callErr.Error()
		responseBody = json.RawMessage(`null`)
	} else {
		for _, part := range result.Content {
			if part.Type == "text" {
				resultText += part.Text
			}
		}
		if result.IsError {
			statusCode = 502
		}
		if rb, err := json.Marshal(result); err == nil {
			responseBody = rb
		}
		if _, found := middleware.ScanForPII(resultText); found {
			piiDetected = true
		}
	}
	return resultText, storage.MCPToolCallEntry{
		ServerID: entry.serverID, ServerName: entry.serverName, ToolName: entry.localName,
		RequestArgs: tc.Function.Arguments, ResponseBody: responseBody,
		StatusCode: statusCode, DurationMS: durationMS, PIIDetected: piiDetected,
	}
}

func filterServers(cfgs []storage.MCPServerConfig, filter []string) []storage.MCPServerConfig {
	if len(filter) == 0 {
		return cfgs
	}
	set := make(map[string]bool, len(filter))
	for _, s := range filter {
		set[s] = true
	}
	var out []storage.MCPServerConfig
	for _, cfg := range cfgs {
		if set[cfg.Name] {
			out = append(out, cfg)
		}
	}
	return out
}

func effectiveMaxToolCalls(cfgs []storage.MCPServerConfig, override int) int {
	if override > 0 {
		return override
	}
	max := 0
	for _, cfg := range cfgs {
		if cfg.MaxToolCalls > max {
			max = cfg.MaxToolCalls
		}
	}
	if max <= 0 {
		return 10
	}
	return max
}

func convertMessages(in []mcpMessage) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, m := range in {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		out = append(out, msg)
	}
	return out
}

func appendToolResult(msgs []map[string]any, toolCallID, name, content string) []map[string]any {
	return append(msgs, map[string]any{
		"role": "tool", "tool_call_id": toolCallID, "name": name, "content": content,
	})
}

func badReq(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{"message": msg, "type": "bad_request"},
	})
}

func newMCPClient(cfg storage.MCPServerConfig) *mcp.Client {
	return mcp.NewClient(mcp.Server{Name: cfg.Name, URL: cfg.URL, AuthType: cfg.AuthType, AuthToken: cfg.AuthToken})
}
