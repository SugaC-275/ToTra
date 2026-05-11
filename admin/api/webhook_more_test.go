package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

const testEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func encryptForTest(t *testing.T, plaintext string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plaintext, testEncKey)
	require.NoError(t, err)
	return enc
}

type stubWebhookSvc struct {
	encryptedSecret string
	weights         map[string]float64
	cfgErr          error
	saveErr         error
}

func (s *stubWebhookSvc) GetWebhookConfig(_ context.Context, _, _ string) (string, map[string]float64, error) {
	return s.encryptedSecret, s.weights, s.cfgErr
}
func (s *stubWebhookSvc) MatchUser(_ context.Context, _ string, _ *services.ParsedEvent) (string, error) {
	return "", nil
}
func (s *stubWebhookSvc) SaveEvent(_ context.Context, _, _ string, _ *services.ParsedEvent, _ []byte) error {
	return s.saveErr
}

func setupWebhookTestApp(svc *stubWebhookSvc) *fiber.App {
	app := fiber.New()
	api.RegisterWebhookRoutes(app, svc, testEncKey)
	return app
}

func TestGitLabWebhook_MissingTenantID(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGitLabWebhook_NotConfigured(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{cfgErr: errors.New("not found")})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestGitLabWebhook_InvalidToken(t *testing.T) {
	enc := encryptForTest(t, "correct-token")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user_username":"alice","commits":[{"id":"abc123","author":{"name":"Alice"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "wrong-token")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestGitLabWebhook_PushHook_OK(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user_username":"alice","commits":[{"id":"abc123","author":{"name":"Alice"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestGitLabWebhook_MergeRequestHook_OK(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"user":{"username":"bob"},"object_attributes":{"action":"merged","iid":7,"title":"Fix login"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestGitLabWebhook_UnsupportedEvent_Skipped(t *testing.T) {
	enc := encryptForTest(t, "my-gl-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-gl-secret")
	req.Header.Set("X-Gitlab-Event", "Tag Push Hook")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "skipped", result["status"])
}

func TestConfluenceWebhook_MissingTenantID(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestConfluenceWebhook_NotConfigured(t *testing.T) {
	app := setupWebhookTestApp(&stubWebhookSvc{cfgErr: errors.New("not found")})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader([]byte(`{}`)))
	resp, _ := app.Test(req)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestConfluenceWebhook_InvalidSignature(t *testing.T) {
	enc := encryptForTest(t, "correct-secret")
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p1","title":"T","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Alice"}}}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", "sha256=invalidsig")
	req.Header.Set("X-Event-Key", "page_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestConfluenceWebhook_PageCreated_OK(t *testing.T) {
	secret := "conf-secret-123"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p1","title":"My Page","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Alice"}}}}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "page_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestConfluenceWebhook_PageUpdated_OK(t *testing.T) {
	secret := "conf-secret-456"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{"page":{"id":"p2","title":"Updated","createdBy":{"displayName":"Alice"},"version":{"by":{"displayName":"Bob"}}}}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "page_updated")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestConfluenceWebhook_UnsupportedEvent_Skipped(t *testing.T) {
	secret := "conf-secret-789"
	enc := encryptForTest(t, secret)
	app := setupWebhookTestApp(&stubWebhookSvc{encryptedSecret: enc, weights: map[string]float64{}})

	body := []byte(`{}`)
	sig := services.ComputeGitHubSig(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/confluence?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", "space_created")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "skipped", result["status"])
}
