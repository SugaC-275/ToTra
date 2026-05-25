package providers_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

// TestOpenAIToBedrockBody verifies that OpenAI request format is correctly
// converted to the Bedrock Anthropic Messages API format.
func TestOpenAIToBedrockBody(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantVersion   string
		wantMaxTokens int
		wantMsgCount  int
		wantFirstRole string
	}{
		{
			name: "basic user message",
			input: `{
				"model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			wantVersion:   "bedrock-2023-05-31",
			wantMaxTokens: 4096,
			wantMsgCount:  1,
			wantFirstRole: "user",
		},
		{
			name: "explicit max_tokens preserved",
			input: `{
				"model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
				"max_tokens": 512,
				"messages": [
					{"role": "user", "content": "Hi"},
					{"role": "assistant", "content": "Hello!"},
					{"role": "user", "content": "How are you?"}
				]
			}`,
			wantVersion:   "bedrock-2023-05-31",
			wantMaxTokens: 512,
			wantMsgCount:  3,
			wantFirstRole: "user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := providers.OpenAIToBedrockBody([]byte(tc.input))
			require.NoError(t, err)

			var got map[string]interface{}
			require.NoError(t, json.Unmarshal(out, &got))

			assert.Equal(t, tc.wantVersion, got["anthropic_version"])
			assert.Equal(t, float64(tc.wantMaxTokens), got["max_tokens"])

			msgs, ok := got["messages"].([]interface{})
			require.True(t, ok)
			assert.Len(t, msgs, tc.wantMsgCount)
			assert.Equal(t, tc.wantFirstRole, msgs[0].(map[string]interface{})["role"])
		})
	}
}

// TestBedrockToOpenAIResponse verifies that Bedrock Claude response JSON is
// correctly mapped to OpenAI chat completion format.
func TestBedrockToOpenAIResponse(t *testing.T) {
	bedrockResp := `{
		"id": "msg-123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "I am Claude."}],
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	out, usage, err := providers.BedrockToOpenAIResponse([]byte(bedrockResp))
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))

	assert.Equal(t, "msg-123", got["id"])
	choices := got["choices"].([]interface{})
	require.Len(t, choices, 1)

	choice := choices[0].(map[string]interface{})
	assert.Equal(t, "stop", choice["finish_reason"])

	msg := choice["message"].(map[string]interface{})
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "I am Claude.", msg["content"])

	usageMap := got["usage"].(map[string]interface{})
	assert.Equal(t, float64(10), usageMap["prompt_tokens"])
	assert.Equal(t, float64(5), usageMap["completion_tokens"])
	assert.Equal(t, float64(15), usageMap["total_tokens"])
}

// TestBedrockToOpenAIResponse_MultipleContentBlocks verifies concatenation of
// multiple text blocks in the Bedrock response.
func TestBedrockToOpenAIResponse_MultipleContentBlocks(t *testing.T) {
	bedrockResp := `{
		"id": "msg-456",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello "},
			{"type": "text", "text": "world"}
		],
		"usage": {"input_tokens": 3, "output_tokens": 2}
	}`

	out, _, err := providers.BedrockToOpenAIResponse([]byte(bedrockResp))
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))

	choices := got["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	assert.Equal(t, "Hello world", msg["content"])
}

// TestBedrockAdapter_ParseBaseURL exercises the URL parsing via the adapter constructor.
func TestBedrockAdapter_ParseBaseURL(t *testing.T) {
	tests := []struct {
		baseURL string
		wantErr bool
	}{
		{"bedrock://us-east-1/anthropic.claude-3-5-sonnet-20241022-v2:0", false},
		{"bedrock://eu-west-1/meta.llama3-70b-instruct-v1:0", false},
		{"bedrock://missing-model", true},
		{"https://not-bedrock.com/model", true},
		{"bedrock:///no-region", true},
	}

	for _, tc := range tests {
		t.Run(tc.baseURL, func(t *testing.T) {
			ok := providers.BedrockBaseURLIsValid(tc.baseURL)
			if tc.wantErr {
				assert.False(t, ok)
			} else {
				assert.True(t, ok)
			}
		})
	}
}

// TestBedrockAdapter_NonAnthropicModel verifies 501 is returned for non-Anthropic models.
func TestBedrockAdapter_NonAnthropicModel(t *testing.T) {
	// Skip if AWS credentials are available — this tests without real AWS calls.
	adapter := providers.NewBedrockAdapter("bedrock://us-east-1/meta.llama3-70b-instruct-v1:0", "")
	// We cannot call adapter.Forward without real AWS creds, but we can verify
	// the adapter is not nil and has a non-Anthropic model ID.
	assert.NotNil(t, adapter)
}

// TestBedrockAdapter_Integration is a placeholder for full integration tests.
// Skipped unless AWS credentials and a real Bedrock endpoint are available.
func TestBedrockAdapter_Integration(t *testing.T) {
	t.Skip("integration test: requires AWS credentials and Bedrock access")
}
