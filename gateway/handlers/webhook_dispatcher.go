package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/yourorg/totra/gateway/storage"
)

// WebhookDispatcher sends signed event payloads to registered webhook endpoints.
type WebhookDispatcher struct {
	store  *storage.WebhookStore
	client *http.Client
}

// NewWebhookDispatcher creates a dispatcher backed by the given store.
func NewWebhookDispatcher(store *storage.WebhookStore) *WebhookDispatcher {
	return &WebhookDispatcher{
		store:  store,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Dispatch sends an event to all webhooks registered for that event type.
// Non-blocking: must be called in a goroutine.
// Signs the payload with HMAC-SHA256; sets X-ToTra-Signature: "sha256=<hex>".
func (d *WebhookDispatcher) Dispatch(ctx context.Context, tenantID, eventType string, payload map[string]any) {
	cfgs, err := d.store.GetForEvent(ctx, tenantID, eventType)
	if err != nil {
		slog.Error("webhook_dispatcher: get configs", "tenant", tenantID, "event", eventType, "err", err)
		return
	}
	if len(cfgs) == 0 {
		return
	}

	// Augment payload with standard fields.
	envelope := make(map[string]any, len(payload)+3)
	for k, v := range payload {
		envelope[k] = v
	}
	envelope["event"] = eventType
	envelope["tenant_id"] = tenantID
	envelope["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	body, err := json.Marshal(envelope)
	if err != nil {
		slog.Error("webhook_dispatcher: marshal payload", "err", err)
		return
	}

	for _, cfg := range cfgs {
		d.send(ctx, cfg, body)
	}
}

// send signs and POSTs to a single webhook URL. Logs on error; no retry.
func (d *WebhookDispatcher) send(ctx context.Context, cfg *storage.WebhookConfig, body []byte) {
	sig := computeHMAC(cfg.Secret, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		slog.Error("webhook_dispatcher: build request", "url", cfg.URL, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ToTra-Signature", "sha256="+sig)

	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("webhook_dispatcher: send", "url", cfg.URL, "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("webhook_dispatcher: non-2xx response",
			"url", cfg.URL,
			"status", resp.StatusCode,
		)
	}
}

// computeHMAC returns the hex-encoded HMAC-SHA256 of body using secret.
func computeHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// sendTestPing dispatches a single test ping to one webhook config by ID.
func (d *WebhookDispatcher) sendTestPing(ctx context.Context, tenantID, webhookID string) error {
	cfg, err := d.store.GetByID(ctx, tenantID, webhookID)
	if err != nil {
		return fmt.Errorf("webhook not found: %w", err)
	}

	payload := map[string]any{
		"event":     "ping",
		"tenant_id": tenantID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	d.send(ctx, cfg, body)
	return nil
}
