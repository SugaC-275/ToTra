package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EvalSuite struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id,omitempty"`
	Name       string    `json:"name"`
	PromptName string    `json:"prompt_name"`
	CreatedAt  time.Time `json:"created_at"`
}

type EvalCase struct {
	ID              string         `json:"id"`
	SuiteID         string         `json:"suite_id"`
	InputVars       map[string]any `json:"input_vars"`
	ExpectedOutput  *string        `json:"expected_output,omitempty"`
	ExpectedContains []string      `json:"expected_contains,omitempty"`
	ScoreMethod     string         `json:"score_method"`
	CreatedAt       time.Time      `json:"created_at"`
}

type EvalRun struct {
	ID            string     `json:"id"`
	SuiteID       string     `json:"suite_id"`
	TenantID      string     `json:"tenant_id,omitempty"`
	PromptVersion int        `json:"prompt_version"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	TotalCases    int        `json:"total_cases"`
	PassedCases   int        `json:"passed_cases"`
	FailedCases   int        `json:"failed_cases"`
	ScorePct      *float64   `json:"score_pct,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type EvalResult struct {
	ID           string    `json:"id"`
	RunID        string    `json:"run_id"`
	CaseID       string    `json:"case_id"`
	ActualOutput *string   `json:"actual_output,omitempty"`
	Passed       bool      `json:"passed"`
	Score        *float64  `json:"score,omitempty"`
	LatencyMS    int       `json:"latency_ms"`
	Error        *string   `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type EvalStore struct{ pool *pgxpool.Pool }

func NewEvalStore(pool *pgxpool.Pool) *EvalStore {
	return &EvalStore{pool: pool}
}

func (s *EvalStore) CreateSuite(ctx context.Context, tenantID, name, promptName string) (*EvalSuite, error) {
	suite := &EvalSuite{TenantID: tenantID, Name: name, PromptName: promptName}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO eval_suites (tenant_id, name, prompt_name) VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		tenantID, name, promptName,
	).Scan(&suite.ID, &suite.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("eval_store create_suite: %w", err)
	}
	return suite, nil
}

func (s *EvalStore) ListSuites(ctx context.Context, tenantID string) ([]*EvalSuite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, prompt_name, created_at
		 FROM eval_suites WHERE tenant_id=$1 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("eval_store list_suites: %w", err)
	}
	defer rows.Close()
	var suites []*EvalSuite
	for rows.Next() {
		var suite EvalSuite
		if err := rows.Scan(&suite.ID, &suite.TenantID, &suite.Name, &suite.PromptName, &suite.CreatedAt); err != nil {
			return nil, err
		}
		suites = append(suites, &suite)
	}
	return suites, rows.Err()
}

func (s *EvalStore) GetSuite(ctx context.Context, tenantID, suiteID string) (*EvalSuite, error) {
	var suite EvalSuite
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, prompt_name, created_at
		 FROM eval_suites WHERE id=$1 AND tenant_id=$2`,
		suiteID, tenantID,
	).Scan(&suite.ID, &suite.TenantID, &suite.Name, &suite.PromptName, &suite.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("eval_store get_suite: %w", err)
	}
	return &suite, nil
}

func (s *EvalStore) AddCase(ctx context.Context, suiteID string, vars map[string]any, expected string, contains []string, method string) (*EvalCase, error) {
	if vars == nil {
		vars = map[string]any{}
	}
	if contains == nil {
		contains = []string{}
	}
	var expectedPtr *string
	if expected != "" {
		expectedPtr = &expected
	}
	c := &EvalCase{
		SuiteID:          suiteID,
		InputVars:        vars,
		ExpectedOutput:   expectedPtr,
		ExpectedContains: contains,
		ScoreMethod:      method,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO eval_cases (suite_id, input_vars, expected_output, expected_contains, score_method)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		suiteID, vars, expectedPtr, contains, method,
	).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("eval_store add_case: %w", err)
	}
	return c, nil
}

func (s *EvalStore) ListCases(ctx context.Context, suiteID string) ([]*EvalCase, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, suite_id, input_vars, expected_output, expected_contains, score_method, created_at
		 FROM eval_cases WHERE suite_id=$1 ORDER BY created_at ASC`,
		suiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("eval_store list_cases: %w", err)
	}
	defer rows.Close()
	var cases []*EvalCase
	for rows.Next() {
		var c EvalCase
		if err := rows.Scan(&c.ID, &c.SuiteID, &c.InputVars, &c.ExpectedOutput, &c.ExpectedContains, &c.ScoreMethod, &c.CreatedAt); err != nil {
			return nil, err
		}
		cases = append(cases, &c)
	}
	return cases, rows.Err()
}

func (s *EvalStore) CreateRun(ctx context.Context, suiteID, tenantID, model string, promptVersion int) (*EvalRun, error) {
	run := &EvalRun{
		SuiteID:       suiteID,
		TenantID:      tenantID,
		Model:         model,
		PromptVersion: promptVersion,
		Status:        "pending",
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO eval_runs (suite_id, tenant_id, model, prompt_version)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		suiteID, tenantID, model, promptVersion,
	).Scan(&run.ID, &run.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("eval_store create_run: %w", err)
	}
	return run, nil
}

func (s *EvalStore) UpdateRunStatus(ctx context.Context, runID, status string, passed, failed, total int, scorePct *float64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE eval_runs SET
		   status=$2, passed_cases=$3, failed_cases=$4, total_cases=$5,
		   score_pct=$6,
		   started_at=CASE WHEN started_at IS NULL AND $2='running' THEN NOW() ELSE started_at END,
		   completed_at=CASE WHEN $2 IN ('completed','failed') THEN NOW() ELSE completed_at END
		 WHERE id=$1`,
		runID, status, passed, failed, total, scorePct,
	)
	if err != nil {
		return fmt.Errorf("eval_store update_run_status: %w", err)
	}
	return nil
}

