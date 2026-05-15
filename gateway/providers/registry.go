package providers

import (
	"context"
	"fmt"
)

// Adapter is the interface every LLM provider must implement.
type Adapter interface {
	Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error)
	BuildFilePrompt(model, docText, userMessage string) []byte
}

// AdapterFactory creates an Adapter for a given base URL and API key.
type AdapterFactory func(baseURL, apiKey string) Adapter

var registry = map[string]AdapterFactory{}

// Register adds a provider factory to the registry. Called from init() in each
// adapter file so new providers can be added without touching main.go.
func Register(providerType string, factory AdapterFactory) {
	registry[providerType] = factory
}

// New looks up a provider by type and returns a fresh Adapter.
func New(providerType, baseURL, apiKey string) (Adapter, error) {
	factory, ok := registry[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %q", providerType)
	}
	return factory(baseURL, apiKey), nil
}
