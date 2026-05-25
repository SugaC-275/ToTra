package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMCPResponse writes a JSON-RPC 2.0 result response to w.
func writeMCPResponse(w http.ResponseWriter, id int, result any) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeMCPError writes a JSON-RPC 2.0 error response.
func writeMCPError(w http.ResponseWriter, id, code int, msg string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func TestListTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "tools/list", req.Method)
		assert.Equal(t, "2.0", req.JSONRPC)
		assert.Equal(t, http.MethodPost, r.Method)

		writeMCPResponse(w, req.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "get_weather",
					"description": "Returns current weather for a city",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL})
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "get_weather", tools[0].Name)
	assert.Equal(t, "Returns current weather for a city", tools[0].Description)
	assert.NotEmpty(t, tools[0].InputSchema)
}

func TestListToolsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMCPResponse(w, 1, map[string]any{"tools": []any{}})
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL})
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestCallTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "tools/call", req.Method)

		writeMCPResponse(w, req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Beijing: 25°C, sunny"},
			},
			"isError": false,
		})
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL})
	args := json.RawMessage(`{"city":"Beijing"}`)
	result, err := client.CallTool(context.Background(), "get_weather", args)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "Beijing: 25°C, sunny", result.Content[0].Text)
	assert.False(t, result.IsError)
}

func TestCallToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMCPError(w, 2, -32601, "method not found")
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL})
	_, err := client.CallTool(context.Background(), "nonexistent", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json-rpc error")
	assert.Contains(t, err.Error(), "method not found")
}

func TestBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeMCPResponse(w, 1, map[string]any{"tools": []any{}})
	}))
	defer srv.Close()

	client := NewClient(Server{
		Name:      "test",
		URL:       srv.URL,
		AuthType:  "bearer",
		AuthToken: "super-secret-token",
	})
	_, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer super-secret-token", gotAuth)
}

func TestNoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeMCPResponse(w, 1, map[string]any{"tools": []any{}})
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL, AuthType: "none"})
	_, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, gotAuth)
}

func TestHTTPNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(Server{Name: "test", URL: srv.URL})
	_, err := client.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until client disconnects.
		<-r.Context().Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := NewClient(Server{Name: "test", URL: srv.URL})
	_, err := client.ListTools(ctx)
	require.Error(t, err)
}
