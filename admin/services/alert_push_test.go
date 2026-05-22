package services_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourorg/totra/admin/services"
)

// newTestPushService builds an AlertPushService with a custom httpClient pointing at srv.
func newTestPushService(srv *httptest.Server) *services.AlertPushService {
	svc := services.NewAlertPushService(nil) // nil pool — DB not used in delivery unit tests
	svc.SetHTTPClient(srv.Client())
	return svc
}

func sampleEvent() services.AlertEvent {
	return services.AlertEvent{
		TenantID:  "tenant-abc",
		EventType: "budget_exceeded",
		Title:     "Budget Alert",
		Message:   "Tenant tenant-abc has exceeded their monthly budget.",
		Severity:  "critical",
		Timestamp: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Metadata:  map[string]any{"threshold_pct": 100},
	}
}

// ---------------------------------------------------------------------------
// deliverSlack
// ---------------------------------------------------------------------------

func TestDeliverSlack_SendsCorrectPayload(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("want Content-Type application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestPushService(srv)
	event := sampleEvent()

	if err := svc.ExportDeliverSlack(context.Background(), srv.URL, event); err != nil {
		t.Fatalf("deliverSlack: %v", err)
	}

	if received["text"] == nil {
		t.Fatal("expected 'text' field in Slack payload")
	}
	attachments, ok := received["attachments"].([]any)
	if !ok || len(attachments) == 0 {
		t.Fatal("expected non-empty 'attachments' in Slack payload")
	}
	att := attachments[0].(map[string]any)
	if att["color"] != "danger" {
		t.Errorf("want color=danger for critical, got %v", att["color"])
	}
	if att["title"] != event.Title {
		t.Errorf("want title=%q, got %v", event.Title, att["title"])
	}
}

func TestDeliverSlack_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := newTestPushService(srv)
	err := svc.ExportDeliverSlack(context.Background(), srv.URL, sampleEvent())
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// deliverWebhook
// ---------------------------------------------------------------------------

func TestDeliverWebhook_SendsFullPayload(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received) //nolint:errcheck
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestPushService(srv)
	event := sampleEvent()

	if err := svc.ExportDeliverWebhook(context.Background(), srv.URL, event); err != nil {
		t.Fatalf("deliverWebhook: %v", err)
	}

	for _, field := range []string{"tenant_id", "event_type", "title", "message", "severity", "timestamp"} {
		if received[field] == nil {
			t.Errorf("missing field %q in webhook payload", field)
		}
	}
	if received["tenant_id"] != event.TenantID {
		t.Errorf("want tenant_id=%q, got %v", event.TenantID, received["tenant_id"])
	}
	if received["event_type"] != event.EventType {
		t.Errorf("want event_type=%q, got %v", event.EventType, received["event_type"])
	}
}

func TestDeliverWebhook_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	svc := newTestPushService(srv)
	err := svc.ExportDeliverWebhook(context.Background(), srv.URL, sampleEvent())
	if err == nil {
		t.Fatal("expected error on 502 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// slackColor helper (via ExportSlackColor)
// ---------------------------------------------------------------------------

func TestSlackColor(t *testing.T) {
	cases := []struct{ severity, want string }{
		{"critical", "danger"},
		{"warning", "warning"},
		{"info", "good"},
		{"", "good"},
	}
	for _, tc := range cases {
		got := services.ExportSlackColor(tc.severity)
		if got != tc.want {
			t.Errorf("slackColor(%q): want %q, got %q", tc.severity, tc.want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// matchesEventType helper (via ExportMatchesEventType)
// ---------------------------------------------------------------------------

func TestMatchesEventType(t *testing.T) {
	configured := []string{"budget_exceeded", "pii_spike"}
	if !services.ExportMatchesEventType(configured, "budget_exceeded") {
		t.Error("expected match for 'budget_exceeded'")
	}
	if services.ExportMatchesEventType(configured, "compliance_violation") {
		t.Error("expected no match for 'compliance_violation'")
	}
}
