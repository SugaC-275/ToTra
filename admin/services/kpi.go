package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EfficiencySnapshot struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	UserName          string    `json:"user_name"`
	YearMonth         string    `json:"year_month"`
	TotalSCU          float64   `json:"total_scu"`
	TotalOutputWeight float64   `json:"total_output_weight"`
	EfficiencyScore   float64   `json:"efficiency_score"`
	PeerGroup         string    `json:"peer_group"`
	Rank              int       `json:"rank"`
	PeerCount         int       `json:"peer_count"`
	SnapshotAt        time.Time `json:"snapshot_at"`
}

// ComputeEfficiencyScore calculates efficiency_score = output / log(scu+1).
// Returns 0 if scu=0 (no AI usage) or output=0 (no output events).
func ComputeEfficiencyScore(outputWeight, totalSCU float64) float64 {
	if totalSCU == 0 || outputWeight == 0 {
		return 0
	}
	return outputWeight / math.Log(totalSCU+1)
}

// IsNewEmployee returns true if the user joined fewer than 90 days ago.
func IsNewEmployee(ageDays int) bool {
	return ageDays < 90
}

// CohortGroup returns the peer_group string for a new employee.
func CohortGroup(hireYearMonth string) string {
	return "cohort_" + hireYearMonth
}

type KPIService struct {
	pool *pgxpool.Pool
}

func NewKPIService(pool *pgxpool.Pool) *KPIService {
	return &KPIService{pool: pool}
}

// RunMonthlySnapshot computes and stores efficiency snapshots for all active users
// in a tenant for the given yearMonth (e.g. "2026-04").
func (s *KPIService) RunMonthlySnapshot(ctx context.Context, tenantID, yearMonth string) error {
	startDate := yearMonth + "-01"
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	nextMonth := t.AddDate(0, 1, 0)
	endDate := nextMonth.Format("2006-01") + "-01"

	type userRow struct {
		ID        string
		Name      string
		Role      string
		CreatedAt time.Time
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, role, created_at FROM users WHERE tenant_id=$1 AND is_active=true`,
		tenantID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	var users []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt); err != nil {
			return err
		}
		users = append(users, u)
	}
	rows.Close()

	snapshotAt := time.Now().UTC()
	type snapshotData struct {
		userRow
		TotalSCU     float64
		OutputWeight float64
		PeerGroup    string
	}
	var snapshots []snapshotData

	for _, u := range users {
		var totalSCU float64
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(scu_cost),0) FROM usage_records
			 WHERE tenant_id=$1 AND user_id=$2 AND request_at >= $3 AND request_at < $4`,
			tenantID, u.ID, startDate, endDate,
		).Scan(&totalSCU)

		var outputWeight float64
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(weight),0) FROM output_events
			 WHERE tenant_id=$1 AND user_id=$2 AND occurred_at >= $3 AND occurred_at < $4`,
			tenantID, u.ID, startDate, endDate,
		).Scan(&outputWeight)

		ageDays := int(time.Since(u.CreatedAt).Hours() / 24)
		pg := u.Role
		if IsNewEmployee(ageDays) {
			pg = CohortGroup(u.CreatedAt.Format("2006-01"))
		}

		snapshots = append(snapshots, snapshotData{
			userRow:      u,
			TotalSCU:     totalSCU,
			OutputWeight: outputWeight,
			PeerGroup:    pg,
		})
	}

	// Compute ranks within each peer group
	type peerEntry struct {
		userIdx int
		score   float64
	}
	groups := map[string][]peerEntry{}
	for i, snap := range snapshots {
		score := ComputeEfficiencyScore(snap.OutputWeight, snap.TotalSCU)
		groups[snap.PeerGroup] = append(groups[snap.PeerGroup], peerEntry{i, score})
	}

	ranks := make([]int, len(snapshots))
	for _, entries := range groups {
		// Sort descending by score (insertion sort)
		for i := 1; i < len(entries); i++ {
			for j := i; j > 0 && entries[j].score > entries[j-1].score; j-- {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
		for rank, entry := range entries {
			ranks[entry.userIdx] = rank + 1
		}
	}

	// Upsert snapshots
	for i, snap := range snapshots {
		score := ComputeEfficiencyScore(snap.OutputWeight, snap.TotalSCU)
		peerCount := len(groups[snap.PeerGroup])
		_, err := s.pool.Exec(ctx,
			`INSERT INTO efficiency_snapshots
			 (id, tenant_id, user_id, year_month, total_scu, total_output_weight,
			  efficiency_score, peer_group, rank, peer_count, snapshot_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			 ON CONFLICT (tenant_id, user_id, year_month) DO UPDATE
			 SET total_scu=$5, total_output_weight=$6, efficiency_score=$7,
			     peer_group=$8, rank=$9, peer_count=$10, snapshot_at=$11`,
			uuid.New().String(), tenantID, snap.ID, yearMonth,
			snap.TotalSCU, snap.OutputWeight, score,
			snap.PeerGroup, ranks[i], peerCount, snapshotAt,
		)
		if err != nil {
			return fmt.Errorf("upsert snapshot for user %s: %w", snap.ID, err)
		}
	}
	return nil
}

// GetSnapshots returns KPI snapshots for a tenant for a given month.
func (s *KPIService) GetSnapshots(ctx context.Context, tenantID, yearMonth string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT es.id, es.user_id, u.name, es.year_month,
		        es.total_scu, es.total_output_weight, es.efficiency_score,
		        es.peer_group, es.rank, es.peer_count, es.snapshot_at
		 FROM efficiency_snapshots es
		 JOIN users u ON es.user_id = u.id
		 WHERE es.tenant_id=$1 AND es.year_month=$2
		 ORDER BY es.peer_group, es.rank`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*EfficiencySnapshot
	for rows.Next() {
		snap := &EfficiencySnapshot{}
		if err := rows.Scan(&snap.ID, &snap.UserID, &snap.UserName, &snap.YearMonth,
			&snap.TotalSCU, &snap.TotalOutputWeight, &snap.EfficiencyScore,
			&snap.PeerGroup, &snap.Rank, &snap.PeerCount, &snap.SnapshotAt); err != nil {
			return nil, err
		}
		results = append(results, snap)
	}
	return results, nil
}

// GetMySnapshots returns up to 12 months of snapshots for a user (for growth curve).
func (s *KPIService) GetMySnapshots(ctx context.Context, userID string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, '' AS user_name, year_month,
		        total_scu, total_output_weight, efficiency_score,
		        peer_group, rank, peer_count, snapshot_at
		 FROM efficiency_snapshots WHERE user_id=$1
		 ORDER BY year_month DESC LIMIT 12`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*EfficiencySnapshot
	for rows.Next() {
		snap := &EfficiencySnapshot{}
		if err := rows.Scan(&snap.ID, &snap.UserID, &snap.UserName, &snap.YearMonth,
			&snap.TotalSCU, &snap.TotalOutputWeight, &snap.EfficiencyScore,
			&snap.PeerGroup, &snap.Rank, &snap.PeerCount, &snap.SnapshotAt); err != nil {
			return nil, err
		}
		results = append(results, snap)
	}
	return results, nil
}
