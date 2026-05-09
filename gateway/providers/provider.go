package providers

import "net/http"

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type ForwardResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
