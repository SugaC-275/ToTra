package services_test

import (
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
