package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/yourorg/totra/gateway/storage"
)

// -- Template definitions (package-level vars, not hardcoded inline strings) --

var slackBudgetAlertTmpl = template.Must(template.New("slack_budget").Parse(`{
  "blocks": [
    {
      "type": "header",
      "text": { "type": "plain_text", "text": "ToTra Budget Alert", "emoji": false }
    },
    {
      "type": "section",
      "fields": [
        { "type": "mrkdwn", "text": "*Tenant:*\n{{.TenantName}}" },
        { "type": "mrkdwn", "text": "*Alert Level:*\n{{.AlertLevel}}" },
        { "type": "mrkdwn", "text": "*Usage:*\n{{printf "%.1f" .PercentUsed}}% of ${{printf "%.2f" .BudgetUSD}}" },
        { "type": "mrkdwn", "text": "*Projected Overage:*\n{{.ProjectedOverageDate}}" }
      ]
    },
    {
      "type": "actions",
      "elements": [
        {
          "type": "button",
          "text": { "type": "plain_text", "text": "View Cost Dashboard" },
          "url": "{{.DashboardURL}}"
        }
      ]
    }
  ]
}`))

var slackGenericAlertTmpl = template.Must(template.New("slack_generic").Parse(`{
  "blocks": [
    {
      "type": "header",
      "text": { "type": "plain_text", "text": "ToTra Alert: {{.EventType}}", "emoji": false }
    },
    {
      "type": "section",
      "text": { "type": "mrkdwn", "text": "*Tenant:* {{.TenantName}}\n*Time:* {{.Timestamp}}\n*Detail:* {{.Detail}}" }
    }
  ]
}`))

var pdAlertTmpl = template.Must(template.New("pd_alert").Parse(`{
  "routing_key": "{{.RoutingKey}}",
  "event_action": "trigger",
  "dedup_key": "totra-{{.TenantID}}-{{.AlertType}}",
  "payload": {
    "summary": "{{.Summary}}",
    "severity": "{{.Severity}}",
    "source": "ToTra Gateway",
    "timestamp": "{{.Timestamp}}",
    "custom_details": {
      "tenant_id": "{{.TenantID}}",
      "alert_type": "{{.AlertType}}",
      "detail": "{{.Detail}}"
    }
  },
  "links": [
    { "href": "{{.DashboardURL}}", "text": "Cost Dashboard" }
  ]
}`))

// -- Data structs for templates --

type slackBudgetData struct {
	TenantName           string
	AlertLevel           string
	PercentUsed          float64
	BudgetUSD            float64
	ProjectedOverageDate string
	DashboardURL         string
}

type slackGenericData struct {
	EventType string
	TenantName string
	Timestamp  string
	Detail     string
}

type pdAlertData struct {
	RoutingKey   string
	TenantID     string
	AlertType    string
	Summary      string
	Severity     string
	Timestamp    string
	Detail       string
	DashboardURL string
}

// -- NotificationDispatcher --

// NotificationDispatcher dispatches alerts to Slack, PagerDuty, or generic webhooks.
type NotificationDispatcher struct {
	store        *storage.WebhookStore
	client       *http.Client
	dashboardURL string // base URL for cost dashboard links, e.g. "https://app.totra.io"
}

// NewNotificationDispatcher creates a dispatcher. dashboardBase may be empty.
func NewNotificationDispatcher(store *storage.WebhookStore, dashboardBase string) *NotificationDispatcher {
	return &NotificationDispatcher{
		store:        store,
		client:       &http.Client{Timeout: 10 * time.Second},
		dashboardURL: strings.TrimRight(dashboardBase, "/"),
	}
}

// DispatchBudgetAlert sends a budget alert to all webhooks subscribed to "budget_alert".
// payload keys: tenant_name, alert_level (warning|critical), percent_used, budget_usd,
//               projected_overage_date (string, may be empty).
func (d *NotificationDispatcher) DispatchBudgetAlert(ctx context.Context, tenantID string, payload map[string]any) {
	d.dispatch(ctx, tenantID, "budget_alert", payload)
}

