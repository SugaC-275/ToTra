package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ParsedEvent is the normalised form of any incoming webhook event.
type ParsedEvent struct {
	Platform        string
	EventType       string
	ExternalEventID string
	Title           string
	Weight          float64
	OccurredAt      time.Time
	SenderLogin     string
	SenderEmail     string
	// Quality signals
	LinesChanged   int     // GitHub: additions + deletions
	ReopenedCount  int     // GitHub: 1 if action=="reopened"
	CycleTimeHours float64 // Jira: hours from created to resolved
}

var defaultWeights = map[string]map[string]float64{
	"github":   {"pr_merged": 5, "push": 1},
	"jira":     {"issue_closed": 3},
	"feishu":   {"task_completed": 2, "doc_created": 2},
	"dingtalk": {"task_completed": 2},
}

// EventWeight returns the weight for a platform+eventType, checking custom weights first.
func EventWeight(platform, eventType string, custom map[string]float64) float64 {
	key := platform + "." + eventType
	if w, ok := custom[key]; ok {
		return w
	}
	if pm, ok := defaultWeights[platform]; ok {
		if w, ok := pm[eventType]; ok {
			return w
		}
	}
	return 0
}

// ComputeGitHubSig returns the sha256= prefixed HMAC for testing.
func ComputeGitHubSig(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyGitHubSignature checks X-Hub-Signature-256.
func VerifyGitHubSignature(body []byte, secret, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	expected := ComputeGitHubSig(body, secret)
	return hmac.Equal([]byte(expected), []byte(header))
}

// VerifyJiraSignature checks X-Hub-Signature (same format as GitHub).
func VerifyJiraSignature(body []byte, secret, header string) bool {
	return VerifyGitHubSignature(body, secret, header)
}

// VerifyFeishuSignature checks X-Lark-Signature: HMAC-SHA256(secret, timestamp+body).
func VerifyFeishuSignature(body []byte, secret, timestamp, header string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(header))
}

