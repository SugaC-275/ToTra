package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockAdapter forwards requests to AWS Bedrock.
// baseURL format: "bedrock://{region}/{modelId}"
// e.g. "bedrock://us-east-1/anthropic.claude-3-5-sonnet-20241022-v2:0"
type BedrockAdapter struct {
	region  string
	modelID string
}

func parseBedrockBaseURL(baseURL string) (region, modelID string, err error) {
	path := strings.TrimPrefix(baseURL, "bedrock://")
	if path == baseURL {
		return "", "", fmt.Errorf("bedrock: baseURL must start with bedrock://, got %q", baseURL)
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("bedrock: baseURL must be bedrock://{region}/{modelId}, got %q", baseURL)
	}
	return parts[0], parts[1], nil
}

func NewBedrockAdapter(baseURL, _ string) *BedrockAdapter {
	region, modelID, err := parseBedrockBaseURL(baseURL)
	if err != nil {
		return &BedrockAdapter{}
	}
	return &BedrockAdapter{region: region, modelID: modelID}
}

func (a *BedrockAdapter) newClient(ctx context.Context) (*bedrockruntime.Client, error) {
	if a.region == "" {
		return nil, fmt.Errorf("bedrock: invalid baseURL — region must be non-empty")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(a.region))
	if err != nil {
		return nil, fmt.Errorf("bedrock: load AWS config: %w", err)
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}

// openAIMessage is the subset of OpenAI chat message fields we need.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIRequest is the incoming OpenAI-format request body.
type openAIRequest struct {
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

// bedrockAnthropicRequest is the Bedrock Anthropic Messages API body format.
type bedrockAnthropicRequest struct {
	AnthropicVersion string          `json:"anthropic_version"`
	MaxTokens        int             `json:"max_tokens"`
	Messages         []openAIMessage `json:"messages"`
}

// bedrockAnthropicResponse is the response from Bedrock for Claude models.
type bedrockAnthropicResponse struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// bedrockStreamChunk is the partial stream event from Bedrock Claude.
type bedrockStreamChunk struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// OpenAIToBedrockBody converts an OpenAI chat request body to Bedrock Anthropic Messages format.
// Exported for unit testing.
func OpenAIToBedrockBody(body []byte) ([]byte, error) {
	return openAIToBedrockBody(body)
}

// BedrockToOpenAIResponse converts a Bedrock Claude response to OpenAI chat completion format.
// Exported for unit testing.
func BedrockToOpenAIResponse(respBody []byte) ([]byte, *Usage, error) {
	return bedrockToOpenAIResponse(respBody)
}

// BedrockBaseURLIsValid returns true if the baseURL is a valid bedrock:// URL.
// Exported for unit testing.
func BedrockBaseURLIsValid(baseURL string) bool {
	_, _, err := parseBedrockBaseURL(baseURL)
	return err == nil
}

// openAIToBedrockBody converts an OpenAI chat request body to Bedrock Anthropic Messages format.
func openAIToBedrockBody(body []byte) ([]byte, error) {
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bedrock: parse openai body: %w", err)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	brReq := bedrockAnthropicRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        maxTokens,
		Messages:         req.Messages,
	}
	return json.Marshal(brReq)
}

// bedrockToOpenAIResponse converts a Bedrock Claude response to OpenAI chat completion format.
func bedrockToOpenAIResponse(respBody []byte) ([]byte, *Usage, error) {
	var brResp bedrockAnthropicResponse
	if err := json.Unmarshal(respBody, &brResp); err != nil {
		return nil, nil, fmt.Errorf("bedrock: parse bedrock response: %w", err)
	}

	text := ""
	for _, c := range brResp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	openAIResp := map[string]interface{}{
		"id":     brResp.ID,
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     brResp.Usage.InputTokens,
			"completion_tokens": brResp.Usage.OutputTokens,
			"total_tokens":      brResp.Usage.InputTokens + brResp.Usage.OutputTokens,
		},
	}

	out, err := json.Marshal(openAIResp)
	if err != nil {
		return nil, nil, fmt.Errorf("bedrock: marshal openai response: %w", err)
	}

	usage := &Usage{
		PromptTokens:     brResp.Usage.InputTokens,
		CompletionTokens: brResp.Usage.OutputTokens,
	}
	return out, usage, nil
}

func (a *BedrockAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	if !strings.HasPrefix(a.modelID, "anthropic.") {
		return &ForwardResult{
			StatusCode: http.StatusNotImplemented,
			Body:       []byte(`{"error":"bedrock: only anthropic.* models are supported"}`),
		}, nil, nil
	}

	brBody, err := openAIToBedrockBody(body)
	if err != nil {
		return nil, nil, err
	}

	client, err := a.newClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	out, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(a.modelID),
		Body:        brBody,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("bedrock: invoke model: %w", err)
	}

	openAIBody, usage, err := bedrockToOpenAIResponse(out.Body)
	if err != nil {
		return nil, nil, err
	}

	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       openAIBody,
	}, usage, nil
}

// ForwardStream calls InvokeModelWithResponseStream and converts Bedrock SSE events
// into OpenAI-compatible SSE chunks delivered to onChunk.
func (a *BedrockAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	if !strings.HasPrefix(a.modelID, "anthropic.") {
		return fmt.Errorf("bedrock: only anthropic.* models are supported for streaming")
	}

	brBody, err := openAIToBedrockBody(body)
	if err != nil {
		return err
	}

	client, err := a.newClient(ctx)
	if err != nil {
		return err
	}

	out, err := client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(a.modelID),
		Body:        brBody,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("bedrock: invoke model stream: %w", err)
	}

	stream := out.GetStream()
	defer stream.Close()

	for event := range stream.Events() {
		chunk, ok := event.(*brtypes.ResponseStreamMemberChunk)
		if !ok {
			continue
		}

		// Parse the Bedrock stream chunk and convert to OpenAI SSE format.
		var bChunk bedrockStreamChunk
		if err := json.Unmarshal(chunk.Value.Bytes, &bChunk); err != nil {
			continue
		}
		if bChunk.Type != "content_block_delta" || bChunk.Delta.Type != "text_delta" {
			continue
		}

		openAIChunk := map[string]interface{}{
			"object": "chat.completion.chunk",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]string{
						"role":    "assistant",
						"content": bChunk.Delta.Text,
					},
					"finish_reason": nil,
				},
			},
		}
		chunkJSON, err := json.Marshal(openAIChunk)
		if err != nil {
			continue
		}
		line := append([]byte("data: "), chunkJSON...)
		line = append(line, '\n')
		if err := onChunk(line); err != nil {
			return err
		}
	}

	return stream.Err()
}

func (a *BedrockAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "以下是用户上传的文档内容：\n\n" + docText},
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func init() {
	Register("bedrock", func(baseURL, apiKey string) Adapter {
		return NewBedrockAdapter(baseURL, apiKey)
	})
}