// Dispatch sends an event to all webhooks registered for that event type.
// Non-blocking: must be called in a goroutine.
func (d *NotificationDispatcher) dispatch(ctx context.Context, tenantID, eventType string, payload map[string]any) {
	cfgs, err := d.store.GetForEvent(ctx, tenantID, eventType)
	if err != nil {
		slog.Error("notification_dispatcher: get configs", "tenant", tenantID, "event", eventType, "err", err)
		return
	}
	for _, cfg := range cfgs {
		switch cfg.WebhookType {
		case "slack":
			d.sendSlack(ctx, cfg, tenantID, eventType, payload)
		case "pagerduty":
			d.sendPagerDuty(ctx, cfg, tenantID, eventType, payload)
		default:
			// Generic HMAC-signed webhook (existing behaviour).
			envelope := augmentPayload(payload, eventType, tenantID)
			body, err := json.Marshal(envelope)
			if err != nil {
				slog.Error("notification_dispatcher: marshal generic", "err", err)
				continue
			}
			d.sendGeneric(ctx, cfg, body)
		}
	}
}

func (d *NotificationDispatcher) sendSlack(ctx context.Context, cfg *storage.WebhookConfig, tenantID, eventType string, payload map[string]any) {
	var buf bytes.Buffer
	dashURL := d.dashboardURL + "/cost?tenant=" + tenantID

	if eventType == "budget_alert" {
		data := slackBudgetData{
			TenantName:           strVal(payload, "tenant_name", tenantID),
			AlertLevel:           strVal(payload, "alert_level", "warning"),
			PercentUsed:          floatVal(payload, "percent_used"),
			BudgetUSD:            floatVal(payload, "budget_usd"),
			ProjectedOverageDate: strVal(payload, "projected_overage_date", "unknown"),
			DashboardURL:         dashURL,
		}
		if err := slackBudgetAlertTmpl.Execute(&buf, data); err != nil {
			slog.Error("notification_dispatcher: slack budget tmpl", "err", err)
			return
		}
	} else {
		data := slackGenericData{
			EventType:  eventType,
			TenantName: strVal(payload, "tenant_name", tenantID),
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Detail:     fmt.Sprintf("%v", payload),
		}
		if err := slackGenericAlertTmpl.Execute(&buf, data); err != nil {
			slog.Error("notification_dispatcher: slack generic tmpl", "err", err)
			return
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, &buf)
	if err != nil {
		slog.Error("notification_dispatcher: slack build request", "url", cfg.URL, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("notification_dispatcher: slack send", "url", cfg.URL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("notification_dispatcher: slack non-2xx", "url", cfg.URL, "status", resp.StatusCode)
	}
}

func (d *NotificationDispatcher) sendPagerDuty(ctx context.Context, cfg *storage.WebhookConfig, tenantID, eventType string, payload map[string]any) {
	alertLevel := strVal(payload, "alert_level", "warning")
	severity := alertLevel // PagerDuty uses the same names: warning, critical
	if severity != "critical" {
		severity = "warning"
	}

	data := pdAlertData{
		RoutingKey:   cfg.Secret, // PD uses the integration key stored in secret field
		TenantID:     tenantID,
		AlertType:    eventType,
		Summary:      fmt.Sprintf("ToTra %s alert for tenant %s", eventType, tenantID),
		Severity:     severity,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Detail:       strVal(payload, "detail", fmt.Sprintf("%v", payload)),
		DashboardURL: d.dashboardURL + "/cost?tenant=" + tenantID,
	}

	var buf bytes.Buffer
	if err := pdAlertTmpl.Execute(&buf, data); err != nil {
		slog.Error("notification_dispatcher: pagerduty tmpl", "err", err)
		return
	}

	const pdEventsURL = "https://events.pagerduty.com/v2/enqueue"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pdEventsURL, &buf)
	if err != nil {
		slog.Error("notification_dispatcher: pagerduty build request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("notification_dispatcher: pagerduty send", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("notification_dispatcher: pagerduty non-2xx", "status", resp.StatusCode)
	}
}

func (d *NotificationDispatcher) sendGeneric(ctx context.Context, cfg *storage.WebhookConfig, body []byte) {
	sig := computeHMAC(cfg.Secret, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		slog.Error("notification_dispatcher: generic build request", "url", cfg.URL, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ToTra-Signature", "sha256="+sig)
	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("notification_dispatcher: generic send", "url", cfg.URL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("notification_dispatcher: generic non-2xx", "url", cfg.URL, "status", resp.StatusCode)
	}
}

func augmentPayload(payload map[string]any, eventType, tenantID string) map[string]any {
	out := make(map[string]any, len(payload)+3)
	for k, v := range payload {
		out[k] = v
	}
	out["event"] = eventType
	out["tenant_id"] = tenantID
	out["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	return out
}

func strVal(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func floatVal(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}
