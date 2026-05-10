package services_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestVerifyGitHubSignature(t *testing.T) {
	body := []byte(`{"action":"closed"}`)
	secret := "mysecret"
	// valid: compute the correct sig
	assert.True(t, services.VerifyGitHubSignature(body, secret, services.ComputeGitHubSig(body, secret)))
	// invalid header
	assert.False(t, services.VerifyGitHubSignature(body, secret, "sha256=invalid"))
	// missing prefix
	assert.False(t, services.VerifyGitHubSignature(body, secret, "nope"))
}

func TestDefaultWeight(t *testing.T) {
	weights := map[string]float64{}
	assert.Equal(t, 5.0, services.EventWeight("github", "pr_merged", weights))
	assert.Equal(t, 1.0, services.EventWeight("github", "push", weights))
	assert.Equal(t, 3.0, services.EventWeight("jira", "issue_closed", weights))
	assert.Equal(t, 2.0, services.EventWeight("feishu", "task_completed", weights))
	assert.Equal(t, 2.0, services.EventWeight("feishu", "doc_created", weights))
	assert.Equal(t, 2.0, services.EventWeight("dingtalk", "task_completed", weights))
}

func TestParseGitHubPREvent(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {"merged": true, "title": "fix: login bug", "number": 42},
		"sender": {"login": "alice-gh", "email": "alice@acme.com"},
		"pusher": {"email": "alice@acme.com"}
	}`)
	event, err := services.ParseGitHubEvent("pull_request", payload)
	assert.NoError(t, err)
	assert.Equal(t, "pr_merged", event.EventType)
	assert.Equal(t, "fix: login bug", event.Title)
	assert.Equal(t, "alice-gh", event.SenderLogin)
	assert.Equal(t, "alice@acme.com", event.SenderEmail)
	assert.Equal(t, "42", event.ExternalEventID)
}

func TestParseGitHubPRNotMerged(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {"merged": false, "title": "wip", "number": 10},
		"sender": {"login": "bob", "email": "bob@acme.com"}
	}`)
	_, err := services.ParseGitHubEvent("pull_request", payload)
	assert.Error(t, err)
}

func TestParseGitHubEvent_LinesChanged(t *testing.T) {
	body := `{
		"action": "closed",
		"pull_request": {
			"merged": true,
			"title": "Add feature",
			"number": 42,
			"additions": 300,
			"deletions": 50,
			"closed_at": "2026-05-10T10:00:00Z"
		},
		"sender": {"login": "alice", "email": "alice@acme.com"}
	}`
	event, err := services.ParseGitHubEvent("pull_request", []byte(body))
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if event.LinesChanged != 350 {
		t.Errorf("LinesChanged = %d, want 350", event.LinesChanged)
	}
}

func TestParseJiraEvent_CycleTime(t *testing.T) {
	body := `{
		"webhookEvent": "jira:issue_updated",
		"issue": {
			"id": "10001", "key": "PROJ-1",
			"fields": {
				"summary": "Fix bug",
				"status": {"name": "Done"},
				"created": "2026-05-03T09:00:00.000Z",
				"resolutiondate": "2026-05-08T09:00:00.000Z",
				"assignee": {"emailAddress": "alice@acme.com", "accountId": "acc1"}
			}
		}
	}`
	event, err := services.ParseJiraEvent([]byte(body))
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	// 5 days * 24h = 120h
	if math.Abs(event.CycleTimeHours-120) > 1 {
		t.Errorf("CycleTimeHours = %v, want 120", event.CycleTimeHours)
	}
}
