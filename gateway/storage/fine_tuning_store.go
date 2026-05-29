package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FineTuningJob struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id,omitempty"`
	UserID         string    `json:"user_id,omitempty"`
	ModelConfigID  string    `json:"model_config_id,omitempty"`
	BaseModel      string    `json:"model"`
	TrainingFile   string    `json:"training_file"`
	ValidationFile string    `json:"validation_file,omitempty"`
	Status         string    `json:"status"`
	FineTunedModel string    `json:"fine_tuned_model,omitempty"`
	ErrorMessage   string    `json:"error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type FineTuningEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type FineTuningStore struct{ pool *pgxpool.Pool }

func NewFineTuningStore(pool *pgxpool.Pool) *FineTuningStore {
	return &FineTuningStore{pool: pool}
}

func (s *FineTuningStore) Create(ctx context.Context, j *FineTuningJob) error {
	var validFile *string
	if j.ValidationFile != "" {
		validFile = &j.ValidationFile
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO fine_tuning_jobs (id, tenant_id, user_id, model_config_id, base_model, training_file, validation_file, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'queued')`,
		j.ID, j.TenantID, j.UserID, j.ModelConfigID, j.BaseModel, j.TrainingFile, validFile,
	)
	if err != nil {
		return fmt.Errorf("fine_tuning_store create: %w", err)
	}
	return nil
}

func (s *FineTuningStore) Get(ctx context.Context, tenantID, jobID string) (*FineTuningJob, error) {
	var j FineTuningJob
	var validFile, fineTunedModel, errMsg *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, base_model, training_file,
		        validation_file, status, fine_tuned_model, error_message, created_at, updated_at
		 FROM fine_tuning_jobs WHERE id = $1 AND tenant_id = $2`,
		jobID, tenantID,
	).Scan(&j.ID, &j.TenantID, &j.UserID, &j.ModelConfigID, &j.BaseModel, &j.TrainingFile,
		&validFile, &j.Status, &fineTunedModel, &errMsg, &j.CreatedAt, &j.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fine_tuning_store get: %w", err)
	}
	if validFile != nil {
		j.ValidationFile = *validFile
	}
	if fineTunedModel != nil {
		j.FineTunedModel = *fineTunedModel
	}
	if errMsg != nil {
		j.ErrorMessage = *errMsg
	}
	return &j, nil
}

func (s *FineTuningStore) List(ctx context.Context, tenantID string, limit int) ([]*FineTuningJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, model_config_id, base_model, training_file,
		        COALESCE(validation_file,''), status, COALESCE(fine_tuned_model,''),
		        COALESCE(error_message,''), created_at, updated_at
		 FROM fine_tuning_jobs WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("fine_tuning_store list: %w", err)
	}
	defer rows.Close()
	var jobs []*FineTuningJob
	for rows.Next() {
		var j FineTuningJob
		if err := rows.Scan(&j.ID, &j.TenantID, &j.UserID, &j.ModelConfigID, &j.BaseModel,
			&j.TrainingFile, &j.ValidationFile, &j.Status, &j.FineTunedModel,
			&j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *FineTuningStore) UpdateStatus(ctx context.Context, jobID, status, fineTunedModel, errMsg string) error {
	var ftm, em *string
	if fineTunedModel != "" {
		ftm = &fineTunedModel
	}
	if errMsg != "" {
		em = &errMsg
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE fine_tuning_jobs SET status=$1, fine_tuned_model=$2, error_message=$3, updated_at=NOW() WHERE id=$4`,
		status, ftm, em, jobID,
	)
	return err
}

func (s *FineTuningStore) Cancel(ctx context.Context, tenantID, jobID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE fine_tuning_jobs SET status='cancelled', updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND status IN ('queued','validating_files','running')`,
		jobID, tenantID,
	)
	return err
}

func (s *FineTuningStore) ListRunning(ctx context.Context) ([]*FineTuningJob, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, model_config_id, base_model, status
		 FROM fine_tuning_jobs WHERE status IN ('queued','validating_files','running')`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []*FineTuningJob
	for rows.Next() {
		var j FineTuningJob
		if err := rows.Scan(&j.ID, &j.TenantID, &j.ModelConfigID, &j.BaseModel, &j.Status); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *FineTuningStore) AddEvent(ctx context.Context, jobID, level, message string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO fine_tuning_events (job_id, level, message) VALUES ($1, $2, $3)`,
		jobID, level, message,
	)
	return err
}

func (s *FineTuningStore) ListEvents(ctx context.Context, tenantID, jobID string) ([]*FineTuningEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT e.id, e.job_id, e.level, e.message, e.created_at
		 FROM fine_tuning_events e
		 JOIN fine_tuning_jobs j ON j.id = e.job_id
		 WHERE j.id = $1 AND j.tenant_id = $2
		 ORDER BY e.created_at ASC`,
		jobID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*FineTuningEvent
	for rows.Next() {
		var ev FineTuningEvent
		if err := rows.Scan(&ev.ID, &ev.JobID, &ev.Level, &ev.Message, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &ev)
	}
	return events, rows.Err()
}
