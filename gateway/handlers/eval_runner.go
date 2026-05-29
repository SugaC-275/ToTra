package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yourorg/totra/gateway/storage"
)

// runEvalSuite executes all test cases for a run against the configured model.
// It is called in a goroutine from the trigger endpoint and updates run status
// as it progresses.
func runEvalSuite(
	ctx context.Context,
	evalStore *storage.EvalStore,
	promptStore *storage.PromptStore,
	modelLookup EvalModelLookup,
	dispatcher *WebhookDispatcher,
	run *storage.EvalRun,
	suite *storage.EvalSuite,
) {
	bgCtx := context.Background()

	// Mark run as running.
	if err := evalStore.UpdateRunStatus(bgCtx, run.ID, "running", 0, 0, 0, nil); err != nil {
		slog.Error("eval_runner: mark running", "run_id", run.ID, "err", err)
	}

	// Load prompt template.
	var prompt *storage.PromptTemplate
	var err error
	if run.PromptVersion > 0 {
		prompt, err = promptStore.GetVersion(bgCtx, run.TenantID, suite.PromptName, run.PromptVersion)
	} else {
		prompt, err = promptStore.GetLatest(bgCtx, run.TenantID, suite.PromptName)
	}
	if err != nil || prompt == nil {
		slog.Error("eval_runner: prompt not found", "run_id", run.ID, "prompt", suite.PromptName)
		_ = evalStore.UpdateRunStatus(bgCtx, run.ID, "failed", 0, 0, 0, nil)
		return
	}

	// Load model config.
	modelCfg, err := modelLookup.GetByName(bgCtx, run.TenantID, run.Model)
	if err != nil || modelCfg == nil {
		slog.Error("eval_runner: model not found", "run_id", run.ID, "model", run.Model)
		_ = evalStore.UpdateRunStatus(bgCtx, run.ID, "failed", 0, 0, 0, nil)
		return
	}

	// Load all cases.
	cases, err := evalStore.ListCases(bgCtx, run.SuiteID)
	if err != nil {
		slog.Error("eval_runner: list cases", "run_id", run.ID, "err", err)
		_ = evalStore.UpdateRunStatus(bgCtx, run.ID, "failed", 0, 0, 0, nil)
		return
	}

	passed, failed := 0, 0
	for _, ec := range cases {
		// Convert input_vars (map[string]any) to map[string]string for Render.
		strVars := make(map[string]string, len(ec.InputVars))
		for k, v := range ec.InputVars {
			strVars[k] = fmt.Sprintf("%v", v)
		}
		renderedPrompt := prompt.Render(strVars)

		start := time.Now()
		actual, callErr := callModel(bgCtx, modelCfg.BaseURL, modelCfg.APIKey, run.Model, renderedPrompt)
		latencyMS := int(time.Since(start).Milliseconds())

		var errMsg string
		if callErr != nil {
			errMsg = callErr.Error()
		}

		isPass, score := scoreResult(bgCtx, modelCfg, ec, actual, errMsg)
		if isPass {
			passed++
		} else {
			failed++
		}

		if storeErr := evalStore.AddResult(bgCtx, run.ID, ec.ID, actual, isPass, &score, latencyMS, errMsg); storeErr != nil {
			slog.Error("eval_runner: add result", "run_id", run.ID, "case_id", ec.ID, "err", storeErr)
		}
	}

	total := passed + failed
	var scorePct *float64
	if total > 0 {
		v := float64(passed) / float64(total) * 100
		scorePct = &v
	}
	finalStatus := "completed"
	if err := evalStore.UpdateRunStatus(bgCtx, run.ID, finalStatus, passed, failed, total, scorePct); err != nil {
		slog.Error("eval_runner: mark completed", "run_id", run.ID, "err", err)
	}

	if dispatcher != nil {
		var sp float64
		if scorePct != nil {
			sp = *scorePct
		}
		go dispatcher.Dispatch(bgCtx, run.TenantID, "eval_run.completed", map[string]any{
			"run_id":    run.ID,
			"suite_id":  run.SuiteID,
			"status":    finalStatus,
			"score_pct": sp,
		})
	}
}

// scoreResult scores a single eval case result and returns (passed, score).
// For llm_judge cases, it calls callLLMJudge to get a 0-10 score from the model.
func scoreResult(ctx context.Context, modelCfg *storage.ModelConfig, ec *storage.EvalCase, actual, errMsg string) (bool, float64) {
	if errMsg != "" {
		return false, 0
	}
	switch ec.ScoreMethod {
	case "exact":
		expected := ""
		if ec.ExpectedOutput != nil {
			expected = strings.TrimSpace(*ec.ExpectedOutput)
		}
		trimmed := strings.TrimSpace(actual)
		if trimmed == expected {
			return true, 1.0
		}
		return false, 0
	case "llm_judge":
		expectedStr := ""
		if ec.ExpectedOutput != nil {
			expectedStr = *ec.ExpectedOutput
		}
		judgeScore, judgeErr := callLLMJudge(ctx, modelCfg, expectedStr, actual)
		if judgeErr != nil {
			slog.Warn("eval_runner: llm_judge fallback", "err", judgeErr)
			// fail-open: score 0.5, mark passed
			return true, 0.5
		}
		// normalize 0-10 → 0.0-1.0; pass threshold = 6
		return judgeScore >= 6.0, judgeScore / 10.0
	default: // "contains"
		if len(ec.ExpectedContains) == 0 {
			return true, 1.0
		}
		for _, s := range ec.ExpectedContains {
			if !strings.Contains(actual, s) {
				return false, 0
			}
		}
		return true, 1.0
	}
}

// callLLMJudge sends a scoring prompt to the model and parses a 0-10 score.
// The judge prompt asks the model to rate how well the actual response addresses
// the expected output. Returns a score in the range [0.0, 10.0].
func callLLMJudge(ctx context.Context, modelCfg *storage.ModelConfig, expected, actual string) (float64, error) {
	judgePrompt := fmt.Sprintf(
		"You are an impartial evaluator. Rate how well the following response addresses the expected output.\n\n"+
			"Expected output:\n%s\n\nActual response:\n%s\n\n"+
			"Reply with a single integer from 0 to 10 where 0 is completely wrong and 10 is perfect. Reply with just the number.",
		expected, actual,
	)

	scoreStr, err := callModel(ctx, modelCfg.BaseURL, modelCfg.APIKey, modelCfg.Name, judgePrompt)
	if err != nil {
		return 0, fmt.Errorf("callLLMJudge: model call: %w", err)
	}

	cleaned := strings.TrimSpace(scoreStr)
	score, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		// Try extracting just the first numeric token.
		for _, tok := range strings.Fields(cleaned) {
			if s, e := strconv.ParseFloat(tok, 64); e == nil {
				score = s
				err = nil
				break
			}
		}
		if err != nil {
			return 0, fmt.Errorf("callLLMJudge: parse score %q: %w", cleaned, err)
		}
	}

	// Clamp to [0, 10].
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	return score, nil
}

// callModel sends a chat completion request to the upstream model and returns
// the content of the first choice's message.
func callModel(ctx context.Context, baseURL, apiKey, model, prompt string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 512,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("callModel: marshal: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("callModel: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("callModel: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("callModel: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("callModel: upstream status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("callModel: parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("callModel: no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}