// ParseGitHubEvent parses a GitHub webhook payload into a ParsedEvent.
// Returns error if the event should be skipped (e.g. PR closed but not merged).
func ParseGitHubEvent(eventType string, body []byte) (*ParsedEvent, error) {
	var raw struct {
		Action      string `json:"action"`
		PullRequest *struct {
			Merged    bool   `json:"merged"`
			Title     string `json:"title"`
			Number    int    `json:"number"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
			ClosedAt  string `json:"closed_at"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
			Email string `json:"email"`
		} `json:"sender"`
		Pusher struct {
			Email string `json:"email"`
		} `json:"pusher"`
		HeadCommit *struct {
			ID string `json:"id"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	switch eventType {
	case "pull_request":
		if raw.PullRequest == nil || !raw.PullRequest.Merged {
			return nil, errors.New("PR not merged, skipping")
		}
		email := raw.Pusher.Email
		if email == "" {
			email = raw.Sender.Email
		}
		reopenedCount := 0
		if raw.Action == "reopened" {
			reopenedCount = 1
		}
		return &ParsedEvent{
			Platform:        "github",
			EventType:       "pr_merged",
			ExternalEventID: fmt.Sprintf("%d", raw.PullRequest.Number),
			Title:           raw.PullRequest.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Sender.Login,
			SenderEmail:     email,
			LinesChanged:    raw.PullRequest.Additions + raw.PullRequest.Deletions,
			ReopenedCount:   reopenedCount,
		}, nil
	case "push":
		if raw.HeadCommit == nil {
			return nil, errors.New("push event has no head commit, skipping")
		}
		return &ParsedEvent{
			Platform:        "github",
			EventType:       "push",
			ExternalEventID: raw.HeadCommit.ID,
			Title:           "push: " + raw.HeadCommit.ID[:8],
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Sender.Login,
			SenderEmail:     raw.Pusher.Email,
		}, nil
	}
	return nil, fmt.Errorf("unsupported github event: %s", eventType)
}

// ParseJiraEvent parses a Jira webhook payload.
func ParseJiraEvent(body []byte) (*ParsedEvent, error) {
	var raw struct {
		WebhookEvent string `json:"webhookEvent"`
		Issue        *struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Fields struct {
				Summary     string   `json:"summary"`
				StoryPoints *float64 `json:"story_points"`
				Status      struct {
					Name string `json:"name"`
				} `json:"status"`
				Created        string `json:"created"`
				ResolutionDate string `json:"resolutiondate"`
				Assignee *struct {
					EmailAddress string `json:"emailAddress"`
					AccountID    string `json:"accountId"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.Issue == nil {
		return nil, errors.New("no issue in payload")
	}
	if !strings.EqualFold(raw.Issue.Fields.Status.Name, "done") &&
		!strings.EqualFold(raw.Issue.Fields.Status.Name, "closed") {
		return nil, fmt.Errorf("issue status %q not done, skipping", raw.Issue.Fields.Status.Name)
	}
	weight := 3.0
	if raw.Issue.Fields.StoryPoints != nil {
		weight = *raw.Issue.Fields.StoryPoints
	}
	email, login := "", ""
	if raw.Issue.Fields.Assignee != nil {
		email = raw.Issue.Fields.Assignee.EmailAddress
		login = raw.Issue.Fields.Assignee.AccountID
	}
	var cycleTimeHours float64
	if raw.Issue.Fields.Created != "" && raw.Issue.Fields.ResolutionDate != "" {
		created, err1 := time.Parse(time.RFC3339, raw.Issue.Fields.Created)
		if err1 != nil {
			created, err1 = time.Parse("2006-01-02T15:04:05.000Z", raw.Issue.Fields.Created)
		}
		resolved, err2 := time.Parse(time.RFC3339, raw.Issue.Fields.ResolutionDate)
		if err2 != nil {
			resolved, err2 = time.Parse("2006-01-02T15:04:05.000Z", raw.Issue.Fields.ResolutionDate)
		}
		if err1 == nil && err2 == nil && resolved.After(created) {
			cycleTimeHours = resolved.Sub(created).Hours()
		}
	}
	return &ParsedEvent{
		Platform:        "jira",
		EventType:       "issue_closed",
		ExternalEventID: raw.Issue.ID,
		Title:           raw.Issue.Key + ": " + raw.Issue.Fields.Summary,
		Weight:          weight,
		OccurredAt:      time.Now().UTC(),
		SenderLogin:     login,
		SenderEmail:     email,
		CycleTimeHours:  cycleTimeHours,
	}, nil
}

// ParseFeishuEvent parses a Feishu webhook payload.
func ParseFeishuEvent(body []byte) (*ParsedEvent, error) {
	var raw struct {
		Event struct {
			EventType string `json:"event_type"`
			Task      *struct {
				TaskID   string `json:"task_id"`
				Summary  string `json:"summary"`
				Assignee *struct {
					OpenID string `json:"open_id"`
				} `json:"assignee"`
			} `json:"task"`
			Doc *struct {
				DocToken string `json:"doc_token"`
				Title    string `json:"title"`
				Creator  struct {
					OpenID string `json:"open_id"`
				} `json:"creator"`
			} `json:"doc"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	switch raw.Event.EventType {
	case "task.completed":
		if raw.Event.Task == nil {
			return nil, errors.New("no task in payload")
		}
		login := ""
		if raw.Event.Task.Assignee != nil {
			login = raw.Event.Task.Assignee.OpenID
		}
		return &ParsedEvent{
			Platform:        "feishu",
			EventType:       "task_completed",
			ExternalEventID: raw.Event.Task.TaskID,
			Title:           raw.Event.Task.Summary,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     login,
		}, nil
	case "docs.created":
		if raw.Event.Doc == nil {
			return nil, errors.New("no doc in payload")
		}
		return &ParsedEvent{
			Platform:        "feishu",
			EventType:       "doc_created",
			ExternalEventID: raw.Event.Doc.DocToken,
			Title:           raw.Event.Doc.Title,
			OccurredAt:      time.Now().UTC(),
			SenderLogin:     raw.Event.Doc.Creator.OpenID,
		}, nil
	}
	return nil, fmt.Errorf("unsupported feishu event: %s", raw.Event.EventType)
}

// WebhookService handles output event storage and user matching.
type WebhookService struct {
	pool *pgxpool.Pool
}

// NewWebhookService creates a new WebhookService.
func NewWebhookService(pool *pgxpool.Pool) *WebhookService {
	return &WebhookService{pool: pool}
}

// GetWebhookConfig retrieves the encrypted webhook secret and weights for a platform.
func (s *WebhookService) GetWebhookConfig(ctx context.Context, tenantID, platform string) (encryptedSecret string, weights map[string]float64, err error) {
	var weightsJSON []byte
	err = s.pool.QueryRow(ctx,
		`SELECT webhook_secret_encrypted, event_weights FROM webhook_configs
		 WHERE tenant_id=$1 AND platform=$2 AND is_active=true`,
		tenantID, platform,
	).Scan(&encryptedSecret, &weightsJSON)
	if err != nil {
		return "", nil, err
	}
	weights = map[string]float64{}
	json.Unmarshal(weightsJSON, &weights) //nolint:errcheck
	return encryptedSecret, weights, nil
}

// MatchUser resolves a ParsedEvent to a ToTra user_id using the 3-step chain.
// Returns empty string if unmatched.
func (s *WebhookService) MatchUser(ctx context.Context, tenantID string, event *ParsedEvent) (string, error) {
	if event.SenderEmail != "" {
		var userID string
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM users WHERE tenant_id=$1 AND email=$2 AND is_active=true`,
			tenantID, event.SenderEmail,
		).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}
	if event.SenderLogin != "" {
		var userID string
		err := s.pool.QueryRow(ctx,
			`SELECT user_id FROM user_integrations
			 WHERE tenant_id=$1 AND platform=$2 AND external_id=$3 AND user_id IS NOT NULL`,
			tenantID, event.Platform, event.SenderLogin,
		).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}
	return "", nil
}

// SaveEvent stores an output_event (userID may be "" → NULL).
func (s *WebhookService) SaveEvent(ctx context.Context, tenantID, userID string, event *ParsedEvent, rawPayload []byte) error {
	var uid *string
	if userID != "" {
		uid = &userID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO output_events
		 (id,tenant_id,user_id,platform,event_type,external_event_id,title,weight,
		  lines_changed,cycle_time_hours,reopened_count,occurred_at,raw_payload)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 ON CONFLICT (platform,external_event_id) DO NOTHING`,
		uuid.New().String(), tenantID, uid,
		event.Platform, event.EventType, event.ExternalEventID,
		event.Title, event.Weight,
		event.LinesChanged, event.CycleTimeHours, event.ReopenedCount,
		event.OccurredAt, rawPayload,
	)
	return err
}
