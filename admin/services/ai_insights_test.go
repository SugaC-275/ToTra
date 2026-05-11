package services_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

func TestBuildKPIPrompt(t *testing.T) {
	snap := services.KPISnapshotForInsight{
		UserName:        "Alice",
		Month:           "2026-05",
		AIQScore:        85.5,
		OSSScore:        72.0,
		GTSScore:        90.0,
		EfficiencyScore: 82.5,
		AnomalyFlagged:  false,
	}
	prompt := services.BuildKPIPrompt(snap)
	assert.Contains(t, prompt, "Alice")
	assert.Contains(t, prompt, "2026-05")
	assert.Contains(t, prompt, "85.5")
	assert.Contains(t, prompt, "AIQ")
}

func TestCallAnthropicAPI_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Great performance this month!"},
			},
		})
	}))
	defer srv.Close()

	result, err := services.CallAnthropicAPI(srv.URL, "test-key", "Analyze this KPI data")
	require.NoError(t, err)
	assert.Equal(t, "Great performance this month!", result)
}

func TestCallAnthropicAPI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := services.CallAnthropicAPI(srv.URL, "bad-key", "prompt")
	assert.Error(t, err)
}

func TestCallAnthropicAPI_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{},
		})
	}))
	defer srv.Close()

	_, err := services.CallAnthropicAPI(srv.URL, "key", "prompt")
	assert.Error(t, err)
}
