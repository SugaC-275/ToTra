package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

var insightHTTPClient = &http.Client{Timeout: 30 * time.Second}

type KPISnapshotForInsight struct {
	UserName        string
	Month           string
	AIQScore        float64
	OSSScore        float64
	GTSScore        float64
	EfficiencyScore float64
	AnomalyFlagged  bool
}

type AIInsightsService struct {
	pool   *pgxpool.Pool
	apiKey string
}

func NewAIInsightsService(pool *pgxpool.Pool, apiKey string) *AIInsightsService {
	return &AIInsightsService{pool: pool, apiKey: apiKey}
}

func (s *AIInsightsService) GenerateInsight(ctx context.Context, userID, month string) (string, error) {
	if s.apiKey == "" {
		return "AI insights are not configured. Please set ANTHROPIC_API_KEY.", nil
	}

	var snap KPISnapshotForInsight
	snap.Month = month
	err := s.pool.QueryRow(ctx,
		`SELECT u.name, es.aiq_score, es.oss_score, es.gts_score,
		        es.efficiency_score, es.anomaly_flagged
		 FROM efficiency_snapshots es
		 JOIN users u ON u.id = es.user_id
		 WHERE es.user_id = $1 AND es.year_month = $2`,
		userID, month,
	).Scan(&snap.UserName, &snap.AIQScore, &snap.OSSScore, &snap.GTSScore,
		&snap.EfficiencyScore, &snap.AnomalyFlagged)
	if err != nil {
		return "No KPI data available for this month.", nil
	}

	if snap.AnomalyFlagged {
		return fmt.Sprintf("An anomaly was detected in %s's usage for %s. KPI scores have been zeroed out pending review.", snap.UserName, month), nil
	}

	prompt := BuildKPIPrompt(snap)
	return CallAnthropicAPI(anthropicAPIURL, s.apiKey, prompt)
}

// BuildKPIPrompt creates the Anthropic prompt from a KPI snapshot.
func BuildKPIPrompt(snap KPISnapshotForInsight) string {
	return fmt.Sprintf(
		`You are an AI performance coach. Analyze the following KPI scores for %s in %s and provide a concise 3-4 sentence insight summary. Focus on strengths and one specific area for improvement. Be encouraging but honest.

AIQ (AI Usage Quality): %.1f/100
OSS (Open Source Contribution): %.1f/100
GTS (Goal & Task Completion): %.1f/100
Overall Efficiency Score: %.1f/100

Provide the insight in plain text, no markdown formatting.`,
		snap.UserName, snap.Month,
		snap.AIQScore, snap.OSSScore, snap.GTSScore, snap.EfficiencyScore,
	)
}

// CallAnthropicAPI sends a prompt to the Anthropic Messages API and returns the response text.
// The apiURL parameter allows injection of a test server URL.
func CallAnthropicAPI(apiURL, apiKey, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := insightHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic API")
	}
	return result.Content[0].Text, nil
}
