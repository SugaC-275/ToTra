package services_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

const testSIEMEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestSIEMConfigService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMConfigService(nil, testSIEMEncKey)
	assert.NotNil(t, svc)
}

func TestSIEMConfig_Fields(t *testing.T) {
	c := services.SIEMConfig{
		ID:          "abc-123",
		TenantID:    "acme",
		Name:        "Splunk",
		EndpointURL: "https://splunk.example.com/hec",
		EventTypes:  []string{"pii_violation", "policy_block"},
		IsActive:    true,
		CreatedAt:   time.Now(),
	}
	assert.Equal(t, "abc-123", c.ID)
	assert.Contains(t, c.EventTypes, "pii_violation")
	assert.True(t, c.IsActive)
}

func TestSIEMDeliveryService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMDeliveryService(nil, testSIEMEncKey)
	assert.NotNil(t, svc)
}

func TestDeliverToEndpoint_OK(t *testing.T) {
	var receivedAuth, receivedContentType string
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := services.DeliverToEndpoint(srv.URL, "my-api-key", map[string]any{
		"source":     "totra",
		"event_type": "pii_violation",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-api-key", receivedAuth)
	assert.Equal(t, "application/json", receivedContentType)
	assert.Contains(t, string(receivedBody), "pii_violation")
}

func TestDeliverToEndpoint_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := services.DeliverToEndpoint(srv.URL, "key", map[string]any{"event_type": "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestDeliverToEndpoint_BadURL(t *testing.T) {
	err := services.DeliverToEndpoint("http://127.0.0.1:0", "key", map[string]any{})
	assert.Error(t, err)
}
