// alert_push_export_test.go exposes internal helpers for white-box unit tests.
// This file is compiled only during `go test`.
package services

import (
	"context"
	"net/http"
)

// SetHTTPClient replaces the HTTP client (for test servers).
func (s *AlertPushService) SetHTTPClient(c *http.Client) {
	s.httpClient = c
}

// ExportDeliverSlack exposes deliverSlack for unit testing.
func (s *AlertPushService) ExportDeliverSlack(ctx context.Context, webhookURL string, event AlertEvent) error {
	return s.deliverSlack(ctx, webhookURL, event)
}

// ExportDeliverWebhook exposes deliverWebhook for unit testing.
func (s *AlertPushService) ExportDeliverWebhook(ctx context.Context, url string, event AlertEvent) error {
	return s.deliverWebhook(ctx, url, event)
}

// ExportSlackColor exposes slackColor for unit testing.
func ExportSlackColor(severity string) string {
	return slackColor(severity)
}

// ExportMatchesEventType exposes matchesEventType for unit testing.
func ExportMatchesEventType(configured []string, eventType string) bool {
	return matchesEventType(configured, eventType)
}
