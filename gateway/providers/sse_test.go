package providers

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSSEChunks_DeliverDataLines(t *testing.T) {
	input := "data: {\"delta\":\"hello\"}\ndata: {\"delta\":\" world\"}\n\n"
	var got []string
	err := readSSEChunks(strings.NewReader(input), func(chunk []byte) error {
		got = append(got, string(chunk))
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Contains(t, got[0], `"delta":"hello"`)
	assert.Contains(t, got[1], `"delta":" world"`)
}

func TestReadSSEChunks_SkipsDONE(t *testing.T) {
	input := "data: {\"delta\":\"hi\"}\ndata: [DONE]\n"
	var got []string
	err := readSSEChunks(strings.NewReader(input), func(chunk []byte) error {
		got = append(got, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Contains(t, got[0], `"delta":"hi"`)
}

func TestReadSSEChunks_SkipsEmptyLines(t *testing.T) {
	input := "\n\ndata: {\"a\":1}\n\n"
	var count int
	err := readSSEChunks(strings.NewReader(input), func(_ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestReadSSEChunks_SkipsNonDataLines(t *testing.T) {
	input := "event: content_block_delta\ndata: {\"text\":\"ok\"}\n"
	var got []string
	err := readSSEChunks(strings.NewReader(input), func(chunk []byte) error {
		got = append(got, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestReadSSEChunks_PropagatesCallbackError(t *testing.T) {
	input := "data: {\"a\":1}\n"
	want := errors.New("write failed")
	err := readSSEChunks(strings.NewReader(input), func(_ []byte) error {
		return want
	})
	assert.Equal(t, want, err)
}

func TestInjectStreamTrue_SetsField(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[]}`)
	out := injectStreamTrue(body)
	assert.Contains(t, string(out), `"stream":true`)
}

func TestInjectStreamTrue_OverwritesExistingFalse(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","stream":false}`)
	out := injectStreamTrue(body)
	assert.Contains(t, string(out), `"stream":true`)
	assert.NotContains(t, string(out), `"stream":false`)
}

func TestInjectStreamTrue_InvalidJSONPassthrough(t *testing.T) {
	body := []byte(`not json`)
	out := injectStreamTrue(body)
	assert.Equal(t, body, out)
}
