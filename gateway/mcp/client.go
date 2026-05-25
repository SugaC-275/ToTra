package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Server holds connection configuration for a single MCP server.
type Server struct {
	Name      string
	URL       string
	AuthType  string // "none" | "bearer"
	AuthToken string
}

// Tool describes a tool exposed by an MCP server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolResult is the return value of a tool call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a single item in a ToolResult.
type ToolContent struct {
	Type string `json:"type"` // "text" | "image" | "resource"
	Text string `json:"text,omitempty"`
}

// jsonRPCRequest is a JSON-RPC 2.0 request envelope.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	ID      int    `json:"id"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response envelope.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client manages communication with a single MCP server.
type Client struct {
	server     Server
	httpClient *http.Client
}

// NewClient returns a Client for the given server.
// The HTTP client uses a 5-second timeout for initial connection; individual
// tool calls are governed by the context passed to each method.
func NewClient(server Server) *Client {
	return &Client{
		server:     server,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// ListTools calls tools/list on the MCP server and returns the available tools.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		Params:  struct{}{},
		ID:      1,
	}

	raw, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: parse tools/list result: %w", err)
	}
	return result.Tools, nil
}

// CallTool calls tools/call on the MCP server with the given arguments.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments json.RawMessage) (*ToolResult, error) {
	params := map[string]any{
		"name":      toolName,
		"arguments": arguments,
	}
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params:  params,
		ID:      2,
	}

	raw, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}

	var result ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: parse tools/call result: %w", err)
	}
	return &result, nil
}

// do performs a JSON-RPC request and returns the raw result bytes.
func (c *Client) do(ctx context.Context, rpcReq jsonRPCRequest) (json.RawMessage, error) {
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.server.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.server.AuthType == "bearer" && c.server.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.server.AuthToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: http request to %s: %w", c.server.URL, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: server %s returned status %d: %s",
			c.server.Name, resp.StatusCode, respBytes)
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("mcp: parse response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp: json-rpc error %d: %s",
			rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}
