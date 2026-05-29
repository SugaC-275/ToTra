package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func init() {
	Register("watsonx", func(baseURL, apiKey string) Adapter {
		return NewWatsonxAdapter(baseURL, apiKey)
	})
}

const (
	watsonxIAMURL      = "https://iam.cloud.ibm.com/identity/token"
	watsonxAPIVersion  = "2023-05-29"
	watsonxTokenLeeway = 30 * time.Second
)

// WatsonxAdapter forwards requests to IBM WatsonX.
// baseURL format: "watsonx://{region}" e.g. "watsonx://us-south"
type WatsonxAdapter struct {
	region string
	apiKey string
	client *http.Client

	// iamURL and mlBaseURL are overrideable for tests.
	iamURL    string
	mlBaseURL string

	mu          sync.RWMutex
	cachedToken string
	tokenExpiry time.Time
}

func parseWatsonxBaseURL(baseURL string) (region string, err error) {
	path := strings.TrimPrefix(baseURL, "watsonx://")
	if path == baseURL {
		return "", fmt.Errorf("watsonx: baseURL must start with watsonx://, got %q", baseURL)
	}
	region = strings.TrimRight(path, "/")
	if region == "" {
		return "", fmt.Errorf("watsonx: baseURL must be watsonx://{region}, got %q", baseURL)
	}
	return region, nil
}

func NewWatsonxAdapter(baseURL, apiKey string) *WatsonxAdapter {
	region, err := parseWatsonxBaseURL(baseURL)
	if err != nil {
		return &WatsonxAdapter{apiKey: apiKey, client: &http.Client{}, iamURL: watsonxIAMURL}
	}
	return &WatsonxAdapter{
		region:    region,
		apiKey:    apiKey,
		client:    &http.Client{},
		iamURL:    watsonxIAMURL,
		mlBaseURL: fmt.Sprintf("https://%s.ml.cloud.ibm.com", region),
	}
}

// NewWatsonxAdapterForTest creates a WatsonxAdapter that contacts testIAMURL for
// token exchange and testMLURL for inference. Intended for unit tests only.
func NewWatsonxAdapterForTest(region, apiKey, testIAMURL, testMLURL string, client *http.Client) *WatsonxAdapter {
	return &WatsonxAdapter{
		region:    region,
		apiKey:    apiKey,
		client:    client,
		iamURL:    testIAMURL,
		mlBaseURL: testMLURL,
	}
}

// getIAMToken returns a valid IAM Bearer token, exchanging the API key when
// the cached token is absent or expired.
func (a *WatsonxAdapter) getIAMToken(ctx context.Context) (string, error) {
	a.mu.RLock()
	tok, expiry := a.cachedToken, a.tokenExpiry
	a.mu.RUnlock()

	if tok != "" && time.Now().Add(watsonxTokenLeeway).Before(expiry) {
		return tok, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock.
	if a.cachedToken != "" && time.Now().Add(watsonxTokenLeeway).Before(a.tokenExpiry) {
		return a.cachedToken, nil
	}

	formData := url.Values{}
	formData.Set("grant_type", "urn:ibm:params:oauth:grant-type:apikey")
	formData.Set("apikey", a.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.iamURL,
		strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("watsonx: create IAM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("watsonx: IAM token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("watsonx: read IAM response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watsonx: IAM token exchange status %d: %s", resp.StatusCode, body)
	}

	var iamResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &iamResp); err != nil || iamResp.AccessToken == "" {
		return "", fmt.Errorf("watsonx: parse IAM response: %w", err)
	}

	expiresIn := iamResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	a.cachedToken = iamResp.AccessToken
	a.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return a.cachedToken, nil
}

// generationURL returns the WatsonX text generation endpoint.
func (a *WatsonxAdapter) generationURL(stream bool) string {
	u := a.mlBaseURL + "/ml/v1/text/generation?version=" + watsonxAPIVersion
	if stream {
		u += "&stream=true"
	}
	return u
}

// translateRequest converts an OpenAI-format body to WatsonX generation format.
func translateWatsonxRequest(body []byte) ([]byte, error) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("watsonx: parse request body: %w", err)
	}

	// Use the last user message as the input text.
	input := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			input = req.Messages[i].Content
			break
		}
	}

	out := map[string]interface{}{
		"model_id": req.Model,
		"input":    input,
		"parameters": map[string]interface{}{
			"max_new_tokens": 500,
		},
	}
	return json.Marshal(out)
}

// translateWatsonxResponse converts a WatsonX generation response to OpenAI format.
func translateWatsonxResponse(body []byte) []byte {
	var resp struct {
		Results []struct {
			GeneratedText string `json:"generated_text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Results) == 0 {
		return body
	}

	out := map[string]interface{}{
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": resp.Results[0].GeneratedText,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
		},
	}
	b, _ := json.Marshal(out)
	return b
}

func (a *WatsonxAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	if a.region == "" {
		return nil, nil, fmt.Errorf("watsonx: invalid baseURL — region must be non-empty")
	}

	wxBody, err := translateWatsonxRequest(body)
	if err != nil {
		return nil, nil, err
	}

	token, err := a.getIAMToken(ctx)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.generationURL(false), bytes.NewReader(wxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("watsonx: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("watsonx: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("watsonx: read response: %w", err)
	}

	translated := translateWatsonxResponse(respBody)
	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: translated},
		extractOpenAIUsage(translated), nil
}

func (a *WatsonxAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	if a.region == "" {
		return fmt.Errorf("watsonx: invalid baseURL — region must be non-empty")
	}

	wxBody, err := translateWatsonxRequest(body)
	if err != nil {
		return err
	}

	token, err := a.getIAMToken(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.generationURL(true), bytes.NewReader(wxBody))
	if err != nil {
		return fmt.Errorf("watsonx: create stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("watsonx: stream forward: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("watsonx: stream upstream status %d: %s", resp.StatusCode, errBody)
	}

	return readSSEChunks(resp.Body, onChunk)
}

func (a *WatsonxAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
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
