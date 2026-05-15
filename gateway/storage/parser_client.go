package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"
)

type ParseResult struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

type ParserClient struct {
	baseURL string
	client  *http.Client
}

func NewParserClient(baseURL string) *ParserClient {
	return &ParserClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Parse sends a file to the parser microservice and returns extracted text.
// Returns an error containing "unsupported format" for 400 responses.
func (c *ParserClient) Parse(ctx context.Context, filename string, data []byte) (*ParseResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, fmt.Errorf("parser: create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("parser: write data: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", &buf)
	if err != nil {
		return nil, fmt.Errorf("parser: create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parser: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parser: read response: %w", err)
	}

	if resp.StatusCode == http.StatusBadRequest {
		return nil, fmt.Errorf("unsupported format")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("parser: status %d: %s", resp.StatusCode, respBody)
	}

	var result ParseResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parser: decode response: %w", err)
	}
	return &result, nil
}
