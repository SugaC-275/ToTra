package providers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

type fakeAdapter struct{}

func (a *fakeAdapter) Forward(_ context.Context, _ []byte) (*providers.ForwardResult, *providers.Usage, error) {
	return &providers.ForwardResult{StatusCode: 200}, &providers.Usage{}, nil
}
func (a *fakeAdapter) ForwardStream(_ context.Context, _ []byte, _ func([]byte) error) error {
	return nil
}
func (a *fakeAdapter) BuildFilePrompt(_, _, _ string) []byte { return []byte("{}") }

func TestRegistry_RegisterAndNew(t *testing.T) {
	providers.Register("test-fake-xyz", func(_, _ string) providers.Adapter { return &fakeAdapter{} })
	got, err := providers.New("test-fake-xyz", "http://x", "key")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestRegistry_New_UnknownProvider(t *testing.T) {
	_, err := providers.New("no-such-provider-abc", "http://x", "key")
	assert.Error(t, err)
}
