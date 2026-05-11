package services

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HRRecord represents a single row from an HR CSV import.
type HRRecord struct {
	Email      string
	Name       string
	Role       string
	Department string
}

// SyncResult summarises the outcome of a CSV sync operation.
type SyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// validRoles defines the set of roles accepted by ParseHRCSV.
var validRoles = map[string]bool{
	"admin":    true,
	"employee": true,
}

// HRSyncService handles HR CSV import operations.
type HRSyncService struct {
	pool *pgxpool.Pool
}

// NewHRSyncService creates a new HRSyncService backed by the given connection pool.
func NewHRSyncService(pool *pgxpool.Pool) *HRSyncService {
	return &HRSyncService{pool: pool}
}

// ParseHRCSV reads a CSV from r and returns a slice of HRRecord.
// The first row must be a header containing email, name, role, and department
// columns (case-insensitive). Extra columns are silently ignored.
// Returns an error if any required column is missing, if any email is empty,
// or if any role is not in validRoles.
func ParseHRCSV(r io.Reader) ([]HRRecord, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // variable number of fields allowed

	// Read header row.
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("hr_sync: reading header: %w", err)
	}

	// Map column names to indices (case-insensitive).
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	required := []string{"email", "name", "role", "department"}
	for _, col := range required {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("hr_sync: missing required column %q", col)
		}
	}

	emailIdx := colIdx["email"]
	nameIdx := colIdx["name"]
	roleIdx := colIdx["role"]
	deptIdx := colIdx["department"]

	var records []HRRecord
	lineNum := 1
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("hr_sync: reading row %d: %w", lineNum, err)
		}
		lineNum++

		email := strings.TrimSpace(row[emailIdx])
		if email == "" {
			return nil, fmt.Errorf("hr_sync: empty email on row %d", lineNum)
		}

		role := strings.TrimSpace(row[roleIdx])
		if !validRoles[role] {
			return nil, fmt.Errorf("hr_sync: invalid role %q on row %d", role, lineNum)
		}

		records = append(records, HRRecord{
			Email:      email,
			Name:       strings.TrimSpace(row[nameIdx]),
			Role:       role,
			Department: strings.TrimSpace(row[deptIdx]),
		})
	}

	return records, nil
}

// SyncFromCSV upserts each HRRecord for the given tenant.
// New users are inserted with api_key_hash='LOCKED' and auth_type='api_key'.
// Existing users have their name and department updated.
// Errors per row are counted in result.Errors; the method itself never returns
// a non-nil error.
func (s *HRSyncService) SyncFromCSV(ctx context.Context, tenantID string, records []HRRecord) (*SyncResult, error) {
	result := &SyncResult{}

	for _, rec := range records {
		var existingID string
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM users WHERE tenant_id=$1 AND email=$2`,
			tenantID, rec.Email,
		).Scan(&existingID)

		if err != nil {
			// User not found — insert.
			_, insertErr := s.pool.Exec(ctx,
				`INSERT INTO users (tenant_id, email, name, role, department, api_key_hash, auth_type)
				 VALUES ($1, $2, $3, $4, $5, 'LOCKED', 'api_key')
				 ON CONFLICT (tenant_id, email) DO NOTHING`,
				tenantID, rec.Email, rec.Name, rec.Role, nullableStr(rec.Department),
			)
			if insertErr != nil {
				result.Errors++
				continue
			}
			result.Created++
		} else {
			// User exists — update name and department.
			_, updateErr := s.pool.Exec(ctx,
				`UPDATE users SET name=$1, department=$2 WHERE id=$3`,
				rec.Name, nullableStr(rec.Department), existingID,
			)
			if updateErr != nil {
				result.Errors++
				continue
			}
			result.Updated++
		}
	}

	return result, nil
}