func (s *EvalStore) AddResult(ctx context.Context, runID, caseID string, actual string, passed bool, score *float64, latencyMS int, errMsg string) error {
	var actualPtr *string
	if actual != "" {
		actualPtr = &actual
	}
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO eval_results (run_id, case_id, actual_output, passed, score, latency_ms, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		runID, caseID, actualPtr, passed, score, latencyMS, errPtr,
	)
	if err != nil {
		return fmt.Errorf("eval_store add_result: %w", err)
	}
	return nil
}

func (s *EvalStore) ListRuns(ctx context.Context, suiteID string, limit int) ([]*EvalRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, suite_id, tenant_id, prompt_version, model, status,
		        total_cases, passed_cases, failed_cases, score_pct,
		        started_at, completed_at, created_at
		 FROM eval_runs WHERE suite_id=$1
		 ORDER BY created_at DESC LIMIT $2`,
		suiteID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("eval_store list_runs: %w", err)
	}
	defer rows.Close()
	var runs []*EvalRun
	for rows.Next() {
		var r EvalRun
		if err := rows.Scan(&r.ID, &r.SuiteID, &r.TenantID, &r.PromptVersion, &r.Model, &r.Status,
			&r.TotalCases, &r.PassedCases, &r.FailedCases, &r.ScorePct,
			&r.StartedAt, &r.CompletedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, &r)
	}
	return runs, rows.Err()
}

func (s *EvalStore) GetRunResults(ctx context.Context, runID string) ([]*EvalResult, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, run_id, case_id, actual_output, passed, score, latency_ms, error, created_at
		 FROM eval_results WHERE run_id=$1 ORDER BY created_at ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("eval_store get_run_results: %w", err)
	}
	defer rows.Close()
	var results []*EvalResult
	for rows.Next() {
		var r EvalResult
		if err := rows.Scan(&r.ID, &r.RunID, &r.CaseID, &r.ActualOutput, &r.Passed, &r.Score,
			&r.LatencyMS, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, &r)
	}
	return results, rows.Err()
}

func (s *EvalStore) GetRun(ctx context.Context, runID string) (*EvalRun, error) {
	var r EvalRun
	err := s.pool.QueryRow(ctx,
		`SELECT id, suite_id, tenant_id, prompt_version, model, status,
		        total_cases, passed_cases, failed_cases, score_pct,
		        started_at, completed_at, created_at
		 FROM eval_runs WHERE id=$1`,
		runID,
	).Scan(&r.ID, &r.SuiteID, &r.TenantID, &r.PromptVersion, &r.Model, &r.Status,
		&r.TotalCases, &r.PassedCases, &r.FailedCases, &r.ScorePct,
		&r.StartedAt, &r.CompletedAt, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("eval_store get_run: %w", err)
	}
	return &r, nil
}

// RunTrend holds summary data for a single eval run, used for charting score
// over time and prompt version progression.
type RunTrend struct {
	RunID         string    `json:"run_id"`
	PromptVersion int       `json:"prompt_version"`
	Model         string    `json:"model"`
	ScorePct      float64   `json:"score_pct"`
	PassedCases   int       `json:"passed_cases"`
	TotalCases    int       `json:"total_cases"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListRunTrends returns historical completed runs for a suite ordered by
// created_at ASC, suitable for rendering a score-over-time chart.
func (s *EvalStore) ListRunTrends(ctx context.Context, suiteID string, limit int) ([]*RunTrend, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, prompt_version, model,
		        COALESCE(score_pct, 0), passed_cases, total_cases, created_at
		 FROM eval_runs
		 WHERE suite_id=$1 AND status='completed'
		 ORDER BY created_at ASC
		 LIMIT $2`,
		suiteID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("eval_store list_run_trends: %w", err)
	}
	defer rows.Close()
	var trends []*RunTrend
	for rows.Next() {
		var t RunTrend
		if err := rows.Scan(&t.RunID, &t.PromptVersion, &t.Model, &t.ScorePct,
			&t.PassedCases, &t.TotalCases, &t.CreatedAt); err != nil {
			return nil, err
		}
		trends = append(trends, &t)
	}
	return trends, rows.Err()
}

// GetBestModel returns the model with the highest score_pct among completed
// runs for the given suite. Ties are broken by most recent run.
func (s *EvalStore) GetBestModel(ctx context.Context, suiteID string) (model string, scorePct float64, err error) {
	queryErr := s.pool.QueryRow(ctx,
		`SELECT model, score_pct
		 FROM eval_runs
		 WHERE suite_id=$1 AND status='completed'
		 ORDER BY score_pct DESC, created_at DESC
		 LIMIT 1`,
		suiteID,
	).Scan(&model, &scorePct)
	if queryErr == pgx.ErrNoRows {
		return "", 0, nil
	}
	if queryErr != nil {
		return "", 0, fmt.Errorf("eval_store get_best_model: %w", queryErr)
	}
	return model, scorePct, nil
}
